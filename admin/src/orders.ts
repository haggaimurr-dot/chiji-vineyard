import type { Order } from './types'

const orderLabels: Record<string, string> = {
  PENDING_PAYMENT: '待付款', CONFIRMED: '已确认', CANCELLED: '已取消', CLOSED: '已关闭', COMPLETED: '已完成',
}
const paymentLabels: Record<string, string> = {
  UNPAID: '未付款', PAID: '已付款', REFUNDING: '退款处理中', REFUNDED: '已退款',
}
const fulfillmentLabels: Record<string, string> = {
  UNFULFILLED: '未发货', PREPARING: '待发货', SHIPPED: '待收货', RECEIVED: '已收货',
}
const refundLabels: Record<string, string> = {
  NONE: '无退款', APPLIED: '待商家审核', APPROVED: '微信退款处理中', REJECTED: '审核未通过',
  SUCCEEDED: '退款成功', PARTIAL: '已部分退款', FAILED: '退款异常待处理',
}

const stateLabel = (labels: Record<string, string>, state: string) => Object.hasOwn(labels, state)
  ? labels[state]!
  : `状态待确认（${state || '未提供'}）`

export const orderStateLabel = (state: string) => stateLabel(orderLabels, state)
export const paymentStateLabel = (state: string) => stateLabel(paymentLabels, state)
export const fulfillmentStateLabel = (state: string) => stateLabel(fulfillmentLabels, state)
export const refundStateLabel = (state: string) => stateLabel(refundLabels, state)

export function orderSummary(order: Order): string {
  const states = [[orderLabels, order.orderStatus], [paymentLabels, order.paymentStatus],
    [fulfillmentLabels, order.fulfillmentStatus], [refundLabels, order.refundStatus]] as const
  const unknown = states.filter(([labels, state]) => !Object.hasOwn(labels, state))
  if (unknown.length) return unknown.map(([labels, state]) => stateLabel(labels, state)).join(' · ')

  const refund = order.refundStatus === 'NONE' ? '' : refundStateLabel(order.refundStatus)
  if (['CANCELLED', 'CLOSED', 'COMPLETED'].includes(order.orderStatus)) {
    return [orderStateLabel(order.orderStatus), refund].filter(Boolean).join(' · ')
  }
  if (['APPLIED', 'APPROVED', 'FAILED'].includes(order.refundStatus)) return refund
  if (order.paymentStatus === 'REFUNDING') return paymentStateLabel(order.paymentStatus)
  if (order.paymentStatus === 'REFUNDED' || order.refundStatus === 'SUCCEEDED') return '已退款'
  if (order.orderStatus === 'PENDING_PAYMENT') return orderStateLabel(order.orderStatus)
  if (order.paymentStatus === 'UNPAID') return paymentStateLabel(order.paymentStatus)
  const fulfillment = order.paymentStatus === 'PAID' && ['UNFULFILLED', 'PREPARING'].includes(order.fulfillmentStatus)
    ? '待发货' : fulfillmentStateLabel(order.fulfillmentStatus)
  return [fulfillment, refund].filter(Boolean).join(' · ')
}

export function canShip(order: Order): boolean {
  return order.orderStatus === 'CONFIRMED' && order.paymentStatus === 'PAID'
    && ['UNFULFILLED', 'PREPARING'].includes(order.fulfillmentStatus)
    && ['NONE', 'REJECTED', 'PARTIAL'].includes(order.refundStatus)
}

export function canEditShipment(order: Order): boolean {
  const shipment = order.shipment
  const syncing = shipment?.syncStatus === 'PENDING' && !!shipment.syncStartedAt
    && Date.now() - Date.parse(shipment.syncStartedAt) < 120_000
  return !!shipment && order.orderStatus === 'CONFIRMED' && order.paymentStatus === 'PAID'
    && order.fulfillmentStatus === 'SHIPPED' && ['NONE', 'REJECTED', 'PARTIAL'].includes(order.refundStatus)
    && !syncing
}

export interface ShipmentInput { carrier: string; carrierCode: string; trackingNo: string }

export function validateShipment(input: ShipmentInput): ShipmentInput {
  const carrier = String(input.carrier || '').trim()
  const carrierCode = String(input.carrierCode || '').trim().toUpperCase()
  const trackingNo = String(input.trackingNo || '').trim()
  if (!carrier || [...carrier].length > 40 || /[\p{Cc}<>]/u.test(carrier)) {
    throw new Error('请输入有效快递公司名称（最多40个字符）')
  }
  if (!/^[A-Z0-9_-]{1,20}$/.test(carrierCode)) throw new Error('快递公司编码须为1–20位字母、数字、下划线或短横线')
  if (!/^[A-Za-z0-9_-]{4,50}$/.test(trackingNo)) throw new Error('运单号须为4–50位字母、数字、下划线或短横线')
  return { carrier, carrierCode, trackingNo }
}
