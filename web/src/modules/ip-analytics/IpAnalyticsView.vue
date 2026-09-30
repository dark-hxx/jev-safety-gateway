<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '../../components/Icon.vue'
import HintTip from '../../components/HintTip.vue'
import NotConnected from '../../components/NotConnected.vue'
import SelectMenu from '../../components/SelectMenu.vue'
import WorldThreatMap from '../../components/WorldThreatMap.vue'
import * as api from '../../api'
import { confirmDialog } from '../../confirm'
import { durationOf, num, pctText, relativeOf, score } from '../../format'
import { useI18n } from '../../i18n'
import type { MessageKey } from '../../i18n/zh'
import type { ASNBucket, BanEntry, GeoBucket, IPRule, IPStat, IpStatsResponse } from '../../types'

/**
 * IP 风险分析与统计。
 *
 * 数据来自 GET /api/stats/ip?hours=&limit=：顶层是窗口 KPI 汇总（覆盖窗口内全部去重
 * IP，不受列表 limit 影响），items 是危险度倒序的每 IP 聚合（后端上限 500 条），
 * bans 是 abuse tracker 的实时临时封禁（单实例、进程内、重启失效）。
 *
 * 危险评分为确定式推导（见 internal/config/ipstats.go 的 ipDanger）：
 *   danger = max(0, 1-avgScore) × blocked/scored × min(1, total/40)
 * 每一档都能由本页同时展示的原始计数复核，不是黑盒。
 *
 * 封禁规则池（手动 / CIDR / 永久封禁 + 白名单）来自 GET/POST/DELETE /api/ip-rules，
 * 与实时 abuse 封禁互补、持久化于 SQLite；白名单优先于封禁但不豁免内容检定。
 * 地理 / 区域分布与 ASN 映射为可选的 GeoIP 归属：配置了本地数据库（JEV_GEOIP_*_DB）时
 * 出排行榜与行内国旗，未配置则保持 NotConnected 占位。全部在管理口查询期离线聚合，
 * 不触及代理热路径。本页只呈现有来源的数字。
 */
const router = useRouter()
const { t } = useI18n()

/** 时间范围分段控件：与 /api/stats/ip?hours= 的 hours 参数一一对应。 */
const RANGES: { label: string; hours: number }[] = [
  { label: '1h', hours: 1 },
  { label: '6h', hours: 6 },
  { label: '24h', hours: 24 },
  { label: '7d', hours: 168 },
]
const rangeIdx = ref(2)
const range = computed(() => RANGES[rangeIdx.value])

/**
 * 列表拉取上限：后端硬上限为 500，这里一次取满，使前端可对整张榜单分页与导出 CSV；
 * KPI 汇总始终反映窗口内全部 IP（后端在 limit 之外单独计算），不受此上限影响。
 */
const FETCH_LIMIT = 500
/** 客户端分页每页条数（列表已在前端全量持有）。 */
const PAGE_SIZE = 12
const summary = ref<IpStatsResponse | null>(null)
const items = ref<IPStat[]>([])
const bans = ref<BanEntry[]>([])
const loading = ref(false)
const loadError = ref('')
const refreshedAt = ref<Date | null>(null)
const pageIdx = ref(0)

/** 每秒推进的墙钟：把封禁 until 渲染成平滑倒计时（until 才是权威到期时刻）。 */
const nowMs = ref(Date.now())

// --- 客户端分页（列表已全量持有）---
const pageCount = computed(() => Math.max(1, Math.ceil(items.value.length / PAGE_SIZE)))
const pagedItems = computed(() => items.value.slice(pageIdx.value * PAGE_SIZE, pageIdx.value * PAGE_SIZE + PAGE_SIZE))
const showingFrom = computed(() => (items.value.length ? pageIdx.value * PAGE_SIZE + 1 : 0))
const showingTo = computed(() => Math.min(items.value.length, (pageIdx.value + 1) * PAGE_SIZE))
function goPage(delta: number): void {
  const next = pageIdx.value + delta
  if (next < 0 || next >= pageCount.value) return
  pageIdx.value = next
}

async function load(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    const r = await api.getIpStats(range.value.hours, FETCH_LIMIT)
    summary.value = r
    items.value = r.items ?? []
    bans.value = r.bans ?? []
    refreshedAt.value = new Date()
    // 数据集变化后，把越界页码收回最后一页。
    if (pageIdx.value >= pageCount.value) pageIdx.value = pageCount.value - 1
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

function selectRange(i: number): void {
  if (i === rangeIdx.value) return
  rangeIdx.value = i
  pageIdx.value = 0
  void load()
}
let timer: number | undefined
let clock: number | undefined
onMounted(() => {
  void load()
  void loadRules()
  timer = window.setInterval(() => void load(), 5000)
  clock = window.setInterval(() => (nowMs.value = Date.now()), 1000)
})
onBeforeUnmount(() => {
  if (timer !== undefined) window.clearInterval(timer)
  if (clock !== undefined) window.clearInterval(clock)
})

// --- KPI（顶层为窗口内全部 IP 的汇总，不随列表 limit 变化）---
const distinctIps = computed(() => summary.value?.distinct_ips ?? 0)
const critCount = computed(() => summary.value?.crit ?? 0)
const highCount = computed(() => summary.value?.high ?? 0)
const threatCount = computed(() => critCount.value + highCount.value)
const totalBlocked = computed(() => summary.value?.total_blocked ?? 0)
const totalEvents = computed(() => summary.value?.total_events ?? 0)
const blockedShare = computed(() => pctText(totalBlocked.value, totalEvents.value))
const banCount = computed(() => bans.value.length)

// --- 可选 GeoIP 归属分布（仅当后端配置了本地数据库时出数据；否则保持 NotConnected）---
// geo_enabled / asn_enabled 由后端按「该维度是否有库打开」回报，与是否命中无关；
// 数组已在后端按事件量降序并 top-N 封顶，前端只做比例条与国旗渲染。
const geoEnabled = computed(() => summary.value?.geo_enabled ?? false)
const asnEnabled = computed(() => summary.value?.asn_enabled ?? false)
const geoBuckets = computed<GeoBucket[]>(() => summary.value?.geo ?? [])
const asnBuckets = computed<ASNBucket[]>(() => summary.value?.asn ?? [])
/**
 * 本网关部署坐标（运维声明的 JEV_GATEWAY_LAT / JEV_GATEWAY_LON）；未声明时为 undefined，
 * 来源图据此不出中心节点与弧线。该字段与 geo_enabled 独立：它描述「服务在哪跑」，
 * 而不是「客户端来自哪」。
 */
const gatewayLoc = computed(() => summary.value?.gateway)
/** 比例条以榜首事件量为满刻度，让相对量级一眼可读（拦截量共用同一刻度叠加其上）。 */
const geoMaxTotal = computed(() => Math.max(1, ...geoBuckets.value.map((b) => b.total)))
const asnMaxTotal = computed(() => Math.max(1, ...asnBuckets.value.map((b) => b.total)))
function barPct(v: number, max: number): string {
  return (max > 0 ? Math.min(100, (v / max) * 100) : 0) + '%'
}

/**
 * ISO-3166-1 alpha-2 国家码 → 区域指示符 emoji 国旗（离线、无图片资源）。
 * 非法 / 空码返回空串，调用方回退到「未知」占位，绝不渲染错误旗帜。
 */
function isoToFlag(iso?: string): string {
  if (!iso || !/^[A-Za-z]{2}$/.test(iso)) return ''
  const base = 0x1f1e6 // 区域指示符 'A'
  const cc = iso.toUpperCase()
  return String.fromCodePoint(base + (cc.charCodeAt(0) - 65), base + (cc.charCodeAt(1) - 65))
}
/** ASN 展示名：优先组织名，否则 ASxxxx；未归属（asn=0）走「未知」占位。 */
function asnLabel(b: ASNBucket): string {
  if (b.asn === 0) return t('ipa.dist.unknown')
  return b.org || 'AS' + b.asn
}
/** 从危险榜 items 建 ip→国家 的映射，供实时封禁列表复用国旗（封禁项本身不带归属地）。 */
const countryByIp = computed(() => {
  const m = new Map<string, string>()
  for (const it of items.value) if (it.country) m.set(it.ip, it.country)
  return m
})

// --- 危险分档样式 ---
interface LevelStyle {
  labelKey: MessageKey
  badge: string
  dot: string
  text: string
  bar: string
}
const LEVEL_STYLES: Record<string, LevelStyle> = {
  crit: { labelKey: 'ipa.level.crit', badge: 'bg-error-container text-error', dot: 'bg-error', text: 'text-error', bar: 'bg-error' },
  high: { labelKey: 'ipa.level.high', badge: 'bg-tertiary-container/30 text-tertiary', dot: 'bg-tertiary', text: 'text-tertiary', bar: 'bg-tertiary' },
  med: { labelKey: 'ipa.level.med', badge: 'bg-primary/15 text-primary', dot: 'bg-primary', text: 'text-primary', bar: 'bg-primary' },
  low: { labelKey: 'ipa.level.low', badge: 'bg-surface-bright text-on-surface-variant', dot: 'bg-outline', text: 'text-on-surface-variant', bar: 'bg-outline' },
}
function levelStyle(level: string): LevelStyle {
  return LEVEL_STYLES[level] ?? LEVEL_STYLES.low
}
function levelLabel(level: string): string {
  return t(levelStyle(level).labelKey)
}
/** 0–1 危险评分渲染为 0–100 的整数刻度。 */
function dangerScale(d: number): number {
  return Math.round(Math.max(0, Math.min(1, d)) * 100)
}
// --- 封禁交叉引用（列表「处置状态」列）---
const banUntilByIp = computed(() => {
  const m = new Map<string, number>()
  for (const b of bans.value) m.set(b.ip, b.until)
  return m
})
/** 从权威到期时刻推导剩余秒数（随每秒墙钟平滑递减，最低 0）。 */
function remainSecOf(until: number): number {
  return Math.max(0, Math.floor((until - nowMs.value) / 1000))
}
function isBanned(ip: string): boolean {
  return banUntilByIp.value.has(ip)
}

// --- 时间渲染（后端时间字段为 unix 毫秒）---
function relOf(ms: number): string {
  return relativeOf(new Date(ms).toISOString())
}
function untilText(ms: number): string {
  const d = new Date(ms)
  return Number.isNaN(d.getTime()) ? '-' : d.toLocaleTimeString()
}

// --- 深链接到审计页并预置来源 IP（始终可用，不依赖是否记录送检摘要）---
function viewAudit(ip: string): void {
  void router.push({ name: 'audit', query: { ip } })
}

// --- CSV 导出（客户端；导出当前已加载的整张榜单，最多 FETCH_LIMIT 条）---
function csvCell(v: string | number): string {
  const s = String(v)
  return /[",\r\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s
}
function exportCsv(): void {
  if (!items.value.length) return
  const header = ['ip', 'level', 'danger', 'total', 'allowed', 'blocked', 'skipped', 'errors', 'scored', 'avg_score', 'min_score', 'first_seen', 'last_seen', 'last_reason']
  const lines = [header.join(',')]
  for (const it of items.value) {
    lines.push([
      it.ip, it.level, it.danger.toFixed(4),
      it.total, it.allowed, it.blocked, it.skipped, it.errors, it.scored,
      it.avg_score ?? '', it.min_score ?? '',
      new Date(it.first_seen).toISOString(), new Date(it.last_seen).toISOString(),
      (it.last_reason || '').replace(/[\r\n]+/g, ' '),
    ].map(csvCell).join(','))
  }
  // 前置 BOM，Excel 才会按 UTF-8 解码中文。
  const blob = new Blob(['﻿' + lines.join('\r\n')], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `ip-analytics-${range.value.hours}h-${Date.now()}.csv`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

// --- 持久化 IP 规则（封禁 / 白名单）管理，GET/POST/DELETE /api/ip-rules ---
const rules = ref<IPRule[]>([])
const rulesError = ref('')
const newPattern = ref('')
const newKind = ref<'block' | 'allow'>('block')
/** 临时封禁时长（秒）预设，或 'permanent' 表示永久；白名单忽略此项。 */
const newDuration = ref<'600' | '3600' | '86400' | 'permanent'>('3600')
const newReason = ref('')
const submitting = ref(false)

/** 规则类型与封禁时长的下拉选项（随语言切换实时更新，SelectMenu 自绘以对齐控制台风格）。 */
const kindOptions = computed(() => [
  { value: 'block', label: t('ipa.manage.kindBlock') },
  { value: 'allow', label: t('ipa.manage.kindAllow') },
])
const durationOptions = computed(() => [
  { value: '600', label: t('ipa.manage.dur10m') },
  { value: '3600', label: t('ipa.manage.dur1h') },
  { value: '86400', label: t('ipa.manage.dur24h') },
  { value: 'permanent', label: t('ipa.manage.durPermanent') },
])

/** SelectMenu 以 string 双向绑定，这里把值收窄回各自的字面量联合，避免类型断言。 */
function setKind(v: string): void {
  newKind.value = v === 'allow' ? 'allow' : 'block'
}
function isDuration(v: string): v is '600' | '3600' | '86400' | 'permanent' {
  return v === '600' || v === '3600' || v === '86400' || v === 'permanent'
}
function setDuration(v: string): void {
  if (isDuration(v)) newDuration.value = v
}

async function loadRules(): Promise<void> {
  try {
    rules.value = await api.listIpRules()
    rulesError.value = ''
  } catch (e) {
    rulesError.value = e instanceof Error ? e.message : String(e)
  }
}

async function addRule(): Promise<void> {
  const pattern = newPattern.value.trim()
  if (!pattern || submitting.value) return
  submitting.value = true
  rulesError.value = ''
  try {
    const permanent = newKind.value === 'allow' || newDuration.value === 'permanent'
    await api.addIpRule({
      pattern,
      kind: newKind.value,
      reason: newReason.value.trim(),
      permanent,
      duration_sec: permanent ? 0 : Number(newDuration.value),
    })
    newPattern.value = ''
    newReason.value = ''
    await Promise.all([loadRules(), load()])
  } catch (e) {
    rulesError.value = e instanceof Error ? e.message : String(e)
  } finally {
    submitting.value = false
  }
}

async function removeRule(rule: IPRule): Promise<void> {
  if (
    !(await confirmDialog({
      title: t('ipa.manage.deleteTitle'),
      message: t('ipa.manage.deleteConfirm', { pattern: rule.pattern }),
      confirmText: t('ipa.manage.delete'),
      danger: true,
    }))
  )
    return
  rulesError.value = ''
  try {
    await api.deleteIpRule(rule.id)
    await Promise.all([loadRules(), load()])
  } catch (e) {
    rulesError.value = e instanceof Error ? e.message : String(e)
  }
}

/** 从排行表一键手动封禁：默认 1 小时临时封禁，随后刷新规则池与统计。 */
async function banIp(ip: string): Promise<void> {
  if (
    !(await confirmDialog({
      title: t('ipa.action.banTitle'),
      message: t('ipa.action.banConfirm', { ip }),
      confirmText: t('ipa.action.ban'),
      danger: true,
    }))
  )
    return
  rulesError.value = ''
  try {
    await api.addIpRule({ pattern: ip, kind: 'block', reason: t('ipa.action.ban'), permanent: false, duration_sec: 3600 })
    await Promise.all([loadRules(), load()])
  } catch (e) {
    rulesError.value = e instanceof Error ? e.message : String(e)
  }
}

/** 规则到期时刻（unix 毫秒）；expires_at 缺省（永久规则）时返回 null。 */
function ruleExpiryMs(rule: IPRule): number | null {
  if (!rule.expires_at) return null
  const ms = Date.parse(rule.expires_at)
  return Number.isNaN(ms) ? null : ms
}
</script>

<template>
  <div class="w-full px-margin-mobile py-margin-mobile md:px-margin md:py-margin flex flex-col gap-space-lg">
    <!-- 页头 -->
    <div class="flex flex-col lg:flex-row lg:items-end justify-between gap-space-md">
      <div class="flex flex-col gap-1">
        <div class="flex items-center gap-space-xs flex-wrap">
          <h1 class="text-title-2 font-title-2 text-on-surface tracking-tight">{{ t('ipa.title') }}</h1>
          <span class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-caption-2 font-caption-2 bg-secondary/15 text-secondary">
            <span class="h-1.5 w-1.5 rounded-full bg-secondary animate-pulse"></span>
            {{ t('dash.refreshCycle', { time: refreshedAt ? refreshedAt.toLocaleTimeString() : '—' }) }}
          </span>
        </div>
        <p class="text-subheadline font-subheadline text-on-surface-variant">{{ t('ipa.sub') }}</p>
      </div>
      <div class="flex items-center gap-space-sm flex-wrap">
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
        <button
          type="button"
          class="inline-flex items-center gap-1.5 px-3 py-2 rounded-xl bg-surface-container hover:bg-surface-container-high text-subheadline font-subheadline text-on-surface transition-all disabled:opacity-50 disabled:cursor-not-allowed"
          :disabled="!items.length"
          :title="items.length ? '' : t('ipa.exportEmpty')"
          @click="exportCsv"
        >
          <Icon name="download" class="text-[16px]" />
          <span>{{ t('ipa.export') }}</span>
        </button>
        <button
          type="button"
          class="inline-flex items-center gap-1.5 px-3 py-2 rounded-xl bg-surface-container hover:bg-surface-container-high text-subheadline font-subheadline text-on-surface transition-all"
          :disabled="loading"
          @click="load()"
        >
          <Icon name="refresh" class="text-[16px]" :class="loading ? 'animate-spin' : ''" />
          <span>{{ t('audit.refresh') }}</span>
        </button>
      </div>
    </div>
    <!-- KPI 卡（数字均来自窗口 KPI 汇总 / 实时封禁，无任何无来源数值） -->
    <section class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-space-md">
      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('ipa.kpi.distinct') }}</span>
          <Icon name="network" class="text-primary text-[18px]" />
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-on-surface tracking-tight mono">{{ num(distinctIps) }}</div>
          <div class="text-caption-2 font-caption-2 text-outline mt-0.5">{{ t('ipa.kpi.distinctSub') }}</div>
        </div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('ipa.kpi.threat') }}</span>
          <Icon name="alert-triangle" class="text-error text-[18px]" />
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-error tracking-tight mono">{{ num(threatCount) }}</div>
          <div class="text-caption-2 font-caption-2 text-error/80 mt-0.5">{{ t('ipa.kpi.threatSub', { crit: num(critCount), high: num(highCount) }) }}</div>
        </div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('ipa.kpi.blocked') }}</span>
          <Icon name="ban" class="text-error text-[18px]" />
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-on-surface tracking-tight mono">{{ num(totalBlocked) }}</div>
          <div class="text-caption-2 font-caption-2 text-outline mt-0.5">{{ t('ipa.kpi.blockedSub', { share: blockedShare }) }}</div>
        </div>
      </div>

      <div class="flex flex-col justify-between p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('ipa.kpi.bans') }}</span>
          <Icon name="gavel" class="text-tertiary text-[18px]" />
        </div>
        <div class="my-space-sm">
          <div class="text-title-2 font-title-2 text-tertiary tracking-tight mono">{{ num(banCount) }}</div>
          <div class="text-caption-2 font-caption-2 text-outline mt-0.5">{{ t('ipa.kpi.bansSub') }}</div>
        </div>
      </div>
    </section>
    <!-- 第二行：危险来源 IP 排行（整行铺满，便于横向浏览与分页） -->
    <section class="flex flex-col gap-space-lg">
      <!-- 危险来源 IP 排行（items[]，危险度倒序，客户端分页） -->
      <div class="flex flex-col rounded-2xl bg-surface-container shadow-lg border border-hairline overflow-hidden">
        <div class="px-space-md py-space-sm border-b border-hairline flex items-center justify-between flex-wrap gap-space-xs">
          <div class="flex flex-col">
            <div class="flex items-center gap-space-xs">
              <span class="text-title-3 font-title-3 text-on-surface">{{ t('ipa.table.title') }}</span>
              <HintTip :text="t('ipa.dangerNote')" />
            </div>
            <span class="text-caption-1 font-caption-1 text-outline">{{ t('ipa.table.sub', { shown: num(items.length), total: num(distinctIps) }) }}</span>
          </div>
          <span v-if="loadError" class="text-caption-1 font-caption-1 text-error">{{ t('ipa.loadError', { msg: loadError }) }}</span>
        </div>

        <div class="overflow-auto">
          <table class="w-full min-w-[48rem] text-left border-collapse">
            <thead class="sticky top-0 z-10 bg-surface-container-high">
              <tr class="text-caption-2 font-caption-2 text-on-surface-variant">
                <th class="px-space-sm py-2.5 font-medium whitespace-nowrap">{{ t('ipa.col.ip') }}</th>
                <th class="px-space-sm py-2.5 font-medium whitespace-nowrap">{{ t('ipa.col.freq') }}</th>
                <th class="px-space-sm py-2.5 font-medium">{{ t('ipa.col.reason') }}</th>
                <th class="px-space-sm py-2.5 font-medium whitespace-nowrap">{{ t('ipa.col.danger') }}</th>
                <th class="px-space-sm py-2.5 font-medium whitespace-nowrap">{{ t('ipa.col.status') }}</th>
                <th class="px-space-sm py-2.5 font-medium text-right">{{ t('ipa.col.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="it in pagedItems"
                :key="it.ip"
                class="border-t border-hairline hover:bg-surface-container-high/60 transition-colors align-top"
              >
                <td class="px-space-sm py-2.5 whitespace-nowrap">
                  <span class="flex items-center gap-1.5">
                    <span
                      v-if="isoToFlag(it.country)"
                      class="text-[15px] leading-none"
                      :title="it.country_name || it.country"
                    >{{ isoToFlag(it.country) }}</span>
                    <span class="font-code-body text-code-body text-on-surface mono">{{ it.ip }}</span>
                  </span>
                  <span class="block text-caption-2 font-caption-2 text-outline">{{ t('ipa.lastSeen', { v: relOf(it.last_seen) }) }}</span>
                </td>
                <td class="px-space-sm py-2.5 whitespace-nowrap">
                  <span class="text-headline font-headline text-on-surface mono">{{ num(it.total) }}</span>
                  <span class="block text-caption-2 font-caption-2 text-outline">{{ t('ipa.freqSub', { scored: num(it.scored), blocked: num(it.blocked) }) }}</span>
                </td>
                <td class="px-space-sm py-2.5">
                  <span class="text-caption-1 font-caption-1 text-on-surface-variant line-clamp-2 break-all">{{ it.last_reason || '-' }}</span>
                </td>
                <td class="px-space-sm py-2.5 whitespace-nowrap">
                  <div class="flex items-center gap-2">
                    <span class="text-headline font-headline mono" :class="levelStyle(it.level).text">{{ dangerScale(it.danger) }}</span>
                    <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-code-badge font-code-badge" :class="levelStyle(it.level).badge">
                      <span class="h-1.5 w-1.5 rounded-full" :class="levelStyle(it.level).dot"></span>
                      {{ levelLabel(it.level) }}
                    </span>
                  </div>
                  <span class="block mt-1 w-24 h-1 bg-surface-container-highest rounded-full overflow-hidden">
                    <span class="block h-1 rounded-full" :class="levelStyle(it.level).bar" :style="{ width: dangerScale(it.danger) + '%' }"></span>
                  </span>
                  <span class="block text-caption-2 font-caption-2 text-outline mt-0.5">{{ t('ipa.avgScore', { v: score(it.avg_score) }) }}</span>
                </td>
                <td class="px-space-sm py-2.5 whitespace-nowrap">
                  <template v-if="isBanned(it.ip)">
                    <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-error-container text-error">
                      <Icon name="ban" class="text-[11px]" />{{ t('ipa.status.banned') }}
                    </span>
                    <span class="block text-caption-2 font-caption-2 text-outline mono">{{ t('ipa.status.remain', { v: durationOf(remainSecOf(banUntilByIp.get(it.ip) ?? 0)) }) }}</span>
                  </template>
                  <span v-else class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-surface-bright text-on-surface-variant">
                    {{ t('ipa.status.watch') }}
                  </span>
                </td>
                <td class="px-space-sm py-2.5 text-right">
                  <div class="inline-flex items-center justify-end gap-1.5">
                    <button
                      type="button"
                      class="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-lg bg-surface-container-high hover:bg-surface-container-highest text-caption-2 font-caption-2 text-on-surface transition-colors"
                      @click="viewAudit(it.ip)"
                    >
                      <Icon name="search-check" class="text-[14px]" />
                      <span>{{ t('ipa.viewAudit') }}</span>
                    </button>
                    <button
                      type="button"
                      class="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-lg bg-error-container hover:bg-error-container/80 text-caption-2 font-caption-2 text-error transition-colors"
                      :title="t('ipa.action.ban')"
                      @click="banIp(it.ip)"
                    >
                      <Icon name="ban" class="text-[14px]" />
                      <span>{{ t('ipa.action.ban') }}</span>
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>

          <div v-if="!items.length && !loading" class="flex flex-col items-center justify-center gap-1.5 py-space-xl px-space-md text-center">
            <Icon name="network" class="text-outline text-[24px]" />
            <span class="text-subheadline font-subheadline text-on-surface-variant">{{ t('ipa.empty') }}</span>
            <span class="text-caption-2 font-caption-2 text-outline">{{ t('ipa.emptyHint') }}</span>
          </div>
        </div>

        <!-- 客户端分页（整张榜单已在前端持有，此处仅切片显示） -->
        <div v-if="items.length" class="px-space-md py-space-sm border-t border-hairline flex items-center justify-between flex-wrap gap-space-sm">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">
            {{ t('audit.paging.showing', { from: num(showingFrom), to: num(showingTo), total: num(items.length) }) }}
          </span>
          <div class="flex items-center gap-space-sm">
            <button
              type="button"
              class="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-surface-container-high hover:bg-surface-container-highest text-caption-1 font-caption-1 text-on-surface transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
              :disabled="pageIdx <= 0"
              @click="goPage(-1)"
            >
              <Icon name="chevron-left" class="text-[14px]" />
              <span>{{ t('audit.prev') }}</span>
            </button>
            <span class="text-caption-1 font-caption-1 text-on-surface-variant whitespace-nowrap">
              {{ t('audit.paging.pageOf', { page: pageIdx + 1, pages: pageCount }) }}
            </span>
            <button
              type="button"
              class="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-surface-container-high hover:bg-surface-container-highest text-caption-1 font-caption-1 text-on-surface transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
              :disabled="pageIdx >= pageCount - 1"
              @click="goPage(1)"
            >
              <span>{{ t('audit.next') }}</span>
              <Icon name="chevron-right" class="text-[14px]" />
            </button>
          </div>
        </div>
      </div>
    </section>
    <!-- 第三行：来源分布（左，整列高）与「实时封禁 + ASN 映射」（右，上下堆叠等高） -->
    <section class="grid grid-cols-1 xl:grid-cols-12 gap-space-lg items-stretch">
      <div class="xl:col-span-7 flex flex-col gap-space-sm p-space-lg rounded-2xl bg-surface-container shadow-md border border-hairline">
        <div class="flex items-center justify-between gap-space-sm">
          <div class="flex flex-col gap-0.5 min-w-0">
            <div class="flex items-center gap-2">
              <Icon name="globe" :class="geoEnabled ? 'text-primary' : 'text-outline'" class="text-[20px]" />
              <span class="text-title-3 font-title-3 text-on-surface">{{ t('ipa.geo.title') }}</span>
            </div>
            <span v-if="geoEnabled" class="text-caption-2 font-caption-2 text-outline">{{ t('ipa.geo.sub', { n: num(geoBuckets.length) }) }}</span>
          </div>
          <NotConnected v-if="!geoEnabled" :reason="t('ipa.geo.reason')" />
        </div>
        <div class="flex-1 min-h-[10rem]">
          <NotConnected v-if="!geoEnabled" variant="placeholder" :reason="t('ipa.geo.reason')" />
          <div v-else-if="geoBuckets.length" class="flex flex-col gap-space-md">
            <WorldThreatMap :buckets="geoBuckets" :gateway="gatewayLoc" />
            <ul class="flex flex-col gap-space-sm">
              <li v-for="g in geoBuckets" :key="g.country || 'unknown'" class="flex flex-col gap-1">
                <div class="flex items-center justify-between gap-space-sm">
                  <span class="flex items-center gap-1.5 min-w-0">
                    <span class="text-[16px] leading-none shrink-0">{{ isoToFlag(g.country) || '🏳️' }}</span>
                    <span class="text-subheadline font-subheadline text-on-surface truncate">{{ g.name || (g.country || t('ipa.dist.unknown')) }}</span>
                    <span v-if="g.country" class="text-code-badge font-code-badge text-outline mono shrink-0">{{ g.country }}</span>
                  </span>
                  <span class="flex items-center gap-2 shrink-0 text-caption-1 font-caption-1 mono">
                    <span class="text-on-surface">{{ num(g.total) }}</span>
                    <span v-if="g.blocked" class="text-error">{{ t('ipa.dist.blockedTag', { n: num(g.blocked) }) }}</span>
                  </span>
                </div>
                <span class="relative block h-1.5 bg-surface-container-highest rounded-full overflow-hidden">
                  <span class="absolute inset-y-0 left-0 rounded-full bg-primary/40" :style="{ width: barPct(g.total, geoMaxTotal) }"></span>
                  <span class="absolute inset-y-0 left-0 rounded-full bg-error" :style="{ width: barPct(g.blocked, geoMaxTotal) }"></span>
                </span>
              </li>
            </ul>
          </div>
          <div v-else class="flex flex-col items-center justify-center gap-1.5 py-space-xl px-space-md text-center h-full">
            <Icon name="globe" class="text-outline text-[24px]" />
            <span class="text-subheadline font-subheadline text-on-surface-variant">{{ t('ipa.geo.empty') }}</span>
          </div>
        </div>
      </div>
      <!-- 右列：实时封禁沙箱 + ASN 映射上下堆叠，两卡均分本列高度；本列经 items-stretch
           与左侧来源分布同高，故两张卡合起来正好与来源分布一致。 -->
      <div class="xl:col-span-5 flex flex-col gap-space-lg min-h-0">
        <!-- 实时封禁沙箱（bans[]，倒计时随每秒墙钟递减） -->
        <div class="flex-1 min-h-0 flex flex-col rounded-2xl bg-surface-container shadow-lg border border-hairline overflow-hidden">
          <div class="px-space-md py-space-sm border-b border-hairline flex items-center justify-between gap-space-xs">
            <div class="flex flex-col">
              <div class="flex items-center gap-space-xs">
                <Icon name="gavel" class="text-tertiary text-[16px]" />
                <span class="text-title-3 font-title-3 text-on-surface">{{ t('ipa.bans.title') }}</span>
              </div>
              <span class="text-caption-2 font-caption-2 text-outline">{{ t('ipa.bans.sub') }}</span>
            </div>
            <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-tertiary/15 text-tertiary whitespace-nowrap">
              {{ t('ipa.bans.count', { n: num(banCount) }) }}
            </span>
          </div>
          <div class="flex-1 min-h-0 overflow-auto divide-y divide-hairline">
            <div v-for="b in bans" :key="b.ip" class="flex items-center justify-between gap-space-sm px-space-md py-space-sm">
              <div class="flex flex-col min-w-0">
                <span class="flex items-center gap-1.5 min-w-0">
                  <span
                    v-if="isoToFlag(countryByIp.get(b.ip))"
                    class="text-[14px] leading-none shrink-0"
                  >{{ isoToFlag(countryByIp.get(b.ip)) }}</span>
                  <span class="font-code-body text-code-body text-on-surface mono truncate">{{ b.ip }}</span>
                </span>
                <span class="text-caption-2 font-caption-2 text-outline">{{ t('ipa.bans.until') }} · {{ untilText(b.until) }}</span>
              </div>
              <div class="flex flex-col items-end shrink-0">
                <span class="text-caption-2 font-caption-2 text-outline">{{ t('ipa.bans.remain') }}</span>
                <span class="text-subheadline font-subheadline mono" :class="remainSecOf(b.until) > 0 ? 'text-tertiary' : 'text-outline'">
                  {{ durationOf(remainSecOf(b.until)) }}
                </span>
              </div>
            </div>
            <div v-if="!bans.length" class="flex flex-col items-center justify-center gap-1.5 py-space-xl px-space-md text-center">
              <Icon name="shield-check" class="text-secondary text-[24px]" />
              <span class="text-subheadline font-subheadline text-on-surface-variant">{{ t('ipa.bans.empty') }}</span>
              <span class="text-caption-2 font-caption-2 text-outline">{{ t('ipa.bans.emptyHint') }}</span>
            </div>
          </div>
        </div>
        <!-- 攻击特征与 ASN 映射 -->
        <div class="flex-1 min-h-0 flex flex-col gap-space-sm p-space-lg rounded-2xl bg-surface-container shadow-md border border-hairline">
          <div class="flex items-center justify-between gap-space-sm">
            <div class="flex flex-col gap-0.5 min-w-0">
              <div class="flex items-center gap-2">
                <Icon name="hub" :class="asnEnabled ? 'text-primary' : 'text-outline'" class="text-[20px]" />
                <span class="text-title-3 font-title-3 text-on-surface">{{ t('ipa.asn.title') }}</span>
              </div>
              <span v-if="asnEnabled" class="text-caption-2 font-caption-2 text-outline">{{ t('ipa.asn.sub', { n: num(asnBuckets.length) }) }}</span>
            </div>
            <NotConnected v-if="!asnEnabled" :reason="t('ipa.asn.reason')" />
          </div>
          <div class="flex-1 min-h-0 overflow-auto">
            <NotConnected v-if="!asnEnabled" variant="placeholder" :reason="t('ipa.asn.reason')" />
            <ul v-else-if="asnBuckets.length" class="flex flex-col gap-space-sm">
              <li v-for="a in asnBuckets" :key="a.asn" class="flex flex-col gap-1">
                <div class="flex items-center justify-between gap-space-sm">
                  <span class="flex items-center gap-1.5 min-w-0">
                    <span class="text-subheadline font-subheadline text-on-surface truncate">{{ asnLabel(a) }}</span>
                    <span v-if="a.asn" class="text-code-badge font-code-badge text-outline mono shrink-0">AS{{ a.asn }}</span>
                  </span>
                  <span class="flex items-center gap-2 shrink-0 text-caption-1 font-caption-1 mono">
                    <span class="text-on-surface">{{ num(a.total) }}</span>
                    <span v-if="a.blocked" class="text-error">{{ t('ipa.dist.blockedTag', { n: num(a.blocked) }) }}</span>
                  </span>
                </div>
                <span class="relative block h-1.5 bg-surface-container-highest rounded-full overflow-hidden">
                  <span class="absolute inset-y-0 left-0 rounded-full bg-primary/40" :style="{ width: barPct(a.total, asnMaxTotal) }"></span>
                  <span class="absolute inset-y-0 left-0 rounded-full bg-error" :style="{ width: barPct(a.blocked, asnMaxTotal) }"></span>
                </span>
              </li>
            </ul>
            <div v-else class="flex flex-col items-center justify-center gap-1.5 py-space-xl px-space-md text-center h-full">
              <Icon name="hub" class="text-outline text-[24px]" />
              <span class="text-subheadline font-subheadline text-on-surface-variant">{{ t('ipa.asn.empty') }}</span>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- 封禁规则池与黑白名单管理（GET/POST/DELETE /api/ip-rules，持久化于 SQLite） -->
    <section class="flex flex-col gap-space-md p-space-lg rounded-2xl bg-surface-container shadow-md border border-hairline">
      <div class="flex flex-col gap-1">
        <div class="flex items-center gap-2 flex-wrap">
          <Icon name="rule" class="text-primary text-[20px]" />
          <span class="text-title-3 font-title-3 text-on-surface">{{ t('ipa.manage.title') }}</span>
          <HintTip :text="t('ipa.manage.note')" />
        </div>
        <p class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('ipa.manage.sub') }}</p>
      </div>
      <form class="grid grid-cols-1 md:grid-cols-12 gap-space-sm items-end" @submit.prevent="addRule">
        <label class="md:col-span-4 flex flex-col gap-1">
          <span class="text-caption-2 font-caption-2 text-on-surface-variant">{{ t('ipa.manage.pattern') }}</span>
          <input
            v-model="newPattern"
            type="text"
            spellcheck="false"
            :placeholder="t('ipa.manage.patternPlaceholder')"
            class="w-full px-3 py-2 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
          />
        </label>
        <label class="md:col-span-2 flex flex-col gap-1">
          <span class="text-caption-2 font-caption-2 text-on-surface-variant">{{ t('ipa.manage.kind') }}</span>
          <SelectMenu
            :model-value="newKind"
            :options="kindOptions"
            :aria-label="t('ipa.manage.kind')"
            @update:model-value="setKind"
          />
        </label>
        <label class="md:col-span-2 flex flex-col gap-1" :class="newKind === 'allow' ? 'opacity-40' : ''">
          <span class="text-caption-2 font-caption-2 text-on-surface-variant">{{ t('ipa.manage.duration') }}</span>
          <SelectMenu
            :model-value="newDuration"
            :options="durationOptions"
            :disabled="newKind === 'allow'"
            :aria-label="t('ipa.manage.duration')"
            @update:model-value="setDuration"
          />
        </label>
        <label class="md:col-span-3 flex flex-col gap-1">
          <span class="text-caption-2 font-caption-2 text-on-surface-variant">{{ t('ipa.manage.reasonLabel') }}</span>
          <input
            v-model="newReason"
            type="text"
            :placeholder="t('ipa.manage.reasonPlaceholder')"
            class="w-full px-3 py-2 rounded-xl bg-surface-container-high text-on-surface text-subheadline font-subheadline focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
          />
        </label>
        <button
          type="submit"
          :disabled="submitting || !newPattern.trim()"
          class="md:col-span-1 inline-flex items-center justify-center gap-1 px-3 py-2 rounded-xl bg-primary-container hover:bg-primary-container/90 text-on-primary-container text-subheadline font-subheadline font-semibold shadow-md transition-all active:scale-[0.98] disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <Icon name="plus" class="text-[16px]" />
          <span class="whitespace-nowrap">{{ submitting ? t('ipa.manage.submitting') : t('ipa.manage.submit') }}</span>
        </button>
      </form>
      <span v-if="rulesError" class="text-caption-1 font-caption-1 text-error">{{ t('ipa.manage.error', { msg: rulesError }) }}</span>
      <div class="flex items-center justify-between">
        <span class="text-subheadline font-subheadline text-on-surface">{{ t('ipa.manage.listTitle') }}</span>
        <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-surface-bright text-on-surface-variant">{{ t('ipa.manage.count', { n: num(rules.length) }) }}</span>
      </div>
      <div class="overflow-auto rounded-xl border border-hairline">
        <table class="w-full min-w-[40rem] text-left border-collapse">
          <thead class="bg-surface-container-high">
            <tr class="text-caption-2 font-caption-2 text-on-surface-variant">
              <th class="px-space-sm py-2 font-medium whitespace-nowrap">{{ t('ipa.manage.colPattern') }}</th>
              <th class="px-space-sm py-2 font-medium whitespace-nowrap">{{ t('ipa.manage.colKind') }}</th>
              <th class="px-space-sm py-2 font-medium whitespace-nowrap">{{ t('ipa.manage.colExpiry') }}</th>
              <th class="px-space-sm py-2 font-medium">{{ t('ipa.manage.colReason') }}</th>
              <th class="px-space-sm py-2 font-medium text-right">{{ t('ipa.col.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in rules" :key="r.id" class="border-t border-hairline align-top">
              <td class="px-space-sm py-2 whitespace-nowrap">
                <span class="font-code-body text-code-body text-on-surface mono">{{ r.pattern }}</span>
                <span v-if="r.is_cidr" class="ml-1 inline-flex items-center px-1.5 py-0.5 rounded text-code-badge font-code-badge bg-surface-bright text-outline">{{ t('ipa.manage.cidrTag') }}</span>
              </td>
              <td class="px-space-sm py-2 whitespace-nowrap">
                <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge" :class="r.kind === 'allow' ? 'bg-secondary/15 text-secondary' : 'bg-error-container text-error'">
                  {{ r.kind === 'allow' ? t('ipa.manage.badgeAllow') : t('ipa.manage.badgeBlock') }}
                </span>
              </td>
              <td class="px-space-sm py-2 whitespace-nowrap">
                <template v-if="ruleExpiryMs(r) === null">
                  <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('ipa.manage.permanent') }}</span>
                </template>
                <template v-else>
                  <span class="text-caption-1 font-caption-1 mono" :class="remainSecOf(ruleExpiryMs(r) ?? 0) > 0 ? 'text-on-surface-variant' : 'text-outline'">
                    {{ remainSecOf(ruleExpiryMs(r) ?? 0) > 0 ? t('ipa.manage.remain', { v: durationOf(remainSecOf(ruleExpiryMs(r) ?? 0)) }) : t('ipa.manage.expired') }}
                  </span>
                  <span class="block text-caption-2 font-caption-2 text-outline">{{ t('ipa.manage.expiresAt', { v: untilText(ruleExpiryMs(r) ?? 0) }) }}</span>
                </template>
              </td>
              <td class="px-space-sm py-2">
                <span class="text-caption-1 font-caption-1 text-on-surface-variant line-clamp-2 break-all">{{ r.reason || '-' }}</span>
              </td>
              <td class="px-space-sm py-2 text-right">
                <button
                  type="button"
                  class="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-lg bg-surface-container-high hover:bg-error-container text-caption-2 font-caption-2 text-on-surface hover:text-error transition-colors"
                  @click="removeRule(r)"
                >
                  <Icon name="trash" class="text-[14px]" />
                  <span>{{ t('ipa.manage.delete') }}</span>
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="!rules.length" class="flex flex-col items-center justify-center gap-1.5 py-space-lg px-space-md text-center">
          <Icon name="rule" class="text-outline text-[24px]" />
          <span class="text-subheadline font-subheadline text-on-surface-variant">{{ t('ipa.manage.empty') }}</span>
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('ipa.manage.emptyHint') }}</span>
        </div>
      </div>
    </section>
  </div>
</template>





