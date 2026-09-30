import { t } from './i18n'
import type { MessageKey } from './i18n/zh'
import type { Decision } from './types'

/** 千分位整数。 */
export function num(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return '0'
  return Math.round(n).toLocaleString('en-US')
}

/** 三位小数分值；无分值返回占位符。 */
export function score(v: number | null | undefined): string {
  return v == null ? '-' : v.toFixed(3)
}

/** 分值进度条宽度（%）。 */
export function scoreWidth(v: number | null | undefined): number {
  if (v == null) return 0
  return Math.max(0, Math.min(100, v * 100))
}

function pad(n: number, width = 2): string {
  return String(n).padStart(width, '0')
}

/** 表格里的时间戳：HH:MM:SS.mmm（本地时区）。 */
export function clockOf(ts: string): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return '-'
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`
}

/** 完整本地时间，用于详情抽屉。 */
export function dateTimeOf(ts: string): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return '-'
  const date = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
  return `${date} ${clockOf(ts)}`
}

/** 相对时间，用于「最近使用」一类展示。 */
export function relativeOf(ts?: string): string {
  if (!ts) return t('time.never')
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return '-'
  const diff = Date.now() - d.getTime()
  if (diff < 0) return dateTimeOf(ts)
  const min = Math.floor(diff / 60000)
  if (min < 1) return t('time.justNow')
  if (min < 60) return t('time.minutesAgo', { n: min })
  const hour = Math.floor(min / 60)
  if (hour < 24) return t('time.hoursAgo', { n: hour })
  const day = Math.floor(hour / 24)
  if (day < 30) return t('time.daysAgo', { n: day })
  return dateTimeOf(ts)
}

/** 秒 → 人类可读时长，用于封禁时长等既有设置项的回显。 */
export function durationOf(sec: number): string {
  if (sec >= 86400 && sec % 86400 === 0) return t('duration.days', { n: sec / 86400 })
  if (sec >= 3600 && sec % 3600 === 0) return t('duration.hours', { n: sec / 3600 })
  if (sec >= 60 && sec % 60 === 0) return t('duration.minutes', { n: sec / 60 })
  return t('duration.seconds', { n: sec })
}

/** 百分比（一位小数），分母为 0 时返回 null 以便调用方降级。 */
export function pct(part: number, whole: number): number | null {
  if (!whole) return null
  return (part / whole) * 100
}

export function pctText(part: number, whole: number): string {
  const v = pct(part, whole)
  return v == null ? '-' : `${v.toFixed(2)}%`
}

/**
 * 带符号的百分比，用于环比：`+12.5%` / `-3.2%` / `+0.0%`。
 * 上期为 0 时返回 null——「从 0 增长到 N」不是百分比能表达的事，调用方据此
 * 显示「上期无数据」，而不是把除零的结果当成一个真实的涨幅。
 */
export function deltaPctText(current: number, previous: number): string | null {
  if (!previous) return null
  const v = ((current - previous) / previous) * 100
  return (v > 0 ? '+' : '') + v.toFixed(1) + '%'
}

// --- 判定状态的展示映射（与 LogEntry.decision 取值一致）---

export interface DecisionStyle {
  /** 主标签，随界面语言变化。 */
  label: string
  /**
   * 机器码（ALLOWED / BLOCKED / SKIPPED / FAIL_OPEN）。它是判定状态的稳定标识，
   * 与 HTTP 方法、接口路径同类，因此**不随界面语言变化**。
   */
  code: string
  /** 前景文字色 class。 */
  text: string
  /** 徽章底色 + 文字色 class。 */
  badge: string
  /** 小圆点底色 class。 */
  dot: string
  /** 分值进度条底色 class。 */
  bar: string
}

/** 配色与机器码是静态的；只有 `label` 需要按当前语言求值，故用 getter 延迟到渲染时取。 */
interface DecisionSpec extends Omit<DecisionStyle, 'label'> {
  labelKey: MessageKey
}

const DECISION_SPECS: Record<string, DecisionSpec> = {
  allow: {
    labelKey: 'decision.allow',
    code: 'ALLOWED',
    text: 'text-secondary',
    badge: 'bg-secondary-container/30 text-secondary',
    dot: 'bg-secondary',
    bar: 'bg-secondary',
  },
  block: {
    labelKey: 'decision.block',
    code: 'BLOCKED',
    text: 'text-error',
    badge: 'bg-error-container text-error',
    dot: 'bg-error',
    bar: 'bg-error',
  },
  skip: {
    labelKey: 'decision.skip',
    code: 'SKIPPED',
    text: 'text-on-surface-variant',
    badge: 'bg-surface-bright text-on-surface',
    dot: 'bg-outline',
    bar: 'bg-outline',
  },
  error: {
    labelKey: 'decision.error',
    code: 'FAIL_OPEN',
    text: 'text-tertiary',
    badge: 'bg-tertiary-container/30 text-tertiary',
    dot: 'bg-tertiary',
    bar: 'bg-tertiary',
  },
}

const UNKNOWN_SPEC: DecisionSpec = {
  labelKey: 'decision.unknown',
  code: 'UNKNOWN',
  text: 'text-outline',
  badge: 'bg-surface-container-high text-on-surface-variant',
  dot: 'bg-outline',
  bar: 'bg-outline',
}

function styleOf(spec: DecisionSpec): DecisionStyle {
  return {
    get label() {
      return t(spec.labelKey)
    },
    code: spec.code,
    text: spec.text,
    badge: spec.badge,
    dot: spec.dot,
    bar: spec.bar,
  }
}

export function decisionStyle(d: Decision | string): DecisionStyle {
  return styleOf(DECISION_SPECS[d] ?? UNKNOWN_SPEC)
}

/** HTTP 方法徽章色：POST/PUT/PATCH 走高亮，其余走中性。 */
export function methodClass(m: string): string {
  return m === 'POST' || m === 'PUT' || m === 'PATCH'
    ? 'bg-primary/20 text-primary'
    : 'bg-surface-container-high text-on-surface-variant'
}
