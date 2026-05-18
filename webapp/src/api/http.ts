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

export async function api<T = unknown>(
  path: string,
  config?: AxiosRequestConfig,
): Promise<ApiResponse<T>> {
  const res = await http.request<ApiResponse<T>>({
    url: path,
    ...config,
  })
  const body = res.data
  if (body && typeof body === 'object' && 'code' in body && body.code !== 0) {
    throw new Error(body.message || '请求失败')
  }
  return body
}

export { http }
