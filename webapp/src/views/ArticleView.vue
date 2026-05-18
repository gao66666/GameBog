<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import AppLayout from '@/layouts/AppLayout.vue'
import ArticleComments from '@/components/ArticleComments.vue'
import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { highlightPreview, renderMarkdown } from '@/composables/useMarkdown'
import { renderStatsHtml } from '@/utils/blog'
import { normalizeArticleTags } from '@/utils/articleTags'
import { ChatLineRound, House, Plus, Star, StarFilled } from '@element-plus/icons-vue'
import '@/assets/article.css'

const txt = {
  toc: '\u76ee\u5f55',
  tags: '\u5173\u952e\u8bcd',
  user: '\u7528\u6237',
  author: '\u4f5c\u8005',
  notFound: '\u6587\u7ae0\u4e0d\u5b58\u5728',
  liked: '\u5df2\u70b9\u8d5e',
  likeFail: '\u70b9\u8d5e\u5931\u8d25\uff1a',
  uncollect: '\u5df2\u53d6\u6d88\u6536\u85cf',
  collected: '\u5df2\u52a0\u5165\u6536\u85cf',
  collectFail: '\u6536\u85cf\u5931\u8d25',
  loginFirst: '\u8bf7\u5148\u767b\u5f55',
  followed: '\u5df2\u5173\u6ce8',
  followFail: '\u5173\u6ce8\u5931\u8d25',
  like: '\u70b9\u8d5e',
  likeLogin: '\u767b\u5f55\u540e\u70b9\u8d5e',
  collect: '\u6536\u85cf',
  collectedLbl: '\u5df2\u6536\u85cf',
  collectLogin: '\u767b\u5f55\u540e\u6536\u85cf',
  uncollectTip: '\u53d6\u6d88\u6536\u85cf',
  home: '\u4f5c\u8005\u4e3b\u9875',
  follow: '\u5173\u6ce8\u4f5c\u8005',
  followLogin: '\u767b\u5f55\u540e\u5173\u6ce8',
  dm: '\u79c1\u4fe1\u4f5c\u8005',
  dmLogin: '\u767b\u5f55\u540e\u79c1\u4fe1',
  sideActions: '\u4f5c\u8005\u64cd\u4f5c',
  selfArticle: '\u4e0d\u80fd\u5173\u6ce8\u81ea\u5df1',
}

const route = useRoute()
const auth = useAuthStore()

const articleId = computed(() => String(route.params.id || ''))
const article = ref<Record<string, unknown> | null>(null)
const loadError = ref('')
const actionHint = ref('')
const sideHint = ref('')
const followDone = ref(false)
const isCollected = ref(false)
const likeCount = ref(0)
const viewCount = ref(0)
const commentCount = ref(0)
const contentHtml = ref('')
const contentRef = ref<HTMLElement | null>(null)
const tocItems = ref<Array<{ id: string; text: string; level: number }>>([])

const tagList = computed(() => normalizeArticleTags(article.value?.tags))

const metaHtml = computed(() => {
  if (!article.value) return ''
  return renderStatsHtml(viewCount.value, likeCount.value, commentCount.value)
})

const authorId = computed(() => {
  const a = article.value
  if (!a) return ''
  const raw = a.author_id ?? a.authorId ?? a.AuthorID ?? ''
  const id = String(raw).trim()
  return id && id !== '0' ? id : ''
})

const authorName = computed(() => {
  const fromUser = author.value?.user_name ?? author.value?.userName
  if (fromUser) return String(fromUser)
  return authorId.value ? `${txt.user} ${authorId.value}` : txt.author
})

const authorInitial = computed(() => {
  const t = authorName.value.trim()
  return t ? t.charAt(0).toUpperCase() : '?'
})

const createdAtText = computed(() => {
  const t = article.value?.created_at ?? article.value?.createdAt
  return t ? String(t) : ''
})

const isOwnArticle = computed(() => {
  const aid = authorId.value
  const uid = String(auth.userId || '').trim()
  if (!aid || !uid) return false
  return uid === aid || Number(uid) === Number(aid)
})

const dmLink = computed(() => {
  if (isOwnArticle.value) return { path: '/me' }
  if (!auth.isLoggedIn) return { path: '/login', query: { redirect: route.fullPath } }
  return { path: '/dm', query: { peer_id: authorId.value } }
})

function onFollowClick() {
  if (isOwnArticle.value) {
    sideHint.value = txt.selfArticle
    return
  }
  followAuthor()
}

function onDmClick(e: MouseEvent) {
  if (isOwnArticle.value) {
    e.preventDefault()
    sideHint.value = txt.selfArticle
  }
}

const collectLabel = computed(() => (isCollected.value ? txt.collectedLbl : txt.collect))

function buildToc() {
  const root = contentRef.value
  if (!root) return
  const headings = Array.from(root.querySelectorAll('h1,h2,h3,h4,h5,h6'))
  const used: Record<string, number> = {}
  tocItems.value = headings.map((h, idx) => {
    let text = (h.textContent || '').trim() || `section-${idx + 1}`
    let id = h.id || text.toLowerCase().replace(/\s+/g, '-').replace(/[^\w\u4e00-\u9fa5\-]+/g, '')
    if (!id) id = `h-${idx + 1}`
    const base = id
    let n = used[base] || 0
    while (document.getElementById(id)) {
      n++
      id = `${base}-${n}`
    }
    used[base] = n
    h.id = id
    const level = Number(h.tagName.replace(/^H/i, '')) || 1
    return { id, text, level }
  })
}

async function loadArticle() {
  loadError.value = ''
  try {
    const resp = await api<Record<string, unknown>>('/api/v1/articles/' + encodeURIComponent(articleId.value))
    article.value = resp.data || null
    if (!article.value) throw new Error(txt.notFound)
    isCollected.value = !!(article.value.is_collected || article.value.isCollected)
    viewCount.value = Number(article.value.view_count ?? article.value.viewCount ?? 0)
    likeCount.value = Number(article.value.like_count ?? article.value.likeCount ?? 0)
    const html = article.value.html_content ?? article.value.htmlContent
    contentHtml.value =
      html && String(html).trim()
        ? String(html)
        : renderMarkdown(String(article.value.content || ''))
    await nextTick()
    highlightPreview(contentRef.value)
    buildToc()
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

async function likeArticle() {
  if (!auth.isLoggedIn) return
  const prev = likeCount.value
  likeCount.value = prev + 1
  try {
    await api('/api/v1/articles/like', {
      method: 'POST',
      data: { article_id: articleId.value, is_cancel: false },
    })
    actionHint.value = txt.liked
    sideHint.value = ''
  } catch (e) {
    likeCount.value = prev
    actionHint.value = txt.likeFail + (e instanceof Error ? e.message : String(e))
  }
}

async function toggleCollect() {
  if (!auth.isLoggedIn) return
  try {
    if (isCollected.value) {
      await api('/api/v1/articles/collect', {
        method: 'DELETE',
        data: { articleId: articleId.value },
      })
      isCollected.value = false
      actionHint.value = txt.uncollect
      sideHint.value = ''
    } else {
      await api('/api/v1/articles/collect', {
        method: 'POST',
        data: { articleId: articleId.value },
      })
      isCollected.value = true
      actionHint.value = txt.collected
      sideHint.value = ''
    }
  } catch {
    actionHint.value = txt.collectFail
  }
}

async function followAuthor() {
  if (!authorId.value) return
  if (!auth.isLoggedIn) {
    sideHint.value = txt.loginFirst
    return
  }
  try {
    await api('/api/v1/follow', { method: 'POST', data: { followingId: authorId.value } })
    followDone.value = true
    sideHint.value = txt.followed
    actionHint.value = ''
  } catch {
    sideHint.value = txt.followFail
  }
}

const author = ref<Record<string, unknown> | null>(null)

async function loadAuthor() {
  if (!authorId.value) return
  try {
    const resp = await api<Record<string, unknown>>('/api/v1/users/' + encodeURIComponent(authorId.value))
    author.value = resp.data || null
  } catch {
    author.value = null
  }
}

function onCommentsUpdated(n: number) {
  commentCount.value = n
}

watch(articleId, async () => {
  followDone.value = false
  sideHint.value = ''
  await loadArticle()
  await loadAuthor()
})

onMounted(async () => {
  await loadArticle()
  await loadAuthor()
})
</script>

<template>
  <AppLayout>
    <div class="container page-article">
      <p v-if="loadError" class="muted" style="padding: 48px 0; text-align: center">{{ loadError }}</p>

      <div v-else-if="article" class="article-shell">
        <aside v-if="tocItems.length" class="article-toc-card">
          <h2 class="article-toc-title">{{ txt.toc }}</h2>
          <nav class="article-toc-list">
            <a
              v-for="t in tocItems"
              :key="t.id"
              :href="'#' + t.id"
              class="article-toc-item"
              :class="'level-' + t.level"
            >
              {{ t.text }}
            </a>
          </nav>
        </aside>

        <main class="article-main">
          <header class="article-hd">
            <figure v-if="article.cover_url || article.coverUrl" class="article-cover">
              <img :src="String(article.cover_url || article.coverUrl)" alt="" loading="lazy" />
            </figure>
            <h1>{{ article.title }}</h1>
            <div class="article-meta">
              <span v-if="createdAtText" class="stat">{{ createdAtText }}</span>
              <span v-html="metaHtml" />
            </div>
            <div v-if="tagList.length" class="article-tags" :aria-label="txt.tags">
              <span v-for="name in tagList" :key="name" class="article-tag">#{{ name }}</span>
            </div>
          </header>

          <section class="article-body">
            <div ref="contentRef" class="content" v-html="contentHtml" />
          </section>

          <footer class="article-footer-actions">
            <button
              type="button"
              class="article-engage-btn"
              :disabled="!auth.isLoggedIn"
              :title="auth.isLoggedIn ? txt.like : txt.likeLogin"
              @click="likeArticle"
            >
              <span class="article-engage-icon" aria-hidden="true">&#128077;</span>
              <span class="article-engage-label">{{ txt.like }}</span>
              <span class="article-engage-count">{{ likeCount }}</span>
            </button>
            <button
              type="button"
              class="article-engage-btn"
              :class="{ 'is-active': isCollected }"
              :disabled="!auth.isLoggedIn"
              :title="auth.isLoggedIn ? (isCollected ? txt.uncollectTip : txt.collect) : txt.collectLogin"
              @click="toggleCollect"
            >
              <el-icon class="article-engage-icon-el" :size="20">
                <StarFilled v-if="isCollected" />
                <Star v-else />
              </el-icon>
              <span class="article-engage-label">{{ collectLabel }}</span>
            </button>
            <p v-if="actionHint" class="article-footer-hint">{{ actionHint }}</p>
          </footer>

          <ArticleComments :article-id="articleId" @updated="onCommentsUpdated" />
        </main>

        <aside v-if="authorId" class="article-sidebar">
          <RouterLink :to="'/u/' + authorId" class="article-sidebar-avatar" :title="authorName">
            {{ authorInitial }}
          </RouterLink>
          <p class="article-sidebar-name">{{ authorName }}</p>
          <nav class="article-sidebar-icons" :aria-label="txt.sideActions">
            <RouterLink :to="'/u/' + authorId" class="article-side-icon article-side-icon--home" :title="txt.home">
              <el-icon :size="22"><House /></el-icon>
              <span class="sr-only">{{ txt.home }}</span>
            </RouterLink>
            <button
              type="button"
              class="article-side-icon article-side-icon--follow"
              :class="{ 'is-done': followDone && !isOwnArticle, 'is-muted': isOwnArticle }"
              :title="isOwnArticle ? txt.selfArticle : auth.isLoggedIn ? txt.follow : txt.followLogin"
              @click="onFollowClick"
            >
              <el-icon :size="22"><Plus /></el-icon>
              <span class="sr-only">{{ txt.follow }}</span>
            </button>
            <RouterLink
              :to="dmLink"
              class="article-side-icon article-side-icon--dm"
              :class="{ 'is-muted': isOwnArticle }"
              :title="isOwnArticle ? txt.selfArticle : auth.isLoggedIn ? txt.dm : txt.dmLogin"
              @click="onDmClick"
            >
              <el-icon :size="22"><ChatLineRound /></el-icon>
              <span class="sr-only">{{ txt.dm }}</span>
            </RouterLink>
          </nav>
          <p v-if="sideHint" class="article-sidebar-hint">{{ sideHint }}</p>
        </aside>
      </div>
    </div>
  </AppLayout>
</template>
