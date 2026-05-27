import { api } from '@/api/http'
import { useAuthStore } from '@/stores/auth'

const DEMO_TEL = import.meta.env.VITE_DEMO_LOGIN_TEL || '13800088001'
const DEMO_PASSWORD = import.meta.env.VITE_DEMO_LOGIN_PASSWORD || 'EvalTest123!'
const DEMO_USERNAME = import.meta.env.VITE_DEMO_LOGIN_NAME || 'demo'

export function isAutoLoginEnabled(): boolean {
  if (localStorage.getItem('gb_token')) return false
  if (import.meta.env.VITE_AUTO_LOGIN === '0') return false
  return true
}

function isUserNotFoundError(err: unknown): boolean {
  const msg = err instanceof Error ? err.message : String(err ?? '')
  return msg.includes('用户不存在') || msg.includes('40001')
}

async function loginDemoUser(auth: ReturnType<typeof useAuthStore>): Promise<boolean> {
  const resp = await api<{ token: string; user_id: number; user_name: string }>('/api/v1/login', {
    method: 'POST',
    data: { tel: DEMO_TEL, password: DEMO_PASSWORD },
  })
  const data = resp.data
  if (!data?.token) return false
  auth.setAuth({
    token: data.token,
    userId: String(data.user_id ?? ''),
    userName: String(data.user_name ?? ''),
  })
  return true
}

async function createDemoUser(): Promise<void> {
  await api<{ user_id: number }>('/api/v1/signup', {
    method: 'POST',
    data: {
      username: DEMO_USERNAME,
      tel: DEMO_TEL,
      password: DEMO_PASSWORD,
    },
  })
}

/** 未登录时：先登录默认账号；不存在则注册后再登录。 */
export async function ensureDefaultLogin(): Promise<void> {
  if (!isAutoLoginEnabled()) return

  const auth = useAuthStore()
  if (auth.isLoggedIn) return

  try {
    if (await loginDemoUser(auth)) return
  } catch (err) {
    if (!isUserNotFoundError(err)) return
  }

  try {
    await createDemoUser()
  } catch {
    // 并发注册或账号已存在时继续尝试登录
  }

  try {
    await loginDemoUser(auth)
  } catch {
    // 网络或后端不可用时静默跳过
  }
}
