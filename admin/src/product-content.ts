import type { Product } from './types'

export function productBatchError(batch: Product['traceability']): string {
  if (!batch) return ''
  if (batch.code && (batch.code.trim().length > 120 || !batch.code.trim() || !batch.batch.trim())) return '批次溯源码须为 1–120 字，并关联批次号'
  for (const value of [batch.harvestedAt, batch.packedAt]) {
    if (!value) continue
    const date = new Date(`${value}T00:00:00Z`)
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value) || !Number.isFinite(date.getTime()) || date.toISOString().slice(0, 10) !== value) return '采收和包装日期须为有效的 YYYY-MM-DD 日期'
  }
  if (batch.packedAt && !batch.harvestedAt) return '填写包装日期前请先填写采收日期'
  if (batch.packedAt && batch.packedAt < batch.harvestedAt) return '包装日期不能早于采收日期'
  return ''
}

export function giftCustomizationError(gift: Product['giftCustomization']): string {
  if (!gift?.enabled) return ''
  if (!Number.isInteger(gift.minimumQuantity) || gift.minimumQuantity < 1 || gift.minimumQuantity > 1000000) return '礼盒定制起订量须为 1–1000000 的整数'
  if (!gift.description?.trim() || gift.description.length > 1000) return '请填写 1–1000 字礼盒定制说明'
  if (!gift.leadTime?.trim() || gift.leadTime.length > 200) return '请填写 1–200 字定制周期'
  if (!Array.isArray(gift.options) || gift.options.length > 20 || gift.options.some(x => !x.trim() || x.length > 80)) return '定制项目最多 20 项，每项为 1–80 字'
  return ''
}
