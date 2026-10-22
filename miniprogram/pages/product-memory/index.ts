import { callApi } from '../../utils/api'
import { available, salesCount } from '../../utils/product'
import { money } from '../../utils/format'
import { readMemory, removeMemory, clearMemory, toggleCompare, MemoryKind, ProductMemory } from '../../utils/product-memory'
import type { Product } from '../../utils/types'

type Entry = ProductMemory & { product: Product | null; error: string; imageFailed: boolean; comparing: boolean; priceText: string; dateText: string }
const dateText = (time: number) => new Date(time + 8 * 3600000).toISOString().slice(0, 16).replace('T', ' ')
const facts = (p: Product) => [
  p.skus.length ? `¥${money(Math.min(...p.skus.map(s => s.price)))} 起` : '暂无规格',
  p.variety || '未提供', p.origin || '未提供',
  p.skus.map(s => `${s.name} · ${s.weightLabel} · ¥${money(s.price)}`).join('\n') || '暂无规格',
  available(p) ? '有货' : '暂时售罄',
  p.saleMode === 'presale' ? `预售 · ${p.expectedShipDate || '日期待定'} 起发货` : '现货',
  `${salesCount(p)} 件`, p.storage || '未提供', p.netWeightNote || '未提供',
]
const labels = ['最低规格价', '品种', '产地', '规格与价格', '库存状态', '发货方式', '销量', '保存说明', '净重说明']

Page({
  data: { kind: 'history' as MemoryKind, entries: [] as Entry[], rows: [] as Array<{ label: string; values: Array<{ id: string; text: string }> }>, loading: false, requestVersion: 0, compareCount: 0 },
  onLoad(options: Record<string, string>) {
    this.setData({ kind: options.kind === 'compare' ? 'compare' : 'history' })
    wx.setNavigationBarTitle({ title: this.data.kind === 'compare' ? '商品对比' : '浏览记录' })
  },
  onShow() { return this.load() },
  onHide() { this.setData({ requestVersion: this.data.requestVersion + 1 }) },
  onUnload() { this.setData({ requestVersion: this.data.requestVersion + 1 }) },
  onPullDownRefresh() { return this.load().finally(() => wx.stopPullDownRefresh()) },
  async load() {
    const version = this.data.requestVersion + 1
    const saved = readMemory(this.data.kind)
    this.setData({ loading: true, requestVersion: version, entries: [], rows: [], compareCount: readMemory('compare').length })
    const entries = await Promise.all(saved.map(async item => {
      let product: Product | null = null, error = ''
      try {
        product = await callApi<Product | null>('product.detail', { id: item.id })
        if (!product) error = '商品已下架或不存在'
      } catch (e) { error = (e as Error).message || '加载失败，请重试' }
      return { ...item, name: product?.name || item.name, cover: product?.cover || item.cover,
        product, error, imageFailed: false, comparing: readMemory('compare').some(p => p.id === item.id),
        priceText: product?.skus.length ? money(Math.min(...product.skus.map(s => s.price))) : '', dateText: dateText(item.viewedAt) }
    }))
    if (version !== this.data.requestVersion) return
    const rows = labels.map((label, i) => ({ label, values: entries.map(entry => ({ id: entry.id, text: entry.product ? facts(entry.product)[i] : '暂不可用' })) }))
    this.setData({ entries, rows, loading: false })
  },
  remove(event: WechatMiniprogram.BaseEvent) {
    try { removeMemory(this.data.kind, event.currentTarget.dataset.id); return this.load() }
    catch (e) { wx.showToast({ title: (e as Error).message || '删除失败，请重试', icon: 'none' }) }
  },
  clear() {
    wx.showModal({ title: this.data.kind === 'history' ? '清空浏览记录' : '清空商品对比', content: '清空后无法恢复，是否继续？', success: result => {
      if (!result.confirm) return
      try { clearMemory(this.data.kind); this.load() }
      catch { wx.showToast({ title: '清空失败，请重试', icon: 'none' }) }
    } })
  },
  toggleCompare(event: WechatMiniprogram.BaseEvent) {
    const index = this.data.entries.findIndex(p => p.id === event.currentTarget.dataset.id)
    const entry = this.data.entries[index]
    if (!entry?.product) return
    try {
      const comparing = toggleCompare(entry.product)
      this.setData({ [`entries[${index}].comparing`]: comparing, compareCount: readMemory('compare').length })
    } catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
  },
  imageError(event: WechatMiniprogram.BaseEvent) {
    const index = this.data.entries.findIndex(p => p.id === event.currentTarget.dataset.id)
    if (index >= 0) this.setData({ [`entries[${index}].imageFailed`]: true })
  },
  openProduct(event: WechatMiniprogram.BaseEvent) {
    const entry = this.data.entries.find(p => p.id === event.currentTarget.dataset.id)
    if (entry?.product) wx.navigateTo({ url: `/pages/product/index?id=${encodeURIComponent(entry.id)}` })
  },
  openCompare() { wx.navigateTo({ url: '/pages/product-memory/index?kind=compare' }) },
  goShop() { wx.switchTab({ url: '/pages/shop/index' }) },
})
