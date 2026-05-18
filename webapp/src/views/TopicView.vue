<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import { ElMessage } from 'element-plus'
import AppLayout from '@/layouts/AppLayout.vue'
import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { articleCoverUrl, fmtTime, pick } from '@/utils/blog'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const topicId = computed(() => String(route.params.id || ''))
const title = ref('加载中...')
const metaHtml = ref('')
const isTemp = ref(false)
const followed = ref(false)
const followHint = ref('')
const articles = ref<Array<Record<string, unknown>>>([])

async function loadTopic() {
  const resp = await api<{ topic?: Record<string, unknown>; isTopicFollowed?: boolean }>(
    '/api/v1/topics/' + encodeURIComponent(topicId.value),
  )
  const t = resp.data?.topic
  if (!t) throw new Error('话题不存在')
  followed.value = !!resp.data?.isTopicFollowed
  title.value = String(t.name || t.Name || '话题')
  isTemp.value = !!(t.isTemporary || t.is_temporary)
  const pills: string[] = []
  if (isTemp.value && (t.expiresAt || t.expires_at)) {
    pills.push('⏱ ' + fmtTime(t.expiresAt || t.expires_at))
  }
  pills.push('📄 ' + String(t.article_count || t.articleCount || 0) + ' 篇文章')
  metaHtml.value = pills.join(' · ')
}

async function loadArticles() {
  const resp = await api<{ article_list?: Array<Record<string, unknown>> }>(
    '/api/v1/topics/' + encodeURIComponent(topicId.value) + '/articles?page=1&size=30',
  )
  articles.value = resp.data?.article_list || []
}

async function toggleFollow() {
  if (!auth.isLoggedIn) {
    followHint.value = '请先登录'
    return
  }
  try {
    if (followed.value) {
      await api('/api/v1/follow/topic', { method: 'DELETE', data: { topicId: topicId.value } })
      followed.value = false
      followHint.value = '已取消关注'
    } else {
      await api('/api/v1/follow/topic', { method: 'POST', data: { topicId: topicId.value } })
      followed.value = true
      followHint.value = '已关注'
    }
  } catch (e) {
    followHint.value = e instanceof Error ? e.message : '操作失败'
  }
}

async function deleteTopic() {
  if (!confirm('确定删除该临时话题及其所有内容吗？')) return
  try {
    await api('/api/v1/topics/' + encodeURIComponent(topicId.value), { method: 'DELETE' })
    router.push('/topics')
  } catch (e) {
    ElMessage.error('删除失败：' + (e instanceof Error ? e.message : String(e)))
  }
}

async function boot() {
  await loadTopic()
  await loadArticles()
}

watch(topicId, () => boot())
onMounted(boot)
</script>

<template>
  <AppLayout>
    <div class="container page-topic">
      <section class="card">
        <h1>{{ title }}</h1>
        <p class="muted">{{ metaHtml }}</p>
        <div style="margin-top: 12px; display: flex; flex-wrap: wrap; gap: 8px">
          <el-button :type="followed ? 'default' : 'primary'" @click="toggleFollow">
            {{ followed ? '已关注' : '关注话题' }}
          </el-button>
          <RouterLink :to="'/topic/' + topicId + '/discuss'">
            <el-button>讨论区</el-button>
          </RouterLink>
          <RouterLink :to="{ path: '/editor', query: { topic_id: topicId } }">
            <el-button>在此话题下写文章</el-button>
          </RouterLink>
          <el-button v-if="isTemp" type="danger" plain @click="deleteTopic">删除临时话题</el-button>
        </div>
        <p v-if="followHint" class="muted">{{ followHint }}</p>
      </section>

      <section class="card" style="margin-top: 20px">
        <h2>话题下的文章</h2>
        <p v-if="!articles.length" class="muted">暂无文章</p>
        <div v-for="a in articles" :key="String(a.id)" class="article-item-row" style="display: flex; gap: 14px; padding: 14px 0; border-bottom: 1px solid #eee">
          <img :src="articleCoverUrl(a)" alt="" style="width: 88px; height: 66px; object-fit: cover; border-radius: 8px" loading="lazy" />
          <div>
            <RouterLink :to="'/article/' + a.id" class="article-item-title">{{ a.title }}</RouterLink>
            <p v-if="a.summary" class="muted">{{ String(a.summary).slice(0, 160) }}</p>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>
