/** 将 API / 编辑器的 tags 规范为独立关键词列表 */
export function normalizeArticleTags(raw: unknown): string[] {
  if (raw == null) return []

  if (typeof raw === 'string') {
    const s = raw.trim()
    if (!s) return []
    if (s.startsWith('[')) {
      try {
        return normalizeArticleTags(JSON.parse(s))
      } catch {
        /* fall through */
      }
    }
    return splitTagTokens(s)
  }

  if (!Array.isArray(raw)) return []

  const out: string[] = []
  for (const item of raw) {
    if (typeof item === 'string') {
      out.push(...splitTagTokens(item))
      continue
    }
    if (item && typeof item === 'object') {
      const o = item as Record<string, unknown>
      const name = String(o.name ?? o.Name ?? o.tag ?? o.tag_name ?? '').trim()
      if (name) out.push(...splitTagTokens(name))
    }
  }

  const seen = new Set<string>()
  return out.filter((t) => {
    const key = t.toLowerCase()
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function splitTagTokens(s: string): string[] {
  return s
    .split(/[\s,，、;；|/#]+/)
    .map((t) => t.replace(/^#+/, '').trim())
    .filter((t) => t.length > 0 && t.length <= 50)
}
