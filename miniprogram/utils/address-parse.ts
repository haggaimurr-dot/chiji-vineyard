// 地址智能解析：把"张三13800138000广东省深圳市南山区科技园1号"这类粘贴文本
// 拆成姓名/手机/省/市/区/详细地址。覆盖常见格式，解析不出就留空让用户手填。
export interface ParsedAddress {
  name: string
  phone: string
  province: string
  city: string
  district: string
  detail: string
}

const MUNICIPALITIES = ['北京市', '天津市', '上海市', '重庆市']

export function parseAddress(raw: string): ParsedAddress {
  const out: ParsedAddress = { name: '', phone: '', province: '', city: '', district: '', detail: '' }
  let text = String(raw || '').replace(/[，、;；\n\r\t]+/g, ' ').replace(/\s{2,}/g, ' ').trim()
  if (!text) return out

  // 1. 手机号（取第一个 11 位 1 开头）
  const phoneMatch = text.match(/1\d{10}/)
  if (phoneMatch) {
    out.phone = phoneMatch[0]
    text = (text.slice(0, phoneMatch.index) + ' ' + text.slice((phoneMatch.index || 0) + 11)).replace(/\s{2,}/g, ' ').trim()
  }

  // 2. 省：xxx省 / xxx自治区 / 直辖市
  let rest = text
  const provMatch = text.match(/([^ ]{1,12}?(?:省|自治区))/)
  if (provMatch && provMatch.index !== undefined) {
    out.province = provMatch[1]
    out.name = text.slice(0, provMatch.index).trim()
    rest = text.slice(provMatch.index + provMatch[1].length).trim()
  } else {
    const muni = MUNICIPALITIES.find(m => text.includes(m.slice(0, 2)))
    if (muni) {
      const idx = text.indexOf(muni.slice(0, 2))
      out.province = muni
      out.name = text.slice(0, idx).trim()
      rest = text.slice(idx + 2).replace(/^市/, '').trim()
    }
  }
  if (out.name.length > 12) out.name = ''

  // 3. 市：xxx市 / 自治州 / 盟 / 地区；直辖市无地级市则 city=province
  const cityMatch = rest.match(/^([^ ]{1,12}?(?:市|自治州|盟|地区))/)
  if (cityMatch) {
    out.city = cityMatch[1]
    rest = rest.slice(cityMatch[1].length).trim()
  } else if (MUNICIPALITIES.includes(out.province)) {
    out.city = out.province
  }

  // 4. 区/县/旗
  const distMatch = rest.match(/^([^ ]{1,12}?(?:区|县|旗))/)
  if (distMatch) {
    out.district = distMatch[1]
    rest = rest.slice(distMatch[1].length).trim()
  }

  // 5. 剩下的是详细地址；末尾若还带着疑似姓名（2-4 个汉字且无数字）则拆出
  out.detail = rest
  if (!out.name && rest) {
    const tail = rest.match(/([\u4e00-\u9fa5]{2,4})$/)
    if (tail && !/\d/.test(tail[1])) {
      out.name = tail[1]
      out.detail = rest.slice(0, rest.length - tail[1].length).trim()
    }
  }
  return out
}
