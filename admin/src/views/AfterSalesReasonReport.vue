<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, money } from '../api'
import { downloadCSV } from '../order-operations'
import { ticketCategories } from '../after-sales'

interface RefundReasonStat { reason: string; count: number; amount: number }
interface TicketCategoryStat { category: string; count: number; open: number }
interface Report { startDate: string; endDate: string; refundTotal: number; refundAmount: number; ticketTotal: number; refundReasons: RefundReasonStat[]; ticketCategories: TicketCategoryStat[] }

const today = new Date().toISOString().slice(0, 10)
const start = ref(today), end = ref(today)
const report = ref<Report | null>(null)
const loading = ref(false), exporting = ref(false), error = ref('')

function defaultRange() {
  const d = new Date(); d.setDate(d.getDate() - 29)
  start.value = d.toISOString().slice(0, 10); end.value = today
}
async function load() {
  loading.value = true; error.value = ''
  try { report.value = await api<Report>('admin.aftersales.reasonReport', { start: start.value, end: end.value }) }
  catch (e) { error.value = (e as Error).message }
  finally { loading.value = false }
}
async function exportCSV() {
  exporting.value = true; error.value = ''
  try { downloadCSV(await api<{ filename: string; content: string }>('admin.aftersales.reasonReport.export', { start: start.value, end: end.value })) }
  catch (e) { error.value = (e as Error).message }
  finally { exporting.value = false }
}
onMounted(() => { defaultRange(); void load() })
</script>

<template>
  <section class="panel">
    <div class="panel-head">
      <div><h3>售后原因分析</h3><p>退款原因与工单类别分布，默认最近 30 天</p></div>
      <div class="row-actions">
        <input v-model="start" type="date" :max="end" aria-label="开始日期" />
        <span class="subtle">至</span>
        <input v-model="end" type="date" :min="start" :max="today" aria-label="结束日期" />
        <button type="button" class="button secondary" :disabled="loading" @click="load">查询</button>
        <button type="button" class="button secondary" :disabled="exporting || !report" @click="exportCSV">{{ exporting ? '导出中…' : '导出 CSV' }}</button>
      </div>
    </div>
    <p v-if="error" class="form-error">{{ error }}</p>
    <p v-if="loading" class="subtle">加载中…</p>
    <div v-else-if="report" class="stat-grid">
      <div class="stat"><b>{{ report.refundTotal }}</b><span>退款笔数</span></div>
      <div class="stat"><b>{{ money(report.refundAmount) }}</b><span>退款金额</span></div>
      <div class="stat"><b>{{ report.ticketTotal }}</b><span>工单数</span></div>
    </div>
    <div v-if="report" class="report-cols">
      <div>
        <h4>退款原因分布</h4>
        <table class="mini-table"><thead><tr><th>原因</th><th>笔数</th><th>金额</th><th>占比</th></tr></thead>
        <tbody>
          <tr v-for="r in report.refundReasons" :key="r.reason"><td>{{ r.reason }}</td><td>{{ r.count }}</td><td>{{ money(r.amount) }}</td><td>{{ report.refundTotal ? Math.round(r.count / report.refundTotal * 100) + '%' : '—' }}</td></tr>
          <tr v-if="!report.refundReasons.length"><td colspan="4" class="subtle">该时段无退款</td></tr>
        </tbody></table>
      </div>
      <div>
        <h4>工单类别分布</h4>
        <table class="mini-table"><thead><tr><th>类别</th><th>数量</th><th>未关闭</th></tr></thead>
        <tbody>
          <tr v-for="t in report.ticketCategories" :key="t.category"><td>{{ ticketCategories[t.category] || t.category }}</td><td>{{ t.count }}</td><td>{{ t.open }}</td></tr>
          <tr v-if="!report.ticketCategories.length"><td colspan="3" class="subtle">该时段无工单</td></tr>
        </tbody></table>
      </div>
    </div>
  </section>
</template>

<style scoped>
.stat-grid { display: flex; gap: 16px; margin: 12px 0; }
.stat { flex: 1; background: var(--bg-soft, #f7f7f7); border-radius: 8px; padding: 12px; text-align: center; }
.stat b { display: block; font-size: 20px; }
.stat span { font-size: 12px; color: var(--muted); }
.report-cols { display: grid; grid-template-columns: 1fr 1fr; gap: 20px; margin-top: 12px; }
.report-cols h4 { margin: 0 0 8px; }
@media (max-width: 720px) { .report-cols { grid-template-columns: 1fr; } }
.mini-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.mini-table th, .mini-table td { padding: 8px; border-bottom: 1px solid var(--border); text-align: left; }
.panel-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; flex-wrap: wrap; }
.row-actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
</style>
