import type { Article } from './types'
import { learningTime } from './articles'

export interface Course {
  id: string; title: string; stage: string; variety: string
  lessons: { id: string; title: string; completed: boolean }[]
  completed: number; percent: number; nextId: string
}
export interface CourseResult { courses: Course[]; total: number; completed: number }
export interface ClassroomQuestion {
  id: string; articleId: string; articleTitle: string; question: string; answer: string
  createdAt: string; answeredAt: string; createdText?: string; answeredText?: string
}
export const questionTimes = (q: ClassroomQuestion) => ({ ...q, createdText: learningTime(q.createdAt), answeredText: learningTime(q.answeredAt) })
const PROGRESS = 'chiji_demo_classroom_progress', QUESTIONS = 'chiji_demo_classroom_questions'
export const demoCompleted = (id: string): boolean => !!(wx.getStorageSync(PROGRESS) || {})[id]
export function demoClassroom(action: string, data: Record<string, any>, articles: Article[]) {
  const published = articles.filter(a => !a.status || a.status === 'PUBLISHED')
  if (action === 'classroom.courses') {
    // Demonstration courses are seeded once and then stored as independent entities.
    const key = 'chiji_demo_courses'
    let entities = wx.getStorageSync(key)
    if (!Array.isArray(entities)) {
      const groups: Record<string, any> = {}
      for (const a of [...published].sort((a,b)=>a.publishedAt.localeCompare(b.publishedAt)||a.id.localeCompare(b.id))) {
        const id = `${a.stage}:${a.variety}`
        groups[id] ||= { id, title: `${a.stage}系列课`, stage: a.stage, variety: a.variety, status: 'PUBLISHED', sortOrder: 0, articleIds: [] }
        groups[id].articleIds.push(a.id)
      }
      entities = Object.values(groups); wx.setStorageSync(key, entities)
    }
    const courses: Course[] = entities.filter((c: any)=>c.status==='PUBLISHED').sort((a: any,b: any)=>a.sortOrder-b.sortOrder||a.id.localeCompare(b.id)).map((e: any)=>{
      const lessons = e.articleIds.map((id: string)=>published.find(a=>a.id===id)).filter(Boolean).map((a: Article)=>({id:a.id,title:a.title,completed:demoCompleted(a.id)}))
      const completed = lessons.filter((l: any)=>l.completed).length
      return {...e,lessons,completed,percent:lessons.length?Math.floor(completed*100/lessons.length):0,nextId:lessons.find((l: any)=>!l.completed)?.id || ''}
    })
    const ids = [...new Set(courses.flatMap(c=>c.lessons.map(l=>l.id)))]
    return { courses, total: ids.length, completed: ids.filter(demoCompleted).length }
  }
  if (action.startsWith('classroom.video.')) {
    const article = published.find(a=>a.id===data.articleId)
    if (!article || article.type!=='video') throw new Error('视频不存在或已下架')
    const key='chiji_demo_video_progress', all=wx.getStorageSync(key)||{}
    if(action==='classroom.video.get') return all[article.id]?.source===article.videoUrl?all[article.id]:{articleId:article.id,source:article.videoUrl,position:0,duration:0}
    if(data.source!==article.videoUrl || !Number.isFinite(data.position) || !Number.isFinite(data.duration) || data.duration<=0 || data.duration>86400 || data.position<0 || data.position>data.duration) throw new Error('视频进度参数无效或视频已更新')
    const progress={articleId:article.id,source:data.source,position:data.position,duration:data.duration}
    wx.setStorageSync(key,{...all,[article.id]:progress});return progress
  }
  if (action === 'classroom.questions') {
    const offset = data.offset ?? 0, limit = data.limit ?? 20
    if (!Number.isInteger(offset) || offset < 0 || offset > 100000 || !Number.isInteger(limit) || limit < 1 || limit > 50 || !['', 'pending', 'answered'].includes(data.status || '')) throw new Error('问答查询参数无效')
    const items: ClassroomQuestion[] = (wx.getStorageSync(QUESTIONS) || []).filter((q: ClassroomQuestion) => (!data.articleId || q.articleId === data.articleId) && (!data.status || (data.status === 'pending' ? !q.answer : !!q.answer)))
    return { items: items.slice(offset, offset + limit), hasMore: items.length > offset + limit }
  }
  const article = published.find(a => a.id === data.articleId)
  if (!article) throw new Error('内容不存在或已下架')
  if (action === 'classroom.complete') {
    if (typeof data.completed !== 'boolean') throw new Error('学习进度参数无效')
    wx.setStorageSync(PROGRESS, { ...(wx.getStorageSync(PROGRESS) || {}), [article.id]: data.completed })
    return { completed: data.completed }
  }
  if (action === 'classroom.ask') {
    const text = typeof data.question === 'string' ? data.question.trim() : ''
    if (!text || [...text].length > 500 || typeof data.eventId !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(data.eventId)) throw new Error('请填写有效问题（最多500字）')
    const items = wx.getStorageSync(QUESTIONS) || []
    if (!items.some((q: { eventId: string }) => q.eventId === data.eventId)) {
      items.unshift({ id: data.eventId, eventId: data.eventId, articleId: article.id, articleTitle: article.title, question: text, answer: '', createdAt: new Date().toISOString(), answeredAt: '' })
      wx.setStorageSync(QUESTIONS, items)
    }
    return { submitted: true }
  }
  throw new Error('课堂操作无效')
}
