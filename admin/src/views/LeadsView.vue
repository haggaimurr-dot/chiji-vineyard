<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { api, dateTime } from '../api'
import { confirmAction } from '../confirm'
import LeadRecordsEditor from '../components/LeadRecordsEditor.vue'
import { validateLeadRecords, calendarReminder } from '../lead-records'
import type { LeadRecords } from '../types'
import { fromLocalDateTime, normalizeLead, reminderState, toLocalDateTime } from '../lead-reminders'
import type { AdminOwner, Lead, LeadFollowUp, LeadHistory, LeadListResult, CustomerProfile, LeadFunnel } from '../types'

const props = defineProps<{ initialLeadId?: string }>()
const labels: Record<Lead['status'], string> = { NEW: '新线索', CONTACTED: '已联系', QUALIFIED: '有意向', WON: '已成交', LOST: '已关闭' }
const changeLabels = { owner: '负责人分配', status: '状态变更', note: '跟进备注', reminder: '提醒调整', profile: '客户档案', amount: '商机金额', contacts: '联系人', contracts: '合同', receipts: '回款', externalReminder: '外部提醒' }
const leads = ref<Lead[]>([]), total = ref(0), channels = ref<string[]>([]), regions = ref<string[]>([])
const keyword = ref(''), searchKeyword = ref(''), status = ref(''), channel = ref(''), region = ref('')
const ownerFilter = ref(''), reminderFilter = ref('')
const owners = ref<AdminOwner[]>([]), pendingReminders = ref<Lead[]>([])
const ownerError = ref(''), reminderError = ref(''), actionError = ref('')
const page = ref(1), pageSize = ref(20), loading = ref(false), error = ref(''), success = ref('')
const exporting = ref(false), exported = ref(0), quickBusy = ref('')
// 公海池与合并
const poolItems = ref<Array<{ lead: Lead; daysIdle: number; lastActive: string }>>([]), poolConfig = ref({ inactiveDays: 7 }), poolBusy = ref(false), poolError = ref(''), poolLoaded = ref(false), poolOpen = ref(false)
const dupItems = ref<Array<{ id: string; organization: string; contact: string; status: string; ownerName: string; createdAt: string; followUpCount: number }>>([])
const mergeSourceIds = ref<string[]>([]), mergeBusy = ref(false)
async function loadPool() {
  if (poolBusy.value) return
  poolBusy.value = true; poolError.value = ''
  try { const r = await api<{ config: { inactiveDays: number }; items: typeof poolItems.value }>('admin.lead.publicPool'); poolConfig.value = r.config; poolItems.value = r.items; poolLoaded.value = true }
  catch (e) { poolError.value = message(e) }
  finally { poolBusy.value = false }
}
async function savePoolConfig() {
  if (poolBusy.value) return
  poolBusy.value = true; poolError.value = ''
  try { await api('admin.lead.publicPool.save', { inactiveDays: poolConfig.value.inactiveDays }); await loadPool(); success.value = '公海规则已更新' }
  catch (e) { poolError.value = message(e) }
  finally { poolBusy.value = false }
}
async function claimLead(id: string) {
  if (poolBusy.value) return
  poolBusy.value = true; poolError.value = ''
  try { await api('admin.lead.update', { id, ownerId: currentUserId.value }); success.value = '已认领回私海'; await loadPool(); await load() }
  catch (e) { poolError.value = message(e) }
  finally { poolBusy.value = false }
}
async function loadDuplicates() {
  if (!selected.value) return
  try { const r = await api<{ items: typeof dupItems.value }>('admin.lead.duplicates', { id: selected.value.id }); dupItems.value = r.items; mergeSourceIds.value = [] }
  catch { dupItems.value = [] }
}
async function mergeLeads() {
  if (!selected.value || !mergeSourceIds.value.length || mergeBusy.value) return
  if (!(await confirmAction({ title: '合并重复线索', message: `确认将 ${mergeSourceIds.value.length} 条重复线索的跟进记录合并到当前线索？来源线索将标记为已关闭。`, danger: true, confirmText: '确认合并' }))) return
  mergeBusy.value = true; saveError.value = ''
  try { const r = await api<{ merged: number; followUpCount: number }>('admin.lead.merge', { targetId: selected.value.id, sourceIds: mergeSourceIds.value }); saveSuccess.value = `已合并 ${r.merged} 条线索，当前共 ${r.followUpCount} 条跟进记录`; mergeSourceIds.value = []; await loadDuplicates(); await load() }
  catch (e) { saveError.value = message(e) }
  finally { mergeBusy.value = false }
}
const selected = ref<Lead | null>(null), dialog = ref<HTMLDialogElement | null>(null)
const note = ref(''), nextStatus = ref<Lead['status']>('NEW'), saving = ref(false), saveError = ref(''), saveSuccess = ref('')
const history = ref<LeadHistory[]>([]), historyLoading = ref(false), historyError = ref('')
const refreshingConflict = ref(false), conflictNeedsRefresh = ref(false)
const draft = reactive({ ownerId: '', reminderLocal: '', reminderDone: false })
const profile = reactive<CustomerProfile>({ email: '', address: '', industry: '', notes: '' })
const amountInput = ref('0.00')
const records = ref<LeadRecords>({ contacts: [], contracts: [], receipts: [] }), recordsConflict = ref(false), externalReminder = ref(false)
const deliveries = ref<Array<{ id: string; status: string; updatedAt: string }>>([]), deliveryError = ref('')
const deliveryLabels: Record<string, string> = { SENDING: '发送中 / 待核实', SENT: '已送达网关', FAILED: '网关拒绝', UNCERTAIN: '结果待核实', CANCELLED: '已取消' }
const savedRecords = (lead: Lead): LeadRecords => ({ contacts: lead.contacts ?? [], contracts: lead.contracts ?? [], receipts: lead.receipts ?? [] })
const recordsChanged = computed(() => JSON.stringify(records.value) !== JSON.stringify(selected.value ? savedRecords(selected.value) : {}))
async function loadDeliveries() {
  if (!selected.value) return
  const id = selected.value.id; deliveryError.value = ''; deliveries.value = []
  try { const result = await api<typeof deliveries.value>('admin.lead.deliveries', { leadId: id }); if (selected.value?.id === id) deliveries.value = result }
  catch (e) { if (selected.value?.id === id) deliveryError.value = message(e) }
}
function useLatestRecords() { if (selected.value) records.value = JSON.parse(JSON.stringify(savedRecords(selected.value))); recordsConflict.value = false }
function downloadCalendar() {
  if (!selected.value) return
  try { const blob = new Blob([calendarReminder(selected.value)], { type: 'text/calendar;charset=utf-8' }); const url = URL.createObjectURL(blob); const a = document.createElement('a'); a.href = url; a.download = 'crm-followup.ics'; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000) } catch (e) { saveError.value = message(e) }
}
const funnel = ref<LeadFunnel | null>(null), funnelError = ref(''), funnelLoading = ref(false)
let funnelRequest = 0
const profileChanged = computed(() => JSON.stringify(profile) !== JSON.stringify(selected.value?.customerProfile))
function money(cents = 0) { return new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' }).format(cents / 100) }
function amountCents() {
  if (!/^\d{1,10}(?:\.\d{1,2})?$/.test(amountInput.value.trim())) throw new Error('商机金额请输入非负数字，最多两位小数')
  const [whole, fraction = ''] = amountInput.value.trim().split('.')
  const cents = Number(whole) * 100 + Number(fraction.padEnd(2, '0'))
  if (cents > 100000000000) throw new Error('商机金额不能超过10亿元')
  return cents
}
interface LeadChannelStat { channel: string; count: number; won: number; opportunityAmount: number }
const channelStats = ref<LeadChannelStat[]>([]), channelTotal = ref<LeadChannelStat | null>(null), channelError = ref(''), channelLoading = ref(false)
const channelStart = ref(''), channelEnd = ref('')
function defaultChannelRange() { const d = new Date(); channelEnd.value = d.toISOString().slice(0, 10); d.setDate(d.getDate() - 29); channelStart.value = d.toISOString().slice(0, 10) }
async function loadChannelStats() {
  channelLoading.value = true; channelError.value = ''
  try { const r = await api<{ items: LeadChannelStat[]; total: LeadChannelStat }>('admin.lead.channelStats', { start: channelStart.value, end: channelEnd.value }); channelStats.value = r.items; channelTotal.value = r.total }
  catch (e) { channelError.value = message(e) }
  finally { channelLoading.value = false }
}
defaultChannelRange()
async function loadFunnel() {
  const request = ++funnelRequest
  funnelLoading.value = true; funnelError.value = ''; funnel.value = null
  try {
    const result = await api<LeadFunnel>('admin.lead.funnel', { ...filters(), status: '' })
    if (request === funnelRequest) funnel.value = result
  } catch (e) { if (request === funnelRequest) funnelError.value = message(e) }
  finally { if (request === funnelRequest) funnelLoading.value = false }
}
const now = ref(new Date())
const localTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone
const currentUserId = (() => { try { return String(JSON.parse(localStorage.getItem('chiji_admin_user') || '{}').id || '') } catch { return '' } })()
const pages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
const first = computed(() => total.value ? (page.value - 1) * pageSize.value + 1 : 0)
const last = computed(() => Math.min(page.value * pageSize.value, total.value))
const finished = computed(() => nextStatus.value === 'WON' || nextStatus.value === 'LOST')
const inactiveOwner = computed(() => draft.ownerId && !owners.value.some(owner => owner.id === draft.ownerId))
const hasChanges = computed(() => !!selected.value && (recordsChanged.value || externalReminder.value !== Boolean(selected.value.externalReminder) || profileChanged.value || amountInput.value !== ((selected.value.opportunityAmount ?? 0) / 100).toFixed(2) || !!note.value.trim() || nextStatus.value !== selected.value.status || draft.ownerId !== selected.value.ownerId || draft.reminderLocal !== toLocalDateTime(selected.value.reminderAt) || draft.reminderDone !== selected.value.reminderDone))
const summary = computed(() => ({
  pending: pendingReminders.value.length,
  overdue: pendingReminders.value.filter(lead => reminderState(lead, now.value) === 'overdue').length,
  today: pendingReminders.value.filter(lead => reminderState(lead, now.value) === 'today').length,
}))
const filters = () => ({ keyword: searchKeyword.value, status: status.value, channel: channel.value, region: region.value, ownerId: ownerFilter.value, reminder: reminderFilter.value })
let requestId = 0, historyRequest = 0, openRequest = 0, ownerRequest = 0, reminderRequest = 0
let mounted = false
let clockTimer: ReturnType<typeof setInterval> | undefined
let pendingLeadId = ''
function message(e: unknown) { return e instanceof Error ? e.message : '请求失败，请稍后重试' }
function isConflict(e: unknown) { return /版本|其他管理员|已被.*(?:修改|更新)|冲突|CONFLICT/i.test(message(e)) }
function changed() { window.dispatchEvent(new CustomEvent('chiji-leads-updated')) }
function formatted(value: string) { return value && !Number.isNaN(new Date(value).getTime()) ? dateTime(value) : '—' }
function state(lead: Lead) { return reminderState(lead, now.value) }
function reminderLabel(lead: Lead) { return { none: '未设置', done: '已完成', overdue: '已到期', today: '今日提醒', scheduled: '待提醒' }[state(lead)] }

async function load() {
  const request = ++requestId
  loading.value = true; error.value = ''; leads.value = []; void loadFunnel()
  try {
    const result = await api<LeadListResult>('admin.lead.list', { ...filters(), limit: pageSize.value, offset: (page.value - 1) * pageSize.value })
    if (request !== requestId) return
    total.value = result.total; channels.value = result.channels; regions.value = result.regions
    if (result.total > 0 && page.value > pages.value) { page.value = pages.value; await load(); return }
    if (!result.total) page.value = 1
    leads.value = result.items.map(normalizeLead)
  } catch (e) {
    if (request === requestId) { total.value = 0; error.value = message(e) }
  } finally { if (request === requestId) loading.value = false }
}
async function loadOwners() {
  const request = ++ownerRequest
  ownerError.value = ''
  try { const result = await api<AdminOwner[]>('admin.lead.owners'); if (request === ownerRequest) owners.value = result.filter(owner => owner.active) }
  catch (e) { if (request === ownerRequest) ownerError.value = message(e) }
}
async function loadReminders() {
  const request = ++reminderRequest
  reminderError.value = ''
  try { const result = await api<Lead[]>('admin.lead.reminders'); if (request === reminderRequest) pendingReminders.value = result.map(normalizeLead) }
  catch (e) { if (request === reminderRequest) reminderError.value = message(e) }
}
async function refresh() { await Promise.allSettled([load(), loadOwners(), loadReminders()]) }
function search() { searchKeyword.value = keyword.value.trim(); page.value = 1; success.value = ''; void load() }
function reset() {
  keyword.value = ''; searchKeyword.value = ''
  if (status.value || channel.value || region.value || ownerFilter.value || reminderFilter.value) { status.value = ''; channel.value = ''; region.value = ''; ownerFilter.value = ''; reminderFilter.value = '' }
  else search()
}
watch([status, channel, region, ownerFilter, reminderFilter, pageSize], search)
watch(hasChanges, value => { if (value) saveSuccess.value = '' })
function showOverdue() {
  keyword.value = ''; searchKeyword.value = ''; status.value = ''; channel.value = ''; region.value = ''; ownerFilter.value = ''
  if (reminderFilter.value !== 'due') reminderFilter.value = 'due'
  else search()
}
function changePage(value: number) {
  if (loading.value || value < 1 || value > pages.value) return
  page.value = value; void load()
}
function setDraft(lead: Lead) { records.value = JSON.parse(JSON.stringify(savedRecords(lead))); recordsConflict.value = false; externalReminder.value = Boolean(lead.externalReminder); Object.assign(profile, lead.customerProfile); amountInput.value = ((lead.opportunityAmount ?? 0) / 100).toFixed(2); note.value = ''; nextStatus.value = lead.status; draft.ownerId = lead.ownerId; draft.reminderLocal = toLocalDateTime(lead.reminderAt); draft.reminderDone = lead.reminderDone }
async function loadHistory() {
  if (!selected.value) return
  const leadId = selected.value.id, request = ++historyRequest
  historyLoading.value = true; historyError.value = ''
  try {
    const entries = await api<LeadHistory[]>('admin.lead.history', { leadId })
    if (request === historyRequest && selected.value?.id === leadId) history.value = entries
  } catch (e) { if (request === historyRequest) historyError.value = message(e) }
  finally { if (request === historyRequest) historyLoading.value = false }
}
async function openLead(lead: Lead) {
  ++historyRequest
  selected.value = normalizeLead(lead); setDraft(selected.value); saveError.value = ''; saveSuccess.value = ''; conflictNeedsRefresh.value = false
  history.value = []; historyError.value = ''; historyLoading.value = true
  await nextTick(); if (!dialog.value?.open) dialog.value?.showModal()
  void loadHistory(); void loadDeliveries(); void loadDuplicates()
}
function closeLead() {
  if (saving.value || refreshingConflict.value) return
  dialog.value?.close(); selected.value = null; ++historyRequest; ++openRequest; historyLoading.value = false; historyError.value = ''
}
function cancelDialog(event: Event) { event.preventDefault(); closeLead() }
async function openById(leadId: string) {
  const request = ++openRequest
  actionError.value = ''
  try {
    const lead = await api<Lead>('admin.lead.get', { leadId })
    if (mounted && request === openRequest) await openLead(lead)
  } catch (e) { if (request === openRequest) actionError.value = message(e) }
}
watch(() => props.initialLeadId, value => { if (value) { pendingLeadId = value; if (mounted) void openById(value) } }, { immediate: true })
function openLeadEvent(event: Event) { const leadId = (event as CustomEvent<{ leadId?: string }>).detail?.leadId; if (leadId) void openById(leadId) }
async function refreshConflict() {
  if (!selected.value || refreshingConflict.value) return
  const previous = selected.value
  refreshingConflict.value = true
  try {
    const latest = normalizeLead(await api<Lead>('admin.lead.get', { leadId: previous.id }))
    if (nextStatus.value === previous.status) nextStatus.value = latest.status
    if (draft.ownerId === previous.ownerId) draft.ownerId = latest.ownerId
    if (draft.reminderLocal === toLocalDateTime(previous.reminderAt) && draft.reminderDone === previous.reminderDone) {
      draft.reminderLocal = toLocalDateTime(latest.reminderAt); draft.reminderDone = latest.reminderDone
    }
    if (!profileChanged.value) Object.assign(profile, latest.customerProfile)
    if (amountInput.value === ((previous.opportunityAmount ?? 0) / 100).toFixed(2)) amountInput.value = ((latest.opportunityAmount ?? 0) / 100).toFixed(2)
    if (recordsChanged.value && JSON.stringify(savedRecords(previous)) !== JSON.stringify(savedRecords(latest))) recordsConflict.value = true
    else if (!recordsChanged.value) records.value = JSON.parse(JSON.stringify(savedRecords(latest)))
    if (externalReminder.value === Boolean(previous.externalReminder)) externalReminder.value = Boolean(latest.externalReminder)
    selected.value = latest
    conflictNeedsRefresh.value = false
    await Promise.allSettled([load(), loadHistory(), loadReminders()])
    saveError.value = '这条线索已被其他管理员修改，已刷新最新记录。填写的内容已保留，请核对后再次保存。'
  } catch (e) { conflictNeedsRefresh.value = true; saveError.value = '刷新最新记录失败：' + message(e) + '。填写的内容已保留，请重试刷新。' }
  finally { refreshingConflict.value = false }
}
function clearReminder() { draft.reminderLocal = ''; draft.reminderDone = false }
function reminderEdited() { draft.reminderDone = false }
async function save() {
  if (!selected.value || saving.value || refreshingConflict.value || conflictNeedsRefresh.value || recordsConflict.value || !hasChanges.value) return
  if ([...note.value.trim()].length > 2000) { saveError.value = '跟进备注最多 2000 字'; return }
  const original = selected.value
  const data: Record<string, unknown> = { leadId: original.id, expectedVersion: original.version }
  if (nextStatus.value !== original.status) data.status = nextStatus.value
  if (draft.ownerId !== original.ownerId) data.ownerId = draft.ownerId
  if (note.value.trim()) data.followUpNote = note.value.trim()
  saveError.value = ''; saveSuccess.value = ''
  try {
    if (recordsChanged.value) { const valid = validateLeadRecords(records.value as unknown as Record<string, unknown>, original.receipts ?? []); for (const key of ['contacts', 'contracts', 'receipts'] as const) if (JSON.stringify(valid[key]) !== JSON.stringify(original[key] ?? [])) data[key] = valid[key] }
    if (externalReminder.value !== Boolean(original.externalReminder)) data.externalReminder = externalReminder.value
    const amount = amountCents()
    if (amount !== (original.opportunityAmount ?? 0)) data.opportunityAmount = amount
    if (profileChanged.value) data.customerProfile = { ...profile }
    if (!finished.value && (draft.reminderLocal !== toLocalDateTime(original.reminderAt) || draft.reminderDone !== original.reminderDone)) {
      data.reminderAt = fromLocalDateTime(draft.reminderLocal); data.reminderDone = Boolean(draft.reminderLocal && draft.reminderDone)
    } else if (finished.value && !draft.reminderLocal && original.reminderAt) {
      data.reminderAt = ''; data.reminderDone = false
    }
  } catch (e) { saveError.value = message(e); return }
  saving.value = true
  try {
    selected.value = normalizeLead(await api<Lead>('admin.lead.update', data))
    setDraft(selected.value); void loadDeliveries(); changed(); saveSuccess.value = '已保存，本次操作已记入跟进历史。'
    await Promise.allSettled([load(), loadHistory(), loadReminders()])
  } catch (e) { if (isConflict(e)) await refreshConflict(); else saveError.value = message(e) }
  finally { saving.value = false }
}
async function completeReminder(lead: Lead) {
  if (quickBusy.value) return
  quickBusy.value = lead.id; actionError.value = ''
  try {
    await api<Lead>('admin.lead.update', { leadId: lead.id, expectedVersion: lead.version, reminderDone: true })
    changed(); success.value = '提醒已标记完成，操作已记入跟进历史。'
    await Promise.allSettled([load(), loadReminders()])
  } catch (e) { actionError.value = message(e); if (isConflict(e)) await load() }
  finally { quickBusy.value = '' }
}
function followUpTime(entry?: LeadFollowUp) { return entry?.createdAt ? formatted(entry.createdAt) : '历史备注 · 原记录时间未知' }
function latestNoteEntry(lead: Lead) { return [...(lead.followUps || [])].reverse().find(entry => entry.note) }
function csvCell(value: unknown) {
  let text = String(value ?? '')
  if (/^[\s\u0000-\u001f]*[=+@-]/.test(text) || /^[\t\r\n]/.test(text)) text = "'" + text
  return '"' + text.replaceAll('"', '""') + '"'
}
async function exportCsv() {
  if (exporting.value || !total.value) return
  const query = filters(), records = new Map<string, Lead>()
  exporting.value = true; exported.value = 0; error.value = ''
  try {
    const expected = total.value
    let offset = 0
    while (offset < expected) {
      const result = await api<LeadListResult>('admin.lead.list', { ...query, limit: 100, offset })
      if (!mounted) return
      if (result.total !== expected || result.items.some(lead => records.has(lead.id))) throw new Error('线索在导出期间发生变化，请刷新后重新导出。')
      result.items.forEach(lead => records.set(lead.id, normalizeLead(lead))); exported.value = records.size; offset += result.items.length
      if (!result.items.length) break
    }
    if (records.size !== expected) throw new Error('导出数据不完整，请刷新后重新导出。')
    const columns = ['客户邮箱', '客户地址', '客户行业', '客户档案备注', '商机金额（元）', '企业/个人', '联系人', '手机', '地区', '渠道', '采购量', '需求', '状态', '负责人', '最近备注', '全部跟进记录', '下次提醒时间', '提醒状态', '提交时间']
    const values = [...records.values()].map(lead => [lead.customerProfile?.email, lead.customerProfile?.address, lead.customerProfile?.industry, lead.customerProfile?.notes, ((lead.opportunityAmount ?? 0) / 100).toFixed(2), lead.organization, lead.contact, lead.phone, lead.region, lead.channel, lead.volume, lead.demand, labels[lead.status], lead.ownerName, lead.followUpNote, (lead.followUps || []).map(entry => followUpTime(entry) + ' | ' + entry.username + ' | ' + (labels[entry.previousStatus] || '原状态') + ' → ' + labels[entry.status] + (entry.note ? ' | ' + entry.note : '')).join('\n'), lead.reminderAt ? formatted(lead.reminderAt) : '', reminderLabel(lead), formatted(lead.createdAt)])
    const csv = '\uFEFF' + [columns, ...values].map(row => row.map(csvCell).join(',')).join('\r\n')
    const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' }))
    const a = document.createElement('a')
    a.href = url; a.download = '合作线索-' + new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(new Date()) + '.csv'
    document.body.appendChild(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(url), 1000)
  } catch (e) { error.value = '导出失败：' + message(e) }
  finally { exporting.value = false }
}
onMounted(() => {
  mounted = true; void refresh()
  if (pendingLeadId) void openById(pendingLeadId)
  clockTimer = setInterval(() => { now.value = new Date() }, 30000)
  window.addEventListener('chiji-open-lead', openLeadEvent)
})
onUnmounted(() => { mounted = false; ++requestId; ++historyRequest; ++openRequest; ++ownerRequest; ++reminderRequest; ++funnelRequest; clearInterval(clockTimer); window.removeEventListener('chiji-open-lead', openLeadEvent) })
</script>

<template>
  <div class="leads-view">
    <div class="toolbar">
      <div><h2>合作线索</h2><p>分配负责人，记录每次沟通，让下一次跟进有着落。</p></div>
      <div class="row-actions"><button class="button secondary" :disabled="loading" @click="refresh">刷新</button><button class="button secondary" :disabled="loading || exporting || !total" @click="exportCsv">{{ exporting ? `正在导出 ${exported} 条…` : '导出筛选结果 CSV' }}</button></div>
    </div>
    <div class="lead-summary" aria-label="跟进提醒概览">
      <div class="summary-item"><span>匹配合作线索</span><strong>{{ total }}<small>条</small></strong><p>按下方筛选条件显示</p></div>
      <div class="summary-item"><span>全部待处理提醒</span><strong>{{ summary.pending }}<small>条</small></strong><p>覆盖所有正在跟进的线索</p></div>
      <div class="summary-item"><span>今日稍后提醒</span><strong>{{ summary.today }}<small>条</small></strong><p>安排今天的后续沟通</p></div>
      <button class="summary-item overdue-summary" @click="showOverdue"><span>已到期 · 待处理</span><strong>{{ summary.overdue }}<small>条</small></strong><p>完成跟进后标记提醒 <b>↗</b></p></button>
    </div>
    <section class="panel crm-funnel" aria-label="成交漏斗" :aria-busy="funnelLoading">
      <div class="funnel-heading"><div><h3>成交漏斗</h3><p>按客户、渠道、地区、负责人及提醒筛选，覆盖全部页；状态筛选仅影响下方列表。</p></div></div>
      <p v-if="funnelLoading" role="status">正在统计成交漏斗…</p>
      <p v-else-if="funnelError" class="error" role="alert">{{ funnelError }} <button class="text-button" @click="loadFunnel">重试统计</button></p>
      <template v-else-if="funnel">
        <div class="funnel-totals"><span>在跟进商机 <strong>{{ money(funnel.activeAmount) }}</strong></span><span>已成交金额 <strong>{{ money(funnel.wonAmount) }}</strong></span><span>成交占比 <strong>{{ (funnel.winRate * 100).toFixed(1) }}%</strong></span></div>
        <div class="funnel-stages"><button v-for="stage in funnel.stages" :key="stage.status" class="funnel-stage" :class="{ active: status === stage.status }" @click="status = status === stage.status ? '' : stage.status"><span>{{ labels[stage.status] }}</span><strong>{{ stage.count }} <small>条</small></strong><div class="funnel-track"><i :style="{ width: `${funnel.total ? stage.count / funnel.total * 100 : 0}%` }" /></div><span>{{ money(stage.amount) }}</span></button></div>
        <p>各阶段显示当前状态分布；成交占比 = 已成交 / 全部线索（含已关闭）。金额为商机估值，不代表收款。{{ funnel.total ? '' : '暂无匹配线索。' }}</p>
      </template>
    </section>
    <section class="panel" aria-label="线索来源渠道统计">
      <div class="panel-head"><div><h3>来源渠道统计</h3><p>按线索来源渠道分组：线索数 / 成单数 / 成单率 / 商机金额，默认最近 30 天。</p></div>
      <div class="row-actions"><input v-model="channelStart" type="date" :max="channelEnd" aria-label="开始日期" /><span class="subtle">至</span><input v-model="channelEnd" type="date" :min="channelStart" aria-label="结束日期" /><button class="button secondary" :disabled="channelLoading" @click="loadChannelStats">查询</button></div></div>
      <p v-if="channelLoading" role="status">正在统计…</p>
      <p v-else-if="channelError" class="error" role="alert">{{ channelError }} <button class="text-button" @click="loadChannelStats">重试</button></p>
      <table v-else class="lead-table"><thead><tr><th>渠道</th><th>线索数</th><th>成单数</th><th>成单率</th><th>商机金额</th></tr></thead><tbody>
      <tr v-for="s in channelStats" :key="s.channel"><td>{{ s.channel }}</td><td>{{ s.count }}</td><td>{{ s.won }}</td><td>{{ s.count ? (s.won / s.count * 100).toFixed(1) + "%" : "—" }}</td><td>{{ money(s.opportunityAmount) }}</td></tr>
      <tr v-if="channelTotal"><td><strong>合计</strong></td><td><strong>{{ channelTotal.count }}</strong></td><td><strong>{{ channelTotal.won }}</strong></td><td><strong>{{ channelTotal.count ? (channelTotal.won / channelTotal.count * 100).toFixed(1) + "%" : "—" }}</strong></td><td><strong>{{ money(channelTotal.opportunityAmount) }}</strong></td></tr>
      <tr v-if="!channelStats.length"><td colspan="5" class="subtle">该时段无线索</td></tr>
      </tbody></table>
    </section>
    <section class="panel">
      <div class="panel-head"><div><h3>公海池</h3><p>超过 {{ poolConfig.inactiveDays }} 天未跟进且有负责人的线索自动流入公海，任何人可认领回私海。</p></div><div class="row-actions"><button class="button secondary" :disabled="poolBusy" @click="poolOpen = !poolOpen; if (poolOpen && !poolLoaded) loadPool()">{{ poolOpen ? "收起" : "展开" }}</button><button class="button secondary" :disabled="poolBusy" @click="loadPool()">刷新</button></div></div>
      <div v-if="poolOpen">
        <p v-if="poolError" class="alert error" role="alert">{{ poolError }}</p>
        <div v-permission="'super'" class="lead-filter-actions"><label>未跟进 <input type="number" v-model.number="poolConfig.inactiveDays" min="1" max="90" :disabled="poolBusy" style="width:70px" /> 天流入公海</label><button class="button secondary" :disabled="poolBusy" @click="savePoolConfig">保存规则</button></div>
        <p v-if="poolBusy" role="status">正在加载公海…</p>
        <p v-else-if="poolLoaded && !poolItems.length" class="subtle">公海暂无线索</p>
        <table v-else-if="poolLoaded" class="lead-table"><thead><tr><th>客户</th><th>联系人</th><th>原负责人</th><th>闲置天数</th><th>操作</th></tr></thead><tbody>
        <tr v-for="item in poolItems" :key="item.lead.id"><td>{{ item.lead.organization }}</td><td>{{ item.lead.contact }}</td><td>{{ item.lead.ownerName || "—" }}</td><td>{{ item.daysIdle }} 天</td><td><button v-permission="'lead.write'" class="button secondary" :disabled="poolBusy" @click="claimLead(item.lead.id)">认领</button></td></tr>
        </tbody></table>
      </div>
    </section>
    <form class="panel lead-filters" @submit.prevent="search">
      <label class="lead-search">搜索线索<input v-model="keyword" type="search" placeholder="客户、联系人、手机、需求或历史备注" /></label>
      <label>状态<select v-model="status"><option value="">全部状态</option><option v-for="(label, key) in labels" :key="key" :value="key">{{ label }}</option></select></label>
      <label>渠道<select v-model="channel"><option value="">全部渠道</option><option v-for="value in channels" :key="value" :value="value">{{ value }}</option></select></label>
      <label>地区<select v-model="region"><option value="">全部地区</option><option v-for="value in regions" :key="value" :value="value">{{ value }}</option></select></label>
      <label>负责人<select v-model="ownerFilter"><option value="">全部负责人</option><option v-if="currentUserId" :value="currentUserId">我负责的</option><option value="__unassigned__">尚未分配</option><option v-for="owner in owners" :key="owner.id" :value="owner.id">{{ owner.username }}</option></select></label>
      <label>提醒<select v-model="reminderFilter"><option value="">全部提醒</option><option value="due">已到期待处理</option><option value="upcoming">后续待提醒</option><option value="done">已完成提醒</option><option value="none">未设置提醒</option></select></label>
      <div class="lead-filter-actions"><button v-permission="'lead.write'" class="button primary" type="submit">搜索</button><button class="button secondary" type="button" @click="reset">重置</button></div>
      <p class="lead-filter-hint">空格分隔多个关键词可组合搜索；筛选后导出全部匹配线索。</p>
    </form>
    <p v-if="success" class="alert lead-success" role="status">{{ success }}</p>
    <div v-if="error" class="alert error" role="alert">{{ error }} <button class="text-button" @click="load">重试加载</button></div>
    <div v-if="ownerError" class="alert error error-line" role="alert"><span>负责人加载失败：{{ ownerError }}</span><button class="text-button" @click="loadOwners">重试</button></div>
    <div v-if="reminderError" class="alert error error-line" role="alert"><span>提醒加载失败：{{ reminderError }}</span><button class="text-button" @click="loadReminders">重试</button></div>
    <div v-if="actionError" class="alert error error-line" role="alert"><span>{{ actionError }}</span><button class="text-button" @click="refresh">刷新线索</button></div>
    <section class="panel table-panel" :aria-busy="loading">
      <div class="lead-table-scroll">
        <table class="lead-table">
          <thead><tr><th>客户 / 联系方式</th><th>渠道 / 采购需求</th><th>状态</th><th>负责人</th><th>下次提醒</th><th>最近备注</th><th>跟进</th></tr></thead>
          <tbody>
            <tr v-if="loading"><td colspan="7" class="lead-empty" role="status">正在加载线索…</td></tr>
            <tr v-else-if="error && !leads.length"><td colspan="7" class="lead-empty">暂时无法加载线索，请重试。</td></tr>
            <tr v-else-if="!leads.length"><td colspan="7" class="lead-empty">{{ searchKeyword || status || channel || region || ownerFilter || reminderFilter ? '没有匹配的线索，试试其他关键词或重置筛选。' : '暂无合作线索。' }}</td></tr>
            <tr v-for="lead in leads" :key="lead.id">
              <td><strong>{{ lead.organization }}</strong><small class="block">{{ lead.contact }} · <a :href="`tel:${lead.phone}`">{{ lead.phone }}</a></small><small class="block">{{ formatted(lead.createdAt) }}</small></td>
              <td><strong>{{ lead.channel }} · {{ lead.region }}</strong><small class="block">{{ lead.volume }}</small><small class="block demand" :title="lead.demand">{{ lead.demand }}</small></td>
              <td><span class="status" :class="lead.status.toLowerCase()">{{ labels[lead.status] }}</span><small class="block">商机 {{ money(lead.opportunityAmount) }}</small></td>
              <td><span v-if="lead.ownerId" class="owner-cell"><i>{{ (lead.ownerName || '管').slice(0, 1) }}</i>{{ lead.ownerName || '原负责人' }}</span><button v-else class="unassigned-button" @click="openLead(lead)">＋ 分配</button></td>
              <td><span class="reminder-status" :class="state(lead)">{{ reminderLabel(lead) }}</span><small v-if="lead.reminderAt" class="block">{{ formatted(lead.reminderAt) }}</small><button v-if="['overdue', 'today', 'scheduled'].includes(state(lead))" class="text-button complete-button" :disabled="Boolean(quickBusy)" @click="completeReminder(lead)">{{ quickBusy === lead.id ? '保存中…' : '✓ 标记完成' }}</button></td>
              <td><span class="lead-note">{{ lead.followUpNote || '—' }}</span><small v-if="lead.followUpNote" class="block">{{ followUpTime(latestNoteEntry(lead)) }}</small></td>
              <td><div class="lead-row-actions"><button class="button secondary small" @click="openLead(lead)">客户档案 / 跟进</button><small>{{ (lead.followUps || []).length }} 条记录</small></div></td>
            </tr>
          </tbody>
        </table>
      </div>
      <nav class="lead-pagination" aria-label="线索分页">
        <span role="status">{{ loading ? '正在加载…' : `共 ${total} 条 · 显示 ${first}–${last} 条` }}</span>
        <div><label>每页<select v-model.number="pageSize"><option :value="10">10 条</option><option :value="20">20 条</option><option :value="50">50 条</option></select></label><button class="button secondary small" :disabled="loading || page === 1" @click="changePage(page - 1)">上一页</button><span>第 {{ page }} / {{ pages }} 页</span><button class="button secondary small" :disabled="loading || page >= pages" @click="changePage(page + 1)">下一页</button></div>
      </nav>
    </section>

    <dialog ref="dialog" class="lead-dialog" aria-labelledby="lead-dialog-title" @cancel="cancelDialog" @click="($event.target === $event.currentTarget) && closeLead()">
      <form v-if="selected" @submit.prevent="save">
        <div class="drawer-head"><div><p class="eyebrow purple">COOPERATION FOLLOW-UP</p><h2 id="lead-dialog-title">{{ selected.organization }}</h2><p class="block">{{ selected.contact }} · <a :href="`tel:${selected.phone}`">{{ selected.phone }}</a></p></div><button class="close" type="button" aria-label="关闭跟进记录" :disabled="saving || refreshingConflict" @click="closeLead">×</button></div>
        <div class="drawer-body">
          <div class="lead-detail"><div><span>渠道 / 地区</span><strong>{{ selected.channel }} · {{ selected.region }}</strong></div><div><span>采购量</span><strong>{{ selected.volume }}</strong></div><div class="detail-demand"><span>合作需求</span><p>{{ selected.demand || '未填写' }}</p></div></div>
          <div class="followup-heading"><h3>本次跟进</h3><small>沟通、分配和提醒调整会留下记录</small></div>
          <fieldset class="lead-edit-fieldset" :disabled="saving || refreshingConflict">
          <h3 class="profile-heading">客户档案</h3>
          <div class="form-grid">
            <label>客户邮箱<input v-model="profile.email" type="email" maxlength="200" placeholder="用于业务联系" /></label>
            <label>所属行业<input v-model="profile.industry" maxlength="200" placeholder="如：生鲜零售、餐饮" /></label>
            <label class="full">客户地址<input v-model="profile.address" maxlength="200" placeholder="填写企业或收货地址" /></label>
            <label class="full">档案备注<textarea v-model="profile.notes" rows="2" maxlength="2000" placeholder="客户背景、采购偏好、合作要求" /></label>
            <label class="full">商机金额（人民币元）<input v-model="amountInput" aria-label="商机金额（人民币元）" type="text" inputmode="decimal" placeholder="0.00" /><small>预计合作金额，最多两位小数；未估值填 0。</small></label>
            <label>线索状态<select v-model="nextStatus"><option v-for="(label, key) in labels" :key="key" :value="key">{{ label }}</option></select></label>
            <label>负责人<select v-model="draft.ownerId" :disabled="Boolean(ownerError)"><option value="">尚未分配</option><option v-if="inactiveOwner" :value="draft.ownerId" disabled>{{ selected.ownerName || '原负责人' }}（已停用）</option><option v-for="owner in owners" :key="owner.id" :value="owner.id">{{ owner.username }}</option></select><small v-if="ownerError" class="owner-inline-error">负责人加载失败，请先<button type="button" class="text-button" @click="loadOwners">重试</button></small></label>
            <label class="full">新增跟进备注<textarea v-model="note" rows="3" maxlength="2000" placeholder="记录本次沟通结果、客户意向或下一步安排…" /><small class="note-help"><span>只记录本次新增内容，历史备注会保留</span><span>{{ note.length }} / 2000</span></small></label>
            <div class="full reminder-editor">
              <div class="reminder-editor-head"><strong>下次跟进提醒</strong><span v-if="draft.reminderLocal && draft.reminderDone" class="reminder-status done">已标记完成</span></div>
              <p v-if="finished" class="finished-reminder-note">成交或关闭后，待处理提醒会自动结束。</p>
              <template v-else>
                <label class="reminder-input-label">提醒时间<input v-model="draft.reminderLocal" type="datetime-local" step="60" aria-label="下次跟进提醒时间" @input="reminderEdited" /><small>按本地时区 {{ localTimezone }} 设置</small></label>
                <div v-if="draft.reminderLocal" class="reminder-editor-actions"><button v-if="!draft.reminderDone" type="button" class="text-button" @click="draft.reminderDone = true">✓ 标记本次提醒完成</button><button v-else type="button" class="text-button" @click="draft.reminderDone = false">重新开启提醒</button><button type="button" class="text-button remove-reminder" @click="clearReminder">移除提醒</button></div>
                <small class="reminder-explanation">保存后生效，到期提醒可在后台顶部查看。日历提醒需要下载并导入手机或电脑日历，后续修改需重新导入。</small>
              </template>
            </div>
          </div>
          <LeadRecordsEditor v-model="records" :saved-receipt-ids="(selected.receipts ?? []).map(r => r.id)" />
          <div class="reminder-editor">
            <label><input v-model="externalReminder" type="checkbox" /> 开启外部到期提醒</label>
            <small class="reminder-explanation">需分配负责人，由已配置的提醒网关投递；演示模式不发送。网关路由由服务端配置。</small>
            <button type="button" class="text-button" @click="downloadCalendar">下载已保存提醒到日历</button>
            <button type="button" class="text-button" @click="loadDeliveries">刷新投递记录</button>
            <p v-if="deliveryError" role="alert">{{ deliveryError }}</p>
            <p v-for="d in deliveries" :key="d.id">{{ deliveryLabels[d.status] || d.status }} · {{ formatted(d.updatedAt) }}</p>
            <small v-if="!deliveryError && !deliveries.length">暂无投递记录</small>
          </div>
          </fieldset>
          <p v-if="recordsConflict" class="alert error" role="alert">其他管理员也修改了联系人、合同或回款，当前草稿已保留。请先核对最新记录；载入后可重新填写本次修改。<button type="button" class="text-button" @click="useLatestRecords">载入最新联系人、合同及回款</button></p>
          <p v-if="saveError" class="alert error form-feedback" role="alert">{{ saveError }}<button v-if="conflictNeedsRefresh" type="button" class="text-button" :disabled="saving || refreshingConflict" @click="refreshConflict">重试刷新</button></p>
          <p v-if="saveSuccess" class="success-text form-feedback" role="status">✓ {{ saveSuccess }}</p>
          <section class="lead-history" aria-label="历史跟进记录">
            <div class="history-heading"><div><h3>跟进历史</h3><p>每一次沟通与调整，都有迹可循。</p></div><span v-if="!historyLoading && !historyError" class="history-count">{{ history.length }} 条记录</span></div>
            <p v-if="historyLoading" class="history-placeholder" role="status">正在加载历史记录…</p>
            <div v-else-if="historyError" class="alert error error-line" role="alert"><span>{{ historyError }}</span><button type="button" class="text-button" @click="loadHistory">重试</button></div>
            <ol v-else-if="history.length">
              <li v-for="entry in history" :key="entry.id"><div class="lead-history-meta"><strong>{{ entry.actorName || '历史记录' }}</strong><time v-if="entry.createdAt" :datetime="entry.createdAt">{{ formatted(entry.createdAt) }}</time><span v-else>原记录时间未知</span></div><div class="history-change-tags"><span v-for="change in entry.changes" :key="change">{{ changeLabels[change] || change }}</span></div><p v-if="entry.note && entry.changes.includes('note')" class="lead-history-note">{{ entry.note }}</p><div class="history-snapshot"><span>状态：{{ labels[entry.status] || entry.status }}</span><span>负责人：{{ entry.ownerName || '未分配' }}</span><span v-if="entry.changes.includes('amount')">商机：{{ money(entry.opportunityAmount) }}</span><span v-if="entry.changes.includes('profile')">档案：{{ entry.customerProfile?.email || '未填写邮箱' }} · {{ entry.customerProfile?.industry || '未填写行业' }} · {{ entry.customerProfile?.address || '未填写地址' }}<template v-if="entry.customerProfile?.notes"> · {{ entry.customerProfile.notes }}</template></span><span v-if="entry.changes.includes('contacts')">联系人：{{ entry.contacts?.map(c => c.name).join('、') || '无' }}</span><span v-if="entry.changes.includes('contracts')">合同：{{ entry.contracts?.map(c => c.number + ' ' + money(c.amount)).join('、') || '无' }}</span><span v-if="entry.changes.includes('receipts')">回款：{{ entry.receipts?.map(r => r.reference + ' ' + money(r.amount)).join('、') || '无' }}</span><span v-if="entry.changes.includes('externalReminder')">外部提醒：{{ entry.externalReminder ? '开启' : '关闭' }}</span><span v-if="entry.changes.includes('reminder')">提醒：{{ entry.reminderAt ? `${formatted(entry.reminderAt)}${entry.reminderDone ? ' · 已完成' : ''}` : '已移除 / 未设置' }}</span></div></li>
            </ol>
            <div v-else class="history-placeholder"><strong>还没有跟进记录</strong><p>保存第一次沟通或分配负责人后，记录会显示在这里。</p></div>
          </section>
        </div>
        <div class="drawer-foot"><span class="save-hint">{{ hasChanges ? '有待保存的修改' : '所有跟进记录都会保留' }}</span><button class="button secondary" type="button" :disabled="saving || refreshingConflict" @click="closeLead">关闭</button><button v-permission="'lead.write'" class="button primary" type="submit" :disabled="saving || refreshingConflict || conflictNeedsRefresh || recordsConflict || !hasChanges">{{ saving ? '正在保存…' : '保存跟进' }}</button></div>
      </form>
      <div v-if="selected" class="lead-detail" style="margin-top:12px">
        <h3>重复线索</h3>
        <p v-if="!dupItems.length" class="subtle">同手机号暂无其他线索</p>
        <div v-else v-for="d in dupItems" :key="d.id"><label><input type="checkbox" :value="d.id" v-model="mergeSourceIds" :disabled="mergeBusy" /> {{ d.organization }} · {{ d.contact }} · {{ labels[d.status as Lead["status"]] || d.status }} · {{ d.ownerName || "未分配" }} · {{ d.followUpCount }} 条跟进</label></div>
        <div v-if="dupItems.length" class="row-actions"><button v-permission="'lead.write'" type="button" class="button secondary" :disabled="mergeBusy || !mergeSourceIds.length" @click="mergeLeads">{{ mergeBusy ? "合并中…" : `合并选中 ${mergeSourceIds.length} 条到当前线索` }}</button></div>
      </div>
    </dialog>
  </div>
</template>

<style scoped>
.crm-funnel { padding: 22px; margin-bottom: 24px; }
.funnel-heading h3, .profile-heading { color: var(--purple-dark); font-family: var(--serif); font-size: 18px; }
.profile-heading { margin-bottom: 16px; }
.crm-funnel p { color: var(--muted); font-size: 11px; line-height: 1.8; margin-top: 10px; }
.funnel-totals { display: flex; flex-wrap: wrap; gap: 18px 32px; margin: 20px 0; font-size: 12px; color: var(--muted); }
.funnel-totals strong { margin-left: 8px; color: var(--purple-dark); }
.funnel-stages { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 12px; }
.funnel-stage { padding: 16px; text-align: left; border: 1px solid var(--border); border-radius: 10px; background: #faf7f1; color: var(--purple-dark); cursor: pointer; }
.funnel-stage.active { border-color: var(--purple); background: var(--purple-soft); }
.funnel-stage > span { display: block; font-size: 11px; }
.funnel-stage > strong { display: block; font-size: 24px; margin: 12px 0; }
.funnel-stage small { font-size: 11px; font-weight: normal; }
.funnel-track { height: 5px; background: #e9e0e9; margin-bottom: 12px; border-radius: 4px; overflow: hidden; }
.funnel-track i { display: block; height: 100%; background: var(--purple); }
@media (max-width: 850px) { .funnel-stages { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

.lead-filters { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 14px; margin-bottom: 20px; padding: 20px; }
.lead-filters label { display: flex; flex: 1; flex-direction: column; gap: 7px; min-width: 115px; color: var(--muted); font-size: 11px; font-weight: 700; }
.lead-filters .lead-search { flex: 2.4; min-width: 250px; }
.lead-filters input, .lead-filters select, .lead-pagination select { min-width: 0; padding: 10px 11px; border: 1px solid var(--border); border-radius: 9px; color: #28231f; background: #faf8f4; font-size: 12px; }
.lead-filters input:focus, .lead-filters select:focus { outline: 2px solid var(--purple-soft); border-color: var(--purple); }
.lead-filter-actions { display: flex; gap: 8px; }
.lead-filter-hint { flex-basis: 100%; color: var(--muted); font-size: 11px; line-height: 1.5; }
.lead-success { color: var(--green); background: var(--green-soft); }
.lead-table-scroll { overflow-x: auto; }
.lead-table { table-layout: fixed; min-width: 800px; }
.lead-table th, .lead-table td { padding: 14px 12px; overflow-wrap: anywhere; }
.lead-table th:first-child { width: 17%; }
.lead-table th:nth-child(2) { width: 12%; }
.lead-table th:nth-child(3) { width: 17%; }
.lead-table th:nth-child(4) { width: 13%; }
.lead-table th:nth-child(5) { width: 9%; }
.lead-table th:nth-child(6) { width: 18%; }
.lead-table th:last-child { width: 14%; }
.lead-note, .lead-history-note { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.7; }
.lead-note { display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
.lead-row-actions { display: flex; flex-direction: column; align-items: flex-start; gap: 9px; }
.lead-row-actions .text-button { padding: 0; font-size: 11px; }
.status.new { color: #805923; background: #f5e6c5; }
.status.won { color: var(--green); background: var(--green-soft); }
.status.lost { color: var(--muted); background: #ede9e2; }
.lead-empty { height: 150px; color: var(--muted); text-align: center; }
.lead-pagination { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 14px; padding: 16px 20px; border-top: 1px solid var(--border); color: var(--muted); font-size: 11px; }
.lead-pagination > div, .lead-pagination label { display: flex; align-items: center; gap: 10px; }
.lead-pagination select { padding: 6px; }
.lead-pagination button:disabled { cursor: default; }
.lead-dialog { width: min(720px, 70vw); height: 100dvh; max-height: 100dvh; margin: 0 0 0 auto; padding: 0; border: 0; color: #28231f; background: var(--paper); box-shadow: -20px 0 50px rgba(0, 0, 0, .13); }
.lead-dialog::backdrop { background: rgba(31, 18, 31, .42); backdrop-filter: blur(3px); }
.lead-dialog form { display: flex; flex-direction: column; height: 100%; }
.lead-dialog .drawer-head, .lead-dialog .drawer-foot { flex-shrink: 0; }
.lead-dialog .drawer-head h2 { max-width: 580px; overflow-wrap: anywhere; }
.lead-history { margin-top: 30px; padding-top: 23px; border-top: 1px solid var(--border); }
.lead-history h3 { display: flex; align-items: center; gap: 8px; font-size: 14px; }
.lead-history ol { margin: 20px 0 0; padding-left: 16px; list-style: none; border-left: 2px solid var(--purple-soft); }
.lead-history li { position: relative; margin-bottom: 24px; }
.lead-history li::before { position: absolute; top: 4px; left: -22px; width: 9px; height: 9px; border: 2px solid var(--paper); border-radius: 50%; background: var(--purple); content: ''; }
.lead-history-meta { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 8px; font-size: 12px; }
.lead-history-meta time, .lead-history-meta > span { color: var(--muted); font-size: 11px; }
.lead-history-status { margin-top: 7px; color: var(--purple); font-size: 11px; }
.lead-history-note { margin-top: 8px; font-size: 13px; }
.lead-history-empty { margin-top: 18px; color: var(--muted); font-size: 12px; }
.lead-summary { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; margin-bottom: 24px; }
.summary-item { padding: 20px 22px; border: 1px solid var(--border); border-radius: 15px; color: inherit; background: var(--paper); text-align: left; box-shadow: 0 5px 18px rgba(48,27,41,.025); }
button.summary-item:hover { border-color: #b9a3bc; background: #fdfafd; }
.summary-item > span { color: var(--muted); font-size: 11px; }
.summary-item strong { display: block; margin-top: 9px; color: var(--purple-dark); font-family: var(--serif); font-size: 30px; font-weight: 600; }
.summary-item strong small { margin-left: 8px; color: var(--muted); font-size: 11px; font-weight: 400; }
.summary-item p { display: flex; justify-content: space-between; margin-top: 8px; color: var(--muted); font-size: 10px; }
.summary-item b { color: var(--purple); font-size: 13px; }
.overdue-summary { border-top: 3px solid #c5905a; }
.overdue-summary strong { color: #995e2c; }
.lead-filters { display: grid; grid-template-columns: 2fr repeat(3, 1fr); align-items: end; }
.lead-filters label, .lead-filters .lead-search { min-width: 0; }
.lead-filter-actions { align-self: end; }
.lead-filter-hint { grid-column: 1 / -1; }
.error-line { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.lead-table { min-width: 850px; }
.lead-table th:first-child { width: 20%; }
.lead-table th:nth-child(2) { width: 16%; }
.lead-table th:nth-child(3) { width: 9%; }
.lead-table th:nth-child(4) { width: 11%; }
.lead-table th:nth-child(5) { width: 17%; }
.lead-table th:nth-child(6) { width: 15%; }
.lead-table th:last-child { width: 12%; }
.lead-table .demand { display: -webkit-box; overflow: hidden; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.owner-cell { display: flex; align-items: center; gap: 7px; font-size: 11px; }
.owner-cell i { display: grid; flex-shrink: 0; place-items: center; width: 24px; height: 24px; border-radius: 50%; color: var(--purple); background: #f0e6f1; font-size: 10px; font-style: normal; }
.unassigned-button { padding: 5px 7px; border: 1px dashed #d4c6d9; border-radius: 6px; color: var(--purple); background: transparent; font-size: 10px; white-space: nowrap; }
.reminder-status { display: inline-flex; padding: 5px 8px; border-radius: 7px; font-size: 10px; font-weight: 650; white-space: nowrap; }
.reminder-status.none { padding-left: 0; color: #a1988e; }
.reminder-status.overdue { color: #a8464c; background: #f7e8e5; }
.reminder-status.today { color: #936328; background: #f6ebd7; }
.reminder-status.scheduled { color: var(--purple); background: var(--purple-soft); }
.reminder-status.done { color: var(--green); background: var(--green-soft); }
.complete-button { margin-top: 8px; padding: 0; font-size: 10px; }
.text-button:disabled { cursor: wait; opacity: .5; }
.lead-row-actions .button { white-space: nowrap; }
.lead-row-actions > small { color: var(--muted); font-size: 10px; }
.lead-dialog { width: min(760px, 76vw); }
.lead-detail { display: grid; grid-template-columns: 1fr 1fr; gap: 15px 24px; padding: 17px 20px; border-radius: 12px; background: #f6f2eb; }
.lead-detail span { display: block; margin-bottom: 6px; color: var(--muted); font-size: 10px; }
.lead-detail strong { font-size: 12px; font-weight: 600; }
.detail-demand { grid-column: 1 / -1; }
.detail-demand p { font-size: 12px; line-height: 1.7; white-space: pre-wrap; overflow-wrap: anywhere; }
.followup-heading { display: flex; align-items: center; justify-content: space-between; gap: 14px; margin: 28px 0 18px; }
.followup-heading h3, .history-heading h3 { color: var(--purple-dark); font-family: var(--serif); font-size: 18px; }
.followup-heading small { color: var(--muted); font-size: 10px; }
.lead-edit-fieldset { margin: 0; padding: 0; border: 0; min-width: 0; }
.note-help { display: flex !important; justify-content: space-between; gap: 15px; }
.owner-inline-error, .remove-reminder { color: var(--danger) !important; }
.reminder-editor { padding: 16px 18px; border: 1px solid var(--border); border-radius: 11px; background: #fcfaf6; }
.reminder-editor-head { display: flex; align-items: center; justify-content: space-between; }
.reminder-editor-head > strong { font-size: 12px; }
.reminder-input-label { display: block; margin-top: 15px; }
.reminder-input-label > input { max-width: 100%; background: var(--paper); }
.reminder-editor-actions { display: flex; justify-content: space-between; margin-top: 12px; }
.reminder-editor-actions > button { padding: 0; font-size: 11px; }
.reminder-explanation { display: block; margin-top: 12px; color: var(--muted); font-size: 10px; line-height: 1.6; }
.finished-reminder-note { margin-top: 10px; color: var(--green); font-size: 12px; }
.form-feedback { margin-top: 16px; line-height: 1.7; }
.history-heading { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.history-heading p { margin-top: 6px; color: var(--muted); font-size: 11px; }
.history-count { padding: 5px 9px; border-radius: 6px; color: var(--muted); background: #f3eee6; font-size: 10px; }
.history-change-tags { display: flex; flex-wrap: wrap; gap: 5px; margin-top: 9px; }
.history-change-tags span { padding: 3px 6px; border-radius: 4px; color: #796781; background: #f1eaf2; font-size: 9px; }
.history-snapshot { display: flex; flex-wrap: wrap; gap: 5px 16px; margin-top: 9px; color: #8b8077; font-size: 10px; line-height: 1.65; }
.history-placeholder { padding: 24px 12px; border-radius: 10px; color: var(--muted); background: #faf7f1; font-size: 12px; text-align: center; }
.history-placeholder strong { color: #776d63; font-size: 12px; font-weight: 500; }
.history-placeholder p { margin-top: 9px; font-size: 11px; }
.save-hint { margin-right: auto; align-self: center; color: var(--muted); font-size: 10px; }
@media (max-width: 1300px) { .lead-summary { gap: 10px; }.summary-item { padding: 17px 15px; }.summary-item p { font-size: 9px; }.lead-filters { grid-template-columns: 2fr 1fr 1fr; } }
/* Mobile support (added 2026-10-23) */
@media (max-width: 900px) {
  .lead-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .lead-filters { grid-template-columns: 1fr; padding: 16px; }
  .lead-detail { grid-template-columns: 1fr; }
  .lead-dialog { width: 100vw; }
  .crm-funnel { padding: 16px; }
}
</style>

