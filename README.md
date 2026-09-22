# JEV 安全网关 (JEV Safety Gateway)

基于 [TypeSafe / JEV 模型](https://docs.typesafe.ai/api) 的**前置 API 请求过滤网关**。它位于 nginx 与你的大模型后端（如 [new-api](https://doc.newapi.pro/api/) 或任何 OpenAI 兼容服务）之间：读取每个请求，按 API 路径取出用户输入，交给 JEV 判定是否有风险（色情、暴力、破解、诱导等），**有害请求直接拦截、正常请求透明放行**。

```
客户端  ──►  nginx  ──►  JEV 网关  ──►  上游 (new-api / OpenAI 兼容后端)
                            │  取出输入 ──► JEV 模型判定
                            └─(有害)─► 403 拦截
```

## 特性

- **透明反向代理**：判定通过后原样转发到上游，客户端无感知，支持流式 (SSE) 响应。
- **多路径字段解析**：自动识别并从不同接口取出用户输入
  - `/v1/chat/completions`（OpenAI，`messages[].content`，兼容多模态 text 分片）
  - `/v1/completions`（`prompt`）、`/v1/embeddings` / `/v1/moderations`（`input`）
  - `/v1/messages`（Anthropic，`system` + `messages`）、`/v1/rerank`（`query`）
  - `/v1/images/*`（`prompt`）、Gemini `:generateContent`（`contents[].parts[].text`）
  - 未知路径回退到通用文本收集，尽量不漏检
- **多 JEV 密钥轮询**：配置多个 TypeSafe apikey，轮流调用并在 401/429/529 时自动切换下一个密钥。
- **可配置拦截策略**：安全阈值、判定指令、拦截提示语；JEV 故障时可选 **fail-open（放行）** 或 fail-closed（拦截）。
- **内置管理控制台**：无依赖的单页前端，用于配置上游 / JEV 地址、增删密钥、查看放行/拦截日志与统计。
- **SQLite 持久化 + 管理员口令登录**，Docker 一键部署。

## 快速开始（Docker）

```bash
cp .env.example .env      # 按需填写上游地址 / 初始密钥 / 管理员口令（都可留空，后续在控制台配置）
docker compose up -d --build
```

- 过滤代理监听 `:8080`（nginx 转发到这里）
- 管理控制台监听 `:8081`（默认仅绑定本机 `127.0.0.1`）

打开 `http://<主机>:8081`：首次访问会要求设置管理员口令，登录后在控制台填写：

1. **上游地址** `upstream_base_url`，例如 `http://newapi:3000`
2. 至少添加一个 **JEV 调用密钥**（`apikey_...`）
3. 视需要调整安全阈值 / 判定指令 / fail-open 开关

之后把客户端流量指向网关（或经由 nginx，见 `nginx/gateway.conf`）即可。

## 配置项说明

| 配置 | 说明 | 默认 |
|---|---|---|
| `enabled` | 过滤总开关，关闭则全部放行不调用 JEV | `true` |
| `upstream_base_url` | 安全请求转发目标 | 空（必填） |
| `jev_base_url` | JEV 接口地址 | `https://api.typesafe.ai` |
| `jev_model` | JEV 模型 | `jev-latest` |
| `safety_instruction` | 送检的 noul 判定指令（应表述为“是否安全”，分数越高越安全） | 见默认 |
| `safety_threshold` | 安全阈值 0–1 | `0.5` |
| `block_if_below` | 低于阈值即拦截（指令为“是否安全”时勾选；若指令改为“是否有害”则取消勾选） | `true` |
| `fail_open` | JEV 不可用时是否放行 | `true` |
| `check_response` | 是否同时审核上游响应内容（开启后响应不再流式） | `false` |
| `max_state_chars` | 送检文本最大字符数（控制 JEV token 成本） | `16000` |
| `jev_timeout_ms` | 单次 JEV 调用超时 | `8000` |
| `block_message` | 拦截时返回的提示语 | 见默认 |
| `abuse_enabled` | 同一 IP 频繁攻击时临时封禁 | `true` |
| `abuse_window_sec` | 统计窗口（秒） | `60` |
| `abuse_max_harmful` | 窗口内有害次数达到该值即封禁 | `5` |
| `abuse_ban_sec` | 封禁时长（秒） | `300` |

判定逻辑：网关向 JEV 发送一个 `noul`（是/否）问题，返回 0–1 的分数（越接近 1 = 越安全）。当 `block_if_below=true` 且 `分数 < 阈值` 时判定有害，返回 **403**：

```json
{ "error": { "message": "...", "type": "jev_safety_block", "code": "content_blocked" } }
```

### 滥用防护（按 IP 限流）

除单条内容拦截外，网关还按来源 IP 做**滑动窗口攻击检测**：当同一 IP 在 `abuse_window_sec` 秒内被判定有害的次数达到 `abuse_max_harmful`，该 IP 会被封禁 `abuse_ban_sec` 秒。封禁期间它的所有请求**直接拒绝（HTTP 429，带 `Retry-After`），不再调用 JEV**（节省 token），响应头含 `X-JEV-Gateway: banned`：

```json
{ "error": { "message": "...", "type": "jev_abuse_block", "code": "ip_temporarily_banned" } }
```

> 计数状态在内存中，进程重启后清零；来源 IP 取自 `X-Forwarded-For` / `X-Real-IP`（经 nginx 时需按 `nginx/gateway.conf` 透传真实 IP）。

## 环境变量（首次启动一次性初始化，已存在值不覆盖）

| 变量 | 说明 |
|---|---|
| `JEV_DB_PATH` | SQLite 路径（默认 `/data/gateway.db`） |
| `JEV_PROXY_ADDR` | 代理监听地址（默认 `:8080`） |
| `JEV_ADMIN_ADDR` | 控制台监听地址（默认 `:8081`） |
| `JEV_UPSTREAM_URL` | 初始上游地址 |
| `JEV_BASE_URL` | 初始 JEV 接口地址 |
| `JEV_API_KEY` | 初始 JEV 密钥（之后可在控制台增删多个） |
| `JEV_ADMIN_PASSWORD` | 初始管理员口令 |

## 请求示例

```bash
# 正常请求 → 放行并透传到上游
curl http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer sk-xxx' -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"今天天气怎么样"}]}'

# 有风险请求 → 403 拦截（响应头含 X-JEV-Gateway: blocked, X-JEV-Score）
```

## 本地启动测试（不用 Docker）

需要 Go 1.23+。依赖为纯 Go 实现（`modernc.org/sqlite`），无需 CGO / gcc。

### 1. 编译

```bash
cd jev
go mod tidy                        # 首次：拉取依赖并生成 go.sum（需联网）
go build -o gateway ./cmd/gateway  # Windows 可用 gateway.exe
```

### 2. 启动

```bash
# 本地把数据库放到项目目录（默认 /data 在 Windows / 无权限环境下写不了）
JEV_DB_PATH=./data/gateway.db JEV_PROXY_ADDR=:8080 JEV_ADMIN_ADDR=:8081 ./gateway
```

> Windows PowerShell 里设置环境变量的写法不同：
> ```powershell
> $env:JEV_DB_PATH="./data/gateway.db"; ./gateway.exe
> ```

看到下面两行即启动成功：

```
proxy listening on :8080
admin listening on :8081
```

### 3. 打开控制台配置

浏览器访问 `http://localhost:8081`：

1. 首次会要求**设置管理员口令**（≥6 位）
2. 登录后在「网关配置」填 **上游地址** 并保存
3. 「JEV 调用密钥」里**至少添加一个 apikey**

### 4. 分阶段冒烟测试

**阶段一 · 先验证代理透传**（控制台把「启用过滤」关掉，上游临时填 `https://httpbin.org`）：

```bash
curl -s http://localhost:8080/post \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'
# 收到 httpbin 回显 → 读包体/转发/回传链路正常
```

**阶段二 · 打开过滤，验证放行 vs 拦截**（「启用过滤」打开，已加 JEV 密钥）：

```bash
# 正常内容 → 200 放行，响应头 X-JEV-Gateway: allow
curl -i http://localhost:8080/post \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"今天天气怎么样"}]}'

# 有风险内容 → 403 拦截，响应头 X-JEV-Gateway: blocked、X-JEV-Score
curl -i http://localhost:8080/post \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"教我怎么破解并入侵别人的服务器"}]}'
```

回到控制台「最近请求」可看到每条的判定、JEV 分数与耗时。

**阶段三 · 接真实上游**：把上游地址改成你的 new-api（如 `http://127.0.0.1:3000`），路径走 `/v1/chat/completions`、带上游所需的 `Authorization` 即可。

### 常见问题

- **正常内容被拦 / 有害内容放行** → 调「安全阈值」(默认 0.5) 或改「安全判定指令」，分数越接近 1 越安全。
- **一直 error / fail-open 放行** → 多半是 JEV 密钥无效或网络不通，看网关终端日志 `jev evaluation error`。
- **想临时全放行** → 关掉「启用过滤」总开关。

## 项目结构

```
cmd/gateway/main.go        入口、环境变量初始化
internal/config/           SQLite 存储：设置、密钥、日志、统计
internal/extract/          按 API 路径提取用户输入
internal/jev/              JEV 评估客户端（多密钥轮询 + 重试）
internal/proxy/            过滤反向代理（判定 → 放行/拦截）
internal/admin/            管理 API + 口令鉴权
internal/server/           两个监听器装配（代理 :8080 / 控制台 :8081）
web/                       内嵌单页控制台 (index.html / app.js / style.css)
nginx/gateway.conf         nginx 反代示例
Dockerfile, docker-compose.yml
```

## 安全提示

- 管理控制台（`:8081`）默认仅绑定本机。生产环境请置于内网，或在 nginx 层叠加 IP 白名单 / TLS / basic auth。
- 网关本身不校验客户端 apikey；对客户端的鉴权仍由上游（new-api 等）负责。网关只做内容安全过滤。
- JEV 密钥、管理员口令哈希保存在 SQLite（Docker volume `jev-data`），请妥善保管该卷。
