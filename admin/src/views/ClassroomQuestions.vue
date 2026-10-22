<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { api, dateTime, isDemo } from '../api'
interface Question { id: string; articleId: string; articleTitle: string; question: string; answer: string; createdAt: string; answeredAt: string }
const items = ref<Question[]>([]), status = ref('pending'), loading = ref(false), more = ref(false), error = ref(''), saving = ref(''), notice = ref('')
const drafts = ref<Record<string, string>>({})
let request = 0, disposed = false
async function load(append = false) {
  if (append && (loading.value || !more.value)) return
  const version = ++request
  loading.value = true; error.value = ''
  if (!append) { items.value = []; more.value = false }
  try {
    const result = await api<{ items: Question[]; hasMore: boolean }>('admin.classroom.questions', { status: status.value, offset: append ? items.value.length : 0, limit: 20 })
    if (disposed || version !== request) return
    items.value = append ? [...items.value, ...result.items] : result.items; more.value = result.hasMore
    for (const q of result.items) drafts.value[q.id] = q.answer
  } catch (e) { if (!disposed && version === request) error.value = (e as Error).message }
  finally { if (!disposed && version === request) loading.value = false }
}
async function answer(q: Question) {
  if (saving.value) return
  const text = (drafts.value[q.id] || '').trim()
  if (!text || [...text].length > 2000) { error.value = '请填写回复，最多2000字'; return }
  saving.value = q.id; error.value = ''; notice.value = ''
  try {
    await api('admin.classroom.answer', { id: q.id, answer: text, expectedAnswer: q.answer })
    if (disposed) return
    notice.value = '回复已保存，学员可在课堂问答查看'; await load()
  } catch (e) { if (!disposed) error.value = (e as Error).message }
  finally { if (!disposed) saving.value = '' }
}
onMounted(() => load())
onUnmounted(() => { disposed = true; request++ })
</script>

<template>
  <section class="classroom-questions">
    <div class="toolbar"><div><h3>课堂问答</h3><p>查看学员问题并回复，提问和回复仅该学员及后台工作人员可见。</p></div><div class="qa-filters"><select v-model="status" :disabled="!!saving" @change="load()"><option value="pending">待回复</option><option value="answered">已回复</option><option value="">全部问题</option></select><button class="button" :disabled="loading || !!saving" @click="load()">刷新</button></div></div>
    <p v-if="isDemo" class="alert">演示问答使用后台独立的本地数据，不与小程序演示提问互通。</p>
    <p v-if="notice" role="status" class="alert">{{ notice }}</p>
    <div v-if="error" role="alert" class="alert error">{{ error }} <button class="text-button" :disabled="loading || !!saving" @click="load()">重新加载</button></div>
    <article v-for="q in items" :key="q.id" class="panel qa-card">
      <div class="qa-meta"><strong>{{ q.articleTitle }}</strong><span>{{ dateTime(q.createdAt) }} · {{ q.answer ? '已回复' : '待回复' }}</span></div>
      <p class="qa-text">{{ q.question }}</p>
      <p v-if="q.answer" class="qa-text qa-answer">已有回复：{{ q.answer }}<small>{{ dateTime(q.answeredAt) }}</small></p>
      <label>老师回复<textarea v-model="drafts[q.id]" :disabled="!!saving" maxlength="2000" rows="4" placeholder="填写实用、清晰的回复，最多2000字" /></label>
      <button v-permission="'article.write'" class="button primary" :disabled="!!saving || loading || !drafts[q.id]?.trim() || drafts[q.id]?.trim() === q.answer" @click="answer(q)">{{ saving === q.id ? '正在保存…' : q.answer ? '更新回复' : '提交回复' }}</button>
    </article>
    <p v-if="loading" role="status">正在加载问答…</p>
    <p v-else-if="!items.length && !error" class="empty">{{ status === 'pending' ? '暂无待回复的问题' : '暂无课堂问答' }}</p>
    <button v-if="more" class="button" :disabled="loading || !!saving" @click="load(true)">加载更多</button>
  </section>
</template>

<style scoped>
.qa-card { padding: 24px; margin-bottom: 18px; }
.qa-meta, .qa-filters { display: flex; gap: 14px; align-items: center; flex-wrap: wrap; }
.qa-meta span { color: #777; font-size: 13px; }
.qa-text { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.7; }
.qa-answer { padding: 14px; background: #f0f5ee; border-radius: 8px; }
.qa-answer small { display: block; color: #777; }
label { display: block; margin-bottom: 16px; }
textarea { display: block; width: 100%; box-sizing: border-box; margin-top: 8px; padding: 12px; border: 1px solid #dcd5ca; border-radius: 10px; font: inherit; line-height: 1.6; resize: vertical; }
.qa-filters select { border: 1px solid #dcd5ca; border-radius: 8px; padding: 9px 12px; font: inherit; background: white; }
</style>
