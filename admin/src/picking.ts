import type { Order, OrderExportResult } from './types'
import { csvCell } from './order-operations'

export function pickingSummary(orders: Order[]) {
  const rows = new Map<string, { name: string; skuName: string; quantity: number; orders: Set<string> }>()
  for (const order of orders) for (const item of order.items) {
    const key = JSON.stringify([item.name, item.skuName])
    const row = rows.get(key) || { name: item.name, skuName: item.skuName, quantity: 0, orders: new Set<string>() }
    row.quantity += item.quantity; row.orders.add(order.id); rows.set(key, row)
  }
  return [...rows.values()]
}

export function pickingCSV(orders: Order[]): OrderExportResult {
  const lines: unknown[][] = [['拣货汇总'], ['商品', '规格', '数量', '涉及订单数']]
  pickingSummary(orders).forEach(row => lines.push([row.name, row.skuName, row.quantity, row.orders.size]))
  lines.push([], ['逐单明细'], ['订单号', '收件人', '手机号', '收货地址', '配送日期', '备注', '商品', '规格', '数量'])
  orders.forEach(order => order.items.forEach(item => lines.push([order.orderNo, order.address.name, order.address.phone,
    `${order.address.province} ${order.address.city} ${order.address.district} ${order.address.detail}`,
    order.deliveryDate || '尽快配送', order.note || '', item.name, item.skuName, item.quantity])))
  return { filename: `picking-${Date.now()}.csv`, count: orders.length, content: '\uFEFF' + lines.map(line => line.map(value => csvCell(value, typeof value !== 'number')).join(',')).join('\r\n') + '\r\n' }
}

export function orderExceptions(order: Order): string[] {
  return [order.shipment?.syncStatus === 'FAILED' ? '微信发货同步失败' : '',
    ['FAILED', 'UNCERTAIN'].includes(order.shippingNoticeStatus || '') ? '发货提醒异常' : '',
    order.refundStatus === 'FAILED' ? '退款失败' : '',
    order.invoice?.status === 'ISSUED' && ['FAILED', 'UNCERTAIN'].includes(order.invoice.emailStatus || '') ? '发票邮件异常' : ''].filter(Boolean)
}
