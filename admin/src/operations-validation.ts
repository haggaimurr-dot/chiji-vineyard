import { validateService } from './service-validation'
import { validateLiveConfig } from './live-validation'
import type { Article, HomeConfig, PrivacyPolicy, Product } from './types'

function validDate(value: string) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const date = new Date(`${value}T00:00:00Z`)
  return Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value
}
export function validateHomePreview(home: HomeConfig, products: Product[], articles: Article[]) {
  if (!home || !Array.isArray(home.banners) || home.banners.length < 1 || home.banners.length > 5 || home.productIds.length > 12 || home.articleIds.length > 6) throw new Error('首页需要 1–5 个轮播位，最多 12 件商品和 6 篇文章')
  validateService(home.customerService)
  validateLiveConfig(home.live)
  const hasProduct = (id: string) => products.some(p => p.id === id && p.status === 'PUBLISHED' && p.skus.some(s => s.active !== false))
  const hasArticle = (id: string) => articles.some(a => a.id === id && a.status === 'PUBLISHED')
  for (const banner of home.banners) {
    if (!banner.title.trim() || [...banner.title].length > 80 || !banner.button.trim()) throw new Error('运营位标题和按钮不能为空，标题最多 80 字')
    if (banner.image && !banner.image.startsWith('https://')) throw new Error('轮播图片必须使用 HTTPS 地址')
    if (!['shop', 'classroom', 'cooperation', 'product', 'article'].includes(banner.target)) throw new Error('运营位跳转类型无效')
    if (banner.target === 'product' && !hasProduct(banner.targetId) || banner.target === 'article' && !hasArticle(banner.targetId)) throw new Error('运营位目标不存在或未发布')
  }
  for (const [ids, exists] of [[home.productIds, hasProduct], [home.articleIds, hasArticle]] as const) {
    if (new Set(ids).size !== ids.length || ids.some(id => !id || !exists(id))) throw new Error('推荐内容不能重复、为空或未发布')
  }
  const text = (v: string, max: number) => !!v?.trim() && [...v].length <= max
  const range = (start: string, end: string) => validDate(start) && validDate(end) && start <= end && Number(end.slice(0, 4)) - Number(start.slice(0, 4)) <= 5
  if ((home.activities?.length || 0) > 12 || (home.announcements?.length || 0) > 20 || (home.launchCalendar?.length || 0) > 36) throw new Error('活动最多 12 条、公告最多 20 条、上市安排最多 36 条')
  const activityIDs = new Set<string>()
  for (const a of home.activities || []) {
    if ((a.cover?.length || 0) > 2000 || a.cover && !a.cover.startsWith('https://') || Array.from(a.location || '').length > 200) throw new Error('活动封面需为 HTTPS 地址，地点最多 200 字')
    if (a.reservationEnabled && !a.id || a.id && (!/^[a-zA-Z0-9_-]{1,80}$/.test(a.id) || activityIDs.has(a.id))) throw new Error('预约活动需要唯一有效标识')
    if (a.capacity !== undefined && (!Number.isInteger(a.capacity) || a.capacity < 0 || a.capacity > 100000)) throw new Error('活动容量须为0–100000的整数，0表示不限额')
    if (a.waitlistEnabled !== undefined && typeof a.waitlistEnabled !== 'boolean') throw new Error('候补开关无效')
    if (a.id) activityIDs.add(a.id)
  }
  if (home.activities?.some(a => !text(a.title, 80) || !text(a.summary, 200) || !text(a.content, 2000) || !range(a.startDate, a.endDate) || a.productId && !hasProduct(a.productId))) throw new Error('活动内容、日期或关联商品无效')
  if (home.announcements?.some(a => !text(a.title, 80) || !text(a.content, 2000) || !validDate(a.publishedAt))) throw new Error('公告标题、正文或发布日期无效')
  if (home.launchCalendar?.some(a => !text(a.variety, 80) || [...a.note].length > 200 || !range(a.startDate, a.endDate) || a.productId && !hasProduct(a.productId))) throw new Error('上市品种、日期或关联商品无效')
}
export function validatePrivacyPreview(privacy: PrivacyPolicy) {
  if (!privacy?.title.trim() || !privacy.version.trim() || privacy.content.length < 10 || privacy.content.some(x => !x.trim())) throw new Error('政策标题、版本及完整正文不能为空')
  if (!validDate(privacy.effectiveDate)) throw new Error('生效日期无效')
  if (privacy.published && (!privacy.operator.trim() || !privacy.contact.trim() || !privacy.address.trim())) throw new Error('发布前请填写主体、联系方式和地址')
}
