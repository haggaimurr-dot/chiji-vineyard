import { callApi, restartAccount } from '../../utils/api'
import { closedKey } from '../../utils/personal-data'
Page({
  data: { closed: false, loading: true, busy: false, error: '', canClose: false, blockingCount: 0, confirmation: '' },
  onShow() { this.load() },
  async load() {
    const closed = wx.getStorageSync(closedKey) === true
    this.setData({ closed, loading: !closed, error: '', canClose: false })
    if (closed) return
    try { this.setData(await callApi<{ canClose: boolean; blockingCount: number }>('account.status')) }
    catch (error) { this.setData({ error: (error as Error).message, closed: wx.getStorageSync(closedKey) === true }) }
    finally { this.setData({ loading: false }) }
  },
  confirmInput(event: WechatMiniprogram.Input) { this.setData({ confirmation: event.detail.value }) },
  policy() { wx.navigateTo({ url: '/pages/privacy/index' }) },
  orders() { wx.navigateTo({ url: '/pages/orders/index' }) },
  async remove(event: WechatMiniprogram.BaseEvent) {
    if (this.data.busy || this.data.closed) return
    const scope = event.currentTarget.dataset.scope
    const content = scope === 'addresses' ? '删除所有已保存的收货地址。历史订单中的配送信息仍保留，删除后无法恢复。' : '清空购物车、收藏、提醒、浏览搜索及学习记录和课堂问答。订单、券包、活动预约和交易售后记录仍保留。删除后无法恢复。'
    this.setData({ busy: true, error: '' })
    try {
      const result = await wx.showModal({ title: '删除个人数据', content, confirmText: '确认删除', confirmColor: '#ad3544' })
      if (!result.confirm) return
      await callApi('account.data.delete', { scope, confirm: '删除个人数据' })
      wx.showToast({ title: '已删除', icon: 'success' })
    } catch (error) { this.setData({ error: (error as Error).message }) }
    finally { this.setData({ busy: false }) }
  },
  async closeAccount() {
    if (this.data.busy || this.data.loading || !this.data.canClose || this.data.confirmation !== '注销账号') return
    this.setData({ busy: true, error: '' })
    try {
      const result = await wx.showModal({ title: '确认注销账号', content: '注销将删除账号资料、地址和使用记录，取消活动预约，并使所有登录会话失效。原有券包和历史订单无法通过重新注册恢复。交易、发票与售后凭证仍保留，详情见本页说明。', confirmText: '确认注销', confirmColor: '#ad3544' })
      if (!result.confirm) return
      await callApi('account.close', { confirm: '注销账号' })
      this.setData({ closed: true, confirmation: '', canClose: false })
    } catch (error) { await this.load(); this.setData({ error: (error as Error).message }) }
    finally { this.setData({ busy: false }) }
  },
  async restart() {
    if (this.data.busy) return
    this.setData({ busy: true, error: '' })
    try {
      const result = await wx.showModal({ title: '重新注册', content: '使用当前微信身份创建新账号。已删除的数据、原有券包及历史订单不会恢复。', confirmText: '重新注册' })
      if (!result.confirm) return
      await restartAccount(); await this.load()
    } catch (error) { this.setData({ error: (error as Error).message }) }
    finally { this.setData({ busy: false }) }
  },
})
