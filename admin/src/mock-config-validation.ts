import { validateShippingDetails } from '../../miniprogram/utils/shipping'
import { validateService } from './service-validation'
import { validateLiveConfig } from './live-validation'
import type { Article, Product } from './types'

function validDate(value: string) {
  return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(value) && Number.isFinite(Date.parse(value)) && new Date(value).toISOString().slice(0, 10) === value
}
const text = (value: unknown, max: number) => typeof value === 'string' && value.trim() !== '' && [...value].length <= max
const range = (start: string, end: string) => validDate(start) && validDate(end) && start <= end && Number(end.slice(0, 4)) - Number(start.slice(0, 4)) <= 5
const pick = (value: any, fields: string[]) => Object.fromEntries(fields.filter(field => field in value).map(field => [field, value[field]]))

export function validateMockConfig(key: string, payload: any, products: Product[], articles: Article[]) {
  if (!payload || typeof payload !== 'object') throw new Error('配置内容无效')
  if (key === 'shipping') {
    if (!Number.isSafeInteger(payload.freight) || payload.freight < 0 || payload.freight > 10000000 || !Number.isSafeInteger(payload.freeShippingThreshold) || payload.freeShippingThreshold < 0 || payload.freeShippingThreshold > 10000000 || !Array.isArray(payload.blockedProvinces) || payload.blockedProvinces.length > 50 || new Set(payload.blockedProvinces).size !== payload.blockedProvinces.length || payload.blockedProvinces.some((item: unknown) => !text(item, 30))) throw new Error('运费配置无效')
    validateShippingDetails(payload)
    return pick(payload, ['freight', 'freeShippingThreshold', 'blockedProvinces', 'regions', 'blockedDates'])
  }
  if (key === 'coupons') {
    if (!Array.isArray(payload.items) || payload.items.length > 100) throw new Error('最多配置100张优惠券')
    const seen = new Set<string>()
    for (const c of payload.items) {
      c.code = String(c.code ?? '').trim().toUpperCase()
      if (!/^[A-Z0-9_-]{1,40}$/.test(c.code) || seen.has(c.code) || !text(c.name, 60) || !Number.isInteger(c.minimum) || c.minimum < 0 || c.minimum > 10000000 || !Number.isInteger(c.amount) || c.amount < 1 || c.amount > 10000000 || !validDate(c.startDate) || !validDate(c.endDate) || c.startDate > c.endDate) throw new Error('优惠券编码、金额或有效期无效（编码不可重复）')
      c.name = c.name.trim(); seen.add(c.code)
    }
    return { items: payload.items.map((item: any) => pick(item, ['code', 'name', 'minimum', 'amount', 'startDate', 'endDate', 'active'])) }
  }
  if (key === 'privacy') {
    if (!text(payload.title, Number.MAX_SAFE_INTEGER) || !text(payload.version, Number.MAX_SAFE_INTEGER) || !Array.isArray(payload.content) || payload.content.length < 10 || payload.content.some((section: unknown) => !text(section, Number.MAX_SAFE_INTEGER))) throw new Error('政策标题、版本及完整正文不能为空')
    if (!validDate(payload.effectiveDate)) throw new Error('生效日期无效')
    if (payload.published && (!payload.operator?.trim() || !payload.contact?.trim() || !payload.address?.trim())) throw new Error('发布前请填写主体、联系方式和地址')
    return pick(payload, ['title', 'version', 'effectiveDate', 'operator', 'contact', 'address', 'published', 'content'])
  }
  if (key !== 'home') throw new Error('配置类型无效')
  validateService(payload.customerService)
  validateLiveConfig(payload.live)
  const h = payload
  if (!Array.isArray(h.banners) || h.banners.length < 1 || h.banners.length > 5 || !Array.isArray(h.productIds) || h.productIds.length > 12 || !Array.isArray(h.articleIds) || h.articleIds.length > 6) throw new Error('首页需要1–5个轮播位，最多12件商品和6篇文章')
  const availableProduct = (id: string) => products.some(p => p.id === id && p.status === 'PUBLISHED' && p.skus.some(s => s.active !== false))
  const availableArticle = (id: string) => articles.some(a => a.id === id && a.status === 'PUBLISHED')
  for (const b of h.banners) {
    // Demo uploads stay local; these formats are never accepted by the formal backend.
    const demoImage = /^\/demo-products\/(shine|kyoho|gift)\.jpg$/.test(b.image) || b.image === '/assets/home/harvest-scene.jpg' || /^data:image\/(jpeg|png|webp);base64,[A-Za-z0-9+/]+={0,2}$/.test(b.image)
    if (!text(b.title, 80) || !text(b.button, Number.MAX_SAFE_INTEGER) || (b.image && !b.image.startsWith('https://') && !demoImage)) throw new Error('运营位标题、按钮或图片地址无效')
    if (!['shop', 'classroom', 'cooperation', 'product', 'article'].includes(b.target)) throw new Error('运营位跳转类型无效')
    if ((b.target === 'product' && !availableProduct(b.targetId)) || (b.target === 'article' && !availableArticle(b.targetId))) throw new Error('运营位目标不存在或未发布')
  }
  for (const [ids, available] of [[h.productIds, availableProduct], [h.articleIds, availableArticle]] as Array<[string[], (id: string) => boolean]>) {
    if (new Set(ids).size !== ids.length || ids.some(id => !id)) throw new Error('推荐内容不能重复或为空')
    if (ids.some(id => !available(id))) throw new Error('推荐内容不存在或未发布')
  }
  if ((h.activities || []).length > 12 || (h.announcements || []).length > 20 || (h.launchCalendar || []).length > 36) throw new Error('活动最多12条、公告最多20条、上市安排最多36条')
  const activityIDs = new Set<string>()
  for (const a of h.activities || []) {
    if ((a.cover?.length || 0) > 2000 || a.cover && !a.cover.startsWith('https://') || Array.from(a.location || '').length > 200) throw new Error('活动封面需为 HTTPS 地址，地点最多 200 字')
    if (a.reservationEnabled && !a.id || a.id && (!/^[a-zA-Z0-9_-]{1,80}$/.test(a.id) || activityIDs.has(a.id))) throw new Error('预约活动需要唯一有效标识')
    if (a.capacity !== undefined && (!Number.isInteger(a.capacity) || a.capacity < 0 || a.capacity > 100000)) throw new Error('活动容量须为0–100000的整数，0表示不限额')
    if (a.waitlistEnabled !== undefined && typeof a.waitlistEnabled !== 'boolean') throw new Error('候补开关无效')
    if (a.id) activityIDs.add(a.id)
  }
  for (const a of h.activities || []) if (!text(a.title, 80) || !text(a.summary, 200) || !text(a.content, 2000) || !range(a.startDate, a.endDate)) throw new Error('活动标题、简介、正文或起止日期无效')
  for (const a of h.announcements || []) if (!text(a.title, 80) || !text(a.content, 2000) || !validDate(a.publishedAt)) throw new Error('公告标题、正文或发布日期无效')
  for (const a of h.launchCalendar || []) if (!text(a.variety, 80) || [...(a.note || '')].length > 200 || !range(a.startDate, a.endDate)) throw new Error('上市品种、说明或起止日期无效')
  for (const a of [...(h.activities || []), ...(h.launchCalendar || [])]) if (a.productId && !availableProduct(a.productId)) throw new Error('关联商品不存在或未上架')
  return pick(payload, ['live', 'customerService', 'activities', 'announcements', 'launchCalendar', 'banners', 'productIds', 'articleIds', 'storyTitle', 'storyCopy', 'cooperationTitle', 'cooperationCopy'])
}
