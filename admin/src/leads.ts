import { validateLeadRecords } from './lead-records'
import type { AdminOwner, Lead, LeadHistory, LeadListResult, LeadStatus, CustomerProfile, LeadFunnel } from './types'

export const LEAD_STORAGE_KEY = 'chiji_admin_demo_leads_v1'
const statuses: LeadStatus[] = ['NEW', 'CONTACTED', 'QUALIFIED', 'WON', 'LOST']
type StoredLead = Omit<Lead, 'followUps' | 'ownerId' | 'ownerName' | 'reminderAt' | 'reminderDone' | 'version'> & Partial<Pick<Lead, 'followUps' | 'ownerId' | 'ownerName' | 'reminderAt' | 'reminderDone' | 'version'>>
type LeadStorage = Pick<Storage, 'getItem' | 'setItem'>
interface LeadState { version: 1; items: Lead[]; history: LeadHistory[] }

export function normalizeLead(lead: StoredLead): Lead {
  const followUps = (lead.followUps || []).map(entry => ({ ...entry }))
  if (lead.followUpNote.trim() && !followUps.length) followUps.push({ id: `legacy-${lead.id}`, note: lead.followUpNote, status: lead.status, previousStatus: lead.status, createdAt: '', username: '历史记录', legacy: true })
  return { ...lead, contacts: lead.contacts ?? [], contracts: lead.contracts ?? [], receipts: lead.receipts ?? [], externalReminder: lead.externalReminder ?? false, customerProfile: lead.customerProfile ?? { email: '', address: '', industry: '', notes: '' }, opportunityAmount: lead.opportunityAmount ?? 0, followUps, ownerId: lead.ownerId ?? '', ownerName: lead.ownerName ?? '', reminderAt: lead.reminderAt ?? '', reminderDone: lead.reminderDone ?? false, version: lead.version ?? 0 }
}

function compareDesc(a: string, b: string): number { return a === b ? 0 : a > b ? -1 : 1 }
function facets(leads: Lead[], field: 'channel' | 'region'): string[] { return [...new Set(leads.map(lead => lead[field].trim()).filter(Boolean))].sort() }
export function pendingLeadReminder(lead: Lead): boolean { return !!lead.reminderAt && !lead.reminderDone && !['WON', 'LOST'].includes(lead.status) }

export function listLeads(leads: StoredLead[], query: Record<string, unknown> = {}): LeadListResult {
  const all = leads.map(normalizeLead)
  const status = String(query.status || '').trim(), channel = String(query.channel || '').trim(), region = String(query.region || '').trim(), ownerId = String(query.ownerId || '').trim(), reminder = String(query.reminder || '').trim()
  if (status && !statuses.includes(status as LeadStatus)) throw new Error('线索状态无效')
  if (!['', 'due', 'upcoming', 'done', 'none'].includes(reminder)) throw new Error('提醒筛选条件无效')
  const words = String(query.keyword || '').trim().toLowerCase().split(/\s+/).filter(Boolean), now = Date.now()
  const items = all.filter(lead => {
    const text = [lead.organization, lead.contact, lead.phone, lead.region, lead.channel, lead.volume, lead.demand, lead.followUpNote, lead.ownerName, ...Object.values(lead.customerProfile ?? {}), ...(lead.contacts ?? []).flatMap(c => [c.name, c.phone, c.email, c.title]), ...(lead.contracts ?? []).map(c => c.number), ...(lead.receipts ?? []).map(r => r.reference), ...lead.followUps.map(entry => entry.note)].join(' ').toLowerCase()
    const matchesReminder = !reminder || (reminder === 'due' && pendingLeadReminder(lead) && Date.parse(lead.reminderAt) <= now) || (reminder === 'upcoming' && pendingLeadReminder(lead) && Date.parse(lead.reminderAt) > now) || (reminder === 'done' && !!lead.reminderAt && lead.reminderDone) || (reminder === 'none' && !lead.reminderAt)
    return (!status || lead.status === status) && (!channel || lead.channel.trim() === channel) && (!region || lead.region.trim() === region) && (!ownerId || lead.ownerId === (ownerId === '__unassigned__' ? '' : ownerId)) && matchesReminder && words.every(word => text.includes(word))
  }).sort((a, b) => a.createdAt === b.createdAt ? compareDesc(a.id, b.id) : compareDesc(a.createdAt, b.createdAt))
  const limit = Number.isInteger(query.limit) && Number(query.limit) > 0 ? Math.min(Number(query.limit), 100) : 20
  const offset = Number.isInteger(query.offset) && Number(query.offset) >= 0 ? Number(query.offset) : 0
  return { items: items.slice(offset, offset + limit), total: items.length, channels: facets(all, 'channel'), regions: facets(all, 'region'), limit, offset }
}

export function leadTimestamp(value: string): string {
  const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$/.exec(value)
  if (!parts) throw new Error('提醒时间必须为含时区的有效 ISO 时间')
  const year = Number(parts[1]), month = Number(parts[2]), day = Number(parts[3])
  const days = [31, year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  const zone = parts[7]
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1] || Number(parts[4]) > 23 || Number(parts[5]) > 59 || Number(parts[6]) > 59 || (zone !== 'Z' && (Number(zone.slice(1, 3)) > 23 || Number(zone.slice(4)) > 59)) || !Number.isFinite(Date.parse(value))) throw new Error('提醒时间无效')
  return new Date(value).toISOString()
}

function validStoredLead(value: unknown): Lead {
  if (!value || typeof value !== 'object') throw new Error('线索格式无效')
  const item = value as Record<string, any>
  if (!['id', 'organization', 'contact', 'phone', 'region', 'channel', 'volume', 'demand', 'followUpNote', 'createdAt'].every(field => typeof item[field] === 'string') || !item.id || !statuses.includes(item.status)) throw new Error('线索格式无效')
  leadTimestamp(item.createdAt)
  if (item.followUps !== undefined && (!Array.isArray(item.followUps) || item.followUps.some((entry: any) => !entry || !['id', 'note', 'createdAt', 'username'].every(field => typeof entry[field] === 'string') || !statuses.includes(entry.status) || !statuses.includes(entry.previousStatus) || (entry.createdAt !== '' && !Number.isFinite(Date.parse(entry.createdAt))) || (entry.legacy !== undefined && typeof entry.legacy !== 'boolean')))) throw new Error('历史跟进格式无效')
  for (const field of ['ownerId', 'ownerName', 'reminderAt']) if (item[field] !== undefined && typeof item[field] !== 'string') throw new Error('分配或提醒格式无效')
  if ((item.reminderDone !== undefined && typeof item.reminderDone !== 'boolean') || (item.version !== undefined && (!Number.isSafeInteger(item.version) || item.version < 0))) throw new Error('提醒或版本号无效')
  if (item.customerProfile !== undefined) validateCustomerProfile(item.customerProfile)
  if (item.opportunityAmount !== undefined) validateOpportunityAmount(item.opportunityAmount)
  const lead = normalizeLead(item as StoredLead)
  if ((!lead.ownerId && lead.ownerName) || (!lead.reminderAt && lead.reminderDone)) throw new Error('分配或提醒格式无效')
  if (lead.reminderAt) lead.reminderAt = leadTimestamp(lead.reminderAt)
  return lead
}

function validStoredHistory(value: unknown, ids: Set<string>): LeadHistory {
  const entry = value as LeadHistory
  if (!entry || !['id', 'leadId', 'actorId', 'actorName', 'createdAt', 'note', 'ownerId', 'ownerName', 'reminderAt'].every(field => typeof (entry as any)[field] === 'string') || !entry.id || !ids.has(entry.leadId) || !statuses.includes(entry.status) || typeof entry.reminderDone !== 'boolean' || !Array.isArray(entry.changes) || entry.changes.some(field => !['owner', 'status', 'note', 'reminder', 'profile', 'amount'].includes(field)) || (!entry.reminderAt && entry.reminderDone)) throw new Error('跟进历史格式无效')
  if (entry.createdAt) leadTimestamp(entry.createdAt)
  if (entry.reminderAt) leadTimestamp(entry.reminderAt)
  return structuredClone(entry)
}

function defaultStorage(): LeadStorage | undefined {
  try { return typeof localStorage === 'undefined' ? undefined : localStorage } catch { throw new Error('无法访问演示线索存储，请检查浏览器存储权限后重试') }
}

export function createDemoLeadStore(seed: StoredLead[], initialHistory: LeadHistory[] = [], storage = defaultStorage()) {
  let state: LeadState = { version: 1, items: seed.map(normalizeLead), history: structuredClone(initialHistory) }
  const initial = structuredClone(state)
  function read(): LeadState {
    if (!storage) return structuredClone(state)
    let raw: string | null
    try { raw = storage.getItem(LEAD_STORAGE_KEY) } catch { throw new Error('无法读取演示线索，请检查浏览器存储权限后重试') }
    if (raw === null) { state = structuredClone(initial); return structuredClone(state) }
    try {
      const saved = JSON.parse(raw)
      if (saved?.version !== 1 || !Array.isArray(saved.items) || (saved.history !== undefined && !Array.isArray(saved.history))) throw new Error('数据版本无效')
      const items = saved.items.map(validStoredLead) as Lead[], ids = new Set(items.map(lead => lead.id))
      if (ids.size !== items.length) throw new Error('线索编号重复')
      const history = (saved.history || []).map((entry: unknown) => validStoredHistory(entry, ids)) as LeadHistory[]
      if (new Set(history.map(entry => entry.id)).size !== history.length) throw new Error('历史编号重复')
      state = { version: 1, items, history }
      return structuredClone(state)
    } catch { throw new Error('演示线索存储格式异常，已保留原数据；请备份并修复本地存储后重试') }
  }
  function update(data: Record<string, unknown>, actor: AdminOwner, owners: AdminOwner[]): Lead {
    const current = read(), index = current.items.findIndex(lead => lead.id === (typeof data.leadId === 'string' ? data.leadId.trim() : data.leadId))
    if (index < 0) throw new Error('合作线索不存在')
    const change = updateLeadWorkflow(current.items[index], data, actor, owners)
    if (!change.history) return change.lead
    current.items[index] = change.lead; current.history.unshift(change.history)
    // Commit in memory only after storage accepts the entire lead/history envelope.
    try { storage?.setItem(LEAD_STORAGE_KEY, JSON.stringify(current)) } catch { throw new Error('演示线索保存失败，请检查浏览器存储空间后重试') }
    state = current
    return structuredClone(change.lead)
  }
  function get(leadId: string): Lead {
    const lead = read().items.find(item => item.id === leadId)
    if (!lead) throw new Error('合作线索不存在')
    return lead
  }
  function history(leadId: string): LeadHistory[] {
    const current = read(), lead = current.items.find(item => item.id === leadId)
    if (!lead) throw new Error('合作线索不存在')
    const entries = current.history.filter(entry => entry.leadId === leadId), known = new Set(entries.map(entry => entry.id))
    for (const entry of lead.followUps) {
      if (known.has(entry.id)) continue
      const changes: LeadHistory['changes'] = []
      if (entry.previousStatus && entry.previousStatus !== entry.status) changes.push('status')
      if (entry.note) changes.push('note')
      entries.push({ id: entry.id, leadId, actorId: '', actorName: entry.username, createdAt: entry.createdAt, note: entry.note, status: entry.status, ownerId: '', ownerName: '', reminderAt: '', reminderDone: false, changes })
      known.add(entry.id)
    }
    return entries.sort((a, b) => (Date.parse(b.createdAt) || 0) - (Date.parse(a.createdAt) || 0) || compareDesc(a.id, b.id))
  }
  function reminders(): Lead[] { return read().items.filter(pendingLeadReminder).sort((a, b) => Date.parse(a.reminderAt) - Date.parse(b.reminderAt) || compareDesc(b.id, a.id)) }
  return { read, get, update, history, reminders }
}

let historySequence = 0
export function updateLeadWorkflow(current: Lead, data: Record<string, unknown>, actor: AdminOwner, owners: AdminOwner[]): { lead: Lead; history?: LeadHistory } {
  if (Object.keys(data).some(key => !['leadId', 'expectedVersion', 'status', 'followUpNote', 'ownerId', 'reminderAt', 'reminderDone', 'customerProfile', 'opportunityAmount', 'contacts', 'contracts', 'receipts', 'externalReminder'].includes(key))) throw new Error('线索更新参数无效')
  if (!Number.isSafeInteger(data.expectedVersion) || Number(data.expectedVersion) < 0) throw new Error('请提供有效的 expectedVersion 版本号')
  if (data.expectedVersion !== current.version) throw new Error('线索版本已被其他人更新，请刷新后重试')
  const lead = structuredClone(current), has = (key: string) => Object.prototype.hasOwnProperty.call(data, key), changes: LeadHistory['changes'] = []
  const records = validateLeadRecords({ contacts: has('contacts') ? data.contacts : (current.contacts ?? []), contracts: has('contracts') ? data.contracts : (current.contracts ?? []), receipts: has('receipts') ? data.receipts : (current.receipts ?? []) }, current.receipts ?? [])
  for (const key of ['contacts', 'contracts', 'receipts'] as const) {
    if (has(key)) { lead[key] = records[key] as any; if (JSON.stringify(lead[key]) !== JSON.stringify(current[key] ?? [])) changes.push(key) }
  }
  if (has('externalReminder')) {
    if (typeof data.externalReminder !== 'boolean') throw new Error('外部提醒格式无效')
    lead.externalReminder = data.externalReminder
    if (lead.externalReminder !== Boolean(current.externalReminder)) changes.push('externalReminder')
  }
  if (has('customerProfile')) {
    lead.customerProfile = validateCustomerProfile(data.customerProfile)
    if (JSON.stringify(lead.customerProfile) !== JSON.stringify(normalizeLead(current).customerProfile)) changes.push('profile')
  }
  if (has('opportunityAmount')) {
    validateOpportunityAmount(data.opportunityAmount)
    lead.opportunityAmount = data.opportunityAmount as number
    if (lead.opportunityAmount !== (current.opportunityAmount ?? 0)) changes.push('amount')
  }
  if (has('ownerId')) {
    if (typeof data.ownerId !== 'string' || Array.from(data.ownerId.trim()).length > 80) throw new Error('负责人参数无效')
    const ownerId = data.ownerId.trim(), owner = ownerId ? owners.find(item => item.id === ownerId && item.active) : undefined
    if (ownerId && !owner) throw new Error('负责人不存在或已停用，请刷新后重试')
    lead.ownerId = ownerId; lead.ownerName = owner?.username || ''
    if (lead.ownerId !== current.ownerId || lead.ownerName !== current.ownerName) changes.push('owner')
  }
  if (has('status')) {
    if (typeof data.status !== 'string' || !statuses.includes(data.status.trim() as LeadStatus)) throw new Error('无效线索状态')
    lead.status = data.status.trim() as LeadStatus
    if (lead.status !== current.status) changes.push('status')
  }
  let note = ''
  if (has('followUpNote')) {
    if (typeof data.followUpNote !== 'string') throw new Error('跟进备注格式无效')
    note = data.followUpNote.trim()
    if (Array.from(note).length > 2000) throw new Error('跟进备注不能超过 2000 字')
    if (note) { lead.followUpNote = note; changes.push('note') }
  }
  if (has('reminderDone') && typeof data.reminderDone !== 'boolean') throw new Error('提醒完成状态无效')
  let reminderChanged = false
  if (has('reminderAt')) {
    if (typeof data.reminderAt !== 'string') throw new Error('提醒时间格式无效')
    lead.reminderAt = data.reminderAt.trim() ? leadTimestamp(data.reminderAt.trim()) : ''
    reminderChanged = lead.reminderAt !== current.reminderAt
    if (!lead.reminderAt || reminderChanged) lead.reminderDone = false
  }
  if (has('reminderDone')) lead.reminderDone = data.reminderDone as boolean
  if (!lead.reminderAt && lead.reminderDone) throw new Error('请先设置提醒时间')
  const scheduled = lead.reminderAt && !lead.reminderDone && (reminderChanged || current.reminderDone)
  if (['WON', 'LOST'].includes(lead.status)) {
    if (scheduled) throw new Error('已成交或关闭的线索不能设置待办提醒')
    if (lead.reminderAt) lead.reminderDone = true
  } else if (scheduled && Date.parse(lead.reminderAt) <= Date.now()) throw new Error('新提醒时间必须晚于当前时间')
  if (lead.reminderAt !== current.reminderAt || lead.reminderDone !== current.reminderDone) changes.push('reminder')
  if (!changes.length) return { lead }
  const createdAt = new Date().toISOString(), id = `lead-history-${Date.now()}-${String(++historySequence).padStart(8, '0')}-${Math.random().toString(36).slice(2, 8)}`
  const history: LeadHistory = { id, leadId: lead.id, actorId: actor.id, actorName: actor.username, createdAt, note, status: lead.status, ownerId: lead.ownerId, ownerName: lead.ownerName, reminderAt: lead.reminderAt, reminderDone: lead.reminderDone, customerProfile: structuredClone(lead.customerProfile), opportunityAmount: lead.opportunityAmount, contacts: structuredClone(lead.contacts ?? []), contracts: structuredClone(lead.contracts ?? []), receipts: structuredClone(lead.receipts ?? []), externalReminder: lead.externalReminder, changes }
  lead.followUps = normalizeLead(current).followUps.concat({ id, note, status: lead.status, previousStatus: current.status, createdAt, username: actor.username })
  lead.version++
  return { lead, history }
}

// Compatibility helpers for consumers that only use follow-up records.
export function recordLeadFollowUp(lead: StoredLead, status: unknown, value: unknown, username: string): Lead {
  const current = normalizeLead(lead)
  return updateLeadWorkflow(current, { status, followUpNote: value, expectedVersion: current.version }, { id: '', username, role: 'OPERATOR', active: true }, []).lead
}
export function restoreDemoLeads(seed: StoredLead[], storage = defaultStorage()): Lead[] { return createDemoLeadStore(seed, [], storage).read().items }
export function persistDemoLeads(leads: Lead[], storage = defaultStorage()): void {
  const prior = storage?.getItem(LEAD_STORAGE_KEY), saved = prior ? JSON.parse(prior) : undefined
  storage?.setItem(LEAD_STORAGE_KEY, JSON.stringify({ version: 1, items: leads, history: saved?.history || [] }))
}
export function demoLeadUsername(storage = defaultStorage()): string {
  try { const user = JSON.parse(storage?.getItem('chiji_admin_user') || 'null'); if (typeof user?.username === 'string' && user.username.trim()) return user.username.trim() } catch { /* mockCall resolves the authenticated actor through the session adapter. */ }
  return 'demo'
}

export function validateOpportunityAmount(value: unknown): void {
  if (!Number.isSafeInteger(value) || Number(value) < 0 || Number(value) > 100000000000) throw new Error('商机金额必须为 0–10亿元，最多两位小数')
}
export function validateCustomerProfile(value: unknown): CustomerProfile {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('客户档案格式无效')
  const input = value as Record<string, unknown>, result: CustomerProfile = { email: '', address: '', industry: '', notes: '' }
  if (Object.keys(input).some(key => !(key in result))) throw new Error('客户档案字段无效')
  for (const key of Object.keys(result) as Array<keyof CustomerProfile>) {
    if (typeof input[key] !== 'string') throw new Error('客户档案格式无效')
    result[key] = (input[key] as string).trim()
    if (Array.from(result[key]).length > (key === 'notes' ? 2000 : 200)) throw new Error('客户档案内容过长')
  }
  if (result.email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(result.email)) throw new Error('客户邮箱格式无效')
  return result
}
export function leadFunnel(leads: Lead[], query: Record<string, unknown> = {}): LeadFunnel {
  const stages = statuses.map(status => {
    const scoped = { ...query, status }, count = listLeads(leads, scoped).total
    let amount = 0
    for (let offset = 0; offset < count; offset += 100) amount += listLeads(leads, { ...scoped, limit: 100, offset }).items.reduce((sum, lead) => sum + (lead.opportunityAmount ?? 0), 0)
    return { status, count, amount }
  })
  const total = stages.reduce((sum, stage) => sum + stage.count, 0), amount = stages.reduce((sum, stage) => sum + stage.amount, 0)
  return { stages, total, amount, activeAmount: stages.slice(0, 3).reduce((sum, stage) => sum + stage.amount, 0), wonAmount: stages[3].amount, winRate: total ? stages[3].count / total : 0 }
}
