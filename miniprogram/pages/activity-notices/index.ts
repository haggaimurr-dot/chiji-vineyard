import { callApi } from '../../utils/api'
import { noticeTime, type ActivityNotice, type ActivityNoticeList } from '../../utils/activity-notice'

Page({
  data: { items: [] as Array<ActivityNotice & { timeText: string }>, unread: 0, hasMore: false, loading: false, error: '', version: 0, opening: '', nextOffset: 0, failedAppend: false, demoMode: getApp<IAppOption>().globalData.demoMode },
  onShow() { return this.load(false) },
  onHide() { this.data.version++ },
  onUnload() { this.data.version++ },
  onPullDownRefresh() { return this.load(false).finally(() => wx.stopPullDownRefresh()) },
  onReachBottom() { if (this.data.hasMore && !this.data.loading && !this.data.error) return this.load(true) },
  retry() { return this.load(this.data.failedAppend) },
  more() { if (this.data.hasMore) return this.load(true) },
  async load(append: boolean) {
    if (append && this.data.loading) return
    const version = this.data.version + 1
    const offset = append ? this.data.nextOffset : 0
    this.setData({ version, loading: true, error: '', failedAppend: append })
    try {
      const result = await callApi<ActivityNoticeList>('activity.notice.list', { offset, limit: 20 })
      if (version !== this.data.version) return
      const incoming = result.items.map(n => ({ ...n, timeText: noticeTime(n.createdAt) }))
      const items = append ? [...this.data.items, ...incoming] : incoming
      this.setData({ items: items.filter((n, i) => items.findIndex(other => other.id === n.id) === i), unread: result.unread, hasMore: result.hasMore, nextOffset: offset + incoming.length })
    } catch (error) {
      if (version === this.data.version) this.setData({ error: (error as Error).message || '通知加载失败，请重试' })
    } finally { if (version === this.data.version) this.setData({ loading: false }) }
  },
  async openActivity(event: WechatMiniprogram.BaseEvent) {
    const notice = this.data.items.find(n => n.id === event.currentTarget.dataset.id)
    if (!notice || this.data.opening || this.data.loading) return
    const version = this.data.version
    this.setData({ opening: notice.id })
    try {
      await callApi('activity.notice.read', { id: notice.id })
      if (version !== this.data.version) return
      this.setData({ items: this.data.items.map(n => n.id === notice.id ? { ...n, read: true } : n), unread: Math.max(0, this.data.unread - (notice.read ? 0 : 1)) })
      wx.navigateTo({ url: `/pages/activity/index?id=${encodeURIComponent(notice.activityId)}` })
    } catch (error) { wx.showToast({ title: (error as Error).message || '操作失败，请重试', icon: 'none' }) }
    finally { this.setData({ opening: '' }) }
  },
})
