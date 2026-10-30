import { callApi } from '../../utils/api'
import { liveViews, liveChannel } from '../../utils/orchard-live'
import type { LiveViewId } from '../../utils/orchard-live'
import type { HomeConfig, OrchardLiveChannel } from '../../utils/types'

Page({
 connectTimer: undefined as ReturnType<typeof setTimeout> | undefined,
 clearTimer() { if (this.connectTimer !== undefined) { clearTimeout(this.connectTimer); this.connectTimer = undefined } },
 data: {
  views: liveViews, selected: 'orchard' as LiveViewId,
  channel: { enabled: false, url: '', hours: '' } as OrchardLiveChannel,
  home: {} as HomeConfig, loading: true, error: '', playing: false,
  status: 'ready', version: 0, visible: false, notice: { show: false, text: '', countdown: '', subscribed: false, pushConfigured: false, busy: false },
  replays: [] as Array<{id:string;title:string;url:string;coverUrl:string;durationSec:number;playedAt:string}>, replayPlaying: '',
 },
 onShow() { this.setData({ visible: true }); return this.loadConfig() },
 onHide() { this.stopPlayback(); this.setData({ visible: false, version: this.data.version + 1 }) },
 onUnload() { this.onHide() },
 onPullDownRefresh() { return this.loadConfig().finally(() => wx.stopPullDownRefresh()) },
 stopPlayback() {
  this.clearTimer()
  if (this.data.playing) wx.createVideoContext(`live-${this.data.selected}`, this).stop()
  this.setData({ playing: false, status: 'ready' })
 },
 async loadConfig() {
  this.stopPlayback()
  const version = this.data.version + 1
  this.setData({ version, loading: true, error: '' })
  try {
   const home = await callApi<HomeConfig>('home.get')
   if (version !== this.data.version || !this.data.visible) return
   this.setData({ home, channel: liveChannel(home, this.data.selected), loading: false })
   this.loadNotice()
   this.loadReplays()
  } catch (_) {
   if (version !== this.data.version || !this.data.visible) return
   this.setData({ loading: false, error: '果园直播信息暂时无法加载，请重试。', channel: { enabled: false, url: '', hours: '' } })
  }
 },
 selectView(event: WechatMiniprogram.BaseEvent) {
  const id = event.currentTarget.dataset.id as LiveViewId
  if (!liveViews.some(view => view.id === id) || id === this.data.selected) return
  const pending = this.data.loading
  this.stopPlayback()
  this.setData({ selected: id, version: this.data.version + 1, loading: false, channel: liveChannel(this.data.home, id), replayPlaying: '' })
  this.loadNotice()
  this.loadReplays()
  // A switch during a pending request must not leave an empty or stale configuration.
  if (pending || !this.data.home.banners) return this.loadConfig()
 },
 async loadNotice() {
  const channel = this.data.selected
  try {
   const res = await callApi<{ noticeAt: string; noticeText: string; subscribed: boolean; pushConfigured: boolean; templateId: string }>('liveNotice.status', { channel })
   const at = Date.parse(res.noticeAt || '')
   const show = !!res.noticeAt && !Number.isNaN(at) && at > Date.now()
   let countdown = ''
   if (show) { const mins = Math.max(1, Math.round((at - Date.now()) / 60000)); countdown = mins >= 60 ? Math.floor(mins / 60) + ' 小时 ' + (mins % 60) + ' 分后开播' : mins + ' 分后开播' }
   this.setData({ notice: { show, text: res.noticeText || '果园直播即将开始', countdown, subscribed: res.subscribed, pushConfigured: res.pushConfigured, busy: false } })
   this.data.noticeTemplateId = res.templateId
  } catch (_) { this.setData({ notice: { show: false, text: '', countdown: '', subscribed: false, pushConfigured: false, busy: false } }) }
 },
 async subscribeNotice() {
  if (this.data.notice.busy) return
  const channel = this.data.selected
  this.setData({ 'notice.busy': true })
  try {
   if (!this.data.notice.pushConfigured || !this.data.noticeTemplateId) { wx.showToast({ title: '开播提醒尚未开通', icon: 'none' }); return }
   const auth = await wx.requestSubscribeMessage({ tmplIds: [this.data.noticeTemplateId] })
   const accept = auth[this.data.noticeTemplateId] === 'accept'
   await callApi('liveNotice.subscribe', { channel, accept })
   this.setData({ 'notice.subscribed': accept })
   wx.showToast({ title: accept ? '已订阅，开播前提醒你' : '已取消订阅', icon: 'success' })
  } catch (e) { wx.showToast({ title: (e as Error).message || '订阅失败', icon: 'none' }) }
  finally { this.setData({ 'notice.busy': false }) }
 },
 async loadReplays() {
  try {
   const replays = await callApi<Array<{id:string;title:string;url:string;coverUrl:string;durationSec:number;playedAt:string}>>('live.replay.list', { channel: this.data.selected })
   this.setData({ replays: replays || [] })
  } catch (_) { this.setData({ replays: [] }) }
 },
 playReplay(event: WechatMiniprogram.BaseEvent) {
  const id = event.currentTarget.dataset.id as string
  this.stopPlayback()
  this.setData({ replayPlaying: id })
 },
 stopReplay() { this.setData({ replayPlaying: '' }) },
 watchLive() {
  if (this.data.loading || this.data.error || !this.data.channel.enabled || !this.data.visible) return
  this.clearTimer()
  this.setData({ playing: true, status: 'connecting', version: this.data.version + 1 })
  this.startTimer()
 },
 startTimer() {
  this.clearTimer()
  const version = this.data.version
  this.connectTimer = setTimeout(() => {
   if (version === this.data.version && this.data.visible && this.data.status === 'connecting') this.playbackFailed()
  }, 20000)
 },
 currentEvent(event: WechatMiniprogram.BaseEvent) {
  const data = event.currentTarget.dataset
  return this.data.visible && this.data.playing && data.id === this.data.selected && Number(data.version) === this.data.version && data.src === this.data.channel.url
 },
 videoPlay(event: WechatMiniprogram.BaseEvent) {
  if (!this.currentEvent(event)) return
  this.clearTimer(); this.setData({ status: 'playing' })
 },
 videoWaiting(event: WechatMiniprogram.BaseEvent) {
  if (!this.currentEvent(event)) return
  this.setData({ status: 'connecting' }); this.startTimer()
 },
 videoPause(event: WechatMiniprogram.BaseEvent) {
  if (!this.currentEvent(event)) return
  this.clearTimer(); this.setData({ status: 'paused' })
 },
 videoError(event: WechatMiniprogram.BaseEvent) { if (this.currentEvent(event)) this.playbackFailed() },
 playbackFailed() { this.stopPlayback(); this.setData({ status: 'failed' }) },
})

