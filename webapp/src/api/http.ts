import axios, { type AxiosRequestConfig } from 'axios'

export interface ApiResponse<T = unknown> {
  code: number
  message?: string
  data?: T
}

const http = axios.create({
  baseURL: '',
  timeout: 120_000,
  headers: { 'Content-Type': 'application/json' },
})

http.interceptors.request.use((config) => {
  const token = localStorage.getItem('gb_token')
  if (token) {
    config.headers = config.headers ?? {}
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

function messageFromAxiosError(err: unknown): string | null {
  if (!err || typeof err !== 'object' || !('response' in err)) return null
  const data = (err as { response?: { data?: unknown } }).response?.data
  if (data && typeof data === 'object') {
    const msg = (data as { message?: string }).message
    if (msg) return msg
    const errText = (data as { error?: string }).error
    if (errText) return errText
  }
  return null
}

export async function api<T = unknown>(
  path: string,
  config?: AxiosRequestConfig,
): Promise<ApiResponse<T>> {
  try {
    const res = await http.request<ApiResponse<T>>({
      url: path,
      ...config,
    })
    const body = res.data
    if (body && typeof body === 'object' && 'code' in body && body.code !== 0) {
      throw new Error(body.message || '请求失败')
    }
    return body
  } catch (e) {
    const biz = messageFromAxiosError(e)
    if (biz) throw new Error(biz)
    throw e
  }
}

export { http }
