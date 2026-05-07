package models

import "strings"

// DefaultArticleCoverURL 未填写或非法外链时的默认封面（网络图，非本地上传）。
const DefaultArticleCoverURL = "https://pic4.zhimg.com/v2-cad31f1efa6d4940651ebec9063fd5cb_r.jpg"

// NormalizeArticleCoverURL 校验网络图片链接；空或非 http(s) 则返回默认图。
func NormalizeArticleCoverURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return DefaultArticleCoverURL
	}
	low := strings.ToLower(u)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return DefaultArticleCoverURL
	}
	return u
}

// EffectiveArticleCoverURL 读取展示用封面（库内为空时回落默认图）。
func EffectiveArticleCoverURL(a *Article) string {
	if a == nil {
		return DefaultArticleCoverURL
	}
	return NormalizeArticleCoverURL(a.CoverURL)
}
