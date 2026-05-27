<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import AppLayout from '@/layouts/AppLayout.vue'
import { api } from '@/api/http'
import { gameCoverUrl, gamePriceLabel, gameTags } from '@/utils/blog'

const games = ref<Array<Record<string, unknown>>>([])
const error = ref('')

async function load() {
  try {
    const resp = await api<{ list?: Array<Record<string, unknown>> }>('/api/v1/games?page=1&size=200')
    games.value = resp.data?.list || []
    error.value = games.value.length ? '' : '暂无游戏，可运行种子脚本导入'
  } catch (e) {
    games.value = []
    error.value = '加载失败：' + (e instanceof Error ? e.message : String(e))
  }
}

const featured = () => games.value.slice(0, 5)

onMounted(load)
</script>

<template>
  <AppLayout>
    <div class="container page-game-library" style="padding-bottom: 48px">
      <section class="card">
        <h1>游戏库</h1>
        <p class="muted">浏览社区收录的游戏与玩家点评</p>
      </section>

      <p v-if="error" class="muted" style="margin-top: 16px">{{ error }}</p>

      <section v-if="featured().length" class="card" style="margin-top: 20px">
        <h2>精选推荐</h2>
        <div style="display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 16px; margin-top: 16px">
          <RouterLink
            v-for="g in featured()"
            :key="String(g.id)"
            :to="'/game/' + g.id"
            class="gl-card"
            style="display: block; text-decoration: none; color: inherit; border: 1px solid #eee; border-radius: 14px; overflow: hidden"
          >
            <img :src="gameCoverUrl(g)" :alt="String(g.name)" style="width: 100%; height: 140px; object-fit: cover" loading="lazy" />
            <div style="padding: 14px">
              <div style="font-weight: 600">{{ g.name }}</div>
              <div style="margin-top: 6px; color: #0071e3">{{ gamePriceLabel(g) }}</div>
              <div style="margin-top: 8px; display: flex; flex-wrap: wrap; gap: 4px">
                <span v-for="t in gameTags(g, 3)" :key="t" style="font-size: 11px; background: #f0f0f5; padding: 2px 8px; border-radius: 6px">{{ t }}</span>
              </div>
            </div>
          </RouterLink>
        </div>
      </section>

      <section class="card" style="margin-top: 20px">
        <h2>全部游戏</h2>
        <div style="display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 14px; margin-top: 16px">
          <RouterLink
            v-for="g in games"
            :key="'all-' + g.id"
            :to="'/game/' + g.id"
            style="text-decoration: none; color: inherit; border: 1px solid #eee; border-radius: 12px; overflow: hidden"
          >
            <img :src="gameCoverUrl(g)" alt="" style="width: 100%; height: 100px; object-fit: cover" loading="lazy" />
            <div style="padding: 10px">
              <div style="font-size: 14px; font-weight: 600">{{ g.name }}</div>
              <div style="margin-top: 4px; font-size: 12px; color: #0071e3">{{ gamePriceLabel(g) }}</div>
            </div>
          </RouterLink>
        </div>
      </section>
    </div>
  </AppLayout>
</template>
