<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '../../components/Icon.vue'
import HintTip from '../../components/HintTip.vue'
import NotConnected from '../../components/NotConnected.vue'
import * as api from '../../api'
import { useConsole } from '../../console'
import { deltaPctText, num, pctText } from '../../format'
import { useI18n } from '../../i18n'
import type { MessageKey } from '../../i18n/zh'
import type { Stats, StatsResponse } from '../../types'
import TrendChart from './TrendChart.vue'
import ScoreDistribution from './ScoreDistribution.vue'

/**
 * 运行概览（驾驶舱）。
 *
 * 真实数据来源：`GET /api/stats?hours=`（区间总量 + 分桶序列 + 分值直方图）、
 * `GET /api/stats/latency?hours=`（单请求总耗时的分位数）与 `GET /api/state`
 * 的 `stats24h`（首屏快照）。
 *
 * 两张图都只渲染后端给出的数字：趋势图按响应的 `bucket_seconds`/`series` 画线，
 * 分桶与空桶补零都在后端完成；分值分布直接用 `score_buckets`/`unscored`。
 * 前端不自行分桶、不插值、不估算占比。
 *
 * 环比与峰值都由 `/api/stats` 一起给出：`previous` 是紧邻本区间之前的等长区间，
 * `peak_pps` 是区间内最繁忙的单个自然秒。两者都由后端算好，前端只做差与渲染。
 */

const { settings, keys, stats24h } = useConsole()
const router = useRouter()
const { t } = useI18n()

/** 时间范围分段控件：与 `/api/stats?hours=` 的 `hours` 参数一一对应。 */
const RANGES: { label: string; hours: number; labelKey: MessageKey }[] = [
  { label: '1h', hours: 1, labelKey: 'range.1h' },
  { label: '6h', hours: 6, labelKey: 'range.6h' },
  { label: '24h', hours: 24, labelKey: 'range.24h' },
  { label: '7d', hours: 168, labelKey: 'range.7d' },
]

const rangeIdx = ref(2)
const range = computed(() => RANGES[rangeIdx.value])
/** 当前范围的界面语言名称；分段控件本身显示 `1h/6h/24h/7d` 这种机器刻度，保持可对照。 */
const rangeText = computed(() => t(range.value.labelKey))

/** 首屏用 `/api/state` 已带回的 24h 快照，避免白屏；切换范围后由 `/api/stats` 更新。 */
const stats = ref<Stats>({ ...stats24h.value })
/** 图表数据；在首次 `/api/stats` 返回前为空，图表显示等待态而非伪造曲线。 */
const series = ref<StatsResponse['series']>([])
const scoreBuckets = ref<number[]>([])
const unscored = ref(0)
const bucketSeconds = ref(0)
/** 区间内最繁忙单秒的请求数；`/api/stats` 返回前为 0，此时不显示数值。 */
const peakPPS = ref(0)
/** 紧邻本区间之前的等长区间，用于环比；未取到数据时为 null。 */
const previous = ref<Stats | null>(null)
/** 延迟分位数；取不到时为 null，卡片回退降级态。 */
const latency = ref<api.LatencyStats | null>(null)
/**
 * 延迟接口的错误原文。**必须与「区间内没有记录」分开**：两者都让 `latency`
 * 为 null，但一个是「确实没有请求可统计」，另一个是接口失败（token 过期、
 * 500、网络中断）。早先的 `catch { latency.value = null }` 把后者静默伪装成
 * 前者——接口挂了看起来和没流量一模一样，而这恰恰是最需要立刻看见的情况。
 */
const latencyError = ref('')
const loading = ref(false)
const loaded = ref(false)
const loadError = ref('')
const refreshedAt = ref<Date | null>(null)

async function loadStats(): Promise<void> {
  loading.value = true
  loadError.value = ''
  // 延迟独立取：它失败只该让那一张卡片降级，不该把整个看板拖成错误态。
  void loadLatency()
  try {
    const r = await api.getStats(range.value.hours)
    stats.value = r
    series.value = r.series ?? []
    scoreBuckets.value = r.score_buckets ?? []
    unscored.value = r.unscored ?? 0
    bucketSeconds.value = r.bucket_seconds ?? 0
    peakPPS.value = r.peak_pps ?? 0
    previous.value = r.previous ?? null
    loaded.value = true
    refreshedAt.value = new Date()
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

async function loadLatency(): Promise<void> {
  try {
    latency.value = await api.getLatency(range.value.hours)
    latencyError.value = ''
  } catch (e) {
    latency.value = null
    latencyError.value = e instanceof Error ? e.message : String(e)
  }
}

/** 分桶粒度的说明，来自后端回传的 `bucket_seconds`。 */
const bucketLabel = computed(() => {
  const s = bucketSeconds.value
  if (!s) return ''
  if (s % 86400 === 0) return t('duration.days', { n: s / 86400 })
  if (s % 3600 === 0) return t('duration.hours', { n: s / 3600 })
  if (s % 60 === 0) return t('duration.minutes', { n: s / 60 })
  return t('duration.seconds', { n: s })
})

function selectRange(i: number): void {
  if (i === rangeIdx.value) return
  rangeIdx.value = i
  void loadStats()
}

/**
 * 「刷新周期 5s」：原型标注的实时遥测节奏，这里落地为真实的 5 秒轮询
 * `/api/stats`（仅查询，不改任何配置）。
 */
let timer: number | undefined
onMounted(() => {
  if (stats24h.value) stats.value = { ...stats24h.value }
  // 首屏立即取一次：`stats24h` 快照里没有分桶序列与延迟分位，若等到第一次轮询
  // （5 秒后）才取，图表和 P99 卡片会先空置一拍。
  void loadStats()
  timer = window.setInterval(() => void loadStats(), 5000)
})
onBeforeUnmount(() => {
  if (timer !== undefined) window.clearInterval(timer)
})

const keysEnabled = computed(() => keys.value.filter((k) => k.enabled).length)

const allowedRate = computed(() => pctText(stats.value.allowed, stats.value.total))
const blockedShare = computed(() => pctText(stats.value.blocked, stats.value.total))
const skippedShare = computed(() => pctText(stats.value.skipped, stats.value.total))
const errorShare = computed(() => pctText(stats.value.errors, stats.value.total))

const adminHost = computed(() => (typeof location !== 'undefined' ? location.host : ''))

/**
 * 总请求量的环比。`null` 表示不可比：首屏快照阶段还没拿到 `previous`，或上期
 * 本就没有记录（此时「涨幅」没有分母，硬算出来的百分比只会是噪声）。
 */
const totalDelta = computed(() => {
  const prev = previous.value
  if (!prev) return null
  return deltaPctText(stats.value.total, prev.total)
})

const totalDeltaUp = computed(() => (totalDelta.value ?? '').startsWith('+'))

/** P99 卡片副信息：给出中位数与样本量，让 P99 有个参照。 */
const latencyDetail = computed(() => {
  const l = latency.value
  if (!l || l.count === 0) return ''
  return t('dash.latencyDetail', { p50: l.p50, p95: l.p95, count: num(l.count) })
})

/**
 * 卡片底注。三种取不到数的情形各说各的：接口失败、区间内没有记录、取样近似。
 * 旧文案对前两种统一说「延迟分布未接入」——那是后端还没有这个接口时的措辞，
 * 接口早已存在，再用它就把「没流量」和「接口挂了」混成了一句话。
 */
const latencyCaption = computed(() => {
  if (latencyError.value) return t('dash.latencyFailedCaption')
  const l = latency.value
  if (!l || l.count === 0) return t('dash.latencyNoRecords')
  return l.sampled ? t('dash.latencySampled') : t('dash.latencyFull')
})
</script>

<template>
  <div class="w-full px-margin-mobile py-margin-mobile md:px-margin md:py-margin flex flex-col gap-space-lg">
    <!-- 环境辉光（纯装饰，无数据含义） -->
    <div class="relative w-full">
      <div class="absolute -top-12 -left-20 w-96 h-96 bg-primary/10 rounded-full blur-3xl pointer-events-none -z-10"></div>
      <div class="absolute top-20 right-0 w-80 h-80 bg-secondary/5 rounded-full blur-3xl pointer-events-none -z-10"></div>
    </div>

    <!-- 1. 驾驶舱横幅与实时遥测 -->
    <section class="relative overflow-hidden rounded-2xl bg-surface-container/90 backdrop-blur-2xl p-space-lg shadow-xl border border-hairline">
      <div class="flex flex-col lg:flex-row lg:items-center justify-between gap-space-md">
        <div class="flex items-start sm:items-center gap-space-md">
          <div class="relative flex items-center justify-center w-14 h-14 rounded-2xl bg-surface-bright shadow-inner">
            <Icon name="shield-lock" class="text-primary text-[32px]" />
            <span v-if="settings.enabled" class="absolute -top-1 -right-1 flex h-3.5 w-3.5">
              <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-secondary opacity-75"></span>
              <span class="relative inline-flex rounded-full h-3.5 w-3.5 bg-secondary"></span>
            </span>
          </div>
          <div class="flex flex-col">
            <div class="flex items-center gap-space-xs flex-wrap">
              <h1 class="text-title-1 font-title-1 text-on-surface tracking-tight">{{ t('dash.title') }}</h1>
              <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-secondary/15 text-secondary">
                {{ t('dash.engineChip', { model: settings.jev_model || t('shell.notSet') }) }}
              </span>
              <span class="inline-flex items-center px-2 py-0.5 rounded-full text-caption-2 font-caption-2 bg-surface-bright text-on-surface-variant">
                {{ t('dash.adminHost', { host: adminHost }) }}
              </span>
            </div>
          </div>
        </div>

        <!-- 实时指标带。原型的第四格是「集群实例」，本网关按设计单进程单实例，
             没有节点名册可显示，因此整格移除而不是留一个永久降级态。 -->
        <div class="grid grid-cols-2 sm:grid-cols-3 gap-space-sm bg-surface-container-high/60 p-space-sm rounded-xl">
          <div class="flex flex-col px-space-xs gap-0.5">
            <span class="eyebrow">{{ t('dash.forwarded', { range: rangeText }) }}</span>
            <span class="text-headline font-headline text-on-surface mono">{{ num(stats.total) }}</span>
          </div>
          <div class="flex flex-col px-space-xs gap-0.5">
            <span class="eyebrow">{{ t('dash.peakPps') }}</span>
            <!-- `peak_pps` 是区间内最繁忙的单个自然秒的请求数，与「转送量」的区间
                 总量是两个量纲：前者是瞬时并发能力，后者是累计。 -->
            <span v-if="loaded" class="text-headline font-headline text-on-surface mono">
              {{ num(peakPPS) }}<span class="text-caption-2 font-caption-2 text-on-surface-variant ml-1">req/s</span>
            </span>
            <span v-else class="text-headline font-headline text-outline mono">—</span>
            <span class="text-caption-2 font-caption-2 text-outline">{{ t('dash.peakPpsSub') }}</span>
          </div>
          <div class="flex flex-col px-space-xs gap-0.5">
            <span class="eyebrow">{{ t('dash.gwState') }}</span>
            <div class="flex items-center gap-1.5 mt-1">
              <span class="w-2 h-2 rounded-full" :class="settings.enabled ? 'bg-secondary' : 'bg-outline'"></span>
              <span class="text-caption-1 font-caption-1 font-semibold" :class="settings.enabled ? 'text-secondary' : 'text-outline'">
                {{ settings.enabled ? t('dash.active') : t('dash.disabled') }}
              </span>
            </div>
            <span class="text-caption-2 font-caption-2 text-outline">
              {{ settings.fail_open ? t('dash.failOpenOn') : t('dash.failCloseOn') }}
            </span>
          </div>
        </div>
      </div>
    </section>

    <!-- 2. 关键指标卡 -->
    <section class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-space-md">
      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('dash.card.total', { range: rangeText }) }}</span>
          <Icon name="swap-horiz" class="text-primary text-[18px]" />
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-on-surface tracking-tight mono">{{ num(stats.total) }}</div>
          <!-- 环比：与紧邻的上一等长区间比。上期没有记录时不显示百分比——
               「从 0 增长到 N」没有可解释的涨幅，显示「—」比显示 100% 诚实。 -->
          <div v-if="totalDelta" class="mt-0.5 flex items-center gap-1 text-caption-2 font-caption-2 text-on-surface-variant">
            <Icon :name="totalDeltaUp ? 'trending-up' : 'trending-down'" class="text-[14px]" />
            <span class="mono">{{ t('dash.card.totalDelta', { delta: totalDelta }) }}</span>
          </div>
          <div v-else-if="loaded" class="mt-0.5 text-caption-2 font-caption-2 text-outline">{{ t('dash.card.totalNoBase') }}</div>
        </div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('dash.card.allowed') }}</span>
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-secondary/15 text-secondary">
            {{ t('dash.tag.normal') }}
          </span>
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-on-surface tracking-tight mono">{{ allowedRate }}</div>
          <div class="text-caption-2 font-caption-2 text-outline mt-0.5">
            {{ t('dash.card.allowedCaption', { n: num(stats.allowed) }) }}
          </div>
        </div>
        <div class="w-full bg-surface-container-highest rounded-full h-1.5 overflow-hidden">
          <div class="bg-secondary h-1.5 rounded-full" :style="{ width: allowedRate === '-' ? '0%' : allowedRate }"></div>
        </div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('dash.card.blocked') }}</span>
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-error-container text-error">
            {{ t('dash.tag.blocked') }}
          </span>
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-error tracking-tight mono">{{ num(stats.blocked) }}</div>
          <div class="text-caption-2 font-caption-2 text-error/80 mt-0.5">
            {{ t('dash.card.blockedCaption', {
              share: blockedShare,
              rule: settings.block_if_below ? t('dash.ruleBelow') : t('dash.ruleAbove'),
            }) }}
          </div>
        </div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('dash.card.skipped') }}</span>
          <Icon name="alt-route" class="text-outline text-[18px]" />
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-on-surface tracking-tight mono">{{ num(stats.skipped) }}</div>
          <div class="text-caption-2 font-caption-2 text-outline mt-0.5">
            {{ t('dash.card.skippedCaption', { share: skippedShare }) }}
          </div>
        </div>
        <div class="w-full bg-surface-container-highest rounded-full h-1.5 overflow-hidden">
          <div class="bg-outline h-1.5 rounded-full" :style="{ width: skippedShare === '-' ? '0%' : skippedShare }"></div>
        </div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('dash.card.errors') }}</span>
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-tertiary/15 text-tertiary">
            {{ t('dash.tag.failOpen') }}
          </span>
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-tertiary tracking-tight mono">{{ num(stats.errors) }}</div>
          <div class="text-caption-2 font-caption-2 text-on-surface-variant mt-0.5">
            {{ t('dash.card.errorsCaption', { share: errorShare }) }}
          </div>
        </div>
        <div class="w-full bg-surface-container-highest rounded-full h-1.5 overflow-hidden">
          <div class="bg-tertiary h-1.5 rounded-full" :style="{ width: errorShare === '-' ? '0%' : errorShare }"></div>
        </div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <!-- 口径是整条请求的总耗时（检定阶段 + 转发阶段），与审计页「总耗时」一致。
               阶段已拆分到每条记录上（审计抽屉里可见），但这张卡片回答的是「端到端
               有多慢」，所以仍取总数而不是其中一段。 -->
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('dash.card.latency') }}</span>
          <Icon name="gauge" class="text-primary text-[18px]" />
        </div>
        <div class="my-space-sm">
          <!-- 三种状态互斥：接口失败 → 失败态（可重试）；有记录 → 数值；否则「无记录」。
               失败态与「无记录」必须长成两个样子，否则接口挂了会被读成没流量。 -->
          <div v-if="latencyError" class="flex items-center gap-1.5 text-caption-1 font-caption-1 text-error">
            <Icon name="alert-circle" class="text-[16px] shrink-0" />
            <span class="break-all">{{ t('dash.latencyFailed') }}</span>
            <button
              type="button"
              class="shrink-0 px-2 py-0.5 rounded-md bg-error-container/40 hover:bg-error-container/60 text-caption-2 font-caption-2 transition-colors"
              @click="loadLatency"
            >
              {{ t('dash.latencyRetry') }}
            </button>
          </div>
          <template v-else-if="latency && latency.count > 0">
            <div class="text-title-2 font-title-2 text-primary tracking-tight mono">
              {{ latency.p99 }}<span class="text-caption-1 font-caption-1 text-on-surface-variant ml-1">ms</span>
            </div>
            <div class="text-caption-2 font-caption-2 text-on-surface-variant mt-0.5">{{ latencyDetail }}</div>
          </template>
          <NotConnected v-else :title="t('dash.latencyNoRecordsTitle')" :reason="t('dash.card.latencyReason')" />
        </div>
        <div class="flex items-center gap-1.5 text-caption-2 font-caption-2 text-outline">
          <span>{{ latencyCaption }}</span>
          <HintTip v-if="latency?.sampled" :text="t('dash.latencySampledTip')" />
          <HintTip v-else-if="latencyError" :text="latencyError" />
        </div>
      </div>
    </section>

    <!-- 3. 趋势与威胁分类 -->
    <section class="grid grid-cols-1 xl:grid-cols-12 gap-space-lg">
      <div class="xl:col-span-7 flex flex-col p-space-lg rounded-2xl bg-surface-container shadow-lg border border-hairline">
        <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-space-sm mb-space-md">
          <div class="flex flex-col">
            <span class="text-title-3 font-title-3 text-on-surface">{{ t('dash.trend', { range: rangeText }) }}</span>
            <span class="text-caption-1 font-caption-1 text-outline flex items-center gap-1.5">
              <span class="h-1.5 w-1.5 rounded-full bg-secondary animate-pulse"></span>
              {{ t('dash.refreshCycle', { time: refreshedAt ? refreshedAt.toLocaleTimeString() : '—' }) }}
            </span>
          </div>
          <div class="inline-flex p-0.5 bg-surface-container-high rounded-lg self-start">
            <button
              v-for="(r, i) in RANGES"
              :key="r.label"
              type="button"
              class="px-3 py-1 rounded-md text-caption-2 font-caption-2 transition-all"
              :class="i === rangeIdx ? 'bg-surface-bright text-on-surface shadow-sm font-semibold' : 'text-on-surface-variant hover:text-on-surface'"
              @click="selectRange(i)"
            >
              {{ r.label }}
            </button>
          </div>
        </div>

        <div class="flex items-center gap-space-md mb-space-sm text-caption-2 font-caption-2 flex-wrap">
          <div class="flex items-center gap-1.5">
            <span class="w-2.5 h-2.5 rounded-full bg-primary"></span>
            <span class="text-on-surface-variant">{{ t('dash.legend.total') }}</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="w-2.5 h-2.5 rounded-full bg-secondary"></span>
            <span class="text-on-surface-variant">{{ t('dash.legend.allowed') }}</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="w-2.5 h-2.5 rounded-full bg-error"></span>
            <span class="text-on-surface-variant">{{ t('dash.legend.blocked') }}</span>
          </div>
        </div>

        <TrendChart :series="series" :bucket-seconds="bucketSeconds" :loaded="loaded" />

        <!-- 底注行：左侧是区间小结、右侧是接口字面量，实测这一行在常见窗口宽度下**已经排满**
             （可用宽度只比内容多几十像素）。因此刷新指示不能是「按需插入的文字」：临时多出一个
             「（刷新中…）」会把小结挤到第二行——卡片瞬时撑高 18px，整个 grid 行与下方内容
             下沉再弹回，就是那下「抽搐」（间隔 5s，正是轮询周期）。

             这里换成**常驻占位**的小转圈：它一直在版面上（空闲时只是 `invisible`），宽度与
             是否在刷新无关，所以刷新前后这一行的折行点、行高完全一致；说明文字留给读屏
             （`sr-only` 脱离文档流，不占版面）。不采用「宁可折行也不省略」的截断方案——
             那会在英文字符更长、窗口较窄时把「拦截 N 次」截掉。 -->
        <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-space-xs pt-space-md text-caption-1 font-caption-1 text-on-surface-variant">
          <div class="flex items-center gap-1.5" :class="loadError ? 'text-error' : ''">
            <Icon :name="loadError ? 'alert-circle' : 'check-circle'" class="text-[16px]" :class="loadError ? 'text-error' : 'text-secondary'" />
            <span v-if="loadError">{{ t('dash.trendError', { msg: loadError }) }}</span>
            <span v-else>
              {{ t('dash.trendSummary', { range: rangeText, allowed: num(stats.allowed), blocked: num(stats.blocked) }) }}
            </span>
            <span class="shrink-0 inline-flex items-center">
              <Icon name="loader" class="text-[14px] text-outline" :class="loading ? 'animate-spin' : 'invisible'" />
              <span v-if="loading" class="sr-only">{{ t('dash.refreshing') }}</span>
            </span>
          </div>
          <span class="font-code-body text-code-body text-outline mono">
            GET /api/stats?hours={{ range.hours }}
            <template v-if="bucketLabel"> · {{ t('dash.bucketPer', { label: bucketLabel }) }}</template>
            <template v-if="series.length"> · {{ t('dash.bucketCount', { n: series.length }) }}</template>
          </span>
        </div>
      </div>

      <div class="xl:col-span-5 flex flex-col p-space-lg rounded-2xl bg-surface-container shadow-lg border border-hairline">
        <div class="flex items-center justify-between mb-space-sm">
          <div class="flex flex-col">
            <span class="text-title-3 font-title-3 text-on-surface">{{ t('dash.scoreTitle') }}</span>
            <span class="text-caption-1 font-caption-1 text-outline">
              {{ t('dash.scoreSub', { range: rangeText }) }}
            </span>
          </div>
          <Icon name="donut" class="text-outline text-[20px]" />
        </div>
        <ScoreDistribution :counts="scoreBuckets" :unscored="unscored" :loaded="loaded" />
        <div class="flex items-center justify-end pt-space-xs">
          <button
            type="button"
            class="inline-flex items-center gap-1 text-caption-1 font-caption-1 text-primary hover:underline"
            @click="router.push({ name: 'audit' })"
          >
            {{ t('dash.viewAudit') }}
            <Icon name="chevron-right" class="text-[14px]" />
          </button>
        </div>
      </div>
    </section>

    <!-- 4. 运行实例与拓扑 -->
    <section class="flex flex-col gap-space-md">
      <div class="flex items-center justify-between flex-wrap gap-space-xs">
        <div class="flex items-center gap-2">
          <span class="text-title-3 font-title-3 text-on-surface">{{ t('dash.topology') }}</span>
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-caption-2 font-caption-2 bg-secondary/15 text-secondary">
            {{ t('dash.singleInstance') }}
          </span>
        </div>
        <span class="text-caption-2 font-caption-2 text-outline">{{ t('dash.storage') }}</span>
      </div>

      <!-- 单节点卡片占满整行：原型这里并排「节点 + 集群拓扑」，而集群拓扑在本
           网关没有后端来源（单进程单实例），留着就是一格永久降级占位。 -->
      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-2">
            <Icon name="server" class="text-secondary text-[20px]" />
            <div class="flex flex-col">
              <span class="text-headline font-headline text-on-surface leading-tight">{{ t('dash.node') }}</span>
              <span class="text-caption-2 font-caption-2 text-outline">{{ t('dash.nodeSub') }}</span>
            </div>
          </div>
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-secondary/15 text-secondary">
            {{ settings.enabled ? t('dash.tag.healthy') : t('dash.tag.disabled') }}
          </span>
        </div>
        <div class="grid grid-cols-2 sm:grid-cols-3 gap-space-sm my-space-md bg-surface-container-low/70 p-space-sm rounded-xl">
          <div class="flex flex-col">
            <span class="eyebrow">{{ t('dash.nodeSwitch') }}</span>
            <span class="text-headline font-headline text-on-surface">{{ settings.enabled ? t('dash.nodeOn') : t('dash.nodeOff') }}</span>
          </div>
          <div class="flex flex-col">
            <span class="eyebrow">{{ t('shell.keyPool') }}</span>
            <span class="text-headline font-headline text-on-surface mono">{{ keysEnabled }} / {{ keys.length }}</span>
            <span class="text-caption-2 font-caption-2 text-outline">{{ t('dash.nodeKeysSub') }}</span>
          </div>
          <div class="flex flex-col">
            <span class="eyebrow">{{ t('shell.jevModel') }}</span>
            <span class="text-headline font-headline text-on-surface mono truncate">{{ settings.jev_model || t('shell.notSet') }}</span>
          </div>
        </div>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-2 gap-space-md mt-space-xs">
        <button
          type="button"
          class="flex items-center justify-between p-space-md rounded-2xl bg-surface-container hover:bg-surface-container-high transition-all group shadow-sm border border-hairline text-left"
          @click="router.push({ name: 'settings' })"
        >
          <div class="flex items-center gap-space-md">
            <div class="w-10 h-10 rounded-xl bg-primary/10 flex items-center justify-center text-primary group-hover:bg-primary group-hover:text-on-primary transition-all">
              <Icon name="sliders" class="text-[22px]" />
            </div>
            <div class="flex flex-col">
              <span class="text-headline font-headline text-on-surface">{{ t('dash.ctaSettings') }}</span>
            </div>
          </div>
          <div class="flex items-center gap-1 text-on-surface-variant group-hover:text-primary transition-colors">
            <span class="text-caption-1 font-caption-1 font-medium">{{ t('dash.ctaSettingsGo') }}</span>
            <Icon name="chevron-right" class="text-[18px]" />
          </div>
        </button>

        <button
          type="button"
          class="flex items-center justify-between p-space-md rounded-2xl bg-surface-container hover:bg-surface-container-high transition-all group shadow-sm border border-hairline text-left"
          @click="router.push({ name: 'audit' })"
        >
          <div class="flex items-center gap-space-md">
            <div class="w-10 h-10 rounded-xl bg-secondary/10 flex items-center justify-center text-secondary group-hover:bg-secondary group-hover:text-on-secondary transition-all">
              <Icon name="search-check" class="text-[22px]" />
            </div>
            <div class="flex flex-col">
              <span class="text-headline font-headline text-on-surface">{{ t('dash.ctaAudit') }}</span>
            </div>
          </div>
          <div class="flex items-center gap-1 text-on-surface-variant group-hover:text-secondary transition-colors">
            <span class="text-caption-1 font-caption-1 font-medium">{{ t('dash.ctaAuditGo') }}</span>
            <Icon name="chevron-right" class="text-[18px]" />
          </div>
        </button>
      </div>
    </section>
  </div>
</template>
