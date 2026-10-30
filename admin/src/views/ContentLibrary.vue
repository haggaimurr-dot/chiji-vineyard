<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, isDemo } from '../api'
import { validateResource } from '../content-library'
import type { ContentResource } from '../types'

const props = defineProps<{ mode: 'tags' | 'media'; resources: ContentResource[]; loading: boolean; loadError: string }>()
const emit = defineEmits<{ refresh: [] }>()
const keyword = ref(''), kind = ref('ALL'), tagFilter = ref(''), editing = ref<ContentResource | null>(null), tagInput = ref('')
const error = ref(''), notice = ref(''), busy = ref(false), removing = ref<ContentResource | null>(null)
const allTags = computed(() => { const s = new Set<string>(); for (const r of props.resources) for (const t of r.tags || []) s.add(t); return [...s].sort() })
const items = computed(() => props.resources.filter(r => (props.mode === 'tags' ? r.kind === 'tag' : r.kind !== 'tag') && (kind.value === 'ALL' || r.kind === kind.value) && (!tagFilter.value || (r.tags || []).includes(tagFilter.value)) && `${r.name} ${r.note || ''} ${(r.tags || []).join(' ')}`.toLowerCase().includes(keyword.value.trim().toLowerCase())))
function edit(resource?: ContentResource) { error.value = ''; editing.value = resource ? { ...resource } : { id: '', name: '', kind: props.mode === 'tags' ? 'tag' : 'image', url: '', note: '', tags: [], version: 0 }; tagInput.value = (editing.value.tags || []).join('，') }
async function save() {
  if (!editing.value || busy.value) return
  error.value = ''
  try {
    editing.value.tags = tagInput.value.split(/[，,、\s]+/).map(t => t.trim()).filter(Boolean).slice(0, 10)
    validateResource(editing.value, isDemo); busy.value = true
    await api('admin.content.resource.save', { resource: editing.value })
    editing.value = null; notice.value = '已保存'; emit('refresh')
  } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
async function remove() {
  if (!removing.value || busy.value) return
  busy.value = true; error.value = ''
  try { await api('admin.content.resource.delete', { resource: removing.value }); removing.value = null; notice.value = '已移出库，已有内容中的标签和素材地址保留。'; emit('refresh') }
  catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
async function upload(event: Event) {
  const input = event.target as HTMLInputElement, file = input.files?.[0]; input.value = ''
  if (!file || !editing.value || busy.value) return
  error.value = ''
  if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type) || !file.size || file.size > 4 * 1024 * 1024) { error.value = '请上传不超过4MB的 JPG、PNG 或 WebP 图片'; return }
  busy.value = true
  try {
    const dataUrl = await new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result)); reader.onerror = () => reject(new Error('读取图片失败')); reader.readAsDataURL(file) })
    const result = await api<{ fileID: string }>('admin.media.upload', { dataUrl })
    editing.value.url = result.fileID; editing.value.name ||= file.name
  } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
</script>

<template>
  <div class="toolbar"><div><h3>{{ mode === 'tags' ? '标签库' : '素材库' }}</h3><p>{{ mode === 'tags' ? '维护常用标签，在内容编辑时关联。移除标签后，已有内容保留原标签。' : '集中维护图片和视频地址，在内容编辑时选择封面或视频。' }}</p></div><button v-permission="mode === 'tags' ? 'article.write' : 'media.upload'" class="button primary" @click="edit()">＋ {{ mode === 'tags' ? '新增标签' : '新增素材' }}</button></div>
  <div class="content-controls"><div class="content-search"><input v-model="keyword" type="search" :placeholder="mode === 'tags' ? '搜索标签名称或备注' : '搜索素材名称或备注'" aria-label="搜索库"/><select v-if="mode === 'media'" v-model="kind" aria-label="素材类型"><option value="ALL">全部素材</option><option value="image">图片</option><option value="video">视频</option></select><select v-model="tagFilter" aria-label="按标签筛选"><option value="">全部标签</option><option v-for="t in allTags" :key="t" :value="t">{{ t }}</option></select></div><button class="text-button" :disabled="loading" @click="emit('refresh')">刷新</button></div>
  <p v-if="loadError" class="alert error" role="alert">{{ loadError }} <button class="text-button" @click="emit('refresh')">重试</button></p>
  <p v-if="notice" class="content-notice" role="status">{{ notice }}</p>
  <div v-if="loading && !resources.length" class="panel content-empty">正在加载…</div>
  <div v-else-if="!items.length && !loadError" class="panel content-empty">{{ keyword || kind !== 'ALL' || tagFilter ? '没有匹配的记录' : mode === 'tags' ? '暂无标签，添加第一个常用标签。' : '暂无素材，上传图片或登记视频地址。' }}</div>
  <div v-else class="content-grid">
    <article v-for="resource in items" :key="resource.id" class="panel content-card">
      <img v-if="resource.kind === 'image'" :src="resource.url" :alt="resource.name" loading="lazy"/>
      <div class="content-card-body"><span class="tag">{{ resource.kind === 'tag' ? '标签' : resource.kind === 'image' ? '图片' : '视频' }}</span><h3>{{ resource.name }}</h3><p v-if="resource.tags && resource.tags.length" class="res-tags"><span v-for="t in resource.tags" :key="t" class="res-tag">{{ t }}</span></p><p>{{ resource.note || '暂无备注' }}</p><a v-if="resource.url" :href="resource.url" target="_blank" rel="noopener noreferrer">查看素材</a><footer><button v-permission="mode === 'tags' ? 'article.write' : 'media.upload'" class="text-button" @click="edit(resource)">编辑</button><button v-permission="mode === 'tags' ? 'article.write' : 'media.upload'" class="text-button" @click="removing = resource; error = ''">移出库</button></footer></div>
    </article>
  </div>
  <div v-if="editing" class="modal-backdrop" @click.self="!busy && (editing = null)"><form class="drawer" role="dialog" aria-modal="true" aria-labelledby="resource-title" @submit.prevent="save"><div class="drawer-head"><h2 id="resource-title">{{ editing.id ? '编辑' : '新增' }}{{ mode === 'tags' ? '标签' : '素材' }}</h2><button type="button" class="close" :disabled="busy" aria-label="关闭" @click="editing = null">×</button></div><div class="drawer-body form-grid"><fieldset class="content-editor-fields full" :disabled="busy"><label class="full">名称<input v-model.trim="editing.name" maxlength="80" required/></label><label v-if="mode === 'media'">类型<select v-model="editing.kind" :disabled="!!editing.id"><option value="image">图片</option><option value="video">视频</option></select></label><label v-if="mode === 'media'" class="full">素材地址<input v-model.trim="editing.url" required placeholder="https://…"/><input v-if="editing.kind === 'image'" type="file" accept="image/jpeg,image/png,image/webp" aria-label="上传素材图片" @change="upload"/><small>图片支持上传（最多4MB）；视频请登记可公开播放的 HTTPS 地址。</small></label><label class="full">标签<input v-model.trim="tagInput" maxlength="200" placeholder="多个标签用逗号/空格分隔，最多10个"/><small>用于筛选检索，例如：果园、采摘、夏季</small></label><label class="full">备注<textarea v-model.trim="editing.note" maxlength="500" rows="3"/></label></fieldset><p v-if="error" class="form-error full" role="alert">{{ error }}</p></div><div class="drawer-foot"><button type="button" class="button secondary" :disabled="busy" @click="editing = null">取消</button><button class="button primary" :disabled="busy">{{ busy ? '处理中…' : '保存' }}</button></div></form></div>
  <div v-if="removing" class="modal-backdrop"><section class="drawer" role="dialog" aria-modal="true" aria-labelledby="remove-resource-title"><div class="drawer-head"><h2 id="remove-resource-title">移出“{{ removing.name }}”？</h2></div><div class="drawer-body"><p>移出后无法从库中选择；已关联文章的标签和素材地址保留，素材文件也会保留。</p><p v-if="error" class="form-error" role="alert">{{ error }}</p></div><div class="drawer-foot"><button class="button secondary" :disabled="busy" @click="removing = null">取消</button><button class="button primary" :disabled="busy" @click="remove">确认移出</button></div></section></div>
</template>
