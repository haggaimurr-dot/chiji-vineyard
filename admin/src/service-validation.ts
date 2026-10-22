import type { CustomerService } from './types'

export function validateService(service?: CustomerService) {
  if (!service) return
  if (typeof service.enabled !== 'boolean' || typeof service.wechat !== 'boolean' || typeof service.phone !== 'string' || typeof service.hours !== 'string' || typeof service.responseTime !== 'string') throw new Error('客服配置格式无效')
  if ([...service.hours].length > 100 || [...service.responseTime].length > 100 || (service.phone && !/^(1[0-9]{10}|0[0-9]{2,3}-?[0-9]{7,8}|400-?[0-9]{3}-?[0-9]{4})$/.test(service.phone))) throw new Error('客服号码格式无效，服务时间和回复说明最多100字')
  if (service.enabled && (!service.hours.trim() || (!service.wechat && !service.phone))) throw new Error('启用客服前请填写服务时间，并开通微信客服或填写电话')
}
