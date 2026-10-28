import { describe, expect, it } from 'vitest'
import { parseRequestEndpoint } from '../requestEndpoint'

describe('request endpoint display', () => {
  it.each([
    ['/backend-api/codex/responses', '/v1/responses'],
    ['/backend-api/codex/responses/compact', '/v1/responses/compact'],
    ['/backend-api/codex/alpha/search', '/v1/alpha/search'],
    ['/v1/tts', '/v1/audio/speech'],
    ['/v1/videos', '/v1/videos/generations'],
  ])('normalizes the exact alias %s with and without a method', (raw, endpoint) => {
    for (const [prefix, requestType] of [['', '-'], ['POST ', 'SSE'], ['GET ', 'WS']]) {
      expect(parseRequestEndpoint(`${prefix}${raw}`)).toEqual({ requestType, endpoint })
    }
  })

  it.each([
    '/v1/responses', '/v1/messages', '/v1/chat/completions', '/v1',
    '/v1beta/models/*action', '/openai/v1/videos', '/v1/videos/edits',
    '/backend-api/codex/unknown', '/backend-api/codex/responses/extra',
    '/v1/tts/', '/v1/tts?format=mp3', 'constructor',
  ])('preserves the unmapped path %s', (endpoint) => {
    expect(parseRequestEndpoint(`POST ${endpoint}`).endpoint).toBe(endpoint)
  })

  it('preserves method parsing and empty-value fallbacks', () => {
    expect(parseRequestEndpoint('  post   /v1/messages  ')).toEqual({ requestType: 'SSE', endpoint: '/v1/messages' })
    for (const raw of [undefined, null, '', '  ']) {
      expect(parseRequestEndpoint(raw)).toEqual({ requestType: '-', endpoint: '-' })
    }
    expect(parseRequestEndpoint('POST')).toEqual({ requestType: 'SSE', endpoint: '-' })
  })
})
