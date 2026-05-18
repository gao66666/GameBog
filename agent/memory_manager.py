"""
长期记忆管理器。

职责：
1. 单次 Memory Manager LLM：**门控是否值得写** + **抽取**原子条目（mongo/qdrant）
2. 单次 decision_llm：**与已有记忆对比后决策**（add/overwrite/ignore 等）并落库
3. MongoDB 结构化事实、Qdrant 向量经验；会话结束等入口见 ``maybe_commit_long_term``
"""

from __future__ import annotations

import asyncio
import hashlib
import json
import math
import re
import zlib
import uuid
from datetime import datetime, timezone
from typing import Any

from langchain_openai import ChatOpenAI, OpenAIEmbeddings
from pymongo import MongoClient, UpdateOne
from qdrant_client import QdrantClient
from qdrant_client.http import models as rest

from agent_log import get_logger

_log = get_logger("memory_manager")


def _lt_preview(text: str, n: int = 120) -> str:
    """日志里预览一段用户话，去换行、截断。"""
    s = (text or "").replace("\n", " ").strip()
    if len(s) <= n:
        return s
    return s[: n - 3] + "..."


# 全局知识库（Wiki/读入文档）向量：user_id=0 + memory_scope=document，与用户对话抽取向量区分
KB_USER_ID = 0
MEMORY_SCOPE_DIALOGUE = "dialogue"
MEMORY_SCOPE_DOCUMENT = "document"

# 与 docker-compose 中 qdrant/qdrant:v1.10.1 及 qdrant-client>=1.10 的 Query API（prefetch + RRF）对齐
QDRANT_DENSE_VECTOR_NAME = "dense"
QDRANT_SPARSE_VECTOR_NAME = "sparse"
_SPARSE_INDEX_SPACE = (1 << 22) - 2  # 约 4M 桶，索引保持 >0

try:
    from config import load_config
except ImportError:  # pragma: no cover - 兼容按包导入的场景
    from .config import load_config


_MONGO_CLIENT: MongoClient | None = None
_QDRANT_CLIENT: QdrantClient | None = None
_EMBEDDINGS: OpenAIEmbeddings | None = None
_MANAGER_LLM: ChatOpenAI | None = None
_DECISION_LLM: ChatOpenAI | None = None


# 强制进入「尽力抽取」的 reason：不再做 worth_storing 门控（与旧版「触发器恒 true」一致）。
_FORCE_LONG_TERM_EXTRACT_REASONS = frozenset(
    {"explicit_remember", "session_end", "timeout", "ws_close"}
)

# 单次 LLM：门控「是否值得写」+ 原子条目抽取（决策与落库仍由 _call_decision_llm + _apply_* 负责）。
LONG_TERM_GATE_AND_EXTRACT_PROMPT = """你是 Memory Manager Agent：在一次输出里完成两件事——
（1）判断本轮是否**值得**进入长期记忆管线（仅当 reason 为 auto 等**普通聊天**时生效）；
（2）若值得（或 reason 要求强制抽取），从上下文中抽出「值得长期保留」的原子条目。

## 门控 worth_storing（仅当 reason **不是** session_end / timeout / ws_close / explicit_remember 时你必须认真判断）
- **worth_storing=true**：用户要求记住；或用户说了关于自己的相对稳定信息（职业、身份、兴趣、习惯、规则、偏好、经历片段等）；**略沾边就 true**，让下游决策模型处理重复与冲突。
- **worth_storing=false**：纯寒暄；一次性解题/翻译/写代码且与用户长期画像无关；临时指令且无稳定偏好。**false 时 items 必须为空数组**。

当 reason **属于** session_end / timeout / ws_close / explicit_remember：**忽略 worth_storing 的语义**（仍可填），必须依据 recent_messages、mid_summaries、user_message、assistant_text **尽力抽取**；仅当绝对没有任何可写长期事实时 items 才可为空。

## 条目怎么拆（worth 为 true 或强制 reason 时）
- 用户自我介绍、职业、兴趣、习惯、规则、稳定偏好 → 一般 **mongo**（profile / preference / rule 等 kind）。
- 经历、案例、较长叙事 → **qdrant**（experience / case / background）。
- 用户明确「希望你记住」→ 写成具体条目。

## 输出（严格 JSON，勿 markdown）
{
  "worth_storing": true,
  "gate_reason": "20字内，说明门控或强制模式下的判断",
  "items": [
    {
      "store": "mongo|qdrant",
      "kind": "rule|profile|status|preference|experience|case|background",
      "title": "短标题",
      "content": "一条清晰的原子事实",
      "tags": ["可选"],
      "importance": 1,
      "confidence": 0.6,
      "evidence": "依据上下文哪一句"
    }
  ]
}
同类合并，**items 最多 5 条**。若 worth_storing 为 false（且非强制 reason），items 必须为 []。
"""


DECISION_PROMPT = """你是长期记忆决策器。根据新记忆条目和已有长期记忆，决定每条新记忆的操作。

已有 MongoDB 记忆（键值对，每项含 fingerprint 用于精确定位）：
已有 Qdrant 记忆（向量相似条目，每项含 point_id 和加权分数 weighted_score，分数已综合相似度×置信度×时间衰减）：

判定规则：
1. MongoDB 条目四种操作：
   - "add"：新增（新内容与所有已有记忆完全不相干）
   - "overwrite"：覆盖已有条目（必须提供 target_fingerprint，用新内容替换旧条目）
   - "delete"：标记删除已有条目（必须提供 target_fingerprint，该条目不再有效）
   - "ignore"：新内容与已有条目本质相同，无需任何操作

2. Qdrant 条目三种操作：
   - "add"：新增（新内容与所有已有向量条目完全不相干）
   - "lower_confidence"：降低已有条目的置信度（新内容与已有条目相反或冲突，必须提供 target_point_id）
   - "ignore"：新内容与已有条目本质相同，无需处理

3. MongoDB 键值分为三个大类（通过 kind 字段区分）：
   - profile（身份）：用户的基本设定，如姓名、职业、背景
   - rule（规范）：最高优先级约束，用户明确要求的硬性规则
   - preference（偏好）：建议性偏好，用户倾向但不强制

4. 判定逻辑：
   - 新内容与所有已有内容完全不相干 → add
   - 新内容与某条已有内容相反/冲突/更新 → overwrite 或 delete（Mongo）/ lower_confidence（Qdrant）
   - 新内容与某条已有内容本质相同（意思一样） → ignore
   - 对于 Mongo，优先 overwrite 而非 delete+add

只返回严格 JSON，格式如下：
{
  "decisions": [
    {
      "store": "mongo",
      "action": "add|overwrite|delete|ignore",
      "new_item_index": 0,
      "target_fingerprint": "覆盖或删除时必填，填已有条目的 fingerprint",
      "reason": "简短理由"
    }
  ]
}
如果所有新条目都不需要任何操作，返回 {"decisions": []}。
"""


async def init_memory_services() -> None:
    """初始化 MongoDB / Qdrant / Embeddings / LLM 客户端。"""
    _mongo_cfg = load_config().get("mongo", {})
    _qdrant_cfg = load_config().get("qdrant", {})
    _manager_cfg = load_config().get("memory_manager_llm", {})
    _embed_cfg = load_config().get("embedding", {})

    global _MONGO_CLIENT, _QDRANT_CLIENT, _EMBEDDINGS, _MANAGER_LLM, _DECISION_LLM
    if _MONGO_CLIENT is None:
        _MONGO_CLIENT = MongoClient(_mongo_cfg.get("uri"))
    if _QDRANT_CLIENT is None:
        _QDRANT_CLIENT = QdrantClient(url=_qdrant_cfg.get("url"))
    if _EMBEDDINGS is None:
        embed_kwargs = dict(
            model=_embed_cfg.get("model", "embedding-3"),
            base_url=_embed_cfg.get("base_url", _manager_cfg.get("base_url")),
            api_key=_embed_cfg.get("api_key", _manager_cfg.get("api_key")),
        )
        dimensions = _embed_cfg.get("dimensions")
        if dimensions and int(dimensions) > 0:
            embed_kwargs["dimensions"] = int(dimensions)
        _EMBEDDINGS = OpenAIEmbeddings(**embed_kwargs)
    if _MANAGER_LLM is None:
        llm_kwargs = dict(
            model=_manager_cfg.get("model", "gpt-4o-mini"),
            base_url=_manager_cfg.get("base_url"),
            api_key=_manager_cfg.get("api_key"),
            temperature=_manager_cfg.get("temperature", 0),
        )
        if _manager_cfg.get("thinking"):
            llm_kwargs["extra_body"] = {"thinking": {"type": "enabled"}}
        else:
            llm_kwargs["extra_body"] = {"thinking": {"type": "disabled"}}
        _MANAGER_LLM = ChatOpenAI(**llm_kwargs)

    decision_cfg = load_config().get("decision_llm", {})
    if _DECISION_LLM is None:
        dec_kwargs = dict(
            model=decision_cfg.get("model", "glm-4-air"),
            base_url=decision_cfg.get("base_url", _manager_cfg.get("base_url")),
            api_key=decision_cfg.get("api_key", _manager_cfg.get("api_key")),
            temperature=decision_cfg.get("temperature", 0),
        )
        if decision_cfg.get("thinking"):
            dec_kwargs["extra_body"] = {"thinking": {"type": "enabled"}}
        else:
            dec_kwargs["extra_body"] = {"thinking": {"type": "disabled"}}
        _DECISION_LLM = ChatOpenAI(**dec_kwargs)

    await _ensure_qdrant_collection()


def _read_qdrant_dense_vector_size(collection_name: str) -> int | None:
    """读取命名向量 dense 或旧版单向量的维度；无法读取时返回 None。"""
    if _QDRANT_CLIENT is None:
        return None
    try:
        info = _QDRANT_CLIENT.get_collection(collection_name=collection_name)
        v = info.config.params.vectors
        if isinstance(v, dict):
            d = v.get(QDRANT_DENSE_VECTOR_NAME)
            if d is not None and hasattr(d, "size"):
                return int(d.size)
            return None
        if v is not None and hasattr(v, "size"):
            return int(v.size)
    except Exception:
        pass
    return None


def _collection_has_dense_sparse_vectors(collection_name: str) -> bool:
    """是否为「dense + sparse」命名向量结构（供 hybrid RRF）。"""
    if _QDRANT_CLIENT is None:
        return False
    try:
        info = _QDRANT_CLIENT.get_collection(collection_name=collection_name)
        params = info.config.params
        vecs = params.vectors
        sparse = getattr(params, "sparse_vectors", None) or {}
        if not isinstance(vecs, dict) or QDRANT_DENSE_VECTOR_NAME not in vecs:
            return False
        if not isinstance(sparse, dict) or QDRANT_SPARSE_VECTOR_NAME not in sparse:
            return False
        return True
    except Exception:
        return False


def _ensure_qdrant_collection_core() -> None:
    """确保长期记忆用 Qdrant 集合存在：命名 dense + sparse；维度或 schema 不符则删库重建。"""
    cfg = load_config().get("qdrant", {})
    collection = cfg.get("collection", "goblog_long_term_memory")
    vector_size = int(cfg.get("vector_size", 1536))

    if _QDRANT_CLIENT is None:
        return

    try:
        exists = _QDRANT_CLIENT.collection_exists(collection)
    except Exception:
        exists = False

    def _create_named_dense_sparse() -> None:
        _QDRANT_CLIENT.create_collection(
            collection_name=collection,
            vectors_config={
                QDRANT_DENSE_VECTOR_NAME: rest.VectorParams(size=vector_size, distance=rest.Distance.COSINE),
            },
            sparse_vectors_config={
                QDRANT_SPARSE_VECTOR_NAME: rest.SparseVectorParams(),
            },
        )

    if not exists:
        try:
            _create_named_dense_sparse()
            _log.info(
                f"Qdrant 已创建集合「{collection}」dense={vector_size} 维 + sparse（docker 镜像建议 qdrant v1.10+）"
            )
        except Exception as e:
            _log.warning(
                f"Qdrant 创建集合失败 collection={collection} err={str(e)[:200]}"
            )
    else:
        actual = _read_qdrant_dense_vector_size(collection)
        schema_ok = _collection_has_dense_sparse_vectors(collection)
        if not schema_ok or actual is None or actual != vector_size:
            reason = []
            if not schema_ok:
                reason.append("非 dense+sparse 命名向量结构")
            if actual is None:
                reason.append("无法读取 dense 维度")
            elif actual != vector_size:
                reason.append(f"维度 {actual}≠config({vector_size})")
            _log.warning(
                f"Qdrant 集合「{collection}」需重建（{'；'.join(reason)}）；"
                f"将删除该集合并写入新 schema（数据清空）。"
            )
            try:
                _QDRANT_CLIENT.delete_collection(collection_name=collection)
            except Exception as e:
                _log.error(f"Qdrant 删除集合「{collection}」失败: {str(e)[:300]}")
                return
            try:
                _create_named_dense_sparse()
                _log.info(f"Qdrant 已重建集合「{collection}」dense={vector_size} + sparse")
            except Exception as e:
                _log.error(f"Qdrant 重建集合失败 collection={collection} err={str(e)[:300]}")
                return


def _ensure_qdrant_collection_sync() -> None:
    """供同步路径（如 Qdrant upsert）在初始化 collection 时调用。"""
    _ensure_qdrant_collection_core()


async def _ensure_qdrant_collection() -> None:
    await asyncio.to_thread(_ensure_qdrant_collection_core)


def _normalize_extracted_items(items: Any) -> list[dict[str, Any]]:
    """将模型输出的 items 列表规范为内部条目结构。"""
    if not isinstance(items, list):
        return []
    normalized: list[dict[str, Any]] = []
    for item in items[:5]:
        if not isinstance(item, dict):
            continue
        store = item.get("store")
        content = str(item.get("content", "")).strip()
        title = str(item.get("title", "")).strip()
        if store not in {"mongo", "qdrant"} or not content:
            continue
        normalized.append(
            {
                "store": store,
                "kind": str(item.get("kind", "background")).strip() or "background",
                "title": title or content[:24],
                "content": content,
                "tags": item.get("tags", []) if isinstance(item.get("tags", []), list) else [],
                "importance": int(item.get("importance", 1) or 1),
                "confidence": float(item.get("confidence", 0.6) or 0.6),
                "evidence": str(item.get("evidence", "")).strip(),
            }
        )
    return normalized


def _parse_bool_gate(val: Any) -> bool | None:
    """解析 worth_storing；无法解析时返回 None（调用方按 True 处理）。"""
    if val is None:
        return None
    if isinstance(val, bool):
        return val
    if isinstance(val, str):
        s = val.strip().lower()
        if s in ("true", "1", "yes"):
            return True
        if s in ("false", "0", "no"):
            return False
    return None


async def _extract_memory_items_gated(
    user_id: int,
    reason: str,
    user_message: str,
    assistant_text: str,
    recent_messages: list[dict],
    mid_summaries: list[dict],
) -> list[dict[str, Any]]:
    """单次 Memory Manager LLM：门控「是否值得写」+ 原子条目抽取（决策仍由 _call_decision_llm 单独一步）。"""
    if _MANAGER_LLM is None:
        await init_memory_services()

    payload = {
        "user_id": user_id,
        "reason": reason,
        "user_message": user_message,
        "assistant_text": assistant_text,
        "recent_messages": recent_messages,
        "mid_summaries": mid_summaries,
    }
    response = await _MANAGER_LLM.ainvoke([
        {"role": "system", "content": LONG_TERM_GATE_AND_EXTRACT_PROMPT},
        {"role": "user", "content": json.dumps(payload, ensure_ascii=False)},
    ])

    text = (response.content or "").strip()
    if text.startswith("```"):
        text = text.split("\n", 1)[1]
        if text.endswith("```"):
            text = text[:-3]

    try:
        data = json.loads(text)
    except json.JSONDecodeError as e:
        _log.warning(
            f"记忆链路 [门控+抽取] 拒绝：非合法 JSON user_id={user_id} err={e!s} raw={_lt_preview(text, 500)}"
        )
        return []

    if not isinstance(data, dict):
        _log.warning(f"记忆链路 [门控+抽取] 拒绝：根非对象 user_id={user_id}")
        return []

    force = reason in _FORCE_LONG_TERM_EXTRACT_REASONS
    worth_raw = data.get("worth_storing", True)
    worth = _parse_bool_gate(worth_raw)
    items_raw = data.get("items", [])
    if not isinstance(items_raw, list):
        _log.warning(f"记忆链路 [门控+抽取] 拒绝：items 非列表 user_id={user_id}")
        return []

    if not force:
        if worth is False:
            return []
        # worth is True 或 None：允许继续规范化（略宽松，避免误杀）

    return _normalize_extracted_items(items_raw)


async def should_trigger_long_term_by_prompt(user_message: str, assistant_text: str = "", reason: str = "") -> bool:
    """是否值得走长期写入（兼容旧调用方）。强制 reason 恒 true；否则与门控+抽取同一次 LLM 等价于「是否有条目」。"""
    if reason in _FORCE_LONG_TERM_EXTRACT_REASONS:
        return True
    if not user_message.strip() and not assistant_text.strip():
        return False
    items = await _extract_memory_items_gated(
        user_id=0,
        reason=reason,
        user_message=user_message,
        assistant_text=assistant_text,
        recent_messages=[],
        mid_summaries=[],
    )
    return len(items) > 0


async def run_long_term_memory_write_agent(
    user_id: int,
    *,
    reason: str,
    user_message: str,
    assistant_text: str,
    recent_messages: list[dict],
    mid_summaries: list[dict],
) -> dict[str, int]:
    """长期记忆写入：1) 单次 LLM 门控+抽取 → 2) 读库 + decision_llm 决策 + 落库。"""
    items = await _extract_memory_items_gated(
        user_id=user_id,
        reason=reason,
        user_message=user_message,
        assistant_text=assistant_text,
        recent_messages=recent_messages,
        mid_summaries=mid_summaries,
    )
    return await _store_memory_items(user_id, items)


async def maybe_commit_long_term(
    user_id: int,
    reason: str,
    user_message: str = "",
    assistant_text: str = "",
    recent_messages: list[dict] | None = None,
    mid_summaries: list[dict] | None = None,
) -> dict[str, int]:
    """统一的长期记忆入口（HTTP session-end 等）：门控+抽取与决策均在 ``run_long_term_memory_write_agent`` 内。"""
    if recent_messages is None or mid_summaries is None:
        recent_messages, mid_summaries = await _load_context(user_id)
    return await run_long_term_memory_write_agent(
        user_id,
        reason=reason,
        user_message=user_message,
        assistant_text=assistant_text,
        recent_messages=recent_messages,
        mid_summaries=mid_summaries,
    )


async def get_long_term_context(
    user_id: int,
    query: str,
    *,
    mongo_limit: int | None = None,
    qdrant_limit: int | None = None,
) -> list[dict[str, Any]]:
    """按当前问题召回长期记忆：Mongo 与 Qdrant **分别**限额；合并时先 Mongo 后 Qdrant 并去重。

    user_id>0：Mongo 个人事实（query 词面筛选 top mongo_limit）+ Qdrant（top qdrant_limit）。
    user_id<=0：仅 Qdrant 全局文档库。
    """
    mem = load_config().get("memory", {})
    ml = int(mongo_limit if mongo_limit is not None else mem.get("mongo_retrieval_limit", 10))
    ql = int(qdrant_limit if qdrant_limit is not None else mem.get("long_term_retrieval_limit", 6))

    if user_id <= 0:
        if ql <= 0:
            return []
    else:
        if ml <= 0 and ql <= 0:
            return []

    if _MONGO_CLIENT is None or _QDRANT_CLIENT is None or _EMBEDDINGS is None:
        await init_memory_services()

    if user_id <= 0:
        mongo_hits: list[dict[str, Any]] = []
        qdrant_hits = (
            await asyncio.to_thread(_search_qdrant_documents, 0, query, ql, True) if ql > 0 else []
        )
        merged = _merge_memory_hits(mongo_hits, qdrant_hits)
    else:
        mongo_hits = (
            await asyncio.to_thread(_search_mongo_documents, user_id, query, ml) if ml > 0 else []
        )
        qdrant_hits = (
            await asyncio.to_thread(_search_qdrant_documents, user_id, query, ql, False)
            if ql > 0
            else []
        )
        merged = _merge_memory_hits(mongo_hits, qdrant_hits)

    return merged

def _sparse_tokenize(text: str) -> list[str]:
    """英文/数字连续片段 + 中文单字；供稀疏向量哈希。"""
    if not (text or "").strip():
        return []
    raw = text.strip()
    low = raw.lower()
    toks: list[str] = []
    toks.extend(re.findall(r"[a-z0-9]+", low))
    for ch in raw:
        if "\u4e00" <= ch <= "\u9fff":
            toks.append(ch)
    return toks


def _stable_sparse_index(token: str) -> int:
    """稳定哈希到整数索引（>0）。"""
    h = zlib.adler32(token.encode("utf-8")) & 0xFFFFFFFF
    return int(h % _SPARSE_INDEX_SPACE) + 1


def _sparse_vector_from_text(text: str) -> rest.SparseVector:
    """词袋 + sqrt(tf)，查询与文档对称。"""
    toks = _sparse_tokenize(text)
    if not toks:
        return rest.SparseVector(indices=[], values=[])
    acc: dict[int, float] = {}
    for t in toks:
        ix = _stable_sparse_index(t)
        acc[ix] = acc.get(ix, 0.0) + 1.0
    indices: list[int] = []
    values: list[float] = []
    for ix in sorted(acc.keys()):
        tf = acc[ix]
        indices.append(ix)
        values.append(math.sqrt(tf))
    return rest.SparseVector(indices=indices, values=values)


def _vectors_for_point(dense: list[float], content: str) -> dict[str, Any]:
    return {
        QDRANT_DENSE_VECTOR_NAME: dense,
        QDRANT_SPARSE_VECTOR_NAME: _sparse_vector_from_text(content),
    }


def _extract_default_vector(vec: Any) -> list[float] | None:
    """兼容单向量 / 命名向量 dict（优先 dense）。"""
    if vec is None:
        return None
    if isinstance(vec, list):
        return vec
    if isinstance(vec, dict):
        d = vec.get(QDRANT_DENSE_VECTOR_NAME)
        if isinstance(d, list):
            return d
        if "" in vec and isinstance(vec[""], list):
            return vec[""]
        for _k, v in vec.items():
            if isinstance(v, list):
                return v
    return None


def _build_qdrant_row_from_payload(payload: dict[str, Any], base_score: float, now: datetime) -> dict[str, Any]:
    """base_score 为混合检索融合分或原始向量相似度；再乘 confidence 与时间衰减。"""
    confidence = float(payload.get("confidence", 0.6) or 0.6)
    created_raw = payload.get("created_at", "")
    time_decay = 1.0
    if created_raw:
        try:
            created = datetime.fromisoformat(created_raw.replace("Z", "+00:00"))
            days = (now - created).total_seconds() / 86400.0
            if days > 7:
                time_decay = max(0.1, 1.0 - (days - 7) * 0.05)
        except Exception:
            pass

    weighted_score = float(base_score) * confidence * time_decay

    row: dict[str, Any] = {
        "store": "qdrant",
        "kind": str(payload.get("kind", "background")),
        "title": str(payload.get("title", "")),
        "content": str(payload.get("content", "")),
        "tags": payload.get("tags", []) if isinstance(payload.get("tags", []), list) else [],
        "importance": int(payload.get("importance", 1) or 1),
        "confidence": confidence,
        "score": weighted_score,
        "source": str(payload.get("source", "memory_manager")),
    }
    ms = payload.get("memory_scope")
    if ms:
        row["memory_scope"] = str(ms)
    if payload.get("memory_scope") == MEMORY_SCOPE_DOCUMENT:
        row["article_id"] = str(payload.get("article_id", ""))
        row["chunk_index"] = int(payload.get("chunk_index", 0) or 0)
        row["content_revision"] = int(payload.get("content_revision", 1) or 1)
        row["ingest_kind"] = str(payload.get("ingest_kind", "raw_chunk"))
        sp = payload.get("section_path")
        if isinstance(sp, list):
            row["section_path"] = sp
        gn = payload.get("game_name")
        if gn:
            row["game_name"] = str(gn)
        pv = payload.get("preview")
        if pv:
            row["preview"] = str(pv)

    return row


def _mongo_query_match_score(query: str, title: str, content: str) -> float:
    """当前轮 query（多为改写句）与条目 title/content 的词面重合度 [0,1]。"""
    toks = _sparse_tokenize(query)
    if not toks:
        return 0.0
    blob = f"{title}\n{content}"
    bl = blob.lower()
    hits = 0
    for t in toks:
        if len(t) == 1 and "\u4e00" <= t <= "\u9fff":
            if t in blob:
                hits += 1
        elif t in bl:
            hits += 1
    return hits / len(toks)


def _search_mongo_documents(user_id: int, query: str, limit: int) -> list[dict[str, Any]]:
    """先按 importance 拉候选池，再按与 query 的词面重合度重排，取前 limit（此前完全忽略 query）。"""
    cfg = load_config().get("mongo", {})
    mem = load_config().get("memory", {})
    if _MONGO_CLIENT is None or limit <= 0:
        return []

    pool_cap = int(mem.get("mongo_retrieval_candidate_cap", 80))
    pool_cap = max(pool_cap, limit * 8)
    pool_cap = min(pool_cap, 200)

    collection = _MONGO_CLIENT[cfg.get("database", "goblog_long_term")][cfg.get("collection", "memory_facts")]

    try:
        cursor = collection.find({"user_id": user_id, "status": "active"}).sort([("importance", -1), ("updated_at", -1)]).limit(pool_cap)
        ranked: list[tuple[float, int, dict[str, Any]]] = []
        for doc in cursor:
            title = str(doc.get("title", ""))
            content = str(doc.get("content", ""))
            rel = _mongo_query_match_score(query, title, content)
            imp = int(doc.get("importance", 1) or 1)
            row = {
                "store": "mongo",
                "kind": str(doc.get("kind", "background")),
                "title": title,
                "content": content,
                "tags": doc.get("tags", []) if isinstance(doc.get("tags", []), list) else [],
                "importance": imp,
                "confidence": float(doc.get("confidence", 0.6) or 0.6),
                # 与 Qdrant 混排：相关度加权 + importance；rel=0 时退化为 importance
                "score": float(imp) + 20.0 * rel,
                "source": str(doc.get("source", "memory_manager")),
            }
            ranked.append((rel, imp, row))
        ranked.sort(key=lambda x: (-x[0], -x[1]))
        return [t[2] for t in ranked[:limit]]
    except Exception:
        return []


def _qdrant_user_scope_filter(user_id: int, kb_only: bool) -> rest.Filter | None:
    """个人记忆 ∪ 全局文档库；kb_only 时仅全局文档库。"""
    if kb_only:
        return rest.Filter(
            must=[
                rest.FieldCondition(key="user_id", match=rest.MatchValue(value=KB_USER_ID)),
                rest.FieldCondition(key="memory_scope", match=rest.MatchValue(value=MEMORY_SCOPE_DOCUMENT)),
            ]
        )
    if user_id > 0:
        return rest.Filter(
            should=[
                rest.Filter(must=[rest.FieldCondition(key="user_id", match=rest.MatchValue(value=user_id))]),
                rest.Filter(
                    must=[
                        rest.FieldCondition(key="user_id", match=rest.MatchValue(value=KB_USER_ID)),
                        rest.FieldCondition(key="memory_scope", match=rest.MatchValue(value=MEMORY_SCOPE_DOCUMENT)),
                    ]
                ),
            ]
        )
    return None


def _qdrant_vector_search(
    client: QdrantClient,
    *,
    collection_name: str,
    query_vector: list[float],
    query_filter: rest.Filter | None,
    limit: int,
) -> list[Any]:
    """命名向量 dense 的近邻搜索。query_points + using；旧版 search 回退。"""
    if hasattr(client, "query_points"):
        resp = client.query_points(
            collection_name=collection_name,
            query=query_vector,
            using=QDRANT_DENSE_VECTOR_NAME,
            query_filter=query_filter,
            limit=limit,
            with_payload=True,
        )
        pts = getattr(resp, "points", None)
        return list(pts) if pts else []
    if hasattr(client, "search"):
        return client.search(
            collection_name=collection_name,
            query_vector=query_vector,
            query_filter=query_filter,
            limit=limit,
        )
    raise RuntimeError("QdrantClient 不支持 query_points/search，请升级或降级 qdrant-client")


def _search_qdrant_dense_only(
    user_id: int,
    query: str,
    limit: int,
    kb_only: bool,
) -> list[dict[str, Any]]:
    """纯向量检索（关闭 hybrid_search 时使用）。"""
    cfg = load_config().get("qdrant", {})
    if _QDRANT_CLIENT is None or limit <= 0 or not query.strip():
        return []

    query_filter = _qdrant_user_scope_filter(user_id, kb_only)
    if query_filter is None:
        return []

    collection = cfg.get("collection", "goblog_long_term_memory")
    try:
        query_vectors = _embed_texts([query])
        if not query_vectors:
            return []
        query_vector = query_vectors[0]
        hits = _qdrant_vector_search(
            _QDRANT_CLIENT,
            collection_name=collection,
            query_vector=query_vector,
            query_filter=query_filter,
            limit=limit,
        )
    except Exception:
        return []

    now = datetime.now(timezone.utc)
    results: list[dict[str, Any]] = []
    for hit in hits:
        payload = hit.payload or {}
        raw_score = float(hit.score or 0)
        results.append(_build_qdrant_row_from_payload(payload, raw_score, now))
    return results


def _search_qdrant_hybrid(
    user_id: int,
    query: str,
    limit: int,
    kb_only: bool,
) -> list[dict[str, Any]]:
    """dense + sparse 双路 prefetch，Qdrant 服务端 RRF 融合；再套 confidence / 时间衰减。"""
    cfg_q = load_config().get("qdrant", {})
    mem = load_config().get("memory", {})
    if _QDRANT_CLIENT is None or limit <= 0 or not query.strip():
        return []

    query_filter = _qdrant_user_scope_filter(user_id, kb_only)
    if query_filter is None:
        return []

    prefetch = max(limit, int(mem.get("hybrid_prefetch_limit", 48)))
    recall_kw = bool(mem.get("hybrid_keyword_recall", True))

    collection = cfg_q.get("collection", "goblog_long_term_memory")

    query_vectors = _embed_texts([query])
    if not query_vectors:
        return []
    query_vector = query_vectors[0]
    sparse_q = _sparse_vector_from_text(query)

    if not recall_kw or not sparse_q.indices:
        return _search_qdrant_dense_only(user_id, query, limit, kb_only)

    rrf_k = mem.get("hybrid_rrf_k")
    fusion_query: Any
    try:
        if rrf_k is not None and getattr(rest, "RrfQuery", None) and getattr(rest, "Rrf", None):
            fusion_query = rest.RrfQuery(rrf=rest.Rrf(k=int(rrf_k)))
        else:
            fusion_query = rest.FusionQuery(fusion=rest.Fusion.RRF)
    except Exception:
        fusion_query = rest.FusionQuery(fusion=rest.Fusion.RRF)

    try:
        resp = _QDRANT_CLIENT.query_points(
            collection_name=collection,
            prefetch=[
                rest.Prefetch(
                    query=query_vector,
                    using=QDRANT_DENSE_VECTOR_NAME,
                    limit=prefetch,
                    filter=query_filter,
                ),
                rest.Prefetch(
                    query=sparse_q,
                    using=QDRANT_SPARSE_VECTOR_NAME,
                    limit=prefetch,
                    filter=query_filter,
                ),
            ],
            query=fusion_query,
            limit=limit,
            with_payload=True,
        )
    except Exception as e:
        _log.warning(f"hybrid RRF query_points 失败，回退纯 dense err={str(e)[:400]}")
        return _search_qdrant_dense_only(user_id, query, limit, kb_only)

    pts = getattr(resp, "points", None) or []
    now = datetime.now(timezone.utc)
    out: list[dict[str, Any]] = []
    for hit in pts:
        payload = hit.payload or {}
        raw_score = float(hit.score or 0)
        out.append(_build_qdrant_row_from_payload(payload, raw_score, now))
    return out


def _search_qdrant_documents(
    user_id: int,
    query: str,
    limit: int,
    kb_only: bool = False,
) -> list[dict[str, Any]]:
    """Qdrant 长期记忆检索：默认 hybrid（dense+sparse RRF）；可在配置中关闭 hybrid_search 或 keyword 分支。"""
    mem = load_config().get("memory", {})
    if mem.get("hybrid_search", True):
        return _search_qdrant_hybrid(user_id, query, limit, kb_only)
    return _search_qdrant_dense_only(user_id, query, limit, kb_only)


def _merge_row_from_hit(item: dict[str, Any]) -> dict[str, Any]:
    row_out: dict[str, Any] = {
        "store": item.get("store"),
        "kind": item.get("kind"),
        "title": item.get("title"),
        "content": item.get("content"),
        "tags": item.get("tags", []),
        "importance": item.get("importance", 1),
        "confidence": item.get("confidence", 0.6),
        "score": float(item.get("score", 0)),
        "source": item.get("source", "memory_manager"),
    }
    for k in (
        "memory_scope",
        "ingest_kind",
        "article_id",
        "chunk_index",
        "content_revision",
        "section_path",
        "game_name",
        "preview",
    ):
        if k in item and item[k] is not None:
            row_out[k] = item[k]
    return row_out


def _merge_memory_hits(
    mongo_hits: list[dict[str, Any]],
    qdrant_hits: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    """先 Mongo（已在各自路径上限额筛选），再按分数追加 Qdrant；按 kind+title+content 去重（Mongo 优先保留）。"""
    merged: list[dict[str, Any]] = []
    seen: set[tuple[str, str, str]] = set()

    def _dedup_key(item: dict[str, Any]) -> tuple[str, str, str]:
        return (
            str(item.get("kind", "")),
            str(item.get("title", "")),
            str(item.get("content", "")),
        )

    for item in mongo_hits:
        k = _dedup_key(item)
        if k in seen:
            continue
        seen.add(k)
        merged.append(_merge_row_from_hit(item))

    for item in sorted(qdrant_hits, key=lambda x: float(x.get("score", 0)), reverse=True):
        k = _dedup_key(item)
        if k in seen:
            continue
        seen.add(k)
        merged.append(_merge_row_from_hit(item))

    return merged



async def _load_context(user_id: int) -> tuple[list[dict], list[dict]]:
    from memory_store import get_mid_summaries, get_short_messages

    cfg = load_config().get("memory", {})
    short_limit = int(cfg.get("short_window_messages", 12))
    mid_limit = int(cfg.get("mid_max_items", 8))
    recent_messages = await get_short_messages(user_id, short_limit)
    mid_summaries = await get_mid_summaries(user_id, mid_limit)
    return recent_messages, mid_summaries


async def _store_memory_items(user_id: int, items: list[dict[str, Any]]) -> dict[str, int]:
    """带决策引擎的长期记忆存储入口。

    1. 按新条目类型选择性读取已有记忆：Mongo 条目只读 Mongo，Qdrant 条目只读 Qdrant。
    2. 交给 decision_llm 判定每个新条目：新增 / 覆盖 / 删除 / 忽略 / 降低置信度。
    3. 分别应用 Mongo 和 Qdrant 的决策。
    """
    if not items:
        return {"mongo": 0, "qdrant": 0}

    mongo_items = [item for item in items if item["store"] == "mongo"]
    qdrant_items = [item for item in items if item["store"] == "qdrant"]

    # 1. 按需读取已有长期记忆：有 Mongo 条目才读 Mongo，有 Qdrant 条目才读 Qdrant
    existing_mongo: list[dict[str, Any]] = []
    existing_qdrant: list[dict[str, Any]] = []
    if mongo_items:
        existing_mongo = await asyncio.to_thread(_read_existing_mongo, user_id)
    if qdrant_items:
        existing_qdrant = await asyncio.to_thread(_read_existing_qdrant, user_id, qdrant_items)

    # 2. 调用决策小模型
    decisions = await _call_decision_llm(user_id, mongo_items, qdrant_items, existing_mongo, existing_qdrant)
    raw_ma = decisions.get("mongo_actions") or []
    decisions["mongo_actions"] = _fix_false_mongo_ignores(existing_mongo, mongo_items, raw_ma)
    ma = decisions.get("mongo_actions") or []
    qa = decisions.get("qdrant_actions") or []
    if not isinstance(ma, list) or not isinstance(qa, list):
        _log.warning(f"记忆链路 [决策] user_id={user_id} 返回结构异常 decisions_keys={list(decisions.keys()) if isinstance(decisions, dict) else type(decisions)}")

    # 3. 执行决策
    mongo_count = await asyncio.to_thread(_apply_mongo_decisions, user_id, decisions.get("mongo_actions", []))
    qdrant_count = await asyncio.to_thread(_apply_qdrant_decisions, user_id, decisions.get("qdrant_actions", []))

    return {"mongo": mongo_count, "qdrant": qdrant_count}


def _read_existing_mongo(user_id: int) -> list[dict[str, Any]]:
    """全量读取用户已有的 Mongo 文档（仅 active 状态）。"""
    docs: list[dict[str, Any]] = []
    if _MONGO_CLIENT is None:
        return docs
    cfg = load_config().get("mongo", {})
    try:
        collection = _MONGO_CLIENT[cfg.get("database", "goblog_long_term")][cfg.get("collection", "memory_facts")]
        cursor = collection.find({"user_id": user_id, "status": "active"})
        for doc in cursor:
            docs.append({
                "_id": str(doc.get("_id", "")),
                "kind": str(doc.get("kind", "")),
                "title": str(doc.get("title", "")),
                "content": str(doc.get("content", "")),
                "tags": doc.get("tags", []) if isinstance(doc.get("tags", []), list) else [],
                "importance": int(doc.get("importance", 1) or 1),
                "confidence": float(doc.get("confidence", 0.6) or 0.6),
                "fingerprint": str(doc.get("fingerprint", "")),
                "status": str(doc.get("status", "active")),
            })
    except Exception as e:
        _log.warning("Mongo 全量读取失败", extra={"user_id": user_id, "error": str(e)[:200]})
        pass
    return docs


def _read_existing_qdrant(user_id: int, new_items: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """对每个新 Qdrant 条目检索 top-3 最相似的已有向量。"""
    hits: list[dict[str, Any]] = []
    if _QDRANT_CLIENT is None or not new_items:
        return hits
    cfg = load_config().get("qdrant", {})
    collection = cfg.get("collection", "goblog_long_term_memory")
    # 与 upsert 路径一致，避免 collection 未创建时 search 直接失败
    _ensure_qdrant_collection_sync()
    seen_ids: set[str] = set()
    now = datetime.now(timezone.utc)
    for item in new_items:
        content = str(item.get("content", "")).strip()
        if not content:
            continue
        try:
            vecs = _embed_texts([content])
            if not vecs:
                _log.warning(
                    f"Qdrant 检索已有记忆跳过：embed 返回空向量 user_id={user_id} collection={collection} content_len={len(content)}"
                )
                continue
            query_filter = rest.Filter(
                must=[rest.FieldCondition(key="user_id", match=rest.MatchValue(value=user_id))]
            )
            results = _qdrant_vector_search(
                _QDRANT_CLIENT,
                collection_name=collection,
                query_vector=vecs[0],
                query_filter=query_filter,
                limit=3,
            )
            for hit in results:
                pid = str(hit.id)
                if pid in seen_ids:
                    continue
                seen_ids.add(pid)
                payload = hit.payload or {}
                raw_score = float(hit.score or 0)
                confidence = float(payload.get("confidence", 0.6) or 0.6)

                created_raw = payload.get("created_at", "")
                time_decay = 1.0
                if created_raw:
                    try:
                        created = datetime.fromisoformat(created_raw.replace("Z", "+00:00"))
                        days = (now - created).total_seconds() / 86400.0
                        if days > 7:
                            time_decay = max(0.1, 1.0 - (days - 7) * 0.05)
                    except Exception:
                        pass

                weighted_score = raw_score * confidence * time_decay

                hits.append({
                    "point_id": pid,
                    "kind": str(payload.get("kind", "")),
                    "title": str(payload.get("title", "")),
                    "content": str(payload.get("content", "")),
                    "tags": payload.get("tags", []) if isinstance(payload.get("tags", []), list) else [],
                    "importance": int(payload.get("importance", 1) or 1),
                    "confidence": confidence,
                    "score": weighted_score,
                    "created_at": str(payload.get("created_at", "")),
                })
        except Exception as e:
            err = str(e)
            # agent_log 的 Formatter 不输出 extra，必须把细节写进 message 才能在控制台/文件里看到
            line = (
                f"Qdrant 检索已有记忆失败 user_id={user_id} collection={collection}: {err[:500]}"
            )
            if "dimension" in err.lower() or "vector size" in err.lower():
                line += " | 若曾改 embedding 维度，请令 config 中 qdrant.vector_size 与之一致，或删该集合并重建"
            _log.warning(line)
    return hits


async def _call_decision_llm(
    user_id: int,
    mongo_items: list[dict[str, Any]],
    qdrant_items: list[dict[str, Any]],
    existing_mongo: list[dict[str, Any]],
    existing_qdrant: list[dict[str, Any]],
) -> dict[str, Any]:
    """调用 decision_llm 判定每个新条目的操作。

    返回格式：
    {
      "mongo_actions": [
        {"action": "add|overwrite|delete|ignore", "fingerprint": "...", "kind": "...",
         "title": "...", "content": "...", "reason": "...",
         "overwrite_fingerprint": "..."}
      ],
      "qdrant_actions": [
        {"action": "add|lower_confidence|ignore", "kind": "...", "title": "...",
         "content": "...", "reason": "...",
         "matched_point_ids": ["..."], "new_confidence": 0.5}
      ]
    }
    """
    if _DECISION_LLM is None:
        await init_memory_services()
    if _DECISION_LLM is None:
        return {"mongo_actions": [], "qdrant_actions": []}

    system_prompt = """你是长期记忆决策器（Decision Agent）。根据新提取的记忆条目和已有长期记忆，逐条判定操作。

## Mongo 操作规则（键值型记忆，kind 为 profile/rule/preference/status）
重要：同一用户会有**多条** Mongo 文档（指纹 fingerprint 不同），每条通常对应一个独立角度（职业、昵称、规则…）。不要把「都和用户有关」误判成重复。

- **新增 (add)**（默认倾向）：新条目陈述的事实**未包含**在任一已有文档的正文里；或与已有文档是**不同维度**的信息（例如已有「用户名与 ID」，新有「职业是软件工程师」→ 必须 add，不是 ignore）。
- **覆盖 (overwrite)**：新内容与**某一条**已有文档描述的是**同一件事**且信息更新（例如改昵称、修正同一职业描述）。必须指定 overwrite_fingerprint 为被替换那条的 fingerprint。
- **删除 (delete)**：用户明确否定旧事实或要求删掉某条。指定 fingerprint。
- **忽略 (ignore)**：仅当新条目与**某一条已有文档**在语义上**完全重复**（同一事实说了两遍、文字等价）时才用；**禁止**因为「都是用户资料」就 ignore。

## Qdrant 操作规则（向量型记忆，kind 为 experience/case/background）
- **新增 (add)**：新内容与已有所有向量完全不相干（相似度 < 0.5）。
- **降低置信度 (lower_confidence)**：新内容与某些已有向量相似（≥ 0.5）但含义不同或存在矛盾，需降低旧向量的置信度（每次 -0.1，最低 0.1）。指定 matched_point_ids 和 new_confidence。
- **忽略 (ignore)**：新内容与已有向量完全一致，无需任何操作。

## 输出格式（严格 JSON）
{
  "mongo_actions": [
    {"action": "add|overwrite|delete|ignore", "fingerprint": "新条目的指纹(hash)", "kind": "...", "title": "...", "content": "...", "reason": "简短理由", "overwrite_fingerprint": "被覆盖文档的fingerprint(仅overwrite需要)"}
  ],
  "qdrant_actions": [
    {"action": "add|lower_confidence|ignore", "kind": "...", "title": "...", "content": "...", "reason": "简短理由", "matched_point_ids": ["point_id1"], "new_confidence": 0.5}
  ]
}

只输出 JSON，不要解释。"""

    # 为新条目预计算指纹
    for item in mongo_items:
        item["_fingerprint"] = hashlib.sha1(
            f"{user_id}:{item['kind']}:{item['title']}:{item['content']}".encode("utf-8")
        ).hexdigest()

    payload = {
        "user_id": user_id,
        "new_mongo_items": [
            {
                "fingerprint": item.get("_fingerprint", ""),
                "kind": item["kind"],
                "title": item["title"],
                "content": item["content"],
                "tags": item.get("tags", []),
                "importance": item.get("importance", 1),
                "confidence": item.get("confidence", 0.6),
            }
            for item in mongo_items
        ],
        "new_qdrant_items": [
            {
                "kind": item["kind"],
                "title": item["title"],
                "content": item["content"],
                "tags": item.get("tags", []),
                "importance": item.get("importance", 1),
                "confidence": item.get("confidence", 0.6),
            }
            for item in qdrant_items
        ],
        "existing_mongo": [
            {
                "fingerprint": doc.get("fingerprint", ""),
                "kind": doc["kind"],
                "title": doc["title"],
                "content": doc["content"],
                "tags": doc.get("tags", []),
                "confidence": doc.get("confidence", 0.6),
            }
            for doc in existing_mongo
        ],
        "existing_qdrant": [
            {
                "point_id": doc.get("point_id", ""),
                "kind": doc["kind"],
                "title": doc["title"],
                "content": doc["content"],
                "tags": doc.get("tags", []),
                "confidence": doc.get("confidence", 0.6),
                "similarity_score": doc.get("score", 0),
            }
            for doc in existing_qdrant
        ],
    }

    try:
        response = await _DECISION_LLM.ainvoke([
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": json.dumps(payload, ensure_ascii=False)},
        ])
        text = response.content.strip()
        if text.startswith("```"):
            text = text.split("\n", 1)[1]
            if text.endswith("```"):
                text = text[:-3]
        return json.loads(text)
    except Exception as e:
        _log.error(f"记忆链路 [决策LLM] 调用失败 err={str(e)[:400]}")
        return {"mongo_actions": [], "qdrant_actions": []}


def _fix_false_mongo_ignores(
    existing_mongo: list[dict[str, Any]],
    mongo_items: list[dict[str, Any]],
    mongo_actions: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    """决策模型误将「全新指纹」判为 ignore 时纠正为 add（常见于把「职业」与「用户基本信息」笼统当成重复）。"""
    existing_fp = {str(d.get("fingerprint", "")) for d in existing_mongo if d.get("fingerprint")}
    by_fp = {str(it.get("_fingerprint", "")): it for it in mongo_items if it.get("_fingerprint")}
    fixed: list[dict[str, Any]] = []
    for i, act in enumerate(mongo_actions or []):
        if not isinstance(act, dict):
            fixed.append(act)
            continue
        a = dict(act)
        fp = str(a.get("fingerprint", "")).strip()
        if not fp and i < len(mongo_items):
            fp = str(mongo_items[i].get("_fingerprint", "")).strip()
            if fp:
                a["fingerprint"] = fp
        if a.get("action") == "ignore" and fp and fp not in existing_fp:
            a["action"] = "add"
            it = by_fp.get(fp)
            if it:
                a.setdefault("kind", it.get("kind"))
                a.setdefault("title", it.get("title"))
                a.setdefault("content", it.get("content"))
                a.setdefault("tags", it.get("tags", []))
                a.setdefault("importance", it.get("importance", 1))
                a.setdefault("confidence", it.get("confidence", 0.6))
            _log.info(
                f"记忆链路 [决策校正] ignore→add fingerprint={fp[:24]}... （库中不存在该指纹，不应忽略）"
            )
        fixed.append(a)
    return fixed


def _embed_texts(texts: list[str]) -> list[list[float]]:
    """批量文本向量化。依赖智谱 text_embedding-v3，失败直接抛错。"""
    if not texts:
        return []
    return _EMBEDDINGS.embed_documents(texts)


def _apply_mongo_decisions(user_id: int, actions: list[dict[str, Any]]) -> int:
    """执行 Mongo 决策：新增/覆盖/软删除。"""
    cfg = load_config().get("mongo", {})
    if _MONGO_CLIENT is None or not actions:
        return 0

    collection = _MONGO_CLIENT[cfg.get("database", "goblog_long_term")][cfg.get("collection", "memory_facts")]
    now = datetime.now(timezone.utc)
    ops = []
    count = 0

    for act in actions:
        action = act.get("action", "ignore")
        if action == "ignore":
            continue

        fingerprint = act.get("fingerprint", "")
        kind = act.get("kind", "background")
        title = act.get("title", "")
        content = act.get("content", "")

        if action == "add":
            doc = {
                "user_id": user_id,
                "kind": kind,
                "title": title,
                "content": content,
                "tags": act.get("tags", []),
                "importance": act.get("importance", 1),
                "confidence": act.get("confidence", 0.6),
                "fingerprint": fingerprint,
                "source": "memory_manager",
                "status": "active",
                "updated_at": now,
                "created_at": now,
            }
            ops.append(UpdateOne({"fingerprint": fingerprint}, {"$set": doc}, upsert=True))
            count += 1

        elif action == "overwrite":
            overwrite_fp = act.get("overwrite_fingerprint", fingerprint)
            doc = {
                "user_id": user_id,
                "kind": kind,
                "title": title,
                "content": content,
                "tags": act.get("tags", []),
                "importance": act.get("importance", 1),
                "confidence": act.get("confidence", 0.6),
                "fingerprint": fingerprint,
                "source": "memory_manager",
                "status": "active",
                "updated_at": now,
            }
            ops.append(UpdateOne({"fingerprint": overwrite_fp}, {"$set": doc}))
            count += 1

        elif action == "delete":
            # 软删除
            ops.append(UpdateOne(
                {"fingerprint": fingerprint},
                {"$set": {"status": "deleted", "updated_at": now}},
            ))
            count += 1

    if ops:
        try:
            collection.bulk_write(ops, ordered=False)
        except Exception as e:
            _log.error("Mongo bulk_write 失败", extra={"ops_count": len(ops), "error": str(e)[:200]})

    return count


def _apply_qdrant_decisions(user_id: int, actions: list[dict[str, Any]]) -> int:
    """执行 Qdrant 决策：新增向量 / 降低已有向量置信度。"""
    cfg = load_config().get("qdrant", {})
    if _QDRANT_CLIENT is None or not actions:
        return 0

    _ensure_qdrant_collection_sync()
    collection = cfg.get("collection", "goblog_long_term_memory")
    now = datetime.now(timezone.utc).isoformat()
    count = 0

    add_points = []
    lc_ok = 0
    for act in actions:
        action = act.get("action", "ignore")
        if action == "ignore":
            continue

        kind = act.get("kind", "background")
        title = act.get("title", "")
        content = act.get("content", "")

        if action == "add":
            vecs = _embed_texts([content])
            if not vecs:
                continue
            point_id = uuid.uuid5(uuid.NAMESPACE_URL, f"{user_id}:{kind}:{title}:{content}")
            payload = {
                "user_id": user_id,
                "memory_scope": MEMORY_SCOPE_DIALOGUE,
                "kind": kind,
                "title": title,
                "content": content,
                "tags": act.get("tags", []),
                "importance": act.get("importance", 1),
                "confidence": act.get("confidence", 0.6),
                "source": "memory_manager",
                "created_at": now,
                "updated_at": now,
            }
            add_points.append(
                rest.PointStruct(id=point_id, vector=_vectors_for_point(vecs[0], content), payload=payload)
            )

        elif action == "lower_confidence":
            matched_ids = act.get("matched_point_ids", [])
            new_conf = float(act.get("new_confidence", 0.5))
            if not matched_ids:
                continue
            for pid_str in matched_ids:
                try:
                    pid = uuid.UUID(pid_str)
                    _QDRANT_CLIENT.set_payload(
                        collection_name=collection,
                        payload={"confidence": max(new_conf, 0.1), "updated_at": now},
                        points=[pid],
                    )
                    lc_ok += 1
                except Exception as e:
                    _log.warning("Qdrant lower_confidence 失败", extra={"point_id": pid_str, "error": str(e)[:100]})

    add_ok = 0
    if add_points:
        try:
            _QDRANT_CLIENT.upsert(collection_name=collection, points=add_points)
            add_ok = len(add_points)
        except Exception as e:
            err = str(e)
            _log.error(
                f"Qdrant upsert 失败 points={len(add_points)} err={err[:400]} "
                f"| 若含 dimension，请删集合「{collection}」或对齐 qdrant.vector_size 与 embedding 维度"
            )

    return add_ok + lc_ok


def ingest_document_chunk_sync(
    article_id: str,
    chunk_index: int,
    content: str,
    *,
    content_revision: int = 1,
    source: str,
    ingest_kind: str = "raw_chunk",
    game_name: str | None = None,
    section_path: list[str] | None = None,
    preview: str | None = None,
) -> dict[str, Any]:
    """将单块文档写入 Qdrant 全局知识库（user_id=0, memory_scope=document）。同一 article_id+chunk_index+content_revision 重复 upsert 即覆盖。"""
    if ingest_kind not in ("raw_chunk", "summary"):
        raise ValueError("ingest_kind must be raw_chunk or summary")

    article_id = article_id.strip()
    source = source.strip()
    content = content.strip()
    if not article_id or not content or not source:
        raise ValueError("article_id, content, source must be non-empty")
    if chunk_index < 0 or content_revision < 1:
        raise ValueError("chunk_index must be >= 0 and content_revision >= 1")

    if _QDRANT_CLIENT is None or _EMBEDDINGS is None:
        raise RuntimeError("memory services not initialized; call init_memory_services first")

    _ensure_qdrant_collection_sync()
    cfg = load_config().get("qdrant", {})
    collection = cfg.get("collection", "goblog_long_term_memory")

    vecs = _embed_texts([content])
    if not vecs:
        raise RuntimeError("embedding returned empty vector")

    point_id = uuid.uuid5(
        uuid.NAMESPACE_URL,
        f"goblog:kb:{article_id}:{chunk_index}:{content_revision}",
    )
    preview_text = (preview or "").strip() or content[:240]
    now = datetime.now(timezone.utc).isoformat()
    path_list = [s.strip() for s in (section_path or []) if isinstance(s, str) and s.strip()]

    payload: dict[str, Any] = {
        "user_id": KB_USER_ID,
        "memory_scope": MEMORY_SCOPE_DOCUMENT,
        "kind": "wiki",
        "title": "",
        "article_id": article_id,
        "chunk_index": chunk_index,
        "content_revision": content_revision,
        "ingest_kind": ingest_kind,
        "source": source,
        "content": content,
        "tags": [],
        "importance": 1,
        "confidence": 1.0,
        "game_name": (game_name.strip() if game_name else ""),
        "section_path": path_list,
        "preview": preview_text,
        "pipeline": "document_ingest",
        "created_at": now,
        "updated_at": now,
    }

    _QDRANT_CLIENT.upsert(
        collection_name=collection,
        points=[rest.PointStruct(id=point_id, vector=_vectors_for_point(vecs[0], content), payload=payload)],
    )
    _log.info(
        "document chunk ingested",
        extra={"article_id": article_id, "chunk_index": chunk_index, "content_revision": content_revision},
    )
    return {"point_id": str(point_id), "collection": collection}


async def ingest_document_chunk(
    article_id: str,
    chunk_index: int,
    content: str,
    *,
    content_revision: int = 1,
    source: str,
    ingest_kind: str = "raw_chunk",
    game_name: str | None = None,
    section_path: list[str] | None = None,
    preview: str | None = None,
) -> dict[str, Any]:
    """异步包装：读入文档块入库（HTTP 或离线脚本调用）。"""
    await init_memory_services()
    return await asyncio.to_thread(
        ingest_document_chunk_sync,
        article_id,
        chunk_index,
        content,
        content_revision=content_revision,
        source=source,
        ingest_kind=ingest_kind,
        game_name=game_name,
        section_path=section_path,
        preview=preview,
    )
