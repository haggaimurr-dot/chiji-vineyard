<script setup lang="ts">
import { computed, ref } from 'vue'
import { shippingFee, type ShippingRule } from '../../../miniprogram/utils/shipping'
const props = defineProps<{ rule: ShippingRule; disabled: boolean }>()
const emit = defineEmits<{ changed: [] }>()
function amount(event: Event) { return Math.round(Number((event.target as HTMLInputElement).value) * 100) }
function copy(index: number) {
  if (props.disabled || (props.rule.regions?.length || 0) >= 100) return
  const original = props.rule.regions?.[index]
  if (!original) return
  props.rule.regions!.push({ ...original, name: `${original.name.slice(0, 50)}（副本）`, province: '', city: '', district: '' })
  emit('changed')
}
const trial = ref({ province: '', city: '', district: '', subtotal: 100, quantity: 1, date: '' })
const quote = computed(() => {
  if (!trial.value.province.trim() || !Number.isFinite(trial.value.subtotal) || trial.value.subtotal < 0 || !Number.isSafeInteger(trial.value.quantity) || trial.value.quantity < 1) return null
  return shippingFee(props.rule, { province: trial.value.province.trim(), city: trial.value.city.trim(), district: trial.value.district.trim() }, Math.round(trial.value.subtotal * 100), trial.value.quantity, trial.value.date)
})
</script>

<template>
  <div>
    <h4>地区模板（最多100条）</h4>
    <p>区县优先于城市、城市优先于省份；禁售省份始终生效。地区附加费在包邮后仍收取，续费按整单规格件数计算。</p>
    <fieldset v-for="(r, index) in rule.regions" :key="index" :disabled="disabled" @input="emit('changed')" @change="emit('changed')">
      <legend>模板 {{ index + 1 }}{{ r.name ? ` · ${r.name}` : '' }}</legend>
      <label>名称<input v-model="r.name" maxlength="60" required /></label>
      <label>省份<input v-model="r.province" maxlength="30" placeholder="如：浙江省" required /></label>
      <label>城市（选填）<input v-model="r.city" maxlength="30" placeholder="如：杭州市" /></label>
      <label>区县（选填，需填写城市）<input v-model="r.district" maxlength="30" /></label>
      <label><input v-model="r.blocked" type="checkbox" />该区域暂停配送</label>
      <label>基础运费（元）<input :value="(r.freight / 100).toFixed(2)" type="number" min="0" max="100000" step="0.01" required @input="r.freight = amount($event)" /></label>
      <label>包邮门槛（元，0表示免基础及续件费）<input :value="(r.freeShippingThreshold / 100).toFixed(2)" type="number" min="0" max="100000" step="0.01" required @input="r.freeShippingThreshold = amount($event)" /></label>
      <label>基础运费包含件数<input v-model.number="r.baseQuantity" type="number" min="1" max="100000" step="1" required /></label>
      <label>每超出一件运费（元）<input :value="(r.extraFreight / 100).toFixed(2)" type="number" min="0" max="100000" step="0.01" required @input="r.extraFreight = amount($event)" /></label>
      <label>地区附加费（元）<input :value="(r.surcharge / 100).toFixed(2)" type="number" min="0" max="100000" step="0.01" required @input="r.surcharge = amount($event)" /></label>
      <div class="template-actions"><button type="button" class="button secondary" :disabled="(rule.regions?.length || 0) >= 100" @click="copy(index)">复制模板</button><button type="button" class="button secondary" @click="rule.regions!.splice(index, 1); emit('changed')">删除模板</button></div>
    </fieldset>
    <p v-if="!rule.regions?.length" class="subtle">暂无地区模板，所有可配送地区使用默认运费。</p>
    <button type="button" class="button secondary" :disabled="disabled || (rule.regions?.length || 0) >= 100" @click="(rule.regions ||= []).push({ name: '', province: '', city: '', district: '', blocked: false, freight: rule.freight, freeShippingThreshold: rule.freeShippingThreshold, baseQuantity: 1, extraFreight: 0, surcharge: 0 }); emit('changed')">新增地区模板</button>
    <label>暂停配送日期（北京时间，逗号分隔）<textarea :value="(rule.blockedDates || []).join(',')" :disabled="disabled" placeholder="2026-10-10,2026-10-11" @input="rule.blockedDates = ($event.target as HTMLTextAreaElement).value.split(',').map(x => x.trim()).filter(Boolean); emit('changed')" /></label>
    <p>暂停日期仅限制指定期望日期；选择尽快配送由商家安排可配送日期。优惠前商品金额达到门槛即包邮，优惠券不抵运费。</p>
    <fieldset :disabled="disabled" class="freight-trial"><legend>运费试算</legend><p>按当前编辑内容试算，修改后需保存才能生效。</p>
      <label>试算省份<input v-model="trial.province" placeholder="如：浙江省" /></label>
      <label>试算城市<input v-model="trial.city" placeholder="如：杭州市" /></label>
      <label>试算区县<input v-model="trial.district" /></label>
      <label>商品金额（元）<input v-model.number="trial.subtotal" type="number" min="0" step="0.01" /></label>
      <label>商品件数<input v-model.number="trial.quantity" type="number" min="1" step="1" /></label>
      <label>试算配送日期<input v-model="trial.date" type="date" /></label>
      <p v-if="quote" role="status">{{ quote.blockedReason || `匹配：${quote.shippingRuleName || '默认运费'}，运费 ¥${(quote.freight / 100).toFixed(2)}（含附加费 ¥${(quote.surcharge / 100).toFixed(2)}）` }}</p>
      <p v-else class="subtle">填写省份、有效商品金额和件数查看结果。</p>
    </fieldset>
  </div>
</template>

<style scoped>
.template-actions{display:flex;gap:8px;flex-wrap:wrap}.freight-trial{margin-top:24px}
</style>
