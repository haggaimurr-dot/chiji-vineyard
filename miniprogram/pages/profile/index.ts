import { callApi } from '../../utils/api'
import { closedKey } from '../../utils/personal-data'
import { available } from '../../utils/product'
import type { OrderSearchResult, Product, UserProfile } from '../../utils/types'
import { openVineyardGame } from '../../utils/vineyard-entry'

Page({
  wallet() { wx.navigateTo({ url: "/pages/coupons/index" }) },
  data: { user: { nickname: '葡萄好友' } as UserProfile, counts: { pending: 0, paid: 0, shipping: 0, refund: 0 }, arrivals: 0, priceDrops: 0, shippingUnread: 0, activityUnread: 0, vineyardOpening: false },
  async onShow() {
    // 登录态缓存：先展示缓存的用户信息，再刷新，避免登录等待期间的空白
    const cachedUser = wx.getStorageSync('chiji_user_cache')
    if (cachedUser && cachedUser.nickname) this.setData({ user: cachedUser })
    if (wx.getStorageSync(closedKey) === true) {
      this.setData({ user: { id: '', nickname: '账号已注销' }, counts: { pending: 0, paid: 0, shipping: 0, refund: 0 }, arrivals: 0, priceDrops: 0, shippingUnread: 0, activityUnread: 0 })
      return
    }
    try { const notices = await callApi<{unread: number}>('activity.notice.list', { limit: 1 }); this.setData({ activityUnread: notices.unread }) }
    catch (_) { this.setData({ activityUnread: 0 }) }
    try {
      const [bootstrap, pending, paid, shipping, refund] = await Promise.all([
        callApi<any>('bootstrap'), ...['pending', 'paid', 'shipping', 'refund'].map(filter => callApi<OrderSearchResult>('order.search', { filter, limit: 1 })),
      ])
      this.setData({ user: bootstrap.user, counts: { pending: pending.total, paid: paid.total, shipping: shipping.total, refund: refund.total } })
      if (bootstrap.user && bootstrap.user.nickname) wx.setStorageSync('chiji_user_cache', bootstrap.user)
    } catch (_) { /* 首页仍可使用 */ }
    try {
      const reminders = await callApi<Product[]>('product.attention.list', { kind: 'restock' })
      this.setData({ arrivals: reminders.filter(available).length })
    } catch (_) { this.setData({ arrivals: 0 }) }
    try {
      const reminders = await callApi<Product[]>('product.attention.list', { kind: 'pricedrop' })
      this.setData({ priceDrops: reminders.filter(p => p.priceDrop?.dropped).length })
    } catch (_) { this.setData({ priceDrops: 0 }) }
    try { const notices = await callApi<{unread: number}>('shippingNotice.list'); this.setData({ shippingUnread: notices.unread }) }
    catch (_) { this.setData({ shippingUnread: 0 }) }
  },
  orders(event: WechatMiniprogram.BaseEvent) { wx.navigateTo({ url: `/pages/orders/index?filter=${event.currentTarget.dataset.filter || 'all'}` }) },
  attention(event: WechatMiniprogram.BaseEvent) { wx.navigateTo({ url: `/pages/attention/index?kind=${event.currentTarget.dataset.kind}` }) },
  productMemory(event: WechatMiniprogram.BaseEvent) { wx.navigateTo({ url: `/pages/product-memory/index?kind=${event.currentTarget.dataset.kind}` }) },
  trace() { wx.navigateTo({url:'/pages/trace/index'}) },
  address() { wx.navigateTo({ url: '/pages/address/index' }) },
  shippingNotices() { wx.navigateTo({ url: '/pages/shipping-notices/index' }) },
  notifySettings() { wx.navigateTo({ url: '/pages/notify-settings/index' }) },
  activityNotices() { wx.navigateTo({ url: '/pages/activity-notices/index' }) },
  policy() { wx.navigateTo({url:'/pages/privacy/index'}) },
  personalData() { wx.navigateTo({url:'/pages/personal-data/index'}) },
  async goVineyardGame() {
    if (this.data.vineyardOpening) return
    this.setData({ vineyardOpening: true })
    try { await openVineyardGame('profile') }
    finally { this.setData({ vineyardOpening: false }) }
  },
  classroom(event: WechatMiniprogram.BaseEvent) { wx.setStorageSync('chiji_classroom_view', event.currentTarget.dataset.view); wx.switchTab({ url: '/pages/classroom/index' }) },
})
