import { money } from './format'

export function parseTargetPrice(value: string): number {
  const text = value.trim()
  if (!/^\d{1,7}(\.\d{1,2})?$/.test(text)) throw new Error('请输入有效金额，最多两位小数')
  const [yuan, cents = ''] = text.split('.')
  const price = Number(yuan) * 100 + Number(cents.padEnd(2, '0'))
  if (price <= 0 || price > 100000000) throw new Error('目标价格须在 ¥0.01～¥1,000,000 之间')
  return price
}

export async function promptTargetPrice(current = 0): Promise<number | null> {
  const result = await wx.showModal({ title: '自定义目标价格', editable: true, placeholderText: '金额（元），最多两位小数', content: current ? money(current) : '', confirmText: '保存目标价' })
  return result.confirm ? parseTargetPrice(result.content || '') : null
}
