import { callApi } from '../../utils/api'
import { requestRestock, saveRestockGrant, savePriceDropGrant, RestockConfig } from '../../utils/restock'
import { available } from '../../utils/product'
import { money } from '../../utils/format'
import { promptTargetPrice } from '../../utils/reminder-target'
import type { Product } from '../../utils/types'

interface HistoryEntry { id: string; productId: string; productName: string; kind: string; skuName: string; currentPrice: number; targetPrice: number; status: string; detail: string; createdAt: string }
const statusText: Record<string, string> = { SENT: '微信提醒已发送', FAILED: '发送失败', BLOCKED: '微信订阅授权已失效', SENDING: '发送结果待核实', UNCERTAIN: '发送结果待核实', IN_APP: '小程序内提醒（演示）' }

Page({
  data: { kind: 'favorite', products: [] as Array<Product & { priceText: string; ready: boolean; baselineText: string; currentText: string; dropText: string; targetText: string }>, loading: true, error: '', busy: false, restockConfig: null as RestockConfig | null, priceDropConfig: null as RestockConfig | null, subscriptionError: '', history: [] as Array<HistoryEntry & { timeText: string; statusText: string; priceText: string; targetText: string }>, historyMore: false, historyLoading: false, historyError: '', requestVersion: 0 },
  onLoad(options: Record<string, string>) {
    this.setData({ kind: ['restock', 'pricedrop', 'history'].includes(options.kind) ? options.kind : 'favorite' })
    wx.setNavigationBarTitle({ title: this.data.kind === 'history' ? '提醒历史' : this.data.kind === 'restock' ? '到货提醒' : this.data.kind === 'pricedrop' ? '降价提醒' : '我的收藏' })
  },
  onShow() { return this.loadProducts() },
  onPullDownRefresh() { this.loadProducts().finally(() => wx.stopPullDownRefresh()) },
  async loadProducts() {
    if (this.data.kind === 'history') return this.loadHistory(true)
    this.setData({ loading: true, error: '' })
    try {
      const [products, restockConfig, priceDropConfig] = await Promise.all([
        callApi<Product[]>('product.attention.list', { kind: this.data.kind }),
        callApi<RestockConfig>('product.restockSubscription.config').catch(() => null),
        callApi<RestockConfig>('product.priceDropSubscription.config').catch(() => null),
      ])
      this.setData({ restockConfig, priceDropConfig })
      this.setData({ products: products.map(p => ({ ...p, ready: available(p), priceText: p.skus.length ? money(Math.min(...p.skus.map(s => s.price))) : '', baselineText: p.priceDrop ? money(p.priceDrop.baselinePrice) : '', targetText: p.priceDrop?.targetPrice ? money(p.priceDrop.targetPrice) : '', currentText: p.priceDrop?.skuName ? money(p.priceDrop.currentPrice) : '', dropText: p.priceDrop?.dropped && p.priceDrop.baselinePrice > p.priceDrop.currentPrice ? money(p.priceDrop.baselinePrice - p.priceDrop.currentPrice) : '' })) })
    } catch (error) { this.setData({ error: (error as Error).message }) }
    finally { this.setData({ loading: false }) }
  },
  onHide() { this.setData({ requestVersion: this.data.requestVersion + 1, historyLoading: false }) },
  onUnload() { this.onHide() },
  onReachBottom() { if (this.data.kind === 'history' && this.data.historyMore) return this.loadHistory(false) },
  async loadHistory(reset = true) {
    if (!reset && this.data.historyLoading) return
    const version = this.data.requestVersion + 1
    const offset = reset ? 0 : this.data.history.length
    this.setData({ requestVersion: version, historyLoading: true, historyError: '', ...(reset ? { loading: true, error: '' } : {}) })
    try {
      const result = await callApi<{ items: HistoryEntry[]; hasMore: boolean }>('product.attention.history', { offset, limit: 20 })
      if (version !== this.data.requestVersion) return
      const items = result.items.map(h => ({ ...h, timeText: new Date(new Date(h.createdAt).getTime() + 8 * 3600000).toISOString().slice(0, 16).replace('T', ' '), statusText: statusText[h.status] || '结果待核实', priceText: money(h.currentPrice), targetText: h.targetPrice ? money(h.targetPrice) : '' }))
      this.setData({ history: reset ? items : [...this.data.history, ...items], historyMore: result.hasMore })
    } catch (e) { if (version === this.data.requestVersion) this.setData(reset ? { error: (e as Error).message } : { historyError: (e as Error).message }) }
    finally { if (version === this.data.requestVersion) this.setData({ loading: false, historyLoading: false }) }
  },
  moreHistory() { return this.loadHistory(false) },
  openHistory() { wx.navigateTo({ url: '/pages/attention/index?kind=history' }) },
  async editTargetPrice(event: WechatMiniprogram.BaseEvent) {
    if (this.data.busy) return
    const product = this.data.products.find(p => p.id === event.currentTarget.dataset.id)
    if (!product?.priceDrop) return
    this.setData({ busy: true })
    try {
      const targetPrice = event.currentTarget.dataset.clear ? 0 : await promptTargetPrice(product.priceDrop.targetPrice)
      if (targetPrice === null) return
      await callApi('product.priceDropTarget.set', { productId: product.id, targetPrice })
      await this.loadProducts()
      wx.showToast({ title: '提醒条件已更新', icon: 'success' })
    } catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
    finally { this.setData({ busy: false }) }
  },
  async remove(event: WechatMiniprogram.BaseEvent) {
    if (this.data.busy) return
    const productId = event.currentTarget.dataset.id
    this.setData({ busy: true })
    try {
      await callApi('product.attention.set', { productId, kind: this.data.kind, enabled: false })
      this.setData({ products: this.data.products.filter(p => p.id !== productId) })
      wx.showToast({ title: '已取消', icon: 'success' })
    } catch (error) { wx.showToast({ title: (error as Error).message, icon: 'none' }) }
    finally { this.setData({ busy: false }) }
  },
  async subscribeRestock(event: WechatMiniprogram.BaseEvent) {
    const config = this.data.restockConfig
    if (this.data.busy || !config?.enabled) return
    const productId = event.currentTarget.dataset.id
    this.setData({ busy: true, subscriptionError: '' })
    try {
      if (!await requestRestock(config)) {
        this.setData({ subscriptionError: '未开启微信提醒，仍可在此查看到货状态。' }); return
      }
      const result = await saveRestockGrant(productId, config)
      this.setData({ products: this.data.products.map(p => p.id === productId ? { ...p, restockPushStatus: result.status } : p) })
      wx.showToast({ title: '已订阅微信到货提醒', icon: 'success' })
    } catch (e) { this.setData({ subscriptionError: (e as Error).message || '订阅保存失败，请重试' }) }
    finally { this.setData({ busy: false }) }
  },
  async subscribePriceDrop(event: WechatMiniprogram.BaseEvent) {
    const config = this.data.priceDropConfig
    if (this.data.busy || !config?.enabled) return
    const productId = event.currentTarget.dataset.id
    this.setData({ busy: true, subscriptionError: '' })
    try {
      if (!await requestRestock(config)) { this.setData({ subscriptionError: '未开启微信提醒，仍可在此查看价格变化。' }); return }
      const result = await savePriceDropGrant(productId, config)
      this.setData({ products: this.data.products.map(p => p.id === productId ? { ...p, priceDrop: { ...p.priceDrop!, pushStatus: result.status } } : p) })
      wx.showToast({ title: '已订阅微信降价提醒', icon: 'success' })
    } catch (e) { this.setData({ subscriptionError: (e as Error).message || '订阅保存失败，请重试' }) }
    finally { this.setData({ busy: false }) }
  },
  openProduct(event: WechatMiniprogram.BaseEvent) { wx.navigateTo({ url: `/pages/product/index?id=${encodeURIComponent(event.currentTarget.dataset.id)}` }) },
  goShop() { wx.switchTab({ url: '/pages/shop/index' }) },
})
