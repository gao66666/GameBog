<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import { http } from '@/api/http'
import '@/assets/agent.css'

type SessionMeta = { session_id: string; title: string; updated_at: string }
type ChatMsg = { role: string; content: string }

const auth = useAuthStore()
const hints = [
  '最近有什么热门文章？',
  '有哪些话题可以看？',
  '有什么好玩的游戏？',
  '帮我搜一下关于Go的文章',
]

const sessions = ref<SessionMeta[]>([])
const currentSessionId = ref('')
const messages = ref<ChatMsg[]>([])
const showWelcome = ref(true)
const input = ref('')
const isSending = ref(false)
const statusText = ref('就绪')
const chatStageRef = ref<HTMLElement | null>(null)
const streamingContent = ref('')
let abortController: AbortController | null = null

const canSend = computed(() => !isSending.value && !!input.value.trim())

function formatSessionTime(iso: string) {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  const now = new Date()
  if (d.toDateString() === now.toDateString()) {
    return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  }
  return `${d.getMonth() + 1}/${d.getDate()}`
}

async function refreshSessions() {
  if (!auth.token) {
    sessions.value = []
    currentSessionId.value = ''
    return
  }
  try {
    const res = await http.get('/api/v1/agent/sessions')
    const body = res.data
    if (body?.code !== 0 && body?.code !== undefined) return
    sessions.value = body.data?.sessions || []
    if (body.data?.current_session_id) {
      currentSessionId.value = body.data.current_session_id
    }
  } catch {
    /* ignore */
  }
}

async function loadHistory() {
  if (!auth.token) return
  try {
    let url = '/api/v1/agent/history?limit=48'
    if (currentSessionId.value) {
      url += '&session_id=' + encodeURIComponent(currentSessionId.value)
    }
    const res = await http.get(url)
    const body = res.data
    if (body?.code !== 0 && body?.code !== undefined) return
    if (body.data?.session_id) currentSessionId.value = body.data.session_id
    const list = (body.data?.messages || []) as ChatMsg[]
    messages.value = list.filter((m) => String(m.content || '').trim())
    showWelcome.value = messages.value.length === 0
    await scrollBottom()
  } catch {
    /* ignore */
  }
}

async function selectSession(sid: string) {
  if (!sid || sid === currentSessionId.value) return
  currentSessionId.value = sid
  messages.value = []
  streamingContent.value = ''
  showWelcome.value = true
  await loadHistory()
}

async function deleteSession(sid: string) {
  if (!sid || !auth.token) return
  try {
    await ElMessageBox.confirm('确定删除该会话？', '提示', { type: 'warning' })
  } catch {
    return
  }
  try {
    const res = await http.delete('/api/v1/agent/sessions/' + encodeURIComponent(sid))
    const body = res.data
    if (!res.data || (body.code !== 0 && body.code !== undefined)) {
      throw new Error(body.message || '删除失败')
    }
    if (body.data?.current_session_id !== undefined) {
      currentSessionId.value = body.data.current_session_id || ''
    }
    await refreshSessions()
    messages.value = []
    showWelcome.value = true
    await loadHistory()
  } catch (e) {
    ElMessage.error('删除失败：' + (e instanceof Error ? e.message : String(e)))
  }
}

async function newSession() {
  if (!auth.token) {
    ElMessage.warning('请先登录')
    return
  }
  try {
    const res = await http.post('/api/v1/agent/sessions')
    const body = res.data
    if (body?.code !== 0 && body?.code !== undefined) throw new Error(body.message || '创建失败')
    const sid = body.data?.session_id || ''
    if (sid) currentSessionId.value = sid
    messages.value = []
    streamingContent.value = ''
    showWelcome.value = true
    await refreshSessions()
  } catch (e) {
    ElMessage.error('新建会话失败：' + (e instanceof Error ? e.message : String(e)))
  }
}

function autoResize(el: HTMLTextAreaElement) {
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, 160) + 'px'
}

async function scrollBottom() {
  await nextTick()
  const el = chatStageRef.value
  if (el) el.scrollTop = el.scrollHeight
}

function handleSSEEvent(event: { type: string; content?: string; tool?: string }) {
  switch (event.type) {
    case 'token':
      streamingContent.value += event.content || ''
      scrollBottom()
      break
    case 'done':
      if (streamingContent.value.trim()) {
        messages.value.push({ role: 'assistant', content: streamingContent.value })
      }
      streamingContent.value = ''
      showWelcome.value = messages.value.length === 0
      break
    case 'error':
      messages.value.push({
        role: 'assistant',
        content: '\n\n[错误: ' + (event.content || '未知错误') + ']',
      })
      streamingContent.value = ''
      showWelcome.value = false
      break
  }
}

async function sendMessage(text: string) {
  const msg = String(text || '').trim()
  if (!msg || isSending.value) return

  if (!auth.token) {
    ElMessage.warning('请先登录后再使用 AI 助手')
    return
  }

  input.value = ''
  isSending.value = true
  statusText.value = '思考中...'
  messages.value.push({ role: 'user', content: msg })
  showWelcome.value = false
  streamingContent.value = ''
  await scrollBottom()

  abortController = new AbortController()

  try {
    const payload: Record<string, string> = { message: msg, token: auth.token }
    if (currentSessionId.value) payload.chat_session_id = currentSessionId.value

    const resp = await fetch('/api/v1/agent/chat', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer ' + auth.token,
      },
      body: JSON.stringify(payload),
      signal: abortController.signal,
    })

    if (!resp.ok) {
      const errText = await resp.text().catch(() => '')
      throw new Error(errText || 'HTTP ' + resp.status)
    }

    const reader = resp.body!.getReader()
    const decoder = new TextDecoder()
    let buffer = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      for (const line of lines) {
        const trimmed = line.trim()
        if (!trimmed.startsWith('data: ')) continue
        try {
          handleSSEEvent(JSON.parse(trimmed.slice(6)))
        } catch {
          /* ignore */
        }
      }
    }

    if (buffer.trim().startsWith('data: ')) {
      try {
        handleSSEEvent(JSON.parse(buffer.trim().slice(6)))
      } catch {
        /* ignore */
      }
    }
  } catch (err) {
    if (err instanceof Error && err.name === 'AbortError') return
    messages.value.push({
      role: 'assistant',
      content: '\n\n[错误: ' + (err instanceof Error ? err.message : String(err)) + ']',
    })
    streamingContent.value = ''
  } finally {
    isSending.value = false
    statusText.value = '就绪'
    abortController = null
    await refreshSessions()
    await scrollBottom()
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    if (canSend.value) sendMessage(input.value)
  }
}

onMounted(async () => {
  await refreshSessions()
  await loadHistory()
})
</script>

<template>
  <div class="page-agent">
    <div class="agent-app">
      <aside class="agent-sidebar">
        <div class="agent-sidebar-brand">
          <div class="agent-sidebar-logo-icon">✦</div>
          <div class="agent-sidebar-brand-text">
            <strong>小博</strong>
            <span>GameBog 智能助手</span>
          </div>
        </div>

        <button type="button" class="agent-btn-new-chat" @click="newSession">+ 新对话</button>

        <div class="agent-session-section">
          <div class="agent-session-list-title">历史会话</div>
          <div class="agent-session-list">
            <div v-if="!auth.isLoggedIn" class="agent-session-empty">登录后查看历史会话</div>
            <div v-else-if="!sessions.length" class="agent-session-empty">暂无会话，点「新对话」开始</div>
            <div
              v-for="s in sessions"
              :key="s.session_id"
              class="agent-session-row"
              :class="{ active: s.session_id === currentSessionId }"
              @click="selectSession(s.session_id)"
            >
              <div class="agent-session-row-main" style="flex: 1; min-width: 0">
                <div class="agent-session-row-title">{{ s.title || '新对话' }}</div>
                <div class="agent-session-row-time">{{ formatSessionTime(s.updated_at) }}</div>
              </div>
              <button
                type="button"
                class="agent-session-row-del"
                title="删除"
                @click.stop="deleteSession(s.session_id)"
              >
                ×
              </button>
            </div>
          </div>
        </div>

        <nav class="agent-sidebar-nav">
          <RouterLink to="/">首页</RouterLink>
          <RouterLink to="/topics">话题</RouterLink>
          <RouterLink to="/game-library">游戏库</RouterLink>
          <RouterLink to="/agent" class="nav-active">AI 助手</RouterLink>
          <RouterLink to="/me">个人中心</RouterLink>
          <RouterLink to="/login">登录</RouterLink>
        </nav>
        <p class="agent-sidebar-hint">删除会话仅移除该对话短期记录；长期记忆仍会保留。</p>
      </aside>

      <section class="agent-workspace">
        <div ref="chatStageRef" class="agent-chat-stage">
          <div v-if="showWelcome && !streamingContent" class="chat-welcome">
            <div class="chat-welcome-inner">
              <h1 class="chat-welcome-greeting">今天想聊些什么？</h1>
              <p class="chat-welcome-sub">我是小博，可以帮你搜文章、逛话题、找游戏。</p>
              <div class="hints">
                <div
                  v-for="h in hints"
                  :key="h"
                  class="hint-chip"
                  @click="sendMessage(h)"
                >
                  {{ h }}
                </div>
              </div>
            </div>
          </div>
          <div v-else class="chat-messages-inner">
            <div
              v-for="(m, i) in messages"
              :key="i"
              class="chat-msg"
              :class="m.role === 'assistant' ? 'assistant' : 'user'"
            >
              <div class="avatar">{{ m.role === 'assistant' ? '博' : (auth.userName?.charAt(0) || '我') }}</div>
              <div class="bubble">{{ m.content }}</div>
            </div>
            <div v-if="streamingContent" class="chat-msg assistant">
              <div class="avatar">博</div>
              <div class="bubble">{{ streamingContent }}</div>
            </div>
          </div>
        </div>

        <div class="agent-composer-wrap">
          <div class="agent-composer-inner">
            <div class="chat-token-status">
              <span class="dot" />
              <span>{{ statusText }}</span>
            </div>
            <div class="chat-input-wrap">
              <textarea
                v-model="input"
                rows="1"
                placeholder="问问小博…（Enter 发送，Shift+Enter 换行）"
                @input="autoResize($event.target as HTMLTextAreaElement)"
                @keydown="onKeydown"
              />
              <button type="button" class="chat-send-btn" :disabled="!canSend" @click="sendMessage(input)">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <line x1="22" y1="2" x2="11" y2="13" />
                  <polygon points="22 2 15 22 11 13 2 9 22 2" />
                </svg>
              </button>
            </div>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>
