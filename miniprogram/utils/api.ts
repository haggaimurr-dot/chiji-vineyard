import { demoClassroom, demoCompleted } from './classroom'
import { clearPersonalStorage, closedKey } from './personal-data'
import { demoCoupons, demoWallet, demoWalletKey, bestCoupon } from './coupons'
import { demoReturnChange, validateAttachments, validateEvidenceBudget, ticketDeadline, type AfterSalesTicket } from './after-sales'
import { shippingFee, type ShippingRule } from './shipping'
import { chinaDate, deliveryStart } from './checkout'
import { canApplyRefund } from './refund'
import { validateDelivery, validateInvoice, checkoutIntent } from './checkout'
import { demoReviews } from '../data/product-content'
import type { Review } from './types'
import { demoHome } from '../data/home'
import { demoReservationKey } from './home-features'
import { demoActivityNoticeKey, type ActivityNotice } from './activity-notice'
import { mockPrivacy, mockService } from '../data/service'
import { demoArticles, demoProducts } from '../data/demo'
import { getDemoAddresses, changeDemoAddress } from './address'
import { available, filterProducts, sortProducts } from './product'
import { matchesShopFilters, validateShopFilters } from './shop-search'
import { recordDemoArticleRead, withDemoArticleStats, searchDemoArticles, setDemoArticleFavorite } from './articles'
import { matchesOrder, orderFilters } from './order-filter'
import type { Address, ApiResponse, CartItem, Order, PriceQuote, Refund } from './types'

type ApiData = Record<string, any>

const CART_KEY = 'chiji_demo_cart'
const ORDER_KEY = 'chiji_demo_orders'
const TOKEN_KEY = 'chiji_mini_token'
const ATTENTION_KEY = 'chiji_demo_attention'
const demoAttention = () => ({ favorite: [], restock: [], pricedrop: [], priceDrops: {}, history: [], restockNotified: {}, ...(wx.getStorageSync(ATTENTION_KEY) || {}) })
const withAttention = (product: typeof demoProducts[number]) => {
  const flags = demoAttention()
  const reminder = flags.priceDrops[product.id]
  const sku = product.skus.find(s => s.id === reminder?.skuId)
  const reached = !!sku && (reminder?.targetPrice > 0 ? sku.price <= reminder.targetPrice : sku.price < reminder?.baselinePrice)
  const ready = !!sku && sku.stock > 0 && sku.limitPerOrder > 0
  const kind = flags.pricedrop.includes(product.id) && reached && ready && !reminder.notified ? 'pricedrop' : flags.restock.includes(product.id) && available(product) && !flags.restockNotified[product.id] ? 'restock' : ''
  if (kind) {
    flags.history.unshift({ id: `demo-${Date.now()}-${Math.random().toString(36).slice(2)}`, productId: product.id, productName: product.name, kind, skuName: kind === 'pricedrop' ? sku!.name : '', currentPrice: kind === 'pricedrop' ? sku!.price : 0, targetPrice: kind === 'pricedrop' ? reminder.targetPrice || 0 : 0, status: 'IN_APP', detail: '演示：小程序内检测到提醒条件满足', createdAt: new Date().toISOString() })
    if (kind === 'pricedrop') reminder.notified = true
    else flags.restockNotified[product.id] = true
    wx.setStorageSync(ATTENTION_KEY, flags)
  }
  return { ...product, favorite: flags.favorite.includes(product.id), restockReminder: flags.restock.includes(product.id), priceDropReminder: flags.pricedrop.includes(product.id), priceDrop: reminder ? { ...reminder, skuName: sku?.name || '', currentPrice: sku?.price || 0, dropped: reached, available: !!sku && sku.stock > 0 && sku.limitPerOrder > 0, pushStatus: 'NONE' } : undefined }
}

const getCart = (): CartItem[] => wx.getStorageSync(CART_KEY) || []
const setCart = (cart: CartItem[]) => wx.setStorageSync(CART_KEY, cart)
const getOrders = (): Order[] => wx.getStorageSync(ORDER_KEY) || []
const setOrders = (orders: Order[]) => wx.setStorageSync(ORDER_KEY, orders)

const hydrateCart = (items: CartItem[], includeInvalid = false) => items.map((item) => {
  const product = demoProducts.find((entry) => entry.id === item.productId)
  const sku = product?.skus.find((entry) => entry.id === item.skuId)
  return { ...item, product, sku }
}).filter(item => includeInvalid || (item.product && item.sku))

export const demoShippingRule: ShippingRule = { freight:1200, freeShippingThreshold:19900, blockedProvinces:['新疆维吾尔自治区','西藏自治区','青海省'], regions:[], blockedDates:[] }
const quote = (items: CartItem[], address?: Partial<Address>, couponCode = '', deliveryDate = '', autoCoupon = false): PriceQuote => {
  validateDelivery(deliveryDate, hydrateCart(items))
  const subtotal = hydrateCart(items).reduce((sum, item) => sum + (item.sku?.price || 0) * item.quantity, 0)
  const fees = shippingFee(demoShippingRule, address || {}, subtotal, items.reduce((n,i) => n+i.quantity,0), deliveryDate)
  couponCode = couponCode.trim().toUpperCase()
  if (autoCoupon) couponCode = bestCoupon(demoWallet(getOrders()), subtotal)
  let discount = 0
  if (couponCode) {
    const coupon = demoCoupons.find(c => c.code === couponCode)
    if (!coupon) throw new Error('优惠券不存在')
    if (!coupon.active || chinaDate(0) < coupon.startDate || chinaDate(0) > coupon.endDate) throw new Error('优惠券未生效或已过期')
    if (getOrders().some(o => o.quote.couponCode === couponCode && (!['CANCELLED', 'CLOSED'].includes(o.orderStatus) || o.paymentStatus !== 'UNPAID'))) throw new Error('此优惠券已使用或被待付款订单占用')
    if (subtotal < coupon.minimum) throw new Error('商品金额未达到优惠券使用门槛')
    discount = Math.max(0, Math.min(coupon.amount, subtotal - 1))
  }
  return {
    discount, couponCode, subtotal,
    ...fees, deliveryMin: deliveryStart(hydrateCart(items)), deliveryMax: chinaDate(30),
    total: subtotal + fees.freight - discount,
  }
}

function validateDemoReviewImages(images: string[]) {
  if (!Array.isArray(images) || images.length > 6) throw new Error('晒图最多6张')
  if (images.some(image => typeof image !== 'string' || !/^data:image\/(jpeg|png|webp);base64,[A-Za-z0-9+/=]+$/.test(image) || image.length > 700000)) throw new Error('图片无效或超过512KB')
}
async function saveDemoReviewImages(images: string[]): Promise<string[]> {
  validateDemoReviewImages(images)
  const paths: string[] = []
  for (const image of images) {
    const match = /^data:image\/(jpeg|png|webp);base64,(.+)$/.exec(image)!
    const filePath = `${wx.env.USER_DATA_PATH}/review-${Date.now()}-${Math.random().toString(36).slice(2)}.${match[1]}`
    await new Promise<void>((resolve, reject) => wx.getFileSystemManager().writeFile({ filePath, data: match[2], encoding: 'base64', success: () => resolve(), fail: reject }))
    paths.push(filePath)
  }
  return paths
}
// Mock has one local participant; occupancy lets acceptance fixtures model other users.
function refreshDemoActivityQueue() {
  const ids: string[] = wx.getStorageSync(demoReservationKey) || []
  const waiting: string[] = wx.getStorageSync('chiji_demo_activity_waitlist') || []
  const occupancy = wx.getStorageSync('chiji_demo_activity_occupancy') || {}
  const notices: ActivityNotice[] = wx.getStorageSync(demoActivityNoticeKey) || []
  const today = new Date(Date.now() + 8 * 3600000).toISOString().slice(0, 10)
  const promoted = waiting.filter(id => {
    const a = demoHome.activities?.find(a => a.id === id)
    if (!a?.reservationEnabled || today > a.endDate || a.capacity && (occupancy[id] || 0) >= a.capacity) return false
    if (!ids.includes(id)) ids.unshift(id)
    notices.unshift({ id: `an-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`, activityId: id, title: a.title, startDate: a.startDate, endDate: a.endDate, createdAt: new Date().toISOString(), read: false, kind: 'PROMOTED' })
    return true
  })
  if (promoted.length) {
    wx.setStorageSync(demoReservationKey, ids)
    wx.setStorageSync('chiji_demo_activity_waitlist', waiting.filter(id => !promoted.includes(id)))
    wx.setStorageSync(demoActivityNoticeKey, notices)
  }
}
function recordDemoCommerce(cart = false) {
  const now = new Date().toISOString(), date = new Date(Date.now() + 8 * 3600000).toISOString().slice(0, 10)
  const events = wx.getStorageSync('chiji_demo_commerce_activity') || []
  let event = events.find((e: any) => e.date === date)
  if (!event) { event = { date, buyerId: 'demo-user', visitedAt: now }; events.push(event) }
  if (cart && !event.cartAddedAt) event.cartAddedAt = now
  wx.setStorageSync('chiji_demo_commerce_activity', events)
  return { recorded: true }
}

async function demoCall<T>(action: string, data: ApiData): Promise<ApiResponse<T>> {
  await new Promise((resolve) => setTimeout(resolve, 120))
  let result: any
  if (action === 'product.restockSubscription.config' || action === 'product.priceDropSubscription.config' || action === 'activity.subscription.config') result = { enabled: false, templateId: '', status: 'NONE', demo: true }
  else if (action === 'analytics.visit') result = recordDemoCommerce()
  else if (action === 'bootstrap') result = { user: { id: 'demo-user', nickname: '葡萄好友' }, cartCount: getCart().reduce((n, x) => n + x.quantity, 0) }
  else if (action === 'home.get') result = demoHome
  else if (action === 'activity.reservation.list') result = wx.getStorageSync(demoReservationKey) || []
  else if (action === 'activity.reservation.status') {
    refreshDemoActivityQueue()
    const ids: string[] = wx.getStorageSync(demoReservationKey) || []
    const waiting: string[] = wx.getStorageSync('chiji_demo_activity_waitlist') || []
    const occupancy = wx.getStorageSync('chiji_demo_activity_occupancy') || {}
    result = { items: [...new Set([...(demoHome.activities || []).map(a => a.id!).filter(Boolean), ...ids, ...waiting])].map(id => ({ activityId: id, status: ids.includes(id) ? 'RESERVED' : waiting.includes(id) ? 'WAITLISTED' : 'NONE', confirmed: (occupancy[id] || 0) + Number(ids.includes(id)), waiting: Number(waiting.includes(id)), position: waiting.includes(id) ? 1 : 0 })) }
  }
  else if (action === 'activity.notice.list') {
    refreshDemoActivityQueue()
    const offset = data.offset ?? 0, limit = data.limit ?? 20
    if (!Number.isInteger(offset) || offset < 0 || offset > 1000000 || !Number.isInteger(limit) || limit < 1 || limit > 50) throw new Error('通知分页参数无效')
    const notices: ActivityNotice[] = wx.getStorageSync(demoActivityNoticeKey) || []
    result = { items: notices.slice(offset, offset + limit), unread: notices.filter(n => !n.read).length, hasMore: offset + limit < notices.length }
  }
  else if (action === 'activity.notice.read') {
    if (typeof data.id !== 'string' || !data.id || data.id.length > 80) throw new Error('通知标识无效')
    const notices: ActivityNotice[] = wx.getStorageSync(demoActivityNoticeKey) || []
    wx.setStorageSync(demoActivityNoticeKey, notices.map(n => n.id === data.id ? { ...n, read: true } : n))
    result = { ok: true }
  }
  else if (action === 'activity.reservation.set') {
    if (typeof data.activityId !== 'string' || !/^[a-zA-Z0-9_-]{1,80}$/.test(data.activityId) || typeof data.enabled !== 'boolean') throw new Error('预约参数无效')
    const item = demoHome.activities?.find(a => a.id === data.activityId && a.reservationEnabled)
    const today = new Date(Date.now() + 8 * 3600000).toISOString().slice(0,10)
    if (data.enabled && !item) throw new Error('活动已下线或未开放预约')
    if (data.enabled && today > item!.endDate) throw new Error('活动已结束，不能预约')
    const ids: string[] = wx.getStorageSync(demoReservationKey) || []
    const waiting: string[] = wx.getStorageSync('chiji_demo_activity_waitlist') || []
    const occupancy = wx.getStorageSync('chiji_demo_activity_occupancy') || {}
    const full = !!item?.capacity && (occupancy[data.activityId] || 0) >= item.capacity
    const wasActive = ids.includes(data.activityId) || waiting.includes(data.activityId)
    if (data.enabled && !wasActive && full && !item?.waitlistEnabled) throw new Error('活动名额已满，暂未开放候补')
    const status = !data.enabled ? 'NONE' : ids.includes(data.activityId) ? 'RESERVED' : full ? 'WAITLISTED' : 'RESERVED'
    const updated = ids.filter(id => id !== data.activityId)
    if (data.enabled && (!wasActive || waiting.includes(data.activityId) && status === 'RESERVED')) {
      const notices: ActivityNotice[] = wx.getStorageSync(demoActivityNoticeKey) || []
      notices.unshift({ kind: status === 'WAITLISTED' ? 'WAITLISTED' : waiting.includes(data.activityId) ? 'PROMOTED' : 'RESERVED', id: `an-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`, activityId: item!.id!, title: item!.title, startDate: item!.startDate, endDate: item!.endDate, createdAt: new Date().toISOString(), read: false })
      wx.setStorageSync(demoActivityNoticeKey, notices)
    }
    if (status === 'RESERVED') updated.unshift(data.activityId)
    wx.setStorageSync('chiji_demo_activity_waitlist', [...waiting.filter(id => id !== data.activityId), ...(status === 'WAITLISTED' ? [data.activityId] : [])])
    wx.setStorageSync(demoReservationKey, updated)
    result = { enabled: data.enabled, status }
  }
  else if (action === 'privacy.get') result = { ...mockPrivacy, content: [...mockPrivacy.content] }
  else if (action === 'service.get') result = { ...mockService }
  else if (action === 'order.events') {const o=getOrders().find(o=>o.id===data.orderId);if(!o)throw new Error('订单不存在');result=o.events || []}
  else if (action === 'order.tracking') {const order=getOrders().find(o=>o.id===data.orderId);if(!(data.packageId ? order?.packages?.find(p=>p.id===data.packageId)?.shipment : order?.shipment))throw new Error('订单尚未发货');if(data.trackingNo && ![order!.shipment!,...(order!.shipment!.parcels || [])].some(p=>p.trackingNo===data.trackingNo && (!data.carrierCode || (p as any).carrierCode===data.carrierCode)))throw new Error('运单不属于该订单');result={status:'DEMO',message:'以下为演示物流轨迹',updatedAt:new Date().toISOString(),events:[{time:new Date().toISOString(),context:'演示：承运商已揽收'}]}}
  else if (action === 'product.list') result = demoProducts.filter((p) => !data.category || data.category === 'all' || p.category === data.category).map(withAttention)
  else if (action === 'product.search') {
    validateShopFilters(data)
    if (data.category && !['all', 'family', 'gift'].includes(data.category)) throw new Error('商品分类无效')
    if (data.sort && !['default', 'priceAsc', 'priceDesc', 'sales'].includes(data.sort)) throw new Error('商品排序无效')
    if ((data.keyword || '').trim().length > 60) throw new Error('搜索关键词最多60字')
    const offset = Math.max(0, Math.trunc(data.offset || 0)), limit = Math.min(50, Math.max(1, Math.trunc(data.limit || 20)))
    const items = sortProducts(filterProducts(demoProducts, data.category || 'all', data.keyword || '').filter(p => matchesShopFilters(p, data)), data.sort || 'default')
    result = { items: items.slice(offset, offset + limit).map(withAttention), total: items.length, offset, limit }
  }
  else if (action === 'trace.query') {const code=String(data.code||'').trim();const p=demoProducts.find(p=>p.traceability?.code===code);if(!p?.traceability)throw new Error('未找到该溯源码，请核对包装上的溯源码');const b=p.traceability;result={code,number:b.batch,orchard:b.orchard,harvestedAt:b.harvestedAt,packedAt:b.packedAt,expiresAt:'',status:'ACTIVE',demo:true}}
  else if (action === 'product.detail') { const p = demoProducts.find(p => p.id === data.id); result = p ? withAttention(p) : null }
  else if (action === 'product.attention.history') {
    const offset = data.offset ?? 0, limit = data.limit ?? 20
    if (!Number.isInteger(offset) || offset < 0 || offset > 1000000 || !Number.isInteger(limit) || limit < 1 || limit > 50) throw new Error('历史分页参数无效')
    demoProducts.forEach(withAttention)
    const history = demoAttention().history
    result = { items: history.slice(offset, offset + limit), hasMore: offset + limit < history.length, offset }
  }
  else if (action === 'product.priceDropTarget.set') {
    const flags = demoAttention(), reminder = flags.priceDrops[data.productId]
    if (!flags.pricedrop.includes(data.productId) || !reminder) throw new Error('请先设置降价提醒')
    if (!Number.isInteger(data.targetPrice) || data.targetPrice < 0 || data.targetPrice > 100000000 || data.targetPrice > reminder.baselinePrice) throw new Error('目标价格无效或高于关注价')
    reminder.targetPrice = data.targetPrice
    wx.setStorageSync(ATTENTION_KEY, flags)
    result = { targetPrice: data.targetPrice }
  }
  else if (action === 'product.attention.list') {
    if (!['favorite', 'restock', 'pricedrop'].includes(data.kind)) throw new Error('关注类型无效')
    result = demoAttention()[data.kind].map((id: string) => demoProducts.find(p => p.id === id)).filter(Boolean).map(withAttention)
  }
  else if (action === 'product.attention.set') {
    if (!['favorite', 'restock', 'pricedrop'].includes(data.kind) || typeof data.enabled !== 'boolean') throw new Error('关注参数无效')
    const p = demoProducts.find(p => p.id === data.productId)
    if (data.enabled && !p) throw new Error('商品不存在或已下架')
    if (data.enabled && data.kind === 'restock' && available(p!)) throw new Error('商品已有货，可直接购买')
    const flags = demoAttention()
    if (data.kind === 'pricedrop') {
      if (data.enabled) {
        const sku = p!.skus.find(s => s.id === data.skuId)
        if (!sku || sku.price <= 0) throw new Error('商品规格不存在或暂不支持降价提醒')
        if (data.targetPrice !== undefined && (!Number.isInteger(data.targetPrice) || data.targetPrice <= 0 || data.targetPrice > 100000000 || data.targetPrice > sku.price)) throw new Error('目标价格无效或高于当前规格价')
        if (!flags.priceDrops[p!.id]) flags.priceDrops[p!.id] = { skuId: sku.id, baselinePrice: sku.price, targetPrice: data.targetPrice || 0 }
      } else { delete flags.priceDrops[data.productId] }
    }
    if (data.kind === 'restock' && !data.enabled) delete flags.restockNotified[data.productId]
    flags[data.kind] = flags[data.kind].filter((id: string) => id !== data.productId)
    if (data.enabled) flags[data.kind].unshift(data.productId)
    wx.setStorageSync(ATTENTION_KEY, flags)
    result = { enabled: data.enabled }
  }
  else if (action === 'product.reviews') {
    const page = data.page === 0 ? 1 : data.page ?? 1, pageSize = data.pageSize === 0 ? 20 : data.pageSize ?? 20
    if (!data.productId || !Number.isInteger(page) || page < 1 || page > 1000000 || !Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100) throw new Error('评价分页参数无效')
    if (!demoProducts.some(p => p.id === data.productId)) throw new Error('商品不存在或已下架')
    const stored: Array<Review & { orderId: string }> = wx.getStorageSync('chiji_demo_reviews') || []
    const items = [...stored, ...demoReviews].filter(r => r.productId === data.productId && (!r.status || r.status === 'APPROVED')).sort((a, b) => b.createdAt.localeCompare(a.createdAt) || b.id.localeCompare(a.id))
    const offset = (page - 1) * pageSize
    result = { total: items.length, average: items.length ? items.reduce((n, r) => n + r.rating, 0) / items.length : 0, items: items.slice(offset, offset + pageSize), page, pageSize, hasMore: offset + pageSize < items.length }
  } else if (action === 'review.order') {
    result = (wx.getStorageSync('chiji_demo_reviews') || []).filter((r: any) => r.orderId === data.orderId)
  } else if (action === 'review.append') {
    const reviews: Review[] = wx.getStorageSync('chiji_demo_reviews') || []
    const review = reviews.find(r => r.id === data.id)
    if (!review) throw new Error('评价不存在')
    if (review.appendAt) throw new Error('每条评价仅可追评一次')
    const content = String(data.content || '').trim()
    if (!content || Array.from(content).length > 500) throw new Error('请填写1–500字内容')
    validateDemoReviewImages(data.images || [])
    Object.assign(review, { appendContent: content, appendImages: await saveDemoReviewImages(data.images || []), appendAt: new Date().toISOString(), status: 'PENDING', rejectReason: '', moderatedAt: '' })
    wx.setStorageSync('chiji_demo_reviews', reviews); result = review
  } else if (action === 'review.create') {
    const order = getOrders().find(o => o.id === data.orderId)
    if (!order || order.orderStatus !== 'COMPLETED' || order.fulfillmentStatus !== 'RECEIVED' || order.paymentStatus !== 'PAID') throw new Error('确认收货后才能评价')
    if (!order.items.some(i => i.productId === data.productId)) throw new Error('该商品不在此订单中')
    const content = String(data.content || '').trim()
    if (!Number.isInteger(data.rating) || data.rating < 1 || data.rating > 5 || !content || Array.from(content).length > 500) throw new Error('请填写 1–5 星评分和 1–500 字评价')
    const reviews: Array<Review & { orderId: string }> = wx.getStorageSync('chiji_demo_reviews') || []
    if (reviews.some(r => r.orderId === data.orderId && r.productId === data.productId)) throw new Error('此订单中的商品已评价')
    validateDemoReviewImages(data.images || [])
    result = { images: await saveDemoReviewImages(data.images || []), id: `review-${Date.now()}`, orderId: data.orderId, productId: data.productId, rating: data.rating, content, createdAt: new Date().toISOString(), status: 'PENDING', demo: true }
    wx.setStorageSync('chiji_demo_reviews', [result, ...reviews])
  }
  else if (action === 'article.list') result = demoArticles.filter((a) => (!a.status || a.status === 'PUBLISHED') && (!data.stage || data.stage === '全部' || a.stage === data.stage) && (!data.variety || data.variety === '全部' || a.variety === data.variety)).map(withDemoArticleStats)
  else if (action === 'article.detail') { const article = demoArticles.find((a) => a.id === data.id && (!a.status || a.status === 'PUBLISHED')); result = article ? { ...withDemoArticleStats(article), completed: demoCompleted(article.id) } : null }
  else if (action === 'article.read') result = recordDemoArticleRead(demoArticles.find((a) => a.id === data.articleId), data.eventId)
  else if (action.startsWith('classroom.')) result = demoClassroom(action, data, demoArticles)
  else if (action === 'article.search') result = searchDemoArticles(demoArticles, data)
  else if (action === 'article.favorite.set') result = setDemoArticleFavorite(demoArticles.find(a => a.id === data.articleId), data.enabled)
  else if (action === 'address.get') { const list = getDemoAddresses(); result = list.find(a => a.isDefault) || list[0] || null }
  else if (action === 'address.list') result = getDemoAddresses()
  else if (['address.save', 'address.default', 'address.delete'].includes(action)) result = changeDemoAddress(action, data.address, data.id)
  else if (action === 'cart.get') result = hydrateCart(getCart(), true)
  else if (action === 'cart.batch') {
    if (!['delete', 'favorite', 'select'].includes(data.operation)) throw new Error('购物袋批量操作类型无效')
    const ids: unknown[] = data.skuIds
    if (!Array.isArray(ids) || !ids.length || ids.length > 100) throw new Error('每次请选择1–100项购物袋商品')
    if (ids.some(id => typeof id !== 'string' || !id.trim() || Array.from(id).length > 80) || new Set(ids).size !== ids.length) throw new Error('购物袋规格编号无效或重复')
    if (data.operation === 'select' && typeof data.selected !== 'boolean') throw new Error('请指定购物袋勾选状态')
    const cart = getCart(), picked = cart.filter(item => ids.includes(item.skuId))
    if (data.operation === 'favorite') {
      const productIds = [...new Set(picked.map(item => item.productId))]
      if (productIds.some(id => !demoProducts.some(p => p.id === id))) throw new Error('商品不存在或已下架，未移入收藏')
      const flags = demoAttention()
      flags.favorite = [...new Set([...flags.favorite, ...productIds])]
      wx.setStorageSync(ATTENTION_KEY, flags)
    }
    const next = data.operation === 'select'
      ? cart.map(item => ids.includes(item.skuId) ? { ...item, selected: data.selected } : item)
      : cart.filter(item => !ids.includes(item.skuId))
    setCart(next)
    result = hydrateCart(next, true)
  }
  else if (action === 'cart.update') {
    const cart = getCart()
    const index = cart.findIndex((x) => x.productId === data.productId && x.skuId === data.skuId)
    if (data.quantity <= 0 && index >= 0) cart.splice(index, 1)
    else if (index >= 0) cart[index] = { ...cart[index], quantity: data.quantity, selected: data.selected ?? cart[index].selected }
    else cart.push({ productId: data.productId, skuId: data.skuId, quantity: data.quantity, selected: true })
    setCart(cart)
    result = hydrateCart(cart, true)
    if (data.addToCart && data.quantity > 0) recordDemoCommerce(true)
  } else if (action === 'coupon.wallet') result = demoWallet(getOrders())
  else if (action === 'coupon.redeem') {
    const code = String(data.code || '').trim().toUpperCase()
    const coupon = demoCoupons.find(c => c.code === code)
    if (!coupon) throw new Error('优惠券不存在')
    if (!coupon.active || chinaDate(0) > coupon.endDate) throw new Error('优惠券已停用或过期')
    const codes: string[] = wx.getStorageSync(demoWalletKey) || []
    wx.setStorageSync(demoWalletKey, [...new Set([...codes, code])])
    result = { code }
  }
  else if (action === 'order.quote') result = quote(data.items, data.address, data.couponCode, data.deliveryDate, data.autoCoupon)
  else if (action === 'order.recover') {
    if (!/^[A-Za-z0-9_-]{16,80}$/.test(data.idempotencyKey || '')) throw new Error('下单幂等键无效')
    result = (getOrders() as any[]).find(o => o.idempotencyKey === data.idempotencyKey) || null
  }
  else if (action === 'order.create') {
    if (!/^[A-Za-z0-9_-]{16,80}$/.test(data.idempotencyKey || '')) throw new Error('下单幂等键无效')
    const fingerprint = checkoutIntent(data as any)
    const existing = (getOrders() as any[]).find(o => o.idempotencyKey === data.idempotencyKey)
    if (existing) {
      if (existing.requestFingerprint !== fingerprint) throw new Error('下单幂等键已用于其他结算内容，请重新发起结算')
      return { ok:true, data:existing } as ApiResponse<T>
    }
    const items = hydrateCart(data.items)
    validateDelivery(data.deliveryDate || '', items)
    if (!items.length || items.some(i => !Number.isInteger(i.quantity) || i.quantity < 1 || i.quantity > i.sku!.stock || i.quantity > i.sku!.limitPerOrder)) throw new Error('商品库存或限购数量不足')
    const pricing = quote(items, data.address, data.couponCode, data.deliveryDate)
    if (pricing.blockedReason) throw new Error(pricing.blockedReason)
    const order: Order = {
      id: `demo-${Date.now()}`, orderNo: `CJ${Date.now()}`, createdAt: new Date().toISOString(),
      orderStatus: 'PENDING_PAYMENT', paymentStatus: 'UNPAID', fulfillmentStatus: 'UNFULFILLED', refundStatus: 'NONE',
      events: [{id:'OE_'+Date.now(),kind:'ORDER_CREATED',detail:'订单已创建',actor:'用户',createdAt:new Date().toISOString()}], items, address: data.address, quote: pricing, note: data.note, deliveryDate: data.deliveryDate || '',
    }
    setOrders([{ ...order, idempotencyKey: data.idempotencyKey, requestFingerprint: fingerprint } as Order, ...getOrders()])
    setCart(getCart().filter((entry) => !data.items.some((picked: CartItem) => picked.productId === entry.productId && picked.skuId === entry.skuId)))
    result = order
  } else if (action === 'payment.create') {
    const orders = getOrders()
    const order = orders.find((x) => x.id === data.orderId)
    if (!order) throw new Error('订单不存在')
    order.orderStatus = 'CONFIRMED'; order.paymentStatus = 'PAID'; order.fulfillmentStatus = 'PREPARING'
    setOrders(orders); result = { demo: true, order }
  } else if (action === 'ticket.list') {
    const offset = Number(data.offset || 0)
    if (!Number.isInteger(offset) || offset < 0) throw new Error('分页参数无效')
    result = (wx.getStorageSync('chiji_demo_tickets') || []).filter((t: AfterSalesTicket) => (!data.orderId || t.orderId === data.orderId) && (!data.status || t.status === data.status)).slice(offset, offset + 50)
  } else if (action === 'ticket.create') {
    const attachments = validateAttachments(data.attachments || [])
    const order = getOrders().find(o => o.id === data.orderId), content = String(data.content || '').trim()
    if (!order) throw new Error('订单不存在')
    if (!['PAID','REFUNDING','REFUNDED'].includes(order.paymentStatus)) throw new Error('请支付订单后提交售后工单')
    if (!['QUALITY','DELIVERY','INVOICE','OTHER'].includes(data.category) || !content || [...content].length > 1000) throw new Error('请选择问题类型并填写描述，最多1000字')
    const tickets: AfterSalesTicket[] = wx.getStorageSync('chiji_demo_tickets') || []
    if (tickets.some(t => t.orderId === order.id && t.status !== 'CLOSED')) throw new Error('此订单已有未关闭工单，请在原工单补充说明')
    const ticket: AfterSalesTicket = {id:'AS_demo_' + Date.now(),orderId:order.id,orderNo:order.orderNo,category:data.category,status:'OPEN',version:1,createdAt:new Date().toISOString(),updatedAt:new Date().toISOString(),dueAt:new Date(Date.now()+48*3600000).toISOString(),userRead:1,adminRead:0,messages:[{role:'USER',content,createdAt:new Date().toISOString(),attachments}]}
    wx.setStorageSync('chiji_demo_tickets',[ticket,...tickets]); result=ticket
  } else if (action === 'ticket.read') {
    const tickets: AfterSalesTicket[] = wx.getStorageSync("chiji_demo_tickets") || []
    const t = tickets.find(t => t.id === data.id)
    if (!t) throw new Error('工单不存在')
    if (t.version !== data.version) throw new Error('工单已更新，请刷新后重试')
    if (!Number.isInteger(data.readThrough) || data.readThrough < 0 || data.readThrough > t.messages.length) throw new Error('已读参数无效')
    t.userRead = Math.max(t.userRead || 0, data.readThrough)
    wx.setStorageSync("chiji_demo_tickets",tickets); result = t
  }
  else if (action === 'ticket.returnTracking') {
    const tickets:AfterSalesTicket[]=wx.getStorageSync('chiji_demo_tickets') || []
    const t=tickets.find(t=>t.id===data.id)
    if(!t?.return?.shipment) throw new Error('退货尚未寄出')
    result={status:'UNAVAILABLE',message:'演示模式不查询真实物流，可复制运单号查询',events:[]}
  }
  else if (action === 'ticket.reply') {
    const attachments = validateAttachments(data.attachments || [])
    const tickets: AfterSalesTicket[] = wx.getStorageSync('chiji_demo_tickets') || [], t = tickets.find(t => t.id === data.id), content=String(data.content || '').trim()
    if (!t) throw new Error('工单不存在')
    if (t.version !== data.version) throw new Error('工单已更新，请刷新后重试')
    if (t.status === 'CLOSED') throw new Error('工单已关闭，如有新问题请新建工单')
    if ((!content && !data.return) || [...content].length > 1000 || data.status || data.owner !== undefined || data.dueAt !== undefined) throw new Error('仅可补充问题说明，最多1000字')
    validateEvidenceBudget(t,attachments)
    if (t.messages.length >= 200) throw new Error('工单记录已达上限，请联系商家处理')
    if(data.return) {
      if(content || attachments.length) throw new Error('退货操作请单独提交')
      t.return=demoReturnChange(t,data.return,false)
      t.messages.push({role:'USER',content:'退货已寄出：'+t.return.shipment!.carrier+' / '+t.return.shipment!.trackingNo,createdAt:new Date().toISOString()})
    }
    if (t.status === 'RESOLVED') { t.status = 'PROCESSING'; t.dueAt = new Date(Date.now()+48*3600000).toISOString() }
    if(content) t.messages.push({role:'USER',content,createdAt:new Date().toISOString(),attachments}); t.userRead=t.messages.length; t.version++; t.updatedAt=new Date().toISOString()
    wx.setStorageSync('chiji_demo_tickets',tickets);result=t
  } else if (action === 'invoice.apply') {
    const orders = getOrders(), order = orders.find(o => o.id === data.orderId)
    if (!order || order.paymentStatus !== 'PAID' || !['NONE','REJECTED'].includes(order.refundStatus)) throw new Error('仅已支付且无进行中退款的订单可申请发票')
    if (order.invoice && order.invoice.status !== 'REJECTED') throw new Error('此订单已申请发票')
    order.invoice = { ...validateInvoice(data.invoice), status: 'APPLIED', amount: order.quote.total }
    setOrders(orders); result = order.invoice
  }
  else if (action === 'order.list' || action === 'order.search') {
    const filter = data.filter || 'all'
    if (!orderFilters.includes(filter)) throw new Error('订单筛选状态无效')
    const items = getOrders().filter(o => matchesOrder(o, data.keyword || '', filter))
      .sort((a, b) => b.createdAt.localeCompare(a.createdAt) || b.id.localeCompare(a.id))
    const offset = Number.isInteger(data.offset) && data.offset > 0 ? data.offset : 0
    const maxLimit = action === 'order.search' ? 100 : 500, defaultLimit = action === 'order.search' ? 20 : 100
    const limit = Number.isInteger(data.limit) && data.limit > 0 && (action === 'order.search' || data.limit <= maxLimit) ? Math.min(maxLimit, data.limit) : defaultLimit
    const page = items.slice(offset, offset + limit)
    result = action === 'order.search' ? { items: page, total: items.length, offset, limit } : page
  }
  else if (action === 'order.shippingSubscription.get' || action === 'order.shippingSubscription.accept') {
    const order = getOrders().find(o => o.id === data.orderId)
    if (!order) throw new Error('订单不存在')
    const subscriptions = wx.getStorageSync('chiji_demo_shipping_subscriptions') || {}
    if (action.endsWith('.accept')) {
      if (order.paymentStatus !== 'PAID' || !['NONE', 'REJECTED', 'PARTIAL'].includes(order.refundStatus) || !['UNFULFILLED','PREPARING','SHIPPED'].includes(order.fulfillmentStatus)) throw new Error('当前订单不能订阅发货通知')
      subscriptions[order.id] = order.shipment ? 'SENT' : 'READY'
      wx.setStorageSync('chiji_demo_shipping_subscriptions', subscriptions)
    }
    result = { enabled: true, templateId: '', status: subscriptions[order.id] || 'NONE', demo: true }
  }
  else if (action === 'shippingNotice.list') {
    const read = wx.getStorageSync('chiji_demo_shipping_read') || []
    const items = getOrders().filter(o => o.shipment).map(o => ({ orderId: o.id, orderNo: o.orderNo, ...o.shipment, shippedAt: o.shipment!.shippedAt || o.createdAt, read: read.includes(o.id) })).sort((a,b) => Number(a.read)-Number(b.read) || b.shippedAt.localeCompare(a.shippedAt))
    result = { items: items.slice(0,200), unread: items.filter(i => !i.read).length }
  }
  else if (action === 'shippingNotice.read') {
    const read = wx.getStorageSync('chiji_demo_shipping_read') || []
    const order = getOrders().find(o => o.id === data.orderId)
    if (order?.shipment && !read.includes(order.id)) wx.setStorageSync('chiji_demo_shipping_read', [...read, order.id])
    result = { read: true }
  }
  else if (action === 'order.demoShip') {
    const orders = getOrders(), order = orders.find(o => o.id === data.orderId)
    if (!order || order.paymentStatus !== 'PAID' || !['UNFULFILLED','PREPARING'].includes(order.fulfillmentStatus) || !['NONE','REJECTED','PARTIAL'].includes(order.refundStatus)) throw new Error('当前订单不能演示发货')
    order.fulfillmentStatus = 'SHIPPED'
    order.shipment = { carrier: '演示快递', trackingNo: `DEMO${Date.now()}`, shippedAt: new Date().toISOString() }
    const subscriptions = wx.getStorageSync('chiji_demo_shipping_subscriptions') || {}
    if (subscriptions[order.id] === 'READY') subscriptions[order.id] = 'SENT'
    wx.setStorageSync('chiji_demo_shipping_subscriptions', subscriptions)
    setOrders(orders); result = order
  }
  else if (action === 'order.get') result = getOrders().find((x) => x.id === data.id)
  else if (action === 'order.cancel') {
    const orders = getOrders(); const order = orders.find((x) => x.id === data.id)
    if (order && order.paymentStatus === 'UNPAID') order.orderStatus = 'CANCELLED'
    setOrders(orders); result = order
  } else if (action === 'order.confirm') {
    const orders = getOrders(); const order = orders.find((x) => x.id === data.id)
    if (order?.fulfillmentStatus === 'SHIPPED') { if(order.packages?.some(p=>!p.shipment)) throw new Error('尚有包裹未发货'); for(const p of order.packages || []) p.receivedAt=new Date().toISOString(); order.fulfillmentStatus = 'RECEIVED'; order.orderStatus = 'COMPLETED'; (order.events ||= []).push({id:'OE_'+Date.now(),kind:'ORDER_RECEIVED',detail:'全部包裹确认收货',actor:'用户',createdAt:new Date().toISOString()}) }
    setOrders(orders); result = order
  } else if (action === 'refund.evidence.upload') {
    const order=getOrders().find(o=>o.id===data.orderId);if(!order||!canApplyRefund(order))throw new Error('当前订单不能上传售后举证')
    const id=`EVI_demo_${Date.now()}_${Math.random().toString(36).slice(2)}`
    const evidence=wx.getStorageSync('chiji_demo_evidence')||{};evidence[id]={orderId:order.id,dataUrl:data.dataUrl};wx.setStorageSync('chiji_demo_evidence',evidence);result={id}
  } else if (action === 'refund.evidence.delete') {
    const evidence=wx.getStorageSync('chiji_demo_evidence')||{};if(evidence[data.id]&&!evidence[data.id].used)delete evidence[data.id];wx.setStorageSync('chiji_demo_evidence',evidence);result={deleted:true}
  } else if (action === 'refund.evidence.get') {
    const evidence=(wx.getStorageSync('chiji_demo_evidence')||{})[data.id];if(!evidence)throw new Error('举证图片不存在');result={dataUrl:evidence.dataUrl}
  } else if (action === 'refund.list') {
    if (!getOrders().some(o => o.id === data.orderId)) throw new Error('订单不存在')
    result = (wx.getStorageSync('chiji_demo_refunds') || []).filter((r: Refund) => r.orderId === data.orderId)
  } else if (action === 'refund.apply') {
    const orders = getOrders(); const order = orders.find((x) => x.id === data.orderId)
    if (!order || !canApplyRefund(order)) throw new Error('当前订单不能申请退款')
    if (!String(data.reason || '').trim()) throw new Error('请选择退款原因')
    const remaining=order.quote.total-(order.refundedAmount||0);const amount=data.amount===undefined?remaining:data.amount
    if(!Number.isSafeInteger(amount)||amount<=0||amount>remaining)throw new Error('退款金额须大于0且不超过剩余可退金额')
    const images:string[]=data.images||[];if(images.length>6||new Set(images).size!==images.length)throw new Error('举证图片最多6张且不可重复')
    if(['SHIPPED','RECEIVED'].includes(order.fulfillmentStatus)&&!images.length)throw new Error('已发货售后请上传举证图片')
    const evidence=wx.getStorageSync('chiji_demo_evidence')||{};if(images.some(id=>evidence[id]?.orderId!==order.id||evidence[id]?.used))throw new Error('举证图片无效')
    images.forEach(id=>evidence[id].used=true);wx.setStorageSync('chiji_demo_evidence',evidence)
    const refund: Refund = { id: `RF_demo_${Date.now()}`, orderId: order.id, orderNo: order.orderNo, amount, images, reason: String(data.reason).trim().slice(0,80), note: String(data.note || '').slice(0,200), status: 'APPLIED', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }
    wx.setStorageSync('chiji_demo_refunds', [refund, ...(wx.getStorageSync('chiji_demo_refunds') || [])])
    order.refundStatus = 'APPLIED'; setOrders(orders); result = refund
  } else if (action === 'lead.submit') result = { id: `lead-${Date.now()}` }
  else throw new Error(`未知演示接口：${action}`)
  return { ok: true, data: result as T, requestId: `demo-${Date.now()}` }
}

export async function callApi<T>(action: string, data: ApiData = {}): Promise<T> {
  const app = getApp<IAppOption>()
  try {
    if (wx.getStorageSync(closedKey) === true && action !== 'privacy.get' && action !== 'service.get') throw new Error('账号已注销，请主动重新注册')
    if (app.globalData.demoMode && ['account.status', 'account.data.delete', 'account.close'].includes(action)) {
      const orders = getOrders()
      const blocked = orders.filter(o => !['COMPLETED', 'CANCELLED', 'CLOSED'].includes(o.orderStatus) || ['APPLIED', 'APPROVED', 'PROCESSING', 'FAILED'].includes(o.refundStatus)).length + (wx.getStorageSync('chiji_demo_tickets') || []).filter((t: any) => !['RESOLVED', 'CLOSED'].includes(t.status)).length + (wx.getStorageSync('chiji_demo_refunds') || []).filter((r: any) => !['SUCCEEDED', 'REJECTED', 'CLOSED'].includes(r.status)).length
      if (action === 'account.status') return { canClose: blocked === 0, blockingCount: blocked } as T
      if (action === 'account.close') {
        if (data.confirm !== '注销账号') throw new Error('请确认注销账号')
        if (blocked) throw new Error('请先处理未完成订单、退款或售后，再注销账号')
      } else if (data.confirm !== '删除个人数据' || !['addresses', 'usage'].includes(data.scope)) throw new Error('请确认删除范围')
      clearPersonalStorage(action === 'account.close' ? 'all' : data.scope)
      if (action === 'account.close') wx.setStorageSync(closedKey, true)
      return { deleted: true, closed: action === 'account.close' } as T
    }
    const response = app.globalData.demoMode
      ? await demoCall<T>(action, data)
      : await containerCall<T>(action, data)
    if (!response?.ok) throw new Error((response as any)?.error?.message || '服务暂时不可用')
    if (action === 'account.data.delete' || action === 'account.close') {
      clearPersonalStorage(action === 'account.close' ? 'all' : data.scope)
      if (action === 'account.close') wx.setStorageSync(closedKey, true)
    }
    return response.data
  } catch (error) {
    const message = error instanceof Error ? error.message : '网络开小差了，请稍后重试'
    throw new Error(message)
  }
}

async function containerCall<T>(action: string, data: ApiData, retry = true): Promise<ApiResponse<T>> {
  const app = getApp<IAppOption>()
  let token = wx.getStorageSync(TOKEN_KEY) as string
  if (!token && action !== 'privacy.get' && action !== 'service.get') token = await loginContainer()
  const response = await wx.cloud.callContainer({
    config: { env: app.globalData.cloudEnv },
    path: '/api', method: 'POST',
    header: { 'X-WX-SERVICE': app.globalData.cloudService, Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    data: { action, data },
  })
  if (response.statusCode === 401 && retry) {
    wx.removeStorageSync(TOKEN_KEY)
    try { await loginContainer() } catch (e) { throw e instanceof Error && e.message.includes('网络') ? e : new Error('登录已过期，请重新进入小程序') }
    return containerCall<T>(action, data, false)
  }
  if (response.statusCode === 401) throw new Error('登录已过期，请重新进入小程序')
  return response.data as ApiResponse<T>
}

let loginInFlight: Promise<string> | null = null
async function loginContainer(confirm = ''): Promise<string> {
  if (confirm) return performLogin(confirm)
  if (!loginInFlight) loginInFlight = performLogin().finally(() => { loginInFlight = null })
  return loginInFlight
}

function friendlyLoginError(error: unknown): Error {
  const message = error instanceof Error ? error.message : String(error || '')
  if (/request:fail|timeout|ENOTFOUND|ECONN/i.test(message)) return new Error('网络连接失败，请检查网络后重试')
  return new Error(message || '微信登录失败，请稍后重试')
}

async function wxLoginWithRetry(): Promise<string> {
  try { return (await wx.login()).code }
  catch (e) {
    try { return (await wx.login()).code } catch (_) { throw friendlyLoginError(e) }
  }
}

async function performLogin(confirm = ''): Promise<string> {
  const app = getApp<IAppOption>()
  const code = await wxLoginWithRetry()
  let response: any
  try {
    response = await wx.cloud.callContainer({
      config: { env: app.globalData.cloudEnv },
      path: '/api', method: 'POST',
      header: { 'X-WX-SERVICE': app.globalData.cloudService, 'Content-Type': 'application/json' },
      data: { action: 'auth.login', data: { code, confirm } },
    })
  } catch (e) { throw friendlyLoginError(e) }
  const result = response.data as ApiResponse<{ token: string }>
  if (!result?.ok || !result.data?.token) {
    const message = (result as any)?.error?.message || '微信登录失败'
    if (message.includes('账号已注销')) wx.setStorageSync(closedKey, true)
    throw new Error(message)
  }
  wx.setStorageSync(TOKEN_KEY, result.data.token)
  lastLoginAt = Date.now()
  return result.data.token
}

// 登录态缓存：登录成功后记录时间，供页面快速判断；token 本体仍在 storage。
let lastLoginAt = 0
export function loginState(): { loggedIn: boolean; at: number } {
  const token = wx.getStorageSync(TOKEN_KEY) as string
  return { loggedIn: !!token, at: lastLoginAt }
}
// 预热登录：小程序启动时即换取 token，首屏接口不再排队等待登录。
export async function ensureLogin(): Promise<void> {
  const app = getApp<IAppOption>()
  if (app.globalData.demoMode) return
  if (wx.getStorageSync(closedKey) === true) return
  try { if (!wx.getStorageSync(TOKEN_KEY)) await loginContainer() } catch (_) { /* 首屏仍可浏览，调用时再登录 */ }
}

export async function restartAccount() {
  if (!getApp<IAppOption>().globalData.demoMode) await loginContainer('重新注册')
  wx.removeStorageSync(closedKey)
}

export async function payOrder(orderId: string) {
  const payment = await callApi<any>('payment.create', { orderId })
  if (payment.demo) return payment.order
  if (payment.alreadyPaid) return callApi<Order>('order.get', { id: orderId })
  await wx.requestPayment(payment)
  return callApi<Order>('order.get', { id: orderId, refreshPayment: true })
}


