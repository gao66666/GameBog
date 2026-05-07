"""
配置加载模块 —— 从 agent/config.yaml 读取 Agent 配置
"""

import os
import yaml

_CONFIG = None


def load_config() -> dict:
    """加载 agent/config.yaml 中的 agent 配置"""
    global _CONFIG
    if _CONFIG is not None:
        return _CONFIG

    # 从 agent 目录读取 config.yaml
    yaml_path = os.path.join(os.path.dirname(__file__), "config.yaml")
    yaml_path = os.path.abspath(yaml_path)

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
    embed_cfg = data.get("embedding", {})
    blog_api_url = data.get("blog_api_url", "http://127.0.0.1:8084/api/v1")

    document_ingest_secret = (os.getenv("DOCUMENT_INGEST_SECRET") or "").strip() or data.get(
        "document_ingest_secret", ""
    )

    _CONFIG = {
        "base_url": llm_cfg.get("base_url", "https://api.openai.com/v1"),
        "api_key": llm_cfg.get("api_key", "sk-your-key-here"),
        "model": llm_cfg.get("model", "gpt-4o-mini"),
        "temperature": llm_cfg.get("temperature", 0.7),
        "thinking": llm_cfg.get("thinking", False),
        "blog_api_url": blog_api_url,
        "summary_llm": {
            "base_url": summary_llm_cfg.get("base_url", llm_cfg.get("base_url", "https://api.openai.com/v1")),
            "api_key": summary_llm_cfg.get("api_key", llm_cfg.get("api_key", "sk-your-key-here")),
            "model": summary_llm_cfg.get("model", llm_cfg.get("model", "gpt-4o-mini")),
            "temperature": summary_llm_cfg.get("temperature", 0),
            "thinking": summary_llm_cfg.get("thinking", False),
        },
        "memory_manager_llm": {
            "base_url": memory_manager_llm_cfg.get("base_url", summary_llm_cfg.get("base_url", llm_cfg.get("base_url", "https://api.openai.com/v1"))),
            "api_key": memory_manager_llm_cfg.get("api_key", summary_llm_cfg.get("api_key", llm_cfg.get("api_key", "sk-your-key-here"))),
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
            "collection": qdrant_cfg.get("collection", "goblog_long_term_memory"),
            "vector_size": qdrant_cfg.get("vector_size", 1024),
        },
        "embedding": {
            "base_url": embed_cfg.get("base_url", llm_cfg.get("base_url", "https://open.bigmodel.cn/api/paas/v4")),
            "api_key": embed_cfg.get("api_key", llm_cfg.get("api_key", "")),
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
            "long_term_retrieval_limit": memory_cfg.get("long_term_retrieval_limit", 6),
            "mongo_retrieval_limit": int(memory_cfg.get("mongo_retrieval_limit", 10)),
            "session_idle_timeout_minutes": memory_cfg.get("session_idle_timeout_minutes", 30),
            "summary_enabled": memory_cfg.get("summary_enabled", True),
            "hybrid_search": memory_cfg.get("hybrid_search", True),
            "hybrid_prefetch_limit": memory_cfg.get("hybrid_prefetch_limit", 48),
            "hybrid_max_union_points": memory_cfg.get("hybrid_max_union_points", 200),
            "hybrid_dense_weight": memory_cfg.get("hybrid_dense_weight", 0.65),
            "hybrid_keyword_weight": memory_cfg.get("hybrid_keyword_weight", 0.35),
            "hybrid_keyword_recall": memory_cfg.get("hybrid_keyword_recall", True),
            "hybrid_rrf_k": memory_cfg.get("hybrid_rrf_k"),
            "mongo_retrieval_candidate_cap": int(memory_cfg.get("mongo_retrieval_candidate_cap", 80)),
        },
        "document_ingest_secret": document_ingest_secret,
        "decision_llm": {
            "base_url": decision_llm_cfg.get("base_url", memory_manager_llm_cfg.get("base_url", summary_llm_cfg.get("base_url", llm_cfg.get("base_url", "https://api.openai.com/v1")))),
            "api_key": decision_llm_cfg.get("api_key", memory_manager_llm_cfg.get("api_key", summary_llm_cfg.get("api_key", llm_cfg.get("api_key", "sk-your-key-here")))),
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
