<script setup lang="ts">
import { computed } from 'vue'
import { num } from '../../format'

/**
 * 「风险分值分布」直方图。
 *
 * 数据来自 `GET /api/stats` 新增的 `score_buckets`（0.2 步长五档，末档含 1.0）
 * 与 `unscored`（区间内已送检但没有分值的记录数）。统计口径是**全部已送检记录**：
 * 拦截的条件本身就是分值越过阈值，只看拦截记录会让分布退化到阈值一侧的单一档位。
 *
 * 只渲染后端计数：不估算、不外推，占比按「有分值样本量」就地相除得到。
 */
const props = defineProps<{
  /** 五档计数，`[0,0.2)` … `[0.8,1.0]`。 */
  counts: number[]
  /** 区间内已送检但没有分值的记录数。 */
  unscored: number
  /** 是否已经拿到过响应。 */
  loaded: boolean
}>()

/** 档位标签与配色：低分侧警示，高分侧安全。 */
const SLOTS = [
  { label: '0.0 – 0.2', hint: '最可疑', bar: 'bg-error', text: 'text-error' },
  { label: '0.2 – 0.4', hint: '', bar: 'bg-error/70', text: 'text-error' },
  { label: '0.4 – 0.6', hint: '', bar: 'bg-tertiary', text: 'text-tertiary' },
  { label: '0.6 – 0.8', hint: '', bar: 'bg-secondary/70', text: 'text-secondary' },
  { label: '0.8 – 1.0', hint: '最安全（含 1.0）', bar: 'bg-secondary', text: 'text-secondary' },
]

/** 有分值的样本量，即五档之和；占比的分母。 */
const sample = computed(() => props.counts.reduce((a, b) => a + (b || 0), 0))

const maxCount = computed(() => Math.max(1, ...props.counts.map((c) => c || 0)))

const rows = computed(() =>
  SLOTS.map((s, i) => {
    const count = props.counts[i] ?? 0
    return {
      ...s,
      count,
      width: (count / maxCount.value) * 100,
      share: sample.value > 0 ? `${((count / sample.value) * 100).toFixed(1)}%` : '-',
    }
  }),
)

/** 未获分值的记录占比：分母取「已送检记录总数」，与后端口径一致。 */
const evaluated = computed(() => sample.value + props.unscored)
const unscoredShare = computed(() =>
  evaluated.value > 0 ? `${((props.unscored / evaluated.value) * 100).toFixed(1)}%` : '-',
)
</script>

<template>
  <div class="flex-1 flex flex-col justify-center gap-space-sm my-space-sm">
    <div v-if="!loaded" class="h-40 flex items-center justify-center">
      <span class="text-caption-1 font-caption-1 text-outline">正在读取统计…</span>
    </div>

    <div v-else class="flex flex-col gap-space-sm">
      <div v-for="r in rows" :key="r.label" class="flex flex-col gap-1">
        <div class="flex items-center justify-between text-caption-2 font-caption-2">
          <div class="flex items-center gap-1.5 min-w-0">
            <span class="mono text-on-surface">{{ r.label }}</span>
            <span v-if="r.hint" class="text-outline truncate">{{ r.hint }}</span>
          </div>
          <div class="flex items-center gap-2 shrink-0">
            <span class="mono text-on-surface">{{ num(r.count) }}</span>
            <span class="mono text-outline w-14 text-right">{{ r.share }}</span>
          </div>
        </div>
        <div class="w-full bg-surface-container-highest rounded-full h-2 overflow-hidden">
          <div class="h-2 rounded-full transition-all" :class="r.bar" :style="{ width: `${r.width}%` }"></div>
        </div>
      </div>

      <div class="flex items-center justify-between text-caption-2 font-caption-2 pt-space-xs border-t border-hairline">
        <div class="flex items-center gap-1.5">
          <span class="w-2 h-2 rounded-full bg-outline"></span>
          <span class="text-on-surface-variant">无分值（JEV 未返回分值）</span>
        </div>
        <div class="flex items-center gap-2 shrink-0">
          <span class="mono text-on-surface-variant">{{ num(props.unscored) }}</span>
          <span class="mono text-outline w-14 text-right">{{ unscoredShare }}</span>
        </div>
      </div>

      <div class="text-caption-2 font-caption-2 text-outline leading-relaxed">
        样本量 {{ num(sample) }} 条有分值记录 · 统计口径为区间内全部已送检记录（含放行与拦截），非仅拦截记录
      </div>
    </div>
  </div>
</template>
