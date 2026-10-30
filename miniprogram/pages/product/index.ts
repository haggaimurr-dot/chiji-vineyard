import { recordView, readMemory, toggleCompare } from '../../utils/product-memory'
import { callApi } from '../../utils/api'
import { requestRestock, saveRestockGrant, savePriceDropGrant, RestockConfig } from '../../utils/restock'
import { money } from '../../utils/format'
import { parseTargetPrice, promptTargetPrice } from '../../utils/reminder-target'
import { available, purchaseLimit } from '../../utils/product'
import type { Product, Sku, ReviewSummary, Review } from '../../utils/types'

type DisplayProduct = Omit<Product, 'skus'> & { skus: Array<Sku & { priceText: string; compareText: string; soldOut: boolean }> }
Page({
  previewReviewImage(e: WechatMiniprogram.BaseEvent) { wx.previewImage({ current: e.currentTarget.dataset.url, urls: e.currentTarget.dataset.urls }) },
  data: {
    product: null as DisplayProduct | null, productId: '',
    images: [] as Array<{ src: string; failed: boolean }>,
    reviews: [] as Array<Review & { stars: string; date: string }>, reviewTotal: 0, reviewAverage: '', reviewsLoading: false, reviewsError: '',
    reviewPage: 0, reviewsHasMore: false, reviewRequestVersion: 0,
    skuIndex: 0, quantity: 1, maxQuantity: 0, soldOut: false,
    loading: true, error: '', submitting: false, attentionBusy: false, comparing: false,
    restockConfig: null as RestockConfig | null, restockError: '',
    priceDropConfig: null as RestockConfig | null, priceDropError: '', priceDropNote: '', targetPriceInput: '',
  },
  onLoad(options: Record<string, string>) { this.setData({ productId: options.id || '' }); return this.loadProduct() },
  async loadProduct() {
    this.setData({ loading: true, error: '' })
    try {
      if (!this.data.productId) throw new Error('商品链接无效，请返回商城重新选择')
      const [product, restockConfig, priceDropConfig] = await Promise.all([
        callApi<Product>('product.detail', { id: this.data.productId }),
        callApi<RestockConfig>('product.restockSubscription.config').catch(() => null),
        callApi<RestockConfig>('product.priceDropSubscription.config').catch(() => null),
      ])
      this.setData({ restockConfig, priceDropConfig })
      if (!product) throw new Error('商品不存在或已下架，请看看其他当季鲜果')
      const skuIndex = Math.max(0, product.skus.findIndex(s => purchaseLimit(s) > 0))
      const sources = [...new Set([product.cover, ...product.gallery].filter(Boolean))]
      this.setData({
        product: { ...product, skus: product.skus.map(s => ({ ...s, priceText: money(s.price), compareText: s.compareAtPrice ? money(s.compareAtPrice) : '', soldOut: purchaseLimit(s) === 0 })) },
        images: sources.length ? sources.map(src => ({ src, failed: false })) : [{ src: '', failed: true }],
        skuIndex, quantity: 1, maxQuantity: purchaseLimit(product.skus[skuIndex]), soldOut: !available(product),
        priceDropNote: product.priceDrop ? `${product.priceDrop.skuName || '已下架规格'} · 关注价 ¥${money(product.priceDrop.baselinePrice)}${product.priceDrop.targetPrice ? ` · 目标价 ¥${money(product.priceDrop.targetPrice)}` : ''}${product.priceDrop.dropped ? (product.priceDrop.targetPrice ? ' · 已到目标价' : ' · 已降价') : ''}` : '',
      })
      try { recordView(product) } catch { /* 浏览记录存储失败不影响商品购买 */ }
      this.setData({ comparing: readMemory('compare').some(p => p.id === product.id) })
      wx.setNavigationBarTitle({ title: product.name })
      await this.loadReviews()
    } catch (error) { this.setData({ product: null, error: (error as Error).message || '商品加载失败，请稍后重试' }) }
    finally { this.setData({ loading: false }) }
  },
  onShow() { if (this.data.product && !this.data.submitting) return this.loadProduct() },
  async toggleAttention(event: WechatMiniprogram.BaseEvent) {
    if (this.data.attentionBusy || !this.data.product) return
    const kind = event.currentTarget.dataset.kind
    const field = kind === 'favorite' ? 'favorite' : kind === 'pricedrop' ? 'priceDropReminder' : 'restockReminder'
    const enabled = !this.data.product[field]
    this.setData({ attentionBusy: true })
    try {
      const target = kind === 'pricedrop' && enabled && this.data.targetPriceInput.trim() ? { targetPrice: parseTargetPrice(this.data.targetPriceInput) } : {}
      await callApi('product.attention.set', { productId: this.data.productId, kind, enabled, ...(kind === 'pricedrop' ? { skuId: this.data.product.skus[this.data.skuIndex]?.id, ...target } : {}) })
      this.setData({ product: { ...this.data.product!, [field]: enabled, ...(kind === 'restock' ? { restockPushStatus: 'NONE' } : {}) }, restockError: '' })
      wx.showToast({ title: enabled ? (kind === 'favorite' ? '已收藏' : kind === 'pricedrop' ? '已设置降价提醒' : '已设置到货提醒') : '已取消', icon: 'success' })
      if (kind === 'pricedrop') { this.setData({ priceDropError: '' }); await this.loadProduct() }
    } catch (error) { wx.showToast({ title: (error as Error).message, icon: 'none' }) }
    finally { this.setData({ attentionBusy: false }) }
  },
  inputTargetPrice(event: WechatMiniprogram.Input) { this.setData({ targetPriceInput: event.detail.value }) },
  async editTargetPrice(event: WechatMiniprogram.BaseEvent) {
    if (this.data.attentionBusy || !this.data.product?.priceDrop) return
    this.setData({ attentionBusy: true })
    try {
      const targetPrice = event.currentTarget.dataset.clear ? 0 : await promptTargetPrice(this.data.product.priceDrop.targetPrice)
      if (targetPrice === null) return
      await callApi('product.priceDropTarget.set', { productId: this.data.productId, targetPrice })
      await this.loadProduct()
      wx.showToast({ title: '提醒条件已更新', icon: 'success' })
    } catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
    finally { this.setData({ attentionBusy: false }) }
  },
  openReminderHistory() { wx.navigateTo({ url: '/pages/attention/index?kind=history' }) },
  async subscribeRestock() {
    const config = this.data.restockConfig
    if (this.data.attentionBusy || !config?.enabled || !this.data.product?.restockReminder) return
    this.setData({ attentionBusy: true, restockError: '' })
    try {
      if (!await requestRestock(config)) {
        this.setData({ restockError: '未开启微信提醒，仍可在「我的 → 到货提醒」查看到货状态。' }); return
      }
      const result = await saveRestockGrant(this.data.productId, config)
      this.setData({ product: { ...this.data.product!, restockPushStatus: result.status } })
      wx.showToast({ title: '已订阅微信到货提醒', icon: 'success' })
    } catch (e) { this.setData({ restockError: (e as Error).message || '订阅保存失败，请重试' }) }
    finally { this.setData({ attentionBusy: false }) }
  },
  async subscribePriceDrop() {
    const config = this.data.priceDropConfig
    if (this.data.attentionBusy || !config?.enabled || !this.data.product?.priceDropReminder) return
    this.setData({ attentionBusy: true, priceDropError: '' })
    try {
      if (!await requestRestock(config)) { this.setData({ priceDropError: '未开启微信提醒，仍可在「我的 → 降价提醒」查看价格变化。' }); return }
      const result = await savePriceDropGrant(this.data.productId, config)
      this.setData({ product: { ...this.data.product!, priceDrop: { ...this.data.product!.priceDrop!, pushStatus: result.status } } })
      wx.showToast({ title: '已订阅微信降价提醒', icon: 'success' })
    } catch (e) { this.setData({ priceDropError: (e as Error).message || '订阅保存失败，请重试' }) }
    finally { this.setData({ attentionBusy: false }) }
  },
  async loadReviews() {
    return this.fetchReviews(true)
  },
  async loadMoreReviews() {
    if (this.data.reviewsLoading || !this.data.reviewsHasMore) return
    return this.fetchReviews(false)
  },
  retryReviews() { return this.data.reviewPage ? this.loadMoreReviews() : this.loadReviews() },
  onUnload() { this.setData({ reviewRequestVersion: this.data.reviewRequestVersion + 1 }) },
  async fetchReviews(reset: boolean) {
    const version = this.data.reviewRequestVersion + 1
    const page = reset ? 1 : this.data.reviewPage + 1
    this.setData({ reviewRequestVersion: version })
    if (reset) this.setData({ reviews: [], reviewPage: 0, reviewTotal: 0, reviewAverage: '', reviewsHasMore: false })
    this.setData({ reviewsLoading: true, reviewsError: '' })
    try {
      const result = await callApi<ReviewSummary>('product.reviews', { productId: this.data.productId, page, pageSize: 20 })
      if (version !== this.data.reviewRequestVersion) return
      if (!Array.isArray(result?.items)) throw new Error('评价加载失败')
      const existing = reset ? [] : this.data.reviews
      const ids = new Set(existing.map(r => r.id))
      const incoming = result.items.filter(r => !ids.has(r.id)).map(r => ({ ...r, stars: '★'.repeat(r.rating) + '☆'.repeat(5-r.rating), date: r.createdAt.slice(0,10) }))
      this.setData({ reviews: [...existing, ...incoming], reviewPage: result.page, reviewsHasMore: result.hasMore, reviewTotal: result.total, reviewAverage: result.average.toFixed(1) })
    } catch (e) { if (version === this.data.reviewRequestVersion) this.setData({ reviewsError: (e as Error).message || '评价加载失败，请重试' }) }
    finally { if (version === this.data.reviewRequestVersion) this.setData({ reviewsLoading: false }) }
  },
  openPairing(event: WechatMiniprogram.BaseEvent) {
    const id = event.currentTarget.dataset.id
    if (id) wx.navigateTo({ url: `/pages/product/index?id=${encodeURIComponent(id)}` })
  },
  goArticle(event: WechatMiniprogram.BaseEvent) {
    const id = event.currentTarget.dataset.id as string
    if (id) wx.navigateTo({ url: '/pages/article/index?id=' + encodeURIComponent(id) })
  },
  copyTraceCode() {
    const code = this.data.product?.traceability?.code
    if (!code) return
    wx.setClipboardData({ data: code, fail: () => wx.showToast({ title: '复制失败，请重试', icon: 'none' }) })
  },
  customizeGift() {
    const product = this.data.product
    if (!product?.giftCustomization?.enabled) return
    wx.navigateTo({ url: `/pages/cooperation/index?giftProductId=${encodeURIComponent(product.id)}`,
      fail: () => wx.showToast({ title: '暂时无法打开定制表单，请重试', icon: 'none' }) })
  },
  imageError(event: WechatMiniprogram.BaseEvent) { this.setData({ [`images[${Number(event.currentTarget.dataset.index)}].failed`]: true }) },
  previewImage(event: WechatMiniprogram.BaseEvent) {
    const current = this.data.images[Number(event.currentTarget.dataset.index)]
    if (current && !current.failed) wx.previewImage({ current: current.src, urls: this.data.images.filter(i => !i.failed).map(i => i.src) })
  },
  chooseSku(event: WechatMiniprogram.BaseEvent) {
    if (this.data.submitting) return
    const skuIndex = Number(event.currentTarget.dataset.index)
    const sku = this.data.product?.skus[skuIndex]
    if (!purchaseLimit(sku)) { wx.showToast({ title: '该规格已售罄，请选择其他规格', icon: 'none' }); return }
    this.setData({ skuIndex, quantity: 1, maxQuantity: purchaseLimit(sku) })
  },
  minus() { if (!this.data.submitting && this.data.quantity > 1) this.setData({ quantity: this.data.quantity - 1 }) },
  plus() { if (!this.data.submitting && this.data.quantity < this.data.maxQuantity) this.setData({ quantity: this.data.quantity + 1 }) },
  canPurchase() {
    const sku = this.data.product?.skus[this.data.skuIndex]
    if (!sku || !purchaseLimit(sku)) { wx.showToast({ title: '该规格暂时无法购买', icon: 'none' }); return false }
    if (this.data.quantity < 1 || this.data.quantity > purchaseLimit(sku)) { wx.showToast({ title: '库存或限购数量已变化，请重新选择', icon: 'none' }); return false }
    return !this.data.submitting
  },
  async addCart() {
    if (!this.canPurchase()) return
    const product = this.data.product!, sku = product.skus[this.data.skuIndex]
    this.setData({ submitting: true })
    try {
      await callApi('cart.update', { productId: product.id, skuId: sku.id, quantity: this.data.quantity, addToCart: true })
      wx.showToast({ title: '已加入购物袋', icon: 'success' })
    } catch (error) { wx.showToast({ title: (error as Error).message || '加入失败，请重试', icon: 'none' }) }
    finally { this.setData({ submitting: false }) }
  },
  buyNow() {
    if (!this.canPurchase()) return
    const product = this.data.product!, sku = product.skus[this.data.skuIndex]
    this.setData({ submitting: true })
    try {
      wx.setStorageSync('chiji_checkout_items', [{ productId: product.id, skuId: sku.id, quantity: this.data.quantity, selected: true }])
      wx.navigateTo({ url: '/pages/checkout/index', fail: () => wx.showToast({ title: '暂时无法打开结算，请重试', icon: 'none' }), complete: () => this.setData({ submitting: false }) })
    } catch { this.setData({ submitting: false }); wx.showToast({ title: '暂时无法结算，请重试', icon: 'none' }) }
  },
  toggleCompare() {
    if (!this.data.product) return
    try { this.setData({ comparing: toggleCompare(this.data.product) }) }
    catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
  },
  openCompare() { wx.navigateTo({ url: '/pages/product-memory/index?kind=compare' }) },
  openCart() { wx.navigateTo({ url: '/pages/cart/index' }) },
  goShop() { wx.switchTab({ url: '/pages/shop/index' }) },
  openOrders() { wx.navigateTo({ url: '/pages/orders/index' }) },
})
