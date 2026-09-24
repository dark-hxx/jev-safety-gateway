<script setup lang="ts">
import { computed } from 'vue'
import { num } from '../format'
import { useI18n } from '../i18n'
import type { GeoBucket } from '../types'
import { WORLD_LAND_PATH_MAIN, WORLD_LAND_PATH_OCEANIA, WORLD_MAP_VIEWBOX } from './worldMapPaths'

/**
 * 全球威胁来源图：在离线内嵌的世界地图轮廓（CC0，见 worldMapPaths.ts）上，为每个
 * 有归属地的国家桶画一个标点（大小随事件量、颜色随拦截占比），再从各标点向中心的
 * 「安全网关」核心节点画一条流动动画弧线——语义是「这些真实来源都汇聚到本网关」。
 *
 * 诚实边界：每个标点都对应一个真实的国家聚合桶（ISO + 事件/拦截计数）；弧线表达的是
 * 「来源→网关」这一真实关系，流动只是视觉修饰，不代表国家间的真实链路。未收录坐标的
 * 国家不落点（仍在下方排行榜里），未知/内网（ISO 为空）也不落点。
 *
 * 中心节点只有在运维声明了本网关的部署坐标（gateway prop）时才绘制：网关自身的公网
 * 出口 IP 离线不可知（可能藏在 nginx 或 NAT 后），所以这个位置是声明的而非推断的。
 * 未声明时只画来源标点、不画弧线，也不把网关钉在一个编造的坐标上。
 */
const props = defineProps<{
  buckets: readonly GeoBucket[]
  /** 本网关的部署坐标（十进制度）；未声明时为 undefined/null。 */
  gateway?: { lat: number; lon: number } | null
}>()

const { t } = useI18n()

/**
 * ISO-3166-1 alpha-2 → 近似经纬度（国家质心，单位度）。覆盖真实流量最常见的来源国；
 * 未收录的国家不落点。这是静态参考数据，不随运行时变化。
 */
const COUNTRY_LONLAT: Record<string, [number, number]> = {
  US: [-98, 39], CA: [-106, 56], MX: [-102, 23], BR: [-51, -10], AR: [-64, -34],
  CL: [-71, -35], CO: [-73, 4], PE: [-75, -9],
  GB: [-2, 54], IE: [-8, 53], FR: [2, 46], DE: [10, 51], NL: [5.5, 52], BE: [4.5, 50.6],
  ES: [-3.7, 40], PT: [-8, 39.5], IT: [12.5, 42], CH: [8, 47], AT: [14, 47.5],
  SE: [15, 62], NO: [10, 62], FI: [26, 64], DK: [10, 56], PL: [19, 52], CZ: [15.5, 49.8],
  RO: [25, 46], UA: [31, 49], RU: [90, 61], TR: [35, 39],
  CN: [104, 35], HK: [114.1, 22.3], TW: [121, 23.7], JP: [138, 37], KR: [127.8, 36.5],
  IN: [79, 22], PK: [70, 30], BD: [90, 24], KZ: [68, 48],
  SG: [103.8, 1.35], MY: [102, 4], TH: [101, 15], VN: [106, 16], ID: [118, -2], PH: [122, 12],
  AU: [134, -25], NZ: [172, -42],
  IR: [53, 32], SA: [45, 24], AE: [54, 24], IL: [35, 31], EG: [30, 27], ZA: [24, -29], NG: [8, 10],
}

// --- 等距圆柱投影：把经纬度线性映射到源 SVG 的 494.7 x 265.7 画布坐标 ---
// 常量按内嵌地图（Wikimedia「Simple world map」）的实测陆地范围标定：主陆地包围盒
// x 17.4–478.3 对应经度 ±180（画布满宽即反子午线，西阿拉斯加 ≈ −167°、东西伯利亚 ≈ +168°
// 恰好落在两端）；y 11.9–246.0 对应纬度 +83（北格陵兰）至 −55（火地岛，主路径不含南极）。
// 纬度尺度比经度略大（该图非严格等比、竖向拉伸），标点因此贴合轮廓而非理想球面。
const PX_PER_LON = 1.374
const X_AT_LON0 = 247.35
const PX_PER_LAT = 1.696
const Y_AT_LAT0 = 152.7
function project(lon: number, lat: number): { x: number; y: number } {
  return { x: X_AT_LON0 + lon * PX_PER_LON, y: Y_AT_LAT0 - lat * PX_PER_LAT }
}

/**
 * 本网关部署坐标映射到画布上的核心节点；运维未声明坐标时为 null。
 * 投影与来源标点共用，所以中心节点和标点的相对位置始终是真实地理关系。
 */
const hub = computed<{ x: number; y: number } | null>(() => {
  const g = props.gateway
  if (!g || !Number.isFinite(g.lat) || !Number.isFinite(g.lon)) return null
  const { x, y } = project(g.lon, g.lat)
  return { x, y }
})

interface Marker {
  iso: string
  name: string
  x: number
  y: number
  r: number
  /** 实心内点半径。 */
  rInner: number
  total: number
  blocked: number
  /** 拦截占比 0–1，驱动颜色深浅。 */
  intensity: number
  /** 连向网关核心节点的弧线路径 d；未声明部署位置时为空串（不画）。 */
  arc: string
  /** 动画错峰用的延迟（秒）。 */
  delay: number
}

/** 标点半径范围（源画布单位）：最小可见，最大不过分抢占。 */
const R_MIN = 2.6
const R_MAX = 7

const maxTotal = computed(() =>
  Math.max(1, ...props.buckets.filter((b) => b.country).map((b) => b.total)),
)

/**
 * 可落点的国家桶 → 标点。仅保留 ISO 非空且在坐标表内的桶；事件量大的后画（叠在上层）。
 * 弧线用二次贝塞尔，从标点到网关核心节点，控制点在中点法线方向偏移，形成向上的弧；
 * 未声明部署坐标时 arc 为空串，模板据此跳过弧线。
 */
const markers = computed<Marker[]>(() => {
  const list: Marker[] = []
  const core = hub.value
  let i = 0
  for (const b of props.buckets) {
    const pos = b.country ? COUNTRY_LONLAT[b.country] : undefined
    if (!pos) continue
    const { x, y } = project(pos[0], pos[1])
    const scale = Math.sqrt(b.total / maxTotal.value)
    const intensity = b.total > 0 ? b.blocked / b.total : 0
    const r = R_MIN + (R_MAX - R_MIN) * scale
    list.push({
      iso: b.country,
      name: b.name || b.country,
      x,
      y,
      r,
      rInner: Math.max(1.4, r * 0.42),
      total: b.total,
      blocked: b.blocked,
      intensity,
      arc: core ? arcTo(x, y, core.x, core.y) : '',
      delay: (i++ % 6) * 0.5,
    })
  }
  // 大点后绘制，避免被小点压住。
  return list.sort((a, b) => a.total - b.total)
})

/** 二次贝塞尔弧：中点沿法线上抬，长弧抬得更高，短弧更平。 */
function arcTo(x1: number, y1: number, x2: number, y2: number): string {
  const mx = (x1 + x2) / 2
  const my = (y1 + y2) / 2
  const dx = x2 - x1
  const dy = y2 - y1
  const dist = Math.hypot(dx, dy) || 1
  const lift = Math.min(38, dist * 0.28)
  // 法线方向（-dy, dx) 归一化后取「上方」分量，让弧线整体上凸。
  const nx = -dy / dist
  const ny = dx / dist
  const sign = ny > 0 ? -1 : 1
  const cx = mx + nx * lift * sign
  const cy = my + ny * lift * sign
  return `M${x1.toFixed(1)} ${y1.toFixed(1)} Q${cx.toFixed(1)} ${cy.toFixed(1)} ${x2.toFixed(1)} ${y2.toFixed(1)}`
}

/** 顶栏「重点区域」摘要：取事件量最高、且已落点的国家桶。 */
const focus = computed(() => {
  const top = props.buckets.find((b) => b.country && COUNTRY_LONLAT[b.country])
  if (!top) return null
  return { name: top.name || top.country, hits: top.total }
})

/** 有归属但未收录坐标（含未知/内网）的桶数——用于「+N 未定位」提示，不落点但不隐藏。 */
const unplotted = computed(
  () => props.buckets.filter((b) => !b.country || !COUNTRY_LONLAT[b.country]).length,
)
</script>
<template>
  <div class="relative w-full">
    <div class="flex items-center justify-between gap-2 mb-2">
      <span class="inline-flex items-center gap-1.5 text-caption-2 font-caption-2 text-on-surface-variant">
        <span class="relative flex h-1.5 w-1.5">
          <span class="absolute inline-flex h-full w-full rounded-full bg-error opacity-60 animate-ping"></span>
          <span class="relative inline-flex h-1.5 w-1.5 rounded-full bg-error"></span>
        </span>
        {{ t('ipa.geo.hotspot') }}
      </span>
      <span v-if="focus" class="text-caption-2 font-caption-2 text-outline truncate">
        {{ t('ipa.geo.focus', { name: focus.name, n: num(focus.hits) }) }}
      </span>
    </div>
    <div class="relative rounded-xl overflow-hidden bg-surface-container-high/50 ring-1 ring-hairline">
      <svg :viewBox="WORLD_MAP_VIEWBOX" class="block w-full h-auto" role="img" :aria-label="t('ipa.geo.mapAria')">
        <g class="land">
          <path :d="WORLD_LAND_PATH_MAIN" />
          <path :d="WORLD_LAND_PATH_OCEANIA" />
        </g>
        <g v-if="hub" fill="none">
          <path
            v-for="m in markers"
            :key="'arc-' + m.iso"
            :d="m.arc"
            class="arc"
            :class="m.intensity >= 0.34 ? 'stroke-error' : 'stroke-primary'"
            :style="{ animationDelay: m.delay + 's' }"
          />
        </g>
        <g v-if="hub">
          <circle
            v-for="m in markers"
            :key="'trav-' + m.iso"
            r="1.4"
            class="travel"
            :class="m.intensity >= 0.34 ? 'fill-error' : 'fill-primary'"
          >
            <animateMotion :dur="3 + m.delay + 's'" repeatCount="indefinite" :path="m.arc" />
          </circle>
        </g>
        <g v-if="hub">
          <circle :cx="hub.x" :cy="hub.y" r="5" class="core-halo fill-secondary/20" />
          <circle :cx="hub.x" :cy="hub.y" r="2.4" class="fill-secondary" />
          <circle :cx="hub.x" :cy="hub.y" r="1" class="fill-on-secondary" />
        </g>
        <g>
          <template v-for="m in markers" :key="m.iso">
            <circle
              :cx="m.x"
              :cy="m.y"
              :r="m.r"
              class="ring"
              :class="m.intensity >= 0.34 ? 'stroke-error' : 'stroke-primary'"
              :style="{ animationDelay: m.delay + 's' }"
            />
            <circle
              :cx="m.x"
              :cy="m.y"
              :r="m.rInner"
              :class="m.intensity >= 0.34 ? 'fill-error' : 'fill-primary'"
            />
          </template>
        </g>
      </svg>
    </div>
    <div v-if="!hub" class="mt-1.5 text-caption-2 font-caption-2 text-outline">
      {{ t('ipa.geo.hubHint') }}
    </div>
    <div v-if="unplotted > 0" class="mt-1.5 text-caption-2 font-caption-2 text-outline">
      {{ t('ipa.geo.unplotted', { n: num(unplotted) }) }}
    </div>
  </div>
</template>

<style scoped>
.land path {
  fill: rgb(var(--c-outline) / 0.16);
  stroke: rgb(var(--c-outline) / 0.28);
  stroke-width: 0.3;
}
.arc {
  stroke-width: 0.8;
  stroke-linecap: round;
  stroke-dasharray: 3 6;
  opacity: 0.75;
  animation: arc-flow 3s linear infinite;
}
@keyframes arc-flow {
  to {
    stroke-dashoffset: -18;
  }
}
.ring {
  fill: none;
  stroke-width: 0.7;
  transform-box: fill-box;
  transform-origin: center;
  animation: ring-pulse 2.6s ease-out infinite;
}
@keyframes ring-pulse {
  0% {
    transform: scale(0.55);
    opacity: 0.85;
  }
  100% {
    transform: scale(2.3);
    opacity: 0;
  }
}
.core-halo {
  transform-box: fill-box;
  transform-origin: center;
  animation: ring-pulse 2.8s ease-out infinite;
}
.travel {
  opacity: 0.9;
}
@media (prefers-reduced-motion: reduce) {
  .arc,
  .ring,
  .core-halo {
    animation: none;
  }
  .arc {
    stroke-dasharray: none;
  }
}
</style>
