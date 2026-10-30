<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { settleConfirm, useConfirmState } from '../confirm'

const state = useConfirmState()
function onKey(e: KeyboardEvent) { if (e.key === 'Escape' && state.open) settleConfirm(false) }
onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div v-if="state.open" class="modal-backdrop confirm-backdrop" @click.self="settleConfirm(false)">
    <section class="panel confirm-dialog" role="alertdialog" aria-modal="true" :aria-label="state.title">
      <h2>{{ state.title }}</h2>
      <p class="confirm-message">{{ state.message }}</p>
      <div class="row-actions">
        <button class="button secondary" @click="settleConfirm(false)">取消</button>
        <button class="button" :class="state.danger ? 'danger' : 'primary'" @click="settleConfirm(true)" autofocus>{{ state.confirmText }}</button>
      </div>
    </section>
  </div>
</template>

<style scoped>
.confirm-backdrop { z-index: 200; }
.confirm-dialog { max-width: 420px; width: calc(100vw - 48px); padding: 24px; }
.confirm-dialog h2 { margin: 0 0 12px; font-size: 16px; }
.confirm-message { margin: 0 0 20px; font-size: 13px; line-height: 1.8; color: var(--ink); white-space: pre-wrap; overflow-wrap: anywhere; }
.row-actions { display: flex; justify-content: flex-end; gap: 10px; }
.button.danger { background: #b3372f; color: #fff; border: none; }
.button.danger:hover { background: #99291f; }
</style>
