import { matchTotp, newTotpSecret } from './totp'
import { defaultPermissions, requirePermission } from './permissions'
// Preview credentials never enter the live API or the mini program's demo store.
interface Account { permissions?: string[]; id: string; username: string; role: string; active: boolean; createdAt: string }
interface Credential { salt: string; digest: string; version: number }
interface Security { enabled: boolean; secret: string; pending: string; expiresAt: number; last: number; recovery: string[] }
interface DemoState {
  security?: Record<string, Security>
  accounts: Account[]
  credentials: Record<string, Credential>
  sessions: Record<string, { id: string; version: number }>
  initialized: boolean
}
const accountKey = 'chiji_admin_demo_accounts_v1'
const configKey = 'chiji_admin_demo_config_v1'
let state: DemoState = { accounts: [{ id: 'admin-demo', username: 'demo', role: 'SUPER_ADMIN', active: true, createdAt: '2026-09-01T00:00:00.000Z' }], credentials: {}, sessions: {}, initialized: false }
try {
  const raw = globalThis.sessionStorage?.getItem(accountKey)
  const saved = raw && JSON.parse(raw)
  if (saved && Array.isArray(saved.accounts) && saved.accounts.length && saved.credentials && saved.sessions) state = saved
} catch { /* Start a fresh preview when the browser store is unavailable or invalid. */ }

function persist(next: DemoState) {
  try { globalThis.sessionStorage?.setItem(accountKey, JSON.stringify(next)) }
  catch { throw new Error('浏览器无法保存演示状态，请允许会话存储后重试') }
  state = next
}
export function readDemoConfig<T>(initial: T): T {
  try {
    const raw = globalThis.sessionStorage?.getItem(configKey)
    const saved = raw && JSON.parse(raw)
    if (saved?.home?.banners && saved.privacy?.content && saved.shipping) return saved as T
  } catch { /* Retain the built-in preview if storage cannot be read. */ }
  return initial
}
export function saveDemoConfig(value: unknown) {
  try { globalThis.sessionStorage?.setItem(configKey, JSON.stringify(value)) }
  catch { throw new Error('浏览器无法保存演示状态，请允许会话存储后重试') }
}
async function digestPassword(password: string, salt: string) {
  const bytes = new TextEncoder().encode(`${salt}\u0000${password}`)
  const digest = await globalThis.crypto.subtle.digest('SHA-256', bytes)
  return Array.from(new Uint8Array(digest), x => x.toString(16).padStart(2, '0')).join('')
}
async function credentialFor(password: string, previous?: Credential): Promise<Credential> {
  if ([...password].length < 10 || [...password].length > 128) throw new Error('管理员密码需要 10–128 位')
  const salt = globalThis.crypto.randomUUID()
  return { salt, digest: await digestPassword(password, salt), version: (previous?.version || 0) + 1 }
}
export function demoAccounts() { return state.accounts.map(a => ({ ...a, permissions: a.permissions ?? defaultPermissions() })) }
export function requireDemoActor(action: string): Account {
  let actor = state.accounts[0]
  if (globalThis.localStorage) {
    const session = state.sessions[globalThis.localStorage.getItem('chiji_admin_token') || '']
    const account = state.accounts.find(a => a.id === session?.id)
    if (!session || !account?.active || session.version !== (state.credentials[account.id]?.version || 0)) throw new Error('登录已失效，请重新登录')
    actor = account
  }
  requirePermission(actor, action)
  return actor
}
export async function demoLogin(data: { username?: string; password?: string; code?: string }) {
  const name = String(data.username || '').trim()
  if (!name || [...name].length > 40 || !data.password) throw new Error('请输入有效的用户名和密码')
  limitDemoLogin(name.toLowerCase())
  const next = structuredClone(state)
  let account = next.accounts.find(a => a.username.toLowerCase() === name.toLowerCase())
  if (!next.initialized) { account = next.accounts[0]; account.username = name; next.initialized = true }
  if (account && !state.initialized) {
    const salt = crypto.randomUUID()
    next.credentials[account.id] = { salt, digest: await digestPassword(data.password, salt), version: 1 }
  }
  const credential = account && next.credentials[account.id]
  if (!account?.active || credential && await digestPassword(data.password, credential.salt) !== credential.digest) throw new Error('用户名或密码错误')
  const security = next.security?.[account.id]
  if (security?.enabled) {
    if (!data.code) return { mfaRequired: true }
    await verifyDemoFactor(security, account.id, data.code.trim())
  }
  const token = `demo-${globalThis.crypto.randomUUID()}`
  next.sessions[token] = { id: account.id, version: credential?.version || 0 }
  persist(next)
  return { token, user: { id: account.id, username: account.username, role: account.role, permissions: account.role === 'SUPER_ADMIN' ? defaultPermissions() : account.permissions ?? defaultPermissions() } }
}
export async function demoAccountSave(input: { id?: string; username: string; password?: string; role: string; active: boolean; permissions?: string[] }, actor: Account) {
  const name = String(input?.username || '').trim()
  const existing = state.accounts.find(a => a.id === input?.id)
  if (!name || [...name].length > 40) throw new Error('管理员用户名不能为空且最多 40 位')
  if (!['OPERATOR', 'SUPER_ADMIN'].includes(input.role)) throw new Error('管理员角色无效')
  if (input.id && !existing) throw new Error('管理员账号不存在')
  if (state.accounts.some(a => a.id !== existing?.id && a.username.toLowerCase() === name.toLowerCase())) throw new Error('管理员用户名已存在')
  if (existing?.id === actor.id && !input.active) throw new Error('不能停用当前账号')
  if (existing?.active && existing.role === 'SUPER_ADMIN' && (!input.active || input.role !== 'SUPER_ADMIN') && !state.accounts.some(a => a.id !== existing.id && a.active && a.role === 'SUPER_ADMIN')) throw new Error('必须保留至少一个启用的超级管理员')
  if (existing && input.password) throw new Error('请使用重置密码功能修改密码')
  if (input.permissions?.some(p => !defaultPermissions().includes(p))) throw new Error('权限标识无效')
  const account: Account = { permissions: input.permissions ?? existing?.permissions, id: existing?.id || `admin-${globalThis.crypto.randomUUID()}`, username: name, role: input.role, active: Boolean(input.active), createdAt: existing?.createdAt || new Date().toISOString() }
  const next = structuredClone(state)
  if (!existing) next.credentials[account.id] = await credentialFor(String(input.password || ''))
  // The serialized state and returned account contain no submitted plaintext password.
  next.accounts = [...next.accounts.filter(a => a.id !== account.id), account]
  persist(next)
  return { ...account }
}
export async function demoResetPassword(id: string, password: string, actor: Account) {
  if (!state.accounts.some(a => a.id === id)) throw new Error('管理员账号不存在')
  const next = structuredClone(state)
  next.credentials[id] = await credentialFor(String(password || ''), next.credentials[id])
  persist(next)
  return { id, sessionRevoked: true, currentSessionRevoked: id === actor.id }
}

export function demoSavePermissions(id: string, permissions: string[], actor: Account) {
  if (actor.role !== 'SUPER_ADMIN') throw new Error('仅超级管理员可以操作')
  const account = state.accounts.find(a => a.id === id)
  if (!account) throw new Error('管理员账号不存在')
  if (account.role !== 'OPERATOR') throw new Error('超级管理员拥有全部权限，无需设置')
  if (!Array.isArray(permissions) || permissions.some(p => !defaultPermissions().includes(p))) throw new Error('权限标识无效')
  const next = structuredClone(state)
  next.accounts.find(a => a.id === id)!.permissions = [...new Set(permissions)]
  persist(next)
  return { id, permissions: [...new Set(permissions)] }
}
const loginAttempts = new Map<string, { attempts: number; expires: number }>()
try {
  const saved = JSON.parse(globalThis.sessionStorage?.getItem('chiji_admin_demo_login_limits') || '[]')
  for (const [name, record] of saved) {
    if (typeof name === 'string' && Number.isFinite(record?.attempts) && Number.isFinite(record?.expires)) loginAttempts.set(name, record)
  }
} catch { /* Invalid preview counters start a new local window. */ }
function limitDemoLogin(name: string) {
  const now = Date.now()
  const record = loginAttempts.get(name) || { attempts: 0, expires: now + 900000 }
  if (record.expires <= now) { record.attempts = 0; record.expires = now + 900000 }
  record.attempts++
  loginAttempts.set(name, record)
  for (const [key, item] of loginAttempts) if (item.expires <= now) loginAttempts.delete(key)
  try { globalThis.sessionStorage?.setItem('chiji_admin_demo_login_limits', JSON.stringify([...loginAttempts])) } catch { /* Retain counters in memory if storage is unavailable. */ }
  if (record.attempts > 5) {
    const retryAfter = Math.max(1, Math.ceil((record.expires - now) / 1000))
    throw Object.assign(new Error(`登录尝试过于频繁，请 ${retryAfter} 秒后重试`), { retryAfter })
  }
}

async function verifyDemoFactor(security: Security, id: string, code: string) {
  const step = await matchTotp(security.secret, code, security.last)
  if (step >= 0) { security.last = step; return }
  const hash = await digestPassword(code.toLowerCase(), id)
  const index = security.recovery.indexOf(hash)
  if (index < 0) throw new Error('动态码或恢复码无效；已使用的动态码不能重复使用')
  security.recovery.splice(index, 1)
}
export async function demoPersonalSecurity(action: string, data: { currentPassword?: string; password?: string; code?: string }, actor: Account) {
  const next = structuredClone(state)
  next.security ||= {}
  const security = next.security[actor.id] ||= { enabled: false, secret: '', pending: '', expiresAt: 0, last: -1, recovery: [] }
  if (action === 'admin.security.status') return { enabled: security.enabled, recoveryRemaining: security.recovery.length }
  limitDemoLogin('security:' + actor.id)
  const credential = next.credentials[actor.id]
  if (!credential || !data.currentPassword || await digestPassword(data.currentPassword, credential.salt) !== credential.digest) throw new Error('原密码错误')
  const result: { secret?: string; expiresAt?: number; recoveryCodes?: string[]; sessionRevoked?: boolean } = {}
  if (action === 'admin.security.setup') {
    if (security.enabled) throw new Error('多因素认证已启用')
    security.pending = newTotpSecret(); security.expiresAt = Math.floor(Date.now() / 1000) + 600
    result.secret = security.pending; result.expiresAt = security.expiresAt
  } else if (action === 'admin.security.enable') {
    if (security.enabled || !security.pending || security.expiresAt <= Date.now() / 1000) throw new Error('绑定已过期，请重新开始')
    const step = await matchTotp(security.pending, data.code || '')
    if (step < 0) throw new Error('动态码无效')
    security.enabled = true; security.secret = security.pending; security.pending = ''; security.last = step
    result.recoveryCodes = []; security.recovery = []
    for (let i = 0; i < 8; i++) {
      const code = Array.from(crypto.getRandomValues(new Uint8Array(10)), b => b.toString(16).padStart(2, '0')).join('')
      result.recoveryCodes.push(code); security.recovery.push(await digestPassword(code, actor.id))
    }
    next.credentials[actor.id] = await credentialFor(data.currentPassword, credential)
    result.sessionRevoked = true
  } else {
    if (security.enabled) await verifyDemoFactor(security, actor.id, String(data.code || '').trim())
    if (action === 'admin.security.disable') next.security[actor.id] = { enabled: false, secret: '', pending: '', expiresAt: 0, last: -1, recovery: [] }
    if (action === 'admin.account.changePassword' && data.password === data.currentPassword) throw new Error('新密码不能与原密码相同')
    next.credentials[actor.id] = await credentialFor(action === 'admin.security.disable' ? data.currentPassword : String(data.password || ''), credential)
    security.pending = ''; security.expiresAt = 0
    result.sessionRevoked = true
  }
  persist(next)
  return result
}
