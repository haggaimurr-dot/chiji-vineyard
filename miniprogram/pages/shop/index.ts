import { readMemory, toggleCompare } from '../../utils/product-memory'
import { callApi } from '../../utils/api'
import { money } from '../../utils/format'
import { available, salesCount, ProductSort } from '../../utils/product'
import type { Product, ProductSearchResult } from '../../utils/types'
import { readSearchHistory, rememberSearch, saveSearchHistory, validateShopFilters, readSearchFavorites, saveSearchFavorite, removeSearchFavorite, ShopSearchFavorite } from '../../utils/shop-search'

type ProductCard = Product & { priceText: string; soldOut: boolean; imageFailed: boolean; comparing: boolean }

Page({
  searchTimer: undefined as ReturnType<typeof setTimeout> | undefined,
  suggestionVersion: 0,
  data: {
    searchFavorites: [] as ShopSearchFavorite[],
    filtersOpen: false, saleMode: 'all', stock: 'all', minPrice: '', maxPrice: '',
    draftSaleMode: 'all', draftStock: 'all', draftMinPrice: '', draftMaxPrice: '', filterCount: 0,
    searchHistory: [] as string[], hotSearches: [] as string[], suggestionsOpen: false, hotLoading: false, hotError: '',
    category: 'all', query: '', products: [] as ProductCard[],
    loading: true, loadingMore: false, error: '', moreError: '', requestVersion: 0,
    offset: 0, total: 0, hasMore: false, sort: 'default' as ProductSort, attentionBusy: false, compareCount: 0,
  },
  onShow() { this.setData({ compareCount: readMemory('compare').length, searchHistory: readSearchHistory(), searchFavorites: readSearchFavorites() }); return this.loadProducts() },
  onHide() { this.suggestionVersion++; this.cancelSearch(); this.setData({ requestVersion: this.data.requestVersion + 1, loadingMore: false, hotLoading: false }) },
  onUnload() { this.suggestionVersion++; this.cancelSearch(); this.setData({ requestVersion: this.data.requestVersion + 1 }) },
  onPullDownRefresh() { return this.loadProducts().finally(() => wx.stopPullDownRefresh()) },
  onReachBottom() { return this.loadMore() },
  cancelSearch() { if (this.searchTimer !== undefined) clearTimeout(this.searchTimer); this.searchTimer = undefined },
  loadMore() {
    if (this.data.hasMore && !this.data.loading && !this.data.loadingMore && !this.data.moreError) return this.loadProducts(true)
  },
  retryMore() { return this.loadProducts(true) },
  async loadProducts(more: unknown = false) {
    // bindtap passes an event, so only a literal true means append.
    more = more === true
    if (more && (this.data.loading || this.data.loadingMore || !this.data.hasMore)) return
    this.cancelSearch()
    const version = this.data.requestVersion + 1, offset = more ? this.data.offset : 0
    this.setData({ requestVersion: version, error: '', moreError: '', loading: !more, loadingMore: !!more,
      ...(more ? {} : { products: [], offset: 0, total: 0, hasMore: false }) })
    try {
      const result = await callApi<ProductSearchResult>('product.search', {
        category: this.data.category, keyword: this.data.query.trim(), sort: this.data.sort, offset, limit: 20,
        saleMode: this.data.saleMode, stock: this.data.stock,
        ...(this.data.minPrice === '' ? {} : { minPrice: Math.round(Number(this.data.minPrice) * 100) }),
        ...(this.data.maxPrice === '' ? {} : { maxPrice: Math.round(Number(this.data.maxPrice) * 100) }),
      })
      if (version !== this.data.requestVersion) return
      if (!result || !Array.isArray(result.items) || !Number.isInteger(result.total) || result.total < 0 || result.offset !== offset) throw new Error('商品信息暂时无法加载')
      const existing = new Set(more ? this.data.products.map(p => p.id) : [])
      const cards = result.items.filter(p => {
        if (existing.has(p.id)) return false
        existing.add(p.id); return true
      }).map(p => ({ ...p, sales: salesCount(p), priceText: p.skus.length ? money(Math.min(...p.skus.map(s => s.price))) : '', soldOut: !available(p), imageFailed: false, comparing: readMemory('compare').some(item => item.id === p.id) }))
      const nextOffset = offset + result.items.length
      const updates: Record<string, unknown> = { offset: nextOffset, total: result.total, hasMore: nextOffset < result.total && result.items.length > 0 }
      // Append only new cards across the native bridge.
      if (more) cards.forEach((p, i) => { updates[`products[${this.data.products.length + i}]`] = p })
      else updates.products = cards
      this.setData(updates)
    } catch (error) {
      if (version === this.data.requestVersion) this.setData({ [more ? 'moreError' : 'error']: (error as Error).message || '网络开小差了，请稍后重试' })
    } finally {
      if (version === this.data.requestVersion) this.setData({ loading: false, loadingMore: false })
    }
  },
  chooseSort(event: WechatMiniprogram.BaseEvent) {
    const value = event.currentTarget.dataset.value
    this.setData({ sort: value === 'price' ? (this.data.sort === 'priceAsc' ? 'priceDesc' : 'priceAsc') : value })
    return this.loadProducts()
  },
  async toggleAttention(event: WechatMiniprogram.BaseEvent) {
    if (this.data.attentionBusy) return
    const { id, kind } = event.currentTarget.dataset
    const p = this.data.products.find(p => p.id === id)
    if (!p) return
    const field = kind === 'favorite' ? 'favorite' : 'restockReminder'
    const enabled = !p[field], version = this.data.requestVersion
    this.setData({ attentionBusy: true })
    try {
      await callApi('product.attention.set', { productId: id, kind, enabled })
      const index = this.data.products.findIndex(p => p.id === id)
      if (version === this.data.requestVersion && index >= 0) this.setData({ [`products[${index}].${field}`]: enabled })
      wx.showToast({ title: enabled ? (kind === 'favorite' ? '已收藏' : '已设置到货提醒') : '已取消', icon: 'success' })
    } catch (error) { wx.showToast({ title: (error as Error).message, icon: 'none' }) }
    finally { this.setData({ attentionBusy: false }) }
  },
  search(event: WechatMiniprogram.Input) {
    this.cancelSearch()
    // Invalidate outstanding responses before the debounce expires.
    this.setData({ query: event.detail.value, requestVersion: this.data.requestVersion + 1, products: [], loading: true, loadingMore: false, error: '', moreError: '', hasMore: false, total: 0, offset: 0 })
    this.searchTimer = setTimeout(() => { this.searchTimer = undefined; this.loadProducts() }, 300)
  },
  submitSearch() {
    try { this.setData({ searchHistory: rememberSearch(this.data.query) }) }
    catch { wx.showToast({ title: '搜索历史未能保存', icon: 'none' }) }
    this.setData({ suggestionsOpen: false })
    return this.loadProducts()
  },
  async showSuggestions() {
    this.setData({ suggestionsOpen: true, searchHistory: readSearchHistory() })
    if (!this.data.hotLoading) return this.loadHotSearches()
  },
  closeSuggestions() { this.setData({ suggestionsOpen: false }) },
  async loadHotSearches() {
    const version = ++this.suggestionVersion
    this.setData({ hotLoading: true, hotError: '' })
    try {
      const result = await callApi<ProductSearchResult>('product.search', { category: 'all', sort: 'sales', offset: 0, limit: 6 })
      if (version === this.suggestionVersion) this.setData({ hotSearches: [...new Set(result.items.map(p => p.name.trim()).filter(s => s && [...s].length <= 60))] })
    } catch { if (version === this.suggestionVersion) this.setData({ hotError: '推荐搜索词暂未加载，请重试' }) }
    finally { if (version === this.suggestionVersion) this.setData({ hotLoading: false }) }
  },
  chooseSearch(event: WechatMiniprogram.BaseEvent) { this.setData({ query: event.currentTarget.dataset.word }); return this.submitSearch() },
  removeSearchHistory(event: WechatMiniprogram.BaseEvent) {
    try { this.setData({ searchHistory: saveSearchHistory(readSearchHistory().filter(s => s !== event.currentTarget.dataset.word)) }) }
    catch { wx.showToast({ title: '搜索历史未能删除', icon: 'none' }) }
  },
  clearSearchHistory() {
    wx.showModal({ title: '清空搜索历史', content: '清空当前设备上的全部搜索历史？', success: result => {
      if (!result.confirm) return
      try { this.setData({ searchHistory: saveSearchHistory([]) }) }
      catch { wx.showToast({ title: '搜索历史未能清空', icon: 'none' }) }
    } })
  },
  saveFilterFavorite() {
    try {
      this.setData({ searchFavorites: saveSearchFavorite(this.data) })
      wx.showToast({ title: '已收藏当前组合', icon: 'success' })
    } catch (e) { wx.showToast({ title: (e as Error).message || '组合收藏未能保存', icon: 'none' }) }
  },
  applyFilterFavorite(event: WechatMiniprogram.BaseEvent) {
    const item = readSearchFavorites().find(v => v.id === event.currentTarget.dataset.id)
    if (!item) { this.setData({ searchFavorites: readSearchFavorites() }); return }
    const { category, query, sort, saleMode, stock, minPrice, maxPrice } = item
    this.setData({ category, query, sort: sort as ProductSort, saleMode, stock, minPrice, maxPrice, filtersOpen: false, suggestionsOpen: false,
      filterCount: Number(saleMode !== 'all') + Number(stock !== 'all') + Number(!!minPrice || !!maxPrice) })
    return this.loadProducts()
  },
  deleteFilterFavorite(event: WechatMiniprogram.BaseEvent) {
    try { this.setData({ searchFavorites: removeSearchFavorite(event.currentTarget.dataset.id) }) }
    catch { wx.showToast({ title: '组合收藏未能删除', icon: 'none' }) }
  },
  openFilters() { this.setData({ filtersOpen: true, draftSaleMode: this.data.saleMode, draftStock: this.data.stock, draftMinPrice: this.data.minPrice, draftMaxPrice: this.data.maxPrice }) },
  closeFilters() { this.setData({ filtersOpen: false }) },
  chooseDraftFilter(event: WechatMiniprogram.BaseEvent) {
    const { field, value } = event.currentTarget.dataset
    if (field === 'draftSaleMode' || field === 'draftStock') this.setData({ [field]: value })
  },
  inputPrice(event: WechatMiniprogram.Input) {
    const field = event.currentTarget.dataset.field
    if (field === 'draftMinPrice' || field === 'draftMaxPrice') this.setData({ [field]: event.detail.value })
  },
  resetDraftFilters() { this.setData({ draftSaleMode: 'all', draftStock: 'all', draftMinPrice: '', draftMaxPrice: '' }) },
  applyFilters() {
    const min = this.data.draftMinPrice.trim(), max = this.data.draftMaxPrice.trim()
    try {
      if ([min, max].some(s => s && !/^\d+(\.\d{1,2})?$/.test(s))) throw new Error('价格请输入最多两位小数的非负金额')
      validateShopFilters({ saleMode: this.data.draftSaleMode, stock: this.data.draftStock, minPrice: min ? Math.round(Number(min) * 100) : undefined, maxPrice: max ? Math.round(Number(max) * 100) : undefined })
    } catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }); return }
    this.setData({ saleMode: this.data.draftSaleMode, stock: this.data.draftStock, minPrice: min, maxPrice: max, filtersOpen: false,
      filterCount: Number(this.data.draftSaleMode !== 'all') + Number(this.data.draftStock !== 'all') + Number(!!min || !!max) })
    return this.loadProducts()
  },
  clearSearch() { this.setData({ query: '' }); return this.loadProducts() },
  resetFilters() { this.setData({ query: '', category: 'all', sort: 'default', saleMode: 'all', stock: 'all', minPrice: '', maxPrice: '', filterCount: 0, filtersOpen: false }); return this.loadProducts() },
  chooseCategory(event: WechatMiniprogram.BaseEvent) { this.setData({ category: event.currentTarget.dataset.value }); return this.loadProducts() },
  imageError(event: WechatMiniprogram.BaseEvent) {
    const index = this.data.products.findIndex(p => p.id === event.currentTarget.dataset.id)
    if (index >= 0) this.setData({ [`products[${index}].imageFailed`]: true })
  },
  openProduct(event: WechatMiniprogram.BaseEvent) { wx.navigateTo({ url: `/pages/product/index?id=${encodeURIComponent(event.currentTarget.dataset.id)}` }) },
  toggleCompare(event: WechatMiniprogram.BaseEvent) {
    const index = this.data.products.findIndex(p => p.id === event.currentTarget.dataset.id)
    if (index < 0) return
    try {
      const comparing = toggleCompare(this.data.products[index])
      this.setData({ [`products[${index}].comparing`]: comparing, compareCount: readMemory('compare').length })
    } catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
  },
  openHistory() { wx.navigateTo({ url: '/pages/product-memory/index?kind=history' }) },
  openCompare() { wx.navigateTo({ url: '/pages/product-memory/index?kind=compare' }) },
  openCart() { wx.navigateTo({ url: '/pages/cart/index' }) },
})
