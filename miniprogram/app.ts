import { callApi , ensureLogin } from './utils/api'
import { closedKey } from './utils/personal-data'

App<IAppOption>({
  globalData: {
    // 云托管联调时改为 false，并填写云环境 ID 与服务名。
    demoMode: true,
    cloudEnv: '',
    cloudService: 'chiji-api',
  },
  onLaunch() {
    if (!this.globalData.demoMode && wx.cloud) {
      wx.cloud.init({
        env: this.globalData.cloudEnv || undefined,
        traceUser: true,
      })
      // 预热登录：提前换取 token，首屏接口不再排队等待
      void ensureLogin()
    }
  },
  onShow() {
    if (wx.getStorageSync(closedKey)) return
    // Analytics failure must not interrupt browsing. Server deduplicates daily UV.
    void callApi('analytics.visit').catch(() => {})
  },
})

