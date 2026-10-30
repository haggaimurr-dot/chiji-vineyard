import { createLogisticsDemo } from './logistics-demo'
import { courseDemo } from './course-demo'
import { runtimeDemo } from './runtime-operations'
import { classroomDemo } from './classroom-demo'
import { reminderDemo } from './reminder-tasks'
import { demoReturnChange, validateAttachments, validateEvidenceBudget, ticketDeadline, type AfterSalesTicket } from './after-sales'
import { searchAdminProducts, parseProductCSV } from './product-management'
import { sameReviewContent, validateResource } from './content-library'
import { can } from './permissions'
import type { ContentResource } from './types'
import type { Dashboard } from './types'
import { orderExceptions } from './picking'
import { demoDashboard } from './dashboard'
const demoCommerceActivity = [
  { buyerId: 'demo-buyer-1', visitedAt: '2026-09-18T11:00:00.000Z', cartAddedAt: '2026-09-18T11:05:00.000Z' },
  { buyerId: 'demo-buyer-2', visitedAt: '2026-09-19T06:00:00.000Z', cartAddedAt: '2026-09-19T06:05:00.000Z' },
  { buyerId: 'demo-cart-only', visitedAt: '2026-09-18T08:00:00.000Z', cartAddedAt: '2026-09-18T08:05:00.000Z' },
  { buyerId: 'demo-visitor', visitedAt: '2026-09-18T09:00:00.000Z' },
]
import { createDemoInventory } from './inventory'
import { paymentDemo } from './payment-demo'
import { createWaybillDemo } from './waybill-demo'
import { demoHome } from '../../miniprogram/data/home'
import { productBatchError, giftCustomizationError } from './product-content'
import { mockPrivacy } from '../../miniprogram/data/service'
import { demoReviews } from '../../miniprogram/data/product-content'
import { demoProductContent } from '../../miniprogram/data/product-content'
import { createMockHistory } from './mock-history'
import { validateMockConfig } from './mock-config-validation'
import { demoPersonalSecurity, demoAccounts, demoSavePermissions, demoAccountSave, demoLogin, demoResetPassword, requireDemoActor, readDemoConfig } from './demo-session'
import type { Article, ArticleStats, ArticleVersion, Lead, Order, OrderSearchResult, Product, Refund, ShipmentItem, Waybill } from './types'
import { canEditShipment, canShip, validateShipment } from './orders'
import { publicationDate, recentBeijingDates } from './content-time'
import { createDemoLeadStore, listLeads, leadFunnel } from './leads'
import { exportOrders, shipmentItemErrors, validateShipmentItems } from './order-operations'

let tickets: AfterSalesTicket[] = [{id:'AS_demo_1',orderId:'o-3',orderNo:'CJ202609180031',refundId:'RF_2',category:'QUALITY',status:'OPEN',version:1,createdAt:'2026-09-19T09:05:00.000Z',updatedAt:'2026-09-19T09:05:00.000Z',messages:[{role:'USER',content:'部分葡萄有坏果，希望商家协助处理。',createdAt:'2026-09-19T09:05:00.000Z'}]}]
const invoiceFiles = new Map<string, {fileName: string; fileData: string}>()

const svg = (title: string, color: string) => `data:image/svg+xml,${encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="800" height="560"><defs><linearGradient id="g"><stop stop-color="${color}"/><stop offset="1" stop-color="#e4c985"/></linearGradient></defs><rect width="800" height="560" fill="url(#g)"/><text x="50" y="440" fill="white" font-size="60" font-family="sans-serif" font-weight="700">${title}</text></svg>`)}`

let products: Product[] = [
  { id: 'p-shine', name: '阳光玫瑰', subtitle: '清甜玫瑰香，脆嫩无籽', category: 'family', saleMode: 'spot', cover: '/demo-products/shine.jpg', gallery: [], badges: ['当季鲜采', '家庭装'], origin: '宁夏青铜峡', description: '清晨采摘，当日预冷发出。', highlights: ['自然成熟', '冷链包装', '坏果包赔'], status: 'PUBLISHED', sortOrder: 10, skus: [{ id: 's-shine-2', name: '家庭装', price: 8990, stock: 86, reserved: 2, sold: 124, limitPerOrder: 5, weightLabel: '净重约2kg' }, { id: 's-shine-4', name: '分享装', price: 15990, stock: 42, reserved: 0, sold: 39, limitPerOrder: 3, weightLabel: '净重约4kg' }] },
  { id: 'p-kyoho', name: '巨峰葡萄', subtitle: '浓郁多汁，经典葡萄香', category: 'family', saleMode: 'spot', cover: '/demo-products/kyoho.jpg', gallery: [], badges: ['人气款'], origin: '辽宁北镇', description: '果肉柔软多汁，酸甜平衡。', highlights: ['藤上熟', '单串套袋', '产地直发'], status: 'PUBLISHED', sortOrder: 20, skus: [{ id: 's-kyoho-25', name: '家庭装', price: 6990, stock: 120, reserved: 3, sold: 202, limitPerOrder: 6, weightLabel: '净重约2.5kg' }] },
  { id: 'p-gift', name: '双拼葡萄礼盒', subtitle: '两种风味，一份体面心意', category: 'gift', saleMode: 'presale', expectedShipDate: '2026-09-28', cover: '/demo-products/gift.jpg', gallery: [], badges: ['节礼装', '预约鲜采'], origin: '迟记合作果园', description: '两种当季葡萄双拼。', highlights: ['礼盒包装', '预约发货', '企业团购'], status: 'PUBLISHED', sortOrder: 30, skus: [{ id: 's-gift-36', name: '臻选礼盒', price: 19900, stock: 30, reserved: 1, sold: 18, limitPerOrder: 5, weightLabel: '净重约3.6kg' }] },
]

products = products.map(product => ({ ...product, ...demoProductContent[product.id], imageNote: '商品示意图' }))
const inventoryStore = createDemoInventory(products)
products = inventoryStore.products()

let orders: Order[] = [
  { id: 'o-1', buyerId: 'demo-buyer-1', orderNo: 'CJ202609190001', createdAt: '2026-09-19T08:32:00.000Z', orderStatus: 'CONFIRMED', paymentStatus: 'PAID', fulfillmentStatus: 'PREPARING', refundStatus: 'NONE', items: [{ name: '阳光玫瑰', skuName: '家庭装', quantity: 2, cover: products[0].cover }], address: { name: '林女士', phone: '138****2056', province: '浙江省', city: '杭州市', district: '西湖区', detail: '文三路***号' }, quote: { total: 17980 } },
  { id: 'o-2', buyerId: 'demo-buyer-2', orderNo: 'CJ202609190002', createdAt: '2026-09-19T06:10:00.000Z', orderStatus: 'CONFIRMED', paymentStatus: 'PAID', fulfillmentStatus: 'PREPARING', refundStatus: 'APPLIED', items: [{ name: '双拼葡萄礼盒', skuName: '臻选礼盒', quantity: 1, cover: products[2].cover }], address: { name: '陈先生', phone: '186****8710', province: '上海市', city: '上海市', district: '浦东新区', detail: '世纪大道***号' }, quote: { total: 19900 } },
  { id: 'o-3', buyerId: 'demo-buyer-1', orderNo: 'CJ202609180031', createdAt: '2026-09-18T11:20:00.000Z', orderStatus: 'CONFIRMED', paymentStatus: 'PAID', fulfillmentStatus: 'SHIPPED', refundStatus: 'APPLIED', shippingNoticeStatus: 'SENT', items: [{ name: '巨峰葡萄', skuName: '家庭装', quantity: 1, cover: products[1].cover }], address: { name: '周女士', phone: '159****3312', province: '江苏省', city: '南京市', district: '鼓楼区', detail: '中山北路***号' }, quote: { total: 8190 }, shipment: { carrier: '顺丰速运', carrierCode: 'SF', trackingNo: 'SF1234567890', shippedAt: '2026-09-18T12:00:00.000Z', syncStatus: 'SUCCESS' } },
]

let refunds: Refund[] = [{id:'RF_2',orderId:'o-3',orderNo:'CJ202609180031',amount:2000,reason:'坏果/品质问题',note:'示例：部分坏果，申请部分退款。图片仅为演示商品图。',images:['EVI_example'],status:'APPLIED',createdAt:'2026-09-19T09:05:00.000Z'},{ id: 'RF_1', orderId: 'o-2', orderNo: 'CJ202609190002', amount: 19900, reason: '发货时间不合适', note: '出差无人签收', status: 'APPLIED', createdAt: '2026-09-19T07:05:00.000Z' }]

let articles: Article[] = [
  { id: 'a-1', title: '秋季修剪：来年结果枝怎么留', summary: '从枝条成熟度、芽眼位置到留枝长度，一次讲清。', cover: svg('秋季修剪', '#58714c'), variety: '通用', stage: '采后管理', type: 'article', content: ['采收结束并不意味着管理结束。', '先去除病弱枝和交叉枝。'], status: 'PUBLISHED', publishedAt: '2026-09-12' },
  { id: 'a-2', title: '葡萄转色期，糖度管理的三个关键', summary: '水分、叶果比和昼夜温差如何共同影响风味。', cover: svg('糖度管理', '#75425f'), variety: '阳光玫瑰', stage: '转色成熟', type: 'video', duration: '04:36', content: ['转色期管理不只是追求高糖。'], status: 'PUBLISHED', publishedAt: '2026-09-08' },
]

const leadStore = createDemoLeadStore([
  { id: 'l-1', organization: '邻里鲜果团', contact: '王女士', phone: '13700001234', region: '杭州', channel: '社区团购', volume: '每周 200 箱', demand: '希望提供家庭小规格包装', status: 'NEW', followUpNote: '', followUps: [], createdAt: '2026-09-19T05:20:00.000Z', ownerId: '', ownerName: '', reminderAt: new Date(Date.now() - 3600000).toISOString(), reminderDone: false, version: 0 },
  { id: 'l-2', organization: '青禾商贸', contact: '刘先生', phone: '13900005678', region: '苏州', channel: '商超/水果店', volume: '首批 500 箱', demand: '需要礼盒与企业定制卡片', status: 'CONTACTED', followUpNote: '示例：已发送报价单，待确认礼盒数量', followUps: [{ id: 'demo-history-l2', note: '示例：已发送报价单，待确认礼盒数量', status: 'CONTACTED', previousStatus: 'NEW', createdAt: '2026-09-18T04:00:00.000Z', username: 'demo' }], createdAt: '2026-09-18T03:10:00.000Z', ownerId: 'admin-demo', ownerName: 'demo', reminderAt: new Date(Date.now() + 86400000).toISOString(), reminderDone: false, version: 1 },
])
let leads: Lead[] = []
orders[0].deliveryDate='2026-10-08'
orders[0].invoice={kind:'company',title:'演示企业',taxId:'91310000DEMO123456',email:'demo@example.com',status:'APPLIED',amount:orders[0].quote.total}
orders.forEach((order, index) => {
  const product=products[[0,2,1][index]];order.items[0].productId=product.id;order.items[0].skuId=product.skus[0].id
  order.items[0].unitPrice = [8990, 19900, 6990][index]
  order.quote.subtotal = order.items.reduce((total, item) => total + (item.unitPrice || 0) * item.quantity, 0)
  order.quote.freight = order.quote.total - order.quote.subtotal
})
let couponConfig = {items: [{code:'DEMO10',name:'演示满99减10',minimum:9900,amount:1000,startDate:'2026-01-01',endDate:'2026-12-31',active:true}]}
let config = readDemoConfig({shipping: {freight:1200,freeShippingThreshold:19900,blockedProvinces:[] as string[]},home:structuredClone({...demoHome,banners:demoHome.banners.map(b=>({...b,image:b.image.replace('/assets/products/','/demo-products/')}))}),privacy:structuredClone(mockPrivacy)})
const clone = <T>(value: T): T => structuredClone(value)
const wait = () => new Promise((resolve) => setTimeout(resolve, 180))
const configHistory = createMockHistory(
  key => key === 'coupons' ? couponConfig : config[key],
  (key, payload) => { if (key === 'coupons') couponConfig = payload as typeof couponConfig; else (config as Record<string, unknown>)[key] = payload },
  (key, payload) => validateMockConfig(key, payload, products, articles),
)
const waybills = new Map<string, Waybill>()
const logisticsDemo = createLogisticsDemo(() => orders, waybills)
const waybillCarriers = [
  { code: 'SF', name: '顺丰速运' }, { code: 'YTO', name: '圆通速递' }, { code: 'ZTO', name: '中通快递' },
  { code: 'STO', name: '申通快递' }, { code: 'YD', name: '韵达快递' }, { code: 'EMS', name: 'EMS' }, { code: 'JD', name: '京东物流' }, { code: 'JT', name: '极兔速递' },
]
const waybillDemo = createWaybillDemo(waybillCarriers)

function validateDemoOrderBatches(order:Order, allocations:NonNullable<Order['batches']>) {
 if(!allocations.length||allocations.length>200)throw new Error('请登记 1–200 条订单批次')
 const seen=new Set<string>(),today=new Date().toLocaleDateString('sv-SE',{timeZone:'Asia/Shanghai'})
 for(const a of allocations){const item=order.items.find(i=>i.skuId===a.skuId),key=a.skuId+'|'+a.batchId
 if(!item||!a.batch||a.batch.id!==a.batchId||a.batch.productId!==item.productId||!Number.isInteger(a.quantity)||a.quantity<1||seen.has(key))throw new Error('批次归属、数量或重复记录无效')
 if((a.batch as any).status!=='ACTIVE'||(a.batch.expiresAt&&a.batch.expiresAt<today))throw new Error('批次已归档或过期');seen.add(key)}
 for(const item of order.items)if(allocations.filter(a=>a.skuId===item.skuId).reduce((n,a)=>n+a.quantity,0)!==item.quantity)throw new Error('每个规格的批次数量合计须等于订单数量')
}
function shipMock(item: ShipmentItem) {
  const errors = shipmentItemErrors(item)
  if (errors.length) throw new Error(errors.join('；'))
  const shipment = validateShipment(item)
  const order = orders.find(order => order.id === item.orderId.trim())
  if (!order) throw new Error('订单不存在')
  const sameShipment = (value: { carrier: string; carrierCode: string; trackingNo: string }) => value.carrier.trim() === shipment.carrier && value.carrierCode.toUpperCase() === shipment.carrierCode && value.trackingNo.toUpperCase() === shipment.trackingNo.toUpperCase()
  // Returning an existing shipment must leave notification state untouched.
  if (order.shipment && ['SHIPPED', 'RECEIVED'].includes(order.fulfillmentStatus)) {
    if (!sameShipment(order.shipment)) throw new Error('订单已发货，不能覆盖原运单')
    return { shipment: order.shipment, syncStatus: order.shipment.syncStatus, noticeStatus: order.shippingNoticeStatus || 'NONE' }
  }
  if (order.packages?.length) throw new Error('拆单订单请按包裹发货')
  if (!canShip(order)) throw new Error('当前订单不能发货')
  if(order.batches?.length) {validateDemoOrderBatches(order,order.batches); for(const a of order.batches){const b=inventoryStore.listBatches({limit:100000}).items.find(b=>b.id===a.batchId);if(!b||b.status!=='ACTIVE'||(b.expiresAt&&b.expiresAt<new Date().toLocaleDateString('sv-SE',{timeZone:'Asia/Shanghai'})))throw new Error('批次已归档或过期，请重新登记')}}
  else if(order.items.some(i=>inventoryStore.listBatches({productId:i.productId,limit:1}).total>0))throw new Error('请先登记订单实际批次和数量，再发货')
  const sameParcel = (parcel: { carrierCode: string; trackingNo: string }) => parcel.carrierCode.toUpperCase() === shipment.carrierCode && parcel.trackingNo.toUpperCase() === shipment.trackingNo.toUpperCase()
  const waybill = waybills.get(order.id)
  if (waybill && waybill.status !== 'CANCELLED' && (waybill.status !== 'READY' || !sameParcel(waybill))) throw new Error('请使用该订单已生成电子面单的承运商和运单号')
  if (orders.some(other => other.id !== order.id && other.shipment && sameParcel(other.shipment)) || [...waybills.values()].some(label => label.orderId !== order.id && sameParcel(label))) throw new Error('运单号已用于其他订单')
  order.fulfillmentStatus = 'SHIPPED'
  order.shipment = { ...shipment, shippedAt: new Date().toISOString(), syncStatus: 'SUCCESS' }
  if (['READY', 'FAILED'].includes(order.shippingNoticeStatus || '')) { order.shippingNoticeStatus = 'SENT'; delete order.shippingNoticeError }
  return { shipment: order.shipment, syncStatus: 'SUCCESS', noticeStatus: order.shippingNoticeStatus || 'NONE' }
}

function orderPage(data: any, legacy = false, unpaginated = false): OrderSearchResult {
  const supported = ['all', 'pending', 'paid', 'shipping_pending', 'shipping', 'shipped', 'received', 'completed', 'cancelled', 'closed', 'refund', 'invoice_pending', 'invoice_email', 'exceptions', 'sync_failed', 'notice_exception', 'refund_failed', 'invoice_exception']
  const filter = String(data.filter || '').trim().toLowerCase() || 'all'
  if (!supported.includes(filter)) throw new Error('订单筛选条件无效')
  const fields = {
    orderStatus: ['PENDING_PAYMENT', 'CONFIRMED', 'COMPLETED', 'CANCELLED', 'CLOSED'],
    paymentStatus: ['UNPAID', 'PAID', 'REFUNDING', 'REFUNDED'],
    fulfillmentStatus: ['UNFULFILLED', 'PREPARING', 'SHIPPED', 'RECEIVED'],
    refundStatus: ['NONE', 'APPLIED', 'APPROVED', 'REJECTED', 'FAILED', 'PARTIAL', 'SUCCEEDED'],
  }
  const requested = Object.entries(fields).map(([key, allowed]) => {
    const value = String(data[key] || '').trim().toUpperCase()
    if (value && !allowed.includes(value)) throw new Error('订单筛选状态无效')
    return { key: key as keyof typeof fields, value }
  })
  const words = [...String(data.keyword || '').replace(/[<>]/g, '').trim()].slice(0, 80).join('').toLowerCase().split(/\s+/).filter(Boolean)
  const items = orders.filter(o => {
    const active = o.orderStatus === 'CONFIRMED' && o.paymentStatus === 'PAID' && ['NONE', 'REJECTED', 'PARTIAL'].includes(o.refundStatus)
    const issues = orderExceptions(o)
    const status = filter === 'exceptions' ? issues.length > 0
      : filter === 'sync_failed' ? o.shipment?.syncStatus === 'FAILED'
      : filter === 'notice_exception' ? ['FAILED','UNCERTAIN'].includes(o.shippingNoticeStatus || '')
      : filter === 'refund_failed' ? o.refundStatus === 'FAILED'
      : filter === 'invoice_exception' ? o.invoice?.status === 'ISSUED' && ['FAILED','UNCERTAIN'].includes(o.invoice.emailStatus || '')
      : filter === 'pending' ? o.orderStatus === 'PENDING_PAYMENT' && o.paymentStatus === 'UNPAID'
      : ['paid', 'shipping_pending'].includes(filter) ? canShip(o)
      : ['shipping', 'shipped'].includes(filter) ? active && o.fulfillmentStatus === 'SHIPPED'
      : filter === 'received' ? o.fulfillmentStatus === 'RECEIVED'
      : filter === 'completed' ? o.orderStatus === 'COMPLETED'
      : filter === 'cancelled' ? o.orderStatus === 'CANCELLED'
      : filter === 'closed' ? o.orderStatus === 'CLOSED'
      : filter === 'refund' ? o.refundStatus !== 'NONE' || ['REFUNDING', 'REFUNDED'].includes(o.paymentStatus)
      : filter === 'invoice_pending' ? o.invoice?.status === 'APPLIED'
      : filter === 'invoice_email' ? o.invoice?.status === 'ISSUED' && ['READY','FAILED','SENDING','UNCERTAIN'].includes(o.invoice.emailStatus || '') : true
    const text = [o.orderNo, ...o.items.map(i => `${i.name} ${i.skuName}`), o.address.name, o.address.phone, o.shipment?.trackingNo].join(' ').toLowerCase()
    return status && requested.every(({ key, value }) => !value || o[key] === value) && words.every(w => text.includes(w))
  }).sort((a, b) => b.createdAt.localeCompare(a.createdAt) || b.id.localeCompare(a.id))
  const max = legacy ? 500 : 100
  const defaultLimit = legacy ? 200 : 20
  const limit = Number.isInteger(data.limit) && data.limit > 0
    ? legacy && data.limit > max ? defaultLimit : Math.min(data.limit, max) : defaultLimit
  const offset = Number.isInteger(data.offset) && data.offset > 0 ? data.offset : 0
  return { items: unpaginated ? items : items.slice(offset, offset + limit), total: items.length, offset, limit }
}

// Demo content uses the same revisions and concurrency rules as the live API.
// Persist only this feature's state; other demo modules keep their existing behavior.
const resourceStorageKey = 'chiji_content_resources_v1'
function readResources(): ContentResource[] { try { return JSON.parse(localStorage.getItem(resourceStorageKey) || '[]') } catch { return [] } }
const articleStorageKey = 'chiji_admin_demo_articles_v1'
const articleHistories: Record<string, ArticleVersion[]> = {}
function exampleStats(id: string): ArticleStats {
  const sample = id === 'a-1' ? [12, 18, 24, 16, 32, 28, 37, 21, 45, 33, 40, 52, 38, 29] : id === 'a-2' ? [8, 11, 14, 9, 20, 18, 22, 12, 27, 19, 24, 30, 22, 17] : Array(14).fill(0)
  const days = recentBeijingDates().map((date, index) => ({ date, views: sample[index], readers: Math.floor(sample[index] * .7) }))
  return { totalViews: sample.reduce((sum, views) => sum + views, 0), totalReaders: Math.floor(days.reduce((sum, day) => sum + day.readers, 0) * .6), todayViews: days[13].views, todayReaders: days[13].readers, days }
}
for (const article of articles) {
  const stats = exampleStats(article.id)
  Object.assign(article, { version: 1, readCount: stats.totalViews, readerCount: stats.totalReaders, updatedAt: `${article.publishedAt}T00:00:00+08:00` })
  articleHistories[article.id] = [{ version: 1, createdAt: article.updatedAt!, createdBy: '演示管理员', reason: 'legacy', article: clone(article) }]
}
function readArticleState() {
  try {
    const stored = localStorage.getItem(articleStorageKey)
    if (!stored) return
    const state = JSON.parse(stored)
    if (!Array.isArray(state.articles) || !state.histories || !state.articles.every((article: Article) => article.id && Array.isArray(article.content) && ['DRAFT', 'PUBLISHED', 'SCHEDULED'].includes(article.status))) return
    articles = state.articles
    for (const id of Object.keys(articleHistories)) delete articleHistories[id]
    for (const article of articles) articleHistories[article.id] = Array.isArray(state.histories[article.id]) ? state.histories[article.id] : [{ version: article.version || 1, createdAt: article.updatedAt || new Date().toISOString(), createdBy: '演示管理员', reason: 'legacy', article: clone(article) }]
  } catch { /* Storage is optional in demo mode. */ }
}
function persistArticles() {
  try { localStorage.setItem(articleStorageKey, JSON.stringify({ articles, histories: articleHistories })) } catch { /* Keep the current session usable if storage is unavailable. */ }
}
function articleActor(): string {
  try { return JSON.parse(localStorage.getItem('chiji_admin_user') || '{}').username || '演示管理员' } catch { return '演示管理员' }
}
function addArticleRevision(article: Article, reason: string, createdBy = articleActor()) {
  (articleHistories[article.id] ||= []).unshift({ version: article.version!, createdAt: article.updatedAt!, createdBy, reason, article: clone(article) })
}
function publishDueArticles() {
  let changed = false
  for (const article of articles) {
    if (article.status !== 'SCHEDULED' || !article.scheduledAt || !Number.isFinite(Date.parse(article.scheduledAt)) || Date.parse(article.scheduledAt) > Date.now()) continue
    article.status = 'PUBLISHED'; article.publishedAt = article.scheduledAt; delete article.scheduledAt
    article.version = (article.version || 1) + 1; article.updatedAt = new Date().toISOString()
    addArticleRevision(article, 'scheduled_publish', '定时发布'); changed = true
  }
  if (changed) persistArticles()
}
function existingArticle(id: unknown): Article {
  const article = articles.find(item => item.id === id)
  if (!article) throw new Error('内容不存在，请刷新列表')
  return article
}
function assertArticleVersion(article: Article, expected: unknown) {
  if (expected !== (article.version || 1)) throw new Error(`内容已更新至 V${article.version || 1}，请重新载入后再保存，避免覆盖其他修改`)
}
function saveArticle(input: Article): Article {
  if (!input || typeof input !== 'object') throw new Error('内容参数无效')
  const current = input.id ? existingArticle(input.id) : undefined
  if (current) assertArticleVersion(current, input.version)
  if (!['DRAFT', 'PUBLISHED', 'SCHEDULED'].includes(input.status) || !['article', 'video'].includes(input.type)) throw new Error('内容类型或发布状态无效')
  const required = ['title', 'summary', 'cover', 'variety', 'stage'] as const
  for (const field of required) if (typeof input[field] !== 'string' || !input[field].trim()) throw new Error('请填写标题、摘要、封面、品种和种植阶段')
  if (input.contentHtml !== undefined && typeof input.contentHtml !== 'string') throw new Error('富文本正文格式无效')
  if (!Array.isArray(input.content) || input.content.some(paragraph => typeof paragraph !== 'string')) throw new Error('正文格式无效')
  const content = input.content.map(paragraph => paragraph.trim()).filter(Boolean)
  if ((!content.length && !input.contentHtml?.trim()) || content.length > 100 || content.join('').length > 50000) throw new Error('请填写正文段落或富文本；正文最多 100 段，总长度不能超过 50000 字')
  if (input.publishedAt && !publicationDate(input.publishedAt)) throw new Error('发布日期无效')
  let scheduledAt: string | undefined
  if (input.status === 'SCHEDULED') {
    if (!input.scheduledAt || !/^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(input.scheduledAt) || !Number.isFinite(Date.parse(input.scheduledAt)) || Date.parse(input.scheduledAt) <= Date.now()) throw new Error('定时发布时间必须包含时区且晚于当前时间')
    scheduledAt = new Date(input.scheduledAt).toISOString()
  }
  const generatedId = typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : `${Date.now()}-${Math.random().toString(36).slice(2)}`
  const article: Article = { id: current?.id || `a-${generatedId}`, title: input.title.trim(), summary: input.summary.trim(), cover: input.cover.trim(), variety: input.variety.trim(), stage: input.stage.trim(), type: input.type, content, status: input.status, publishedAt: input.publishedAt || new Date().toISOString(), version: current ? (current.version || 1) + 1 : 1, updatedAt: new Date().toISOString(), readCount: current?.readCount || 0, readerCount: current?.readerCount || 0 }
  if (input.tags && (!Array.isArray(input.tags) || input.tags.length > 20 || input.tags.some(tag => typeof tag !== 'string' || !tag.trim() || [...tag.trim()].length > 80))) throw new Error('每篇内容最多20个标签，每个标签不能为空且最多80字')
  article.tags = [...new Set((input.tags || []).map(tag => tag.trim()))]
  if (input.contentHtml) article.contentHtml = input.contentHtml.trim()
  if (input.videoUrl) article.videoUrl = input.videoUrl.trim()
  if (input.duration) article.duration = input.duration.trim()
  if (scheduledAt) article.scheduledAt = scheduledAt
  if (current?.reviewStatus) {
    if (sameReviewContent(article, current)) Object.assign(article, { reviewStatus: current.reviewStatus, reviewNote: current.reviewNote, reviewedBy: current.reviewedBy, reviewedAt: current.reviewedAt })
    else article.reviewStatus = 'UNSUBMITTED'
    if (article.status !== 'DRAFT' && article.reviewStatus !== 'APPROVED') throw new Error('此内容需要重新提交审核，通过后才能发布')
  }
  articles = [article, ...articles.filter(item => item.id !== article.id)]
  addArticleRevision(article, current ? 'save' : 'create'); persistArticles()
  return article
}
readArticleState()

function demoDashboardExport(d: Dashboard): { filename: string; content: string } {
  const yuan = (cents: number) => (cents / 100).toFixed(2)
  const rows: string[][] = [
    ['经营概览报表', `${d.startDate} 至 ${d.endDate}`],
    ['指标', '数值'],
    ['净收入（元）', yuan(d.revenue)],
    ['毛收入（元）', yuan(d.grossRevenue)],
    ['退款金额（元）', yuan(d.refundedAmount)],
    ['支付订单数', String(d.paidOrders)],
    [],
    ['每日趋势', ''],
    ['日期', '支付订单数', '毛收入（元）', '退款金额（元）', '净收入（元）'],
    ...d.trend.map(t => [t.date, String(t.paidOrders), yuan(t.grossRevenue), yuan(t.refundedAmount), yuan(t.netRevenue)]),
  ]
  const esc = (v: string) => /["\n,]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v
  const content = '\ufeff' + rows.map(r => r.map(esc).join(',')).join('\r\n')
  return { filename: `经营概览_${d.startDate}_${d.endDate}.csv`, content }
}

export async function mockCall<T>(action: string, data: any = {}): Promise<T> {
  await wait()
  if (action === 'admin.dashboard') leads = leadStore.read().items
  if (action.startsWith('admin.article.') || action === 'admin.dashboard') { readArticleState(); publishDueArticles() }
  let result: any
  const actor = action === 'admin.login' ? null : requireDemoActor(action)
  if (action.startsWith('admin.runtime.')) return await runtimeDemo(action, data, actor!.username) as T
  if (actor) configHistory.setActor(actor.username, actor.id)
  if (action.startsWith('admin.payment.') && actor?.role !== 'SUPER_ADMIN') throw new Error('仅超级管理员可以操作')
  if (['admin.config.versions', 'admin.config.rollback', 'admin.audit.list', 'admin.audit.export', 'admin.config.compare'].includes(action) && actor?.role !== 'SUPER_ADMIN') throw new Error('仅超级管理员可以操作')
  if (action === 'admin.login') { result = await demoLogin(data); if (result.user) configHistory.login(result.user.username, result.user.id) }
  else if (action.startsWith('admin.security.') || action === 'admin.account.changePassword') { result = await demoPersonalSecurity(action, data, actor!); if (action !== 'admin.security.status') configHistory.audit(action, actor!.id, { sessionsRevoked: !!result.sessionRevoked }) }
  else if (action === 'admin.session') result = demoAccounts().find(a => a.id === actor!.id)
  else if (action.startsWith('admin.reminder.')) {
    result = reminderDemo(action, data)
    if (action === 'admin.reminder.resolve') configHistory.audit('REMINDER_RESOLVE', data.productId, { ...data, after: result.status })
  }
  else if (action === 'admin.account.permissions') result = demoSavePermissions(data.accountId, data.permissions, actor!)
  else if (action.startsWith('admin.payment.')) result = paymentDemo(action, data)
  else if (action === 'admin.media.upload') result = { fileID: data.dataUrl }
  else if (action === 'admin.dashboard.export') {
    const demo = demoDashboard(orders, refunds, data, { pendingShipment: 0, pendingRefund: 0, newLeads: 0, products: 0, articles: 0 }, demoCommerceActivity)
    result = demoDashboardExport(demo)
  }
    else if (action === 'admin.dashboard') result = demoDashboard(orders, refunds, data, { pendingShipment: orders.filter((o) => o.paymentStatus === 'PAID' && ['UNFULFILLED', 'PREPARING'].includes(o.fulfillmentStatus) && ['NONE', 'REJECTED', 'PARTIAL'].includes(o.refundStatus)).length, pendingRefund: refunds.filter((r) => r.status === 'APPLIED').length, newLeads: leads.filter((l) => l.status === 'NEW').length, products: products.length, articles: articles.filter((a) => a.status === 'PUBLISHED').length }, demoCommerceActivity)
  else if (action === 'admin.review.list') {
    let items = JSON.parse(localStorage.getItem('chiji_admin_demo_reviews') || 'null') || clone(demoReviews)
    const page = data.page || 1
    if (!Number.isInteger(page) || page < 1 || page > 1000000) throw new Error('分页参数无效')
    if (data.status && !['PENDING', 'APPROVED', 'REJECTED'].includes(data.status)) throw new Error('评价状态无效')
    if (data.status) items = items.filter((r: any) => (r.status || 'APPROVED') === data.status)
    items.sort((a: any, b: any) => b.createdAt.localeCompare(a.createdAt) || b.id.localeCompare(a.id))
    result = { items: items.slice((page-1)*20, page*20), total: items.length, hasMore: page*20 < items.length }
  } else if (action === 'admin.review.moderate') {
    const items = JSON.parse(localStorage.getItem('chiji_admin_demo_reviews') || 'null') || clone(demoReviews)
    const item = items.find((r: any) => r.id === data.id)
    if (!item) throw new Error('评价不存在')
    if (!['APPROVED', 'REJECTED'].includes(data.status)) throw new Error('审核状态无效')
    if ((item.status || 'APPROVED') === data.status) throw new Error('评价已处于该审核状态')
    const reason = String(data.reason || '').trim()
    if (data.status === 'REJECTED' && (!reason || Array.from(reason).length > 500)) throw new Error('请填写1–500字驳回原因')
    Object.assign(item, { status: data.status, rejectReason: data.status === 'REJECTED' ? reason : '', moderatedAt: new Date().toISOString() })
    localStorage.setItem('chiji_admin_demo_reviews', JSON.stringify(items)); result = item
  } else if (action === 'admin.review.reply') {
    const items = JSON.parse(localStorage.getItem('chiji_admin_demo_reviews') || 'null') || clone(demoReviews)
    const item = items.find((r: any) => r.id === data.id)
    if (!item) throw new Error('评价不存在')
    if (item.replyAt) throw new Error('该评价已回复')
    const content = String(data.content || '').trim()
    if (!content || Array.from(content).length > 500) throw new Error('请填写1–500字回复')
    Object.assign(item, { reply: content, replyAt: new Date().toISOString() }); localStorage.setItem('chiji_admin_demo_reviews', JSON.stringify(items)); result = item
  }
  else if (action === 'admin.product.search') result = searchAdminProducts(products,data)
  else if (action === 'admin.product.import') { const preview=parseProductCSV(data.content||''); if(data.preview) result=preview; else {if(preview.errors.length)throw new Error(preview.errors.join('；'));inventoryStore.importProducts(preview.products,actor?.username||'演示管理员');products=inventoryStore.products();result={created:preview.products.length,skus:preview.rows}} }
  else if (action === 'admin.order.batches.save') {
 const order=orders.find(o=>o.id===data.orderId);if(!order||!canShip(order))throw new Error('仅待发货订单可登记批次');if((order.batchVersion||0)!==data.expectedVersion)throw new Error('订单批次已变化，请刷新后重试');
 const allocations=(data.items||[]).map((a:any)=>({...a,batch:inventoryStore.listBatches({limit:100000}).items.find(b=>b.id===a.batchId)}));
 validateDemoOrderBatches(order,allocations);order.batches=structuredClone(allocations);order.batchVersion=(order.batchVersion||0)+1;result=order
 }
 else if (action === 'admin.batch.list' || action === 'admin.order.batches.options') result=inventoryStore.listBatches(data)
  else if (action === 'admin.batch.save') result=inventoryStore.saveBatch(data.batch)
  else if (action === 'admin.product.list') result = products
  else if (action === 'admin.product.save') { const p = data.product as Product; const batchError = productBatchError(p.traceability) || giftCustomizationError(p.giftCustomization); if (batchError) throw new Error(batchError); if (!p.id) p.id = `p-${Date.now()}`;p.skus.forEach((sku,index)=>{if(!sku.id)sku.id=p.id+'-sku-'+index}); inventoryStore.saveProduct(p, actor?.username || '演示管理员'); products = inventoryStore.products(); result = p }
  else if (action === 'admin.inventory.set') { const sku=products.flatMap(p=>p.skus).find(s=>s.id===data.skuId); if(!sku) throw new Error('规格不存在'); inventoryStore.adjust({items:[{skuId:sku.id,stock:data.stock,expectedStock:sku.stock}],reason:'单项库存调整'},actor?.username || '演示管理员'); result=data }
  else if (action === 'admin.inventory.locations') result=inventoryStore.locations()
  else if (action === 'admin.inventory.warehouse.save') result=inventoryStore.saveWarehouse(data)
  else if (action === 'admin.inventory.lots') result=inventoryStore.lots(data)
  else if (action === 'admin.inventory.lot.movements') result=inventoryStore.lotHistory(data)
  else if (['admin.inventory.receive','admin.inventory.loss','admin.inventory.allocate'].includes(action)) result=inventoryStore.changeLot(action,data,actor?.username || '演示管理员')
  else if (action === 'admin.inventory.movements') result=inventoryStore.list(data)
  else if (action === 'admin.inventory.alerts') result=inventoryStore.alerts()
  else if (action === 'admin.inventory.alert.save') result=inventoryStore.saveAlert(data)
  else if (action === 'admin.coupon.get') result = couponConfig
  else if (action === 'admin.coupon.save') {
    result = configHistory.save('coupons', data)
  }
  else if (action === 'admin.ticket.list') result = tickets.filter(t => (!data.orderId || t.orderId === data.orderId) && (!data.status || t.status === data.status)).slice(data.offset || 0, (data.offset || 0) + 50)
  else if (action === 'admin.ticket.create') {
    const attachments = validateAttachments(data.attachments || [])
    const o = orders.find(o => o.id === data.orderId), content = String(data.content || '').trim()
    if (!o) throw new Error('订单不存在')
    if (!['PAID','REFUNDING','REFUNDED'].includes(o.paymentStatus)) throw new Error('请支付订单后提交售后工单')
    if (data.refundId && !refunds.some(r => r.id === data.refundId && r.orderId === o.id)) throw new Error('退款申请与订单不匹配')
    if (!['QUALITY','DELIVERY','INVOICE','OTHER'].includes(data.category) || !content || [...content].length > 1000) throw new Error('请选择问题类型并填写描述，最多1000字')
    if (tickets.some(t => t.orderId === o.id && t.status !== 'CLOSED')) throw new Error('此订单已有未关闭工单，请在原工单补充说明')
    result = {id: 'AS_' + crypto.randomUUID(), orderId:o.id, orderNo:o.orderNo, refundId:data.refundId || '',category:data.category,status:'OPEN',version:1,createdAt:new Date().toISOString(),updatedAt:new Date().toISOString(),dueAt:new Date(Date.now()+48*3600000).toISOString(),adminRead:1,userRead:0,messages:[{role:'ADMIN',content,createdAt:new Date().toISOString(),attachments}]}
    tickets.unshift(result)
  }
  else if (action === 'admin.ticket.read') {
    
    const t = tickets.find(t => t.id === data.id)
    if (!t) throw new Error('工单不存在')
    if (t.version !== data.version) throw new Error('工单已更新，请刷新后重试')
    if (!Number.isInteger(data.readThrough) || data.readThrough < 0 || data.readThrough > t.messages.length) throw new Error('已读参数无效')
    t.adminRead = Math.max(t.adminRead || 0, data.readThrough)
     result = t
  }
  else if (action === 'admin.ticket.returnTracking') {
    const t=tickets.find(t=>t.id===data.id)
    if(!t?.return?.shipment) throw new Error('退货尚未寄出')
    result={status:'UNAVAILABLE',message:'演示模式不查询真实物流，可复制运单号查询',events:[]}
  }
  else if (action === 'admin.ticket.update') {
    const attachments = validateAttachments(data.attachments || [])
    const t = tickets.find(t => t.id === data.id), content = String(data.content || '').trim()
    if (!t) throw new Error('工单不存在')
    if (t.version !== data.version) throw new Error('工单已更新，请刷新后重试')
    if (t.status === 'CLOSED') throw new Error('工单已关闭，如有新问题请新建工单')
    validateEvidenceBudget(t,attachments)
    if (t.messages.length >= 200) throw new Error('工单记录已达上限，请联系商家处理')
    if ([...content].length > 1000) throw new Error('回复最多1000字')
    if (data.return) {
      if(data.status || data.owner !== undefined || data.dueAt !== undefined || content || attachments.length) throw new Error('退货操作请单独提交')
      const o=orders.find(o=>o.id===t.orderId)
      if(!o || !['SHIPPED','RECEIVED'].includes(o.fulfillmentStatus)) throw new Error('仅已发货订单可办理实物退货')
      if(o.items.some(item => !item.skuId)) throw new Error('订单规格信息不完整，无法办理退货')
      const next=demoReturnChange(t,data.return,true,o.items.map(item => ({...item,skuId:item.skuId!})),tickets)
      if(data.return.action==='RECEIVE') { inventoryStore.returnReceipt(next.items,t.id,actor?.username || '演示管理员',next.inspection || '退货验收'); next.receivedBy=actor?.username || '演示管理员' }
      t.return=next
      if(data.return.action==='AUTHORIZE') t.status='PROCESSING'
      t.messages.push({role:'ADMIN',content:data.return.action==='AUTHORIZE'?'商家已确认退货地址与明细，请寄回并登记运单':'退货已验收：'+next.inspection,createdAt:new Date().toISOString()})
    }
    if(['RESOLVED','CLOSED'].includes(data.status) && t.return && t.return.status!=='RECEIVED') throw new Error('请完成退货验收入库后结单')
    const transitions: Record<string,string[]> = {OPEN:['OPEN','PROCESSING','RESOLVED'],PROCESSING:['PROCESSING','RESOLVED'],RESOLVED:['RESOLVED','CLOSED']}
    if (data.status && !transitions[t.status]?.includes(data.status)) throw new Error('工单状态不能如此变更')
    if (['RESOLVED','CLOSED'].includes(data.status) && !content) throw new Error('请填写处理结果后结单')
    if (data.owner && (!demoAccounts().some(a => a.username === data.owner && a.active) || [...data.owner].length > 40)) throw new Error('负责人须为有效管理员账号')
    const dueAt = data.dueAt === undefined ? undefined : ticketDeadline(data.dueAt)
    if (!content && attachments.length) throw new Error('请填写附件说明')
    if (data.status) t.status = data.status
    if (data.owner !== undefined) t.owner = String(data.owner).trim()
    if (!content && attachments.length) throw new Error('请填写附件说明')
    if (dueAt !== undefined) t.dueAt = dueAt
    if (content) t.messages.push({role:'ADMIN',content,createdAt:new Date().toISOString(),attachments})
    t.adminRead = t.messages.length
    t.version++; t.updatedAt = new Date().toISOString(); result = t
  }
  else if (action === 'admin.invoice.autoConfig') result = {configured:true,enabled:false}
  else if (action === 'admin.invoice.autoIssue') {
    const o = orders.find(o => o.id === data.orderId)
    if (!o?.invoice) throw new Error('没有发票申请')
    if (o.invoice.status === 'ISSUED') result = o.invoice
    else {
      if (o.invoice.status !== 'APPLIED') throw new Error('没有待处理发票申请')
      if (o.paymentStatus !== 'PAID' || !['NONE','REJECTED'].includes(o.refundStatus)) { Object.assign(o.invoice,{autoStatus:'BLOCKED',autoError:'订单存在退款，暂停自动开票'}) }
      else {
        const key = o.invoice.autoKey || 'INV_' + crypto.randomUUID()
        // Demo PDF is visibly identified as a simulation and has no tax validity.
        const stream = 'BT /F1 20 Tf 50 750 Td (DEMO ONLY - NOT A TAX INVOICE) Tj ET\n'
        const objects = ['<</Type/Catalog/Pages 2 0 R>>', '<</Type/Pages/Kids[3 0 R]/Count 1>>', '<</Type/Page/Parent 2 0 R/MediaBox[0 0 595 842]/Resources<</Font<</F1 4 0 R>>>>/Contents 5 0 R>>', '<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>', `<</Length ${stream.length}>>\nstream\n${stream}endstream`]
        let pdf = '%PDF-1.4\n'; const offsets: number[] = []
        objects.forEach((object,index) => { offsets.push(pdf.length); pdf += `${index + 1} 0 obj\n${object}\nendobj\n` })
        const xref = pdf.length
        pdf += `xref\n0 6\n0000000000 65535 f \n${offsets.map(offset => String(offset).padStart(10,'0') + ' 00000 n \n').join('')}trailer\n<</Size 6/Root 1 0 R>>\nstartxref\n${xref}\n%%EOF`

        const fileName = 'DEMO-' + o.orderNo + '.pdf'
        invoiceFiles.set(o.id,{fileName,fileData:btoa(pdf)})
        Object.assign(o.invoice,{autoKey:key,autoStatus:'ISSUED',autoError:'',status:'ISSUED',number:'DEMO-'+o.orderNo,fileName,issuedAt:new Date().toISOString(),emailStatus:'SENT',sentAt:new Date().toISOString()})
      }
      result = o.invoice
    }
  }
  else if (action === 'admin.invoice.update') {
    const o=orders.find(o=>o.id===data.orderId); if (!o?.invoice || o.invoice.status!=='APPLIED') throw new Error('没有待处理发票申请')
    if (['PROCESSING','UNCERTAIN'].includes(o.invoice.autoStatus || '')) throw new Error('自动开票结果待确认，请先核实原请求')
    if (data.status==='ISSUED') {
      if (o.paymentStatus!=='PAID' || !['NONE','REJECTED'].includes(o.refundStatus)) throw new Error('订单退款状态已变化，不能开票')
      if (!data.number?.trim()) throw new Error('请输入发票号码')
      if (!/\.pdf$/i.test(data.fileName || '') || !data.fileData || data.fileData.length > 4194304 || !data.fileData.startsWith('JVBERi0')) throw new Error('请上传有效的 PDF 发票，大小不超过3MB')
      invoiceFiles.set(o.id, {fileName: data.fileName, fileData: data.fileData})
      Object.assign(o.invoice, {number: data.number.trim(), fileName: data.fileName, issuedAt: new Date().toISOString(), emailStatus: 'SENT', sentAt: new Date().toISOString()})
    }
    else if(data.status==='REJECTED'){if (!data.reason?.trim()) throw new Error('请输入拒绝原因');o.invoice.reason=data.reason.trim()}
    else throw new Error('无效发票状态');o.invoice.status=data.status;result=o.invoice
  }
  else if (action === 'admin.invoice.file') { const file = invoiceFiles.get(data.orderId); if (!file) throw new Error('发票文件不存在'); result = file }
  else if (action === 'admin.invoice.retryEmail') {
    const o = orders.find(o => o.id === data.orderId)
    if (!o?.invoice || o.invoice.status !== 'ISSUED' || !o.invoice.fileName) throw new Error('请先上传发票并登记开票')
    if (!['SENT','UNCERTAIN'].includes(o.invoice.emailStatus || '')) {
      if (o.paymentStatus !== 'PAID' || !['NONE','REJECTED'].includes(o.refundStatus)) throw new Error('订单退款状态已变化，不能发送发票')
      Object.assign(o.invoice, { emailStatus: 'SENT', sentAt: new Date().toISOString(), emailError: '' })
    }
    result = o.invoice
  }
  else if (action === 'admin.invoice.resolveEmail') {
    const o = orders.find(o => o.id === data.orderId)
    if (!o?.invoice || o.invoice.emailStatus !== 'UNCERTAIN') throw new Error('仅发送结果待核实的发票可登记核实结果')
    if (!['SENT','FAILED'].includes(data.status) || !data.reason?.trim()) throw new Error('请核对邮件服务记录并填写核实说明')
    Object.assign(o.invoice, { emailStatus: data.status, emailError: data.status === 'FAILED' ? '已核实未发送，可重试' : '', ...(data.status === 'SENT' ? {sentAt: new Date().toISOString()} : {}) }); result = o.invoice
  }
  else if (action === 'admin.order.list') result = orderPage(data, true).items
  else if (action === 'admin.order.search') result = orderPage(data)
  else if (action === 'admin.order.picking') {
    const ids = data.orderIds
    if (!Array.isArray(ids) || !ids.length || ids.length > 50 || ids.some((id: unknown) => typeof id !== 'string' || !/^[A-Za-z0-9_-]{1,80}$/.test(id.trim())) || new Set(ids.map((id: string) => id.trim())).size !== ids.length) throw new Error('拣货单请选择1至50笔不同的有效订单')
    const items = ids.map((id: string) => orders.find(order => order.id === id.trim()))
    if (items.some((order: Order | undefined) => !order || !canShip(order))) throw new Error('部分订单已不可拣货，请刷新后只选择可发货订单')
    result = items
  }
  else if (action === 'admin.order.export') {
    let items: Order[]
    if (Object.prototype.hasOwnProperty.call(data, 'orderIds')) {
      if (!Array.isArray(data.orderIds) || !data.orderIds.length || data.orderIds.length > 500 || data.orderIds.some((id: unknown) => typeof id !== 'string' || !/^[A-Za-z0-9_-]{1,80}$/.test(id.trim())) || new Set(data.orderIds.map((id: string) => id.trim())).size !== data.orderIds.length) throw new Error('请选择1至500个不同的有效订单')
      const ids = new Set(data.orderIds.map((id: string) => id.trim()))
      items = orders.filter(order => ids.has(order.id)).sort((a, b) => b.createdAt.localeCompare(a.createdAt) || b.id.localeCompare(a.id))
      if (items.length !== ids.size) throw new Error('部分所选订单不存在，请刷新重试')
    } else items = orderPage(data, true, true).items
    if (items.length > 10000) throw new Error('导出最多10000条，请缩小筛选范围')
    result = exportOrders(items, `orders-${new Date().toISOString().slice(0, 10)}.csv`)
  }
  else if (['admin.order.split','admin.order.package.ship','admin.order.packages.sync','admin.order.events'].includes(action)) {
    const o=orders.find(o => o.id===data.orderId)
    if(!o) throw new Error('订单不存在')
    if(action==='admin.order.events') result=o.events || []
    else {
      if(o.paymentStatus!=='PAID' || !['NONE','REJECTED','PARTIAL'].includes(o.refundStatus) || !['CONFIRMED','COMPLETED'].includes(o.orderStatus)) throw new Error('当前订单不能操作')
      const event=(kind:string,detail:string,packageId='') => { (o.events ||= []).push({id:'OE_'+Date.now()+'_'+Math.random().toString(36).slice(2),kind,detail,actor:actor?.username || '演示管理员',packageId,createdAt:new Date().toISOString()}) }
      if(action==='admin.order.split') {
        if(!canShip(o) || o.shipment || waybills.has(o.id)) throw new Error('仅未发货且未生成面单的订单可拆单')
        if((o.packageVersion || 0)!==data.expectedVersion) throw new Error('包裹已变更，请刷新')
        const plan=data.packages
        if(!Array.isArray(plan) || plan.length<2 || plan.length>10) throw new Error('请拆成2至10个包裹')
        const totals=o.items.map(() => 0)
        for(const items of plan) {
          if(!Array.isArray(items) || !items.length) throw new Error('包裹不能为空')
          const seen=new Set<number>()
          for(const item of items) {
            if(!Number.isInteger(item.itemIndex) || !o.items[item.itemIndex] || !Number.isSafeInteger(item.quantity) || item.quantity<=0 || seen.has(item.itemIndex)) throw new Error('包裹商品或数量无效')
            seen.add(item.itemIndex); totals[item.itemIndex]+=item.quantity
          }
        }
        if(totals.some((n,i)=>n!==o.items[i].quantity)) throw new Error('商品分配合计须等于订单数量')
        o.packages=plan.map((items:any,i:number)=>({id:'PKG_'+Date.now()+'_'+i,number:o.orderNo+'-'+String(i+1).padStart(2,'0'),items:JSON.parse(JSON.stringify(items))}))
        o.packageVersion=(o.packageVersion || 0)+1
        event('ORDER_SPLIT','拆分为'+plan.length+'个履约子单（包裹）')
      } else if(action==='admin.order.package.ship') {
        const p=o.packages?.find(p => p.id===data.packageId), input=validateShipment(data)
        if(!p) throw new Error('包裹不存在')
        if(p.shipment) {
          if(p.shipment.carrier!==input.carrier || p.shipment.carrierCode!==input.carrierCode || p.shipment.trackingNo!==input.trackingNo) throw new Error('包裹已发货，不能覆盖')
        } else {
          if(!canShip(o) || (o.packageVersion || 0)!==data.expectedVersion) throw new Error('包裹已变更或当前不能发货，请刷新')
          const matches=(shipment:any)=>shipment && shipment.carrierCode?.toUpperCase()===input.carrierCode && shipment.trackingNo.toUpperCase()===input.trackingNo.toUpperCase()
          if(orders.some(other => matches(other.shipment) || other.packages?.some(p=>matches(p.shipment))) || [...waybills.values()].some(matches)) throw new Error('运单号已被使用')
          p.shipment={...input,shippedAt:new Date().toISOString(),syncStatus:'PENDING'}
          if(!o.shipment) o.shipment={...p.shipment,carrierCode:input.carrierCode,syncStatus:'PENDING'}
          if(o.packages!.every(p=>p.shipment)) o.fulfillmentStatus='SHIPPED'
          o.packageVersion=(o.packageVersion || 0)+1
          event('PACKAGE_SHIPPED',p.number+' 已发货：'+input.trackingNo,p.id)
        }
      } else {
        if(!o.shipment || !o.packages?.length) throw new Error('没有可同步的包裹')
        for(const p of o.packages) if(p.shipment) {p.shipment.syncStatus='SUCCESS';p.shipment.syncError=''}
        o.shipment.syncStatus='SUCCESS';o.shipment.syncError=''
        event('PACKAGES_SYNC','演示：微信包裹同步成功')
      }
      result=JSON.parse(JSON.stringify(o))
    }
  }
  else if (action === 'admin.order.ship') result = shipMock(data)
  else if (action === 'admin.order.batchShip') {
    if (!Array.isArray(data.items) || !data.items.length || data.items.length > 50) throw new Error('每批须选择1至50个订单')
    if (data.items.some((item: ShipmentItem) => typeof item?.orderId !== 'string' || !/^[A-Za-z0-9_-]{1,80}$/.test(item.orderId.trim()))) throw new Error('订单ID无效')
    const duplicateErrors = validateShipmentItems(data.items).filter(error => error.includes('重复'))
    if (duplicateErrors.length) throw new Error(duplicateErrors.join('；'))
    const results = data.items.map((item: ShipmentItem) => {
      try { return { orderId: item.orderId.trim(), success: true, ...shipMock(item) } }
      catch (error) { return { orderId: item.orderId.trim(), success: false, error: (error as Error).message } }
    })
    const succeeded = results.filter((item: { success: boolean }) => item.success).length
    result = { results, succeeded, failed: results.length - succeeded }
  }
  else if (['admin.logistics.tasks','admin.logistics.task.save','admin.order.addParcel','admin.waybill.cancel','admin.waybill.history'].includes(action)) result = logisticsDemo(action, data, '演示运营员')
  else if (action === 'admin.waybill.settings') result = waybillDemo.get()
  else if (action === 'admin.waybill.settings.save') {
    result = waybillDemo.save(data)
    configHistory.audit('WAYBILL_CONFIG_SAVE', 'waybill', { revision: result.revision, carrierCount: Object.keys(result.accounts).length, rateCount: result.rates.length })
  }
  else if (action === 'admin.waybill.quote') {
    const order = orders.find(order => order.id === data.orderId)
    if (!order) throw new Error('订单不存在')
    result = waybillDemo.quote(order.address.province, data.weight)
  }
  else if (action === 'admin.waybill.config') result = { configured: waybillDemo.carriers().length>0, carriers: waybillDemo.carriers(), message: '演示模式：示例面单不可交寄，不会向快递100申请真实运单号' }
  else if (action === 'admin.waybill.get') {
    if (!orders.some(order => order.id === data.orderId)) throw new Error('订单不存在')
    result = waybills.get(data.orderId) || null
  }
  else if (action === 'admin.waybill.create') {
    const order = orders.find(order => order.id === data.orderId)
    if (!order || !canShip(order)) throw new Error('当前订单不能申请电子面单')
    const carrier = waybillDemo.carriers().find(carrier => carrier.code === String(data.carrierCode || '').trim().toUpperCase())
    if (!carrier || typeof data.weight !== 'number' || !Number.isFinite(data.weight) || data.weight <= 0 || data.weight > 100) throw new Error('请选择已配置的承运商并填写0至100kg内的重量')
    let existing = waybills.get(order.id)
    if (existing?.status === 'CANCELLED') { waybills.delete(order.id); existing = undefined }
    if (existing) {
      if (existing.carrierCode !== carrier.code) throw new Error('该订单已申请其他承运商面单，请先核实原面单')
      result = existing
    } else {
      const now = new Date().toISOString()
      const label: Waybill = { id: `DEMO-WB-${order.id}-${Date.now()}`, orderId: order.id, orderNo: order.orderNo, carrier: carrier.name, carrierCode: carrier.code, trackingNo: `DEMO${Date.now()}${waybills.size}`, labelUrl: '', status: 'READY', createdAt: now, updatedAt: now, demo: true }
      waybills.set(order.id, label); result = label
    }
  }
  else if (action === 'admin.waybill.label') {
    const label = waybills.get(data.orderId)
    if (!label || label.status !== 'READY') throw new Error('该订单没有可打印的电子面单')
    result = { dataUrl: '', mime: 'text/html', demo: true }
  }
  else if (action === 'admin.order.updateShipment') {
    const o = orders.find(x => x.id === data.orderId)
    if (!o || !canEditShipment(o)) throw new Error('当前订单不能修改发货信息，请刷新确认订单状态')
    const expected = data.expectedShipment
    if (!expected || !['carrier', 'carrierCode', 'trackingNo'].every(key => typeof expected[key] === 'string'
      && expected[key] === o.shipment![key as 'carrier' | 'carrierCode' | 'trackingNo'])) {
      throw new Error('发货信息已变化，请刷新后重新编辑')
    }
    const shipment = validateShipment(data)
    const sameParcel = (parcel: { carrierCode: string; trackingNo: string }) => parcel.carrierCode.toUpperCase() === shipment.carrierCode && parcel.trackingNo.toUpperCase() === shipment.trackingNo.toUpperCase()
    const waybill = waybills.get(o.id)
    if (waybill && waybill.status !== 'CANCELLED' && (waybill.status !== 'READY' || !sameParcel(waybill))) throw new Error('请使用该订单已生成电子面单的承运商和运单号')
    if (orders.some(other => other.id !== o.id && other.shipment && sameParcel(other.shipment)) || [...waybills.values()].some(label => label.orderId !== o.id && sameParcel(label))) throw new Error('运单号已用于其他订单')
    o.shipment = { ...o.shipment!, ...shipment, syncStatus: 'SUCCESS' }
    delete o.shipment.syncError
    delete o.shipment.syncAttempt
    delete o.shipment.syncStartedAt
    result = { shipment: o.shipment, syncStatus: 'SUCCESS', noticeStatus: o.shippingNoticeStatus || 'NONE' }
  }
  else if (action === 'admin.order.retryShippingNotice') { const o = orders.find(o => o.id === data.orderId); if (!o?.shipment) throw new Error('订单尚未发货'); if (['READY','FAILED'].includes(o.shippingNoticeStatus || '')) o.shippingNoticeStatus = 'SENT'; result = { status: o.shippingNoticeStatus || 'NONE' } }
  else if (action === 'admin.order.resyncShipment') {
    const o = orders.find(x => x.id === data.orderId)
    if (!o?.shipment || !['SHIPPED', 'RECEIVED'].includes(o.fulfillmentStatus)) throw new Error('订单尚未发货')
    if (o.shipment.syncStatus === 'PENDING' && o.shipment.syncStartedAt
      && Date.now() - Date.parse(o.shipment.syncStartedAt) < 120_000) throw new Error('物流信息正在同步，请稍后重试')
    o.shipment.syncStatus = 'SUCCESS'; delete o.shipment.syncError
    delete o.shipment.syncAttempt; delete o.shipment.syncStartedAt
    result = { shipment: o.shipment, syncStatus: 'SUCCESS' }
  }
  else if (action === 'admin.refund.evidence.get') {if(data.id!=='EVI_example')throw new Error('举证图片不存在');result={dataUrl:products[1].cover}}
  else if (action === 'admin.refund.list') result = refunds.map(r=>({...r,orderTotal:orders.find(o=>o.id===r.orderId)?.quote.total,fulfillmentStatus:orders.find(o=>o.id===r.orderId)?.fulfillmentStatus}))
  else if (['admin.refund.reject', 'admin.refund.approve', 'admin.refund.sync'].includes(action)) {
    const r = refunds.find(x => x.id === data.refundId), o = orders.find(x => x.id === r?.orderId)
    if (!r || !o) throw new Error('退款申请不存在')
    if (action === 'admin.refund.sync') result = r
    else {
      if (r.status !== 'APPLIED') throw new Error('退款申请状态已变化')
      if (action === 'admin.refund.reject') {
        const reason = String(data.reason || '').trim(); if (!reason) throw new Error('请输入拒绝原因')
        r.status = 'REJECTED'; r.rejectReason = reason.slice(0,160); o.refundStatus = 'REJECTED'
      } else { if(r.amount<=0||r.amount>o.quote.total-(o.refundedAmount||0))throw new Error('退款金额异常');r.status='SUCCEEDED';o.refundedAmount=(o.refundedAmount||0)+r.amount;o.refundStatus=o.refundedAmount===o.quote.total?'SUCCEEDED':'PARTIAL';if(o.refundStatus==='SUCCEEDED'){o.paymentStatus='REFUNDED';if(['UNFULFILLED','PREPARING'].includes(o.fulfillmentStatus))o.orderStatus='CLOSED'} }
      r.updatedAt = new Date().toISOString(); result = r
    }
  }
  else if (action === 'admin.content.library') result = readResources()
  else if (['admin.content.resource.save', 'admin.content.resource.delete'].includes(action)) {
    const resource: ContentResource = clone(data.resource)
    validateResource(resource, true)
    if (!actor || !can(actor, resource.kind === 'tag' ? 'article.write' : 'media.upload')) throw new Error('权限不足，无法执行此操作')
    const resources = readResources(), current = resources.find(r => r.id === resource.id)
    if ((current && (current.version !== resource.version || current.kind !== resource.kind)) || (!current && resource.version)) throw new Error('资源已变化，请刷新后重试')
    if (action.endsWith('.delete') && !current) throw new Error('资源不存在')
    resource.id ||= `RES${crypto.randomUUID().replace(/-/g, '').slice(0, 20)}`
    resource.name = resource.name.trim(); resource.version = (current?.version || 0) + 1
    localStorage.setItem(resourceStorageKey, JSON.stringify(action.endsWith('.delete') ? resources.filter(r => r.id !== resource.id) : [resource, ...resources.filter(r => r.id !== resource.id)]))
    result = resource
  }
  else if (['admin.article.submit', 'admin.article.review'].includes(action)) {
    const article = existingArticle(data.articleId)
    assertArticleVersion(article, data.expectedVersion)
    if (article.status !== 'DRAFT') throw new Error('仅草稿可以提交或审核')
    if (action.endsWith('.submit')) {
      if (article.reviewStatus === 'PENDING') throw new Error('内容已待审核')
      article.reviewStatus = 'PENDING'; article.reviewNote = ''; article.reviewedBy = ''; article.reviewedAt = ''
    } else {
      if (article.reviewStatus !== 'PENDING') throw new Error('仅待审核草稿可以审核')
      const note = String(data.note || '').trim()
      if (!['APPROVED', 'REJECTED'].includes(data.decision) || note.length > 500 || (data.decision === 'REJECTED' && !note)) throw new Error('请选择通过或退回；退回原因必填，最多500字')
      article.reviewStatus = data.decision; article.reviewNote = note; article.reviewedBy = articleActor(); article.reviewedAt = new Date().toISOString()
    }
    article.version = (article.version || 1) + 1; article.updatedAt = new Date().toISOString()
    addArticleRevision(article, article.reviewStatus!); persistArticles(); result = article
  }
  else if (action.startsWith('admin.course.')) result = courseDemo(action, data, articles)
  else if (action.startsWith('admin.classroom.')) result = classroomDemo(action, data, articles[0])
  else if (action === 'admin.article.list') result = articles
  else if (action === 'admin.article.save') result = saveArticle(data.article)
  else if (action === 'admin.article.versions') { const article = existingArticle(data.articleId); result = (articleHistories[article.id] || []).slice().sort((a, b) => b.version - a.version) }
  else if (action === 'admin.article.restore') {
    const current = existingArticle(data.articleId); assertArticleVersion(current, data.expectedVersion)
    const snapshot = articleHistories[current.id]?.find(item => item.version === data.version)
    if (!snapshot) throw new Error('历史版本不存在')
    const restored: Article = { ...clone(snapshot.article), id: current.id, status: 'DRAFT', version: (current.version || 1) + 1, updatedAt: new Date().toISOString(), readCount: current.readCount || 0, readerCount: current.readerCount || 0 }
    if (current.reviewStatus || restored.reviewStatus) { restored.reviewStatus = 'UNSUBMITTED'; delete restored.reviewNote; delete restored.reviewedBy; delete restored.reviewedAt }
    delete restored.scheduledAt
    articles = [restored, ...articles.filter(item => item.id !== current.id)]
    addArticleRevision(restored, `restore:${snapshot.version}`); persistArticles(); result = restored
  }
  else if (action === 'admin.article.stats') { const article = existingArticle(data.articleId); result = { ...exampleStats(article.id), totalViews: article.readCount || 0, totalReaders: article.readerCount || 0 } }
  else if (action === 'admin.lead.list' || action === 'admin.lead.search') result = listLeads(leadStore.read().items, data)
  else if (action === 'admin.lead.funnel') result = leadFunnel(leadStore.read().items, data)
  else if (action === 'admin.lead.get') result = leadStore.get(String(data.leadId || '').trim())
  else if (action === 'admin.lead.owners') result = demoAccounts().filter(account => account.active).map(({ id, username, role, active }) => ({ id, username, role, active }))
  else if (action === 'admin.lead.history') result = leadStore.history(String(data.leadId || '').trim())
  else if (action === 'admin.lead.deliveries') { leadStore.get(String(data.leadId)); result = [] }
  else if (action === 'admin.lead.reminders') result = leadStore.reminders()
  else if (action === 'admin.lead.update') result = leadStore.update(data, actor!, demoAccounts())
  else if (action === 'admin.config.get') result = config
  else if (action === 'admin.home.save') result = configHistory.save('home', data.home)
  else if (action === 'admin.privacy.save') result = configHistory.save('privacy', data.privacy)
  else if (action === 'admin.config.compare') result = configHistory.compare(data)
  else if (action === 'admin.audit.export') result = configHistory.exportAudit(data)
  else if (action === 'admin.config.versions') result = configHistory.versions(data)
  else if (action === 'admin.config.rollback') result = configHistory.rollback(data)
  else if (action === 'admin.audit.list') result = configHistory.listAudit(data)
  else if (action === 'admin.inventory.batch') result=inventoryStore.adjust(data,actor?.username || '演示管理员')
  else if (action === 'admin.order.tracking') {const o=orders.find(o=>o.id===data.orderId);const parcel=data.packageId ? o?.packages?.find(p=>p.id===data.packageId)?.shipment : o?.shipment;if(!parcel)throw new Error('订单尚未发货');result={status:'DEMO',message:'演示轨迹',updatedAt:new Date().toISOString(),events:[{time:'2026-09-19 15:30:00',context:'演示：快件到达配送站'},{time:'2026-09-18 20:00:00',context:'演示：承运商已揽收'}]}}
  else if (action === 'admin.account.list') result = demoAccounts()
  else if (action === 'admin.account.save') result = await demoAccountSave(data.account, actor!)
  else if (action === 'admin.account.resetPassword') result = await demoResetPassword(data.accountId, data.password, actor!)
  else if (action === 'admin.config.save') result = { shipping: configHistory.save('shipping', data.shipping) }
  else throw new Error(`演示接口未实现：${action}`)
  const auditActions: Record<string, string> = {
    'admin.review.moderate': 'REVIEW_MODERATE', 'admin.review.reply': 'REVIEW_REPLY',
    'admin.ticket.create': 'TICKET_CREATE', 'admin.ticket.update': 'TICKET_UPDATE', 'admin.invoice.autoIssue': 'INVOICE_AUTO_REQUEST',
    'admin.invoice.retryEmail': 'INVOICE_EMAIL_RETRY', 'admin.invoice.resolveEmail': 'INVOICE_EMAIL_RESOLVE',
    'admin.order.batches.save': 'ORDER_BATCH_SAVE', 'admin.inventory.warehouse.save':'INVENTORY_WAREHOUSE_SAVE','admin.inventory.receive':'INVENTORY_PURCHASE','admin.inventory.loss':'INVENTORY_LOSS','admin.inventory.allocate':'INVENTORY_ALLOCATE', 'admin.product.import': 'PRODUCT_IMPORT', 'admin.batch.save': 'PRODUCT_BATCH_SAVE', 'admin.media.upload': 'MEDIA_UPLOAD', 'admin.product.save': 'PRODUCT_SAVE', 'admin.inventory.set': 'INVENTORY_SET', 'admin.inventory.batch': 'INVENTORY_BATCH', 'admin.inventory.alert.save': 'INVENTORY_ALERT_SAVE',
    'admin.invoice.update': 'INVOICE_UPDATE', 'admin.order.ship': 'ORDER_SHIP', 'admin.order.resyncShipment': 'ORDER_SHIPMENT_RESYNC', 'admin.order.updateShipment': 'ORDER_SHIPMENT_UPDATE',
    'admin.order.retryShippingNotice': 'SHIPPING_NOTICE_RETRY', 'admin.refund.reject': 'REFUND_REJECT', 'admin.refund.approve': 'REFUND_APPROVE', 'admin.refund.sync': 'REFUND_SYNC',
    'admin.article.submit': 'ARTICLE_REVIEW', 'admin.article.review': 'ARTICLE_REVIEW', 'admin.content.resource.save': 'CONTENT_RESOURCE', 'admin.content.resource.delete': 'CONTENT_RESOURCE', 'admin.article.save': 'ARTICLE_SAVE', 'admin.article.restore': 'ARTICLE_RESTORE', 'admin.lead.update': 'LEAD_UPDATE', 'admin.account.save': 'ADMIN_ACCOUNT_SAVE', 'admin.account.resetPassword': 'ADMIN_PASSWORD_RESET', 'admin.account.permissions': 'ADMIN_PERMISSIONS_SAVE',
    'admin.order.batchShip': 'ORDER_BATCH_SHIP', 'admin.order.export': 'ORDER_EXPORT', 'admin.waybill.create': 'WAYBILL_CREATE', 'admin.waybill.label': 'WAYBILL_LABEL_SAVE',
  }
  if (auditActions[action]) {
    if (actor) configHistory.setActor(actor.username, actor.id)
    const target = data.orderId || data.refundId || data.leadId || data.skuId || data.accountId || result?.id || ''
    const detail = action === 'admin.article.submit' || action === 'admin.article.review' ? { status: result.reviewStatus, note: result.reviewNote, version: result.version }
      : action.startsWith('admin.content.resource.') ? { name: result.name, kind: result.kind, action }
      : action === 'admin.account.save' ? { username: result.username, role: result.role, active: result.active }
      : action === 'admin.product.save' || action.startsWith('admin.article.') ? { title: result.title || result.name }
      : action === 'admin.inventory.batch' ? { reason: data.reason, items: data.items }
      : action === 'admin.inventory.set' ? { stock: data.stock }
      : { status: result?.status || result?.syncStatus || data.status || '', reason: data.reason || '' }
    configHistory.audit(auditActions[action], target, detail)
  }
  return clone(result as T)
}
