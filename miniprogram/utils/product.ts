import type { Product, Sku } from './types'

export const purchaseLimit = (sku?: Sku) => sku ? Math.max(0, Math.min(sku.stock, sku.limitPerOrder)) : 0
export const available = (product: Product) => product.skus.some(sku => purchaseLimit(sku) > 0)
export const salesCount = (product: Product) => product.skus.reduce((sum, sku) => sum + Math.max(0, sku.sold || 0), 0)
export type ProductSort = 'default' | 'priceAsc' | 'priceDesc' | 'sales'
export function sortProducts(products: Product[], sort: ProductSort) {
  const price = (p: Product) => p.skus.length ? Math.min(...p.skus.map(s => s.price)) : Infinity
  return [...products].sort((a, b) => {
    if (sort === 'sales') return salesCount(b) - salesCount(a)
    if (sort === 'priceAsc' || sort === 'priceDesc') {
      if (!a.skus.length || !b.skus.length) return Number(!a.skus.length) - Number(!b.skus.length)
      return sort === 'priceAsc' ? price(a) - price(b) : price(b) - price(a)
    }
    return 0
  })
}
export function filterProducts(products: Product[], category: string, query: string) {
  const words = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean)
  return products.filter(product => {
    const text = [product.name, product.subtitle, product.origin, ...product.badges, ...product.skus.map(s => `${s.name} ${s.weightLabel}`)].join(' ').toLocaleLowerCase()
    return (category === 'all' || product.category === category) && words.every(word => text.includes(word))
  })
}
