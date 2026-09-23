import { createRouter, createWebHistory } from 'vue-router'
import { DEFAULT_SCREEN, SCREEN_MODULES } from '../modules'

/**
 * 业务界面的路由表，由 `modules/index.ts` 的注册表派生。
 *
 * 采用 history 模式（URL 形如 `/dashboard`），因此管理口的静态文件服务必须为
 * 未命中静态文件的 GET 请求回退到 `index.html`（见 `internal/admin/handler.go`
 * 的 `spaFileServer`），否则直接打开或刷新深链接会 404。
 *
 * 认证入口不是路由：未登录时 `App.vue` 展示登录视图，不占用业务路径，
 * 因此登录成功后仍停留在用户原本要访问的 URL 上。
 */
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    ...SCREEN_MODULES.map((m) => ({
      path: m.path,
      name: m.name,
      component: m.component,
      meta: { title: m.title },
    })),
    { path: '/', redirect: { name: DEFAULT_SCREEN } },
    { path: '/:pathMatch(.*)*', redirect: { name: DEFAULT_SCREEN } },
  ],
})
