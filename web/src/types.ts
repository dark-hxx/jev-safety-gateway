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
  /** 是否先解码送检文本中的 base64 片段；默认开启，关闭时按原样送检。 */
  expand_base64: boolean
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
  /**
   * 触发滥用封禁时是否把该 IP 一并写入永久封禁规则；默认关闭。
   * 内存态封禁随进程重启消失，只有落成规则才是真的永久——自动永久封禁
   * 由启发式判定做出，所以默认关闭，且只能到「IP 风险分析」页删除规则解封。
   */
  abuse_ban_permanent: boolean
}

/**
 * 与 internal/config/models.go 的 `JEVKey` 对应（`key` 仅在新增时提交，列表返回掩码）。
 * `calls` 是累计尝试次数，等于 `ok_calls + err_calls`；每次尝试都记在当时使用的那把
 * 密钥上，包括可重试的 401/429/529 与网络失败。
 */
export interface JEVKey {
  id: number
  label: string
  masked: string
  enabled: boolean
  calls: number
  /** 成功返回分值的次数。 */
  ok_calls: number
  /** 失败的次数。 */
  err_calls: number
  /** 最近一次失败的原因；从未失败时省略。 */
  last_error?: string
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
  /**
   * 网关为本次请求生成的全局唯一 ID（`req_` + 12 位十六进制），同时也作为
   * `X-JEV-Request-Id` 响应头回给客户端；据此可以在审计流水里对上任意一次请求。
   * 始终由网关生成，不采用客户端传来的请求 ID。
   */
  trace_id: string
  /**
   * 送检阶段的耗时（毫秒）：读 body、抽取、JEV 检定。仅在被 IP 规则或滥用封禁
   * 拦下的记录上省略——那些请求从未走到这一步。
   */
  inspect_ms?: number
  /**
   * 转发阶段的耗时（毫秒）：从发起上游请求到收到响应首字节。是首字节时间而不是整段
   * 交换——流式响应在客户端拿到响应之后还会持续很久，计入会把一条 60 秒的 SSE
   * 拖进延迟分位数。未转发的记录省略（不补 0）。
   */
  upstream_ms?: number
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
  /**
   * 区间内最繁忙的单个自然秒的请求数（按 `ts/1000` 分组取最大），用于说明
   * 「峰值流量」是瞬时并发能力，而不是区间总量。
   */
  peak_pps: number
  /**
   * 紧邻本区间之前、等长的那个区间，用于环比。没有更早数据时各计数为 0，
   * 前端据此显示「—」而不是把 0 当成一个真实的下跌。
   */
  previous: Stats
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

/**
 * 与 internal/config/models.go 的 `IPStat` 对应：某来源 IP 在时间窗内的审计聚合，
 * 附带一个确定式、可解释的危险评分（`danger` / `level`，后端计算、从不落库）。
 * 时间字段为 unix 毫秒；`avg_score` / `min_score` 在无送检记录时后端省略。
 */
export interface IPStat {
  ip: string
  total: number
  allowed: number
  blocked: number
  skipped: number
  errors: number
  /** 携带 JEV 分值的记录数；为 0 时 avg/min 不出现。 */
  scored: number
  avg_score?: number
  min_score?: number
  /** 窗口内最早一条记录的时刻，unix 毫秒。 */
  first_seen: number
  /** 窗口内最近一条记录的时刻，unix 毫秒。 */
  last_seen: number
  last_reason: string
  /** 0–1 危险评分。 */
  danger: number
  /** 危险分档：crit / high / med / low。 */
  level: string
  /** ISO-3166-1 alpha-2 国家码（如 "US"）；未配置 GeoIP 或未命中时省略。 */
  country?: string
  /** 国家英文名；同 `country` 一起出现。 */
  country_name?: string
  /** 自治系统号；未配置 ASN 库或未命中时省略。 */
  asn?: number
  /** 自治系统组织名；同 `asn` 一起出现。 */
  asn_org?: string
}

/**
 * 与 internal/config/models.go 的 `GeoBucket` 对应：窗口内按国家聚合的事件/拦截量。
 * `country` 为 ISO 国家码，空串表示未解析/内网 IP（归入「未知」一行）。
 */
export interface GeoBucket {
  country: string
  name: string
  total: number
  blocked: number
}

/** 与 internal/config/models.go 的 `ASNBucket` 对应：窗口内按 ASN 聚合的事件/拦截量。 */
export interface ASNBucket {
  asn: number
  org: string
  total: number
  blocked: number
}

/**
 * 与 internal/config/models.go 的 `IPStatsSummary` 对应：窗口内**全部**去重 IP 的 KPI 汇总，
 * 与返回列表的 limit 无关（截断只影响 `items`，不影响这些总量）。
 */
export interface IPStatsSummary {
  distinct_ips: number
  blocked_ips: number
  crit: number
  high: number
  total_blocked: number
  total_events: number
}

/**
 * 与 internal/admin 的 banEntry 对应：一条来自 abuse tracker 的实时临时封禁。
 * 封禁为进程内、单实例、重启即失效。
 */
export interface BanEntry {
  ip: string
  /** 封禁到期时刻，unix 毫秒。 */
  until: number
  /** 剩余封禁秒数，最低为 0（取样于响应生成时刻）。 */
  remain_sec: number
}

/**
 * `GET /api/stats/ip?hours=&limit=` 的响应：窗口 KPI 汇总（`IPStatsSummary` 的字段被平铺在顶层）、
 * 危险度倒序的每 IP 列表，以及从 abuse tracker 读到的实时封禁。`items` 在空窗口下后端可能省略为 null。
 */
export interface IpStatsResponse extends IPStatsSummary {
  hours: number
  items: IPStat[] | null
  bans: BanEntry[]
  ban_count: number
  /** 是否配置了国家库；为假时前端保持「未接入」占位，`geo` 省略。 */
  geo_enabled?: boolean
  /** 是否配置了 ASN 库；为假时前端保持「未接入」占位，`asn` 省略。 */
  asn_enabled?: boolean
  /**
   * 本网关的部署坐标（JEV_GATEWAY_LAT / JEV_GATEWAY_LON，十进制度），供来源图把中心
   * 节点落在服务真实位置。运维未声明时整个字段省略——前端据此不出中心节点与弧线，
   * 而不是把网关钉在一个编造的坐标上。（0,0 是几内亚湾的真实坐标，所以「未声明」必须
   * 由字段是否存在表达，不能靠数值是否为 0 判断。）
   */
  gateway?: { lat: number; lon: number }
  /** 按事件量降序的国家聚合排行（top-N）；维度未启用时省略。 */
  geo?: GeoBucket[]
  /** 按事件量降序的 ASN 聚合排行（top-N）；维度未启用时省略。 */
  asn?: ASNBucket[]
}

/**
 * IPRule 是持久化的 IP 访问规则（GET/POST/DELETE /api/ip-rules），与内存态 abuse
 * 封禁互补：block 为手动/CIDR/永久封禁，allow 为白名单放行（优先于任何封禁，但不
 * 豁免内容检定）。时间字段是 Go time.Time 的 RFC3339 字符串；`expires_at` 为空表示
 * 永久（allow 恒为永久，字段省略）。
 */
export interface IPRule {
  id: number
  pattern: string
  is_cidr: boolean
  kind: 'block' | 'allow'
  expires_at?: string
  reason: string
  created_at: string
}

/** 新增规则的请求体。allow 规则忽略时长；临时 block 需 duration_sec > 0。 */
export interface IPRuleInput {
  pattern: string
  kind: 'block' | 'allow'
  reason: string
  permanent: boolean
  duration_sec: number
}

/** GET /api/ip-rules 的响应；空规则池时后端可能省略 items 为 null。 */
export interface IPRulesResponse {
  items: IPRule[] | null
}
