import { callApi } from '../../utils/api'
import { buyAgain } from '../../utils/orders'
import { money, orderPresentation } from '../../utils/format'
import { orderFilters } from '../../utils/order-filter'
import type { OrderSearchResult } from '../../utils/types'

Page({
  data: {
    filter: 'all', keyword: '', queryKeyword: '', loading: false, error: '', failedMore: false,
    hasMore: false, offset: 0, total: 0, requestVersion: 0, rebuyId: '',
    tabs: [{ key: 'all', label: '全部' }, { key: 'pending', label: '待付款' }, { key: 'paid', label: '待发货' }, { key: 'shipping', label: '运输中' }, { key: 'received', label: '已收货' }, { key: 'completed', label: '已完成' }, { key: 'cancelled', label: '已取消' }, { key: 'closed', label: '已关闭' }, { key: 'refund', label: '退款 / 售后' }],
    orders: [] as any[],
  },
  onLoad(options: Record<string, string>) { this.setData({ filter: orderFilters.includes(options.filter) ? options.filter : 'all' }) },
  onShow() { return this.load(false, false) },
  onUnload() { this.setData({ requestVersion: this.data.requestVersion + 1 }) },
  onPullDownRefresh() { return this.load(false, false).finally(() => wx.stopPullDownRefresh()) },
  onReachBottom() { return this.loadMore() },
  loadMore() { if (this.data.hasMore && !this.data.loading && !this.data.error) return this.load(true) },
  async load(more = false, submit = true) {
    if (more && (this.data.loading || !this.data.hasMore)) return
    const version = this.data.requestVersion + 1, offset = more ? this.data.offset : 0
    const keyword = !more && submit ? this.data.keyword.trim() : this.data.queryKeyword
    const filter = this.data.filter
    this.setData({ requestVersion: version, queryKeyword: keyword, loading: true, error: '', failedMore: false,
      ...(more ? {} : { orders: [], offset: 0, total: 0, hasMore: false }) })
    try {
      const result = await callApi<OrderSearchResult>('order.search', { keyword, filter, offset, limit: 20 })
      if (version !== this.data.requestVersion) return
      const formatted = result.items.map(o => ({ ...o, ...orderPresentation(o), totalText: money(o.quote.total), firstCover: o.items[0]?.product?.cover || o.items[0]?.cover, firstName: o.items[0]?.product?.name || o.items[0]?.name || '葡萄商品', count: o.items.reduce((n, i) => n + i.quantity, 0) }))
      const existing = new Set(more ? this.data.orders.map(o => o.id) : [])
      const orders = more ? [...this.data.orders, ...formatted.filter(o => !existing.has(o.id))] : formatted
      const nextOffset = result.offset + result.items.length
      this.setData({ orders, offset: nextOffset, total: result.total, hasMore: nextOffset < result.total && result.items.length > 0 })
    } catch (e) {
      if (version === this.data.requestVersion) this.setData({ error: (e as Error).message || '订单加载失败，请重试', failedMore: more })
    } finally { if (version === this.data.requestVersion) this.setData({ loading: false }) }
  },
  input(event: WechatMiniprogram.Input) { this.setData({ keyword: event.detail.value }) },
  search() { return this.load() },
  clearSearch() { this.setData({ keyword: '' }); return this.load() },
  retry() { return this.load(this.data.failedMore, false) },
  tab(event: WechatMiniprogram.BaseEvent) {
    const filter = event.currentTarget.dataset.key
    if (!orderFilters.includes(filter) || filter === this.data.filter) return
    this.setData({ filter }); return this.load()
  },
  open(event: WechatMiniprogram.BaseEvent) { wx.navigateTo({ url: `/pages/order-detail/index?id=${encodeURIComponent(event.currentTarget.dataset.id)}` }) },
  goReview(event: WechatMiniprogram.BaseEvent) { wx.navigateTo({ url: `/pages/order-detail/index?id=${encodeURIComponent(event.currentTarget.dataset.id)}&tab=review` }) },
  async rebuy(event: WechatMiniprogram.BaseEvent) {
    if (this.data.rebuyId) return
    const order = this.data.orders.find(o => o.id === event.currentTarget.dataset.id)
    if (!order) return
    this.setData({ rebuyId: order.id })
    try { await buyAgain(order) }
    catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
    finally { this.setData({ rebuyId: '' }) }
  },
  goShop() { wx.switchTab({ url: '/pages/shop/index' }) },
})
