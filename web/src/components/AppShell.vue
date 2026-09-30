<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import Icon from './Icon.vue'
import PreferenceBar from './PreferenceBar.vue'
import { SCREEN_MODULES } from '../modules'
import { useI18n } from '../i18n'

/**
 * 控制台外壳：左侧导航 + 顶部状态条。
 *
 * 导航完全由 `modules/index.ts` 的模块注册表驱动（图标、中英文名、路径都在模块里），
 * 壳层不认识任何具体界面，因此新增界面不需要改动本文件。当前项由路由判定。
 *
 * 壳层只展示**真实数据**：过滤开关状态、JEV 检定模型、密钥池数量。
 * 原型中「Engine Core v2.4.1-rc」「Apple HIG Spec」一类无来源信息已移除，
 * 版本/构建信息缺口见 docs/admin-console-backend-gaps.md。
 *
 * 界面语言由 `i18n` 提供：每个标题只显示**当前语言**的一条（中英不再同屏并排）。
 *
 * **窄屏（<lg）**：按 DESIGN.md 的断点约定（「Mobile: single stacked view」「Fixed
 * 260px sidebar … collapsing to a floating navigation pill or drawer on smaller
 * breakpoints」），侧栏收成抽屉：汉堡打开，点遮罩 / 选中菜单 / 切路由 / Esc 关闭；
 * 顶栏压成一行放下（汉堡 + 状态胶囊 + 主开关 + 退出），主题与语言偏好移进抽屉。
 * ≥lg 一切照旧。
 */
defineProps<{
  enabled: boolean
  jevModel: string
  keysEnabled: number
  keysTotal: number
  busy: boolean
}>()

const emit = defineEmits<{
  (e: 'logout'): void
  (e: 'toggle-enabled', next: boolean): void
}>()

const route = useRoute()
const { t, localized } = useI18n()

/** 与 Tailwind `lg`（1024px）一致：这是「固定侧栏」的起始宽度。 */
const wideQuery = window.matchMedia('(min-width: 1024px)')

/**
 * 是否处于固定侧栏宽度。用 matchMedia 而不只靠 CSS 变体，是为了让收起的抽屉
 * 能真的从无障碍树与 Tab 顺序里移出去——只写 `-translate-x-full` 的话，抽屉
 * 虽然看不见，键盘仍然会聚焦到屏幕外的导航项上。
 */
const wide = ref(wideQuery.matches)
const navOpen = ref(false)

/** 抽屉已收起（只在窄屏且未打开时成立）。 */
const drawerHidden = computed(() => !wide.value && !navOpen.value)

function syncWide(): void {
  wide.value = wideQuery.matches
  if (wideQuery.matches) navOpen.value = false
}

function onKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape') navOpen.value = false
}

// 打开抽屉时锁住页面滚动，否则遮罩上的触摸滑动会带着后面的页面一起滚。
watch([navOpen, wide], () => {
  document.body.style.overflow = navOpen.value && !wide.value ? 'hidden' : ''
})

// 切路由即收起抽屉：手机上点完菜单要立刻看到目标页。
watch(
  () => route.name,
  () => {
    navOpen.value = false
  },
)

onMounted(() => {
  syncWide()
  wideQuery.addEventListener('change', syncWide)
  window.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => {
  wideQuery.removeEventListener('change', syncWide)
  window.removeEventListener('keydown', onKeydown)
  document.body.style.overflow = ''
})

const linkClass =
  'w-full text-left flex items-center gap-space-sm px-space-sm py-2 rounded-lg transition-all'
const linkActiveClass = `${linkClass} bg-surface-raised text-on-surface font-semibold shadow-sm`
const linkIdleClass = `${linkClass} text-subheadline font-subheadline text-on-surface-variant hover:bg-surface-container hover:text-on-surface`
</script>

<template>
  <div class="min-h-screen bg-surface">
    <!-- 窄屏抽屉遮罩；点它关闭。≥lg 不渲染。 -->
    <div v-if="navOpen" class="fixed inset-0 bg-black/40 z-40 lg:hidden" @click="navOpen = false"></div>

    <!-- 左侧导航：≥lg 恒为固定侧栏，<lg 为可开合抽屉 -->
    <aside
      id="shell-nav"
      v-bind="drawerHidden ? { inert: true } : {}"
      :aria-hidden="drawerHidden ? 'true' : undefined"
      :class="navOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'"
      class="fixed left-0 top-0 h-full w-64 max-w-[80vw] bg-surface-container-low z-50 flex flex-col justify-between py-space-md border-r border-hairline overflow-y-auto transition-transform duration-200 ease-out"
    >
      <div class="flex flex-col gap-space-md">
        <div class="px-space-md flex items-center gap-space-sm">
          <!-- 原型此处引用公网图片资源，离线约束下改为本地内联图标标记 -->
          <div class="h-8 w-8 rounded-lg bg-surface-raised flex items-center justify-center text-primary shadow-sm shrink-0">
            <Icon name="shield-check" class="text-[18px]" />
          </div>
          <div class="flex flex-col min-w-0">
            <span class="text-headline font-headline text-on-surface tracking-tight leading-tight">{{ t('app.name') }}</span>
            <span class="text-caption-2 font-caption-2 text-outline leading-tight">{{ t('app.tagline') }}</span>
          </div>
          <!-- 抽屉里的关闭入口：遮罩与 Esc 都能关，但手机上先看到的是这个按钮 -->
          <button
            type="button"
            class="ml-auto shrink-0 inline-flex items-center justify-center h-8 w-8 rounded-lg text-on-surface-variant hover:bg-surface-container transition-colors lg:hidden"
            :aria-label="t('shell.closeNav')"
            @click="navOpen = false"
          >
            <Icon name="x" class="text-[16px]" />
          </button>
        </div>

        <div class="px-space-md pt-space-xs">
          <span class="eyebrow">{{ t('nav.section') }}</span>
        </div>

        <nav class="flex flex-col gap-space-xs px-space-sm">
          <RouterLink
            v-for="item in SCREEN_MODULES"
            :key="item.name"
            :to="{ name: item.name }"
            :class="route.name === item.name ? linkActiveClass : linkIdleClass"
            :aria-current="route.name === item.name ? 'page' : undefined"
          >
            <Icon :name="item.icon" class="text-[16px] shrink-0" />
            <span>{{ localized(item.title) }}</span>
          </RouterLink>
        </nav>
      </div>

      <div class="px-space-md flex flex-col gap-space-sm">
        <!-- 窄屏时顶栏放不下主题与语言，移到这里；≥lg 顶栏里那份才是唯一入口 -->
        <div class="lg:hidden flex justify-center">
          <PreferenceBar compact />
        </div>
        <div class="p-space-sm rounded-xl bg-surface-raised flex flex-col gap-space-xs">
          <div class="flex items-center justify-between text-caption-2 font-caption-2 text-on-surface-variant">
            <span>{{ t('shell.jevModel') }}</span>
            <span class="font-code-badge text-code-badge text-primary truncate max-w-[7rem]" :title="jevModel">
              {{ jevModel || t('shell.notSet') }}
            </span>
          </div>
          <div class="flex items-center gap-space-xs text-caption-1 font-caption-1 text-on-surface">
            <span class="h-1.5 w-1.5 rounded-full" :class="enabled ? 'bg-secondary' : 'bg-outline'"></span>
            <span>{{ enabled ? t('shell.filterOn') : t('shell.filterOff') }}</span>
          </div>
          <div class="flex items-center justify-between text-caption-2 font-caption-2 text-on-surface-variant">
            <span>{{ t('shell.keyPool') }}</span>
            <span class="font-code-body text-code-body text-on-surface">
              {{ t('shell.keysCount', { enabled: keysEnabled, total: keysTotal }) }}
            </span>
          </div>
        </div>
        <div class="flex items-center justify-between text-caption-2 font-caption-2 text-outline px-space-xs">
          <span>{{ t('shell.offline') }}</span>
          <span>go:embed</span>
        </div>
      </div>
    </aside>

    <div class="lg:pl-64">
      <!-- 顶部状态条：≥lg 从侧栏右侧起；<lg 横跨整屏 -->
      <header class="fixed top-0 left-0 right-0 lg:left-64 h-16 z-30 material-bar">
        <div class="h-16 w-full px-margin-mobile md:px-space-md flex items-center justify-between gap-space-sm lg:gap-space-md">
          <div class="flex items-center gap-space-sm lg:gap-space-md min-w-0">
            <button
              type="button"
              class="shrink-0 inline-flex items-center justify-center h-8 w-8 rounded-lg text-on-surface hover:bg-surface-container transition-colors lg:hidden"
              :aria-label="t('shell.openNav')"
              aria-controls="shell-nav"
              :aria-expanded="navOpen"
              @click="navOpen = true"
            >
              <Icon name="menu" class="text-[18px]" />
            </button>
            <div class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-surface-raised shrink-0">
              <span
                class="h-2 w-2 rounded-full"
                :class="enabled ? 'bg-secondary animate-pulse' : 'bg-outline'"
              ></span>
              <span class="text-caption-2 font-caption-2" :class="enabled ? 'text-secondary' : 'text-outline'">
                {{ enabled ? t('shell.filteringActive') : t('shell.filteringOff') }}
              </span>
            </div>
            <div class="flex items-center gap-2 pl-space-xs">
              <span class="hidden sm:inline text-caption-2 font-caption-2 text-on-surface-variant">{{ t('shell.masterSwitch') }}</span>
              <label class="relative inline-flex items-center cursor-pointer">
                <input
                  class="sr-only peer"
                  type="checkbox"
                  :checked="enabled"
                  :disabled="busy"
                  :aria-label="t('shell.masterSwitch')"
                  @change="emit('toggle-enabled', ($event.target as HTMLInputElement).checked)"
                />
                <div
                  class="w-11 h-6 bg-surface-container-highest rounded-full peer peer-checked:bg-secondary-container peer-checked:after:translate-x-full after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-5 after:w-5 after:transition-all peer-disabled:opacity-50 shadow-sm"
                ></div>
              </label>
            </div>
          </div>

          <div class="flex items-center gap-space-sm shrink-0">
            <div class="hidden lg:flex"><PreferenceBar /></div>
            <div class="hidden lg:block w-px h-6 bg-hairline"></div>
            <button
              class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-surface-container hover:bg-surface-container-high text-subheadline font-subheadline text-on-surface transition-all"
              type="button"
              :aria-label="t('shell.logout')"
              @click="emit('logout')"
            >
              <Icon name="logout" class="text-[16px]" />
              <span class="hidden lg:inline">{{ t('shell.logout') }}</span>
            </button>
            <div class="hidden lg:flex w-8 h-8 rounded-full bg-primary items-center justify-center">
              <Icon name="person" class="text-on-primary text-[18px]" />
            </div>
          </div>
        </div>
      </header>

      <main class="relative w-full pt-16 bg-surface min-h-screen">
        <slot />
      </main>
    </div>
  </div>
</template>
