package agentstream

import (
	"encoding/json"
	"testing"
)

func TestTranslateToolInvokeStart(t *testing.T) {
	tr, err := NewTranslator()
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"tool_call_id": "tc_1",
		"input": map[string]any{
			"tool":         "search_articles",
			"args_summary": map[string]any{"query": "复活道具"},
		},
	})
	env := FactEnvelope{
		V: 1, Kind: "stage", Phase: "tool_invoke", Status: "start",
		RequestID: "r1", Payload: payload,
	}
	res := tr.Translate(env)
	if len(res.Events) != 1 {
		t.Fatalf("events=%d", len(res.Events))
	}
	if res.Events[0].Type != "tool_start" {
		t.Fatalf("type=%s", res.Events[0].Type)
	}
	if res.Events[0].Content == "" {
		t.Fatal("empty content")
	}
	if !contains(res.Events[0].Content, "复活道具") {
		t.Fatalf("content=%q", res.Events[0].Content)
	}
}

func TestTranslateToolInvokeEndFail(t *testing.T) {
	tr, err := NewTranslator()
	if err != nil {
		t.Fatal(err)
	}
	row := 0
	payload, _ := json.Marshal(map[string]any{
		"tool_call_id": "tc_1",
		"input": map[string]any{
			"tool":         "search_articles",
			"args_summary": map[string]any{"query": "x"},
		},
		"output": map[string]any{"ok": false, "row_count": row, "preview": ""},
	})
	env := FactEnvelope{
		V: 1, Kind: "stage", Phase: "tool_invoke", Status: "end",
		Payload: payload,
	}
	res := tr.Translate(env)
	if len(res.Events) < 2 {
		t.Fatalf("events=%d", len(res.Events))
	}
	foundThinking := false
	for _, ev := range res.Events {
		if ev.Type == "thinking" {
			foundThinking = true
		}
		if ev.Type == "tool_end" && ev.Meta["partial"] != true {
			t.Fatalf("meta=%v", ev.Meta)
		}
	}
	if !foundThinking {
		t.Fatal("missing partial thinking")
	}
}

func TestTranslateToolInvokeEndOkMissingRowCount(t *testing.T) {
	tr, err := NewTranslator()
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"tool_call_id": "tc_2",
		"input": map[string]any{
			"tool":         "list_latest_articles",
			"args_summary": map[string]any{},
		},
		"output": map[string]any{"ok": true, "preview": "..."},
	})
	env := FactEnvelope{
		V: 1, Kind: "stage", Phase: "tool_invoke", Status: "end",
		Payload: payload,
	}
	res := tr.Translate(env)
	var endContent string
	for _, ev := range res.Events {
		if ev.Type == "tool_end" {
			endContent = ev.Content
		}
	}
	if endContent == "" {
		t.Fatal("missing tool_end")
	}
	if contains(endContent, "{{") {
		t.Fatalf("unreplaced template: %q", endContent)
	}
}

func TestTranslateToolInvokeStartMissingQuery(t *testing.T) {
	tr, err := NewTranslator()
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"tool_call_id": "tc_3",
		"input": map[string]any{
			"tool":         "search_articles",
			"args_summary": map[string]any{},
		},
	})
	env := FactEnvelope{
		V: 1, Kind: "stage", Phase: "tool_invoke", Status: "start",
		Payload: payload,
	}
	res := tr.Translate(env)
	if len(res.Events) != 1 {
		t.Fatalf("events=%d", len(res.Events))
	}
	if contains(res.Events[0].Content, "{{") {
		t.Fatalf("unreplaced start template: %q", res.Events[0].Content)
	}
}

func TestSkipLegacyAfterFact(t *testing.T) {
	tr, _ := NewTranslator()
	saw := false
	fact := []byte(`{"v":1,"kind":"chunk","phase":"llm_output","status":"delta","seq":1,"request_id":"r","ts":1,"payload":{"delta":"hi"}}`)
	lines, delta, _ := ProcessAgentDataPayload(tr, fact, &saw)
	if !saw || delta != "hi" || len(lines) != 1 {
		t.Fatalf("saw=%v delta=%q lines=%d", saw, delta, len(lines))
	}
	legacy := []byte(`{"type":"token","content":"hi"}`)
	lines2, delta2, _ := ProcessAgentDataPayload(tr, legacy, &saw)
	if len(lines2) != 0 || delta2 != "" {
		t.Fatalf("legacy should skip lines=%d delta=%q", len(lines2), delta2)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || len(s) > 0 && containsHelper(s, sub)))
}

func containsHelper(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
