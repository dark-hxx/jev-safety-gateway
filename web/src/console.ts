import { inject, provide, type ComputedRef, type InjectionKey } from 'vue'
import type { JEVKey, Settings, Stats } from './types'

/**
 * 控制台会话上下文：登录后 `GET /api/state` 带回的共享状态，由 `App.vue` 装载。
 *
 * 壳层与各路由界面之间不互相传参，都从这里取用。这样新增界面模块时，
 * `App.vue` 不需要为它增加任何绑定——只要 `useConsole()` 就能拿到配置、
 * 密钥池与 24h 统计，并在写入后请求全局刷新。
 */
export interface ConsoleContext {
  /** 当前配置；配置界面保存成功后由 `App.vue` 同步。 */
  settings: ComputedRef<Settings>
  /** 密钥池。 */
  keys: ComputedRef<JEVKey[]>
  /** `/api/state` 带回的 24h 统计快照（首屏用，避免白屏）。 */
  stats24h: ComputedRef<Stats>
  /** 重新拉取 `/api/state`；失败时保持现有数据，不清空界面。 */
  refresh: () => Promise<void>
  /** 配置保存成功：同步全局配置并刷新密钥池与统计。 */
  applySettings: (s: Settings) => Promise<void>
}

const CONSOLE_KEY: InjectionKey<ConsoleContext> = Symbol('jev-console')

export function provideConsole(ctx: ConsoleContext): void {
  provide(CONSOLE_KEY, ctx)
}

export function useConsole(): ConsoleContext {
  const ctx = inject(CONSOLE_KEY)
  if (!ctx) throw new Error('控制台上下文缺失：useConsole() 只能在已登录的控制台内使用')
  return ctx
}
