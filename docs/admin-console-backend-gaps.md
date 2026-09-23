# 控制台前端重构 — 后端缺口清单与方案设计

本文是 `redesign-admin-console` 的配套设计文档：控制台按新原型（`docs/prototype/jev_1`、`jev_2`、`jev_4`）重构后，
原型中出现、但现有后端**不支持的界面元素**逐项列出，并给出补齐方案。

**范围**：以「只设计、不实施」为基线——不新增 `/api/*` 路径、不改 `logs` / `jev_keys` 表结构、不改 `internal/proxy` 埋点。
界面侧对每一项都已落实「明确标注的降级态」，不显示任何无来源数值。

**本次实际实施的后端改动只有两处**（Shape 阶段确认的 Q11/Q12/Q13，细节见「本次已实施的后端改动」一节）：

1. `GET /api/stats` 在**既有路径**上扩展响应，新增逐桶时间序列与风险分值直方图（驾驶舱两张图的唯一数据来源，`internal/config` 只读聚合 + `internal/admin` 响应组装）；
2. `internal/admin` 的静态资源改为 SPA 回落（history 路由的深链接可直接打开）。

除此之外，本文档的其余条目仍是**方案设计**，未实施。

**现状事实**（全部来自代码，非推断）：

| 端点 | 方法 | 已返回内容 |
| --- | --- | --- |
| `/api/state` | GET | `{settings, keys, stats24h}` |
| `/api/settings` | GET / PUT | 完整 `Settings`（16 个字段） |
| `/api/keys` | GET / POST | `JEVKey[]`（`key` 仅入库，列表返回 `masked`）；空池返回 `null` |
| `/api/keys/{id}/enable` `/disable` | POST | 启停单把密钥 |
| `/api/keys/{id}` | DELETE | 删除单把密钥 |
| `/api/logs` | GET | `{items, total}`，参数 `limit/offset/decision/model/q/since` |
| `/api/stats` | GET | `{total, allowed, blocked, skipped, errors}` + 本次新增 `{bucket_seconds, series[], score_buckets[], unscored}`，参数 `hours` |

`logs` 表列：`id, ts, method, path, kind, decision, score, model, latency_ms, ip, reason, snippet`（索引 `idx_logs_ts(ts DESC)`）。
`jev_keys` 表列：`id, label, key, enabled, calls, last_used, created_at`。

## 结论摘要

| 编号 | 界面元素 | 所在界面 | 缺口类型 | 建议优先级 |
| --- | --- | --- | --- | --- |
| G1 | 时间序列趋势图（按时间分桶） | 驾驶舱 | B 只读聚合 | 高 |
| G2 | 延迟分位数（P99 / P95） | 驾驶舱、审计 | B + 索引 | 高 |
| G3 | 峰值流量 PPS | 驾驶舱 | D 主链路埋点 | 中 |
| G4 | 威胁类型分类占比 | 驾驶舱 | C 表变更 + 分类口径 | 中 |
| G5 | Token 估算与风险级 | 审计 | D 主链路埋点 + C | 低 |
| G6 | 耗时分解（检定 / 转发） | 审计 | D 主链路埋点 | 中 |
| G7 | 全局请求唯一 ID | 审计 | D 主链路埋点 + C | 中 |
| G8 | 完整原始请求体 | 审计 | C 表变更（**建议不补**） | 不做 |
| G9 | 地理位置（IP 归属地） | 审计 | E 外部数据源 | 低 |
| G10 | 处置规则矩阵 / 命名规则链 | 审计 | C 表变更 + 规则引擎 | 低 |
| G11 | 导出 CSV | 审计 | F 写接口（只读流） | 中 |
| G12 | 加入黑名单 | 审计 | F 写接口 | 中 |
| G13 | 重放测试 | 审计 | F 写接口 | 低 |
| G14 | 密钥健康成功率 | 配置 | C 表变更 + D 埋点 | 中 |
| G15 | 密钥健康心跳 | 配置 | E 进程内探活组件 | 低 |
| G16 | 分发池负载 | 配置 | B 派生或丢弃 | 低 |
| G17 | 权重轮询 | 配置 | C 表变更 + 路由改造 | 低 |
| G18 | 永久封禁 | 配置 | C 语义扩展（设置项） | 低 |
| G19 | 连通性测试 | 配置 | F 写接口（主动探测） | 中 |
| G20 | 多集群节点与地理分布 | 驾驶舱 | E 新组件 | 不做 |
| G21 | 集群 SLA / 网关开销 | 驾驶舱 | E 新组件 | 不做 |
| G22 | WORM 合规归档与审计校验码 | 配置 | E 新组件 | 不做 |
| G23 | 版本 / 构建信息 | 壳层 | B 只读（编译期注入） | 高 |

> **本次状态**：G1 的「逐桶时间序列」与 G4 在驾驶舱的原始形态已被 Q11/Q12 的两项聚合取代并**已实施**
> （见下一节），因此驾驶舱的趋势图与分布图不再是降级态；G1 中仍未做的部分（逐桶的 `skipped`/`errors` 细分、
> 独立 `/api/stats/timeseries` 路径）与其余条目一样保持「设计未实施」。G2、G3、G5…G23 的降级态均未变化。

---

## 本次已实施的后端改动（Q11 / Q12 / Q13）

三处改动都不新增 `/api/*` 路径、不改表结构、不触碰 `internal/proxy`，因此不改变放行/拦截/跳过/错误的判定语义、
`X-JEV-Gateway` / `X-JEV-Score` 响应头与 SSE 流式透传。

### Q11 + Q12：`GET /api/stats` 响应扩展（`internal/config` 只读聚合 + `internal/admin` 组装）

- **接口**：仍是 `GET /api/stats?hours=N`，响应在既有 `{total, allowed, blocked, skipped, errors}` 之后追加四个字段：

  | 字段 | 类型 | 含义 |
  | --- | --- | --- |
  | `bucket_seconds` | int | 本次分桶宽度（秒），由 `hours` 自适应：≤6h→300、≤24h→900、≤72h→3600、≤168h→10800、更长→86400 |
  | `series` | `[{ts,total,allowed,blocked}]` | 自 `since` 起按桶对齐的时间序列，空桶补 0，最旧在前；`ts` 为桶起点（unix 毫秒） |
  | `score_buckets` | `[5]int` | `noul` 分值直方图，0.2 步长五档：`[0,0.2) [0.2,0.4) [0.4,0.6) [0.6,0.8) [0.8,1.0]`（末档含 1.0） |
  | `unscored` | int | 区间内 `score IS NULL` 的记录数（JEV 不可达而 fail-open，或未送检） |

- **模型**：`config.StatBucket`、`config.ScoreHistogram`、`const ScoreSlots = 5`（`internal/config/models.go`）。
- **查询**：`Store.StatsSeries(since, until, bucketSeconds)` 与 `Store.ScoreHistogram(since)`（`internal/config/store.go`），
  均只读既有 `logs` 行、复用 `idx_logs_ts`，无新索引、无新表。
- **口径（重要边界）**：
  - 序列只查 `ts >= since` 的行，因此**逐桶求和恒等于同窗口的 `total`**（桶对齐只影响首桶起点，不影响计数）；
  - `series[].total` 含全部判定，`allowed + blocked` 不等于 `total`——差额是 `skipped` 与 `errors`，
    这与顶部 KPI 的口径一致，界面上「拦截逐桶趋势见下方态势图」即由此而来；
  - 直方图的统计口径是**区间内全部已送检记录**（`decision IN ('allow','block','error')`），不是仅拦截记录；
    `sum(score_buckets) + unscored` 等于该口径的行数，界面按此标注样本量；
  - `bucket_seconds` 由 `hours` 推导（`config.BucketSeconds`），前端不自行分桶、不插值，因此不会出现无来源的数据点。
- **降级**：接口不可用时两张图各自呈现「未接入 / 暂无数据」占位，不绘制曲线、不显示 0 值。
- **侵入范围**：仅 `internal/config`、`internal/admin`；`internal/proxy` 与 `logs` 表未改动。

### Q13：管理口静态资源 SPA 回落（`internal/admin`）

- **背景**：控制台改用 history 路由（`createWebHistory`），`/dashboard`、`/settings`、`/audit` 是真实路径，
  刷新或直接打开深链接会请求这些路径；原先的 `http.FileServer` 对不存在路径返回 404，深链接无法直接打开。
- **实现**：`New()` 的 `ui` 换成 `spaFileServer(webFS)`（`internal/admin/handler.go`）——
  非 GET/HEAD 直接放行给后续分支；`path.Clean` 后以 `api/` 开头的路径交回 `http.NotFound`，**不吞掉未知 API 路径**；
  命中真实文件（如 `/assets/*.js`）时走 `FileServer`；其余一律回落 `index.html`（`text/html; charset=utf-8` + `Cache-Control: no-cache`）。
- **边界**：只影响管理口静态资源投递，不改变任何 `/api/*` 的鉴权与响应；代理口（`:8080`）完全不涉及。
- **侵入范围**：仅 `internal/admin`。

---

## B 类：只读聚合接口（无库表变更，纯查询）

### G1 时间序列趋势图

- **界面元素**：驾驶舱「流量与安全威胁态势」折线/面积图，`1h / 6h / 24h / 7d` 四档。
- **本次已实现的部分**：逐桶序列已随 `GET /api/stats` 一并返回（`bucket_seconds` + `series`，
  见「本次已实施的后端改动」），驾驶舱据此绘制「入站总量 / 安全放行 / 拦截」三序列，切换档位即以对应 `hours` 重新查询重绘。
- **仍为缺口的部分**：每桶只给出三个计数，没有 `skipped` / `errors` 的逐桶细分（无法按桶画出跳过与容灾曲线）；
  也没有独立的 `/api/stats/timeseries` 路径（当前不需要，调用方只有控制台）。
- **后续接口（如需细分）**：`GET /api/stats/timeseries?hours=24&bucket=300`
  - 请求：`hours`（1..168，缺省 24）、`bucket`（秒，缺省按 `hours` 自适应）。
  - 响应：`{"bucket_sec":900,"points":[{"t":1730000000000,"total":12,"allowed":10,"blocked":1,"skipped":1,"errors":0}]}`
- **库表/模型变更**：无。在既有 `config.StatBucket` 上追加 `skipped` / `errors` 字段即可。
- **采集点**：无（纯聚合既有 `logs` 行）。
- **查询草案**：`SELECT (ts/?) * ? AS bucket, COUNT(*), SUM(decision='allow'), SUM(decision='block'), SUM(decision='skip'), SUM(decision='error') FROM logs WHERE ts >= ? GROUP BY bucket ORDER BY bucket`（`ts` 已是 unix 毫秒，分桶为整除；参数全部绑定）。
- **降级策略（已落地）**：已不再是降级态；接口不可用时两张图各自呈现占位并标注「未接入」。
- **侵入范围**：仅 `internal/config`、`internal/admin`。

### G2 延迟分位数

- **界面元素**：驾驶舱「P99 检定延迟」卡片；审计视图 KPI 条「平均网关耗时 P95」。
- **现状态**：`logs.latency_ms` 逐行存在，但没有分位数聚合。
- **建议接口**：`GET /api/stats/latency?hours=24` → `{"count":1260,"p50":41,"p90":88,"p95":132,"p99":310,"avg":57}`
- **库表/模型变更**：无表变更；建议补索引 `CREATE INDEX IF NOT EXISTS idx_logs_ts_latency ON logs(ts DESC, latency_ms)` 以支撑区间扫描。
- **采集点**：无。
- **查询草案**：取区间内 `latency_ms` 排序值定位分位（`SELECT latency_ms FROM logs WHERE ts >= ? ORDER BY latency_ms`，Go 侧取下标；样本量巨大时改用近似分位或按 `LIMIT` 采样并在响应中标注 `sampled:true`）。
- **降级策略（已落地）**：卡片显示「未接入」徽章，不显示任何延迟数值。
- **侵入范围**：仅 `internal/config`、`internal/admin`。

### G16 分发池负载

- **界面元素**：配置视图「分发池吞吐负载 68.4% Cap」。
- **现状态**：`jev_keys.calls` 为累计调用次数，可派生「密钥间调用分布」，但没有任何吞吐/负载率数据。
- **建议接口**：`GET /api/keys/stats?hours=1` → `{"keys":[{"id":1,"calls":842,"share":0.68}]}`
  - 依赖 G14 的按时段计数才能限定 `hours`；否则退化为全生命周期 `calls` 占比（响应中标注 `scope:"lifetime"`）。
- **库表/模型变更**：无（或随 G14 一起加计数器）。
- **采集点**：无。
- **降级策略（已落地）**：显示「未接入」徽章。原型中的百分比与 `Cap` 描述不呈现。
- **侵入范围**：仅 `internal/config`、`internal/admin`。

### G23 版本 / 构建信息

- **界面元素**：壳层底部（原型为 `Engine Core v2.4.1-rc`）、驾驶舱横幅版本徽章。
- **现状态**：二进制没有版本号，前端已改为展示真实可取的信息（检定模型、管理口地址）。
- **建议接口**：`GET /api/version` → `{"version":"v2.4.1","commit":"5335b40","built_at":"2026-09-23T07:00:00Z","go":"go1.23"}`
  - 或直接并入 `/api/state`，避免多一次请求。
- **库表/模型变更**：无；`cmd/gateway` 增加 `var version/commit/buildTime string`，用 `-ldflags "-X main.version=..."` 在构建期注入（缺省 `dev`）。
- **采集点**：无。
- **降级策略（已落地）**：壳层不显示任何版本字符串，只显示真实存在的检定模型与管理口标识；原型里的 `Apple HIG Spec` 一类宣传文案已移除。
- **侵入范围**：`cmd/gateway`、`internal/admin`、`Dockerfile`（构建参数）。

---

## C 类：需要 `logs` / `jev_keys` 表或模型变更

> 迁移方式统一为「启动时 `ALTER TABLE ... ADD COLUMN`，失败即已存在则忽略」——SQLite 支持带默认值的加列，
> 既有行取默认值，旧数据不需回填。所有新列必须给默认值，避免 `NOT NULL` 无默认导致的启动失败。

### G4 威胁类型分类占比

- **界面元素**：驾驶舱「安全威胁类型分布」环形图（越狱注入 / PII / 爬虫泛洪 / 违规内容四类）。
- **本次的口径变化**：Shape 阶段确认（Q12）用**风险分值分布**替代该四分类——JEV 只返回一个 0~1 的 `noul` 分值、
  不返回分类标签，四分类没有真实数据来源，而分值直方图可以直接从既有的 `logs.score` 聚合出来。
  该直方图已随 `GET /api/stats` 落地（`score_buckets` + `unscored`，见「本次已实施的后端改动」），
  因此驾驶舱此处不再是降级态；本条目剩下的部分是「真要按威胁类型分类」的缺口。
- **现状态**：只有 `decision`（是否拦截）与 `reason` 自由文本，没有威胁类型枚举。
- **建议模型**：`logs.threat_type TEXT NOT NULL DEFAULT ''`，取值收敛为 `jailbreak|pii|abuse_content|crawler|unknown`。
- **建议接口**：`GET /api/stats/threats?hours=24` → `{"items":[{"type":"jailbreak","count":540}]}`
- **分类口径（关键前置）**：JEV 只返回一个 0~1 的 `noul` 分值，**不返回分类标签**。得到四分类只有三条路：
  1. 让 JEV 在其响应里附带类型字段（需上游配合，`internal/jev` 解析该字段，未返回则记 `unknown`）；
  2. 本地按 `safety_instruction` 之外的第二轮提问做分类（每次请求翻倍检定成本，不推荐）；
  3. 后端不做分类，界面只按 `reason` 文本聚合（口径不稳定）。
  建议先落地路径 1 的**接收侧**（列 + 接口 + 未知兜底），分类来源另行决策。
- **采集点**：`internal/proxy/proxy.go` 的 `decide()` 返回签名末尾追加 `threatType string`；`pipeline` 无需其他改动。
- **降级策略（已落地）**：环形图区域为「威胁分类未接入」占位块，并以真实 `blocked` 计数作为副标题。
- **侵入范围**：`internal/config`（模型 + 迁移）、`internal/jev`（可选字段解析）、`internal/proxy`（`decide` 签名，见「主链路侵入」）。

### G5 Token 估算与风险级

- **界面元素**：审计 KPI「Token 消耗量 Est 849.2 M」、详情抽屉「Token 估算 / 风险级别」。
- **现状态**：不统计 Token，也没有风险级字段。`snippet` 是截断后的送检摘要，可由其长度粗略估算，但会是**编造数字**，因此当前不呈现。
- **建议模型**：`logs.in_chars INTEGER NOT NULL DEFAULT 0`、`logs.out_chars INTEGER NOT NULL DEFAULT 0`（字符数，非 Token）；
  Token 估算放在查询期按 `chars/4` 折算并在响应中标注 `estimated:true`。风险级用 `logs.risk_level TEXT NOT NULL DEFAULT ''`。
- **建议接口**：并入 `/api/stats?hours=` 的响应（新增 `in_chars/out_chars`）与 `GET /api/logs` 的行字段。
- **采集点**：`extract.Output`/`Extract` 已返回送检文本，`in_chars` 可在 `decide()` 内取 `len([]rune(text))`；`out_chars` 需在 `forwardChecked` 路径统计（`CheckResponse=false` 时流式转发无法统计，需标注为 0 并解释）。
- **降级策略（已落地）**：KPI 条不呈现 Token 卡片；抽屉中「Token 估算与风险级」显示「未接入」。
- **侵入范围**：`internal/config`、`internal/proxy`（`decide` 与响应审计路径）。

### G6 耗时分解

- **界面元素**：审计抽屉「安全模型 / 耗时分析」（检定耗时 vs 转发耗时）。
- **现状态**：只有端到端 `latency_ms`。
- **建议模型**：`logs.jev_ms INTEGER NOT NULL DEFAULT 0`、`logs.upstream_ms INTEGER NOT NULL DEFAULT 0`。
- **采集点**：`internal/proxy/proxy.go` 在 `decide()` 内包裹 JEV 调用计时；转发耗时在 `forward()` 返回前计时（流式转发下等于「首字节时间」，需在字段语义上写清）。
- **降级策略（已落地）**：抽屉显示「未接入」，不进位显示 0。
- **侵入范围**：`internal/config`、`internal/proxy`。

### G7 全局请求唯一 ID

- **界面元素**：审计抽屉「全局网关唯一 ID `req_9fa7b401e92d`」。
- **现状态**：仅有 SQLite 自增 `id`（前端已如实展示为 `#126`）。
- **建议模型**：`logs.req_id TEXT NOT NULL DEFAULT ''`；生成 `req_` + 12 位十六进制（`crypto/rand`），并同时写入响应头 `X-JEV-Request-Id`，便于与上游日志对账。
- **建议接口**：无新增（`/api/logs` 行内新增字段即可）。
- **采集点**：`ServeHTTP` 入口生成并贯穿；响应头写入位置在 `writeBlocked`/`forward`/`forwardChecked` 三处需一致。
- **降级策略（已落地）**：抽屉显示「未接入」并注明「仅持久化了 SQLite 自增 id」。
- **侵入范围**：`internal/config`、`internal/proxy`、`internal/admin`。**注意**：新增响应头属于对外可见的协议变化，需在 change 中评估客户端兼容性。

### G8 完整原始请求体（**建议不补**）

- **界面元素**：审计抽屉「原始送检请求体 Raw JSON Payload」+「复制完整 JSON」。
- **现状态**：按设计只保留截断后的 `snippet`。
- **设计判断**：**不建议**持久化完整原始载荷。理由：① 原始载荷含用户业务数据与可能的 PII，落库即扩大合规面与泄露面；② 与 `max_state_chars` 的「最小必要留痕」设计相冲突；③ 审计所需的最小证据（判定、分值、原因、摘要）已具备。
- **替代方案**：如确需排查，提供**按需**的短时开关 `debug_capture_raw`（内存环形缓冲 N 条 + 显式过期），并让界面按钮只在开关打开时可用；仍不落库。
- **降级策略（已落地）**：抽屉显示「未接入」，并说明「按设计不持久化原始载荷」。
- **侵入范围**：若采纳替代方案，仅 `internal/proxy`（内存缓冲）+ `internal/admin`（读取接口）。

### G10 处置规则矩阵 / 命名规则链

- **界面元素**：审计抽屉「触发规则链 Rule Evaluation Matrix（RULE-4011/…）」。
- **现状态**：判定来自「单一阈值 + `block_if_below` 方向」，没有命名规则与规则链。
- **建议模型**：新增 `rules(id, name, kind, condition, action, enabled, priority)` 与 `logs.matched_rule_ids TEXT NOT NULL DEFAULT ''`（逗号分隔）。
- **建议接口**：`GET/POST/PUT/DELETE /api/rules`；评测结果并入 `/api/logs` 行字段。
- **设计前置**：规则引擎是**功能级**变更（多规则、优先级、与既有阈值判定共存/替换语义），不只是数据缺口，应作为独立 change 立项，先确定「阈值判定」与「规则链」的关系（推荐：阈值判定作为内置规则 `RULE-THRESHOLD` 参与链）。
- **降级策略（已落地）**：抽屉显示「未接入」，注明「判定由单一阈值产生」。
- **侵入范围**：`internal/config`（新表）、`internal/proxy`（评测入口）、`internal/admin`。

### G14 密钥健康成功率

- **界面元素**：配置视图每把密钥的「842.1k 次 · 99.98% OK」。
- **现状态**：`jev_keys.calls` 有累计调用数（已如实展示），但没有成功/失败计数。
- **建议模型**：`jev_keys.ok_calls INTEGER NOT NULL DEFAULT 0`、`jev_keys.err_calls INTEGER NOT NULL DEFAULT 0`。
- **采集点**：`internal/jev/client.go` 的轮询/重试循环里，对最终成功与被判定为失败（非 401/429/529 的其它状态、网络错误、JSON 解析失败）分别自增。**注意**：401/429/529 会切到下一把密钥，属于「该密钥失败」而非整体失败，应记在前一把密钥上。
- **降级策略（已落地）**：只显示真实累计调用次数与最近使用时间，成功率显示「未接入」。
- **侵入范围**：`internal/config`、`internal/jev`（`KeyProvider` 需新增计数回调，接口实现方在 `internal/server`）。

### G17 权重轮询

- **界面元素**：配置视图「权重轮询 (RR) 自动负载均衡」。
- **现状态**：`internal/jev` 已按启用密钥做原子游标轮询——**轮询已真实存在**，缺的是权重。
- **建议模型**：`jev_keys.weight INTEGER NOT NULL DEFAULT 1`；选取算法由「游标取模」改为「平滑加权轮询（SWRR）」，保持无锁原子状态。
- **建议接口**：`/api/keys` 的增行与更新接口接受 `weight` 字段。
- **降级策略（已落地）**：显示真实的「轮询分发 (Round-Robin)」徽章，并在旁标注权重配置「未接入」。
- **侵入范围**：`internal/config`、`internal/jev`（选取算法）、`internal/admin`。

### G18 永久封禁

- **界面元素**：配置视图封禁时长预设「永久」。
- **现状态**：`abuse_ban_sec` 为秒数，`internal/abuse` 以「窗口内触发 → 封禁至 now+N 秒」实现，没有永久语义。
- **建议语义**：约定 `abuse_ban_sec < 0` 表示永久；`internal/abuse.Banned()` 对永久封禁返回 `until = zero time`，界面显示「永久」；重启即清空（进程内计数器的既有性质，需在界面注明）。
- **采集点**：`internal/abuse/tracker.go`（判断分支）、`internal/proxy` 的 429 响应正文（`writeBanned` 需处理 `until` 为零值时的文案）。
- **降级策略（已落地）**：不提供「永久」预设，只给 10 分钟 / 1 小时 / 24 小时三档与自由秒数输入，并在旁注明原因。
- **侵入范围**：`internal/abuse`、`internal/proxy`（仅文案分支）、`internal/config`（校验放行负数）。

---

## D 类：需要 `internal/proxy` 主链路埋点（侵入范围与兼容性）

### G3 峰值流量 PPS

- **界面元素**：驾驶舱横幅「峰值流量 PPS 842 req/s」。
- **现状态**：无速率采集。
- **建议实现**：进程内滑动窗口计数器（与 `internal/abuse` 同构，1 秒粒度、窗口 60 秒），在 `ServeHTTP` 入口计数；暴露 `GET /api/stats/throughput?window=60` → `{"current":12,"peak":842,"window_sec":60}`。
  另需一个低频（如 1 秒）ticker 采样当前速率以维护峰值——可复用同一 `internal/abuse` 风格的自有 goroutine，**不引入新进程**。
- **降级策略（已落地）**：显示「未接入」徽章 + 「需要新增指标采集」。
- **侵入范围**：`ServeHTTP` 入口一行计数（不改变任何判定/转发分支）。

### G9 地理位置

- **界面元素**：审计列表「客户端 IP + Geo」、抽屉「地理位置 / 代理链」。
- **现状态**：只存 `ip`（来自 `clientIP(r)`）。
- **方案选项**：
  1. 不引入外部服务 → 内置 `GeoIP2-City` 或 `GeoLite2` 只读库（需嵌入数据文件，与「不内嵌大文件、纯 Go」约束冲突，`GeoLite2` 的 mmdb 读取库为纯 Go，数据文件需随镜像分发，属**外部数据源**）；
  2. 接受「无地理位置」，只在抽屉展示 IP 与代理链。
- **建议**：不纳入本次及后续默认范围（选项 2）。若合规确需，作为独立 change 引入本地 mmdb 文件并在 `Dockerfile` 增加数据文件挂载点，**不得**调用外部查询 API。
- **降级策略（已落地）**：列表不呈现 Geo 列；抽屉显示「未接入」。
- **侵入范围**：若走选项 1，`internal/proxy` 需在落日志前做一次查表（毫秒级，可接受）；否则无。

### 主链路嵌入的兼容性影响（G3/G4/G5/G6/G7 汇总）

| 变更 | 影响面 | 兼容性判断 |
| --- | --- | --- |
| `decide()` 返回值追加字段 | `internal/proxy` 内部函数签名 | 编译期即可发现，无对外影响 |
| `logs` 表加列（带默认值） | 数据库迁移 | 旧行取默认值；旧二进制读新库不受影响（列名固定，`SELECT` 显式列名） |
| 新增响应头 `X-JEV-Request-Id` | 对外协议 | **需评估**：客户端不应因未知响应头失败；建议仅在 `JEV_DEBUG` 之外默认开启前先在 change 中确认 |
| 流式转发下 `out_chars`/`upstream_ms` 不可得 | 数据语义 | 需在字段文档与界面徽章中明确「流式路径不统计」，不得填 0 冒充真实值 |
| 入口计数（PPS） | 热路径 | 单个原子自增，无锁、无 IO，可忽略 |

**共同底线**：以上埋点均不得改变放行/拦截/跳过/错误的判定语义、`X-JEV-Gateway` / `X-JEV-Score` 的既有取值、
以及 SSE 流式透传（`CheckResponse=false` 时不得缓冲响应体）。

---

## F 类：写操作接口

### G11 导出 CSV

- **界面元素**：审计视图「导出 CSV 日志」。
- **建议接口**：`GET /api/logs/export.csv?decision=&model=&q=&since=`（与 `/api/logs` 同参数，忽略 `limit/offset`，
  或支持 `limit` 上限 50000 并设 `Content-Disposition: attachment`）。
- **实现要点**：`text/csv; charset=utf-8` + UTF-8 BOM（Excel 兼容）；逐行 `bufio.Writer` 流式输出，不整表入内存；
  字段做 CSV 转义；建议加 `audit_export` 级别的操作留痕（谁在何时导出了多少行）。
- **降级策略（已落地）**：按钮不呈现，改以「未接入」徽章说明原因。
- **侵入范围**：`internal/admin`（`internal/config` 复用 `LogFilter`）；导出留痕需新增表或复用 `logs`（建议新增 `admin_audit`）。

### G19 连通性测试

- **界面元素**：配置视图「上游业务接口地址」「JEV 安全检定接口」旁的「测试连通」按钮与 ping 结果。
- **现状态**：没有任何主动探测接口；地址是否可用只会在真实请求时暴露。
- **建议接口**：`POST /api/settings/test-connectivity` body `{"target":"upstream"|"jev"}`
  → `{"ok":true,"status":200,"latency_ms":31,"checked_at":"…"}`（JEV 侧应请求其健康/最轻量端点，不要用 `/v1/systemone`，避免污染检定调用量与配额）。
- **实现要点**：给探测单独的短超时（如 3s）与结果缓存（如 10s），避免界面连点造成探测风暴；
  目标地址取**表单当前值**还是**已保存值**需明确——建议取已保存值，并在响应中回显所用地址，防止「测的与存的不一致」。
- **降级策略（已落地）**：按钮不呈现，改为在分组标题旁显示「未接入」徽章，说明后端未提供探测接口；地址输入框保留可编辑与保存。
- **侵入范围**：仅 `internal/admin`（出站 HTTP 探测）；**不触及** `internal/proxy`，也不新增对外地址。

### G12 加入黑名单

- **界面元素**：审计抽屉「将 IP 加入黑名单」。
- **现状态**：`internal/abuse` 只有「按阈值自动封禁」与内存态封禁表；`Banned()` 可读，无手动写入接口。
- **建议接口**：`POST /api/abuse/ban` body `{"ip":"203.0.113.7","seconds":3600,"reason":"manual"}`；`DELETE /api/abuse/ban?ip=` 解封；`GET /api/abuse/bans` 列出当前封禁。
- **语义**：手动封禁应与自动封禁共用同一张内存表，但**不得**计入 strike 计数；重启即失效（与现有一致），若需持久化则需新表 `bans(ip, until, reason, created_by, created_at)`。
- **降级策略（已落地）**：抽屉不提供该操作按钮，并说明可用的替代路径（调低阈值 / 运维侧处理）。
- **侵入范围**：`internal/abuse`（导出写入方法）、`internal/admin`；**不触及** `internal/proxy` 判定分支。

### G13 重放测试

- **界面元素**：审计抽屉「重放测试」。
- **现状态**：无原始载荷（见 G8），因此**无法真实重放**——重放需要请求体，而请求体按设计不落库。
- **设计判断**：**不纳入**。可行替代是「用 `snippet` 重新送检一次」的**重检**功能：`POST /api/logs/{id}/rescore`
  → 取该行 `snippet` 调用 JEV，返回新分值并**只作对比展示，不写回日志、不改变判定**。这既不依赖原始载荷，也不产生写语义风险。
- **降级策略（已落地）**：不提供该操作；在抽屉中说明缺原因。
- **侵入范围**：若采纳重检方案，`internal/admin`（新增接口）+ `internal/jev`（复用 `Score`）；无主链路改动。

---

## E 类：需要新组件或外部数据源（建议不纳入）

### G15 密钥健康心跳

- **界面元素**：配置视图「健康心跳 15s」。
- **现状态**：不做主动探活；只在真实请求遇到 401/429/529 时切下一把密钥。
- **为什么不建议**：定期向检定服务发探活请求会**放大外部调用量**（并按 `jev_keys.calls` 语义污染计数），
  且与「密钥失效由真实请求暴露」的既有设计重复。若确需，建议做成显式开关 + 默认为关，探活不计入 `calls`。
- **降级策略（已落地）**：显示「未接入」并说明真实的失效切换机制。

### G20 多集群节点与地理分布

- **界面元素**：驾驶舱「全球边缘集群运行状态」（Cluster-East-01 Tokyo 等 3 张卡）。
- **现状态**：单进程单实例，无节点注册/心跳/调度。
- **为什么不建议**：这不是数据缺口而是**架构变更**（多副本部署、共享审计存储、节点名册、健康上报、会话与配置的一致性模型）。
  与「不引入运行期外部服务」的约束直接冲突，应作为独立架构 change 立项，由部署形态决策驱动。
- **降级策略（已落地）**：以「本机网关实例」真实卡片（过滤开关、密钥池、检定模型）+「集群拓扑未接入」占位块呈现。

### G21 集群 SLA / 网关开销

- **界面元素**：配置视图横幅「99.992% SLA / 42μs Overhead」。
- **现状态**：SLA 需要可用性时序，Overhead 需要「网关处理耗时 − 上游耗时」，两者分别依赖 G3 与 G6 的采集能力。
- **建议**：不独立立项；待 G3/G6 落地后作为派生指标并入 `/api/stats`。
- **降级策略（已落地）**：文案替换为真实内容（判定语义说明、`fail_open` 状态），不显示 SLA/Overhead 数字。

### G22 WORM 合规归档与审计校验码

- **界面元素**：配置视图「合规基准与审计归档 WORM / 审计校验码 0x4D2A…9F81」。
- **现状态**：审计数据在 SQLite `logs` 表，可被 DBA 修改，无不可变存储、无校验码。
- **为什么不建议在本项目内做**：WORM 需要外部不可变存储（对象存储 + 合规锁）或追加式日志链，
  属于基础设施与合规流程决策，超出「SQLite + 进程内」的既有边界。
- **轻量替代（可选）**：按日对 `logs` 行按 `id` 升序计算 SHA-256 链式校验码（`checksum_n = H(checksum_{n-1} || 行内容)`），
  落一张 `log_checksums(day, from_id, to_id, checksum, created_at)`，提供 `GET /api/audit/checksum?day=` 供外部对账。
  这能在不引入外部存储的前提下提供「篡改可发现」。仍属独立 change。
- **降级策略（已落地）**：显示「未接入」，并说明「可由运维侧自行备份」。

---

## 实施建议顺序

1. **已完成**：G1 的逐桶时间序列、G4 的驾驶舱形态（由风险分值直方图替代）——两项都只动 `internal/config` + `internal/admin`，
   已随本次 change 交付。
2. **第一批（纯只读，零主链路侵入）**：G23 版本、G2 延迟分位、G11 导出 CSV，
   以及 G1 剩余的分桶细分。这批同样只动 `internal/config` + `internal/admin`，界面可再去掉 2 处降级态。
3. **第二批（主链路轻量埋点）**：G3、G6、G7（+ 响应头兼容性评估）。
4. **第三批（需要上游配合或独立立项）**：G4（分类来源决策）、G5、G14、G17、G18、G19。
5. **不纳入**：G8（建议不补）、G13（改为重检）、G15、G20、G21、G22；G9 仅在合规确需时以本地 mmdb 方案评估。

每一批落地后，前端只需替换对应位置的降级组件为真实组件；`web/src/components/NotConnected.vue` 的 `reason`
文本与本文的「降级策略」行一一对应，可作为验收清单核对。
