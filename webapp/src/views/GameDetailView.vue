<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppLayout from '@/layouts/AppLayout.vue'
import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { gameCoverUrl, gamePriceLabel } from '@/utils/blog'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const gameId = computed(() => String(route.params.id || ''))

const game = ref<Record<string, unknown> | null>(null)
const stock = ref(-1)
const isFree = ref(false)
const purchasing = ref(false)
const accountBalance = ref<number | null>(null)

const reviews = ref<Array<Record<string, unknown>>>([])
const reviewsEmpty = ref('')
const reviewMsg = ref('')
const editingId = ref('')
const rating = ref(5)
const reviewContent = ref('')

const myReview = computed(() => {
  if (!auth.userId) return null
  return reviews.value.find((r) => String(r.userId ?? r.user_id) === auth.userId) || null
})

const showComposer = computed(() => auth.isLoggedIn && !myReview.value && !editingId.value)

async function loadAccountBalance() {
  if (!auth.isLoggedIn) {
    accountBalance.value = null
    return
  }
  try {
    const resp = await api<Record<string, unknown>>('/api/v1/users/me')
    const bal = resp.data?.account_balance ?? resp.data?.accountBalance
    accountBalance.value = bal != null ? Number(bal) : null
  } catch {
    accountBalance.value = null
  }
}

async function loadGame() {
  const resp = await api<{
    game?: Record<string, unknown>
    stock?: number
    free?: boolean
  }>('/api/v1/games/' + encodeURIComponent(gameId.value))
  game.value = resp.data?.game || null
  stock.value = Number(resp.data?.stock ?? -1)
  isFree.value = Boolean(resp.data?.free) || gamePriceLabel(game.value || {}) === '免费'
}

async function loadReviews() {
  const resp = await api<{ list?: Array<Record<string, unknown>> }>(
    '/api/v1/games/' + encodeURIComponent(gameId.value) + '/reviews?page=1&size=50',
  )
  reviews.value = resp.data?.list || []
  reviewsEmpty.value = reviews.value.length ? '' : '暂无点评'
}

function stars(n: unknown) {
  const x = Math.max(1, Math.min(5, Number(n) || 0))
  return '★'.repeat(x) + '☆'.repeat(5 - x)
}

function startEdit(r: Record<string, unknown>) {
  editingId.value = String(r.id)
  rating.value = Number(r.rating) || 5
  reviewContent.value = String(r.content || '')
}

function cancelEdit() {
  editingId.value = ''
  reviewContent.value = ''
  rating.value = 5
}

async function submitReview() {
  if (!auth.isLoggedIn) {
    reviewMsg.value = '请先登录'
    return
  }
  if (!reviewContent.value.trim()) {
    reviewMsg.value = '请填写内容'
    return
  }
  reviewMsg.value = '提交中…'
  try {
    if (editingId.value) {
      await api('/api/v1/game-reviews/' + encodeURIComponent(editingId.value), {
        method: 'PUT',
        data: { rating: rating.value, content: reviewContent.value.trim() },
      })
    } else {
      await api('/api/v1/games/' + encodeURIComponent(gameId.value) + '/reviews', {
        method: 'POST',
        data: { rating: rating.value, content: reviewContent.value.trim() },
      })
    }
    reviewMsg.value = '已保存'
    cancelEdit()
    await loadReviews()
  } catch (e) {
    reviewMsg.value = e instanceof Error ? e.message : String(e)
  }
}

async function deleteReview(id: string) {
  if (!confirm('确定删除？')) return
  await api('/api/v1/game-reviews/' + encodeURIComponent(id), { method: 'DELETE' })
  await loadReviews()
}

async function purchase() {
  if (!auth.isLoggedIn) {
    router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  if (purchasing.value || isFree.value) return

  const name = String(game.value?.name || '该游戏')
  const priceText = gamePriceLabel(game.value || {})
  const msg = `确认使用账户余额购买「${name}」（${priceText}）？`

  try {
    await ElMessageBox.confirm(msg, '确认', { type: 'warning' })
  } catch {
    return
  }

  purchasing.value = true
  const idem = `game-${gameId.value}-${Date.now()}`
  try {
    const resp = await api<{ order?: { code?: string }; activation_code?: string }>(
      '/api/v1/games/' + encodeURIComponent(gameId.value) + '/purchase',
      { method: 'POST', data: { idempotency_key: idem } },
    )
    const code = resp.data?.activation_code || resp.data?.order?.code || ''
    await loadAccountBalance()
    await loadGame()
    ElMessage.success(code ? `购买成功，激活码：${code}` : '购买成功')
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '购买失败')
  } finally {
    purchasing.value = false
  }
}

async function boot() {
  await loadGame()
  await loadAccountBalance()
  await loadReviews()
}

watch(gameId, boot)
onMounted(boot)
</script>

<template>
  <AppLayout>
    <div class="container page-game-detail" style="padding-bottom: 48px">
      <section v-if="game" class="card" style="display: flex; gap: 24px; flex-wrap: wrap">
        <img :src="gameCoverUrl(game)" :alt="String(game.name)" style="width: 280px; max-width: 100%; border-radius: 14px; object-fit: cover" />
        <div style="flex: 1; min-width: 240px">
          <h1>{{ game.name }}</h1>
          <p class="muted">{{ game.publisher }} · {{ game.developer }}</p>
          <p style="font-size: 20px; font-weight: 600; margin: 12px 0">{{ gamePriceLabel(game) }}</p>
          <p v-if="!isFree && stock >= 0" class="muted" style="font-size: 13px">库存 {{ stock }}</p>
          <p v-if="auth.isLoggedIn && accountBalance != null && !isFree" class="muted" style="font-size: 13px">
            账户余额 {{ accountBalance }}
          </p>
          <div v-if="!isFree" style="margin: 16px 0">
            <el-button type="primary" :loading="purchasing" @click="purchase">
              购买
            </el-button>
            <p v-if="!auth.isLoggedIn" class="muted" style="margin-top: 8px; font-size: 13px">登录后可购买</p>
          </div>
          <p>{{ game.description || '暂无简介' }}</p>
        </div>
      </section>

      <section class="card" style="margin-top: 20px">
        <h2>玩家点评</h2>
        <p v-if="!auth.isLoggedIn" class="muted">登录后可发表点评</p>

        <div v-if="showComposer" style="margin: 16px 0; padding: 16px; background: #f5f5f7; border-radius: 12px">
          <el-rate v-model="rating" :max="5" />
          <el-input v-model="reviewContent" type="textarea" :rows="4" placeholder="写下你的点评…" style="margin-top: 8px" />
          <el-button type="primary" style="margin-top: 8px" @click="submitReview">发表点评</el-button>
          <p class="muted">{{ reviewMsg }}</p>
        </div>

        <div v-else-if="editingId" style="margin: 16px 0; padding: 16px; background: #f5f5f7; border-radius: 12px">
          <el-rate v-model="rating" :max="5" />
          <el-input v-model="reviewContent" type="textarea" :rows="4" style="margin-top: 8px" />
          <div style="margin-top: 8px; display: flex; gap: 8px">
            <el-button type="primary" @click="submitReview">保存</el-button>
            <el-button @click="cancelEdit">取消</el-button>
          </div>
          <p class="muted">{{ reviewMsg }}</p>
        </div>

        <p v-if="reviewsEmpty" class="muted">{{ reviewsEmpty }}</p>
        <article v-for="r in reviews" :key="String(r.id)" style="padding: 16px 0; border-bottom: 1px solid #eee">
          <div style="display: flex; align-items: center; gap: 12px; flex-wrap: wrap">
            <strong>{{ r.userName || r.user_name || '玩家' }}</strong>
            <span>{{ stars(r.rating) }}</span>
            <span class="muted" style="font-size: 12px">{{ r.reviewedAt || r.reviewed_at }}</span>
          </div>
          <p style="margin-top: 8px; white-space: pre-wrap">{{ r.content }}</p>
          <div v-if="auth.userId && String(r.userId ?? r.user_id) === auth.userId" style="margin-top: 8px">
            <el-button link type="primary" @click="startEdit(r)">编辑</el-button>
            <el-button link type="danger" @click="deleteReview(String(r.id))">删除</el-button>
          </div>
        </article>
      </section>
    </div>
  </AppLayout>
</template>
