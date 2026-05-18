<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import AppLayout from '@/layouts/AppLayout.vue'
import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const mode = ref<'login' | 'register'>('login')
const loading = ref(false)

const loginForm = reactive({ tel: '', password: '' })
const regForm = reactive({ username: '', tel: '', password: '' })

async function doLogin(tel: string, password: string) {
  const resp = await api<{ token: string; user_id: number; user_name: string }>('/api/v1/login', {
    method: 'POST',
    data: { tel, password },
  })
  const data = resp.data
  if (!data?.token) throw new Error('登录返回缺少 token')
  auth.setAuth({
    token: data.token,
    userId: String(data.user_id ?? ''),
    userName: String(data.user_name ?? ''),
  })
  const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
  router.push(redirect || '/')
}

async function onLogin() {
  if (!loginForm.tel.trim() || !loginForm.password) {
    ElMessage.warning('请填写手机号和密码')
    return
  }
  loading.value = true
  try {
    await doLogin(loginForm.tel.trim(), loginForm.password)
  } catch (e) {
    ElMessage.error('登录失败：' + (e instanceof Error ? e.message : String(e)))
  } finally {
    loading.value = false
  }
}

async function onRegister() {
  if (!regForm.username.trim() || !regForm.tel.trim() || !regForm.password) {
    ElMessage.warning('请填写用户名、手机号和密码')
    return
  }
  loading.value = true
  try {
    const signup = await api<{ user_id: number }>('/api/v1/signup', {
      method: 'POST',
      data: {
        username: regForm.username.trim(),
        tel: regForm.tel.trim(),
        password: regForm.password,
      },
    })
    if (!signup.data?.user_id) throw new Error('注册返回缺少 user_id')
    await doLogin(regForm.tel.trim(), regForm.password)
  } catch (e) {
    ElMessage.error('注册失败：' + (e instanceof Error ? e.message : String(e)))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AppLayout>
    <div class="container page-login">
      <section class="card login-card">
        <h2>{{ mode === 'login' ? '登录' : '注册' }}</h2>
        <p class="muted">
          {{ mode === 'login' ? '使用手机号 + 密码登录。' : '注册成功后会自动登录，并跳转到首页。' }}
        </p>

        <el-form v-if="mode === 'login'" label-position="top" @submit.prevent="onLogin">
          <el-form-item label="手机号">
            <el-input v-model="loginForm.tel" inputmode="tel" autocomplete="username" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="loginForm.password" type="password" show-password autocomplete="current-password" />
          </el-form-item>
          <el-button type="primary" native-type="submit" :loading="loading" style="width: 100%">登录</el-button>
        </el-form>

        <el-form v-else label-position="top" @submit.prevent="onRegister">
          <el-form-item label="账号（用户名）">
            <el-input v-model="regForm.username" maxlength="50" />
          </el-form-item>
          <el-form-item label="手机号（唯一）">
            <el-input v-model="regForm.tel" maxlength="20" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="regForm.password" type="password" show-password />
          </el-form-item>
          <el-button type="primary" native-type="submit" :loading="loading" style="width: 100%">注册并登录</el-button>
        </el-form>

        <p class="muted" style="margin-top: 12px; font-size: 13px">
          测试账号：手机号 <strong>123456</strong>，密码 <strong>123456</strong>
        </p>
        <p class="muted" style="margin-top: 8px; font-size: 13px">
          <template v-if="mode === 'login'">
            还没有账号？
            <el-button link type="primary" @click="mode = 'register'">去注册</el-button>
          </template>
          <template v-else>
            已有账号？
            <el-button link type="primary" @click="mode = 'login'">去登录</el-button>
          </template>
        </p>
      </section>
    </div>
  </AppLayout>
</template>
