import { afterEach, describe, expect, it, vi } from 'vitest'
import config from '../../vite.config.js'

afterEach(() => vi.unstubAllEnvs())

const resolveConfig = (command = 'serve') => config({
  command,
  mode: command === 'serve' ? 'development' : 'production',
  isSsrBuild: false,
  isPreview: false,
})

describe('vite dev server proxy', () => {
  it('proxies API requests to the local backend by default', () => {
    vi.stubEnv('VITE_API_PROXY_TARGET', undefined)
    const resolved = resolveConfig()

    expect(resolved.server.proxy['/api'].target).toBe('http://127.0.0.1:8080')
    expect(resolved.server.proxy['/api'].changeOrigin).toBe(true)
  })

  it('allows overriding the backend proxy target', () => {
    vi.stubEnv('VITE_API_PROXY_TARGET', 'http://127.0.0.1:9090')
    const resolved = resolveConfig()

    expect(resolved.server.proxy['/api'].target).toBe('http://127.0.0.1:9090')
  })

  it('does not add the dev proxy to production build config', () => {
    const resolved = resolveConfig('build')

    expect(resolved.server?.proxy).toBeUndefined()
  })
})
