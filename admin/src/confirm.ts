import { reactive } from 'vue'

export interface ConfirmOptions {
  title?: string
  message: string
  confirmText?: string
  danger?: boolean
}

const state = reactive({
  open: false,
  title: '请确认',
  message: '',
  confirmText: '确认',
  danger: false,
  resolve: null as null | ((value: boolean) => void),
})

// 统一二次确认：返回 Promise，确认 resolve(true)，取消/关闭/Esc resolve(false)。
// 敏感操作（退款、配置发布、权限变更、删除等）一律经此弹窗，替代原生 confirm()。
export function confirmAction(options: ConfirmOptions): Promise<boolean> {
  return new Promise((resolve) => {
    state.title = options.title || '请确认'
    state.message = options.message
    state.confirmText = options.confirmText || '确认'
    state.danger = !!options.danger
    state.resolve = resolve
    state.open = true
  })
}

export function useConfirmState() {
  return state
}

export function settleConfirm(value: boolean) {
  state.open = false
  const resolve = state.resolve
  state.resolve = null
  resolve?.(value)
}
