<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId } from 'vue'
import Icon from './Icon.vue'
import { useI18n } from '../i18n'

/**
 * 长说明的收纳抽屉。
 *
 * 页面上留一句短提示（防误用的那一句），把「为什么 / 会怎样」这类长句收进 ⓘ：
 * 默认视图安静，需要时悬停、聚焦或点一下就能读到全文，信息一条不少。
 *
 * 气泡用 `position: fixed` + `Teleport` 渲染到 `body`，因此不受祖先
 * `overflow: hidden`（横幅、卡片、侧栏滚动区）裁切，也不必参与任何 z-index 层叠。
 * 代价是位置只在打开那一刻算一次——所以打开期间一旦滚动或改变窗口尺寸就直接关闭，
 * 而不是让气泡停在错误的位置上。
 */
const props = withDefaults(
  defineProps<{
    /** 收进气泡的详细说明（调用方传入已翻译的文案）。 */
    text: string
    /** 无障碍名称；缺省为文案表里的「详细说明」。 */
    label?: string
    /** 气泡相对图标的方向；靠近视口边缘时组件会自动翻到另一侧。 */
    side?: 'top' | 'bottom'
  }>(),
  { side: 'top' },
)

const { t } = useI18n()

const open = ref(false)
const anchor = ref<HTMLElement | null>(null)
const tipId = useId()

/** 气泡固定宽度（px），用于水平夹紧与翻转判断。 */
const WIDTH = 288
const GAP = 8

const placement = ref<'top' | 'bottom'>('top')
const centerX = ref(0)
const edgeY = ref(0)

const ariaLabel = computed(() => props.label || t('hint.more'))

const style = computed(() => ({
  left: `${centerX.value}px`,
  top: `${edgeY.value}px`,
  width: `${WIDTH}px`,
  transform: placement.value === 'top' ? 'translate(-50%, -100%)' : 'translate(-50%, 0)',
}))

function place(): void {
  const el = anchor.value
  if (!el) return
  const r = el.getBoundingClientRect()
  // 先按声明方向摆，放不下就翻到另一侧；水平方向夹紧在视口内，气泡不会溢出。
  const room = 120
  let side = props.side
  if (side === 'top' && r.top < room) side = 'bottom'
  else if (side === 'bottom' && window.innerHeight - r.bottom < room) side = 'top'
  const half = WIDTH / 2
  placement.value = side
  centerX.value = Math.min(Math.max(r.left + r.width / 2, half + GAP), window.innerWidth - half - GAP)
  edgeY.value = side === 'top' ? r.top - GAP : r.bottom + GAP
}

function onViewportChange(): void {
  open.value = false
}

function show(): void {
  place()
  open.value = true
  window.addEventListener('scroll', onViewportChange, true)
  window.addEventListener('resize', onViewportChange)
}

function hide(): void {
  open.value = false
  window.removeEventListener('scroll', onViewportChange, true)
  window.removeEventListener('resize', onViewportChange)
}

function toggle(): void {
  if (open.value) hide()
  else show()
}

onBeforeUnmount(() => {
  window.removeEventListener('scroll', onViewportChange, true)
  window.removeEventListener('resize', onViewportChange)
})
</script>

<template>
  <span ref="anchor" class="inline-flex shrink-0 align-middle">
    <button
      type="button"
      class="inline-flex items-center justify-center w-4 h-4 rounded-full text-outline hover:text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 transition-colors"
      :aria-label="ariaLabel"
      :aria-describedby="open ? tipId : undefined"
      :aria-expanded="open"
      @mouseenter="show"
      @mouseleave="hide"
      @focus="show"
      @blur="hide"
      @click.prevent="toggle"
      @keydown.escape="hide"
    >
      <Icon name="info" class="text-[13px]" />
    </button>

    <Teleport to="body">
      <div
        v-if="open"
        :id="tipId"
        role="tooltip"
        class="fixed z-50 px-3 py-2 rounded-xl bg-inverse-surface text-inverse-on-surface text-caption-2 font-caption-2 leading-relaxed shadow-lg pointer-events-none"
        :style="style"
      >
        {{ text }}
      </div>
    </Teleport>
  </span>
</template>
