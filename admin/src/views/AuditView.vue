<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, dateTime } from '../api'
import type { AuditLog, AuditPage } from '../types'
import { downloadCSV } from '../order-operations'

const actionLabels: Record<string, string> = {
  AUDIT_EXPORT: '导出操作日志',
  PAYMENT_RECONCILE: '微信支付对账', PAYMENT_ALERT_RESOLVE: '处理支付告警',
  'admin.account.changePassword': '个人修改密码', 'admin.security.setup': '开始绑定验证器', 'admin.security.enable': '启用多因素认证', 'admin.security.disable': '关闭多因素认证',
  ADMIN_LOGIN: '管理员登录', SHIPPING_NOTICE_RETRY: '重试发货通知',
  CONFIG_SAVE: '保存配置', CONFIG_ROLLBACK: '回滚配置', COUPON_SAVE: '保存优惠券',
  ADMIN_ACCOUNT_SAVE: '维护管理员账号', ADMIN_PASSWORD_RESET: '重置管理员密码',
  PRODUCT_SAVE: '保存商品', INVENTORY_SET: '调整库存', INVENTORY_BATCH: '批量调整库存',
  ARTICLE_SAVE: '保存文章', ORDER_SHIP: '订单发货', INVOICE_UPDATE: '维护发票', INVOICE_EMAIL_RETRY: '重试发票邮件', INVOICE_EMAIL_RESOLVE: '核实发票邮件结果',
  ARTICLE_RESTORE: '恢复文章', ORDER_SHIPMENT_UPDATE: '更正发货信息', ORDER_SHIPMENT_RESYNC: '重试同步物流',
  ORDER_BATCH_SHIP: '批量发货', ORDER_EXPORT: '导出订单', WAYBILL_CREATE: '创建电子面单', WAYBILL_LABEL_SAVE: '保存面单', WAYBILL_CONFIG_SAVE: '电子面单配置',
  REFUND_APPROVE: '同意退款', REFUND_REJECT: '拒绝退款', REFUND_SYNC: '同步退款',
  LEAD_UPDATE: '跟进合作线索', MEDIA_UPLOAD: '上传素材',
}
const filters = reactive({ action: '', username: '', targetId: '', startDate: '', endDate: '' })
const applied = ref({ ...filters })
const items = ref<AuditLog[]>([]), selected = ref<AuditLog | null>(null)
const loading = ref(false), error = ref('')
const exporting = ref(false), exportMessage = ref('')
const busy = computed(() => loading.value || exporting.value)

async function exportLogs() {
  if (busy.value) return
  exporting.value = true
  error.value = ''
  exportMessage.value = ''
  try {
    const result = await api<{ filename: string; content: string; count: number }>('admin.audit.export', { ...applied.value })
    downloadCSV(result)
    exportMessage.value = `已导出 ${result.count} 条日志。`
  } catch (e) { error.value = (e as Error).message }
  finally { exporting.value = false }
}
const total = ref(0), offset = ref(0), limit = ref(20)
const pageNumber = computed(() => Math.floor(offset.value / limit.value) + 1)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / limit.value)))

async function load(nextOffset = offset.value) {
  if (busy.value) return
  loading.value = true
  error.value = ''
  try {
    const page = await api<AuditPage>('admin.audit.list', {
      ...Object.fromEntries(Object.entries(applied.value).filter(([, value]) => value)),
      limit: limit.value, offset: nextOffset,
    })
    items.value = page.items
    total.value = page.total
    offset.value = page.offset
  } catch (e) {
    error.value = (e as Error).message
    items.value = []
    total.value = 0
    offset.value = 0
  } finally { loading.value = false }
}

async function search() {
  if (busy.value) return
  if (filters.startDate && filters.endDate && filters.startDate > filters.endDate) {
    error.value = '开始日期不能晚于结束日期'
    return
  }
  applied.value = { ...filters, action: filters.action.trim(), username: filters.username.trim(), targetId: filters.targetId.trim() }
  await load(0)
}

async function reset() {
  if (busy.value) return
  Object.assign(filters, { action: '', username: '', targetId: '', startDate: '', endDate: '' })
  await search()
}

onMounted(() => load())
</script>

<template>
  <div class="toolbar"><div><h2>操作日志</h2><p>时间与日期筛选均使用北京时间；CSV 导出已查询条件下的全部日志，最多 10000 条</p></div><div class="row-actions"><button class="button secondary" :disabled="busy" @click="load()">刷新日志</button><button class="button primary" :disabled="busy" @click="exportLogs">{{ exporting ? '导出中…' : '导出日志 CSV' }}</button></div></div>
  <p v-if="exportMessage" class="success-text" role="status">{{ exportMessage }}</p>
  <form class="panel audit-filters" @submit.prevent="search">
    <fieldset :disabled="busy"><div class="form-grid audit-filter-grid">
      <label>操作类型<input v-model="filters.action" list="audit-actions" maxlength="100" placeholder="全部类型或输入操作标识" /><datalist id="audit-actions"><option v-for="(label, action) in actionLabels" :key="action" :value="action">{{ label }}</option></datalist></label>
      <label>操作人<input v-model="filters.username" maxlength="100" placeholder="管理员用户名" /></label>
      <label>目标 ID<input v-model="filters.targetId" maxlength="200" placeholder="配置、订单或其他目标 ID" /></label>
      <label>开始日期<input v-model="filters.startDate" type="date" :max="filters.endDate || undefined" /></label>
      <label>结束日期<input v-model="filters.endDate" type="date" :min="filters.startDate || undefined" /><small>包含当天全部记录</small></label>
    </div><div class="audit-filter-actions"><button class="button primary">{{ loading ? '查询中…' : '查询日志' }}</button><button type="button" class="button secondary" @click="reset">重置筛选</button><label>每页<select v-model.number="limit" @change="load(0)"><option :value="10">10 条</option><option :value="20">20 条</option><option :value="50">50 条</option></select></label></div></fieldset>
  </form>
  <p v-if="error" class="alert error" role="alert">{{ error }} <button class="text-button" :disabled="busy" @click="load()">重试</button></p>
  <section class="panel table-panel">
    <div class="audit-table"><table><thead><tr><th>时间（北京时间）</th><th>操作人</th><th>操作类型</th><th>目标 ID</th><th>详情</th></tr></thead><tbody>
      <tr v-if="loading"><td colspan="5" class="audit-empty" role="status">正在加载操作日志…</td></tr>
      <tr v-else-if="!items.length"><td colspan="5" class="audit-empty">{{ error ? '操作日志加载失败' : '暂无符合条件的操作日志' }}</td></tr>
      <tr v-else v-for="log in items" :key="log.id"><td>{{ dateTime(log.createdAt) }}</td><td><strong>{{ log.username || '系统' }}</strong></td><td>{{ actionLabels[log.action] || log.action }}<small class="block">{{ log.action }}</small></td><td class="audit-target">{{ log.targetId || '—' }}</td><td><button class="text-button" @click="selected = log">查看详情</button></td></tr>
    </tbody></table></div>
    <div class="audit-pagination"><span>共 {{ total }} 条 · 第 {{ pageNumber }} / {{ pageCount }} 页</span><div><button class="button secondary small" :disabled="busy || offset === 0" @click="load(Math.max(0, offset - limit))">上一页</button><button class="button secondary small" :disabled="busy || offset + limit >= total" @click="load(offset + limit)">下一页</button></div></div>
  </section>
  <div v-if="selected" class="modal-backdrop" @click.self="selected = null" @keydown.esc="selected = null">
    <section class="drawer" role="dialog" aria-modal="true" aria-labelledby="audit-detail-title"><div class="drawer-head"><h2 id="audit-detail-title">操作日志详情</h2><button class="close" aria-label="关闭日志详情" @click="selected = null">×</button></div><div class="drawer-body">
      <dl class="audit-metadata"><dt>日志 ID</dt><dd>{{ selected.id }}</dd><dt>操作人</dt><dd>{{ selected.username || '系统' }}</dd><dt>管理员 ID</dt><dd>{{ selected.adminId || '—' }}</dd><dt>操作类型</dt><dd>{{ actionLabels[selected.action] || selected.action }}（{{ selected.action }}）</dd><dt>目标 ID</dt><dd>{{ selected.targetId || '—' }}</dd><dt>操作时间</dt><dd>{{ dateTime(selected.createdAt) }}（北京时间）</dd></dl>
      <h3 class="audit-detail-heading">操作详情</h3><pre class="audit-json">{{ JSON.stringify(selected.detail, null, 2) }}</pre>
    </div><div class="drawer-foot"><button class="button secondary" @click="selected = null">关闭</button></div></section>
  </div>
</template>

<style scoped>
.audit-filters{padding:24px;margin-bottom:20px}.audit-filters fieldset{border:0;margin:0;padding:0;min-width:0}.audit-filter-grid{grid-template-columns:repeat(3,minmax(150px,1fr));gap:16px}.audit-filter-actions{display:flex;align-items:center;gap:12px;margin-top:20px}.audit-filter-actions label{display:flex;align-items:center;gap:10px;margin-left:auto;color:var(--muted);font-size:12px}.audit-filter-actions select{padding:9px;border:1px solid var(--border);border-radius:8px;background:#fbf9f5}.audit-table{overflow:auto}.audit-empty{padding:38px;text-align:center;color:var(--muted)}.audit-target{max-width:260px;overflow-wrap:anywhere}.audit-pagination{display:flex;align-items:center;justify-content:space-between;padding:16px 24px;border-top:1px solid var(--border);color:var(--muted);font-size:11px}.audit-pagination>div{display:flex;gap:8px}.audit-metadata{display:grid;grid-template-columns:90px 1fr;gap:14px;font-size:12px}.audit-metadata dt{color:var(--muted)}.audit-metadata dd{margin:0;overflow-wrap:anywhere}.audit-detail-heading{margin-top:28px;font-size:14px}.audit-json{padding:18px;border:1px solid var(--border);border-radius:10px;background:#f8f4ee;font-size:12px;line-height:1.6;white-space:pre-wrap;overflow-wrap:anywhere}
/* Mobile support (added 2026-10-23) */
@media (max-width: 900px) {
  .audit-filters { padding: 16px; }
  .audit-filter-grid { grid-template-columns: 1fr; }
  .audit-filter-actions { flex-wrap: wrap; }
  .audit-filter-actions label { margin-left: 0; }
  .audit-pagination { flex-wrap: wrap; gap: 10px; padding: 14px 16px; }
}
</style>
