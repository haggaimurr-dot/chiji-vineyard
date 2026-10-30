<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import ProductTools from './ProductTools.vue'
import { api, money } from '../api'
import { confirmAction } from '../confirm'
import type { Product } from '../types'
import { productBatchError, giftCustomizationError } from '../product-content'
import InventoryPanel from './InventoryPanel.vue'
import InventoryLots from './InventoryLots.vue'
const tab = ref('products'), inventoryRevision = ref(0), alertCount = ref(0)

const products = ref<Product[]>([]), total=ref(0), offset=ref(0), busy=ref(false), batchProductId=ref('')
const query=ref({keyword:'',category:'all',status:'',saleMode:''}),pageSize=ref(20)
let request=0,searchTimer:ReturnType<typeof setTimeout>|undefined
function search(){request++;clearTimeout(searchTimer);searchTimer=setTimeout(()=>load(0),300)}
onUnmounted(()=>{clearTimeout(searchTimer);request++})
const batch = ref<Array<{skuId:string;name:string;expectedStock:number;stock:number;selected:boolean}>>([]), batchOpen=ref(false), batchBusy=ref(false), batchReason=ref(''), batchError=ref('')
function openBatch(){batch.value=products.value.flatMap(p=>p.skus.filter(s=>s.active!==false).map(s=>({skuId:s.id,name:p.name+' / '+s.name,expectedStock:s.stock,stock:s.stock,selected:false})));batchReason.value='';batchError.value='';batchOpen.value=true}
async function saveBatch(){const items=batch.value.filter(x=>x.selected);if(!items.length||items.length>200){batchError.value='一次请选择 1–200 个规格';return}batchBusy.value=true;batchError.value='';try{await api('admin.inventory.batch',{items:items.map(({skuId,stock,expectedStock})=>({skuId,stock,expectedStock})),reason:batchReason.value});batchOpen.value=false;await load()}catch(e){batchError.value=(e as Error).message}finally{batchBusy.value=false}}
const editing = ref<Product | null>(null)
const saving = ref(false)
const error = ref('')
const selected = ref<string[]>([])
const selBusy = ref(false)
const selMsg = ref('')
const pageAllSelected = computed(() => products.value.length > 0 && selected.value.length === products.value.length)
function toggleSelect(id: string) { const i = selected.value.indexOf(id); if (i < 0) selected.value.push(id); else selected.value.splice(i, 1) }
function toggleSelectAll() { selected.value = pageAllSelected.value ? [] : products.value.map(p => p.id) }
async function batchStatus(status: 'PUBLISHED' | 'DRAFT') {
  if (!selected.value.length || selBusy.value) return
  if (!(await confirmAction({ title: '批量上下架', message: `确认将 ${selected.value.length} 个商品${status === 'PUBLISHED' ? '上架' : '下架为草稿'}？`, danger: status !== 'PUBLISHED' }))) return
  selBusy.value = true; selMsg.value = ''
  try {
    const result = await api<{ updated: number }>('admin.product.batchStatus', { ids: selected.value, status })
    selMsg.value = `已${status === 'PUBLISHED' ? '上架' : '下架'} ${result.updated} 个商品`
    selected.value = []
    await load()
  } catch (e) { selMsg.value = (e as Error).message }
  finally { selBusy.value = false }
}
async function duplicateProduct(product: Product) {
  if (selBusy.value) return
  if (!(await confirmAction({ title: '复制商品', message: `复制「${product.name}」为草稿（规格库存置零）？` }))) return
  selBusy.value = true; selMsg.value = ''
  try {
    const created = await api<Product>('admin.product.duplicate', { id: product.id })
    selMsg.value = `已复制为草稿：${created.name}`
    selected.value = []
    await load()
  } catch (e) { selMsg.value = (e as Error).message }
  finally { selBusy.value = false }
}
const blank = (): Product => ({ id: '', name: '', subtitle: '', category: 'family', saleMode: 'spot', cover: '', gallery: [], badges: [], origin: '', description: '', highlights: [], status: 'DRAFT', sortOrder: 100, skus: [{ id: '', name: '家庭装', price: 0, stock: 0, limitPerOrder: 1, weightLabel: '' }] })

async function load(next=offset.value) { const id=++request;busy.value=true;error.value='';try { const result=await api<{items:Product[];total:number;offset:number}>('admin.product.search',{...query.value,offset:next,limit:pageSize.value});if(id!==request)return;products.value=result.items;total.value=result.total;offset.value=result.offset;inventoryRevision.value++;selected.value=selected.value.filter(id=>result.items.some(p=>p.id===id)) } catch(e) { if(id===request)error.value = (e as Error).message } finally{if(id===request)busy.value=false} }
onMounted(()=>load(0))
function edit(product?: Product) { editing.value = JSON.parse(JSON.stringify(product || blank())); editing.value?.skus.forEach(s=>s.expectedStock=s.stock); error.value = '' }
function addTraceability() {
  if (editing.value) editing.value.traceability = { orchard: '', batch: '', harvestedAt: '', packedAt: '', packing: '', steps: [], demo: true }
}
function addSku() { editing.value?.skus.push({ id: '', name: '', price: 0, stock: 0, limitPerOrder: 1, weightLabel: '' }) }
function priceInput(event: Event, index: number) { if (editing.value) editing.value.skus[index].price = Math.round(Number((event.target as HTMLInputElement).value) * 100) }
async function save() {
  if (!editing.value) return
  error.value = productBatchError(editing.value.traceability) || giftCustomizationError(editing.value.giftCustomization)
  if (error.value) return
  saving.value = true; error.value = ''
  try { await api('admin.product.save', { product: editing.value }); editing.value = null; await load() }
  catch (e) { error.value = (e as Error).message }
  finally { saving.value = false }
}
async function uploadCover(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]; if (!file || !editing.value) return
  if (file.size > 4 * 1024 * 1024) return alert('图片不能超过 4MB')
  const dataUrl = await new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result)); reader.onerror = reject; reader.readAsDataURL(file) })
  const result = await api<{ fileID: string }>('admin.media.upload', { dataUrl })
  editing.value.cover = result.fileID
}
async function inventory(product: Product, skuId: string, current: number) {
  const raw = window.prompt(`设置 ${product.name} 库存`, String(current)); if (raw === null) return
  const stock = Number(raw); if (!Number.isInteger(stock) || stock < 0) return alert('请输入非负整数')
  const reason=window.prompt('库存调整原因');if(!reason?.trim())return
  try{await api('admin.inventory.batch', {items:[{skuId,stock,expectedStock:current}],reason});await load()}catch(e){alert((e as Error).message)}
}
</script>

<template>
  <div class="toolbar"><div><h2>商品与库存</h2><p>管理现货、预售、礼盒规格和可售库存</p></div><div class="row-actions"><button v-permission="'inventory.write'" class="button secondary" :disabled="busy||!products.length" @click="openBatch">批量调整本页库存</button><button v-permission="['product.write', 'inventory.write']" class="button primary" @click="edit()">＋ 新建商品</button></div></div>
  <nav class="inventory-tabs" aria-label="商品与库存功能"><button class="button" :class="tab==='products'?'primary':'secondary'" @click="tab='products'">商品列表</button><button class="button" :class="tab==='movements'?'primary':'secondary'" @click="tab='movements'">库存流水</button><button class="button" :class="tab==='alerts'?'primary':'secondary'" @click="tab='alerts'">低库存告警（{{alertCount}}）</button><button v-permission="['product.write','inventory.write']" class="button" :class="tab==='import'?'primary':'secondary'" @click="tab='import'">批量导入</button><button class="button" :class="tab==='batches'?'primary':'secondary'" @click="batchProductId='';tab='batches'">批次管理</button><button class="button" :class="tab==='lots'?'primary':'secondary'" @click="tab='lots'">批次库存 / 多仓库</button></nav>
  <InventoryLots v-if="tab==='lots'" @saved="load" />
  <ProductTools :mode="tab" :product-id="batchProductId" :selected-product="products.find(p=>p.id===batchProductId)" @saved="load(0)" />
  <div v-show="tab==='movements'||tab==='alerts'"><InventoryPanel :mode="tab" :revision="inventoryRevision" @count="alertCount=$event" @adjusted="load" /></div>
  <p v-if="error&&!editing" class="form-error" role="alert">{{error}}</p>
  <form v-show="tab==='products'" class="product-filters panel" @submit.prevent="load(0)"><label>搜索商品<input v-model.trim="query.keyword" maxlength="60" placeholder="商品 / 产地 / 规格" @input="search"/></label><label>分类<select v-model="query.category" @change="load(0)"><option value="all">全部分类</option><option value="family">家庭装</option><option value="gift">节礼装</option></select></label><label>状态<select v-model="query.status" @change="load(0)"><option value="">全部状态</option><option value="PUBLISHED">销售中</option><option value="DRAFT">草稿</option></select></label><label>销售模式<select v-model="query.saleMode" @change="load(0)"><option value="">全部模式</option><option value="spot">现货</option><option value="presale">预售</option></select></label><button class="button primary" :disabled="busy">查询</button><button type="button" class="button secondary" @click="query={keyword:'',category:'all',status:'',saleMode:''};load(0)">重置</button></form>
  <div v-show="tab==='products'&&(selected.length||selMsg)" class="batch-bar panel"><span>已选 {{selected.length}} 个商品</span><div class="row-actions"><button v-permission="['product.write','inventory.write']" class="button secondary" :disabled="selBusy||!selected.length" @click="batchStatus('PUBLISHED')">批量上架</button><button v-permission="['product.write','inventory.write']" class="button secondary" :disabled="selBusy||!selected.length" @click="batchStatus('DRAFT')">批量下架</button><button class="text-button" :disabled="selBusy" @click="selected=[]">清空选择</button></div><span v-if="selMsg" class="batch-msg">{{selMsg}}</span></div>
  <p v-if="tab==='products'&&busy" aria-live="polite">加载中…</p>
  <section v-show="tab==='products'" class="panel table-panel"><table><thead><tr><th><input type="checkbox" :checked="pageAllSelected" @change="toggleSelectAll" aria-label="全选本页商品" /></th><th>商品</th><th>销售模式</th><th>规格 / 售价</th><th>可售库存</th><th>状态</th><th></th></tr></thead><tbody><tr v-for="product in products" :key="product.id"><td><input type="checkbox" :checked="selected.includes(product.id)" @change="toggleSelect(product.id)" :aria-label="'选择'+product.name" /></td><td><div class="product-cell"><img :src="product.cover" loading="lazy" :alt="product.name"/><div><strong>{{ product.name }}</strong><small>{{ product.subtitle }}</small></div></div></td><td><span class="tag" :class="product.saleMode">{{ product.saleMode === 'spot' ? '现货' : '预售' }}</span><small v-if="product.expectedShipDate" class="block">{{ product.expectedShipDate }} 起发</small></td><td><div class="sku-line" v-for="sku in product.skus" :key="sku.id"><span>{{ sku.name }}</span><b>{{ money(sku.price) }}</b></div></td><td><button v-permission="'inventory.write'" class="stock-link" v-for="sku in product.skus" :key="sku.id" @click="inventory(product, sku.id, sku.stock)">{{ sku.name }} <strong>{{ sku.stock }}</strong></button></td><td><span class="status" :class="product.status.toLowerCase()">{{ product.status === 'PUBLISHED' ? '销售中' : '草稿' }}</span></td><td><button v-permission="['product.write', 'inventory.write']" class="text-button" @click="edit(product)">编辑</button><button v-permission="['product.write', 'inventory.write']" class="text-button" @click="duplicateProduct(product)">复制</button><button class="text-button" @click="batchProductId=product.id;tab='batches'">批次</button></td></tr><tr v-if="!products.length&&!busy"><td colspan="7">暂无匹配商品</td></tr></tbody></table><div class="product-pagination"><span>共 {{total}} 款商品</span><select v-model.number="pageSize" @change="load(0)"><option :value="20">20 / 页</option><option :value="50">50 / 页</option></select><button class="button secondary" :disabled="busy||offset===0" @click="load(offset-pageSize)">上一页</button><span>第 {{Math.floor(offset/pageSize)+1}} 页</span><button class="button secondary" :disabled="busy||offset+pageSize>=total" @click="load(offset+pageSize)">下一页</button><button class="button secondary" :disabled="busy" @click="load()">刷新</button></div></section>

  <div v-if="batchOpen" class="modal-backdrop"><form class="drawer" @submit.prevent="saveBatch"><div class="drawer-head"><h2>批量调整可售库存</h2><button type="button" class="close" :disabled="batchBusy" @click="batchOpen=false">×</button></div><div class="drawer-body"><p>填写调整后的可售库存，预占库存保持独立。本次列出当前商品页的规格，最多选择 200 个。若提交前库存变化，整批不会保存，请关闭并刷新后重新操作。</p><button type="button" class="text-button" @click="batch.forEach(x=>x.selected=true)">全选</button><button type="button" class="text-button" @click="batch.forEach(x=>x.selected=false)">取消全选</button><table><thead><tr><th>选择</th><th>商品 / 规格</th><th>当前</th><th>调整后</th></tr></thead><tbody><tr v-for="row in batch" :key="row.skuId"><td><input type="checkbox" v-model="row.selected"/></td><td>{{row.name}}</td><td>{{row.expectedStock}}</td><td><input v-model.number="row.stock" type="number" min="0" max="2147483647" step="1" :disabled="!row.selected" required/></td></tr></tbody></table><label>调整原因<textarea v-model.trim="batchReason" maxlength="200" required rows="2"/></label><p v-if="batchError" class="form-error">{{batchError}}</p></div><div class="drawer-foot"><button type="button" class="button secondary" :disabled="batchBusy" @click="batchOpen=false">取消</button><button v-permission="'inventory.write'" class="button primary" :disabled="batchBusy">{{batchBusy?'保存中…':'保存所选库存'}}</button></div></form></div>
  <div v-if="editing" class="modal-backdrop" @click.self="editing = null"><form class="drawer" @submit.prevent="save"><div class="drawer-head"><div><p class="eyebrow purple">PRODUCT EDITOR</p><h2>{{ editing.id ? '编辑商品' : '新建商品' }}</h2></div><button type="button" class="close" @click="editing = null">×</button></div><div class="drawer-body form-grid">
    <label>商品名称<input v-model.trim="editing.name" required /></label><label>副标题<input v-model.trim="editing.subtitle" required /></label>
    <label>商品类型<select v-model="editing.category"><option value="family">家庭装</option><option value="gift">节礼装</option></select></label><label>销售模式<select v-model="editing.saleMode"><option value="spot">现货</option><option value="presale">预售</option></select></label>
    <label v-if="editing.saleMode === 'presale'">预计发货日期<input v-model="editing.expectedShipDate" type="date" /></label><label>产地<input v-model.trim="editing.origin" required /></label>
    <label>葡萄品种<input v-model.trim="editing.variety" placeholder="如阳光玫瑰；双拼请填写全部品种" maxlength="120" /></label>
    <label>图片说明<input v-model.trim="editing.imageNote" placeholder="如商品示意图 / 果园实拍" maxlength="120" /></label>
    <label class="full">封面图片 URL / cloud fileID<input v-model.trim="editing.cover" required placeholder="上传至云存储后粘贴地址" /><input class="file-input" type="file" accept="image/jpeg,image/png,image/webp" @change="uploadCover" /></label>
    <label class="full">商品描述<textarea v-model.trim="editing.description" rows="3" /></label>
    <label class="full">详情图片（每行一个地址）<textarea :value="editing.gallery.join('\n')" @input="editing.gallery = ($event.target as HTMLTextAreaElement).value.split('\n').map(x=>x.trim()).filter(Boolean)" rows="3" placeholder="果串细节、规格、包装与开箱图片地址" /></label>
    <label class="full">净重口径<textarea v-model.trim="editing.netWeightNote" rows="2" maxlength="500" placeholder="说明果梗、外箱、内托是否计入重量；每种规格重量在下方填写" /></label>
    <label class="full">保存说明<textarea v-model.trim="editing.storage" rows="3" maxlength="1000" placeholder="根据实际商品填写保存方式、食用建议" /></label>
    <div class="full sku-editor">
      <div class="sku-head"><strong>果园与批次资料</strong><button v-if="!editing.traceability" type="button" class="text-button" @click="addTraceability">＋ 添加批次资料</button></div>
      <p>示例数据保留「示例资料」标记；有可核实的真实记录后再切换。这里展示商品当前批次，订单实发批次以包装标识为准。</p>
      <div v-if="editing.traceability" class="form-grid">
        <label>资料类型<select v-model="editing.traceability.demo"><option :value="true">示例资料（mock）</option><option :value="false">真实资料</option></select></label>
        <label>果园 / 地块<input v-model.trim="editing.traceability.orchard" maxlength="200" /></label>
        <label>批次号<input v-model.trim="editing.traceability.batch" maxlength="120" /></label>
        <label>批次溯源码<input v-model.trim="editing.traceability.code" maxlength="120" placeholder="填写与当前批次对应的溯源码" /><small>买家可复制此码，联系客服核实批次资料</small></label>
        <label>采收日期<input v-model="editing.traceability.harvestedAt" type="date" /><small>预售未采收时留空</small></label>
        <label>包装日期<input v-model="editing.traceability.packedAt" type="date" :min="editing.traceability.harvestedAt || undefined" /></label>
        <label class="full">包装说明<textarea v-model.trim="editing.traceability.packing" maxlength="1000" rows="3" /></label>
        <div class="full"><div class="sku-head"><strong>溯源流程</strong><button type="button" class="text-button" @click="editing.traceability.steps.push({title:'',detail:''})">＋ 添加流程</button></div>
          <div v-for="(step,index) in editing.traceability.steps" :key="index" class="form-grid">
            <label>流程标题<input v-model.trim="step.title" maxlength="80" required /></label>
            <label>流程说明<textarea v-model.trim="step.detail" maxlength="1000" rows="2" required /></label>
            <button type="button" class="text-button" @click="editing.traceability.steps.splice(index,1)">删除流程</button>
          </div>
        </div>
        <button type="button" class="text-button" @click="delete editing.traceability">移除批次资料</button>
      </div>
    </div>
    <div class="full sku-editor">
      <div class="sku-head"><strong>礼盒定制</strong><button v-if="!editing.giftCustomization" type="button" class="text-button" @click="editing.giftCustomization = {enabled:true, description:'', options:[], minimumQuantity:1, leadTime:''}">＋ 添加礼盒定制</button></div>
      <div v-if="editing.giftCustomization" class="form-grid">
        <label>服务状态<select v-model="editing.giftCustomization.enabled"><option :value="true">开启</option><option :value="false">关闭</option></select></label>
        <label>起订量（盒）<input v-model.number="editing.giftCustomization.minimumQuantity" type="number" min="1" max="1000000" step="1" :required="editing.giftCustomization.enabled" /></label>
        <label class="full">定制说明<textarea v-model.trim="editing.giftCustomization.description" maxlength="1000" rows="3" :required="editing.giftCustomization.enabled" placeholder="填写支持的定制服务、费用及确认方式" /></label>
        <label class="full">定制项目（每行一项）<textarea :value="editing.giftCustomization.options.join('\n')" @input="editing.giftCustomization.options = ($event.target as HTMLTextAreaElement).value.split('\n').map(x=>x.trim()).filter(Boolean)" rows="3" placeholder="祝福卡片、企业标识、包装配色等实际支持的项目" /></label>
        <label class="full">定制周期<input v-model.trim="editing.giftCustomization.leadTime" maxlength="200" :required="editing.giftCustomization.enabled" placeholder="如方案确认后 7–10 天，具体以沟通为准" /></label>
        <p class="full">买家从商品详情提交需求，报价与交期需沟通确认。</p>
        <button type="button" class="text-button" @click="delete editing.giftCustomization">移除礼盒定制</button>
      </div>
    </div>
    <label class="full">标签（逗号分隔）<input :value="editing.badges.join(',')" @change="editing.badges = ($event.target as HTMLInputElement).value.split(',').map(x=>x.trim()).filter(Boolean)" /></label>
    <label class="full">卖点（逗号分隔）<input :value="editing.highlights.join(',')" @change="editing.highlights = ($event.target as HTMLInputElement).value.split(',').map(x=>x.trim()).filter(Boolean)" /></label>
    <div class="full sku-editor"><div class="sku-head"><strong>商品规格</strong><button type="button" class="text-button" @click="addSku">＋ 添加规格</button></div><div class="sku-form" v-for="(sku,index) in editing.skus" :key="index"><input v-model.trim="sku.name" placeholder="规格名" required /><input v-model.trim="sku.weightLabel" placeholder="重量说明" required /><input :value="(sku.price/100).toFixed(2)" type="number" step="0.01" min="0.01" placeholder="售价（元）" @input="priceInput($event,index)" /><input v-model.number="sku.stock" type="number" min="0" placeholder="库存" /><input v-model.number="sku.limitPerOrder" type="number" min="1" placeholder="限购" /><button type="button" @click="editing.skus.splice(index,1)">删除</button></div></div>
    <label>发布状态<select v-model="editing.status"><option value="DRAFT">保存草稿</option><option value="PUBLISHED">立即上架</option></select></label><label>排序值<input v-model.number="editing.sortOrder" type="number" /></label>
    <p v-if="error" class="form-error full">{{ error }}</p>
  </div><div class="drawer-foot"><button type="button" class="button secondary" @click="editing=null">取消</button><button v-permission="['product.write', 'inventory.write']" class="button primary" :disabled="saving">{{ saving ? '保存中…' : '保存商品' }}</button></div></form></div>
</template>

<style scoped>
.table-panel{overflow-x:auto}.table-panel table{min-width:800px}.stock-link{white-space:nowrap}
.product-filters{display:flex;flex-wrap:wrap;gap:12px;padding:20px;margin-bottom:16px;align-items:end}.product-filters label{display:grid;gap:8px;font-size:13px}.product-filters input,.product-filters select,.product-pagination select{padding:10px;border:1px solid var(--border);border-radius:8px}.product-pagination{display:flex;flex-wrap:wrap;justify-content:flex-end;align-items:center;gap:12px;padding:20px;font-size:13px}
.inventory-tabs{display:flex;flex-wrap:wrap;gap:10px;margin-bottom:20px}
.batch-bar{display:flex;align-items:center;gap:16px;padding:12px 20px;margin-bottom:16px;flex-wrap:wrap;font-size:13px}
.batch-msg{color:var(--purple);font-size:12px}
</style>
