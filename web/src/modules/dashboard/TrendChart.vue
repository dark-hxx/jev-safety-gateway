<script setup lang="ts">
import { computed, ref } from 'vue'
import { num } from '../../format'
import { useI18n } from '../../i18n'
import type { StatBucket } from '../../types'

/**
 * 「流量与安全威胁态势」趋势图。
 *
 * 只渲染后端给出的分桶序列：`series` 已覆盖查询区间的每一个桶、空桶计数为 0，
 * 因此这里不做分桶、不插值、不补点，点位与桶的 `ts` 一一对应。
 *
 * 用内联 SVG 手绘（离线约束下不引入图表库）：坐标是 0–1000 / 0–100 的归一化空间，
 * 由 `preserveAspectRatio="none"` 拉伸到容器，线宽由 `vector-effect` 在缩放后保持恒定。
 */
const props = defineProps<{
  series: StatBucket[]
  /** 后端回传的分桶粒度（秒），决定横轴标签是否带日期。 */
  bucketSeconds: number
  /** 是否已经拿到过响应：false 时显示等待态，而不是画一个看起来像「0 流量」的坐标轴。 */
  loaded: boolean
}>()

const { t } = useI18n()

const H = 100
const W = 1000
/** 顶部留白，避免峰值线贴边。 */
const PAD_TOP = 6

/** 纵轴上界：取三序列最大值，全 0 时退化为 1 以免除零。 */
const maxY = computed(() => {
  let m = 0
  for (const b of props.series) m = Math.max(m, b.total, b.allowed, b.blocked)
  return m > 0 ? m : 1
})

const hasData = computed(() => props.series.some((b) => b.total > 0 || b.allowed > 0 || b.blocked > 0))

/** 第 i 个桶的 x 坐标（0–1000）。只有一个桶时置于中间。 */
function xAt(i: number): number {
  const n = props.series.length
  if (n <= 1) return W / 2
  return (i / (n - 1)) * W
}

function yAt(v: number): number {
  return PAD_TOP + (1 - v / maxY.value) * (H - PAD_TOP)
}

function line(key: 'total' | 'allowed' | 'blocked'): string {
  return props.series.map((b, i) => `${xAt(i)},${yAt(b[key])}`).join(' ')
}

/** 入站总量填充到基线（`total` 恒为三者的上包络）。 */
function area(): string {
  if (!props.series.length) return ''
  const pts = props.series.map((b, i) => `${xAt(i)},${yAt(b.total)}`)
  return `0,${H} ${pts.join(' ')} ${W},${H}`
}

/** 纵轴刻度：0 / 半高 / 峰值。 */
const yTicks = computed(() => {
  const m = maxY.value
  return [
    { key: 'max', label: num(m), y: yAt(m) },
    { key: 'half', label: num(m / 2), y: yAt(m / 2) },
    { key: 'zero', label: '0', y: yAt(0) },
  ]
})

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

/** 横轴标签：桶粒度达到小时量级时带上日期，否则只给时分。 */
function axisLabel(ts: number): string {
  const d = new Date(ts)
  const hm = `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
  return props.bucketSeconds >= 3600 ? `${d.getMonth() + 1}/${d.getDate()} ${hm}` : hm
}

/** 横轴刻度：最多 5 个，取自真实桶的 `ts`。 */
const xTicks = computed(() => {
  const n = props.series.length
  if (n === 0) return []
  const count = Math.min(5, n)
  const out: { key: number; label: string; x: number }[] = []
  for (let k = 0; k < count; k++) {
    const i = count === 1 ? 0 : Math.round((k / (count - 1)) * (n - 1))
    out.push({ key: i, label: axisLabel(props.series[i].ts), x: (xAt(i) / W) * 100 })
  }
  return out
})

// --- 悬停读数：按鼠标位置落到最近的桶，显示该桶的真实计数 ---

const hovered = ref<number | null>(null)

function onMove(e: MouseEvent): void {
  const n = props.series.length
  if (n === 0) return
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  if (rect.width === 0) return
  const ratio = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width))
  hovered.value = n === 1 ? 0 : Math.round(ratio * (n - 1))
}

const hoverBucket = computed(() => (hovered.value === null ? null : props.series[hovered.value]))
/** 读数框的水平位置（%），夹在两端内以免溢出卡片。 */
const hoverLeft = computed(() =>
  hovered.value === null ? 0 : Math.min(88, Math.max(12, (xAt(hovered.value) / W) * 100)),
)
</script>

<template>
  <div class="relative w-full h-72" @mousemove="onMove" @mouseleave="hovered = null">
    <!-- 等待态：没有响应就不画坐标轴，避免看起来像「0 流量」 -->
    <div v-if="!loaded" class="w-full h-full flex items-center justify-center rounded-xl bg-surface-container-low/80">
      <span class="text-caption-1 font-caption-1 text-outline">{{ t('common.loadingStats') }}</span>
    </div>

    <div v-else class="w-full h-full rounded-xl bg-surface-container-low/80 px-space-sm py-space-xs flex flex-col">
      <div class="flex-1 flex gap-space-xs min-h-0">
        <!-- 纵轴刻度 -->
        <div class="relative w-10 shrink-0 mono text-code-badge font-code-badge text-outline">
          <span
            v-for="t in yTicks"
            :key="t.key"
            class="absolute right-0 -translate-y-1/2 leading-none"
            :style="{ top: `${(t.y / H) * 100}%` }"
          >
            {{ t.label }}
          </span>
        </div>

        <div class="relative flex-1 min-w-0">
          <svg
            class="w-full h-full"
            :viewBox="`0 0 ${W} ${H}`"
            preserveAspectRatio="none"
            role="img"
            :aria-label="t('chart.aria')"
          >
            <line
              v-for="t in yTicks"
              :key="'grid-' + t.key"
              x1="0"
              :y1="t.y"
              :x2="W"
              :y2="t.y"
              class="stroke-surface-container-highest"
              stroke-width="0.5"
              stroke-dasharray="3 3"
              vector-effect="non-scaling-stroke"
            />
            <polygon v-if="series.length" :points="area()" class="fill-primary/10" stroke="none" />
            <polyline
              v-if="series.length"
              :points="line('total')"
              class="stroke-primary"
              fill="none"
              stroke-width="2"
              stroke-linejoin="round"
              vector-effect="non-scaling-stroke"
            />
            <polyline
              v-if="series.length"
              :points="line('allowed')"
              class="stroke-secondary"
              fill="none"
              stroke-width="1.5"
              stroke-linejoin="round"
              vector-effect="non-scaling-stroke"
            />
            <polyline
              v-if="series.length"
              :points="line('blocked')"
              class="stroke-error"
              fill="none"
              stroke-width="1.5"
              stroke-linejoin="round"
              vector-effect="non-scaling-stroke"
            />
            <line
              v-if="hovered !== null"
              :x1="xAt(hovered)"
              y1="0"
              :x2="xAt(hovered)"
              :y2="H"
              class="stroke-on-surface-variant"
              stroke-width="1"
              vector-effect="non-scaling-stroke"
            />
          </svg>

          <!-- 全 0 时明确说明，而不是让用户对着贴底的直线猜 -->
          <div v-if="!hasData" class="absolute inset-0 flex items-center justify-center pointer-events-none">
            <span class="px-2 py-0.5 rounded-full bg-surface-container-high text-caption-2 font-caption-2 text-on-surface-variant">
              {{ t('chart.empty') }}
            </span>
          </div>

          <!-- 悬停读数 -->
          <div
            v-if="hoverBucket"
            class="absolute top-1 -translate-x-1/2 pointer-events-none rounded-lg bg-surface-bright/95 backdrop-blur shadow-lg border border-hairline px-2 py-1.5 flex flex-col gap-0.5 whitespace-nowrap z-10"
            :style="{ left: `${hoverLeft}%` }"
          >
            <span class="text-caption-2 font-caption-2 text-outline mono">{{ new Date(hoverBucket.ts).toLocaleString() }}</span>
            <span class="text-caption-2 font-caption-2 text-on-surface mono">{{ t('chart.hoverIn', { n: num(hoverBucket.total) }) }}</span>
            <span class="text-caption-2 font-caption-2 text-secondary mono">{{ t('chart.hoverAllowed', { n: num(hoverBucket.allowed) }) }}</span>
            <span class="text-caption-2 font-caption-2 text-error mono">{{ t('chart.hoverBlocked', { n: num(hoverBucket.blocked) }) }}</span>
          </div>
        </div>
      </div>

      <!-- 横轴刻度：位置来自真实桶的 ts -->
      <div class="relative h-4 mt-space-xs ml-11">
        <span
          v-for="t in xTicks"
          :key="'x-' + t.key"
          class="absolute -translate-x-1/2 mono text-code-badge font-code-badge text-outline leading-none"
          :style="{ left: `${t.x}%` }"
        >
          {{ t.label }}
        </span>
      </div>
    </div>
  </div>
</template>
