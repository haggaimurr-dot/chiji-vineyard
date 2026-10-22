<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api, dateTime } from '../api'
import type { Lead } from '../types'

const props = defineProps<{ currentUserId?: string }>()
const emit = defineEmits<{ open: [leadId: string] }>()
const root = ref<HTMLElement>()
const toggle = ref<HTMLButtonElement>()
const expanded = ref(false)
const scope = ref<'all' | 'mine'>('all')
const leads = ref<Lead[]>([])
const loading = ref(false)
const error = ref('')
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined
let request = 0
const pending = computed(() => leads.value.filter(lead =>
  lead.reminderAt && !lead.reminderDone && !['WON', 'LOST'].includes(lead.status) &&
  Number.isFinite(Date.parse(lead.reminderAt)) && (scope.value === 'all' || lead.ownerId === props.currentUserId),
).sort((a, b) => Date.parse(a.reminderAt) - Date.parse(b.reminderAt)))
const due = computed(() => pending.value.filter(lead => Date.parse(lead.reminderAt) <= now.value))
const upcoming = computed(() => pending.value.filter(lead => Date.parse(lead.reminderAt) > now.value))
async function refresh() {
  const id = ++request
  loading.value = true
  try {
    const result = await api<Lead[]>('admin.lead.reminders')
    if (id !== request) return
    leads.value = result; now.value = Date.now(); error.value = ''
  } catch (cause) {
    if (id === request) error.value = (cause as Error).message
  } finally { if (id === request) loading.value = false }
}
function close(restoreFocus = false) { expanded.value = false; if (restoreFocus) toggle.value?.focus() }
function openLead(leadId: string) { close(); emit('open', leadId) }
function outside(event: MouseEvent) { if (expanded.value && !root.value?.contains(event.target as Node)) close() }
function escape(event: KeyboardEvent) { if (event.key === 'Escape' && expanded.value) close(true) }
function visible() { if (document.visibilityState === 'visible') void refresh() }
function openPanel() { expanded.value = !expanded.value; if (expanded.value) void refresh() }
onMounted(() => {
  void refresh()
  timer = setInterval(() => { now.value = Date.now(); visible() }, 60_000)
  window.addEventListener('chiji-leads-updated', refresh)
  window.addEventListener('focus', visible)
  document.addEventListener('visibilitychange', visible)
  document.addEventListener('mousedown', outside)
  document.addEventListener('keydown', escape)
})
onBeforeUnmount(() => {
  ++request; clearInterval(timer)
  window.removeEventListener('chiji-leads-updated', refresh)
  window.removeEventListener('focus', visible)
  document.removeEventListener('visibilitychange', visible)
  document.removeEventListener('mousedown', outside)
  document.removeEventListener('keydown', escape)
})
</script>

<template>
  <div ref="root" class="lead-reminders">
    <button ref="toggle" class="reminder-toggle" :aria-expanded="expanded" aria-controls="lead-reminder-panel" @click="openPanel">
      <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4" stroke-linecap="round" stroke-linejoin="round" /></svg>
      跟进提醒<span v-if="due.length && !error" class="reminder-count">{{ due.length }}</span><span v-if="error" class="reminder-stale" title="提醒刷新失败">!</span>
    </button>
    <section v-if="expanded" id="lead-reminder-panel" class="reminder-popover" aria-label="跟进提醒列表">
      <header><div><h3>跟进提醒</h3><p>到期 {{ due.length }} 条 · 待提醒 {{ upcoming.length }} 条</p></div><button class="text-button" :disabled="loading" @click="refresh">{{ loading ? '刷新中…' : '刷新' }}</button></header>
      <div class="reminder-scopes"><button :class="{ selected: scope === 'all' }" @click="scope = 'all'">全部提醒</button><button v-if="currentUserId" :class="{ selected: scope === 'mine' }" @click="scope = 'mine'">我的提醒</button></div>
      <p v-if="error" class="reminder-error" role="alert">{{ error }}，请点击刷新重试。</p>
      <div v-if="loading && !leads.length" class="reminder-empty">正在加载提醒…</div>
      <div v-else-if="!pending.length && !error" class="reminder-empty">暂无待跟进提醒<span>在线索中设置下次跟进时间。</span></div>
      <div v-if="pending.length" class="reminder-list">
        <button v-for="lead in pending" :key="lead.id" class="reminder-row" @click="openLead(lead.id)"><span class="reminder-dot" :class="{ overdue: Date.parse(lead.reminderAt) <= now }"></span><span class="reminder-customer"><strong>{{ lead.organization }}</strong><small>{{ lead.ownerName || '未分配' }} · {{ lead.contact }}</small><small>{{ dateTime(lead.reminderAt) }}</small></span><span class="reminder-state" :class="{ overdue: Date.parse(lead.reminderAt) <= now }">{{ Date.parse(lead.reminderAt) <= now ? '已到期' : '待提醒' }}</span></button>
      </div>
      <footer>后台站内提醒 · 每分钟刷新</footer>
    </section>
  </div>
</template>

<style scoped>
.lead-reminders { position: relative; }
.reminder-toggle { display: flex; align-items: center; gap: 7px; padding: 9px 11px; border: 1px solid var(--border); border-radius: 10px; color: var(--purple); background: var(--paper); font-size: 12px; font-weight: 700; }
.reminder-toggle:hover, .reminder-toggle[aria-expanded="true"] { background: var(--purple-soft); }
.reminder-count { display: grid; place-items: center; min-width: 18px; height: 18px; padding: 0 4px; border-radius: 20px; color: white; background: var(--danger); font-size: 10px; }.reminder-stale { color: var(--danger); }
.reminder-popover { position: absolute; z-index: 40; top: calc(100% + 13px); right: 0; width: 355px; max-height: min(580px, calc(100vh - 120px)); overflow: auto; border: 1px solid var(--border); border-radius: 16px; background: var(--paper); box-shadow: 0 18px 48px rgba(48, 27, 41, .17); }
.reminder-popover header { display: flex; justify-content: space-between; align-items: center; padding: 19px 19px 12px; }.reminder-popover h3 { font-size: 16px; }.reminder-popover header p { margin-top: 6px; color: var(--muted); font-size: 11px; }
.reminder-scopes { display: flex; gap: 6px; padding: 0 19px 13px; border-bottom: 1px solid var(--border); }.reminder-scopes button { padding: 6px 10px; border: 0; border-radius: 8px; background: transparent; color: var(--muted); font-size: 11px; }.reminder-scopes button.selected { background: var(--purple-soft); color: var(--purple); font-weight: 700; }
.reminder-error { margin: 12px 19px; color: var(--danger); font-size: 12px; line-height: 1.7; }.reminder-empty { padding: 30px 19px; text-align: center; color: var(--muted); font-size: 13px; }.reminder-empty span { display: block; margin-top: 8px; font-size: 11px; }
.reminder-row { display: flex; align-items: center; gap: 10px; width: 100%; padding: 15px 19px; border: 0; border-bottom: 1px solid var(--border); text-align: left; background: transparent; }.reminder-row:hover { background: #f7f3ec; }
.reminder-dot { flex: 0 0 7px; height: 7px; border-radius: 50%; background: var(--green); }.reminder-dot.overdue { background: var(--danger); }
.reminder-customer { flex: 1; min-width: 0; }.reminder-customer strong { display: block; overflow-wrap: anywhere; font-size: 13px; }.reminder-customer small { display: block; margin-top: 5px; color: var(--muted); font-size: 11px; }
.reminder-state { flex-shrink: 0; padding: 4px 6px; border-radius: 6px; background: var(--green-soft); color: var(--green); font-size: 10px; }.reminder-state.overdue { color: var(--danger); background: #f8e8e7; }.reminder-popover footer { padding: 12px 19px; color: var(--muted); font-size: 10px; }
</style>
