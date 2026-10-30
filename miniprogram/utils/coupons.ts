import { chinaDate } from './checkout'
import type { Order } from './types'

export interface WalletCoupon {
  code: string; name: string; minimum: number; amount: number; startDate: string; endDate: string; active: boolean
  type?: 'AMOUNT' | 'PERCENT' | 'FREIGHT'; percent?: number; maxDiscount?: number; newUserOnly?: boolean
  status: string; reason: string
}
export function couponType(c: WalletCoupon): 'AMOUNT' | 'PERCENT' | 'FREIGHT' {
  return c.type === 'PERCENT' || c.type === 'FREIGHT' ? c.type : 'AMOUNT'
}
export function couponFace(c: WalletCoupon): string {
  const t = couponType(c)
  if (t === 'PERCENT') return `${c.percent || 0} 折`
  if (t === 'FREIGHT') return '免邮'
  return `¥${(c.amount / 100).toFixed(2)}`
}
export const demoCoupons = [
  { code: 'DEMO10', name: '演示满99减10券', minimum: 9900, amount: 1000, startDate: '2020-01-01', endDate: '2099-12-31', active: true },
  { code: 'DEMO20', name: '演示满199减20券', minimum: 19900, amount: 2000, startDate: '2020-01-01', endDate: '2099-12-31', active: true },
]
export const demoWalletKey = 'chiji_demo_coupon_wallet'
export function demoWallet(orders: Order[]): WalletCoupon[] {
  const codes: string[] = wx.getStorageSync(demoWalletKey) || []
  return codes.map(code => {
    const c = demoCoupons.find(c => c.code === code)!
    if (!c) return null
    const related = orders.filter(o => o.quote.couponCode === code)
    const status = related.some(o => o.paymentStatus !== 'UNPAID') ? 'USED'
      : related.some(o => !['CANCELLED', 'CLOSED'].includes(o.orderStatus)) ? 'RESERVED'
      : !c.active ? 'DISABLED' : chinaDate(0) > c.endDate ? 'EXPIRED' : chinaDate(0) < c.startDate ? 'UPCOMING' : 'AVAILABLE'
    const reasons: Record<string, string> = { USED: '已使用', RESERVED: '待付款订单占用中', DISABLED: '优惠券已停用或下架', EXPIRED: '已过期', UPCOMING: '尚未生效', AVAILABLE: '可使用' }
    return { ...c, status, reason: reasons[status] }
  }).filter((c): c is WalletCoupon => c !== null)
}
export function couponSaving(c: WalletCoupon, subtotal: number): number {
  if (c.status !== 'AVAILABLE' || subtotal < c.minimum) return 0
  const t = couponType(c)
  if (t === 'PERCENT') {
    const raw = Math.floor(subtotal * (100 - (c.percent || 0)) / 100)
    const capped = c.maxDiscount ? Math.min(raw, c.maxDiscount) : raw
    return Math.max(0, Math.min(capped, subtotal - 1))
  }
  // 免邮券的抵扣额取决于运费（客户端未知），自动选券时不参与比较
  if (t === 'FREIGHT') return 0
  return Math.max(0, Math.min(c.amount, subtotal - 1))
}
export function couponSelectable(c: WalletCoupon, subtotal: number): boolean {
  return c.status === 'AVAILABLE' && subtotal >= c.minimum
}
export function bestCoupon(wallet: WalletCoupon[], subtotal: number): string {
  const compare = (a: string, b: string) => a < b ? -1 : a > b ? 1 : 0
  const sorted = [...wallet].sort((a, b) => couponSaving(b, subtotal) - couponSaving(a, subtotal) || compare(a.endDate, b.endDate) || compare(a.code, b.code))
  return sorted[0] && couponSaving(sorted[0], subtotal) > 0 ? sorted[0].code : ''
}
