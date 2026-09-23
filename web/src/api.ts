/**
 * 管理接口客户端。只调用 internal/admin 已存在的路由：
 *   POST /api/login  POST /api/setup  GET /api/setup-status  POST /api/logout
 *   GET  /api/state  GET/PUT /api/settings
 *   GET/POST /api/keys  POST /api/keys/{id}/enable|disable  DELETE /api/keys/{id}
 *   GET  /api/logs   GET /api/stats?hours=
 * 本次重构不新增任何接口。
 */
import type {
  JEVKey,
  LogEntry,
  LogsResponse,
  Settings,
  SetupStatus,
  StatBucket,
  StateResponse,
  Stats,
  StatsResponse,
} from './types'

const TOKEN_KEY = 'jev_admin_token'

let token = localStorage.getItem(TOKEN_KEY) || ''

export function hasToken(): boolean {
  return token !== ''
}

/** 鉴权相关路径不触发自动登出：这些路径上的 401 表示「口令错误 / 未登录」。 */
const AUTH_PATHS = ['/api/login', '/api/setup', '/api/setup-status', '/api/logout']

/** 会话失效时由 App 注册的回调，避免 api 层直接依赖视图层。 */
let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn
}

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers = new Headers(opts.headers)
  headers.set('Content-Type', 'application/json')
  if (token) headers.set('Authorization', 'Bearer ' + token)

  const res = await fetch(path, { ...opts, headers })
  const text = await res.text()
  let data: unknown = {}
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = {}
    }
  }
  const message = (data as { error?: string }).error

  if (res.status === 401 && !AUTH_PATHS.includes(path)) {
    clearToken()
    onUnauthorized?.()
    throw new Error(message || '登录状态已失效，请重新登录')
  }
  if (!res.ok) throw new Error(message || 'HTTP ' + res.status)
  return data as T
}

export function clearToken(): void {
  token = ''
  localStorage.removeItem(TOKEN_KEY)
}

function setToken(t: string): void {
  token = t
  localStorage.setItem(TOKEN_KEY, t)
}

// --- 认证 ---

export function setupStatus(): Promise<SetupStatus> {
  return request<SetupStatus>('/api/setup-status')
}

export async function setup(password: string): Promise<void> {
  const r = await request<{ token: string }>('/api/setup', {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
  setToken(r.token)
}

export async function login(password: string): Promise<void> {
  const r = await request<{ token: string }>('/api/login', {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
  setToken(r.token)
}

/** 登出：先撤销服务端会话，再清本地 token；失败不阻塞登出。 */
export async function logout(): Promise<void> {
  const t = token
  clearToken()
  if (!t) return
  try {
    await fetch('/api/logout', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + t },
    })
  } catch {
    /* 网络失败不阻塞登出 */
  }
}

// --- 状态 / 配置 / 密钥 ---

/** `GET /api/state`：后端在没有密钥时返回 `keys: null`，这里归一化为空数组。 */
export async function getState(): Promise<StateResponse> {
  const r = await request<StateResponse>('/api/state')
  return { ...r, keys: r.keys ?? [] }
}

export function getSettings(): Promise<Settings> {
  return request<Settings>('/api/settings')
}

export function saveSettings(s: Settings): Promise<Settings> {
  return request<Settings>('/api/settings', { method: 'PUT', body: JSON.stringify(s) })
}

/** `GET /api/keys`：后端在密钥池为空时返回 `null`，这里归一化为空数组。 */
export async function listKeys(): Promise<JEVKey[]> {
  return (await request<JEVKey[] | null>('/api/keys')) ?? []
}

export function addKey(label: string, key: string): Promise<{ id: number }> {
  return request<{ id: number }>('/api/keys', { method: 'POST', body: JSON.stringify({ label, key }) })
}

export function setKeyEnabled(id: number, enabled: boolean): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>(`/api/keys/${id}/${enabled ? 'enable' : 'disable'}`, { method: 'POST' })
}

export function deleteKey(id: number): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>(`/api/keys/${id}`, { method: 'DELETE' })
}

// --- 统计 / 日志 ---

/**
 * `GET /api/stats?hours=`：既有区间总量字段，加上本次新增的 `bucket_seconds` /
 * `series` / `score_buckets` / `unscored`（趋势图与风险分值分布的数据来源）。
 * 分桶口径完全由后端决定，前端只渲染。
 */
export function getStats(hours: number): Promise<StatsResponse> {
  return request<StatsResponse>(`/api/stats?hours=${encodeURIComponent(String(hours))}`)
}

/** `/api/logs` 的筛选条件，语义与 internal/config.LogFilter 一致（条件之间为「与」）。 */
export interface LogFilter {
  limit: number
  offset: number
  decision?: string
  model?: string
  q?: string
  since?: number
}

export function queryLogs(f: LogFilter): Promise<LogsResponse> {
  const p = new URLSearchParams()
  p.set('limit', String(f.limit))
  p.set('offset', String(f.offset))
  if (f.decision) p.set('decision', f.decision)
  if (f.model) p.set('model', f.model)
  if (f.q) p.set('q', f.q)
  if (f.since && f.since > 0) p.set('since', String(f.since))
  return request<LogsResponse>('/api/logs?' + p.toString())
}

export type { JEVKey, LogEntry, LogsResponse, Settings, StatBucket, Stats, StatsResponse, StateResponse }
