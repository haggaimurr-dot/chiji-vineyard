<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../api'
import type { HomeConfig, Product } from '../types'

const props = defineProps<{ home: HomeConfig; products: Product[]; disabled: boolean; pending: boolean; dirty: boolean }>()
const emit = defineEmits<{ save: []; preview: [] }>()
function add() {
  if (props.disabled || (props.home.activities?.length || 0) >= 12) return
  ;(props.home.activities ||= []).push({ id: `activity-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`, reservationEnabled: false, title: '', summary: '', content: '', startDate: '', endDate: '', productId: '' })
}
const oversell = ref<Array<{ activityId: string; title: string; capacity: number; reserved: number; waitlisted: number; overBy: number }> | null>(null)
const oversellBusy = ref(false), oversellError = ref('')
async function checkOversell() {
  if (oversellBusy.value) return
  oversellBusy.value = true; oversellError.value = ''
  try { const r = await api<{ items: typeof oversell.value }>('admin.activity.oversellCheck'); oversell.value = r.items }
  catch (e) { oversellError.value = (e as Error).message }
  finally { oversellBusy.value = false }
}
function move(index: number, delta: number) {
  const items = props.home.activities || [], target = index + delta
  if (props.disabled || target < 0 || target >= items.length) return
  ;[items[index], items[target]] = [items[target], items[index]]
}
</script>

<template>
  <form id="activity-management" class="panel settings-card activity-settings" @submit.prevent="emit('save')">
    <div class="panel-head"><div><h3>活动管理 <span class="change-state">{{ pending ? '保存值待回读确认' : dirty ? '有未保存修改' : '与已读取保存值一致' }}</span></h3><p>最多12条；活动按设置顺序展示，日期与预约规则使用北京时间。</p></div><button type="button" class="button secondary" :disabled="disabled" @click="emit('preview')">预览活动</button></div>
    <fieldset class="activity-fields" :disabled="disabled">
      <p class="subtle">活动与首页一同保存；本次保存也会提交首页的当前修改。</p>
      <p v-if="!home.activities?.length" class="subtle">暂无活动，新增后填写内容与起止日期。</p>
      <fieldset v-for="(item, index) in home.activities" :key="item.id || index">
        <legend>活动 {{ index + 1 }}{{ item.title ? ` · ${item.title}` : '' }}</legend>
        <div class="form-grid">
          <label>活动标题<input v-model.trim="item.title" maxlength="80" required /></label>
          <label>活动简介<input v-model.trim="item.summary" maxlength="200" required /></label>
          <label>专题封面（可留空）<input v-model.trim="item.cover" type="url" pattern="https://.*" maxlength="2000" placeholder="https://…" /></label>
          <label>活动地点（可留空）<input v-model.trim="item.location" maxlength="200" placeholder="例如：果园体验区" /></label>
          <label class="full">活动详情<textarea v-model.trim="item.content" rows="4" maxlength="2000" required /></label>
          <label>活动开始日期<input v-model="item.startDate" type="date" required /></label>
          <label>活动结束日期<input v-model="item.endDate" type="date" :min="item.startDate" required /></label>
          <label>关联商品（可留空）<select v-model="item.productId"><option value="">不关联商品</option><option v-for="p in products" :key="p.id" :value="p.id">{{ p.name }}</option><option v-if="item.productId && !products.some(p => p.id === item.productId)" :value="item.productId">{{ item.productId }}（已下架，请更换）</option></select></label>
          <label>预约容量（0表示不限额）<input v-model.number="item.capacity" type="number" min="0" max="100000" step="1" placeholder="0" /></label>
          <label><input v-model="item.waitlistEnabled" type="checkbox" />满额后允许候补</label>
          <label><input v-model="item.reservationEnabled" type="checkbox" @change="item.id ||= `activity-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`" />开放活动预约</label>
        </div>
        <div class="activity-actions"><button type="button" class="text-button" :disabled="index === 0" @click="move(index, -1)">上移</button><button type="button" class="text-button" :disabled="index === (home.activities?.length || 0) - 1" @click="move(index, 1)">下移</button><button type="button" class="text-button" @click="home.activities?.splice(index, 1)">删除活动</button></div>
      </fieldset>
      <div class="activity-actions"><button type="button" class="button secondary" :disabled="(home.activities?.length || 0) >= 12" @click="add">新增活动</button><button class="button primary" :disabled="pending">{{ pending ? '请先回读上次保存值' : '保存活动与首页' }}</button><button type="button" class="button secondary" :disabled="oversellBusy" @click="checkOversell">{{ oversellBusy ? '检测中…' : '超售检测' }}</button></div>
<p v-if="oversellError" class="form-error" role="alert">{{ oversellError }}</p>
<div v-if="oversell"><p v-if="!oversell.length" class="subtle">暂无设置容量的活动</p>
<p v-for="row in oversell" :key="row.activityId" :class="{ 'form-error': row.overBy > 0 }">{{ row.title }}：容量 {{ row.capacity }} / 已预约 {{ row.reserved }} / 候补 {{ row.waitlisted }}<strong v-if="row.overBy > 0">　超售 {{ row.overBy }} 人</strong><span v-else>　正常</span></p></div>
    </fieldset>
  </form>
</template>

<style scoped>
.activity-settings{scroll-margin-top:120px}.activity-settings>.panel-head{gap:16px;flex-wrap:wrap}.activity-fields{border:0;margin:0;padding:0 24px;min-width:0}.activity-actions{display:flex;gap:12px;flex-wrap:wrap;margin-top:18px}.change-state{margin-left:8px;color:var(--muted);font-size:10px;font-weight:400}.activity-fields>p{line-height:1.8}
</style>
