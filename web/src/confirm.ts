import { reactive } from 'vue'

/**
 * 全局确认弹框的调用入口。
 *
 * 替代 `window.confirm`：原生弹框由浏览器绘制，样式无法对齐控制台，标题栏还会
 * 带上页面地址（`127.0.0.1:18081 显示`），在自建的界面里像一处未完成的占位。
 * 更实际的问题是它**同步阻塞**——`alert`/`confirm` 期间整个选项卡停止响应，
 * 而这里每个调用点后面都还跟着一次 await 的网络请求。
 *
 * 用法与 `window.confirm` 等形，只是换成 await：
 *
 * ```ts
 * if (!(await confirmDialog({ title, message, danger: true }))) return
 * ```
 *
 * 状态放在模块作用域而不是 provide/inject：弹框同一时刻只该有一个，调用点又散布在
 * 各路由页面里，模块级单例不必为它穿一层依赖。宿主组件（`ConfirmDialog.vue`）在
 * `App.vue` 挂载一次即可。
 */
export interface ConfirmOptions {
  /** 标题：一句话说清要做什么，动词开头。 */
  title: string
  /** 正文：说明后果，尤其是不可撤销的部分；含糊的「确定吗」等于没问。 */
  message: string
  /**
   * 确认按钮文案，**必填**。这里不给「确定」之类的兜底：确认按钮应当写明这次要做什么
   * （「删除」「封禁此来源 IP」），一个通用的「确定」在危险操作上等于没写。
   */
  confirmText: string
  /** 取消按钮文案，缺省用「取消」——它没有歧义，不需要每个调用点再写一遍。 */
  cancelText?: string
  /** 危险操作（不可撤销、影响线上流量）：确认按钮改为实心警示色并获得初始焦点。 */
  danger?: boolean
}

/** 弹框组件读取的状态；`options` 在关闭动画结束后才清空，否则内容会先消失再滑出。 */
export const confirmState = reactive<{ open: boolean; options: ConfirmOptions | null }>({
  open: false,
  options: null,
})

/** 当前等待答复的调用方。同一时刻只会有一个。 */
let pending: ((ok: boolean) => void) | null = null

/**
 * 弹出确认框，`true` 表示用户确认。
 *
 * 弹框会盖住页面，正常情况下不会并发调用；真的并发时把前一个按「取消」结掉，
 * 而不是让它的 await 永远悬在那里——那种悬空会让调用方的整个函数体静默不执行。
 */
export function confirmDialog(options: ConfirmOptions): Promise<boolean> {
  pending?.(false)
  confirmState.options = options
  confirmState.open = true
  return new Promise<boolean>((resolve) => {
    pending = resolve
  })
}

/** 关闭弹框并把结果交给调用方；由 `ConfirmDialog.vue` 调用。 */
export function settleConfirm(ok: boolean): void {
  confirmState.open = false
  const resolve = pending
  pending = null
  resolve?.(ok)
}

/** 关闭动画结束后丢弃文案。 */
export function clearConfirmOptions(): void {
  if (!confirmState.open) confirmState.options = null
}
