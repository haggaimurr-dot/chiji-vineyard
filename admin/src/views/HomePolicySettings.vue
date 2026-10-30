<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, toRaw, watch } from 'vue'
import { api, isDemo } from '../api'
import OperationsPreview from '../components/OperationsPreview.vue'
import ActivityManagement from './ActivityManagement.vue'
import type { HomeConfig, PrivacyPolicy, Product, Article } from '../types'
import LiveReplayManager from './LiveReplayManager.vue'
type ConfigKey = 'home' | 'privacy'
type Config = { home: HomeConfig; privacy: PrivacyPolicy }
const props=defineProps<{disabled?:boolean}>()
const emit=defineEmits<{saved:[key:ConfigKey];busy:[value:boolean]}>()
const home=ref<HomeConfig|null>(null), privacy=ref<PrivacyPolicy|null>(null)
const savedHome=ref<HomeConfig|null>(null), savedPrivacy=ref<PrivacyPolicy|null>(null)
const products=ref<Product[]>([]),articles=ref<Article[]>([]),error=ref(''),message=ref('')
const loading=ref(false),busy=ref<ConfigKey|''>(''),pendingReadback=ref<ConfigKey|''>('')
const uploading=ref(false)
const working=computed(()=>!!busy.value||uploading.value)
watch(working,value=>emit('busy',value),{flush:'sync'})
const preview=ref<{key:ConfigKey;mode:'editing'|'saved';home:HomeConfig|null;privacy:PrivacyPolicy|null;products:Product[];articles:Article[];generation:number}|null>(null)
const previewElement=ref<HTMLElement|null>(null)
let mounted=true, generation=0
let pendingSnapshot: HomeConfig | PrivacyPolicy | null = null
const clone = <T,>(value:T):T => JSON.parse(JSON.stringify(toRaw(value))) as T
const normalizeHome = (value:HomeConfig):HomeConfig => ({...value,live:{orchard:{enabled:false,url:'',hours:'',...value.live?.orchard},stall:{enabled:false,url:'',hours:'',...value.live?.stall}},customerService:value.customerService||{enabled:false,wechat:false,phone:"",hours:"",responseTime:""},activities:value.activities||[],announcements:value.announcements||[],launchCalendar:value.launchCalendar||[]})
const fingerprint = (value:unknown) => JSON.stringify(value,(_key,item:unknown)=>item&&typeof item==='object'&&!Array.isArray(item)?Object.fromEntries(Object.entries(item).sort(([a],[b])=>a.localeCompare(b))):item)
const same = (a:unknown,b:unknown) => fingerprint(a) === fingerprint(b)
const homeDirty=computed(()=>home.value !== null && !same(home.value,savedHome.value))
const privacyDirty=computed(()=>privacy.value !== null && !same(privacy.value,savedPrivacy.value))
const feedback = (e:unknown) => e instanceof Error ? e.message : '请求失败，请重试'
onBeforeUnmount(()=>{mounted=false})
onMounted(load)

async function readConfig() {
  const [config,ps,as]=await Promise.all([api<Config>('admin.config.get'),api<Product[]>('admin.product.list'),api<Article[]>('admin.article.list')])
  return {config:{home:normalizeHome(config.home),privacy:config.privacy},products:ps.filter(p=>p.status==='PUBLISHED').sort((a,b)=>a.sortOrder-b.sortOrder),articles:as.filter(a=>a.status==='PUBLISHED').sort((a,b)=>b.publishedAt.localeCompare(a.publishedAt))}
}
async function load() {
  if(loading.value||working.value)return
  loading.value=true;error.value=''
  try {
    const result=await readConfig()
    if(!mounted)return
    home.value=clone(result.config.home);privacy.value=clone(result.config.privacy)
    savedHome.value=clone(result.config.home);savedPrivacy.value=clone(result.config.privacy)
    products.value=result.products;articles.value=result.articles
  } catch(e) {if(mounted)error.value=feedback(e)}
  finally {if(mounted)loading.value=false}
}
async function openPreview(key:ConfigKey,mode:'editing'|'saved') {
  const h=mode==='editing'?home.value:savedHome.value,p=mode==='editing'?privacy.value:savedPrivacy.value
  if((key==='home'&&!h)||(key==='privacy'&&!p))return
  preview.value={key,mode,home:clone(h),privacy:clone(p),products:clone(products.value),articles:clone(articles.value),generation:++generation}
  await nextTick()
  if(mounted){previewElement.value?.scrollIntoView({behavior:'smooth',block:'start'});previewElement.value?.focus({preventScroll:true})}
}
async function confirmSaved(key:ConfigKey,snapshot:HomeConfig|PrivacyPolicy|null) {
  const result=await readConfig()
  if(!mounted)return
  products.value=result.products;articles.value=result.articles
  const latest=key==='home'?result.config.home:result.config.privacy
  const current=key==='home'?home.value:privacy.value
  // Preserve edits made after an earlier failed readback, and surface concurrent server changes.
  if(key==='home'){
    savedHome.value=clone(result.config.home)
    if(same(current,snapshot)&&same(latest,snapshot))home.value=clone(result.config.home)
  }else{
    savedPrivacy.value=clone(result.config.privacy)
    if(same(current,snapshot)&&same(latest,snapshot))privacy.value=clone(result.config.privacy)
  }
  pendingReadback.value='';pendingSnapshot=null
  message.value=same(latest,snapshot)
    ? key==='home'?'首页配置已保存并回读确认':result.config.privacy.published?'隐私政策已发布并回读确认':'政策草稿已保存并回读确认，用户端不可见'
    : '已回读服务端最新值，与本次提交不同。当前编辑已保留，请核对已保存预览。'
  await openPreview(key,'saved')
}
async function save(key:ConfigKey) {
  if(props.disabled||working.value||loading.value||pendingReadback.value)return
  const current=key==='home'?home.value:privacy.value
  if(!current)return
  const snapshot=clone(current)
  if (key==='home' && snapshot.live) { for (const id of ['orchard','stall'] as const) { const ch = (snapshot.live as any)[id]; if (ch && typeof ch.noticeAt === 'string' && ch.noticeAt.length === 16) ch.noticeAt = ch.noticeAt + ':00+08:00' } }
  busy.value=key;error.value='';message.value=''
  let acknowledged=false
  try{
    await api(`admin.${key}.save`,{[key]:snapshot})
    if(!mounted)return
    acknowledged=true;emit('saved',key);pendingReadback.value=key;pendingSnapshot=snapshot
    if(key==='home')savedHome.value=null;else savedPrivacy.value=null
    if(preview.value?.key===key&&preview.value.mode==='saved')preview.value=null
    await confirmSaved(key,snapshot)
  }catch(e){if(mounted)error.value=acknowledged?`写入已确认，但保存值回读失败：${feedback(e)}。请重试回读后再确认展示。`:`保存未确认：${feedback(e)}。当前编辑已保留。`}
  finally{if(mounted)busy.value=''}
}
async function retryReadback() {
  const key=pendingReadback.value
  if(props.disabled||!key||busy.value)return
  busy.value=key;error.value='';message.value=''
  try{await confirmSaved(key,pendingSnapshot)}catch(e){if(mounted)error.value=`保存值回读失败：${feedback(e)}。请再次重试。`}
  finally{if(mounted)busy.value=''}
}
function move<T>(ids:T[],index:number,delta:number){const j=index+delta;if(j<0||j>=ids.length)return;[ids[index],ids[j]]=[ids[j],ids[index]]}
async function uploadBanner(event:Event,index:number) {
  const input=event.target as HTMLInputElement,file=input.files?.[0]
  input.value=''
  if(!file||props.disabled||working.value||pendingReadback.value)return
  const banner=home.value?.banners[index]
  if(!banner)return
  error.value='';message.value=''
  if(!['image/jpeg','image/png','image/webp'].includes(file.type)){error.value='请上传 JPG、PNG 或 WebP 图片';return}
  if(!file.size||file.size>4*1024*1024){error.value='图片不能为空，且不能超过 4MB';return}
  uploading.value=true
  try{
    const dataUrl=await new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result));reader.onerror=()=>reject(new Error('图片读取失败，请重试'));reader.onabort=()=>reject(new Error('图片读取已取消'));reader.readAsDataURL(file)})
    const result=await api<{fileID:string}>('admin.media.upload',{dataUrl})
    if(mounted&&home.value?.banners[index]===banner){banner.image=result.fileID;message.value='轮播图片已替换到当前编辑，请预览并保存首页配置'}
  }catch(e){if(mounted)error.value=`图片上传失败：${feedback(e)}。原图片已保留。`}
  finally{uploading.value=false}
}
</script>
<template>
 <p v-if="loading" class="config-feedback" role="status">正在加载首页配置、隐私政策与推荐内容…</p>
 <p v-if="isDemo" class="config-feedback demo-feedback">演示保存仅更新当前后台的演示数据；后台与小程序演示模式各自独立。真实跨端更新需连接同一后端。</p>
 <p v-if="uploading" class="config-feedback" role="status">正在上传轮播图片…</p><p v-if="error" class="form-error" role="alert">{{error}}</p><p v-if="message" class="success-text" role="status">{{message}}</p>
 <button v-if="error&&!home&&!loading" type="button" class="button secondary recovery-button" @click="load">重新加载配置</button>
 <button v-if="pendingReadback" type="button" class="button secondary recovery-button" :disabled="props.disabled||working" @click="retryReadback">{{ busy ? '正在回读…' : '重试回读保存值' }}</button>
 <form v-if="home" class="panel settings-card operations-config" @submit.prevent="save('home')">
  <div class="panel-head"><div><h3>首页运营位 <span class="change-state">{{ pendingReadback === 'home' ? '保存值待回读确认' : homeDirty ? '有未保存修改' : '与已读取保存值一致' }}</span></h3><p>首张轮播图作为首页果园照片，建议使用果园采摘实拍；果园介绍位于图片下方，其余运营内容在“果园近况”中展开。保存后首页再次显示时读取。</p></div><div class="preview-actions"><button type="button" class="button small secondary" :disabled="working" @click="openPreview('home','editing')">预览当前编辑</button><button type="button" class="text-button" :disabled="working||!savedHome" @click="openPreview('home','saved')">查看已保存预览</button></div></div>
  <fieldset class="settings-fields" :disabled="props.disabled||working">
  <fieldset v-for="(banner,index) in home.banners" :key="index"><legend>轮播位 {{index+1}}</legend><div class="form-grid">
   <label>标题<input v-model.trim="banner.title" required maxlength="80"/></label><label>说明<input v-model.trim="banner.copy"/></label>
   <label class="full">轮播图片（可留空）<input v-model.trim="banner.image" type="text" placeholder="https://…"/><input type="file" accept="image/jpeg,image/png,image/webp" :aria-label="'上传轮播图片 '+(index+1)" :disabled="!!pendingReadback" @change="uploadBanner($event,index)"/><small>支持 JPG、PNG、WebP，单张不超过 4MB；正式模式上传后生成 HTTPS 地址。</small></label>
   <label>按钮文字<input v-model.trim="banner.button" required/></label><label>跳转类型<select v-model="banner.target" @change="banner.targetId='' "><option value="shop">商城</option><option value="classroom">种植课堂</option><option value="cooperation">合作</option><option value="product">商品详情</option><option value="article">文章详情</option></select></label>
   <label v-if="banner.target==='product'">目标商品<select v-model="banner.targetId" required><option value="">请选择</option><option v-for="p in products" :value="p.id">{{p.name}}</option></select></label>
   <label v-if="banner.target==='article'">目标文章<select v-model="banner.targetId" required><option value="">请选择</option><option v-for="a in articles" :value="a.id">{{a.title}}</option></select></label>
  </div><button type="button" class="text-button" :disabled="index===0" @click="[home.banners[index-1],home.banners[index]]=[home.banners[index],home.banners[index-1]]">上移</button><button type="button" class="text-button" :disabled="home.banners.length===1" @click="home.banners.splice(index,1)">删除</button></fieldset>
  <button type="button" class="button secondary" :disabled="home.banners.length>=5" @click="home.banners.push({title:'',copy:'',image:'',button:'查看详情',target:'shop',targetId:''})">＋ 添加轮播位</button>
  <h4>推荐商品（留空则按商品排序展示前三件）</h4><div v-for="(id,index) in home.productIds" :key="index" class="selection-row"><select v-model="home.productIds[index]" required><option value="">请选择</option><option v-for="p in products" :value="p.id">{{p.name}}</option><option v-if="id&&!products.some(p=>p.id===id)" :value="id">{{id}}（已下架，请更换）</option></select><button type="button" @click="move(home.productIds,index,-1)">上移</button><button type="button" @click="home.productIds.splice(index,1)">删除</button></div><button type="button" class="text-button" :disabled="home.productIds.length>=12" @click="home.productIds.push('')">＋ 推荐商品</button>
  <h4>推荐文章（留空则展示最新两篇）</h4><div v-for="(id,index) in home.articleIds" :key="index" class="selection-row"><select v-model="home.articleIds[index]" required><option value="">请选择</option><option v-for="a in articles" :value="a.id">{{a.title}}</option><option v-if="id&&!articles.some(a=>a.id===id)" :value="id">{{id}}（未发布，请更换）</option></select><button type="button" @click="move(home.articleIds,index,-1)">上移</button><button type="button" @click="home.articleIds.splice(index,1)">删除</button></div><button type="button" class="text-button" :disabled="home.articleIds.length>=6" @click="home.articleIds.push('')">＋ 推荐文章</button>
<h4>公告</h4><fieldset v-for="(item,index) in home.announcements" :key="index"><legend>公告 {{index+1}}</legend><div class="form-grid"><label>标题<input v-model.trim="item.title" type="text" maxlength="80" required/></label><label>正文<textarea v-model.trim="item.content" rows="4" maxlength="2000" required/></label><label>发布日期<input v-model.trim="item.publishedAt" type="date" required/></label></div><button type="button" class="text-button" :disabled="index===0" @click="home.announcements && move(home.announcements,index,-1)">上移</button><button type="button" class="text-button" @click="home.announcements?.splice(index,1)">删除</button></fieldset><button type="button" class="text-button" :disabled="(home.announcements?.length || 0)>=20" @click="home.announcements?.push({title:'',content:'',publishedAt:''})">＋ 添加公告</button>
<h4>上市日历</h4><fieldset v-for="(item,index) in home.launchCalendar" :key="index"><legend>上市日历 {{index+1}}</legend><div class="form-grid"><label>品种<input v-model.trim="item.variety" type="text" maxlength="80" required/></label><label>预计上市日期<input v-model.trim="item.startDate" type="date" required/></label><label>预计结束日期<input v-model.trim="item.endDate" type="date" :min="item.startDate" required/></label><label>说明<input v-model.trim="item.note" type="text" maxlength="200"/></label><label>关联商品（可留空）<select v-model="item.productId"><option value="">不关联商品</option><option v-for="p in products" :key="p.id" :value="p.id">{{p.name}}</option><option v-if="item.productId&&!products.some(p=>p.id===item.productId)" :value="item.productId">{{item.productId}}（已下架，请更换）</option></select></label></div><button type="button" class="text-button" :disabled="index===0" @click="home.launchCalendar && move(home.launchCalendar,index,-1)">上移</button><button type="button" class="text-button" @click="home.launchCalendar?.splice(index,1)">删除</button></fieldset><button type="button" class="text-button" :disabled="(home.launchCalendar?.length || 0)>=36" @click="home.launchCalendar?.push({variety:'',startDate:'',endDate:'',note:'',productId:''})">＋ 添加上市日历</button>
<p class="muted">公告在发布日期起展示；活动自动标记即将开始、进行中或已结束。上市日期为预计产期，最多跨 5 年；保存即发布，删除后小程序下次进入首页生效。</p>
  <fieldset v-if="home.customerService"><legend>合作与个人中心客服</legend>
   <p class="muted">客服资料随首页配置保存、预览及回滚。原生微信客服需在微信公众平台绑定接待人员；演示模式不会发起真实会话或拨号。</p>
   <div class="form-grid">
    <label>客服状态<select v-model="home.customerService.enabled"><option :value="false">关闭</option><option :value="true">启用</option></select></label>
    <label>微信在线客服<select v-model="home.customerService.wechat"><option :value="false">关闭</option><option :value="true">开启原生客服</option></select></label>
    <label>客服电话（可留空）<input v-model.trim="home.customerService.phone" type="tel" maxlength="20" placeholder="手机、座机或400号码"/></label>
    <label>服务时间<input v-model.trim="home.customerService.hours" maxlength="100" :required="home.customerService.enabled" placeholder="例如：工作日 09:00–18:00（北京时间）"/></label>
    <label>合作回复时效（可留空）<input v-model.trim="home.customerService.responseTime" maxlength="100"/></label>
   </div>
  </fieldset>
  <fieldset v-if="home.live" class="live-config"><legend>果园现状 · 双机位直播</legend>
   <p class="muted">首页“查看果园现状”进入双机位页面。关闭或未接入的机位显示“直播暂未开启”；填写 HTTPS HLS（.m3u8）播放地址后再启用。这里只保存播放地址，摄像头推流由直播服务提供。</p>
   <fieldset v-for="view in [{id:'orchard' as const,title:'果园内部'},{id:'stall' as const,title:'园外摊位'}]" :key="view.id"><legend>{{view.title}}</legend><div class="form-grid">
    <label>机位状态<select v-model="home.live[view.id].enabled"><option :value="false">关闭</option><option :value="true">开启</option></select></label>
    <label>HLS 播放地址<input v-model.trim="home.live[view.id].url" type="url" maxlength="2000" :required="home.live[view.id].enabled" placeholder="https://直播域名/机位.m3u8"/></label>
    <label>直播时间说明<input v-model.trim="home.live[view.id].hours" maxlength="100" placeholder="例如：每日 08:00–18:00，雨天暂停"/></label>
    <label>下次开播预告时间<input v-model="home.live[view.id].noticeAt" type="datetime-local" :disabled="false"/></label>
    <label>预告文案<input v-model.trim="home.live[view.id].noticeText" maxlength="100" placeholder="例如：明早 8 点采摘直播，欢迎预约"/></label>
   </div></fieldset>
   <p class="muted">可在下方首页预览中查看机位配置状态。真实观看需配置播放域名并使用微信真机验收；带有效期的签名地址过期后需更新。</p>
  </fieldset>
  <LiveReplayManager />
  <label>果园介绍标题<input v-model.trim="home.storyTitle"/></label><label>果园介绍正文<textarea v-model.trim="home.storyCopy" rows="3"/></label><label>合作入口标题<input v-model.trim="home.cooperationTitle"/></label><label>合作入口说明<input v-model.trim="home.cooperationCopy"/></label>
  <button class="button primary" :disabled="working||!!pendingReadback">{{ busy === 'home' ? '保存并回读中…' : pendingReadback ? '请先回读上次保存值' : '保存并预览首页' }}</button>
  </fieldset>
 </form>
 <ActivityManagement v-if="home" :home="home" :products="products" :disabled="!!props.disabled||working||loading" :pending="!!pendingReadback" :dirty="homeDirty" @save="save('home')" @preview="openPreview('home','editing')" />
 <form v-if="privacy" class="panel settings-card operations-config" @submit.prevent="save('privacy')"><div class="panel-head"><div><h3>隐私政策 <span class="change-state">{{ pendingReadback === 'privacy' ? '保存值待回读确认' : privacyDirty ? '有未保存修改' : '与已读取保存值一致' }}</span></h3><p>填写真实运营主体后发布；未发布的草稿不会向用户公开</p></div><div class="preview-actions"><button type="button" class="button small secondary" :disabled="working" @click="openPreview('privacy','editing')">预览当前编辑</button><button type="button" class="text-button" :disabled="working||!savedPrivacy" @click="openPreview('privacy','saved')">查看已保存预览</button></div></div>
  <fieldset class="settings-fields" :disabled="props.disabled||working">
  <label>政策标题<input v-model.trim="privacy.title" required/></label><label>版本<input v-model.trim="privacy.version" required/></label><label>生效日期<input v-model="privacy.effectiveDate" type="date" required/></label>
  <label>营业执照主体全称<input v-model.trim="privacy.operator" :required="privacy.published"/></label><label>隐私事务邮箱或电话<input v-model.trim="privacy.contact" :required="privacy.published"/></label><label>联系地址<input v-model.trim="privacy.address" :required="privacy.published"/></label>
  <label v-for="(_,index) in privacy.content" :key="index">正文 {{index+1}}<textarea v-model="privacy.content[index]" rows="7" required/></label>
  <label>发布状态<select v-model="privacy.published"><option :value="false">草稿</option><option :value="true">正式发布</option></select></label><button class="button primary" :disabled="working||!!pendingReadback">{{ busy === 'privacy' ? '保存并回读中…' : pendingReadback ? '请先回读上次保存值' : '保存并预览隐私政策' }}</button>
  </fieldset>
 </form>
 <div v-if="preview" ref="previewElement" class="preview-anchor" tabindex="-1"><OperationsPreview :key="preview.generation" :target="preview.key" :mode="preview.mode" :home="preview.home" :privacy="preview.privacy" :products="preview.products" :articles="preview.articles" :demo="isDemo" @close="preview=null"/></div>
</template>

<style scoped>
.live-config>.muted{font-size:12px;line-height:1.8;color:var(--muted);margin:12px 0}.live-config>fieldset{margin-top:18px}
.config-feedback{margin-top:22px;padding:12px 15px;border:1px solid var(--border);border-radius:10px;color:var(--muted);font-size:12px;line-height:1.7}.demo-feedback{color:#765b25;background:#f7edcf}.recovery-button{margin-top:12px}.operations-config>.panel-head{gap:20px;align-items:flex-start}.preview-actions{display:flex;gap:10px;align-items:center;flex-shrink:0}.preview-actions button:disabled{cursor:default;opacity:.5}.change-state{margin-left:8px;color:var(--muted);font-size:10px;font-weight:400}.operations-config>.settings-fields{margin:0;padding:0 24px;border:0;border-radius:0;min-width:0}.settings-fields>label{display:block;margin-top:20px;color:#504942;font-size:11px;font-weight:700}.settings-fields>.button{margin-top:18px}.settings-fields h4{margin:22px 0 12px}.settings-fields>label>select,.settings-fields .selection-row select{padding:10px 12px;border:1px solid var(--border);border-radius:9px;background:#fbf9f5}.settings-fields>.muted{margin-top:16px;color:var(--muted);font-size:11px;line-height:1.8}.preview-anchor{scroll-margin-top:118px;outline:none}
@media(max-width:1300px){.operations-config>.panel-head{flex-wrap:wrap}.preview-actions{flex-wrap:wrap}}
</style>
