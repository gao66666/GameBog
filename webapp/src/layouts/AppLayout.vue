<script setup lang="ts">
import AppNav from '@/components/AppNav.vue'
import { onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { ensureWSConnected } from '@/composables/useWebSocket'

const auth = useAuthStore()

onMounted(() => {
  if (auth.isLoggedIn) ensureWSConnected()
})
</script>

<template>
  <div class="vue-app-shell">
    <AppNav />
    <main class="vue-main">
      <slot />
    </main>
  </div>
</template>

<style scoped>
.vue-app-shell {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
}
.vue-main {
  flex: 1;
}
</style>
