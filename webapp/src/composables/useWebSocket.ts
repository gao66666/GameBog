import { onMounted, onUnmounted, ref } from 'vue'
import { useAuthStore } from '@/stores/auth'

export type WsStatus = '未连接' | '连接中...' | '已连接' | '已断开' | '连接错误'

export type UseWebSocketOptions = {
  connectOnMount?: boolean
  onMessage?: (data: string) => void
}

let sharedWs: WebSocket | null = null
let sharedToken = ''
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

export function ensureWSConnected() {
  const token = localStorage.getItem('gb_token') || ''
  if (!token) return

  if (
    sharedWs &&
    (sharedWs.readyState === WebSocket.OPEN || sharedWs.readyState === WebSocket.CONNECTING) &&
    sharedToken === token
  ) {
    return
  }

  if (sharedWs && sharedToken && sharedToken !== token) {
    try {
      sharedWs.close()
    } catch {
      /* ignore */
    }
    sharedWs = null
  }

  sharedToken = token
  notifyStatus('连接中...')

  const ws = new WebSocket(wsURL(token))
  sharedWs = ws

  ws.onopen = () => notifyStatus('已连接')
  ws.onclose = () => notifyStatus('已断开')
  ws.onerror = () => notifyStatus('连接错误')
  ws.onmessage = (ev) => notifyMessage(String(ev.data ?? ''))
}

export function useWebSocket(options?: UseWebSocketOptions) {
  const auth = useAuthStore()
  const status = ref<WsStatus>('未连接')

  const onStatus = (s: WsStatus) => {
    status.value = s
  }
  const onMessage = (data: string) => {
    options?.onMessage?.(data)
  }

  onMounted(() => {
    statusListeners.add(onStatus)
    messageListeners.add(onMessage)
    if (options?.connectOnMount !== false && auth.isLoggedIn) {
      ensureWSConnected()
    }
  })

  onUnmounted(() => {
    statusListeners.delete(onStatus)
    messageListeners.delete(onMessage)
  })

  return { status, ensureWSConnected }
}
