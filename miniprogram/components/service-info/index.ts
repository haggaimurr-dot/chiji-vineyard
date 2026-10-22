import { callApi } from '../../utils/api'
import { defaultService } from '../../data/service'
import type { CustomerService, PrivacyPolicy } from '../../utils/types'

Component({
  data: { service: defaultService as CustomerService, policy: null as PrivacyPolicy | null, demo: false, loading: true, error: '', identityError: '', requestVersion: 0 },
  lifetimes: { attached() { this.load() }, detached() { this.data.requestVersion++ } },
  pageLifetimes: { show() { this.load() }, hide() { this.data.requestVersion++ } },
  methods: {
    async load() {
      const version = this.data.requestVersion + 1
      this.setData({ requestVersion: version, demo: getApp<IAppOption>().globalData.demoMode, loading: true, error: '', identityError: '', service: defaultService, policy: null })
      const [service, policy] = await Promise.allSettled([callApi<CustomerService>('service.get'), callApi<PrivacyPolicy>('privacy.get')])
      if (version !== this.data.requestVersion) return
      this.setData({ loading: false,
        service: service.status === 'fulfilled' ? service.value : defaultService,
        error: service.status === 'rejected' ? '客服信息加载失败，请重试' : '',
        policy: policy.status === 'fulfilled' ? policy.value : null,
        identityError: policy.status === 'rejected' ? '主体与隐私资料暂不可用' : '',
      })
    },
    mockContact() { wx.showToast({ title: '演示客服，不发起真实会话', icon: 'none' }) },
    phone() {
      if (this.data.demo) return wx.showToast({ title: '演示号码，不拨打电话', icon: 'none' })
      if (this.data.service.enabled && this.data.service.phone) wx.makePhoneCall({ phoneNumber: this.data.service.phone, fail() { wx.showToast({ title: '未能拨通，请稍后重试', icon: 'none' }) } })
    },
    contactError() { wx.showToast({ title: '客服暂不可用，请稍后重试', icon: 'none' }) },
    privacy() { wx.navigateTo({ url: '/pages/privacy/index' }) },
  },
})
