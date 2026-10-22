<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'

const enabled = ref(false)
const remaining = ref(0)
const ready = ref(false)
const busy = ref(false)
const error = ref('')
const currentPassword = ref('')
const password = ref('')
const confirmation = ref('')
const code = ref('')
const secret = ref('')
const expiresAt = ref(0)
async function load() {
  try {
    const status = await api<{ enabled: boolean; recoveryRemaining: number }>('admin.security.status')
    enabled.value = status.enabled; remaining.value = status.recoveryRemaining; ready.value = true
  } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
async function submit(action: string) {
  if (busy.value) return
  error.value = ''
  if (!currentPassword.value) { error.value = '请输入原密码'; return }
  if (action === 'admin.account.changePassword') {
    if ([...password.value].length < 10 || [...password.value].length > 128) { error.value = '新密码需要 10–128 位'; return }
    if (password.value !== confirmation.value) { error.value = '两次输入的密码不一致'; return }
  }
  if ((enabled.value || action === 'admin.security.enable') && !code.value.trim()) { error.value = '请输入动态码或恢复码'; return }
  busy.value = true
  try {
    const result = await api<{ secret?: string; expiresAt?: number; sessionRevoked?: boolean; recoveryCodes?: string[] }>(action, { currentPassword: currentPassword.value, password: password.value, code: code.value.trim() })
    if (result.secret) { secret.value = result.secret; expiresAt.value = result.expiresAt || 0 }
    if (result.sessionRevoked) {
      currentPassword.value = ''; password.value = ''; confirmation.value = ''; code.value = ''; secret.value = ''
      window.dispatchEvent(new CustomEvent('chiji-admin-session-expired', { detail: { message: '账号安全设置已更新，全部旧会话已失效，请重新登录。', recoveryCodes: result.recoveryCodes } }))
    }
  } catch (e) { error.value = (e as Error).message }
  finally { busy.value = false }
}
</script>

<template>
  <section class="panel security-panel">
    <div class="panel-head"><div><h3>个人账号安全</h3><p>修改自己的密码，管理验证器和登录保护。</p></div></div>
    <form class="security-form" @submit.prevent="submit('admin.account.changePassword')">
      <p v-if="!ready" class="subtle">正在加载安全设置… <button type="button" class="text-button" @click="load">重试</button></p>
      <fieldset :disabled="busy || !ready">
        <label>原密码<input v-model="currentPassword" type="password" autocomplete="current-password" maxlength="256" /></label>
        <h4>多因素认证 · {{ enabled ? '已启用' : '未启用' }}</h4>
        <p class="subtle">启用后，登录需要密码和验证器中的 6 位动态码。修改密码和关闭认证也需要第二步验证。</p>
        <p v-if="enabled">剩余恢复码：{{ remaining }} 个。每个恢复码只能使用一次。</p>
        <div v-if="secret" class="setup-box">
          <p>在验证器中手动添加账号：名称填写当前用户名，类型选择「基于时间」，密钥填写：</p>
          <code>{{ secret }}</code>
          <p>请勿分享此密钥。绑定于 {{ new Date(expiresAt * 1000).toLocaleTimeString('zh-CN') }} 过期，输入验证器生成的动态码完成绑定。</p>
        </div>
        <label v-if="enabled || secret">{{ enabled ? '动态码或恢复码' : '验证器动态码' }}<input v-model="code" autocomplete="one-time-code" maxlength="32" placeholder="6 位动态码或一次性恢复码" /></label>
        <div class="row-actions">
          <button v-if="!enabled" class="button secondary" type="button" @click="submit('admin.security.setup')">{{ secret ? '重新生成绑定密钥' : '绑定验证器' }}</button>
          <button v-if="secret" class="button primary" type="button" @click="submit('admin.security.enable')">验证并启用</button>
          <button v-if="secret" class="text-button" type="button" @click="secret = ''; code = ''">取消绑定</button>
          <button v-if="enabled" class="button secondary" type="button" @click="submit('admin.security.disable')">关闭多因素认证</button>
        </div>
        <h4>修改密码</h4>
        <label>新密码<input v-model="password" type="password" autocomplete="new-password" maxlength="256" placeholder="10–128 位，不能与原密码相同" /></label>
        <label>确认新密码<input v-model="confirmation" type="password" autocomplete="new-password" maxlength="256" /></label>
        <p class="subtle">修改密码或启用、关闭认证后，所有设备的旧登录会话都会失效。</p>
        <button class="button primary">{{ busy ? '提交中…' : '修改密码并重新登录' }}</button>
      </fieldset>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
    </form>
  </section>
</template>

<style scoped>
.security-form { padding: 24px; max-width: 680px; }
fieldset { border: 0; padding: 0; margin: 0; min-width: 0; }
label { display: block; font-size: 13px; margin: 18px 0; }
input { display: block; width: 100%; padding: 12px; margin-top: 8px; border: 1px solid var(--border); border-radius: 9px; background: var(--paper); }
h4 { margin-top: 28px; }
p { font-size: 13px; line-height: 1.8; margin: 12px 0; }
.setup-box { background: var(--purple-soft); padding: 16px; border-radius: 10px; }
code { overflow-wrap: anywhere; user-select: all; }
.row-actions { flex-wrap: wrap; }
</style>
