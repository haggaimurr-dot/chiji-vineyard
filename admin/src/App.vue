<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, isDemo, ApiError } from './api'
import { can, type AdminUser } from './permissions'
import PersonalSecurity from './views/PersonalSecurity.vue'
import DashboardView from './views/DashboardView.vue'
import ProductsView from './views/ProductsView.vue'
import OrdersView from './views/OrdersView.vue'
import ContentView from './views/ContentView.vue'
import LeadsView from './views/LeadsView.vue'
import SettingsView from './views/SettingsView.vue'
import LeadReminders from './components/LeadReminders.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import ReviewsView from './views/ReviewsView.vue'
import AuditView from './views/AuditView.vue'
import RuntimeStatus from './views/RuntimeStatus.vue'
import PaymentsView from './views/PaymentsView.vue'
import PaymentAlerts from './components/PaymentAlerts.vue'
import ReviewAlerts from './components/ReviewAlerts.vue'
import ReminderTasks from './views/ReminderTasks.vue'
import ActivityCheckin from './views/ActivityCheckin.vue'

const token = ref(localStorage.getItem('chiji_admin_token') || '')
const readUser = () => { try { return JSON.parse(localStorage.getItem('chiji_admin_user') || '{"username":"","role":""}') } catch { return { username: '', role: '' } } }
const currentUser = ref<AdminUser>(readUser())
const username = ref('')
const password = ref('')
const loginError = ref('')
const recoveryCodes = ref<string[]>([])
const mfaRequired = ref(false)
const code = ref('')
// 安全拦截：密码过期 / 强制 MFA 绑定（受限会话，只能完成对应安全操作）
const securityMode = ref<'' | 'pwd' | 'mfa'>('')
const newPassword = ref('')
const confirmPassword = ref('')
const mfaSecret = ref('')
const mfaUri = ref('')
const securityBusy = ref(false)
const loggingIn = ref(false)
const retryAt = ref(0)
const now = ref(Date.now())
const retrySeconds = computed(() => Math.max(0, Math.ceil((retryAt.value - now.value) / 1000)))
let ticker: ReturnType<typeof setInterval> | undefined
let sessionPoll: ReturnType<typeof setInterval> | undefined
const active = ref('dashboard')
const navOpen = ref(false)
const initialLeadId = ref('')
const leadNavigation = ref(0)
function openLead(leadId: string) { initialLeadId.value = leadId; leadNavigation.value++; active.value = 'leads'; navOpen.value = false }
function navigate(key: string) { initialLeadId.value = ''; active.value = key; navOpen.value = false }
const menuItems = [
  { key: 'security', label: '账号安全', icon: '安' },
  { key: 'dashboard', label: '经营概览', icon: '览' },
  { key: 'products', label: '商品与库存', icon: '果' },
  { key: 'reminders', label: '收藏与提醒', icon: '铃' },
  { key: 'orders', label: '订单与售后', icon: '单' },
  { key: 'reviews', label: '商品评价', icon: '评' },
  { key: 'content', label: '种植内容', icon: '课' },
  { key: 'checkin', label: '活动签到', icon: '签' },
  { key: 'leads', label: '合作线索', icon: '合' },
  { key: 'settings', label: '运营配置', icon: '设' },
  { key: 'payments', label: '微信支付', icon: '付' },
  { key: 'audit', label: '操作日志', icon: '志' },
  { key: 'runtime', label: '健康检查', icon: '康' },
]
const menu = computed(() => menuItems.filter((item) => (item.key !== 'reminders' || can(currentUser.value, 'product.read')) && (!['settings', 'audit', 'payments', 'runtime'].includes(item.key) || currentUser.value.role === 'SUPER_ADMIN')))
const views: Record<string, any> = { security: PersonalSecurity, payments: PaymentsView, reviews: ReviewsView, dashboard: DashboardView, products: ProductsView, orders: OrdersView, content: ContentView, leads: LeadsView, settings: SettingsView, audit: AuditView, checkin: ActivityCheckin, runtime: RuntimeStatus }
views.reminders = ReminderTasks
const currentView = computed(() => menu.value.some((item) => item.key === active.value) ? views[active.value] : views[menu.value[0]?.key || ''])
const currentLabel = computed(() => menu.value.find((item) => item.key === active.value)?.label || menu.value[0]?.label || '暂无可用模块')

function sessionExpired(event: Event) {
  const savedCodes = (event as CustomEvent<{ recoveryCodes?: string[] }>).detail?.recoveryCodes
  if (savedCodes) recoveryCodes.value = savedCodes
  localStorage.removeItem('chiji_admin_token')
  localStorage.removeItem('chiji_admin_user')
  token.value = ''
  mfaRequired.value = false
  code.value = ''
  password.value = ''
  securityMode.value = ''
  active.value = 'dashboard'
  currentUser.value = { username: '', role: '' }
  loginError.value = (event as CustomEvent<{ message?: string }>).detail?.message || '登录已失效，请重新登录'
}
async function refreshSession() {
  if (!token.value) return
  try {
    const user = await api<AdminUser>('admin.session')
    currentUser.value = user
    localStorage.setItem('chiji_admin_user', JSON.stringify(user))
    if (!menu.value.some(item => item.key === active.value)) active.value = menu.value[0]?.key || ''
  } catch { /* API handles expired sessions; retry transient failures on the next refresh. */ }
}
onMounted(() => {
  window.addEventListener('chiji-admin-session-expired', sessionExpired)
  window.addEventListener('chiji-admin-permissions-changed', refreshSession)
  ticker = setInterval(() => { now.value = Date.now() }, 1000)
  sessionPoll = setInterval(refreshSession, 60000)
  refreshSession()
})
onUnmounted(() => {
  window.removeEventListener('chiji-admin-session-expired', sessionExpired)
  window.removeEventListener('chiji-admin-permissions-changed', refreshSession)
  clearInterval(ticker); clearInterval(sessionPoll)
})

async function login() {
  if (loggingIn.value || retrySeconds.value) return
  loginError.value = ''
  if (!username.value || !password.value) { loginError.value = '请输入用户名和密码'; return }
  loggingIn.value = true
  try {
    const result = await api<{ token?: string; user?: AdminUser; mfaRequired?: boolean; passwordExpired?: boolean; mfaSetupRequired?: boolean }>('admin.login', { username: username.value, password: password.value, code: code.value.trim() })
    if (result.mfaRequired) { mfaRequired.value = true; return }
    if (result.passwordExpired && result.token) { enterSecurityMode('pwd', result.token); return }
    if (result.mfaSetupRequired && result.token) { enterSecurityMode('mfa', result.token); return }
    if (!result.token || !result.user) throw new Error('登录响应无效')
    mfaRequired.value = false
    code.value = ''
    localStorage.setItem('chiji_admin_token', result.token)
    localStorage.setItem('chiji_admin_user', JSON.stringify(result.user))
    currentUser.value = result.user
    active.value = 'dashboard'
    password.value = ''
    token.value = result.token
  } catch (error) {
    loginError.value = (error as Error).message
    if (error instanceof ApiError && error.status === 429) retryAt.value = Date.now() + error.retryAfter * 1000
  }
  finally { loggingIn.value = false }
}

function enterSecurityMode(mode: 'pwd' | 'mfa', limitedToken: string) {
  localStorage.setItem('chiji_admin_token', limitedToken)
  newPassword.value = ''; confirmPassword.value = ''; mfaSecret.value = ''; mfaUri.value = ''; code.value = ''
  loginError.value = ''
  securityMode.value = mode
}
function exitSecurityMode(message: string) {
  localStorage.removeItem('chiji_admin_token')
  securityMode.value = ''
  mfaSecret.value = ''; mfaUri.value = ''; newPassword.value = ''; confirmPassword.value = ''; code.value = ''
  password.value = ''
  loginError.value = message
}
async function submitPasswordChange() {
  if (securityBusy.value) return
  if (newPassword.value.length < 10) { loginError.value = '新密码至少 10 位'; return }
  if (newPassword.value !== confirmPassword.value) { loginError.value = '两次输入的新密码不一致'; return }
  securityBusy.value = true; loginError.value = ''
  try {
    await api('admin.account.changePassword', { currentPassword: password.value, password: newPassword.value })
    exitSecurityMode('密码已更新，请用新密码重新登录')
  } catch (error) { loginError.value = (error as Error).message }
  finally { securityBusy.value = false }
}
async function startMfaSetup() {
  if (securityBusy.value) return
  securityBusy.value = true; loginError.value = ''
  try {
    const result = await api<{ secret: string; uri: string }>('admin.security.setup', { currentPassword: password.value })
    mfaSecret.value = result.secret; mfaUri.value = result.uri
  } catch (error) { loginError.value = (error as Error).message }
  finally { securityBusy.value = false }
}
async function confirmMfaSetup() {
  if (securityBusy.value) return
  if (!code.value.trim()) { loginError.value = '请输入验证器中的 6 位动态码'; return }
  securityBusy.value = true; loginError.value = ''
  try {
    const result = await api<{ recoveryCodes?: string[] }>('admin.security.enable', { currentPassword: password.value, code: code.value.trim() })
    localStorage.removeItem('chiji_admin_token')
    securityMode.value = ''
    password.value = ''; code.value = ''
    if (result.recoveryCodes?.length) { recoveryCodes.value = result.recoveryCodes; return }
    loginError.value = '二次验证已绑定，请重新登录'
  } catch (error) { loginError.value = (error as Error).message }
  finally { securityBusy.value = false }
}

async function logout() {
  if (!isDemo) {
    try { await api('admin.logout') }
    catch (error) {
      if (token.value) { window.alert(`退出登录失败：${(error as Error).message}`); return }
    }
  }
  localStorage.removeItem('chiji_admin_token')
  localStorage.removeItem('chiji_admin_user')
  token.value = ''
  mfaRequired.value = false
  code.value = ''
  password.value = ''
  currentUser.value = { username: '', role: '' }
  active.value = 'dashboard'
}
</script>

<template>
  <main v-if="!token" class="login-page">
    <section class="login-brand">
      <div class="brand-mark">迟</div>
      <p class="eyebrow">CHIJIGRAPE OPERATIONS</p>
      <h1>从枝头到餐桌，<br />把每一步经营好。</h1>
      <p>统一管理商品、订单、种植内容与渠道合作线索。</p>
      <div class="login-orbs"><i></i><i></i><i></i></div>
    </section>
    <section class="login-panel">
      <form class="login-card" @submit.prevent="login">
        <div v-if="recoveryCodes.length" class="demo-note" role="status"><strong>多因素认证已启用，请保存恢复码</strong><p>恢复码仅显示这一次；验证器丢失时，每个恢复码可以使用一次。请保存到安全位置。</p><pre style="user-select: all; overflow-wrap: anywhere; white-space: pre-wrap">{{ recoveryCodes.join('\n') }}</pre><button type="button" class="button secondary" @click="recoveryCodes = []">我已安全保存</button></div>
        <p class="eyebrow purple">WELCOME BACK</p>
        <h2>登录运营后台</h2>
        <p class="subtle">使用管理员账号继续</p>
        <div v-if="isDemo" class="demo-note">演示模式：首次可使用任意账号密码。创建账号及重置密码仅在本浏览器会话生效，重置后请使用新密码登录。</div>
        <template v-if="!securityMode">
        <label>用户名<input v-model.trim="username" autocomplete="username" placeholder="请输入管理员用户名" /></label>
        <label>密码<input v-model="password" type="password" autocomplete="current-password" placeholder="至少 10 位安全密码" /></label>
        <label v-if="mfaRequired">动态码或恢复码<input v-model="code" autocomplete="one-time-code" maxlength="32" placeholder="验证器中的 6 位动态码或一次性恢复码" /><small>启用多因素认证的账号需要完成第二步验证。</small></label>
        <p v-if="loginError" class="form-error" role="alert">{{ loginError }}</p>
        <button class="button primary wide" :disabled="loggingIn || retrySeconds > 0 || recoveryCodes.length > 0">{{ retrySeconds ? `${retrySeconds} 秒后重试` : loggingIn ? '正在登录…' : '进入后台' }}</button>
        </template>
        <template v-if="securityMode === 'pwd'">
        <div class="demo-note" role="status"><strong>密码已过期</strong><p>按安全策略，密码每 90 天需要更换一次。请设置新密码后重新登录。</p></div>
        <label>新密码<input v-model="newPassword" type="password" autocomplete="new-password" placeholder="至少 10 位" /></label>
        <label>确认新密码<input v-model="confirmPassword" type="password" autocomplete="new-password" placeholder="再次输入新密码" /></label>
        <button type="button" class="button primary wide" :disabled="securityBusy" @click="submitPasswordChange">{{ securityBusy ? '提交中…' : '修改密码并重新登录' }}</button>
        <button type="button" class="button secondary wide" @click="exitSecurityMode('')">返回登录</button>
        </template>
        <template v-if="securityMode === 'mfa'">
        <div class="demo-note" role="status"><strong>需要绑定二次验证</strong><p>按安全策略，所有管理员必须启用多因素认证。请用验证器 App 扫描或手动输入密钥，完成绑定后重新登录。</p></div>
        <button v-if="!mfaSecret" type="button" class="button primary wide" :disabled="securityBusy" @click="startMfaSetup">{{ securityBusy ? '生成中…' : '生成绑定密钥' }}</button>
        <template v-else>
        <label>绑定密钥（手动输入）<input :value="mfaSecret" readonly style="user-select: all" /></label>
        <p class="subtle" style="overflow-wrap: anywhere">{{ mfaUri }}</p>
        <label>验证器动态码<input v-model="code" autocomplete="one-time-code" maxlength="32" placeholder="6 位动态码" /></label>
        <button type="button" class="button primary wide" :disabled="securityBusy" @click="confirmMfaSetup">{{ securityBusy ? '验证中…' : '验证并启用' }}</button>
        </template>
        <button type="button" class="button secondary wide" @click="exitSecurityMode('')">返回登录</button>
        </template>
        <p class="login-foot">迟记葡萄牙 · 内部运营系统</p>
      </form>
    </section>
  </main>

  <div v-else class="shell">
    <div v-if="navOpen" class="nav-backdrop" @click="navOpen = false"></div>
    <aside class="sidebar" :class="{ open: navOpen }">
      <div class="logo"><span>迟</span><div><strong>迟记葡萄牙</strong><small>运营中心</small></div></div>
      <nav><button v-for="item in menu" :key="item.key" :class="{ active: active === item.key }" @click="navigate(item.key)"><span>{{ item.icon }}</span>{{ item.label }}</button></nav>
      <div class="sidebar-foot"><div class="operator"><span>运</span><div><strong>{{ currentUser.username }}</strong><small>{{ currentUser.role === 'SUPER_ADMIN' ? '超级管理员' : '运营人员' }} · 在线</small></div></div><button class="logout" @click="logout">退出登录</button></div>
    </aside>
    <section class="workspace">
      <header class="topbar"><button class="menu-toggle" type="button" aria-label="打开菜单" @click="navOpen = true">☰</button><div><p class="eyebrow purple">CHIJIGRAPE</p><h1>{{ currentLabel }}</h1></div><div class="top-actions"><PaymentAlerts v-if="currentUser.role === 'SUPER_ADMIN'" @open="navigate('payments')" /><ReviewAlerts v-if="can(currentUser, 'review.read')" @open="navigate('reviews')" /><LeadReminders v-if="can(currentUser, 'lead.read')" :current-user-id="currentUser.id" @open="openLead" /><span v-if="isDemo" class="demo-badge">演示数据</span><span class="date">{{ new Date().toLocaleDateString('zh-CN', { month: 'long', day: 'numeric', weekday: 'short' }) }}</span></div></header>
      <div class="view"><p v-if="!currentView" class="subtle">当前账号暂无可用模块，请联系超级管理员分配权限。</p><component v-else :is="currentView" :key="`${active === 'leads' ? `leads-${leadNavigation}` : active}-${currentUser.permissions?.join(',')}`" v-bind="active === 'leads' ? { initialLeadId } : {}" /></div>
    </section>
    <ConfirmDialog />
  </div>
</template>
