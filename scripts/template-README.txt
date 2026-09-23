JEV 安全网关 · 本地测试包
=========================

构建时间：@@BUILD_TIME@@
提交版本：@@COMMIT@@
控制台版本：@@VERSION@@
前端状态：@@FRONTEND@@

包内容
------
  jev-safety-gateway.exe  Go 后端二进制，管理控制台前端已内嵌（//go:embed），无需 Node/Go
  .env               一次性初始化配置（上游地址、JEV 接口地址、调用密钥、管理员口令）
  start-jev-safety-gateway.ps1  启动脚本
  stop-jev-safety-gateway.ps1   停止脚本
  README.txt         本说明

本目录就是运行目录
------------------
  start-jev-safety-gateway.ps1 / stop-jev-safety-gateway.ps1 必须和 jev-safety-gateway.exe 放在同一目录才能运行。
  仓库里 scripts\template-*.ps1 只是打包模板，直接运行会因为同目录没有 jev-safety-gateway.exe
  而报错——请在仓库根目录执行 scripts\start-local-test.ps1 一步构建并启动。

快速开始
--------
1. 解压到任意目录（路径不要含特殊字符）。
2. 用记事本编辑 .env，至少填写上游地址与 JEV 调用密钥：

     JEV_UPSTREAM_URL=http://127.0.0.1:3000
     JEV_BASE_URL=https://api.typesafe.ai
     JEV_API_KEY=apikey_xxxxxxxx

   已存在的配置不会被这些变量覆盖，后续修改请到控制台完成。

3. 在该目录打开 PowerShell（或直接右键 start-jev-safety-gateway.ps1 → 使用 PowerShell 运行）：

     .\start-jev-safety-gateway.ps1

   若提示脚本被禁止运行，可执行：

     powershell -ExecutionPolicy Bypass -File .\start-jev-safety-gateway.ps1

4. 浏览器打开 http://127.0.0.1:8081
   - 首次进入需设置管理员口令（至少 6 位）
   - 登录后在「网关配置」确认上游地址，并确认已添加至少一个 JEV 调用密钥

5. 停止：在另一个窗口运行 .\stop-jev-safety-gateway.ps1，或在启动窗口按 Ctrl+C。

端口与数据
----------
  代理监听     :8080  （业务流量入口，nginx 转发到这里）
  管理控制台   :8081  （默认仅本机可访问）
  数据库       data\jev-safety-gateway.db（SQLite，纯 Go 实现，无需额外服务）

可用环境变量（覆盖默认值，均在启动脚本中生效）
----------------------------------------------
  JEV_DB_PATH     数据库路径，默认 <包目录>\data\jev-safety-gateway.db
  JEV_PROXY_ADDR  代理监听地址，默认 :8080
  JEV_ADMIN_ADDR  控制台监听地址，默认 :8081

常见问题
--------
  * 端口被占用：先运行 stop-jev-safety-gateway.ps1，或改用 JEV_PROXY_ADDR/JEV_ADMIN_ADDR 换端口。
  * 请求一直 error/放行：多为 JEV 密钥无效或网络不通，看启动窗口的 jev evaluation error 日志。
  * 想临时全放行：登录控制台关闭「启用过滤」总开关。
  * 忘记管理员口令：删除 data\jev-safety-gateway.db 后重启，可重新初始化（历史配置与日志同时丢失）。

安全提示
--------
  控制台默认只绑定本机；生产环境请置于内网，或在网关层叠加 TLS / IP 白名单 / 认证。
  本包内 .env 可能包含密钥与初始口令，请勿提交到代码仓库或随意外发。
