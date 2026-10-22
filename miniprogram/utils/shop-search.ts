import { available } from './product'
import type { Product } from './types'

export interface ShopFilters { saleMode?: string; stock?: string; minPrice?: number; maxPrice?: number }
export function validateShopFilters(d: ShopFilters) {
  if (d.saleMode && !['all', 'spot', 'presale'].includes(d.saleMode)) throw new Error('发货方式无效')
  if (d.stock && !['all', 'available', 'soldOut'].includes(d.stock)) throw new Error('库存筛选无效')
  for (const n of [d.minPrice, d.maxPrice]) if (n != null && (!Number.isSafeInteger(n) || n < 0 || n > 100000000)) throw new Error('价格区间无效')
  if (d.minPrice != null && d.maxPrice != null && d.minPrice > d.maxPrice) throw new Error('最低价格不能高于最高价格')
}
export function matchesShopFilters(p: Product, d: ShopFilters) {
  const price = p.skus.length ? Math.min(...p.skus.map(s => s.price)) : undefined
  return (!d.saleMode || d.saleMode === 'all' || p.saleMode === d.saleMode)
    && (!d.stock || d.stock === 'all' || available(p) === (d.stock === 'available'))
    && (d.minPrice == null || (price != null && price >= d.minPrice))
    && (d.maxPrice == null || (price != null && price <= d.maxPrice))
}
const historyKey = () => `chiji_${getApp<IAppOption>().globalData.demoMode ? 'demo' : 'live'}_search_history`
export function readSearchHistory(): string[] {
  try {
    const value: unknown = wx.getStorageSync(historyKey())
    return Array.isArray(value) ? [...new Set(value.filter((s): s is string => typeof s === 'string' && !!s.trim() && [...s].length <= 60).map(s => s.trim()))].slice(0, 10) : []
  } catch { return [] }
}
export function saveSearchHistory(items: string[]) {
  wx.setStorageSync(historyKey(), items.slice(0, 10))
  return items.slice(0, 10)
}
export function rememberSearch(word: string) {
  word = word.trim()
  return word ? saveSearchHistory([word, ...readSearchHistory().filter(s => s.toLowerCase() !== word.toLowerCase())]) : readSearchHistory()
}


export interface SavedShopSearch {
  category: string; query: string; sort: string; saleMode: string; stock: string; minPrice: string; maxPrice: string
}
export interface ShopSearchFavorite extends SavedShopSearch { id: string; label: string }
export const SEARCH_FAVORITE_LIMIT = 10
const favoritesKey = () => `chiji_${getApp<IAppOption>().globalData.demoMode ? 'demo' : 'live'}_search_favorites`
function normalizeSearch(value: SavedShopSearch): SavedShopSearch {
  if (!value || !['all', 'family', 'gift'].includes(value.category) || !['default', 'priceAsc', 'priceDesc', 'sales'].includes(value.sort)
    || typeof value.query !== 'string' || [...value.query.trim()].length > 60
    || !['all', 'spot', 'presale'].includes(value.saleMode) || !['all', 'available', 'soldOut'].includes(value.stock)) throw new Error('收藏条件无效')
  const prices = [value.minPrice, value.maxPrice].map(s => {
    if (typeof s !== 'string' || (s.trim() && !/^\d+(\.\d{1,2})?$/.test(s.trim()))) throw new Error('收藏价格无效')
    return s.trim() ? String(Number(s)) : ''
  })
  validateShopFilters({ saleMode: value.saleMode, stock: value.stock, minPrice: prices[0] ? Math.round(Number(prices[0]) * 100) : undefined, maxPrice: prices[1] ? Math.round(Number(prices[1]) * 100) : undefined })
  return { category: value.category, query: value.query.trim(), sort: value.sort, saleMode: value.saleMode, stock: value.stock, minPrice: prices[0], maxPrice: prices[1] }
}
function favorite(value: SavedShopSearch): ShopSearchFavorite {
  const v = normalizeSearch(value)
  const parts = [v.query ? `“${v.query}”` : '', v.category === 'family' ? '家庭装' : v.category === 'gift' ? '节礼装' : '',
    v.saleMode === 'spot' ? '现货' : v.saleMode === 'presale' ? '预售' : '', v.stock === 'available' ? '有货' : v.stock === 'soldOut' ? '售罄' : '',
    v.minPrice || v.maxPrice ? `¥${v.minPrice || '0'}至${v.maxPrice ? '¥' + v.maxPrice : '不限'}` : '',
    v.sort === 'priceAsc' ? '价格升序' : v.sort === 'priceDesc' ? '价格降序' : v.sort === 'sales' ? '销量优先' : ''].filter(Boolean)
  return { ...v, id: JSON.stringify(v), label: parts.join(' · ') || '全部商品 · 综合排序' }
}
export function readSearchFavorites(): ShopSearchFavorite[] {
  try {
    const values: unknown = wx.getStorageSync(favoritesKey())
    if (!Array.isArray(values)) return []
    const result: ShopSearchFavorite[] = [], seen = new Set<string>()
    for (const value of values) {
      try { const item = favorite(value); if (!seen.has(item.id)) { result.push(item); seen.add(item.id) } } catch { /* Ignore invalid stored entries. */ }
      if (result.length === SEARCH_FAVORITE_LIMIT) break
    }
    return result
  } catch { return [] }
}
export function saveSearchFavorite(value: SavedShopSearch): ShopSearchFavorite[] {
  const item = favorite(value), items = readSearchFavorites().filter(v => v.id !== item.id)
  if (items.length >= SEARCH_FAVORITE_LIMIT) throw new Error(`最多收藏 ${SEARCH_FAVORITE_LIMIT} 组，请先删除一组`)
  const result = [item, ...items]
  wx.setStorageSync(favoritesKey(), result)
  return result
}
export function removeSearchFavorite(id: string): ShopSearchFavorite[] {
  const result = readSearchFavorites().filter(v => v.id !== id)
  wx.setStorageSync(favoritesKey(), result)
  return result
}
