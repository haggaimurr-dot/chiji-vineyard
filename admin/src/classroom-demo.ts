interface Question { id: string; articleId: string; articleTitle: string; question: string; answer: string; createdAt: string; answeredAt: string }
const KEY = 'chiji_admin_demo_classroom_questions'
export function classroomDemo(action: string, data: any, article?: { id: string; title: string }) {
  const saved = localStorage.getItem(KEY)
  const items: Question[] = saved ? JSON.parse(saved) : article ? [{ id: 'demo-classroom-question', articleId: article.id, articleTitle: article.title, question: '学习这节内容后，实际操作前需要记录哪些果园情况？', answer: '', createdAt: new Date().toISOString(), answeredAt: '' }] : []
  if (action === 'admin.classroom.questions') {
    const offset = data.offset ?? 0, limit = data.limit ?? 20
    if (!Number.isInteger(offset) || offset < 0 || offset > 100000 || !Number.isInteger(limit) || limit < 1 || limit > 50 || !['', 'pending', 'answered'].includes(data.status || '')) throw new Error('问答查询参数无效')
    localStorage.setItem(KEY, JSON.stringify(items))
    const filtered = items.filter(q => (!data.articleId || q.articleId === data.articleId) && (!data.status || (data.status === 'pending' ? !q.answer : !!q.answer)))
    return { items: filtered.slice(offset, offset + limit), hasMore: filtered.length > offset + limit }
  }
  const q = items.find(q => q.id === data.id), text = typeof data.answer === 'string' ? data.answer.trim() : ''
  if (!text || [...text].length > 2000) throw new Error('请填写有效回复，最多2000字')
  if (!q || q.answer !== (data.expectedAnswer || '')) throw new Error('问题不存在或回复已更新，请刷新后重试')
  q.answer = text; q.answeredAt = new Date().toISOString()
  localStorage.setItem(KEY, JSON.stringify(items))
  return { answered: true }
}
