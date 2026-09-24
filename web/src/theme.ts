import { computed, ref, watch } from 'vue'

/**
 * 主题模式：`system`（默认）/ `light` / `dark`。
 *
 * 落地方式与 tailwind.config.js 的 `darkMode: 'class'` 对齐：解析出实际生效的主题后，
 * 在 `<html>` 上挂 `dark` 或 `light` 类，两类调色板由 `style.css` 的 CSS 变量提供
 * （见其中的 `--c-*` 定义）。这样组件里不必写任何 `dark:` 变体——用的是同一批语义色。
 *
 * 首屏避免闪烁靠 `index.html` 里同键的同步脚本：它在 CSS 之前读 localStorage 并挂类，
 * 因此本模块只负责后续的切换与跟随系统。
 */
export type ThemeMode = 'system' | 'light' | 'dark'
export type ResolvedTheme = 'light' | 'dark'

/** 与 `index.html` 的首屏脚本共用同一个键。 */
export const THEME_KEY = 'jev-console-theme'

const MEDIA = '(prefers-color-scheme: dark)'

function read(): ThemeMode {
  try {
    const saved = localStorage.getItem(THEME_KEY)
    if (saved === 'system' || saved === 'light' || saved === 'dark') return saved
  } catch {
    /* 隐私模式下读不到：按默认的跟随系统处理 */
  }
  return 'system'
}

const mode = ref<ThemeMode>(read())

/** 系统偏好；「跟随系统」模式下由它决定实际主题。 */
const prefersDark = ref(false)
const media = typeof window !== 'undefined' && typeof window.matchMedia === 'function' ? window.matchMedia(MEDIA) : null
if (media) {
  prefersDark.value = media.matches
  media.addEventListener('change', (e) => {
    prefersDark.value = e.matches
  })
}

const resolved = computed<ResolvedTheme>(() =>
  mode.value === 'system' ? (prefersDark.value ? 'dark' : 'light') : mode.value,
)

function apply(theme: ResolvedTheme): void {
  const root = document.documentElement
  root.classList.toggle('dark', theme === 'dark')
  root.classList.toggle('light', theme === 'light')
  // 原生控件（滚动条、下拉、date picker）跟随主题走，而不是永远按暗色渲染。
  root.style.colorScheme = theme
}

watch(resolved, apply, { immediate: true })

export function setThemeMode(next: ThemeMode): void {
  mode.value = next
  try {
    localStorage.setItem(THEME_KEY, next)
  } catch {
    /* 存不下就只在本次会话生效，不影响界面 */
  }
}

/** 组件侧入口：`mode` 是用户选择，`resolved` 是实际生效的主题。 */
export function useTheme() {
  return {
    mode: computed(() => mode.value),
    resolved,
    setThemeMode,
  }
}
