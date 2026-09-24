import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
// 主题与语言模块在导入时自读 localStorage 并应用（主题挂 <html> 类、语言写 <html lang>），
// 因此必须在挂载前导入——首屏不应先按错误主题/语言渲染一帧。
import './theme'
import './i18n'
import './style.css'

createApp(App).use(router).mount('#app')
