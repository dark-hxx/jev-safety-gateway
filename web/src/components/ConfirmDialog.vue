<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import { clearConfirmOptions, confirmState, settleConfirm } from '../confirm'
import { t } from '../i18n'
import Icon from './Icon.vue'

/**
 * 确认弹框宿主。在 `App.vue` 挂载一次，由 `confirmDialog()`（`../confirm`）驱动。
 *
 * 用 Teleport 挂到 body：弹框要盖住侧栏与顶栏，留在任何页面容器里都会受限（同
 * `SelectMenu` / `HintTip` 的做法）。z 轴因此取全局最高层，压过详情抽屉（z-40）
 * 与下拉浮层（z-50）。
 *
 * 可访问性与键盘行为：
 * - `role="dialog"` + `aria-modal`，标题与正文用 aria-labelledby / aria-describedby 关联；
 * - 打开时把初始焦点放进弹框，关闭时还给打开它的那个元素，否则键盘用户会被丢回页面开头；
 * - Esc 取消，Tab 在弹框内循环，不跑到背后的页面上；
 * - 打开期间锁 body 滚动，否则遮罩上滚轮会带着页底一起滚。
 *
 * Esc / Tab 绑在弹框根节点而不是 window：事件从焦点处冒泡，先经过这里，
 * `stopPropagation` 就能挡住页面级监听（审计页的抽屉也在监听 Esc 关自己），
 * 不会一下关掉两层。
 */
const titleId = useId()
const messageId = useId()

const dialogEl = ref<HTMLElement | null>(null)

const options = computed(() => confirmState.options)
const danger = computed(() => options.value?.danger === true)

/** 弹框内的可聚焦元素（当前就是两个按钮）。 */
function focusables(): HTMLElement[] {
  const el = dialogEl.value
  if (!el) return []
  return Array.from(el.querySelectorAll<HTMLElement>('button'))
}

function cancelButton(): HTMLElement | null {
  return dialogEl.value?.querySelector<HTMLElement>('[data-role="cancel"]') ?? null
}

function onKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    settleConfirm(false)
    return
  }
  if (e.key !== 'Tab') return
  // 焦点被挡在外面时（例如脚本挪走了焦点），把 Tab 拉回弹框，而不是放它去遍历背后的页面。
  e.preventDefault()
  e.stopPropagation()
  const list = focusables()
  if (!list.length) return
  const i = list.indexOf(document.activeElement as HTMLElement)
  const next = e.shiftKey ? i - 1 : i + 1
  list[(next + list.length) % list.length]?.focus()
}

/** 打开前的焦点，关闭后还回去。 */
let restoreFocus: HTMLElement | null = null

function releaseScroll(): void {
  document.body.style.overflow = ''
}

watch(
  () => confirmState.open,
  async (open) => {
    if (open) {
      restoreFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
      document.body.style.overflow = 'hidden'
      await nextTick()
      // 危险操作的初始焦点落在「取消」：回车键不该顺手执行不可撤销的那一项。
      const list = focusables()
      ;(danger.value ? cancelButton() : list[list.length - 1])?.focus()
    } else {
      releaseScroll()
      restoreFocus?.focus()
      restoreFocus = null
    }
  },
)

onBeforeUnmount(releaseScroll)
</script>

<template>
  <Teleport to="body">
    <Transition
      enter-active-class="transition-opacity duration-150"
      enter-from-class="opacity-0"
      leave-active-class="transition-opacity duration-100"
      leave-to-class="opacity-0"
      @after-leave="clearConfirmOptions"
    >
      <div v-if="confirmState.open && options" class="fixed inset-0 z-[60] flex items-center justify-center p-4">
        <div class="absolute inset-0 bg-black/40" @click="settleConfirm(false)"></div>

        <div
          ref="dialogEl"
          role="dialog"
          aria-modal="true"
          :aria-labelledby="titleId"
          :aria-describedby="messageId"
          class="relative w-full max-w-sm rounded-2xl bg-surface-container-high border border-hairline shadow-overlay flex flex-col"
          @keydown="onKeydown"
        >
          <div class="flex items-start gap-space-sm px-space-md pt-space-md">
            <span
              class="shrink-0 h-7 w-7 rounded-full flex items-center justify-center"
              :class="danger ? 'bg-error-container text-error' : 'bg-primary-container text-on-primary-container'"
            >
              <Icon :name="danger ? 'alert-circle' : 'check-circle'" class="text-[16px]" />
            </span>
            <div class="flex flex-col gap-1 min-w-0 pt-0.5">
              <h2 :id="titleId" class="text-headline font-headline text-on-surface">{{ options.title }}</h2>
              <p :id="messageId" class="text-footnote font-footnote text-on-surface-variant break-words">
                {{ options.message }}
              </p>
            </div>
          </div>

          <div class="flex items-center justify-end gap-space-sm px-space-md py-space-md">
            <button
              data-role="cancel"
              type="button"
              class="px-3.5 py-2 rounded-xl bg-surface-container-highest hover:bg-surface-variant text-subheadline font-subheadline text-on-surface transition-colors"
              @click="settleConfirm(false)"
            >
              {{ options.cancelText || t('dialog.cancel') }}
            </button>
            <button
              type="button"
              class="px-3.5 py-2 rounded-xl transition-colors text-subheadline font-subheadline"
              :class="
                danger
                  ? 'bg-error hover:bg-error/90 text-on-error'
                  : 'bg-primary hover:bg-primary/90 text-on-primary'
              "
              @click="settleConfirm(true)"
            >
              {{ options.confirmText }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
