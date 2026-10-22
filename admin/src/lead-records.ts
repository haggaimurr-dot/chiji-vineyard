import type { Lead, LeadReceipt, LeadRecords } from './types'

function text(value: unknown, limit: number, required = false): string {
  if (typeof value !== 'string' || [...value.trim()].length > limit || (required && !value.trim())) throw new Error('CRM 内容为空或过长')
  return value.trim()
}
function date(value: string, required = false): boolean {
  return !value ? !required : /^\d{4}-\d{2}-\d{2}$/.test(value) && Number.isFinite(Date.parse(value)) && new Date(value).toISOString().slice(0, 10) === value
}
function amount(value: unknown) {
  if (!Number.isSafeInteger(value) || Number(value) <= 0 || Number(value) > 100000000000) throw new Error('金额必须大于零且不超过10亿元')
  return value as number
}
export function validateLeadRecords(input: Record<string, unknown>, previous: LeadReceipt[] = []): LeadRecords {
  for (const [key, limit] of [['contacts', 50], ['contracts', 100], ['receipts', 500]] as const) {
    if (!Array.isArray(input[key]) || (input[key] as unknown[]).length > limit || (input[key] as unknown[]).some(v => !v || typeof v !== 'object')) throw new Error('联系人/合同/回款记录格式或数量无效')
  }
  const contacts = (input.contacts as any[]).map(c => {
    if (typeof c.primary !== 'boolean') throw new Error('主要联系人格式无效')
    const result = { id: text(c.id, 80, true), name: text(c.name, 100, true), phone: text(c.phone, 50), email: text(c.email, 200), title: text(c.title, 100), primary: c.primary }
    if (result.email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(result.email)) throw new Error('联系人邮箱无效')
    return result
  })
  if (new Set(contacts.map(c => c.id)).size !== contacts.length || contacts.filter(c => c.primary).length > 1) throw new Error('联系人编号重复或主要联系人超过一个')
  const contracts = (input.contracts as any[]).map(c => {
    const result = { id: text(c.id, 80, true), number: text(c.number, 100, true), amount: amount(c.amount), signedAt: text(c.signedAt, 10), dueAt: text(c.dueAt, 10), status: c.status }
    if (!['DRAFT', 'SIGNED', 'CANCELLED'].includes(result.status) || !date(result.signedAt, result.status === 'SIGNED') || !date(result.dueAt) || (result.dueAt && result.signedAt && result.dueAt < result.signedAt)) throw new Error('合同状态或日期无效')
    return result
  })
  if (new Set(contracts.map(c => c.id)).size !== contracts.length || new Set(contracts.map(c => c.number)).size !== contracts.length) throw new Error('合同编号重复')
  const totals = new Map<string, number>()
  const receipts = (input.receipts as any[]).map(r => {
    const result = { id: text(r.id, 80, true), contractId: text(r.contractId, 80, true), amount: amount(r.amount), receivedAt: text(r.receivedAt, 10, true), reference: text(r.reference, 200, true) }
    const contract = contracts.find(c => c.id === result.contractId)
    if (!contract || contract.status !== 'SIGNED' || !date(result.receivedAt, true)) throw new Error('回款必须关联已签订合同，日期必须有效')
    const total = (totals.get(contract.id) ?? 0) + result.amount
    if (total > contract.amount) throw new Error('累计回款不能超过合同金额')
    totals.set(contract.id, total)
    return result
  })
  if (new Set(receipts.map(r => r.id)).size !== receipts.length || new Set(receipts.map(r => r.reference)).size !== receipts.length) throw new Error('回款编号或流水号重复')
  for (const r of previous) {
    const next = receipts.find(item => item.id === r.id)
    if (!next || Object.keys(r).some(key => next[key as keyof LeadReceipt] !== r[key as keyof LeadReceipt])) throw new Error('已登记回款不可修改或删除')
  }
  return { contacts, contracts, receipts }
}

export function calendarReminder(lead: Lead, now = new Date()): string {
  if (!lead.reminderAt || lead.reminderDone || ['WON', 'LOST'].includes(lead.status)) throw new Error('请先保存有效的待办提醒')
  const stamp = (d: Date) => d.toISOString().replace(/[-:]/g, '').replace(/\.\d+Z$/, 'Z')
  const escape = (v: string) => v.replace(/\\/g, '\\\\').replace(/\r\n|\r|\n/g, '\\n').replace(/;/g, '\\;').replace(/,/g, '\\,')
  const lines = ['BEGIN:VCALENDAR', 'VERSION:2.0', 'PRODID:-//Chiji//CRM//ZH', 'BEGIN:VEVENT', `UID:${escape(lead.id)}@chiji-crm`, `SEQUENCE:${lead.version}`, `DTSTAMP:${stamp(now)}`, `DTSTART:${stamp(new Date(lead.reminderAt))}`, `DTEND:${stamp(new Date(Date.parse(lead.reminderAt) + 15 * 60000))}`, `SUMMARY:${escape('跟进：' + lead.organization)}`, `DESCRIPTION:${escape('负责人：' + (lead.ownerName || '未分配') + '\n线索编号：' + lead.id)}`, 'BEGIN:VALARM', 'TRIGGER:PT0M', 'ACTION:DISPLAY', 'DESCRIPTION:合作线索跟进提醒', 'END:VALARM', 'END:VEVENT', 'END:VCALENDAR']
  // Fold at UTF-8 byte boundaries so non-ASCII names remain valid in calendar clients.
  return lines.map(line => { let out = '', count = 0; for (const ch of line) { const size = new TextEncoder().encode(ch).length; if (count + size > 75) { out += '\r\n '; count = 1 }; out += ch; count += size }; return out }).join('\r\n') + '\r\n'
}
