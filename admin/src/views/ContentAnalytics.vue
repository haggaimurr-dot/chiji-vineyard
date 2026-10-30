<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, money } from '../api'

interface Row { articleId: string; title: string; views: number; readers: number; favorites: number; conversions: number; conversionAmount: number }
const today = new Date().toISOString().slice(0, 10)
const start = ref(today), end = ref(today)
const items = ref<Row[]>([])
const total = ref<Row | null>(null)
const loading = ref(false), error = ref('')

function defaultRange() { const d = new Date(); d.setDate(d.getDate() - 29); start.value = d.toISOString().slice(0, 10); end.value = today }
async function load() {
  loading.value = true; error.value = ''
  try {
    const r = await api<{ items: Row[]; total: Row }>('admin.content.analytics', { start: start.value, end: end.value })
    items.value = r.items; total.value = r.total
  } catch (e) { error.value = (e as Error).message }
  finally { loading.value = false }
}
onMounted(() => { defaultRange(); void load() })
</script>

<template>
  <section class="panel">
    <div class="panel-head">
      <div><h3>内容数据分析</h3><p>阅读 / 收藏 / 转化（末次点击口径：从内容页商品入口下单且已付款），默认最近 30 天</p></div>
      <div class="row-actions">
        <input v-model="start" type="date" :max="end" aria-label="开始日期" />
        <span class="subtle">至</span>
        <input v-model="end" type="date" :min="start" :max="today" aria-label="结束日期" />
        <button type="button" class="button secondary" :disabled="loading" @click="load">查询</button>
      </div>
    </div>
    <p v-if="error" class="form-error">{{ error }}</p>
    <p v-if="loading" class="subtle">加载中…</p>
    <table v-else class="mini-table"><thead><tr><th>内容</th><th>阅读量</th><th>阅读人数</th><th>收藏数</th><th>转化订单</th><th>转化金额</th><th>转化率</th></tr></thead>
    <tbody>
      <tr v-for="r in items" :key="r.articleId"><td>{{ r.title }}</td><td>{{ r.views }}</td><td>{{ r.readers }}</td><td>{{ r.favorites }}</td><td>{{ r.conversions }}</td><td>{{ money(r.conversionAmount) }}</td><td>{{ r.readers ? (r.conversions / r.readers * 100).toFixed(2) + '%' : '—' }}</td></tr>
      <tr v-if="total" class="total-row"><td><strong>合计</strong></td><td><strong>{{ total.views }}</strong></td><td><strong>{{ total.readers }}</strong></td><td><strong>{{ total.favorites }}</strong></td><td><strong>{{ total.conversions }}</strong></td><td><strong>{{ money(total.conversionAmount) }}</strong></td><td><strong>{{ total.readers ? (total.conversions / total.readers * 100).toFixed(2) + '%' : '—' }}</strong></td></tr>
      <tr v-if="!items.length"><td colspan="7" class="subtle">该时段无已发布内容</td></tr>
    </tbody></table>
  </section>
</template>

<style scoped>
.panel-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; flex-wrap: wrap; }
.row-actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.mini-table { width: 100%; border-collapse: collapse; font-size: 13px; margin-top: 8px; }
.mini-table th, .mini-table td { padding: 8px; border-bottom: 1px solid var(--border); text-align: left; }
.total-row td { border-top: 2px solid var(--border); }
</style>
