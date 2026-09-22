# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

JEV Safety Gateway is a content-filtering reverse proxy for LLM API traffic. It sits between clients and an OpenAI-compatible upstream (e.g. new-api), extracts the user input from each request, asks the TypeSafe/JEV model whether it is safe, and either forwards safe requests transparently or returns HTTP 403. Written in pure Go (no CGO — SQLite is `modernc.org/sqlite`).

## Build & Run

Requires Go 1.23+.

```bash
go mod tidy                          # first time / after dependency changes (generates go.sum)
go build -o gateway ./cmd/gateway    # produces gateway.exe on Windows
go vet ./...                         # static checks

# run locally (default /data path is unwritable on Windows, so override it)
JEV_DB_PATH=./data/gateway.db JEV_PROXY_ADDR=:8080 JEV_ADMIN_ADDR=:8081 ./gateway
# PowerShell: $env:JEV_DB_PATH="./data/gateway.db"; ./gateway.exe

# Docker
docker compose up -d --build
```

Success prints `proxy listening on :8080` and `admin listening on :8081`.

There is currently no test suite in the repo (`go test ./...` finds no tests).

## Two listeners, one process

The process runs two independent HTTP servers (`internal/server`):
- **Proxy (`:8080`, public)** — the filtering reverse proxy that all client LLM traffic hits. Sits behind nginx (`nginx/gateway.conf`). Exposes `/healthz`.
- **Admin (`:8081`, private, bind to 127.0.0.1)** — the config API under `/api/*` plus the embedded single-page console. All `/api/*` routes except `login`/`setup`/`setup-status` require a bearer token from `POST /api/login`.

The gateway does **not** authenticate client apikeys — that remains the upstream's job. It only does content safety filtering.

## Request flow (the core path)

`internal/proxy/proxy.go` `ServeHTTP` → `decide`:
1. If an IP is already abuse-banned, reject with 429 before any JEV call (fast path).
2. Read+buffer the body (needed to both inspect and forward).
3. `decide` returns one of four decisions logged to SQLite: **allow / block / skip / error**.
   - Skips non-POST/PUT/PATCH, non-JSON, disabled gateway, or bodies with no extractable input — these forward untouched.
   - Otherwise `extract.Extract` pulls user text by path, `jev.Score` evaluates it, and the score is compared against `SafetyThreshold`.
4. A genuine harmful block (score present) records an abuse "strike"; enough strikes in the window ban the IP.
5. Safe requests forward via `httputil.ReverseProxy` with `FlushInterval` set so **SSE streaming is preserved**.

**Critical:** enabling `CheckResponse` switches to `forwardChecked`, which buffers the entire upstream response to audit it — this **breaks streaming**. Off by default.

## Package responsibilities

- `internal/config` — SQLite store (`store.go`) + data models (`models.go`). Single-connection (`SetMaxOpenConns(1)`) WAL-mode DB. Settings are JSON-blobbed into a `kv` table and cached in memory; `jev_keys` and `logs` are normal tables. `Settings()` returns a cached copy; `UpdateSettings` validates + persists + refreshes the cache.
- `internal/extract` — path-based input extraction. `Extract(path, body)` dispatches on path suffix (`/chat/completions`, `/messages`, `:generateContent`, etc.) to shape-specific parsers, focusing on user/system roles; unknown paths fall back to `extractGeneric` (collects every string leaf) so nothing goes unchecked. `Output` extracts model-generated text from responses (JSON or SSE) for response auditing. `Clamp` is rune-safe truncation to `MaxStateChars`.
- `internal/jev` — client for `POST {base}/v1/systemone`. Sends a `noul` question; response `noul` is a 0–1 probability. **Round-robins over enabled keys** (atomic cursor) and **retries the next key on 401/429/529** (or network errors); other statuses fail immediately. `KeyProvider` is an interface implemented by a `keyAdapter` in `internal/server` — the jev package does not import config.
- `internal/abuse` — in-memory, process-local sliding-window strike counter + temporary IP bans. Resets on restart (fine for single-instance).
- `internal/admin` — config API + embedded UI. In-memory bearer sessions with sliding 12h expiry (`auth.go`); bcrypt admin password hash stored in the `kv` table.
- `web` — dependency-free SPA (`index.html`/`app.js`/`style.css`) embedded via `//go:embed` (`embed.go`).

## Scoring semantics (easy to get backwards)

The `noul` score means **higher = safer** when the instruction is phrased "is this safe?". `BlockIfBelow=true` blocks when `score < threshold`. If you rephrase `SafetyInstruction` as "is this harmful?", you must flip `BlockIfBelow` to false (blocks when `score >= threshold`). The `X-JEV-Gateway` (allow/blocked/banned) and `X-JEV-Score` response headers reflect the outcome.

`FailOpen` controls behavior when JEV is unreachable: true forwards anyway (error decision), false blocks (fail-closed).

## Configuration model

Settings are runtime-editable from the admin console and persisted in SQLite — they are the source of truth. Environment variables (`JEV_UPSTREAM_URL`, `JEV_BASE_URL`, `JEV_API_KEY`, `JEV_ADMIN_PASSWORD`) only **bootstrap first-run values and never overwrite existing DB values** (see `bootstrap` in `cmd/gateway/main.go`). Infra vars `JEV_DB_PATH` / `JEV_PROXY_ADDR` / `JEV_ADMIN_ADDR` are read every start.

## Notes

- Default `SafetyInstruction` and `BlockMessage` are Chinese; the README is Chinese. Match the existing language in user-facing strings.
- Logging to SQLite is best-effort (errors ignored) — never let logging failures break the request path.

<comet-ambient-resume>
<!-- Managed by Comet. Edits inside this block may be replaced by comet init/update. -->
<!-- Contract: comet.resume_probe.v2 -->

## Comet Ambient Resume

在这个仓库中，开始处理需要改动或调查的任务前，如果可能存在活跃 Comet workflow，把当前用户请求传入只读探针：`comet resume-probe . --stdin --json`。

- 如果用户通过宿主明确调用任意 Comet Skill（例如 `@comet`、`/comet`、`@comet-native` 或 `/comet-hotfix`），显式调用优先于本恢复协议；不要运行 resume probe，直接进入被调用的 Skill。
- 如果用户通过宿主明确调用的是非 Comet 的 Skill 或斜杠命令，任务意图已由该调用明确：不要运行 resume probe，直接执行该 Skill。
- 如果你正在 Comet 流程内（包括正在等待用户回复你在流程中提出的问题），不要运行 resume probe；把这类回复（例如方案/选项选择）当作当前 change 的继续，直接按用户的选择推进。
- 只信任返回的 `workflow`、`skill` 和 `entrySource`；它们只由项目配置或无配置兼容回退决定。不得扫描或切换另一套 workflow。
- 如果 probe 返回 `auto_resume`，简短说明选中的 active change，并进入 `nextCommand` 指向的永久入口。不要把状态命令当作恢复入口直接推进。
- 如果 probe 返回 `ask_user`，只问一个简短问题并等待用户回复。
- 如果当前请求未明确调用 Comet Skill，且 probe 返回 `out_of_scope` 或 `none`，不要进入 Comet workflow。
- `out_of_scope` 或 `none` 只表示不要因为这个新请求进入 Comet workflow；它绝不表示要暂停或退出一个已在进行的 Comet 流程。
- 如果配置或状态无效且没有 `nextCommand`，停止并报告原因；不要猜测另一个 workflow。
- 不能只因为存在 active change 就把无关任务挂到该 change。Native 的未提交改动由 Native 入口检查，不由探针自动归因。
</comet-ambient-resume>
