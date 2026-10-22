<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, isDemo } from '../api'
import type { Review } from '../../../miniprogram/utils/types'
const items = ref<Review[]>([]), page = ref(1), total = ref(0), hasMore = ref(false), busy = ref(false), error = ref(''), saving = ref('')
const drafts = ref<Record<string,string>>({})
const preview = ref('')
const status = ref(''), reasons = ref<Record<string,string>>({})
const statusLabel = (item: Review) => item.status === 'PENDING' ? '待审核' : item.status === 'REJECTED' ? '已驳回' : '已通过'
async function load(target = page.value) {
  if (busy.value || saving.value) return
  busy.value = true; error.value = ''
  try { const result = await api<{items: Review[]; total: number; hasMore: boolean}>('admin.review.list', { page: target, status: status.value }); items.value = result.items; total.value = result.total; hasMore.value = result.hasMore; page.value = target }
  catch (e) { error.value = (e as Error).message }
  finally { busy.value = false }
}
async function moderate(item: Review, decision: 'APPROVED' | 'REJECTED') {
  if (saving.value || busy.value) return
  const reason = (reasons.value[item.id] || '').trim()
  if (decision === 'REJECTED' && (!reason || Array.from(reason).length > 500)) { error.value = '请填写1–500字驳回原因'; return }
  saving.value = item.id; error.value = ''
  try {
    const result = await api<Review>('admin.review.moderate', { id: item.id, status: decision, reason })
    Object.assign(item, result); item.rejectReason = result.rejectReason || ''; delete reasons.value[item.id]
    saving.value = ''
    await load(page.value)
    if (!items.value.length && page.value > 1 && !error.value) await load(page.value - 1)
  } catch (e) { error.value = (e as Error).message }
  finally { saving.value = '' }
}
async function reply(item: Review) {
  if (saving.value || busy.value) return
  const content = (drafts.value[item.id] || '').trim()
  if (!content || Array.from(content).length > 500) { error.value = '请填写1–500字回复'; return }
  saving.value = item.id; error.value = ''
  try { const result = await api<Review>('admin.review.reply', { id: item.id, content }); Object.assign(item, result); delete drafts.value[item.id] }
  catch (e) { error.value = (e as Error).message }
  finally { saving.value = '' }
}
onMounted(() => load())
</script>
<template>
  <section class="panel">
    <div class="panel-head"><div><h2>商品评价</h2><p class="subtle">共 {{total}} 条评价 · 新评价和追评审核通过后公开展示，每条评价可回复一次。</p></div><button class="button secondary" :disabled="busy || !!saving" @click="load()">刷新</button></div>
    <div class="review-filter"><label>审核状态 <select v-model="status" :disabled="busy || !!saving" @change="load(1)"><option value="">全部评价</option><option value="PENDING">待审核</option><option value="APPROVED">已通过</option><option value="REJECTED">已驳回</option></select></label></div>
    <p v-if="isDemo" class="demo-note">演示评价独立保存在本浏览器，与小程序演示数据各自独立。</p>
    <p v-if="error" class="form-error" role="alert">{{error}}</p><p v-if="busy" class="subtle">正在加载评价…</p>
    <p v-if="!busy && !error && !items.length" class="subtle">暂无商品评价</p>
    <article v-for="item in items" :key="item.id" class="review-card">
      <div class="review-heading"><strong>匿名买家 · {{item.rating}} 星</strong><span class="subtle">商品 {{item.productId}} · {{item.createdAt}}</span></div>
      <p><strong>{{statusLabel(item)}}</strong><small v-if="item.moderatedAt">审核时间：{{item.moderatedAt}}</small></p>
      <p v-if="item.rejectReason" class="form-error">驳回原因：{{item.rejectReason}}</p>
      <p class="review-text">{{item.content}}</p><div class="photos"><button v-for="photo in item.images" :key="photo" class="photo" aria-label="查看评价晒图" @click="preview = photo"><img :src="photo" alt="买家晒图" loading="lazy"/></button></div>
      <div v-if="item.appendAt" class="review-extra"><strong>追加评价</strong><small>{{item.appendAt}}</small><p class="review-text">{{item.appendContent}}</p><div class="photos"><button v-for="photo in item.appendImages" :key="photo" class="photo" aria-label="查看追评晒图" @click="preview = photo"><img :src="photo" alt="追评晒图" loading="lazy"/></button></div></div>
      <div class="review-extra"><strong>评价审核</strong><label v-if="item.status !== 'REJECTED'">驳回原因<textarea v-model="reasons[item.id]" maxlength="500" placeholder="驳回时必填，将向买家展示" :disabled="!!saving || busy"/></label><div class="review-actions"><button v-permission="'review.reply'" v-if="item.status === 'PENDING' || item.status === 'REJECTED'" class="button primary" :disabled="!!saving || busy" @click="moderate(item, 'APPROVED')">审核通过</button><button v-permission="'review.reply'" v-if="item.status !== 'REJECTED'" class="button secondary" :disabled="!!saving || busy" @click="moderate(item, 'REJECTED')">驳回评价</button></div></div>
      <div v-if="item.reply" class="review-extra"><strong>商家回复</strong><small>{{item.replyAt}}</small><p class="review-text">{{item.reply}}</p></div>
      <form v-else @submit.prevent="reply(item)"><label>商家回复<textarea v-model="drafts[item.id]" maxlength="500" placeholder="回应买家的品尝体验（最多500字）" :disabled="!!saving || busy"/></label><button v-permission="'review.reply'" class="button primary" :disabled="!!saving || busy">{{saving === item.id ? '正在提交…' : '发布回复'}}</button></form>
    </article>
    <div class="panel-head"><button class="button secondary" :disabled="page <= 1 || busy || !!saving" @click="load(page-1)">上一页</button><span>第 {{page}} 页</span><button class="button secondary" :disabled="!hasMore || busy || !!saving" @click="load(page+1)">下一页</button></div>
  </section>
  <div v-if="preview" class="image-overlay" role="dialog" aria-modal="true" aria-label="评价图片" @click="preview = ''"><button class="button secondary" @click="preview = ''">关闭图片</button><img :src="preview" alt="评价图片大图" @click.stop/></div>
</template>
<style scoped>
.review-filter{padding:0 24px}.review-actions{display:flex;gap:12px;flex-wrap:wrap}
.review-card{margin:20px 24px;padding:22px 0;border-bottom:1px solid var(--border)}.review-heading{display:flex;justify-content:space-between;gap:16px;flex-wrap:wrap}.review-text{white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.9;margin:14px 0}.review-extra{padding:16px;margin:16px 0;border-radius:12px;background:var(--cream)}small{display:block;margin-top:6px;color:var(--muted)}.photos{display:flex;flex-wrap:wrap;gap:10px}.photo{padding:0;border:0;background:none;cursor:zoom-in}.photo img{width:100px;height:100px;border-radius:10px;object-fit:cover}textarea{width:100%;min-height:90px;margin:10px 0}.image-overlay{position:fixed;inset:0;z-index:100;background:#000b;display:flex;align-items:center;justify-content:center;gap:20px;flex-direction:column}.image-overlay>img{max-width:90vw;max-height:80vh;object-fit:contain}
</style>
