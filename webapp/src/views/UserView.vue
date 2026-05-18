<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import AppLayout from '@/layouts/AppLayout.vue'
import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { ensureWSConnected } from '@/composables/useWebSocket'
import { articleCoverUrl, pick, renderStatsHtml } from '@/utils/blog'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const uid = ref('')
const userName = ref('-')
const userPoints = ref('-')
const online = ref('-')
const actionMsg = ref('')
const articles = ref<Array<Record<string, unknown>>>([])
const articlesEmpty = ref('加载中...')
const page = ref(1)
const total = ref(0)
const size = 10

watch(
  () => route.params.id,
  (id) => {
    uid.value = String(id || '')
    page.value = 1
    if (uid.value) boot()
  },
)

async function loadUser() {
  const resp = await api<Record<string, unknown>>('/api/v1/users/' + encodeURIComponent(uid.value))
  const d = resp.data || {}
  userName.value = String(pick(d, ['user_name', 'userName'], '-'))
  if (auth.userId && String(auth.userId) === uid.value) {
    router.replace('/me')
  }
}

async function loadOnline() {
  try {
    const resp = await api<{ online?: boolean }>('/api/v1/users/' + encodeURIComponent(uid.value) + '/online')
    online.value = resp.data?.online ? '在线' : '离线'
  } catch {
    online.value = '-'
  }
}

async function loadPoints() {
  try {
    const resp = await api<{ balance?: number }>('/api/v1/users/' + encodeURIComponent(uid.value) + '/points')
    userPoints.value = String(resp.data?.balance ?? 0)
  } catch {
    userPoints.value = '-'
  }
}

async function followUser() {
  if (!auth.isLoggedIn) return
  try {
    await api('/api/v1/follow', { method: 'POST', data: { followingId: uid.value } })
    actionMsg.value = '已关注'
  } catch (e) {
    const m = e instanceof Error ? e.message : String(e)
    if (m.includes('重复') || m.includes('已关注')) actionMsg.value = '已关注'
    else actionMsg.value = '关注失败：' + m
  }
}

async function loadArticles() {
  articlesEmpty.value = '加载中...'
  try {
    const resp = await api<{ article_list?: Array<Record<string, unknown>>; total?: number }>(
      `/api/v1/articles/list?author_id=${encodeURIComponent(uid.value)}&page=${page.value}&size=${size}`,
    )
    articles.value = resp.data?.article_list || []
    total.value = Number(resp.data?.total || 0)
    articlesEmpty.value = articles.value.length ? '' : '暂无文章'
  } catch (e) {
    articlesEmpty.value = '加载失败：' + (e instanceof Error ? e.message : String(e))
  }
}

const maxPage = () => Math.max(1, Math.ceil(total.value / size))

let onlineTimer = 0
function scheduleOnline() {
  const now = Date.now()
  if (now - onlineTimer < 800) return
  onlineTimer = now
  loadOnline()
}

function onVis() {
  if (!document.hidden) scheduleOnline()
}

async function boot() {
  if (!uid.value) return
  ensureWSConnected()
  await loadUser()
  await loadOnline()
  await loadPoints()
  await loadArticles()
}

onMounted(() => {
  uid.value = String(route.params.id || '')
  boot()
  window.addEventListener('focus', scheduleOnline)
  document.addEventListener('visibilitychange', onVis)
})

onUnmounted(() => {
  window.removeEventListener('focus', scheduleOnline)
  document.removeEventListener('visibilitychange', onVis)
})
</script>

<template>
  <AppLayout>
    <div class="container page-user">
      <section class="card">
        <h1>{{ userName }} 的主页</h1>
        <p class="muted">UID {{ uid }} · {{ online }} · 积分 {{ userPoints }}</p>
        <div v-if="auth.isLoggedIn && auth.userId !== uid" style="margin-top: 12px; display: flex; gap: 8px">
          <el-button type="primary" @click="followUser">关注</el-button>
          <RouterLink :to="{ path: '/dm', query: { peer_id: uid } }">
            <el-button>私信</el-button>
          </RouterLink>
        </div>
        <p v-if="actionMsg" class="muted">{{ actionMsg }}</p>
      </section>

      <section class="card" style="margin-top: 20px">
        <h2>TA 的文章</h2>
        <p v-if="articlesEmpty" class="muted">{{ articlesEmpty }}</p>
        <div v-for="a in articles" :key="String(a.id)" class="item" style="padding: 12px 0; border-bottom: 1px solid #eee">
          <RouterLink :to="'/article/' + a.id">{{ a.title || '文章 ' + a.id }}</RouterLink>
          <p v-if="a.summary" class="muted">{{ String(a.summary).slice(0, 160) }}</p>
          <p class="muted" v-html="renderStatsHtml(a.viewCount, a.likeCount, a.commentCount)" />
        </div>
        <div v-if="total > size" class="me-pager" style="margin-top: 16px">
          <button type="button" :disabled="page <= 1" @click="page--; loadArticles()">上一页</button>
          <span class="muted">第 {{ page }} / {{ maxPage() }} 页</span>
          <button type="button" :disabled="page >= maxPage()" @click="page++; loadArticles()">下一页</button>
        </div>
      </section>
    </div>
  </AppLayout>
</template>
