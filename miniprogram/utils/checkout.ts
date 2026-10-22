import type { Invoice, CartItem } from './types'
export function chinaDate(offset = 0): string { return new Date(Date.now() + 8 * 3600000 + offset * 86400000).toISOString().slice(0, 10) }
export function deliveryStart(items: CartItem[]): string { return items.reduce((date, item) => item.product?.saleMode === 'presale' && (item.product.expectedShipDate || '') > date ? item.product.expectedShipDate! : date, chinaDate(2)) }
export function validateDelivery(date: string, items: CartItem[]) {
  if (!date) return
  const parsed = new Date(date + 'T00:00:00Z')
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || Number.isNaN(parsed.getTime()) || parsed.toISOString().slice(0, 10) !== date || date < deliveryStart(items) || date > chinaDate(30)) throw new Error('配送日期须在两天后至30天内，且不早于预售发货日期')
}
export function validateInvoice(invoice: Invoice): Invoice { const i = { ...invoice, title: invoice.title.trim(), email: invoice.email.trim(), taxId: invoice.taxId.trim().toUpperCase() }; if (!i.title || i.title.length > 100 || i.email.length > 200 || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(i.email) || !['personal', 'company'].includes(i.kind) || (i.kind === 'company' && !/^[A-Z0-9]{15,20}$/.test(i.taxId))) throw new Error('请填写发票抬头、有效邮箱及企业税号'); if (i.kind === 'personal') i.taxId = ''; return i }

export function checkoutIntent(data: { items: CartItem[]; address: any; couponCode?: string; deliveryDate?: string; note?: string }): string {
  const a = data.address || {}
  return JSON.stringify({ items: data.items.map(i => ({ productId: i.productId, skuId: i.skuId, quantity: i.quantity })).sort((a,b) => a.skuId.localeCompare(b.skuId) || a.productId.localeCompare(b.productId)), address: { name:a.name, phone:a.phone, province:a.province, city:a.city, district:a.district || '', detail:a.detail }, couponCode:(data.couponCode || '').trim().toUpperCase(), deliveryDate:data.deliveryDate || '', note:(data.note || '').trim() })
}
export function purchaseKey(): string { return `checkout_${Date.now().toString(36)}_${Math.random().toString(36).slice(2)}_${Math.random().toString(36).slice(2)}` }
