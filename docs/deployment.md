# 部署指南

三种部署方式，按推荐顺序：**Docker / docker-compose** → **Linux 裸机（systemd）** → **Windows 服务**。
功能完全一致，差别只在安装、日志与升级的落地方式。

先读「[共同的硬约束](#共同的硬约束先读这段)」一节——它决定了你不能怎么部署，三种方式都适用。

## 选哪种

| | Docker / compose | Linux 裸机 | Windows 服务 |
|---|---|---|---|
| 适用 | 首选，升级最省事 | 已有 Linux 主机、不想引入 Docker | Windows 主机 |
| 安装 | `docker compose up -d --build` | `sudo ./install.sh` | `.\scripts\install-service.ps1` |
| 数据位置 | 具名卷 `jev-safety-gateway-data` | `/var/lib/jev-safety-gateway/` | `%ProgramData%\jev-safety-gateway\data\` |
| 日志 | `docker compose logs` | `journalctl -u jev-safety-gateway` | `%ProgramData%\jev-safety-gateway\logs\gateway.log` |
| 开机自启 | `restart: unless-stopped` | systemd `enable` | SCM `start= auto` |
| 崩溃重启 | 同上 | `Restart=always` | SCM failure actions |
| 优雅停机 | `docker stop` | `systemctl stop` | `sc.exe stop` |

三者都不需要 CGO / gcc：依赖是纯 Go 的 `modernc.org/sqlite`，二进制静态链接。

## 共同的硬约束（先读这段）

### 只能跑单实例

**同一份数据库上绝不能同时运行两个网关进程。** 原因有两条，都在代码里：

- 滥用防护的计数是**进程内存**里的滑动窗口（`internal/abuse`），进程重启即清零。两个副本各算各的，
  同一个 IP 在一个副本被封、在另一个副本照常放行，封禁形同虚设。
- SQLite 是单文件 WAL，`internal/config` 用 `SetMaxOpenConns(1)` 刻意串行化写入。两个进程写同一个
  库会互相争锁，`busy_timeout` 到了就报错。

推论：

- 不要给这个服务配多副本 / 负载均衡的横向扩容。
- 升级时不能新旧两个实例同时在线（滚动更新、蓝绿都不行），只能**停 → 换 → 起**。
- 升级有短暂中断。停机是优雅的：`server.Run` 会给在途请求最多 10 秒把响应放完（含 SSE 流），
  所以中断窗口通常只有这一两秒加上进程启动时间。

### 升级前必须备份

数据库是 SQLite 三件套：`jev-safety-gateway.db`、`.db-wal`、`.db-shm`。

**`-wal` 可能远大于主库**（实测主库 40 KB / WAL 3.4 MB），数据主要落在 WAL 里——只留主库等于没备份。
备份必须整体做，且要在**服务停止后**做，否则 WAL 可能还在被写入。

各方式的备份命令见对应章节。

### 表结构变更需要手工处理

`internal/config/store.go` 的 `migrate()` 只有 `CREATE TABLE IF NOT EXISTS`，没有 `ALTER`，也没有
`PRAGMA user_version`。这带来两种截然不同的升级行为：

- **加配置项：安全。** 设置是以 JSON blob 存在 `kv` 表里的，`load()` 把 blob 解码在 `DefaultSettings()`
  **之上**，所以老库里缺的键会保持新版本的默认值，而不是读成零值。
- **给表加列：不安全。** `logs` 表已存在时 `CREATE TABLE IF NOT EXISTS` 会被跳过，新列不会补上，
  任何引用该列的查询会直接 `no such column` 失败。
- **加新表：安全。** 全新的表——例如持久化 IP 封禁/白名单规则的 `ip_rules` 表——由 `CREATE TABLE
  IF NOT EXISTS` 自动创建，不触碰既有表，升级后启动即生效、无需手工操作；老库首次启动时该表为空
  （不影响内存态 abuse 自动封禁，二者互补）。

所以：**升级前先看 release notes 有没有提到表结构变化**。如果有，按说明在服务停止时手工执行 `ALTER TABLE`，
再启动新版本。数据库里没有版本号可以查，无法从库本身判断它是什么结构——这也是必须保留备份的原因。

## 数据与配置的位置

四种口径，别搞混：

| 场景 | 数据库 | 说明 |
|---|---|---|
| 开发机直接跑 | 仓库根 `data/` | 相对启动时的工作目录。**不受版本控制，删了无法找回** |
| Docker | 具名卷 `jev-safety-gateway-data` | 挂到容器内 `/data` |
| Linux 裸机 | `/var/lib/jev-safety-gateway/` | systemd `StateDirectory` 管理 |
| Windows 服务 | `%ProgramData%\jev-safety-gateway\data\` | 安装脚本创建 |

配置**不**在这些文件里——运行时配置（上游地址、阈值、密钥、口令）全部存在 SQLite 里，是唯一真源。
环境变量只在**首次启动**时提供初始值，之后不再覆盖已存在的值（见 `cmd/jev-safety-gateway/main.go` 的 `bootstrap`）。

> 仓库里的 `data/` 是开发机的运行态，**不要删除其中任何文件**。`rm` 不进回收站、本机没有卷影副本，
> 删掉就是永久丢失。生产部署请用上表里各自的独立路径，与仓库树分开。

---

## 方式一：Docker / docker-compose

### 安装

```bash
cp .env.example .env      # 按需填写；全部可留空，之后在控制台配置
docker compose up -d --build
```

- 代理监听 `:8080`（给 nginx 或客户端）
- 控制台映射到宿主机 `127.0.0.1:8081`，**只在本机可访问**

镜像里的版本号由 Dockerfile 从 `web/package.json` 自动解析，所以默认构建也会报出真实版本而不是 `dev`。
提交号与构建时间不在构建上下文里（`.git` 被 `.dockerignore` 排除），想带上就显式传：

```bash
JEV_COMMIT=$(git rev-parse --short HEAD) \
JEV_BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
docker compose up -d --build
```

### 配置

打开 `http://<主机>:8081` 完成首次设置：管理员口令 → 上游地址 → 至少一个 JEV 密钥。
远程机器用 SSH 隧道，不要把 8081 暴露到公网：

```bash
ssh -L 8081:127.0.0.1:8081 <user>@<host>
```

### 升级

```bash
docker compose stop                 # 优雅停机，在途请求放完
# 备份，见下
docker compose up -d --build
```

### 备份与恢复

卷名已显式写死为 `jev-safety-gateway-data`，命令在任何机器上都一样。

```bash
# 备份（必须先停：WAL 停止写入后再打包）
docker compose stop
docker run --rm \
  -v jev-safety-gateway-data:/data \
  -v "$PWD:/backup" \
  alpine tar czf "/backup/jev-safety-gateway-$(date +%F).tar.gz" -C /data .
docker compose start
```

```bash
# 恢复（会先清空卷内容）
docker compose stop
docker run --rm \
  -v jev-safety-gateway-data:/data \
  -v "$PWD:/backup" \
  alpine sh -c 'rm -rf /data/* && tar xzf /backup/<备份文件>.tar.gz -C /data'
docker compose start
```

### 日志

```bash
docker compose logs -f
```

容器日志已配 `max-size: 10m` / `max-file: 3`，不会无限增长。

---

## 方式二：Linux 裸机（systemd）

### 安装

发布包解压后目录里就有 `install.sh`：

```bash
tar xzf jev-safety-gateway-<版本>-linux-amd64.tar.gz
cd jev-safety-gateway-<版本>-linux-amd64
sudo ./install.sh
```

脚本是幂等的，重复执行即为升级。它会：建系统用户 `jev-safety-gateway`、装二进制到
`/usr/local/bin/`、装 unit 到 `/etc/systemd/system/`、按需生成 `/etc/jev-safety-gateway/env`
（权限 `0600`）、`systemctl enable --now`。

**它从不删除、也从不覆盖数据库与配置。** 卸载（`sudo ./install.sh --uninstall`）同样保留它们，
只在末尾打印手工清理命令。

### 配置

```bash
sudo nano /etc/jev-safety-gateway/env      # 上游地址、JEV 密钥、管理员口令
sudo systemctl restart jev-safety-gateway
```

也可以留空，直接在控制台里配。远程访问控制台用 SSH 隧道：

```bash
ssh -L 8081:127.0.0.1:8081 <user>@<host>
```

监听地址由 unit 写死（`:8080` 与 `127.0.0.1:8081`），**不要**写进 `env` 文件——该文件里的同名变量会
覆盖 unit 里的安全默认值。需要改动请用 systemd drop-in：

```bash
sudo systemctl edit jev-safety-gateway
```

### 升级

```bash
# 备份，见下
sudo ./install.sh          # 重新解压新版本包后执行，幂等
```

### 备份与恢复

```bash
sudo systemctl stop jev-safety-gateway
sudo tar czf ~/jev-safety-gateway-$(date +%F).tar.gz \
  /var/lib/jev-safety-gateway /etc/jev-safety-gateway
sudo systemctl start jev-safety-gateway
```

恢复：停服务 → 解包覆盖两个目录 → 起服务。

### 日志

```bash
journalctl -u jev-safety-gateway -f
```

由 journald 负责轮转，**不要**设 `JEV_LOG_FILE`（那会让应用自己写文件并轮转，与 journald 重复）。

### 没有 CI 时的手工构建

交叉编译不需要目标平台的工具链：

```bash
VERSION=$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' web/package.json | head -n1)
COMMIT=$(git rev-parse --short HEAD)
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)

for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath \
    -ldflags="-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.buildTime=$NOW" \
    -o jev-safety-gateway ./cmd/jev-safety-gateway
  # 与 release.yml 相同的包内布局
  pkg="jev-safety-gateway-$VERSION-linux-$arch"
  mkdir -p "dist/$pkg" && cp jev-safety-gateway "dist/$pkg/" && \
    cp deploy/systemd/jev-safety-gateway.service deploy/systemd/env.example \
       deploy/linux/install.sh docs/deployment.md "dist/$pkg/" && \
    chmod +x "dist/$pkg/install.sh" && \
    tar czf "dist/$pkg.tar.gz" -C dist "$pkg"
done
```

---

## 方式三：Windows 服务

原生 Windows 服务，**不需要 nssm 之类的外部包装器**：二进制自身带服务支持
（`cmd/jev-safety-gateway/entry_windows.go`），`sc.exe` 直接指向 exe。

### 安装

以**管理员身份**打开 PowerShell：

```powershell
.\scripts\build-local-test.ps1        # 先产出二进制到 out\jev-safety-gateway\
.\scripts\install-service.ps1
```

脚本是幂等的，重复执行即为升级。它注册服务（`start= auto`）、设置崩溃自动重启
（5 秒后重试，最多 3 次，24 小时复位）、把基础设施环境变量写进服务的注册表
`Environment` 值，然后启动。

运行账号默认是 `LocalSystem`。要换成受限账号用 `-ServiceAccount`，并自行确保该账号对安装目录
有读写权限：

```powershell
.\scripts\install-service.ps1 -ServiceAccount 'NT SERVICE\jev-safety-gateway'
```

### 配置

密钥**不**默认写进注册表。装完后打开 `http://127.0.0.1:8081` 在控制台里配置管理员口令、上游地址
与 JEV 密钥——推荐这样做，注册表对管理员与 SYSTEM 可读。

需要无人值守首次配置时，用 `-EnvFile` 传入一个 `KEY=VALUE` 文件：

```powershell
.\scripts\install-service.ps1 -EnvFile .\.env
```

### 升级

```powershell
# 备份，见下
.\scripts\build-local-test.ps1
.\scripts\install-service.ps1        # 停服务 → 换二进制 → 再起；data\ 与 logs\ 原样保留
```

旧二进制会自动备份为 `jev-safety-gateway.exe.<时间戳>.bak`。

### 备份与恢复

```powershell
Stop-Service jev-safety-gateway
Compress-Archive -Path "$env:ProgramData\jev-safety-gateway" `
  -DestinationPath "$env:USERPROFILE\jev-safety-gateway-$(Get-Date -Format yyyyMMdd).zip"
Start-Service jev-safety-gateway
```

恢复：停服务 → 解压覆盖 → 起服务。

### 日志

SCM 不提供控制台，stdout/stderr 会被丢弃，所以安装脚本设了 `JEV_LOG_FILE`，应用自己写文件并按
10 MiB 轮转、保留 3 份：

```powershell
Get-Content "$env:ProgramData\jev-safety-gateway\logs\gateway.log" -Wait -Tail 50
```

### 卸载

```powershell
.\scripts\uninstall-service.ps1            # 保留数据库与日志
.\scripts\uninstall-service.ps1 -Purge     # 连数据一起删（需二次确认，且会先自动备份）
```

### 手工构建

`.\scripts\build-local-test.ps1` 已经完成版本注入（读 `web/package.json` + `git rev-parse`），
不需要额外步骤。

---

## 版本号与构建标识

版本号的真源是 **`web/package.json` 的 `version`**，不是 git tag（仓库没有打 tag 的习惯）。
`GET /api/version` 与控制台登录页展示的三项来自链接期注入：

| 字段 | 来源 | 缺失时 |
|---|---|---|
| `version` | `web/package.json` | Dockerfile 自行解析；Windows 打包脚本同样读取 |
| `commit` | `git rev-parse --short HEAD` | 回落 Go 内嵌的 `vcs.revision`（仅当 `.git` 在构建上下文里） |
| `builtAt` | 构建时刻（UTC） | 回落 `vcs.time` |

`GET /api/version` 是**唯一免鉴权的 `/api/` 路由**，登录页需要在拿到 token 前显示版本与守护进程地址。
它只返回构建标识与实际绑定的监听地址，不含审计数据。若这条公开面不可接受，请把管理口限制在内网——
不要靠给它加 token 来收紧，那会让登录页退回降级态。

## 反向代理

`nginx/gateway.conf` 是可直接使用的示例，默认 `upstream` 写的是 `127.0.0.1:8080`（裸机口径）。
若 nginx 与网关都在 compose 里，改成服务名 `jev-safety-gateway:8080`。

```bash
sudo cp nginx/gateway.conf /etc/nginx/conf.d/ && sudo nginx -s reload
```

配置里透传 `X-Real-IP` / `X-Forwarded-For` 是必需的：网关按来源 IP 做滥用封禁，少了这两个头，
所有客户端会被算成同一个来源。

## 排障

| 现象 | 多半是 |
|---|---|
| 控制台打不开 | 它只绑 `127.0.0.1:8081`。远程请用 SSH 隧道，不要改绑 `0.0.0.0` |
| 一直 `error` / fail-open 放行 | JEV 密钥无效或网络不通。看日志里的 `jev evaluation error` |
| 正常内容被拦 | 调「安全阈值」（默认 0.5）或改「安全判定指令」。分数越接近 1 越安全 |
| 有害内容放行 | 同上，反方向调 |
| 想临时全放行 | 控制台关掉「启用过滤」总开关 |
| 服务起不来（Windows） | 端口被占用、数据目录不可写、服务账号权限不足。看 `logs\gateway.log` |
| 服务起不来（Linux） | `journalctl -u jev-safety-gateway -n 50` |
| 忘了管理员口令 | **不要删库**。备份三件套后只删 `kv` 表里的 `admin_hash` 一行，详见 README「常见问题」 |
| 大文件上传被拒 | 检查 `reject_oversize_body`（默认关）。关着时超 8 MiB 的包体原样转发不送检 |

## 相关文档

- `README.md` — 配置项清单、请求示例、分阶段冒烟测试、本地开发
- `CLAUDE.md` — 代码结构与请求流程（给改代码的人看）
