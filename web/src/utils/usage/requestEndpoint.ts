const endpointAliases = new Map<string, string>([
  ['/backend-api/codex/responses', '/v1/responses'],
  ['/backend-api/codex/responses/compact', '/v1/responses/compact'],
  ['/backend-api/codex/alpha/search', '/v1/alpha/search'],
  ['/v1/tts', '/v1/audio/speech'],
  ['/v1/videos', '/v1/videos/generations'],
])

export const parseRequestEndpoint = (rawEndpoint: unknown): { requestType: string; endpoint: string } => {
  const raw = String(rawEndpoint ?? '').trim().replace(/\s+/g, ' ')
  if (!raw) return { requestType: '-', endpoint: '-' }
  const [first, ...rest] = raw.split(' ')
  const upperMethod = first.toUpperCase()
  const hasMethod = upperMethod === 'GET' || upperMethod === 'POST'
  const requestType = upperMethod === 'POST' ? 'SSE' : upperMethod === 'GET' ? 'WS' : '-'
  const path = hasMethod ? rest.join(' ').trim() : raw
  // 仅归一化已确认的入口别名，其余路径保留版本前缀和原值。
  return { requestType, endpoint: endpointAliases.get(path) ?? (path || '-') }
}
