<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import Icon from '../../components/Icon.vue'
import NotConnected from '../../components/NotConnected.vue'
import SelectMenu from '../../components/SelectMenu.vue'
import * as api from '../../api'
import { useConsole } from '../../console'
import { clockOf, dateTimeOf, decisionStyle, methodClass, num, score, scoreWidth } from '../../format'
import { useI18n } from '../../i18n'
import type { MessageKey } from '../../i18n/zh'
import type { LogEntry, ModelCount } from '../../types'

/**
 * 转发审计记录。
 *
 * 数据全部来自既有 `GET /api/logs`（参数语义：limit / offset / decision / model / path /
 * ip / q / since，响应 `{items,total}`）、`GET /api/logs/models`（模型下拉选项）与
 * `GET /api/state` 带回的 `stats24h`。
 *
 * 三个文本条件的匹配语义各不相同，与后端一致：模型为**精确**匹配（取值来自下拉，
 * 精确才不会把 gpt-4o-mini 一起带出来）、来源 IP 为**前缀**匹配（完整地址与
 * `194.26.*` 都可用）、请求路径为**子串**匹配。
 *
 * 已落地的原型能力：处置入口「加入黑名单」（`POST /api/ip-rules`，见 `banIp`）、
 * 全局请求唯一 ID 与耗时分解（`trace_id` 与 `inspect_ms` / `upstream_ms`，见详情抽屉）、
 * CSV 导出（`GET /api/logs/export`，走与列表完全相同的筛选条件）。
 * 仍然没有后端来源的是：地理位置（只落原始 IP，归属地在 IP 风险分析页离线解析）、
 * Token 估算与风险级、处置规则矩阵、完整原始请求体、重放测试——这些不显示无来源
 * 数值，也不提供无后端支撑的操作。
 *
 * 所有字段经 Vue 模板插值渲染，默认转义，等价于原实现的 `escapeHtml`，可防 XSS。
 */
const { settings, stats24h } = useConsole()
const { t } = useI18n()
const route = useRoute()

/**
 * 送检摘要是否落盘。关闭时后端写入的 `snippet` 恒为空串，界面用占位符说明，
 * 而不是显示成与「没抽到可送检内容」无法区分的 `-`；开关打开期间写入的旧记录
 * 照常有值、照常显示。
 */
const snippetRecorded = computed(() => settings.value.record_snippet)

/** 关闭摘要记录时，审计列表与详情中代替摘要的占位文本。 */
const snippetPlaceholder = computed(() => t('audit.snippetOff'))

const PAGE_SIZE = 50

/** 时间范围控件：换算为 `/api/logs` 的 `since`（unix 毫秒下界）。 */
const SINCE_OPTIONS: { labelKey: MessageKey; ms: number }[] = [
  { labelKey: 'audit.since.all', ms: 0 },
  { labelKey: 'audit.since.1h', ms: 3600_000 },
  { labelKey: 'audit.since.6h', ms: 6 * 3600_000 },
  { labelKey: 'audit.since.24h', ms: 24 * 3600_000 },
  { labelKey: 'audit.since.7d', ms: 7 * 86400_000 },
]

/**
 * 判定状态控件：`value` 取值与后端 `decision` 完全一致，`code` 是机器码（不随语言变化），
 * 界面显示的是按当前语言取到的 `labelKey`。`code` 在筛选条件的副标题里保留，便于与日志对照。
 */
const DECISIONS: { value: string; labelKey: MessageKey; code: string }[] = [
  { value: '', labelKey: 'decision.all', code: 'ALL' },
  { value: 'allow', labelKey: 'decision.allow', code: 'ALLOWED' },
  { value: 'block', labelKey: 'decision.block', code: 'BLOCKED' },
  { value: 'skip', labelKey: 'decision.skip', code: 'SKIPPED' },
  { value: 'error', labelKey: 'decision.error', code: 'ERROR' },
]

const decision = ref('')
const model = ref('')
const path = ref('')
// 支持从其它页面（如 IP 风险分析）带 ?ip= 深链接进入，预置来源 IP 前缀筛选。
// 仅初始化 ref，不会触发筛选 watch；onMounted 的首次 load() 会读到该预置值。
const ip = ref(typeof route.query.ip === 'string' ? route.query.ip : '')
const q = ref('')
const sinceIdx = ref(0)
const offset = ref(0)

/** 模型下拉的选项：`GET /api/logs/models` 返回的真实取值与次数。 */
const modelOptions = ref<ModelCount[]>([])

const items = ref<LogEntry[]>([])
const total = ref(0)
const loading = ref(false)
const loadError = ref('')
const selected = ref<LogEntry | null>(null)

/** 处置（一键封禁）的反馈。列表卡头与详情抽屉共用同一条，两处入口都能看到结果。 */
const actionMsg = ref<{ text: string; ok: boolean } | null>(null)

const pages = computed(() => Math.max(1, Math.ceil(total.value / PAGE_SIZE)))
const page = computed(() => Math.floor(offset.value / PAGE_SIZE) + 1)
const canPrev = computed(() => offset.value > 0)
const canNext = computed(() => total.value > 0 && offset.value + PAGE_SIZE < total.value)

const sinceMs = computed(() => {
  const o = SINCE_OPTIONS[sinceIdx.value]
  return o.ms > 0 ? Date.now() - o.ms : 0
})

async function load(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    const res = await api.queryLogs(currentFilter())
    items.value = res.items ?? []
    total.value = res.total ?? 0
    // 记录在翻页途中被过滤/清理时，回到最后一个有效页。
    if (!items.value.length && offset.value > 0 && total.value > 0) {
      offset.value = Math.max(0, (Math.ceil(total.value / PAGE_SIZE) - 1) * PAGE_SIZE)
      await load()
    }
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
    items.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

/**
 * 当前筛选条件，列表查询与 CSV 导出共用同一套取值——导出的必须是屏幕上正在看的
 * 那批记录，两处各拼一份参数迟早会漂移。
 */
function currentFilter(): api.LogFilter {
  return {
    limit: PAGE_SIZE,
    offset: offset.value,
    decision: decision.value,
    model: model.value.trim(),
    path: path.value.trim(),
    ip: ip.value.trim(),
    q: q.value.trim(),
    since: sinceMs.value,
  }
}

/**
 * 导出当前筛选条件下的整份流水（不是当前页）。`limit` / `offset` 不参与导出。
 *
 * 触发方式交给浏览器：后端回 `Content-Disposition: attachment`，浏览器按它命名落盘，
 * 所以不需要（也不应该）先把几十万行读进前端内存再自己造 Blob。`download` 属性置空串
 * 表示「文件名以后端给的为准」。
 */
function exportCsv(): void {
  const a = document.createElement('a')
  a.href = api.logsExportURL(currentFilter())
  a.download = ''
  document.body.appendChild(a)
  a.click()
  a.remove()
}

/** 任一筛选控件变化：立即回到第 1 页，并对连续输入做 300ms 防抖。 */
let debounceTimer: number | undefined
function onFilterChange(): void {
  offset.value = 0
  if (debounceTimer !== undefined) window.clearTimeout(debounceTimer)
  debounceTimer = window.setTimeout(() => void load(), 300)
}

watch([decision, sinceIdx, model, path, ip, q], onFilterChange)

/**
 * 拉取模型下拉的选项。失败时不阻塞列表：下拉退化为只剩「全部模型」与当前选中项，
 * 而不是编造选项——控制台只显示有来源的值。
 */
async function loadModels(): Promise<void> {
  try {
    const res = await api.queryLogModels(sinceMs.value)
    modelOptions.value = res.items ?? []
  } catch {
    modelOptions.value = []
  }
}

/**
 * 选项只受时间窗影响（与其他筛选条件无关，否则选中一个模型后下拉里就只剩它自己），
 * 因此只跟时间窗联动重取。
 */
watch(sinceIdx, () => void loadModels())

/**
 * 真正渲染的选项。若当前选中的模型已不在列表里（时间窗变化或该模型记录被清理），
 * 仍保留它并标注 `count === 0`，否则原生 select 会显示成空选项、而筛选条件其实
 * 还在生效——界面不能显示与实际过滤不一致的状态。聚合只返回 count ≥ 1 的模型，
 * 所以 count 为 0 唯一标识这条兜底选项。
 */
const modelChoices = computed<ModelCount[]>(() => {
  const cur = model.value.trim()
  if (!cur || modelOptions.value.some((m) => m.value === cur)) return modelOptions.value
  return [{ value: cur, count: 0 }, ...modelOptions.value]
})

/** 模型筛选下拉的选项（SelectMenu 自绘以对齐控制台风格）：首项为「全部模型」，其余带最近计数。 */
const modelSelectOptions = computed(() => [
  { value: '', label: t('audit.filter.allModels') },
  ...modelChoices.value.map((m) => ({
    value: m.value,
    label: m.count > 0 ? `${m.value} (${num(m.count)})` : t('audit.filter.noRecent', { model: m.value }),
  })),
])

function go(delta: number): void {
  const next = offset.value + delta * PAGE_SIZE
  if (next < 0 || next >= total.value) return
  offset.value = next
  void load()
}

function refresh(): void {
  void load()
  void loadModels()
}

// --- 活跃筛选条件 ---

interface Chip {
  label: string
  clear: () => void
}

const chips = computed<Chip[]>(() => {
  const out: Chip[] = []
  if (decision.value) {
    const d = DECISIONS.find((x) => x.value === decision.value)
    const name = d ? t(d.labelKey) : decision.value
    out.push({ label: t('audit.chip.decision', { v: name }), clear: () => (decision.value = '') })
  }
  if (model.value.trim()) out.push({ label: t('audit.chip.model', { v: model.value.trim() }), clear: () => (model.value = '') })
  if (ip.value.trim()) out.push({ label: t('audit.chip.ip', { v: ip.value.trim() }), clear: () => (ip.value = '') })
  if (path.value.trim()) out.push({ label: t('audit.chip.path', { v: path.value.trim() }), clear: () => (path.value = '') })
  if (q.value.trim()) out.push({ label: t('audit.chip.q', { v: q.value.trim() }), clear: () => (q.value = '') })
  if (sinceIdx.value > 0) {
    out.push({
      label: t('audit.chip.since', { v: t(SINCE_OPTIONS[sinceIdx.value].labelKey) }),
      clear: () => (sinceIdx.value = 0),
    })
  }
  return out
})

function clearAll(): void {
  decision.value = ''
  model.value = ''
  ip.value = ''
  path.value = ''
  q.value = ''
  sinceIdx.value = 0
}

// --- 详情抽屉 ---

function openDetail(e: LogEntry): void {
  selected.value = e
}

function closeDetail(): void {
  selected.value = null
}

function onKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape' && selected.value) closeDetail()
}

// --- 处置 ---

/** 反馈消息的自动消退计时器；卸载时清掉，避免在已销毁的组件上写状态。 */
let actionTimer: number | undefined

function flashAction(text: string, ok: boolean): void {
  if (actionTimer !== undefined) window.clearTimeout(actionTimer)
  actionMsg.value = { text, ok }
  actionTimer = window.setTimeout(() => (actionMsg.value = null), 6000)
}

/**
 * 一键封禁来源 IP：固定 1 小时临时封禁，与「IP 风险分析」排行表的快捷封禁同语义。
 * 后端 `POST /api/ip-rules` 对同一 pattern+kind 是刷新而非重复插入，重复点击不会堆规则。
 *
 * 永久封禁与自定义时长不在这一页做——规则池（IP 风险分析页）是权威视图，
 * 且在列表里判断「该 IP 是否已在封禁中」需要 CIDR 匹配，前端做不可靠，故不做状态标记。
 */
async function banIp(ip: string): Promise<void> {
  const target = ip.trim()
  if (!target) return
  if (!window.confirm(t('audit.action.banConfirm', { ip: target }))) return
  actionMsg.value = null
  try {
    await api.addIpRule({
      pattern: target,
      kind: 'block',
      reason: t('audit.action.banReason'),
      permanent: false,
      duration_sec: 3600,
    })
    flashAction(t('audit.action.banOk', { ip: target }), true)
  } catch (e) {
    flashAction(t('audit.action.banFail', { msg: e instanceof Error ? e.message : String(e) }), false)
  }
}

onMounted(() => {
  void load()
  void loadModels()
  window.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => {
  if (debounceTimer !== undefined) window.clearTimeout(debounceTimer)
  if (actionTimer !== undefined) window.clearTimeout(actionTimer)
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div class="w-full px-margin-mobile py-margin-mobile md:px-margin md:py-margin flex flex-col gap-space-lg">
    <!-- 页头 -->
    <div class="flex flex-col lg:flex-row lg:items-end justify-between gap-space-md">
      <div class="flex flex-col gap-1">
        <h1 class="text-title-2 font-title-2 text-on-surface tracking-tight">{{ t('audit.title') }}</h1>
        <p class="text-subheadline font-subheadline text-on-surface-variant">
          {{ t('audit.sub') }}
        </p>
      </div>
      <div class="flex items-center gap-space-sm">
        <button
          type="button"
          class="inline-flex items-center gap-1.5 px-3 py-2 rounded-xl bg-surface-container hover:bg-surface-container-high text-subheadline font-subheadline text-on-surface transition-all"
          @click="exportCsv"
        >
          <Icon name="download" class="text-[16px]" />
          <span>{{ t('audit.export') }}</span>
        </button>
        <button
          type="button"
          class="inline-flex items-center gap-1.5 px-3 py-2 rounded-xl bg-surface-container hover:bg-surface-container-high text-subheadline font-subheadline text-on-surface transition-all disabled:opacity-50"
          :disabled="!chips.length"
          @click="clearAll"
        >
          <Icon name="filter-off" class="text-[16px]" />
          <span>{{ t('audit.clearFilters') }}</span>
        </button>
        <button
          type="button"
          class="inline-flex items-center gap-1.5 px-3 py-2 rounded-xl bg-surface-container hover:bg-surface-container-high text-subheadline font-subheadline text-on-surface transition-all"
          :disabled="loading"
          @click="refresh"
        >
          <Icon name="refresh" class="text-[16px]" :class="loading ? 'animate-spin' : ''" />
          <span>{{ t('audit.refresh') }}</span>
        </button>
      </div>
    </div>

    <!-- 24 小时真实计数（来源 GET /api/state 的 stats24h） -->
    <section class="grid grid-cols-2 lg:grid-cols-4 gap-space-md">
      <div class="flex flex-col p-space-sm rounded-2xl bg-surface-container shadow-sm border border-hairline">
        <span class="eyebrow">{{ t('audit.stat.total') }}</span>
        <span class="text-title-3 font-title-3 text-on-surface mono">{{ num(stats24h.total) }}</span>
      </div>
      <div class="flex flex-col p-space-sm rounded-2xl bg-surface-container shadow-sm border border-hairline">
        <span class="eyebrow">{{ t('audit.stat.allowed') }}</span>
        <span class="text-title-3 font-title-3 text-secondary mono">{{ num(stats24h.allowed) }}</span>
      </div>
      <div class="flex flex-col p-space-sm rounded-2xl bg-surface-container shadow-sm border border-hairline">
        <span class="eyebrow">{{ t('audit.stat.blocked') }}</span>
        <span class="text-title-3 font-title-3 text-error mono">{{ num(stats24h.blocked) }}</span>
      </div>
      <div class="flex flex-col p-space-sm rounded-2xl bg-surface-container shadow-sm border border-hairline">
        <span class="eyebrow">{{ t('audit.stat.skipped') }}</span>
        <span class="text-title-3 font-title-3 text-on-surface-variant mono">{{ num(stats24h.skipped) }}</span>
      </div>
    </section>

    <!-- 筛选面板 -->
    <section class="flex flex-col gap-space-md p-space-md rounded-2xl bg-surface-container shadow-md border border-hairline">
      <div class="flex flex-col xl:flex-row xl:items-center gap-space-md flex-wrap">
        <div class="flex items-center gap-space-sm flex-wrap">
          <span class="eyebrow shrink-0">{{ t('audit.filter.decision') }}</span>
          <div class="inline-flex p-0.5 bg-surface-container-high rounded-lg flex-wrap">
            <button
              v-for="d in DECISIONS"
              :key="d.value"
              type="button"
              class="px-3 py-1 rounded-md text-caption-2 font-caption-2 transition-all"
              :class="decision === d.value ? 'bg-surface-bright text-on-surface shadow-sm font-semibold' : 'text-on-surface-variant hover:text-on-surface'"
              :title="d.code"
              @click="decision = d.value"
            >
              {{ t(d.labelKey) }}
            </button>
          </div>
        </div>

        <div class="flex items-center gap-space-sm flex-wrap">
          <span class="eyebrow shrink-0">{{ t('audit.filter.since') }}</span>
          <div class="inline-flex p-0.5 bg-surface-container-high rounded-lg flex-wrap">
            <button
              v-for="(o, i) in SINCE_OPTIONS"
              :key="o.labelKey"
              type="button"
              class="px-3 py-1 rounded-md text-caption-2 font-caption-2 transition-all"
              :class="sinceIdx === i ? 'bg-surface-bright text-on-surface shadow-sm font-semibold' : 'text-on-surface-variant hover:text-on-surface'"
              @click="sinceIdx = i"
            >
              {{ t(o.labelKey) }}
            </button>
          </div>
        </div>
      </div>

      <!-- 四个条件按原型顺序：来源 IP → 目标模型 → 请求路径 → 关键词 -->
      <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-space-sm">
        <label class="flex flex-col gap-1.5">
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('audit.filter.ip') }}</span>
          <input
            v-model="ip"
            type="text"
            :placeholder="t('audit.filter.ipPlaceholder')"
            spellcheck="false"
            class="w-full px-3 py-2 rounded-xl bg-surface-container-high text-on-surface text-subheadline font-subheadline focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset mono"
          />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('audit.filter.model') }}</span>
          <SelectMenu
            v-model="model"
            :options="modelSelectOptions"
            mono
            :aria-label="t('audit.filter.model')"
          />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('audit.filter.path') }}</span>
          <input
            v-model="path"
            type="text"
            :placeholder="t('audit.filter.pathPlaceholder')"
            spellcheck="false"
            class="w-full px-3 py-2 rounded-xl bg-surface-container-high text-on-surface text-subheadline font-subheadline focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset mono"
          />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('audit.filter.q', { snippet: snippetRecorded ? t('audit.filter.qSnippet') : '' }) }}</span>
          <input
            v-model="q"
            type="text"
            :placeholder="t('audit.filter.qPlaceholder')"
            spellcheck="false"
            class="w-full px-3 py-2 rounded-xl bg-surface-container-high text-on-surface text-subheadline font-subheadline focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
          />
        </label>
      </div>

      <div v-if="chips.length" class="flex items-center gap-space-xs flex-wrap">
        <span class="eyebrow">{{ t('audit.chips.title') }}</span>
        <button
          v-for="c in chips"
          :key="c.label"
          type="button"
          class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-surface-container-high hover:bg-surface-container-highest text-caption-2 font-caption-2 text-on-surface-variant transition-colors"
          @click="c.clear"
        >
          <span class="max-w-[16rem] truncate">{{ c.label }}</span>
          <Icon name="x" class="text-[12px]" />
        </button>
        <button type="button" class="text-caption-2 font-caption-2 text-primary hover:underline" @click="clearAll">
          {{ t('audit.chips.clearAll') }}
        </button>
      </div>
    </section>

    <!-- 列表 -->
    <section class="flex flex-col rounded-2xl bg-surface-container shadow-lg border border-hairline overflow-hidden">
      <div class="px-space-md py-space-sm border-b border-hairline flex items-center justify-between flex-wrap gap-space-xs">
        <div class="flex items-center gap-space-sm">
          <span class="text-subheadline font-subheadline text-on-surface">{{ t('audit.stream') }}</span>
          <span v-if="loading" class="text-caption-2 font-caption-2 text-outline">{{ t('audit.querying') }}</span>
        </div>
        <div class="flex items-center gap-space-sm">
          <span
            v-if="actionMsg"
            class="inline-flex items-center gap-1 text-caption-1 font-caption-1"
            :class="actionMsg.ok ? 'text-secondary' : 'text-error'"
          >
            <Icon :name="actionMsg.ok ? 'check-circle' : 'alert-circle'" class="text-[14px]" />
            {{ actionMsg.text }}
          </span>
          <span v-if="loadError" class="text-caption-1 font-caption-1 text-error">{{ loadError }}</span>
          <span v-else class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('audit.totalRecords', { n: num(total) }) }}</span>
        </div>
      </div>

      <!-- 容器最大高度 + 内部纵向滚动 + 表头吸顶 -->
      <div class="max-h-[32rem] overflow-auto">
        <table class="w-full min-w-[62rem] text-left border-collapse">
          <thead class="sticky top-0 z-10 bg-surface-container-high">
            <tr class="text-caption-2 font-caption-2 text-on-surface-variant">
              <th class="px-space-sm py-2.5 font-medium whitespace-nowrap">{{ t('audit.col.time') }}</th>
              <th class="px-space-sm py-2.5 font-medium whitespace-nowrap">{{ t('audit.col.path') }}</th>
              <th class="px-space-sm py-2.5 font-medium whitespace-nowrap">{{ t('audit.col.model') }}</th>
              <th class="px-space-sm py-2.5 font-medium whitespace-nowrap">{{ t('audit.col.decision') }}</th>
              <th class="px-space-sm py-2.5 font-medium whitespace-nowrap">{{ t('audit.col.score') }}</th>
              <th class="px-space-sm py-2.5 font-medium whitespace-nowrap">{{ t('audit.col.latency') }}</th>
              <th class="px-space-sm py-2.5 font-medium whitespace-nowrap">{{ t('audit.col.ip') }}</th>
              <th class="px-space-sm py-2.5 font-medium">{{ t('audit.col.reason') }}</th>
              <th class="px-space-sm py-2.5 font-medium min-w-[14rem]">{{ t('audit.col.snippet') }}</th>
              <th class="px-space-sm py-2.5"><span class="sr-only">{{ t('audit.col.actions') }}</span></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="e in items"
              :key="e.id"
              class="border-t border-hairline hover:bg-surface-container-high/60 cursor-pointer transition-colors align-top"
              @click="openDetail(e)"
            >
              <td class="px-space-sm py-2.5 whitespace-nowrap">
                <span class="font-code-body text-code-body text-on-surface mono">{{ clockOf(e.ts) }}</span>
                <span class="block text-caption-2 font-caption-2 text-outline mono">#{{ e.id }}</span>
              </td>
              <td class="px-space-sm py-2.5">
                <span class="inline-flex px-1.5 py-0.5 rounded text-code-badge font-code-badge mono" :class="methodClass(e.method)">
                  {{ e.method }}
                </span>
                <span class="block font-code-body text-code-body text-on-surface-variant mono truncate max-w-[18rem]" :title="e.path">
                  {{ e.path }}
                </span>
              </td>
              <td class="px-space-sm py-2.5">
                <span class="font-code-body text-code-body text-on-surface-variant mono">{{ e.model || '-' }}</span>
              </td>
              <td class="px-space-sm py-2.5 whitespace-nowrap">
                <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-code-badge font-code-badge" :class="decisionStyle(e.decision).badge">
                  <span class="h-1.5 w-1.5 rounded-full" :class="decisionStyle(e.decision).dot"></span>
                  <span>{{ decisionStyle(e.decision).label }}</span>
                </span>
                <span class="block text-caption-2 font-caption-2 text-outline mono">{{ decisionStyle(e.decision).code }}</span>
              </td>
              <td class="px-space-sm py-2.5">
                <span class="font-code-body text-code-body mono" :class="decisionStyle(e.decision).text">{{ score(e.score) }}</span>
                <span class="block mt-1 w-14 h-1 bg-surface-container-highest rounded-full overflow-hidden">
                  <span class="block h-1 rounded-full" :class="decisionStyle(e.decision).bar" :style="{ width: scoreWidth(e.score) + '%' }"></span>
                </span>
              </td>
              <td class="px-space-sm py-2.5 whitespace-nowrap">
                <span class="font-code-body text-code-body text-on-surface-variant mono">{{ e.latency_ms }} ms</span>
              </td>
              <td class="px-space-sm py-2.5 whitespace-nowrap">
                <span class="font-code-body text-code-body text-on-surface-variant mono">{{ e.ip || '-' }}</span>
              </td>
              <td class="px-space-sm py-2.5">
                <span class="text-caption-1 font-caption-1 text-on-surface-variant line-clamp-2">{{ e.reason || '-' }}</span>
              </td>
              <td class="px-space-sm py-2.5">
                <span class="text-caption-1 font-caption-1 text-on-surface-variant line-clamp-2 break-all">{{ e.snippet || (snippetRecorded ? '-' : snippetPlaceholder) }}</span>
              </td>
              <td class="px-space-sm py-2.5 text-right">
                <span class="inline-flex items-center gap-1">
                  <!-- 该行本身可点开详情，按钮必须 stop，否则点封禁会顺带打开抽屉。
                       没有来源 IP 的记录（如本地/健康检查流量）不给入口。 -->
                  <button
                    v-if="e.ip"
                    type="button"
                    class="inline-flex items-center justify-center p-1 rounded-lg text-error hover:bg-error-container transition-colors"
                    :title="t('audit.action.ban', { ip: e.ip })"
                    :aria-label="t('audit.action.ban', { ip: e.ip })"
                    @click.stop="banIp(e.ip)"
                  >
                    <Icon name="ban" class="text-[15px]" />
                  </button>
                  <Icon name="chevron-right" class="text-outline text-[16px]" />
                </span>
              </td>
            </tr>
          </tbody>
        </table>

        <div v-if="!items.length && !loading" class="flex flex-col items-center justify-center gap-1.5 py-space-xl">
          <Icon name="search" class="text-outline text-[24px]" />
          <span class="text-subheadline font-subheadline text-on-surface-variant">{{ t('audit.empty') }}</span>
          <span class="text-caption-2 font-caption-2 text-outline">
            {{ chips.length ? t('audit.emptyFiltered') : t('audit.emptyNone') }}
          </span>
        </div>
      </div>

      <!-- 服务端分页 -->
      <div class="px-space-md py-space-sm border-t border-hairline flex items-center justify-between flex-wrap gap-space-sm">
        <span class="text-caption-1 font-caption-1 text-on-surface-variant">
          {{ items.length ? t('audit.paging.showing', { from: num(offset + 1), to: num(offset + items.length), total: num(total) }) : t('audit.totalRecords', { n: num(total) }) }}
          <span class="text-outline">{{ t('audit.paging.perPage', { n: PAGE_SIZE }) }}</span>
        </span>
        <div class="flex items-center gap-space-sm">
          <button
            type="button"
            class="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-surface-container-high hover:bg-surface-container-highest text-caption-1 font-caption-1 text-on-surface transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
            :disabled="!canPrev || loading"
            @click="go(-1)"
          >
            <Icon name="chevron-left" class="text-[14px]" />
            <span>{{ t('audit.prev') }}</span>
          </button>
          <span class="text-caption-1 font-caption-1 text-on-surface-variant whitespace-nowrap">
            {{ t('audit.paging.pageOf', { page, pages }) }}
          </span>
          <button
            type="button"
            class="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-surface-container-high hover:bg-surface-container-highest text-caption-1 font-caption-1 text-on-surface transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
            :disabled="!canNext || loading"
            @click="go(1)"
          >
            <span>{{ t('audit.next') }}</span>
            <Icon name="chevron-right" class="text-[14px]" />
          </button>
        </div>
      </div>
    </section>

    <!-- 详情抽屉 -->
    <Transition
      enter-active-class="transition-transform duration-200 ease-out"
      enter-from-class="translate-x-full"
      leave-active-class="transition-transform duration-150 ease-in"
      leave-to-class="translate-x-full"
    >
      <aside
        v-if="selected"
        class="fixed top-16 right-0 bottom-0 w-[420px] max-w-full z-40 bg-surface-container-low border-l border-hairline shadow-overlay flex flex-col"
      >
        <div class="px-space-md py-space-sm border-b border-hairline flex items-center justify-between gap-space-sm">
          <div class="flex items-center gap-space-xs min-w-0">
            <Icon name="search-check" class="text-primary text-[18px] shrink-0" />
            <div class="flex flex-col min-w-0">
              <span class="text-headline font-headline text-on-surface">{{ t('audit.detail.title') }}</span>
              <span class="text-caption-2 font-caption-2 text-outline mono truncate">#{{ selected.id }} · {{ selected.path }}</span>
            </div>
          </div>
          <button
            type="button"
            class="p-1 rounded-lg text-on-surface-variant hover:bg-surface-container-high transition-colors shrink-0"
            :aria-label="t('audit.detail.close')"
            @click="closeDetail"
          >
            <Icon name="x" class="text-[18px]" />
          </button>
        </div>

        <div class="flex-1 overflow-auto px-space-md py-space-md flex flex-col gap-space-md">
          <!-- 判定结论 -->
          <div class="flex items-center justify-between p-space-sm rounded-xl" :class="decisionStyle(selected.decision).badge">
            <div class="flex items-center gap-2">
              <span class="h-2.5 w-2.5 rounded-full" :class="decisionStyle(selected.decision).dot"></span>
              <span class="text-headline font-headline">{{ decisionStyle(selected.decision).label }}</span>
              <span class="text-caption-2 font-caption-2 opacity-80 mono">{{ decisionStyle(selected.decision).code }}</span>
            </div>
            <span class="text-title-3 font-title-3 mono">{{ score(selected.score) }}</span>
          </div>

          <!-- 全部已持久化字段 -->
          <div class="flex flex-col rounded-xl bg-surface-container overflow-hidden">
            <div class="px-space-sm py-2 border-b border-hairline">
              <span class="eyebrow">{{ t('audit.detail.persisted') }}</span>
            </div>
            <dl class="divide-y divide-hairline">
              <div class="flex items-start justify-between gap-space-sm px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant shrink-0">{{ t('audit.field.id') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface mono text-right">{{ selected.id }}</dd>
              </div>
              <div class="flex items-start justify-between gap-space-sm px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant shrink-0">{{ t('audit.field.time') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface mono text-right">{{ dateTimeOf(selected.ts) }}</dd>
              </div>
              <div class="flex items-start justify-between gap-space-sm px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant shrink-0">{{ t('audit.field.method') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface mono text-right">{{ selected.method }}</dd>
              </div>
              <div class="flex items-start justify-between gap-space-sm px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant shrink-0">{{ t('audit.field.path') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface mono text-right break-all">{{ selected.path }}</dd>
              </div>
              <div class="flex items-start justify-between gap-space-sm px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant shrink-0">{{ t('audit.field.kind') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface mono text-right">{{ selected.kind || '-' }}</dd>
              </div>
              <div class="flex items-start justify-between gap-space-sm px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant shrink-0">{{ t('audit.field.model') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface mono text-right break-all">{{ selected.model || '-' }}</dd>
              </div>
              <div class="flex items-start justify-between gap-space-sm px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant shrink-0">{{ t('audit.field.score') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface mono text-right">{{ score(selected.score) }}</dd>
              </div>
              <div class="flex items-start justify-between gap-space-sm px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant shrink-0">{{ t('audit.field.traceId') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface mono text-right break-all">{{ selected.trace_id || '-' }}</dd>
              </div>
              <div class="flex items-start justify-between gap-space-sm px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant shrink-0">{{ t('audit.field.latency') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface mono text-right">{{ selected.latency_ms }} ms</dd>
              </div>
              <!-- 耗时分解：检定阶段（读 body + 抽取 + JEV）与转发阶段（到上游首字节）。
                   未转发的记录没有转发阶段，显示占位而不是 0——0 ms 会被读成「瞬间返回」。 -->
              <div class="flex items-start justify-between gap-space-sm px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant shrink-0">{{ t('audit.field.inspect') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface mono text-right">
                  {{ selected.inspect_ms != null ? selected.inspect_ms + ' ms' : '-' }}
                </dd>
              </div>
              <div class="flex flex-col gap-0.5 px-space-sm py-2">
                <div class="flex items-start justify-between gap-space-sm">
                  <dt class="text-caption-1 font-caption-1 text-on-surface-variant shrink-0">{{ t('audit.field.upstream') }}</dt>
                  <dd class="text-caption-1 font-caption-1 text-on-surface mono text-right">
                    {{ selected.upstream_ms != null ? selected.upstream_ms + ' ms' : t('audit.field.notForwarded') }}
                  </dd>
                </div>
                <span v-if="selected.upstream_ms != null" class="text-caption-2 font-caption-2 text-outline text-right">
                  {{ t('audit.upstreamNote') }}
                </span>
              </div>
              <div class="flex items-start justify-between gap-space-sm px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant shrink-0">{{ t('audit.field.ip') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface mono text-right">{{ selected.ip || '-' }}</dd>
              </div>
              <div class="flex flex-col gap-1 px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('audit.field.reason') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface break-words">{{ selected.reason || '-' }}</dd>
              </div>
              <div class="flex flex-col gap-1 px-space-sm py-2">
                <dt class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('audit.field.snippet') }}</dt>
                <dd class="text-caption-1 font-caption-1 text-on-surface break-words">{{ selected.snippet || (snippetRecorded ? '-' : snippetPlaceholder) }}</dd>
                <span v-if="!snippetRecorded" class="text-caption-2 font-caption-2 text-outline">
                  {{ t('audit.snippetOffNote') }}
                </span>
              </div>
            </dl>
          </div>

          <!-- 未持久化字段：明确降级，不留空值也不编造。地理位置是唯一一项：
               审计只落原始来源 IP，归属地在「IP 风险分析」页离线解析。 -->
          <div class="flex flex-col gap-space-sm p-space-sm rounded-xl border border-dashed border-outline-variant/60">
            <span class="eyebrow">{{ t('audit.detail.notPersisted') }}</span>
            <div class="flex flex-col gap-space-xs">
              <div class="flex items-center justify-between gap-space-sm">
                <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('audit.missing.geo') }}</span>
                <NotConnected :reason="t('audit.missing.geoReason')" />
              </div>
            </div>
          </div>
        </div>

        <!-- 处置：一键封禁来源 IP，走与「IP 风险分析」页规则池同一个接口 -->
        <div class="px-space-md py-space-sm border-t border-hairline flex flex-col gap-space-xs">
          <span class="eyebrow">{{ t('audit.action.section') }}</span>
          <button
            v-if="selected.ip"
            type="button"
            class="inline-flex items-center justify-center gap-1.5 px-3 py-2 rounded-lg bg-error-container hover:bg-error-container/80 text-caption-1 font-caption-1 text-error transition-colors"
            @click="banIp(selected.ip)"
          >
            <Icon name="ban" class="text-[15px]" />
            <span>{{ t('audit.action.banButton') }}</span>
          </button>
          <span v-else class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('audit.action.banNoIp') }}</span>
          <span
            v-if="actionMsg"
            class="inline-flex items-center gap-1 text-caption-1 font-caption-1"
            :class="actionMsg.ok ? 'text-secondary' : 'text-error'"
          >
            <Icon :name="actionMsg.ok ? 'check-circle' : 'alert-circle'" class="text-[14px]" />
            {{ actionMsg.text }}
          </span>
        </div>
      </aside>
    </Transition>

    <!-- 抽屉遮罩 -->
    <Transition
      enter-active-class="transition-opacity duration-200"
      enter-from-class="opacity-0"
      leave-active-class="transition-opacity duration-150"
      leave-to-class="opacity-0"
    >
      <div v-if="selected" class="fixed inset-0 top-16 bg-black/40 z-30" @click="closeDetail"></div>
    </Transition>
  </div>
</template>
