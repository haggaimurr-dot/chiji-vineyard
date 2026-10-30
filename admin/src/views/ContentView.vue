<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, dateTime, isDemo, uploadVideo } from '../api'
import CoursesView from './CoursesView.vue'
import ClassroomQuestions from './ClassroomQuestions.vue'
import ContentLibrary from './ContentLibrary.vue'
import { reviewLabels } from '../content-library'
import RichTextEditor from '../components/RichTextEditor.vue'
import ArticlePreview from '../components/ArticlePreview.vue'
import { contentParagraphs, hasContent, paragraphsToHtml, sanitizeContent } from '../content'
import { checkVideoFile, checkVideoPlayback, videoAddressError, videoDuration } from '../video'
import { beijingInput, publicationDate, scheduleISO } from '../content-time'
import type { Article, ArticleStats, ArticleVersion, ContentResource } from '../types'

const articles = ref<Article[]>([])
const section = ref<'articles' | 'review' | 'tags' | 'media' | 'questions' | 'courses'>('articles')
const resources = ref<ContentResource[]>([]), resourceLoading = ref(false), resourceError = ref('')
const reviewing = ref<Article | null>(null), reviewNote = ref(''), reviewError = ref(''), reviewBusy = ref(false)
const reviewFilter = ref('PENDING'), submitting = ref('')
const reviewItems = computed(() => articles.value.filter(a => reviewFilter.value === 'ALL' ? !!a.reviewStatus : a.reviewStatus === reviewFilter.value))
const tagOptions = computed(() => resources.value.filter(r => r.kind === 'tag'))
const linkProducts = ref<Array<{id: string; name: string}>>([])
async function loadLinkProducts() { if (linkProducts.value.length) return; try { const r = await api<Array<{id: string; name: string}>>('admin.product.list', {}); linkProducts.value = (r || []).map((p: any) => ({ id: p.id, name: p.name || p.id })) } catch { linkProducts.value = [] } }
async function loadResources() {
  if (resourceLoading.value) return
  resourceLoading.value = true; resourceError.value = ''
  try { resources.value = await api<ContentResource[]>('admin.content.library') }
  catch (e) { resourceError.value = (e as Error).message }
  finally { resourceLoading.value = false }
}
function useResource(event: Event, kind: 'image' | 'video') {
  const input = event.target as HTMLSelectElement
  const resource = resources.value.find(r => r.id === input.value)
  if (editing.value && resource) { if (kind === 'image') editing.value.cover = resource.url || ''; else editing.value.videoUrl = resource.url || '' }
  input.value = ''
}
async function submitReview(article: Article) {
  if (submitting.value) return
  submitting.value = article.id; loadError.value = ''
  try { await api('admin.article.submit', { articleId: article.id, expectedVersion: article.version || 1 }); notice.value = '已提交审核'; await load() }
  catch (e) { loadError.value = (e as Error).message }
  finally { submitting.value = '' }
}
function openReview(article: Article) { reviewing.value = clone(article); reviewNote.value = ''; reviewError.value = '' }
async function decideReview(decision: 'APPROVED' | 'REJECTED') {
  if (!reviewing.value || reviewBusy.value) return
  reviewError.value = ''
  if (decision === 'REJECTED' && !reviewNote.value.trim()) { reviewError.value = '请填写退回原因'; return }
  reviewBusy.value = true
  try { await api('admin.article.review', { articleId: reviewing.value.id, expectedVersion: reviewing.value.version || 1, decision, note: reviewNote.value }); reviewing.value = null; notice.value = decision === 'APPROVED' ? '审核通过，可返回内容列表安排发布。' : '内容已退回，修改后可重新提交。'; await load() }
  catch (e) { reviewError.value = (e as Error).message }
  finally { reviewBusy.value = false }
}
const loading = ref(false), loadError = ref(''), notice = ref('')
const keyword = ref(''), filter = ref('ALL')
const editing = ref<Article | null>(null), schedule = ref(''), publishDate = ref('')
const saving = ref(false), uploading = ref(false), error = ref('')
const showPreview = ref(false), previewing = ref<Article | null>(null)
const editorBusy = ref(false), editorError = ref(''), videoUploading = ref(false), videoChecking = ref(false)
const uploadProgress = ref(0), videoState = ref<'idle' | 'valid' | 'invalid'>('idle'), videoMessage = ref(''), checkedSource = ref('')
const busy = computed(() => saving.value || uploading.value || editorBusy.value || videoUploading.value || videoChecking.value)
let videoOperation: AbortController | undefined
const inspecting = ref<Article | null>(null), detailMode = ref<'versions' | 'stats'>('versions')
const detailLoading = ref(false), detailError = ref(''), versions = ref<ArticleVersion[]>([])
const selectedVersion = ref<ArticleVersion | null>(null), statistics = ref<ArticleStats | null>(null)
const restoring = ref(false), restoreConfirm = ref(false)
const restorePrompt = ref<HTMLDivElement | null>(null)
let detailRequest = 0, refreshTimer: ReturnType<typeof setInterval> | undefined
let disposed = false

const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value))
const blank = (): Article => ({ id: '', title: '', summary: '', cover: '', variety: '通用', stage: '采后管理', type: 'article', content: [''], status: 'DRAFT', publishedAt: '', tags: [] })
const statusLabels: Record<string, string> = { DRAFT: '草稿', SCHEDULED: '待定时发布', PUBLISHED: '已发布' }
const reasonLabels: Record<string, string> = { create: '新建内容', save: '编辑保存', legacy: '初始版本', baseline: '初始版本', restore: '恢复历史版本', PENDING: '提交审核', APPROVED: '审核通过', REJECTED: '审核退回', scheduled_publish: '定时发布' }
const statusName = (status: string) => statusLabels[status] || status
const reasonName = (reason: string) => reason.startsWith('restore:') ? `恢复自 V${reason.slice(8)}` : reasonLabels[reason] || reason || '内容更新'
const number = (value?: number) => (value || 0).toLocaleString('zh-CN')
const filteredArticles = computed(() => articles.value.filter(article => (filter.value === 'ALL' || article.status === filter.value) && `${article.title} ${article.summary} ${article.variety} ${article.stage} ${(article.tags || []).join(" ")}`.toLowerCase().includes(keyword.value.toLowerCase().trim())))
const statuses = computed(() => [
  { value: 'ALL', label: '全部', count: articles.value.length },
  ...(['PUBLISHED', 'SCHEDULED', 'DRAFT'] as const).map(value => ({ value, label: statusName(value), count: articles.value.filter(article => article.status === value).length })),
])
const periodViews = computed(() => statistics.value?.days.reduce((sum, day) => sum + day.views, 0) || 0)
const chartMax = computed(() => Math.max(1, ...statistics.value?.days.map(day => day.views) || [0]))

async function load() {
  if (loading.value) return
  loading.value = true; loadError.value = ''
  try { const result = await api<Article[]>('admin.article.list'); if (!disposed) articles.value = result }
  catch (e) { if (!disposed) loadError.value = (e as Error).message }
  finally { loading.value = false }
}
onMounted(() => {
  void load()
  void loadResources()
  refreshTimer = setInterval(() => { if (articles.value.some(article => article.status === 'SCHEDULED') && !saving.value && !restoring.value && !uploading.value) void load() }, 30000)
})
onUnmounted(() => { disposed = true; detailRequest++; clearInterval(refreshTimer); videoOperation?.abort() })

function edit(article?: Article) {
  editing.value = clone(article || blank())
  editing.value.tags ||= []
  editing.value.productIds ||= []
  void loadLinkProducts()
  editing.value.contentHtml = sanitizeContent(editing.value.contentHtml || paragraphsToHtml(editing.value.content || []))
  showPreview.value = false; editorError.value = ''
  if (editing.value.id) editing.value.version ||= 1
  schedule.value = beijingInput(editing.value.scheduledAt)
  publishDate.value = publicationDate(editing.value.publishedAt)
  error.value = ''; notice.value = ''
}
function closeEditor() { if (!busy.value) editing.value = null }
async function reloadEditor() {
  if (!editing.value?.id || busy.value) return
  const id = editing.value.id
  await load()
  if (loadError.value) { error.value = loadError.value; return }
  const current = articles.value.find(article => article.id === id)
  if (current) edit(current)
  else error.value = '这篇内容已不存在，请关闭编辑器并刷新列表'
}
async function save() {
  if (!editing.value || busy.value) return
  error.value = ''
  const article = clone(editing.value)
  article.contentHtml = sanitizeContent(article.contentHtml || '')
  article.content = contentParagraphs(article.contentHtml)
  if (!hasContent(article.contentHtml)) { error.value = '请填写正文或插入正文图片'; return }
  if (!article.title.trim() || !article.summary.trim() || !article.cover.trim() || !article.variety.trim() || !article.stage.trim()) { error.value = '请填写标题、摘要、封面、品种和种植阶段'; return }
  if (!isDemo && new TextEncoder().encode(article.contentHtml).length > 100 * 1024) { error.value = '正文过长，请缩减到 100KB 以内'; return }
  if ((article.contentHtml.match(/<img\b/gi) || []).length > 30) { error.value = '正文最多插入 30 张图片'; return }
  if (article.content.length > 100 || article.content.join('').length > 50000) { error.value = '正文最多 100 段，总长度不能超过 50000 字'; return }
  if (publishDate.value && !publicationDate(publishDate.value)) { error.value = '发布日期无效'; return }
  article.publishedAt = publishDate.value ? (publicationDate(article.publishedAt) === publishDate.value ? article.publishedAt : publishDate.value) : ''
  try {
    if (article.status === 'SCHEDULED') article.scheduledAt = scheduleISO(schedule.value)
    else delete article.scheduledAt
  } catch (e) { error.value = (e as Error).message; return }
  saving.value = true
  try {
    if (article.type === 'video') {
      if ((checkedSource.value !== article.videoUrl?.trim() || videoState.value !== 'valid') && !(await validateVideo())) throw new Error(videoMessage.value || '请先校验视频可播放性')
      article.duration = editing.value?.duration
    } else { delete article.videoUrl; delete article.duration }
    const saved = await api<Article>('admin.article.save', { article })
    articles.value = [saved, ...articles.value.filter(item => item.id !== saved.id)]
    editing.value = null
    notice.value = saved.status === 'SCHEDULED' ? `已保存 V${saved.version || 1}，将于 ${dateTime(saved.scheduledAt)}（北京时间）发布。` : `已保存 V${saved.version || 1} · ${statusName(saved.status)}`
    await load()
  } catch (e) { error.value = (e as Error).message }
  finally { saving.value = false }
}
async function uploadCover(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || !editing.value || busy.value) return
  error.value = ''
  if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) { error.value = '请上传 JPG、PNG 或 WebP 图片'; return }
  if (!file.size || file.size > 4 * 1024 * 1024) { error.value = '图片不能为空，且不能超过 4MB'; return }
  uploading.value = true
  try {
    const dataUrl = await new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result)); reader.onerror = () => reject(new Error('图片读取失败，请重试')); reader.readAsDataURL(file) })
    const result = await api<{ fileID: string }>('admin.media.upload', { dataUrl })
    if (editing.value) editing.value.cover = result.fileID
  } catch (e) { error.value = (e as Error).message }
  finally { uploading.value = false }
}

function resetVideo() {
  videoOperation?.abort()
  checkedSource.value = ''; videoState.value = 'idle'; videoMessage.value = ''
  if (editing.value) editing.value.duration = undefined
}
watch(() => editing.value?.videoUrl, resetVideo, { flush: 'sync' })
watch(() => editing.value?.type, () => {
  resetVideo()
  if (editing.value?.type === 'article') editing.value.videoUrl = undefined
}, { flush: 'sync' })

async function validateVideo(): Promise<boolean> {
  const article = editing.value
  if (!article || article.type !== 'video') return true
  const source = article.videoUrl?.trim() || ''
  const invalid = videoAddressError(source, isDemo)
  if (invalid) { videoState.value = 'invalid'; videoMessage.value = invalid; return false }
  videoOperation?.abort()
  const controller = new AbortController()
  videoOperation = controller
  videoChecking.value = true; videoState.value = 'idle'; videoMessage.value = '正在加载视频画面…'
  try {
    const metadata = await checkVideoPlayback(source, controller.signal)
    if (controller.signal.aborted || editing.value !== article || article.videoUrl?.trim() !== source) return false
    article.duration = videoDuration(metadata.duration)
    checkedSource.value = source; videoState.value = 'valid'
    videoMessage.value = `可播放 · ${article.duration} · ${metadata.width} × ${metadata.height}`
    return true
  } catch (e) {
    if (!controller.signal.aborted && (e as Error).name !== 'AbortError' && editing.value === article && article.videoUrl?.trim() === source) { videoState.value = 'invalid'; videoMessage.value = (e as Error).message }
    return false
  } finally { if (videoOperation === controller) videoChecking.value = false }
}

async function uploadVideoFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  const article = editing.value
  if (!file || !article || busy.value) return
  videoUploading.value = true; uploadProgress.value = 0; videoMessage.value = '正在检查视频文件…'; videoState.value = 'idle'
  const controller = new AbortController()
  videoOperation = controller
  let localUrl = ''
  try {
    await checkVideoFile(file)
    localUrl = URL.createObjectURL(file)
    await checkVideoPlayback(localUrl, controller.signal)
    videoMessage.value = '正在上传视频…'
    const result = await uploadVideo(file, percent => { uploadProgress.value = percent }, controller.signal)
    if (editing.value !== article || controller.signal.aborted) return
    article.videoUrl = result.fileID
    await validateVideo()
  } catch (e) {
    if ((e as Error).name !== 'AbortError' && editing.value === article) { videoState.value = 'invalid'; videoMessage.value = (e as Error).message }
  } finally {
    if (localUrl) URL.revokeObjectURL(localUrl)
    videoUploading.value = false
  }
}
function cancelVideo() {
  videoOperation?.abort()
  videoMessage.value = '操作已取消，可重新上传或校验'; videoState.value = 'idle'
}
function preview(article: Article) { previewing.value = clone(article) }

function closeDetails() {
  if (restoring.value) return
  inspecting.value = null; detailRequest++; restoreConfirm.value = false
}
async function openDetails(article: Article, mode: 'versions' | 'stats') {
  inspecting.value = clone(article); detailMode.value = mode
  versions.value = []; selectedVersion.value = null; statistics.value = null; restoreConfirm.value = false
  await loadDetails()
}
async function loadDetails() {
  if (!inspecting.value || restoring.value) return
  const request = ++detailRequest, id = inspecting.value.id, mode = detailMode.value
  detailLoading.value = true; detailError.value = ''
  try {
    if (mode === 'versions') {
      const result = await api<ArticleVersion[]>('admin.article.versions', { articleId: id })
      if (request !== detailRequest) return
      versions.value = result; selectedVersion.value = result[0] || null
      if (result[0] && inspecting.value) inspecting.value = { ...clone(result[0].article), version: result[0].version }
    } else {
      const result = await api<ArticleStats>('admin.article.stats', { articleId: id })
      if (request === detailRequest) statistics.value = result
    }
  } catch (e) { if (request === detailRequest) detailError.value = (e as Error).message }
  finally { if (request === detailRequest) detailLoading.value = false }
}
function selectVersion(version: ArticleVersion) { if (!restoring.value) { selectedVersion.value = version; restoreConfirm.value = false } }
async function beginRestore() {
  restoreConfirm.value = true
  await nextTick()
  restorePrompt.value?.scrollIntoView({ block: 'nearest' })
}
async function restore() {
  if (!inspecting.value || !selectedVersion.value || restoring.value || !restoreConfirm.value) return
  restoring.value = true; detailError.value = ''
  const source = selectedVersion.value.version
  try {
    const restored = await api<Article>('admin.article.restore', { articleId: inspecting.value.id, version: source, expectedVersion: inspecting.value.version || 1 })
    articles.value = [restored, ...articles.value.filter(article => article.id !== restored.id)]
    inspecting.value = null; detailRequest++; restoreConfirm.value = false
    notice.value = `已将 V${source} 恢复为草稿 V${restored.version || 1}。阅读统计已保留，可编辑后重新发布。`
    await load()
  } catch (e) { detailError.value = (e as Error).message; restoreConfirm.value = false }
  finally { restoring.value = false }
}
</script>

<template>
  <div class="toolbar"><div><h2>种植内容</h2><p>发布种植知识，安排发布时间，管理内容审核、标签与素材，追踪阅读表现</p></div><button v-permission="'article.write'" class="button primary" @click="edit()">＋ 新建内容</button></div>
  <div class="content-tabs content-module-tabs" role="tablist" aria-label="种植内容功能"><button v-for="tab in [{ key: 'courses', label: '课程编排' }, { key: 'questions', label: '课堂问答' }, { key: 'articles', label: '内容列表' }, { key: 'review', label: '内容审核' }, { key: 'tags', label: '标签库' }, { key: 'media', label: '素材库' }]" :key="tab.key" role="tab" :aria-selected="section === tab.key" :class="{ active: section === tab.key }" @click="section = tab.key as typeof section">{{ tab.label }}<span v-if="tab.key === 'review'"> {{ articles.filter(a => a.reviewStatus === 'PENDING').length }}</span></button></div>
  <CoursesView v-if="section === 'courses'" />
  <ClassroomQuestions v-if="section === 'questions'" />
  <ContentLibrary v-else-if="section === 'tags' || section === 'media'" :key="section" :mode="section" :resources="resources" :loading="resourceLoading" :load-error="resourceError" @refresh="loadResources"/>
  <template v-if="section === 'review'">
    <div class="content-controls"><div class="segmented"><button v-for="value in ['PENDING', 'APPROVED', 'REJECTED', 'UNSUBMITTED', 'ALL']" :key="value" :class="{ active: reviewFilter === value }" @click="reviewFilter = value">{{ reviewLabels[value] || '全部审核记录' }}</button></div><button class="text-button" :disabled="loading" @click="load">刷新</button></div>
    <p class="content-help">草稿可提交审核，通过后由运营人员安排发布。修改已送审内容后，需要重新提交审核。</p>
    <p v-if="loadError" class="alert error" role="alert">{{ loadError }}</p><p v-if="notice" class="content-notice" role="status">{{ notice }}</p>
    <div v-if="loading && !articles.length" class="panel content-empty">正在加载…</div><div v-else-if="!reviewItems.length && !loadError" class="panel content-empty">暂无{{ reviewLabels[reviewFilter] || '审核' }}内容</div>
    <div v-else class="content-grid"><article v-for="article in reviewItems" :key="article.id" class="panel content-card"><div class="content-card-body"><span class="tag">{{ reviewLabels[article.reviewStatus || ''] }}</span><h3>{{ article.title }}</h3><p>{{ article.summary }}</p><p v-if="article.reviewNote">审核意见：{{ article.reviewNote }}</p><small v-if="article.reviewedAt">{{ article.reviewedBy }} · {{ dateTime(article.reviewedAt) }}</small><footer><button class="text-button" @click="preview(article)">预览</button><button v-permission="'article.review'" v-if="article.reviewStatus === 'PENDING'" class="text-button" @click="openReview(article)">审核内容</button><button v-permission="'article.write'" class="text-button" @click="edit(article)">编辑</button><button v-permission="'article.write'" v-if="article.status === 'DRAFT' && ['REJECTED', 'UNSUBMITTED'].includes(article.reviewStatus || '')" class="text-button" :disabled="!!submitting" @click="submitReview(article)">重新提交</button></footer></div></article></div>
  </template>
  <template v-if="section === 'articles'">
  <div class="content-controls">
    <div class="segmented"><button v-for="status in statuses" :key="status.value" :class="{ active: filter === status.value }" @click="filter = status.value">{{ status.label }} <span>{{ status.count }}</span></button></div>
    <div class="content-search"><input v-model="keyword" type="search" placeholder="搜索标题、品种、阶段或标签" aria-label="搜索内容"/><button class="text-button" :disabled="loading" @click="load">{{ loading ? '刷新中…' : '刷新' }}</button></div>
  </div>
  <div v-if="loadError" class="alert error" role="alert">{{ loadError }} <button class="text-button" :disabled="loading" @click="load">重新加载</button></div>
  <div v-if="notice" class="content-notice" role="status">{{ notice }}</div>
  <div v-if="loading && !articles.length" class="panel content-empty" role="status">正在加载内容…</div>
  <div v-else-if="!filteredArticles.length && !loadError" class="panel content-empty"><h3>{{ articles.length ? '没有匹配的内容' : '从第一篇种植知识开始' }}</h3><p>{{ articles.length ? '试试其他搜索词或发布状态。' : '保存草稿、立即发布，或安排未来的发布时间。' }}</p></div>
  <div v-else class="content-grid">
    <article v-for="article in filteredArticles" :key="article.id" class="content-card panel">
      <img v-if="article.cover" :src="article.cover" :alt="article.title"/>
      <div class="content-card-body"><div class="content-meta"><span>{{ article.stage }} · {{ article.variety }}</span><span class="tag">{{ article.type === 'video' ? '视频' : '图文' }}</span></div><h3>{{ article.title }}</h3><p>{{ article.summary }}</p>
        <div class="content-state"><span class="status" :class="article.status.toLowerCase()">{{ statusName(article.status) }}</span><span>V{{ article.version || 1 }}</span><span>{{ number(article.readCount) }} 次阅读<span v-if="isDemo"> · 示例</span></span></div>
        <div class="content-timing"><template v-if="article.status === 'SCHEDULED'">◷ {{ dateTime(article.scheduledAt) }} 发布 · 北京时间</template><template v-else-if="article.status === 'PUBLISHED'">发布于 {{ dateTime(article.publishedAt) }}</template><template v-else>更新于 {{ dateTime(article.updatedAt) }}</template></div>
        <div class="content-resource-tags"><span v-for="tag in article.tags" :key="tag" class="tag">{{ tag }}</span><span v-if="article.reviewStatus" class="tag">{{ reviewLabels[article.reviewStatus] }}</span></div><p v-if="article.reviewNote" class="content-help">审核意见：{{ article.reviewNote }}</p>
        <footer><button v-permission="'article.write'" v-if="article.status === 'DRAFT' && article.reviewStatus !== 'PENDING' && article.reviewStatus !== 'APPROVED'" class="text-button" :disabled="!!submitting" @click="submitReview(article)">提交审核</button><button class="text-button" @click="preview(article)">预览</button><button class="text-button" @click="openDetails(article, 'stats')">阅读统计</button><button class="text-button" @click="openDetails(article, 'versions')">内容版本</button><button v-permission="'article.write'" class="text-button" @click="edit(article)">编辑</button></footer>
      </div>
    </article>
  </div>

  </template>

  <div v-if="editing" class="modal-backdrop" @click.self="closeEditor"><form class="drawer content-drawer" role="dialog" aria-modal="true" aria-labelledby="content-editor-title" novalidate @submit.prevent="save">
    <div class="drawer-head"><div><p class="eyebrow purple">CONTENT EDITOR</p><h2 id="content-editor-title">{{ editing.id ? '编辑内容' : '新建内容' }}<small v-if="editing.id" class="content-version">V{{ editing.version || 1 }}</small></h2></div><button type="button" class="close" :disabled="busy" aria-label="关闭编辑器" @click="closeEditor">×</button></div>
    <div class="content-tabs"><button type="button" :class="{ active: !showPreview }" @click="showPreview = false">编辑内容</button><button type="button" :class="{ active: showPreview }" @click="showPreview = true">预览当前内容</button><span>包含尚未保存的修改</span></div>
    <div v-show="!showPreview" class="drawer-body form-grid">
      <fieldset class="content-editor-fields full" :disabled="busy">
        <label class="full">标题<input v-model.trim="editing.title" required maxlength="120"/></label><label class="full">摘要<textarea v-model.trim="editing.summary" required maxlength="260" rows="2"/></label>
        <label>品种<input v-model.trim="editing.variety" required/></label><label>种植阶段<input v-model.trim="editing.stage" required/></label>
        <div class="full"><p class="editor-label">关联商品（小程序文章详情展示入口）</p><div class="content-resource-tags"><label v-for="p in linkProducts" :key="p.id" class="content-tag-choice"><input v-model="editing.productIds" type="checkbox" :value="p.id"/>{{ p.name }}</label></div><small v-if="!linkProducts.length">暂无商品数据。</small></div>
        <div class="full"><p class="editor-label">标签</p><div class="content-resource-tags"><label v-for="tag in [...new Set([...tagOptions.map(r => r.name), ...(editing.tags || [])])]" :key="tag" class="content-tag-choice"><input v-model="editing.tags" type="checkbox" :value="tag"/>{{ tag }}</label></div><small v-if="!tagOptions.length">可在标签库添加常用标签。</small><p v-if="resourceError" class="form-error">{{ resourceError }} <button type="button" class="text-button" @click="loadResources">重试加载库</button></p></div>
        <p v-if="editing.reviewStatus" class="full content-help">审核状态：{{ reviewLabels[editing.reviewStatus] }}。修改正文、标签或素材后需重新审核；审核通过后可以直接调整发布状态。</p>
        <label>类型<select v-model="editing.type"><option value="article">图文</option><option value="video">短视频</option></select></label><label>发布状态<select v-model="editing.status"><option value="DRAFT">保存草稿</option><option value="PUBLISHED">立即发布</option><option value="SCHEDULED">定时发布</option></select></label>
        <label v-if="editing.status === 'SCHEDULED'" class="full content-schedule">定时发布时间（北京时间 UTC+8）<input v-model="schedule" type="datetime-local" required/><small>此处始终按北京时间保存，到时自动发布。修改为草稿可取消定时发布。</small></label>
        <label v-else-if="editing.status === 'PUBLISHED'" class="full">展示发布日期（北京时间）<input v-model="publishDate" type="date"/><small>留空使用当前时间。立即发布后小程序用户即可阅读。</small></label>
        <label class="full">从素材库选择封面<select aria-label="选择封面素材" @change="useResource($event, 'image')"><option value="">选择图片素材</option><option v-for="r in resources.filter(r => r.kind === 'image')" :key="r.id" :value="r.id">{{ r.name }}</option></select></label>
        <label v-if="editing.type === 'video'" class="full">从素材库选择视频<select aria-label="选择视频素材" @change="useResource($event, 'video')"><option value="">选择视频素材</option><option v-for="r in resources.filter(r => r.kind === 'video')" :key="r.id" :value="r.id">{{ r.name }}</option></select></label>
        <label class="full">封面 URL / cloud fileID<input v-model.trim="editing.cover" required/><input class="file-input" type="file" accept="image/jpeg,image/png,image/webp" @change="uploadCover"/><small v-if="uploading">正在上传封面…</small></label>
        <div v-if="editing.type === 'video'" class="full video-upload-panel">
          <label>视频地址<input v-model.trim="editing.videoUrl" placeholder="https://…/video.mp4" aria-describedby="video-help" /></label>
          <div class="video-actions"><label class="video-file-label">上传 MP4<input type="file" accept="video/mp4,.mp4" aria-label="上传视频" @change="uploadVideoFile" /></label><button type="button" class="button secondary small" @click="validateVideo">校验可播放性</button></div>
          <p id="video-help" class="content-help">MP4 不超过 20MB，建议 H.264 / AAC 编码。地址需可公开访问；云存储 fileID 请先转换为 HTTPS 地址。</p>
          <p v-if="isDemo" class="content-help">演示上传仅在本次页面会话中可用。</p>
        </div>
        <div class="full"><p class="editor-label">正文</p><RichTextEditor :key="editing.id || 'new'" v-model="editing.contentHtml!" :disabled="saving || uploading || videoUploading || videoChecking" @busy="editorBusy = $event" @error="editorError = $event" /><p v-if="editorError" class="form-error" role="alert">{{ editorError }}</p><p class="content-help">支持标题、格式、列表、引用、链接和图片。每次保存会生成一个内容版本。</p></div>
      </fieldset>
      <div v-if="editing.type === 'video' && (videoUploading || videoChecking || videoMessage)" class="full video-check-status" :class="{ valid: videoState === 'valid', invalid: videoState === 'invalid' }" role="status"><p>{{ videoMessage }}</p><progress v-if="videoUploading" :value="uploadProgress" max="100" aria-label="视频上传进度" /><span v-if="videoUploading">{{ uploadProgress }}%</span><button v-if="videoUploading || videoChecking" type="button" class="text-button" @click="cancelVideo">取消操作</button></div>
    </div>
    <div v-if="showPreview" class="drawer-body content-preview-area"><ArticlePreview :article="editing" /></div>
    <div v-if="error" class="form-error content-save-error" role="alert">{{ error }}<button v-if="editing.id" type="button" class="text-button" :disabled="busy || loading" @click="reloadEditor">重新载入最新内容（替换当前填写）</button></div>
    <div class="drawer-foot"><button type="button" class="button secondary" :disabled="busy" @click="closeEditor">取消</button><button type="button" class="button secondary" @click="showPreview = !showPreview">{{ showPreview ? '返回编辑' : '预览' }}</button><button v-permission="'article.write'" class="button primary" :disabled="busy">{{ saving ? '保存中…' : busy ? '等待上传 / 校验…' : editing.status === 'SCHEDULED' ? '保存并安排发布' : '保存内容' }}</button></div>
  </form></div>

  <div v-if="previewing" class="modal-backdrop" @click.self="previewing = null"><section class="drawer content-preview-drawer" role="dialog" aria-modal="true" aria-labelledby="content-preview-title"><div class="drawer-head"><h2 id="content-preview-title">内容预览</h2><button class="close" aria-label="关闭预览" @click="previewing = null">×</button></div><div class="drawer-body content-preview-area"><ArticlePreview :article="previewing" /></div><div class="drawer-foot"><button class="button secondary" @click="previewing = null">关闭预览</button></div></section></div>

  <div v-if="inspecting" class="modal-backdrop" @click.self="closeDetails"><section class="drawer content-detail-drawer" role="dialog" aria-modal="true" aria-labelledby="content-detail-title">
    <div class="drawer-head"><div><p class="eyebrow purple">{{ detailMode === 'versions' ? 'CONTENT HISTORY' : 'READING INSIGHTS' }}</p><h2 id="content-detail-title">{{ detailMode === 'versions' ? '内容版本' : '阅读统计' }}</h2><p class="content-detail-subtitle">{{ inspecting.title }}</p></div><button class="close" :disabled="restoring" aria-label="关闭详情" @click="closeDetails">×</button></div>
    <div class="drawer-body">
      <div v-if="detailError" class="alert error" role="alert">{{ detailError }} <button class="text-button" :disabled="detailLoading || restoring" @click="loadDetails">重新加载</button></div>
      <div v-if="detailLoading" class="content-empty" role="status">{{ detailMode === 'versions' ? '正在加载内容版本…' : '正在加载阅读统计…' }}</div>
      <template v-else-if="detailMode === 'versions'">
        <p class="content-detail-note">每次保存和发布都会保留版本。恢复后生成新的草稿版本，保留原有阅读统计。</p>
        <div v-if="!versions.length && !detailError" class="content-empty">暂无历史版本</div>
        <div v-else class="content-version-list"><button v-for="version in versions" :key="version.version" class="content-version-row" :class="{ selected: selectedVersion?.version === version.version }" :disabled="restoring" @click="selectVersion(version)"><span><strong>V{{ version.version }}</strong><span v-if="version.version === (inspecting.version || 1)" class="tag presale">当前</span><span>{{ reasonName(version.reason) }}</span></span><small>{{ dateTime(version.createdAt) }} · {{ version.createdBy || '管理员' }}</small></button></div>
        <article v-if="selectedVersion" class="content-snapshot"><div class="content-snapshot-head"><span class="status" :class="selectedVersion.article.status.toLowerCase()">{{ statusName(selectedVersion.article.status) }}</span><span>{{ selectedVersion.article.type === 'video' ? '视频' : '图文' }} · {{ selectedVersion.article.variety }} · {{ selectedVersion.article.stage }}</span></div><img v-if="selectedVersion.article.cover" :src="selectedVersion.article.cover" alt="此版本封面"/><h3>{{ selectedVersion.article.title }}</h3><p class="content-snapshot-summary">{{ selectedVersion.article.summary }}</p><small v-if="selectedVersion.article.scheduledAt">原定时发布：{{ dateTime(selectedVersion.article.scheduledAt) }} · 北京时间</small><small v-if="selectedVersion.article.publishedAt">原发布日期：{{ dateTime(selectedVersion.article.publishedAt) }}</small><small v-if="selectedVersion.article.videoUrl">视频：{{ selectedVersion.article.videoUrl }}</small><p v-for="(paragraph, index) in selectedVersion.article.content" :key="index">{{ paragraph }}</p><details v-if="selectedVersion.article.contentHtml" class="content-html-snapshot"><summary>查看此版本富文本 HTML</summary><pre>{{ selectedVersion.article.contentHtml }}</pre></details></article>
        <div v-if="restoreConfirm && selectedVersion" ref="restorePrompt" class="content-restore-confirm" role="alert"><strong>将 V{{ selectedVersion.version }} 恢复为新的草稿 V{{ (inspecting.version || 1) + 1 }}？</strong><p>当前内容将退出发布状态并取消定时发布。历史版本和阅读统计会保留，重新发布后用户才能阅读。</p><div class="row-actions"><button v-permission="'article.write'" class="button secondary small" :disabled="restoring" @click="restoreConfirm = false">返回查看</button><button v-permission="'article.write'" class="button primary small" :disabled="restoring" @click="restore">{{ restoring ? '恢复中…' : '确认恢复为草稿' }}</button></div></div>
      </template>
      <template v-else-if="statistics">
        <div v-if="isDemo" class="content-demo-stat">以下为演示示例数据，用于预览统计功能；正式模式记录小程序实际阅读。</div>
        <div class="content-stats-grid"><div class="metric"><span>累计阅读次数 · PV</span><strong>{{ number(statistics.totalViews) }}</strong><small>内容详情页的有效打开次数</small></div><div class="metric"><span>累计阅读人数 · UV</span><strong>{{ number(statistics.totalReaders) }}</strong><small>按用户去重，恢复版本后保留</small></div><div class="metric"><span>今日阅读次数</span><strong>{{ number(statistics.todayViews) }}</strong><small>北京时间当天</small></div><div class="metric"><span>今日阅读人数</span><strong>{{ number(statistics.todayReaders) }}</strong><small>当天按用户去重</small></div></div>
        <div class="content-chart-head"><h3>近 14 天阅读趋势</h3><span>{{ number(periodViews) }} 次阅读 · 北京时间</span></div>
        <div v-if="!periodViews" class="content-stats-empty">这段时间暂无阅读。发布内容并分享给读者后，阅读数据会显示在这里。</div>
        <div class="content-chart" role="img" aria-label="近14天阅读次数与阅读人数柱状图，精确数值见下方表格"><div v-for="(day, index) in statistics.days" :key="day.date" class="content-chart-day" :title="`${day.date}：${day.views} 次阅读，${day.readers} 人`"><div class="content-chart-bars"><i class="views" :style="{ height: `${day.views / chartMax * 100}%` }"></i><i class="readers" :style="{ height: `${day.readers / chartMax * 100}%` }"></i></div><small>{{ index === 0 || index === statistics.days.length - 1 || index % 3 === 0 ? day.date.slice(5) : ' ' }}</small></div></div><div class="content-chart-legend"><span><i class="views"></i>阅读次数 PV</span><span><i class="readers"></i>阅读人数 UV</span></div>
        <details class="content-stats-table"><summary>查看每日数据</summary><table><thead><tr><th>日期（北京时间）</th><th>阅读次数 PV</th><th>阅读人数 UV</th></tr></thead><tbody><tr v-for="day in statistics.days.slice().reverse()" :key="day.date"><td>{{ day.date }}</td><td>{{ number(day.views) }}</td><td>{{ number(day.readers) }}</td></tr></tbody></table></details><p class="content-detail-note">同一读者在不同日期可分别计入当日 UV，累计 UV 按全部记录去重，因此每日 UV 之和可能大于累计人数。</p>
      </template>
    </div>
    <div class="drawer-foot"><button class="button secondary" :disabled="restoring" @click="closeDetails">关闭</button><button v-permission="'article.write'" v-if="detailMode === 'versions' && selectedVersion && !restoreConfirm" class="button primary" :disabled="detailLoading || restoring" @click="beginRestore">恢复 V{{ selectedVersion.version }} 为草稿</button></div>
  </section></div>
  <div v-if="reviewing" class="modal-backdrop"><section class="drawer content-preview-drawer" role="dialog" aria-modal="true" aria-labelledby="review-title"><div class="drawer-head"><h2 id="review-title">审核：{{ reviewing.title }}</h2><button class="close" :disabled="reviewBusy" aria-label="关闭审核" @click="reviewing = null">×</button></div><div class="drawer-body"><ArticlePreview :article="reviewing"/><label>审核意见<textarea v-model.trim="reviewNote" maxlength="500" rows="3" :disabled="reviewBusy" placeholder="退回时请说明修改原因"/></label><p v-if="reviewError" class="form-error" role="alert">{{ reviewError }}</p></div><div class="drawer-foot"><button class="button secondary" :disabled="reviewBusy" @click="reviewing = null">取消</button><button v-permission="'article.review'" class="button secondary" :disabled="reviewBusy" @click="decideReview('REJECTED')">退回修改</button><button v-permission="'article.review'" class="button primary" :disabled="reviewBusy" @click="decideReview('APPROVED')">审核通过</button></div></section></div>
</template>
