/**
 * 后端数据模型的前端镜像。字段与 internal/config/models.go 一一对应，
 * 这里只做类型声明，不新增任何后端不存在的字段。
 */

/** 与 internal/config/models.go 的 `Settings` 对应。 */
export interface Settings {
  enabled: boolean
  upstream_base_url: string
  jev_base_url: string
  jev_model: string
  safety_instruction: string
  safety_threshold: number
  block_if_below: boolean
  fail_open: boolean
  check_response: boolean
  abuse_enabled: boolean
  abuse_window_sec: number
  abuse_max_harmful: number
  abuse_ban_sec: number
  block_message: string
  max_state_chars: number
  jev_timeout_ms: number
}

/** 与 internal/config/models.go 的 `JEVKey` 对应（`key` 仅在新增时提交，列表返回掩码）。 */
export interface JEVKey {
  id: number
  label: string
  masked: string
  enabled: boolean
  calls: number
  last_used?: string
  created_at: string
}

/** 判定状态。与 internal/config 中的 decision 取值一致。 */
export type Decision = 'allow' | 'block' | 'skip' | 'error'

/** 与 internal/config/models.go 的 `LogEntry` 对应。 */
export interface LogEntry {
  id: number
  ts: string
  method: string
  path: string
  kind: string
  decision: Decision
  score?: number
  model: string
  latency_ms: number
  ip: string
  reason: string
  snippet: string
}

/** 与 internal/config/models.go 的 `Stats` 对应。 */
export interface Stats {
  total: number
  allowed: number
  blocked: number
  skipped: number
  errors: number
}

/**
 * 与 internal/config/models.go 的 `StatBucket` 对应：趋势图上的一个时间桶。
 * 桶由后端按桶边界对齐并补全，空桶也出现在序列中（计数为 0），
 * 因此前端直接绘制即可，不需要插值。
 */
export interface StatBucket {
  /** 桶起始时刻，unix 毫秒。 */
  ts: number
  total: number
  allowed: number
  blocked: number
}

/**
 * `GET /api/stats` 的响应：`Stats` 的既有字段，加上本次新增的聚合字段。
 * `/api/state` 的 `stats24h` 仍是纯 `Stats`，不含这些新增字段。
 */
export interface StatsResponse extends Stats {
  /** 后端按 `hours` 推导并回传的分桶粒度（秒）。 */
  bucket_seconds: number
  /** 覆盖整个查询区间的连续分桶序列，最旧的在前。 */
  series: StatBucket[]
  /** 风险分值分布的五档计数：`[0,0.2)` … `[0.8,1.0]`，末档含 1.0。 */
  score_buckets: number[]
  /** 区间内已送检但没有分值的记录数（JEV 未返回分值）。 */
  unscored: number
}

/** `GET /api/state` 的响应。 */
export interface StateResponse {
  settings: Settings
  keys: JEVKey[]
  stats24h: Stats
}

/** `GET /api/logs` 的响应。 */
export interface LogsResponse {
  items: LogEntry[] | null
  total: number
}

/** `GET /api/setup-status` 的响应。 */
export interface SetupStatus {
  needs_setup: boolean
}
