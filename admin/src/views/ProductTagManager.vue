<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { confirmAction } from '../confirm'
import type { ProductTag, SpecTemplate } from '../types'

const tags = ref<ProductTag[]>([])
const tagName = ref('')
const tagBusy = ref(false)
const tagError = ref('')
const editingTag = ref<ProductTag | null>(null)

const templates = ref<SpecTemplate[]>([])
const tplName = ref('')
const tplSpecs = ref<Array<{ name: string; values: string }>>([{ name: '', values: '' }])
const tplBusy = ref(false)
const tplError = ref('')
const editingTpl = ref<SpecTemplate | null>(null)

async function loadTags() {
  try { tags.value = (await api<{ items: ProductTag[] }>('admin.product.tag.list')).items }
  catch (e) { tagError.value = (e as Error).message }
}
async function loadTemplates() {
  try { templates.value = (await api<{ items: SpecTemplate[] }>('admin.spec.template.list')).items }
  catch (e) { tplError.value = (e as Error).message }
}
onMounted(() => { void loadTags(); void loadTemplates() })

async function saveTag() {
  const name = tagName.value.trim()
  if (!name) { tagError.value = '请输入标签名称'; return }
  tagBusy.value = true; tagError.value = ''
  try {
    await api('admin.product.tag.save', { id: editingTag.value?.id || '', name })
    tagName.value = ''; editingTag.value = null
    await loadTags()
  } catch (e) { tagError.value = (e as Error).message }
  finally { tagBusy.value = false }
}
function editTag(t: ProductTag) { editingTag.value = t; tagName.value = t.name }
function cancelTagEdit() { editingTag.value = null; tagName.value = '' }
async function deleteTag(t: ProductTag) {
  if (!(await confirmAction({ title: '删除标签', message: `删除标签「${t.name}」？已打标商品将自动移除该标签。`, danger: true, confirmText: '删除' }))) return
  tagBusy.value = true
  try { await api('admin.product.tag.delete', { id: t.id }); await loadTags() }
  catch (e) { tagError.value = (e as Error).message }
  finally { tagBusy.value = false }
}

function addSpecRow() { tplSpecs.value.push({ name: '', values: '' }) }
function removeSpecRow(i: number) { tplSpecs.value.splice(i, 1) }
function editTemplate(t: SpecTemplate) {
  editingTpl.value = t; tplName.value = t.name
  tplSpecs.value = t.specs.map(s => ({ name: s.name, values: s.values.join('，') }))
}
function cancelTplEdit() { editingTpl.value = null; tplName.value = ''; tplSpecs.value = [{ name: '', values: '' }] }
async function saveTemplate() {
  const name = tplName.value.trim()
  if (!name) { tplError.value = '请输入模板名称'; return }
  const specs = tplSpecs.value.map(s => ({ name: s.name.trim(), values: s.values.split(/[，,]/).map(v => v.trim()).filter(Boolean) }))
  tplBusy.value = true; tplError.value = ''
  try {
    await api('admin.spec.template.save', { id: editingTpl.value?.id || '', name, specs })
    cancelTplEdit(); await loadTemplates()
  } catch (e) { tplError.value = (e as Error).message }
  finally { tplBusy.value = false }
}
async function deleteTemplate(t: SpecTemplate) {
  if (!(await confirmAction({ title: '删除规格模板', message: `删除模板「${t.name}」？`, danger: true, confirmText: '删除' }))) return
  tplBusy.value = true
  try { await api('admin.spec.template.delete', { id: t.id }); await loadTemplates() }
  catch (e) { tplError.value = (e as Error).message }
  finally { tplBusy.value = false }
}
</script>

<template>
  <div class="tag-manager">
    <section class="panel">
      <div class="panel-head"><div><h3>商品标签</h3><p>标签改名全局生效；删除后商品自动移除该标签</p></div></div>
      <form class="tag-form" @submit.prevent="saveTag">
        <input v-model.trim="tagName" maxlength="20" placeholder="标签名称，如：新品 / 热卖 / 有机" :disabled="tagBusy" />
        <button class="button primary" :disabled="tagBusy">{{ editingTag ? '保存改名' : '添加标签' }}</button>
        <button v-if="editingTag" type="button" class="button secondary" @click="cancelTagEdit">取消</button>
      </form>
      <p v-if="tagError" class="form-error">{{ tagError }}</p>
      <div class="tag-list">
        <span v-for="t in tags" :key="t.id" class="tag-chip">{{ t.name }}
          <button type="button" @click="editTag(t)" aria-label="改名">改名</button>
          <button type="button" @click="deleteTag(t)" aria-label="删除">×</button>
        </span>
        <span v-if="!tags.length" class="subtle">暂无标签</span>
      </div>
    </section>
    <section class="panel">
      <div class="panel-head"><div><h3>规格模板</h3><p>定义规格维度与取值，建商品时一键生成 SKU 组合</p></div></div>
      <form @submit.prevent="saveTemplate">
        <label>模板名称<input v-model.trim="tplName" maxlength="30" placeholder="如：鲜果通用规格" :disabled="tplBusy" /></label>
        <div v-for="(row, i) in tplSpecs" :key="i" class="spec-row">
          <input v-model.trim="row.name" maxlength="20" placeholder="规格名，如：装量" :disabled="tplBusy" />
          <input v-model="row.values" placeholder="取值，逗号分隔，如：2斤装，5斤装" :disabled="tplBusy" />
          <button type="button" class="text-button" @click="removeSpecRow(i)" :disabled="tplSpecs.length <= 1">删除</button>
        </div>
        <div class="row-actions">
          <button type="button" class="button secondary" @click="addSpecRow" :disabled="tplSpecs.length >= 4">＋ 规格维度</button>
          <button class="button primary" :disabled="tplBusy">{{ editingTpl ? '保存模板' : '添加模板' }}</button>
          <button v-if="editingTpl" type="button" class="button secondary" @click="cancelTplEdit">取消</button>
        </div>
      </form>
      <p v-if="tplError" class="form-error">{{ tplError }}</p>
      <div class="tpl-list">
        <div v-for="t in templates" :key="t.id" class="tpl-card">
          <div><strong>{{ t.name }}</strong><small>{{ t.specs.map(s => `${s.name}(${s.values.length})`).join(' × ') }}</small></div>
          <div class="row-actions"><button type="button" class="text-button" @click="editTemplate(t)">编辑</button><button type="button" class="text-button" @click="deleteTemplate(t)">删除</button></div>
        </div>
        <span v-if="!templates.length" class="subtle">暂无模板</span>
      </div>
    </section>
  </div>
</template>

<style scoped>
.tag-manager { display: grid; gap: 16px; }
.tag-form { display: flex; gap: 10px; margin: 12px 0; flex-wrap: wrap; }
.tag-form input { flex: 1; min-width: 200px; padding: 10px; border: 1px solid var(--border); border-radius: 8px; }
.tag-list { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 8px; }
.tag-chip { display: inline-flex; align-items: center; gap: 6px; padding: 6px 10px; border-radius: 16px; background: var(--purple-soft); font-size: 13px; }
.tag-chip button { border: none; background: none; cursor: pointer; color: var(--purple); font-size: 12px; }
.spec-row { display: grid; grid-template-columns: 140px 1fr auto; gap: 8px; margin-top: 8px; align-items: center; }
.spec-row input { padding: 10px; border: 1px solid var(--border); border-radius: 8px; }
.tpl-list { display: grid; gap: 8px; margin-top: 12px; }
.tpl-card { display: flex; justify-content: space-between; align-items: center; padding: 10px 14px; border: 1px solid var(--border); border-radius: 10px; }
.tpl-card small { display: block; color: var(--muted); }
</style>
