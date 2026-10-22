<script setup lang="ts">
import { computed } from 'vue'
import type { LeadRecords } from '../types'
const props = defineProps<{ modelValue: LeadRecords; savedReceiptIds: string[] }>()
const emit = defineEmits<{ 'update:modelValue': [LeadRecords] }>()
const records = computed(() => props.modelValue)
const id = () => crypto.randomUUID()
function add(kind: keyof LeadRecords) {
  const next = JSON.parse(JSON.stringify(props.modelValue)) as LeadRecords
  if (kind === 'contacts') next.contacts.push({ id: id(), name: '', phone: '', email: '', title: '', primary: false })
  if (kind === 'contracts') next.contracts.push({ id: id(), number: '', amount: 0, signedAt: '', dueAt: '', status: 'DRAFT' })
  if (kind === 'receipts') next.receipts.push({ id: id(), contractId: next.contracts.find(c => c.status === 'SIGNED')?.id ?? '', amount: 0, receivedAt: '', reference: '' })
  emit('update:modelValue', next)
}
function remove(kind: keyof LeadRecords, index: number) { const next = JSON.parse(JSON.stringify(props.modelValue)) as LeadRecords; next[kind].splice(index, 1); emit('update:modelValue', next) }
function setAmount(kind: 'contracts' | 'receipts', index: number, event: Event) {
  const value = (event.target as HTMLInputElement).value
  const [whole, fraction = ''] = value.split('.')
  records.value[kind][index].amount = /^\d+(?:\.\d{1,2})?$/.test(value) ? Number(whole) * 100 + Number(fraction.padEnd(2, '0')) : NaN
}
const total = computed(() => records.value.receipts.reduce((n, r) => n + r.amount, 0))
function paid(id: string) { return records.value.receipts.filter(r => r.contractId === id).reduce((n, r) => n + r.amount, 0) }
const money = (n: number) => new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' }).format(n / 100)
</script>

<template>
  <section class="records" aria-label="联系人合同回款">
    <div class="heading"><h3>联系人</h3><button type="button" class="text-button" @click="add('contacts')">新增联系人</button></div>
    <p v-if="!records.contacts.length">尚未新增联系人，原线索联系人仍显示在档案顶部。</p>
    <div v-for="(c, i) in records.contacts" :key="c.id" class="record form-grid">
      <label>联系人姓名<input v-model="c.name" required maxlength="100" /></label><label>职务<input v-model="c.title" maxlength="100" /></label>
      <label>联系电话<input v-model="c.phone" maxlength="50" /></label><label>联系人邮箱<input v-model="c.email" type="email" maxlength="200" /></label>
      <label class="inline"><input v-model="c.primary" type="checkbox" />主要联系人</label><button type="button" class="text-button" @click="remove('contacts', i)">移除联系人</button>
    </div>
    <div class="heading"><h3>合同</h3><button type="button" class="text-button" @click="add('contracts')">新增合同</button></div>
    <div v-for="(c, i) in records.contracts" :key="c.id" class="record form-grid">
      <label>合同编号<input v-model="c.number" required maxlength="100" /></label><label>合同状态<select v-model="c.status"><option value="DRAFT">草稿</option><option value="SIGNED">已签订</option><option value="CANCELLED">已取消</option></select></label>
      <label>合同金额（元）<input type="number" min="0.01" max="1000000000" step="0.01" required :value="c.amount / 100" @input="setAmount('contracts', i, $event)" /></label>
      <label>签订日期<input v-model="c.signedAt" type="date" :required="c.status === 'SIGNED'" /></label><label>回款截止日期<input v-model="c.dueAt" type="date" /></label>
      <p>已回款 {{ money(paid(c.id)) }} · 待回款 {{ money(c.amount - paid(c.id)) }}</p><button v-if="!paid(c.id)" type="button" class="text-button" @click="remove('contracts', i)">移除合同</button>
    </div>
    <div class="heading"><h3>回款 · {{ money(total) }}</h3><button type="button" class="text-button" @click="add('receipts')">登记回款</button></div>
    <p>实际到账后登记；保存的回款记录保留，累计金额不得超过合同金额。</p>
    <fieldset v-for="(r, i) in records.receipts" :key="r.id" class="record form-grid" :disabled="savedReceiptIds.includes(r.id)">
      <label>关联合同<select v-model="r.contractId" required><option value="">选择已签订合同</option><option v-for="c in records.contracts.filter(c => c.status === 'SIGNED')" :key="c.id" :value="c.id">{{ c.number }}</option></select></label>
      <label>回款金额（元）<input type="number" min="0.01" max="1000000000" step="0.01" required :value="r.amount / 100" @input="setAmount('receipts', i, $event)" /></label>
      <label>到账日期<input v-model="r.receivedAt" type="date" required /></label><label>银行/支付流水号<input v-model="r.reference" required maxlength="200" /></label>
      <button v-if="!savedReceiptIds.includes(r.id)" type="button" class="text-button" @click="remove('receipts', i)">撤销本次登记</button><small v-else>已保存回款</small>
    </fieldset>
  </section>
</template>

<style scoped>
.records { margin-top: 24px; }.heading { display: flex; justify-content: space-between; align-items: center; margin: 24px 0 12px; }h3 { color: var(--purple-dark); font-size: 16px; }.record { border: 1px solid var(--border); border-radius: 10px; padding: 16px; margin: 12px 0; min-width: 0; }.records p { font-size: 11px; line-height: 1.7; color: var(--muted); }.inline { display: flex; align-items: center; gap: 8px; }.inline input { width: auto; }
</style>
