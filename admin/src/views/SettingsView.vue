<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import ShippingRegions from './ShippingRegions.vue'
import { validateShippingDetails, type ShippingRule } from '../../../miniprogram/utils/shipping'
import { api, isDemo } from '../api'
import { currentAdmin } from '../permissions'
import RuntimeOperations from './RuntimeOperations.vue'
import AccountSettings from './AccountSettings.vue'
import CouponSettings from './CouponSettings.vue'
import HomePolicySettings from './HomePolicySettings.vue'
import ConfigVersions from './ConfigVersions.vue'
import WaybillSettings from './WaybillSettings.vue'

const shipping = ref<ShippingRule>({ freight: 0, freeShippingThreshold: 0, blockedProvinces: [], regions: [], blockedDates: [] })
const saved = ref(false)
const error = ref('')
const ready = ref(false)
const busy = ref(false)
const couponBusy = ref(false), homeBusy = ref(false), rollbackBusy = ref(false), reloading = ref(false)
const editorEpoch = ref(0), historyRevision = ref(0)
const locked = computed(() => busy.value || couponBusy.value || homeBusy.value || rollbackBusy.value || reloading.value)
async function loadShipping() {
  reloading.value = true
  ready.value = false
  saved.value = false
  error.value = ''
  try {
    const data = await api<{ shipping: typeof shipping.value }>('admin.config.get')
    shipping.value = { ...data.shipping, regions: data.shipping.regions || [], blockedDates: data.shipping.blockedDates || [] }
    ready.value = true
  } catch (e) { error.value = (e as Error).message }
  finally { reloading.value = false }
}
onMounted(loadShipping)
async function refreshAfterRollback() {
  // New editor instances discard drafts and any pending saved previews/readbacks.
  editorEpoch.value++
  couponBusy.value = false
  homeBusy.value = false
  await loadShipping()
}
async function save() {
  if (!ready.value || locked.value) return
  saved.value = false
  error.value = ''
  busy.value = true
  try { validateShippingDetails(shipping.value); await api('admin.config.save', { shipping: shipping.value }); saved.value = true; historyRevision.value++ }
  catch (e) { error.value = (e as Error).message }
  finally { busy.value = false }
}
</script>

<template>
  <div class="toolbar"><div><h2>运营配置</h2><p>维护分地区运费模板、活动、首页内容与账号</p></div></div>
  <nav class="config-shortcuts" aria-label="运营配置快捷入口"><a href="#shipping-templates">分地区运费模板</a><a href="#activity-management">活动管理</a></nav>
  <ConfigVersions :locked="locked" :revision="historyRevision" @busy="rollbackBusy = $event" @restored="refreshAfterRollback" />
  <div class="settings-grid">
    <form id="shipping-templates" class="panel settings-card" @submit.prevent="save">
      <div class="panel-head"><div><h3>分地区运费模板</h3><p>未匹配地区使用默认运费，金额均以人民币元展示</p></div></div>
      <label>基础运费（元）<input :value="(shipping.freight / 100).toFixed(2)" type="number" min="0" step="0.01" required :disabled="!ready || locked" @input="shipping.freight = Math.round(Number(($event.target as HTMLInputElement).value) * 100); saved = false" /></label>
      <label>满额包邮（元）<input :value="(shipping.freeShippingThreshold / 100).toFixed(2)" type="number" min="0" step="0.01" required :disabled="!ready || locked" @input="shipping.freeShippingThreshold = Math.round(Number(($event.target as HTMLInputElement).value) * 100); saved = false" /></label>
      <label>禁售省份（逗号分隔）<textarea :value="shipping.blockedProvinces.join(',')" :disabled="!ready || locked" @input="shipping.blockedProvinces = ($event.target as HTMLTextAreaElement).value.split(',').map(x => x.trim()).filter(Boolean); saved = false" rows="3" /></label>
      <ShippingRegions :rule="shipping" :disabled="!ready || locked" @changed="saved = false" />
      <p v-if="reloading" class="success-text" role="status">正在读取最新运费配置…</p>
      <p v-if="saved" class="success-text" role="status">配置已保存</p>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <button v-if="!ready && error" type="button" class="button secondary" :disabled="locked" @click="loadShipping">重新加载运费</button>
      <button class="button primary" :disabled="!ready || locked">{{ busy ? '保存中…' : '保存配置' }}</button>
    </form>
    <section class="panel checklist">
      <div class="panel-head"><div><h3>生产上线检查</h3><p>正式支付前必须全部完成</p></div></div>
      <ul>
        <li><span>1</span><div><strong>关闭小程序演示模式</strong><small>demoMode=false，并填写云环境与云托管服务名</small></div></li>
        <li><span>2</span><div><strong>配置商户证书与 API v3 密钥</strong><small>所有密钥仅保存在 Go 云托管服务环境中</small></div></li>
        <li><span>3</span><div><strong>配置支付/退款通知地址</strong><small>指向云托管域名的 /wechat-pay-notify</small></div></li>
        <li><span>4</span><div><strong>验证订单发货管理同步</strong><small>真实低额订单完成支付、发货与退款测试</small></div></li>
        <li><span>5</span><div><strong>替换图片与法律文本</strong><small>使用真实果园素材、隐私政策和售后规则</small></div></li>
      </ul>
    </section>
    <AccountSettings />
  </div>
  <WaybillSettings />
  <RuntimeOperations v-if="isDemo && currentAdmin().role === 'SUPER_ADMIN'" />
  <CouponSettings :key="`coupons-${editorEpoch}`" :disabled="locked" @busy="couponBusy = $event" @saved="historyRevision++" />
  <HomePolicySettings :key="`home-privacy-${editorEpoch}`" :disabled="locked" @busy="homeBusy = $event" @saved="historyRevision++" />
</template>

<style scoped>
.config-shortcuts{display:flex;gap:12px;flex-wrap:wrap;margin-bottom:20px}.config-shortcuts a{padding:10px 16px;border:1px solid var(--border);border-radius:9px;color:var(--purple);text-decoration:none;background:white}#shipping-templates{scroll-margin-top:120px}
</style>
