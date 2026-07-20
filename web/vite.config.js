import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

function getApiProxyTarget() {
  return process.env.VITE_API_PROXY_TARGET?.trim() || 'http://127.0.0.1:8080'
}

export default defineConfig(({ command }) => {
  const apiProxy = {
    '/api': {
      target: getApiProxyTarget(),
      changeOrigin: true,
    },
  }

  return {
    base: './',
    plugins: [react()],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, 'src'),
      },
    },
    server: command === 'serve' ? { proxy: apiProxy } : undefined,
    preview: { proxy: apiProxy },
  }
})
