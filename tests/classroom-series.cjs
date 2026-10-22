const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
function runtime(globals) {
  const cache = new Map()
  return function load(relative) {
    const filename = path.resolve(root, relative)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(source, { module, exports: module.exports, console, Error, setTimeout, clearTimeout, ...globals, require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)) }, { filename })
    return module.exports
  }
}
const instantiate = spec => {
  const page = { ...spec, data: structuredClone(spec.data) }
  page.setData = (updates, cb) => { Object.assign(page.data, updates); cb?.() }
  return page
}
async function run() {
  const storage = new Map(), calls = []
  let spec, mode = true, remote
  const wx = {
    getStorageSync: key => structuredClone(storage.get(key)), setStorageSync: (key, value) => storage.set(key, structuredClone(value)), removeStorageSync: key => storage.delete(key),
    showToast() {}, setNavigationBarTitle() {}, stopPullDownRefresh() {},
    cloud: { callContainer: async ({ data: { action, data } }) => { calls.push({ action, data }); return { statusCode: 200, data: { ok: true, data: await remote(action, data) } } } },
  }
  const globals = { wx, Page: value => { spec = value }, getApp: () => ({ globalData: { demoMode: mode } }) }
  const load = runtime(globals), { callApi } = load('miniprogram/utils/api.ts')
  const { demoArticles } = load('miniprogram/data/demo.ts'), id = demoArticles[0].id
  assert.equal((await callApi('classroom.courses')).completed, 0)
  await callApi('article.read', { articleId: id, eventId: 'opened-lesson' })
  assert.equal((await callApi('classroom.courses')).completed, 0, 'opening is not completion')
  await callApi('classroom.complete', { articleId: id, completed: true })
  await callApi('classroom.complete', { articleId: id, completed: true })
  assert.equal((await callApi('classroom.courses')).completed, 1, 'completion is idempotent')
  const fresh = runtime(globals)('miniprogram/utils/api.ts')
  assert.equal((await fresh.callApi('article.detail', { id })).completed, true, 'completion survives reload')
  await callApi('classroom.complete', { articleId: id, completed: false })
  assert.equal((await callApi('classroom.courses')).completed, 0)
  const { demoClassroom } = load('miniprogram/utils/classroom.ts')
  const fixtures = [
    { ...demoArticles[0], id: 'late', publishedAt: '2026-10-02', status: 'PUBLISHED' },
    { ...demoArticles[0], id: 'early', publishedAt: '2026-10-01', status: 'PUBLISHED' },
    { ...demoArticles[0], id: 'draft', status: 'DRAFT' },
    { ...demoArticles[0], id: 'scheduled', status: 'SCHEDULED' },
  ]
  const seededCourses = storage.get('chiji_demo_courses')
  storage.delete('chiji_demo_courses')
  demoClassroom('classroom.complete', { articleId: 'early', completed: true }, fixtures)
  const series = demoClassroom('classroom.courses', {}, fixtures)
  assert.equal(series.total, 2); assert.equal(series.completed, 1)
  assert.equal(series.courses[0].percent, 50); assert.equal(series.courses[0].nextId, 'late')
  assert.equal(series.courses[0].lessons[0].id, 'early')
  assert.throws(() => demoClassroom('classroom.complete', { articleId: 'draft', completed: true }, fixtures), /下架/)
  await assert.rejects(callApi('classroom.complete', { articleId: id, completed: 'true' }), /参数/)
  storage.set('chiji_demo_courses', seededCourses)
  await callApi('classroom.ask', { articleId: id, question: '需要记录什么？', eventId: 'ask-1' })
  await callApi('classroom.ask', { articleId: id, question: '需要记录什么？', eventId: 'ask-1' })
  assert.equal((await callApi('classroom.questions')).items.length, 1, 'request retries do not duplicate questions')
  assert.equal((await fresh.callApi('classroom.questions')).items.length, 1)
  assert.equal((await callApi('classroom.questions', { articleId: 'other' })).items.length, 0)
  await assert.rejects(callApi('classroom.ask', { articleId: id, question: ' ', eventId: 'ask-2' }), /有效/)
  await assert.rejects(callApi('classroom.ask', { articleId: id, question: '葡'.repeat(501), eventId: 'ask-2' }), /有效/)
  load('miniprogram/pages/article/index.ts')
  const detail = instantiate(spec)
  await detail.onLoad({ id })
  await detail.toggleCompleted(); assert.equal(detail.data.article.completed, true)
  detail.onQuestionInput({ detail: { value: '修剪前如何检查枝条？' } })
  await detail.submitQuestion(); assert.equal(detail.data.questionDraft, ''); assert.equal(detail.data.questions.length, 2)
  load('miniprogram/pages/classroom/index.ts')
  const page = instantiate(spec)
  await page.pickView({ currentTarget: { dataset: { value: 'progress' } } })
  assert.equal(page.data.completedLessons, 1)
  await page.pickView({ currentTarget: { dataset: { value: 'questions' } } })
  assert.equal(page.data.questions.length, 2)

  // A response lost after commit must retry with the same event ID and retain the draft.
  mode = false; storage.set('chiji_mini_token', 'test-token')
  let attempts = 0
  remote = async action => { if (action === 'classroom.ask' && ++attempts === 1) throw new Error('响应丢失'); return action === 'classroom.questions' ? { items: [], hasMore: false } : { submitted: true } }
  detail.onQuestionInput({ detail: { value: '重试问题' } })
  await detail.submitQuestion(); const event = calls.at(-1).data.eventId
  assert.equal(detail.data.questionDraft, '重试问题'); assert.equal(detail.data.questionBusy, false)
  await detail.submitQuestion()
  assert.equal(calls.filter(c => c.action === 'classroom.ask').at(-1).data.eventId, event)

  const waiting = []
  remote = () => new Promise((resolve, reject) => waiting.push({ resolve, reject }))
  page.setData({ view: 'series' }); const old = page.load()
  page.setData({ view: 'questions' }); const current = page.load()
  waiting[1].resolve({ items: [], hasMore: false }); await current
  waiting[0].resolve({ courses: [{ id: 'stale' }], total: 1, completed: 0 }); await old
  assert.equal(page.data.courses.length, 0, 'stale series response cannot restore a previous tab')

  const adminStorage = new Map(), adminGlobals = { localStorage: { getItem: key => adminStorage.get(key) || null, setItem: (key, value) => adminStorage.set(key, value) } }
  const { classroomDemo } = runtime(adminGlobals)('admin/src/classroom-demo.ts')
  const pending = classroomDemo('admin.classroom.questions', { status: 'pending' }, demoArticles[0]).items[0]
  classroomDemo('admin.classroom.answer', { id: pending.id, answer: '先记录天气、枝条长势和土壤情况。', expectedAnswer: '' })
  assert.equal(classroomDemo('admin.classroom.questions', { status: 'pending' }).items.length, 0)
  assert.equal(classroomDemo('admin.classroom.questions', { status: 'answered' }).items.length, 1)
  assert.throws(() => classroomDemo('admin.classroom.answer', { id: pending.id, answer: '过期回复', expectedAnswer: '' }), /更新/)
  mode = true
  assert.equal((await fresh.callApi('classroom.questions')).items.length, 2, 'admin demonstration replies do not alter mini demonstration questions')
  const { courseDemo } = runtime(adminGlobals)('admin/src/course-demo.ts')
  const course = courseDemo('admin.course.save', { course: {title:'跨阶段课程',summary:'',stage:'',variety:'',status:'PUBLISHED',sortOrder:2,articleIds:['late','early']},expectedVersion:0 },fixtures)
  assert.deepEqual(Array.from(course.articleIds),['late','early'])
  assert.throws(()=>courseDemo('admin.course.save',{course,expectedVersion:0},fixtures),/更新/)
  assert.throws(()=>courseDemo('admin.course.save',{course:{...course,articleIds:['draft']},expectedVersion:1},fixtures),/已发布/)
  assert.throws(()=>courseDemo('admin.course.save',{course:{...course,articleIds:['early','early']},expectedVersion:1},fixtures),/参数/)
  assert.equal(courseDemo('admin.course.list',{},fixtures)[0].id,course.id)
  storage.set('chiji_demo_courses',[course,{...course,id:'second',articleIds:['early']}])
  const explicit=demoClassroom('classroom.courses',{},fixtures)
  assert.equal(explicit.total,2,'shared lessons counted once overall')
  assert.equal(explicit.courses[0].lessons[0].id,'late','explicit ordering wins over publication dates')
  const videos=[{...demoArticles[0],id:'video-progress',type:'video',videoUrl:'https://example.com/a.mp4',status:'PUBLISHED'}]
  const saved=demoClassroom('classroom.video.save',{articleId:'video-progress',source:videos[0].videoUrl,position:35,duration:120},videos)
  assert.equal(saved.position,35)
  assert.equal(demoClassroom('classroom.video.get',{articleId:'video-progress'},videos).position,35)
  assert.equal(demoClassroom('classroom.video.get',{articleId:'video-progress'},[{...videos[0],videoUrl:'https://example.com/b.mp4'}]).position,0)
  assert.throws(()=>demoClassroom('classroom.video.save',{articleId:'video-progress',source:videos[0].videoUrl,position:121,duration:120},videos),/参数/)
  const {requirePermission}=runtime(adminGlobals)('admin/src/permissions.ts')
  assert.throws(()=>requirePermission({role:'OPERATOR',username:'editor',permissions:['article.read','article.write']},'admin.article.review'),/权限/)
  requirePermission({role:'OPERATOR',username:'reviewer',permissions:['article.read','article.review']},'admin.article.review')
  assert.throws(()=>requirePermission({role:'OPERATOR',username:'reviewer',permissions:['article.read','article.review']},'admin.article.save'),/权限/)
  console.log('Classroom series passed: ordering, publication gating, completion persistence, private questions, retries, stale tabs and admin replies')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
