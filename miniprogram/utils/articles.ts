import type { Article } from './types'

const ARTICLE_READ_KEY = 'chiji_demo_article_reads'
const DEMO_READER_ID = 'demo-user'
const ARTICLE_FAVORITES_KEY = 'chiji_demo_article_favorites'
const getDemoFavorites = (): Record<string, string> => wx.getStorageSync(ARTICLE_FAVORITES_KEY) || {}

interface DemoArticleReads {
  lastReadAt?: string
  eventIds: string[]
  readerIds: string[]
  readCount: number
  readerCount: number
}

const getDemoReads = (): Record<string, DemoArticleReads> => wx.getStorageSync(ARTICLE_READ_KEY) || {}

// Legacy demonstration articles are published; all returned objects are copies.
export function withDemoArticleStats(article: Article): Article {
  const reads = getDemoReads()[article.id]
  return {
    ...article,
    content: [...article.content],
    status: article.status || 'PUBLISHED',
    version: article.version || 1,
    readCount: reads?.readCount ?? article.readCount ?? 0,
    readerCount: reads?.readerCount ?? article.readerCount ?? 0,
    favorite: !!getDemoFavorites()[article.id],
    lastReadAt: reads?.lastReadAt || '',
  }
}

export function recordDemoArticleRead(article: Article | undefined, eventId: unknown): { recorded: boolean } {
  if (!article || (article.status && article.status !== 'PUBLISHED')) throw new Error('内容不存在或尚未发布')
  if (typeof eventId !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(eventId)) throw new Error('阅读记录参数无效')
  const stats = getDemoReads()
  const reads = stats[article.id] || {
    eventIds: [], readerIds: [], readCount: article.readCount || 0, readerCount: article.readerCount || 0,
  }
  if (reads.eventIds.includes(eventId)) return { recorded: false }
  const isNewReader = !reads.readerIds.includes(DEMO_READER_ID)
  stats[article.id] = {
    lastReadAt: new Date().toISOString(),
    eventIds: [...reads.eventIds, eventId],
    readerIds: isNewReader ? [...reads.readerIds, DEMO_READER_ID] : reads.readerIds,
    readCount: reads.readCount + 1,
    readerCount: reads.readerCount + (isNewReader ? 1 : 0),
  }
  wx.setStorageSync(ARTICLE_READ_KEY, stats)
  return { recorded: true }
}

let visitSequence = 0
export const articleReadEventId = () => `read-${Date.now().toString(36)}-${++visitSequence}-${Math.random().toString(36).slice(2, 12)}`

export function setDemoArticleFavorite(article: Article | undefined, enabled: unknown) {
  if (!article || (article.status && article.status !== 'PUBLISHED')) throw new Error('内容不存在或已下架')
  if (typeof enabled !== 'boolean') throw new Error('收藏参数无效')
  const favorites = getDemoFavorites()
  if (enabled) favorites[article.id] = favorites[article.id] || new Date().toISOString()
  else delete favorites[article.id]
  wx.setStorageSync(ARTICLE_FAVORITES_KEY, favorites)
  return { enabled }
}

export function searchDemoArticles(articles: Article[], data: Record<string, any>) {
  const keyword = String(data.keyword || '').trim().toLowerCase()
  const offset = data.offset ?? 0, limit = data.limit ?? 20
  if ([...keyword].length > 100 || !Number.isInteger(offset) || offset < 0 || offset > 100000 || !Number.isInteger(limit) || limit < 1 || limit > 50 || !['', 'all', 'favorite', 'history'].includes(data.view || '')) throw new Error('课堂查询参数无效')
  const favorites = getDemoFavorites()
  const filtered = articles.filter(a => !a.status || a.status === 'PUBLISHED').map(withDemoArticleStats).filter(a =>
    (!data.stage || data.stage === '全部' || a.stage === data.stage) &&
    (!data.variety || data.variety === '全部' || a.variety === data.variety) &&
    (!keyword || [a.title, a.summary, a.stage, a.variety].join(' ').toLowerCase().includes(keyword)) &&
    (data.view !== 'favorite' || a.favorite) && (data.view !== 'history' || a.lastReadAt))
  filtered.sort((a, b) => {
    const left = data.view === 'history' ? a.lastReadAt! : data.view === 'favorite' ? favorites[a.id] : a.publishedAt
    const right = data.view === 'history' ? b.lastReadAt! : data.view === 'favorite' ? favorites[b.id] : b.publishedAt
    return right.localeCompare(left) || a.id.localeCompare(b.id)
  })
  return { articles: filtered.slice(offset, offset + limit), hasMore: filtered.length > offset + limit }
}

export function learningTime(value?: string) {
  if (!value) return ''
  const date = new Date(new Date(value).getTime() + 8 * 3600 * 1000)
  if (Number.isNaN(date.getTime())) return ''
  return `${date.getUTCFullYear()}-${String(date.getUTCMonth() + 1).padStart(2, '0')}-${String(date.getUTCDate()).padStart(2, '0')} ${String(date.getUTCHours()).padStart(2, '0')}:${String(date.getUTCMinutes()).padStart(2, '0')}`
}
