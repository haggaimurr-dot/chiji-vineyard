import type { Dashboard, Order, Refund } from './types'

export function beijingDate(value = new Date()) {
  return new Date(value.getTime() + 8 * 3600000).toISOString().slice(0, 10)
}
export function shiftDate(date: string, days: number) {
  return new Date(Date.parse(`${date}T00:00:00+08:00`) + days * 86400000 + 8 * 3600000).toISOString().slice(0, 10)
}
export function dashboardRange(data: { startDate?: string; endDate?: string }, now = new Date()) {
  const endDate = data.endDate || beijingDate(now)
  const valid = (value: string) => /^\d{4}-\d{2}-\d{2}$/.test(value) && Number.isFinite(Date.parse(value)) && new Date(value).toISOString().slice(0, 10) === value
  if (!valid(endDate)) throw new Error('结束日期无效')
  const startDate = data.startDate || shiftDate(endDate, -29)
  if (!valid(startDate)) throw new Error('开始日期无效')
  if (startDate > endDate || endDate > beijingDate(now) || (Date.parse(endDate) - Date.parse(startDate)) / 86400000 > 365) throw new Error('请选择不超过366天且不晚于今天的日期范围')
  return { startDate, endDate }
}

export interface CommerceActivity { buyerId: string; visitedAt: string; cartAddedAt?: string }

export function demoDashboard(orders: Order[], refunds: Refund[], data: { startDate?: string; endDate?: string }, counts: Pick<Dashboard, 'pendingShipment' | 'pendingRefund' | 'newLeads' | 'products' | 'articles'>, activity: CommerceActivity[] = []): Dashboard {
  const range = dashboardRange(data)
  const trend: Dashboard['trend'] = []
  for (let date = range.startDate; date <= range.endDate; date = shiftDate(date, 1)) trend.push({ date, grossRevenue: 0, refundedAmount: 0, netRevenue: 0, paidOrders: 0 })
  const days = new Map(trend.map(day => [day.date, day]))
  const buyers = new Map<string, number>()
  const completedRefunds = new Map<string, number>()
  for (const refund of refunds) if (refund.status === 'SUCCEEDED') completedRefunds.set(refund.orderId, (completedRefunds.get(refund.orderId) || 0) + refund.amount)
  let grossRevenue = 0, refundedAmount = 0, paidOrders = 0
  const funnel = { created: 0, paid: 0, received: 0 }
  for (const order of orders) {
    const day = days.get(beijingDate(new Date(order.createdAt)))
    if (!day) continue
    funnel.created++
    if (!['PAID', 'REFUNDING', 'REFUNDED'].includes(order.paymentStatus)) continue
    const refund = completedRefunds.get(order.id) || 0
    funnel.paid++; paidOrders++; day.paidOrders++
    if (order.fulfillmentStatus === 'RECEIVED') funnel.received++
    grossRevenue += order.quote.total; refundedAmount += refund
    day.grossRevenue += order.quote.total; day.refundedAmount += refund; day.netRevenue += order.quote.total - refund
    if (order.buyerId && order.quote.total > refund) buyers.set(order.buyerId, (buyers.get(order.buyerId) || 0) + 1)
  }
  const repeatBuyers = [...buyers.values()].filter(count => count >= 2).length
  const start = Date.parse(`${range.startDate}T00:00:00+08:00`), end = Date.parse(`${shiftDate(range.endDate, 1)}T00:00:00+08:00`)
  const visitors = new Map<string, number>()
  for (const event of activity) {
    const visit = Date.parse(event.visitedAt), cart = Date.parse(event.cartAddedAt || '')
    if (!event.buyerId || visit < start || visit >= end || !Number.isFinite(visit)) continue
    const existing = visitors.get(event.buyerId) ?? Infinity
    visitors.set(event.buyerId, Math.min(existing, cart >= visit && cart < end ? cart : Infinity))
  }
  let cartUsers = 0, createdUsers = 0, paidUsers = 0, receivedUsers = 0
  for (const [buyer, cart] of visitors) {
    if (!Number.isFinite(cart)) continue
    cartUsers++
    const cohort = orders.filter(o => o.buyerId === buyer && Date.parse(o.createdAt) >= cart && Date.parse(o.createdAt) < end)
    if (cohort.length) createdUsers++
    const paid = cohort.filter(o => ['PAID', 'REFUNDING', 'REFUNDED'].includes(o.paymentStatus))
    if (paid.length) paidUsers++
    if (paid.some(o => o.fulfillmentStatus === 'RECEIVED')) receivedUsers++
  }
  const startedAt = activity.map(a => a.visitedAt).sort()[0] || null
  const behaviorFunnel = { visitors: visitors.size, cartUsers, createdUsers, paidUsers, receivedUsers, startedAt, partial: !startedAt || start < Date.parse(startedAt) }
  return { ...counts, ...range, revenue: grossRevenue - refundedAmount, grossRevenue, refundedAmount, paidOrders, buyers: buyers.size, repeatBuyers, repurchaseRate: buyers.size ? repeatBuyers / buyers.size * 100 : 0, trend, funnel, behaviorFunnel }
}
