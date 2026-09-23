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
  if (!ts) return '从未使用'
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return '-'
  const diff = Date.now() - d.getTime()
  if (diff < 0) return dateTimeOf(ts)
  const min = Math.floor(diff / 60000)
  if (min < 1) return '刚刚'
  if (min < 60) return `${min} 分钟前`
  const hour = Math.floor(min / 60)
  if (hour < 24) return `${hour} 小时前`
  const day = Math.floor(hour / 24)
  if (day < 30) return `${day} 天前`
  return dateTimeOf(ts)
}

/** 秒 → 人类可读时长，用于封禁时长等既有设置项的回显。 */
export function durationOf(sec: number): string {
  if (sec >= 86400 && sec % 86400 === 0) return `${sec / 86400} 天`
  if (sec >= 3600 && sec % 3600 === 0) return `${sec / 3600} 小时`
  if (sec >= 60 && sec % 60 === 0) return `${sec / 60} 分钟`
  return `${sec} 秒`
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

// --- 判定状态的展示映射（与 LogEntry.decision 取值一致）---

export interface DecisionStyle {
  /** 中文主标签。 */
  label: string
  /** 原型中的英文副标签。 */
  en: string
  /** 前景文字色 class。 */
  text: string
  /** 徽章底色 + 文字色 class。 */
  badge: string
  /** 小圆点底色 class。 */
  dot: string
  /** 分值进度条底色 class。 */
  bar: string
}

const DECISION_STYLES: Record<string, DecisionStyle> = {
  allow: {
    label: '放行',
    en: 'ALLOWED',
    text: 'text-secondary',
    badge: 'bg-secondary-container/30 text-secondary',
    dot: 'bg-secondary',
    bar: 'bg-secondary',
  },
  block: {
    label: '拦截',
    en: 'BLOCKED',
    text: 'text-error',
    badge: 'bg-error-container text-error',
    dot: 'bg-error',
    bar: 'bg-error',
  },
  skip: {
    label: '跳过',
    en: 'SKIPPED',
    text: 'text-on-surface-variant',
    badge: 'bg-surface-bright text-on-surface',
    dot: 'bg-outline',
    bar: 'bg-outline',
  },
  error: {
    label: '错误',
    en: 'FAIL_OPEN',
    text: 'text-tertiary',
    badge: 'bg-tertiary-container/30 text-tertiary',
    dot: 'bg-tertiary',
    bar: 'bg-tertiary',
  },
}

const UNKNOWN_STYLE: DecisionStyle = {
  label: '未知',
  en: 'UNKNOWN',
  text: 'text-outline',
  badge: 'bg-surface-container-high text-on-surface-variant',
  dot: 'bg-outline',
  bar: 'bg-outline',
}

export function decisionStyle(d: Decision | string): DecisionStyle {
  return DECISION_STYLES[d] ?? UNKNOWN_STYLE
}

/** HTTP 方法徽章色：POST/PUT/PATCH 走高亮，其余走中性。 */
export function methodClass(m: string): string {
  return m === 'POST' || m === 'PUT' || m === 'PATCH'
    ? 'bg-primary/20 text-primary'
    : 'bg-surface-container-high text-on-surface-variant'
}
