export const runtimeTasks = [
  { key: 'expireOrders', name: '超时订单关闭', interval: 60, action: 'internal.expireOrders' },
  { key: 'publishArticles', name: '内容定时发布', interval: 30, action: 'internal.publishArticles' },
  { key: 'restockNotices', name: '到货提醒', interval: 60, action: 'internal.restockNotices' },
  { key: 'priceDropNotices', name: '降价提醒', interval: 60, action: 'internal.priceDropNotices' },
  { key: 'autoInvoices', name: '自动开票', interval: 60, action: 'internal.autoInvoices' },
  { key: 'paymentReconcile', name: '支付对账', interval: 86400, action: 'internal.paymentReconcile' },
  { key: 'paymentFinanceReconcile', name: '财务对账', interval: 86400, action: 'internal.paymentFinanceReconcile' },
] as const
export interface RuntimeRun { id: string; task: string; source: string; status: 'SUCCESS' | 'FAILED'; startedAt: string; finishedAt: string; durationMs: number; summary: string; actor: string; retryOf?: string }
export interface RuntimeMigration { id: string; name: string; status: 'PENDING' | 'APPLIED'; appliedAt?: string }
export interface RuntimeSettings { scaleToZero: boolean; externalScheduler: boolean; provider: string; endpoint: string; mediaBase: string }
interface Check { name: string; passed: boolean; detail: string }
interface MediaAcceptance { checkedAt: string; base: string; passed: boolean; checks: Check[] }
interface Snapshot { settings: RuntimeSettings; migrations: RuntimeMigration[]; media: MediaAcceptance | null }
interface Backup { id: string; createdAt: string; checksum: string; snapshot: Snapshot }
interface Drill { id: string; backupId: string; startedAt: string; durationMs: number; status: 'SUCCESS' | 'FAILED'; summary: string; checksum: string }
interface State extends Snapshot { runs: RuntimeRun[]; backups: Backup[]; drills: Drill[] }
export interface RuntimeOverview extends Omit<State, 'backups'> { backups: Omit<Backup, 'snapshot'>[]; alerts: { key: string; message: string }[]; demo: true }
const storageKey = 'chiji_runtime_operations_demo_v1'
const migrationNames = [
  ['schema-baseline', '各模块基础表（schema.sql）'],
  ['20261005_identity', '身份与权限'], ['20261006_account_security', '账号安全'],
  ['20261006_after_sales', '售后工单'], ['20261006_product_management', '商品批次'],
  ['20261007_activity_notices', '活动通知'], ['20261007_classroom', '课堂学习'],
  ['20261007_reminder_targets_history', '提醒目标价与历史'],
  ['20261008_activity_capacity', '活动容量与候补'], ['20261008_auth_sessions', '登录会话'],
  ['20261008_coupon_wallet', '优惠券钱包'], ['20261008_inventory_lots', '仓库批次库存'],
  ['20261008_payment_finance', '支付财务'], ['config-media-runtime', '配置版本、媒体与任务执行记录'],
]
function initialState(): State {
  return { settings: { scaleToZero: true, externalScheduler: false, provider: '云函数定时触发器', endpoint: 'https://api.example.com/api', mediaBase: 'https://media.example.com' },
    migrations: migrationNames.map(([id, name]) => ({ id: id!, name: name!, status: 'PENDING' })), media: null, runs: [], backups: [], drills: [] }
}
function read(): State {
  const raw = globalThis.localStorage?.getItem(storageKey)
  if (!raw) return initialState()
  try { return JSON.parse(raw) as State } catch { throw new Error('运行模块演示数据损坏，请检查浏览器存储') }
}
function save(state: State) { globalThis.localStorage?.setItem(storageKey, JSON.stringify(state)) }
function now() { return new Date().toISOString() }
function id() { return crypto.randomUUID() }
function httpsOrigin(value: string) {
  let url: URL
  try { url = new URL(value) } catch { throw new Error('请输入有效的 HTTPS 地址') }
  if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || url.hostname === 'localhost' || /^127\.|^10\.|^192\.168\.|^172\.(1[6-9]|2\d|3[01])\.|^\[/.test(url.hostname)) throw new Error('请输入不含凭据、查询参数和片段的公网 HTTPS 地址')
  return url
}
async function checksum(value: unknown) {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify(value)))
  return Array.from(new Uint8Array(digest), x => x.toString(16).padStart(2, '0')).join('')
}
function overview(state: State): RuntimeOverview {
  const alerts: RuntimeOverview['alerts'] = []
  if (state.migrations.some(m => m.status === 'PENDING')) alerts.push({ key: 'migrations', message: '数据库迁移尚未全部模拟完成' })
  if (state.settings.scaleToZero && !state.settings.externalScheduler) alerts.push({ key: 'scheduler', message: '缩容到零时需要外部调度唤醒服务' })
  if (!state.media?.passed || state.media.base !== state.settings.mediaBase) alerts.push({ key: 'media', message: '媒体域名尚未通过模拟验收' })
  for (const task of runtimeTasks) {
    const last = state.runs.find(r => r.task === task.key)
    if (!last) alerts.push({ key: task.key, message: `${task.name}尚无执行记录` })
    else if (last.status === 'FAILED') alerts.push({ key: task.key, message: `${task.name}最近一次执行失败：${last.summary}` })
    else if (Date.now() - Date.parse(last.finishedAt) > task.interval * 3 * 1000) alerts.push({ key: task.key, message: `${task.name}超过三个调度周期未执行` })
  }
  const drill = state.drills[0]
  if (!drill || drill.status !== 'SUCCESS' || Date.now() - Date.parse(drill.startedAt) > 30 * 86400000) alerts.push({ key: 'backup', message: '缺少近30天内成功的恢复演练' })
  return { ...structuredClone(state), backups: state.backups.map(({ snapshot: _, ...b }) => b), alerts, demo: true }
}
export async function runtimeDemo(action: string, data: Record<string, unknown>, actor: string): Promise<unknown> {
  const state = read()
  if (action === 'admin.runtime.overview') return overview(state)
  if (action === 'admin.runtime.settings') {
    const d = data as unknown as RuntimeSettings
    if (typeof d.scaleToZero !== 'boolean' || typeof d.externalScheduler !== 'boolean') throw new Error('调度配置无效')
    if (d.scaleToZero && !d.externalScheduler) throw new Error('缩容到零必须启用外部调度')
    const endpoint = httpsOrigin(String(d.endpoint || ''))
    if (endpoint.pathname !== '/api') throw new Error('调度地址必须指向 /api')
    const media = httpsOrigin(String(d.mediaBase || ''))
    if (media.pathname !== '/') throw new Error('媒体域名不能包含路径')
    const provider = String(d.provider || '').trim()
    if (d.externalScheduler && (!provider || provider.length > 80)) throw new Error('请填写外部调度平台，最多80字')
    state.settings = { scaleToZero: d.scaleToZero, externalScheduler: d.externalScheduler, provider, endpoint: endpoint.href, mediaBase: media.origin }
    if (state.media?.base !== media.origin) state.media = null
  } else if (action === 'admin.runtime.migrate') {
    const at = now()
    const pending = state.migrations.filter(m => m.status === 'PENDING')
    pending.forEach(m => { m.status = 'APPLIED'; m.appliedAt = at })
    state.runs.unshift({ id: id(), task: 'migrations', source: '手动模拟', actor, status: 'SUCCESS', startedAt: at, finishedAt: at, durationMs: 0, summary: `模拟应用 ${pending.length} 项迁移；未连接数据库` })
  } else if (action === 'admin.runtime.run') {
    const task = runtimeTasks.find(t => t.key === data.task)
    if (!task) throw new Error('未知运行任务')
    const source = data.source === 'external' ? '外部调度模拟' : '手动模拟'
    if (source === '外部调度模拟' && !state.settings.externalScheduler) throw new Error('请先配置外部调度')
    if (state.migrations.some(m => m.status !== 'APPLIED')) throw new Error('请先完成模拟数据库迁移')
    const retry = data.retryOf ? state.runs.find(r => r.id === data.retryOf && r.task === task.key && r.status === 'FAILED') : null
    if (data.retryOf && !retry) throw new Error('只能重试对应任务的失败记录')
    const startedAt = now(), failed = data.fail === true
    state.runs.unshift({ id: id(), task: task.key, source, actor, status: failed ? 'FAILED' : 'SUCCESS', startedAt, finishedAt: now(), durationMs: 0, summary: failed ? '模拟上游服务超时，可重试' : '模拟执行完成；未访问数据库或发送真实消息', ...(retry ? { retryOf: retry.id } : {}) })
  } else if (action === 'admin.runtime.mediaCheck') {
    httpsOrigin(state.settings.mediaBase)
    const failed = data.fail === true
    const names = ['HTTPS 与证书', '图片 MIME 与完整读取', '视频 Range 返回206', 'HEAD 与 Content-Length', '缓存 ETag 与304', '微信真机图片与视频播放']
    state.media = { checkedAt: now(), base: state.settings.mediaBase, passed: !failed, checks: names.map((name, index) => ({ name, passed: !failed || index !== 2, detail: failed && index === 2 ? '模拟 Range 未返回206' : '模拟通过，需在正式域名和真机复验' })) }
  } else if (action === 'admin.runtime.backup') {
    const snapshot = structuredClone({ settings: state.settings, migrations: state.migrations, media: state.media })
    state.backups.unshift({ id: id(), createdAt: now(), checksum: await checksum(snapshot), snapshot })
  } else if (action === 'admin.runtime.drill') {
    const backup = state.backups.find(b => b.id === data.backupId)
    if (!backup) throw new Error('请选择已有备份')
    const startedAt = now(), start = Date.now()
    // Restore into an isolated object; the live demo state is never replaced.
    const restored = structuredClone(backup.snapshot)
    if (data.fail === true) restored.migrations = []
    const actual = await checksum(restored), passed = actual === backup.checksum
    state.drills.unshift({ id: id(), backupId: backup.id, startedAt, durationMs: Date.now() - start, status: passed ? 'SUCCESS' : 'FAILED', checksum: actual,
      summary: passed ? `隔离副本恢复成功，SHA-256 一致；核对 ${restored.migrations.length} 项迁移、调度配置及媒体验收` : '隔离副本 SHA-256 不一致，恢复演练失败' })
  } else throw new Error('未知运行模块操作')
  state.runs = state.runs.slice(0, 200)
  state.backups = state.backups.slice(0, 20)
  state.drills = state.drills.slice(0, 50)
  save(state)
  return overview(state)
}
