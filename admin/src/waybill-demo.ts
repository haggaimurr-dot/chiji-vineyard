import type { WaybillSettings, WaybillQuotes } from './types'
export function createWaybillDemo(carriers: WaybillSettings['carriers']) {
  const storageKey = 'chiji-demo-waybill-settings-v1'
  let config: WaybillSettings = {
    revision: 0, senderName: '示例寄件人', senderPhone: '13800000000', senderAddress: '宁夏回族自治区青铜峡市示例果园路1号', enterpriseConfigured: true, carriers,
    accounts: Object.fromEntries(carriers.map(c => [c.code, {disabled: false, partnerId: '', partnerKey: '', partnerSecret: '', partnerIdSet: true, partnerKeySet: true, partnerSecretSet: true, partnerName: '', net: 'DEMO', code: '', checkMan: '', tempId: 'DEMO-V2', expType: '标准快递', payType: 'MONTHLY'}])),
    rates: carriers.slice(0, 3).map((c, index) => ({carrierCode: c.code, province: '', firstWeight: 1, firstFee: [1800,800,700][index], additionalWeight: 1, additionalFee: [600,300,250][index], surcharge: 0, delivery: '示例：2至3天'})),
  }
  try { const raw = typeof sessionStorage !== 'undefined' && sessionStorage.getItem(storageKey); if (raw) config = JSON.parse(raw) } catch { /* Start from demo defaults. */ }
  const ready = (code: string) => !!config.accounts[code] && !config.accounts[code].disabled
  return {
    get: () => structuredClone(config),
    carriers: () => carriers.filter(c => ready(c.code)),
    save(input: WaybillSettings) {
      if (input.revision !== config.revision) throw new Error('电子面单配置已被修改，请重新读取后保存')
      const next = structuredClone(input)
      if (!next.senderName?.trim() || next.senderName.length>30 || !/^1\d{10}$/.test(next.senderPhone) || !next.senderAddress?.trim() || next.senderAddress.length>240) throw new Error('请填写有效的寄件人、手机号及完整寄件地址')
      for (const [code, a] of Object.entries(next.accounts)) {
        if (!carriers.some(c => c.code===code)) throw new Error('承运商编码无效')
        const previous = config.accounts[code]
        for (const key of ['partnerId','partnerKey','partnerSecret'] as const) {
          a[`${key}Set`] = !!a[key]?.trim() || !!previous?.[`${key}Set`]
          a[key] = '' // Never keep entered credentials in browser storage.
        }
        if (!a.disabled && (!a.partnerIdSet || !a.tempId?.trim() || !a.expType?.trim() || (code!=='JD' && !a.partnerKeySet) || (code==='SF' && !a.partnerSecretSet) || (['ZTO','STO'].includes(code) && !a.net?.trim()) || !['','MONTHLY','SHIPPER'].includes(a.payType) || ['taobao','cainiao','jdalpha','pinduoduowx','douyin','kuaishou','weipinhui','wechatchannels','xiaohongshu'].includes(a.net?.toLowerCase()))) throw new Error(`承运商 ${code} 的账号、网点、产品或模板未配置完整`)
      }
      const seen = new Set<string>()
      if (next.rates.length>200) throw new Error('价目表数量超出限制')
      for (const r of next.rates) {
        r.province = r.province.trim()
        const key = `${r.carrierCode}:${r.province}`
        if (seen.has(key)) throw new Error('同一承运商的同一省份只能配置一条协议价')
        seen.add(key)
        if ([r.firstWeight,r.additionalWeight].some(w => Math.abs(w*1000-Math.round(w*1000))>1e-7)) throw new Error('协议价重量最多保留三位小数（精确到克）')
        if (!next.accounts[r.carrierCode] || [r.firstWeight,r.additionalWeight].some(w => !Number.isFinite(w) || w<0.01 || w>1000) || [r.firstFee,r.additionalFee,r.surcharge].some(f => !Number.isInteger(f) || f<0 || f>10000000)) throw new Error('协议价重量、金额或地区无效')
      }
      next.revision++; next.carriers = carriers; next.enterpriseConfigured = true
      if (typeof sessionStorage !== 'undefined') sessionStorage.setItem(storageKey, JSON.stringify(next))
      config = next
      return structuredClone(config)
    },
    quote(province: string, weight: number): WaybillQuotes {
      if (!Number.isFinite(weight) || weight<0.01 || weight>100) throw new Error('包裹重量须为0.01至100公斤')
      const items = carriers.filter(c => ready(c.code)).map(c => {
        const r = config.rates.find(r => r.carrierCode===c.code && r.province===province) || config.rates.find(r => r.carrierCode===c.code && !r.province)
        if (!r) return {carrierCode: c.code, carrier: c.name, available: false, message: '未配置目的省份的协议价'}
        const first = Math.round(r.firstWeight*1000), step = Math.round(r.additionalWeight*1000)
        const units = Math.ceil(Math.max(0, Math.ceil(weight*1000-1e-7)-first)/step)
        return {carrierCode: c.code, carrier: c.name, available: true, fee: r.firstFee+units*r.additionalFee+r.surcharge, billableWeight: (first+units*step)/1000, delivery: r.delivery}
      }).sort((a,b) => Number(b.available)-Number(a.available) || (a.fee || 0)-(b.fee || 0))
      return {items, province, weight, revision: config.revision, estimated: true, message: '演示协议价预估，未含体积重及特殊附加费；正式费用以承运商账单为准'}
    },
  }
}
