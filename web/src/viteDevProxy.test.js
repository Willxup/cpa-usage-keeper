import { describe, expect, it } from 'vitest'
import config from '../vite.config.js'

function resolveConfigWithEnv(value) {
  const previous = process.env.VITE_API_PROXY_TARGET
  if (value === undefined) {
    delete process.env.VITE_API_PROXY_TARGET
  } else {
    process.env.VITE_API_PROXY_TARGET = value
  }

  try {
    return typeof config === 'function'
      ? config({ command: 'serve', mode: 'development', isSsrBuild: false, isPreview: false })
      : config
  } finally {
    if (previous === undefined) {
      delete process.env.VITE_API_PROXY_TARGET
    } else {
      process.env.VITE_API_PROXY_TARGET = previous
    }
  }
}

function resolveBuildConfig() {
  return typeof config === 'function'
    ? config({ command: 'build', mode: 'production', isSsrBuild: false, isPreview: false })
    : config
}

describe('vite dev server proxy', () => {
  it('proxies API requests to the local backend by default', () => {
    const resolved = resolveConfigWithEnv(undefined)

    expect(resolved.server?.proxy?.['/api']?.target).toBe('http://127.0.0.1:8080')
    expect(resolved.server?.proxy?.['/api']?.changeOrigin).toBe(true)
  })

  it('allows overriding the backend proxy target', () => {
    const resolved = resolveConfigWithEnv('http://127.0.0.1:9090')

    expect(resolved.server?.proxy?.['/api']?.target).toBe('http://127.0.0.1:9090')
  })

  it('does not add the dev proxy to production build config', () => {
    const resolved = resolveBuildConfig()

    expect(resolved.server?.proxy).toBeUndefined()
  })
})
