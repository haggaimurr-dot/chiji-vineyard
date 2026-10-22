import { reservationActions, reservationData, activityStatus, todayInChina } from '../../utils/activity'
import { defaultHome } from '../../data/home'
import { callApi } from '../../utils/api'
import { money } from '../../utils/format'
import { recommendProducts, preferenceKey, preferenceOptions } from '../../utils/home-features'
import type { Preference } from '../../utils/home-features'
import type { Article, Product, HomeConfig } from '../../utils/types'
import { openVineyardGame } from '../../utils/vineyard-entry'

const seasonStatus = (start: string, end: string, today: string) => today < start ? '即将上市' : today > end ? '本季结束' : '上市中'
const emptyHome: HomeConfig = { ...defaultHome, banners: [], productIds: [], articleIds: [], storyTitle: '', storyCopy: '', cooperationTitle: '', cooperationCopy: '' }

Page({
  data: {
    products: [] as Array<Product & { priceText: string; imageFailed: boolean }>,
    articles: [] as Array<Article & { imageFailed: boolean }>,
    demoMode: getApp<IAppOption>().globalData.demoMode,
    detailsExpanded: false,
    vineyardOpening: false,
    orchardImage: getApp<IAppOption>().globalData.demoMode ? '/assets/home/harvest-scene.jpg' : '',
    orchardImageFailed: false,
    orchardImageIllustration: getApp<IAppOption>().globalData.demoMode,
    bannerImageFailed: [] as boolean[],
    loading: true,
    home: emptyHome,
    bannerAvailable: [] as boolean[],
    publishedProductIds: [] as string[],
    publishedArticleIds: [] as string[],
    activities: [] as Array<NonNullable<HomeConfig['activities']>[number] & { status: string; available: boolean }>,
    announcements: [] as NonNullable<HomeConfig['announcements']>,
    calendar: [] as Array<NonNullable<HomeConfig['launchCalendar']>[number] & { status: string; available: boolean }>,
    calendarItems: [] as Array<NonNullable<HomeConfig['launchCalendar']>[number] & { status: string; available: boolean }>,
    months: ['全部', ...Array.from({ length: 12 }, (_, i) => `${i + 1}月`)],
    monthIndex: 0,
    error: '',
    requestVersion: 0,
    allProducts: [] as Product[],
    recommendations: [] as Array<Product & { reason: string; priceText: string; imageFailed: boolean }>,
    preferenceOptions,
    preference: 'all' as Preference,
    personalized: true,
    recommendationOffset: 0,
    ...reservationData,
  },
  onShow() { return this.loadHome() },
  toggleDetails() { this.setData({ detailsExpanded: !this.data.detailsExpanded }) },
  goLive() { wx.navigateTo({ url: '/pages/orchard-live/index' }) },
  activityNotices() { wx.navigateTo({ url: '/pages/activity-notices/index' }) },
  onHide() { this.data.requestVersion++ },
  onUnload() { this.data.requestVersion++ },
  onPullDownRefresh() { return this.loadHome().finally(() => wx.stopPullDownRefresh()) },
  async loadHome() {
    const version = this.data.requestVersion + 1
    this.setData({ loading: true, error: '', requestVersion: version })
    try {
      const [products, articles, home] = await Promise.all([
        callApi<Product[]>('product.list'),
        callApi<Article[]>('article.list'),
        callApi<HomeConfig>('home.get'),
      ])
      if (version !== this.data.requestVersion) return
      if (!Array.isArray(products) || !Array.isArray(articles) || !home || !Array.isArray(home.banners) || !Array.isArray(home.productIds) || !Array.isArray(home.articleIds)) throw new Error('首页内容暂时无法加载，请重试')
      const today = todayInChina()
      const available = (id: string) => products.some(p => p.id === id && p.skus.length > 0)
      const calendar = (home.launchCalendar || []).map(item => ({ ...item, status: seasonStatus(item.startDate, item.endDate, today), available: available(item.productId) }))
      this.setData({
        home,
        orchardImage: home.banners[0]?.image || (this.data.demoMode ? '/assets/home/harvest-scene.jpg' : ''),
        orchardImageFailed: false,
        orchardImageIllustration: this.data.demoMode || (home.banners[0]?.image || '').includes('/assets/home/harvest-scene.jpg'),
        bannerImageFailed: home.banners.map(() => false),
        publishedProductIds: products.filter(p => p.skus.length > 0).map(p => p.id),
        publishedArticleIds: articles.map(a => a.id),
        bannerAvailable: home.banners.map(b => b.target === 'product' ? available(b.targetId) : b.target === 'article' ? articles.some(a => a.id === b.targetId) : ['shop', 'classroom', 'cooperation'].includes(b.target)),
        activities: (home.activities || []).map(item => ({ ...item, status: activityStatus(item, today), available: available(item.productId) })),
        announcements: (home.announcements || []).filter(item => item.publishedAt <= today).sort((a, b) => b.publishedAt.localeCompare(a.publishedAt)),
        calendar,
        products: (home.productIds.length ? home.productIds.map(id=>products.find(p=>p.id===id)).filter((p): p is Product=>!!p) : products.slice(0,3)).filter(p=>p.skus.length>0).map((product) => ({ ...product, priceText: money(Math.min(...product.skus.map(s=>s.price))), imageFailed: false })),
        articles: (home.articleIds.length ? home.articleIds.map(id=>articles.find(a=>a.id===id)).filter((a): a is Article=>!!a) : articles.slice(0,2)).map(article=>({...article,imageFailed:false})),
      })
      this.filterCalendar()
      const settings = wx.getStorageSync(preferenceKey)
      this.setData({ allProducts: products, preference: ['all', 'family', 'gift'].includes(settings?.preference) ? settings.preference : 'all', personalized: settings?.enabled !== false, recommendationOffset: 0 })
      this.refreshRecommendations()
      this.refreshReservationActivities()
      this.setData({ loading: false })
      await this.loadReservations(version)
    } catch (error) {
      if (version !== this.data.requestVersion) return
      const message = (error as Error).message || '首页内容暂时无法加载，请重试'
      this.setData({ error: message, home: emptyHome, bannerAvailable: [], bannerImageFailed: [], publishedProductIds: [], publishedArticleIds: [], products: [], articles: [], activities: [], announcements: [], calendar: [], calendarItems: [], allProducts: [], recommendations: [], reservationActivities: [], activityReminders: [], unavailableReservations: [] })
      wx.showToast({ title: message, icon: 'none' })
    } finally {
      if (version === this.data.requestVersion) this.setData({ loading: false })
    }
  },
  imageError(event: WechatMiniprogram.BaseEvent) {
    const { kind, index, src, version } = event.currentTarget.dataset
    if (this.data.loading || this.data.error || Number(version) !== this.data.requestVersion) return
    if (kind === 'orchard') {
      if (this.data.orchardImage === src) this.setData({ orchardImageFailed: true })
      return
    }
    const i = Number(index)
    if (!Number.isInteger(i) || i < 0) return
    // Ignore failures belonging to a replaced image or an earlier refresh.
    if (kind === 'banner' && this.data.home.banners[i]?.image === src) this.setData({ [`bannerImageFailed[${i}]`]: true })
    if (kind === 'product' && this.data.products[i]?.cover === src) this.setData({ [`products[${i}].imageFailed`]: true })
    if (kind === 'article' && this.data.articles[i]?.cover === src) this.setData({ [`articles[${i}].imageFailed`]: true })
    if (kind === 'recommendation' && this.data.recommendations[i]?.cover === src) this.setData({ [`recommendations[${i}].imageFailed`]: true })
  },
  refreshRecommendations() {
    this.setData({ recommendations: recommendProducts(this.data.allProducts, this.data.preference, this.data.personalized, this.data.home.productIds, this.data.recommendationOffset).map(p => ({ ...p, priceText: money(Math.min(...p.skus.filter(s => s.stock > 0 && s.limitPerOrder > 0).map(s => s.price))), imageFailed: false })) })
  },
  choosePreference(event: WechatMiniprogram.BaseEvent) {
    const preference = event.currentTarget.dataset.id as Preference
    if (!['all', 'family', 'gift'].includes(preference) || !this.data.personalized) return
    this.setData({ preference, recommendationOffset: 0 })
    wx.setStorageSync(preferenceKey, { preference, enabled: this.data.personalized })
    this.refreshRecommendations()
  },
  togglePersonalization(event: WechatMiniprogram.SwitchChange) {
    this.setData({ personalized: event.detail.value, recommendationOffset: 0 })
    wx.setStorageSync(preferenceKey, { preference: this.data.preference, enabled: event.detail.value })
    this.refreshRecommendations()
  },
  nextRecommendations() {
    this.setData({ recommendationOffset: this.data.recommendationOffset + 1 })
    this.refreshRecommendations()
  },
  ...reservationActions,
  chooseMonth(event: WechatMiniprogram.BaseEvent) {
    this.setData({ monthIndex: Number(event.currentTarget.dataset.index) })
    this.filterCalendar()
  },
  filterCalendar() {
    const month = this.data.monthIndex
    this.setData({ calendarItems: this.data.calendar.filter(item => {
      if (!month) return true
      // Include every month touched by the date range, including ranges spanning years.
      const start = new Date(`${item.startDate}T00:00:00Z`), end = new Date(`${item.endDate}T00:00:00Z`)
      for (let year = start.getUTCFullYear(); year <= end.getUTCFullYear(); year++) {
        const first = new Date(Date.UTC(year, month - 1, 1)), last = new Date(Date.UTC(year, month, 0))
        if (first <= end && last >= start) return true
      }
      return false
    }) })
  },
  openAnnouncement(event: WechatMiniprogram.BaseEvent) {
    if (this.data.loading || this.data.error) return
    const item = this.data.announcements[Number(event.currentTarget.dataset.index)]
    if (item) wx.showModal({ title: item.title, content: `${item.publishedAt}\n\n${item.content}`, showCancel: false, confirmText: '知道了' })
  },
  openActivity(event: WechatMiniprogram.BaseEvent) {
    if (this.data.loading || this.data.error) return
    const item = this.data.activities[Number(event.currentTarget.dataset.index)]
    if (!item) return
    const query = item.id ? `id=${encodeURIComponent(item.id)}` : `index=${Number(event.currentTarget.dataset.index)}&title=${encodeURIComponent(item.title)}`
    wx.navigateTo({ url: `/pages/activity/index?${query}` })
  },
  openReservedActivity(event: WechatMiniprogram.BaseEvent) {
    const id = event.currentTarget.dataset.id as string
    if (this.data.loading || this.data.error || !this.data.home.activities?.some(a => a.id === id)) return
    wx.navigateTo({ url: `/pages/activity/index?id=${encodeURIComponent(id)}` })
  },
  openBanner(event: WechatMiniprogram.BaseEvent) {
    if (this.data.loading || this.data.error) return
    const index = Number(event.currentTarget.dataset.index)
    const b=this.data.home.banners[index]
    if (!b) return
    if (!this.data.bannerAvailable[index]) { wx.showToast({ title: '该内容已下架或暂不可用', icon: 'none' }); return }
    if(b.target==='product'||b.target==='article')wx.navigateTo({url:`/pages/${b.target}/index?id=${encodeURIComponent(b.targetId)}`})
    else if(['shop','classroom','cooperation'].includes(b.target))wx.switchTab({url:`/pages/${b.target}/index`})
  },
  openProduct(event: WechatMiniprogram.BaseEvent) {
    const id = event.currentTarget.dataset.id
    if (this.data.loading || this.data.error || !this.data.publishedProductIds.includes(id)) return
    wx.navigateTo({ url: `/pages/product/index?id=${encodeURIComponent(id)}` })
  },
  openArticle(event: WechatMiniprogram.BaseEvent) {
    const id = event.currentTarget.dataset.id
    if (this.data.loading || this.data.error || !this.data.publishedArticleIds.includes(id)) return
    wx.navigateTo({ url: `/pages/article/index?id=${encodeURIComponent(id)}` })
  },
  goShop() { wx.switchTab({ url: '/pages/shop/index' }) },
  goClassroom() { wx.switchTab({ url: '/pages/classroom/index' }) },
  goCooperation() { wx.switchTab({ url: '/pages/cooperation/index' }) },
  async goVineyardGame() {
    if (this.data.vineyardOpening) return
    this.setData({ vineyardOpening: true })
    try { await openVineyardGame('home') }
    finally { this.setData({ vineyardOpening: false }) }
  },
})

