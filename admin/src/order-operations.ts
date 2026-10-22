import type { Order, OrderExportResult, ShipmentItem } from './types'
import { validateShipment } from './orders'
export { canShip } from './orders'

export const orderFilter = (value: string): string => value === 'shipping' ? 'shipping_pending' : value

export function shipmentItemErrors(item: ShipmentItem): string[] {
  const errors: string[] = []
  if (!item || typeof item !== 'object') return ['发货参数无效']
  if (typeof item.orderId !== 'string' || !/^[A-Za-z0-9_-]{1,80}$/.test(item.orderId.trim())) errors.push('订单ID须为1至80位字母、数字、下划线或短横线')
  if (['carrier', 'carrierCode', 'trackingNo'].some(field => typeof item[field as keyof ShipmentItem] !== 'string')) errors.push('发货信息须为文本')
  else {
    try { validateShipment(item) } catch (error) { errors.push((error as Error).message) }
  }
  return errors
}

export function validateShipmentItems(items: ShipmentItem[]): string[] {
  if (!Array.isArray(items) || !items.length || items.length > 50) return ['每批须选择1至50个订单']
  const errors: string[] = [], orderIds = new Set<string>(), trackingNumbers = new Set<string>()
  items.forEach((item, index) => {
    const itemErrors = shipmentItemErrors(item)
    errors.push(...itemErrors.map(error => `第${index + 1}行：${error}`))
    const id = typeof item?.orderId === 'string' ? item.orderId.trim() : ''
    const tracking = typeof item?.trackingNo === 'string' && typeof item?.carrierCode === 'string' ? `${item.carrierCode.trim().toUpperCase()}:${item.trackingNo.trim().toUpperCase()}` : ''
    if (id && orderIds.has(id)) errors.push(`第${index + 1}行：订单重复`)
    if (!itemErrors.length && tracking && trackingNumbers.has(tracking)) errors.push(`第${index + 1}行：运单号重复`)
    if (id) orderIds.add(id)
    if (!itemErrors.length && tracking) trackingNumbers.add(tracking)
  })
  return errors
}

// Quote every field, neutralize spreadsheet formulas, and preserve identifier zeros.
export function csvCell(value: unknown, text = true): string {
  let cell = value == null ? '' : String(value).replace(/\u0000/g, '')
  const trimmed = cell.replace(/^[\s\u0000-\u001f\u007f-\u009f\uFEFF]+/, '')
  if (text && (/[\t\r\n]/.test(cell) || /^[=+\-@]/.test(trimmed) || /^[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$/.test(trimmed))) cell = `'${cell}`
  return `"${cell.replace(/"/g, '""')}"`
}

const csvTime = (value?: string) => {
  if (!value || !Number.isFinite(Date.parse(value))) return value || ''
  return new Date(Date.parse(value) + 8 * 60 * 60 * 1000).toISOString().slice(0, 19).replace('T', ' ')
}
const label = (state: string | undefined, labels: Record<string, string>) => labels[state || ''] || state || ''

export function exportOrders(orders: Order[], filename = 'orders.csv'): OrderExportResult {
  const headers = ['订单编号', '订单ID', '下单时间', '订单状态', '支付状态', '发货状态', '退款状态', '商品金额（元）', '运费（元）', '优惠金额（元）', '实付金额（元）', '已退款金额（元）', '商品明细', '收件人', '手机号', '省', '市', '区', '详细地址', '预约配送日期', '订单备注', '发票类型', '发票抬头', '税号', '发票邮箱', '发票状态', '发票号码', '快递公司', '快递编码', '运单号', '发货时间', '微信发货同步']
  const lines = orders.map(order => {
    const invoice = order.invoice
    const values = [order.orderNo, order.id, csvTime(order.createdAt),
      label(order.orderStatus, { PENDING_PAYMENT: '待付款', CONFIRMED: '已确认', COMPLETED: '已完成', CANCELLED: '已取消', CLOSED: '已关闭' }),
      label(order.paymentStatus, { UNPAID: '未付款', PAID: '已付款', REFUNDING: '退款中', REFUNDED: '已退款' }),
      label(order.fulfillmentStatus, { UNFULFILLED: '待发货', PREPARING: '备货中', SHIPPED: '已发货', RECEIVED: '已收货' }),
      label(order.refundStatus, { NONE: '无退款', APPLIED: '申请中', APPROVED: '已批准', REJECTED: '已拒绝', FAILED: '退款失败', PARTIAL: '部分退款', SUCCEEDED: '退款成功' }),
      ((order.quote.subtotal || 0) / 100).toFixed(2), ((order.quote.freight || 0) / 100).toFixed(2),
      ((order.quote.discount || 0) / 100).toFixed(2), (order.quote.total / 100).toFixed(2), ((order.refundedAmount || 0) / 100).toFixed(2),
      order.items.map(item => `${item.name} / ${item.skuName} × ${item.quantity}（单价 ${((item.unitPrice || 0) / 100).toFixed(2)} 元）`).join('；'),
      order.address.name, order.address.phone, order.address.province, order.address.city, order.address.district, order.address.detail,
      order.deliveryDate || '', order.note || '', label(invoice?.kind, { personal: '个人', company: '企业' }), invoice?.title || '', invoice?.taxId || '', invoice?.email || '',
      label(invoice?.status, { APPLIED: '待开票', REQUESTED: '待开票', ISSUED: '已开票', REJECTED: '已拒绝' }), invoice?.number || '',
      order.shipment?.carrier || '', order.shipment?.carrierCode || '', order.shipment?.trackingNo || '', csvTime(order.shipment?.shippedAt),
      label(order.shipment?.syncStatus, { PENDING: '待同步', SUCCESS: '已同步', FAILED: '同步失败' })]
    return values.map((value, index) => csvCell(value, index < 7 || index > 11)).join(',')
  })
  return { filename, count: orders.length, content: '\uFEFF' + [headers.map(header => csvCell(header)).join(','), ...lines].join('\r\n') + '\r\n' }
}

export function downloadCSV(result: OrderExportResult): void {
  const blob = new Blob([result.content], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob), link = document.createElement('a')
  link.href = url; link.download = result.filename; link.hidden = true
  document.body.appendChild(link)
  try { link.click() } finally { link.remove(); setTimeout(() => URL.revokeObjectURL(url), 1000) }
}
