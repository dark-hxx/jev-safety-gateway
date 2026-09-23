import type { ScreenModule } from './types'

/**
 * 界面模块注册表。左侧导航与路由表都由它派生，顺序即导航顺序。
 *
 * 新增界面：新建 `modules/<name>/` 目录并导出一个 `ScreenModule`，在此数组追加一项即可。
 */
export const SCREEN_MODULES: ScreenModule[] = [
  {
    name: 'dashboard',
    path: '/dashboard',
    title: { zh: '运行概览', en: 'Dashboard' },
    icon: 'gauge',
    component: () => import('./dashboard/DashboardView.vue'),
  },
  {
    name: 'settings',
    path: '/settings',
    title: { zh: '网关安全配置', en: 'Gateway & Security Settings' },
    icon: 'sliders',
    component: () => import('./settings/SettingsView.vue'),
  },
  {
    name: 'audit',
    path: '/audit',
    title: { zh: '转发审计记录', en: 'Forwarding & Audit Stream' },
    icon: 'layers',
    component: () => import('./audit/AuditView.vue'),
  },
]

/** 默认界面：根路径与未匹配路径都重定向到这里。 */
export const DEFAULT_SCREEN = 'dashboard'

export type { ScreenModule }
