import { computed, ref, watch } from 'vue'
import { zh, type MessageKey } from './zh'
import { en } from './en'

/**
 * 控制台国际化。手写而非引入 vue-i18n：控制台只有中英两种语言、没有复数/日期本地化需求，
 * 而 `web/dist` 是提交入库并被 `//go:embed` 打包的构建产物——少一个运行时依赖，
 * 离线构建链路就少一处需要跟着升级的东西。
 *
 * 文案真源是 `zh.ts`；`en.ts` 的类型是 `Record<MessageKey, string>`，
 * 因此漏译是编译错误而不是运行时回退。
 */
export type Locale = 'zh' | 'en'

/** 与 `index.html` 的首屏脚本共用同一个键：主题/语言必须在绘制前就能读到。 */
export const LOCALE_KEY = 'jev-console-locale'

const DICTS: Record<Locale, Record<MessageKey, string>> = { zh, en }

function read(): Locale {
  try {
    const saved = localStorage.getItem(LOCALE_KEY)
    if (saved === 'zh' || saved === 'en') return saved
  } catch {
    /* 隐私模式下 localStorage 不可用：退回按浏览器语言判断 */
  }
  // 默认跟随浏览器语言：中文环境下与产品默认一致，其它语言环境下不必先找切换按钮。
  const nav = typeof navigator !== 'undefined' ? navigator.language : ''
  return nav && !nav.toLowerCase().startsWith('zh') ? 'en' : 'zh'
}

const locale = ref<Locale>(read())

watch(
  locale,
  (l) => {
    document.documentElement.lang = l === 'zh' ? 'zh-CN' : 'en'
    try {
      localStorage.setItem(LOCALE_KEY, l)
    } catch {
      /* 存不下就只在本次会话生效，不影响界面 */
    }
  },
  { immediate: true },
)

/**
 * 取文案。`t()` 在渲染期读取 `locale`，因此模板与 `computed` 里的调用都会随语言切换自动重渲染。
 *
 * `{name}` 占位符由 `params` 替换；键不存在时返回键本身——界面照常可用，
 * 缺的那条一眼就能在界面上定位。
 */
export function t(key: MessageKey, params?: Record<string, string | number>): string {
  let s = DICTS[locale.value][key] ?? zh[key] ?? key
  if (params) {
    for (const [k, v] of Object.entries(params)) s = s.split(`{${k}}`).join(String(v))
  }
  return s
}

/**
 * 取「中英双写」数据里的当前语言一条。模块注册表的标题（`modules/index.ts`）是这种形状：
 * 它随模块一起注册，不进文案表，因此用这个而不是 `t()`。
 */
export function localized(pair: { zh: string; en: string }): string {
  return locale.value === 'en' ? pair.en : pair.zh
}

export function setLocale(next: Locale): void {
  locale.value = next
}

function toggleLocale(): void {
  locale.value = locale.value === 'zh' ? 'en' : 'zh'
}

/** 组件侧入口。`locale` 用于「仅中文界面保留英文副标题」这类版式分支。 */
export function useI18n() {
  return {
    locale: computed(() => locale.value),
    t,
    localized,
    setLocale,
    toggleLocale,
  }
}
