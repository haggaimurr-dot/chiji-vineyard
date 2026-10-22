import type { Article, ContentResource } from './types'

export const reviewLabels: Record<string, string> = { UNSUBMITTED: '待提交', PENDING: '待审核', APPROVED: '已通过', REJECTED: '已退回' }
export function sameReviewContent(a: Article, b: Article) {
  return (['title', 'summary', 'cover', 'variety', 'stage', 'type', 'videoUrl', 'contentHtml', 'content', 'tags'] as const).every(key => JSON.stringify(a[key] ?? (key === 'content' || key === 'tags' ? [] : '')) === JSON.stringify(b[key] ?? (key === 'content' || key === 'tags' ? [] : '')))
}
export function validateResource(resource: ContentResource, demo = false) {
  if (!resource.name.trim() || resource.name.trim().length > 80 || (resource.note || '').length > 500) throw new Error('名称必填且最多80字，备注最多500字')
  if (!['tag', 'image', 'video'].includes(resource.kind)) throw new Error('素材类型无效')
  if (resource.kind !== 'tag') {
    const source = resource.url || ''
    if (demo && resource.kind === 'image' && /^data:image\/(jpeg|png|webp);base64,/.test(source)) return
    let url: URL
    try { url = new URL(source) } catch { throw new Error('素材地址必须是有效 HTTPS 地址') }
    if (url.protocol !== 'https:' || !url.hostname || url.username || url.password) throw new Error('素材地址必须是有效 HTTPS 地址')
  }
}
