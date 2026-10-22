<script setup lang="ts">
import { computed, ref } from 'vue'
import { money } from '../api'
import type { Article, HomeConfig, PrivacyPolicy, Product } from '../types'

const props = defineProps<{
  target: 'home' | 'privacy'
  mode: 'editing' | 'saved'
  home: HomeConfig | null
  privacy: PrivacyPolicy | null
  products: Product[]
  articles: Article[]
  demo: boolean
}>()
defineEmits<{ close: [] }>()

const bannerIndex = ref(0)
const detailsExpanded = ref(false)
const liveExpanded = ref(false)
const month = ref(0)
const detail = ref<{ title: string; paragraphs: string[]; productId?: string; cover?: string } | null>(null)
function openActivity(item: NonNullable<HomeConfig['activities']>[number]) {
  detail.value = { title: item.title || '活动标题待填写', cover: item.cover, paragraphs: [item.summary, item.startDate + ' 至 ' + item.endDate + ' · 北京时间', status(item.startDate, item.endDate), item.location ? '活动地点：' + item.location : '', item.content, item.reservationEnabled ? '开放活动预约' : '暂未开放预约', item.capacity ? `预约容量：${item.capacity} 人` : '预约不限额', item.waitlistEnabled ? '满额可候补，按加入顺序递补' : '满额不接受候补', item.productId ? '关联商品：' + (availableProducts.value.find(p => p.id === item.productId)?.name || '商品暂不可用') : ''], productId: status(item.startDate, item.endDate) === '进行中' && productExists(item.productId) ? item.productId : undefined }
}
const failedImages = ref(new Set<string>())
const today = new Date(Date.now() + 8 * 3600000).toISOString().slice(0, 10)
const banner = computed(() => props.home?.banners[bannerIndex.value])
const availableProducts = computed(() => props.products.filter(p => p.status === 'PUBLISHED' && p.skus.some(s => s.active !== false)).sort((a, b) => a.sortOrder - b.sortOrder))
const availableArticles = computed(() => props.articles.filter(a => a.status === 'PUBLISHED').sort((a, b) => b.publishedAt.localeCompare(a.publishedAt)))
const recommendedProducts = computed(() => {
  const ids = props.home?.productIds || []
  // The mini-program selects its first three products before excluding products without active SKUs.
  const candidates = ids.length
    ? ids.map(id => props.products.find(p => p.id === id)).filter((p): p is Product => !!p)
    : [...props.products].sort((a, b) => a.sortOrder - b.sortOrder).slice(0, 3)
  return candidates.filter(p => p.status === 'PUBLISHED' && p.skus.some(s => s.active !== false))
})
const recommendedArticles = computed(() => {
  const ids = props.home?.articleIds || []
  return ids.length
    ? ids.map(id => availableArticles.value.find(a => a.id === id)).filter((a): a is Article => !!a)
    : availableArticles.value.slice(0, 2)
})
const announcements = computed(() => (props.home?.announcements || []).filter(a => a.publishedAt && a.publishedAt <= today).sort((a, b) => b.publishedAt.localeCompare(a.publishedAt)))
const scheduledNoticeCount = computed(() => (props.home?.announcements || []).filter(a => a.publishedAt > today).length)
const unavailableReferences = computed(() => {
  const home = props.home
  if (!home) return []
  const messages: string[] = []
  for (const id of home.productIds) if (!productExists(id)) messages.push(`推荐商品 ${id || '未选择'} 暂不可用`)
  for (const id of home.articleIds) if (!availableArticles.value.some(a => a.id === id)) messages.push(`推荐文章 ${id || '未选择'} 暂不可用`)
  home.banners.forEach((item, index) => {
    if (item.target === 'product' && !productExists(item.targetId)) messages.push(`轮播位 ${index + 1} 的商品目标暂不可用`)
    if (item.target === 'article' && !availableArticles.value.some(a => a.id === item.targetId)) messages.push(`轮播位 ${index + 1} 的文章目标暂不可用`)
  })
  return messages
})
const calendar = computed(() => (props.home?.launchCalendar || []).filter(item => {
  if (!month.value) return true
  const start = new Date(`${item.startDate}T00:00:00Z`), end = new Date(`${item.endDate}T00:00:00Z`)
  // Bound malformed draft dates before iterating through a range.
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime()) || end < start || end.getUTCFullYear() - start.getUTCFullYear() > 5) return false
  for (let year = start.getUTCFullYear(); year <= end.getUTCFullYear(); year++) {
    if (new Date(Date.UTC(year, month.value - 1, 1)) <= end && new Date(Date.UTC(year, month.value, 0)) >= start) return true
  }
  return false
}))
function status(start: string, end: string, season = false) {
  if (!validDate(start) || !validDate(end) || start > end) return '日期待完善'
  return today < start ? (season ? '即将上市' : '即将开始') : today > end ? (season ? '本季结束' : '已结束') : (season ? '上市中' : '进行中')
}
function validDate(value:string) { const parsed = new Date(`${value}T00:00:00Z`); return /^\d{4}-\d{2}-\d{2}$/.test(value) && Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0,10) === value }
function showImage(url: string) { return !!url && !failedImages.value.has(url) }
function imageFailed(url: string) { failedImages.value = new Set([...failedImages.value, url]) }
function price(product: Product) { const sku = product.skus.find(s => s.active !== false); return sku ? `${money(sku.price)} 起` : '暂无在售规格' }
function productExists(id: string) { return availableProducts.value.some(p => p.id === id) }
function openProduct(id: string) {
  const product = availableProducts.value.find(p => p.id === id)
  detail.value = product
    ? { title: `商品详情 · ${product.name}`, paragraphs: [product.subtitle, price(product), product.description, ...product.highlights] }
    : { title: '商品暂不可用', paragraphs: ['当前选择的商品不存在、已下架或没有在售规格，请更换运营位关联商品。'] }
}
function openArticle(id: string) {
  const article = availableArticles.value.find(a => a.id === id)
  detail.value = article
    ? { title: `文章详情 · ${article.title}`, paragraphs: [article.summary, ...article.content] }
    : { title: '文章暂不可用', paragraphs: ['当前选择的文章不存在或未发布，请更换运营位关联文章。'] }
}
function openTarget(target: string, id = '') {
  if (target === 'product') return openProduct(id)
  if (target === 'article') return openArticle(id)
  const names: Record<string, string> = { shop: '商城', classroom: '种植课堂', cooperation: '合作' }
  detail.value = { title: `跳转至${names[target] || '未设置的页面'}`, paragraphs: [`小程序将打开${names[target] || '所选'}页面；此处用于确认入口文字与跳转目标。`] }
}
function cycleBanner(delta: number) { const count = props.home?.banners.length || 1; bannerIndex.value = (bannerIndex.value + delta + count) % count }
</script>

<template>
  <section class="panel preview-panel" aria-label="内容预览">
    <div class="panel-head">
      <div><h3>{{ target === 'home' ? '首页' : '隐私政策' }}预览 <span class="preview-badge" :class="mode">{{ mode === 'saved' ? '已回读保存值' : '当前编辑 · 未保存' }}</span></h3><p>布局供内容核对，实际展示请在小程序中验收。北京时间 {{ today }}</p></div>
      <button type="button" class="text-button" @click="$emit('close')">关闭预览</button>
    </div>
    <p v-if="demo" class="preview-note">演示预览仅使用当前后台的演示数据，与小程序演示数据各自独立；跨端更新需两端连接同一真实后端。</p>
    <div class="preview-layout">
      <div class="phone-preview">
        <div class="phone-title">迟记葡萄园 <span>{{ target === 'home' ? '首页' : '隐私政策' }}</span></div>
        <template v-if="target === 'home' && home">
          <div v-if="banner" class="preview-hero">
            <img v-if="showImage(banner.image)" :src="banner.image" alt="轮播图片" @error="imageFailed(banner.image)"/>
            <span v-if="banner.image && !showImage(banner.image)" class="hero-image-note">图片暂不可用</span><span v-else-if="demo" class="hero-image-note">演示素材 · 示意图</span>
          </div>
          <div class="orchard-preview-intro"><h4>{{ home.storyTitle || '把一串葡萄，慢慢种好。' }}</h4><p>{{ home.storyCopy || '种植、照料、采摘，是果园里平常的日子。' }}</p><small>迟记 · 与你分享果园的日常</small></div>
          <div class="orchard-preview-entrances"><button type="button" @click="openTarget('shop')"><span>01</span><strong>当季葡萄</strong><span>↗</span></button><button type="button" @click="openTarget('classroom')"><span>02</span><strong>种植手记</strong><span>↗</span></button><button type="button" @click="openTarget('cooperation')"><span>03</span><strong>联系果园</strong><span>↗</span></button></div>
          <button type="button" class="orchard-preview-more" :aria-expanded="liveExpanded" @click="liveExpanded = !liveExpanded">查看果园现状 ↗</button>
          <div v-if="liveExpanded" class="preview-section"><h4>果园现状 · 双机位</h4><section v-for="view in [{id:'orchard' as const,title:'果园内部'},{id:'stall' as const,title:'园外摊位'}]" :key="view.id"><h5>{{view.title}}</h5><p>{{home.live?.[view.id]?.enabled && home.live?.[view.id]?.url ? '已配置播放地址，等待真机验证画面' : '直播暂未开启'}}</p><p v-if="home.live?.[view.id]?.hours">直播时间：{{home.live[view.id].hours}}</p></section><small>此处预览机位状态；实际直播在小程序中观看。</small></div>
          <button type="button" class="orchard-preview-more" :aria-expanded="detailsExpanded" @click="detailsExpanded = !detailsExpanded">{{ detailsExpanded ? '收起果园近况 −' : '看看果园近况 ＋' }}</button>
          <div v-if="detailsExpanded" class="preview-content">
            <div v-if="home.banners.length > 1" class="banner-controls"><button type="button" aria-label="上一张轮播" @click="cycleBanner(-1)">‹</button><span>{{ bannerIndex + 1 }} / {{ home.banners.length }}</span><button type="button" aria-label="下一张轮播" @click="cycleBanner(1)">›</button></div>
            <button v-if="banner" type="button" class="preview-link" @click="openTarget(banner.target, banner.targetId)">{{ banner.button || '查看运营内容' }} →</button>
            <section class="preview-section"><h4>公告 <small>果园最新消息</small></h4><button v-for="(item, index) in announcements" :key="index" type="button" class="notice-item" @click="detail = { title: item.title, paragraphs: [item.publishedAt, item.content] }"><span>{{ item.title }}<small>{{ item.publishedAt }}</small></span><span>›</span></button><p v-if="!announcements.length" class="preview-empty">暂无公告</p></section>
            <section class="preview-section"><h4>活动专区</h4><button v-for="(item, index) in home.activities" :key="index" type="button" class="activity-item" @click="openActivity(item)"><span class="preview-pill">{{ status(item.startDate, item.endDate) }}</span><strong>{{ item.title || '活动标题待填写' }}</strong><p>{{ item.summary }}</p><small>{{ item.startDate }} — {{ item.endDate }} · {{ item.reservationEnabled ? '开放预约 · ' : '' }}查看详情 →</small></button><p v-if="!home.activities?.length" class="preview-empty">暂无活动，敬请期待</p></section>
            <section class="preview-section"><h4>上市日历</h4><div class="month-controls"><button v-for="(_, index) in 13" :key="index" type="button" :class="{ active: month === index }" @click="month = index">{{ index ? index + '月' : '全部' }}</button></div><div v-for="(item, index) in calendar" :key="index" class="calendar-item"><strong>{{ item.variety || '品种待填写' }}</strong><span class="preview-pill">{{ status(item.startDate, item.endDate, true) }}</span><p>{{ item.startDate }} — {{ item.endDate }}</p><p>{{ item.note }}</p><button v-if="productExists(item.productId)" type="button" class="preview-link" @click="openProduct(item.productId)">查看商品 →</button></div><p v-if="!calendar.length" class="preview-empty">暂无{{ month ? month + '月' : '' }}上市安排</p><small>上市时间随天气与成熟情况调整，以实际供应为准。</small></section>
            <section class="preview-section"><div class="section-title"><h4>当季鲜果</h4><button type="button" class="preview-link" @click="openTarget('shop')">全部商品 →</button></div><div class="product-row"><button v-for="item in recommendedProducts" :key="item.id" type="button" class="product-card" @click="openProduct(item.id)"><img v-if="showImage(item.cover)" :src="item.cover" :alt="item.name" @error="imageFailed(item.cover)"/><div v-else class="image-placeholder">商品图片</div><div><span class="preview-pill">{{ item.saleMode === 'presale' ? '预约鲜采' : '当季现货' }}</span><strong>{{ item.name }}</strong><p>{{ item.subtitle }}</p><b>{{ price(item) }}</b></div></button></div><p v-if="!recommendedProducts.length" class="preview-empty">暂无可展示的推荐商品</p></section>
            <section class="preview-section story"><small>ORCHARD NOTE</small><h4>{{ home.storyTitle }}</h4><p>{{ home.storyCopy }}</p><button type="button" class="preview-link" @click="openTarget('classroom')">去种植课堂 →</button></section>
            <section class="preview-section"><div class="section-title"><h4>果园里的知识</h4><button type="button" class="preview-link" @click="openTarget('classroom')">更多内容 →</button></div><button v-for="item in recommendedArticles" :key="item.id" type="button" class="article-item" @click="openArticle(item.id)"><img v-if="showImage(item.cover)" :src="item.cover" :alt="item.title" @error="imageFailed(item.cover)"/><div><small>{{ item.stage }} · {{ item.variety }}</small><strong>{{ item.title }}</strong><p>{{ item.summary }}</p></div></button><p v-if="!recommendedArticles.length" class="preview-empty">暂无可展示的推荐文章</p></section>
            <section class="preview-section" v-if="home.customerService"><h4>合作与个人中心客服</h4><template v-if="home.customerService.enabled"><p>服务时间：{{home.customerService.hours}}</p><p>微信在线客服：{{home.customerService.wechat ? '开启' : '关闭'}}</p><p v-if="home.customerService.phone">电话：{{home.customerService.phone}}</p><p v-if="home.customerService.responseTime">合作回复：{{home.customerService.responseTime}}</p></template><p v-else>客服暂未开放</p><small>客服入口展示预览；此处不会发起会话或拨号。</small></section>
            <button type="button" class="cooperation-item" @click="openTarget('cooperation')"><strong>{{ home.cooperationTitle }}</strong><p>{{ home.cooperationCopy }}</p><span>合作咨询 →</span></button>
          </div>
          <div class="phone-tabs"><span class="active">首页</span><span>商城</span><span>种植课堂</span><span>合作</span><span>我的</span></div>
        </template>
        <article v-else-if="target === 'privacy' && privacy" class="policy-preview">
          <div class="policy-state" :class="{ published: privacy.published }">{{ privacy.published ? mode === 'editing' ? '拟正式发布 · 保存后用户可见' : '正式发布 · 已保存发布状态' : '草稿 · 仅后台预览，用户不可见' }}</div>
          <h4>{{ privacy.title || '政策标题待填写' }}</h4><p class="policy-meta">版本 {{ privacy.version || '待填写' }} · 生效日期 {{ privacy.effectiveDate || '待填写' }}</p>
          <div class="policy-identity"><p>运营主体：{{ privacy.operator || '待填写' }}</p><p>联系地址：{{ privacy.address || '待填写' }}</p><p>隐私联系方式：{{ privacy.contact || '待填写' }}</p></div>
          <p v-for="(paragraph, index) in privacy.content" :key="index" class="policy-paragraph">{{ paragraph || '本节正文待填写' }}</p>
        </article>
      </div>
      <aside class="preview-explainer">
        <h4>核对内容</h4>
        <p>{{ mode === 'editing' ? '这是打开预览时的编辑快照。修改后请重新点击“预览当前编辑”，保存前不会影响用户端。' : '这是从后台接口重新读取的保存值。页面展示仍需在小程序端确认。' }}</p>
        <template v-if="target === 'home'"><p>首张轮播图片用作首页果园照片，果园介绍展示在图片下方；三个常用入口置于介绍之后。公告、活动、推荐和上市日历默认收起，点击“看看果园近况”展开核对。</p><p v-if="scheduledNoticeCount">有 {{ scheduledNoticeCount }} 条公告尚未到发布日期，用户端暂不展示。</p><p>图片加载失败时显示占位内容，请检查图片地址在小程序端是否可访问。</p><p v-for="warning in unavailableReferences" :key="warning" class="reference-warning">{{ warning }}</p></template>
        <p v-else-if="privacy && !privacy.published">草稿不会作为用户端正式政策展示。发布前请填写真实运营主体、联系方式与地址。</p>
        <section v-if="detail" class="preview-detail" role="status"><img v-if="showImage(detail.cover || '')" :src="detail.cover" alt="活动专题封面" style="width:100%;max-height:220px;object-fit:cover;border-radius:12px" @error="imageFailed(detail.cover || '')" /><p v-else-if="detail.cover">活动封面暂不可用</p><h4>{{ detail.title }}</h4><p v-for="(paragraph, index) in detail.paragraphs" :key="index">{{ paragraph }}</p><button v-if="detail.productId" type="button" class="button small secondary" @click="openProduct(detail.productId)">查看关联商品</button><button type="button" class="text-button" @click="detail = null">收起详情</button></section>
        <p v-else-if="target === 'home'" class="detail-placeholder">点击预览中的入口，在这里查看目标与详情。</p>
      </aside>
    </div>
  </section>
</template>

<style scoped>
.preview-panel{margin-top:24px;overflow:hidden}.preview-badge{display:inline-block;margin-left:8px;padding:5px 9px;border-radius:20px;font-size:11px;color:#8b6129;background:#f5e6c5}.preview-badge.saved{color:var(--green);background:var(--green-soft)}.preview-note{margin:18px 24px 0;padding:12px 15px;border-radius:10px;color:#765b25;background:#f7edcf;font-size:12px;line-height:1.7}.preview-layout{display:grid;grid-template-columns:minmax(0,420px) minmax(200px,1fr);gap:28px;padding:24px}.phone-preview{border:1px solid var(--border);border-radius:22px;overflow:hidden;background:var(--cream);font-size:12px}.phone-title{display:flex;justify-content:space-between;padding:17px 18px;background:white;font-weight:750}.phone-title span{color:var(--muted);font-weight:400}.preview-hero{position:relative;min-height:240px;display:flex;align-items:center;background:linear-gradient(130deg,#3f6042,#572b5c);color:white}.preview-hero>img{position:absolute;width:100%;height:100%;object-fit:cover;opacity:.5}.hero-content{position:relative;padding:26px 22px;min-width:0}.hero-content small{font-size:9px;letter-spacing:.18em}.hero-content h4{margin:14px 0 10px;font-family:var(--serif);font-size:26px;line-height:1.4;overflow-wrap:anywhere}.hero-content p{color:#f0eaf0;line-height:1.7}.hero-content button{margin-top:20px;padding:9px 14px;border:0;border-radius:8px;color:#42234c;background:#eddba6;font-size:12px;font-weight:700}.banner-controls{display:flex;align-items:center;justify-content:center;gap:18px;background:#eae5df}.banner-controls button{width:32px;border:0;background:transparent;color:var(--purple);font-size:24px}.banner-controls span{font-size:10px}.promise-strip{display:flex;justify-content:space-around;padding:13px 5px;background:#fffdf9;font-size:10px;color:var(--green)}.preview-content{padding:16px}.preview-section{margin-bottom:20px;padding:15px;border:1px solid var(--border);border-radius:13px;background:var(--paper)}.preview-section h4{margin:0 0 12px;font-family:var(--serif);font-size:18px}.preview-section h4 small{font-size:10px;font-family:inherit;font-weight:400;color:var(--muted)}.preview-section small{font-size:10px;color:var(--muted);line-height:1.6}.preview-section p{line-height:1.7;overflow-wrap:anywhere}.preview-section button{font:inherit;text-align:left}.notice-item{display:flex;justify-content:space-between;align-items:center;width:100%;padding:10px 0;border:0;border-bottom:1px solid var(--border);background:transparent;color:inherit}.notice-item small{display:block;margin-top:5px}.preview-empty{color:var(--muted);font-size:11px;padding:10px 0}.activity-item{display:block;width:100%;padding:13px;margin-top:10px;border:0;border-radius:10px;background:var(--green-soft);color:#344a33}.activity-item strong{display:block;margin-top:10px;font-size:14px}.activity-item p{margin:7px 0}.preview-pill{display:inline-flex;padding:4px 7px;border-radius:15px;color:var(--green);background:#e5eee1;font-size:9px}.activity-item .preview-pill{color:var(--purple);background:white}.month-controls{display:flex;gap:5px;overflow:auto;padding-bottom:12px}.month-controls button{flex-shrink:0;padding:6px 9px;border:0;border-radius:20px;background:var(--cream);color:var(--muted);font-size:10px}.month-controls button.active{color:white;background:var(--purple)}.calendar-item{padding:12px 0;border-bottom:1px solid var(--border)}.calendar-item .preview-pill{float:right}.calendar-item p{margin-top:6px;font-size:10px;color:var(--muted)}.preview-link{padding:5px 0;border:0;background:transparent;color:var(--purple);font-size:10px!important}.section-title{display:flex;justify-content:space-between;align-items:start}.product-row{display:flex;gap:10px;overflow:auto;padding-bottom:5px}.product-card{width:155px;flex-shrink:0;padding:0;border:1px solid var(--border);border-radius:10px;overflow:hidden;background:white;color:inherit}.product-card>img,.image-placeholder{width:100%;height:108px;object-fit:cover}.image-placeholder{display:flex;justify-content:center;align-items:center;background:#e7e0d6;color:var(--muted);font-size:11px}.product-card>div:not(.image-placeholder){padding:10px}.product-card strong{display:block;margin-top:8px;font-size:12px}.product-card p{margin:5px 0;font-size:10px;color:var(--muted)}.product-card b{color:var(--purple);font-size:13px}.story{background:#f0eadf}.story h4{margin-top:10px}.story p{color:var(--muted)}.article-item{display:flex;gap:10px;width:100%;padding:12px 0;border:0;border-bottom:1px solid var(--border);color:inherit;background:transparent}.article-item img{width:68px;height:78px;object-fit:cover;border-radius:7px}.article-item strong{display:block;margin:5px 0;font-size:12px}.article-item p{color:var(--muted);font-size:10px}.cooperation-item{width:100%;padding:18px;border:0;border-radius:12px;text-align:left;color:white;background:var(--purple);font:inherit}.cooperation-item strong{font-family:var(--serif);font-size:17px}.cooperation-item p{margin:8px 0;color:#e2d4e6;font-size:11px;line-height:1.7}.cooperation-item span{font-size:11px}.phone-tabs{display:flex;justify-content:space-around;padding:17px 8px;border-top:1px solid var(--border);background:white;color:var(--muted);font-size:10px}.phone-tabs .active{color:var(--purple);font-weight:750}.preview-explainer{min-width:0;align-self:start;position:sticky;top:124px}.preview-explainer h4{font-size:14px;margin:0 0 12px}.preview-explainer>p{margin-top:13px;color:var(--muted);font-size:12px;line-height:1.9}.preview-detail{margin-top:22px;padding:18px;border:1px solid var(--border);border-radius:12px;background:var(--cream)}.preview-detail p{margin:12px 0;font-size:12px;line-height:1.9;white-space:pre-wrap;overflow-wrap:anywhere}.detail-placeholder{padding:16px;border:1px dashed var(--border);border-radius:12px}.policy-preview{padding:24px;background:var(--paper)}.policy-state{padding:10px;border-radius:8px;color:#8b6129;background:#f5e6c5;font-size:11px}.policy-state.published{color:var(--green);background:var(--green-soft)}.policy-preview h4{margin:22px 0 10px;font-size:23px;font-family:var(--serif)}.policy-meta{font-size:10px;color:var(--muted)}.policy-identity{margin:22px 0;padding:13px;border-radius:10px;background:var(--cream)}.policy-identity p{margin:6px 0;font-size:11px;line-height:1.8;overflow-wrap:anywhere}.policy-paragraph{margin:24px 0;line-height:1.9;white-space:pre-wrap;overflow-wrap:anywhere}
.preview-explainer .reference-warning{color:var(--danger)}
.phone-preview:has(.orchard-preview-intro){background:#f8f6f0;color:#303c2d}
.phone-preview:has(.orchard-preview-intro) .preview-hero{margin:20px 20px 0;min-height:0;height:325px;border-radius:3px;background:#e5e7dc}
.phone-preview:has(.orchard-preview-intro) .preview-hero>img{opacity:1;object-fit:cover;object-position:center}
.orchard-preview-intro{padding:26px 20px 23px}.orchard-preview-intro h4{font-family:var(--serif);font-size:22px;font-weight:500;line-height:1.6;margin:0}.orchard-preview-intro p{font-size:12px;line-height:1.95;color:#747868;white-space:pre-line;margin:10px 0 14px}.orchard-preview-intro small{font-size:10px;color:#858679}
.orchard-preview-entrances{margin:0 20px;border-top:1px solid #dcded1}.orchard-preview-entrances button{display:flex;align-items:center;gap:14px;width:100%;padding:20px 0;border:0;border-bottom:1px solid #dcded1;background:transparent;color:#526344;text-align:left}.orchard-preview-entrances strong{flex:1;font-family:var(--serif);font-size:16px;font-weight:500}.orchard-preview-entrances span{font-size:11px;color:#858679}
.orchard-preview-more{display:block;width:calc(100% - 40px);margin:18px 20px 24px;padding:10px 0;border:0;text-align:left;background:transparent;color:#5a684c;font-size:13px}
.hero-image-note{position:absolute;right:10px;bottom:8px;z-index:2;padding:3px 6px;border-radius:5px;color:white;background:rgba(0,0,0,.45);font-size:9px}
@media(max-width:1250px){.preview-layout{grid-template-columns:minmax(0,1fr)}.phone-preview{max-width:420px;margin:auto;width:100%}.preview-explainer{position:static}.preview-panel .panel-head{gap:12px}}
</style>
