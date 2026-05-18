<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import AppLayout from '@/layouts/AppLayout.vue'
import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { highlightPreview, renderMarkdown } from '@/composables/useMarkdown'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const articleId = ref('')
const statusText = ref('新建文章')
const msg = ref('')
const saving = ref(false)

const title = ref('')
const summary = ref('')
const content = ref('')
const tags = ref('')
const coverUrl = ref('')
const topicId = ref<number | ''>('')
const topics = ref<Array<{ id: number; name: string; isTemporary?: boolean }>>([])

const previewRef = ref<HTMLElement | null>(null)
const previewHtml = computed(() => renderMarkdown(content.value))

const isEdit = computed(() => !!articleId.value)
const saveLabel = computed(() => (isEdit.value ? '更新' : '发表'))

watch(content, async () => {
  await nextTick()
  highlightPreview(previewRef.value)
})

function normalizeTags(raw: string) {
  const parts = String(raw || '')
    .trim()
    .split(/\s+/g)
    .map((s) => s.trim())
    .filter(Boolean)
  const out: string[] = []
  const seen = new Set<string>()
  for (const t of parts) {
    if (seen.has(t)) continue
    seen.add(t)
    out.push(t)
    if (out.length >= 5) break
  }
  return out
}

async function loadTopics(prefer?: number) {
  try {
    const resp = await api<{ topics?: Array<Record<string, unknown>> }>('/api/v1/topics')
    const list = (resp.data?.topics || []) as Array<Record<string, unknown>>
    topics.value = list
      .map((t) => ({
        id: Number(t.id ?? t.ID),
        name: String(t.name ?? t.Name ?? ''),
        isTemporary: !!(t.isTemporary ?? t.is_temporary ?? t.IsTemporary),
      }))
      .filter((t) => t.id > 0 && t.name)
    if (prefer && topics.value.some((t) => t.id === prefer)) {
      topicId.value = prefer
    } else if (!topicId.value && topics.value.length) {
      topicId.value = topics.value[0].id
    }
  } catch (e) {
    topics.value = []
    ElMessage.error('话题加载失败：' + (e instanceof Error ? e.message : String(e)))
  }
}

async function loadArticle(id: string) {
  statusText.value = '加载中...'
  try {
    const resp = await api<Record<string, unknown>>('/api/v1/articles/' + encodeURIComponent(id))
    const d = resp.data
    if (!d) {
      statusText.value = '文章不存在'
      return
    }
    title.value = String(d.title || '')
    summary.value = String(d.summary || '')
    content.value = String(d.content || '')
    const tagList = Array.isArray(d.tags) ? d.tags : []
    tags.value = normalizeTags(
      tagList.map((x) => String((x as { name?: string }).name || '')).join(' '),
    ).join(' ')
    const rawCover =
      d.cover_url_edit != null
        ? String(d.cover_url_edit)
        : String(d.coverUrlRaw || d.cover_url_raw || '')
    coverUrl.value = rawCover.trim()
    const tid = Number(d.category_id ?? d.categoryId ?? 0)
    if (tid > 0) topicId.value = tid
    statusText.value = '正在编辑文章 #' + id
  } catch (e) {
    statusText.value = '加载失败'
    ElMessage.error(e instanceof Error ? e.message : String(e))
  }
}

async function saveArticle() {
  if (!auth.isLoggedIn) {
    ElMessage.warning('请先登录')
    router.push('/login')
    return
  }
  if (!title.value.trim() || !summary.value.trim() || !content.value.trim()) {
    ElMessage.warning('标题、摘要和内容不能为空')
    return
  }
  let tagNames: string[] = []
  try {
    tagNames = normalizeTags(tags.value)
    if (tagNames.length > 5) throw new Error('标签最多 5 个')
    tags.value = tagNames.join(' ')
  } catch (e) {
    ElMessage.warning(e instanceof Error ? e.message : '标签格式不正确')
    return
  }
  const tid = Number(topicId.value)
  if (!tid) {
    ElMessage.warning('请选择话题')
    return
  }

  const edit = isEdit.value
  const url = edit
    ? '/api/v1/articles/' + encodeURIComponent(articleId.value)
    : '/api/v1/articles'
  const method = edit ? 'PUT' : 'POST'

  saving.value = true
  msg.value = edit ? '更新中...' : '发表中...'
  try {
    const resp = await api<{ article_id?: number; id?: number }>(url, {
      method,
      data: {
        title: title.value.trim(),
        summary: summary.value.trim(),
        content: content.value.trim(),
        tags: tagNames,
        section_id: tid,
        cover_url: coverUrl.value.trim(),
      },
    })
    const aid = String(resp.data?.article_id ?? resp.data?.id ?? articleId.value)
    if (!edit && aid) {
      articleId.value = aid
      await router.replace({ path: '/editor', query: { id: aid } })
      statusText.value = '已发表文章 #' + aid
      msg.value = '发表成功'
    } else {
      statusText.value = '更新成功'
      msg.value = '更新成功'
    }
    ElMessage.success(msg.value)
  } catch (e) {
    msg.value = (edit ? '更新失败：' : '发表失败：') + (e instanceof Error ? e.message : String(e))
    ElMessage.error(msg.value)
  } finally {
    saving.value = false
  }
}

function resetNew() {
  title.value = ''
  summary.value = ''
  content.value = ''
  tags.value = ''
  coverUrl.value = ''
  articleId.value = ''
  statusText.value = '新建文章'
  msg.value = ''
  router.replace({ path: '/editor' })
}

function onImportFile(file: File) {
  if (!/\.(md|txt)$/i.test(file.name) && file.type !== 'text/plain') {
    ElMessage.warning('仅支持 .md 或 .txt 文件')
    return
  }
  const reader = new FileReader()
  reader.onload = () => {
    content.value = String(reader.result || '')
    if (!title.value.trim()) {
      title.value = file.name.replace(/\.(md|txt)$/i, '')
    }
    ElMessage.success('已导入 ' + file.name)
  }
  reader.onerror = () => ElMessage.error('读取文件失败')
  reader.readAsText(file, 'utf-8')
}

onMounted(async () => {
  const preferRaw =
    route.query.topic_id || route.query.topicId || route.query.section_id || ''
  const prefer = Number(preferRaw)
  await loadTopics(Number.isFinite(prefer) && prefer > 0 ? prefer : undefined)

  const qid = String(route.query.id || '')
  if (qid) {
    articleId.value = qid
    await loadArticle(qid)
  }
})
</script>

<template>
  <AppLayout>
    <div class="container page-editor editor-page">
      <section class="card editor-card">
        <div class="editor-header row">
          <h1 class="editor-title">{{ isEdit ? '编辑文章' : '写文章' }}</h1>
          <span class="muted">{{ statusText }}</span>
        </div>

        <div class="editor-toolbar row">
          <div class="row" style="gap: 8px; flex-wrap: wrap">
            <el-button @click="resetNew">新建</el-button>
            <el-button type="primary" :loading="saving" @click="saveArticle">{{ saveLabel }}</el-button>
            <label class="el-button">
              导入 .md / .txt
              <input type="file" accept=".md,.txt,text/plain" hidden @change="(e) => { const f = (e.target as HTMLInputElement).files?.[0]; if (f) onImportFile(f) }" />
            </label>
          </div>
          <span class="muted">{{ msg }}</span>
        </div>

        <div class="editor-layout">
          <el-form label-position="top">
            <el-form-item label="标题">
              <el-input v-model="title" maxlength="200" placeholder="请输入标题" />
            </el-form-item>
            <el-form-item label="话题">
              <el-select v-model="topicId" placeholder="选择话题" style="width: 100%" :disabled="!topics.length">
                <el-option
                  v-for="t in topics"
                  :key="t.id"
                  :label="t.name + (t.isTemporary ? '（临时）' : '')"
                  :value="t.id"
                />
              </el-select>
              <div class="muted" style="margin-top: 6px">临时话题 48 小时后会自动清理其文章与讨论区。</div>
            </el-form-item>
            <el-form-item label="标签（空格分割，最多 5 个）">
              <el-input v-model="tags" maxlength="200" placeholder="例如：Go 后端 缓存" />
            </el-form-item>
            <el-form-item label="摘要（必填）">
              <el-input v-model="summary" type="textarea" :rows="3" maxlength="500" />
            </el-form-item>
            <el-form-item label="封面图链接（可选 HTTPS）">
              <el-input v-model="coverUrl" type="url" maxlength="512" placeholder="https://..." />
            </el-form-item>
          </el-form>

          <div class="editor-frame">
            <div class="editor-frame-header">
              <div class="label">内容（Markdown）</div>
              <div class="label preview-label">预览</div>
            </div>
            <div class="editor-frame-body">
              <div class="editor-subpane editor-subpane-left">
                <el-input v-model="content" type="textarea" :rows="18" placeholder="在这里编写 Markdown..." />
              </div>
              <div class="editor-subpane editor-subpane-right">
                <div ref="previewRef" class="editor-preview content" v-html="previewHtml" />
              </div>
            </div>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<style scoped>
.editor-page {
  padding-bottom: 48px;
}
.editor-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}
.editor-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 16px;
}
</style>
