"""配置加载模块 —— 从 agent/config/config.yaml 读取 Agent 配置"""

import os
import yaml

_AGENT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
_CONFIG_YAML = os.path.join(_AGENT_ROOT, "config", "config.yaml")
_PROMPTS_YAML = os.path.join(_AGENT_ROOT, "config", "prompts.yaml")

_CONFIG = None

_PROMPTS_CACHE: dict | None = None
_PROMPTS_MTIME: float | None = None

_REQUIRED_PROMPT_KEYS = (
    "routing",
    "system",
    "execute_system",
    "analyse",
    "plan",
    "output",
    "server_summaries",
)


def _validate_prompts_blob(blob: dict) -> bool:
    if not isinstance(blob, dict):
        return False
    for k in _REQUIRED_PROMPT_KEYS:
        if k == "server_summaries":
            ss = blob.get("server_summaries")
            if not isinstance(ss, dict):
                return False
            for g in ("public", "user"):
                meta = ss.get(g)
                if not isinstance(meta, dict):
                    return False
                if not str(meta.get("description", "")).strip():
                    return False
            continue
        v = blob.get(k)
        if not isinstance(v, str) or not v.strip():
            return False
    ap = blob.get("analyse", "")
    if "{tools_short_catalog}" not in ap:
        return False
    pl = blob.get("plan", "")
    if "{tools_detail}" not in pl or "{analyse_json}" not in pl:
        return False
    out = blob.get("output", "")
    if "{analyse_json}" not in out or "final_reply" not in out:
        return False
    return True


def get_prompts() -> dict:
    """读取 agent/config/prompts.yaml；按文件 mtime 热更新（无需重启进程）。返回只读 dict，请勿原地修改。"""
    global _PROMPTS_CACHE, _PROMPTS_MTIME
    try:
        mtime = os.path.getmtime(_PROMPTS_YAML)
    except OSError:
        if _PROMPTS_CACHE is not None:
            return _PROMPTS_CACHE
        raise FileNotFoundError(f"缺少提示词文件: {_PROMPTS_YAML}") from None

    if _PROMPTS_CACHE is not None and _PROMPTS_MTIME == mtime:
        return _PROMPTS_CACHE

    try:
        with open(_PROMPTS_YAML, "r", encoding="utf-8") as f:
            loaded = yaml.safe_load(f) or {}
    except Exception:
        if _PROMPTS_CACHE is not None:
            return _PROMPTS_CACHE
        raise

    if not _validate_prompts_blob(loaded):
        if _PROMPTS_CACHE is not None:
            return _PROMPTS_CACHE
        raise ValueError("prompts.yaml 结构不完整或字段为空，请对照仓库内默认 prompts.yaml 检查")

    _PROMPTS_CACHE = loaded
    _PROMPTS_MTIME = mtime
    return _PROMPTS_CACHE


def _resolve_key(block: dict, *env_names: str, default: str = "") -> str:
    for name in env_names:
        val = (os.getenv(name) or "").strip()
        if val:
            return val
    return str(block.get("api_key") or default).strip()


def load_config() -> dict:
    """加载 agent/config/config.yaml 中的 agent 配置"""
    global _CONFIG
    if _CONFIG is not None:
        return _CONFIG

    yaml_path = _CONFIG_YAML

    with open(yaml_path, "r", encoding="utf-8") as f:
        data = yaml.safe_load(f)

    llm_cfg = data.get("llm", {})
    summary_llm_cfg = data.get("summary_llm", {})
    memory_manager_llm_cfg = data.get("memory_manager_llm", {})
    decision_llm_cfg = data.get("decision_llm", {})
    redis_cfg = data.get("redis", {})
    qdrant_cfg = data.get("qdrant", {})
    mongo_cfg = data.get("mongo", {})
    memory_cfg = data.get("memory", {})
    orchestration_cfg = data.get("orchestration", {})
    turn_audit_cfg = data.get("turn_audit", {})
    embed_cfg = data.get("embedding", {})
    blog_api_url = data.get("blog_api_url", "http://127.0.0.1:8084/api/v1")

    document_ingest_secret = (os.getenv("DOCUMENT_INGEST_SECRET") or "").strip() or data.get(
        "document_ingest_secret", ""
    )

    llm_key = _resolve_key(llm_cfg, "DEEPSEEK_API_KEY", "LLM_API_KEY", "OPENAI_API_KEY")
    summary_key = _resolve_key(summary_llm_cfg, "DEEPSEEK_API_KEY", "LLM_API_KEY") or llm_key
    memory_mgr_key = _resolve_key(memory_manager_llm_cfg, "DEEPSEEK_API_KEY", "LLM_API_KEY") or summary_key
    decision_key = _resolve_key(decision_llm_cfg, "DEEPSEEK_API_KEY", "LLM_API_KEY") or memory_mgr_key
    embed_key = _resolve_key(embed_cfg, "ZHIPU_API_KEY", "EMBEDDING_API_KEY", "OPENAI_API_KEY")

    _CONFIG = {
        "base_url": llm_cfg.get("base_url", "https://api.openai.com/v1"),
        "api_key": llm_key or llm_cfg.get("api_key", "sk-your-key-here"),
        "model": llm_cfg.get("model", "gpt-4o-mini"),
        "temperature": llm_cfg.get("temperature", 0.7),
        "thinking": llm_cfg.get("thinking", False),
        "blog_api_url": blog_api_url,
        "summary_llm": {
            "base_url": summary_llm_cfg.get("base_url", llm_cfg.get("base_url", "https://api.openai.com/v1")),
            "api_key": summary_key or summary_llm_cfg.get("api_key", llm_key),
            "model": summary_llm_cfg.get("model", llm_cfg.get("model", "gpt-4o-mini")),
            "temperature": summary_llm_cfg.get("temperature", 0),
            "thinking": summary_llm_cfg.get("thinking", False),
        },
        "memory_manager_llm": {
            "base_url": memory_manager_llm_cfg.get("base_url", summary_llm_cfg.get("base_url", llm_cfg.get("base_url", "https://api.openai.com/v1"))),
            "api_key": memory_mgr_key or memory_manager_llm_cfg.get("api_key", summary_key),
            "model": memory_manager_llm_cfg.get("model", summary_llm_cfg.get("model", llm_cfg.get("model", "gpt-4o-mini"))),
            "temperature": memory_manager_llm_cfg.get("temperature", 0),
            "thinking": memory_manager_llm_cfg.get("thinking", False),
        },
        "redis": {
            "host": redis_cfg.get("host", "localhost"),
            "port": redis_cfg.get("port", 6379),
            "password": redis_cfg.get("password", ""),
            "db": redis_cfg.get("db", 0),
        },
        "qdrant": {
            "url": qdrant_cfg.get("url", "http://127.0.0.1:6333"),
            "user_memory_collection": qdrant_cfg.get("user_memory_collection", "goblog_user_memory"),
            "kb_collection": qdrant_cfg.get("kb_collection", "goblog_kb_documents"),
            "collection": qdrant_cfg.get("collection"),
            "vector_size": qdrant_cfg.get("vector_size", 1024),
        },
        "embedding": {
            "base_url": embed_cfg.get("base_url", llm_cfg.get("base_url", "https://open.bigmodel.cn/api/paas/v4")),
            "api_key": embed_key or embed_cfg.get("api_key", llm_cfg.get("api_key", "")),
            "model": embed_cfg.get("model", "embedding-3"),
            "dimensions": int(embed_cfg["dimensions"]) if embed_cfg.get("dimensions") else 1024,
        },
        "mongo": {
            "uri": mongo_cfg.get("uri", "mongodb://root:123456@localhost:27017/?authSource=admin"),
            "database": mongo_cfg.get("database", "goblog_long_term"),
            "collection": mongo_cfg.get("collection", "memory_facts"),
        },
        "memory": {
            "short_ttl_minutes": memory_cfg.get("short_ttl_minutes", 60),
            "mid_ttl_days": memory_cfg.get("mid_ttl_days", 7),
            "mid_max_items": memory_cfg.get("mid_max_items", 8),
            "mid_facts_max_chars": int(memory_cfg.get("mid_facts_max_chars", 560)),
            "mid_facts_max_items": int(memory_cfg.get("mid_facts_max_items", 10)),
            "mid_fact_max_chars_per_item": int(memory_cfg.get("mid_fact_max_chars_per_item", 180)),
            "short_window_messages": memory_cfg.get("short_window_messages", 12),
            "long_term_retrieval_limit": int(memory_cfg.get("long_term_retrieval_limit", 10)),
            "mongo_rule_preference_limit": int(memory_cfg.get("mongo_rule_preference_limit", 5)),
            "mongo_retrieval_limit": int(
                memory_cfg.get("mongo_retrieval_limit", memory_cfg.get("mongo_rule_preference_limit", 5))
            ),
            "session_idle_timeout_minutes": memory_cfg.get("session_idle_timeout_minutes", 30),
            "summary_enabled": memory_cfg.get("summary_enabled", True),
            "hybrid_search": memory_cfg.get("hybrid_search", True),
            "hybrid_prefetch_limit": memory_cfg.get("hybrid_prefetch_limit", 48),
            "hybrid_max_union_points": memory_cfg.get("hybrid_max_union_points", 200),
            "hybrid_dense_weight": memory_cfg.get("hybrid_dense_weight", 0.55),
            "hybrid_keyword_weight": memory_cfg.get("hybrid_keyword_weight", 0.45),
            "hybrid_keyword_recall": memory_cfg.get("hybrid_keyword_recall", True),
            "hybrid_rrf_k": memory_cfg.get("hybrid_rrf_k"),
            "hint_rerank_enabled": bool(memory_cfg.get("hint_rerank_enabled", True)),
            "hint_rerank_hint_weight": float(memory_cfg.get("hint_rerank_hint_weight", 0.65)),
            "hint_rerank_bm25_weight": float(memory_cfg.get("hint_rerank_bm25_weight", 0.35)),
            "mongo_retrieval_candidate_cap": int(memory_cfg.get("mongo_retrieval_candidate_cap", 80)),
            "memory_hints_max_items": int(memory_cfg.get("memory_hints_max_items", 8)),
            "memory_hints_max_chars": int(memory_cfg.get("memory_hints_max_chars", 64)),
        },
        "document_ingest_secret": document_ingest_secret,
        "orchestration": {
            "post_turn_async_log": bool(orchestration_cfg.get("post_turn_async_log", True)),
            "tool_retrieval_enabled": bool(orchestration_cfg.get("tool_retrieval_enabled", True)),
            "tool_retrieval_top_k": int(orchestration_cfg.get("tool_retrieval_top_k", 10)),
            "tool_retrieval_min_pool_for_skip": int(
                orchestration_cfg.get("tool_retrieval_min_pool_for_skip", 4)
            ),
            "tool_retrieval_dense_weight": float(
                orchestration_cfg.get("tool_retrieval_dense_weight", 0.65)
            ),
            "tool_retrieval_keyword_weight": float(
                orchestration_cfg.get("tool_retrieval_keyword_weight", 0.35)
            ),
            "tool_retrieval_fallback_all": bool(orchestration_cfg.get("tool_retrieval_fallback_all", True)),
            "dynamic_tool_injection": bool(orchestration_cfg.get("dynamic_tool_injection", True)),
            "max_orchestration_cycles": int(orchestration_cfg.get("max_orchestration_cycles", 3)),
        },
        "turn_audit": {
            "sample_size": int(turn_audit_cfg.get("sample_size", 5)),
            "max_turn_json_chars": int(turn_audit_cfg.get("max_turn_json_chars", 14000)),
            "model": turn_audit_cfg.get("model"),
            "base_url": turn_audit_cfg.get("base_url"),
            "api_key": turn_audit_cfg.get("api_key"),
            "thinking": turn_audit_cfg.get("thinking"),
        },
        "decision_llm": {
            "base_url": decision_llm_cfg.get("base_url", memory_manager_llm_cfg.get("base_url", summary_llm_cfg.get("base_url", llm_cfg.get("base_url", "https://api.openai.com/v1")))),
            "api_key": decision_key or decision_llm_cfg.get("api_key", memory_mgr_key),
            "model": decision_llm_cfg.get("model", "glm-4-air"),
            "temperature": decision_llm_cfg.get("temperature", 0),
            "thinking": decision_llm_cfg.get("thinking", False),
        },
    }
    # 与 Go 博客共用同一 Redis 时，用环境变量指向同一 host（Docker 内一般为 redis，宿主机为 localhost）
    if os.getenv("REDIS_HOST"):
        _CONFIG["redis"]["host"] = os.getenv("REDIS_HOST").strip()
    if os.getenv("REDIS_PORT"):
        try:
            _CONFIG["redis"]["port"] = int(os.getenv("REDIS_PORT", "6379"))
        except ValueError:
            pass
    if os.getenv("REDIS_PASSWORD") is not None:
        _CONFIG["redis"]["password"] = os.getenv("REDIS_PASSWORD", "")
    if os.getenv("REDIS_DB"):
        try:
            _CONFIG["redis"]["db"] = int(os.getenv("REDIS_DB", "0"))
        except ValueError:
            pass
    return _CONFIG
