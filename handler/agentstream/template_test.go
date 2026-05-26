package agentstream

import "testing"

func TestRenderTemplateOrFallback(t *testing.T) {
	got := RenderTemplateOrFallback(
		"{{label}}：找到 {{row_count}} 篇",
		"文章搜索：已获取",
		map[string]string{"label": "文章搜索"},
	)
	if got != "文章搜索：已获取" {
		t.Fatalf("got %q", got)
	}
	got2 := RenderTemplateOrFallback(
		"正在{{label}}：「{{query}}」",
		"正在搜索…",
		map[string]string{"label": "文章搜索"},
	)
	if got2 != "正在搜索…" {
		t.Fatalf("got %q", got2)
	}
}
