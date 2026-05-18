<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { fmtTime } from '@/utils/blog'

const props = defineProps<{ articleId: string }>()
const emit = defineEmits<{ updated: [count: number] }>()

const auth = useAuthStore()
const CY_MARK = '👁 '
const CY_MAX = 200
const COMMENT_MAX = 500

type CommentNode = {
  id: number | string
  content?: string
  commentType?: string
  comment_type?: string
  createdAt?: string
  created_at?: string
  user?: { userName?: string; userId?: number | string }
  children?: CommentNode[]
  replyUser?: { userName?: string }
}

const list = ref<CommentNode[]>([])
const emptyText = ref('加载中...')
const parentId = ref('0')
const pendingCY = ref(false)
const input = ref('')
const msg = ref('')

const replyLabel = computed(() => {
  if (parentId.value === '0') return ''
  return '回复中 · '
})

const charMax = computed(() => (pendingCY.value && parentId.value === '0' ? CY_MAX : COMMENT_MAX))

async function load() {
  if (!props.articleId) return
  try {
    const resp = await api<{ list?: CommentNode[]; total?: number }>(
      `/api/v1/articles/comments?article_id=${encodeURIComponent(props.articleId)}&page=1&size=50&limit=5`,
    )
    list.value = resp.data?.list || []
    const total = Number(resp.data?.total ?? list.value.length)
    emptyText.value = list.value.length ? '' : '暂无评论，来抢沙发吧'
    emit('updated', total)
  } catch (e) {
    list.value = []
    emptyText.value = '评论加载失败：' + (e instanceof Error ? e.message : String(e))
  }
}

function cancelReply() {
  parentId.value = '0'
  pendingCY.value = false
}

function startReply(id: string | number, name: string) {
  parentId.value = String(id)
  pendingCY.value = false
  void name
}

function insertCY() {
  if (!input.value.startsWith(CY_MARK)) input.value = CY_MARK + input.value
  pendingCY.value = true
}

async function submit() {
  if (!auth.isLoggedIn) {
    msg.value = '请先登录'
    return
  }
  const content = input.value.trim()
  if (!content) {
    msg.value = '评论不能为空'
    return
  }
  try {
    if (pendingCY.value && parentId.value === '0') {
      await api('/api/v1/articles/comments/cy', {
        method: 'POST',
        data: { articleId: props.articleId, content },
      })
    } else {
      await api('/api/v1/articles/comments', {
        method: 'POST',
        data: {
          articleId: props.articleId,
          parentId: parentId.value,
          content,
        },
      })
    }
    input.value = ''
    cancelReply()
    msg.value = '已发送'
    await load()
  } catch (e) {
    msg.value = '发送失败：' + (e instanceof Error ? e.message : String(e))
  }
}

async function deleteComment(id: string | number) {
  if (!confirm('确定删除这条评论吗？')) return
  try {
    await api('/api/v1/articles/comments/' + encodeURIComponent(String(id)), { method: 'DELETE' })
    await load()
  } catch {
    msg.value = '删除失败'
  }
}

function isCY(c: CommentNode) {
  return String(c.commentType || c.comment_type || '').toLowerCase() === 'cy'
}

watch(() => props.articleId, () => load(), { immediate: true })
</script>

<template>
  <section class="comment-section">
    <h2 class="comment-section-title">评论</h2>

    <div v-if="auth.isLoggedIn" class="comment-editor-block">
      <p v-if="replyLabel" class="muted">
        {{ replyLabel }}<el-button link type="primary" @click="cancelReply">取消</el-button>
      </p>
      <p v-if="pendingCY" class="muted">CY 插眼模式</p>
      <el-input v-model="input" type="textarea" :rows="4" :maxlength="charMax" show-word-limit placeholder="写下评论…" />
      <div style="margin-top: 12px; display: flex; flex-wrap: wrap; gap: 10px">
        <el-button type="primary" @click="submit">发送</el-button>
        <el-button :disabled="parentId !== '0'" @click="insertCY">插眼 CY</el-button>
      </div>
      <p v-if="msg" class="muted" style="margin-top: 8px">{{ msg }}</p>
    </div>

    <p v-else class="muted" style="margin-bottom: 20px">登录后可评论</p>

    <p v-if="emptyText && !list.length" class="muted" style="text-align: center; padding: 24px 0">{{ emptyText }}</p>

    <div v-for="c in list" :key="String(c.id)" class="comment-item" :class="{ 'comment-item--cy': isCY(c) }">
      <div class="comment-header">
        <span class="comment-user">{{ c.user?.userName || '匿名' }}</span>
        <span v-if="isCY(c)" class="comment-badge">CY</span>
        <span class="muted" style="font-size: 12px">{{ fmtTime(c.createdAt || c.created_at) }}</span>
        <el-button v-if="auth.isLoggedIn" link type="primary" size="small" @click="startReply(c.id, c.user?.userName || '')">
          回复
        </el-button>
        <el-button
          v-if="auth.userId && String(c.user?.userId) === auth.userId"
          link
          type="danger"
          size="small"
          @click="deleteComment(c.id)"
        >
          删除
        </el-button>
      </div>
      <div class="comment-content">{{ c.content }}</div>
      <div v-if="c.children?.length" class="comment-children">
        <div v-for="ch in c.children" :key="String(ch.id)" class="comment-child-row">
          <span class="comment-user">{{ ch.user?.userName }}</span>
          <span v-if="ch.replyUser?.userName" class="muted"> → {{ ch.replyUser.userName }}</span>
          <div class="comment-content">{{ ch.content }}</div>
        </div>
      </div>
    </div>
  </section>
</template>
