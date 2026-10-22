import { defaultHome } from '../../data/home'
import { callApi } from '../../utils/api'
import { activityStatus, reservationActions, reservationData } from '../../utils/activity'
import type { Activity } from '../../utils/activity'
import type { HomeConfig, Product } from '../../utils/types'

Page({
  data: {
    activityId: '', legacyIndex: -1, legacyTitle: '',
    activity: null as Activity | null,
    home: { ...defaultHome, activities: [] } as HomeConfig,
    status: '', reserved: false, productAvailable: false, productError: '',
    paragraphs: [] as string[],
    coverFailed: false,
    subscription: null as { enabled: boolean; templateId: string; status: string } | null,
    subscriptionBusy: false, subscriptionError: '',
    loading: true, error: '', requestVersion: 0,
    demoMode: getApp<IAppOption>().globalData.demoMode,
    ...reservationData,
  },
  onLoad(options: Record<string, string>) {
    this.setData({ activityId: options.id || '', legacyIndex: options.index === undefined ? -1 : Number(options.index), legacyTitle: options.title || '' })
  },
  onShow() { return this.loadActivity() },
  onHide() { this.data.requestVersion++ },
  onUnload() { this.data.requestVersion++ },
  onPullDownRefresh() { return this.loadActivity().finally(() => wx.stopPullDownRefresh()) },
  async loadActivity() {
    const version = this.data.requestVersion + 1
    this.setData({ loading: true, error: '', activity: null, subscription: null, subscriptionError: '', productAvailable: false, productError: '', coverFailed: false, requestVersion: version })
    try {
      if (!this.data.activityId && (!Number.isInteger(this.data.legacyIndex) || this.data.legacyIndex < 0 || !this.data.legacyTitle)) throw new Error('活动链接无效，请返回首页重新选择')
      const results = await Promise.allSettled([callApi<HomeConfig>('home.get'), callApi<Product[]>('product.list')])
      if (version !== this.data.requestVersion) return
      if (results[0].status === 'rejected') throw results[0].reason
      const home = results[0].value
      if (!home || !Array.isArray(home.activities)) throw new Error('活动不存在或已下线')
      const activity = this.data.activityId ? home.activities.find(a => a.id === this.data.activityId) : home.activities[this.data.legacyIndex]
      if (!activity || (!this.data.activityId && activity.title !== this.data.legacyTitle)) throw new Error('活动不存在或已变更，请返回首页重新选择')
      const products = results[1].status === 'fulfilled' && Array.isArray(results[1].value) ? results[1].value : null
      this.setData({
        home, activity, activityId: activity.id || '', status: activityStatus(activity),
        paragraphs: activity.content.split(/\r?\n/).filter(line => line.trim()),
        productAvailable: !!products?.some(p => p.id === activity.productId && p.skus.length > 0),
        productError: products ? '' : '关联商品加载失败，请下拉刷新重试',
        loading: false,
      })
      wx.setNavigationBarTitle({ title: activity.title })
      await this.loadReservations(version)
      await this.loadSubscription(version)
    } catch (error) {
      if (version === this.data.requestVersion) this.setData({ error: (error as Error).message || '活动加载失败，请重试', activity: null })
    } finally {
      if (version === this.data.requestVersion) this.setData({ loading: false })
    }
  },
  ...reservationActions,
  async loadSubscription(expectedVersion?: number) {
    const version = expectedVersion === undefined ? this.data.requestVersion : expectedVersion
    if (this.data.demoMode || !this.data.activityId) return
    try {
      const subscription = await callApi<{ enabled: boolean; templateId: string; status: string }>('activity.subscription.config', { activityId: this.data.activityId })
      if (version === this.data.requestVersion) this.setData({ subscription, subscriptionError: '' })
    } catch {
      if (version === this.data.requestVersion) this.setData({ subscription: null, subscriptionError: '微信提醒加载失败，点击重试' })
    }
  },
  retrySubscription() { return this.loadSubscription() },
  async subscribeActivity() {
    const config = this.data.subscription
    if (this.data.subscriptionBusy || !config?.enabled || !this.data.reserved || this.data.reservationBusy || this.data.loading) return
    if (['SENT', 'SENDING', 'UNCERTAIN'].includes(config.status)) return
    const version = this.data.requestVersion
    const activityId = this.data.activityId
    this.setData({ subscriptionBusy: true, subscriptionError: '' })
    try {
      // Invoke from the user tap, with template configuration already loaded.
      const result = await wx.requestSubscribeMessage({ tmplIds: [config.templateId] })
      if (result[config.templateId] !== 'accept') {
        if (version === this.data.requestVersion) this.setData({ subscriptionError: '未开启微信提醒，预约仍然有效' })
        return
      }
      const saved = await callApi<{ status: string }>('activity.subscription.accept', { activityId, templateId: config.templateId })
      if (version === this.data.requestVersion) this.setData({ subscription: { ...config, status: saved.status } })
    } catch (error) {
      if (version === this.data.requestVersion) this.setData({ subscriptionError: (error as Error).message || '微信提醒未保存，请重试' })
    } finally { this.setData({ subscriptionBusy: false }) }
  },
  coverError(event: WechatMiniprogram.BaseEvent) {
    if (Number(event.currentTarget.dataset.version) === this.data.requestVersion) this.setData({ coverFailed: true })
  },
  onShareAppMessage() {
    const activity = this.data.activity
    if (!activity || this.data.loading || this.data.error) return { title: '迟记葡萄牙 · 果园活动', path: '/pages/home/index' }
    const query = activity.id ? `id=${encodeURIComponent(activity.id)}` : `index=${this.data.legacyIndex}&title=${encodeURIComponent(activity.title)}`
    return { title: activity.title, path: `/pages/activity/index?${query}`, ...(activity.cover && !this.data.coverFailed ? { imageUrl: activity.cover } : {}) }
  },
  openProduct() {
    const activity = this.data.activity
    if (this.data.loading || this.data.error || !activity || this.data.status !== '进行中' || !this.data.productAvailable) return
    wx.navigateTo({ url: `/pages/product/index?id=${encodeURIComponent(activity.productId)}` })
  },
  goHome() { wx.switchTab({ url: '/pages/home/index' }) },
})
