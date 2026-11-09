// 组件级真实浏览器几何回归；通过 browser-test run 执行，完整页面另行验收。
import assert from 'node:assert/strict'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'

const webRoot = fileURLToPath(new URL('../../../../../', import.meta.url))
const fixtureId = '\0credential-health-panel-layout'
const fixtureEntry = 'virtual:credential-health-panel-layout'
const health = {
  window_seconds: 18000,
  bucket_seconds: 600,
  window_start: '2026-07-16T10:00:00+08:00',
  window_end: '2026-07-16T15:00:00+08:00',
  total_success: 9,
  total_failure: 1,
  success_rate: 90,
  input_tokens: 0,
  cache_read_tokens: 0,
  buckets: [{
    start_time: '2026-07-16T10:00:00+08:00',
    end_time: '2026-07-16T10:10:00+08:00',
    success: 9,
    failure: 1,
    rate: 0.9,
  }],
}

// 编译真实组件和样式，不启动开发服务器，也不以手写 DOM 代替组件。
const bundle = await build({
  root: webRoot,
  configFile: path.join(webRoot, 'vite.config.js'),
  configLoader: 'runner',
  cacheDir: path.join(tmpdir(), 'credential-health-panel-vite-cache'),
  logLevel: 'error',
  plugins: [{
    name: 'credential-health-panel-layout-fixture',
    resolveId: (id) => id === fixtureEntry ? fixtureId : null,
    load: (id) => id === fixtureId ? `
      import { createElement } from 'react'
      import { createRoot } from 'react-dom/client'
      import '@/styles/global.scss'
      import { CredentialHealthPanel } from '@/components/usage/credentials/CredentialHealthPanel'
      createRoot(document.getElementById('root')).render(createElement(CredentialHealthPanel, {
        displayName: 'Test credential',
        health: ${JSON.stringify(health)},
        variant: document.documentElement.dataset.variant,
      }))
    ` : null,
  }],
  build: {
    write: false,
    minify: false,
    cssMinify: false,
    rollupOptions: { input: fixtureEntry, output: { format: 'iife', name: 'HealthPanelFixture' } },
  },
})
const script = bundle.output.find((item) => item.type === 'chunk')?.code
assert.ok(script, 'fixture bundle must include the component')

const { launchBrowser } = await import(process.env.BROWSER_TEST_RUNTIME)
const scenarios = [
  { variant: 'compact', viewport: 390, width: 310 },
  { variant: 'compact', viewport: 390, width: 350 },
  { variant: 'compact', viewport: 1440, width: 460 },
  { variant: 'compact', viewport: 1440, width: 700 },
  { variant: 'full', viewport: 390, width: 310 },
  { variant: 'full', viewport: 1440, width: 700 },
]
// 包括两端所有特殊定位的桶，以及两端相邻和中间的普通桶。
const indices = [0, 1, 2, 3, 4, 14, 25, 26, 27, 28, 29]

for (const engine of ['chromium', 'webkit']) {
  const browser = await launchBrowser(engine)
  try {
    const page = await browser.newPage()
    const pageErrors = []
    page.on('pageerror', (error) => pageErrors.push(error.message))
    await page.route('http://credential-health.test/', (route) => route.fulfill({ status: 200, body: '<!doctype html>' }))
    await page.goto('http://credential-health.test/')
    for (const { variant, viewport, width } of scenarios) {
      await page.setViewportSize({ width: viewport, height: 700 })
      await page.mouse.move(0, 0)
      await page.setContent(`<!doctype html><html data-variant="${variant}"><head></head><body><div id="root" style="width:${width}px;height:auto;margin:120px auto 0"></div></body></html>`)
      await page.addStyleTag({ content: '[role="listitem"], [role="tooltip"] { transition-duration: 0s !important; }' })
      await page.addScriptTag({ content: script })
      const cells = page.locator('[role="listitem"]')
      await cells.first().waitFor()
      assert.equal(await page.locator('#root').evaluate((element) => element.getBoundingClientRect().width), width)
      assert.equal(await cells.count(), 30, `${engine} ${variant}: expected 30 buckets`)
      assert.equal(await cells.first().locator('[role="tooltip"]').evaluate((element) => getComputedStyle(element).opacity), '0')
      assert.match(await cells.first().locator('[role="tooltip"]').textContent(), /10:00 - 10:10.*90\.0%/)
      assert.match(await cells.last().locator('[role="tooltip"]').textContent(), /14:50 - 15:00.*0\.0%/)

      for (const index of indices) {
        const cell = cells.nth(index)
        await cell.hover()
        const layout = await cell.evaluate((element) => {
          const tooltip = element.querySelector('[role="tooltip"]')
          const arrowStyle = getComputedStyle(tooltip, '::after')
          // 伪元素没有可直接读取的矩形；在相同定位上下文中复制其计算样式来测量。
          const arrow = document.createElement('span')
          for (const property of [
            'position', 'box-sizing', 'top', 'right', 'bottom', 'left', 'width', 'height',
            'border-top', 'border-right', 'border-bottom', 'border-left', 'transform', 'transform-origin',
          ]) arrow.style.setProperty(property, arrowStyle.getPropertyValue(property))
          arrow.style.visibility = 'hidden'
          tooltip.append(arrow)
          const cellRect = element.getBoundingClientRect()
          const tooltipRect = tooltip.getBoundingClientRect()
          const arrowRect = arrow.getBoundingClientRect()
          const result = {
            opacity: Number(getComputedStyle(tooltip).opacity),
            arrowVisible: arrowStyle.content !== 'none' && arrowStyle.content !== 'normal' && arrowRect.width > 0 && arrowRect.height > 0,
            cellCenter: (cellRect.left + cellRect.right) / 2,
            arrowCenter: (arrowRect.left + arrowRect.right) / 2,
            tooltipLeft: tooltipRect.left,
            tooltipRight: tooltipRect.right,
          }
          arrow.remove()
          return result
        })
        const label = `${engine} ${variant} viewport=${viewport} width=${width} bucket=${index}`
        assert.equal(layout.opacity, 1, `${label}: tooltip should appear on hover`)
        assert.ok(layout.arrowVisible, `${label}: tooltip arrow should be rendered`)
        assert.ok(Math.abs(layout.arrowCenter - layout.cellCenter) <= 0.35,
          `${label}: arrow offset ${(layout.arrowCenter - layout.cellCenter).toFixed(2)}px`)
        if (index < 4 || index >= 26) {
          assert.ok(layout.tooltipLeft >= -0.5 && layout.tooltipRight <= viewport + 0.5,
            `${label}: tooltip outside viewport (${layout.tooltipLeft.toFixed(2)}..${layout.tooltipRight.toFixed(2)})`)
        }
      }
      console.log(`PASS ${engine} ${variant} viewport=${viewport} width=${width}: ${indices.length} buckets`)
    }
    assert.deepEqual(pageErrors, [], `${engine}: unexpected browser errors`)
  } finally {
    await browser.close()
  }
}
