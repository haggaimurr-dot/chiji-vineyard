<template>
  <div class="runtime-status">
    <div class="head">
      <h3>服务健康状态</h3>
      <span :class="['badge', healthy ? 'ok' : 'bad']">{{ healthy === null ? '…' : (healthy ? '全部正常' : '有异常') }}</span>
      <button class="btn" @click="load" :disabled="loading">{{ loading ? '检查中…' : '重新检查' }}</button>
      <span class="muted" v-if="checkedAt">检查时间：{{ checkedAt }}</span>
    </div>
    <table>
      <thead><tr><th>组件</th><th>状态</th><th>上次运行</th><th>说明</th></tr></thead>
      <tbody>
        <tr v-for="it in items" :key="it.component" :class="{ bad: !['OK','DISABLED'].includes(it.status) }">
          <td>{{ it.label }}</td>
          <td><span :class="['dot', ['OK','DISABLED'].includes(it.status) ? 'ok' : 'bad']"></span>{{ statusText(it.status) }}</td>
          <td>{{ it.lastRunAgo || '—' }}</td>
          <td class="muted">{{ it.detail || '—' }}</td>
        </tr>
      </tbody>
    </table>
    <p class="muted">心跳由各后台任务每轮写入；超过阈值未更新即标红。后端 API / 数据库异常也会在此标红。</p>
  </div>
</template>
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../api'

interface Item { component: string; label: string; status: string; lastRunAgo: string; detail: string }
const items = ref<Item[]>([])
const healthy = ref<boolean | null>(null)
const checkedAt = ref('')
const loading = ref(false)

function statusText(s: string) { return s === 'OK' ? '正常' : s === 'STALE' ? '心跳超时' : s === 'DOWN' ? '不可用' : s === 'DISABLED' ? '未启用' : '未知' }

async function load() {
  loading.value = true
  try {
    const res = await api<{ healthy: boolean; items: Item[]; checkedAt: string }>('admin.runtime.status', {})
    items.value = res.items || []
    healthy.value = res.healthy
    checkedAt.value = res.checkedAt
  } catch (e: any) { alert(e.message || '检查失败') }
  finally { loading.value = false }
}
onMounted(load)
</script>
<style scoped>
.runtime-status { padding: 12px 0; }
.head { display: flex; gap: 12px; align-items: center; margin-bottom: 8px; }
.badge { padding: 2px 12px; border-radius: 12px; font-size: 13px; }
.badge.ok { background: #e6f4ea; color: #1a7f37; }
.badge.bad { background: #fdecea; color: #c62828; }
.muted { color: #888; font-size: 13px; }
table { width: 100%; border-collapse: collapse; margin-top: 8px; }
th, td { border: 1px solid #e5e0d5; padding: 8px 10px; text-align: left; font-size: 14px; }
th { background: #faf8f3; }
tr.bad td { background: #fdf0ef; }
.dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; margin-right: 6px; }
.dot.ok { background: #2da44e; }
.dot.bad { background: #d92d20; }
</style>
