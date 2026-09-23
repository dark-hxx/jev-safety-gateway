<script setup lang="ts">
import Icon from './Icon.vue'

/**
 * 「未接入」标注。用于原型中出现、但当前后端没有数据来源或接口的元素。
 *
 * 两种形态：
 * - `badge`：内联小标签，贴在保留原有版式的卡片角上。
 * - `placeholder`：占位块，保留原型中该区域的形状与尺寸，但不绘制任何无来源数值。
 *
 * 两者都**不显示任何数值**，只说明缺口与后续计划。
 */
withDefaults(
  defineProps<{
    variant?: 'badge' | 'placeholder'
    /** 占位块标题，默认「未接入」。 */
    title?: string
    /** 缺口说明：为什么没有数据、后续由谁补。 */
    reason?: string
  }>(),
  { variant: 'badge', title: '未接入', reason: '' },
)
</script>

<template>
  <span
    v-if="variant === 'badge'"
    class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-tertiary-container/20 text-tertiary text-code-badge font-code-badge whitespace-nowrap"
    :title="reason || '当前后端无对应数据来源，详见 docs/admin-console-backend-gaps.md'"
  >
    <Icon name="info" class="text-[11px]" />
    {{ title }}
  </span>

  <div
    v-else
    class="w-full h-full min-h-[6rem] flex flex-col items-center justify-center gap-1.5 rounded-xl border border-dashed border-outline-variant/60 bg-surface-container-low/40 p-space-sm text-center"
  >
    <Icon name="info" class="text-tertiary text-[20px]" />
    <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-tertiary-container/20 text-tertiary text-code-badge font-code-badge">
      {{ title }}
    </span>
    <span v-if="reason" class="text-caption-2 font-caption-2 text-outline max-w-md leading-relaxed">
      {{ reason }}
    </span>
  </div>
</template>
