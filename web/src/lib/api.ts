import { type AnalysisResponse, type AuthFilesManagementResponse, type AuthManagedSessionsResponse, type AuthSessionResponse, type CpaApiKeyDisplayItem, type CpaApiKeyOptionsResponse, type CpaApiKeySettingsResponse, type CpaApiKeysResponse, type OverviewRealtimeBlock, type OverviewRealtimeWindow, type PricingEntry, type PricingResponse, type PricingSyncPreviewResponse, type QuotaAutoRefreshSettings, type StatusResponse, type UpdateCheckResponse, type UsageActivityRequest, type UsageActivityResponse, type UsageEventModelFilterOptionsResponse, type UsageEventRequestLogResponse, type UsageEventSourceFilterOptionsResponse, type UsageRangeRequest, type UsedModelsResponse, type UsageIdentitiesPageResponse, type UsageIdentitiesResponse, type UsageEventsResponse, type UsageIdentity, type UsageIdentityAuthType, type UsageOverviewResponse, type UsageQuotaCacheResponse, type UsageQuotaInspectionStatusResponse, type UsageQuotaRefreshResponse, type UsageQuotaRefreshTaskResponse, type UsageQuotaResetCreditsResponse, type UsageQuotaResetResponse, type VersionResponse } from './types'
import { isCPAMCEmbed } from '@/embed/cpamcEmbed'
import { resolveUsageRequestRange } from '@/utils/usage/rangeQuery'

export class ApiError extends Error {
  status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

const APP_BASE_PATH_PLACEHOLDER = '__APP_BASE_PATH__'
const EMBED_SESSION_STORAGE_KEY = 'cpa_usage_keeper_embed_session'
const EMBED_SESSION_HEADER = 'X-CPA-Usage-Keeper-Embed-Session'

declare global {
  interface Window {
    __APP_BASE_PATH__?: string
  }
}

function normalizeBasePath(basePath: string | undefined): string {
  if (!basePath || basePath === '/' || basePath === APP_BASE_PATH_PLACEHOLDER) {
    return ''
  }
  return basePath.endsWith('/') ? basePath.slice(0, -1) : basePath
}

function realtimeBucketSecondsForWindow(window: OverviewRealtimeWindow): number {
  if (window === '60m') return 120
  if (window === '30m') return 60
  return 30
}

function realtimeResponseParticleTotal(particles: OverviewRealtimeBlock['response_distribution']['ttft']['particles']): number {
  return particles.reduce((total, particle) => total + Math.max(1, Number(particle.count) || 0), 0)
}

function normalizeOverviewRealtimeBlock(
  block: Partial<OverviewRealtimeBlock> & {
    current_usage?: Partial<OverviewRealtimeBlock['current_usage']>;
    response_distribution?: Partial<OverviewRealtimeBlock['response_distribution']>;
  },
  fallbackWindow?: OverviewRealtimeWindow,
): OverviewRealtimeBlock {
  const currentUsage: Partial<OverviewRealtimeBlock['current_usage']> = block.current_usage ?? {}
  const responseDistribution: Partial<OverviewRealtimeBlock['response_distribution']> = block.response_distribution ?? {}
  const ttftParticles = responseDistribution.ttft?.particles ?? []
  const latencyParticles = responseDistribution.latency?.particles ?? []
  const resolvedWindow = block.window ?? fallbackWindow ?? '15m'
  return {
    window: resolvedWindow,
    timezone: block.timezone,
    bucket_seconds: block.bucket_seconds ?? realtimeBucketSecondsForWindow(resolvedWindow),
    window_start: block.window_start,
    window_end: block.window_end,
    token_velocity: block.token_velocity ?? [],
    response_level: block.response_level ?? [],
    response_distribution: {
      ttft: {
        average_line: responseDistribution.ttft?.average_line ?? [],
        particles: ttftParticles,
        total_particles: responseDistribution.ttft?.total_particles ?? realtimeResponseParticleTotal(ttftParticles),
        sampled: responseDistribution.ttft?.sampled ?? false,
        max_particles: responseDistribution.ttft?.max_particles ?? 1000,
      },
      latency: {
        average_line: responseDistribution.latency?.average_line ?? [],
        particles: latencyParticles,
        total_particles: responseDistribution.latency?.total_particles ?? realtimeResponseParticleTotal(latencyParticles),
        sampled: responseDistribution.latency?.sampled ?? false,
        max_particles: responseDistribution.latency?.max_particles ?? 1000,
      },
    },
    current_usage: {
      models: currentUsage.models ?? [],
      api_keys: currentUsage.api_keys ?? [],
      auth_files: currentUsage.auth_files ?? [],
      ai_providers: currentUsage.ai_providers ?? [],
    },
    request_level: block.request_level ?? [],
    cache_level: block.cache_level ?? [],
  }
}

export interface FetchKeyOverviewRealtimeOptions {
  window?: OverviewRealtimeWindow
  signal?: AbortSignal
}

export interface FetchUsageOverviewRealtimeOptions extends FetchKeyOverviewRealtimeOptions {
  apiKeyId?: string
}

interface EmbedLoginResponse {
  session_token?: string
}

export function appPath(path: string): string {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  return `${normalizeBasePath(window.__APP_BASE_PATH__)}${normalizedPath}`
}

export function apiPath(path: string): string {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  return `${normalizeBasePath(window.__APP_BASE_PATH__)}/api/v1${normalizedPath}`
}

async function parseApiError(response: Response, fallback: string): Promise<never> {
  let message = fallback
  try {
    const payload = await response.json() as { error?: string }
    if (payload.error) {
      message = payload.error
    }
  } catch {
    // ignore invalid error payloads
  }
  throw new ApiError(message, response.status)
}

function isMutatingMethod(method: string | undefined): boolean {
  const normalized = (method ?? 'GET').toUpperCase()
  return normalized !== 'GET' && normalized !== 'HEAD'
}

function embedSessionStorage(): Storage | null {
  if (typeof window === 'undefined') return null
  try {
    return window.sessionStorage ?? null
  } catch {
    return null
  }
}

function readEmbedSessionToken(): string {
  if (!isCPAMCEmbed()) return ''
  const storage = embedSessionStorage()
  if (!storage) return ''
  try {
    return storage.getItem(EMBED_SESSION_STORAGE_KEY)?.trim() ?? ''
  } catch {
    return ''
  }
}

function storeEmbedSessionToken(token: string): void {
  const trimmed = token.trim()
  if (!trimmed) return
  const storage = embedSessionStorage()
  if (!storage) return
  try {
    storage.setItem(EMBED_SESSION_STORAGE_KEY, trimmed)
  } catch {
    // 浏览器可能在隐私/嵌入场景禁用 sessionStorage；此时保持 cookie-first 行为即可。
  }
}

export function clearEmbedSessionToken(): void {
  const storage = embedSessionStorage()
  if (!storage) return
  try {
    storage.removeItem(EMBED_SESSION_STORAGE_KEY)
  } catch {
    // 清理 fallback token 是 best-effort，不能阻断登录/登出流程。
  }
}

async function apiFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const headers = new Headers(init?.headers)
  if (isMutatingMethod(init?.method)) {
    headers.set('X-CPA-Usage-Keeper-Request', 'fetch')
  }
  if (isCPAMCEmbed()) {
    headers.set('X-CPA-Usage-Keeper-Embed', 'cpamc')
    const embedSessionToken = readEmbedSessionToken()
    if (embedSessionToken) {
      headers.set(EMBED_SESSION_HEADER, embedSessionToken)
    }
  }
  const response = await fetch(input, {
    ...init,
    credentials: 'include',
    headers,
  })
  if (response.status === 401) {
    clearEmbedSessionToken()
  }
  return response
}

export async function getSession(signal?: AbortSignal): Promise<AuthSessionResponse> {
  const response = await apiFetch(apiPath('/auth/session'), { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load auth session: ${response.status}`)
  }
  const session = await response.json()
  if (isCPAMCEmbed() && !session.authenticated) {
    clearEmbedSessionToken()
  }
  return session
}

async function readEmbedLoginResponse(response: Response): Promise<EmbedLoginResponse> {
  if (!isCPAMCEmbed()) return {}
  try {
    return await response.json() as EmbedLoginResponse
  } catch {
    return {}
  }
}

async function activateEmbedSessionFallback(response: Response): Promise<void> {
  const payload = await readEmbedLoginResponse(response)
  if (!payload.session_token) return
  const session = await getSession()
  if (!session.authenticated) {
    storeEmbedSessionToken(payload.session_token)
  }
}

export async function login(password: string): Promise<void> {
  if (isCPAMCEmbed()) {
    clearEmbedSessionToken()
  }
  const response = await apiFetch(apiPath('/auth/login'), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ password }),
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to login: ${response.status}`)
  }
  await activateEmbedSessionFallback(response)
}

export async function loginWithCPAAPIKey(apiKey: string): Promise<void> {
  if (isCPAMCEmbed()) {
    clearEmbedSessionToken()
  }
  const response = await apiFetch(apiPath('/auth/api-key-login'), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ apiKey }),
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to login with CPA API key: ${response.status}`)
  }
  await activateEmbedSessionFallback(response)
}

export async function logout(): Promise<void> {
  try {
    const response = await apiFetch(apiPath('/auth/logout'), {
      method: 'POST',
    })
    if (!response.ok) {
      await parseApiError(response, `Failed to logout: ${response.status}`)
    }
  } finally {
    clearEmbedSessionToken()
  }
}

export async function fetchAuthSessions(signal?: AbortSignal): Promise<AuthManagedSessionsResponse> {
  const response = await apiFetch(apiPath('/auth/sessions'), { signal, cache: 'no-store' })
  if (!response.ok) {
    await parseApiError(response, `Failed to load auth sessions: ${response.status}`)
  }
  return response.json()
}

export async function revokeAuthSession(id: string): Promise<void> {
  const response = await apiFetch(apiPath(`/auth/sessions/${encodeURIComponent(id)}`), {
    method: 'DELETE',
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to revoke auth session: ${response.status}`)
  }
}

const buildUsageRangeParams = (request: UsageRangeRequest): URLSearchParams => {
  const params = new URLSearchParams()
  params.set('range', resolveUsageRequestRange(request.range))
  if (request.unit) {
    params.set('unit', request.unit)
  }
  if (request.start) {
    params.set('start', request.start)
  }
  if (request.end) {
    params.set('end', request.end)
  }
  return params
}

export async function fetchKeyOverview(request: UsageRangeRequest, signal?: AbortSignal): Promise<UsageOverviewResponse> {
  const params = buildUsageRangeParams(request)
  const response = await apiFetch(`${apiPath('/key-overview')}?${params.toString()}`, { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load key overview: ${response.status}`)
  }
  return response.json()
}

export interface FetchUsageActivityOptions {
  request: UsageActivityRequest
  apiKeyId?: string
  signal?: AbortSignal
}

const buildUsageActivityParams = (request: UsageActivityRequest): URLSearchParams => {
  // Activity 专属窗口绕过公共 Usage range；其余请求继续复用 Overview 的时间参数。
  if ('window' in request) {
    const params = new URLSearchParams()
    params.set('window', request.window)
    return params
  }
  return buildUsageRangeParams(request)
}

export async function fetchKeyActivity({ request, signal }: FetchUsageActivityOptions): Promise<UsageActivityResponse> {
  const params = buildUsageActivityParams(request)
  const response = await apiFetch(`${apiPath('/key-activity')}?${params.toString()}`, { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load key activity: ${response.status}`)
  }
  return response.json()
}

export async function fetchKeyOverviewRealtime(options: FetchKeyOverviewRealtimeOptions = {}): Promise<OverviewRealtimeBlock> {
  const { window, signal } = options
  const params = new URLSearchParams()
  if (window) {
    params.set('window', window)
  }
  const query = params.toString()
  const response = await apiFetch(`${apiPath('/key-overview/realtime')}${query ? `?${query}` : ''}`, { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load key overview realtime: ${response.status}`)
  }
  const payload = await response.json() as Partial<OverviewRealtimeBlock> & {
    current_usage?: Partial<OverviewRealtimeBlock['current_usage']>;
  }
  return normalizeOverviewRealtimeBlock(payload, window)
}

export async function fetchUsageOverview(request: UsageRangeRequest, signal?: AbortSignal, apiKeyId?: string): Promise<UsageOverviewResponse> {
  const params = buildUsageRangeParams(request)
  const selectedAPIKeyId = apiKeyId?.trim()
  if (selectedAPIKeyId) {
    params.set('api_key_id', selectedAPIKeyId)
  }
  const query = params.toString()
  const response = await apiFetch(`${apiPath('/usage/overview')}${query ? `?${query}` : ''}`, { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load usage overview: ${response.status}`)
  }
  return response.json()
}

export async function fetchUsageActivity({ request, apiKeyId, signal }: FetchUsageActivityOptions): Promise<UsageActivityResponse> {
  const params = buildUsageActivityParams(request)
  const selectedAPIKeyId = apiKeyId?.trim()
  if (selectedAPIKeyId) {
    params.set('api_key_id', selectedAPIKeyId)
  }
  const response = await apiFetch(`${apiPath('/usage/activity')}?${params.toString()}`, { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load usage activity: ${response.status}`)
  }
  return response.json()
}

export async function fetchUsageOverviewRealtime(options: FetchUsageOverviewRealtimeOptions = {}): Promise<OverviewRealtimeBlock> {
  const { signal, apiKeyId, window } = options
  const params = new URLSearchParams()
  const selectedAPIKeyId = apiKeyId?.trim()
  if (selectedAPIKeyId) {
    params.set('api_key_id', selectedAPIKeyId)
  }
  if (window) {
    params.set('window', window)
  }
  const query = params.toString()
  const response = await apiFetch(`${apiPath('/usage/overview/realtime')}${query ? `?${query}` : ''}`, { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load usage overview realtime: ${response.status}`)
  }
  const payload = await response.json() as Partial<OverviewRealtimeBlock> & {
    current_usage?: Partial<OverviewRealtimeBlock['current_usage']>;
  }
  return normalizeOverviewRealtimeBlock(payload, window)
}

export interface FetchUsageEventsOptions {
  page?: number
  pageSize?: number
  model?: string
  // Request Events 页面沿用 Source 命名；这里传的是 usage identity，后端会转换为 auth_index 查询。
  source?: string
  result?: string
  apiKeyId?: string
}

export type UsageEventsExportFormat = 'csv' | 'json'

export interface UsageEventsExportFile {
  blob: Blob
  filename: string
}

interface UsageEventRequestLogDownloadURLResponse {
  download_url?: string
}

function buildUsageEventsParams(request: UsageRangeRequest, options?: FetchUsageEventsOptions, includePagination = true): URLSearchParams {
  const params = buildUsageRangeParams(request)
  if (includePagination && typeof options?.page === 'number' && Number.isFinite(options.page) && options.page > 0) {
    params.set('page', String(Math.floor(options.page)))
  }
  if (includePagination && typeof options?.pageSize === 'number' && Number.isFinite(options.pageSize) && options.pageSize > 0) {
    params.set('page_size', String(Math.floor(options.pageSize)))
  }
  const model = options?.model?.trim()
  if (model) {
    params.set('model', model)
  }
  const source = options?.source?.trim()
  if (source) {
    // Source 下拉的 value 不是 usage_events.source 原始字段，而是后端用于 auth_index 查询的 identity。
    params.set('source', source)
  }
  const result = options?.result?.trim()
  if (result) {
    params.set('result', result)
  }
  const selectedAPIKeyId = options?.apiKeyId?.trim()
  if (selectedAPIKeyId) {
    params.set('api_key_id', selectedAPIKeyId)
  }
  return params
}

function parseAttachmentFilename(contentDisposition: string | null, fallback: string): string {
  const match = contentDisposition?.match(/filename="([^"]+)"/i)
  return match?.[1]?.trim() || fallback
}

export async function fetchUsageEventModelFilterOptions(signal?: AbortSignal): Promise<UsageEventModelFilterOptionsResponse> {
  const response = await apiFetch(apiPath('/usage/events/filters/models'), { signal, cache: 'no-store' })
  if (!response.ok) {
    await parseApiError(response, `Failed to load usage event model filters: ${response.status}`)
  }
  return response.json()
}

export async function fetchUsageEventSourceFilterOptions(signal?: AbortSignal): Promise<UsageEventSourceFilterOptionsResponse> {
  const response = await apiFetch(apiPath('/usage/events/filters/sources'), { signal, cache: 'no-store' })
  if (!response.ok) {
    await parseApiError(response, `Failed to load usage event source filters: ${response.status}`)
  }
  return response.json()
}

export async function fetchUsageEvents(request: UsageRangeRequest, signal?: AbortSignal, options?: FetchUsageEventsOptions): Promise<UsageEventsResponse> {
  const params = buildUsageEventsParams(request, options)
  const query = params.toString()
  const response = await apiFetch(`${apiPath('/usage/events')}${query ? `?${query}` : ''}`, { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load usage events: ${response.status}`)
  }
  return response.json()
}

export async function fetchUsageEventRequestLog(eventId: string, signal?: AbortSignal): Promise<UsageEventRequestLogResponse> {
  const response = await apiFetch(apiPath(`/usage/events/${encodeURIComponent(eventId)}/request-log`), { signal, cache: 'no-store' })
  if (!response.ok) {
    await parseApiError(response, `Failed to load usage event request log: ${response.status}`)
  }
  return response.json()
}

export async function createUsageEventRequestLogDownloadURL(eventId: string): Promise<string> {
  const response = await apiFetch(apiPath(`/usage/events/${encodeURIComponent(eventId)}/request-log/download-token`), { method: 'POST', cache: 'no-store' })
  if (!response.ok) {
    await parseApiError(response, `Failed to create usage event request log download URL: ${response.status}`)
  }
  const payload = await response.json() as UsageEventRequestLogDownloadURLResponse
  const downloadURL = payload.download_url?.trim()
  if (!downloadURL) {
    throw new ApiError('request log download URL is missing', response.status)
  }
  return downloadURL
}

export async function exportUsageEvents(request: UsageRangeRequest, format: UsageEventsExportFormat, options?: FetchUsageEventsOptions): Promise<UsageEventsExportFile> {
  const params = buildUsageEventsParams(request, options, false)
  params.set('format', format)
  const query = params.toString()
  const response = await apiFetch(`${apiPath('/usage/events/export')}${query ? `?${query}` : ''}`)
  if (!response.ok) {
    await parseApiError(response, `Failed to export usage events: ${response.status}`)
  }
  return {
    blob: await response.blob(),
    filename: parseAttachmentFilename(response.headers.get('Content-Disposition'), `usage-events.${format}`),
  }
}

export type UsageIdentityPageSort = 'priority' | 'total_requests' | 'total_tokens' | 'last_used_at'

export interface FetchUsageIdentitiesPageOptions {
  authType?: UsageIdentityAuthType
  activeOnly?: boolean
  types?: string[]
  sort?: UsageIdentityPageSort
  page?: number
  pageSize?: number
}

export async function fetchUsageIdentities(signal?: AbortSignal): Promise<UsageIdentitiesResponse> {
  const response = await apiFetch(apiPath('/usage/identities'), { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load usage identities: ${response.status}`)
  }
  return response.json()
}

export async function fetchUsageIdentitiesPage(signal?: AbortSignal, options?: FetchUsageIdentitiesPageOptions): Promise<UsageIdentitiesPageResponse> {
  // Credentials 两个分区共用分页接口，通过 auth_type 控制服务端过滤。
  const params = new URLSearchParams()
  if (options?.authType) {
    params.set('auth_type', String(options.authType))
  }
  if (typeof options?.activeOnly === 'boolean') {
    params.set('active_only', String(options.activeOnly))
  }
  if (options?.sort) {
    params.set('sort', options.sort)
  }
  for (const type of options?.types ?? []) {
    if (type !== '') {
      params.append('type', type)
    }
  }
  if (typeof options?.page === 'number' && Number.isFinite(options.page) && options.page > 0) {
    params.set('page', String(Math.floor(options.page)))
  }
  if (typeof options?.pageSize === 'number' && Number.isFinite(options.pageSize) && options.pageSize > 0) {
    params.set('page_size', String(Math.floor(options.pageSize)))
  }
  const query = params.toString()
  const response = await apiFetch(`${apiPath('/usage/identities/page')}${query ? `?${query}` : ''}`, { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load usage identities page: ${response.status}`)
  }
  return response.json()
}

export async function updateUsageIdentityAlias(id: string, alias: string | null): Promise<UsageIdentity> {
  const response = await apiFetch(apiPath(`/usage/identities/${encodeURIComponent(id)}`), {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ alias }),
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to update usage identity alias: ${response.status}`)
  }
  return response.json()
}

export async function fetchUsageQuotaCache(authIndexes: string[], signal?: AbortSignal): Promise<UsageQuotaCacheResponse> {
  // cache 只读后端已有结果，不携带刷新 limit，避免把缓存读取误当队列提交。
  const response = await apiFetch(apiPath('/quota/cache'), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ auth_indexes: authIndexes }),
    signal,
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to load cached usage quotas: ${response.status}`)
  }
  return response.json()
}

export async function refreshUsageQuotas(authIndexes: string[], signal?: AbortSignal): Promise<UsageQuotaRefreshResponse> {
  // refresh 会创建后台任务，前端提交当前页所有 auth_index。
  const response = await apiFetch(apiPath('/quota/refresh'), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ auth_indexes: authIndexes }),
    signal,
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to refresh usage quotas: ${response.status}`)
  }
  return response.json()
}

export async function fetchUsageQuotaInspectionStatus(signal?: AbortSignal): Promise<UsageQuotaInspectionStatusResponse> {
  const response = await apiFetch(apiPath('/quota/inspection'), { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load quota inspection status: ${response.status}`)
  }
  return response.json()
}

export async function startUsageQuotaInspection(signal?: AbortSignal): Promise<UsageQuotaInspectionStatusResponse> {
  const response = await apiFetch(apiPath('/quota/inspection'), {
    method: 'POST',
    signal,
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to start quota inspection: ${response.status}`)
  }
  return response.json()
}


export async function resetUsageQuota(authIndex: string, signal?: AbortSignal): Promise<UsageQuotaResetResponse> {
  const response = await apiFetch(apiPath('/quota/reset'), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ auth_index: authIndex }),
    signal,
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to reset usage quota: ${response.status}`)
  }
  return response.json()
}

export async function fetchUsageQuotaResetCredits(authIndex: string, signal?: AbortSignal): Promise<UsageQuotaResetCreditsResponse> {
  const response = await apiFetch(apiPath(`/quota/reset-credits/${encodeURIComponent(authIndex)}`), { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load quota reset credits: ${response.status}`)
  }
  return response.json()
}

export async function fetchUsageQuotaRefreshTask(authIndex: string, signal?: AbortSignal): Promise<UsageQuotaRefreshTaskResponse> {
  const response = await apiFetch(apiPath(`/quota/refresh/${encodeURIComponent(authIndex)}`), { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load usage quota refresh task: ${response.status}`)
  }
  return response.json()
}

export async function setAuthFilesDisabled(names: string[], disabled: boolean): Promise<AuthFilesManagementResponse> {
  const response = await apiFetch(apiPath('/auth-files/status'), {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ names, disabled }),
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to update auth file status: ${response.status}`)
  }
  return response.json()
}

export async function deleteAuthFiles(names: string[]): Promise<AuthFilesManagementResponse> {
  const response = await apiFetch(apiPath('/auth-files'), {
    method: 'DELETE',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ names }),
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to delete auth files: ${response.status}`)
  }
  return response.json()
}

export async function fetchAnalysis(request: UsageRangeRequest, signal?: AbortSignal, apiKeyId?: string): Promise<AnalysisResponse> {
  const params = buildUsageRangeParams(request)
  const selectedAPIKeyId = apiKeyId?.trim()
  if (selectedAPIKeyId) {
    params.set('api_key_id', selectedAPIKeyId)
  }
  const query = params.toString()
  const response = await apiFetch(`${apiPath('/usage/analysis')}${query ? `?${query}` : ''}`, { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load analysis: ${response.status}`)
  }
  return response.json()
}


export async function fetchCpaApiKeyOptions(signal?: AbortSignal): Promise<CpaApiKeyOptionsResponse> {
  const response = await apiFetch(apiPath('/usage/api-keys/options'), { signal, cache: 'no-store' })
  if (!response.ok) {
    await parseApiError(response, `Failed to load CPA API key options: ${response.status}`)
  }
  return response.json()
}

export async function fetchCpaApiKeys(signal?: AbortSignal): Promise<CpaApiKeysResponse> {
  const response = await apiFetch(apiPath('/usage/api-keys'), { signal, cache: 'no-store' })
  if (!response.ok) {
    await parseApiError(response, `Failed to load CPA API keys: ${response.status}`)
  }
  return response.json()
}

export async function fetchCpaApiKeySettings(signal?: AbortSignal): Promise<CpaApiKeySettingsResponse> {
  const response = await apiFetch(apiPath('/usage/api-keys/settings'), { signal, cache: 'no-store' })
  if (!response.ok) {
    await parseApiError(response, `Failed to load CPA API key settings: ${response.status}`)
  }
  return response.json()
}

export async function updateCpaApiKeyAlias(id: string, keyAlias: string): Promise<CpaApiKeyDisplayItem> {
  const response = await apiFetch(apiPath(`/usage/api-keys/${encodeURIComponent(id)}`), {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ keyAlias }),
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to update CPA API key alias: ${response.status}`)
  }
  return response.json()
}

export async function fetchUsedModels(signal?: AbortSignal): Promise<UsedModelsResponse> {
  const response = await apiFetch(apiPath('/models/used'), { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load used models: ${response.status}`)
  }
  return response.json()
}

export async function fetchStatus(signal?: AbortSignal): Promise<StatusResponse> {
  const response = await apiFetch(apiPath('/status'), { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load status: ${response.status}`)
  }
  return response.json()
}

export async function fetchVersion(signal?: AbortSignal): Promise<VersionResponse> {
  const response = await apiFetch(apiPath('/version'), { signal, cache: 'no-store' })
  if (!response.ok) {
    await parseApiError(response, `Failed to load version: ${response.status}`)
  }
  return response.json()
}

export async function fetchQuotaAutoRefreshSettings(signal?: AbortSignal): Promise<QuotaAutoRefreshSettings> {
  const response = await apiFetch(apiPath('/quota/auto-refresh/settings'), { signal, cache: 'no-store' })
  if (!response.ok) {
    await parseApiError(response, `Failed to load quota auto refresh settings: ${response.status}`)
  }
  return response.json()
}

export async function updateQuotaAutoRefreshSettings(settings: QuotaAutoRefreshSettings): Promise<QuotaAutoRefreshSettings> {
  const response = await apiFetch(apiPath('/quota/auto-refresh/settings'), {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(settings),
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to update quota auto refresh settings: ${response.status}`)
  }
  return response.json()
}

export async function fetchUpdateCheck(signal?: AbortSignal): Promise<UpdateCheckResponse> {
  const response = await apiFetch(apiPath('/update/check'), { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to check for updates: ${response.status}`)
  }
  return response.json()
}

export async function fetchPricing(signal?: AbortSignal): Promise<PricingResponse> {
  const response = await apiFetch(apiPath('/pricing'), { signal })
  if (!response.ok) {
    await parseApiError(response, `Failed to load pricing: ${response.status}`)
  }
  return response.json()
}

export async function fetchPricingSyncPreview(signal?: AbortSignal): Promise<PricingSyncPreviewResponse> {
  const response = await apiFetch(apiPath('/pricing/sync/preview'), { signal, cache: 'no-store' })
  if (!response.ok) {
    await parseApiError(response, `Failed to preview pricing sync: ${response.status}`)
  }
  return response.json()
}

export async function updatePricing(model: string, pricing: Omit<PricingEntry, 'model'>): Promise<PricingEntry> {
  const response = await apiFetch(apiPath('/pricing'), {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ model, ...pricing }),
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to update pricing: ${response.status}`)
  }
  return response.json()
}

export async function deletePricing(model: string): Promise<void> {
  const params = new URLSearchParams({ model })
  const response = await apiFetch(`${apiPath('/pricing')}?${params.toString()}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    await parseApiError(response, `Failed to delete pricing: ${response.status}`)
  }
}
