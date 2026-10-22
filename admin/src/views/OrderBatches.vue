<script setup lang="ts">
import { ref, computed } from 'vue'
import { api } from '../api'
import { canShip } from '../orders'
import type { Order } from '../types'
import type { ManagedBatch } from '../product-management'
const props=defineProps<{order:Order;disabled?:boolean}>(),emit=defineEmits<{changed:[]}>()
const open=ref(false),busy=ref(false),error=ref(''),choices=ref<ManagedBatch[]>([])
const rows=ref<Array<{skuId:string;batchId:string;quantity:number}>>([])
const editable=computed(()=>canShip(props.order))
async function show(){open.value=true;error.value='';rows.value=(props.order.batches||[]).map(a=>({skuId:a.skuId,batchId:a.batchId,quantity:a.quantity}));busy.value=true;choices.value=[];try{
 for(const productId of new Set(props.order.items.map(i=>i.productId))){
  if(!productId)continue
  let offset=0,total=1
  while(offset<total){const page=await api<{items:ManagedBatch[];total:number}>('admin.order.batches.options',{productId,status:'ACTIVE',offset,limit:50});choices.value.push(...page.items);total=page.total;offset+=50}
 }
 if(!rows.value.length)rows.value=props.order.items.map(i=>({skuId:i.skuId||'',batchId:'',quantity:i.quantity}))
}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
async function save(){busy.value=true;error.value='';try{await api('admin.order.batches.save',{orderId:props.order.id,expectedVersion:props.order.batchVersion||0,items:rows.value});open.value=false;emit('changed')}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
function options(skuId:string){const item=props.order.items.find(i=>i.skuId===skuId);return choices.value.filter(b=>b.productId===item?.productId)}
</script>
<template>
 <button class="button small secondary" :disabled="disabled" @click="show">订单批次{{order.batches?.length?'（已登记）':''}}</button>
 <div v-if="open" class="modal-backdrop"><section class="drawer" role="dialog" aria-modal="true" aria-label="订单批次"><div class="drawer-head"><h2>订单批次 · {{order.orderNo}}</h2><button class="close" :disabled="busy" @click="open=false">×</button></div><div class="drawer-body">
 <p>按实际发出的规格登记批次，可拆分多个批次。每个规格的数量合计须等于订单数量；发货后记录保留。</p>
 <p v-if="error" class="form-error" role="alert">{{error}}</p><p v-if="busy">处理中…</p>
 <form v-if="editable" @submit.prevent="save"><fieldset :disabled="busy"><div v-for="(r,index) in rows" :key="index" class="allocation">
 <label>规格<select v-model="r.skuId" required @change="r.batchId=''"><option v-for="i in order.items" :key="i.skuId" :value="i.skuId">{{i.name}} · {{i.skuName}}（订单 {{i.quantity}}）</option></select></label>
 <label>实际批次<select v-model="r.batchId" required><option value="">请选择批次</option><option v-for="b in options(r.skuId)" :key="b.id" :value="b.id">{{b.number}} · {{b.orchard}}</option></select></label>
 <label>数量<input v-model.number="r.quantity" type="number" min="1" step="1" required/></label><button type="button" class="text-button" @click="rows.splice(index,1)">移除</button>
 </div><button type="button" class="text-button" :disabled="rows.length>=200" @click="rows.push({skuId:order.items[0]?.skuId||'',batchId:'',quantity:1})">＋ 拆分批次</button><p v-if="!choices.length">暂无可用批次，请在商品与批次模块登记。</p><button v-permission="'order.ship'" class="button primary" :disabled="!rows.length">保存订单批次</button></fieldset></form>
 <div v-for="(a,index) in order.batches||[]" :key="index" class="allocation"><strong>{{order.items.find(i=>i.skuId===a.skuId)?.skuName}} · {{a.batch.number}} × {{a.quantity}}</strong><p>果园：{{a.batch.orchard}} · 采收：{{a.batch.harvestedAt||'待采收'}} · 包装：{{a.batch.packedAt||'待包装'}}</p><p>溯源码：{{a.batch.code||a.batch.id}}</p></div>
 <p v-if="!editable&&!order.batches?.length">此订单未登记批次。</p>
 </div><div class="drawer-foot"><button class="button secondary" :disabled="busy" @click="open=false">关闭</button></div></section></div>
</template>
<style scoped>
fieldset{border:0;padding:0}.allocation{border-bottom:1px solid var(--border);padding:16px 0}label{display:block;margin:10px 0}select,input{display:block;width:100%;padding:10px;border:1px solid var(--border);border-radius:8px}p{line-height:1.7;overflow-wrap:anywhere}
</style>
