# 部署指南

三种部署方式，按推荐顺序：**Docker / docker-compose** → **Linux 裸机（systemd）** → **Windows 服务**。
功能完全一致，差别只在安装、日志与升级的落地方式。

先读「[共同的硬约束](#共同的硬约束先读这段)」一节——它决定了你不能怎么部署，三种方式都适用。

## 选哪种

| | Docker / compose | Linux 裸机 | Windows 服务 |
|---|---|---|---|
| 适用 | 首选，升级最省事 | 已有 Linux 主机、不想引入 Docker | Windows 主机 |
| 安装 | `docker compose up -d --build` | `sudo ./install.sh` | `.\scripts\install-service.ps1` |
| 数据位置 | 部署目录下的 `./data/` | `/var/lib/jev-safety-gateway/` | `%ProgramData%\jev-safety-gateway\data\` |
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
| Docker | 部署目录下的 `./data/` | compose 把它绑定挂载到容器内 `/data`（旧版用具名卷，见下方迁移） |
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
git clone https://github.com/dark-hxx/jev-safety-gateway.git
cd jev-safety-gateway
cp .env.example .env               # 按需填写；全部可留空，之后在控制台配置
mkdir -p ./data ./geoip
sudo chown -R 10001:10001 ./data   # Linux 必需，见下
docker compose up -d --build
```

- 代理监听 `:8080`（给 nginx 或客户端）
- 控制台映射到宿主机 `127.0.0.1:8081`，**只在本机可访问**
- 数据库落在部署目录的 `./data/`（绑定挂载到容器 `/data`）——宿主上直接可见，备份就是打包这个目录
- GeoIP 库放在部署目录的 `./geoip/`（只读挂载到容器 `/geoip`），把两个 `.mmdb` 放进去即生效

> **为什么必须 `chown`**：镜像以非 root 的 `app`（uid/gid 10001）运行。具名卷会由镜像里
> `/data` 的属主初始化，而**绑定挂载不会**——它直接用宿主目录的属主，root 或你自己的 uid
> 都不是 10001，于是进程建不了库文件，容器反复重启，日志里是
> `open store: … permission denied`。Docker Desktop（Windows / macOS）一般不受影响。
> 只读的 `./geoip` 不用改属主。

镜像里的版本号由 Dockerfile 从 `web/package.json` 自动解析，所以默认构建也会报出真实版本而不是 `dev`。
提交号与构建时间不在构建上下文里（`.git` 被 `.dockerignore` 排除），想带上就显式传：

```bash
JEV_COMMIT=$(git rev-parse --short HEAD) \
JEV_BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
docker compose up -d --build
```

### 从旧版具名卷迁移（一次性）

旧版 compose 把数据库放在具名卷 `jev-safety-gateway-data` 里，现在改为绑定挂载 `./data`。
**用旧版部署过的，升级前务必先把卷里的数据搬过来**，否则 `up -d` 之后容器会以一个空库启动
（旧卷不会被自动删除，数据仍在，搬完确认无误后再留着做保险）：

```bash
docker compose stop
mkdir -p ./data
docker run --rm \
  -v jev-safety-gateway-data:/from \
  -v "$PWD/data":/to \
  alpine sh -c 'cp -a /from/. /to/'
sudo chown -R 10001:10001 ./data   # 上面是 root 写的文件，得改回容器用户的属主
docker compose up -d --build
```

（PowerShell 把 `"$PWD/data"` 写成 `"${PWD}/data"`。）若跳过这步而以空库启动，需要重新设
管理员口令、重填上游地址与 JEV 密钥；审计日志与封禁规则仍留在旧卷里，恢复步骤同上。

### 配置

打开 `http://<主机>:8081` 完成首次设置：管理员口令 → 上游地址 → 至少一个 JEV 密钥。
远程机器用 SSH 隧道，不要把 8081 暴露到公网：

```bash
ssh -L 8081:127.0.0.1:8081 <user>@<host>
```

### 与别的 compose 项目互通网络

上游（new-api 等）在**另一套 compose / 另一台容器**里时，网关解析不到它的服务名：容器名只在
**同一张用户自定义网络**内可解析，而两个 compose 项目默认各建一张网，日志里是

```
upstream proxy error: dial tcp: lookup newapi on 127.0.0.11:53: no such host
```

（`127.0.0.11` 是 Docker 的内嵌 DNS。）先查出上游的网络名与别名：

```bash
docker network ls
docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} aliases={{$v.Aliases}}{{"\n"}}{{end}}' <上游容器名>
```

再让网关加入那张网（`name:` 填查到的实际网络名，如 `new-api_default`）：

```yaml
services:
  jev-safety-gateway:
    # ...原有内容不动...
    networks:
      - default
      - newapi            # 新增

networks:
  newapi:
    external: true
    name: <实际网络名>
```

`docker compose up -d` 重建网关容器即可。两点注意：

- 服务一旦写了 `networks:` 就**只**加入列出的网络，漏掉 `default` 会把网关从自己那张网里摘出去。
- 不用重启上游，也不用写 `depends_on`：网关不在启动时解析上游，而是每个请求现解析，接上网络后下一个请求就通。

别名里没有 `newapi`（服务名可能是 `new-api` 之类）时，把上游地址改成 `http://别名:端口`。
它已经写进库的话改 `.env` 无效，得去控制台改（原因见 README 的说明：上游只在库里该值为空时写入）。

### 升级

```bash
docker compose stop                 # 优雅停机，在途请求放完
# 备份，见下
docker compose up -d --build
```

> 从旧版具名卷升级上来的，先做一次上文的「从旧版具名卷迁移」，再做这一步。

### 备份与恢复

数据库就在部署目录的 `./data/` 下，`.db` / `-wal` / `-shm` 三件套都在里面，直接打包整个目录即可。

```bash
# 备份（必须先停：WAL 停止写入后再打包）
docker compose stop
tar czf "jev-safety-gateway-$(date +%F).tar.gz" -C ./data .
docker compose start
```

```bash
# 恢复（先把现有目录挪走而不是删掉，出问题还能退回来）
docker compose stop
mv ./data "./data.before-restore-$(date +%F)"
mkdir -p ./data
tar xzf <备份文件>.tar.gz -C ./data
docker compose start
```

> **`./data/` 里任何一个文件都不要单独删**。数据主要落在 WAL（`-wal` 常远大于主库），
> 只留主库等于没备份；`rm` 不进回收站、没有卷影副本，删掉就是永久丢失。

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

## 可选 GeoIP（国家 / ASN 归属）

IP 风险分析页的「全球与区域威胁来源分布」「攻击特征与 ASN 映射」两块面板，以及危险榜 /
实时封禁列表的行内国旗，需要本地 GeoIP 数据库才会出数据；**未配置时面板保持「未接入」占位，
其余功能不受影响**。

约束与定位：

- **不随发行物捆绑**。GeoLite2 有其许可条款，且本项目坚持离线，运营方自备数据库文件。
  从 MaxMind 免费账号下载 `GeoLite2-Country.mmdb` 与 `GeoLite2-ASN.mmdb`（两者独立，
  只配一个也能工作，另一维度保持未接入）。
- **纯本地只读、不上热路径**。归属解析只发生在管理口 `/api/stats/ip` 的聚合查询里，
  代理转发（`proxy.ServeHTTP`）从不调用它，运行时也不联网。
- **缺失即降级**。路径为空、文件不存在或打不开 → 该维度关闭、启动不失败、面板保持未接入。

用两个**基础设施环境变量**开启（每次启动读取，与 `JEV_DB_PATH` 同类，**不写入数据库、
控制台里改不了**）：

| 变量 | 指向 |
|---|---|
| `JEV_GEOIP_COUNTRY_DB` | 本地 `GeoLite2-Country.mmdb` 的路径 |
| `JEV_GEOIP_ASN_DB` | 本地 `GeoLite2-ASN.mmdb` 的路径 |
| `JEV_GATEWAY_LAT` / `JEV_GATEWAY_LON` | 可选：本网关的部署坐标（十进制度，WGS84），见下 |

`JEV_GATEWAY_LAT` / `JEV_GATEWAY_LON` 只用来源图上的**中心节点**：各来源标点向它汇聚。
网关自身的公网出口 IP 离线不可知（可能藏在 nginx 或 NAT 之后），所以这个位置只能由运维
**声明**，不做推断。两个都必须给且落在纬度 −90..90、经度 −180..180 才算声明；只给一个、
给不出数或超出范围会被忽略并打一行日志，等同于没配（图里只画来源标点，不画中心节点与
弧线，也不把网关钉在 0,0 这个真实坐标上）。例：上海约 `31.23` / `121.47`，法兰克福约
`50.11` / `8.68`。

三平台落地：

- **Docker / compose**：`docker-compose.yml` 已把 `./geoip` 只读挂到容器 `/geoip`，两个 env 的默认值
  也已指向容器内路径——**把 `GeoLite2-Country.mmdb` / `GeoLite2-ASN.mmdb` 放进部署目录的 `./geoip/`
  即可**，不用改任何配置（目录为空 = 该维度关闭；`.mmdb` 已在 `.gitignore` 里，不会被误提交）。
- **Linux 裸机（systemd）**：在 `/etc/jev-safety-gateway/env` 里填**绝对路径**（见 `env.example` 注释）。
  若 unit 开了沙箱（`ProtectSystem` / `ReadOnlyPaths`），数据库须放在服务可读的路径下，
  例如 `/etc/jev-safety-gateway/geoip/`。
- **Windows 服务**：`install-service.ps1` 的 `-EnvFile` 会把 `KEY=VALUE` 写进服务注册表的
  `Environment`，两条 GeoIP 变量走同一通道即可，值填绝对路径。

配置后重启进程；启动日志会打印激活了哪几个维度（`geoip: country database active …` /
`geoip: ASN database active …`，以及声明了部署位置时的 `geoip: gateway location declared at …`），
未配置或打不开则记为 disabled。

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
sudo cp nginx/gateway.conf /etc/nginx/conf.d/ && sudo nginx -t && sudo nginx -s reload
```

配置里透传 `X-Real-IP` / `X-Forwarded-For` 是必需的：网关按来源 IP 做滥用封禁，少了这两个头，
所有客户端会被算成同一个来源。示例用的是**覆盖**（`$remote_addr`）而不是追加
（`$proxy_add_x_forwarded_for`）：网关取 `X-Forwarded-For` 的**第一段**，追加会把客户端自带的头
拼到最前面，来源 IP 就由客户端自己说了算——既能绕过封禁，也能借封禁规则去封掉别人的 IP。
覆盖的前提是**网关只经这一层访问**；nginx 前面还有 CDN / 另一级代理时要改成信任链的写法，
否则拿到的是上一跳的地址。

> ⚠️ 网关对 `X-Forwarded-For` 的信任是**无条件**的（`clientIP` 不做可信代理判断，这是刻意的，
> 见 `internal/proxy/proxy.go` 的注释）。所以「代理口只绑 127.0.0.1」不是加固建议而是这套配置的
> **前置条件**——见下一节。

## 公网加固（防端口扫描）

公网上的扫描器会拿成百上千个路径打入口：`/.env`、`/wp-login.php`、`/actuator/env`、
`/phpmyadmin/`……它们既占审计表、又白白消耗上游连接。三层分工，**前两层装上就有，第三层按需**：

| 层 | 位置 | 作用 | 默认 |
|---|---|---|---|
| 端口收敛 | `docker-compose.yml` / 宿主防火墙 | 代理口只对本机开放，公网只能经 nginx | Docker 已生效，裸机需自查 |
| nginx 444 | `nginx/gateway.conf` | 扫描特征路径直接断连：不进网关进程、不写审计行 | 已生效 |
| 网关路径白名单 | 控制台 →「设置 → 安全防护 → 路径白名单」 | 非已知接口路径返回 404，可计入自动封禁 | **关闭** |

### 为什么先做端口收敛

Docker 默认的 `-p 8080:8080` 会把代理口直接放到公网，那有两条路可绕过 nginx：扫描器直连 8080
就跳过了全部 nginx 规则；客户端也能自己伪造 `X-Forwarded-For`，既绕过封禁、也能把任意 IP 封掉。
所以 compose 现在发布的是 `127.0.0.1:8080:8080`。容器内仍监听所有接口，同一 compose 网络里的
nginx 用服务名 `jev-safety-gateway:8080` 照样直连。

**裸机同理，但 unit 写死的是 `:8080`（所有接口）**，需要你自己确认公网进不来，例如只放行 nginx 所在链路：

```bash
# 只允许本机回环访问 8080（nginx 与网关同机时）
sudo iptables -A INPUT -p tcp --dport 8080 ! -i lo -j DROP
```

不这么做的话，下面两层的第三层（以及网关的滥用封禁）都能被直连绕过。

### nginx 那层拦了什么

`nginx/gateway.conf` 里默认启用的是一份**扫描特征 deny-list**：隐藏路径段（`/.`）、脚本与
配置/密钥后缀（`.php` … `.sql` … `.pem`）、常见后台与中间件路径（`wp-admin` / `actuator` /
`phpmyadmin` …）、以及 `GET /`。命中即 `return 444`——不回状态码也不回 banner，扫描器拿不到
任何指纹，成本也最低。`.json` / `.yml` / `.log` 这类后缀**刻意没有**列入，因为它们可能是正常
接口路径的一部分。

文件末尾还留了一段**注释掉的严格白名单**（`^/(v1|v1beta)/` 之外一律 444）。它比 deny-list 严格
得多，但有个坑写在注释里：**nginx 的前缀清单必须始终是控制台「路径白名单」前缀的超集**，否则
在控制台里新加的前缀会在 nginx 就被 444，请求根本到不了网关、控制台看起来毫无反应。两边不一致
是这套配置最容易踩的坑，所以默认不启用。

### 第三层：网关内置路径白名单

nginx 的 deny-list 只能拦「猜得到的坏路径」，对付不了「猜不到的坏路径」，也管不了审计留痕与自动
封禁。这一层由网关自己做，配置在控制台里，**默认关闭**：

- **路径白名单**（`path_allowlist_enabled`）：开启后，既不在内置接口表里、也不在下方前缀里的路径，
  直接返回 404，**不转发上游**。404 的响应体刻意做成上游 `not_found` 的形状，且不带
  `X-JEV-Gateway` 头，扫描器无法从响应里认出网关。审计行记为 `kind=path` / `decision=block`，
  与控制台里「内容拦截」区分开。
- **前缀清单**（`path_allowlist_prefixes`，默认 `/v1/,/v1beta/`）：裸机部署时若客户端走的是
  `https://api.example.com/`（前缀被 nginx 剥掉）之类，把这些前缀加进来即可。留空回落默认值。
- **未知路径计入自动封禁**（`abuse_count_unknown_path`）：每次路径拒绝都记一次滥用计数，达到
  「封禁阈值」后按「封禁时长」封掉该 IP——扫描器会被自动挡在 429 上。需要同时开「滥用防护」。

三点注意：

- 这三项都**默认关闭**，升级不会改变现有行为。
- **IP 白名单不解救路径门禁**。白名单的作用一直只是免除 IP 封禁，从不绕过内容过滤，路径门禁
  遵循同一约定——否则给扫描器加个白名单反而把它放进来了。
- `/healthz` 不在接口表里，但它是网关自己的探活路径，仍会正常响应；除它之外，开了路径白名单后
  任何非 API 路径（含 `GET /`）都会被 404。

### 验证

关掉这两层之前，先确认你的客户端确实在用的路径都还在：

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://<你的域名>/v1/models     # 期望 200 或上游返回的状态
curl -s -o /dev/null -w '%{http_code}\n' https://<你的域名>/wp-login.php  # 期望 000（444 断连）
```

改完 nginx 先用 `nginx -t` 检查语法再 `reload`；`return 444` 在浏览器里表现为「连接被重置」，
这是预期行为，不是配置错误。

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
| 正常接口被 404、审计里 `kind=path` | 开了「路径白名单」但客户端前缀没列进「前缀清单」。补上前缀，或先关掉白名单 |
| 扫描器能绕过 nginx / 冒充来源 IP | 代理口被直接暴露了。compose 用 `127.0.0.1:8080:8080`，裸机加防火墙，见「公网加固」 |

## 相关文档

- `README.md` — 配置项清单、请求示例、分阶段冒烟测试、本地开发
- `CLAUDE.md` — 代码结构与请求流程（给改代码的人看）
