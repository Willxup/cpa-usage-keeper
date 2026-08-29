import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { UsageEvent } from '@/lib/types'
import { RequestEventsDetailsCard } from '../RequestEventsDetailsCard'

const event: UsageEvent = {
  id: 'compact-columns',
  timestamp: '2026-08-23T09:10:11+08:00',
  api_key: 'Production Key',
  model: 'gpt-5.6',
  model_alias: 'keeper-gpt',
  reasoning_effort: 'high',
  service_tier: 'priority',
  response_service_tier: 'flex',
  endpoint: 'POST /v1/messages',
  executor_type: 'OpenAIResponsesExecutor',
  source: 'OpenAI Team',
  source_raw: 'openai-team',
  source_type: 'openai',
  auth_index: '1',
  isDelete: true,
  failed: false,
  latency_ms: 120,
  ttft_ms: 45,
  speed_tps: 30,
  client_ip: '192.0.2.10',
  x_forwarded_for: '203.0.113.5, 198.51.100.8',
  user_agent: 'keeper-client/1.0',
  tokens: {
    input_tokens: 100,
    output_tokens: 60,
    reasoning_tokens: 20,
    cache_read_tokens: 20,
    cache_creation_tokens: 5,
    total_tokens: 200,
  },
  cost_usd: 0.1234,
  cost_available: true,
  pricing_style: 'claude',
}

const textFromMarkup = (value: string) => value.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim()

const renderCard = () => renderToStaticMarkup(
  <RequestEventsDetailsCard
    events={[event]}
    loading={false}
    totalCount={1}
    modelOptions={['gpt-5.6']}
    sourceOptions={[{ value: 'openai-team', label: 'OpenAI Team' }]}
    modelFilter="__all__"
    sourceFilter="__all__"
    resultFilter="__all__"
    onModelFilterChange={() => undefined}
    onSourceFilterChange={() => undefined}
    onResultFilterChange={() => undefined}
  />,
)

const extractTableHeaders = (html: string) => (
  Array.from(html.matchAll(/<th\b[^>]*>(.*?)<\/th>/gs), (match) => textFromMarkup(match[1]))
)

const extractFirstTableRowCells = (html: string) => {
  const row = html.match(/<tbody><tr>(.*?)<\/tr><\/tbody>/s)?.[1] ?? ''
  return Array.from(row.matchAll(/<td\b[^>]*>(.*?)<\/td>/gs), (match) => textFromMarkup(match[1]))
}

const extractFirstTableRowCellMarkup = (html: string) => {
  const row = html.match(/<tbody><tr>(.*?)<\/tr><\/tbody>/s)?.[1] ?? ''
  return Array.from(row.matchAll(/(<td\b[^>]*>.*?<\/td>)/gs), (match) => match[1])
}

describe('RequestEventsDetailsCard compact columns', () => {
  it('renders the agreed 17 display columns in order', () => {
    const html = renderCard()

    expect(extractTableHeaders(html)).toEqual([
      'Timestamp',
      'API Key',
      'Source',
      'Model',
      'Effort',
      'Speed Mode',
      'Result',
      'Request',
      'Latency',
      'Speed',
      'Tokens',
      'Cache',
      'Cost',
      'Executor',
      'Client IP',
      'X-Forwarded-For',
      'User Agent',
    ])
  })

  it('stacks only the fields assigned to each compact column', () => {
    const html = renderCard()
    const cells = extractFirstTableRowCells(html)

    expect(cells).toHaveLength(17)
    expect(cells[2]).toContain('OpenAI Team')
    expect(cells[2]).toContain('Deleted')
    expect(cells[2]).not.toContain('openai')
    expect(html).toMatch(/data-provider-brand-icon="openai"[^>]*style="width:25px;height:25px"/)
    expect(cells[3]).toBe('gpt-5.6keeper-gpt')
    expect(cells[4]).toBe('high')
    expect(cells[5]).toBe('Fast / Flex')
    expect(cells[7]).toBe('SSE/messages')
    expect(cells[8]).toBe('120msTTFT 45ms')
    expect(cells[9]).toBe('30.0 t/s')
    expect(cells[10]).toBe('200Input 100Output 60 (Reasoning 20)')
    expect(cells[11]).toBe('20.00%Read 20Write 5')
    expect(cells[12]).toBe('$0.1234Claude Style')
    expect(cells[13]).toBe('OpenAIResponsesExecutor')
    expect(cells.slice(14)).toEqual([
      '192.0.2.10',
      '203.0.113.5, 198.51.100.8',
      'keeper-client/1.0',
    ])
  })

  it('emphasizes standalone primary values except client metadata', () => {
    const cellMarkup = extractFirstTableRowCellMarkup(renderCard())

    for (const index of [1, 4, 5, 9, 13]) {
      expect(cellMarkup[index]).toContain('requestEventsPrimaryCell')
    }
    expect(cellMarkup[12]).toContain('requestEventsStackedPrimary')
    for (const index of [14, 15, 16]) {
      expect(cellMarkup[index]).not.toContain('requestEventsPrimaryCell')
    }
  })
})
