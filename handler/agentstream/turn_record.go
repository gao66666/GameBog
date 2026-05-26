package agentstream

// AgentTurnRecord 单轮 Agent 对话生命周期快照（Go ChatProxy 从 SSE fact 聚合）。
type AgentTurnRecord struct {
	SchemaVersion int    `json:"schema_version"`
	RequestID     string `json:"request_id"`
	UserID        uint64 `json:"user_id"`
	ChatSessionID string `json:"chat_session_id,omitempty"`
	Status        string `json:"status"` // ok | error | aborted
	StartedAtMs   int64  `json:"started_at_ms"`
	FinishedAtMs  int64  `json:"finished_at_ms"`
	TotalMs       int64  `json:"total_ms"`

	Input struct {
		Message string `json:"message"`
	} `json:"input"`

	Rewrite      *TurnRewrite      `json:"rewrite,omitempty"`
	Route        *TurnRoute        `json:"route,omitempty"`
	ToolRetrieve *TurnToolRetrieve `json:"tool_retrieve,omitempty"`
	Analyses     []TurnAnalyse     `json:"analyses,omitempty"`
	Retrieves    []TurnRetrieve    `json:"retrieves,omitempty"`
	Plans        []TurnPlan        `json:"plans,omitempty"`
	Tools        []TurnToolCall    `json:"tools,omitempty"`
	Output       *TurnOutput       `json:"output,omitempty"`

	PlanningCycleComplete bool           `json:"planning_cycle_complete"`
	EarlyExit             string         `json:"early_exit,omitempty"`
	Timings               map[string]int      `json:"timings,omitempty"`
	Usage                 *TurnTokenUsage     `json:"usage,omitempty"`
	Error                 *TurnError          `json:"error,omitempty"`
}

// TurnTokenUsage 单轮 LLM token 消耗（Agent done 帧 usage 字段）。
type TurnTokenUsage struct {
	InputTokens  int                       `json:"input_tokens"`
	OutputTokens int                       `json:"output_tokens"`
	TotalTokens  int                       `json:"total_tokens"`
	ByPhase      map[string]TurnPhaseUsage `json:"by_phase,omitempty"`
}

type TurnPhaseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type TurnRewrite struct {
	Text       string `json:"text"`
	DurationMs int    `json:"duration_ms,omitempty"`
}

type TurnRoute struct {
	Servers     []string `json:"servers"`
	ToolHints   []string `json:"tool_hints,omitempty"`
	MemoryHints []string `json:"memory_hints,omitempty"`
	HasToken    bool     `json:"has_token"`
	DurationMs  int      `json:"duration_ms,omitempty"`
}

type TurnToolRetrieve struct {
	Tools      []string `json:"tools"`
	Mode       string   `json:"mode,omitempty"`
	Count      int      `json:"count,omitempty"`
	DurationMs int      `json:"duration_ms,omitempty"`
}

type TurnAnalyse struct {
	Phase       string `json:"phase,omitempty"`
	Cycle       int    `json:"cycle,omitempty"`
	Disposition string `json:"disposition"`
	Intent      string `json:"intent,omitempty"`
	DurationMs  int    `json:"duration_ms,omitempty"`
}

type TurnMemoryRef struct {
	Store string   `json:"store"`
	ID    string   `json:"id"`
	Kind  string   `json:"kind,omitempty"`
	Score *float64 `json:"score,omitempty"`
}

type TurnRetrieve struct {
	Hits       int              `json:"hits"`
	Memories   []TurnMemoryRef  `json:"memories,omitempty"`
	DurationMs int              `json:"duration_ms,omitempty"`
}

type TurnPlan struct {
	RequiresTools bool `json:"requires_tools"`
	StepCount     int  `json:"step_count"`
	DurationMs    int  `json:"duration_ms,omitempty"`
}

// TurnToolCall 不含工具 output / preview，仅调用元数据。
type TurnToolCall struct {
	ToolCallID  string         `json:"tool_call_id"`
	Tool        string         `json:"tool"`
	ArgsSummary map[string]any `json:"args_summary,omitempty"`
	OK          bool           `json:"ok"`
	RowCount    *int           `json:"row_count,omitempty"`
	ErrorCode   string         `json:"error_code,omitempty"`
	DurationMs  int            `json:"duration_ms,omitempty"`
}

type TurnOutput struct {
	Text       string `json:"text"`
	Preview    string `json:"preview,omitempty"`
	DurationMs int    `json:"duration_ms,omitempty"`
	CharCount  int    `json:"char_count"`
}

type TurnError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
