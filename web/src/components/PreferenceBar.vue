<script setup lang="ts">
import Icon from './Icon.vue'
import { useI18n } from '../i18n'
import { useTheme, type ThemeMode } from '../theme'

/**
 * 外观偏好控件：主题（跟随系统 / 浅色 / 深色）与界面语言（中 / EN）。
 *
 * 壳层顶栏与登录页都放这一个组件，因此登录前就能切换——首次进入控制台的人
 * 正是最需要把界面调成自己语言与主题的人。
 *
 * 两个偏好都只落在 localStorage，不经后端：它们属于**使用者所在浏览器**的显示偏好，
 * 不是网关的运行配置，不该被共享的 `/api/settings` 带成全局值。
 */
defineProps<{ compact?: boolean }>()

const { locale, t, setLocale } = useI18n()
const { mode, setThemeMode } = useTheme()

const THEMES: { value: ThemeMode; icon: string; labelKey: 'pref.theme.system' | 'pref.theme.light' | 'pref.theme.dark' }[] = [
  { value: 'system', icon: 'monitor', labelKey: 'pref.theme.system' },
  { value: 'light', icon: 'sun', labelKey: 'pref.theme.light' },
  { value: 'dark', icon: 'moon', labelKey: 'pref.theme.dark' },
]
</script>

<template>
  <div class="flex items-center" :class="compact ? 'gap-1' : 'gap-space-sm'">
    <!-- 主题：三态分段控件，当前模式常亮 -->
    <div
      class="inline-flex items-center p-0.5 rounded-full bg-surface-container-high/80 shadow-sm backdrop-blur-md"
      role="group"
      :aria-label="t('pref.theme')"
    >
      <button
        v-for="opt in THEMES"
        :key="opt.value"
        type="button"
        class="rounded-full transition-all duration-200 flex items-center justify-center"
        :class="[
          compact ? 'h-6 w-6' : 'h-7 w-7',
          mode === opt.value ? 'bg-surface-bright text-on-surface shadow-sm' : 'text-on-surface-variant hover:text-on-surface',
        ]"
        :title="`${t('pref.theme')} · ${t(opt.labelKey)}`"
        :aria-label="t('pref.theme.switchTo', { name: t(opt.labelKey) })"
        :aria-pressed="mode === opt.value"
        @click="setThemeMode(opt.value)"
      >
        <Icon :name="opt.icon" :class="compact ? 'text-[13px]' : 'text-[15px]'" />
      </button>
    </div>

    <!-- 语言：两态分段控件，直接显示目标语言的自称，不依赖当前界面语言 -->
    <div
      class="inline-flex items-center p-0.5 rounded-full bg-surface-container-high/80 shadow-sm backdrop-blur-md"
      role="group"
      :aria-label="t('pref.lang')"
    >
      <button
        v-for="opt in (['zh', 'en'] as const)"
        :key="opt"
        type="button"
        class="rounded-full transition-all duration-200 font-caption-2 text-caption-2 leading-none flex items-center justify-center"
        :class="[
          compact ? 'h-6 px-2' : 'h-7 px-2.5',
          locale === opt ? 'bg-surface-bright text-on-surface shadow-sm font-semibold' : 'text-on-surface-variant hover:text-on-surface',
        ]"
        :aria-label="t('pref.lang.switchTo', { name: opt === 'zh' ? '中文' : 'English' })"
        :aria-pressed="locale === opt"
        @click="setLocale(opt)"
      >
        {{ opt === 'zh' ? '中' : 'EN' }}
      </button>
    </div>
  </div>
</template>
