import { callApi } from '../../utils/api'
import { learningTime } from '../../utils/articles'
import type { Article } from '../../utils/types'
import { questionTimes, type Course, type CourseResult, type ClassroomQuestion } from '../../utils/classroom'

Page({
  data: {
    stages: ['全部', '采后管理', '转色成熟', '病虫防治'], stage: '全部',
    varieties: ['全部', '通用', '阳光玫瑰', '巨峰'], variety: '全部', articles: [] as Article[],
    views: [{ value: 'series', label: '系列课程' }, { value: 'progress', label: '学习进度' }, { value: 'questions', label: '课堂问答' }, { value: 'all', label: '全部内容' }, { value: 'favorite', label: '我的收藏' }, { value: 'history', label: '学习记录' }],
    view: 'all', keyword: '', loading: true, loadingMore: false, hasMore: false,
    courses: [] as Course[], questions: [] as ClassroomQuestion[], totalLessons: 0, completedLessons: 0, progressPercent: 0, expandedCourse: '',
    error: '', moreError: '', favoriteBusy: false, requestVersion: 0,
  },
  searchTimer: undefined as ReturnType<typeof setTimeout> | undefined,
  onShow() {
    const view = wx.getStorageSync('chiji_classroom_view')
    if (view === 'favorite' || view === 'history') { this.setData({ view, keyword: '', stage: '全部', variety: '全部' }); wx.removeStorageSync('chiji_classroom_view') }
    return this.load()
  },
  onHide() { clearTimeout(this.searchTimer); this.setData({ requestVersion: this.data.requestVersion + 1, loadingMore: false }) },
  onUnload() { clearTimeout(this.searchTimer); this.data.requestVersion++ },
  onPullDownRefresh() { return this.load().finally(() => wx.stopPullDownRefresh()) },
  onReachBottom() { return this.loadMore() },
  async load(append = false) {
    clearTimeout(this.searchTimer)
    if (['series', 'progress', 'questions'].includes(this.data.view)) return this.loadLearning(append)
    if (append && (this.data.loading || this.data.loadingMore || !this.data.hasMore)) return
    const version = append ? this.data.requestVersion : this.data.requestVersion + 1
    const offset = append ? this.data.articles.length : 0
    this.setData(append ? { loadingMore: true, moreError: '' } : { requestVersion: version, loading: true, loadingMore: false, error: '', moreError: '', articles: [], hasMore: false })
    try {
      const result = await callApi<{ articles: Article[]; hasMore: boolean }>('article.search', {
        stage: this.data.stage, variety: this.data.variety, keyword: this.data.keyword.trim(), view: this.data.view, offset, limit: 20,
      })
      if (version !== this.data.requestVersion) return
      const articles = result.articles.map(article => ({ ...article, lastReadText: learningTime(article.lastReadAt) }))
      this.setData({ articles: append ? [...this.data.articles, ...articles] : articles, hasMore: result.hasMore })
    } catch (error) {
      if (version === this.data.requestVersion) this.setData(append ? { moreError: (error as Error).message } : { error: (error as Error).message })
    } finally { if (version === this.data.requestVersion) this.setData({ loading: false, loadingMore: false }) }
  },
  async loadLearning(append = false) {
    if (append && (this.data.view !== 'questions' || this.data.loading || this.data.loadingMore || !this.data.hasMore)) return
    const version = append ? this.data.requestVersion : this.data.requestVersion + 1
    this.setData(append ? { loadingMore: true, moreError: '' } : { requestVersion: version, loading: true, loadingMore: false, error: '', moreError: '', hasMore: false, questions: [], courses: [] })
    try {
      if (this.data.view === 'questions') {
        const result = await callApi<{ items: ClassroomQuestion[]; hasMore: boolean }>('classroom.questions', { offset: append ? this.data.questions.length : 0, limit: 20 })
        if (version !== this.data.requestVersion) return
        const questions = result.items.map(questionTimes)
        this.setData({ questions: append ? [...this.data.questions, ...questions] : questions, hasMore: result.hasMore })
      } else {
        const result = await callApi<CourseResult>('classroom.courses')
        if (version !== this.data.requestVersion) return
        this.setData({ courses: result.courses, totalLessons: result.total, completedLessons: result.completed, progressPercent: result.total ? Math.floor(result.completed * 100 / result.total) : 0 })
      }
    } catch (error) {
      if (version === this.data.requestVersion) this.setData(append ? { moreError: (error as Error).message } : { error: (error as Error).message })
    } finally { if (version === this.data.requestVersion) this.setData({ loading: false, loadingMore: false }) }
  },
  expandCourse(event: WechatMiniprogram.BaseEvent) { const id = event.currentTarget.dataset.id; this.setData({ expandedCourse: this.data.expandedCourse === id ? '' : id }) },
  loadMore() { return this.load(true) },
  retry() { return this.load() },
  onSearch(event: WechatMiniprogram.Input) {
    clearTimeout(this.searchTimer)
    this.setData({ keyword: event.detail.value, requestVersion: this.data.requestVersion + 1, loading: true, loadingMore: false, articles: [], error: '', moreError: '', hasMore: false })
    this.searchTimer = setTimeout(() => this.load(), 300)
  },
  confirmSearch() { return this.load() },
  clearSearch() { this.setData({ keyword: '' }); return this.load() },
  pickView(event: WechatMiniprogram.BaseEvent) { this.setData({ view: event.currentTarget.dataset.value }); return this.load() },
  pickStage(event: WechatMiniprogram.BaseEvent) { this.setData({ stage: event.currentTarget.dataset.value }); return this.load() },
  onVariety(event: WechatMiniprogram.PickerChange) { this.setData({ variety: this.data.varieties[Number(event.detail.value)] }); return this.load() },
  async toggleFavorite(event: WechatMiniprogram.BaseEvent) {
    if (this.data.favoriteBusy || this.data.loading || this.data.loadingMore) return
    const id = event.currentTarget.dataset.id
    const article = this.data.articles.find(a => a.id === id)
    if (!article) return
    const enabled = !article.favorite
    this.setData({ favoriteBusy: true })
    try {
      await callApi('article.favorite.set', { articleId: id, enabled })
      // Refresh the current query so older fetches cannot restore stale flags,
      // and removing a favorite keeps pagination offsets consistent.
      await this.load()
      wx.showToast({ title: enabled ? '已收藏' : '已取消收藏', icon: 'success' })
    } catch (error) { wx.showToast({ title: (error as Error).message, icon: 'none' }) }
    finally { this.setData({ favoriteBusy: false }) }
  },
  open(event: WechatMiniprogram.BaseEvent) { wx.navigateTo({ url: `/pages/article/index?id=${encodeURIComponent(event.currentTarget.dataset.id)}` }) },
})
