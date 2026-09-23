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
  reject_oversize_body: boolean
  abuse_enabled: boolean
  abuse_window_sec: number
  abuse_max_harmful: number
  abuse_ban_sec: number
  block_message: string
  max_state_chars: number
  jev_timeout_ms: number
  /** 是否记录用户送检摘要；默认关闭，关闭时后端不持久化送检文本。 */
  record_snippet: boolean
  /** 是否复用相同送检内容的检定结果；默认开启。 */
  dedup_enabled: boolean
  /** 检定结果复用窗口（秒）；≤0 时后端回退为 60。 */
  dedup_window_sec: number
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

/** 与 internal/config/models.go 的 `ModelCount` 对应：一个模型取值及其出现次数。 */
export interface ModelCount {
  value: string
  count: number
}

/** `GET /api/logs/models` 的响应，用于审计页的模型下拉选项。 */
export interface LogModelsResponse {
  items: ModelCount[] | null
}

/** `GET /api/setup-status` 的响应。 */
export interface SetupStatus {
  needs_setup: boolean
}

/**
 * `GET /api/version` 的响应：构建标识 + 实际绑定的监听地址。
 *
 * 这是唯一免鉴权的 `/api/` 路由（登录页在登录前就要读它），所以字段里没有任何
 * 来自审计日志的内容。地址由后端在 listen 成功后回填，是**实际**绑定值而非配置串：
 * 配置写 `:8080` 时这里也是 `:8080`（表示不限网卡）。
 */
export interface VersionInfo {
  version: string
  /** 短提交号；无 git 元信息时为空串。 */
  commit: string
  built_at: string
  /** 构建该二进制的 Go 版本，例如 go1.23.4。 */
  go: string
  /** 代理（业务）监听地址。 */
  proxy_addr: string
  /** 管理口监听地址。 */
  admin_addr: string
}

/**
 * 与 internal/config/models.go 的 `LatencyStats` 对应：区间内单请求总耗时的分布。
 *
 * `count` 始终是区间内的真实记录数；`sampled` 为真时表示记录数超过后端取样上限，
 * 各分位数由按时间均匀的样本算出，只能当参考值展示，不要标成精确值。
 */
export interface LatencyStats {
  count: number
  sampled: boolean
  p50: number
  p90: number
  p95: number
  p99: number
  avg: number
  min: number
  max: number
}
