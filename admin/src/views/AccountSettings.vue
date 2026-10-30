<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { api } from '../api'
import { permissionOptions, defaultPermissions } from '../permissions'

type Account = { id: string; username: string; role: 'SUPER_ADMIN' | 'OPERATOR'; active: boolean; permissions?: string[] }
type AccountResult = Account & { currentSessionRevoked?: boolean }
type ResetResult = { id: string; sessionRevoked: boolean; currentSessionRevoked: boolean }
type Mode = 'create' | 'reset' | 'toggle' | 'permissions'

const accounts = ref<Account[]>([])
const ready = ref(false)
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const message = ref('')
const formError = ref('')
const dialog = ref<HTMLDialogElement | null>(null)
const firstInput = ref<HTMLInputElement | null>(null)
const mode = ref<Mode>('create')
const target = ref<Account | null>(null)
const username = ref('')
const role = ref<Account['role']>('OPERATOR')
const active = ref(true)
const permissions = ref<string[]>(defaultPermissions())
const password = ref('')
const confirmation = ref('')
const currentUser = (() => {
  try {
    const stored = JSON.parse(localStorage.getItem('chiji_admin_user') || '{}')
    return stored && typeof stored === 'object' ? stored as { id?: string; username?: string } : {}
  }
  catch { return {} as { id?: string; username?: string } }
})()
const activeSuperAdmins = computed(() => accounts.value.filter(a => a.active && a.role === 'SUPER_ADMIN').length)
const title = computed(() => mode.value === 'permissions' ? '设置账号权限' : mode.value === 'create' ? '添加后台账号' : mode.value === 'reset' ? '重置账号密码' : target.value?.active ? '停用后台账号' : '启用后台账号')
const submitLabel = computed(() => mode.value === 'permissions' ? '保存权限' : mode.value === 'create' ? '创建账号' : mode.value === 'reset' ? '确认重置密码' : target.value?.active ? '确认停用' : '确认启用')

function isCurrent(account: Account) { return currentUser.id ? account.id === currentUser.id : account.username === currentUser.username }
function disabledReason(account: Account) {
  if (!account.active) return ''
  if (isCurrent(account)) return '不能停用当前登录账号'
  if (account.role === 'SUPER_ADMIN' && activeSuperAdmins.value === 1) return '必须保留至少一个启用的超级管理员'
  return ''
}

async function load() {
  loading.value = true
  error.value = ''
  try { accounts.value = await api<Account[]>('admin.account.list'); ready.value = true }
  catch (e) { error.value = (e as Error).message }
  finally { loading.value = false }
}
onMounted(load)

async function openEditor(nextMode: Mode, account: Account | null = null) {
  if (busy.value || loading.value) return
  mode.value = nextMode
  target.value = account ? { ...account } : null
  username.value = ''
  role.value = 'OPERATOR'
  active.value = true
  permissions.value = account?.permissions ? [...account.permissions] : defaultPermissions()
  password.value = ''
  confirmation.value = ''
  formError.value = ''
  message.value = ''
  await nextTick()
  dialog.value?.showModal()
  firstInput.value?.focus()
}
function closeEditor() {
  if (busy.value) return
  dialog.value?.close()
  password.value = ''
  confirmation.value = ''
  formError.value = ''
  target.value = null
}
function expiredSession() {
  window.dispatchEvent(new CustomEvent('chiji-admin-session-expired', { detail: { message: '密码已重置，请使用新密码重新登录。' } }))
}

type LoginLog = { id: string; adminId: string; username: string; userAgent: string; ip: string; createdAt: string; expiresAt: number; revoked: boolean }
type LoginLogResult = { items: LoginLog[]; total: number; limit: number; offset: number }

const subTab = ref<'accounts' | 'logs'>('accounts')
const isSuper = computed(() => {
  try { return (JSON.parse(localStorage.getItem('chiji_admin_user') || '{}') as { role?: string }).role === 'SUPER_ADMIN' }
  catch { return false }
})
const logs = ref<LoginLog[]>([])
const logsTotal = ref(0)
const logsLoading = ref(false)
const logsError = ref('')
const logFilter = ref('')
const logPage = ref(1)
const logPageSize = 20

async function loadLogs() {
  logsLoading.value = true
  logsError.value = ''
  try {
    const result = await api<LoginLogResult>('admin.login.logs', { adminId: logFilter.value || undefined, limit: logPageSize, offset: (logPage.value - 1) * logPageSize })
    logs.value = result.items
    logsTotal.value = result.total
  } catch (e) { logsError.value = (e as Error).message }
  finally { logsLoading.value = false }
}
function switchTab(next: 'accounts' | 'logs') {
  subTab.value = next
  if (next === 'logs' && !logs.value.length && !logsLoading.value) loadLogs()
}

async function submit() {
  if (busy.value) return
  formError.value = ''
  if (mode.value === 'create' && !username.value.trim()) { formError.value = '请输入管理员用户名'; return }
  if (mode.value === 'create' || mode.value === 'reset') {
    if (Array.from(password.value).length < 10) { formError.value = '密码至少需要 10 位'; return }
    if (Array.from(password.value).length > 128) { formError.value = '密码最多 128 位'; return }
    if (password.value !== confirmation.value) { formError.value = '两次输入的密码不一致'; return }
  }
  busy.value = true
  error.value = ''
  try {
    if (mode.value === 'permissions' && target.value) {
      await api('admin.account.permissions', { accountId: target.value.id, permissions: permissions.value })
      const account = accounts.value.find(a => a.id === target.value!.id)
      if (account) account.permissions = [...permissions.value]
      message.value = `${target.value.username} 的权限已更新，已有登录会话立即按新权限校验。`
      busy.value = false
      closeEditor()
      return
    }
    if (mode.value === 'reset' && target.value) {
      const result = await api<ResetResult>('admin.account.resetPassword', { accountId: target.value.id, password: password.value })
      message.value = `${target.value.username} 的密码已重置，全部旧登录会话已失效。${target.value.active ? '请使用新密码重新登录。' : '账号仍保持停用状态。'}`
      busy.value = false
      closeEditor()
      if (result.currentSessionRevoked) expiredSession()
      return
    }
    const account = mode.value === 'create'
      ? { username: username.value.trim(), password: password.value, role: role.value, active: active.value, permissions: role.value === 'OPERATOR' ? permissions.value : undefined }
      : { ...target.value!, active: !target.value!.active }
    const result = await api<AccountResult>('admin.account.save', { account })
    const safeAccount: Account = { id: result.id, username: result.username, role: result.role, active: result.active, permissions: result.permissions }
    const index = accounts.value.findIndex(a => a.id === result.id)
    if (index < 0) accounts.value.push(safeAccount)
    else accounts.value[index] = safeAccount
    message.value = mode.value === 'create' ? `账号 ${result.username} 已创建。` : `账号 ${result.username} 已${result.active ? '启用' : '停用'}。`
    loading.value = true
    busy.value = false
    closeEditor()
    // Read back the persisted list; a failed refresh must not hide a completed save.
    try { accounts.value = await api<Account[]>('admin.account.list') }
    catch (e) { error.value = `操作已完成，列表刷新失败：${(e as Error).message}` }
    finally { loading.value = false }
  } catch (e) { formError.value = (e as Error).message }
  finally { busy.value = false }
}
</script>

<template>
  <section class="panel account-panel" aria-labelledby="accounts-title">
    <div class="panel-head">
      <div><h3 id="accounts-title">后台账号</h3><p>管理超级管理员和运营人员，按操作分配运营权限，重置密码后全部旧会话失效</p></div>
      <button class="button small secondary" type="button" :disabled="!ready || busy || loading" @click="openEditor('create')">＋ 添加账号</button>
    </div>
    <div class="sub-tabs" role="tablist" aria-label="账号安全子功能">
      <button type="button" role="tab" :aria-selected="subTab === 'accounts'" :class="{ active: subTab === 'accounts' }" @click="switchTab('accounts')">账号管理</button>
      <button type="button" role="tab" :aria-selected="subTab === 'logs'" :class="{ active: subTab === 'logs' }" @click="switchTab('logs')">登录记录</button>
    </div>
    <template v-if="subTab === 'accounts'">
    <p v-if="loading" class="account-note" role="status">正在加载账号…</p>
    <div v-if="error" class="account-feedback"><p class="form-error" role="alert">{{ error }}</p><button class="text-button" type="button" :disabled="busy || loading" @click="load">重新加载</button></div>
    <p v-if="message" class="success-text account-note" role="status">{{ message }}</p>
    <p v-if="ready && !accounts.length" class="account-note">暂无后台账号</p>
    <div v-for="account in accounts" :key="account.id" class="account-row">
      <div class="account-identity"><strong>{{ account.username }} <span v-if="isCurrent(account)" class="current-tag">当前账号</span></strong><small>{{ account.role === 'SUPER_ADMIN' ? '超级管理员' : '运营人员' }}</small></div>
      <span class="status" :class="{ inactive: !account.active }">{{ account.active ? '启用中' : '已停用' }}</span>
      <div class="account-actions">
        <button v-if="account.role === 'OPERATOR'" class="text-button" type="button" :disabled="busy || loading" :aria-label="`设置 ${account.username} 的权限`" @click="openEditor('permissions', account)">设置权限</button>
        <button class="text-button" type="button" :disabled="busy || loading" :aria-label="`重置 ${account.username} 的密码`" @click="openEditor('reset', account)">重置密码</button>
        <button class="text-button" type="button" :disabled="busy || loading || !!disabledReason(account)" :title="disabledReason(account)" :aria-label="`${account.active ? '停用' : '启用'} ${account.username}`" @click="openEditor('toggle', account)">{{ account.active ? '停用' : '启用' }}</button>
      </div>
    </div>
    <p class="account-note subtle">当前账号不可停用；至少保留一个启用的超级管理员。</p>
    </template>
    <template v-if="subTab === 'logs'">
      <div class="log-filters">
        <label v-if="isSuper">账号筛选<select v-model="logFilter" @change="logPage = 1; loadLogs()"><option value="">全部账号</option><option v-for="a in accounts" :key="a.id" :value="a.id">{{ a.username }}</option></select></label>
        <button class="text-button" type="button" :disabled="logsLoading" @click="loadLogs">刷新</button>
      </div>
      <p v-if="logsLoading" class="account-note" role="status">正在加载登录记录…</p>
      <p v-if="logsError" class="form-error account-note" role="alert">{{ logsError }}</p>
      <div class="table-wrap"><table>
        <thead><tr><th>登录时间（北京时间）</th><th>账号</th><th>IP 地址</th><th>设备/浏览器</th><th>状态</th></tr></thead>
        <tbody>
          <tr v-for="log in logs" :key="log.id">
            <td>{{ new Date(log.createdAt).toLocaleString('zh-CN') }}</td>
            <td>{{ log.username || log.adminId }}</td>
            <td>{{ log.ip || '—' }}</td>
            <td class="ua-cell">{{ log.userAgent || '—' }}</td>
            <td>{{ log.revoked ? '已失效' : '有效' }}</td>
          </tr>
          <tr v-if="!logsLoading && !logs.length"><td colspan="5">暂无登录记录</td></tr>
        </tbody>
      </table></div>
      <div class="log-pagination"><span>共 {{ logsTotal }} 条</span><div><button class="text-button" type="button" :disabled="logPage <= 1 || logsLoading" @click="logPage--; loadLogs()">上一页</button><button class="text-button" type="button" :disabled="logsLoading || logPage * logPageSize >= logsTotal" @click="logPage++; loadLogs()">下一页</button></div></div>
      <p class="account-note subtle">登录记录来自会话签发时的 IP 与设备信息；非超级管理员仅能查看自己的记录。</p>
    </template>
  </section>

  <Teleport to="body">
    <dialog ref="dialog" class="account-dialog" aria-labelledby="account-dialog-title" :aria-busy="busy" @cancel.prevent="closeEditor">
      <form novalidate @submit.prevent="submit">
        <div class="account-dialog-head"><div><p class="eyebrow purple">ACCOUNT SECURITY</p><h3 id="account-dialog-title">{{ title }}</h3></div><button class="text-button" type="button" :disabled="busy" aria-label="关闭账号面板" @click="closeEditor">关闭</button></div>
        <p v-if="target" class="target-summary"><strong>{{ target.username }}</strong> · {{ target.role === 'SUPER_ADMIN' ? '超级管理员' : '运营人员' }}</p>
        <template v-if="mode === 'create'">
          <label>管理员用户名<input ref="firstInput" v-model.trim="username" required maxlength="40" autocomplete="off" :disabled="busy" placeholder="请输入用户名" /></label>
          <label>账号角色<select v-model="role" :disabled="busy"><option value="OPERATOR">运营人员</option><option value="SUPER_ADMIN">超级管理员</option></select></label>
          <p class="account-help">运营人员可处理商品、订单、内容和线索；超级管理员还可管理账号与运营配置。</p>
          <label class="account-checkbox"><input v-model="active" type="checkbox" :disabled="busy" /> 创建后立即启用</label>
        </template>
        <fieldset v-if="mode === 'permissions' || (mode === 'create' && role === 'OPERATOR')" class="permission-list" :disabled="busy"><legend>操作权限</legend><div class="row-actions"><button class="text-button" type="button" @click="permissions = defaultPermissions()">全选</button><button class="text-button" type="button" @click="permissions = []">清空</button></div><label v-for="option in permissionOptions" :key="option.key" class="account-checkbox"><input v-model="permissions" type="checkbox" :value="option.key" />{{ option.label }}</label><p class="account-help">仅授予工作所需权限。未选择任何权限的账号可以登录，但无法访问业务模块。写入权限通常需要配合对应查看权限使用。</p></fieldset>
        <template v-if="mode === 'create' || mode === 'reset'">
          <p v-if="mode === 'reset'" class="account-warning">重置后，此账号在所有设备的旧登录会话都会失效。{{ target && isCurrent(target) ? '这是当前账号，完成后将返回登录页。' : '请将新密码安全告知账号使用者。' }}{{ !target?.active && mode === 'reset' ? '账号不会自动启用。' : '' }}</p>
          <label>{{ mode === 'create' ? '初始密码' : '新密码' }}<input v-if="mode === 'reset'" ref="firstInput" v-model="password" type="password" required maxlength="256" autocomplete="new-password" :disabled="busy" placeholder="至少 10 位，最多 128 位" /><input v-else v-model="password" type="password" required maxlength="256" autocomplete="new-password" :disabled="busy" placeholder="至少 10 位，最多 128 位" /></label>
          <label>确认密码<input v-model="confirmation" type="password" required maxlength="256" autocomplete="new-password" :disabled="busy" placeholder="再次输入密码" /></label>
          <p class="account-help">密码仅用于本次提交，保存或取消后清空。</p>
        </template>
        <p v-else-if="mode === 'toggle'" class="account-warning">{{ target?.active ? '停用期间此账号无法登录，已登录会话也无法继续操作。' : '启用后此账号可使用原密码登录；如需新密码，请重置密码。' }}</p>
        <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
        <div class="account-dialog-actions"><button class="button secondary" type="button" :disabled="busy" @click="closeEditor">取消</button><button class="button primary" :disabled="busy">{{ busy ? '提交中…' : submitLabel }}</button></div>
      </form>
    </dialog>
  </Teleport>
</template>

<style scoped>
.permission-list { margin-top: 20px; padding: 14px; border: 1px solid var(--border); border-radius: 9px; }
.permission-list legend { font-size: 13px; font-weight: 600; }
.account-row { grid-template-columns: minmax(0, 1fr) auto auto; }
.account-identity { min-width: 0; overflow-wrap: anywhere; }
.account-actions { display: flex; gap: 18px; }
.current-tag { display: inline-block; margin-left: 6px; padding: 3px 6px; border-radius: 5px; background: var(--purple-soft); color: var(--purple); font-size: 10px; font-weight: 500; }
.status.inactive { color: var(--muted); background: #eeebe7; }
.account-note { margin: 16px 24px; font-size: 12px; }
.sub-tabs { display: flex; gap: 8px; padding: 0 24px; margin-bottom: 4px; }
.sub-tabs button { padding: 8px 16px; border: 1px solid var(--border); border-radius: 8px; background: transparent; color: var(--muted); font-size: 13px; cursor: pointer; }
.sub-tabs button.active { background: var(--purple-soft); color: var(--purple); border-color: transparent; font-weight: 600; }
.log-filters { display: flex; align-items: center; gap: 16px; padding: 8px 24px 4px; }
.log-filters label { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--muted); }
.log-filters select { padding: 8px; border: 1px solid var(--border); border-radius: 8px; background: #fbf9f5; }
.table-wrap { overflow-x: auto; margin: 8px 24px 0; }
.table-wrap table { width: 100%; border-collapse: collapse; font-size: 13px; }
.table-wrap th, .table-wrap td { padding: 10px 12px; text-align: left; border-bottom: 1px solid var(--border); white-space: nowrap; }
.ua-cell { max-width: 320px; white-space: normal !important; overflow-wrap: anywhere; font-size: 12px; color: var(--muted); }
.log-pagination { display: flex; align-items: center; justify-content: space-between; padding: 12px 24px; color: var(--muted); font-size: 12px; }
.log-pagination > div { display: flex; gap: 12px; }
.account-feedback { padding: 0 24px; }
.account-feedback p { margin-bottom: 8px; }
.account-dialog { width: min(480px, calc(100vw - 32px)); max-height: calc(100dvh - 48px); overflow-y: auto; margin: auto; padding: 28px; border: 1px solid var(--border); border-radius: 18px; background: var(--paper); color: var(--ink); box-shadow: 0 24px 80px #27142633; }
.account-dialog::backdrop { background: #1f121f6b; backdrop-filter: blur(3px); }
.account-dialog-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 22px; }
.account-dialog h3 { margin: 8px 0 0; font-size: 20px; }
.account-dialog label { display: block; margin-top: 18px; font-size: 12px; font-weight: 600; }
.account-dialog input:not([type=checkbox]), .account-dialog select { display: block; width: 100%; padding: 12px; margin-top: 8px; border: 1px solid var(--border); border-radius: 9px; background: #fbf9f5; color: inherit; }
.account-dialog input:focus, .account-dialog select:focus { outline: 2px solid var(--purple); outline-offset: 2px; }
.account-dialog .account-checkbox { display: flex; align-items: center; gap: 8px; }
.account-checkbox input { margin: 0; accent-color: var(--purple); }
.account-help { font-size: 11px; color: var(--muted); line-height: 1.8; margin: 9px 0 0; }
.account-warning { font-size: 12px; line-height: 1.9; padding: 13px 15px; border-radius: 10px; background: #faf1df; color: #78551a; }
.target-summary { font-size: 13px; overflow-wrap: anywhere; }
.account-dialog-actions { display: flex; justify-content: flex-end; gap: 12px; margin-top: 26px; }
@media (max-width: 620px) { .account-row { grid-template-columns: minmax(0, 1fr) auto; gap: 12px; }.account-actions { grid-column: 1 / -1; justify-content: flex-end; }.account-dialog { padding: 22px; } }
</style>
