import { privacyDraft } from './privacy'

export const defaultService = { enabled: false, wechat: false, phone: '', hours: '', responseTime: '' }
export const mockService = { enabled: true, wechat: true, phone: '400-000-0000', hours: '周一至周六 09:00–18:00（北京时间，示例）', responseTime: '1 个工作日内（示例）' }
export const mockPrivacy = {
  ...privacyDraft,
  operator: '迟记葡萄牙果业有限公司（虚构演示主体）',
  contact: 'privacy@example.com（演示邮箱）',
  address: '浙江省杭州市示例路 100 号（虚构演示地址）',
  published: false,
}
