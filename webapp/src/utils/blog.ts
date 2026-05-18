export const DEFAULT_ARTICLE_COVER =
  'https://pic4.zhimg.com/v2-cad31f1efa6d4940651ebec9063fd5cb_r.jpg'

export const DEFAULT_GAME_COVER =
  'https://pic4.zhimg.com/v2-cad31f1efa6d4940651ebec9063fd5cb_r.jpg'

export function pick<T>(obj: Record<string, unknown> | null | undefined, keys: string[], fallback?: T): T {
  if (!obj) return fallback as T
  for (const k of keys) {
    if (obj[k] !== undefined && obj[k] !== null) return obj[k] as T
  }
  return fallback as T
}

export function articleCoverUrl(a: Record<string, unknown> | null | undefined) {
  const u = String(pick(a, ['coverUrl', 'cover_url'], '') || '').trim()
  return /^https?:\/\//i.test(u) ? u : DEFAULT_ARTICLE_COVER
}

export function gameCoverUrl(g: Record<string, unknown> | null | undefined) {
  const u = String(pick(g, ['coverUrl', 'cover_url'], '') || '').trim()
  return /^https?:\/\//i.test(u) ? u : DEFAULT_GAME_COVER
}

export function fmtTime(iso: unknown) {
  if (!iso) return ''
  try {
    const d = new Date(String(iso))
    if (isNaN(d.getTime())) return ''
    return d.toLocaleString()
  } catch {
    return ''
  }
}

export function renderStatsHtml(viewCount: unknown, likeCount: unknown, commentCount?: unknown) {
  const parts: string[] = []
  const v = Number(viewCount) || 0
  const l = Number(likeCount) || 0
  const c = Number(commentCount) || 0
  if (v > 0) parts.push(`<span class="stat"><span class="stat-icon" aria-hidden="true">👁</span>${v}</span>`)
  if (l > 0) parts.push(`<span class="stat"><span class="stat-icon" aria-hidden="true">👍</span>${l}</span>`)
  if (c > 0) parts.push(`<span class="stat"><span class="stat-icon" aria-hidden="true">💬</span>${c}</span>`)
  return parts.join(' · ')
}

export function normalizeGithubURL(raw: string) {
  let candidate = String(raw || '').trim()
  if (!candidate) return ''
  if (/^@/.test(candidate)) candidate = 'https://github.com/' + candidate.slice(1)
  else if (!/^https?:\/\//i.test(candidate)) {
    if (/^(www\.)?github\.com\//i.test(candidate)) candidate = 'https://' + candidate
    else if (/^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,38})$/.test(candidate)) candidate = 'https://github.com/' + candidate
    else candidate = 'https://' + candidate
  }
  try {
    return new URL(candidate).href
  } catch {
    return ''
  }
}

export function gamePriceLabel(g: Record<string, unknown>) {
  const pc = Number(g.priceCents ?? g.price_cents ?? -1)
  if (!Number.isFinite(pc) || pc < 0) return '价格待定'
  if (pc === 0) return '免费'
  const yuan = pc / 100
  if (Math.abs(yuan - Math.round(yuan)) < 1e-6) return '¥' + Math.round(yuan)
  return '¥' + yuan.toFixed(2)
}

export function gameTags(g: Record<string, unknown>, max = 4): string[] {
  const out: string[] = []
  const raw = g.tags
  if (Array.isArray(raw)) {
    raw.forEach((t) => {
      const s = String(t || '').trim()
      if (s) out.push(s)
    })
  } else if (typeof raw === 'string') {
    try {
      const j = JSON.parse(raw)
      if (Array.isArray(j)) j.forEach((t) => { const s = String(t || '').trim(); if (s) out.push(s) })
    } catch { /* ignore */ }
  }
  if (out.length) return out.slice(0, max)
  return [g.publisher, g.developer].filter(Boolean).map(String).slice(0, max)
}
