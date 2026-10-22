import type { Address } from './types'
const KEY = 'chiji_demo_addresses'
export function getDemoAddresses(): Address[] {
  const saved = wx.getStorageSync(KEY)
  if (Array.isArray(saved)) return saved
  const legacy = wx.getStorageSync('chiji_default_address')
  const list = legacy ? [{ ...legacy, id: 'legacy', isDefault: true }] : []
  wx.setStorageSync(KEY, list)
  wx.removeStorageSync('chiji_default_address')
  return list
}
export function changeDemoAddress(action: string, address: Address, id?: string): Address | Address[] {
  const list = getDemoAddresses()
  const index = list.findIndex(a => a.id === (action === 'address.save' ? address.id : id))
  let savedIndex = index
  if (action === 'address.save') {
    address = { ...address, name: address.name.trim(), phone: address.phone.trim(), detail: address.detail.trim() }
    if (!address.name || !/^1\d{10}$/.test(address.phone) || !address.province || !address.city || !address.detail) throw new Error('收货地址不完整')
    if (address.id && index < 0) throw new Error('地址不存在')
    address.isDefault = !!address.isDefault || !list.length || !!list[index]?.isDefault
    if (index < 0) { address.id = `adr-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`; savedIndex = list.length; list.push(address) }
    else list[index] = address
    if (address.isDefault) list.forEach((a, i) => { a.isDefault = i === savedIndex })
  } else {
    if (index < 0) throw new Error('地址不存在')
    if (action === 'address.default') list.forEach((a, i) => { a.isDefault = i === index })
    else { const [removed] = list.splice(index, 1); if (removed.isDefault && list.length) list[0].isDefault = true }
  }
  wx.setStorageSync(KEY, list)
  return action === 'address.save' ? list[savedIndex] : list
}
