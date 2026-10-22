<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, dateTime } from '../api'
import { runtimeTasks, type RuntimeOverview, type RuntimeSettings } from '../runtime-operations'

const state = ref<RuntimeOverview | null>(null), loading = ref(false), busy = ref(false), error = ref(''), message = ref('')
const settings = ref<RuntimeSettings>({ scaleToZero: true, externalScheduler: false, provider: '', endpoint: '', mediaBase: '' })
const task = ref<string>(runtimeTasks[0].key), source = ref('manual'), fail = ref(false), filterTask = ref(''), filterStatus = ref(''), page = ref(0)
const backupId = ref(''), drillFail = ref(false), tab = ref('monitor')
const filtered = computed(() => (state.value?.runs || []).filter(r => (!filterTask.value || r.task === filterTask.value) && (!filterStatus.value || r.status === filterStatus.value)))
const records = computed(() => filtered.value.slice(page.value * 10, page.value * 10 + 10))
const taskName = (key: string) => runtimeTasks.find(t => t.key === key)?.name || '数据库迁移'
let timer: ReturnType<typeof setInterval> | undefined
async function load(initial = false) {
  if (busy.value || loading.value) return
  loading.value = true
  try {
    state.value = await api<RuntimeOverview>('admin.runtime.overview')
    if (initial) { settings.value = { ...state.value.settings }; backupId.value = state.value.backups[0]?.id || '' }
    error.value = ''
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function change(action: string, data: Record<string, unknown> = {}) {
  if (busy.value || loading.value) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    state.value = await api<RuntimeOverview>(`admin.runtime.${action}`, data)
    page.value = 0
    if (action === 'backup') backupId.value = state.value.backups[0]?.id || ''
    message.value = action === 'drill' && state.value.drills[0]?.status === 'FAILED' ? '恢复演练模拟失败，请查看校验记录。' : '模拟操作已完成，结果已保存。'
  } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
onMounted(() => { void load(true); timer = setInterval(() => void load(), 30000) })
onUnmounted(() => clearInterval(timer))
</script>

<template>
  <section id="runtime-operations" class="panel runtime-panel">
    <div class="panel-head"><div><h3>配置、媒体与运行任务</h3><p>数据库迁移、外部调度、执行记录、集中监控与备份恢复</p></div><button class="button secondary" :disabled="busy || loading" @click="load()">{{ loading ? '刷新中…' : '刷新监控' }}</button></div>
    <p class="demo-notice">Mock 演示：操作仅保存浏览器模拟数据，不执行真实数据库迁移、网络验收、定时调度或生产备份。监控每30秒刷新。</p>
    <nav class="runtime-tabs" aria-label="运行模块"><button v-for="item in [{key:'monitor',name:'集中监控'},{key:'migrations',name:'数据库迁移'},{key:'scheduler',name:'外部调度'},{key:'media',name:'媒体域名验收'},{key:'records',name:'任务执行记录'},{key:'backup',name:'备份恢复演练'}]" :key="item.key" :class="{active:tab === item.key}" @click="tab = item.key">{{ item.name }}</button></nav>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p><p v-if="message" role="status">{{ message }}</p>
    <template v-if="state">
      <div v-if="tab === 'monitor'">
        <div class="runtime-metrics"><div><strong>{{ state.migrations.filter(m => m.status === 'APPLIED').length }}/{{ state.migrations.length }}</strong><span>已模拟迁移</span></div><div><strong>{{ state.runs.filter(r => r.status === 'FAILED').length }}</strong><span>保留记录中的失败次数</span></div><div><strong>{{ state.alerts.length }}</strong><span>当前告警</span></div><div><strong>{{ state.drills[0]?.status === 'SUCCESS' ? '通过' : '待处理' }}</strong><span>最近恢复演练</span></div></div>
        <ul v-if="state.alerts.length" class="runtime-alerts"><li v-for="alert in state.alerts" :key="alert.key">{{ alert.message }}</li></ul><p v-else class="success-text">所有演示检查已通过。</p>
        <p>外部调度：{{ state.settings.externalScheduler ? state.settings.provider : '未启用' }} · 媒体验收：{{ state.media?.passed ? '模拟通过' : '待验收' }}</p>
      </div>
      <div v-else-if="tab === 'migrations'">
        <p>按依赖顺序模拟执行；重复执行跳过已应用项目。配置与媒体基础表包含在基础迁移中。</p><button class="button primary" :disabled="busy || loading || state.migrations.every(m => m.status === 'APPLIED')" @click="change('migrate')">模拟执行全部迁移</button>
        <div class="runtime-table"><table><thead><tr><th>模块 / 迁移</th><th>状态</th><th>应用时间</th></tr></thead><tbody><tr v-for="m in state.migrations" :key="m.id"><td>{{ m.name }}<small>{{ m.id }}</small></td><td>{{ m.status === 'APPLIED' ? '模拟已应用' : '待执行' }}</td><td>{{ m.appliedAt ? dateTime(m.appliedAt) : '—' }}</td></tr></tbody></table></div>
      </div>
      <div v-else-if="tab === 'scheduler'">
        <form class="runtime-form" @submit.prevent="change('settings', { ...settings })"><fieldset :disabled="busy || loading"><label class="runtime-checkbox"><input v-model="settings.scaleToZero" type="checkbox"/>允许缩容到零</label><label class="runtime-checkbox"><input v-model="settings.externalScheduler" type="checkbox"/>启用外部调度</label><label>外部调度平台<input v-model.trim="settings.provider" maxlength="80" placeholder="云函数定时触发器"/></label><label>任务 API 地址<input v-model.trim="settings.endpoint" type="url" required placeholder="https://api.example.com/api"/></label><label>媒体域名<input v-model.trim="settings.mediaBase" type="url" required placeholder="https://media.example.com"/></label><p>缩容到零必须启用外部调度；生产调度器需通过服务端密钥配置 X-Job-Secret。此处不接收密钥。</p><button class="button primary">保存模拟配置</button></fieldset></form>
        <div class="runtime-table"><table><thead><tr><th>任务</th><th>建议频率</th><th>调用动作</th></tr></thead><tbody><tr v-for="t in runtimeTasks" :key="t.key"><td>{{ t.name }}</td><td>{{ t.interval === 86400 ? '每日北京时间10:00后，传昨日账单日期' : `每${t.interval}秒` }}</td><td><code>{{ t.action }}</code></td></tr></tbody></table></div>
        <p>财务对账按 REFUND、BASIC、FEES、OPERATION 分别调用；调度模拟需在任务执行记录中手动触发。</p>
      </div>
      <div v-else-if="tab === 'media'">
        <p>待验收域名：{{ state.settings.mediaBase }}。修改域名后旧验收自动失效。</p><div class="runtime-controls"><button class="button primary" :disabled="busy || loading" @click="change('mediaCheck')">模拟验收通过</button><button class="button secondary" :disabled="busy || loading" @click="change('mediaCheck', {fail:true})">模拟视频 Range 失败</button></div>
        <p v-if="!state.media">尚无验收记录</p><template v-else><p>{{ dateTime(state.media.checkedAt) }} · {{ state.media.passed ? '模拟通过' : '模拟失败' }}</p><ul class="runtime-alerts"><li v-for="check in state.media.checks" :key="check.name"><strong>{{ check.passed ? '✓' : '✕' }} {{ check.name }}</strong>：{{ check.detail }}</li></ul></template>
      </div>
      <div v-else-if="tab === 'records'">
        <form class="runtime-controls" @submit.prevent="change('run', {task, source, fail})"><select v-model="task" aria-label="执行任务"><option v-for="t in runtimeTasks" :key="t.key" :value="t.key">{{ t.name }}</option></select><select v-model="source" aria-label="触发来源"><option value="manual">手动模拟</option><option value="external">外部调度模拟</option></select><label class="runtime-checkbox"><input v-model="fail" type="checkbox"/>模拟失败</label><button class="button primary" :disabled="busy || loading">模拟执行</button></form>
        <div class="runtime-controls"><select v-model="filterTask" aria-label="筛选任务" @change="page = 0"><option value="">全部任务</option><option value="migrations">数据库迁移</option><option v-for="t in runtimeTasks" :key="t.key" :value="t.key">{{ t.name }}</option></select><select v-model="filterStatus" aria-label="筛选状态" @change="page = 0"><option value="">全部状态</option><option value="SUCCESS">成功</option><option value="FAILED">失败</option></select></div>
        <p v-if="!filtered.length">暂无匹配执行记录</p><div v-else class="runtime-table"><table><thead><tr><th>任务 / 执行ID</th><th>状态 / 来源</th><th>时间 / 耗时</th><th>结果</th><th>操作</th></tr></thead><tbody><tr v-for="r in records" :key="r.id"><td>{{ taskName(r.task) }}<small>{{ r.id }}</small><small v-if="r.retryOf">重试自 {{ r.retryOf }}</small></td><td>{{ r.status === 'SUCCESS' ? '成功' : '失败' }}<small>{{ r.source }} · {{ r.actor }}</small></td><td>{{ dateTime(r.startedAt) }}<small>结束 {{ dateTime(r.finishedAt) }} · {{ r.durationMs }}ms</small></td><td>{{ r.summary }}</td><td><button v-if="r.status === 'FAILED'" class="text-button" :disabled="busy || loading" @click="change('run', {task:r.task, retryOf:r.id})">模拟重试</button></td></tr></tbody></table></div>
        <div class="runtime-controls"><button class="button secondary" :disabled="page === 0" @click="page--">上一页</button><span>第{{ page + 1 }}页 · {{ filtered.length }}条，最多保留200条</span><button class="button secondary" :disabled="(page + 1) * 10 >= filtered.length" @click="page++">下一页</button></div>
      </div>
      <div v-else-if="tab === 'backup'">
        <p>备份范围为本模块演示配置、迁移状态与媒体验收；演练恢复至隔离副本并核对 SHA-256，不覆盖当前数据。未包含真实订单、媒体文件或其他模块数据。</p>
        <div class="runtime-controls"><button class="button primary" :disabled="busy || loading" @click="change('backup')">创建模拟备份</button><select v-model="backupId" aria-label="恢复备份"><option value="">选择备份</option><option v-for="b in state.backups" :key="b.id" :value="b.id">{{ dateTime(b.createdAt) }} · {{ b.id.slice(0,8) }}</option></select><label class="runtime-checkbox"><input v-model="drillFail" type="checkbox"/>模拟校验失败</label><button class="button secondary" :disabled="busy || loading || !backupId" @click="change('drill', {backupId, fail:drillFail})">执行恢复演练</button></div>
        <p v-if="!state.backups.length">尚未创建模拟备份</p><div v-else class="runtime-table"><table><thead><tr><th>备份ID</th><th>创建时间</th><th>SHA-256</th></tr></thead><tbody><tr v-for="b in state.backups" :key="b.id"><td>{{ b.id }}</td><td>{{ dateTime(b.createdAt) }}</td><td class="hash">{{ b.checksum }}</td></tr></tbody></table></div>
        <h4>恢复演练记录</h4><p v-if="!state.drills.length">暂无演练记录</p><div v-for="d in state.drills" :key="d.id" class="drill-record"><strong>{{ d.status === 'SUCCESS' ? '模拟成功' : '模拟失败' }}</strong> · {{ dateTime(d.startedAt) }} · {{ d.durationMs }}ms<p>{{ d.summary }}</p><small>备份 {{ d.backupId }} · 演练 {{ d.id }}</small></div><p>最多保留20份模拟备份、50条演练记录；清理浏览器数据会删除全部演示记录。</p>
      </div>
    </template>
  </section>
</template>

<style scoped>
.runtime-panel{margin:24px 0;padding:24px;scroll-margin-top:100px;min-width:0}.demo-notice{padding:12px;background:#fff6de;border-radius:8px;color:#76541d}.runtime-tabs,.runtime-controls{display:flex;gap:10px;flex-wrap:wrap;margin:18px 0;align-items:center}.runtime-tabs button{padding:10px 14px;border:1px solid var(--border);border-radius:8px;background:white;cursor:pointer}.runtime-tabs button.active{color:var(--purple);border-color:var(--purple)}.runtime-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px}.runtime-metrics>div{padding:16px;border:1px solid var(--border);border-radius:8px}.runtime-metrics strong,.runtime-metrics span{display:block}.runtime-metrics strong{font-size:24px;margin-bottom:8px}.runtime-alerts{padding-left:24px;line-height:2}.runtime-table{overflow-x:auto;margin:18px 0}.runtime-table table{width:100%;border-collapse:collapse;text-align:left}.runtime-table th,.runtime-table td{padding:12px;border-bottom:1px solid var(--border);vertical-align:top;overflow-wrap:anywhere}.runtime-table small{display:block;color:var(--muted);margin-top:6px}.runtime-form{max-width:700px}.runtime-form fieldset{border:0;padding:0;display:grid;gap:14px}.runtime-form label{display:grid;gap:8px}.runtime-form input,.runtime-controls select{padding:10px;border:1px solid var(--border);border-radius:6px;max-width:100%;min-width:0}.runtime-form label.runtime-checkbox,.runtime-checkbox{display:flex;gap:8px;align-items:center}.runtime-checkbox input{width:auto}.hash{max-width:260px;word-break:break-all}.drill-record{padding:14px 0;border-bottom:1px solid var(--border);overflow-wrap:anywhere}.drill-record p{margin:8px 0}@media(max-width:800px){.runtime-metrics{grid-template-columns:repeat(2,minmax(0,1fr))}.runtime-panel{padding:16px}.runtime-table table{min-width:650px}}
</style>
