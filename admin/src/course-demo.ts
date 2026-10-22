import type { Article } from './types'
export interface CourseEntity { id: string; title: string; summary: string; stage: string; variety: string; status: string; sortOrder: number; version: number; articleIds: string[] }
const KEY = 'chiji_admin_courses'
export function courseDemo(action: string, data: any, articles: Article[]) {
 const courses: CourseEntity[] = JSON.parse(localStorage.getItem(KEY) || '[]')
 if (action === 'admin.course.list') return courses.sort((a,b)=>a.sortOrder-b.sortOrder || a.id.localeCompare(b.id))
 const c: CourseEntity = { ...data.course, title: (data.course?.title || '').trim() }
 if (!c.title || [...c.title].length>120 || [...(c.summary || '')].length>1000 || !['DRAFT','PUBLISHED'].includes(c.status) || !Number.isInteger(c.sortOrder) || Math.abs(c.sortOrder)>100000 || !Array.isArray(c.articleIds) || c.articleIds.length>200 || new Set(c.articleIds).size!==c.articleIds.length) throw new Error('课程参数无效')
 for (const id of c.articleIds) { const a=articles.find(a=>a.id===id);if(!a || c.status==='PUBLISHED' && a.status!=='PUBLISHED') throw new Error('发布课程只能关联已发布内容') }
 if(c.status==='PUBLISHED' && !c.articleIds.length) throw new Error('发布课程前请添加课时')
 const index=courses.findIndex(v=>v.id===c.id)
 if(c.id && (index<0 || courses[index].version!==data.expectedVersion)) throw new Error('课程已更新，请刷新后重试')
 c.id ||= `CR_${Date.now()}_${Math.random().toString(36).slice(2)}`; c.version=index<0?1:courses[index].version+1
 if(index<0) courses.push(c);else courses[index]=c
 localStorage.setItem(KEY,JSON.stringify(courses));return c
}
