<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../api'

interface CheckinResult { code: string; checked: boolean; activityId: string; openid: string; checkedAt: string; checkedBy: string; message: string }
const code = ref('')
const result = ref<CheckinResult | null>(null)
const busy = ref(false), error = ref('')
const stats = ref<{ reserved: number; checked: number } | null>(null)
const activityId = ref('')

async function checkin() {
  if (busy.value || !code.value.trim()) return
  busy.value = true; error.value = ''; result.value = null
  try {
    result.value = await api<CheckinResult>('admin.activity.checkin', { code: code.value.trim() })
    code.value = ''
    if (activityId.value) await loadStats()
  } catch (e) { error.value = (e as Error).message }
  finally { busy.value = false; }
}
async function loadStats() {
  try { stats.value = await api('admin.activity.checkinStats', { activityId: activityId.value.trim() || undefined }) }
  catch (e) { error.value = (e as Error).message }
}
</script>

<template>
  <section class="panel">
    <div class="panel-head"><div><h3>活动现场签到</h3><p>输入用户出示的 8 位签到码核销；重复核销会提示已签到</p></div></div>
    <form class="checkin-form" @submit.prevent="checkin">
      <input v-model="code" maxlength="16" placeholder="输入签到码" aria-label="签到码" :disabled="busy" autocomplete="off" autocapitalize="characters" />
      <button class="button primary" :disabled="busy || !code.trim()">{{ busy ? '核销中…' : '核销签到' }}</button>
    </form>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p>
    <div v-if="result" class="result" :class="{ dup: result.message.includes('已核销') }">
      <strong>{{ result.message }}</strong>
      <p>签到码：{{ result.code }} · 活动：{{ result.activityId }}</p>
      <p v-if="result.checkedAt">核销时间：{{ result.checkedAt }}（{{ result.checkedBy }}）</p>
    </div>
    <div class="stats-row">
      <label>活动 ID（可留空看全部）<input v-model="activityId" placeholder="activity-…" /></label>
      <button type="button" class="button secondary" @click="loadStats">查看签到统计</button>
      <p v-if="stats" class="subtle">已预约 {{ stats.reserved }} 人 · 已签到 {{ stats.checked }} 人</p>
    </div>
  </section>
</template>

<style scoped>
.checkin-form { display: flex; gap: 12px; margin: 16px 0; max-width: 480px; }
.checkin-form input { flex: 1; padding: 12px; font-size: 18px; letter-spacing: 2px; text-transform: uppercase; }
.result { margin: 12px 0; padding: 12px 16px; border-radius: 8px; background: #eef7ee; border: 1px solid #bfe0bf; }
.result.dup { background: #fef3e2; border-color: #f0d9a8; }
.result p { margin: 6px 0 0; font-size: 13px; color: var(--muted); }
.stats-row { margin-top: 20px; display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; }
.stats-row label { display: flex; flex-direction: column; gap: 6px; font-size: 13px; }
</style>
