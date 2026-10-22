import { callApi } from '../../utils/api'
import type { ShippingNotice } from '../../utils/types'

Page({
  data: { items: [] as ShippingNotice[], unread: 0, loading: false, error: '', demo: getApp().globalData.demoMode },
  onShow() { return this.load() },
  onPullDownRefresh() { this.load().finally(() => wx.stopPullDownRefresh()) },
  async load() {
    if (this.data.loading) return
    this.setData({ loading: true, error: '' })
    try { this.setData(await callApi<{ items: ShippingNotice[]; unread: number }>('shippingNotice.list')) }
    catch (e) { this.setData({ error: (e as Error).message }) }
    finally { this.setData({ loading: false }) }
  },
  open(event: WechatMiniprogram.BaseEvent) {
    wx.navigateTo({ url: `/pages/order-detail/index?id=${encodeURIComponent(event.currentTarget.dataset.id)}` })
  },
})
