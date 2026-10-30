<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, isDemo } from '../api'
import { can, currentAdmin } from '../permissions'
import { reminderLabels as labels, type ReminderTask } from '../reminder-tasks'
const data = ref<{ items: ReminderTask[]; counts: Record<string, number>; hasMore: boolean }>({ items: [], counts: {}, hasMore: false })
const kind = ref(''), status = ref(''), offset = ref(0), busy = ref(false), error = ref(''), message = ref('')
const selected = ref<ReminderTask | null>(null), note = ref(''), action = ref('')
type History = { id: string; productName: string; kind: string; skuName: string; status: string; detail: string; createdAt: string }
const historyUser = ref(''), history = ref<History[]>([]), historyOffset = ref(0), historyMore = ref(false)
const at = (s: string) => new Date(s).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })
type KindStat = { kind: string; byStatus: Record<string, number>; total: number; sent: number; clicked: number; clickRate: number }
const stats = ref<KindStat[]>([])
const kindName = (k: string) => k === 'restock' ? '到货提醒' : '降价提醒'
async function loadStats() { try { const r = await api<{ items: KindStat[] }>('admin.reminder.stats', {}); stats.value = r.items } catch { stats.value = [] } }
async function refresh() { data.value = await api('admin.reminder.tasks', { kind: kind.value, status: status.value, offset: offset.value }) }
async function load(page = 0) {
  void loadStats()
  if (busy.value) return
  busy.value = true; error.value = ''; offset.value = page
  try { await refresh() } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
function open(t: ReminderTask) { selected.value = { ...t }; note.value = ''; action.value = t.status === 'FAILED' ? 'retry' : 'stop'; message.value = '' }
async function resolve() {
  if (busy.value || !selected.value) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    await api('admin.reminder.resolve', { ...selected.value, action: action.value, note: note.value })
    selected.value = null; historyUser.value = ''; message.value = '处置已保存；重新排队的任务将在满足商品条件时由定时任务发送。'; await refresh()
  } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
async function showHistory(user: string, page = 0) {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { const result = await api<{ items: History[]; hasMore: boolean }>('admin.reminder.history', { openid: user, offset: page }); history.value = result.items; historyMore.value = result.hasMore; historyUser.value = user; historyOffset.value = page }
  catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
onMounted(() => { load(); loadStats() })
</script>

<template>
  <section class="panel reminder-panel">
    <div class="section-head"><div><h2>通知任务看板</h2><p class="subtle">到货与降价订阅通知 · 时间为北京时间</p></div><button class="button" :disabled="busy" @click="load(offset)">刷新</button></div>
    <p v-if="isDemo" class="demo-note">演示任务仅用于查看与模拟处置，不发送微信消息；刷新页面恢复示例数据。</p>
    <div class="reach-stats"><div v-for="s in stats" :key="s.kind" class="reach-card"><h3>{{ kindName(s.kind) }}</h3><dl><div><dt>订阅总数</dt><dd>{{ s.total }}</dd></div><div><dt>已发送</dt><dd>{{ s.sent }}</dd></div><div><dt>点击回流</dt><dd>{{ s.clicked }}</dd></div><div><dt>点击率</dt><dd>{{ s.clickRate.toFixed(1) }}%</dd></div></dl><p class="subtle">状态分布：{{ Object.entries(s.byStatus).map(([k, v]) => (labels[k] || k) + '×' + v).join('、') || '—' }}</p></div></div>
    <div class="counts"><button v-for="(label, key) in labels" :key="key" :disabled="busy" @click="status = String(key); load()"><span>{{ label }}</span><strong>{{ data.counts[key] || 0 }}</strong></button></div>
    <form class="filters" @submit.prevent="load()"><label>通知类型<select v-model="kind" :disabled="busy"><option value="">全部类型</option><option value="restock">到货提醒</option><option value="pricedrop">降价提醒</option></select></label><label>任务状态<select v-model="status" :disabled="busy"><option value="">全部状态</option><option value="issues">失败处置入口</option><option v-for="(label, key) in labels" :key="key" :value="key">{{ label }}</option></select></label><button class="button primary" :disabled="busy">筛选</button><button type="button" class="button" :disabled="busy" @click="status = 'issues'; load()">查看待处置任务</button></form>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p><p v-if="message" role="status">{{ message }}</p>
    <div class="table-wrap"><table><thead><tr><th>商品 / 用户</th><th>类型</th><th>状态</th><th>失败说明 / 处置备注</th><th>更新时间</th><th>操作</th></tr></thead><tbody><tr v-for="t in data.items" :key="`${t.kind}-${t.openid}-${t.productId}`"><td>{{ t.productName }}<small>{{ t.openid }}</small></td><td>{{ t.kind === 'restock' ? '到货' : '降价' }}</td><td>{{ labels[t.status] || t.status }}</td><td class="detail">{{ t.detail || '—' }}</td><td>{{ at(t.updatedAt) }}</td><td><button class="button" :disabled="busy" @click="showHistory(t.openid)">发送记录</button><button v-if="can(currentAdmin(), 'product.write') && ['FAILED','BLOCKED','UNCERTAIN'].includes(t.status)" class="button" :disabled="busy" @click="open(t)">处置</button></td></tr><tr v-if="!data.items.length"><td colspan="6">{{ busy ? '正在加载…' : '暂无符合条件的通知任务' }}</td></tr></tbody></table></div>
    <div class="filters"><button class="button" :disabled="busy || offset === 0" @click="load(offset - 20)">上一页</button><span>第 {{ offset / 20 + 1 }} 页</span><button class="button" :disabled="busy || !data.hasMore" @click="load(offset + 20)">下一页</button></div>
    <p class="subtle">明确失败沿用定时重试规则；重新排队不会立即发送。授权失效需要用户主动重新订阅。发送中禁止处置，待核实任务不能直接重发。</p>
  </section>
  <section v-if="selected" class="panel reminder-panel"><h2>失败处置 · {{ selected.productName }}</h2><p>{{ labels[selected.status] }} · {{ selected.detail }}</p><form @submit.prevent="resolve"><label>处理方式<select v-model="action" :disabled="busy"><option v-if="selected.status === 'FAILED'" value="retry">重新排队</option><option v-if="selected.status === 'UNCERTAIN'" value="sent">已核实微信发送成功</option><option value="stop">停止此通知任务</option></select></label><p class="subtle">请先核实微信发送结果与失败原因，并记录依据。停止任务保留用户的收藏和关注；不会删除发送历史。</p><label>处置备注<textarea v-model.trim="note" maxlength="240" required :disabled="busy" placeholder="填写核查依据和处理措施"></textarea></label><div class="filters"><button class="button primary" :disabled="busy || !note">确认处置</button><button type="button" class="button" :disabled="busy" @click="selected = null">取消</button></div></form></section>
  <section v-if="historyUser" class="panel reminder-panel"><div class="section-head"><h2>该用户发送记录</h2><button class="button" @click="historyUser = ''">关闭</button></div><p class="subtle">{{ historyUser }} · 包含该用户全部到货及降价提醒记录</p><div v-for="h in history" :key="h.id" class="history"><strong>{{ h.productName }} · {{ h.skuName }} · {{ labels[h.status] }}</strong><p>{{ at(h.createdAt) }} · {{ h.detail || '—' }}</p></div><p v-if="!history.length">暂无发送记录</p><div class="filters"><button class="button" :disabled="busy || historyOffset === 0" @click="showHistory(historyUser, historyOffset - 20)">上一页</button><button class="button" :disabled="busy || !historyMore" @click="showHistory(historyUser, historyOffset + 20)">下一页</button></div></section>
</template>
<style scoped>
.reminder-panel { padding: 24px; margin-bottom: 20px; }
.reach-stats { display: grid; grid-template-columns: repeat(2, 1fr); gap: 12px; margin: 16px 0; }.reach-card { padding: 14px 16px; background: #fbf9f6; border: 1px solid #e9e2d8; border-radius: 12px; }.reach-card h3 { margin: 0 0 8px; font-size: 13px; }.reach-card dl { display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px; margin: 0; }.reach-card dt { font-size: 11px; color: #8a7f72; }.reach-card dd { margin: 2px 0 0; font-size: 18px; font-weight: 700; color: #75425f; }
.counts { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin: 20px 0; }
.counts button { display: grid; gap: 8px; padding: 16px; text-align: left; background: #f7f5fa; border: 1px solid #e7e1ed; border-radius: 12px; }
.counts strong { font-size: 24px; color: #75425f; }
.filters { display: flex; gap: 12px; flex-wrap: wrap; align-items: end; margin: 16px 0; }
label { display: grid; gap: 8px; } select, textarea { padding: 10px; border: 1px solid #ddd; border-radius: 8px; } textarea { min-height: 90px; }
.table-wrap { overflow-x: auto; } table { width: 100%; } th, td { padding: 12px; text-align: left; } td { white-space: nowrap; } small { display: block; color: #777; } .detail { white-space: normal; min-width: 180px; overflow-wrap: anywhere; } .history { border-top: 1px solid #eee; padding: 12px 0; }
@media(max-width: 1100px) { .counts { grid-template-columns: repeat(2, 1fr); } }
</style>
