<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/layouts/AppLayout.vue'
import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { ensureWSConnected, useWebSocket } from '@/composables/useWebSocket'
import { ElMessageBox } from 'element-plus'
import { Delete } from '@element-plus/icons-vue'

type Peer = { user_id?: number | string; userId?: number | string; user_name?: string; userName?: string }
type DmMsg = { fromUserId?: number; from_user_id?: number; content?: string; sentAt?: string; sent_at?: string }

const route = useRoute()
const auth = useAuthStore()

const peers = ref<Peer[]>([])
const peersEmpty = ref('加载中...')
const activePeerId = ref('')
const activePeerName = ref('')
const messages = ref<DmMsg[]>([])
const messagesEmpty = ref('请选择左侧会话')
const input = ref('')
const dmMsg = ref('')
const dmStatus = ref('')

function peerUid(p: Peer) {
  return String(p.user_id ?? p.userId ?? '')
}

function isMe(m: DmMsg) {
  return String(m.fromUserId ?? m.from_user_id ?? '') === String(auth.userId)
}

async function loadPeers() {
  peersEmpty.value = '加载中...'
  const resp = await api<{ peers?: Peer[] }>('/api/v1/dm/peers')
  peers.value = resp.data?.peers || []
  peersEmpty.value = peers.value.length ? '' : '暂无私信对象'
}

async function loadMessages() {
  if (!activePeerId.value) return
  try {
    const resp = await api<{ messages?: DmMsg[] }>(
      '/api/v1/dm/messages?peer_id=' + encodeURIComponent(activePeerId.value),
    )
    messages.value = resp.data?.messages || []
    messagesEmpty.value = messages.value.length ? '' : '暂无消息'
  } catch (e) {
    messagesEmpty.value = e instanceof Error ? e.message : String(e)
  }
}

function selectPeer(uid: string, name: string) {
  activePeerId.value = uid
  activePeerName.value = name
  dmMsg.value = ''
  dmStatus.value = ''
  loadMessages()
}

async function deleteConversation(uid: string) {
  try {
    await ElMessageBox.confirm('确定删除与该用户的全部消息吗？', '提示', { type: 'warning' })
  } catch {
    return
  }
  try {
    await api('/api/v1/dm/conversation/delete', { method: 'POST', data: { peerId: uid } })
    if (activePeerId.value === uid) {
      activePeerId.value = ''
      activePeerName.value = ''
      messages.value = []
      messagesEmpty.value = '请选择左侧会话'
    }
    await loadPeers()
  } catch (e) {
    dmMsg.value = '删除失败：' + (e instanceof Error ? e.message : String(e))
  }
}

async function sendMessage() {
  if (!activePeerId.value) return
  const content = input.value.trim()
  if (!content) {
    dmMsg.value = '内容不能为空'
    return
  }
  dmMsg.value = '发送中...'
  try {
    await api('/api/v1/dm/messages', {
      method: 'POST',
      data: { toUserId: activePeerId.value, content },
    })
    input.value = ''
    dmMsg.value = '已发送'
    await loadPeers()
    await loadMessages()
  } catch (e) {
    dmMsg.value = '发送失败：' + (e instanceof Error ? e.message : String(e))
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    sendMessage()
  }
}

useWebSocket({
  onMessage(data) {
    let obj: { type?: string; count?: number; toUserId?: number; fromUserId?: number }
    try {
      obj = JSON.parse(data)
    } catch {
      return
    }
    if (obj.type === 'dm_unread' && (obj.count ?? 0) > 0) {
      loadPeers().then(() => activePeerId.value && loadMessages())
      api('/api/v1/dm/unread_clear', { method: 'POST' }).catch(() => {})
      localStorage.removeItem('gb_dm_hint')
      return
    }
    if (obj.type === 'dm') {
      if (String(obj.toUserId) !== auth.userId) return
      loadPeers().catch(() => {})
      if (activePeerId.value && activePeerId.value === String(obj.fromUserId)) {
        loadMessages().catch(() => {})
      }
    }
  },
})

async function initPeerFromQuery() {
  const init = String(route.query.peer_id || '')
  if (!init) {
    if (peers.value.length) {
      const p = peers.value[0]
      selectPeer(peerUid(p), String(p.user_name || p.userName || peerUid(p)))
    }
    return
  }
  if (!peers.value.some((p) => peerUid(p) === init)) {
    peers.value = [{ user_id: init, user_name: '用户 ' + init }, ...peers.value]
    try {
      const u = await api<Record<string, unknown>>('/api/v1/users/' + encodeURIComponent(init))
      const nm = String(u.data?.user_name || u.data?.userName || '用户 ' + init)
      peers.value = peers.value.map((p) => (peerUid(p) === init ? { ...p, user_name: nm } : p))
    } catch {
      /* ignore */
    }
  }
  const p = peers.value.find((x) => peerUid(x) === init)
  selectPeer(init, String(p?.user_name || p?.userName || init))
}

onMounted(async () => {
  if (!auth.isLoggedIn) {
    peersEmpty.value = ''
    messagesEmpty.value = '请先登录后再使用私信'
    return
  }
  ensureWSConnected()
  await loadPeers()
  try {
    const r = await api<{ count?: number }>('/api/v1/dm/unread_count')
    if ((r.data?.count ?? 0) > 0) dmStatus.value = '未读 ' + r.data?.count
  } catch {
    /* ignore */
  }
  await initPeerFromQuery()
  api('/api/v1/dm/unread_clear', { method: 'POST' }).catch(() => {})
  localStorage.removeItem('gb_dm_hint')
})

watch(() => route.query.peer_id, () => initPeerFromQuery())
</script>

<template>
  <AppLayout>
    <div class="container page-dm">
      <section class="card dm-layout">
        <aside class="dm-aside">
          <h2 class="dm-aside-title">私信</h2>
          <p v-if="peersEmpty" class="muted">{{ peersEmpty }}</p>
          <div
            v-for="p in peers"
            :key="peerUid(p)"
            class="dm-peer"
            :class="{ active: peerUid(p) === activePeerId }"
            @click="selectPeer(peerUid(p), String(p.user_name || p.userName || peerUid(p)))"
          >
            <div>
              <div class="dm-peer-name">{{ p.user_name || p.userName || peerUid(p) }}</div>
              <div class="dm-peer-uid">UID {{ peerUid(p) }}</div>
            </div>
            <button
              type="button"
              class="dm-peer-del"
              title="删除与该用户的全部消息"
              @click.stop="deleteConversation(peerUid(p))"
            >
              <el-icon :size="16"><Delete /></el-icon>
            </button>
          </div>
        </aside>

        <main class="dm-main">
          <div class="dm-messages">
            <div class="dm-messages-top">
              <h3 class="dm-chat-title">{{ activePeerId ? activePeerName : '选择会话' }}</h3>
              <p v-if="dmStatus" class="dm-chat-status">{{ dmStatus }}</p>
            </div>
            <div class="dm-messages-scroll">
              <p v-if="messagesEmpty" class="dm-messages-empty">{{ messagesEmpty }}</p>
            <div
              v-for="(m, i) in messages"
              :key="i"
              class="dm-row"
              :class="isMe(m) ? 'dm-row--me' : 'dm-row--peer'"
            >
              <div class="dm-bubble" :class="{ me: isMe(m) }">
                <div class="dm-bubble-text">{{ m.content }}</div>
                <div class="dm-bubble-time">{{ m.sentAt || m.sent_at }}</div>
              </div>
            </div>
            </div>
          </div>

          <form class="dm-input-area" @submit.prevent="sendMessage">
            <el-input v-model="input" type="textarea" :rows="2" placeholder="输入消息，Enter 发送" @keydown="onKeydown" />
            <div class="dm-input-actions">
              <el-button type="primary" native-type="submit">发送</el-button>
              <span class="dm-input-hint">{{ dmMsg }}</span>
            </div>
          </form>
        </main>
      </section>
    </div>
  </AppLayout>
</template>

<style scoped>
.page-dm {
  padding-bottom: 32px;
}

.dm-layout {
  display: flex;
  min-height: 520px;
  padding: 0;
  overflow: hidden;
}

.dm-aside {
  width: 240px;
  flex-shrink: 0;
  border-right: 1px solid #e5e5e5;
  padding: 16px;
  background: #fafafa;
}

.dm-aside-title {
  font-size: 16px;
  margin: 0 0 12px;
  font-weight: 600;
}

.dm-peer {
  padding: 10px;
  border-radius: 10px;
  cursor: pointer;
  margin-bottom: 6px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}

.dm-peer.active {
  background: #e8f4ff;
}

.dm-peer-name {
  font-size: 14px;
  font-weight: 500;
  color: #1d1d1f;
}

.dm-peer-uid {
  font-size: 11px;
  color: #86868b;
  margin-top: 2px;
}

.dm-peer-del {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  padding: 0;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: #a1a1a6;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.dm-peer-del:hover {
  background: #ffecec;
  color: #e03e3e;
}

.dm-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: #fff;
}

.dm-messages {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
  background: #fff;
}

.dm-messages-top {
  flex-shrink: 0;
  text-align: center;
  padding: 14px 16px 10px;
  background: #fff;
  border-bottom: 1px solid rgba(120, 160, 200, 0.2);
}

.dm-chat-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: #3d5a73;
}

.dm-chat-status {
  margin: 4px 0 0;
  font-size: 12px;
  color: #86868b;
}

.dm-messages-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  min-height: 200px;
}

.dm-messages-empty {
  text-align: center;
  color: #86868b;
  font-size: 14px;
  padding: 40px 0;
}

.dm-row {
  display: flex;
  margin-bottom: 14px;
}

.dm-row--me {
  justify-content: flex-start;
}

.dm-row--peer {
  justify-content: flex-end;
}

.dm-bubble {
  max-width: 70%;
  padding: 10px 12px;
  border-radius: 8px;
  background: #fff;
  color: #4a5f73;
  border: 1px solid rgba(120, 160, 200, 0.22);
  box-shadow: 0 1px 3px rgba(100, 140, 180, 0.12);
}

.dm-bubble.me {
  background: #d4e8ff;
  color: #2f4a63;
  border-color: rgba(100, 150, 210, 0.35);
}

.dm-bubble-text {
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 15px;
  line-height: 1.45;
}

.dm-bubble-time {
  font-size: 11px;
  color: #7a92a8;
  margin-top: 6px;
}

.dm-input-area {
  flex-shrink: 0;
  padding: 12px 16px;
  background: #f5f9fd;
  border-top: 1px solid rgba(120, 160, 200, 0.25);
}

.dm-input-actions {
  margin-top: 8px;
  display: flex;
  gap: 8px;
  align-items: center;
}

.dm-input-hint {
  font-size: 12px;
  color: #86868b;
}
</style>
