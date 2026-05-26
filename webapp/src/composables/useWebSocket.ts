import { onMounted, onUnmounted, ref } from 'vue'
import { useAuthStore } from '@/stores/auth'

export type WsStatus = '未连接' | '连接中...' | '已连接' | '已断开' | '连接错误'

export type UseWebSocketOptions = {
  connectOnMount?: boolean
  onMessage?: (data: string) => void
}

let sharedWs: WebSocket | null = null
let sharedToken = ''
let connectGen = 0
let reconnectTimer: ReturnType<typeof setTimeout> | null = null

const statusListeners = new Set<(s: WsStatus) => void>()
const messageListeners = new Set<(data: string) => void>()

function wsURL(token: string) {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const sync = location.pathname === '/me' ? '&sync=1' : ''
  return `${proto}//${location.host}/api/v1/ws?token=${encodeURIComponent(token)}${sync}`
}

function notifyStatus(s: WsStatus) {
  statusListeners.forEach((fn) => fn(s))
}

function notifyMessage(data: string) {
  messageListeners.forEach((fn) => fn(data))
}

/** 与浏览器 readyState 对齐，避免晚订阅的页面一直显示「未连接」。 */
export function deriveWsStatus(): WsStatus {
  if (!sharedWs) return '未连接'
  switch (sharedWs.readyState) {
    case WebSocket.CONNECTING:
      return '连接中...'
    case WebSocket.OPEN:
      return '已连接'
    case WebSocket.CLOSING:
    case WebSocket.CLOSED:
      return '已断开'
    default:
      return '未连接'
  }
}

function clearReconnectTimer() {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
}

function scheduleReconnect(gen: number) {
  clearReconnectTimer()
  if (!localStorage.getItem('gb_token')) return
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    if (gen !== connectGen) return
    if (sharedWs?.readyState === WebSocket.OPEN) return
    ensureWSConnected()
  }, 3000)
}

export function ensureWSConnected() {
  const token = localStorage.getItem('gb_token') || ''
  if (!token) return

  if (
    sharedWs &&
    (sharedWs.readyState === WebSocket.OPEN || sharedWs.readyState === WebSocket.CONNECTING) &&
    sharedToken === token
  ) {
    notifyStatus(deriveWsStatus())
    return
  }

  if (sharedWs) {
    connectGen++
    try {
      sharedWs.close()
    } catch {
      /* ignore */
    }
    sharedWs = null
  }

  clearReconnectTimer()
  sharedToken = token
  const gen = ++connectGen
  notifyStatus('连接中...')

  const ws = new WebSocket(wsURL(token))
  sharedWs = ws

  ws.onopen = () => {
    if (sharedWs !== ws || gen !== connectGen) return
    notifyStatus('已连接')
  }
  ws.onclose = () => {
    if (sharedWs === ws) sharedWs = null
    if (gen !== connectGen) return
    notifyStatus('已断开')
    scheduleReconnect(gen)
  }
  ws.onerror = () => {
    if (gen !== connectGen) return
    notifyStatus('连接错误')
  }
  ws.onmessage = (ev) => notifyMessage(String(ev.data ?? ''))
}

export function useWebSocket(options?: UseWebSocketOptions) {
  const auth = useAuthStore()
  const status = ref<WsStatus>(deriveWsStatus())

  const onStatus = (s: WsStatus) => {
    status.value = s
  }
  const onMessage = (data: string) => {
    options?.onMessage?.(data)
  }

  onMounted(() => {
    statusListeners.add(onStatus)
    messageListeners.add(onMessage)
    status.value = deriveWsStatus()
    if (options?.connectOnMount !== false && auth.isLoggedIn) {
      ensureWSConnected()
      status.value = deriveWsStatus()
    }
  })

  onUnmounted(() => {
    statusListeners.delete(onStatus)
    messageListeners.delete(onMessage)
  })

  return { status, ensureWSConnected }
}
