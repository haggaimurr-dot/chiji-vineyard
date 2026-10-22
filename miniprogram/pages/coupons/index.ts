import { callApi } from '../../utils/api'
import { couponSaving, type WalletCoupon } from '../../utils/coupons'
import { money } from '../../utils/format'

Page({
  data: { coupons: [] as any[], visible: [] as any[], filter: 'usable', code: '', busy: false, redeemBusy: false, error: '', selecting: false, subtotal: 0, demo: getApp().globalData.demoMode },
  onLoad(options: Record<string, string>) { this.setData({ selecting: options.select === '1', subtotal: Math.max(0, Number(options.subtotal) || 0) }) },
  onShow() { this.load() },
  async onPullDownRefresh() { await this.load(); wx.stopPullDownRefresh() },
  async load() {
    if (this.data.busy) return
    this.setData({ busy: true, error: '' })
    try {
      const wallet = await callApi<WalletCoupon[]>('coupon.wallet')
      const coupons = wallet.map(c => ({ ...c, amountText: money(c.amount), minimumText: money(c.minimum), usable: c.status === 'AVAILABLE', selectable: couponSaving(c, this.data.subtotal) > 0, reason: this.data.selecting && c.status === 'AVAILABLE' && this.data.subtotal < c.minimum ? '本单商品金额未达到门槛' : c.reason }))
      this.setData({ coupons }); this.render()
    } catch (e) { this.setData({ coupons: [], visible: [], error: (e as Error).message }) }
    finally { this.setData({ busy: false }) }
  },
  render() { this.setData({ visible: this.data.coupons.filter(c => this.data.filter === 'all' || (this.data.filter === 'usable' ? c.usable : !c.usable)) }) },
  filter(event: WechatMiniprogram.BaseEvent) { this.setData({ filter: event.currentTarget.dataset.filter }); this.render() },
  input(event: WechatMiniprogram.Input) { this.setData({ code: event.detail.value }) },
  async redeem() {
    if (this.data.redeemBusy || this.data.busy) return
    this.setData({ redeemBusy: true })
    try { await callApi('coupon.redeem', { code: this.data.code }); this.setData({ code: '' }); await this.load(); wx.showToast({ title: '已加入券包', icon: 'success' }) }
    catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
    finally { this.setData({ redeemBusy: false }) }
  },
  choose(event: WechatMiniprogram.BaseEvent) {
    if (!this.data.selecting || this.data.busy || this.data.redeemBusy || this.data.error) return
    const coupon = this.data.coupons.find(c => c.code === event.currentTarget.dataset.code)
    if (!coupon?.selectable) return
    this.getOpenerEventChannel().emit?.('couponSelected', { code: coupon.code })
    wx.navigateBack()
  },
})
