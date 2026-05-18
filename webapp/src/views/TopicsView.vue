<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import AppLayout from '@/layouts/AppLayout.vue'
import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { fmtTime, pick } from '@/utils/blog'

const auth = useAuthStore()
const topics = ref<Array<Record<string, unknown>>>([])
const topicCount = ref('加载中...')
const topicEmpty = ref('')
const page = ref(1)
const total = ref(0)
const size = 10
const createName = ref('')
const createKind = ref('long')
const createMsg = ref('')

async function load() {
  topicCount.value = '加载中...'
  try {
    const resp = await api<{ topics?: Array<Record<string, unknown>>; total?: number }>(
      `/api/v1/topics?page=${page.value}&size=${size}`,
    )
    topics.value = (resp.data?.topics || []) as Array<Record<string, unknown>>
    total.value = Number(resp.data?.total || 0)
    topicCount.value = total.value ? `共 ${total.value} 个话题` : '暂无话题'
    topicEmpty.value = topics.value.length ? '' : '暂无话题'
  } catch (e) {
    topicCount.value = '加载失败'
    topicEmpty.value = e instanceof Error ? e.message : String(e)
  }
}

async function createTopic() {
  if (!auth.isLoggedIn) {
    createMsg.value = '请先登录'
    return
  }
  if (!createName.value.trim()) {
    createMsg.value = '请输入话题名称'
    return
  }
  createMsg.value = '创建中...'
  try {
    await api('/api/v1/topics', {
      method: 'POST',
      data: { name: createName.value.trim(), kind: createKind.value },
    })
    createMsg.value = '创建成功'
    createName.value = ''
    page.value = 1
    await load()
  } catch (e) {
    createMsg.value = '失败：' + (e instanceof Error ? e.message : String(e))
  }
}

const maxPage = () => Math.max(1, Math.ceil(total.value / size))

onMounted(load)
</script>

<template>
  <AppLayout>
    <div class="container page-topics">
      <section class="card">
        <h1>话题广场</h1>
        <p class="muted">{{ topicCount }}</p>
        <div v-if="auth.isLoggedIn" class="form" style="margin-top: 16px">
          <el-input v-model="createName" placeholder="新话题名称" style="max-width: 280px; margin-right: 8px" />
          <el-radio-group v-model="createKind" style="margin-right: 8px">
            <el-radio label="long">长期</el-radio>
            <el-radio label="temp">临时</el-radio>
          </el-radio-group>
          <el-button type="primary" @click="createTopic">创建</el-button>
          <p class="muted">{{ createMsg }}</p>
        </div>
      </section>

      <section class="card" style="margin-top: 20px">
        <p v-if="topicEmpty" class="muted">{{ topicEmpty }}</p>
        <div v-for="t in topics" :key="String(t.id)" class="topic-card" style="margin-bottom: 12px; padding: 14px; border: 1px solid #eee; border-radius: 12px">
          <RouterLink :to="'/topic/' + t.id" class="topic-card-title">{{ t.name || t.Name }}</RouterLink>
          <div class="topic-card-meta muted">
            <span>{{ (t.isTemporary || t.is_temporary) ? '临时' : '长期' }}</span>
            <span v-if="t.expiresAt || t.expires_at"> · {{ fmtTime(t.expiresAt || t.expires_at) }}</span>
          </div>
        </div>
        <div v-if="total > size" class="me-pager">
          <button type="button" :disabled="page <= 1" @click="page--; load()">上一页</button>
          <span class="muted">第 {{ page }} / {{ maxPage() }} 页</span>
          <button type="button" :disabled="page >= maxPage()" @click="page++; load()">下一页</button>
        </div>
      </section>
    </div>
  </AppLayout>
</template>
