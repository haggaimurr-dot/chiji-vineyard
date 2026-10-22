export interface ShippingRegion {
  name: string; province: string; city: string; district: string; blocked: boolean
  freight: number; freeShippingThreshold: number; baseQuantity: number; extraFreight: number; surcharge: number
}
export interface ShippingRule {
  freight: number; freeShippingThreshold: number; blockedProvinces: string[]
  regions?: ShippingRegion[]; blockedDates?: string[]
}
export function validateShippingDetails(rule: ShippingRule) {
  const text = (v: unknown, n: number) => typeof v === 'string' && v.trim() !== '' && [...v].length <= n
  const money = (v: unknown) => Number.isSafeInteger(v) && Number(v) >= 0 && Number(v) <= 10000000
  if (!Array.isArray(rule.regions || []) || (rule.regions || []).length > 100 || !Array.isArray(rule.blockedDates || []) || (rule.blockedDates || []).length > 366) throw new Error('配送规则数量超限或格式无效')
  const seen = new Set<string>()
  for (const r of rule.regions || []) {
    const key = `${r?.province}/${r?.city}/${r?.district}`
    if (!r || !text(r.name, 60) || !text(r.province, 30) || typeof r.city !== 'string' || typeof r.district !== 'string' || (r.city && !text(r.city, 30)) || (r.district && (!r.city || !text(r.district, 30))) || typeof r.blocked !== 'boolean' || seen.has(key) || !Number.isSafeInteger(r.baseQuantity) || r.baseQuantity < 1 || r.baseQuantity > 100000 || ![r.freight,r.freeShippingThreshold,r.extraFreight,r.surcharge].every(money)) throw new Error('区域配送规则无效或地区重复')
    seen.add(key)
  }
  seen.clear()
  for (const date of rule.blockedDates || []) {
    if (typeof date !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(date) || !Number.isFinite(Date.parse(date)) || new Date(date).toISOString().slice(0,10) !== date || seen.has(date)) throw new Error('不可配送日期无效或重复')
    seen.add(date)
  }
}
export function shippingFee(rule: ShippingRule, address: { province?: string; city?: string; district?: string }, subtotal: number, quantity: number, date = '') {
  let matched: ShippingRegion | undefined, specificity = 0
  for (const r of rule.regions || []) {
    if (r.province !== address.province || (r.city && r.city !== address.city) || (r.district && r.district !== address.district)) continue
    const score = 1 + Number(!!r.city) + Number(!!r.district)
    if (score > specificity) { specificity = score; matched = r }
  }
  const rate = matched || rule, threshold = rate.freeShippingThreshold
  const surcharge = matched?.surcharge || 0
  const freight = (subtotal >= threshold ? 0 : rate.freight + (matched ? Math.max(0, quantity - matched.baseQuantity) * matched.extraFreight : 0)) + surcharge
  const blockedReason = rule.blockedProvinces.includes(address.province || '') || matched?.blocked ? '该地区暂不支持生鲜配送' : date && rule.blockedDates?.includes(date) ? '所选日期暂停配送，请选择其他日期' : undefined
  return { freight, surcharge, freeShippingThreshold: threshold, shippingRuleName: matched?.name, blockedReason }
}
