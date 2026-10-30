<template>
  <div class="satis-stats">
    <h3>客服满意度统计</h3>
    <div class="row">
      <label>近 <input type="number" v-model.number="days" min="1" max="365" style="width:64px"> 天</label>
      <button class="btn" @click="load" :disabled="loading">{{ loading ? '加载中…' : '刷新' }}</button>
    </div>
    <div v-if="stats" class="summary">
      <div class="kpi"><span class="num">{{ stats.average.toFixed(2) }}</span><span class="lbl">平均分</span></div>
      <div class="kpi"><span class="num">{{ stats.total }}</span><span class="lbl">评价数</span></div>
    </div>
    <table v-if="stats">
      <thead><tr><th>星级</th><th>数量</th><th>占比</th></tr></thead>
      <tbody>
        <tr v-for="r in [5,4,3,2,1]" :key="r">
          <td>{{ '★'.repeat(r) }}</td>
          <td>{{ stats.distribution[r] || 0 }}</td>
          <td>{{ stats.total ? ((stats.distribution[r] || 0) / stats.total * 100).toFixed(1) : '0.0' }}%</td>
        </tr>
      </tbody>
    </table>
    <p v-else class="muted">暂无评价数据</p>
  </div>
</template>
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../api'

interface Stats { days: number; total: number; average: number; distribution: Record<string, number> }
const days = ref(30)
const stats = ref<Stats | null>(null)
const loading = ref(false)

async function load() {
  loading.value = true
  try { stats.value = await api<Stats>('admin.ticket.satisfactionStats', { days: days.value }) }
  catch (e: any) { alert(e.message || '加载失败') }
  finally { loading.value = false }
}
onMounted(load)
</script>
<style scoped>
.satis-stats { padding: 12px 0; }
.row { display: flex; gap: 12px; align-items: center; margin: 8px 0; }
.summary { display: flex; gap: 24px; margin: 12px 0; }
.kpi { background: #faf8f3; border: 1px solid #e5e0d5; border-radius: 8px; padding: 12px 24px; text-align: center; }
.kpi .num { font-size: 28px; font-weight: 700; display: block; }
.kpi .lbl { font-size: 13px; color: #888; }
.muted { color: #888; font-size: 13px; }
table { width: 100%; border-collapse: collapse; margin-top: 8px; }
th, td { border: 1px solid #e5e0d5; padding: 8px 10px; text-align: left; font-size: 14px; }
th { background: #faf8f3; }
</style>
