<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, dateTime } from '../api'
import { downloadCSV } from '../order-operations'
import { pickingCSV, pickingSummary } from '../picking'
import type { Order } from '../types'
const props = defineProps<{ selectedIds: string[]; disabled: boolean }>()
const emit = defineEmits<{ 'update:busy': [boolean] }>()
const orders = ref<Order[]>([]), busy = ref(false), error = ref(''), generatedAt = ref('')
const summary = computed(() => pickingSummary(orders.value))
async function generate() {
  if (busy.value || props.disabled) return
  busy.value = true; emit('update:busy', true); error.value = ''; orders.value = []
  try { orders.value = await api<Order[]>('admin.order.picking', { orderIds: props.selectedIds }); generatedAt.value = new Date().toISOString() }
  catch (e) { error.value = (e as Error).message }
  finally { busy.value = false; emit('update:busy', false) }
}
function print() {
  const popup = window.open('', '_blank', 'width=900,height=750')
  if (!popup) { error.value = '请允许弹出窗口后重试打印'; return }
  popup.document.title = '迟记葡萄牙 · 拣货单'
  const style = popup.document.createElement('style')
  style.textContent = '@page{size:A4;margin:12mm}body{font:14px sans-serif;color:#222}table{width:100%;border-collapse:collapse;margin:16px 0}td,th{border:1px solid #aaa;padding:8px;text-align:left}article{break-inside:avoid;border-bottom:1px solid #aaa;padding:12px 0}small{display:block}p{line-height:1.6}'
  popup.document.head.append(style)
  const content = document.getElementById('picking-print-content')
  if (!content) { popup.close(); return }
  popup.document.body.append(content.cloneNode(true)); popup.focus(); popup.print()
}
</script>

<template>
  <section class="panel picking-panel">
    <div class="panel-head"><div><h3>拣货单</h3><p>勾选当前页待发货订单，生成商品汇总与包装明细（每次最多50单）。</p></div><button v-permission="'order.read'" class="button primary" :disabled="disabled || busy || !selectedIds.length || selectedIds.length > 50" @click="generate">{{ busy ? '正在生成…' : '生成拣货单' }}</button></div>
    <p class="subtle">生成时重新核对订单状态。拣货单为当时快照，出库前请核对最新订单与售后状态。</p>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <template v-if="orders.length">
      <div class="operations-actions"><button class="button secondary" @click="print">打印拣货单</button><button v-permission="'order.export'" class="button secondary" @click="downloadCSV(pickingCSV(orders))">导出拣货单</button></div>
      <div id="picking-print-content">
        <h3>迟记葡萄牙 · 拣货单</h3><p>生成时间：{{ dateTime(generatedAt) }} · {{ orders.length }} 笔订单 · {{ summary.reduce((sum, row) => sum + row.quantity, 0) }} 件商品</p>
        <table><thead><tr><th>商品</th><th>规格</th><th>数量</th><th>涉及订单</th><th>核对</th></tr></thead><tbody><tr v-for="(row, index) in summary" :key="index"><td>{{ row.name }}</td><td>{{ row.skuName }}</td><td>{{ row.quantity }}</td><td>{{ row.orders.size }}</td><td>□</td></tr></tbody></table>
        <article v-for="order in orders" :key="order.id"><strong>{{ order.orderNo }} · {{ order.address.name }} · {{ order.address.phone }}</strong><p>{{ order.address.province }} {{ order.address.city }} {{ order.address.district }} {{ order.address.detail }}</p><small>配送：{{ order.deliveryDate || '尽快配送' }} · 备注：{{ order.note || '无' }}</small><p v-for="(item, index) in order.items" :key="index">□ {{ item.name }} · {{ item.skuName }} × {{ item.quantity }}</p></article>
      </div>
    </template>
  </section>
</template>

<style scoped>
.picking-panel { padding: 20px; margin: 16px 0; overflow-x: auto; }
.picking-panel article { padding: 16px 0; border-bottom: 1px solid #eee; }
.picking-panel .operations-actions { margin: 16px 0; }
</style>
