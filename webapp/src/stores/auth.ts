import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

const TOKEN_KEY = 'gb_token'
const UID_KEY = 'gb_user_id'
const NAME_KEY = 'gb_user_name'

export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem(TOKEN_KEY) || '')
  const userId = ref(localStorage.getItem(UID_KEY) || '')
  const userName = ref(localStorage.getItem(NAME_KEY) || '')

  const isLoggedIn = computed(() => !!token.value)

  function setAuth(payload: { token: string; userId: string; userName: string }) {
    token.value = payload.token
    userId.value = payload.userId
    userName.value = payload.userName
    localStorage.setItem(TOKEN_KEY, payload.token)
    localStorage.setItem(UID_KEY, payload.userId)
    localStorage.setItem(NAME_KEY, payload.userName)
  }

  function clearAuth() {
    token.value = ''
    userId.value = ''
    userName.value = ''
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(UID_KEY)
    localStorage.removeItem(NAME_KEY)
  }

  return { token, userId, userName, isLoggedIn, setAuth, clearAuth }
})
