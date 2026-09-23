import type { Component } from 'vue'

/**
 * 一个界面模块：路由、导航元信息与视图组件由模块自己声明。
 *
 * 壳层（`AppShell.vue`）与路由表（`router/index.ts`）都只遍历注册表，
 * 因此新增界面只需新增一个模块目录并在 `modules/index.ts` 注册，
 * 无需改动壳层、现有界面与认证入口。
 */
export interface ScreenModule {
  /** 路由名，用于 `router.push({ name })`。 */
  name: string
  /** 路由路径，即业务界面的 URL。 */
  path: string
  /** 左侧导航与页面标题的中英文名。 */
  title: { zh: string; en: string }
  /** 导航图标名（见 `components/Icon.vue`）。 */
  icon: string
  /** 视图组件；用动态导入以便按模块分包。 */
  component: () => Promise<Component>
}
