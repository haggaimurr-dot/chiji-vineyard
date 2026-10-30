import { callApi } from '../../utils/api'

interface KindState { kind: string; label: string; wechatAccepted: boolean; wechatEnabled: boolean; smsEnabled: boolean }

Page({
  data: { wechatConfigured: false, wechatTemplateId: '', smsConfigured: false, kinds: [] as KindState[], loading: true, busy: '' as string },
  async onShow() { await this.load() },
  async load() {
    this.setData({ loading: true })
    try {
      const cfg = await callApi<{ wechatConfigured: boolean; wechatTemplateId: string; smsConfigured: boolean; kinds: KindState[] }>('notify.config', {})
      this.setData({ wechatConfigured: cfg.wechatConfigured, wechatTemplateId: cfg.wechatTemplateId || '', smsConfigured: cfg.smsConfigured, kinds: cfg.kinds || [] })
    } catch (_) { /* 未登录时为空 */ }
    finally { this.setData({ loading: false }) }
  },
  async subscribe(e: WechatMiniprogram.BaseEvent) {
    const kind = e.currentTarget.dataset.kind as string
    const tmplId = this.data.wechatTemplateId
    if (!tmplId) { wx.showToast({ title: '微信模板尚未配置', icon: 'none' }); return }
    this.setData({ busy: kind })
    try {
      const result = await wx.requestSubscribeMessage({ tmplIds: [tmplId] })
      const accept = result[tmplId] === 'accept'
      const sub = await callApi<{ subscribed: boolean }>('notify.subscribe', { kind, accept })
      wx.showToast({ title: sub.subscribed ? '已订阅微信通知' : '已取消订阅', icon: 'success' })
      await this.load()
    } catch (err) { wx.showToast({ title: (err as Error).message || '订阅失败', icon: 'none' }) }
    finally { this.setData({ busy: '' }) }
  },
  async togglePref(e: WechatMiniprogram.BaseEvent) {
    const { kind, channel } = e.currentTarget.dataset as { kind: string; channel: string }
    const enabled = (e.detail as { value: boolean }).value
    try {
      await callApi('notify.pref.set', { kind, channel, enabled })
      await this.load()
    } catch (err) { wx.showToast({ title: (err as Error).message || '设置失败', icon: 'none' }); await this.load() }
  },
})
