import { chinaDate } from './checkout'
import type { Order } from './types'

export interface WalletCoupon {
  code: string; name: string; minimum: number; amount: number; startDate: string; endDate: string; active: boolean
  status: string; reason: string
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
  return c.status === 'AVAILABLE' && subtotal >= c.minimum ? Math.max(0, Math.min(c.amount, subtotal - 1)) : 0
}
export function bestCoupon(wallet: WalletCoupon[], subtotal: number): string {
  const compare = (a: string, b: string) => a < b ? -1 : a > b ? 1 : 0
  const sorted = [...wallet].sort((a, b) => couponSaving(b, subtotal) - couponSaving(a, subtotal) || compare(a.endDate, b.endDate) || compare(a.code, b.code))
  return sorted[0] && couponSaving(sorted[0], subtotal) > 0 ? sorted[0].code : ''
}
