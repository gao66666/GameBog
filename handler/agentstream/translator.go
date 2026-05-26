package agentstream

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gao66666/GoBlog/shared"
)

// PresentationEvent 写给前端的用户可见 SSE 事件。
type PresentationEvent struct {
	Type     string         `json:"type"`
	Content  string         `json:"content,omitempty"`
	Skeleton string         `json:"skeleton,omitempty"`
	Meta     map[string]any `json:"meta,omitempty"`
}

// TranslateResult 翻译结果。
type TranslateResult struct {
	Events      []PresentationEvent
	AnswerDelta string
	StreamError bool
}

// Translator fact → presentation。
type Translator struct {
	reg *shared.ToolsRegistry
}

func NewTranslator() (*Translator, error) {
	reg, err := shared.LoadToolsRegistry()
	if err != nil {
		return nil, err
	}
	return &Translator{reg: reg}, nil
}

func (t *Translator) partialHint() string {
	if t.reg != nil && t.reg.Presentation.PartialUnavailableHint != "" {
		return t.reg.Presentation.PartialUnavailableHint
	}
	return "部分信息暂时不可用，将继续为您整理已有结果"
}

func (t *Translator) toolSpec(name string) (shared.ToolRegistrySpec, bool) {
	if t.reg == nil || t.reg.Tools == nil {
		return shared.ToolRegistrySpec{}, false
	}
	spec, ok := t.reg.Tools[name]
	return spec, ok
}

func templateVars(spec shared.ToolRegistrySpec, args map[string]any, extra map[string]string) map[string]string {
	vars := map[string]string{"label": spec.Label}
	for k, v := range args {
		vars[k] = stringFromAny(v)
	}
	for k, v := range extra {
		vars[k] = v
	}
	return vars
}

func shortenIntent(intent string, maxRunes int) string {
	intent = strings.TrimSpace(intent)
	if intent == "" {
		return "正在理解您的问题…"
	}
	if utf8.RuneCountInString(intent) <= maxRunes {
		return intent
	}
	runes := []rune(intent)
	return string(runes[:maxRunes]) + "…"
}

// Translate 将单条 fact 帧译为用户事件；rewrite/route 等无展示项返回 nil Events。
func (t *Translator) Translate(env FactEnvelope) TranslateResult {
	var res TranslateResult

	switch env.Kind {
	case "control":
		switch env.Phase {
		case "error":
			var p struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(env.Payload, &p)
			msg := strings.TrimSpace(p.Message)
			if msg == "" {
				msg = "服务暂时不可用，请稍后再试"
			}
			res.Events = []PresentationEvent{{Type: "error", Content: msg}}
			res.StreamError = true
		case "done":
			var p struct {
				Timings               map[string]any `json:"timings"`
				PlanningCycleComplete bool           `json:"planning_cycle_complete"`
				EarlyExit             string         `json:"early_exit"`
				Usage                 map[string]any `json:"usage"`
			}
			_ = json.Unmarshal(env.Payload, &p)
			meta := map[string]any{
				"timings":                 p.Timings,
				"planning_cycle_complete": p.PlanningCycleComplete,
				"early_exit":              p.EarlyExit,
			}
			if len(p.Usage) > 0 {
				meta["usage"] = p.Usage
			}
			res.Events = []PresentationEvent{{
				Type: "done",
				Meta: meta,
			}}
		}
	case "chunk":
		if env.Phase != "llm_output" || env.Status != "delta" {
			return res
		}
		var p struct {
			Delta string `json:"delta"`
		}
		_ = json.Unmarshal(env.Payload, &p)
		if p.Delta == "" {
			return res
		}
		res.AnswerDelta = p.Delta
		res.Events = []PresentationEvent{{Type: "text_chunk", Content: p.Delta}}
	case "stage":
		switch env.Phase {
		case "analysis_pre", "analyse":
			if env.Status != "end" {
				return res
			}
			var p struct {
				Output struct {
					Intent      string `json:"intent"`
					Resolution  string `json:"resolution"`
					Disposition string `json:"disposition"`
					Analysis    string `json:"analysis"`
				} `json:"output"`
			}
			_ = json.Unmarshal(env.Payload, &p)
			hint := p.Output.Intent
			if hint == "" {
				hint = p.Output.Analysis
			}
			res.Events = []PresentationEvent{{
				Type:    "thinking",
				Content: shortenIntent(hint, 48),
			}}
		case "retrieve":
			if env.Status != "end" {
				return res
			}
			res.Events = []PresentationEvent{{
				Type:     "phase",
				Skeleton: "retrieving",
				Content:  "正在回忆相关背景…",
			}}
		case "plan", "output":
			if env.Status != "end" {
				return res
			}
			res.Events = []PresentationEvent{{
				Type:     "phase",
				Skeleton: "planning",
				Content:  "正在整理回答思路…",
			}}
		case "tool_invoke":
			res = t.translateToolInvoke(env)
		}
	}
	return res
}

func (t *Translator) renderToolTemplate(
	tmpl, fallback string,
	spec shared.ToolRegistrySpec,
	args map[string]any,
	extra map[string]string,
) string {
	return RenderTemplateOrFallback(tmpl, fallback, templateVars(spec, args, extra))
}

func (t *Translator) translateToolInvoke(env FactEnvelope) TranslateResult {
	var res TranslateResult
	var p struct {
		ToolCallID string `json:"tool_call_id"`
		Input      struct {
			Tool        string         `json:"tool"`
			ArgsSummary map[string]any `json:"args_summary"`
		} `json:"input"`
		Output *struct {
			Ok       bool   `json:"ok"`
			RowCount *int   `json:"row_count"`
			Preview  string `json:"preview"`
		} `json:"output"`
	}
	if json.Unmarshal(env.Payload, &p) != nil {
		return res
	}

	spec, ok := t.toolSpec(p.Input.Tool)
	if !ok {
		spec = shared.ToolRegistrySpec{Label: p.Input.Tool}
	}

	meta := map[string]any{"tool_call_id": p.ToolCallID, "tool": p.Input.Tool}

	if env.Status == "start" {
		fallback := "正在调用" + spec.Label + "…"
		content := t.renderToolTemplate(spec.StartTemplate, fallback, spec, p.Input.ArgsSummary, nil)
		res.Events = []PresentationEvent{{Type: "tool_start", Content: content, Meta: meta}}
		return res
	}

	if env.Status != "end" || p.Output == nil {
		return res
	}

	extra := map[string]string{}
	if p.Output.RowCount != nil {
		extra["row_count"] = strconv.Itoa(*p.Output.RowCount)
	}

	var content string
	if p.Output.Ok {
		content = t.renderToolTemplate(
			spec.EndOkTemplate,
			spec.Label+"：已获取",
			spec,
			p.Input.ArgsSummary,
			extra,
		)
	} else {
		content = t.renderToolTemplate(
			spec.EndFailTemplate,
			spec.Label+"：未能获取到相关信息",
			spec,
			p.Input.ArgsSummary,
			extra,
		)
		res.Events = append(res.Events, PresentationEvent{
			Type:    "thinking",
			Content: t.partialHint(),
		})
	}

	meta["ok"] = p.Output.Ok
	if !p.Output.Ok {
		meta["partial"] = true
	}
	res.Events = append(res.Events, PresentationEvent{
		Type:    "tool_end",
		Content: content,
		Meta:    meta,
	})
	return res
}
