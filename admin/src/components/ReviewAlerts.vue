<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
const emit = defineEmits<{ open: [] }>()
const count = ref(0), failed = ref(false)
let timer: ReturnType<typeof setInterval> | undefined
let mounted = true
async function refresh() {
  try { const result = await api<{ count: number }>('admin.review.alertCount'); if (mounted) { count.value = result.count; failed.value = false } }
  catch { if (mounted) failed.value = true }
}
onMounted(() => { void refresh(); timer = setInterval(refresh, 60000) })
onUnmounted(() => { mounted = false; if (timer) clearInterval(timer) })
</script>
<template><button v-if="count || failed" class="button review-alert-badge" @click="emit('open')" :aria-label="failed ? '差评提醒加载失败，打开评价模块重试' : `${count}条差评待处理`">{{ failed ? '差评提醒加载失败' : `差评 ${count}` }}</button></template>
<style scoped>.review-alert-badge { color: #9d321e; background: #fff2eb; border-color: #f3c8b8; }</style>
