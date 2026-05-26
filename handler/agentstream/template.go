package agentstream

import (
	"fmt"
	"strconv"
	"strings"
)

// RenderTemplate 将 {{key}} 替换为 vars 中的值（简单展示模板）。
func RenderTemplate(tmpl string, vars map[string]string) string {
	if tmpl == "" {
		return ""
	}
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

// HasUnreplacedTemplate 是否仍含未替换的 {{var}}。
func HasUnreplacedTemplate(s string) bool {
	return strings.Contains(s, "{{")
}

// RenderTemplateOrFallback 渲染后若仍有占位符或为空则返回 fallback。
func RenderTemplateOrFallback(tmpl, fallback string, vars map[string]string) string {
	if strings.TrimSpace(tmpl) == "" {
		return fallback
	}
	content := RenderTemplate(tmpl, vars)
	if HasUnreplacedTemplate(content) || strings.TrimSpace(content) == "" {
		return fallback
	}
	return content
}

func stringFromAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprint(x)
	}
}
