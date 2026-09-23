# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

JEV Safety Gateway is a content-filtering reverse proxy for LLM API traffic. It sits between clients and an OpenAI-compatible upstream (e.g. new-api), extracts the user input from each request, asks the TypeSafe/JEV model whether it is safe, and either forwards safe requests transparently or returns HTTP 403. Written in pure Go (no CGO — SQLite is `modernc.org/sqlite`).

## Build & Run

Requires Go 1.23+.

```bash
go mod tidy                          # first time / after dependency changes (generates go.sum)
go build -o jev-safety-gateway ./cmd/jev-safety-gateway   # produces jev-safety-gateway.exe on Windows
go vet ./...                         # static checks
go test ./...                        # unit tests (config aggregation, admin API + SPA fallback, embedded dist)

# run locally — the DB lands in ./data/jev-safety-gateway.db relative to the working directory
JEV_PROXY_ADDR=:8080 JEV_ADMIN_ADDR=:8081 ./jev-safety-gateway
# PowerShell: .\jev-safety-gateway.exe    (the listen-address defaults are already :8080 / :8081)

# admin console frontend — ONLY needed when web/src changes; web/dist is committed
npm --prefix web install             # first time
npm --prefix web run build           # regenerate web/dist (then rebuild the Go binary)
npm --prefix web run typecheck       # vue-tsc --noEmit

# Docker
docker compose up -d --build
```

Success prints `proxy listening on :8080` and `admin listening on :8081`.

`go build` and `go test` never need Node: `web/dist` is committed and embedded, and the `web` package tests
assert the embedded bundle exists, its assets are present, and no external CDN/font reference sneaked in.
When `web/src` changes, rebuild `web/dist` in the same commit so the binary and the source stay in sync.

## Two listeners, one process

The process runs two independent HTTP servers (`internal/server`):
- **Proxy (`:8080`, public)** — the filtering reverse proxy that all client LLM traffic hits. Sits behind nginx (`nginx/gateway.conf`). Exposes `/healthz`.
- **Admin (`:8081`, private, bind to 127.0.0.1)** — the config API under `/api/*` plus the embedded console. All `/api/*` routes except `login`/`setup`/`setup-status` require a bearer token from `POST /api/login`. The console uses history-mode routing, so non-`api/` paths that miss a real file fall back to `index.html` (`spaFileServer` in `internal/admin/handler.go`); unknown `/api/*` paths still 404.

The gateway does **not** authenticate client apikeys — that remains the upstream's job. It only does content safety filtering.

## Request flow (the core path)

`internal/proxy/proxy.go` `ServeHTTP` → `decide`:
1. If an IP is already abuse-banned, reject with 429 before any JEV call (fast path).
2. Buffer the body so it can be both inspected and forwarded (`readBody`). Bodies up to `maxInspectBody` (8 MiB) are held in memory; a larger one keeps only a prefix for logging and **streams the unread remainder through untouched** (`requestBody.reader()` re-joins prefix + remainder). A body is never truncated — a truncated body is corrupt JSON, so the upstream would reject every large client request without the client ever learning the gateway was at fault. `RejectOversizeBody` (default off) switches this to a fail-closed 413 instead; off keeps large file uploads working.
3. `decide` returns one of four decisions logged to SQLite: **allow / block / skip / error**.
   - Skips non-POST/PUT/PATCH, a disabled gateway, an un-inspectable content type (anything that is neither JSON nor `multipart/form-data`), bodies with no extractable input, and oversize bodies (logged as `skip` / `oversize`) — these forward untouched.
   - Otherwise `extract.Extract` pulls user text by path, `jev.Score` evaluates it, and the score is compared against `SafetyThreshold`.
   - Before calling JEV, `decide` looks up the dedup cache (`internal/scorecache`, keyed on the text plus every evaluation parameter); a hit reuses the stored score and appends `；命中相同内容缓存` to the reason. Only successful evaluations are cached, and a cached block still counts as an abuse strike.
   - The audit row stores the submission text only when `RecordSnippet` is on (default **off**); the `JEV_DEBUG` trace is gated by the same switch, so user text never reaches stdout while recording is off.
4. A genuine harmful block (score present) records an abuse "strike"; enough strikes in the window ban the IP.
5. Safe requests forward via `httputil.ReverseProxy` with `FlushInterval` set so **SSE streaming is preserved**. `ContentLength` is re-set from the client's original framing (`-1` stays chunked), and `X-JEV-Gateway` is added on the **response** only — never on the request forwarded upstream.

**Critical:** enabling `CheckResponse` switches to `forwardChecked`, which buffers the entire upstream response to audit it — this **breaks streaming**. Off by default.

## Package responsibilities

- `internal/config` — SQLite store (`store.go`) + data models (`models.go`). Single-connection (`SetMaxOpenConns(1)`) WAL-mode DB. Settings are JSON-blobbed into a `kv` table and cached in memory; `jev_keys` and `logs` are normal tables. `Settings()` returns a cached copy; `UpdateSettings` validates + persists + refreshes the cache. `LogFilter` matching differs per field on purpose: **model exact** (the value comes from a `DISTINCT` dropdown, and substring would drag in `gpt-4o-mini` when picking `gpt-4o`), **ip prefix**, **path substring**; user input goes through `likeEscape` (paired with `ESCAPE '\'`) so a literal `%`/`_` matches itself.
- `internal/extract` — path-based input extraction. `Extract(path, contentType, body)` is table-driven (`endpoints` in `extract.go`, first match wins, so narrower paths are listed first): it covers the whole OpenAI surface (`/chat/completions`, `/completions`, `/responses`, `/embeddings`, `/moderations`, `/images/*`, `/audio/speech`, `/videos*`, `/realtime/sessions`, `/assistants`, threads, `/rerank`), Anthropic `/messages`, Gemini `:generateContent`, and marks endpoints that provably carry no user text (`files`, `uploads`, `batches`, `fine_tuning`, `vector_stores`, `models`) as `noText` so a miss reads as "nothing to check" rather than "unknown endpoint". Unknown paths fall back to `genericText` (collects every string leaf, skipping `data:` URIs) so nothing goes unchecked; non-JSON, non-form bodies yield nothing. `multipart/form-data` is handled by content type before path dispatch (`extractForm` reads the text fields and skips file parts). `Output(contentType, body)` extracts model-generated text from responses (JSON or SSE) for response auditing — its `outputKeys` include `delta`/`summary_text`, without which a streamed Responses-API reply would audit as empty. `Clamp` is rune-safe truncation to `MaxStateChars`.
  - **Extraction picks the newest client-supplied turn only** (`newestTurn` / `textFromResponses`), never the whole conversation: clients inject a large benign system prompt plus full history on every call, and concatenating it dilutes the safety signal until a harmful newest turn scores as safe. `system` / `instructions` are ignored on purpose; OpenAI role `tool`, Anthropic `tool_result` and Responses `function_call_output` **do** count, since in an agent loop the newest submission is often a tool's output (itself attacker-reachable).
- `internal/jev` — client for `POST {base}/v1/systemone`. Sends a `noul` question; response `noul` is a 0–1 probability. **Round-robins over enabled keys** (atomic cursor) and **retries the next key on 401/429/529** (or network errors); other statuses fail immediately. `KeyProvider` is an interface implemented by a `keyAdapter` in `internal/server` — the jev package does not import config.
- `internal/abuse` — in-memory, process-local sliding-window strike counter + temporary IP bans. Resets on restart (fine for single-instance).
- `internal/admin` — config API + embedded UI. In-memory bearer sessions with sliding 12h expiry (`auth.go`); bcrypt admin password hash stored in the `kv` table. The audit console reads `GET /api/logs` (filters `decision` exact / `model` exact / `path` substring / `ip` prefix / `q` keyword / `since`, plus `limit`+`offset` paging) and `GET /api/logs/models` (the distinct model values with counts, for the model dropdown; scoped by `since` only, never by the other filters, or picking one model would hide every other choice).
- `web` — the admin console frontend: a Vue 3 + Vite + TypeScript + Tailwind project. `web/src` holds the source (router, per-screen modules, components, API wrapper); `web/dist` is the committed build output embedded via `//go:embed` (`embed.go`). `go build` needs no Node — only rebuild `web/dist` (`npm --prefix web run build`) when the source changes. Offline by construction: no CDN, no bundled font files.

## Scoring semantics (easy to get backwards)

The `noul` score means **higher = safer** when the instruction is phrased "is this safe?". `BlockIfBelow=true` blocks when `score < threshold`. If you rephrase `SafetyInstruction` as "is this harmful?", you must flip `BlockIfBelow` to false (blocks when `score >= threshold`). The `X-JEV-Gateway` (allow/blocked/banned) and `X-JEV-Score` response headers reflect the outcome.

`FailOpen` controls behavior when JEV is unreachable: true forwards anyway (error decision), false blocks (fail-closed).

## Configuration model

Settings are runtime-editable from the admin console and persisted in SQLite — they are the source of truth. `load()` decodes the stored blob **on top of `DefaultSettings()`**, so a key missing from the blob (a setting added by a newer build) keeps its default instead of silently reading as the zero value. Environment variables (`JEV_UPSTREAM_URL`, `JEV_BASE_URL`, `JEV_API_KEY`, `JEV_ADMIN_PASSWORD`) only **bootstrap first-run values and never overwrite existing DB values** (see `bootstrap` in `cmd/jev-safety-gateway/main.go`). Infra vars `JEV_DB_PATH` / `JEV_PROXY_ADDR` / `JEV_ADMIN_ADDR` are read every start.

## Notes

- **绝不删除 `data/` 下的任何文件**（`jev-safety-gateway.db` 及其 `-wal` / `-shm`）。这是本地运行态：
  不受版本控制、`rm` 不进回收站、本机没有卷影副本——删掉就是永久丢失，无法找回。清理任务、打包脚本、
  "清空输出目录" 一类操作一律不得把它作为目标；需要重置配置时必须先取得用户明确同意，并先备份副本。
  注意 `jev-safety-gateway.db-wal` 可能远大于主库（实测主库 40 KB / WAL 3.4 MB），数据主要落在 WAL 里，
  单独留下主库没有意义——三者必须作为一个整体保护。本地 test 启动脚本（`scripts/start-local-test.ps1`）
  默认就把库写在这个仓库根 `data/` 下，`-DbPath` 换了位置也一样受此规则保护。
  （测试包内 `out/<pkg>/data/` 是一次性构建产物，不受此限。）
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
