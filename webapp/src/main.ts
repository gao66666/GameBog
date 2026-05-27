import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'

import App from './App.vue'
import router from './router'
import { ensureDefaultLogin } from '@/utils/autoLogin'

import '../../web/static/styles.css'
import '../../web/static/editor.css'
import 'highlight.js/styles/github-dark-dimmed.css'

async function bootstrap() {
  await ensureDefaultLogin()

  const app = createApp(App)
  app.use(createPinia())
  app.use(router)
  app.use(ElementPlus, { locale: zhCn })
  app.mount('#app')
}

bootstrap()
