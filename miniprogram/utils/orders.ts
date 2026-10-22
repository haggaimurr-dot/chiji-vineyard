import { callApi } from './api'
import { purchaseLimit } from './product'
import type { CartItem, Order, Product } from './types'

export async function buyAgain(order: Order) {
  const products = new Map<string, Product | null>()
  for (const id of [...new Set(order.items.map(item => item.productId))]) {
    products.set(id, await callApi<Product | null>('product.detail', { id }))
  }
  const items: CartItem[] = [], changes: string[] = []
  for (const old of order.items) {
    const product = products.get(old.productId)
    const sku = product?.skus.find(s => s.id === old.skuId)
    const max = purchaseLimit(sku)
    const label = old.product?.name || old.name || '商品'
    if (!product || !sku || max < 1) { changes.push(`${label}：已下架或售罄，已跳过`); continue }
    const quantity = Math.min(old.quantity, max)
    if (quantity !== old.quantity) changes.push(`${label}：受库存或限购影响，调整为 ${quantity} 件`)
    items.push({ productId: product.id, skuId: sku.id, quantity, selected: true })
  }
  if (!items.length) throw new Error('原订单商品暂时均无法购买，请到商城选购')
  if (changes.length) {
    const result = await wx.showModal({ title: '购买内容有变化', content: `${changes.join('\n')}\n商品按当前价格结算。`, confirmText: '继续购买' })
    if (!result.confirm) return
  }
  wx.setStorageSync('chiji_checkout_items', items)
  wx.navigateTo({ url: '/pages/checkout/index' })
}


