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
  status: 'ready', version: 0, visible: false,
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
  this.setData({ selected: id, version: this.data.version + 1, loading: false, channel: liveChannel(this.data.home, id) })
  // A switch during a pending request must not leave an empty or stale configuration.
  if (pending || !this.data.home.banners) return this.loadConfig()
 },
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

