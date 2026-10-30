<template>
  <div class="notify-stats">
    <h3>通知触达统计</h3>
    <p class="muted">订单发货/收货的微信与短信通知发送情况。微信需配置 WECHAT_ORDER_TEMPLATE_ID，短信需配置 SMS_PROVIDER/SMS_SIGN（见部署 env 说明）。</p>
    <button class="btn" @click="load" :disabled="loading">{{ loading ? '加载中…' : '刷新' }}</button>
    <table v-if="items.length">
      <thead><tr><th>通道</th><th>总计</th><th>已送达</th><th>送达率</th><th>失败</th><th>跳过（未配置）</th><th>退订拦截</th></tr></thead>
      <tbody>
        <tr v-for="it in items" :key="it.channel">
          <td>{{ it.channel === 'WECHAT' ? '微信订阅消息' : '短信' }}</td>
          <td>{{ it.total }}</td>
          <td>{{ it.sent }}</td>
          <td>{{ it.deliveryRate.toFixed(1) }}%</td>
          <td>{{ it.failed }}</td>
          <td>{{ it.skipped }}</td>
          <td>{{ it.blocked }}</td>
        </tr>
      </tbody>
    </table>
    <p v-else class="muted">暂无通知记录</p>
  </div>
</template>
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../api'

interface StatItem { channel: string; total: number; sent: number; deliveryRate: number; failed: number; skipped: number; blocked: number }
const items = ref<StatItem[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await api<{ items: StatItem[] }>('admin.notify.stats', {})
    items.value = res.items || []
  } catch (e: any) { alert(e.message || '加载失败') }
  finally { loading.value = false }
}
onMounted(load)
</script>
<style scoped>
.notify-stats { padding: 12px 0; }
.muted { color: #888; font-size: 13px; margin: 8px 0; }
.btn { margin: 8px 0; }
table { width: 100%; border-collapse: collapse; margin-top: 8px; }
th, td { border: 1px solid #e5e0d5; padding: 8px 10px; text-align: left; font-size: 14px; }
th { background: #faf8f3; }
</style>
