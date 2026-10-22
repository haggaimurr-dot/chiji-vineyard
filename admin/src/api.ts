import { currentAdmin, requirePermission } from './permissions'
import { requireDemoActor } from './demo-session'
import { mockCall } from './mock'
import { checkVideoFile, registerDemoVideo } from './video'

const useMock = import.meta.env.VITE_USE_MOCK !== 'false'
const baseUrl = import.meta.env.VITE_API_BASE_URL || ''

export const isDemo = useMock

export class ApiError extends Error {
  constructor(message: string, public readonly code = '', public readonly status = 0, public readonly retryAfter = 0) {
    super(message)
    this.name = 'ApiError'
  }
}

export async function uploadVideo(file: File, onProgress: (percent: number) => void, signal?: AbortSignal): Promise<{ fileID: string }> {
  if (useMock) requireDemoActor('admin.media.upload')
  else requirePermission(currentAdmin(), 'admin.media.upload')
  await checkVideoFile(file)
  if (signal?.aborted) throw new DOMException('上传已取消', 'AbortError')
  if (useMock) {
    onProgress(100)
    return { fileID: registerDemoVideo(file) }
  }
  if (!baseUrl) throw new Error('VITE_API_BASE_URL 未配置')
  const endpoint = new URL(baseUrl, window.location.href)
  endpoint.pathname = `${endpoint.pathname.replace(/\/$/, '')}/media/upload`
  endpoint.search = ''
  endpoint.hash = ''
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    const abort = () => xhr.abort()
    const cleanup = () => signal?.removeEventListener('abort', abort)
    xhr.open('POST', endpoint.href)
    xhr.timeout = 120000
    xhr.setRequestHeader('Authorization', localStorage.getItem('chiji_admin_token') || '')
    xhr.upload.onprogress = event => {
      if (event.lengthComputable) onProgress(Math.min(99, Math.round(event.loaded / event.total * 100)))
    }
    xhr.onload = () => {
      cleanup()
      if (xhr.status === 401) expireAdminSession('登录已失效，请重新登录后上传')
      try {
        const result = JSON.parse(xhr.responseText)
        if (xhr.status < 200 || xhr.status >= 300 || !result.ok || !result.data?.fileID) throw new Error(result.error?.message || '视频上传失败')
        onProgress(100)
        resolve(result.data)
      } catch (error) { reject(error instanceof SyntaxError ? new Error('视频上传失败，请检查服务上传大小限制与响应格式') : error) }
    }
    xhr.onerror = () => { cleanup(); reject(new Error('视频上传网络中断，请重试')) }
    xhr.ontimeout = () => { cleanup(); reject(new Error('视频上传超时，请检查网络后重试')) }
    xhr.onabort = () => { cleanup(); reject(new DOMException('上传已取消', 'AbortError')) }
    signal?.addEventListener('abort', abort, { once: true })
    if (signal?.aborted) { cleanup(); reject(new DOMException('上传已取消', 'AbortError')); return }
    const form = new FormData()
    form.append('file', file)
    xhr.send(form)
  })
}

export function expireAdminSession(message = '登录已失效，请重新登录') {
  localStorage.removeItem('chiji_admin_token')
  localStorage.removeItem('chiji_admin_user')
  window.dispatchEvent(new CustomEvent('chiji-admin-session-expired', { detail: { message } }))
}

export async function api<T>(action: string, data: Record<string, unknown> = {}): Promise<T> {
  if (useMock) {
    // Match the real HTTP transport and detach Vue reactive request objects.
    try { return await mockCall<T>(action, JSON.parse(JSON.stringify(data))) }
    catch (error) {
      const retryAfter = (error as { retryAfter?: number }).retryAfter
      if (retryAfter) throw new ApiError((error as Error).message, 'LOGIN_RATE_LIMITED', 429, retryAfter)
      if ((error as Error).message.includes('权限不足')) window.dispatchEvent(new Event('chiji-admin-permissions-changed'))
      if (action !== 'admin.login' && (error as Error).message.includes('登录已失效')) expireAdminSession((error as Error).message)
      throw error
    }
  }
  if (!baseUrl) throw new Error('VITE_API_BASE_URL 未配置')
  const response = await fetch(baseUrl, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: localStorage.getItem('chiji_admin_token') || '' },
    body: JSON.stringify({ action, data }),
  })
  const result = await response.json()
  if (!response.ok || !result.ok) {
    if (response.status === 403) window.dispatchEvent(new Event('chiji-admin-permissions-changed'))
    if (response.status === 401 && action !== 'admin.login') expireAdminSession(result.error?.message || '登录已失效，请重新登录')
    throw new ApiError(result.error?.message || '请求失败', result.error?.code || '', response.status, Number(result.retryAfter || response.headers.get('Retry-After')) || 60)
  }
  return result.data as T
}

export const money = (cents: number) => `¥${(cents / 100).toFixed(2)}`
export const dateTime = (value?: string) => {
  if (!value) return '—'
  const date = new Date(/^\d{4}-\d{2}-\d{2}$/.test(value) ? `${value}T00:00:00+08:00` : value)
  if (!Number.isFinite(date.getTime())) return '—'
  return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', dateStyle: 'medium', ...(!/^\d{4}-\d{2}-\d{2}$/.test(value) ? { timeStyle: 'short' as const } : {}) }).format(date)
}

