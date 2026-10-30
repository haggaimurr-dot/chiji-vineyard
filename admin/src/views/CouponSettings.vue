<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
type Coupon = {code:string;name:string;minimum:number;amount:number;startDate:string;endDate:string;active:boolean}
const props=defineProps<{disabled?:boolean}>()
const emit=defineEmits<{saved:[];busy:[value:boolean]}>()
const items=ref<Coupon[]>([]), busy=ref(false), loading=ref(false), ready=ref(false), error=ref(''), saved=ref(false)
async function load(){if(loading.value)return;loading.value=true;error.value='';try{const c=await api<{items?:Coupon[]}>('admin.coupon.get');items.value=c.items||[];ready.value=true}catch(e){error.value=(e as Error).message}finally{loading.value=false}}
onMounted(load)
function add(){if(!ready.value||busy.value||props.disabled||items.value.length>=100)return;items.value.push({code:'',name:'',minimum:9900,amount:1000,startDate:'',endDate:'',active:false});saved.value=false}
async function save(){if(!ready.value||busy.value||props.disabled)return;busy.value=true;emit('busy',true);error.value='';saved.value=false;try{const c=await api<{items:Coupon[]}>('admin.coupon.save',{items:items.value});items.value=c.items;saved.value=true;emit('saved')}catch(e){error.value=(e as Error).message}finally{busy.value=false;emit('busy',false)}}
type CouponStat={code:string;name:string;minimum:number;amount:number;issued:number;used:number;expired:number;active:number;redeemRate:number}
const stats=ref<CouponStat[]>([]),statTotal=ref<CouponStat|null>(null),statBusy=ref(false),statError=ref(''),statLoaded=ref(false)
async function loadStats(){if(statBusy.value)return;statBusy.value=true;statError.value='';try{const r=await api<{items:CouponStat[];total:CouponStat}>('admin.coupon.stats');stats.value=r.items;statTotal.value=r.total;statLoaded.value=true}catch(e){statError.value=(e as Error).message}finally{statBusy.value=false}}
function yuan(n:number){return (n/100).toFixed(2)}
</script>
<template><section class="panel coupon-settings"><div class="panel-head"><div><h3>优惠券</h3><p>按兑换码使用，每位用户限一次；商品金额达到门槛，运费不抵扣。取消或关闭订单后释放，退款不返券。</p></div><button class="button secondary" :disabled="!ready || busy || props.disabled || items.length>=100" @click="add">添加优惠券</button></div><form @submit.prevent="save"><div class="coupon-editor" v-for="(c,index) in items" :key="index"><label>兑换码<input v-model="c.code" required maxlength="40" :disabled="busy || props.disabled" placeholder="字母、数字、下划线或横线"/></label><label>名称<input v-model="c.name" required maxlength="60" :disabled="busy || props.disabled"/></label><label>使用门槛（元）<input type="number" min="0" step="0.01" :value="c.minimum/100" @input="c.minimum=Math.round(Number(($event.target as HTMLInputElement).value)*100)" :disabled="busy || props.disabled" required/></label><label>抵扣金额（元）<input type="number" min="0.01" step="0.01" :value="c.amount/100" @input="c.amount=Math.round(Number(($event.target as HTMLInputElement).value)*100)" :disabled="busy || props.disabled" required/></label><label>开始日期<input type="date" v-model="c.startDate" required :disabled="busy || props.disabled"/></label><label>截止日期<input type="date" v-model="c.endDate" :min="c.startDate" required :disabled="busy || props.disabled"/></label><label><input type="checkbox" v-model="c.active" :disabled="busy || props.disabled"/> 启用</label><button class="button secondary" type="button" :disabled="busy || props.disabled" @click="items.splice(index,1)">删除</button></div><p v-if="ready && !items.length" class="subtle">尚未配置优惠券</p><p v-if="loading" class="subtle" role="status">正在加载优惠券配置…</p><p v-if="error" class="form-error" role="alert">{{error}}</p><button v-if="!ready && error" type="button" class="button secondary" :disabled="loading || props.disabled" @click="load">重新加载优惠券</button><p v-if="saved" class="success-text">优惠券已保存</p><button class="button primary" :disabled="!ready || busy || props.disabled">{{busy?'保存中…':'保存优惠券'}}</button></form>
<section><div class="panel-head"><div><h3>发券效果统计</h3><p>按模板聚合领取 / 核销 / 过期数，核销率 = 核销 ÷ 领取。</p></div><button class="button secondary" :disabled="statBusy" @click="loadStats">刷新统计</button></div>
<p v-if="statError" class="form-error" role="alert">{{statError}}</p>
<p v-if="statBusy" class="subtle" role="status">正在统计…</p>
<p v-else-if="!statLoaded" class="subtle">点击刷新统计查看各模板领取与核销情况</p>
<div v-else><table class="stat-table"><thead><tr><th>模板</th><th>面额</th><th>领取</th><th>核销</th><th>过期</th><th>有效</th><th>核销率</th></tr></thead><tbody>
<tr v-for="s in stats" :key="s.code"><td>{{s.name}}<br><small>{{s.code}}</small></td><td>{{yuan(s.amount)}} 元 / 满 {{yuan(s.minimum)}} 元</td><td>{{s.issued}}</td><td>{{s.used}}</td><td>{{s.expired}}</td><td>{{s.active}}</td><td>{{s.redeemRate}}%</td></tr>
<tr v-if="statTotal"><td><strong>合计</strong></td><td>—</td><td><strong>{{statTotal.issued}}</strong></td><td><strong>{{statTotal.used}}</strong></td><td><strong>{{statTotal.expired}}</strong></td><td><strong>{{statTotal.active}}</strong></td><td><strong>{{statTotal.redeemRate}}%</strong></td></tr>
</tbody></table><p v-if="!stats.length" class="subtle">暂无优惠券模板</p></div></section>
</section></template>
<style scoped>
.stat-table{width:100%;border-collapse:collapse;margin-top:8px}
.stat-table th,.stat-table td{border:1px solid var(--border);padding:6px 10px;text-align:left}
.stat-table small{color:var(--muted)}
</style>
<style scoped>.coupon-settings{margin-top:24px;padding:24px}.coupon-editor{display:grid;grid-template-columns:repeat(4,minmax(120px,1fr));gap:16px;padding:20px 0;border-bottom:1px solid #eee;margin-bottom:20px}.coupon-editor label{display:flex;flex-direction:column;gap:8px}.coupon-editor input{padding:10px;width:100%}.coupon-editor input[type=checkbox]{width:auto}@media(max-width:800px){.coupon-editor{grid-template-columns:1fr 1fr}}</style>

