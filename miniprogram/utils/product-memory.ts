import type { Product } from './types'

export interface ProductMemory { id: string; name: string; cover: string; viewedAt: number }
export const HISTORY_LIMIT = 50
export const COMPARE_LIMIT = 4
export type MemoryKind = 'history' | 'compare'
const key = (kind: MemoryKind) => `chiji_${getApp<IAppOption>().globalData.demoMode ? 'demo' : 'live'}_product_${kind}`
export function readMemory(kind: MemoryKind): ProductMemory[] {
  let value: unknown
  try { value = wx.getStorageSync(key(kind)) } catch { return [] }
  if (!Array.isArray(value)) return []
  const ids = new Set<string>()
  return value.filter((item): item is ProductMemory => {
    if (!item || typeof item.id !== 'string' || !item.id || typeof item.name !== 'string' || typeof item.cover !== 'string' || !Number.isFinite(item.viewedAt) || ids.has(item.id)) return false
    ids.add(item.id); return true
  }).slice(0, kind === 'history' ? HISTORY_LIMIT : COMPARE_LIMIT)
}
const snapshot = (p: Product): ProductMemory => ({ id: p.id, name: p.name, cover: p.cover || '', viewedAt: Date.now() })
export function recordView(product: Product) {
  wx.setStorageSync(key('history'), [snapshot(product), ...readMemory('history').filter(p => p.id !== product.id)].slice(0, HISTORY_LIMIT))
}
export function toggleCompare(product: Product): boolean {
  const items = readMemory('compare')
  const exists = items.some(p => p.id === product.id)
  if (!exists && items.length >= COMPARE_LIMIT) throw new Error(`最多对比 ${COMPARE_LIMIT} 款，请先移除一款`)
  wx.setStorageSync(key('compare'), exists ? items.filter(p => p.id !== product.id) : [...items, snapshot(product)])
  return !exists
}
export function removeMemory(kind: MemoryKind, id: string) { wx.setStorageSync(key(kind), readMemory(kind).filter(p => p.id !== id)) }
export function clearMemory(kind: MemoryKind) { wx.setStorageSync(key(kind), []) }
