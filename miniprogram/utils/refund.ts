import type { Order } from './types'

export const canApplyRefund = (order: Order) => order.paymentStatus === 'PAID'
  && ['NONE', 'REJECTED', 'PARTIAL'].includes(order.refundStatus)
  && ['UNFULFILLED', 'PREPARING', 'SHIPPED', 'RECEIVED'].includes(order.fulfillmentStatus)
  && (order.refundedAmount || 0) < order.quote.total

export function refundCents(value: string, remaining: number): number {
  if (!/^\d+(\.\d{1,2})?$/.test(value.trim())) throw new Error('请输入退款金额，最多两位小数')
  const [yuan, fraction = ''] = value.trim().split('.')
  const amount = Number(yuan) * 100 + Number(fraction.padEnd(2, '0'))
  if (!Number.isSafeInteger(amount) || amount <= 0 || amount > remaining) throw new Error('金额须大于0且不超过剩余可退金额')
  return amount
}
