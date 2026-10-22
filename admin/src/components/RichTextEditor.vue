<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { EditorContent, useEditor } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'
import ImageExtension from '@tiptap/extension-image'
import { api } from '../api'
import { isSafeContentUrl, sanitizeContent } from '../content'

const props = withDefaults(defineProps<{ modelValue: string; disabled?: boolean }>(), { disabled: false })
const emit = defineEmits<{
  'update:modelValue': [value: string]
  busy: [value: boolean]
  error: [message: string]
}>()

const imageInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
const message = ref('')
const linkOpen = ref(false)
const linkHref = ref('')
const locked = computed(() => props.disabled || uploading.value)
let disposed = false
let externalRevision = 0
let lastEmitted = sanitizeContent(props.modelValue)
let linkSelection = { from: 1, to: 1 }
let editingLink = false

function reportError(value: string) {
  if (disposed) return
  message.value = value
  emit('error', value)
}

const editor = useEditor({
  content: lastEmitted,
  editable: !props.disabled,
  extensions: [
    StarterKit.configure({
      heading: { levels: [2, 3] },
      codeBlock: false,
      code: false,
      horizontalRule: false,
      link: {
        openOnClick: false,
        defaultProtocol: 'https',
        isAllowedUri: (url) => isSafeContentUrl(url),
        HTMLAttributes: { target: '_blank', rel: 'noopener noreferrer' },
      },
    }),
    ImageExtension.configure({ allowBase64: true }),
  ],
  editorProps: {
    attributes: { class: 'article-editor', role: 'textbox', 'aria-label': '文章正文', 'aria-multiline': 'true' },
    transformPastedHTML: sanitizeContent,
    handlePaste: (_view, event) => {
      const file = Array.from(event.clipboardData?.files || []).find((item) => item.type.startsWith('image/'))
      if (!file) return false
      event.preventDefault()
      void uploadImage(file)
      return true
    },
  },
  onUpdate: ({ editor: currentEditor }) => {
    lastEmitted = sanitizeContent(currentEditor.getHTML())
    linkOpen.value = false
    emit('update:modelValue', lastEmitted)
  },
  onCreate: ({ editor: currentEditor }) => currentEditor.setEditable(!locked.value, false),
})

watch(() => props.modelValue, (value) => {
  const instance = editor.value
  if (!instance) return
  const clean = sanitizeContent(value)
  // Parent updates echo the sanitized output. Do not replace the document on
  // that echo, which would reset the cursor and interrupt Chinese IME input.
  if (clean === lastEmitted || clean === sanitizeContent(instance.getHTML())) return
  externalRevision += 1
  instance.commands.setContent(clean, { emitUpdate: false })
  lastEmitted = clean
  linkOpen.value = false
  reportError('')
})
watch(locked, (value) => editor.value?.setEditable(!value, false))

function openLink() {
  const instance = editor.value
  if (!instance || locked.value) return
  const { from, to } = instance.state.selection
  linkSelection = { from, to }
  editingLink = instance.isActive('link')
  linkHref.value = String(instance.getAttributes('link').href || '')
  linkOpen.value = !linkOpen.value
  reportError('')
}

function applyLink() {
  const instance = editor.value
  if (!instance || locked.value) return
  const href = linkHref.value.trim()
  if (!isSafeContentUrl(href)) return reportError('请输入完整的 HTTPS 链接；本地调试地址可以使用 HTTP。')
  const chain = instance.chain().focus().setTextSelection(linkSelection)
  if (!editingLink && linkSelection.from === linkSelection.to) {
    chain.insertContent({ type: 'text', text: href, marks: [{ type: 'link', attrs: { href } }] }).run()
  } else {
    chain.extendMarkRange('link').setLink({ href }).run()
  }
  linkOpen.value = false
  reportError('')
}

function removeLink() {
  if (!editor.value || locked.value) return
  editor.value.chain().focus().setTextSelection(linkSelection).extendMarkRange('link').unsetLink().run()
  linkOpen.value = false
  reportError('')
}

function pickImage() {
  if (!locked.value) imageInput.value?.click()
}

async function imageChanged(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (file) await uploadImage(file)
}

async function uploadImage(file: File) {
  const instance = editor.value
  if (!instance || locked.value) return
  if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) return reportError('正文图片支持 JPG、PNG、WebP 格式。')
  if (!file.size || file.size > 4 * 1024 * 1024) return reportError('正文图片不能为空，且不能超过 4MB。')
  const { from, to } = instance.state.selection
  const revision = externalRevision
  uploading.value = true
  emit('busy', true)
  reportError('')
  linkOpen.value = false
  try {
    const dataUrl = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(String(reader.result))
      reader.onerror = () => reject(new Error('图片读取失败，请重新选择。'))
      reader.readAsDataURL(file)
    })
    if (!isSafeContentUrl(dataUrl, true)) throw new Error('图片内容或大小不符合要求，请重新选择。')
    const probe = new window.Image()
    probe.src = dataUrl
    await probe.decode().catch(() => { throw new Error('图片无法解码，请选择有效的 JPG、PNG 或 WebP 图片。') })
    const result = await api<{ fileID: string; url?: string }>('admin.media.upload', { dataUrl })
    const src = result.url || result.fileID
    if (!src || !isSafeContentUrl(src, true)) throw new Error('上传完成，但图片地址不可用。请检查媒体存储配置。')
    if (disposed || instance.isDestroyed || revision !== externalRevision) return
    instance.chain().focus().setTextSelection({ from, to }).setImage({ src, alt: file.name }).run()
  } catch (error) {
    reportError(error instanceof Error ? error.message : '图片上传失败，请重试。')
  } finally {
    uploading.value = false
    if (!disposed) emit('busy', false)
  }
}

onBeforeUnmount(() => {
  disposed = true
  if (uploading.value) emit('busy', false)
})
</script>

<template>
  <div class="rich-editor" :class="{ 'is-disabled': locked }">
    <div class="rich-toolbar" role="toolbar" aria-label="正文格式">
      <button type="button" :disabled="locked || !editor" :class="{ active: editor?.isActive('paragraph') }" :aria-pressed="editor?.isActive('paragraph') || false" title="正文段落" @mousedown.prevent @click="editor?.chain().focus().setParagraph().run()">正文</button>
      <button type="button" :disabled="locked || !editor" :class="{ active: editor?.isActive('heading', { level: 2 }) }" :aria-pressed="editor?.isActive('heading', { level: 2 }) || false" title="二级标题" @mousedown.prevent @click="editor?.chain().focus().toggleHeading({ level: 2 }).run()">H2</button>
      <button type="button" :disabled="locked || !editor" :class="{ active: editor?.isActive('heading', { level: 3 }) }" :aria-pressed="editor?.isActive('heading', { level: 3 }) || false" title="三级标题" @mousedown.prevent @click="editor?.chain().focus().toggleHeading({ level: 3 }).run()">H3</button>
      <span class="toolbar-divider" aria-hidden="true" />
      <button type="button" :disabled="locked || !editor" :class="{ active: editor?.isActive('bold') }" :aria-pressed="editor?.isActive('bold') || false" aria-label="粗体" title="粗体（Ctrl/⌘ + B）" @mousedown.prevent @click="editor?.chain().focus().toggleBold().run()"><strong>B</strong></button>
      <button type="button" :disabled="locked || !editor" :class="{ active: editor?.isActive('italic') }" :aria-pressed="editor?.isActive('italic') || false" aria-label="斜体" title="斜体（Ctrl/⌘ + I）" @mousedown.prevent @click="editor?.chain().focus().toggleItalic().run()"><em>I</em></button>
      <button type="button" :disabled="locked || !editor" :class="{ active: editor?.isActive('underline') }" :aria-pressed="editor?.isActive('underline') || false" aria-label="下划线" title="下划线（Ctrl/⌘ + U）" @mousedown.prevent @click="editor?.chain().focus().toggleUnderline().run()"><u>U</u></button>
      <span class="toolbar-divider" aria-hidden="true" />
      <button type="button" :disabled="locked || !editor" :class="{ active: editor?.isActive('bulletList') }" :aria-pressed="editor?.isActive('bulletList') || false" title="无序列表" @mousedown.prevent @click="editor?.chain().focus().toggleBulletList().run()">• 列表</button>
      <button type="button" :disabled="locked || !editor" :class="{ active: editor?.isActive('orderedList') }" :aria-pressed="editor?.isActive('orderedList') || false" title="有序列表" @mousedown.prevent @click="editor?.chain().focus().toggleOrderedList().run()">1. 列表</button>
      <button type="button" :disabled="locked || !editor" :class="{ active: editor?.isActive('blockquote') }" :aria-pressed="editor?.isActive('blockquote') || false" title="引用" @mousedown.prevent @click="editor?.chain().focus().toggleBlockquote().run()">❝ 引用</button>
      <button type="button" :disabled="locked || !editor" :class="{ active: editor?.isActive('link') || linkOpen }" :aria-pressed="editor?.isActive('link') || linkOpen || false" title="添加或编辑链接" @mousedown.prevent @click="openLink">链接</button>
      <button type="button" :disabled="locked || !editor" title="上传正文图片（不超过 4MB），也可直接粘贴图片" @mousedown.prevent @click="pickImage">{{ uploading ? '上传中…' : '＋ 图片' }}</button>
      <span class="toolbar-divider" aria-hidden="true" />
      <button type="button" :disabled="locked || !editor?.can().undo()" aria-label="撤销" title="撤销（Ctrl/⌘ + Z）" @mousedown.prevent @click="editor?.chain().focus().undo().run()">↶</button>
      <button type="button" :disabled="locked || !editor?.can().redo()" aria-label="重做" title="重做（Ctrl/⌘ + Shift + Z）" @mousedown.prevent @click="editor?.chain().focus().redo().run()">↷</button>
    </div>
    <div v-if="linkOpen" class="link-tools" role="group" aria-label="编辑链接">
      <input v-model="linkHref" type="url" :disabled="locked" aria-label="链接地址" placeholder="https://example.com" @keydown.enter.prevent="applyLink" @keydown.esc.prevent="linkOpen = false" />
      <button type="button" :disabled="locked" @click="applyLink">应用</button>
      <button v-if="editingLink" type="button" :disabled="locked" @click="removeLink">移除链接</button>
      <button type="button" :disabled="locked" @click="linkOpen = false">取消</button>
    </div>
    <EditorContent :editor="editor" class="editor-content" />
    <div class="editor-footnote">支持格式编辑与粘贴图片；正文图片支持 JPG、PNG、WebP，单张不超过 4MB。</div>
    <p v-if="message" class="editor-error" role="alert">{{ message }}</p>
    <p v-if="uploading" class="editor-progress" role="status">正在上传正文图片，请稍候…</p>
    <input ref="imageInput" class="hidden-image-input" type="file" accept="image/jpeg,image/png,image/webp" :disabled="locked" @change="imageChanged" />
  </div>
</template>

<style scoped>
.rich-editor { overflow: hidden; border: 1px solid #ded7e4; border-radius: 12px; background: #fff; font-weight: 400; }
.rich-toolbar { display: flex; flex-wrap: wrap; gap: 4px; align-items: center; padding: 8px; background: #faf8fc; border-bottom: 1px solid #e9e3ed; }
.rich-toolbar button, .link-tools button { min-height: 32px; padding: 5px 9px; border: 1px solid transparent; border-radius: 6px; background: transparent; color: #62566b; cursor: pointer; font: inherit; font-size: 13px; }
.rich-toolbar button:hover:enabled, .link-tools button:hover:enabled { background: #eee6f3; }
.rich-toolbar button.active { color: #6c427e; background: #eadff0; border-color: #d8c5e2; }
.rich-toolbar button:disabled, .link-tools button:disabled { cursor: default; opacity: .4; }
.rich-toolbar button:focus-visible, .link-tools button:focus-visible { outline: 2px solid #9e77b2; outline-offset: 1px; }
.toolbar-divider { width: 1px; height: 20px; margin: 0 3px; background: #ded7e4; }
.link-tools { display: flex; flex-wrap: wrap; gap: 6px; padding: 9px 12px; border-bottom: 1px solid #e9e3ed; background: #fbf9fc; }
.link-tools input { flex: 1; min-width: 150px; padding: 7px 9px; border: 1px solid #ded7e4; border-radius: 6px; font: inherit; font-size: 13px; }
.link-tools button { color: #765187; border-color: #ded7e4; background: #fff; }
.editor-content :deep(.article-editor) { min-height: 250px; padding: 18px 20px; outline: none; color: #302a35; line-height: 1.8; font-size: 15px; word-break: break-word; }
.editor-content :deep(.article-editor:focus) { box-shadow: inset 0 0 0 2px #cdb6d9; }
.editor-content :deep(.article-editor > :first-child) { margin-top: 0; }
.editor-content :deep(.article-editor > :last-child) { margin-bottom: 0; }
.editor-content :deep(p) { margin: 0 0 14px; }
.editor-content :deep(h2) { margin: 24px 0 12px; font-size: 22px; line-height: 1.5; }
.editor-content :deep(h3) { margin: 20px 0 10px; font-size: 18px; line-height: 1.5; }
.editor-content :deep(ul), .editor-content :deep(ol) { margin: 12px 0; padding-left: 24px; }
.editor-content :deep(ul) { list-style: disc; }
.editor-content :deep(ol) { list-style: decimal; }
.editor-content :deep(li) { margin: 6px 0; }
.editor-content :deep(li p) { margin: 0; }
.editor-content :deep(blockquote) { margin: 16px 0; padding: 10px 16px; border-left: 3px solid #8b5c9f; color: #62566b; background: #f8f5fa; }
.editor-content :deep(blockquote p:last-child) { margin-bottom: 0; }
.editor-content :deep(a) { color: #765187; text-decoration: underline; }
.editor-content :deep(img) { display: block; max-width: 100%; height: auto; margin: 16px auto; border-radius: 8px; }
.editor-content :deep(img.ProseMirror-selectednode) { outline: 3px solid #b58acb; }
.is-disabled .editor-content { background: #faf9fb; }
.editor-footnote { padding: 9px 12px; color: #8c8193; background: #faf8fc; border-top: 1px solid #eee8f2; font-size: 12px; line-height: 1.6; }
.editor-error { margin: 0; padding: 9px 12px; color: #b63843; background: #fff6f6; font-size: 13px; line-height: 1.6; }
.editor-progress { margin: 0; padding: 9px 12px; color: #765187; background: #f8f2fc; font-size: 13px; }
.hidden-image-input { display: none; }
@media (max-width: 600px) { .editor-content :deep(.article-editor) { padding: 14px; } .rich-toolbar { gap: 2px; } .toolbar-divider { margin: 0 1px; } }
</style>
