<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import Icon from './Icon.vue'

/**
 * 与控制台设计语言一致的下拉选择器。
 *
 * 原生 <select> 的展开列表由操作系统绘制，无法用 CSS 对齐控制台的 Cupertino/HIG
 * 视觉（圆角、发丝边、表面容器色、选中态高亮），因此这里以「按钮 + 浮层列表」自绘：
 * 闭合态复用输入控件的样式，展开的列表用 Teleport + position:fixed 渲染到 body，
 * 避免被卡片的 overflow 裁切（与 HintTip 同思路）。位置只在打开一刻计算，打开期间
 * 一旦滚动或改变窗口尺寸就直接关闭，而不是让浮层停在错位处。
 */
interface SelectOption {
  value: string
  label: string
}

const props = withDefaults(
  defineProps<{
    modelValue: string
    options: readonly SelectOption[]
    disabled?: boolean
    ariaLabel?: string
    /** 以等宽字体渲染标签（技术标识如模型名 / IP 用，对齐控制台其它 mono 文本）。 */
    mono?: boolean
  }>(),
  { disabled: false, ariaLabel: '', mono: false },
)

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const open = ref(false)
const anchor = ref<HTMLElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const activeIndex = ref(-1)
const listId = useId()

const selectedLabel = computed(
  () => props.options.find((o) => o.value === props.modelValue)?.label ?? '',
)

// 浮层的固定定位（打开时按锚点一次性计算）。
const GAP = 4
const left = ref(0)
const top = ref(0)
const width = ref(0)
const placement = ref<'bottom' | 'top'>('bottom')
function place(): void {
  const el = anchor.value
  if (!el) return
  const r = el.getBoundingClientRect()
  const rowHeight = 40
  const estimate = Math.min(props.options.length, 6) * rowHeight + 8
  const below = window.innerHeight - r.bottom
  placement.value = below < estimate && r.top > below ? 'top' : 'bottom'
  left.value = r.left
  width.value = r.width
  top.value = placement.value === 'bottom' ? r.bottom + GAP : r.top - GAP
}

const menuStyle = computed(() => ({
  left: `${left.value}px`,
  top: `${top.value}px`,
  width: `${width.value}px`,
  transform: placement.value === 'bottom' ? 'translateY(0)' : 'translateY(-100%)',
}))

function onViewportChange(): void {
  close()
}

function onDocPointer(e: MouseEvent): void {
  const target = e.target as Node
  if (anchor.value?.contains(target) || menu.value?.contains(target)) return
  close()
}

function focusTrigger(): void {
  anchor.value?.querySelector('button')?.focus()
}
async function openMenu(): Promise<void> {
  if (props.disabled) return
  place()
  open.value = true
  const i = props.options.findIndex((o) => o.value === props.modelValue)
  activeIndex.value = i >= 0 ? i : 0
  window.addEventListener('scroll', onViewportChange, true)
  window.addEventListener('resize', onViewportChange)
  document.addEventListener('mousedown', onDocPointer, true)
  await nextTick()
  menu.value?.focus()
}

function close(): void {
  if (!open.value) return
  open.value = false
  window.removeEventListener('scroll', onViewportChange, true)
  window.removeEventListener('resize', onViewportChange)
  document.removeEventListener('mousedown', onDocPointer, true)
}

function toggle(): void {
  if (open.value) close()
  else void openMenu()
}

function choose(value: string): void {
  emit('update:modelValue', value)
  close()
  focusTrigger()
}

function move(delta: number): void {
  const n = props.options.length
  if (n > 0) activeIndex.value = (activeIndex.value + delta + n) % n
}
function onListKeydown(e: KeyboardEvent): void {
  switch (e.key) {
    case 'ArrowDown':
      e.preventDefault()
      move(1)
      break
    case 'ArrowUp':
      e.preventDefault()
      move(-1)
      break
    case 'Home':
      e.preventDefault()
      activeIndex.value = 0
      break
    case 'End':
      e.preventDefault()
      activeIndex.value = props.options.length - 1
      break
    case 'Enter':
    case ' ': {
      e.preventDefault()
      const opt = props.options[activeIndex.value]
      if (opt) choose(opt.value)
      break
    }
    case 'Escape':
      e.preventDefault()
      close()
      focusTrigger()
      break
    case 'Tab':
      close()
      break
  }
}

function onTriggerKeydown(e: KeyboardEvent): void {
  if (open.value) return
  if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    void openMenu()
  }
}

// 选项集合变化时收起，避免高亮停留在越界索引上。
watch(() => props.options, close)

onBeforeUnmount(close)
</script>

<template>
  <div ref="anchor" class="relative">
    <button
      type="button"
      :disabled="disabled"
      :aria-label="ariaLabel || undefined"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="open ? listId : undefined"
      class="w-full flex items-center justify-between gap-2 pl-3 pr-2.5 py-2 rounded-xl bg-surface-container-high text-on-surface text-subheadline font-subheadline text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 hover:bg-surface-container-highest transition-colors shadow-inset disabled:cursor-not-allowed disabled:opacity-60"
      @click="toggle"
      @keydown="onTriggerKeydown"
    >
      <span class="truncate" :class="mono ? 'mono' : ''">{{ selectedLabel }}</span>
      <Icon
        name="chevron-down"
        class="shrink-0 text-outline text-[16px] transition-transform duration-150"
        :class="open ? 'rotate-180' : ''"
      />
    </button>
    <Teleport to="body">
      <ul
        v-if="open"
        :id="listId"
        ref="menu"
        role="listbox"
        tabindex="-1"
        class="fixed z-50 max-h-64 overflow-auto py-1 rounded-xl bg-surface-container-high border border-hairline shadow-lg focus:outline-none"
        :style="menuStyle"
        @keydown="onListKeydown"
      >
        <li
          v-for="(o, i) in options"
          :key="o.value"
          role="option"
          :aria-selected="o.value === modelValue"
          class="mx-1 px-2.5 py-2 rounded-lg flex items-center justify-between gap-2 cursor-pointer text-subheadline font-subheadline transition-colors"
          :class="[
            i === activeIndex ? 'bg-surface-container-highest' : '',
            o.value === modelValue ? 'text-primary' : 'text-on-surface',
          ]"
          @mouseenter="activeIndex = i"
          @click="choose(o.value)"
        >
          <span class="truncate" :class="mono ? 'mono' : ''">{{ o.label }}</span>
          <Icon v-if="o.value === modelValue" name="check-circle" class="shrink-0 text-[16px]" />
        </li>
      </ul>
    </Teleport>
  </div>
</template>
