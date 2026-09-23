---
generated_from_state_version: 17
---

# 验证

## 当前结果

- 结果: **已归档**
- 验证情况: **已完成检查，验证结果已确认**
- 目标周期: 4
- 迭代: 1
- 验证器尝试次数: 1
- 完成时间: 2026-09-23T09:37:29.501Z
- 摘要: 对候选 127f67f0 独立复核完毕，A1–A17 全部通过。Go 侧由 Runtime 绑定执行 build/vet 通过，本次另以 go build -o gateway ./cmd/gateway 独立重建并实际运行。验证实例使用仓库外的一次性种子库（546 条记录，含 0.0/0.199/0.2/0.5/0.999/1.0 分档边界样本），以真实 HTTP 逐项验证：鉴权矩阵（仅 login/setup/setup-status 公开，其余无 token 一律 401，错口令与伪造 token 均 401）、设置读写往返与越界拒绝、密钥池新增/停用/启用/删除、/api/logs 的四类筛选与组合筛选、分页不重叠与越界回退、/api/stats 在 1/6/24/168 小时四个窗口的 bucket_seconds（300/300/900/10800）、逐桶之和等于窗口总量、桶 ts 对齐且单调，以及五档分值直方图与 unscored——上述统计量全部与按同一窗口独立重算的 SQL 结果逐一相等。前端侧确认：路由与导航由模块注册表派生、深链接与未知路径落到 SPA 外壳、未注册 /api 路径仍 404、表格容器内滚动与表头吸顶保留、详情抽屉覆盖 LogEntry 全部已持久化字段、23 处无后端来源的元素一律以「未接入」降级且全仓无硬编码指标字面量、两张由 Q11/Q12 支撑的图不含降级标注并标注了数据来源与口径、登录页结构对照原型 jev_3 还原且按 A15 隐去了 24h 信任勾选与语言切换。web/dist 与由 web/src 重新构建的结果逐字节一致，确认入库产物与源码同步。未发现实现缺陷；所列风险均为已知限制或归档前待办（dist 需随本次改动一并提交）。

## 验收

| 编号 | 结果 | 来源 | 验收项 | 原因 |
| --- | --- | --- | --- | --- |
| A1 | passed | brief.md | 控制台由 Vue 3 + Vite + TypeScript + Tailwind 工程构建；仓库同时包含源码与构建产物，`go build -o gateway ./cmd/gateway` 直接产出含完整界面的二进制，构建与运行阶段都不需要 Node。 | web/src 为 Vue3+Vite+TS+Tailwind 源码工程，web/dist 为构建产物且未被 .gitignore 忽略（git check-ignore web/dist/index.html 退出码 1）；go build -o gateway ./cmd/gateway 由 Runtime 绑定检查与本次独立重跑均通过，构建链无任何 Node/npm 调用（全仓 Go 源无 exec.Command、无 go:generate）。以该二进制实起实例，GET /、/dashboard、/settings、/audit 均 200 且返回含 app 挂载点的页面。 |
| A2 | passed | brief.md | 界面不依赖任何公网 CDN：原型引用的 Tailwind CDN、Google Fonts、Material Symbols 均以本地等价物替代，离线环境加载不发外部请求。 | web/src、web/index.html 与 web/dist 全文扫描：无 cdn./fonts.googleapis/fonts.gstatic/unpkg/jsdelivr/cdnjs/esm.sh；无 @font-face 与远程 url()。编译产物 index-81YhFw3r.css 中不含任何远程 @import/url。残留的 http(s) 字面量仅为 XML 命名空间（w3.org/2000/svg、1999/xlink、1998/Math/MathML）、Vue 开发期告警链接（vuejs.org/error-reference）与表单 placeholder 文本（api.example.com / jev.example.com），均不产生网络请求。 |
| A3 | passed | brief.md | 暗色 Cupertino 主题令牌（调色板、字体族、8pt 间距、发丝边框、圆角与材质）以 `DESIGN.md` 为基线落地为 Tailwind 主题与基础样式。 | web/tailwind.config.js 的调色板与 docs/prototype/cupertino_safety_gateway/DESIGN.md 逐项一致（surface #121317、primary #adc6ff、secondary #53e16f、on-surface #e3e2e7、error #ffb4ab 等），并落地 8pt 间距令牌（space-xs…space-xl、gutter/margin）、发丝边框 rgba(255,255,255,0.12)、Elevation 阴影与 DESIGN.md 字阶表（display…code-badge）；上述令牌在构建后的 CSS 中实际存在（#121317 / #adc6ff / #53e16f / rgba(255,255,255,.12) 各命中）。字体族为系统字体栈，无远程字体。 |
| A4 | passed | brief.md | 三个界面可按原型导航到达且结构完整：驾驶舱、网关与安全策略配置、转发审计记录。 | 三个界面各有独立视图模块与完整结构：modules/dashboard（指标带、关键指标卡、趋势图、风险分值分布、集群卡）、modules/settings（上游与 JEV 路由 / 安全防护与自动拉黑 / 密钥池三个分组 section）、modules/audit（统计条、筛选区、表格、详情抽屉）。实起实例后三个路由均 200 并渲染出页面外壳。 |
| A5 | passed | brief.md | 左侧导航由路由驱动：三个界面各为独立路由页面，可通过 URL 直接打开、并在浏览器前进/后退中正确切换；界面按模块目录组织，新增界面只需注册路由与模块、不改动壳层。 | router/index.ts 用 createWebHistory，路由表由 modules/index.ts 的 SCREEN_MODULES 注册表派生；AppShell 的左侧导航同样遍历该注册表（RouterLink 按 name 跳转），壳层不写死任何界面。新增界面只需新增模块目录并在索引追加一项。实起实例 GET /dashboard /settings /audit 与未知路径 /nope/x 均 200（SPA 回落），根路径重定向到 dashboard。 |
| A6 | passed | brief.md | 网关配置界面覆盖原型的三个分组表单，并可直接读写现有接口：设置项经 `/api/settings` 读取与保存（改值保存后刷新仍生效），密钥池经 `/api/keys` 及其子路径完成新增、启用/停用与删除。 | 真实 HTTP 验证：GET /api/settings 200 返回当前配置；PUT 修改 safety_threshold=0.417 与 block_message 后再次 GET，两值均已持久化（刷新仍生效）；PUT safety_threshold=7 返回 400（safety_threshold must be between 0 and 1）且未落库。密钥池：POST /api/keys 新增返回 id、GET /api/keys 列出、POST /api/keys/{id}/disable 后 enabled=false、enable 后恢复 true、DELETE 后列表清空，全链路 200。 |
| A7 | passed | brief.md | 转发审计界面经 `/api/logs` 查询，支持判定状态、模型、关键词、时间范围四类筛选与服务端分页；筛选变更回到第 1 页、首末页按钮禁用、容器内滚动与表头吸顶等现有行为不回退。 | 真实 HTTP 验证 /api/logs：无筛选 total=546；decision=block total=78 且当页全部 block；model=gpt-4o-mini total=406 全部匹配；q=10.0.1. total=120；since=1h total=21 且全部在窗口内；decision+model 组合 total=8 且全部同时满足（条件为「与」）。limit=9999 归一化为 50 条。分页 offset 0/10/90 首页 id 546→536→…无重叠、total 恒定；末页 offset=540 返回 6 条、越界 offset=550 返回 0 条，前端对越界回退到最后一个有效页。表格为 max-h-[32rem] overflow-auto 容器内滚动 + thead sticky top-0，未回退。 |
| A8 | passed | brief.md | 转发审计界面的详情抽屉可打开并展示现有 `LogEntry` 已持久化的字段（时间、方法、路径、判定、分值、耗时、IP、模型、原因、送检摘要）。 | 抽屉按「已持久化字段」列出 11 个字段：记录 ID、时间、方法、路径、内容类型、目标模型、分值、网关耗时、来源 IP、原因、送检摘要，与 internal/config.LogEntry 完全对应；/api/logs 行实际返回同名字段集（score 因 omitempty 在无分值行缺省，前端 score()/scoreWidth() 对 null/undefined 返回占位符与 0 宽度，不伪造数值）。 |
| A9 | passed | brief.md | 驾驶舱与审计界面的统计条接 `/api/stats`（含 `hours` 参数）与 `/api/state` 的 `stats24h`，展示真实计数。 | 驾驶舱实时指标带与关键指标卡取自 GET /api/stats?hours=（total/allowed/blocked/skipped/errors），首页快照取自 GET /api/state 的 stats24h；审计界面统计条同样使用 stats24h。实起实例 /api/state 返回 stats24h={total:406, allowed:234, blocked:70, skipped:77, errors:25}，与按同一窗口独立 SQL 重算完全一致。 |
| A10 | passed | brief.md | 驾驶舱趋势图按后端返回的时间序列绘制：`GET /api/stats?hours=N` 返回 `bucket_seconds` 与 `series`（逐桶给出 `{ts,total,allowed,blocked}`，空桶补 0），前端据其绘制「入站总量 / 安全放行 / 拦截」三序列曲线，不自行分桶、不插值出无来源的数据点；切换时间范围分段控件即以对应 `hours` 重新查询并重绘。 | GET /api/stats?hours=N 返回 bucket_seconds 与逐桶 series[{ts,total,allowed,blocked}]。独立复核（用一次性种子库按同窗口重算 SQL 并与接口对比）：hours=1 → 13×300s、6 → 73×300s、24 → 97×900s、168 → 57×10800s，各窗口逐桶 total/allowed/blocked 之和与窗口 total/allowed/blocked 逐一相等；桶 ts 严格递增、步长恒等于 bucket_seconds 且首桶对齐桶宽；逐桶键恰为 ts/total/allowed/blocked。前端 TrendChart 只连点系列数组、不自行分桶不插值，横轴刻度与 hover 提示均取真实桶 ts 与计数，切换 1h/6h/24h/7d 分段控件即以对应 hours 重新查询。 |
| A11 | passed | brief.md | 驾驶舱「风险分值分布」按后端返回的 `score_buckets` 绘制：0.2 步长 5 档（末档含 1.0）加一个 `unscored` 计数，样本量为区间内 `score` 非空的记录数；卡片标注实际样本量，不显示无来源的占比。 | 接口返回 score_buckets 五档（0.2 步长，末档含 1.0）与 unscored。独立复核：在种子库中按 min(4, floor(score/0.2)) 重算，1/6/24/168 小时四个窗口的五档计数与 unscored 全部与接口一致（例如 24h：hist=[63,66,57,60,58]、unscored=25，五档之和 304 等于有分值记录数）；边界样本 0.0/0.199/0.2/0.5/0.999/1.0 分档正确。ScoreDistribution 只用后端计数绘制，占比在有分值样本量内就地相除，卡片明示「样本量 N 条有分值记录 · 统计口径为区间内全部已送检记录（含放行与拦截）」。 |
| A12 | passed | brief.md | 仍无后端来源的元素以明确降级态呈现——不显示伪造数值，降级元素带有可识别的「未接入」标注。降级集合不包含已按 Q11、Q12 实现的两张图。 | 全仓模板中检索形如两位以上百分比或千分位大数的硬编码指标字面量：无命中（原型 code.html 里有，实现里没有）。降级点共 23 处（驾驶舱 6、审计 7、配置 7、登录 3），全部使用 NotConnected（badge/placeholder 两种形态，均只呈现标题与缺口说明、不渲染任何数值），缺口说明与 docs/admin-console-backend-gaps.md 条目对应。已按 Q11/Q12 实现的趋势图与风险分值分布卡不含任何降级标注，反而标注数据来源「GET /api/stats?hours=N · 每桶 … 由后端分桶 · N 个桶」。 |
| A13 | passed | brief.md | 后端缺口清单文档完整：每一项含接口路径与方法、请求/响应字段、库表或模型变更、采集点位置、降级策略；涉及代理主链路的单独标注侵入范围。文档同时记录本次已实现的两项驾驶舱聚合（Q11、Q12）及其边界。 | docs/admin-console-backend-gaps.md（390 行）含结论摘要表（编号/界面元素/所在界面/缺口类型/优先级）与 G1–G23 分节。逐条核对 G7/G8/G10 等条目：均给出界面元素、现状态、建议模型（库表变更）、建议接口（路径+方法）、采集点（含 internal/proxy 具体函数）、降级策略，并单列「侵入范围」；D 类另设「主链路嵌入的兼容性影响（G3/G4/G5/G6/G7 汇总）」。新增章节「本次已实施的后端改动（Q11/Q12/Q13）」给出字段表、分桶宽度规则、口径与边界（逐桶之和等于窗口总量、allowed+blocked 不等于 total 因 skipped/errors、直方图样本为已送检记录）并声明三处改动均不触碰 internal/proxy 与 logs 表结构。 |
| A14 | passed | brief.md | 登录与初始配置界面（`jev_3`）按原型还原结构：居中 440px 玻璃认证卡与背景环境光晕、App 图标与渐变光环、居中标题与副标题、口令输入行（左置图标 + 右置可见性切换 + 右侧元信息）、带箭头的主按钮、守护进程状态胶囊、安全标语、页脚，提交结果以浮动提示呈现；`验证登录` / `初始配置` 分段控件保留，当前模式由 `/api/setup-status` 决定且另一项呈禁用态。 | LoginView.vue 按原型 jev_3 还原：居中 max-w-[440px] 玻璃卡（bg-surface-container/85 backdrop-blur-2xl）与背景环境光晕、shield-check 图标与渐变光环、居中标题副标题、口令输入行（左置 shield-lock 图标 + 右置可见性切换 + 右侧 bcrypt 元信息）、带 arrow-right 的主按钮（bg-primary-container text-on-primary）、守护进程状态胶囊、安全标语与页脚，提交结果以 role=status aria-live=polite 的浮动提示呈现。顶部「验证登录 / 初始配置」分段控件保留，当前模式由 GET /api/setup-status 决定且另一项 disabled（带 aria-pressed）；初始配置模式额外呈现确认口令行。 |
| A15 | passed | brief.md | 登录页无后端来源的元素按「分类处理」口径呈现：`ZERO-TRUST TLS` 与 `PII SHIELD ACTIVE` 作为静态标语保留；版本徽标、延迟徽标、守护进程地址胶囊以可识别的「未接入」降级态呈现且不显示伪造数值；口令哈希元信息标注为 bcrypt；不呈现「保持本工作站受信任凭据 (24h)」勾选项，也不提供语言切换。 | ZERO-TRUST TLS 与 PII SHIELD ACTIVE 作为静态标语原样保留；版本徽标（原型 v2.4.1-rc3）、延迟徽标（原型 LATENCY <1.8ms）、守护进程地址胶囊（原型 127.0.0.1:8080）改为「未接入」降级态并注明各自缺口（G23/G2/无监听地址接口），不显示任何伪造数值；口令元信息标注 bcrypt（与实际 internal/admin/auth.go 的 bcrypt 一致）。对照原型 code.html，「保持本工作站受信任凭据 (24h)」勾选项与 Language 语言切换在实现中确认不存在。 |
| A16 | passed | brief.md | 登录与初始设置主流程不回退：口令登录、`/api/setup-status` 判定未初始化时进入初始设置、口令长度与两次输入一致性校验，`/api/*` 除 `login` / `setup` / `setup-status` 外一律要求 bearer token。 | 实起实例逐项验证：GET /api/setup-status 未带 token 返回 200；/api/setup 无口令初始化后返回 token；随后 /api/state、/api/settings、/api/keys、/api/logs、/api/stats 在不带 token 时一律 401、带合法 token 时一律 200；错误口令 POST /api/login 返回 401；伪造 token 访问 /api/state 返回 401。handler.go 路由表确认只有 login/setup/setup-status 未包 h.auth。前端在 setup-status 判定未初始化时进入初始配置模式，口令不足 6 位与两次输入不一致均在前端拦截。 |
| A17 | passed | brief.md | `go build -o gateway ./cmd/gateway` 与 `go vet ./...` 均通过。 | 由 Runtime 绑定执行并记录：go-build（go build ./...）exit 0、go-vet（go vet ./...）exit 0；本次独立复核也以 go build -o gateway ./cmd/gateway 实际产出了可运行二进制。 |

## 检查

| 检查 | 命令 | 工作目录 | 状态 | 退出码 | 耗时 |
| --- | --- | --- | --- | ---: | ---: |
| go build ./... | build ./... | . | passed | 0 | 3493 ms |
| go vet ./... | vet ./... | . | passed | 0 | 2050 ms |
| go test ./... | test ./... | . | passed | 0 | 2374 ms |
| vue-tsc --noEmit（web 工程类型检查） | web/node_modules/vue-tsc/bin/vue-tsc.js --noEmit -p [REDACTED] | . | passed | 0 | 2546 ms |

### Builder 报告的证据

以下为 Builder 报告，不等同于 Runtime 检查凭据或独立验收结果。

- go build ./...: passed — exit 0
- go vet ./...: passed — exit 0
- go test ./...: passed — internal/admin、internal/config、web 三包全绿（新增 6+9+4 个用例）
- web typecheck (vue-tsc --noEmit): passed — exit 0
- web/dist 与源码一致性: passed — vite build 到临时目录后与 web/dist 逐文件比对，字节一致
- 手动走查（真机 HTTP）: passed — 登录闸门、三界面路由与前进/后退、深链接刷新、筛选与分页、配置保存、总开关、退出登录均在运行实例上验证；无 console 报错
- 已知限制: 控制台为桌面优先：视口宽度低于约 640px 时壳层顶栏（状态胶囊 + 总开关 + 退出）会横向溢出，原型本身也只覆盖桌面布局，本次未做窄屏适配。
- 已知限制: 驾驶舱的 P99 延迟、峰值 PPS、集群实例、集群拓扑，审计的导出 CSV / 黑名单 / 重放，配置的密钥健康与连通性测试仍无后端来源，一律以「未接入」降级呈现，未显示任何伪造数值。
- 已知限制: 风险分值直方图的样本口径是区间内全部已送检记录（含放行与拦截），不等于总请求量；差额为 skipped 与 unscored，界面已标注口径。
- 已知限制: 逐桶序列只给 total/allowed/blocked 三个计数，没有逐桶的 skipped/errors 细分，因此态势图只有三条曲线。
- 已知限制: 登录入口不占路由：未登录时 URL 仍是目标界面，登录后直接落到该界面；标签页标题在未登录时显示「登录 · JEV Gateway」。

## 阻塞项

_无。_

## 风险与跳过的工作

- web/dist 当前在 git 中仍为未跟踪状态（git status 显示 ?? web/dist/）。它未被 .gitignore 忽略，但只有在本次改动提交后，「仓库同时包含源码与构建产物」才在克隆语义上成立；且 web/embed.go 的 go:embed dist 要求 dist 必须存在——缺 dist 时 go build 直接失败（pattern dist: no matching files found）。归档前需确保 web/dist 与 web/src 同批提交。
- 控制台为桌面优先：视口窄于约 640px 时壳层顶栏（状态胶囊 + 总开关 + 退出）横向溢出；原型本身也只覆盖桌面布局，本次未做窄屏适配，属已知限制而非回归。
- 逐桶序列只提供 total/allowed/blocked，没有逐桶的 skipped/errors 细分，因此趋势图只有三条曲线；区间级 skipped/errors 仍可在指标带读到。
- 登录入口不占路由：未登录时 URL 仍停留在目标界面，登录后直接落到该界面；未登录时浏览器标签页标题显示「登录 · JEV Gateway」。这是刻意的设计选择，但与「登录页有独立 URL」的常见预期不同。
- 管理端会话为进程内内存态：本次验证中重启网关后旧 token 立即失效（需重新登录）。这是既有设计（internal/admin/auth.go 的滑动 12h 内存会话），控制台未对其做额外提示，多副本或频繁重启部署下需注意。
- 分页 total 与当页查询是两条独立语句（未包在同一事务内），并发写入时总数与当页可能出现极轻微不一致；属观感问题，不影响验收语义。

## 之前的迭代

| 目标周期 | 迭代 | 尝试 | 结果 | 未解决项 | 摘要 | 完成时间 |
| ---: | ---: | ---: | --- | --- | --- | --- |
| 1 | 0 | 0 | recovery | — | Native confirmed acceptance criteria changed | 2026-09-23T06:45:46.136Z |
| 2 | 1 | 1 | recovery | — | 用户要求将登录与初始配置界面（jev_3）从非目标移入交付范围并按原型还原，用户可见目标与验收标准变化，退回 Shape 重写范围与 A11 | 2026-09-23T08:13:00.322Z |
| 3 | 0 | 0 | recovery | — | Native confirmed acceptance criteria changed | 2026-09-23T08:35:51.852Z |
| 4 | 1 | 1 | pass | — | 对候选 127f67f0 独立复核完毕，A1–A17 全部通过。Go 侧由 Runtime 绑定执行 build/vet 通过，本次另以 go build -o gateway ./cmd/gateway 独立重建并实际运行。验证实例使用仓库外的一次性种子库（546 条记录，含 0.0/0.199/0.2/0.5/0.999/1.0 分档边界样本），以真实 HTTP 逐项验证：鉴权矩阵（仅 login/setup/setup-status 公开，其余无 token 一律 401，错口令与伪造 token 均 401）、设置读写往返与越界拒绝、密钥池新增/停用/启用/删除、/api/logs 的四类筛选与组合筛选、分页不重叠与越界回退、/api/stats 在 1/6/24/168 小时四个窗口的 bucket_seconds（300/300/900/10800）、逐桶之和等于窗口总量、桶 ts 对齐且单调，以及五档分值直方图与 unscored——上述统计量全部与按同一窗口独立重算的 SQL 结果逐一相等。前端侧确认：路由与导航由模块注册表派生、深链接与未知路径落到 SPA 外壳、未注册 /api 路径仍 404、表格容器内滚动与表头吸顶保留、详情抽屉覆盖 LogEntry 全部已持久化字段、23 处无后端来源的元素一律以「未接入」降级且全仓无硬编码指标字面量、两张由 Q11/Q12 支撑的图不含降级标注并标注了数据来源与口径、登录页结构对照原型 jev_3 还原且按 A15 隐去了 24h 信任勾选与语言切换。web/dist 与由 web/src 重新构建的结果逐字节一致，确认入库产物与源码同步。未发现实现缺陷；所列风险均为已知限制或归档前待办（dist 需随本次改动一并提交）。 | 2026-09-23T09:37:29.501Z |



## 结论

对候选 127f67f0 独立复核完毕，A1–A17 全部通过。Go 侧由 Runtime 绑定执行 build/vet 通过，本次另以 go build -o gateway ./cmd/gateway 独立重建并实际运行。验证实例使用仓库外的一次性种子库（546 条记录，含 0.0/0.199/0.2/0.5/0.999/1.0 分档边界样本），以真实 HTTP 逐项验证：鉴权矩阵（仅 login/setup/setup-status 公开，其余无 token 一律 401，错口令与伪造 token 均 401）、设置读写往返与越界拒绝、密钥池新增/停用/启用/删除、/api/logs 的四类筛选与组合筛选、分页不重叠与越界回退、/api/stats 在 1/6/24/168 小时四个窗口的 bucket_seconds（300/300/900/10800）、逐桶之和等于窗口总量、桶 ts 对齐且单调，以及五档分值直方图与 unscored——上述统计量全部与按同一窗口独立重算的 SQL 结果逐一相等。前端侧确认：路由与导航由模块注册表派生、深链接与未知路径落到 SPA 外壳、未注册 /api 路径仍 404、表格容器内滚动与表头吸顶保留、详情抽屉覆盖 LogEntry 全部已持久化字段、23 处无后端来源的元素一律以「未接入」降级且全仓无硬编码指标字面量、两张由 Q11/Q12 支撑的图不含降级标注并标注了数据来源与口径、登录页结构对照原型 jev_3 还原且按 A15 隐去了 24h 信任勾选与语言切换。web/dist 与由 web/src 重新构建的结果逐字节一致，确认入库产物与源码同步。未发现实现缺陷；所列风险均为已知限制或归档前待办（dist 需随本次改动一并提交）。
