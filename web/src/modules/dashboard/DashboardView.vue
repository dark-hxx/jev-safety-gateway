<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '../../components/Icon.vue'
import NotConnected from '../../components/NotConnected.vue'
import * as api from '../../api'
import { useConsole } from '../../console'
import { num, pctText } from '../../format'
import type { Stats, StatsResponse } from '../../types'
import TrendChart from './TrendChart.vue'
import ScoreDistribution from './ScoreDistribution.vue'

/**
 * 运行概览（驾驶舱）。
 *
 * 真实数据来源：`GET /api/stats?hours=`（区间总量 + 分桶序列 + 分值直方图）
 * 与 `GET /api/state` 的 `stats24h`（首屏快照）。
 *
 * 两张图都只渲染后端给出的数字：趋势图按响应的 `bucket_seconds`/`series` 画线，
 * 分桶与空桶补零都在后端完成；分值分布直接用 `score_buckets`/`unscored`。
 * 前端不自行分桶、不插值、不估算占比。
 *
 * 原型中其余缺少后端来源的元素（多集群节点与地理位置、P99 延迟、峰值 PPS、
 * Token 估算、SLA/Overhead、同比环比）一律以 `NotConnected` 降级态呈现，
 * 不显示任何无来源数值。
 */
const { settings, keys, stats24h } = useConsole()
const router = useRouter()

/** 时间范围分段控件：与 `/api/stats?hours=` 的 `hours` 参数一一对应。 */
const RANGES = [
  { label: '1h', hours: 1, zh: '最近 1 小时' },
  { label: '6h', hours: 6, zh: '最近 6 小时' },
  { label: '24h', hours: 24, zh: '最近 24 小时' },
  { label: '7d', hours: 168, zh: '最近 7 天' },
] as const

const rangeIdx = ref(2)
const range = computed(() => RANGES[rangeIdx.value])

/** 首屏用 `/api/state` 已带回的 24h 快照，避免白屏；切换范围后由 `/api/stats` 更新。 */
const stats = ref<Stats>({ ...stats24h.value })
/** 图表数据；在首次 `/api/stats` 返回前为空，图表显示等待态而非伪造曲线。 */
const series = ref<StatsResponse['series']>([])
const scoreBuckets = ref<number[]>([])
const unscored = ref(0)
const bucketSeconds = ref(0)
const loading = ref(false)
const loaded = ref(false)
const loadError = ref('')
const refreshedAt = ref<Date | null>(null)

async function loadStats(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    const r = await api.getStats(range.value.hours)
    stats.value = r
    series.value = r.series ?? []
    scoreBuckets.value = r.score_buckets ?? []
    unscored.value = r.unscored ?? 0
    bucketSeconds.value = r.bucket_seconds ?? 0
    loaded.value = true
    refreshedAt.value = new Date()
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

/** 分桶粒度的中文说明，来自后端回传的 `bucket_seconds`。 */
const bucketLabel = computed(() => {
  const s = bucketSeconds.value
  if (!s) return ''
  if (s % 86400 === 0) return `${s / 86400} 天`
  if (s % 3600 === 0) return `${s / 3600} 小时`
  if (s % 60 === 0) return `${s / 60} 分钟`
  return `${s} 秒`
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
</script>

<template>
  <div class="w-full px-margin py-margin flex flex-col gap-space-lg">
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
              <h1 class="text-title-1 font-title-1 text-on-surface tracking-tight">安全监控驾驶舱</h1>
              <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-secondary/15 text-secondary">
                ENGINE {{ settings.jev_model || '未设置' }}
              </span>
              <span class="inline-flex items-center px-2 py-0.5 rounded-full text-caption-2 font-caption-2 bg-surface-bright text-on-surface-variant">
                管理口 {{ adminHost }}
              </span>
            </div>
            <p class="text-subheadline font-subheadline text-on-surface-variant mt-0.5">
              实时流量深度规约 · 提示词越狱阻断 · PII 数据脱敏路由保护
            </p>
          </div>
        </div>

        <!-- 实时指标带 -->
        <div class="grid grid-cols-2 sm:grid-cols-4 gap-space-sm bg-surface-container-high/60 p-space-sm rounded-xl">
          <div class="flex flex-col px-space-xs gap-0.5">
            <span class="eyebrow">{{ range.label }} 转送量</span>
            <span class="text-headline font-headline text-on-surface mono">{{ num(stats.total) }}</span>
            <span class="text-caption-1 font-caption-1 text-outline">来自 /api/stats</span>
          </div>
          <div class="flex flex-col px-space-xs gap-0.5">
            <span class="eyebrow">峰值流量 PPS</span>
            <NotConnected reason="后端未采集吞吐速率时序，无法给出秒级峰值。" />
            <span class="text-caption-1 font-caption-1 text-outline">需要新增指标采集</span>
          </div>
          <div class="flex flex-col px-space-xs gap-0.5">
            <span class="eyebrow">网关状态</span>
            <div class="flex items-center gap-1.5 mt-1">
              <span class="w-2 h-2 rounded-full" :class="settings.enabled ? 'bg-secondary' : 'bg-outline'"></span>
              <span class="text-caption-1 font-caption-1 font-semibold" :class="settings.enabled ? 'text-secondary' : 'text-outline'">
                {{ settings.enabled ? 'Active · 过滤中' : 'Disabled · 全量放行' }}
              </span>
            </div>
            <span class="text-caption-2 font-caption-2 text-outline">
              {{ settings.fail_open ? 'Fail-Open 已开启' : 'Fail-Close 已开启' }}
            </span>
          </div>
          <div class="flex flex-col px-space-xs gap-0.5">
            <span class="eyebrow">集群实例</span>
            <NotConnected reason="当前为单进程单实例部署，没有集群节点数据。" />
            <span class="text-caption-2 font-caption-2 text-outline">密钥池 {{ keysEnabled }} / {{ keys.length }} 可用</span>
          </div>
        </div>
      </div>
    </section>

    <!-- 2. 关键指标卡 -->
    <section class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-space-md">
      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ range.label }} 总请求量</span>
          <Icon name="swap-horiz" class="text-primary text-[18px]" />
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-on-surface tracking-tight mono">{{ num(stats.total) }}</div>
          <div class="mt-0.5"><NotConnected reason="后端只提供当前区间序列，没有上一同长区间的聚合，无法计算环比。" /></div>
        </div>
        <div class="text-caption-2 font-caption-2 text-outline">环比未接入</div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">正常放行率</span>
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-secondary/15 text-secondary">
            Normal
          </span>
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-on-surface tracking-tight mono">{{ allowedRate }}</div>
          <div class="text-caption-2 font-caption-2 text-outline mt-0.5">{{ num(stats.allowed) }} 次放行请求</div>
        </div>
        <div class="w-full bg-surface-container-highest rounded-full h-1.5 overflow-hidden">
          <div class="bg-secondary h-1.5 rounded-full" :style="{ width: allowedRate === '-' ? '0%' : allowedRate }"></div>
        </div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">高危威胁拦截</span>
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-error-container text-error">
            Blocked
          </span>
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-error tracking-tight mono">{{ num(stats.blocked) }}</div>
          <div class="text-caption-2 font-caption-2 text-error/80 mt-0.5">
            占比 {{ blockedShare }} · {{ settings.block_if_below ? '低于阈值即拦截' : '高于阈值即拦截' }}
          </div>
        </div>
        <div class="text-caption-2 font-caption-2 text-outline">拦截逐桶趋势见下方态势图</div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">策略跳过与降级</span>
          <Icon name="alt-route" class="text-outline text-[18px]" />
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-on-surface tracking-tight mono">{{ num(stats.skipped) }}</div>
          <div class="text-caption-2 font-caption-2 text-outline mt-0.5">占比 {{ skippedShare }} · 未送检直接放行</div>
        </div>
        <div class="w-full bg-surface-container-highest rounded-full h-1.5 overflow-hidden">
          <div class="bg-outline h-1.5 rounded-full" :style="{ width: skippedShare === '-' ? '0%' : skippedShare }"></div>
        </div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">异常容灾放行</span>
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-tertiary/15 text-tertiary">
            Fail-Open
          </span>
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-tertiary tracking-tight mono">{{ num(stats.errors) }}</div>
          <div class="text-caption-2 font-caption-2 text-on-surface-variant mt-0.5">占比 {{ errorShare }} · 检定异常或超时</div>
        </div>
        <div class="w-full bg-surface-container-highest rounded-full h-1.5 overflow-hidden">
          <div class="bg-tertiary h-1.5 rounded-full" :style="{ width: errorShare === '-' ? '0%' : errorShare }"></div>
        </div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">P99 检定延迟</span>
          <Icon name="gauge" class="text-primary text-[18px]" />
        </div>
        <div class="my-space-sm">
          <NotConnected reason="logs 表只存单请求耗时，没有延迟分位数聚合接口。" />
        </div>
        <div class="text-caption-2 font-caption-2 text-outline">延迟分布未接入</div>
      </div>
    </section>

    <!-- 3. 趋势与威胁分类 -->
    <section class="grid grid-cols-1 xl:grid-cols-12 gap-space-lg">
      <div class="xl:col-span-7 flex flex-col p-space-lg rounded-2xl bg-surface-container shadow-lg border border-hairline">
        <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-space-sm mb-space-md">
          <div class="flex flex-col">
            <span class="text-title-3 font-title-3 text-on-surface">{{ range.zh }}流量与安全威胁态势</span>
            <span class="text-caption-1 font-caption-1 text-outline flex items-center gap-1.5">
              <span class="h-1.5 w-1.5 rounded-full bg-secondary animate-pulse"></span>
              刷新周期 5s · 最后刷新 {{ refreshedAt ? refreshedAt.toLocaleTimeString() : '—' }}
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
            <span class="text-on-surface-variant">入站总请求 (Total)</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="w-2.5 h-2.5 rounded-full bg-secondary"></span>
            <span class="text-on-surface-variant">安全放行 (Allowed)</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="w-2.5 h-2.5 rounded-full bg-error"></span>
            <span class="text-on-surface-variant">越狱/攻击拦截 (Blocked)</span>
          </div>
        </div>

        <TrendChart :series="series" :bucket-seconds="bucketSeconds" :loaded="loaded" />

        <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-space-xs pt-space-md text-caption-1 font-caption-1 text-on-surface-variant">
          <div class="flex items-center gap-1.5" :class="loadError ? 'text-error' : ''">
            <Icon :name="loadError ? 'alert-circle' : 'check-circle'" class="text-[16px]" :class="loadError ? 'text-error' : 'text-secondary'" />
            <span v-if="loadError">统计查询失败：{{ loadError }}</span>
            <span v-else>
              {{ range.zh }}内 放行 {{ num(stats.allowed) }} 次 · 拦截 {{ num(stats.blocked) }} 次
              <span v-if="loading" class="text-outline">（刷新中…）</span>
            </span>
          </div>
          <span class="font-code-body text-code-body text-outline mono">
            GET /api/stats?hours={{ range.hours }}
            <template v-if="bucketLabel"> · 每桶 {{ bucketLabel }}（由后端分桶）</template>
            <template v-if="series.length"> · {{ series.length }} 个桶</template>
          </span>
        </div>
      </div>

      <div class="xl:col-span-5 flex flex-col p-space-lg rounded-2xl bg-surface-container shadow-lg border border-hairline">
        <div class="flex items-center justify-between mb-space-sm">
          <div class="flex flex-col">
            <span class="text-title-3 font-title-3 text-on-surface">风险分值分布</span>
            <span class="text-caption-1 font-caption-1 text-outline">
              {{ range.zh }} · JEV noul 分值直方图（0.2 步长五档，末档含 1.0）
            </span>
          </div>
          <Icon name="donut" class="text-outline text-[20px]" />
        </div>
        <ScoreDistribution :counts="scoreBuckets" :unscored="unscored" :loaded="loaded" />
        <div class="flex flex-col gap-space-sm pt-space-xs">
          <div class="flex items-center justify-between text-caption-1 font-caption-1 text-on-surface-variant">
            <span>逐条判定原因与送检摘要可在转发审计记录中查看</span>
            <button
              type="button"
              class="inline-flex items-center gap-1 text-primary hover:underline"
              @click="router.push({ name: 'audit' })"
            >
              查看审计
              <Icon name="chevron-right" class="text-[14px]" />
            </button>
          </div>
        </div>
      </div>
    </section>

    <!-- 4. 运行实例与拓扑 -->
    <section class="flex flex-col gap-space-md">
      <div class="flex items-center justify-between flex-wrap gap-space-xs">
        <div class="flex items-center gap-2">
          <span class="text-title-3 font-title-3 text-on-surface">运行实例与拓扑</span>
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-caption-2 font-caption-2 bg-secondary/15 text-secondary">
            单实例进程内运行
          </span>
        </div>
        <span class="text-caption-2 font-caption-2 text-outline">审计存储 SQLite (WAL) · 配置持久化于管理口进程</span>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-3 gap-space-md">
        <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2">
              <Icon name="server" class="text-secondary text-[20px]" />
              <div class="flex flex-col">
                <span class="text-headline font-headline text-on-surface leading-tight">本机网关实例</span>
                <span class="text-caption-2 font-caption-2 text-outline">进程内过滤与审计</span>
              </div>
            </div>
            <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-secondary/15 text-secondary">
              {{ settings.enabled ? 'Healthy' : 'Disabled' }}
            </span>
          </div>
          <div class="grid grid-cols-2 gap-space-sm my-space-md bg-surface-container-low/70 p-space-sm rounded-xl">
            <div class="flex flex-col">
              <span class="eyebrow">过滤开关</span>
              <span class="text-headline font-headline text-on-surface">{{ settings.enabled ? '已启用' : '已关闭' }}</span>
            </div>
            <div class="flex flex-col">
              <span class="eyebrow">密钥池</span>
              <span class="text-headline font-headline text-on-surface mono">{{ keysEnabled }} / {{ keys.length }}</span>
              <span class="text-caption-2 font-caption-2 text-outline">启用 / 总数</span>
            </div>
          </div>
          <div class="flex items-center justify-between text-caption-2 font-caption-2 text-on-surface-variant pt-space-xs">
            <span>检定模型</span>
            <span class="font-code-body text-code-body text-on-surface mono truncate max-w-[10rem]">{{ settings.jev_model || '未设置' }}</span>
          </div>
        </div>

        <div class="md:col-span-2 flex flex-col p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
          <div class="flex items-center justify-between mb-space-sm">
            <div class="flex items-center gap-2">
              <Icon name="network" class="text-outline text-[20px]" />
              <div class="flex flex-col">
                <span class="text-headline font-headline text-on-surface leading-tight">多集群节点与地理分布</span>
                <span class="text-caption-2 font-caption-2 text-outline">Cluster Topology &amp; Geo Distribution</span>
              </div>
            </div>
            <NotConnected reason="网关为单进程部署，没有节点注册、心跳与地理调度数据。" />
          </div>
          <div class="flex-1">
            <NotConnected
              variant="placeholder"
              title="集群拓扑未接入"
              reason="需要节点名册、心跳上报与地理信息采集；涉及新增后台组件，属后续 change 范围。"
            />
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
              <span class="text-headline font-headline text-on-surface">调整网关安全过滤策略与规则</span>
              <span class="text-caption-1 font-caption-1 text-outline">配置上游路由、安全判定阈值、自动拉黑与调用密钥池</span>
            </div>
          </div>
          <div class="flex items-center gap-1 text-on-surface-variant group-hover:text-primary transition-colors">
            <span class="text-caption-1 font-caption-1 font-medium">网关配置</span>
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
              <span class="text-headline font-headline text-on-surface">实时检定流与转发审计日志</span>
              <span class="text-caption-1 font-caption-1 text-outline">查看来源 IP、判定原因与送检内容摘要</span>
            </div>
          </div>
          <div class="flex items-center gap-1 text-on-surface-variant group-hover:text-secondary transition-colors">
            <span class="text-caption-1 font-caption-1 font-medium">审计流水</span>
            <Icon name="chevron-right" class="text-[18px]" />
          </div>
        </button>
      </div>
    </section>
  </div>
</template>
