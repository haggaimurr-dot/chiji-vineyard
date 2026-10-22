export const MAX_VIDEO_BYTES = 20 * 1024 * 1024
const demoVideos = new Set<string>()

export function registerDemoVideo(file: File): string {
  const url = URL.createObjectURL(file)
  demoVideos.add(url)
  return url
}

export function videoAddressError(value: string, allowDemo = false): string {
  if (!value.trim()) return '请上传视频或填写视频地址'
  if (value.startsWith('cloud://')) return '请将云存储 fileID 转换为可公开访问的 HTTPS 视频地址'
  if (allowDemo && demoVideos.has(value)) return ''
  if (!/^https?:\/\//i.test(value) || /[\u0000-\u0020\u007f\\]/.test(value)) return '请输入完整的 HTTPS 视频地址，地址不能包含空白或反斜线'
  try {
    const url = new URL(value)
    const local = ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
    if ((url.protocol === 'https:' || (local && url.protocol === 'http:')) && !url.username && !url.password && value.length <= 2048) return ''
  } catch { /* Return the same actionable address error. */ }
  return '请输入完整的 HTTPS 视频地址；本地开发可使用 localhost 的 HTTP 地址'
}

export async function checkVideoFile(file: File): Promise<void> {
  if (file.size === 0) throw new Error('视频文件为空')
  if (file.size > MAX_VIDEO_BYTES) throw new Error('视频不能超过 20MB，请压缩后重试')
  if (!/\.mp4$/i.test(file.name) || (file.type && file.type !== 'video/mp4')) throw new Error('请上传 MP4 视频，建议使用 H.264 视频和 AAC 音频编码')
  const header = new Uint8Array(await file.slice(0, 12).arrayBuffer())
  if (header.length < 12 || String.fromCharCode(...header.slice(4, 8)) !== 'ftyp') throw new Error('文件不是有效的 MP4 视频')
}

export interface VideoMetadata { duration: number; width: number; height: number }

export function checkVideoPlayback(source: string, signal?: AbortSignal): Promise<VideoMetadata> {
  return new Promise((resolve, reject) => {
    const video = document.createElement('video')
    let settled = false
    const finish = (error?: Error) => {
      if (settled) return
      settled = true
      const metadata = { duration: video.duration, width: video.videoWidth, height: video.videoHeight }
      clearTimeout(timer)
      signal?.removeEventListener('abort', abort)
      video.onloadeddata = video.onerror = null
      video.removeAttribute('src')
      video.load()
      if (error) reject(error)
      else resolve(metadata)
    }
    const abort = () => finish(new DOMException('播放校验已取消', 'AbortError'))
    const timer = window.setTimeout(() => finish(new Error('视频加载超时，请确认地址可公开访问、未过期，再重试')), 15000)
    video.preload = 'auto'
    video.muted = true
    video.playsInline = true
    video.onloadeddata = () => {
      if (!Number.isFinite(video.duration) || video.duration <= 0 || video.videoWidth <= 0 || video.videoHeight <= 0) finish(new Error('无法读取有效的视频画面或时长'))
      else finish()
    }
    video.onerror = () => {
      const messages: Record<number, string> = {
        2: '视频网络加载失败，请检查地址、访问权限或防盗链设置',
        3: '视频解码失败，请使用 H.264 视频和 AAC 音频编码的 MP4 文件',
        4: '地址不是可播放的视频，或视频格式、访问权限不受支持',
      }
      finish(new Error(messages[video.error?.code || 0] || '视频无法播放，请更换地址或重新上传'))
    }
    signal?.addEventListener('abort', abort, { once: true })
    if (signal?.aborted) { abort(); return }
    video.src = source
    video.load()
  })
}

export function videoDuration(seconds: number): string {
  const total = Math.floor(seconds)
  return `${Math.floor(total / 60).toString().padStart(2, '0')}:${(total % 60).toString().padStart(2, '0')}`
}
