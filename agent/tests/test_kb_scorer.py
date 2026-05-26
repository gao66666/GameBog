"""kb_scorer 单测。"""

from eval.kb_scorer import kb_chunk_point_id, kb_retrieval_metrics, score_kb_rag_case


def test_kb_recall_mrr():
    exp = ("game:elden-ring", 300, 1)
    pid = kb_chunk_point_id(exp[0], exp[1], exp[2])
    hits = [
        {
            "memory_scope": "document",
            "article_id": "game:apex",
            "chunk_index": 300,
            "section_path": ["APEX", "玩法"],
            "memory_id": "x",
        },
        {
            "memory_scope": "document",
            "article_id": exp[0],
            "chunk_index": exp[1],
            "section_path": ["艾尔登法环", "玩法"],
            "memory_id": pid,
        },
    ]
    m = kb_retrieval_metrics(
        hits,
        expected_chunks=[],
        expected_sections=[{"article_id": exp[0], "section": "玩法"}],
        k=5,
    )
    assert m["recall_at_k"] == 1.0
    assert m["mrr"] == 0.5
