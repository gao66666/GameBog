<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api/http'

export interface LedgerRow {
  txnId?: string
  amount: number
  type?: string
  refType?: string
  refId?: string
  balanceAfter: number
  description?: string
  createdAt?: string
}

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

const loading = ref(false)
const rows = ref<LedgerRow[]>([])
const total = ref(0)
const page = ref(1)
const size = 20

function pick(obj: Record<string, unknown>, keys: string[], fallback: unknown = '') {
  for (const k of keys) {
    if (obj[k] !== undefined && obj[k] !== null) return obj[k]
  }
  return fallback
}

function normalizeRow(raw: Record<string, unknown>): LedgerRow {
  return {
    txnId: String(pick(raw, ['txnId', 'txn_id'], '')),
    amount: Number(pick(raw, ['amount'], 0)),
    type: String(pick(raw, ['type'], '')),
    refType: String(pick(raw, ['refType', 'ref_type'], '')),
    refId: String(pick(raw, ['refId', 'ref_id'], '')),
    balanceAfter: Number(pick(raw, ['balanceAfter', 'balance_after'], 0)),
    description: String(pick(raw, ['description'], '')),
    createdAt: String(pick(raw, ['createdAt', 'created_at'], '')),
  }
}

function refTypeLabel(refType: string) {
  const map: Record<string, string> = {
    initial_grant: '新用户赠送',
    article: '发文奖励',
    comment: '评论奖励',
    article_liked: '文章被赞',
    comment_liked: '评论被赞',
    checkin: '签到',
    mall_redeem: '商城兑换',
  }
  return map[refType] || refType || '—'
}

function formatTime(s: string) {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  return d.toLocaleString('zh-CN', { hour12: false })
}

function formatAmount(n: number) {
  if (n > 0) return '+' + n
  return String(n)
}

function extractLedgerList(payload: unknown): Record<string, unknown>[] {
  if (payload == null) return []
  if (Array.isArray(payload)) {
    return payload.filter((x) => x && typeof x === 'object') as Record<string, unknown>[]
  }
  if (typeof payload !== 'object') return []
  const d = payload as Record<string, unknown>
  const raw = d.list ?? d.transactions ?? d.transaction_list
  if (Array.isArray(raw)) {
    return raw.filter((x) => x && typeof x === 'object') as Record<string, unknown>[]
  }
  return []
}

async function load() {
  loading.value = true
  try {
    const resp = await api<Record<string, unknown>>(
      `/api/v1/points/transactions?page=${page.value}&size=${size}`,
    )
    const payload = resp.data ?? resp
    const list = extractLedgerList(payload)
    rows.value = list.map((r) => normalizeRow(r))
    const p = payload && typeof payload === 'object' ? (payload as Record<string, unknown>) : {}
    total.value = Number(p.total ?? list.length)
  } catch (e) {
    rows.value = []
    total.value = 0
    ElMessage.error(e instanceof Error ? e.message : '加载积分流水失败')
  } finally {
    loading.value = false
  }
}

function close() {
  emit('update:modelValue', false)
}

const maxPage = () => Math.max(1, Math.ceil(total.value / size))

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      page.value = 1
      load()
    }
  },
)

watch(page, () => {
  if (props.modelValue) load()
})
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="积分流水"
    width="640px"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div v-loading="loading" class="ledger-wrap">
      <div v-if="!loading && !rows.length" class="ledger-empty muted">暂无流水记录</div>
      <table v-else class="ledger-table">
        <thead>
          <tr>
            <th>时间</th>
            <th>说明</th>
            <th>变动</th>
            <th>余额</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.txnId">
            <td class="ledger-time">{{ formatTime(row.createdAt || '') }}</td>
            <td>
              <div>{{ row.description || refTypeLabel(row.refType || '') }}</div>
              <div v-if="row.refType" class="ledger-sub muted">{{ refTypeLabel(row.refType) }}</div>
            </td>
            <td :class="row.amount >= 0 ? 'ledger-plus' : 'ledger-minus'">
              {{ formatAmount(row.amount) }}
            </td>
            <td>{{ row.balanceAfter }}</td>
          </tr>
        </tbody>
      </table>
      <div v-if="total > size" class="ledger-pager">
        <el-button size="small" :disabled="page <= 1" @click="page--">上一页</el-button>
        <span class="muted">第 {{ page }} / {{ maxPage() }} 页 · 共 {{ total }} 条</span>
        <el-button size="small" :disabled="page >= maxPage()" @click="page++">下一页</el-button>
      </div>
    </div>
    <template #footer>
      <el-button @click="close">关闭</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.ledger-wrap {
  min-height: 120px;
}
.ledger-empty {
  text-align: center;
  padding: 32px 0;
}
.ledger-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}
.ledger-table th,
.ledger-table td {
  padding: 10px 8px;
  border-bottom: 1px solid #eee;
  text-align: left;
  vertical-align: top;
}
.ledger-table th {
  color: #86868b;
  font-weight: 500;
}
.ledger-time {
  white-space: nowrap;
  font-size: 13px;
}
.ledger-sub {
  font-size: 12px;
  margin-top: 2px;
}
.ledger-plus {
  color: #2e7d32;
  font-weight: 600;
}
.ledger-minus {
  color: #c45c26;
  font-weight: 600;
}
.ledger-pager {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-top: 16px;
}
</style>
