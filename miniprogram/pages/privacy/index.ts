import { callApi } from '../../utils/api'
import type { PrivacyPolicy } from '../../utils/types'
Page({
  data: { policy: null as PrivacyPolicy | null, loading: true, error: '', demo: false, requestVersion: 0 },
  onShow() { return this.load() },
  onHide() { this.data.requestVersion++ },
  onUnload() { this.data.requestVersion++ },
  onPullDownRefresh() { return this.load().finally(() => wx.stopPullDownRefresh()) },
  async load() {
    const version = this.data.requestVersion + 1
    const demo = getApp<IAppOption>().globalData.demoMode
    this.setData({ loading: true, error: '', policy: null, demo, requestVersion: version })
    try {
      const policy = await callApi<PrivacyPolicy>('privacy.get')
      if (version !== this.data.requestVersion) return
      if (!policy || !Array.isArray(policy.content) || (!demo && !policy.published)) throw new Error('隐私政策正在完善，请联系商家')
      this.setData({ policy })
    } catch (error) {
      if (version === this.data.requestVersion) this.setData({ policy: null, error: (error as Error).message || '隐私政策暂时无法加载，请重试' })
    } finally {
      if (version === this.data.requestVersion) this.setData({ loading: false })
    }
  },
  copyContact() { if (!this.data.loading && !this.data.error && this.data.policy?.contact) wx.setClipboardData({ data: this.data.policy.contact }) },
})
