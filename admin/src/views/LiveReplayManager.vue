<template>
  <fieldset class="live-config"><legend>直播带货统计</legend>
    <p class="muted">近 {{ statsDays }} 天直播间来源订单（已付款）。归因口径：从直播间挂品进入下单。</p>
    <div class="row"><label>近 <input type="number" v-model.number="statsDays" min="1" max="365" style="width:64px"> 天</label><button class="button small" @click="loadStats">刷新</button></div>
    <table v-if="sourceStats.length">
      <thead><tr><th>机位</th><th>订单数</th><th>金额</th></tr></thead>
      <tbody><tr v-for="it in sourceStats" :key="it.channel"><td>{{ it.channel === 'orchard' ? '果园内部' : '园外摊位' }}</td><td>{{ it.orders }}</td><td>¥{{ (it.amount / 100).toFixed(2) }}</td></tr></tbody>
    </table>
    <p v-else class="muted">暂无直播来源订单</p>
  </fieldset>
  <fieldset class="live-config"><legend>直播回放</legend>
    <p class="muted">按机位维护回放视频，用户在直播页可回看。地址须为 HTTPS 的 .mp4 或 .m3u8。</p>
    <div class="row">
      <label>机位<select v-model="filterChannel"><option value="">全部</option><option value="orchard">果园内部</option><option value="stall">园外摊位</option></select></label>
      <button class="button small" @click="load" :disabled="loading">{{ loading ? '加载中…' : '刷新' }}</button>
      <button class="button small primary" @click="startAdd">新增回放</button>
    </div>
    <table v-if="items.length">
      <thead><tr><th>标题</th><th>机位</th><th>直播日期</th><th>时长</th><th>操作</th></tr></thead>
      <tbody>
        <tr v-for="it in filtered" :key="it.id">
          <td>{{ it.title }}</td>
          <td>{{ it.channel === 'orchard' ? '果园内部' : '园外摊位' }}</td>
          <td>{{ it.playedAt || '—' }}</td>
          <td>{{ fmtDuration(it.durationSec) }}</td>
          <td><button class="button small" @click="startEdit(it)">编辑</button> <button class="button small danger" @click="remove(it)">删除</button></td>
        </tr>
      </tbody>
    </table>
    <p v-else class="muted">暂无回放</p>
    <div v-if="editing" class="edit-box">
      <h4>{{ editing.id ? '编辑回放' : '新增回放' }}</h4>
      <label>机位<select v-model="editing.channel"><option value="orchard">果园内部</option><option value="stall">园外摊位</option></select></label>
      <label>标题<input v-model.trim="editing.title" maxlength="120" placeholder="例如：8月12日 采摘直播回放"/></label>
      <label>回放地址<input v-model.trim="editing.url" type="url" maxlength="2000" placeholder="https://…/replay.mp4"/></label>
      <label>封面地址（可选）<input v-model.trim="editing.coverUrl" type="url" maxlength="2000" placeholder="https://…/cover.jpg"/></label>
      <label>时长（秒）<input v-model.number="editing.durationSec" type="number" min="0" max="86400"/></label>
      <label>直播日期<input v-model.trim="editing.playedAt" maxlength="32" placeholder="例如：2026-10-30"/></label>
      <div class="row">
        <button class="button small primary" @click="save" :disabled="saving">{{ saving ? '保存中…' : '保存' }}</button>
        <button class="button small" @click="editing = null">取消</button>
      </div>
      <p v-if="error" class="error">{{ error }}</p>
    </div>
  </fieldset>
</template>
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { api } from '../api'

interface Replay { id: string; channel: string; title: string; url: string; coverUrl: string; durationSec: number; playedAt: string }
const items = ref<Replay[]>([])
const filterChannel = ref('')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const editing = ref<(Replay & { id: string }) | null>(null)
const statsDays = ref(30)
const sourceStats = ref<Array<{ channel: string; orders: number; amount: number }>>([])
async function loadStats() {
  try { const res = await api<{ items: Array<{ channel: string; orders: number; amount: number }> }>('admin.live.sourceStats', { days: statsDays.value }); sourceStats.value = res.items || [] }
  catch (e: any) { error.value = e.message || '统计加载失败' }
}

const filtered = computed(() => filterChannel.value ? items.value.filter(i => i.channel === filterChannel.value) : items.value)
function fmtDuration(s: number) { if (!s) return '—'; const m = Math.floor(s / 60); return m >= 60 ? `${Math.floor(m / 60)}时${m % 60}分` : `${m}分${s % 60}秒` }

async function load() {
  loading.value = true
  try { items.value = await api<Replay[]>('admin.live.replay.list', {}) || [] }
  catch (e: any) { error.value = e.message || '加载失败' }
  finally { loading.value = false }
}
function startAdd() { error.value = ''; editing.value = { id: '', channel: 'orchard', title: '', url: '', coverUrl: '', durationSec: 0, playedAt: '' } }
function startEdit(it: Replay) { error.value = ''; editing.value = { ...it } }
async function save() {
  if (!editing.value) return
  saving.value = true; error.value = ''
  try {
    await api('admin.live.replay.save', { ...editing.value })
    editing.value = null; await load()
  } catch (e: any) { error.value = e.message || '保存失败' }
  finally { saving.value = false }
}
async function remove(it: Replay) {
  if (!confirm(`删除回放「${it.title}」？`)) return
  try { await api('admin.live.replay.delete', { id: it.id }); await load() }
  catch (e: any) { alert(e.message || '删除失败') }
}
onMounted(() => { load(); loadStats() })
</script>
<style scoped>
.live-config { margin-top: 16px; }
.row { display: flex; gap: 12px; align-items: center; margin: 8px 0; flex-wrap: wrap; }
.muted { color: #888; font-size: 13px; }
.error { color: #c62828; }
table { width: 100%; border-collapse: collapse; margin-top: 8px; }
th, td { border: 1px solid #e5e0d5; padding: 8px 10px; text-align: left; font-size: 14px; }
th { background: #faf8f3; }
.edit-box { border: 1px solid #e5e0d5; border-radius: 8px; padding: 12px; margin-top: 12px; background: #fff; }
.edit-box label { display: block; margin: 8px 0; font-size: 14px; }
.edit-box input, .edit-box select { width: 100%; max-width: 480px; margin-top: 4px; }
.button.danger { color: #c62828; }
</style>
