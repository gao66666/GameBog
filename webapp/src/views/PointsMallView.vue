<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppLayout from '@/layouts/AppLayout.vue'
import PointsLedgerDialog from '@/components/PointsLedgerDialog.vue'
import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import '@/assets/points-mall.css'

const router = useRouter()
const auth = useAuthStore()

interface Product {
  id: string
  name: string
  subtitle?: string
  description?: string
  pricePoints: number
  coverUrl?: string
  stock: number
  status?: string
}

interface Order {
  orderId: string
  productName: string
  pointsSpent: number
  code: string
  createdAt?: string
}

const loading = ref(true)
const walletBalance = ref(0)
const products = ref<Product[]>([])
const orders = ref<Order[]>([])
const redeemingId = ref('')
const showPointsLedger = ref(false)

async function loadWallet() {
  try {
    const resp = await api<{ balance?: number }>('/api/v1/points/wallet')
    walletBalance.value = Number(resp.data?.balance ?? 0)
  } catch {
    walletBalance.value = 0
  }
}

async function loadProducts() {
  const resp = await api<{ products?: Product[] }>('/api/v1/points/mall/products')
  products.value = (resp.data?.products ?? []).map((p) => ({
    ...p,
    id: String(p.id),
  }))
}

async function loadOrders() {
  if (!auth.isLoggedIn) {
    orders.value = []
    return
  }
  const resp = await api<{ orders?: Order[] }>('/api/v1/points/mall/orders')
  orders.value = (resp.data?.orders ?? []).map((o) => ({
    ...o,
    orderId: String(o.orderId ?? (o as Record<string, unknown>).order_id ?? ''),
    code: String(o.code ?? ''),
  }))
}

async function refresh() {
  loading.value = true
  try {
    await Promise.all([loadWallet(), loadProducts(), loadOrders()])
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

async function redeem(p: Product) {
  if (!auth.isLoggedIn) {
    router.push({ path: '/login', query: { redirect: '/points-mall' } })
    return
  }
  if (p.stock <= 0) {
    ElMessage.warning('已售罄')
    return
  }
  if (walletBalance.value < p.pricePoints) {
    ElMessage.warning('积分不足，可先签到或互动获取积分')
    return
  }
  try {
    await ElMessageBox.confirm(
      `使用 ${p.pricePoints} 积分兑换「${p.name}」？兑换后将展示激活码，请妥善保存。`,
      '确认兑换',
      { type: 'info' },
    )
  } catch {
    return
  }

  redeemingId.value = p.id
  const idem = `redeem-${p.id}-${Date.now()}`
  try {
    const resp = await api<{ order?: Order; activation_code?: string }>('/api/v1/points/mall/redeem', {
      method: 'POST',
      data: { product_id: p.id, idempotency_key: idem },
    })
    const code = resp.data?.activation_code ?? resp.data?.order?.code ?? ''
    ElMessage.success('兑换成功')
    if (code) {
      await ElMessageBox.alert(`激活码：${code}\n\n（演示环境码，非真实平台密钥）`, '请保存激活码', {
        confirmButtonText: '已复制到剪贴板',
      }).catch(() => {})
      try {
        await navigator.clipboard.writeText(code)
      } catch {
        /* ignore */
      }
    }
    await refresh()
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '兑换失败')
  } finally {
    redeemingId.value = ''
  }
}

onMounted(async () => {
  if (!auth.isLoggedIn) {
    router.push({ path: '/login', query: { redirect: '/points-mall' } })
    return
  }
  await refresh()
})
</script>

<template>
  <AppLayout>
    <PointsLedgerDialog v-model="showPointsLedger" />
    <div class="container page-mall">
      <header class="mall-hd">
        <div>
          <h1 class="mall-title">积分商城</h1>
          <p class="mall-sub muted">使用积分兑换游戏激活码（演示环境）</p>
        </div>
        <div class="mall-hd-actions">
          <button type="button" class="mall-balance mall-balance--click" title="查看积分流水" @click="showPointsLedger = true">
            我的积分：<strong>{{ walletBalance }}</strong>
          </button>
          <el-button @click="router.push('/me')">返回个人中心</el-button>
          <el-button :loading="loading" @click="refresh">刷新</el-button>
        </div>
      </header>

      <section v-if="loading" class="mall-empty muted">加载中…</section>

      <section v-else class="mall-grid">
        <article v-for="p in products" :key="p.id" class="mall-card">
          <img class="mall-cover" :src="p.coverUrl || ''" alt="" loading="lazy" />
          <div class="mall-card-body">
            <h2 class="mall-name">{{ p.name }}</h2>
            <p v-if="p.subtitle" class="mall-en muted">{{ p.subtitle }}</p>
            <p class="mall-desc">{{ p.description }}</p>
            <div class="mall-meta">
              <span class="mall-price">{{ p.pricePoints }} 积分</span>
              <span class="mall-stock" :class="{ out: p.stock <= 0 }">库存 {{ p.stock }}</span>
            </div>
            <el-button
              type="primary"
              :disabled="p.stock <= 0"
              :loading="redeemingId === p.id"
              @click="redeem(p)"
            >
              {{ p.stock > 0 ? '立即兑换' : '已售罄' }}
            </el-button>
          </div>
        </article>
      </section>

      <section class="mall-orders">
        <h2 class="mall-orders-title">我的兑换记录</h2>
        <div v-if="!orders.length" class="mall-empty muted">暂无兑换记录</div>
        <div v-for="o in orders" :key="o.orderId" class="mall-order-row">
          <div>
            <div class="mall-order-name">{{ o.productName }}</div>
            <div class="muted">消耗 {{ o.pointsSpent }} 积分 · {{ o.createdAt || '' }}</div>
          </div>
          <code class="mall-code">{{ o.code }}</code>
        </div>
      </section>
    </div>
  </AppLayout>
</template>
