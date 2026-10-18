import { createApp } from 'vue'
import App from './App.vue'
import { router, resetAuthCheck } from './router'
import { setUnauthorizedHandler } from './lib/api'
import { initTheme } from './lib/theme'
import { vTip } from './lib/tooltip'
import './styles.css'

initTheme()
setUnauthorizedHandler(() => {
  resetAuthCheck()
  if (router.currentRoute.value.name !== 'login') router.push({ name: 'login' })
})
createApp(App).use(router).directive('tip', vTip).mount('#app')
