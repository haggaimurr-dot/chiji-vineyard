import { csvCell } from './order-operations'
type ConfigKey = 'shipping' | 'coupons' | 'home' | 'privacy'
type Snapshot = { id: string; key: ConfigKey; version: number; action: 'BASELINE' | 'SAVE' | 'ROLLBACK'; sourceVersion: number | null; username: string; createdAt: string; payload: unknown }
type Log = { id: string; adminId: string; username: string; action: string; targetId: string; detail: unknown; createdAt: string }
const keys: ConfigKey[] = ['shipping', 'coupons', 'home', 'privacy']

function configKey(value: unknown): ConfigKey {
  if (!keys.includes(value as ConfigKey)) throw new Error('配置类型无效')
  return value as ConfigKey
}

function pagination(data: { limit?: number; offset?: number }) {
  const limit = data.limit ?? 20, offset = data.offset ?? 0
  if (!Number.isInteger(limit) || limit < 1 || limit > 100 || !Number.isInteger(offset) || offset < 0) throw new Error('分页参数无效')
  return { limit, offset }
}

function dateBoundary(date?: string) {
  if (!date) return undefined
  const value = Date.parse(`${date}T00:00:00+08:00`)
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !Number.isFinite(value) || new Date(value + 8 * 3600000).toISOString().slice(0, 10) !== date) throw new Error('日期无效')
  return value
}

function redactDetail(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(redactDetail)
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, item]) => {
    const lower = key.toLowerCase().replace(/[_\- ]/g, '')
    const sensitive = lower === 'passwordchanged' && typeof item === 'boolean' ? false : /password|token|secret|credential/.test(lower) || ['authorization', 'cookie', 'hash'].includes(lower)
    return [key, sensitive ? '[已隐藏]' : redactDetail(item)]
  }))
  return value
}

// The demonstration history belongs to this browser session, as do its configs.
export function createMockHistory(read: (key: ConfigKey) => unknown, write: (key: ConfigKey, payload: unknown) => void, validate: (key: ConfigKey, payload: any) => unknown) {
  let snapshots: Snapshot[] = [], logs: Log[] = []
  let username = 'demo', adminId = 'admin-demo', nextSnapshot = 0, nextLog = 0
  const storageKey = 'chiji_admin_demo_history_v1'
  try {
    const raw = globalThis.sessionStorage?.getItem(storageKey), saved = raw && JSON.parse(raw)
    if (saved && Array.isArray(saved.snapshots) && Array.isArray(saved.logs) && keys.every(key => saved.payloads?.[key])) {
      snapshots = saved.snapshots; logs = saved.logs
      nextSnapshot = snapshots.length; nextLog = logs.length
      for (const key of keys) write(key, structuredClone(saved.payloads[key]))
    }
  } catch { /* Invalid or unavailable session storage starts a fresh history. */ }
  const currentVersions = (): Record<ConfigKey, number> => Object.fromEntries(keys.map(key => [key, snapshots.filter(item => item.key === key).at(-1)?.version ?? 0])) as Record<ConfigKey, number>

  function commit(nextSnapshots: Snapshot[], nextLogs: Log[], changedKey?: ConfigKey, changedPayload?: unknown) {
    const payloads = Object.fromEntries(keys.map(key => [key, key === changedKey ? changedPayload : read(key)]))
    try { globalThis.sessionStorage?.setItem(storageKey, JSON.stringify({ snapshots: nextSnapshots, logs: nextLogs, payloads })) }
    catch { throw new Error('浏览器无法保存演示状态，请允许会话存储后重试') }
    if (changedKey) write(changedKey, structuredClone(changedPayload))
    snapshots = nextSnapshots; logs = nextLogs
    nextSnapshot = snapshots.length; nextLog = logs.length
  }

  function log(action: string, targetId: string, detail: unknown): Log {
    return { id: String(nextLog + 1), adminId, username, action, targetId, detail: structuredClone(detail), createdAt: new Date().toISOString() }
  }

  function audit(action: string, targetId: string, detail: unknown) {
    commit(snapshots, [...logs, log(action, targetId, detail)])
  }

  function snapshot(key: ConfigKey, payload: unknown, action: Snapshot['action'], version: number, id: number, sourceVersion: number | null = null): Snapshot {
    return { id: `DEMO_VER_${id}`, key, version, action, sourceVersion, username, createdAt: new Date().toISOString(), payload: structuredClone(payload) }
  }

  function save(value: unknown, payload: unknown) {
    const key = configKey(value), clean = validate(key, structuredClone(payload))
    const current = currentVersions()[key], next = [...snapshots]
    if (!current) next.push(snapshot(key, read(key), 'BASELINE', 1, nextSnapshot + 1))
    const item = snapshot(key, clean, 'SAVE', current ? current + 1 : 2, next.length + 1)
    commit([...next, item], [...logs, log('CONFIG_SAVE', key, { key, version: item.version })], key, clean)
    return structuredClone(clean)
  }

  function versions(data: { key?: string; limit?: number; offset?: number }) {
    if (data.key) configKey(data.key)
    const { limit, offset } = pagination(data)
    const items = snapshots.filter(item => !data.key || item.key === data.key).reverse()
    return structuredClone({ items: items.slice(offset, offset + limit), total: items.length, limit, offset, currentVersions: currentVersions() })
  }

  function rollback(data: { key: string; versionId: string; expectedVersion: number; reason: string }) {
    const key = configKey(data.key), reason = String(data.reason ?? '').trim()
    if (!reason || [...reason].length > 500) throw new Error('请填写回滚原因（最多500字）')
    if (!Number.isInteger(data.expectedVersion) || data.expectedVersion < 0) throw new Error('当前版本参数无效')
    if (currentVersions()[key] !== data.expectedVersion) throw new Error('配置已变化，请刷新版本列表后重试')
    const source = snapshots.find(item => item.id === data.versionId && item.key === key)
    if (!source) throw new Error('配置版本不存在')
    if (source.version === data.expectedVersion) throw new Error('该版本已是当前版本')
    const clean = validate(key, structuredClone(source.payload))
    const item = snapshot(key, clean, 'ROLLBACK', data.expectedVersion + 1, nextSnapshot + 1, source.version)
    commit([...snapshots, item], [...logs, log('CONFIG_ROLLBACK', key, { key, version: item.version, sourceVersion: source.version, reason })], key, clean)
    return structuredClone({ key, version: item.version, payload: clean })
  }

  function listAudit(data: { action?: string; username?: string; targetId?: string; startDate?: string; endDate?: string; limit?: number; offset?: number }) {
    const { limit, offset } = pagination(data), start = dateBoundary(data.startDate), end = dateBoundary(data.endDate)
    if (start !== undefined && end !== undefined && start > end) throw new Error('开始日期不能晚于结束日期')
    const items = logs.filter(item => (!data.action || item.action === data.action) && (!data.username || item.username.includes(data.username.trim())) && (!data.targetId || item.targetId.includes(data.targetId.trim())) && (start === undefined || Date.parse(item.createdAt) >= start) && (end === undefined || Date.parse(item.createdAt) < end + 86400000)).reverse()
    return structuredClone({ items: items.slice(offset, offset + limit).map(item => ({ ...item, detail: redactDetail(item.detail) })), total: items.length, limit, offset })
  }

  function compare(data: { key: string; fromVersion: number; toVersion: number }) {
    const key = configKey(data.key)
    if (![data.fromVersion, data.toVersion].every(value => Number.isInteger(value) && value > 0)) throw new Error('对比版本无效')
    const before = snapshots.find(item => item.key === key && item.version === data.fromVersion)
    const after = snapshots.find(item => item.key === key && item.version === data.toVersion)
    if (!before || !after) throw new Error('对比版本不存在或不属于此配置')
    return structuredClone({ key, fromVersion: data.fromVersion, toVersion: data.toVersion, before: before.payload, after: after.payload })
  }

  function exportAudit(data: Parameters<typeof listAudit>[0]) {
    const page = listAudit({ ...data, limit: 1, offset: 0 })
    if (page.total > 10000) throw new Error('筛选结果超过 10000 条日志，请缩小筛选范围后导出')
    const items: Log[] = []
    for (let offset = 0; offset < page.total; offset += 100) items.push(...listAudit({ ...data, limit: 100, offset }).items)
    const headers = ['日志 ID', '管理员 ID', '操作人', '操作类型', '目标 ID', '时间（北京时间）', '操作详情']
    const lines = items.map(item => [item.id, item.adminId, item.username, item.action, item.targetId,
      new Date(Date.parse(item.createdAt) + 8 * 3600000).toISOString().slice(0, 19).replace('T', ' '), JSON.stringify(item.detail)].map(value => csvCell(value)).join(','))
    const result = { filename: `操作日志_${new Date(Date.now() + 8 * 3600000).toISOString().slice(0, 19).replace(/[-:T]/g, '')}.csv`, count: items.length, content: '\uFEFF' + [headers.map(value => csvCell(value)).join(','), ...lines].join('\r\n') + '\r\n' }
    audit('AUDIT_EXPORT', '', { count: items.length })
    return result
  }

  return { compare, exportAudit, save, versions, rollback, audit, listAudit, setActor: (value: string, id = 'admin-demo') => { username = value; adminId = id }, login: (value: string, id = 'admin-demo') => { username = value; adminId = id; audit('ADMIN_LOGIN', id, {}) } }
}
