<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Delete } from '@element-plus/icons-vue'
import AppLayout from '@/layouts/AppLayout.vue'
import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { ensureWSConnected, useWebSocket } from '@/composables/useWebSocket'
import '@/assets/me.css'

const router = useRouter()
const auth = useAuthStore()
const DM_HINT_KEY = 'gb_dm_hint'
const DEFAULT_COVER =
  'https://pic4.zhimg.com/v2-cad31f1efa6d4940651ebec9063fd5cb_r.jpg'

const activeTab = ref('articles')
const showSettings = ref(false)
const settingsMsg = ref('')
const dmDot = ref(false)
const checkedInToday = ref(false)
const checkinLoading = ref(false)

const profile = reactive({
  email: '',
  github: '',
  balance: '—',
})
const stats = reactive({
  articles: '0',
  follows: '0',
  fans: '0',
})

const settingsForm = reactive({
  name: '',
  email: '',
  password: '',
  github: '',
})

const articles = ref<Array<Record<string, unknown>>>([])
const articlesEmpty = ref('加载中...')
const artPage = ref(1)
const artTotal = ref(0)
const artSize = 8

const collections = ref<Array<Record<string, unknown>>>([])
const colEmpty = ref('加载中...')

const cyList = ref<Array<Record<string, unknown>>>([])
const cyEmpty = ref('')
const cyLoaded = ref(false)

const games = ref<Array<Record<string, unknown>>>([])
const gameEmpty = ref('加载中...')

const topics = ref<Array<Record<string, unknown>>>([])
const topicEmpty = ref('加载中...')

const notifications = ref<Array<{ content: string; meta: string }>>([])

const { status: wsStatus } = useWebSocket({
  onMessage(data) {
    try {
      const obj = JSON.parse(data)
      if (obj?.type === 'dm' || obj?.type === 'dm_unread') {
        dmDot.value = true
        localStorage.setItem(DM_HINT_KEY, '1')
      }
      if (obj && (obj.event_id || obj.sender_id || obj.sender_name || obj.content)) {
        notifications.value.unshift({
          content: String(obj.content || '有一条新通知'),
          meta: (obj.sender_name ? '来自 ' + obj.sender_name : '') + (obj.type ? ' · ' + obj.type : ''),
        })
      }
    } catch {
      /* ignore */
    }
  },
})

function pick(obj: Record<string, unknown> | null | undefined, keys: string[], fallback: unknown = '') {
  if (!obj) return fallback
  for (const k of keys) {
    if (obj[k] !== undefined && obj[k] !== null) return obj[k]
  }
  return fallback
}

function coverUrl(a: Record<string, unknown>) {
  const u = String(pick(a, ['coverUrl', 'cover_url'], '') || '').trim()
  return /^https?:\/\//i.test(u) ? u : DEFAULT_COVER
}

function firstChar(name: string) {
  const t = String(name || '').trim()
  return t ? t.charAt(0).toUpperCase() : 'G'
}

function normalizeGithub(raw: string) {
  let c = String(raw || '').trim()
  if (!c) return ''
  if (/^@/.test(c)) c = 'https://github.com/' + c.slice(1)
  else if (!/^https?:\/\//i.test(c)) {
    if (/^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,38})$/.test(c)) c = 'https://github.com/' + c
    else c = 'https://' + c
  }
  try {
    return new URL(c).href
  } catch {
    return ''
  }
}

async function loadMe() {
  if (!auth.isLoggedIn) {
    router.push('/login')
    return
  }
  try {
    const resp = await api<Record<string, unknown>>('/api/v1/users/me')
    const d = resp.data || {}
    const uid = String(pick(d, ['user_id', 'userId'], auth.userId))
    const uname = String(pick(d, ['user_name', 'userName'], auth.userName))
    auth.setAuth({ token: auth.token, userId: uid, userName: uname })
    profile.email = String(pick(d, ['email', 'Email'], ''))
    profile.github = String(pick(d, ['github', 'github_url', 'githubUrl'], ''))
    settingsForm.github = profile.github
    const bal = pick(d, ['account_balance', 'accountBalance'], null)
    profile.balance = bal != null && String(bal).trim() !== '' ? String(bal) : '—'
  } catch {
    profile.balance = '—'
  }
}

async function loadArticles() {
  articlesEmpty.value = '加载中...'
  if (!auth.userId) {
    articlesEmpty.value = '未登录'
    return
  }
  try {
    const url = `/api/v1/articles/list?author_id=${encodeURIComponent(auth.userId)}&page=${artPage.value}&size=${artSize}`
    const resp = await api<Record<string, unknown>>(url)
    const data = resp.data || {}
    articles.value = (pick(data, ['article_list', 'articles'], []) as Array<Record<string, unknown>>) || []
    artTotal.value = Number(pick(data, ['total'], 0)) || 0
    stats.articles = String(artTotal.value)
    articlesEmpty.value = articles.value.length ? '' : '暂无文章'
  } catch (e) {
    articlesEmpty.value = '加载失败：' + (e instanceof Error ? e.message : String(e))
  }
}

async function loadCollections() {
  colEmpty.value = '加载中...'
  try {
    const resp = await api<Record<string, unknown>>('/api/v1/articles/collect')
    const list = (pick(resp.data || {}, ['list', 'article_list', 'collections'], []) as Array<Record<string, unknown>>) || []
    collections.value = list
    colEmpty.value = list.length ? '' : '暂无收藏'
  } catch (e) {
    colEmpty.value = '加载失败：' + (e instanceof Error ? e.message : String(e))
  }
}

async function loadCy() {
  cyEmpty.value = '加载中...'
  try {
    const resp = await api<Record<string, unknown>>('/api/v1/comments/cy')
    cyList.value = (pick(resp.data || {}, ['list'], []) as Array<Record<string, unknown>>) || []
    cyEmpty.value = cyList.value.length ? '' : '暂无插眼记录'
  } catch (e) {
    cyEmpty.value = '加载失败'
  }
}

async function loadGames() {
  try {
    const resp = await api<Record<string, unknown>>('/api/v1/games/play')
    games.value = (pick(resp.data || {}, ['list', 'plays'], []) as Array<Record<string, unknown>>) || []
    gameEmpty.value = games.value.length ? '' : '暂无游戏记录'
  } catch {
    gameEmpty.value = '加载失败'
  }
}

async function loadTopics() {
  try {
    const resp = await api<Record<string, unknown>>('/api/v1/follow/topics')
    topics.value = (pick(resp.data || {}, ['list', 'topics'], []) as Array<Record<string, unknown>>) || []
    topicEmpty.value = topics.value.length ? '' : '暂未关注话题'
  } catch {
    topicEmpty.value = '加载失败'
  }
}

function isToday(iso: string) {
  if (!iso) return false
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return false
  const now = new Date()
  return (
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate()
  )
}

async function loadCheckinStatus() {
  try {
    const resp = await api<{ list?: Array<Record<string, unknown>> }>(
      '/api/v1/points/transactions?page=1&size=30',
    )
    const list = resp.data?.list ?? []
    checkedInToday.value = list.some((row) => {
      const ref = String(row.refType ?? row.ref_type ?? '')
      const desc = String(row.description ?? '')
      if (ref !== 'checkin' && !desc.includes('签到')) return false
      return isToday(String(row.createdAt ?? row.created_at ?? ''))
    })
  } catch {
    checkedInToday.value = false
  }
}

async function doCheckin() {
  if (checkedInToday.value || checkinLoading.value) return
  checkinLoading.value = true
  try {
    const resp = await api<{ balance?: number; message?: string }>('/api/v1/points/checkin', {
      method: 'POST',
      data: {},
    })
    checkedInToday.value = true
    ElMessage.success(resp.data?.message || '签到成功')
  } catch (e) {
    const m = e instanceof Error ? e.message : String(e)
    if (m.includes('已经签到')) {
      checkedInToday.value = true
      ElMessage.info(m)
    } else {
      ElMessage.error(m || '签到失败')
    }
  } finally {
    checkinLoading.value = false
  }
}

async function loadCounts() {
  try {
    const f = await api<{ count?: number }>('/api/v1/follow/users/count')
    stats.follows = String(f.data?.count ?? 0)
  } catch {
    stats.follows = '0'
  }
  try {
    const u = await api<Record<string, unknown>>('/api/v1/users/' + auth.userId)
    stats.fans = String(pick(u.data || {}, ['follower_count', 'followerCount'], 0))
  } catch {
    stats.fans = '0'
  }
  await loadCheckinStatus()
}

async function loadDmUnread() {
  dmDot.value = localStorage.getItem(DM_HINT_KEY) === '1'
  try {
    const r = await api<{ count?: number }>('/api/v1/dm/unread_count')
    if ((r.data?.count ?? 0) > 0) dmDot.value = true
  } catch {
    /* ignore */
  }
}

async function deleteArticle(id: string | number) {
  if (!confirm('确定删除这篇文章吗？')) return
  try {
    await api('/api/v1/articles/' + encodeURIComponent(String(id)), { method: 'DELETE' })
    await loadArticles()
    ElMessage.success('已删除')
  } catch (e) {
    ElMessage.error('删除失败：' + (e instanceof Error ? e.message : String(e)))
  }
}

async function saveSettings() {
  const payload: Record<string, string> = {}
  if (settingsForm.name.trim()) payload.name = settingsForm.name.trim()
  if (settingsForm.email.trim()) payload.email = settingsForm.email.trim()
  if (settingsForm.password.trim()) payload.password = settingsForm.password.trim()
  if (settingsForm.github.trim()) {
    const n = normalizeGithub(settingsForm.github)
    if (!n) {
      settingsMsg.value = 'GitHub 链接格式不正确'
      return
    }
    if (n !== profile.github) payload.github = n
  } else if (profile.github) {
    payload.github = ''
  }
  if (!Object.keys(payload).length) {
    settingsMsg.value = '没有需要更新的内容'
    return
  }
  try {
    await api('/api/v1/users/update', { method: 'POST', data: payload })
    settingsMsg.value = '已保存'
    await loadMe()
    showSettings.value = false
    ElMessage.success('已保存')
  } catch (e) {
    settingsMsg.value = '保存失败：' + (e instanceof Error ? e.message : String(e))
  }
}

function onTabChange(name: string | number) {
  if (name === 'cy' && !cyLoaded.value) {
    cyLoaded.value = true
    loadCy()
  }
}

function logout() {
  auth.clearAuth()
  router.push('/login')
}

const artMaxPage = () => Math.max(1, Math.ceil(artTotal.value / artSize))

onMounted(async () => {
  dmDot.value = localStorage.getItem(DM_HINT_KEY) === '1'
  await loadMe()
  ensureWSConnected()
  await Promise.all([loadArticles(), loadCollections(), loadGames(), loadTopics(), loadCounts(), loadDmUnread()])
})
</script>

<template>
  <AppLayout>
    <div class="container page-me">
      <div class="me-wrap">
        <aside class="me-side">
          <div class="me-card">
            <div class="me-avatar">{{ firstChar(auth.userName) }}</div>
            <div class="me-name">{{ auth.userName || '-' }}</div>
            <div class="me-uid">UID: {{ auth.userId || '-' }}</div>
            <div v-if="profile.email" class="me-email">{{ profile.email }}</div>
            <div class="me-stats">
              <div class="me-stat"><span class="me-stat-num">{{ stats.articles }}</span><span class="me-stat-label">文章</span></div>
              <div class="me-stat"><span class="me-stat-num">{{ stats.follows }}</span><span class="me-stat-label">关注</span></div>
              <div class="me-stat"><span class="me-stat-num">{{ stats.fans }}</span><span class="me-stat-label">粉丝</span></div>
              <div class="me-stat"><span class="me-stat-num">{{ profile.balance }}</span><span class="me-stat-label">余额</span></div>
            </div>
            <div class="me-actions">
              <el-badge :is-dot="dmDot" class="me-action-badge">
                <RouterLink to="/dm" class="me-action-btn">私信</RouterLink>
              </el-badge>
              <RouterLink to="/points-mall" class="me-action-btn">积分商城</RouterLink>
              <button
                type="button"
                class="me-action-btn"
                :disabled="checkedInToday || checkinLoading"
                @click="doCheckin"
              >
                {{ checkedInToday ? '已签到' : checkinLoading ? '签到中…' : '签到' }}
              </button>
              <button type="button" class="me-action-btn" @click="showSettings = true">设置</button>
              <button type="button" class="me-action-btn" @click="logout">退出</button>
            </div>
            <div class="ws-line">
              <span class="ws-dot" :class="{ on: wsStatus === '已连接' }" />
              {{ wsStatus }}
            </div>
          </div>
        </aside>

        <main class="me-main me-card">
          <el-tabs v-model="activeTab" @tab-change="onTabChange">
            <el-tab-pane label="文章" name="articles">
              <div class="me-panel-hd">
                <span>我的文章</span>
                <el-button type="primary" size="small" @click="router.push('/editor')">+ 写文章</el-button>
              </div>
              <div v-if="articlesEmpty" class="me-empty">{{ articlesEmpty }}</div>
              <div v-for="a in articles" :key="String(a.id)" class="me-item-row">
                <img class="me-item-thumb" :src="coverUrl(a)" alt="" loading="lazy" />
                <div class="me-item-body">
                  <a class="me-item-title" :href="'/article/' + a.id">{{ a.title || '文章 ' + a.id }}</a>
                  <div v-if="a.summary" class="me-item-desc">{{ String(a.summary).slice(0, 160) }}</div>
                  <div class="me-item-meta">
                    阅读 {{ a.view_count ?? a.viewCount ?? 0 }} · 点赞 {{ a.like_count ?? a.likeCount ?? 0 }}
                  </div>
                </div>
                <div class="me-item-actions">
                  <el-button size="small" @click="router.push({ path: '/editor', query: { id: String(a.id) } })">编辑</el-button>
                  <button
                    type="button"
                    class="me-icon-btn me-icon-btn--danger"
                    title="删除文章"
                    @click="deleteArticle(a.id as string | number)"
                  >
                    <el-icon :size="16"><Delete /></el-icon>
                  </button>
                </div>
              </div>
              <div v-if="artTotal > artSize" style="margin-top: 16px; display: flex; gap: 8px; align-items: center">
                <el-button :disabled="artPage <= 1" @click="artPage--; loadArticles()">上一页</el-button>
                <span class="muted">第 {{ artPage }} / {{ artMaxPage() }} 页</span>
                <el-button :disabled="artPage >= artMaxPage()" @click="artPage++; loadArticles()">下一页</el-button>
              </div>
            </el-tab-pane>

            <el-tab-pane label="收藏" name="collections">
              <div v-if="colEmpty" class="me-empty">{{ colEmpty }}</div>
              <div v-for="c in collections" :key="String(c.article_id ?? c.articleId ?? c.id)" class="me-item-row">
                <img class="me-item-thumb" :src="coverUrl(c)" alt="" />
                <div>
                  <a class="me-item-title" :href="'/article/' + (c.article_id ?? c.articleId ?? c.id)">
                    {{ c.title || '文章' }}
                  </a>
                  <div v-if="c.summary" class="me-item-desc">{{ String(c.summary).slice(0, 120) }}</div>
                </div>
              </div>
            </el-tab-pane>

            <el-tab-pane label="插眼" name="cy">
              <div v-if="cyEmpty" class="me-empty">{{ cyEmpty || '切换到本标签加载' }}</div>
              <div v-for="row in cyList" :key="String(row.id)" class="me-item-row" style="flex-direction: column">
                <a :href="'/article/' + (row.article_id ?? row.articleId)">{{ row.article_title ?? row.articleTitle ?? '文章' }}</a>
                <div class="me-item-desc">{{ row.content }}</div>
              </div>
            </el-tab-pane>

            <el-tab-pane label="游戏" name="games">
              <div v-if="gameEmpty" class="me-empty">{{ gameEmpty }}</div>
              <div v-for="g in games" :key="String(g.game_id ?? g.gameId)" class="me-item-row">
                <div>{{ g.game_name ?? g.name ?? '游戏' }} — {{ g.status }}</div>
              </div>
            </el-tab-pane>

            <el-tab-pane label="话题" name="topics">
              <div v-if="topicEmpty" class="me-empty">{{ topicEmpty }}</div>
              <div v-for="t in topics" :key="String(t.topic_id ?? t.id)" class="me-item-row">
                <a class="me-item-title" :href="'/topic/' + (t.topic_id ?? t.id)">
                  {{ t.topic_name ?? t.topicName ?? t.name }}
                </a>
              </div>
            </el-tab-pane>

            <el-tab-pane label="通知" name="notifications">
              <div v-if="!notifications.length" class="me-empty">暂无实时通知（WebSocket 推送会显示在此）</div>
              <div v-for="(n, i) in notifications" :key="i" style="padding: 12px 0; border-bottom: 1px solid #eee">
                <div>{{ n.content }}</div>
                <div class="me-item-meta">{{ n.meta }}</div>
              </div>
            </el-tab-pane>
          </el-tabs>
        </main>
      </div>
    </div>

    <el-dialog v-model="showSettings" title="编辑资料" width="400px">
      <el-form label-position="top">
        <el-form-item label="用户名"><el-input v-model="settingsForm.name" /></el-form-item>
        <el-form-item label="邮箱"><el-input v-model="settingsForm.email" /></el-form-item>
        <el-form-item label="新密码"><el-input v-model="settingsForm.password" type="password" show-password placeholder="留空不修改" /></el-form-item>
        <el-form-item label="GitHub"><el-input v-model="settingsForm.github" /></el-form-item>
      </el-form>
      <p class="muted">{{ settingsMsg }}</p>
      <template #footer>
        <el-button @click="showSettings = false">取消</el-button>
        <el-button type="primary" @click="saveSettings">保存</el-button>
      </template>
    </el-dialog>
  </AppLayout>
</template>
