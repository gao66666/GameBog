"""工具调用准确性：工具名、参数摘要、tool_retrieve 候选集。"""

from __future__ import annotations

import re
from typing import Any


def _tools(turn: dict[str, Any]) -> list[dict[str, Any]]:
    raw = turn.get("tools")
    return [t for t in raw if isinstance(t, dict)] if isinstance(raw, list) else []


def _tool_retrieve_names(turn: dict[str, Any]) -> list[str]:
    tr = turn.get("tool_retrieve")
    if not isinstance(tr, dict):
        return []
    names: list[str] = []
    for item in tr.get("tools") or []:
        if isinstance(item, str):
            names.append(item)
        elif isinstance(item, dict) and item.get("name"):
            names.append(str(item["name"]))
        elif isinstance(item, dict) and item.get("tool"):
            names.append(str(item["tool"]))
    return names


def _match_args(actual: dict[str, Any], rules: dict[str, Any]) -> list[str]:
    failures: list[str] = []
    for key, rule in (rules or {}).items():
        if key == "keys_present":
            for k in rule if isinstance(rule, list) else []:
                if k not in actual:
                    failures.append(f"args missing key {k!r}")
            continue
        elif key.endswith("_present"):
            field = key[: -len("_present")]
            if field not in actual or actual[field] in (None, ""):
                failures.append(f"args.{field} not present")
        elif key.endswith("_contains"):
            field = key[: -len("_contains")]
            want = str(rule).lower()
            got = str(actual.get(field, "")).lower()
            if want not in got:
                failures.append(f"args.{field} missing substring {rule!r} (got {actual.get(field)!r})")
        elif key.endswith("_regex"):
            field = key[: -len("_regex")]
            pat = str(rule)
            got = str(actual.get(field, ""))
            if not re.search(pat, got, re.I):
                failures.append(f"args.{field} !~ {pat!r} (got {got!r})")
    return failures


def score_tool_accuracy(turn: dict[str, Any], expect: dict[str, Any]) -> dict[str, Any]:
    """expect 字段见 cases/tool_cases.yaml 注释。"""
    failures: list[str] = []
    metrics: dict[str, Any] = {}

    tools = _tools(turn)
    names = [str(t.get("tool", "")) for t in tools if t.get("tool")]

    must_first = str(expect.get("tool_must_first", "")).strip()
    if must_first:
        metrics["tool_first_match"] = bool(names) and names[0] == must_first
        if not metrics["tool_first_match"]:
            failures.append(f"first tool={names[:1]!r} want {must_first!r}")
    else:
        metrics["tool_first_match"] = True

    must_all = [str(x) for x in (expect.get("tools_must_include") or [])]
    if must_all:
        metrics["tools_must_include"] = all(n in names for n in must_all)
        if not metrics["tools_must_include"]:
            failures.append(f"tools={names} must include {must_all}")
    else:
        metrics["tools_must_include"] = True

    must_exc = [str(x) for x in (expect.get("tools_must_exclude") or [])]
    if must_exc:
        metrics["tools_must_exclude"] = all(n not in names for n in must_exc)
        if not metrics["tools_must_exclude"]:
            failures.append(f"tools={names} must exclude {must_exc}")
    else:
        metrics["tools_must_exclude"] = True

    max_calls = expect.get("max_tool_calls")
    if max_calls is not None:
        metrics["tool_calls_within_max"] = len(names) <= int(max_calls)
        if not metrics["tool_calls_within_max"]:
            failures.append(f"tool_calls={len(names)} > max={max_calls}")
    else:
        metrics["tool_calls_within_max"] = True

    metrics["tool_call_count"] = len(names)

    # 按工具名匹配 args 规则（对第一次出现的该工具）
    checks = expect.get("tool_args") or {}
    metrics["tool_args_match"] = True
    if isinstance(checks, dict) and checks:
        for tool_name, rules in checks.items():
            if not isinstance(rules, dict):
                continue
            matched = next((t for t in tools if str(t.get("tool")) == tool_name), None)
            if not matched:
                metrics["tool_args_match"] = False
                failures.append(f"no invocation for tool {tool_name!r} to check args")
                continue
            args = matched.get("args_summary") if isinstance(matched.get("args_summary"), dict) else {}
            arg_fail = _match_args(args, rules)
            if arg_fail:
                metrics["tool_args_match"] = False
                failures.extend(arg_fail)

    retrieve_any = [str(x) for x in (expect.get("tool_retrieve_must_include") or [])]
    retrieved = _tool_retrieve_names(turn)
    if retrieve_any:
        metrics["tool_retrieve_match"] = any(n in retrieved for n in retrieve_any)
        if not metrics["tool_retrieve_match"]:
            failures.append(f"tool_retrieve={retrieved} want any of {retrieve_any}")
    else:
        metrics["tool_retrieve_match"] = True

    passed = len(failures) == 0
    return {"passed": passed, "failures": failures, "metrics": metrics}
