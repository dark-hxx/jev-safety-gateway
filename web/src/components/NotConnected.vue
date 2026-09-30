<script setup lang="ts">
import { computed } from 'vue'
import HintTip from './HintTip.vue'
import Icon from './Icon.vue'
import { useI18n } from '../i18n'

/**
 * 「未接入」标注。只用于**此时确实取不到数据**的元素：登录页还没拿到
 * `/api/version`、仪表盘区间内没有请求记录、未配置 GeoIP/ASN 库这类场景。
 *
 * 不用于「后端根本没有这个字段」——那类控件一律直接删掉，不留永久降级态：
 * 一个永远不会亮起来的徽标只是噪声，不构成任何信息。
 *
 * 两种形态：
 * - `badge`：内联小标签，贴在保留原有版式的卡片角上。
 * - `placeholder`：占位块，保留原型中该区域的形状与尺寸，但不绘制任何无来源数值。
 *
 * 两者都**不显示任何数值**，只说明缺口与后续计划。
 *
 * 缺口说明（`reason`）两种形态下一律走悬浮气泡，不占用页面正文：默认视图只留
 * 「未接入」这个短徽标，想知道「为什么没有数据」再悬停展开。
 *
 * `title` / `reason` 由调用方按当前界面语言传入（`t(...)`）；缺省时的兜底文案同样走文案表，
 * 因此这里不放任何字面量。
 */
const props = withDefaults(
  defineProps<{
    variant?: 'badge' | 'placeholder'
    /** 占位块标题；缺省为「未接入」。 */
    title?: string
    /** 缺口说明：为什么没有数据、后续由谁补。 */
    reason?: string
  }>(),
  { variant: 'badge' },
)

const { t } = useI18n()

const label = computed(() => props.title || t('notconnected.label'))
const tip = computed(() => props.reason || t('notconnected.tip'))
</script>

<template>
  <span
    v-if="variant === 'badge'"
    class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-tertiary-container/20 text-tertiary text-code-badge font-code-badge whitespace-nowrap"
    :title="tip"
  >
    <Icon name="info" class="text-[11px]" />
    {{ label }}
  </span>

  <div
    v-else
    class="w-full h-full min-h-[6rem] flex flex-col items-center justify-center gap-1.5 rounded-xl border border-dashed border-outline-variant/60 bg-surface-container-low/40 p-space-sm text-center"
  >
    <Icon name="info" class="text-tertiary text-[20px]" />
    <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-tertiary-container/20 text-tertiary text-code-badge font-code-badge">
      {{ label }}
    </span>
    <HintTip :text="tip" />
  </div>
</template>
