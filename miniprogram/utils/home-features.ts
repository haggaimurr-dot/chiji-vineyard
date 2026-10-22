import type { Product } from './types'

export type Preference = 'all' | 'family' | 'gift'
export const preferenceKey = 'chiji_home_preferences'
export const demoReservationKey = 'chiji_demo_activity_reservations'
export const preferenceOptions = [{ id: 'all', label: '随心挑选' }, { id: 'family', label: '家庭自享' }, { id: 'gift', label: '送礼分享' }]

export function recommendProducts(products: Product[], preference: Preference, enabled: boolean, featuredIDs: string[], offset = 0) {
 const favorites = products.filter(p => p.favorite)
 const categories = new Set(favorites.map(p => p.category))
 const score = (p: Product) => !enabled ? 0 :
   (preference !== 'all' && p.category === preference ? 8 : 0) + (p.favorite ? 4 : categories.has(p.category) ? 2 : 0)
 const featuredRank = (id: string) => featuredIDs.includes(id) ? featuredIDs.indexOf(id) : featuredIDs.length
 const ranked = products.filter(p => p.skus.some(s => s.stock > 0 && s.limitPerOrder > 0))
   .map((p, index) => ({ p, index, score: score(p) }))
   .sort((a, b) => b.score - a.score || featuredRank(a.p.id) - featuredRank(b.p.id) || a.index - b.index)
 if (!ranked.length) return []
 const start = ((offset % ranked.length) + ranked.length) % ranked.length
 return [...ranked.slice(start), ...ranked.slice(0, start)].slice(0, 3).map(({ p }) => ({
   ...p, reason: enabled && preference !== 'all' && p.category === preference ? '符合你的购买偏好' : enabled && p.favorite ? '你收藏的鲜果' : enabled && categories.has(p.category) ? '与你的收藏同类' : '当季在售推荐',
 }))
}
