import { marked } from 'marked'
import hljs from 'highlight.js'

let inited = false

function escapeHtml(str: string) {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
}

function initMarked() {
  if (inited) return
  const renderer = new marked.Renderer()
  renderer.html = (html: string | { text?: string }) => {
    const text = typeof html === 'object' ? html.text || '' : html
    return escapeHtml(text)
  }
  renderer.code = (code: string | { text?: string; lang?: string }, infostring?: string) => {
    let codeText = ''
    let lang = ''
    if (code && typeof code === 'object') {
      codeText = String(code.text || '')
      lang = String(code.lang || '')
    } else {
      codeText = String(code || '')
      lang = String((infostring || '').trim().split(/\s+/)[0] || '')
    }
    const cls = lang ? `language-${lang}` : ''
    return `<pre><code${cls ? ` class="${cls}"` : ''}>${escapeHtml(codeText)}</code></pre>`
  }
  marked.setOptions({
    gfm: true,
    breaks: true,
    renderer,
  })
  hljs.configure({ ignoreUnescapedHTML: true })
  inited = true
}

export function renderMarkdown(text: string): string {
  initMarked()
  const raw = String(text || '').trim()
  if (!raw) {
    return '<div class="muted">在左侧输入内容，右侧将实时预览 Markdown 效果</div>'
  }
  try {
    return marked.parse(raw) as string
  } catch {
    return `<p>${escapeHtml(raw)}</p>`
  }
}

export function highlightPreview(root: HTMLElement | null) {
  if (!root) return
  root.querySelectorAll('pre code').forEach((el) => {
    try {
      hljs.highlightElement(el as HTMLElement)
    } catch {
      /* ignore */
    }
  })
}
