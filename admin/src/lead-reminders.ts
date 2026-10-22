import type { Lead } from './types'

export type ReminderState = 'none' | 'done' | 'overdue' | 'today' | 'scheduled'

export function normalizeLead(lead: Lead): Lead {
  return { ...lead, contacts: lead.contacts ?? [], contracts: lead.contracts ?? [], receipts: lead.receipts ?? [], externalReminder: lead.externalReminder ?? false, customerProfile: lead.customerProfile ?? { email: '', address: '', industry: '', notes: '' }, opportunityAmount: lead.opportunityAmount ?? 0, ownerId: lead.ownerId || '', ownerName: lead.ownerName || '', reminderAt: lead.reminderAt || '', reminderDone: Boolean(lead.reminderDone), version: lead.version || 0 }
}

export function reminderState(lead: Pick<Lead, 'reminderAt' | 'reminderDone' | 'status'>, now = new Date()): ReminderState {
  if (!lead.reminderAt) return 'none'
  if (lead.reminderDone) return 'done'
  if (lead.status === 'WON' || lead.status === 'LOST') return 'none'
  const target = new Date(lead.reminderAt)
  if (Number.isNaN(target.getTime())) return 'none'
  if (target.getTime() <= now.getTime()) return 'overdue'
  if (target.getFullYear() === now.getFullYear() && target.getMonth() === now.getMonth() && target.getDate() === now.getDate()) return 'today'
  return 'scheduled'
}

/** datetime-local uses the browser's wall clock; keep UTC conversion at the API boundary. */
export function toLocalDateTime(value: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (number: number) => String(number).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export function fromLocalDateTime(value: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime()) || toLocalDateTime(date.toISOString()) !== value) throw new Error('请选择有效的提醒时间')
  return date.toISOString()
}
