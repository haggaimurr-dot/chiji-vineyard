<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { confirmAction } from '../confirm'

interface OrderAlert { id: string; orderId: string; orderNo: string; kind: string; detail: string; status: string; createdAt: string; resolvedBy: string }

const kindLabel: Record<string, string> = { SHIP_TIMEOUT: '超时未发货', TRACKING_STUCK: '物流停滞', NO_TRACKING: '无揽收轨迹' }
const alerts = ref<OrderAlert[]>([])
const openCount = ref(0)
const loading = ref(false)
const error = ref('')
const showResolved = ref(false)
const busy = ref('')

async function load() {
  loading.value = true; error.value = ''
  try {
    const result = await api<{ items: OrderAlert[]; openCount: number }>('admin.order.alerts', { status: showResolved.value ? '' : 'OPEN', limit: 50 })
    alerts.value = result.items; openCount.value = result.openCount
  } catch (e) { error.value = (e as Error).message }
  finally { loading.value = false }
}
onMounted(load)

async function resolve(alert: OrderAlert) {
  if (!(await confirmAction({ title: '处理提醒', message: `确认已处理「${alert.detail}」？`, confirmText: '已处理' }))) return
  busy.value = alert.id
  try {
    await api('admin.order.alert.resolve', { id: alert.id })
    await load()
  } catch (e) { error.value = (e as Error).message }
  finally { busy.value = '' }
}
defineExpose({ refresh: load, openCount })
</script>

<template>
  <section class="panel">
    <div class="panel-head">
      <div><h3>订单异常提醒 <span v-if="openCount" class="alert-badge">{{ openCount }}</span></h3>
      <p>超时未发货（付款超 48 小时）与物流异常（轨迹停滞 48 小时 / 发货 24 小时无揽收）自动提醒；发货、揽收或轨迹恢复后自动消除</p></div>
      <div class="row-actions">
        <button type="button" class="button secondary" @click="showResolved = !showResolved; load()">{{ showResolved ? '只看未处理' : '查看已处理' }}</button>
        <button type="button" class="button secondary" :disabled="loading" @click="load">刷新</button>
      </div>
    </div>
    <p v-if="error" class="form-error">{{ error }}</p>
    <p v-if="loading" class="subtle">加载中…</p>
    <div v-else-if="!alerts.length" class="subtle empty">暂无{{ showResolved ? '' : '未处理的' }}超时提醒</div>
    <div v-for="a in alerts" :key="a.id" class="alert-row" :class="{ resolved: a.status !== 'OPEN' }">
      <div><strong>{{ a.orderNo || a.orderId }}</strong> <span class="kind-tag">{{ kindLabel[a.kind] || a.kind }}</span><small>{{ a.createdAt }}</small><p>{{ a.detail }}</p>
      <small v-if="a.status !== 'OPEN'" class="subtle">已处理{{ a.resolvedBy ? `（${a.resolvedBy}）` : '' }}</small></div>
      <button v-if="a.status === 'OPEN'" type="button" class="button secondary" :disabled="busy === a.id" @click="resolve(a)">{{ busy === a.id ? '处理中…' : '标记已处理' }}</button>
    </div>
  </section>
</template>

<style scoped>
.alert-badge { display: inline-block; min-width: 22px; text-align: center; padding: 2px 6px; border-radius: 11px; background: #b33232; color: #fff; font-size: 12px; }
.alert-row { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; padding: 12px 0; border-top: 1px solid var(--border); }
.alert-row.resolved { opacity: 0.65; }
.alert-row small { display: block; color: var(--muted); font-size: 11px; margin-top: 2px; }
.alert-row p { margin: 6px 0 0; font-size: 13px; }
.kind-tag { display: inline-block; padding: 1px 8px; border-radius: 10px; background: #fef3e2; color: #b26a00; font-size: 11px; margin-left: 6px; vertical-align: 1px; }
.empty { padding: 16px 0; }
</style>
