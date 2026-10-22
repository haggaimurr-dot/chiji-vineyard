export const money = (cents: number) => (cents / 100).toFixed(2)

type OrderState = { orderStatus: string; paymentStatus: string; fulfillmentStatus: string; refundStatus: string }

const orderLabels: Record<string, string> = { PENDING_PAYMENT: '待付款', CONFIRMED: '已确认', CANCELLED: '已取消', CLOSED: '已关闭', COMPLETED: '已完成' }
const paymentLabels: Record<string, string> = { UNPAID: '未付款', PAID: '已付款', REFUNDING: '退款中', REFUNDED: '已退款' }
const fulfillmentLabels: Record<string, string> = { UNFULFILLED: '未发货', PREPARING: '备货中', SHIPPED: '运输中', RECEIVED: '已收货' }
const refundLabels: Record<string, string> = { NONE: '无退款', APPLIED: '退款审核中', APPROVED: '退款处理中', REJECTED: '退款未通过', SUCCEEDED: '退款成功', FAILED: '退款异常待处理', PARTIAL: '已部分退款' }

export const refundStatusText = (status: string) => refundLabels[status] || '退款状态待确认'

export const orderStatusText = (order: OrderState) => {
  if (order.orderStatus === 'CANCELLED') return '已取消'
  if (order.orderStatus === 'CLOSED') return '已关闭'
  if (order.refundStatus !== 'NONE' && refundLabels[order.refundStatus]) return order.refundStatus === 'SUCCEEDED' ? '已退款' : refundLabels[order.refundStatus]
  if (order.paymentStatus === 'REFUNDED') return '已退款'
  if (order.paymentStatus === 'REFUNDING') return '退款处理中'
  if (order.orderStatus === 'COMPLETED') return '已完成'
  if (order.fulfillmentStatus === 'RECEIVED') return '已收货'
  if (order.orderStatus === 'PENDING_PAYMENT' && order.paymentStatus === 'UNPAID') return '待付款'
  if (order.orderStatus === 'CONFIRMED' && order.paymentStatus === 'PAID') {
    if (order.fulfillmentStatus === 'SHIPPED') return '运输中'
    if (['UNFULFILLED', 'PREPARING'].includes(order.fulfillmentStatus)) return '待发货'
  }
  return orderLabels[order.orderStatus] || '订单状态待确认'
}

export function orderPresentation(order: OrderState) {
  const canPay = order.orderStatus === 'PENDING_PAYMENT' && order.paymentStatus === 'UNPAID'
  const active = order.orderStatus === 'CONFIRMED' && order.paymentStatus === 'PAID'
    && ['NONE', 'REJECTED', 'PARTIAL'].includes(order.refundStatus)
  const canSubscribeShipping = active && ['UNFULFILLED', 'PREPARING', 'SHIPPED'].includes(order.fulfillmentStatus)
  const canDemoShip = active && ['UNFULFILLED', 'PREPARING'].includes(order.fulfillmentStatus)
  const canConfirm = !['CANCELLED', 'CLOSED'].includes(order.orderStatus) && order.fulfillmentStatus === 'SHIPPED'
    && ['PAID', 'REFUNDED'].includes(order.paymentStatus)
  let statusHint = '可查看订单、付款、配送和售后进度'
  if (order.orderStatus === 'CANCELLED') statusHint = '订单已取消，可再次购买所需商品'
  else if (order.orderStatus === 'CLOSED') statusHint = '订单已关闭，如有疑问请联系客服'
  else if (['APPLIED', 'APPROVED', 'FAILED'].includes(order.refundStatus) || order.paymentStatus === 'REFUNDING') statusHint = '请在退款与售后中查看审核结果和退款记录'
  else if (order.paymentStatus === 'REFUNDED' || order.refundStatus === 'SUCCEEDED') statusHint = '退款已完成，请留意原支付账户到账情况'
  else if (canPay) statusHint = '请在下单后 30 分钟内完成支付'
  else if (order.fulfillmentStatus === 'RECEIVED' || order.orderStatus === 'COMPLETED') statusHint = '已确认收货，感谢你对迟记葡萄牙的信任'
  else if (order.fulfillmentStatus === 'SHIPPED') statusHint = '葡萄正在路上，请留意配送电话'
  else if (active) statusHint = '商家正在准备鲜果，发货后可查看物流信息'
  return {
    statusText: orderStatusText(order), statusHint,
    orderStateText: orderLabels[order.orderStatus] || '订单状态待确认',
    paymentStateText: paymentLabels[order.paymentStatus] || '付款状态待确认',
    fulfillmentStateText: fulfillmentLabels[order.fulfillmentStatus] || '配送状态待确认',
    refundStateText: refundStatusText(order.refundStatus),
    canPay, canConfirm, canSubscribeShipping, canDemoShip,
  }
}
