import { callApi } from '../../utils/api'
import { money } from '../../utils/format'
import { purchaseLimit } from '../../utils/product'
import type { Address, CartItem, PriceQuote } from '../../utils/types'

Page({
  wallet() { wx.navigateTo({ url: "/pages/coupons/index" }) },
  data: { items: [] as Array<any>, invalidItems: [] as Array<any>, totalText: '0.00', allSelected: false, selectedCount: 0, managing: false, busy: false, hint: '', canTopUp: false, hintVersion: 0 },
  onShow() { return this.load() },
  async load() {
    if (this.data.busy) return
    this.setData({ busy: true })
    try { this.render(await callApi<CartItem[]>('cart.get')) }
    catch (error) { this.showError(error) }
    finally { this.setData({ busy: false }) }
  },
  render(items: CartItem[]) {
    const invalidItems: any[] = [], hydrated: any[] = []
    for (const x of items) {
      const reason = !x.product ? '商品已下架或删除' : !x.sku ? '规格已失效' : x.sku.stock <= 0 ? '暂时售罄' : purchaseLimit(x.sku) <= 0 ? '暂不可购买' : ''
      if (reason) invalidItems.push({ ...x, reason, nameText: x.product?.name || x.name || '失效商品', specText: x.sku?.name || x.skuName || x.skuId })
      else hydrated.push({ ...x, priceText: money(x.sku!.price), lineText: money(x.sku!.price * x.quantity), quantityWarning: x.quantity > purchaseLimit(x.sku) ? '库存或限购已变化，请调整数量' : '' })
    }
    const selected = hydrated.filter(x => x.selected)
    const total = selected.reduce((sum, x) => sum + x.sku.price * x.quantity, 0)
    this.setData({ items: hydrated, invalidItems, totalText: money(total), selectedCount: selected.length, allSelected: !!hydrated.length && hydrated.every(x => x.selected) })
    void this.refreshHint()
  },
  async refreshHint() {
    const version = this.data.hintVersion + 1
    const selected = this.data.items.filter((x: CartItem) => x.selected)
    this.setData({ hintVersion: version, hint: '', canTopUp: false })
    if (!selected.length || selected.some(x => x.quantity > purchaseLimit(x.sku))) return
    try {
      const addresses = await callApi<Address[]>('address.list')
      if (version !== this.data.hintVersion) return
      const address = addresses.find(x => x.isDefault) || addresses[0]
      const quote = await callApi<PriceQuote>('order.quote', { items: selected.map(x => ({ productId: x.productId, skuId: x.skuId, quantity: x.quantity })), address: address || {} })
      if (version !== this.data.hintVersion) return
      const scope = address ? '当前收货地区' : '默认配送规则'
      if (quote.blockedReason) {
        this.setData({ hint: quote.blockedReason })
      } else if (Number.isFinite(quote.freeShippingThreshold) && Number.isFinite(quote.subtotal)) {
        const gap = Math.max(0, quote.freeShippingThreshold - quote.subtotal)
        const extra = quote.surcharge ? '，地区附加运费仍需支付' : ''
        this.setData({ hint: gap > 0 && quote.freight > (quote.surcharge || 0) ? `${scope}：再购 ¥${money(gap)} 可免基础运费${extra}` : `${scope}：已免基础运费${extra}`, canTopUp: gap > 0 && quote.freight > (quote.surcharge || 0) })
      }
    } catch (_) {
      if (version === this.data.hintVersion) this.setData({ hint: '凑单提示暂不可用，运费以结算页为准', canTopUp: false })
    }
  },
  async update(event: WechatMiniprogram.BaseEvent, delta = 0) {
    if (this.data.busy) return
    const item = this.data.items[Number(event.currentTarget.dataset.index)]
    if (!item) return
    const quantity = Math.max(0, Math.min(item.sku.stock, item.sku.limitPerOrder, item.quantity + delta))
    this.setData({ busy: true })
    try { this.render(await callApi<CartItem[]>('cart.update', { productId: item.productId, skuId: item.skuId, quantity, selected: item.selected })) }
    catch (error) { this.showError(error) }
    finally { this.setData({ busy: false }) }
  },
  minus(event: WechatMiniprogram.BaseEvent) { this.update(event, -1) },
  plus(event: WechatMiniprogram.BaseEvent) { this.update(event, 1) },
  async toggle(event: WechatMiniprogram.BaseEvent) {
    if (this.data.busy) return
    const item = this.data.items[Number(event.currentTarget.dataset.index)]
    if (!item) return
    this.setData({ busy: true })
    try { this.render(await callApi<CartItem[]>('cart.update', { productId: item.productId, skuId: item.skuId, quantity: item.quantity, selected: !item.selected })) }
    catch (error) { this.showError(error) }
    finally { this.setData({ busy: false }) }
  },
  async toggleAll() {
    if (this.data.busy) return
    const items = [...this.data.items], selected = !this.data.allSelected
    this.setData({ busy: true })
    try {
      if (items.length) this.render(await callApi<CartItem[]>('cart.batch', { operation: 'select', skuIds: items.map(item => item.skuId), selected }))
    } catch (error) { this.showError(error) }
    finally { this.setData({ busy: false }) }
  },
  toggleManage() {
    if (!this.data.busy) this.setData({ managing: !this.data.managing })
  },
  deleteSelected() { return this.batchAction(false) },
  moveToFavorites() { return this.batchAction(true) },
  async clearInvalid() {
    if (this.data.busy || !this.data.invalidItems.length) return
    const skuIds = this.data.invalidItems.map((x: CartItem) => x.skuId)
    this.setData({ busy: true })
    try {
      const result = await wx.showModal({ title: '清理失效商品', content: `确定清理 ${skuIds.length} 项失效商品吗？`, confirmText: '清理', confirmColor: '#a33b3b' })
      if (!result.confirm) return
      this.render(await callApi<CartItem[]>('cart.batch', { operation: 'delete', skuIds }))
      wx.showToast({ title: '已清理失效商品', icon: 'success' })
    } catch (error) {
      try { this.render(await callApi<CartItem[]>('cart.get')) } catch (_) {}
      this.showError(error)
    } finally { this.setData({ busy: false }) }
  },
  async batchAction(move: boolean) {
    if (this.data.busy) return
    const selected = this.data.items.filter((item: CartItem) => item.selected)
    if (!selected.length) return wx.showToast({ title: '请先选择商品', icon: 'none' })
    this.setData({ busy: true })
    try {
      if (!move) {
        const result = await wx.showModal({ title: '删除商品', content: `确定删除选中的 ${selected.length} 项商品吗？`, confirmText: '删除', confirmColor: '#a33b3b' })
        if (!result.confirm) return
      }
      this.render(await callApi<CartItem[]>('cart.batch', { operation: move ? 'favorite' : 'delete', skuIds: selected.map((item: CartItem) => item.skuId) }))
      wx.showToast({ title: move ? '已移入收藏' : '已删除', icon: 'success' })
    } catch (error) {
      // Refresh after an interrupted request so completed changes remain visible.
      try { this.render(await callApi<CartItem[]>('cart.get')) } catch (_) {}
      this.showError(error)
    } finally { this.setData({ busy: false }) }
  },
  showError(error: unknown) {
    wx.showToast({ title: error instanceof Error ? error.message : '操作失败，请重试', icon: 'none' })
  },
  checkout() {
    if (this.data.busy) return
    const selected = this.data.items.filter((x: CartItem) => x.selected)
    if (!selected.length) return wx.showToast({ title: '请先选择商品', icon: 'none' })
    if (selected.some(x => !Number.isInteger(x.quantity) || x.quantity < 1 || x.quantity > purchaseLimit(x.sku))) return wx.showToast({ title: '请先调整商品数量', icon: 'none' })
    wx.setStorageSync('chiji_checkout_items', selected.map((x: CartItem) => ({ productId: x.productId, skuId: x.skuId, quantity: x.quantity, selected: true })))
    wx.navigateTo({ url: '/pages/checkout/index' })
  },
  goShop() { wx.switchTab({ url: '/pages/shop/index' }) },
})

