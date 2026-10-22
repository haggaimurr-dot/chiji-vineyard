import type { Order } from './types'

export const orderFilters = ['all', 'pending', 'paid', 'shipping_pending', 'shipping', 'shipped', 'received', 'completed', 'cancelled', 'closed', 'refund']

export function matchesOrder(order: Order, keyword: string, filter: string) {
  const active = order.orderStatus === 'CONFIRMED' && order.paymentStatus === 'PAID'
    && ['NONE', 'REJECTED', 'PARTIAL'].includes(order.refundStatus)
  const status = filter === 'pending' ? order.orderStatus === 'PENDING_PAYMENT' && order.paymentStatus === 'UNPAID'
    : ['paid', 'shipping_pending'].includes(filter) ? active && ['UNFULFILLED', 'PREPARING'].includes(order.fulfillmentStatus)
    : ['shipping', 'shipped'].includes(filter) ? active && order.fulfillmentStatus === 'SHIPPED'
    : filter === 'received' ? order.fulfillmentStatus === 'RECEIVED'
    : filter === 'completed' ? order.orderStatus === 'COMPLETED'
    : filter === 'cancelled' ? order.orderStatus === 'CANCELLED'
    : filter === 'closed' ? order.orderStatus === 'CLOSED'
    : filter === 'refund' ? order.refundStatus !== 'NONE' || ['REFUNDING', 'REFUNDED'].includes(order.paymentStatus) : true
  const text = [order.orderNo, ...order.items.map(i => [i.name, i.product?.name, i.skuName, i.sku?.name].join(' '))].join(' ').toLowerCase()
  return status && keyword.trim().slice(0, 80).toLowerCase().split(/\s+/).filter(Boolean).every(word => text.includes(word))
}

