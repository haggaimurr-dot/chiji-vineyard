// Shared by the mini program and the admin UI. Provider times without an offset
// are China local time; do not interpret them in the browser's local timezone.
export interface ProgressOrder {
  createdAt: string; paidAt?: string; orderStatus: string; paymentStatus: string
  fulfillmentStatus: string; refundStatus: string
  shipment?: { shippedAt?: string }
}
export interface ProgressTracking {
  status: string; state?: string; message: string; updatedAt: string
  events: Array<{ time: string; context: string }>
}
export interface ProgressNode { key: string; label: string; time: string; detail: string; pending: boolean }
function timestamp(value?: string): number {
  if (!value) return NaN
  const normalized = value.trim().replace(' ', 'T')
  return Date.parse(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$/.test(normalized) ? normalized + '+08:00' : normalized)
}
export function logisticsAlerts(order: ProgressOrder, tracking?: ProgressTracking | null, now = Date.now()): string[] {
  if (!order.shipment || ['CANCELLED', 'CLOSED', 'COMPLETED'].includes(order.orderStatus) || order.fulfillmentStatus === 'RECEIVED') return []
  if (!tracking) return []
  const alerts: string[] = []
  if (['2', '4', '6'].includes(tracking.state || '')) alerts.push(({ '2': '运输异常，请联系承运商核实', '4': '快件退签，请核实收货情况', '6': '快件退回，请联系商家处理' } as Record<string, string>)[tracking.state!])
  if (tracking.status === 'STALE') alerts.push('物流查询暂不可用，当前显示上次轨迹，请稍后刷新')
  if (['UNAVAILABLE', 'DEMO'].includes(tracking.status)) return alerts
  if (tracking.state === '3') return alerts
  const latest = Math.max(...tracking.events.map(event => timestamp(event.time)).filter(Number.isFinite))
  if (tracking.events.length && Number.isFinite(latest) && now - latest >= 48 * 3600000) alerts.push('物流轨迹超过48小时未更新，请核实配送进度')
  if (!tracking.events.length && now - timestamp(order.shipment.shippedAt) >= 24 * 3600000) alerts.push('发货超过24小时暂无揽收轨迹，请联系承运商核实')
  return alerts
}
export function orderProgress(order: ProgressOrder, tracking?: ProgressTracking | null): ProgressNode[] {
  const nodes: ProgressNode[] = []
  const add = (key: string, label: string, time = '', detail = '', pending = false) => nodes.push({ key, label, time, detail, pending })
  add('created', '订单已提交', order.createdAt)
  if (order.paidAt || ['PAID', 'REFUNDING', 'REFUNDED'].includes(order.paymentStatus)) add('paid', '付款成功', order.paidAt)
  if (order.shipment) add('shipped', '商家已发货', order.shipment.shippedAt)
  const events = (tracking?.events || []).map((event, index) => ({ ...event, index }))
    .sort((a, b) => (timestamp(a.time) || 0) - (timestamp(b.time) || 0))
  for (const event of events) add(`tracking-${event.index}`, tracking?.status === 'DEMO' ? '演示物流节点' : '物流进度', event.time, event.context)
  if (order.fulfillmentStatus === 'RECEIVED') add('received', '已确认收货')
  if (order.orderStatus === 'COMPLETED') add('completed', '订单已完成')
  if (order.orderStatus === 'CANCELLED') add('cancelled', '订单已取消')
  else if (order.orderStatus === 'CLOSED') add('closed', '订单已关闭')
  else if (order.orderStatus !== 'COMPLETED' && order.fulfillmentStatus !== 'RECEIVED') {
    add('next', order.paymentStatus === 'UNPAID' ? '等待付款' : order.shipment ? (tracking?.state === '3' ? '等待确认收货' : '等待送达') : '等待商家发货', '', '', true)
  }
  if (order.refundStatus !== 'NONE' && order.refundStatus !== 'REJECTED') add('refund', ({ APPLIED: '退款审核中', APPROVED: '退款处理中', SUCCEEDED: '退款成功', PARTIAL: '部分退款成功', FAILED: '退款异常待处理' } as Record<string, string>)[order.refundStatus] || '售后处理中')
  return nodes
}
