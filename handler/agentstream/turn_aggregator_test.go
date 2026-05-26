package agentstream

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTurnAggregatorLifecycle(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	agg := NewTurnAggregator(TurnMeta{
		RequestID:     "req_1",
		UserID:        42,
		ChatSessionID: "s1",
		UserMessage:   "搜一下 Go 文章",
		StartedAt:     start,
	})

	dur := 12
	agg.Observe(FactEnvelope{
		Phase: "rewrite", Kind: "stage", Status: "end", DurationMs: &dur,
		Payload: mustJSON(map[string]any{
			"output": map[string]any{"rewritten": "搜索 Go 语言相关文章"},
		}),
	})
	agg.Observe(FactEnvelope{
		Phase: "route", Kind: "stage", Status: "end", DurationMs: &dur,
		Payload: mustJSON(map[string]any{
			"output": map[string]any{
				"servers": []string{"public"}, "tool_hints": []string{"文章搜索"},
				"has_token": false,
			},
		}),
	})
	agg.Observe(FactEnvelope{
		Phase: "retrieve", Kind: "stage", Status: "end", DurationMs: &dur,
		Payload: mustJSON(map[string]any{
			"output": map[string]any{
				"hits": 2,
				"memories": []map[string]any{
					{"store": "mongo", "id": "fp_abc", "kind": "profile", "score": 1.2},
					{"store": "qdrant", "id": "pt_xyz", "kind": "experience", "score": 0.88},
				},
			},
		}),
	})
	agg.Observe(FactEnvelope{
		Phase: "tool_invoke", Kind: "stage", Status: "start",
		Payload: mustJSON(map[string]any{
			"tool_call_id": "tc_1",
			"input":        map[string]any{"tool": "search_articles", "args_summary": map[string]any{"query": "Go"}},
		}),
	})
	row := 3
	agg.Observe(FactEnvelope{
		Phase: "tool_invoke", Kind: "stage", Status: "end", DurationMs: &dur,
		Payload: mustJSON(map[string]any{
			"tool_call_id": "tc_1",
			"input":        map[string]any{"tool": "search_articles", "args_summary": map[string]any{"query": "Go"}},
			"output": map[string]any{
				"ok": true, "row_count": row, "preview": "should-not-store",
			},
		}),
	})
	agg.Observe(FactEnvelope{
		Phase: "done", Kind: "control", Status: "end",
		Payload: mustJSON(map[string]any{
			"timings":                 map[string]int{"total_ms": 900},
			"planning_cycle_complete": true,
		}),
	})

	rec := agg.Finalize(TurnFinalizeInput{OutputText: "找到 3 篇相关文章。"})
	if rec.Status != "ok" {
		t.Fatalf("status=%s", rec.Status)
	}
	if rec.Rewrite == nil || rec.Rewrite.Text == "" {
		t.Fatal("missing rewrite")
	}
	if len(rec.Tools) != 1 {
		t.Fatalf("tools=%d", len(rec.Tools))
	}
	if rec.Tools[0].Tool != "search_articles" || !rec.Tools[0].OK {
		t.Fatalf("tool=%+v", rec.Tools[0])
	}
	if rec.Tools[0].RowCount == nil || *rec.Tools[0].RowCount != 3 {
		t.Fatalf("row_count=%v", rec.Tools[0].RowCount)
	}
	b, _ := json.Marshal(rec)
	if strings.Contains(string(b), "should-not-store") {
		t.Fatal("tool preview leaked into record")
	}
	if rec.Output == nil || rec.Output.Text == "" {
		t.Fatal("missing output")
	}
	if rec.Timings["total_ms"] != 900 {
		t.Fatalf("timings=%v", rec.Timings)
	}
	if len(rec.Retrieves) != 1 || len(rec.Retrieves[0].Memories) != 2 {
		t.Fatalf("retrieves=%+v", rec.Retrieves)
	}
	if rec.Retrieves[0].Memories[0].ID != "fp_abc" {
		t.Fatalf("memory ref=%+v", rec.Retrieves[0].Memories[0])
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
