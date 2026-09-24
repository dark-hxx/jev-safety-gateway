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
- **内置管理控制台**：Vue 3 单页应用，含运行概览（转送量、判定构成、逐桶流量趋势、风险分值分布）、网关与安全策略配置、转发审计记录（可按判定状态、目标模型、来源 IP、请求路径、关键词、时间范围筛选，服务端分页）三个界面；构建产物随仓库提交，运行期无公网依赖。
- **SQLite 持久化 + 管理员口令登录**，Docker 一键部署。

## 快速开始（Docker）

```bash
cp .env.example .env      # 按需填写上游地址 / 初始密钥 / 管理员口令（都可留空，后续在控制台配置）
docker compose up -d --build
```

- 过滤代理监听 `:8080`（nginx 转发到这里）
- 管理控制台监听 `127.0.0.1:8081`（**只绑本机**）

打开 `http://127.0.0.1:8081`：首次访问会要求设置管理员口令，登录后在控制台填写：

1. **上游地址** `upstream_base_url`，例如 `http://newapi:3000`
2. 至少添加一个 **JEV 调用密钥**（`apikey_...`）
3. 视需要调整安全阈值 / 判定指令 / fail-open 开关

之后把客户端流量指向网关（或经由 nginx，见 `nginx/gateway.conf`）即可。

## 部署指南

三个生产部署目标，选一个即可。分平台的完整安装、升级、备份与排错见 **[docs/deployment.md](docs/deployment.md)**；本节给出选型与最小上手步骤。

| 目标 | 适用场景 | 部署产物 |
|---|---|---|
| **Docker / compose** | 有容器运行时，或要与 new-api 同机编排 | `Dockerfile`、`docker-compose.yml` |
| **Linux 裸机** | 发行版带 systemd，不想引入容器 | `deploy/linux/install.sh`（幂等，可重复执行） |
| **Windows 服务** | Windows Server，需要开机自启与进程托管 | `scripts/install-service.ps1` |

### 先看两条硬约束（三个目标都一样）

1. **同一份数据库只能跑一个实例。** 滥用计数在进程内存里（`internal/abuse`），配置存储串行化在单条 SQLite 连接上（`internal/config`）。起两个进程会同时丢失封禁状态并争抢数据库文件锁——没有多副本模式，也不能两个实例在线滚动升级。
2. **升级前必须整体备份 SQLite 三件套**（`.db` / `.db-wal` / `.db-shm`）。`-wal` 可能远大于主库（实测主库 40 KB / WAL 3.4 MB），数据主要落在 WAL 里，**单独留下主库没有意义**。

> 表结构迁移目前只有 `CREATE TABLE IF NOT EXISTS`：给**既有表新增列**的变更不会自动生效（启动后报 `no such column`），升级前请先读 [docs/deployment.md](docs/deployment.md) 的迁移一节。设置项本身不受影响——`load()` 在默认值之上解码存储的 JSON，新版本加的设置项在旧库上自动取默认值。

### Docker / compose

```bash
cp .env.example .env      # 按需填上游地址 / 初始密钥 / 管理员口令，都可留空
docker compose up -d --build
```

- **监听**：代理 `8080:8080`（nginx 转发到这里）；控制台容器内绑 `:8081`，宿主机映射 `127.0.0.1:8081:8081`——**是端口映射而非绑定地址**保住了它的私密性，所以镜像里必须是 `:8081`（`Dockerfile` 的 `ENV` 已设）。
- **数据**：命名卷 `jev-safety-gateway-data` 挂到 `/data`。卷名在 compose 里写死，不含项目名前缀，因此下面的备份命令在任何目录名下都一样。
- **日志**：容器日志已设 `max-size: 10m` / `max-file: 3` 上限（`json-file` 默认无限增长，长期运行必须设）。容器内**不要**再设 `JEV_LOG_FILE`，`docker logs` 就够。
- **版本号**：`docker compose up -d --build` 会自动从 `web/package.json` 解析出真实版本（不会显示 `dev`）。`.git` 不在构建上下文里，提交号与构建时间拿不到，要显式传：

  ```bash
  JEV_COMMIT=$(git rev-parse --short HEAD) docker compose up -d --build
  ```

- **升级**：先备份卷，再 `docker compose up -d --build`。
- **备份**：

  ```bash
  docker run --rm -v jev-safety-gateway-data:/data -v "$PWD":/backup \
    alpine tar czf /backup/jev-safety-gateway-$(date +%F).tar.gz -C /data .
  ```

> 若 new-api 也在同一个 compose 里，用服务名互联（上游地址填 `http://newapi:3000`），无需暴露 3000 端口。nginx 若也容器化，见 `docker-compose.yml` 里的注释块——`nginx/gateway.conf` 的 upstream 要改成服务名 `jev-safety-gateway:8080`。

### Linux 裸机（systemd）

发布包内 `install.sh`、`jev-safety-gateway`、`jev-safety-gateway.service`、`env.example`、`deployment.md` 放在同一目录，直接跑：

```bash
sudo ./install.sh
```

- **幂等**：重复执行就是升级——停服务、换二进制、再起。它**从不删除也不覆盖**数据库与 `/etc/jev-safety-gateway/env`（升级不会重置上游地址与密钥）。
- **路径**：

  | 内容 | 位置 |
  |---|---|
  | 二进制 | `/usr/local/bin/jev-safety-gateway` |
  | systemd unit | `/etc/systemd/system/jev-safety-gateway.service` |
  | 初始化配置（含密钥） | `/etc/jev-safety-gateway/env`（`0600`） |
  | 数据库三件套 | `/var/lib/jev-safety-gateway/` |

- **服务用户**：专用系统账号 `jev-safety-gateway`（`nologin`），unit 内已加 `ProtectSystem=strict` 等加固；数据库目录由 `StateDirectory=` 交给 systemd 管理，权限自动正确。
- **日志**：走 journald，**不要设 `JEV_LOG_FILE`**（那是给 Windows 服务的）。
- **常用命令**：

  ```bash
  systemctl status jev-safety-gateway
  journalctl -u jev-safety-gateway -f
  systemctl restart jev-safety-gateway
  ```

- **升级**：把新二进制覆盖到同目录后重跑 `sudo ./install.sh`。
- **备份**：

  ```bash
  sudo tar czf ~/jev-safety-gateway-$(date +%F).tar.gz \
    /var/lib/jev-safety-gateway /etc/jev-safety-gateway
  ```

- **卸载**：`sudo ./install.sh --uninstall`。库与配置**有意保留**，脚本末尾会打印它们的路径与手工清理命令。

### Windows 服务（原生）

无需 nssm：`cmd/jev-safety-gateway/entry_windows.go` 用 `golang.org/x/sys/windows/svc` 检测 SCM，并把 Stop/Shutdown 映射到 `server.Run` 已经在等的那个 context，所以 `sc.exe stop` 与 Ctrl+C 走**同一条关闭路径**，在途请求会被排空。

以**管理员**身份：

```powershell
.\scripts\install-service.ps1 -ExePath .\out\jev-safety-gateway\jev-safety-gateway.exe
```

- **安装目录**：默认 `%ProgramData%\jev-safety-gateway`，二进制、`data\`、`logs\` 都在这里（和仓库里的 `data\` 是两回事，脚本从不删除数据目录）。
- **环境变量**：Windows 服务没有 shell，变量写在服务注册表 `Environment`（`REG_MULTI_SZ`）。脚本**只放基础设施变量**（`JEV_DB_PATH` / `JEV_LOG_FILE` / `JEV_PROXY_ADDR` / `JEV_ADMIN_ADDR`）。
  **密钥与管理员口令请留在控制台里配置**——注册表对管理员与 SYSTEM 可读，`-EnvFile` 会把初始密钥写进去，生产环境不推荐。
- **日志**：`JEV_LOG_FILE` 已自动指向 `logs\gateway.log`，**必须设**——SCM 不给服务控制台，stderr 会被丢弃，不设等于没有日志。
- **崩溃自启**：已配置 `sc.exe failure` 动作 `restart/5000` ×3、计数每天重置。
- **升级**：停服务 → 替换 exe → 起服务（重跑 `install-service.ps1` 亦可，它会停服务再换文件）。
- **卸载**：`.\scripts\uninstall-service.ps1`，保留 `data\` 与 `logs\`。加 `-Purge` 才会删，且需手工输入 `DELETE` 确认，并会先自动备份。

### 反向代理与远程访问

- **nginx**：`nginx/gateway.conf` 是现成示例，upstream 默认 `127.0.0.1:8080`（裸机口径），容器部署改成服务名。它已透传 `X-Forwarded-For` / `X-Real-IP`——**滥用防护按来源 IP 计数，少透传这一项会让所有客户端共用一个计数**。
- **控制台远程访问**：默认只绑本机。远程请走 SSH 隧道 `ssh -L 8081:127.0.0.1:8081 <user>@<host>`，或在 nginx 层叠加 IP 白名单 / TLS / basic auth。**不要改绑 `0.0.0.0`**。

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
| `reject_oversize_body` | 请求体超过 8 MiB 而无法送检时是否直接拒绝（关闭则原样转发） | `false` |
| `max_state_chars` | 送检文本最大字符数（控制 JEV token 成本） | `16000` |
| `jev_timeout_ms` | 单次 JEV 调用超时 | `8000` |
| `record_snippet` | 是否持久化用户送检摘要（关闭时日志不含任何送检文本） | `false` |
| `dedup_enabled` | 相同送检内容在窗口内复用上次检定结果 | `true` |
| `dedup_window_sec` | 检定结果复用窗口（秒，≤0 回退为 `60`） | `60` |
| `block_message` | 拦截时返回的提示语 | 见默认 |
| `abuse_enabled` | 同一 IP 频繁攻击时临时封禁 | `true` |
| `abuse_window_sec` | 统计窗口（秒） | `60` |
| `abuse_max_harmful` | 窗口内有害次数达到该值即封禁 | `5` |
| `abuse_ban_sec` | 封禁时长（秒） | `300` |

判定逻辑：网关向 JEV 发送一个 `noul`（是/否）问题，返回 0–1 的分数（越接近 1 = 越安全）。当 `block_if_below=true` 且 `分数 < 阈值` 时判定有害，返回 **403**：

```json
{ "error": { "message": "...", "type": "jev_safety_block", "code": "content_blocked" } }
```

### 端点覆盖与请求体转发

网关对**所有** OpenAI 格式端点做同一套处理：能从请求里取出用户文本就送检，取不出就原样转发；转发**不改变字节**，因此流式（SSE）、分块请求、大文件上传都不受影响。

- 覆盖的端点：`/chat/completions`、`/completions`、`/responses`、`/embeddings`、`/moderations`、`/images/*`、`/audio/speech`、`/videos*`、`/realtime/sessions`、`/assistants`、`/threads/*`、`/rerank`、Anthropic `/messages`、Gemini `:generateContent`；`multipart/form-data`（转写、图片编辑等）按表单字段取文本，跳过文件部分。
- 明知不含用户文本的端点（`/files`、`/uploads`、`/batches`、`/fine_tuning/*`、`/vector_stores*`、`/models`）记为 `skip`，不会被误判为“未知端点”。
- 未识别的路径回退为“收集请求体里所有字符串”（跳过 `data:` 内嵌数据），因此新端点也不会漏检。
- 送检的**只是最新一轮用户输入**，不带客户端注入的系统提示词和整段历史：否则有害内容会被大量无害文本稀释而判为安全。OpenAI 的 `tool` 角色、Anthropic 的 `tool_result`、Responses 的 `function_call_output` 都算最新一轮（Agent 循环里最新提交的常常就是工具输出）。
- 请求体超过 8 MiB 时无法送检：默认**原样转发**（记为 `skip` / `oversize`，大文件上传照常可用）；勾选 `reject_oversize_body` 后改为失败关闭，返回 **413**：

```json
{ "error": { "message": "...", "type": "jev_oversize_body", "code": "request_too_large" } }
```

### 送检摘要记录与检定结果复用

`record_snippet` 默认**关闭**：审计日志只保留判定结果、分值与原因，不写入用户送检文本，`JEV_DEBUG` 调试日志同样不打印该文本；控制台的「转发审计记录」以「未记录」占位展示。该开关只影响开关关闭后写入的新记录，不追溯修改已有记录。

`dedup_enabled` 默认**开启**：同一段送检文本（连同 JEV 地址、模型、判定指令、阈值与拦截方向一起参与比对）在 `dedup_window_sec` 秒内重复出现时，直接复用上一次的分值，不再调用 JEV；审计记录的「原因」列会追加「；命中相同内容缓存」。缓存只保存文本的 sha256 摘要与分值，不保存送检文本本身；命中拦截时照常累计滥用计数，封禁语义不变。缓存位于进程内存中，重启即清空。

### 滥用防护（按 IP 限流）

除单条内容拦截外，网关还按来源 IP 做**滑动窗口攻击检测**：当同一 IP 在 `abuse_window_sec` 秒内被判定有害的次数达到 `abuse_max_harmful`，该 IP 会被封禁 `abuse_ban_sec` 秒。封禁期间它的所有请求**直接拒绝（HTTP 429，带 `Retry-After`），不再调用 JEV**（节省 token），响应头含 `X-JEV-Gateway: banned`：

```json
{ "error": { "message": "...", "type": "jev_abuse_block", "code": "ip_temporarily_banned" } }
```

> 计数状态在内存中，进程重启后清零；来源 IP 取自 `X-Forwarded-For` / `X-Real-IP`（经 nginx 时需按 `nginx/gateway.conf` 透传真实 IP）。

## 环境变量（首次启动一次性初始化，已存在值不覆盖）

| 变量 | 说明 |
|---|---|
| `JEV_DB_PATH` | SQLite 路径（默认 `./data/jev-safety-gateway.db`，相对启动时的工作目录；Docker 镜像内由 `ENV` 固定为 `/data/jev-safety-gateway.db`） |
| `JEV_PROXY_ADDR` | 代理监听地址（默认 `:8080`） |
| `JEV_ADMIN_ADDR` | 控制台监听地址（默认 `127.0.0.1:8081`，只绑本机；Docker 镜像内由 `ENV` 设为 `:8081`，靠端口映射保持私密） |
| `JEV_LOG_FILE` | 把日志同时写入该文件并按 10 MiB 轮转、保留 3 份。Windows 服务用（SCM 不提供控制台，stderr 会被丢弃）；Linux 走 journald，不要设 |
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

```powershell
go mod tidy                        # 首次：拉取依赖并生成 go.sum（需联网）
go build -o .\jev-safety-gateway.exe .\cmd\jev-safety-gateway
```

版本号、提交号与构建时间通过链接期注入（控制台登录页与壳层底部展示，见 `GET /api/version`）：

```powershell
$ver = (Get-Content .\web\package.json -Raw -Encoding UTF8 | ConvertFrom-Json).version
$sha = (git rev-parse --short HEAD)
$now = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
go build -trimpath -ldflags "-s -w -X main.version=$ver -X main.commit=$sha -X main.buildTime=$now" `
  -o .\jev-safety-gateway.exe .\cmd\jev-safety-gateway
```

三个 `-X` 都是可选的：不注入时版本显示 `dev`，提交号与构建时间回落到 Go 内嵌的 VCS 信息
（`debug.ReadBuildInfo()`），因此直接 `go build` 也能报出真实提交。`scripts\build-local-test.ps1`
已自动完成上述注入。Docker 构建上下文不含 `.git`（见 `.dockerignore`），镜像内的构建标识**只能**靠
`--build-arg VERSION/COMMIT/BUILD_TIME` 传入。

### 2. 启动

```powershell
# 数据库默认落在当前工作目录下的 .\data\jev-safety-gateway.db，目录会自动创建，无需额外配置
.\jev-safety-gateway.exe

# 想换位置再显式指定：
# $env:JEV_DB_PATH = "D:\somewhere\jev-safety-gateway.db"
```

看到下面两行即启动成功：

```
proxy listening on :8080
admin listening on 127.0.0.1:8081
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
- **忘记管理员口令** → 口令哈希是 `kv` 表里的单行 `admin_hash`。先**停止网关**并备份整个 `data\` 三件套，
  再只删这一行（`DELETE FROM kv WHERE "key"='admin_hash';`，库内其余配置与日志全部保留），重启后
  控制台会回到「首次配置」流程，此时 `JEV_ADMIN_PASSWORD` 也会重新生效。**不要用删库来重置口令**——
  那会连同全部配置与审计日志一起永久丢失。删除前请确认备份副本可读。

## 本地 test 打包与启动（Windows）

`scripts\` 下的仓库脚本只有两个，产物都是同一个测试包目录（默认 `out\jev-safety-gateway\`）加一个 zip，可整包拷到测试机运行：

| 脚本 | 作用 |
| --- | --- |
| `start-local-test.ps1` | 构建 + 启动，日常用这个 |
| `build-local-test.ps1` | 只打包 |

同目录的 `template-start-jev-safety-gateway.ps1` / `template-stop-jev-safety-gateway.ps1` / `template-README.txt` 是**打包模板**：
打包时被复制进测试包并去掉 `template-` 前缀。它们必须和 `jev-safety-gateway.exe` 同目录才能运行，
在仓库里直接跑只会报错——那不是构建失败。

### 构建并启动

```powershell
.\scripts\start-local-test.ps1                        # 前端 + 后端 → 打包 → 启动
.\scripts\start-local-test.ps1 -SkipFrontend -NoZip    # 只构建后端、不出 zip，再启动（改 Go 代码时最快）
.\scripts\start-local-test.ps1 -SkipBuild              # 不重新构建，直接重启已有测试包
```

构建前它会先停掉测试包里正在运行的网关——Windows 上 `jev-safety-gateway.exe` 被占用时 `go build` 无法覆盖它，
不先停就会构建失败。测试包里已有的 `.env` 默认保留，不会被 `.env.example` 覆盖。

**数据库落在仓库根目录的 `data\jev-safety-gateway.db`**，不是测试包目录：运行态和源码在同一棵树里，好找好备份，
构建脚本的 `-Clean` 也只清测试包，碰不到它。启动前会打印一行 `数据库：<路径>` 便于核对。想换位置用 `-DbPath`。
测试包目录里那份 `data\` 只在脱离仓库、直接用包内 `start-jev-safety-gateway.ps1` 启动（拷到测试机单独跑）时才会用到。

> **不要删除仓库 `data\` 下的任何文件**（`jev-safety-gateway.db` 及其 `-wal` / `-shm`）。它不受版本控制、
> `rm` 不进回收站、本机没有卷影副本——删掉就是永久丢失。注意 `-wal` 可能远大于主库（实测主库 40 KB /
> WAL 3.4 MB），数据主要落在 WAL 里，**单独留下主库没有意义，三者必须作为一个整体保护**。需要重置配置时
> 请先停止网关、备份副本，再取得明确同意后操作；忘记管理员口令的正确做法见「常见问题」，而不是删库。
> （测试包内 `out\<包名>\data\` 是一次性构建产物，`-Clean` 清掉它不受此限。）

| 参数 | 说明 |
| --- | --- |
| `-SkipBuild` | 跳过构建，直接启动已有测试包 |
| `-OutputDir <路径>` | 测试包目录，默认 `out\jev-safety-gateway` |
| `-DbPath <路径>` | 数据库路径，默认仓库根目录的 `data\jev-safety-gateway.db`；相对路径按仓库根解析 |
| `-RefreshEnv` | 用 `.env.example` 覆盖测试包内的 `.env`（重置上游地址与密钥） |
| `-NoRestart` | 包内已有网关在运行时不再自动停止，直接报错退出 |
| `-Clean` | 构建前清空测试包目录（包内 `.env` 与 `data\` 一并删除；仓库根目录的 `data\` 不受影响） |
| `-SkipFrontend` / `-Offline` / `-NoZip` | 透传给 `build-local-test.ps1` |
| `-ProxyAddr` / `-AdminAddr` | 覆盖监听地址，默认 `:8080` / `127.0.0.1:8081` |

### 只打包

```powershell
.\scripts\build-local-test.ps1                 # 前端 + 后端 → out\jev-safety-gateway\ + zip
.\scripts\build-local-test.ps1 -SkipFrontend   # 只构建后端（前端沿用仓库已提交产物）
.\scripts\build-local-test.ps1 -Offline        # 不联网：缺少 web\node_modules 时直接报错
.\scripts\build-local-test.ps1 -KeepEnv        # 保留测试包里已有的 .env（不被 .env.example 覆盖）
```

| 参数 | 说明 |
| --- | --- |
| `-OutputDir <路径>` | 输出目录，默认 `out\jev-safety-gateway`，必须位于仓库内 |
| `-SkipFrontend` | 跳过前端构建 |
| `-SkipBackend` | 跳过后端构建（不能与 `-SkipFrontend` 同时使用） |
| `-Offline` | 不访问网络，缺少前端依赖时报错退出 |
| `-NoZip` | 不生成 zip |
| `-Clean` | 构建前清空输出目录 |
| `-KeepEnv` | 输出目录已有 `.env` 时保留，不用 `.env.example` 覆盖 |

产物结构：

```
out\jev-safety-gateway\
  jev-safety-gateway.exe  Go 后端二进制，管理控制台前端经 //go:embed 内嵌
  .env               由 .env.example 生成的初始化配置，需填写上游地址与密钥
  start-jev-safety-gateway.ps1  启动脚本（读取同目录 .env，数据库默认 data\jev-safety-gateway.db）
  stop-jev-safety-gateway.ps1   停止脚本（按 jev-safety-gateway.pid 或进程路径匹配停止）
  README.txt         包内使用说明（含构建时间、提交号与前端状态）
out\jev-safety-gateway-<日期>-<短提交>.zip
```

在测试机上：解压 → 编辑 `.env` → 运行 `.\start-jev-safety-gateway.ps1` → 浏览器打开 `http://127.0.0.1:8081` 完成首次配置。

## 项目结构

```
cmd/jev-safety-gateway/    入口、环境变量初始化；Windows 服务入口（entry_windows.go）
internal/config/           SQLite 存储：设置、密钥、日志、统计
internal/extract/          按 API 路径提取用户输入
internal/jev/              JEV 评估客户端（多密钥轮询 + 重试）
internal/proxy/            过滤反向代理（判定 → 放行/拦截）
internal/logx/             日志（JEV_LOG_FILE 文件输出 + 按大小轮转）
internal/admin/            管理 API + 口令鉴权
internal/server/           两个监听器装配（代理 :8080 / 控制台 127.0.0.1:8081）
web/                       管理控制台前端工程（Vue 3 + Vite + TS + Tailwind）
  src/                       源码（路由、模块、组件、接口封装）
  dist/                      构建产物，随源码入库并经 //go:embed 内嵌
nginx/gateway.conf         nginx 反代示例
scripts/                   Windows 本地 test 打包与启动、Windows 服务安装/卸载
deploy/                    Linux 部署产物（systemd unit、install.sh、env.example）
.github/workflows/         CI 与发布（打 tag 出全平台包 + 推 ghcr 镜像）
docs/deployment.md         部署指南（三平台安装/升级/备份、单实例约束、迁移注意）
Dockerfile, docker-compose.yml
```

## 安全提示

- 管理控制台（`127.0.0.1:8081`）默认只绑本机。生产环境请置于内网，或在 nginx 层叠加 IP 白名单 / TLS / basic auth。
  远程访问请用 SSH 隧道（`ssh -L 8081:127.0.0.1:8081 <user>@<host>`），**不要**改绑 `0.0.0.0`——控制台除自身登录外没有别的保护。
- 网关本身不校验客户端 apikey；对客户端的鉴权仍由上游（new-api 等）负责。网关只做内容安全过滤。
- JEV 密钥、管理员口令哈希保存在 SQLite（Docker volume `jev-safety-gateway-data`），请妥善保管该卷。
- **`GET /api/version` 是唯一免鉴权的 `/api/` 路由**：登录页需要在拿到 token 之前显示版本号与守护进程地址。
  它只返回构建标识（版本、短提交号、构建时间、Go 版本）与实际绑定的监听地址，不含任何审计数据；
  审计日志派生的延迟分位在受保护的 `GET /api/stats/latency` 下。若这条公开面不可接受，
  请把管理口限制在内网——不要靠给它加 token 来收紧，那会让登录页退回降级态。
