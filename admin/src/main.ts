import { createApp } from 'vue'
import App from './App.vue'
import { can, currentAdmin } from './permissions'
import './styles.css'

const applyPermission = (el: HTMLButtonElement, binding: { value: string | string[] }) => {
  const permissions = Array.isArray(binding.value) ? binding.value : [binding.value]
  if (!permissions.every(permission => can(currentAdmin(), permission))) {
    el.disabled = true
    el.title = '当前账号没有此操作权限'
    el.setAttribute('aria-disabled', 'true')
  }
}
createApp(App).directive('permission', { mounted: applyPermission, updated: applyPermission }).mount('#app')
