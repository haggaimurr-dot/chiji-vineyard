import { callApi } from './api'
import type { HomeConfig } from './types'

export interface ReservationState { activityId: string; status: 'NONE' | 'RESERVED' | 'WAITLISTED'; confirmed: number; waiting: number; position: number }
export type Activity = NonNullable<HomeConfig['activities']>[number]
export const todayInChina = () => new Date(Date.now() + 8 * 3600000).toISOString().slice(0, 10)
export const activityStatus = (activity: Activity, today = todayInChina()) => today < activity.startDate ? '即将开始' : today > activity.endDate ? '已结束' : '进行中'
export function activityReminders(activities: Activity[], reservations: string[], today = todayInChina()) {
  return activities.filter(a => a.id && reservations.includes(a.id) && today <= a.endDate)
    .sort((a, b) => a.startDate.localeCompare(b.startDate))
    .map(a => {
      const days = Math.round((Date.parse(`${a.startDate}T00:00:00+08:00`) - Date.parse(`${today}T00:00:00+08:00`)) / 86400000)
      return { ...a, reminder: days > 1 ? `还有${days}天开始` : days === 1 ? '明天开始' : days === 0 ? '今天开始' : '活动进行中' }
    })
}
export const reservationData = {
  reservations: [] as string[],
  waitlisted: [] as string[], reservationStates: [] as ReservationState[],
  waitlistedActivity: false, queuePosition: 0, capacityText: '', reservationFull: false,
  reservationActivities: [] as Array<Activity & { reserved: boolean; ended: boolean; waitlisted: boolean; position: number; full: boolean; capacityText: string }>,
  activityReminders: [] as ReturnType<typeof activityReminders>,
  unavailableReservations: [] as string[],
  reservationLoading: false, reservationError: '', reservationBusy: '', reservationVersion: 0,
}
interface ReservationPage {
  data: typeof reservationData & {
    home: HomeConfig; activityId?: string; requestVersion: number;
    loading: boolean; error: string; demoMode: boolean;
  }
  setData(updates: Record<string, unknown>): void
  refreshReservationActivities(): void
  loadReservations(homeVersion?: unknown): Promise<void>
}
export const reservationActions = {
  refreshReservationActivities(this: ReservationPage) {
    const today = todayInChina()
    const active = [...this.data.reservations, ...this.data.waitlisted]
    const state = (id: string) => this.data.reservationStates.find(s => s.activityId === id)
    const capacity = (a: Activity) => a.capacity ? `名额 ${state(a.id || '')?.confirmed || 0}/${a.capacity} · 候补 ${state(a.id || '')?.waiting || 0} 人` : '不限额'
    const activity = this.data.home.activities?.find(a => a.id === this.data.activityId)
    this.setData({
      activityReminders: activityReminders(this.data.home.activities || [], this.data.reservations, today),
      reserved: this.data.reservations.includes(this.data.activityId || ''),
      waitlistedActivity: this.data.waitlisted.includes(this.data.activityId || ''),
      queuePosition: state(this.data.activityId || '')?.position || 0,
      capacityText: activity ? capacity(activity) : '',
      reservationFull: !!activity?.capacity && (state(activity.id || '')?.confirmed || 0) >= activity.capacity,
      reservationActivities: (this.data.home.activities || []).filter(a => a.id && (a.reservationEnabled || active.includes(a.id))).map(a => ({ ...a, reserved: this.data.reservations.includes(a.id!), ended: today > a.endDate, waitlisted: this.data.waitlisted.includes(a.id!), position: state(a.id!)?.position || 0, full: !!a.capacity && (state(a.id!)?.confirmed || 0) >= a.capacity, capacityText: capacity(a) })),
      unavailableReservations: active.filter(id => !(this.data.home.activities || []).some(a => a.id === id)),
    })
  },
  async loadReservations(this: ReservationPage, homeVersion?: unknown) {
    // bindtap supplies an event; use the current home version in that case.
    const version = typeof homeVersion === 'number' ? homeVersion : this.data.requestVersion
    const reservationVersion = this.data.reservationVersion + 1
    this.setData({ reservationLoading: true, reservationError: '', reservationVersion })
    try {
      const status = await callApi<{items: ReservationState[]}>('activity.reservation.status')
      if (version !== this.data.requestVersion || reservationVersion !== this.data.reservationVersion) return
      if (!status || !Array.isArray(status.items)) throw new Error('预约容量状态暂不可用')
      this.setData({ reservations: status.items.filter(s => s.status === 'RESERVED').map(s => s.activityId), reservationStates: status.items, waitlisted: status.items.filter(s => s.status === 'WAITLISTED').map(s => s.activityId) })
      this.refreshReservationActivities()
    } catch (error) {
      if (version === this.data.requestVersion && reservationVersion === this.data.reservationVersion) this.setData({ reservationError: (error as Error).message || '预约状态加载失败，请重试' })
    } finally {
      if (reservationVersion === this.data.reservationVersion) this.setData({ reservationLoading: false })
    }
  },
  changeReservation(this: ReservationPage, event: WechatMiniprogram.BaseEvent) {
    const id = event.currentTarget.dataset.id as string
    if (this.data.loading || this.data.error || this.data.reservationLoading || this.data.reservationBusy || this.data.reservationError || !id) return
    const enabled = !this.data.reservations.includes(id) && !this.data.waitlisted.includes(id)
    const item = this.data.home.activities?.find(a => a.id === id)
    if (enabled && (!item?.reservationEnabled || todayInChina() > item.endDate)) return
    const full = !!item?.capacity && (this.data.reservationStates.find(s => s.activityId === id)?.confirmed || 0) >= item.capacity
    if (enabled && full && !item?.waitlistEnabled) { wx.showToast({ title: '活动名额已满', icon: 'none' }); return }
    const version = this.data.requestVersion
    wx.showModal({ title: enabled ? full ? '加入候补' : '确认预约' : '取消预约 / 候补', content: enabled ? `预约「${item!.title}」？\n${item!.startDate} 至 ${item!.endDate}\n${full ? '名额已满，将按加入顺序候补；递补成功后会生成站内通知。' : '预约成功后可在小程序内查看活动通知。'}活动安排以公告为准。${this.data.demoMode ? '\n演示预约仅保存在本机。' : ''}` : '确认取消此活动预约？', confirmText: enabled ? '确认预约' : '取消预约', success: async result => {
      if (!result.confirm || version !== this.data.requestVersion || this.data.reservationBusy || this.data.reservationLoading || this.data.reservationError) return
      this.setData({ reservationBusy: id, reservationVersion: this.data.reservationVersion + 1 })
      try {
        const result = await callApi<{ enabled: boolean; status?: string }>('activity.reservation.set', { activityId: id, enabled })
        if (version !== this.data.requestVersion) return
        const reservations = this.data.reservations.filter(value => value !== id)
        if (result.enabled && result.status !== 'WAITLISTED') reservations.unshift(id)
        this.setData({ reservations, waitlisted: [...this.data.waitlisted.filter(value => value !== id), ...(result.status === 'WAITLISTED' ? [id] : [])] })
        this.refreshReservationActivities()
        if (result.enabled) wx.showModal({ title: result.status === 'WAITLISTED' ? '已加入候补' : '预约成功', content: `${result.status === 'WAITLISTED' ? '已候补' : '已预约'}「${item!.title}」\n${item!.startDate} 至 ${item!.endDate}（北京时间）\n${result.status === 'WAITLISTED' ? '候补登记不代表预约成功；递补后生成站内通知。' : '成功通知已保存，可在首页或「我的 → 活动通知」查看。'}`, confirmText: '查看通知', cancelText: '知道了', success: answer => { if (answer.confirm) wx.navigateTo({ url: '/pages/activity-notices/index' }) } })
        else wx.showToast({ title: '已取消预约', icon: 'success' })
        await this.loadReservations(version)
      } catch (error) {
        if (version === this.data.requestVersion) {
          wx.showToast({ title: (error as Error).message || '操作失败，请重试', icon: 'none' })
          // A lost response may already have committed; reread before allowing another action.
          await this.loadReservations(version)
        }
      } finally {
        this.setData({ reservationBusy: '' })
        if (version !== this.data.requestVersion && !this.data.loading && !this.data.error) await this.loadReservations()
      }
    } })
  },
}
