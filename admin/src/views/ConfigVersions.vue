<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, dateTime } from '../api'
import type { ConfigKey, ConfigVersion, ConfigVersionsPage } from '../types'
import { configDiff } from '../config-diff'

const props = defineProps<{ locked: boolean; revision: number }>()
const emit = defineEmits<{ busy: [value: boolean]; restored: [key: ConfigKey] }>()
const labels: Record<ConfigKey, string> = { shipping: '运费规则', coupons: '优惠券', home: '首页运营位', privacy: '隐私政策' }
const actions: Record<ConfigVersion['action'], string> = { BASELINE: '初始快照', SAVE: '保存配置', ROLLBACK: '回滚配置' }
const key = ref<ConfigKey | ''>('')
const isSuper = computed(() => { try { return JSON.parse(localStorage.getItem('chiji-admin') || '{}').role === 'SUPER_ADMIN' } catch { return false } })
interface ChangeOrder { id: string; configKey: ConfigKey; payload: unknown; status: string; createdBy: string; createdAt: string; decidedBy: string; decidedAt: string; note: string }
const orders = ref<ChangeOrder[]>([])
const ordersLoading = ref(false), ordersError = ref(''), decideNote = ref(''), decidingId = ref('')
async function loadOrders() {
  if (!isSuper.value) return
  ordersLoading.value = true; ordersError.value = ''
  try { const r = await api<{ items: ChangeOrder[] }>('admin.config.changeOrders', { status: 'PENDING' }); orders.value = r.items }
  catch (e) { ordersError.value = (e as Error).message }
  finally { ordersLoading.value = false }
}
async function decide(order: ChangeOrder, approve: boolean) {
  if (decidingId.value) return
  decidingId.value = order.id; ordersError.value = ''
  try {
    await api('admin.config.decide', { id: order.id, approve, note: decideNote.value.trim() })
    decideNote.value = ''
    await loadOrders()
    await load(0)
  } catch (e) { ordersError.value = (e as Error).message }
  finally { decidingId.value = '' }
}
const items = ref<ConfigVersion[]>([])
const currentVersions = ref<Partial<Record<ConfigKey, number>>>({})
const total = ref(0), limit = ref(20), offset = ref(0)
const loading = ref(false), error = ref(''), message = ref('')
const selected = ref<ConfigVersion | null>(null)
const reason = ref(''), confirming = ref(false), expectedVersion = ref<number | null>(null)
const rollingBack = ref(false), rollbackError = ref('')
const fromVersion = ref(1), toVersion = ref(1), comparing = ref(false), compareError = ref('')
const comparison = ref<{ fromVersion: number; toVersion: number; before: unknown; after: unknown } | null>(null)
const changes = computed(() => comparison.value ? configDiff(comparison.value.before, comparison.value.after) : [])
const changeLabels = { added: '新增', removed: '删除', changed: '修改' }
let compareRequest = 0

async function compare() {
  if (!selected.value || comparing.value) return
  const requestId = ++compareRequest
  comparing.value = true
  compareError.value = ''
  comparison.value = null
  try {
    const result = await api<{ fromVersion: number; toVersion: number; before: unknown; after: unknown }>('admin.config.compare', {
      key: selected.value.key, fromVersion: fromVersion.value, toVersion: toVersion.value,
    })
    if (requestId === compareRequest) comparison.value = result
  } catch (e) { if (requestId === compareRequest) compareError.value = (e as Error).message }
  finally { if (requestId === compareRequest) comparing.value = false }
}
const locked = computed(() => props.locked || rollingBack.value)
const currentVersion = computed(() => selected.value ? currentVersions.value[selected.value.key] : undefined)
const canRollback = computed(() => !!selected.value && currentVersion.value !== undefined && selected.value.version !== currentVersion.value && !loading.value && !locked.value)
const pageNumber = computed(() => Math.floor(offset.value / limit.value) + 1)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / limit.value)))
let request = 0

async function load(nextOffset = offset.value) {
  const requestId = ++request
  loading.value = true
  error.value = ''
  try {
    const page = await api<ConfigVersionsPage>('admin.config.versions', { key: key.value || undefined, limit: limit.value, offset: nextOffset })
    if (requestId !== request) return
    if (nextOffset > 0 && nextOffset >= page.total) {
      await load(page.total > 0 ? Math.floor((page.total - 1) / limit.value) * limit.value : 0)
      return
    }
    items.value = page.items
    total.value = page.total
    offset.value = page.offset
    currentVersions.value = page.currentVersions
  } catch (e) {
    if (requestId !== request) return
    error.value = (e as Error).message
    items.value = []
    total.value = 0
    offset.value = 0
    currentVersions.value = {}
  } finally {
    if (requestId === request) loading.value = false
  }
}

function inspect(version: ConfigVersion) {
  ++compareRequest
  comparing.value = false
  comparison.value = null
  compareError.value = ''
  fromVersion.value = version.version
  toVersion.value = currentVersions.value[version.key] || version.version
  selected.value = version
  confirming.value = false
  reason.value = ''
  rollbackError.value = ''
  expectedVersion.value = null
}

function prepareRollback() {
  if (!canRollback.value || !selected.value) return
  expectedVersion.value = currentVersions.value[selected.value.key] ?? null
  rollbackError.value = ''
  confirming.value = true
}

async function rollback() {
  if (rollingBack.value || props.locked || !selected.value || expectedVersion.value === null) return
  if (!reason.value.trim()) { rollbackError.value = '请填写回滚原因'; return }
  rollingBack.value = true
  emit('busy', true)
  rollbackError.value = ''
  message.value = ''
  const target = selected.value
  try {
    const result = await api<{ key: ConfigKey; version: number; payload: unknown }>('admin.config.rollback', {
      key: target.key, versionId: target.id, expectedVersion: expectedVersion.value, reason: reason.value.trim(),
    })
    selected.value = null
    confirming.value = false
    message.value = `${labels[result.key]}已恢复为 v${target.version} 的内容，并生成新版本 v${result.version}。`
    emit('restored', result.key)
    await load(0)
  } catch (e) {
    const failure = e as Error & { code?: string }
    if (['CONFIG_VERSION_CONFLICT', 'CONFIG_STALE', 'VERSION_CONFLICT', 'CONFLICT'].includes(failure.code || '') || /配置.*(变化|更新|冲突|过期)/.test(failure.message)) {
      confirming.value = false
      expectedVersion.value = null
      await load()
      rollbackError.value = '当前配置已被更新，版本列表已刷新。请核对当前版本后重新发起回滚。'
    } else rollbackError.value = failure.message
  } finally {
    rollingBack.value = false
    emit('busy', false)
  }
}

function close() { if (!rollingBack.value) selected.value = null }
watch(() => props.revision, () => { confirming.value = false; void load(); void loadOrders() })
onMounted(() => { load(); loadOrders() })
</script>

<template>
  <section v-if="isSuper" class="panel table-panel history-panel">
    <div class="panel-head"><div><h3>配置变更审批</h3><p>普通管理员提交的变更单，批准后立即生效并记版本</p></div><button class="button secondary small" :disabled="ordersLoading" @click="loadOrders()">刷新</button></div>
    <p v-if="ordersError" class="history-notice form-error" role="alert">{{ ordersError }}</p>
    <div class="history-table"><table><thead><tr><th>配置</th><th>提交人</th><th>时间</th><th>变更内容</th><th>审批</th></tr></thead><tbody>
      <tr v-if="ordersLoading"><td colspan="5" class="history-empty">正在加载变更单…</td></tr>
      <tr v-else-if="!orders.length"><td colspan="5" class="history-empty">暂无待审批变更单</td></tr>
      <tr v-else v-for="order in orders" :key="order.id"><td><strong>{{ labels[order.configKey] }}</strong></td><td>{{ order.createdBy }}</td><td>{{ dateTime(order.createdAt) }}</td><td><pre class="history-json mini">{{ JSON.stringify(order.payload, null, 2) }}</pre></td>
        <td><div class="row-actions"><button class="button primary small" :disabled="!!decidingId" @click="decide(order, true)">{{ decidingId === order.id ? '处理中…' : '批准' }}</button><button class="button secondary small" :disabled="!!decidingId" @click="decide(order, false)">驳回</button></div><input v-model="decideNote" class="note-input" maxlength="200" placeholder="审批备注（可选）" /></td></tr>
    </tbody></table></div>
  </section>
  <section class="panel table-panel history-panel">
    <div class="panel-head"><div><h3>配置版本与回滚</h3><p>保存与回滚均保留完整快照；时间为北京时间</p></div><button class="button secondary small" :disabled="loading || locked" @click="load()">刷新版本</button></div>
    <div class="history-filters">
      <label>配置类型<select v-model="key" :disabled="loading || locked" @change="load(0)"><option value="">全部配置</option><option v-for="(label, value) in labels" :key="value" :value="value">{{ label }}</option></select></label>
      <label>每页条数<select v-model.number="limit" :disabled="loading || locked" @change="load(0)"><option :value="10">10 条</option><option :value="20">20 条</option><option :value="50">50 条</option></select></label>
      <div class="current-versions"><span v-for="(version, configKey) in currentVersions" :key="configKey">{{ labels[configKey] }} {{ version ? `v${version}` : '尚未记录版本' }}</span></div>
    </div>
    <p v-if="message" class="history-notice success-text" role="status">{{ message }}</p>
    <p v-if="error" class="history-notice form-error" role="alert">{{ error }} <button class="text-button" :disabled="loading || locked" @click="load()">重试</button></p>
    <div class="history-table"><table><thead><tr><th>配置 / 版本</th><th>操作</th><th>操作人</th><th>时间（北京时间）</th><th>详情</th></tr></thead><tbody>
      <tr v-if="loading"><td colspan="5" class="history-empty" role="status">正在加载配置版本…</td></tr>
      <tr v-else-if="!items.length"><td colspan="5" class="history-empty">{{ error ? '配置版本加载失败' : '暂无配置版本' }}</td></tr>
      <tr v-else v-for="version in items" :key="version.id"><td><strong>{{ labels[version.key] }} · v{{ version.version }}</strong><small v-if="currentVersions[version.key] === version.version" class="block">当前版本</small></td><td>{{ actions[version.action] }}<small v-if="version.sourceVersion !== null" class="block">来自 v{{ version.sourceVersion }}</small></td><td>{{ version.username || '系统' }}</td><td>{{ dateTime(version.createdAt) }}</td><td><button class="text-button" :disabled="rollingBack" @click="inspect(version)">查看快照</button></td></tr>
    </tbody></table></div>
    <div class="history-pagination"><span>共 {{ total }} 条 · 第 {{ pageNumber }} / {{ pageCount }} 页</span><div><button class="button secondary small" :disabled="loading || locked || offset === 0" @click="load(Math.max(0, offset - limit))">上一页</button><button class="button secondary small" :disabled="loading || locked || offset + limit >= total" @click="load(offset + limit)">下一页</button></div></div>
  </section>

  <div v-if="selected" class="modal-backdrop" @click.self="close" @keydown.esc="close">
    <section class="drawer" role="dialog" aria-modal="true" aria-labelledby="config-version-title">
      <div class="drawer-head"><h2 id="config-version-title">{{ labels[selected.key] }} · v{{ selected.version }}</h2><button class="close" :disabled="rollingBack" aria-label="关闭快照" @click="close">×</button></div>
      <div class="drawer-body">
        <dl class="history-metadata"><dt>版本 ID</dt><dd>{{ selected.id }}</dd><dt>操作</dt><dd>{{ actions[selected.action] }}</dd><dt>来源版本</dt><dd>{{ selected.sourceVersion === null ? '—' : `v${selected.sourceVersion}` }}</dd><dt>操作人</dt><dd>{{ selected.username || '系统' }}</dd><dt>记录时间</dt><dd>{{ dateTime(selected.createdAt) }}（北京时间）</dd><dt>当前版本</dt><dd>{{ currentVersion === undefined ? '未加载' : `v${currentVersion}` }}</dd></dl>
        <h3 class="snapshot-title">完整配置快照</h3><pre class="history-json">{{ JSON.stringify(selected.payload, null, 2) }}</pre>
        <h3 class="snapshot-title">版本差异对比</h3>
        <form class="compare-form" @submit.prevent="compare">
          <label>原版本<input v-model.number="fromVersion" type="number" min="1" :max="currentVersion || undefined" required :disabled="comparing" /></label>
          <span aria-hidden="true">→</span>
          <label>目标版本<input v-model.number="toVersion" type="number" min="1" :max="currentVersion || undefined" required :disabled="comparing" /></label>
          <button class="button secondary small" :disabled="comparing">{{ comparing ? '对比中…' : '对比版本' }}</button>
        </form>
        <p class="subtle">填写同一配置的版本号；数组按位置对比，字段路径以 / 分隔，数组下标从 0 开始。</p>
        <p v-if="compareError" class="form-error" role="alert">{{ compareError }}</p>
        <div v-if="comparison" aria-live="polite">
          <p>v{{ comparison.fromVersion }} → v{{ comparison.toVersion }}：{{ changes.length ? `共 ${changes.length} 处差异` : '配置内容完全相同' }}</p>
          <div v-for="change in changes" :key="change.path" class="config-change">
            <strong>{{ changeLabels[change.kind] }} · {{ change.path }}</strong>
            <div class="compare-values"><div><small>原值</small><pre>{{ change.kind === 'added' ? '（不存在）' : JSON.stringify(change.before, null, 2) }}</pre></div><div><small>目标值</small><pre>{{ change.kind === 'removed' ? '（不存在）' : JSON.stringify(change.after, null, 2) }}</pre></div></div>
          </div>
        </div>
        <p v-if="selected.version === currentVersion" class="subtle history-notice">该版本已是当前配置。</p>
        <form v-if="confirming" class="rollback-form" @submit.prevent="rollback">
          <p>将当前 v{{ expectedVersion }} 的内容替换为此快照，并生成新版本；四个配置编辑表单的未保存修改将清除。此操作会立即生效。</p>
          <label>回滚原因<textarea v-model="reason" :disabled="rollingBack || props.locked" required maxlength="500" rows="3" placeholder="填写本次回滚的原因，便于后续追溯" /></label>
          <div class="row-actions"><button class="button primary" :disabled="rollingBack || props.locked || !reason.trim()">{{ rollingBack ? '正在回滚…' : '确认回滚' }}</button><button type="button" class="button secondary" :disabled="rollingBack" @click="confirming = false">取消</button></div>
        </form>
        <p v-if="rollbackError" class="form-error" role="alert">{{ rollbackError }}</p>
      </div>
      <div class="drawer-foot"><button class="button secondary" :disabled="rollingBack" @click="close">关闭</button><button v-if="!confirming" class="button primary" :disabled="!canRollback" @click="prepareRollback">回滚至此版本</button></div>
    </section>
  </div>
</template>

<style scoped>
.compare-form{display:flex;align-items:end;gap:12px;flex-wrap:wrap;margin-bottom:12px}.compare-form label{display:grid;gap:8px;font-size:12px}.compare-form input{width:100px;padding:9px;border:1px solid var(--border);border-radius:8px}.config-change{margin:16px 0;padding:14px;border:1px solid var(--border);border-radius:10px;font-size:12px;overflow-wrap:anywhere}.compare-values{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-top:12px}.compare-values>div{min-width:0}.compare-values small{color:var(--muted)}.compare-values pre{white-space:pre-wrap;overflow-wrap:anywhere;padding:10px;background:var(--cream);border-radius:6px}.compare-values>div:last-child pre{background:var(--purple-soft)}
.history-panel{margin-bottom:24px}.history-filters{display:flex;align-items:end;gap:16px;padding:18px 24px;border-bottom:1px solid var(--border)}.history-filters label{display:grid;gap:7px;font-size:11px;font-weight:700}.history-filters select{padding:9px 12px;border:1px solid var(--border);border-radius:8px;background:#fbf9f5}.current-versions{display:flex;flex-wrap:wrap;gap:8px;margin-left:auto;color:var(--muted);font-size:11px}.current-versions span{padding:7px 9px;background:var(--cream);border-radius:8px}.history-notice{margin:16px 24px}.history-table{overflow:auto}.history-empty{text-align:center;padding:34px;color:var(--muted)}.history-pagination{display:flex;align-items:center;justify-content:space-between;padding:16px 24px;border-top:1px solid var(--border);color:var(--muted);font-size:11px}.history-pagination>div{display:flex;gap:8px}.history-metadata{display:grid;grid-template-columns:90px 1fr;gap:12px;font-size:12px}.history-metadata dt{color:var(--muted)}.history-metadata dd{margin:0;overflow-wrap:anywhere}.snapshot-title{margin-top:24px;font-size:14px}.history-json{padding:18px;border:1px solid var(--border);border-radius:10px;background:#f8f4ee;font-size:12px;line-height:1.6;white-space:pre-wrap;overflow-wrap:anywhere}.rollback-form{display:grid;gap:16px;margin-top:24px;padding:18px;border:1px solid #dec9e0;border-radius:12px;background:var(--purple-soft)}.rollback-form p{font-size:12px;line-height:1.8}.rollback-form label{display:grid;gap:9px;font-size:12px;font-weight:700}.rollback-form textarea{width:100%;padding:12px;border:1px solid var(--border);border-radius:9px;background:var(--paper)}
.history-json.mini{max-height:180px;overflow:auto;font-size:11px;padding:10px}.note-input{width:150px;margin-top:6px;padding:6px;border:1px solid var(--border);border-radius:6px;font-size:11px}
@media(max-width:1180px){.history-filters{flex-wrap:wrap}.current-versions{width:100%;margin-left:0}.history-table table{min-width:640px}}
/* Mobile support (added 2026-10-23) */
@media (max-width: 700px) {
  .compare-values { grid-template-columns: 1fr; }
  .history-filters { padding: 14px 16px; }
  .history-notice { margin: 14px 16px; }
  .history-pagination { flex-wrap: wrap; gap: 10px; padding: 14px 16px; }
  .history-metadata { grid-template-columns: 70px 1fr; }
}
</style>
