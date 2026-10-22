<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Article } from '../types'
import { paragraphsToHtml, sanitizeContent } from '../content'
import { isDemo } from '../api'
import { videoAddressError } from '../video'
import { publicationDate } from '../content-time'

const props = defineProps<{ article: Article }>()
const coverFailed = ref(false)
const videoFailed = ref(false)
const videoKey = ref(0)
watch(() => props.article.cover, () => { coverFailed.value = false })
watch(() => props.article.videoUrl, () => { videoFailed.value = false; videoKey.value++ })
const body = computed(() => sanitizeContent(props.article.contentHtml || paragraphsToHtml(props.article.content || [])))
const addressError = computed(() => videoAddressError(props.article.videoUrl || '', isDemo))
</script>

<template>
  <article class="article-preview">
    <p class="preview-caption">小程序阅读预览 · {{ article.status === 'PUBLISHED' ? '已选发布' : article.status === 'SCHEDULED' ? '待定时发布' : '草稿' }}</p>
    <img v-if="article.cover && !coverFailed" class="preview-cover" :src="article.cover" :alt="article.title || '内容封面'" @error="coverFailed = true" />
    <div v-else class="preview-cover preview-placeholder">{{ coverFailed ? '封面加载失败，请检查地址' : '尚未设置封面' }}</div>
    <div class="preview-copy">
      <p class="preview-meta">{{ article.stage || '种植阶段' }} · {{ article.variety || '品种' }} · {{ publicationDate(article.publishedAt) }}</p>
      <h2>{{ article.title || '尚未填写标题' }}</h2><p class="preview-summary">{{ article.summary || '尚未填写摘要' }}</p>
      <template v-if="article.type === 'video'">
        <div v-if="addressError || videoFailed" class="preview-video-error" role="status"><p>{{ addressError || '视频播放失败，请检查地址、访问权限和编码格式' }}</p><button v-if="!addressError" type="button" class="text-button" @click="videoFailed = false; videoKey++">重新加载视频</button></div>
        <video v-else :key="videoKey" class="preview-video" :src="article.videoUrl" :poster="coverFailed ? undefined : article.cover" controls playsinline preload="metadata" @error="videoFailed = true" />
      </template>
      <div v-if="body" class="preview-rich-text" v-html="body" @click="($event.target as HTMLElement).closest('a') && $event.preventDefault()" />
      <p v-else class="content-help">尚未填写正文</p>
      <p class="preview-disclaimer">种植建议需结合当地气候、土壤与植保要求。涉及农资使用时，请遵循产品标签、登记范围及安全间隔期。</p>
    </div>
  </article>
</template>
