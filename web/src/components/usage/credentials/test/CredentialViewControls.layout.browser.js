// 真实组件的触控尺寸与跨行分隔线回归；完整 Keeper 页面仍需另行验收。
import assert from 'node:assert/strict'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'

const webRoot = fileURLToPath(new URL('../../../../../', import.meta.url))
const entry = 'virtual:credential-view-controls'
const fixture = '\0credential-view-controls'
const bundle = await build({
  root: webRoot,
  configFile: path.join(webRoot, 'vite.config.js'),
  configLoader: 'runner',
  cacheDir: path.join(tmpdir(), 'credential-view-controls-vite-cache'),
  logLevel: 'error',
  plugins: [{
    name: 'credential-view-controls-fixture',
    resolveId: (id) => id === entry ? fixture : null,
    load: (id) => id === fixture ? `
      import { createElement } from 'react'
      import { createRoot } from 'react-dom/client'
      import '@/styles/global.scss'
      import i18n from '@/i18n'
      import { CredentialsSection } from '@/components/usage/credentials/CredentialsSection'
      import { createCredentialSectionProps } from '@/components/usage/credentials/test/credentialSectionFixtures'
      const { mode, language } = window.fixture
      i18n.changeLanguage(language).then(() => createRoot(document.getElementById('root')).render(
        createElement(CredentialsSection, createCredentialSectionProps({ viewMode: mode, total: 25, totalPages: 3 }))
      ))
    ` : null,
  }],
  build: { write: false, minify: false, cssMinify: false, rollupOptions: { input: entry, output: { format: 'iife', name: 'CredentialControlsFixture' } } },
})
const script = bundle.output.find((item) => item.type === 'chunk')?.code
assert.ok(script)
const { launchBrowser } = await import(process.env.BROWSER_TEST_RUNTIME)
const scenarios = [240, 268, 320, 340, 380, 499, 501, 550, 600, 800].flatMap((width) =>
  (width >= 268 ? [1, 2] : [1]).map((scale) => ({ width, scale })))
let checks = 0
for (const engine of ['chromium', 'webkit']) {
  const browser = await launchBrowser(engine)
  try {
    for (const touch of [false, true]) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, hasTouch: touch })
      const page = await context.newPage()
      const errors = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.route('http://credential-controls.test/', (route) => route.fulfill({ body: '<!doctype html>' }))
      await page.goto('http://credential-controls.test/')
      for (const { width, scale } of scenarios) {
        for (const mode of ['cards', 'list']) {
          const language = scale === 2 || width === 501 ? 'en' : 'zh'
          await page.setContent(`<div id="root" style="width:${width}px"></div>`)
          await page.evaluate((value) => { window.fixture = value }, { mode, language })
          await page.addScriptTag({ content: script })
          const controls = page.locator('[class*="credentialsTitleControls"]')
          await controls.waitFor()
          if (scale === 2) await controls.evaluate((element) => {
            const sizes = [...element.querySelectorAll('*')]
              .filter((child) => child instanceof HTMLElement)
              .map((child) => [child, parseFloat(getComputedStyle(child).fontSize)])
            sizes.forEach(([child, size]) => { child.style.fontSize = `${size * 2}px` })
          })
          const geometry = await controls.evaluate((element) => {
            const children = [...element.children].filter((child) => child.getBoundingClientRect().height > 0)
            const orphan = children.some((child, index) => {
              if (!child.className.includes('Divider')) return false
              const before = children[index - 1]?.getBoundingClientRect()
              const after = children[index + 1]?.getBoundingClientRect()
              return !before || !after || Math.abs(before.y + before.height / 2 - after.y - after.height / 2) > 1
            })
            const heights = [element.querySelector('label'), ...element.querySelectorAll('[role="group"]')].map((child) => child.getBoundingClientRect().height)
            const hitAreas = [...element.querySelectorAll('[role="group"] button')].map((button) => {
              const rect = button.getBoundingClientRect()
              return { width: rect.width, height: rect.height }
            })
            return { orphan, heights, hitAreas, width: element.clientWidth, scrollWidth: element.scrollWidth }
          })
          const label = `${engine} ${touch ? 'touch' : 'pointer'} ${mode} width=${width} text=${scale * 100}%`
          assert.equal(geometry.orphan, false, `${label}: divider stranded across rows`)
          assert.ok(geometry.scrollWidth <= geometry.width + 1, `${label}: controls overflow`)
          assert.ok(geometry.heights.every((height) => Math.abs(height - (touch ? 52 : 34)) <= 1), `${label}: inconsistent control height`)
          const target = touch ? 44 : 24
          assert.ok(geometry.hitAreas.every((rect) => rect.width >= target && rect.height >= target), `${label}: undersized button`)
          if (mode === 'list') {
            const next = page.getByRole('button', { name: language === 'en' ? 'Next' : '下一页', exact: true })
            await next.focus()
            await page.waitForFunction((button) => {
              const bar = button.closest('[class*="credentialPagination_"]').getBoundingClientRect()
              const rect = button.getBoundingClientRect()
              return rect.left >= bar.left + 2 && rect.right <= bar.right - 2
            }, await next.elementHandle(), { timeout: 1000 })
            const focus = await next.evaluate((button) => {
              const pagination = button.closest('[class*="credentialPagination_"]')
              const bar = pagination.getBoundingClientRect()
              const rect = button.getBoundingClientRect()
              return { left: rect.left - bar.left, right: bar.right - rect.right, height: bar.height }
            })
            assert.ok(focus.left >= 2 && focus.right >= 2, `${label}: pagination focus must stay visible`)
            assert.ok(focus.height >= (touch ? 64 : 51), `${label}: pagination clips controls`)
          }
          checks += 1
        }
      }
      assert.deepEqual(errors, [], `${engine}: page errors`)
      await context.close()
    }
  } finally {
    await browser.close()
  }
}
console.log(`PASS ${checks} control layouts across Chromium/WebKit, pointer/touch, cards/list, narrow containers and 200% control text`)
