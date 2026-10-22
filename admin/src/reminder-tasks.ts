export type ReminderTask = { openid: string; productId: string; productName: string; kind: string; status: string; detail: string; updatedAt: string }
export const reminderLabels: Record<string, string> = { NONE: '待授权', READY: '等待触发', SENDING: '发送中', SENT: '已发送', FAILED: '发送失败', BLOCKED: '授权失效', UNCERTAIN: '待核实', CANCELLED: '已停止' }
export function resolutionStatus(status: string, action: string) {
  if (action === 'retry' && status === 'FAILED') return 'READY'
  if (action === 'sent' && status === 'UNCERTAIN') return 'SENT'
  if (action === 'stop' && ['FAILED', 'BLOCKED', 'UNCERTAIN'].includes(status)) return 'CANCELLED'
  throw new Error('任务状态已变化或不支持此处置，请刷新后重试')
}
let tasks: ReminderTask[] = ['READY', 'FAILED', 'BLOCKED', 'UNCERTAIN', 'SENDING', 'SENT', 'NONE'].map((status, i) => ({ openid: `demo-reminder-${i}`, productId: i % 2 ? 'p-kyoho' : 'p-shine', productName: i % 2 ? '巨峰葡萄' : '阳光玫瑰', kind: i % 2 ? 'pricedrop' : 'restock', status, detail: status === 'FAILED' ? '微信明确拒绝：模板参数无效' : status === 'BLOCKED' ? '用户订阅授权已失效' : status === 'UNCERTAIN' ? '请求超时，结果待核实' : '', updatedAt: new Date().toISOString() }))
const history = tasks.filter(t => !['READY', 'NONE'].includes(t.status)).map((t, i) => ({ ...t, id: `demo-attempt-${i}`, createdAt: t.updatedAt, skuName: t.kind === 'pricedrop' ? '家庭装' : '', currentPrice: 6990, targetPrice: 7000 }))
export function reminderDemo(action: string, data: any) {
  if (action === 'admin.reminder.tasks') {
    const counts: Record<string, number> = {}
    tasks.forEach(t => { counts[t.status] = (counts[t.status] || 0) + 1 })
    const items = tasks.filter(t => (!data.kind || t.kind === data.kind) && (!data.status || (data.status === 'issues' ? ['FAILED', 'BLOCKED', 'UNCERTAIN', 'SENDING'].includes(t.status) : t.status === data.status)))
    const offset = data.offset || 0, limit = data.limit || 20
    return { counts, items: structuredClone(items.slice(offset, offset + limit)), hasMore: offset + limit < items.length }
  }
  if (action === 'admin.reminder.history') {
    const items = history.filter(h => h.openid === data.openid), offset = data.offset || 0
    return { items: structuredClone(items.slice(offset, offset + 20)), hasMore: offset + 20 < items.length }
  }
  const task = tasks.find(t => t.openid === data.openid && t.productId === data.productId && t.kind === data.kind)
  if (!task || task.status !== data.status) throw new Error('任务状态已变化，请刷新后处置')
  if (!data.note?.trim() || [...data.note.trim()].length > 240) throw new Error('请填写 1–240 字处置备注')
  const before = task.status, next = resolutionStatus(before, data.action)
  task.status = next; task.detail = `人工处置：${data.note.trim()}`; task.updatedAt = new Date().toISOString()
  if (before === 'UNCERTAIN') history.filter(h => h.openid === task.openid && h.status === 'UNCERTAIN').forEach(h => { h.status = next; h.detail = task.detail })
  return { status: next }
}
