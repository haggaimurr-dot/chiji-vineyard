<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { api, dateTime } from '../api'
import ReturnGoods from './ReturnGoods.vue'
const returnEditing = ref(false)
import { currentAdmin, can } from '../permissions'
import { confirmAction } from '../confirm'
import { ticketCategories, ticketStatuses, ticketReminder, validateAttachments, type TicketAttachment, type AfterSalesTicket } from '../after-sales'
const props = defineProps<{orderId?: string; orderNo?: string; refundId?: string; orders: Array<{id:string; orderNo:string}>}>()
const tickets = ref<AfterSalesTicket[]>([]), status = ref(''), offset = ref(0), loading = ref(false), busy = ref(false), error = ref(''), message = ref('')
const selected = ref<AfterSalesTicket | null>(null), reply = ref(''), owner = ref(''), nextStatus = ref('')
const creating = ref(false), orderId = ref(''), category = ref('OTHER'), content = ref('')
const attachments = ref<TicketAttachment[]>([]), deadline = ref('')
type Faq = { id: string; title: string; content: string; sort: number; active: boolean }
const faqs = ref<Faq[]>([]), faqManaging = ref(false), faqForm = ref<{id: string; title: string; content: string; sort: number}>({id: '', title: '', content: '', sort: 0}), faqBusy = ref(false), faqMsg = ref('')
const canFaqManage = () => can(currentAdmin(), 'refund.approve')
async function loadFaqs() { try { const r = await api<{items: Faq[]}>('admin.faq.list', {}); faqs.value = r.items } catch { faqs.value = [] } }
function insertFaq(f: Faq) { reply.value = (reply.value ? reply.value + '\n' : '') + f.content }
async function loadFaqManage() { faqManaging.value = true; faqMsg.value = ''; try { const r = await api<{items: Faq[]}>('admin.faq.manage', {}); faqs.value = r.items } catch (e) { faqMsg.value = (e as Error).message } }
function editFaq(f?: Faq) { faqForm.value = f ? {id: f.id, title: f.title, content: f.content, sort: f.sort} : {id: '', title: '', content: '', sort: 0}; faqMsg.value = '' }
async function saveFaq() {
  if (faqBusy.value || !faqForm.value.title.trim() || !faqForm.value.content.trim()) return
  faqBusy.value = true; faqMsg.value = ''
  try { await api('admin.faq.save', faqForm.value); await loadFaqManage(); editFaq(); faqMsg.value = '已保存' }
  catch (e) { faqMsg.value = (e as Error).message }
  finally { faqBusy.value = false }
}
async function deleteFaq(id: string) {
  if (faqBusy.value || !await confirmAction({ title: '删除快捷回复', message: '确认删除这条快捷回复？', danger: true })) return
  faqBusy.value = true
  try { await api('admin.faq.delete', {id}); await loadFaqManage() }
  catch (e) { faqMsg.value = (e as Error).message }
  finally { faqBusy.value = false }
}
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
onMounted(() => { load(); loadFaqs(); timer=setInterval(() => { if (!returnEditing.value && !busy.value && !loading.value && !creating.value && !reply.value && !attachments.value.length && !deadline.value && (!selected.value || (owner.value === (selected.value.owner || '') && nextStatus.value === selected.value.status))) load(true) },30000) })
onUnmounted(() => { if(timer) clearInterval(timer); request++ })
// SLA 时效统计
const slaStart = ref(''), slaEnd = ref('')
const slaReport = ref<{ config: { firstResponseHours: number; resolveHours: number }; total: number; responded: number; resolved: number; avgFirstResponseHours?: number; avgResolveHours?: number; firstResponseOverdue: number; resolveOverdue: number; rows: Array<{ id: string; orderNo: string; status: string; createdAt: string; firstResponseAt?: string; firstResponseHours?: number; resolvedAt?: string; resolveHours?: number; firstResponseDue: boolean; resolveDue: boolean }> } | null>(null)
const slaBusy = ref(false), slaError = ref('')
const slaCfg = ref({ firstResponseHours: 4, resolveHours: 48 })
async function loadSLA() {
 if (!slaStart.value || !slaEnd.value || slaBusy.value) return
 slaBusy.value = true; slaError.value = ''
 try { const r = await api<typeof slaReport.value>('admin.ticket.sla', { startDate: slaStart.value, endDate: slaEnd.value }); slaReport.value = r; if (r) slaCfg.value = { ...r.config } }
 catch (e) { slaError.value = (e as Error).message }
 finally { slaBusy.value = false }
}
async function saveSLA() {
 if (slaBusy.value) return
 slaBusy.value = true; slaError.value = ''
 try { const r = await api<{ firstResponseHours: number; resolveHours: number }>('admin.ticket.sla.save', slaCfg.value); if (slaReport.value) slaReport.value.config = r; message.value = 'SLA 标准已更新' }
 catch (e) { slaError.value = (e as Error).message }
 finally { slaBusy.value = false }
}
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
 <form v-if="selected.status !== 'CLOSED' && can(currentAdmin(), 'refund.approve')" class="ticket-form" @submit.prevent="save"><label>调整处理期限（未来30天内；留空沿用）<input type="datetime-local" v-model="deadline" :disabled="busy"/></label><label>工单附件（JPEG/PNG，最多3张，合计512KB）<input type="file" accept="image/jpeg,image/png" multiple :disabled="busy" @change="attach"/></label><div v-for="(file,index) in attachments" :key="index">{{file.name}} <button type="button" :disabled="busy" @click="attachments.splice(index,1)">移除</button></div><label>负责人（管理员用户名）<input v-model="owner" maxlength="40" :disabled="busy" placeholder="留空表示未分配"/></label><label>处理状态<select v-model="nextStatus" :disabled="busy"><option :value="selected.status">{{ticketStatuses[selected.status]}}</option><option v-if="selected.status === 'OPEN'" value="PROCESSING">处理中</option><option v-if="['OPEN','PROCESSING'].includes(selected.status)" value="RESOLVED">已解决</option><option v-if="selected.status === 'RESOLVED'" value="CLOSED">已关闭</option></select></label><div class="faq-quick" v-if="faqs.length"><label>快捷回复短语<select :disabled="busy" @change="(e) => { const f = faqs.find(x => x.id === (e.target as HTMLSelectElement).value); if (f) insertFaq(f); (e.target as HTMLSelectElement).value = '' }"><option value="">选择快捷短语插入…</option><option v-for="f in faqs" :key="f.id" :value="f.id">{{ f.title }}</option></select></label><button type="button" class="button small secondary" v-if="canFaqManage()" @click="loadFaqManage()">{{ faqManaging ? "收起维护" : "维护快捷回复" }}</button></div>
<label>回复 / 处理结果（用户可见）<textarea v-model="reply" maxlength="1000" :disabled="busy" :required="nextStatus === 'RESOLVED' || nextStatus === 'CLOSED'" /></label><button class="button small primary" :disabled="busy">{{busy ? '正在保存…' : '保存并回复'}}</button></form></section>
 <section class="sla-section">
  <h4>SLA 时效统计</h4>
  <div v-if="faqManaging && canFaqManage()" class="faq-manage">
  <h4>维护快捷回复</h4>
  <div class="faq-list"><div v-for="f in faqs" :key="f.id" class="faq-item"><strong>{{ f.title }}</strong><span class="subtle">排序 {{ f.sort }}</span><p>{{ f.content }}</p><div class="row-actions"><button type="button" class="button small secondary" @click="editFaq(f)">编辑</button><button type="button" class="button small" @click="deleteFaq(f.id)">删除</button></div></div><p v-if="!faqs.length" class="subtle">暂无快捷回复</p></div>
  <form class="ticket-form" @submit.prevent="saveFaq"><h4>{{ faqForm.id ? "编辑" : "新增" }}快捷回复</h4><label>标题（≤60 字）<input v-model.trim="faqForm.title" maxlength="60" required /></label><label>内容<textarea v-model.trim="faqForm.content" maxlength="2000" required /></label><label>排序<input v-model.number="faqForm.sort" type="number" min="0" max="9999" /></label><div class="row-actions"><button class="button small primary" :disabled="faqBusy">{{ faqBusy ? "保存中…" : "保存" }}</button><button type="button" class="button small secondary" @click="editFaq()">清空</button></div><p v-if="faqMsg" role="status">{{ faqMsg }}</p></form>
</div>
<p class="sla-note">首次响应：工单创建→商家首次回复；解决：创建→已解决/已关闭。阈值可在下方调整（仅超级管理员）。</p>
  <div class="ticket-filters"><label>开始日期 <input type="date" v-model="slaStart" :disabled="slaBusy" /></label><label>结束日期 <input type="date" v-model="slaEnd" :disabled="slaBusy" /></label><button class="button small primary" :disabled="slaBusy || !slaStart || !slaEnd" @click="loadSLA">统计</button></div>
  <p v-if="slaError" class="alert error" role="alert">{{ slaError }}</p>
  <div v-if="slaReport">
   <p>工单总数 <strong>{{ slaReport.total }}</strong> · 已响应 <strong>{{ slaReport.responded }}</strong> · 已解决 <strong>{{ slaReport.resolved }}</strong> · 平均首次响应 <strong>{{ slaReport.avgFirstResponseHours ?? "—" }} 小时</strong> · 平均解决 <strong>{{ slaReport.avgResolveHours ?? "—" }} 小时</strong> · 响应超时 <strong>{{ slaReport.firstResponseOverdue }}</strong> · 解决超时 <strong>{{ slaReport.resolveOverdue }}</strong></p>
   <div class="ticket-filters" v-permission="'super'"><label>首次响应阈值（小时）<input type="number" v-model.number="slaCfg.firstResponseHours" min="0.5" max="720" step="0.5" :disabled="slaBusy" /></label><label>解决阈值（小时）<input type="number" v-model.number="slaCfg.resolveHours" min="0.5" max="720" step="0.5" :disabled="slaBusy" /></label><button class="button small secondary" :disabled="slaBusy" @click="saveSLA">保存阈值</button></div>
   <div class="ticket-list"><div v-for="row in slaReport.rows" :key="row.id" class="ticket-item"><strong>{{ row.orderNo }} · {{ ticketStatuses[row.status] || row.status }}</strong><span>创建 {{ dateTime(row.createdAt) }} · 首次响应 {{ row.firstResponseHours ?? "—" }} 小时 · 解决 {{ row.resolveHours ?? "—" }} 小时</span><strong v-if="row.firstResponseDue || row.resolveDue" role="status" class="sla-overdue">{{ [row.firstResponseDue ? "响应超时" : "", row.resolveDue ? "解决超时" : ""].filter(Boolean).join(" / ") }}</strong></div></div>
  </div>
 </section>
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
.faq-quick { display:flex; align-items:end; gap:10px; margin-bottom:8px; } .faq-quick label { display:grid; gap:6px; font-size:12px; } .faq-manage { margin-top:16px; padding-top:16px; border-top:1px solid var(--border); } .faq-item { padding:10px 0; border-bottom:1px dashed var(--border); } .faq-item p { margin:6px 0; white-space:pre-wrap; }
.ticket-message { padding:12px; border-bottom:1px solid var(--border); }
.ticket-message p { white-space:pre-wrap; overflow-wrap:anywhere; }
.sla-section { margin:16px 24px; padding:16px; border:1px solid var(--border); border-radius:10px; }
.sla-note { color:var(--muted); font-size:12px; }
.sla-overdue { color:var(--danger,#c00); }
</style>
