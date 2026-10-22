<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { can, currentAdmin } from '../permissions'
import { api, dateTime, isDemo, money } from '../api'
import type { Order } from '../types'

const props = defineProps<{ order: Order; disabled?: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const mode = ref(''), number = ref(''), reason = ref(''), busy = ref(false), error = ref(''), message = ref('')
const autoConfig = ref<{configured: boolean; enabled: boolean}>({configured: false, enabled: false})
const autoLabel = (status?: string) => ({READY: '等待自动开票', PROCESSING: '自动开票中', FAILED: '自动开票失败', UNCERTAIN: '开票结果待核实', BLOCKED: '退款期间暂停开票', ISSUED: '自动开票完成'}[status || ''] || '')
onMounted(async () => { if (can(currentAdmin(), 'order.read')) { try { autoConfig.value = await api('admin.invoice.autoConfig') } catch(e) { error.value = (e as Error).message } } })
async function autoIssue() {
 if (busy.value || props.disabled) return
 busy.value = true; error.value = ''; message.value = ''
 try { const result = await api<NonNullable<Order['invoice']>>('admin.invoice.autoIssue', {orderId: props.order.id}); message.value = autoLabel(result.autoStatus); emit('changed') }
 catch(e) { error.value = (e as Error).message } finally { busy.value = false }
}
const file = ref<File | null>(null), dialog = ref<HTMLElement | null>(null)
let trigger: HTMLElement | null = null
const emailLabel = (status?: string) => ({ READY: '等待发送', SENDING: '正在发送（刷新查看结果）', SENT: '邮件服务已接受', FAILED: '发送失败', UNCERTAIN: '发送结果待核实' }[status || ''] || '暂无邮件发送记录')
async function open(value: string) {
  trigger = document.activeElement as HTMLElement | null
  mode.value = value; number.value = ''; reason.value = ''; file.value = null; error.value = ''
  await nextTick(); dialog.value?.querySelector<HTMLElement>('input, textarea, button')?.focus()
}
function close() { if (!busy.value) { mode.value = ''; nextTick(() => trigger?.focus()) } }
function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); close() }
  if (event.key !== 'Tab') return
  const items = Array.from(dialog.value?.querySelectorAll<HTMLElement>('input:not(:disabled), textarea:not(:disabled), button:not(:disabled)') || [])
  if (event.shiftKey && document.activeElement === items[0]) { event.preventDefault(); items.at(-1)?.focus() }
  else if (!event.shiftKey && document.activeElement === items.at(-1)) { event.preventDefault(); items[0]?.focus() }
}
function selectFile(event: Event) { file.value = (event.target as HTMLInputElement).files?.[0] || null }
async function pdfData(selected: File): Promise<string> {
  if (!/\.pdf$/i.test(selected.name) || selected.size > 3 * 1024 * 1024) throw new Error('请选择不超过3MB的 PDF 发票')
  const content = await selected.arrayBuffer()
  if (new TextDecoder().decode(content.slice(0, 5)) !== '%PDF-') throw new Error('文件不是有效 PDF')
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result).split(',')[1] || '')
    reader.onerror = () => reject(new Error('文件读取失败，请重新选择'))
    reader.readAsDataURL(selected)
  })
}
async function save() {
  if (busy.value) return
  error.value = ''; busy.value = true
  try {
    let result: NonNullable<Order['invoice']>
    if (mode.value === 'ISSUED') {
      if (!number.value.trim()) throw new Error('请输入发票号码')
      if (!file.value) throw new Error('请选择已开具的 PDF 发票')
      const fileData = await pdfData(file.value)
      result = await api('admin.invoice.update', { orderId: props.order.id, status: 'ISSUED', number: number.value.trim(), fileName: file.value.name, fileData })
    } else {
      if (!reason.value.trim()) throw new Error('请填写原因或核实说明')
      result = await api(mode.value === 'REJECTED' ? 'admin.invoice.update' : 'admin.invoice.resolveEmail', { orderId: props.order.id, status: mode.value === 'REJECTED' ? 'REJECTED' : mode.value, reason: reason.value.trim() })
    }
    message.value = result.status === 'REJECTED' ? '申请已退回' : emailLabel(result.emailStatus)
    busy.value = false; close(); emit('changed')
  } catch (e) { error.value = (e as Error).message }
  finally { busy.value = false }
}
async function retry() {
  if (busy.value || props.disabled) return
  busy.value = true; error.value = ''
  try { const result = await api<NonNullable<Order['invoice']>>('admin.invoice.retryEmail', { orderId: props.order.id }); message.value = emailLabel(result.emailStatus); emit('changed') }
  catch (e) { error.value = (e as Error).message }
  finally { busy.value = false }
}
async function download() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try {
    const result = await api<{ fileName: string; fileData: string }>('admin.invoice.file', { orderId: props.order.id })
    const url = URL.createObjectURL(new Blob([Uint8Array.from(atob(result.fileData), c => c.charCodeAt(0))], { type: 'application/pdf' }))
    const anchor = document.createElement('a'); anchor.href = url; anchor.download = result.fileName; anchor.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  } catch (e) { error.value = (e as Error).message }
  finally { busy.value = false }
}
</script>

<template>
  <small v-if="order.invoice?.autoStatus" class="block">{{ autoLabel(order.invoice.autoStatus) }}</small><small v-if="order.invoice?.autoError" class="block error">{{order.invoice.autoError}}</small>
  <small v-if="order.invoice?.status === 'APPLIED' && !autoConfig.configured" class="block">自动开票服务未配置</small>
  <small v-else-if="order.invoice?.status === 'APPLIED' && autoConfig.enabled && !order.invoice.autoStatus" class="block">等待自动开票</small>
  <div v-if="order.invoice?.status === 'APPLIED' && autoConfig.configured" class="order-actions"><button v-permission="'invoice.write'" class="button small primary" :disabled="busy || disabled" @click="autoIssue">{{order.invoice.autoStatus === 'UNCERTAIN' ? '按原请求核查开票' : order.invoice.autoStatus === 'PROCESSING' ? '核查开票状态' : ['FAILED','BLOCKED'].includes(order.invoice.autoStatus || '') ? '重试自动开票' : '自动开票'}}</button></div>
  <small v-if="order.invoice?.createdAt" class="block">申请：{{ dateTime(order.invoice.createdAt) }}</small>
  <template v-if="order.invoice?.status === 'ISSUED'">
    <small class="block">{{ emailLabel(order.invoice.emailStatus) }}</small>
    <small v-if="order.invoice.issuedAt" class="block">开票：{{ dateTime(order.invoice.issuedAt) }}</small>
    <small v-if="order.invoice.sentAt" class="block">发送：{{ dateTime(order.invoice.sentAt) }}</small>
    <small v-if="order.invoice.emailError" class="block error">{{ order.invoice.emailError }}</small>
    <div class="order-actions">
      <button v-permission="'invoice.read'" v-if="order.invoice.fileName" class="button small secondary" :disabled="busy || disabled" @click="download">下载发票</button>
      <button v-permission="'invoice.write'" v-if="['READY','FAILED','SENDING'].includes(order.invoice.emailStatus || '')" class="button small primary" :disabled="busy || disabled" @click="retry">{{ order.invoice.emailStatus === 'SENDING' ? '核查发送状态' : '重试发送' }}</button>
      <template v-if="order.invoice.emailStatus === 'UNCERTAIN'"><button v-permission="'invoice.write'" class="button small secondary" :disabled="busy || disabled" @click="open('SENT')">核实已发送</button><button v-permission="'invoice.write'" class="button small secondary" :disabled="busy || disabled" @click="open('FAILED')">核实未发送</button></template>
    </div>
  </template>
  <div v-if="order.invoice?.status === 'APPLIED' && !['PROCESSING','UNCERTAIN'].includes(order.invoice.autoStatus || '')" class="order-actions"><button v-permission="'invoice.write'" class="button small primary" :disabled="busy || disabled" @click="open('ISSUED')">上传发票并发送</button><button v-permission="'invoice.write'" class="button small secondary" :disabled="busy || disabled" @click="open('REJECTED')">退回</button></div>
  <small v-if="message" class="block" role="status">{{ message }}</small><small v-if="error && !mode" class="block error" role="alert">{{ error }}</small>
  <Teleport to="body"><div v-if="mode" class="modal-backdrop" @click.self="close"><section ref="dialog" class="drawer" role="dialog" aria-modal="true" aria-label="处理发票申请" @keydown="keydown">
    <div class="drawer-head"><h2>{{ mode === 'ISSUED' ? '上传发票并发送邮件' : mode === 'REJECTED' ? '退回发票申请' : '核实邮件发送结果' }}</h2><button class="close" :disabled="busy" aria-label="关闭" @click="close">×</button></div>
    <form class="drawer-body" @submit.prevent="save">
      <p>订单：{{ order.orderNo }}</p><p>抬头：{{ order.invoice?.title }} · {{ money(order.invoice?.amount || 0) }}</p><p v-if="order.invoice?.kind === 'company'">税号：{{ order.invoice.taxId }}</p><p>收件邮箱：{{ order.invoice?.email }}</p>
      <template v-if="mode === 'ISSUED'"><p>核对 PDF 中的抬头、税号、金额和发票号码。保存后自动发送至申请邮箱。</p><p v-if="isDemo">演示模式模拟发送，不会向真实邮箱发邮件。</p><label>发票号码<input v-model="number" maxlength="80" required :disabled="busy" /></label><label>PDF 发票（最大3MB）<input type="file" accept=".pdf,application/pdf" required :disabled="busy" @change="selectFile" /></label></template>
      <label v-else>{{ mode === 'REJECTED' ? '退回原因（用户可见）' : '已核对邮件服务记录，核实说明' }}<textarea v-model="reason" maxlength="200" required :disabled="busy" /></label>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p><div class="order-actions"><button v-permission="'invoice.write'" class="button primary" :disabled="busy">{{ busy ? '正在处理…' : mode === 'ISSUED' ? '保存并发送邮件' : '保存' }}</button><button type="button" class="button secondary" :disabled="busy" @click="close">取消</button></div>
    </form>
  </section></div></Teleport>
</template>

<style scoped>
.drawer-body p { margin-bottom: 14px; overflow-wrap: anywhere; }
.drawer-body label { display: grid; gap: 8px; margin: 20px 0; }
.drawer-body input, .drawer-body textarea { width: 100%; padding: 12px; border: 1px solid var(--border); border-radius: 8px; background: var(--paper); }
.drawer-body textarea { min-height: 100px; resize: vertical; }
.block.error { color: var(--danger); }
</style>
