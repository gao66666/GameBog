"""
长期记忆管理器。

职责：
1. 单次 Memory Manager LLM：**门控是否值得写** + **抽取**原子条目
2. 单次 decision_llm：**与已有记忆对比后决策**并落库
3. **MongoDB**：个人画像（profile）、规则（rule）、偏好（preference）、状态（status）
4. **Qdrant 用户记忆库**：与用户相关的事件/项目/经历（非 Wiki 知识）；对话/反思不写 kb_collection
"""

from __future__ import annotations

import asyncio
import hashlib
import json
import re
import uuid
from datetime import datetime, timezone
from typing import Any

from langchain_openai import ChatOpenAI, OpenAIEmbeddings
from pymongo import MongoClient, UpdateOne
from qdrant_client import QdrantClient
from qdrant_client.http import models as rest

from infra.agent_log import get_logger

_log = get_logger("memory_manager")


def _lt_preview(text: str, n: int = 120) -> str:
    """日志里预览一段用户话，去换行、截断。"""
    s = (text or "").replace("\n", " ").strip()
    if len(s) <= n:
        return s
    return s[: n - 3] + "..."


# 全局游戏 Wiki 文档库：仅 ingest_document_chunk → kb_collection；对话/反思长期记忆只写 user_memory_collection。
KB_USER_ID = 0
MEMORY_SCOPE_DIALOGUE = "dialogue"
MEMORY_SCOPE_DOCUMENT = "document"

# 与 docker-compose 中 qdrant/qdrant:v1.18.0 及 qdrant-client>=1.15.2 对齐（服务端 BM25 + prefetch RRF）
QDRANT_DENSE_VECTOR_NAME = "dense"
QDRANT_SPARSE_VECTOR_NAME = "sparse"
QDRANT_BM25_MODEL = "qdrant/bm25"

from infra.config import get_prompts, load_config
from infra.llm_util import chat_openai_kwargs


_MONGO_CLIENT: MongoClient | None = None
_QDRANT_CLIENT: QdrantClient | None = None
_EMBEDDINGS: OpenAIEmbeddings | None = None
_MANAGER_LLM: ChatOpenAI | None = None
_DECISION_LLM: ChatOpenAI | None = None


# 强制进入「尽力抽取」的 reason：不再做 worth_storing 门控（与旧版「触发器恒 true」一致）。
_FORCE_LONG_TERM_EXTRACT_REASONS = frozenset(
    {"explicit_remember", "session_end", "timeout", "ws_close"}
)

# 对话/反思长期记忆：Mongo 画像与规则 vs Qdrant 用户叙事（非 Wiki 知识库）
MONGO_MEMORY_KINDS = frozenset({"profile", "rule", "preference", "status"})
# Mongo 检索：profile（用户画像）全量；rule/preference 按 query 匹配，合计上限见 mongo_rule_preference_limit
MONGO_ALWAYS_INCLUDE_KINDS = frozenset({"profile"})
MONGO_QUERY_MATCH_KINDS = frozenset({"rule", "preference"})
QDRANT_MEMORY_KINDS = frozenset({"experience", "case", "background", "event", "project"})
_SKIP_DIALOGUE_MEMORY_KINDS = frozenset({"document", "wiki", "knowledge", "kb", "article"})
_KIND_DEFAULT_STORE: dict[str, str] = {
    "profile": "mongo",
    "rule": "mongo",
    "preference": "mongo",
    "status": "mongo",
    "experience": "qdrant",
    "case": "qdrant",
    "background": "qdrant",
    "event": "qdrant",
    "project": "qdrant",
}

async def init_memory_services(*, ensure_kb: bool = True) -> None:
    """初始化 MongoDB / Qdrant / Embeddings / LLM 客户端。

    ensure_kb=False 时仅确保用户 memory collection（对话/反思写入路径，不碰 Wiki kb_collection）。
    """
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
        _MANAGER_LLM = ChatOpenAI(**chat_openai_kwargs(_manager_cfg, temperature=_manager_cfg.get("temperature", 0)))

    decision_cfg = load_config().get("decision_llm", {})
    if _DECISION_LLM is None:
        _DECISION_LLM = ChatOpenAI(**chat_openai_kwargs(decision_cfg, temperature=decision_cfg.get("temperature", 0)))

    if ensure_kb:
        await _ensure_qdrant_collections()
    else:
        await _ensure_user_memory_collection()


async def _ensure_user_memory_collection() -> None:
    await asyncio.to_thread(_ensure_user_memory_collection_core)


def _qdrant_collection_names() -> tuple[str, str]:
    """个人长期记忆与全局文档 KB 分 collection 存储。"""
    cfg = load_config().get("qdrant", {})
    user_col = str(cfg.get("user_memory_collection") or "goblog_user_memory")
    kb_col = str(cfg.get("kb_collection") or "goblog_kb_documents")
    return user_col, kb_col


def _user_memory_collection_name() -> str:
    """对话抽取 / 异步反思 / session-end 等用户长期记忆 Qdrant 写入目标。"""
    return _qdrant_collection_names()[0]


def _kb_collection_name() -> str:
    """游戏 Wiki 等文档 ingest 专用 Qdrant collection（与用户记忆隔离）。"""
    return _qdrant_collection_names()[1]


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
    """是否为「dense + sparse(BM25/IDF)」命名向量结构（供 hybrid RRF）。"""
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
        sparse_cfg = sparse[QDRANT_SPARSE_VECTOR_NAME]
        modifier = getattr(sparse_cfg, "modifier", None)
        return modifier == rest.Modifier.IDF
    except Exception:
        return False


def _create_named_dense_sparse_collection(collection: str, vector_size: int) -> None:
    if _QDRANT_CLIENT is None:
        return
    _QDRANT_CLIENT.create_collection(
        collection_name=collection,
        vectors_config={
            QDRANT_DENSE_VECTOR_NAME: rest.VectorParams(size=vector_size, distance=rest.Distance.COSINE),
        },
        sparse_vectors_config={
            QDRANT_SPARSE_VECTOR_NAME: rest.SparseVectorParams(modifier=rest.Modifier.IDF),
        },
    )


def _ensure_one_qdrant_collection(collection: str, vector_size: int) -> None:
    """确保单个 collection 存在且为 dense+sparse 命名向量 schema。"""
    if _QDRANT_CLIENT is None:
        return

    try:
        exists = _QDRANT_CLIENT.collection_exists(collection)
    except Exception:
        exists = False

    if not exists:
        try:
            _create_named_dense_sparse_collection(collection, vector_size)
            _log.info(
                f"Qdrant 已创建集合「{collection}」dense={vector_size} 维 + sparse/BM25（docker 镜像建议 qdrant v1.15.2+）"
            )
        except Exception as e:
            _log.warning(f"Qdrant 创建集合失败 collection={collection} err={str(e)[:200]}")
        return

    actual = _read_qdrant_dense_vector_size(collection)
    schema_ok = _collection_has_dense_sparse_vectors(collection)
    if schema_ok and actual is not None and actual == vector_size:
        return

    reason: list[str] = []
    if not schema_ok:
        reason.append("非 dense+sparse(BM25/IDF) 命名向量结构")
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
        _create_named_dense_sparse_collection(collection, vector_size)
        _log.info(f"Qdrant 已重建集合「{collection}」dense={vector_size} + sparse/BM25")
    except Exception as e:
        _log.error(f"Qdrant 重建集合失败 collection={collection} err={str(e)[:300]}")


def _ensure_payload_indexes(collection: str, role: str) -> None:
    """为 filter 字段创建 Qdrant payload 索引（已存在则忽略）。"""
    if _QDRANT_CLIENT is None:
        return
    specs: list[tuple[str, rest.PayloadSchemaType]] = []
    if role == "user":
        specs = [("user_id", rest.PayloadSchemaType.INTEGER)]
    elif role == "kb":
        specs = [
            ("memory_scope", rest.PayloadSchemaType.KEYWORD),
            ("article_id", rest.PayloadSchemaType.KEYWORD),
        ]
    for field_name, field_schema in specs:
        try:
            _QDRANT_CLIENT.create_payload_index(
                collection_name=collection,
                field_name=field_name,
                field_schema=field_schema,
            )
            _log.info(f"Qdrant payload 索引已创建 collection={collection} field={field_name}")
        except Exception as e:
            msg = str(e).lower()
            if "already exists" in msg or "already exist" in msg:
                continue
            _log.warning(
                f"Qdrant payload 索引创建失败 collection={collection} field={field_name} err={str(e)[:200]}"
            )


def _ensure_user_memory_collection_core() -> None:
    """仅确保用户长期记忆 collection（反思/对话写入不依赖 KB collection）。"""
    cfg = load_config().get("qdrant", {})
    vector_size = int(cfg.get("vector_size", 1536))
    if _QDRANT_CLIENT is None:
        return
    name = _user_memory_collection_name()
    _ensure_one_qdrant_collection(name, vector_size)
    _ensure_payload_indexes(name, "user")


def _ensure_kb_collection_core() -> None:
    """确保 Wiki/文档 KB collection（仅 document ingest 使用）。"""
    cfg = load_config().get("qdrant", {})
    vector_size = int(cfg.get("vector_size", 1536))
    if _QDRANT_CLIENT is None:
        return
    name = _kb_collection_name()
    _ensure_one_qdrant_collection(name, vector_size)
    _ensure_payload_indexes(name, "kb")


def _ensure_qdrant_collections_core() -> None:
    """确保个人记忆 / 文档 KB 两个 collection 及 payload 索引。"""
    _ensure_user_memory_collection_core()
    _ensure_kb_collection_core()


def _ensure_user_memory_collection_sync() -> None:
    _ensure_user_memory_collection_core()


def _ensure_kb_collection_sync() -> None:
    _ensure_kb_collection_core()


def _ensure_qdrant_collection_sync() -> None:
    """供同步路径（如 Qdrant upsert）在初始化 collection 时调用。"""
    _ensure_qdrant_collections_core()


async def _ensure_qdrant_collections() -> None:
    await asyncio.to_thread(_ensure_qdrant_collections_core)


# 兼容旧调用名
_ensure_qdrant_collection_core = _ensure_qdrant_collections_core


async def _ensure_qdrant_collection() -> None:
    await _ensure_qdrant_collections()


def _coerce_dialogue_memory_item(store: str, kind: str) -> tuple[str, str] | None:
    """按 kind 纠正 store；Wiki/知识类 kind 直接丢弃。"""
    kind = (kind or "background").strip().lower()
    if kind in _SKIP_DIALOGUE_MEMORY_KINDS:
        return None
    expected = _KIND_DEFAULT_STORE.get(kind)
    if expected:
        return expected, kind
    store = store.strip().lower()
    if store == "mongo":
        return "mongo", kind if kind in MONGO_MEMORY_KINDS else "profile"
    if store == "qdrant":
        return "qdrant", kind if kind in QDRANT_MEMORY_KINDS else "background"
    return None


def _normalize_extracted_items(items: Any) -> list[dict[str, Any]]:
    """将模型输出的 items 列表规范为内部条目结构，并按 Mongo/Qdrant 分工纠正路由。"""
    if not isinstance(items, list):
        return []
    normalized: list[dict[str, Any]] = []
    for item in items[:5]:
        if not isinstance(item, dict):
            continue
        store = str(item.get("store", "")).strip().lower()
        content = str(item.get("content", "")).strip()
        title = str(item.get("title", "")).strip()
        kind = str(item.get("kind", "")).strip().lower()
        coerced = _coerce_dialogue_memory_item(store, kind)
        if coerced is None or not content:
            continue
        store, kind = coerced
        normalized.append(
            {
                "store": store,
                "kind": kind,
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
        await init_memory_services(ensure_kb=False)

    payload = {
        "user_id": user_id,
        "reason": reason,
        "user_message": user_message,
        "assistant_text": assistant_text,
        "recent_messages": recent_messages,
        "mid_summaries": mid_summaries,
    }
    response = await _MANAGER_LLM.ainvoke([
        {"role": "system", "content": get_prompts()["memory_gate_and_extract"]},
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
    """长期记忆写入：1) 单次 LLM 门控+抽取 → 2) 读库 + decision_llm 决策 + 落库。

    Mongo：个人画像与规则；Qdrant 用户记忆库：事件/项目/经历（非 Wiki 知识）；仅 user_memory_collection。
    """
    if _MONGO_CLIENT is None or _QDRANT_CLIENT is None or _EMBEDDINGS is None:
        await init_memory_services(ensure_kb=False)

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
    memory_hints: list[str] | None = None,
) -> list[dict[str, Any]]:
    """按当前问题召回长期记忆。

    - Mongo：profile 全量 + rule/preference 词面匹配（合计上限 mongo_rule_preference_limit，默认 5）
    - Qdrant：Dense= 检索改写句；Sparse= memory_hints 拼接 BM25
    """
    mem = load_config().get("memory", {})
    rp_limit = int(
        mongo_limit
        if mongo_limit is not None
        else mem.get("mongo_rule_preference_limit", mem.get("mongo_retrieval_limit", 5))
    )
    ql = int(qdrant_limit if qdrant_limit is not None else mem.get("long_term_retrieval_limit", 10))

    if user_id <= 0:
        if ql <= 0:
            return []
    elif ql <= 0 and rp_limit <= 0:
        pass

    if _MONGO_CLIENT is None or _QDRANT_CLIENT is None or _EMBEDDINGS is None:
        await init_memory_services()

    if user_id <= 0:
        mongo_hits: list[dict[str, Any]] = []
        qdrant_hits = (
            await asyncio.to_thread(
                _search_qdrant_documents,
                0,
                query,
                ql,
                True,
                memory_hints=memory_hints,
            )
            if ql > 0
            else []
        )
        merged = _merge_memory_hits(mongo_hits, qdrant_hits)
    else:
        mongo_hits = await asyncio.to_thread(
            _search_mongo_documents,
            user_id,
            query,
            rule_preference_limit=rp_limit,
            memory_hints=memory_hints,
        )
        qdrant_hits = (
            await asyncio.to_thread(
                _search_qdrant_documents,
                user_id,
                query,
                ql,
                False,
                memory_hints=memory_hints,
            )
            if ql > 0
            else []
        )
        merged = _merge_memory_hits(mongo_hits, qdrant_hits)

    return merged


async def retrieve_kb_documents(
    query: str,
    *,
    limit: int | None = None,
    memory_hints: list[str] | None = None,
) -> list[dict[str, Any]]:
    """仅检索公共知识库（goblog_kb_documents）；与用户库相同的 hybrid/dense 策略。"""
    mem = load_config().get("memory", {})
    ql = int(limit if limit is not None else mem.get("long_term_retrieval_limit", 10))
    if ql <= 0 or not (query or "").strip():
        return []
    if _QDRANT_CLIENT is None or _EMBEDDINGS is None:
        await init_memory_services()
    return await asyncio.to_thread(
        _search_qdrant_documents,
        0,
        query,
        ql,
        True,
        memory_hints=memory_hints,
    )


def kb_chunk_point_id(article_id: str, chunk_index: int, content_revision: int = 1) -> str:
    """与 ingest_document_chunk_sync 一致的确定性 point id。"""
    import uuid

    return str(
        uuid.uuid5(
            uuid.NAMESPACE_URL,
            f"goblog:kb:{article_id.strip()}:{int(chunk_index)}:{int(content_revision)}",
        )
    )


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


def _sparse_document_from_text(text: str) -> rest.Document | None:
    """服务端 BM25 稀疏向量（Qdrant 分词 + IDF 在服务端计算）。"""
    if not (text or "").strip():
        return None
    return rest.Document(text=text.strip(), model=QDRANT_BM25_MODEL)


def _sparse_text_from_hints(memory_hints: list[str] | None) -> str:
    """路由 memory_hints 拼成一条 sparse 检索文本。"""
    return " ".join(str(h).strip() for h in (memory_hints or []) if str(h).strip())


def _sparse_query_text(query: str, memory_hints: list[str] | None) -> str:
    """BM25 查询文本：仅拼接路由 memory_hints；无 hints 时回退改写句。"""
    hints_text = _sparse_text_from_hints(memory_hints)
    if hints_text:
        return hints_text
    return (query or "").strip()


# 领域内区分度较低的通用检索词（小节名、泛意图）；排序与 Sparse 重排时靠后、权重更低
_LOW_DISCRIMINATION_HINTS: frozenset[str] = frozenset(
    {
        "游戏",
        "问题",
        "攻略",
        "背景",
        "游戏名片",
        "游戏背景",
        "游戏介绍",
        "介绍",
        "玩法",
        "配置",
        "配置需求",
        "注意事项",
        "需求",
        "设定",
        "剧情",
        "元素",
        "新手",
    }
)


def sort_memory_hints_for_retrieval(hints: list[str] | None) -> list[str]:
    """路由后规整 memory_hints：实体/专名优先，领域内低区分度词靠后（与 prompts 约定一致）。"""

    def sort_key(h: str) -> tuple[int, int, str]:
        hs = h.strip()
        if not hs:
            return (3, 0, hs)
        if hs in _LOW_DISCRIMINATION_HINTS:
            return (2, len(hs), hs)
        if len(hs) <= 3 and hs in ("游戏", "问题", "攻略", "配置", "玩法"):
            return (2, len(hs), hs)
        # 实体/专名：非泛词，略长者优先（同 tier 内）
        return (0, -len(hs), hs)

    ordered = sorted((str(h).strip() for h in (hints or []) if str(h).strip()), key=sort_key)
    out: list[str] = []
    seen: set[str] = set()
    for h in ordered:
        key = h.lower()
        if key in seen:
            continue
        seen.add(key)
        out.append(h)
    return out


def _hit_retrieval_blob(hit: dict[str, Any]) -> str:
    parts: list[str] = [
        str(hit.get("content", "")),
        str(hit.get("title", "")),
        str(hit.get("game_name", "")),
        str(hit.get("preview", "")),
    ]
    sp = hit.get("section_path")
    if isinstance(sp, list):
        parts.extend(str(s) for s in sp if s)
    return "\n".join(parts)


def _hint_match_score(hint: str, blob: str) -> float:
    """单条 hint 与文档文本的词面相关度 [0,1]。"""
    h = (hint or "").strip()
    if not h or not blob:
        return 0.0
    if h in blob:
        return 1.0
    compact_h = re.sub(r"\s+", "", h)
    compact_b = re.sub(r"\s+", "", blob)
    if compact_h and compact_h in compact_b:
        return 0.95
    toks = _sparse_tokenize(h)
    if not toks:
        return 0.0
    hits = 0
    for t in toks:
        if len(t) == 1 and "\u4e00" <= t <= "\u9fff":
            if t in blob:
                hits += 1
        elif t in blob.lower():
            hits += 1
    return hits / len(toks)


def _hint_weighted_score_for_hit(hit: dict[str, Any], ordered_hints: list[str]) -> float:
    """按 hints 顺序位次衰减加权；越靠前、区分度越高的词权重越大。"""
    if not ordered_hints:
        return 0.0
    blob = _hit_retrieval_blob(hit)
    total = 0.0
    for i, hint in enumerate(ordered_hints):
        w = 1.0 / (1.0 + i * 0.5)
        total += w * _hint_match_score(hint, blob)
    return total


def _minmax_normalize_scores(scores: dict[str, float]) -> dict[str, float]:
    """将一路分数 min-max 归一化到 [0,1]；全相同则均为 1.0。"""
    if not scores:
        return {}
    vals = list(scores.values())
    vmin, vmax = min(vals), max(vals)
    if vmax <= vmin:
        return {k: 1.0 for k in scores}
    span = vmax - vmin
    return {k: (v - vmin) / span for k, v in scores.items()}


def _rerank_hits_by_memory_hints(
    hits: list[dict[str, Any]],
    memory_hints: list[str] | None,
) -> list[dict[str, Any]]:
    """Sparse 召回后：hint 词面分与 BM25 分各自 min-max 归一化后相加，再按合分重排。"""
    ordered = sort_memory_hints_for_retrieval(memory_hints)
    if not ordered or not hits:
        return hits

    hint_raw: dict[str, float] = {}
    bm25_raw: dict[str, float] = {}
    for h in hits:
        pid = str(h.get("memory_id", "")).strip()
        if not pid:
            continue
        hint_raw[pid] = _hint_weighted_score_for_hit(h, ordered)
        bm25_raw[pid] = float(h.get("sparse_score", h.get("score", 0)) or 0)

    hint_norm = _minmax_normalize_scores(hint_raw)
    bm25_norm = _minmax_normalize_scores(bm25_raw)
    mem = load_config().get("memory", {})
    hw = float(mem.get("hint_rerank_hint_weight", 0.65))
    bw = float(mem.get("hint_rerank_bm25_weight", 0.35))
    tw = hw + bw
    if tw <= 0:
        hw, bw, tw = 0.65, 0.35, 1.0
    hw, bw = hw / tw, bw / tw

    scored: list[tuple[float, dict[str, Any]]] = []
    for h in hits:
        pid = str(h.get("memory_id", "")).strip()
        if not pid:
            continue
        combined = hw * hint_norm.get(pid, 0.0) + bw * bm25_norm.get(pid, 0.0)
        row = dict(h)
        row["hint_score"] = hint_raw.get(pid, 0.0)
        row["sparse_rerank_score"] = combined
        scored.append((combined, row))

    scored.sort(key=lambda x: -x[0])
    return [row for _, row in scored]


def _fuse_dense_sparse_normalized(
    dense_rows: list[dict[str, Any]],
    sparse_rows: list[dict[str, Any]],
    *,
    dense_weight: float,
    sparse_weight: float,
) -> list[tuple[str, float, dict[str, Any]]]:
    """Dense 与 Sparse（BM25）分数分别 min-max 归一化后加权相加，得到最终合分与排序。"""
    by_id: dict[str, dict[str, Any]] = {}
    dense_raw: dict[str, float] = {}
    sparse_raw: dict[str, float] = {}

    for row in dense_rows:
        pid = str(row.get("memory_id", "")).strip()
        if not pid:
            continue
        by_id[pid] = row
        dense_raw[pid] = float(row.get("dense_score", row.get("score", 0)) or 0)

    for row in sparse_rows:
        pid = str(row.get("memory_id", "")).strip()
        if not pid:
            continue
        if pid not in by_id:
            by_id[pid] = row
        sparse_raw[pid] = float(row.get("sparse_score", row.get("score", 0)) or 0)

    all_ids = set(dense_raw) | set(sparse_raw)
    if not all_ids:
        return []

    dense_norm = _minmax_normalize_scores({pid: dense_raw.get(pid, 0.0) for pid in all_ids})
    sparse_norm = _minmax_normalize_scores({pid: sparse_raw.get(pid, 0.0) for pid in all_ids})

    dw = max(0.0, float(dense_weight))
    sw = max(0.0, float(sparse_weight))
    total_w = dw + sw
    if total_w <= 0:
        dw, sw, total_w = 0.55, 0.45, 1.0
    dw, sw = dw / total_w, sw / total_w

    fused: list[tuple[str, float, dict[str, Any]]] = []
    for pid in all_ids:
        combined = dw * dense_norm[pid] + sw * sparse_norm[pid]
        row = dict(by_id[pid])
        row["dense_score_norm"] = dense_norm[pid]
        row["sparse_score_norm"] = sparse_norm[pid]
        fused.append((pid, combined, row))

    fused.sort(key=lambda x: -x[1])
    return fused


def _qdrant_hits_to_rows(hits: list[Any], *, score_field: str = "score") -> list[dict[str, Any]]:
    now = datetime.now(timezone.utc)
    rows: list[dict[str, Any]] = []
    for hit in hits:
        payload = hit.payload or {}
        raw_score = float(hit.score or 0)
        pid = str(getattr(hit, "id", "") or "")
        row = _build_qdrant_row_from_payload(payload, raw_score, now, point_id=pid)
        row[score_field] = raw_score
        rows.append(row)
    return rows


def _vectors_for_point(dense: list[float], content: str) -> dict[str, Any]:
    vecs: dict[str, Any] = {QDRANT_DENSE_VECTOR_NAME: dense}
    sparse_doc = _sparse_document_from_text(content)
    if sparse_doc is not None:
        vecs[QDRANT_SPARSE_VECTOR_NAME] = sparse_doc
    return vecs


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


def _build_qdrant_row_from_payload(
    payload: dict[str, Any],
    base_score: float,
    now: datetime,
    *,
    point_id: str | None = None,
) -> dict[str, Any]:
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
    pid = (point_id or "").strip()
    if pid:
        row["memory_id"] = pid
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


def _memory_recall_queries(query: str, memory_hints: list[str] | None = None) -> list[str]:
    """改写句 + 路由 memory_hints，多路召回取 max（与 tool_retrieval 同思路）。"""
    queries: list[str] = []
    q = (query or "").strip()
    if q:
        queries.append(q)
    for hint in memory_hints or []:
        hs = str(hint).strip()
        if hs and hs not in queries:
            queries.append(hs)
    return queries


def _mongo_query_match_score_multi(queries: list[str], title: str, content: str) -> float:
    if not queries:
        return 0.0
    return max(_mongo_query_match_score(q, title, content) for q in queries)


def _mongo_doc_to_row(doc: dict[str, Any], *, score: float) -> dict[str, Any]:
    fp = str(doc.get("fingerprint", "")).strip()
    row: dict[str, Any] = {
        "store": "mongo",
        "kind": str(doc.get("kind", "profile")),
        "title": str(doc.get("title", "")),
        "content": str(doc.get("content", "")),
        "tags": doc.get("tags", []) if isinstance(doc.get("tags", []), list) else [],
        "importance": int(doc.get("importance", 1) or 1),
        "confidence": float(doc.get("confidence", 0.6) or 0.6),
        "score": score,
        "source": str(doc.get("source", "memory_manager")),
    }
    if fp:
        row["memory_id"] = fp
    return row


def _search_mongo_documents(
    user_id: int,
    query: str,
    *,
    rule_preference_limit: int | None = None,
    memory_hints: list[str] | None = None,
) -> list[dict[str, Any]]:
    """Mongo 召回：profile（用户画像）全量；rule/preference 按 query 词面匹配，合计不超过 rule_preference_limit。"""
    queries = _memory_recall_queries(query, memory_hints)
    cfg = load_config().get("mongo", {})
    mem = load_config().get("memory", {})
    if _MONGO_CLIENT is None:
        return []

    rp_limit = int(
        rule_preference_limit
        if rule_preference_limit is not None
        else mem.get("mongo_rule_preference_limit", mem.get("mongo_retrieval_limit", 5))
    )
    rp_limit = max(0, rp_limit)

    pool_cap = int(mem.get("mongo_retrieval_candidate_cap", 80))
    pool_cap = max(pool_cap, rp_limit * 8)
    pool_cap = min(pool_cap, 200)

    collection = _MONGO_CLIENT[cfg.get("database", "goblog_long_term")][cfg.get("collection", "memory_facts")]
    base_filter = {"user_id": user_id, "status": "active"}

    try:
        always_rows: list[dict[str, Any]] = []
        query_ranked: list[tuple[float, int, dict[str, Any]]] = []

        profile_cursor = collection.find({**base_filter, "kind": {"$in": list(MONGO_ALWAYS_INCLUDE_KINDS)}})
        for doc in profile_cursor:
            imp = int(doc.get("importance", 1) or 1)
            always_rows.append(_mongo_doc_to_row(doc, score=float(imp) + 100.0))

        # legacy：无 kind 或误标为 background 等，视同个人画像全量
        legacy_cursor = collection.find(
            {**base_filter, "kind": {"$nin": list(MONGO_ALWAYS_INCLUDE_KINDS | MONGO_QUERY_MATCH_KINDS)}}
        )
        for doc in legacy_cursor:
            imp = int(doc.get("importance", 1) or 1)
            always_rows.append(_mongo_doc_to_row(doc, score=float(imp) + 100.0))

        query_cursor = (
            collection.find({**base_filter, "kind": {"$in": list(MONGO_QUERY_MATCH_KINDS)}})
            .sort([("importance", -1), ("updated_at", -1)])
            .limit(pool_cap)
        )
        for doc in query_cursor:
            title = str(doc.get("title", ""))
            content = str(doc.get("content", ""))
            rel = _mongo_query_match_score_multi(queries, title, content)
            if rel <= 0:
                continue
            imp = int(doc.get("importance", 1) or 1)
            row = _mongo_doc_to_row(doc, score=float(imp) + 20.0 * rel)
            query_ranked.append((rel, imp, row))

        query_ranked.sort(key=lambda x: (-x[0], -x[1]))
        retrieved = [t[2] for t in query_ranked[:rp_limit]] if rp_limit > 0 else []
        return always_rows + retrieved
    except Exception:
        return []


def _user_memory_filter(user_id: int) -> rest.Filter | None:
    if user_id <= 0:
        return None
    return rest.Filter(
        must=[rest.FieldCondition(key="user_id", match=rest.MatchValue(value=user_id))]
    )


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


def _merge_dense_rows_by_max_score(row_lists: list[list[dict[str, Any]]]) -> list[dict[str, Any]]:
    """多路 Dense 召回：同一 memory_id 保留最高分。"""
    best: dict[str, dict[str, Any]] = {}
    for rows in row_lists:
        for row in rows:
            pid = str(row.get("memory_id", "")).strip()
            if not pid:
                continue
            score = float(row.get("dense_score", row.get("score", 0)) or 0)
            prev = best.get(pid)
            if prev is None or score > float(prev.get("dense_score", prev.get("score", 0)) or 0):
                merged = dict(row)
                merged["dense_score"] = score
                best[pid] = merged
    return sorted(best.values(), key=lambda x: -float(x.get("dense_score", x.get("score", 0)) or 0))


def _dense_rows_for_embedded_queries(
    collection_name: str,
    queries: list[str],
    cap: int,
    query_filter: rest.Filter | None,
) -> list[dict[str, Any]]:
    """多 query 各做 Dense，同 chunk 取 max 分后按分降序（最多 cap 条候选）。"""
    if _QDRANT_CLIENT is None or cap <= 0 or not queries:
        return []
    per_limit = max(1, (cap + len(queries) - 1) // len(queries))
    try:
        vectors = _embed_texts(queries)
        if not vectors or len(vectors) != len(queries):
            return []
    except Exception:
        return []

    row_lists: list[list[dict[str, Any]]] = []
    for query_vector in vectors:
        try:
            hits = _qdrant_vector_search(
                _QDRANT_CLIENT,
                collection_name=collection_name,
                query_vector=query_vector,
                query_filter=query_filter,
                limit=per_limit,
            )
            row_lists.append(_qdrant_hits_to_rows(hits, score_field="dense_score"))
        except Exception:
            continue
    return _merge_dense_rows_by_max_score(row_lists)[:cap]


def _search_qdrant_dense_multi(
    collection_name: str,
    query: str,
    limit: int,
    query_filter: rest.Filter | None,
    *,
    prefetch: int | None = None,
) -> list[dict[str, Any]]:
    """按改写拆出的各检索要点分别做 Dense，再按 point 取 max 分合并。"""
    from core.rag import parse_rewrite_intents

    if _QDRANT_CLIENT is None or limit <= 0 or not (query or "").strip():
        return []

    cap = max(limit, int(prefetch or limit))
    dense_queries = parse_rewrite_intents(query)
    if not dense_queries:
        dense_queries = [(query or "").strip()]
    if not dense_queries[0]:
        return []
    rows = _dense_rows_for_embedded_queries(collection_name, dense_queries, cap, query_filter)
    return rows[:limit]


def _search_qdrant_dense_only(
    collection_name: str,
    query: str,
    limit: int,
    query_filter: rest.Filter | None,
) -> list[dict[str, Any]]:
    """纯向量检索（关闭 hybrid_search 时使用）。"""
    if not (query or "").strip():
        return []
    return _search_qdrant_dense_multi(collection_name, query, limit, query_filter)


def _search_qdrant_sparse_only(
    collection_name: str,
    sparse_text: str,
    limit: int,
    query_filter: rest.Filter | None,
) -> list[dict[str, Any]]:
    if _QDRANT_CLIENT is None or limit <= 0 or not sparse_text.strip():
        return []
    sparse_q = _sparse_document_from_text(sparse_text)
    if sparse_q is None:
        return []
    try:
        resp = _QDRANT_CLIENT.query_points(
            collection_name=collection_name,
            query=sparse_q,
            using=QDRANT_SPARSE_VECTOR_NAME,
            query_filter=query_filter,
            limit=limit,
            with_payload=True,
        )
        return _qdrant_hits_to_rows(list(resp.points or []), score_field="sparse_score")
    except Exception:
        return []


def _search_qdrant_hybrid(
    collection_name: str,
    query: str,
    limit: int,
    query_filter: rest.Filter | None,
    *,
    memory_hints: list[str] | None = None,
) -> list[dict[str, Any]]:
    """Dense= 改写各检索要点分别向量检索后合并；Sparse=memory_hints；再归一化加权融合。"""
    mem = load_config().get("memory", {})
    dense_text = (query or "").strip()
    if _QDRANT_CLIENT is None or limit <= 0 or not dense_text:
        return []

    prefetch_n = max(limit, int(mem.get("hybrid_prefetch_limit", 48)))
    recall_kw = bool(mem.get("hybrid_keyword_recall", True))
    ordered_hints = sort_memory_hints_for_retrieval(memory_hints)

    sparse_text = _sparse_query_text(query, ordered_hints or memory_hints)
    sparse_q = _sparse_document_from_text(sparse_text)

    if not recall_kw or sparse_q is None:
        return _search_qdrant_dense_multi(
            collection_name, query, limit, query_filter, prefetch=prefetch_n
        )

    try:
        dense_rows = _search_qdrant_dense_multi(
            collection_name, query, limit, query_filter, prefetch=prefetch_n
        )
        sparse_rows = _search_qdrant_sparse_only(
            collection_name, sparse_text, prefetch_n, query_filter
        )
        if mem.get("hint_rerank_enabled", True) and ordered_hints:
            sparse_rows = _rerank_hits_by_memory_hints(sparse_rows, ordered_hints)

        fused = _fuse_dense_sparse_normalized(
            dense_rows,
            sparse_rows,
            dense_weight=float(mem.get("hybrid_dense_weight", 0.65)),
            sparse_weight=float(mem.get("hybrid_keyword_weight", 0.35)),
        )
        out: list[dict[str, Any]] = []
        for _pid, combined, row in fused[:limit]:
            row["score"] = float(combined)
            out.append(row)
        return out
    except Exception as e:
        _log.warning(f"hybrid 归一化融合失败，回退纯 dense err={str(e)[:400]}")
        return _search_qdrant_dense_only(collection_name, query, limit, query_filter)


def _search_qdrant_in_collection(
    collection_name: str,
    query: str,
    limit: int,
    query_filter: rest.Filter | None,
    *,
    memory_hints: list[str] | None = None,
) -> list[dict[str, Any]]:
    mem = load_config().get("memory", {})
    if mem.get("hybrid_search", True):
        return _search_qdrant_hybrid(
            collection_name,
            query,
            limit,
            query_filter,
            memory_hints=memory_hints,
        )
        return _search_qdrant_dense_only(
            collection_name, query, limit, query_filter, memory_hints=memory_hints
        )


def _merge_qdrant_hit_lists(*hit_lists: list[dict[str, Any]], limit: int) -> list[dict[str, Any]]:
    merged: list[dict[str, Any]] = []
    for hits in hit_lists:
        merged.extend(hits)
    merged.sort(key=lambda x: float(x.get("score", 0)), reverse=True)
    return merged[:limit]


def _search_qdrant_documents(
    user_id: int,
    query: str,
    limit: int,
    kb_only: bool = False,
    *,
    memory_hints: list[str] | None = None,
) -> list[dict[str, Any]]:
    """分 collection 检索：Dense=改写句；Sparse=memory_hints；登录用户两库各查后合并。"""
    if limit <= 0 or not (query or "").strip():
        return []

    user_col, kb_col = _qdrant_collection_names()
    if kb_only or user_id <= 0:
        return _search_qdrant_in_collection(kb_col, query, limit, None, memory_hints=memory_hints)

    user_hits = _search_qdrant_in_collection(
        user_col,
        query,
        limit,
        _user_memory_filter(user_id),
        memory_hints=memory_hints,
    )
    kb_hits = _search_qdrant_in_collection(kb_col, query, limit, None, memory_hints=memory_hints)
    return _merge_qdrant_hit_lists(user_hits, kb_hits, limit=limit)


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
        "memory_id",
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
    from memory.memory_store import get_mid_summaries, get_short_messages

    cfg = load_config().get("memory", {})
    short_limit = int(cfg.get("short_window_messages", 12))
    mid_limit = int(cfg.get("mid_max_items", 8))
    recent_messages = await get_short_messages(user_id, short_limit)
    mid_summaries = await get_mid_summaries(user_id, mid_limit)
    return recent_messages, mid_summaries


async def _store_memory_items(user_id: int, items: list[dict[str, Any]]) -> dict[str, int]:
    """带决策引擎的长期记忆存储入口。

    Mongo：profile/rule/preference/status（个人画像与规则）。
    Qdrant 用户库：event/project/experience/case/background（用户叙事，非 Wiki 知识）。
    """
    if not items:
        return {"mongo": 0, "qdrant": 0}

    # 反思/对话写入：Mongo + 用户 Qdrant；不初始化 Wiki kb_collection
    await init_memory_services(ensure_kb=False)

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
    user_col, _kb_col = _qdrant_collection_names()
    collection = _user_memory_collection_name()
    assert collection == user_col
    _ensure_user_memory_collection_sync()
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
        await init_memory_services(ensure_kb=False)
    if _DECISION_LLM is None:
        return {"mongo_actions": [], "qdrant_actions": []}

    system_prompt = get_prompts()["memory_decision"]

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
    """执行 Qdrant 决策：新增向量 / 降低置信度（仅 user_memory_collection，非 Wiki kb_collection）。"""
    if not actions or user_id <= 0:
        return 0
    if _QDRANT_CLIENT is None:
        _log.warning(
            "Qdrant 未初始化，跳过长期记忆向量写入",
            extra={"user_id": user_id, "actions": len(actions)},
        )
        return 0

    _ensure_user_memory_collection_sync()
    collection = _user_memory_collection_name()
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

    _ensure_kb_collection_sync()
    collection = _kb_collection_name()

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


def ingest_markdown_document_sync(
    article_id: str,
    markdown: str,
    *,
    content_revision: int = 1,
    source: str,
    game_name: str | None = None,
    chunk_size: int = 900,
    chunk_overlap: int = 80,
    section_order: list[str] | None = None,
) -> dict[str, Any]:
    """将整篇 Markdown 分片后写入公共 Qdrant 知识库。"""
    from eval.markdown_chunker import assign_chunk_indices, chunk_markdown

    article_id = article_id.strip()
    markdown = markdown.strip()
    source = source.strip()
    if not article_id or not markdown or not source:
        raise ValueError("article_id, markdown, source must be non-empty")
    if content_revision < 1:
        raise ValueError("content_revision must be >= 1")
    if chunk_size < 200:
        chunk_size = 200

    if _QDRANT_CLIENT is None or _EMBEDDINGS is None:
        raise RuntimeError("memory services not initialized; call init_memory_services first")

    _ensure_kb_collection_sync()
    doc_title = (game_name or "").strip()
    chunks = chunk_markdown(
        markdown,
        chunk_size=chunk_size,
        chunk_overlap=chunk_overlap,
        doc_title=doc_title,
    )
    indexed = assign_chunk_indices(chunks, section_order=section_order)

    point_ids: list[str] = []
    for ch, ci in indexed:
        out = ingest_document_chunk_sync(
            article_id,
            ci,
            ch.content,
            content_revision=content_revision,
            source=source,
            ingest_kind="raw_chunk",
            game_name=game_name,
            section_path=ch.section_path,
            preview=ch.content[:240],
        )
        point_ids.append(str(out.get("point_id", "")))

    collection = _kb_collection_name()
    return {
        "collection": collection,
        "article_id": article_id,
        "chunks_ingested": len(point_ids),
        "point_ids": point_ids,
    }


async def ingest_markdown_document(
    article_id: str,
    markdown: str,
    *,
    content_revision: int = 1,
    source: str,
    game_name: str | None = None,
    chunk_size: int = 900,
    chunk_overlap: int = 80,
    section_order: list[str] | None = None,
) -> dict[str, Any]:
    await init_memory_services()
    return await asyncio.to_thread(
        ingest_markdown_document_sync,
        article_id,
        markdown,
        content_revision=content_revision,
        source=source,
        game_name=game_name,
        chunk_size=chunk_size,
        chunk_overlap=chunk_overlap,
        section_order=section_order,
    )
