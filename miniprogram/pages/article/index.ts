import { callApi } from '../../utils/api'
import { articleReadEventId } from '../../utils/articles'
import type { Article } from '../../utils/types'
import { questionTimes, type ClassroomQuestion } from '../../utils/classroom'

Page({
  data: {
    article: null as Article | null, articleId: '',
    loading: true, error: '', coverFailed: false,
    favoriteBusy: false,
    completeBusy: false, questionDraft: '', questionBusy: false, questionError: '', questions: [] as ClassroomQuestion[], questionsLoading: false, questionsError: '', questionsMore: false,
    contentHtml: '', paragraphs: [] as string[],
    videoPosition: 0, videoProgressError: '', videoUrl: '', videoError: '', videoLoading: false, videoAttempt: 0,
  },
  loadRequest: 0,
  videoRequest: 0,
  videoSample: null as { articleId: string; source: string; position: number; duration: number } | null,
  videoSaveBusy: false,
  videoLastSave: 0,
  readEventId: '',
  readBusy: false,
  readRecorded: false,
  readRendered: false,
  visible: false,
  disposed: false,
  questionEventId: '',
  questionRequest: 0,
  onLoad(options: Record<string, string>) {
    this.readEventId = articleReadEventId()
    this.setData({ articleId: (options.id || '').trim() })
    return this.loadArticle()
  },
  onShow() { this.visible = true; if (this.data.article) void this.loadQuestions(); return this.recordRead() },
  onHide() { this.visible = false; void this.saveVideoProgress() },
  async loadArticle() {
    const request = ++this.loadRequest
    ++this.videoRequest
    this.readRendered = false
    this.setData({ loading: true, error: '', article: null, videoPosition: 0, videoProgressError: '', videoUrl: '', videoError: '', videoLoading: false })
    try {
      if (!this.data.articleId) throw new Error('内容链接无效，请返回种植课堂重新选择')
      const article = await callApi<Article | null>('article.detail', { id: this.data.articleId })
      if (this.disposed || request !== this.loadRequest) return
      if (!article) throw new Error('内容不存在或已下架，请看看其他课堂内容')
      const contentHtml = typeof article.contentHtml === 'string' ? article.contentHtml.trim() : ''
      await new Promise<void>(resolve => this.setData({
        article, contentHtml, paragraphs: contentHtml ? [] : (Array.isArray(article.content) ? article.content : []),
        coverFailed: !article.cover, loading: false,
      }, resolve))
      if (this.disposed || request !== this.loadRequest) return
      this.readRendered = true
      wx.setNavigationBarTitle({ title: article.title })
      await Promise.all([this.loadVideo(), this.recordRead(), this.loadQuestions()])
    } catch (error) {
      if (request === this.loadRequest) this.setData({ article: null, error: (error as Error).message || '内容加载失败，请稍后重试' })
    } finally {
      if (request === this.loadRequest) this.setData({ loading: false })
    }
  },
  goProduct(event: WechatMiniprogram.BaseEvent) {
    const id = event.currentTarget.dataset.id as string
    if (id) wx.navigateTo({ url: '/pages/product/index?id=' + encodeURIComponent(id) })
  },
  async recordRead() {
    if (!this.visible || this.disposed || !this.readRendered || !this.data.article || this.readRecorded || this.readBusy) return
    const articleId = this.data.article.id
    this.readBusy = true
    try {
      await callApi<{ recorded: boolean }>('article.read', { articleId, eventId: this.readEventId })
      this.readRecorded = true
      // A retry may have been accepted before its response was lost. Fetch counters
      // so both newly accepted and deduplicated requests display the actual totals.
      const latest = await callApi<Article | null>('article.detail', { id: articleId })
      if (!this.disposed && latest && this.data.article?.id === articleId) {
        this.setData({ article: { ...this.data.article, readCount: latest.readCount, readerCount: latest.readerCount } })
      }
    } catch (_) {
      // Keep the content readable; returning to the page retries the same event ID.
    } finally { this.readBusy = false }
  },
  async loadVideo() {
    const article = this.data.article
    if (!article || article.type !== 'video') return
    const request = ++this.videoRequest
    const source = (article.videoUrl || '').trim()
    this.setData({ videoPosition: 0, videoProgressError: '', videoUrl: '', videoError: '', videoLoading: !!source, videoAttempt: request })
    if (!source) {
      this.setData({ videoError: '此内容暂未提供视频，可先阅读下方正文', videoLoading: false })
      return
    }
    try {
      let videoUrl = source
      if (source.startsWith('cloud://')) {
        const result = await wx.cloud.getTempFileURL({ fileList: [source] })
        if (request !== this.videoRequest) return
        const file = result.fileList[0]
        if (!file || file.status !== 0 || !file.tempFileURL) throw new Error('视频地址暂时无法获取，请稍后重试')
        videoUrl = file.tempFileURL
      }
      if (!/^https:\/\/[^\s/?#]+(?:[/?#][^\s]*)?$/i.test(videoUrl)) throw new Error('视频地址无效，请联系商家更新内容')
      let position = 0
      try {
        const progress = await callApi<{position: number; duration: number}>('classroom.video.get', {articleId: article.id})
        position = progress.position < progress.duration - 1 ? progress.position : 0
      } catch (_) { if(request===this.videoRequest) this.setData({videoProgressError:'暂时无法读取播放进度，视频仍可播放'}) }
      if (request === this.videoRequest) this.setData({ videoUrl, videoPosition: position, videoLoading: false })
    } catch (error) {
      const videoError = source.startsWith('cloud://') ? '视频地址暂时无法获取，请稍后重试' : (error as Error).message || '视频暂时无法播放，请稍后重试'
      if (request === this.videoRequest) this.setData({ videoUrl: '', videoError, videoLoading: false })
    }
  },
  videoError(event: WechatMiniprogram.CustomEvent) {
    if (!this.data.videoUrl || Number(event.currentTarget.dataset.attempt) !== this.data.videoAttempt) return
    this.setData({ videoError: '视频暂时无法播放，请检查网络后重试', videoLoading: false })
  },
  retryVideo() {
    if (this.data.videoLoading || !this.data.videoError || !this.data.article?.videoUrl) return
    return this.loadVideo()
  },
  onVideoTime(event: WechatMiniprogram.CustomEvent) {
    const article=this.data.article
    if(!article || !this.data.videoUrl || Number(event.currentTarget.dataset.attempt)!==this.data.videoAttempt) return
    const {currentTime, duration}=event.detail
    if(!Number.isFinite(currentTime) || !Number.isFinite(duration) || duration<=0) return
    this.videoSample={articleId:article.id,source:article.videoUrl || '',position:Math.min(currentTime,duration),duration}
    if(Date.now()-this.videoLastSave>=10000) void this.saveVideoProgress()
  },
  onVideoPause() { return this.saveVideoProgress() },
  onVideoEnded() { if(this.videoSample) this.videoSample={...this.videoSample,position:this.videoSample.duration}; return this.saveVideoProgress() },
  async saveVideoProgress() {
    const sample=this.videoSample
    if(!sample || this.videoSaveBusy) return
    this.videoSaveBusy=true; this.videoLastSave=Date.now()
    try {
      await callApi('classroom.video.save',sample)
      if(this.videoSample===sample) this.videoSample=null
      if(!this.disposed) this.setData({videoProgressError:''})
    } catch (_) { if(!this.disposed) this.setData({videoProgressError:'播放进度未保存，暂停或返回时将重试'}) }
    finally { this.videoSaveBusy=false; if((!this.visible || this.disposed) && this.videoSample && this.videoSample!==sample) void this.saveVideoProgress() }
  },
  coverError() { this.setData({ coverFailed: true }) },
  async toggleCompleted() {
    const article = this.data.article
    if (!article || this.data.completeBusy) return
    this.setData({ completeBusy: true })
    try {
      const result = await callApi<{ completed: boolean }>('classroom.complete', { articleId: article.id, completed: !article.completed })
      if (!this.disposed && this.data.article?.id === article.id) this.setData({ article: { ...this.data.article, completed: result.completed } })
    } catch (error) { wx.showToast({ title: (error as Error).message, icon: 'none' }) }
    finally { if (!this.disposed) this.setData({ completeBusy: false }) }
  },
  onQuestionInput(event: WechatMiniprogram.Input) { this.questionEventId = ''; this.setData({ questionDraft: event.detail.value, questionError: '' }) },
  async loadQuestions(append = false) {
    if (append && (this.data.questionsLoading || !this.data.questionsMore)) return
    const version = ++this.questionRequest
    this.setData({ questionsLoading: true, questionsError: '' })
    try {
      const result = await callApi<{ items: ClassroomQuestion[]; hasMore: boolean }>('classroom.questions', { articleId: this.data.articleId, offset: append ? this.data.questions.length : 0, limit: 20 })
      if (this.disposed || version !== this.questionRequest) return
      const questions = result.items.map(questionTimes)
      this.setData({ questions: append ? [...this.data.questions, ...questions] : questions, questionsMore: result.hasMore })
    } catch (error) { if (!this.disposed && version === this.questionRequest) this.setData({ questionsError: (error as Error).message }) }
    finally { if (!this.disposed && version === this.questionRequest) this.setData({ questionsLoading: false }) }
  },
  moreQuestions() { return this.loadQuestions(true) },
  retryQuestions() { return this.loadQuestions() },
  async submitQuestion() {
    if (this.data.questionBusy || !this.data.article) return
    const question = this.data.questionDraft.trim()
    if (!question || [...question].length > 500) { this.setData({ questionError: '请输入问题，最多500字' }); return }
    this.questionEventId ||= articleReadEventId()
    this.setData({ questionBusy: true, questionError: '' })
    try {
      await callApi('classroom.ask', { articleId: this.data.article.id, question, eventId: this.questionEventId })
      if (this.disposed) return
      this.questionEventId = ''; this.setData({ questionDraft: '' })
      wx.showToast({ title: '问题已提交', icon: 'success' })
      await this.loadQuestions()
    } catch (error) { if (!this.disposed) this.setData({ questionError: (error as Error).message }) }
    finally { if (!this.disposed) this.setData({ questionBusy: false }) }
  },
  async toggleFavorite() {
    const article = this.data.article
    if (!article || this.data.favoriteBusy) return
    const enabled = !article.favorite
    this.setData({ favoriteBusy: true })
    try {
      await callApi('article.favorite.set', { articleId: article.id, enabled })
      if (!this.disposed && this.data.article?.id === article.id) this.setData({ article: { ...this.data.article, favorite: enabled } })
      wx.showToast({ title: enabled ? '已收藏' : '已取消收藏', icon: 'success' })
    } catch (error) { wx.showToast({ title: (error as Error).message, icon: 'none' }) }
    finally { if (!this.disposed) this.setData({ favoriteBusy: false }) }
  },
  goClassroom() { wx.switchTab({ url: '/pages/classroom/index' }) },
  onUnload() { void this.saveVideoProgress(); this.visible = false; this.disposed = true; ++this.loadRequest; ++this.videoRequest },
  onShareAppMessage() {
    const article = this.data.article
    return {
      title: article?.title || '迟记葡萄种植课堂',
      path: article ? `/pages/article/index?id=${encodeURIComponent(article.id)}` : '/pages/classroom/index',
    }
  },
})

