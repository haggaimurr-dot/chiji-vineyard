import { chinaDate, deliveryStart, checkoutIntent, purchaseKey } from '../../utils/checkout'
import { callApi, payOrder } from '../../utils/api'
import { money } from '../../utils/format'
import { purchaseLimit } from '../../utils/product'
import type { Address, CartItem, Order, PriceQuote } from '../../utils/types'

Page({
  privacy() { wx.navigateTo({url:'/pages/privacy/index'}) },
  data: { items: [] as any[], address: null as Address | null, quote: null as any, couponInput: '', couponCode: '', autoCoupon: true, redeemBusy: false, quoteBusy: false, quoteVersion: 0, quoteError: '', deliveryDate: '', deliveryMin: chinaDate(2), deliveryMax: chinaDate(30), demo: getApp().globalData.demoMode, note: '', agreed: false, submitting: false, createdOrderId: '' },
  async onLoad() {
    try {
      const storageKey = getApp().globalData.demoMode ? 'chiji_demo_checkout_attempt' : 'chiji_checkout_attempt'
      const pending = wx.getStorageSync(storageKey)
      if (pending?.key) {
        const existing = await callApi<Order | null>('order.recover', { idempotencyKey: pending.key })
        if (existing) {
          this.setData({ createdOrderId: existing.id })
          wx.removeStorageSync(storageKey)
          wx.removeStorageSync('chiji_checkout_items')
          wx.redirectTo({ url: `/pages/order-detail/index?id=${existing.id}` })
          return
        }
      }
      const raw: CartItem[] = wx.getStorageSync('chiji_checkout_items') || []
      if (!raw.length) throw new Error('请先选择要购买的商品')
      const hydrated = raw.map(x => ({ ...x }))
      // 每次进入结算都重新查询商品；保留此次选择的数量。
      for (let i = 0; i < hydrated.length; i += 1) {
        const product: any = await callApi('product.detail', { id: hydrated[i].productId })
        const sku = product?.skus.find((s: any) => s.id === hydrated[i].skuId)
        if (!product || !sku || !Number.isInteger(hydrated[i].quantity) || hydrated[i].quantity > purchaseLimit(sku) || hydrated[i].quantity < 1) {
          wx.showToast({ title: '商品库存或限购已变化，请重新选择', icon: 'none' })
          this.setData({ quoteError: '商品库存或限购已变化，请返回重新选择' })
          return
        }
        hydrated[i] = { ...hydrated[i], product, sku }
      }
      this.setData({ deliveryMin: deliveryStart(hydrated), items: hydrated.map((x) => ({ ...x, lineText: money(x.sku!.price * x.quantity) })) }, () => this.refreshQuote())
    } catch (e) { this.setData({ items: [], quote: null, quoteError: (e as Error).message }) }
  },
  async onShow() {
    try {
      const addresses = await callApi<Address[]>('address.list')
      const address = addresses.find(a => a.id === this.data.address?.id) || addresses.find(a => a.isDefault) || addresses[0] || null
      this.setData({ address }, () => this.refreshQuote())
    }
    catch (_) { this.setData({ address: null }) }
  },
  async refreshQuote() {
    if (!this.data.items.length) return
    const version = this.data.quoteVersion + 1
    this.setData({ quoteVersion: version, quoteBusy: true, quoteError: '' })
    try {
      const quote = await callApi<PriceQuote>('order.quote', { items: this.data.items, address: this.data.address || {}, couponCode: this.data.couponCode, autoCoupon: this.data.autoCoupon, deliveryDate: this.data.deliveryDate })
      if (version !== this.data.quoteVersion) return
      this.setData({ couponCode: quote.couponCode || '', deliveryMin: quote.deliveryMin || deliveryStart(this.data.items), deliveryMax: quote.deliveryMax || chinaDate(30), quote: { ...quote, discountText: money(quote.discount || 0), subtotalText: money(quote.subtotal), freightText: money(quote.freight), totalText: money(quote.total) } })
    } catch (error) { if (version === this.data.quoteVersion) this.setData({ quote: null, quoteError: (error as Error).message }) }
    finally { if (version === this.data.quoteVersion) this.setData({ quoteBusy: false }) }
  },
  couponInput(event: WechatMiniprogram.Input) { this.setData({ couponInput: event.detail.value }) },
  async useCoupon() {
    if (this.data.submitting || this.data.redeemBusy || this.data.quoteBusy) return
    this.setData({ redeemBusy: true })
    try {
      const result = await callApi<{code: string}>('coupon.redeem', { code: this.data.couponInput })
      this.setData({ couponCode: result.code, autoCoupon: false, couponInput: '' })
      await this.refreshQuote()
    } catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
    finally { this.setData({ redeemBusy: false }) }
  },
  clearCoupon() { if (this.data.submitting || this.data.redeemBusy) return; this.setData({ autoCoupon: false, couponCode: '', couponInput: '' }, () => this.refreshQuote()) },
  matchCoupon() { if (this.data.submitting || this.data.redeemBusy) return; this.setData({ autoCoupon: true }, () => this.refreshQuote()) },
  wallet() {
    if (this.data.submitting || this.data.redeemBusy || this.data.quoteBusy) return
    wx.navigateTo({ url: `/pages/coupons/index?select=1&subtotal=${this.data.quote?.subtotal || 0}`, events: {
      couponSelected: (coupon: {code: string}) => { this.setData({ autoCoupon: false, couponCode: coupon.code }, () => this.refreshQuote()) },
    } })
  },
  delivery(event: WechatMiniprogram.PickerChange) { this.setData({ deliveryDate: String(event.detail.value) }, () => this.refreshQuote()) },
  clearDelivery() { this.setData({ deliveryDate: '' }, () => this.refreshQuote()) },
  address() { wx.navigateTo({ url: `/pages/address/index?select=1&id=${encodeURIComponent(this.data.address?.id || '')}`, events: { addressSelected: (address: Address) => { this.setData({ address }, () => this.refreshQuote()) } } }) },
  note(event: WechatMiniprogram.Input) { this.setData({ note: event.detail.value }) },
  agree() { this.setData({ agreed: !this.data.agreed }) },
  async submit() {
    if (this.data.createdOrderId) return wx.redirectTo({ url: `/pages/order-detail/index?id=${this.data.createdOrderId}` })
    if (this.data.quoteBusy || this.data.redeemBusy || !this.data.quote || this.data.quoteError) return wx.showToast({ title: '请等待有效报价后再支付', icon: 'none' })
    if (!this.data.address) return wx.showToast({ title: '请填写收货地址', icon: 'none' })
    if (this.data.quote?.blockedReason) return wx.showToast({ title: this.data.quote.blockedReason, icon: 'none' })
    if (!this.data.agreed) return wx.showToast({ title: '请阅读并同意购买须知', icon: 'none' })
    if (this.data.submitting) return
    this.setData({ submitting: true })
    try {
      const payload = { items: this.data.items, address: this.data.address, note: this.data.note, couponCode: this.data.couponCode, deliveryDate: this.data.deliveryDate }
      const fingerprint = checkoutIntent(payload)
      const storageKey = getApp().globalData.demoMode ? 'chiji_demo_checkout_attempt' : 'chiji_checkout_attempt'
      const previous = wx.getStorageSync(storageKey)
      const attempt = previous?.fingerprint === fingerprint ? previous : { fingerprint, key: purchaseKey() }
      wx.setStorageSync(storageKey, attempt)
      const order = await callApi<Order>('order.create', { ...payload, idempotencyKey: attempt.key })
      this.setData({ createdOrderId: order.id })
      // Once an order exists, payment retries belong on its detail page.
      wx.removeStorageSync('chiji_checkout_items')
      wx.removeStorageSync(storageKey)
      try { await payOrder(order.id) }
      catch (error) { wx.showToast({ title: (error as Error).message, icon: 'none' }) }
      wx.redirectTo({ url: `/pages/order-detail/index?id=${order.id}` })

    } catch (error) { wx.showToast({ title: (error as Error).message, icon: 'none' }) }
    finally { this.setData({ submitting: false }) }
  },
})
