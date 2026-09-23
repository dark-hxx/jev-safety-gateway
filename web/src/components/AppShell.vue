<script setup lang="ts">
import { RouterLink, useRoute } from 'vue-router'
import Icon from './Icon.vue'
import { SCREEN_MODULES } from '../modules'

/**
 * 控制台外壳：左侧固定导航 + 顶部状态条。
 *
 * 导航完全由 `modules/index.ts` 的模块注册表驱动（图标、中英文名、路径都在模块里），
 * 壳层不认识任何具体界面，因此新增界面不需要改动本文件。当前项由路由判定。
 *
 * 壳层只展示**真实数据**：过滤开关状态、JEV 检定模型、密钥池数量。
 * 原型中「Engine Core v2.4.1-rc」「Apple HIG Spec」一类无来源信息已移除，
 * 版本/构建信息缺口见 docs/admin-console-backend-gaps.md。
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

const linkClass =
  'w-full text-left flex items-center gap-space-sm px-space-sm py-2 rounded-lg transition-all'
const linkActiveClass = `${linkClass} bg-surface-container-high text-on-surface font-semibold shadow-sm`
const linkIdleClass = `${linkClass} text-subheadline font-subheadline text-on-surface-variant hover:bg-surface-container hover:text-on-surface`
</script>

<template>
  <div class="min-h-screen bg-surface">
    <!-- 左侧导航 -->
    <aside
      class="fixed left-0 top-0 h-full w-64 bg-surface-container-low z-50 flex flex-col justify-between py-space-md border-r border-hairline"
    >
      <div class="flex flex-col gap-space-md">
        <div class="px-space-md flex items-center gap-space-sm">
          <!-- 原型此处引用公网图片资源，离线约束下改为本地内联图标标记 -->
          <div class="h-8 w-8 rounded-lg bg-surface-container-high flex items-center justify-center text-primary shadow-sm">
            <Icon name="shield-check" class="text-[18px]" />
          </div>
          <div class="flex flex-col">
            <span class="text-headline font-headline text-on-surface tracking-tight leading-tight">JEV Safety Gateway</span>
            <span class="text-caption-2 font-caption-2 text-outline leading-tight">AI Safety Control</span>
          </div>
        </div>

        <div class="px-space-md pt-space-xs">
          <span class="eyebrow">导航 Navigation</span>
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
            <span class="flex flex-col leading-tight">
              <span>{{ item.title.zh }}</span>
              <span class="text-caption-2 font-caption-2 text-outline">{{ item.title.en }}</span>
            </span>
          </RouterLink>
        </nav>
      </div>

      <div class="px-space-md flex flex-col gap-space-sm">
        <div class="p-space-sm rounded-xl bg-surface-container-high flex flex-col gap-space-xs">
          <div class="flex items-center justify-between text-caption-2 font-caption-2 text-on-surface-variant">
            <span>检定模型</span>
            <span class="font-code-badge text-code-badge text-primary truncate max-w-[7rem]" :title="jevModel">
              {{ jevModel || '未设置' }}
            </span>
          </div>
          <div class="flex items-center gap-space-xs text-caption-1 font-caption-1 text-on-surface">
            <span class="h-1.5 w-1.5 rounded-full" :class="enabled ? 'bg-secondary' : 'bg-outline'"></span>
            <span>{{ enabled ? '过滤已启用' : '过滤已关闭' }}</span>
          </div>
          <div class="flex items-center justify-between text-caption-2 font-caption-2 text-on-surface-variant">
            <span>密钥池</span>
            <span class="font-code-body text-code-body text-on-surface">{{ keysEnabled }} / {{ keysTotal }} 启用</span>
          </div>
        </div>
        <div class="flex items-center justify-between text-caption-2 font-caption-2 text-outline px-space-xs">
          <span>离线资源 · 无公网依赖</span>
          <span>go:embed</span>
        </div>
      </div>
    </aside>

    <div class="pl-64">
      <!-- 顶部状态条 -->
      <header class="fixed top-0 left-64 right-0 h-16 z-40 material-bar">
        <div class="h-16 w-full px-space-md flex items-center justify-between gap-space-md">
          <div class="flex items-center gap-space-md min-w-max">
            <div class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-surface-container-high">
              <span
                class="h-2 w-2 rounded-full"
                :class="enabled ? 'bg-secondary animate-pulse' : 'bg-outline'"
              ></span>
              <span class="text-caption-2 font-caption-2" :class="enabled ? 'text-secondary' : 'text-outline'">
                {{ enabled ? '已启用过滤 · Filtering Active' : '过滤已关闭 · Filtering Off' }}
              </span>
            </div>
            <div class="flex items-center gap-2 pl-space-xs">
              <span class="text-caption-2 font-caption-2 text-on-surface-variant">主网关防护</span>
              <label class="relative inline-flex items-center cursor-pointer">
                <input
                  class="sr-only peer"
                  type="checkbox"
                  :checked="enabled"
                  :disabled="busy"
                  @change="emit('toggle-enabled', ($event.target as HTMLInputElement).checked)"
                />
                <div
                  class="w-11 h-6 bg-surface-container-highest rounded-full peer peer-checked:bg-secondary-container peer-checked:after:translate-x-full after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-5 after:w-5 after:transition-all peer-disabled:opacity-50 shadow-sm"
                ></div>
              </label>
            </div>
          </div>

          <div class="flex items-center gap-space-md">
            <button
              class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-surface-container hover:bg-surface-container-high text-subheadline font-subheadline text-on-surface transition-all"
              type="button"
              @click="emit('logout')"
            >
              <Icon name="logout" class="text-[16px]" />
              <span>退出</span>
            </button>
            <div class="w-8 h-8 rounded-full bg-primary flex items-center justify-center">
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
