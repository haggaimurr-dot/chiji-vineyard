<script setup lang="ts">
import { ref, watch } from 'vue'
import { generateBatchQR } from '../batch-qr'
import type { ManagedBatch } from '../product-management'
const props = defineProps<{ batch: ManagedBatch }>()
const emit = defineEmits<{ close: [] }>()
const kind = ref<'internal'|'external'>('internal'), image = ref(''), error = ref(''), busy = ref(false)
let request = 0
watch(() => [props.batch, kind.value], async () => {
  const id = ++request
  image.value = ''; error.value = ''; busy.value = true
  try {
    const data = await generateBatchQR(props.batch, kind.value)
    if (id === request) image.value = data
  } catch (e) { if (id === request) error.value = (e as Error).message }
  finally { if (id === request) busy.value = false }
}, { immediate: true })
</script>
<template>
  <div class="modal-backdrop"><section class="drawer" role="dialog" aria-modal="true" aria-label="批次二维码">
    <div class="drawer-head"><h2>批次二维码 · {{batch.number}}</h2><button type="button" class="close" @click="emit('close')">×</button></div>
    <div class="drawer-body">
      <label>二维码用途<select v-model="kind"><option value="internal">内部溯源码</option><option v-if="batch.externalTrace" value="external">外部平台链接</option></select></label>
      <p>{{kind==='internal'?'在小程序「批次溯源查询」中扫描此码，也可手动输入下方溯源码。':'使用微信扫一扫或浏览器扫码访问外部平台，资料由该平台提供。'}}</p>
      <p v-if="busy" role="status">生成中…</p><p v-if="error" class="form-error" role="alert">{{error}}</p>
      <img v-if="image" :src="image" alt="批次溯源二维码" class="qr-image" />
      <p class="qr-value">{{kind==='external'?batch.externalTrace?.url:batch.code||batch.id}}</p>
      <p v-if="batch.status==='ARCHIVED'">此批次已归档，二维码仍可查询历史资料。</p>
    </div>
    <div class="drawer-foot"><button class="button secondary" @click="emit('close')">关闭</button><a v-if="image&&!busy" class="button primary" :href="image" :download="`${batch.id}-${kind}-二维码.png`">下载 PNG</a></div>
  </section></div>
</template>
<style scoped>
select{display:block;padding:10px;margin-top:8px}.qr-image{display:block;width:280px;max-width:100%;margin:16px auto}.qr-value{overflow-wrap:anywhere}p{line-height:1.7;margin:12px 0}
</style>
