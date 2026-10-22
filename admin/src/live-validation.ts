import { validLiveUrl } from '../../miniprogram/utils/orchard-live'
import type { HomeConfig } from './types'

export function validateLiveConfig(live: HomeConfig['live']) {
 if (live === undefined) return
 if (!live || typeof live !== 'object' || !live.orchard || !live.stall) throw new Error('请完整配置果园内部和园外摊位两个机位')
 for (const channel of [live.orchard, live.stall]) {
  if (typeof channel.enabled !== 'boolean' || typeof channel.url !== 'string' || typeof channel.hours !== 'string') throw new Error('直播机位配置无效')
  if ([...channel.hours].length > 100) throw new Error('直播时间说明最多100字')
  if (!channel.url && channel.enabled) throw new Error('开启机位前请填写直播播放地址')
  if (channel.url && !validLiveUrl(channel.url)) throw new Error('直播播放地址须为有效的 HTTPS HLS（.m3u8）地址')
 }
}
