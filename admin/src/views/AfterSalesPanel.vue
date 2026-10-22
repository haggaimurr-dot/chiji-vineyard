<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { api, dateTime } from '../api'
import ReturnGoods from './ReturnGoods.vue'
const returnEditing = ref(false)
import { currentAdmin, can } from '../permissions'
import { ticketCategories, ticketStatuses, ticketReminder, validateAttachments, type TicketAttachment, type AfterSalesTicket } from '../after-sales'
const props = defineProps<{orderId?: string; orderNo?: string; refundId?: string; orders: Array<{id:string; orderNo:string}>}>()
const tickets = ref<AfterSalesTicket[]>([]), status = ref(''), offset = ref(0), loading = ref(false), busy = ref(false), error = ref(''), message = ref('')
const selected = ref<AfterSalesTicket | null>(null), reply = ref(''), owner = ref(''), nextStatus = ref('')
const creating = ref(false), orderId = ref(''), category = ref('OTHER'), content = ref('')
const attachments = ref<TicketAttachment[]>([]), deadline = ref('')
let request = 0
let timer: ReturnType<typeof setInterval> | undefined
async function load(keepSelection = false) {
 const version = ++request; loading.value = true; error.value = ''
 try { const result = await api<AfterSalesTicket[]>('admin.ticket.list', {orderId: props.orderId || '', status: status.value, offset: offset.value}); if (version === request) { tickets.value = result; selected.value = keepSelection === true ? result.find(t => t.id === selected.value?.id) || null : null; if (keepSelection === true && selected.value) { owner.value=selected.value.owner || ''; nextStatus.value=selected.value.status } } }
 catch(e) { if (version === request) error.value = (e as Error).message }
 finally { if (version === request) loading.value = false }
}
async function select(ticket: AfterSalesTicket) { busy.value = true; selected.value = ticket; reply.value = ''; owner.value = ticket.owner || ''; nextStatus.value = ticket.status; error.value = ''; message.value = ''; attachments.value = []; deadline.value = '';
 try { const result = await api<AfterSalesTicket>('admin.ticket.read', {id:ticket.id, version:ticket.version, readThrough:ticket.messages.length}); if (selected.value?.id === result.id) selected.value = result; tickets.value = tickets.value.map(t => t.id === result.id ? result : t) } catch(e) { error.value = (e as Error).message } finally { busy.value=false }
 }
function startCreate() { selected.value = null; creating.value = true; orderId.value = props.orderId || ''; content.value = ''; attachments.value = []; error.value = '' }
async function create() {
 if (busy.value) return; busy.value = true; error.value = ''
 try { await api('admin.ticket.create', {orderId: orderId.value.trim(), refundId: props.refundId || '', category: category.value, content: content.value.trim(), attachments:attachments.value}); creating.value = false; message.value = '工单已创建'; offset.value = 0; await load() }
 catch(e) { error.value = (e as Error).message } finally { busy.value = false }
}
async function save() {
 if (busy.value || !selected.value) return; busy.value = true; error.value = ''
 try { const result = await api<AfterSalesTicket>('admin.ticket.update', {id: selected.value.id, version: selected.value.version, owner: owner.value.trim(), status: nextStatus.value, content: reply.value.trim(), attachments:attachments.value, ...(deadline.value ? {dueAt:new Date(deadline.value).toISOString()} : {})}); tickets.value = tickets.value.map(t => t.id === result.id ? result : t); await select(result); message.value = '工单已更新' }
 catch(e) { error.value = (e as Error).message } finally { busy.value = false }
}
function page(step: number) { offset.value = Math.max(0, offset.value + step * 50); load() }
watch(() => props.orderId, () => { offset.value = 0; load() })
async function attach(event: Event) {
 if(busy.value) return; busy.value=true
 const input = event.target as HTMLInputElement, files = Array.from(input.files || [])
 try {
  if (attachments.value.length + files.length > 3 || files.some(f => !['image/jpeg','image/png'].includes(f.type)) || files.reduce((n,f) => n+f.size,0) > 512*1024) throw new Error('最多3张JPEG/PNG，合计512KB')
  const added = await Promise.all(files.map(file => new Promise<TicketAttachment>((resolve,reject) => { const reader=new FileReader(); reader.onload=()=>resolve({name:file.name,dataUrl:String(reader.result)}); reader.onerror=()=>reject(new Error('附件读取失败')); reader.readAsDataURL(file) })))
  attachments.value = validateAttachments([...attachments.value,...added])
 } catch(e) { error.value=(e as Error).message } finally { input.value=''; busy.value=false }
}
onMounted(() => { load(); timer=setInterval(() => { if (!returnEditing.value && !busy.value && !loading.value && !creating.value && !reply.value && !attachments.value.length && !deadline.value && (!selected.value || (owner.value === (selected.value.owner || '') && nextStatus.value === selected.value.status))) load(true) },30000) })
onUnmounted(() => { if(timer) clearInterval(timer); request++ })
</script>
<template>
<section class="panel after-sales-panel" v-if="can(currentAdmin(), 'refund.read')">
 <div class="panel-head"><div><h3>售后工单</h3><p>默认48小时处理；未读回复与超时提醒每30秒更新</p></div><div class="row-actions"><button class="button small secondary" :disabled="loading || busy" @click="load()">刷新工单</button><button v-permission="'refund.approve'" class="button small primary" :disabled="busy" @click="startCreate">新建工单</button></div></div>
 <div class="ticket-filters"><label>工单状态 <select v-model="status" :disabled="loading || busy" @change="offset = 0; load()"><option value="">全部状态</option><option v-for="(label,key) in ticketStatuses" :key="key" :value="key">{{label}}</option></select></label><span v-if="message" role="status">{{message}}</span></div>
 <p v-if="error" class="alert error" role="alert">{{error}}</p><p v-if="loading">正在加载工单…</p><p v-else-if="!tickets.length">暂无售后工单</p>
 <form v-if="creating" class="ticket-form" @submit.prevent="create"><h4>新建售后工单</h4><label>订单<select aria-label="订单" v-model="orderId" required :disabled="busy || !!props.orderId"><option value="">请选择当前页订单</option><option v-if="props.orderId" :value="props.orderId">{{props.orderNo || props.orders.find(o => o.id === props.orderId)?.orderNo || '已选订单'}}</option><template v-else><option v-for="order in props.orders" :key="order.id" :value="order.id">{{order.orderNo}}</option></template></select></label><label>问题类型<select v-model="category" :disabled="busy"><option v-for="(label,key) in ticketCategories" :key="key" :value="key">{{label}}</option></select></label><label>工单附件（JPEG/PNG，最多3张，合计512KB）<input type="file" accept="image/jpeg,image/png" multiple :disabled="busy" @change="attach"/></label><div v-for="(file,index) in attachments" :key="index">{{file.name}} <button type="button" :disabled="busy" @click="attachments.splice(index,1)">移除</button></div><label>问题描述<textarea v-model="content" required maxlength="1000" :disabled="busy" /></label><div class="row-actions"><button class="button small primary" :disabled="busy">创建工单</button><button type="button" class="button small secondary" :disabled="busy" @click="creating = false">取消</button></div></form>
 <div class="ticket-list" v-if="!loading"><button v-for="ticket in tickets" :key="ticket.id" class="ticket-item" :class="{chosen: selected?.id === ticket.id}" :disabled="busy" @click="select(ticket)"><strong>{{ticket.orderNo}} · {{ticketCategories[ticket.category]}}</strong><span>{{ticketStatuses[ticket.status]}} · {{ticket.owner || '未分配'}}</span><small>{{ticket.id}} · {{dateTime(ticket.updatedAt)}}</small><small>{{ticket.messages[0]?.content}}</small><strong role="status">{{ticketReminder(ticket, 'ADMIN').text}}</strong><small>处理期限：{{dateTime(ticketReminder(ticket, 'ADMIN').dueAt)}}</small></button></div>
 <div class="row-actions"><button class="button small secondary" :disabled="!offset || loading || busy" @click="page(-1)">上一页</button><span>第 {{offset / 50 + 1}} 页</span><button class="button small secondary" :disabled="tickets.length < 50 || loading || busy" @click="page(1)">下一页</button></div>
 <section v-if="selected" class="ticket-detail"><h4>{{selected.orderNo}} · {{ticketStatuses[selected.status]}}</h4><p v-if="selected.refundId">关联退款：{{selected.refundId}}</p><div class="ticket-message" v-for="(entry,index) in selected.messages" :key="index"><small>{{entry.role === 'USER' ? '用户' : '商家'}} · {{dateTime(entry.createdAt)}}</small><p>{{entry.content}}</p><a v-for="(file,i) in entry.attachments" :key="i" :href="file.dataUrl" :download="file.name" class="evidence"><img :src="file.dataUrl" :alt="file.name"/><span>{{file.name}} · 下载附件</span></a></div>
 <ReturnGoods :ticket="selected" :disabled="busy" @updated="select" @editing="returnEditing = $event"/>
 <form v-if="selected.status !== 'CLOSED' && can(currentAdmin(), 'refund.approve')" class="ticket-form" @submit.prevent="save"><label>调整处理期限（未来30天内；留空沿用）<input type="datetime-local" v-model="deadline" :disabled="busy"/></label><label>工单附件（JPEG/PNG，最多3张，合计512KB）<input type="file" accept="image/jpeg,image/png" multiple :disabled="busy" @change="attach"/></label><div v-for="(file,index) in attachments" :key="index">{{file.name}} <button type="button" :disabled="busy" @click="attachments.splice(index,1)">移除</button></div><label>负责人（管理员用户名）<input v-model="owner" maxlength="40" :disabled="busy" placeholder="留空表示未分配"/></label><label>处理状态<select v-model="nextStatus" :disabled="busy"><option :value="selected.status">{{ticketStatuses[selected.status]}}</option><option v-if="selected.status === 'OPEN'" value="PROCESSING">处理中</option><option v-if="['OPEN','PROCESSING'].includes(selected.status)" value="RESOLVED">已解决</option><option v-if="selected.status === 'RESOLVED'" value="CLOSED">已关闭</option></select></label><label>回复 / 处理结果（用户可见）<textarea v-model="reply" maxlength="1000" :disabled="busy" :required="nextStatus === 'RESOLVED' || nextStatus === 'CLOSED'" /></label><button class="button small primary" :disabled="busy">{{busy ? '正在保存…' : '保存并回复'}}</button></form></section>
</section>
</template>
<style scoped>
.evidence { display:inline-flex; flex-direction:column; gap:6px; margin:8px; max-width:180px; overflow-wrap:anywhere; }
.evidence img { width:160px; height:120px; object-fit:contain; }
.after-sales-panel { margin-top:24px; padding-bottom:20px; }
.ticket-filters,.ticket-list,.ticket-detail,.ticket-form { margin:16px 24px; }
.ticket-filters { display:flex; flex-wrap:wrap; gap:16px; align-items:center; }
.ticket-list { display:grid; gap:10px; }
.ticket-item { display:grid; gap:6px; padding:14px; border:1px solid var(--border); border-radius:10px; text-align:left; background:var(--paper); overflow-wrap:anywhere; }
.ticket-item.chosen { border-color:var(--purple); }
.ticket-item small { color:var(--muted); }
.ticket-form { display:grid; gap:14px; padding:16px; border:1px solid var(--border); border-radius:10px; }
.ticket-form label { display:grid; gap:8px; }
input,select,textarea { padding:10px; border:1px solid var(--border); border-radius:8px; background:var(--paper); max-width:100%; }
textarea { min-height:100px; resize:vertical; }
.ticket-message { padding:12px; border-bottom:1px solid var(--border); }
.ticket-message p { white-space:pre-wrap; overflow-wrap:anywhere; }
</style>
