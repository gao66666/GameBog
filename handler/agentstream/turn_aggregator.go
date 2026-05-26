package agentstream

import (
	"encoding/json"
	"time"
)

const turnRecordSchemaVersion = 1

// TurnMeta ChatProxy 侧已知上下文。
type TurnMeta struct {
	RequestID     string
	UserID        uint64
	ChatSessionID string
	UserMessage   string
	StartedAt     time.Time
}

// TurnFinalizeInput 流结束时的补充信息。
type TurnFinalizeInput struct {
	OutputText string
	StreamErr  bool
	Aborted    bool
}

// TurnAggregator 从 Agent SSE fact 帧聚合单轮生命周期。
type TurnAggregator struct {
	meta          TurnMeta
	pendingTools  map[string]*TurnToolCall
	record        AgentTurnRecord
	sawDone       bool
	sawError      bool
}

func NewTurnAggregator(meta TurnMeta) *TurnAggregator {
	rec := AgentTurnRecord{
		SchemaVersion: turnRecordSchemaVersion,
		RequestID:     meta.RequestID,
		UserID:        meta.UserID,
		ChatSessionID: meta.ChatSessionID,
		Status:        "ok",
		StartedAtMs:   meta.StartedAt.UnixMilli(),
		Timings:       map[string]int{},
	}
	rec.Input.Message = meta.UserMessage
	return &TurnAggregator{
		meta:         meta,
		pendingTools: map[string]*TurnToolCall{},
		record:       rec,
	}
}

func (a *TurnAggregator) Observe(env FactEnvelope) {
	if a == nil {
		return
	}
	dur := durationMs(env.DurationMs)

	switch env.Kind {
	case "control":
		switch env.Phase {
		case "done":
			a.sawDone = true
			a.observeDone(env.Payload)
		case "error":
			a.sawError = true
			a.record.Status = "error"
			a.observeError(env.Payload)
		}
	case "stage":
		switch env.Phase {
		case "rewrite":
			a.observeRewrite(env.Payload, dur)
		case "route":
			a.observeRoute(env.Payload, dur)
		case "tool_retrieve":
			a.observeToolRetrieve(env.Payload, dur)
		case "analyse":
			a.observeAnalyse(env.Payload, dur)
		case "retrieve":
			a.observeRetrieve(env.Payload, dur)
		case "plan":
			a.observePlan(env.Payload, dur)
		case "output":
			a.observeOutputStage(env.Payload, dur)
		case "tool_invoke":
			if env.Status == "start" {
				a.observeToolStart(env.Payload)
			} else if env.Status == "end" {
				a.observeToolEnd(env.Payload, dur)
			}
		}
	}
}

func (a *TurnAggregator) Finalize(in TurnFinalizeInput) AgentTurnRecord {
	if a == nil {
		return AgentTurnRecord{}
	}
	now := time.Now()
	a.record.FinishedAtMs = now.UnixMilli()
	if !a.meta.StartedAt.IsZero() {
		a.record.TotalMs = now.Sub(a.meta.StartedAt).Milliseconds()
	}

	text := in.OutputText
	if text != "" {
		a.record.Output = &TurnOutput{
			Text:      text,
			CharCount: len([]rune(text)),
		}
		if a.record.Output.Preview == "" && len(text) > 240 {
			a.record.Output.Preview = string([]rune(text)[:240])
		}
	}

	switch {
	case in.Aborted:
		a.record.Status = "aborted"
	case in.StreamErr || a.sawError:
		a.record.Status = "error"
	case !a.sawDone:
		a.record.Status = "aborted"
	default:
		a.record.Status = "ok"
	}

	// 丢弃未闭合的工具 start
	for _, pending := range a.pendingTools {
		if pending != nil {
			a.record.Tools = append(a.record.Tools, *pending)
		}
	}
	a.pendingTools = nil
	return a.record
}

func durationMs(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func (a *TurnAggregator) observeRewrite(payload json.RawMessage, dur int) {
	var body struct {
		Output struct {
			Rewritten string `json:"rewritten"`
		} `json:"output"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return
	}
	a.record.Rewrite = &TurnRewrite{Text: body.Output.Rewritten, DurationMs: dur}
}

func (a *TurnAggregator) observeRoute(payload json.RawMessage, dur int) {
	var body struct {
		Output struct {
			Servers     []string `json:"servers"`
			ToolHints   []string `json:"tool_hints"`
			MemoryHints []string `json:"memory_hints"`
			HasToken    bool     `json:"has_token"`
		} `json:"output"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return
	}
	a.record.Route = &TurnRoute{
		Servers:     append([]string(nil), body.Output.Servers...),
		ToolHints:   append([]string(nil), body.Output.ToolHints...),
		MemoryHints: append([]string(nil), body.Output.MemoryHints...),
		HasToken:    body.Output.HasToken,
		DurationMs:  dur,
	}
}

func (a *TurnAggregator) observeToolRetrieve(payload json.RawMessage, dur int) {
	var body struct {
		Output struct {
			Tools []string `json:"tools"`
			Mode  string   `json:"mode"`
			Count int      `json:"count"`
		} `json:"output"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return
	}
	tools := body.Output.Tools
	if len(tools) == 0 && body.Output.Count > 0 {
		// 兼容仅 count 字段
		tools = nil
	}
	a.record.ToolRetrieve = &TurnToolRetrieve{
		Tools:      append([]string(nil), tools...),
		Mode:       body.Output.Mode,
		Count:      body.Output.Count,
		DurationMs: dur,
	}
}

func (a *TurnAggregator) observeAnalyse(payload json.RawMessage, dur int) {
	var body struct {
		Output struct {
			Disposition string `json:"disposition"`
			Intent      string `json:"intent"`
			Phase       string `json:"phase"`
			Cycle       int    `json:"cycle"`
		} `json:"output"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return
	}
	a.record.Analyses = append(a.record.Analyses, TurnAnalyse{
		Phase:       body.Output.Phase,
		Cycle:       body.Output.Cycle,
		Disposition: body.Output.Disposition,
		Intent:      body.Output.Intent,
		DurationMs:  dur,
	})
}

func (a *TurnAggregator) observeRetrieve(payload json.RawMessage, dur int) {
	var body struct {
		Output struct {
			Hits     int `json:"hits"`
			Memories []struct {
				Store string   `json:"store"`
				ID    string   `json:"id"`
				Kind  string   `json:"kind"`
				Score *float64 `json:"score"`
			} `json:"memories"`
		} `json:"output"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return
	}
	tr := TurnRetrieve{
		Hits:       body.Output.Hits,
		DurationMs: dur,
	}
	for _, m := range body.Output.Memories {
		if m.Store == "" || m.ID == "" {
			continue
		}
		tr.Memories = append(tr.Memories, TurnMemoryRef{
			Store: m.Store,
			ID:    m.ID,
			Kind:  m.Kind,
			Score: m.Score,
		})
	}
	a.record.Retrieves = append(a.record.Retrieves, tr)
}

func (a *TurnAggregator) observePlan(payload json.RawMessage, dur int) {
	var body struct {
		Output struct {
			RequiresTools bool `json:"requires_tools"`
			StepCount     int  `json:"step_count"`
		} `json:"output"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return
	}
	a.record.Plans = append(a.record.Plans, TurnPlan{
		RequiresTools: body.Output.RequiresTools,
		StepCount:     body.Output.StepCount,
		DurationMs:    dur,
	})
}

func (a *TurnAggregator) observeOutputStage(payload json.RawMessage, dur int) {
	var body struct {
		Output struct {
			ReplyPreview string `json:"reply_preview"`
		} `json:"output"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return
	}
	if a.record.Output == nil {
		a.record.Output = &TurnOutput{}
	}
	a.record.Output.Preview = body.Output.ReplyPreview
	a.record.Output.DurationMs = dur
}

func (a *TurnAggregator) observeToolStart(payload json.RawMessage) {
	var body struct {
		ToolCallID string `json:"tool_call_id"`
		Input      struct {
			Tool        string         `json:"tool"`
			ArgsSummary map[string]any `json:"args_summary"`
		} `json:"input"`
	}
	if json.Unmarshal(payload, &body) != nil || body.ToolCallID == "" {
		return
	}
	a.pendingTools[body.ToolCallID] = &TurnToolCall{
		ToolCallID:  body.ToolCallID,
		Tool:        body.Input.Tool,
		ArgsSummary: body.Input.ArgsSummary,
	}
}

func (a *TurnAggregator) observeToolEnd(payload json.RawMessage, dur int) {
	var body struct {
		ToolCallID string `json:"tool_call_id"`
		Input      struct {
			Tool        string         `json:"tool"`
			ArgsSummary map[string]any `json:"args_summary"`
		} `json:"input"`
		Output struct {
			OK        bool   `json:"ok"`
			ErrorCode string `json:"error_code"`
			RowCount  *int   `json:"row_count"`
		} `json:"output"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return
	}
	id := body.ToolCallID
	rec := a.pendingTools[id]
	if rec == nil {
		rec = &TurnToolCall{ToolCallID: id}
	} else {
		delete(a.pendingTools, id)
	}
	if rec.Tool == "" {
		rec.Tool = body.Input.Tool
	}
	if rec.ArgsSummary == nil && body.Input.ArgsSummary != nil {
		rec.ArgsSummary = body.Input.ArgsSummary
	}
	rec.OK = body.Output.OK
	rec.ErrorCode = body.Output.ErrorCode
	rec.RowCount = body.Output.RowCount
	rec.DurationMs = dur
	a.record.Tools = append(a.record.Tools, *rec)
}

func (a *TurnAggregator) observeDone(payload json.RawMessage) {
	var body struct {
		Timings               map[string]int `json:"timings"`
		PlanningCycleComplete bool           `json:"planning_cycle_complete"`
		EarlyExit             string         `json:"early_exit"`
		Usage                 *TurnTokenUsage `json:"usage"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return
	}
	if len(body.Timings) > 0 {
		a.record.Timings = body.Timings
	}
	a.record.PlanningCycleComplete = body.PlanningCycleComplete
	a.record.EarlyExit = body.EarlyExit
	if body.Usage != nil && body.Usage.TotalTokens > 0 {
		a.record.Usage = body.Usage
	} else if body.Usage != nil && (body.Usage.InputTokens > 0 || body.Usage.OutputTokens > 0) {
		u := *body.Usage
		if u.TotalTokens == 0 {
			u.TotalTokens = u.InputTokens + u.OutputTokens
		}
		a.record.Usage = &u
	}
}

func (a *TurnAggregator) observeError(payload json.RawMessage) {
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return
	}
	a.record.Error = &TurnError{Code: body.Code, Message: body.Message}
}
