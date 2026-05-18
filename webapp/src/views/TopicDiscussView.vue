<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import AppLayout from '@/layouts/AppLayout.vue'
import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { fmtTime, pick } from '@/utils/blog'

const route = useRoute()
const auth = useAuthStore()
const topicId = computed(() => String(route.params.id || ''))
const title = ref('')
const messages = ref<Array<Record<string, unknown>>>([])
const discussCount = ref('')
const input = ref('')
const msg = ref('')
const chatBoxRef = ref<HTMLElement | null>(null)

async function loadTopicTitle() {
  const resp = await api<{ topic?: Record<string, unknown> }>('/api/v1/topics/' + encodeURIComponent(topicId.value))
  const t = resp.data?.topic
  title.value = String(t?.name || t?.Name || '话题')
}

async function loadDiscussions() {
  const box = chatBoxRef.value
  const nearBottom = box ? box.scrollHeight - box.scrollTop - box.clientHeight < 40 : true
  const resp = await api<{ list?: Array<Record<string, unknown>>; total?: number }>(
    '/api/v1/topics/' + encodeURIComponent(topicId.value) + '/discussions?page=1&size=50',
  )
  const list = resp.data?.list || []
  const total = Number(resp.data?.total ?? list.length)
  messages.value = [...list].reverse()
  discussCount.value = total > list.length ? `本页 ${list.length} 条 · 共 ${total} 条` : `共 ${total} 条`
  if (nearBottom) {
    await nextTick()
    if (box) box.scrollTop = box.scrollHeight
  }
}

async function sendDiscussion() {
  if (!auth.isLoggedIn) {
    msg.value = '请先登录'
    return
  }
  const content = input.value.trim()
  if (!content) {
    msg.value = '内容不能为空'
    return
  }
  msg.value = ''
  try {
    await api('/api/v1/topics/' + encodeURIComponent(topicId.value) + '/discussions', {
      method: 'POST',
      data: { content },
    })
    input.value = ''
    await loadDiscussions()
  } catch (e) {
    msg.value = '发送失败：' + (e instanceof Error ? e.message : String(e))
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    sendDiscussion()
  }
}

watch(topicId, async () => {
  await loadTopicTitle()
  await loadDiscussions()
})

onMounted(async () => {
  await loadTopicTitle()
  await loadDiscussions()
})
</script>

<template>
  <AppLayout>
    <div class="container page-topic-discuss">
      <section class="card">
        <RouterLink :to="'/topic/' + topicId" class="muted">← 返回 {{ title }}</RouterLink>
        <h1 style="margin-top: 8px">{{ title }} · 讨论区</h1>
        <p class="muted">{{ discussCount }}</p>
      </section>

      <section class="card" style="margin-top: 16px">
        <div
          ref="chatBoxRef"
          class="topic-chat-box"
          style="max-height: 480px; overflow-y: auto; padding: 12px; background: #f5f5f7; border-radius: 12px"
        >
          <p v-if="!messages.length" class="muted">暂无讨论，来发第一条吧</p>
          <div
            v-for="(d, i) in messages"
            :key="i"
            :class="['discuss-item', String(d.userId || d.user_id) === auth.userId ? 'discuss-item-me' : 'discuss-item-other']"
            style="display: flex; gap: 10px; margin-bottom: 12px"
          >
            <div
              class="discuss-avatar"
              style="width: 36px; height: 36px; border-radius: 50%; background: #667eea; color: #fff; display: flex; align-items: center; justify-content: center; font-size: 14px"
            >
              {{ String(d.userName || d.user_name || '?').charAt(0) }}
            </div>
            <div class="discuss-bubble" style="background: #fff; padding: 10px 14px; border-radius: 12px; max-width: 75%">
              <div class="discuss-header" style="font-size: 12px; color: #86868b; margin-bottom: 4px">
                <span>{{ String(d.userId || d.user_id) === auth.userId ? (auth.userName || '我') : (d.userName || d.user_name || '匿名') }}</span>
                <span> · {{ fmtTime(d.createdAt || d.created_at) }}</span>
              </div>
              <div>{{ d.content }}</div>
            </div>
          </div>
        </div>

        <div style="margin-top: 16px">
          <el-input v-model="input" type="textarea" :rows="3" placeholder="输入讨论内容…" @keydown="onKeydown" />
          <div style="margin-top: 8px; display: flex; align-items: center; gap: 12px">
            <el-button type="primary" @click="sendDiscussion">发送</el-button>
            <span class="muted">{{ msg }}</span>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>
