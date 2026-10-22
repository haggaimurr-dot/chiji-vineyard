<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, isDemo } from '../api'
import type { WaybillSettings, WaybillAccount } from '../types'
const settings = ref<WaybillSettings|null>(null), busy = ref(false), error = ref(''), message = ref('')
const fields: Array<{key: 'partnerId'|'partnerKey'|'partnerSecret'|'net'|'tempId'|'expType'|'partnerName'|'code'|'checkMan'; label: string; secret?: boolean}> = [
  { key: 'partnerId', label: '网点/月结账号', secret: true }, { key: 'partnerKey', label: '密码/顾客编码', secret: true },
  { key: 'partnerSecret', label: '校验码（顺丰必填）', secret: true }, { key: 'net', label: '网点编码（中通/申通必填）' },
  { key: 'tempId', label: 'V2 图片模板 ID' }, { key: 'expType', label: '快递产品名称' },
  { key: 'partnerName', label: '网点名称' }, { key: 'code', label: '账号类型编码（可选）' }, { key: 'checkMan', label: '审核人（可选）' },
]
async function load() {
  busy.value = true; error.value = ''; message.value = ''
  try { settings.value = await api<WaybillSettings>('admin.waybill.settings') }
  catch (e) { error.value = (e as Error).message }
  finally { busy.value = false }
}
function toggle(code: string) {
  if (!settings.value) return
  const accounts = settings.value.accounts
  if (accounts[code]) accounts[code].disabled = !accounts[code].disabled
  else accounts[code] = { disabled: false, partnerId: '', partnerKey: '', partnerSecret: '', net: '', tempId: '', expType: '', partnerName: '', code: '', checkMan: '', payType: 'MONTHLY' }
}
function stored(account: WaybillAccount, key: string) { return !!(account as unknown as Record<string, unknown>)[`${key}Set`] }
async function save() {
  if (!settings.value || busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try { settings.value = await api<WaybillSettings>('admin.waybill.settings.save', settings.value); message.value = '电子面单配置已保存，订单面单重新打开后使用最新配置' }
  catch (e) { error.value = (e as Error).message }
  finally { busy.value = false }
}
function addRate() {
  const code = Object.keys(settings.value?.accounts || {}).find(code => !settings.value?.accounts[code].disabled)
  if (!settings.value || !code) { error.value = '请先开启承运商'; return }
  settings.value.rates.push({carrierCode: code, province: '', firstWeight: 1, firstFee: 0, additionalWeight: 1, additionalFee: 0, surcharge: 0, delivery: ''})
}
onMounted(load)
</script>

<template>
  <section class="panel">
    <div class="panel-head"><div><h3>电子面单 · 账号与协议价</h3><p>配置寄件资料和多承运商网点/月结账号，在订单中比较预估运费并打印面单。</p></div><button class="button secondary" :disabled="busy" @click="load">重新读取</button></div>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p><p v-if="message" class="alert" role="status">{{ message }}</p>
    <p v-if="isDemo" class="alert">演示配置仅用于预览，示例面单不可交寄。</p>
    <form v-if="settings" class="waybill-settings" @submit.prevent="save">
      <fieldset :disabled="busy">
        <p v-if="!settings.enterpriseConfigured" class="alert">请在服务端配置 KUAIDI100_KEY 和 KUAIDI100_SECRET，完成后才能生成真实运单。</p>
        <div class="shipment-fields"><label>寄件人<input v-model="settings.senderName" maxlength="30" required /></label><label>寄件手机号<input v-model="settings.senderPhone" maxlength="20" required /></label></div>
        <label>完整寄件地址<input v-model="settings.senderAddress" maxlength="240" placeholder="省、市、区县、街道及门牌号" required /></label>
        <p class="subtle">已保存的账号、密码和校验码不会回显，留空表示保留。关闭承运商后不能新建面单，已有面单仍可打印。</p>
        <div v-for="carrier in settings.carriers" :key="carrier.code" class="waybill-account">
          <label class="waybill-enable"><input type="checkbox" :checked="!!settings.accounts[carrier.code] && !settings.accounts[carrier.code].disabled" @change="toggle(carrier.code)" />{{ carrier.name }} · {{ carrier.code }}</label>
          <div v-if="settings.accounts[carrier.code] && !settings.accounts[carrier.code].disabled" class="shipment-fields">
            <label v-for="field in fields" :key="field.key">{{ field.label }}<input v-model="settings.accounts[carrier.code][field.key]" :type="field.secret ? 'password' : 'text'" autocomplete="off" maxlength="500" :placeholder="stored(settings.accounts[carrier.code], field.key) ? '已保存，留空保留' : '未填写'" /></label>
            <label>付款方式<select v-model="settings.accounts[carrier.code].payType"><option value="MONTHLY">月结</option><option value="SHIPPER">寄方付</option></select></label>
          </div>
        </div>
        <div class="panel-head"><div><h3>承运商协议价</h3><p>金额单位：分（100分＝1元）。重量单位：公斤。按首重＋向上取整的续重＋附加费比较，属于预估费用。</p></div><button type="button" class="button secondary" @click="addRate">添加协议价</button></div>
        <div v-for="(rate, index) in settings.rates" :key="index" class="waybill-account shipment-fields">
          <label>承运商<select v-model="rate.carrierCode" required><option v-for="carrier in settings.carriers.filter(c => settings?.accounts[c.code])" :key="carrier.code" :value="carrier.code">{{ carrier.name }}</option></select></label>
          <label>目的省份<input v-model="rate.province" maxlength="30" placeholder="留空覆盖全国；指定省份优先，如浙江省" /></label>
          <label>首重（公斤）<input v-model.number="rate.firstWeight" type="number" min="0.01" max="1000" step="0.001" required /></label><label>首重价（分）<input v-model.number="rate.firstFee" type="number" min="0" max="10000000" step="1" required /></label>
          <label>续重单位（公斤）<input v-model.number="rate.additionalWeight" type="number" min="0.01" max="1000" step="0.001" required /></label><label>每单位续重价（分）<input v-model.number="rate.additionalFee" type="number" min="0" max="10000000" step="1" required /></label>
          <label>附加费（分）<input v-model.number="rate.surcharge" type="number" min="0" max="10000000" step="1" required /></label><label>预计时效<input v-model="rate.delivery" maxlength="80" placeholder="例如：2至3天，以实际配送为准" /></label>
          <button type="button" class="button secondary" @click="settings.rates.splice(index, 1)">移除此协议价</button>
        </div>
        <p v-if="!settings.rates.length" class="subtle">尚无协议价，订单比较运费时会显示“未配置”，请按签约网点提供的实际价目表录入。</p>
        <button class="button primary">{{ busy ? '保存中…' : '保存电子面单配置' }}</button>
      </fieldset>
    </form>
    <p v-else-if="busy" role="status">正在读取电子面单配置…</p>
  </section>
</template>

<style scoped>
.waybill-settings fieldset { border: 0; padding: 0; min-width: 0; }
.waybill-settings label { display: block; margin-bottom: 14px; font-size: 13px; }
.waybill-settings input:not([type=checkbox]), .waybill-settings select { width: 100%; display: block; margin-top: 7px; padding: 10px; border: 1px solid var(--border); border-radius: 8px; }
.waybill-account { border: 1px solid var(--border); border-radius: 12px; padding: 16px; margin: 16px 0; }
.waybill-enable { display: flex !important; gap: 10px; align-items: center; font-weight: 700; }
</style>
