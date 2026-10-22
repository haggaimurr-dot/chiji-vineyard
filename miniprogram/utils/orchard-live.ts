import type { HomeConfig, OrchardLiveChannel } from './types'

export const liveViews = [
 { id: 'orchard', title: '果园内部', description: '看看葡萄生长的地方，和果农日常的照料。' },
 { id: 'stall', title: '园外摊位', description: '看看今日的摊位，和挑选、装箱的日常。' },
] as const
export type LiveViewId = typeof liveViews[number]['id']
export function validLiveUrl(value: unknown): value is string {
 return typeof value === 'string' && value.length <= 2000 && /^https:\/\/[^\s/?#@:]+(?::\d+)?\/[^\s?#]*\.m3u8(?:\?[^\s#]*)?$/i.test(value)
}
export function liveChannel(home: HomeConfig, id: LiveViewId): OrchardLiveChannel {
 const channel = home.live?.[id]
 return { enabled: channel?.enabled === true && validLiveUrl(channel?.url), url: validLiveUrl(channel?.url) ? channel.url : '', hours: typeof channel?.hours === 'string' ? channel.hours : '' }
}
