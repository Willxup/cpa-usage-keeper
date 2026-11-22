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
      import { AiProviderCredentialsSection } from '@/components/usage/credentials/AiProviderCredentialsSection'
      import { createCredentialSectionProps, createAiProviderSectionProps } from '@/components/usage/credentials/test/credentialSectionFixtures'
      const { mode, language, section } = window.fixture
      const provider = section === 'providers'
      const component = provider ? AiProviderCredentialsSection : CredentialsSection
      const props = provider ? createAiProviderSectionProps : createCredentialSectionProps
      i18n.changeLanguage(language).then(() => createRoot(document.getElementById('root')).render(
        createElement(component, props({ viewMode: mode, total: 25, totalPages: 3 }))
      ))
    ` : null,
  }],
  build: { write: false, minify: false, cssMinify: false, rollupOptions: { input: entry, output: { format: 'iife', name: 'CredentialControlsFixture' } } },
})
const script = bundle.output.find((item) => item.type === 'chunk')?.code
assert.ok(script)
const { launchBrowser } = await import(process.env.BROWSER_TEST_RUNTIME)
const scenarios = [240, 268, 279, 280, 320, 340, 379, 380, 381, 410, 499, 501, 550, 599, 600, 601, 800, 815, 816, 817, 850, 900, 950, 1000, 1100, 1280, 1440].flatMap((width) =>
  (width >= 268 ? [1, 2] : [1]).map((scale) => ({ width, scale })))
let checks = 0
let providerChecks = 0
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
        for (const { mode, contentMode } of [{ mode: 'cards', contentMode: 'quota' }, { mode: 'list', contentMode: 'quota' }, { mode: 'list', contentMode: 'health' }]) {
          const language = scale === 2 || width === 501 ? 'en' : 'zh'
          await page.setContent(`<div id="root" style="width:${width}px"></div>`)
          await page.evaluate((value) => {
            localStorage.setItem('cpa.credentials.displayMode', value.contentMode)
            window.fixture = value
          }, { mode, contentMode, language })
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
            const header = element.closest('[class*="credentialSectionHeader"]')
            const title = header.querySelector('h3')
            const help = header.querySelector('[class*="credentialSectionHeading"] button')
            const filter = element.querySelector('input[type="checkbox"]')
            const groups = [...element.querySelectorAll('[role="group"]')]
            const allChildren = [...element.children]
            const children = allChildren.filter((child) => child.getBoundingClientRect().height > 0)
            const orphan = children.some((child, index) => {
              if (!child.className.includes('Divider')) return false
              const before = children[index - 1]?.getBoundingClientRect()
              const after = children[index + 1]?.getBoundingClientRect()
              return !before || !after || Math.abs(before.y + before.height / 2 - after.y - after.height / 2) > 1
            })
            const heights = [element.querySelector('label'), ...element.querySelectorAll('[role="group"]')].map((child) => child.getBoundingClientRect().height)
            const hitAreas = [...element.querySelectorAll('[role="group"] button')].map((button) => {
              const rect = button.getBoundingClientRect()
              return { width: rect.width, height: rect.height, icons: button.querySelectorAll('svg').length, text: button.textContent.trim() }
            })
            const inside = (outer, inner) => {
              const a = outer.getBoundingClientRect(), b = inner.getBoundingClientRect()
              return b.left >= a.left - 1 && b.right <= a.right + 1 && b.top >= a.top - 1 && b.bottom <= a.bottom + 1
            }
            return {
              orphan, heights, hitAreas, width: element.clientWidth, scrollWidth: element.scrollWidth,
              groupNames: groups.map((group) => group.getAttribute('aria-label')),
              ordered: [title, help, filter, groups[0]].every((child, index, all) => index === 0 || !!(all[index - 1].compareDocumentPosition(child) & Node.DOCUMENT_POSITION_FOLLOWING)),
              lastGroup: element.lastElementChild?.contains(groups.at(-1)),
              structure: allChildren.length === 1 + groups.length * 2 && groups.every((group, index) => allChildren[index * 2 + 1].className.includes('Divider') && allChildren[index * 2 + 2].contains(group)),
              intact: groups.every((group) => inside(element, group) && [...group.querySelectorAll('button')].every((button) => inside(group, button))),
              usageCount: document.querySelectorAll('[aria-label="Quota usage display mode"], [aria-label="限额用量显示模式"]').length,
              paginationUsageCount: document.querySelectorAll('[class*="credentialPagination_"] [aria-label="Quota usage display mode"], [class*="credentialPagination_"] [aria-label="限额用量显示模式"]').length,
              pageWidth: document.documentElement.clientWidth, pageScrollWidth: document.documentElement.scrollWidth,
            }
          })
          const label = `${engine} ${touch ? 'touch' : 'pointer'} ${mode}/${contentMode} width=${width} text=${scale * 100}%`
          const names = language === 'en'
            ? ['View layout', 'List side panel', 'Quota usage display mode']
            : ['视图布局', '列表右侧内容', '限额用量显示模式']
          const expected = mode === 'cards' ? [names[0], names[2]] : contentMode === 'quota' ? names : [names[0], names[1]]
          assert.deepEqual(geometry.groupNames, expected, `${label}: wrong header groups or order`)
          assert.equal(geometry.usageCount, expected.includes(names[2]) ? 1 : 0, `${label}: usage switch must be absent or unique`)
          assert.ok(geometry.ordered && geometry.lastGroup && geometry.structure, `${label}: header order or divider structure is incorrect`)
          assert.ok(geometry.intact, `${label}: a control group is split or overflows`)
          assert.equal(geometry.paginationUsageCount, 0, `${label}: usage switch must not appear in pagination`)
          assert.ok(geometry.pageScrollWidth <= geometry.pageWidth + 1, `${label}: page overflows`)
          assert.equal(geometry.orphan, false, `${label}: divider stranded across rows`)
          assert.ok(geometry.scrollWidth <= geometry.width + 1, `${label}: controls overflow`)
          assert.ok(geometry.heights.every((height) => Math.abs(height - (touch ? 52 : 34)) <= 1), `${label}: inconsistent control height`)
          const target = touch ? 44 : 24
          assert.ok(geometry.hitAreas.every((rect) => rect.width >= target && rect.height >= target), `${label}: undersized button`)
          assert.ok(geometry.hitAreas.every((rect) => rect.icons === 1 && rect.text === ''), `${label}: options must remain icon-only`)
          if (mode === 'list') {
            const visual = await page.evaluate(({ layoutName, contentName }) => {
              const group = (name) => document.querySelector(`[role="group"][aria-label="${name}"]`)
              const layout = group(layoutName)
              const content = group(contentName)
              const style = (element, properties) => properties.map((property) => getComputedStyle(element)[property])
              const selected = (element) => element.querySelector('[aria-pressed="true"]')
              return {
                text: content.textContent,
                children: [...content.children].map((child) => child.tagName),
                icons: content.querySelectorAll('button > svg').length,
                groupStyle: style(content, ['height', 'borderRadius', 'backgroundColor']),
                layoutStyle: style(layout, ['height', 'borderRadius', 'backgroundColor']),
                selectedStyle: style(selected(content), ['color', 'backgroundColor', 'borderRadius']),
                selectedLayoutStyle: style(selected(layout), ['color', 'backgroundColor', 'borderRadius']),
              }
            }, { layoutName: language === 'en' ? 'View layout' : '视图布局', contentName: language === 'en' ? 'List side panel' : '列表右侧内容' })
            assert.equal(visual.text, '', `${label}: list content control must not display option text`)
            assert.deepEqual(visual.children, ['BUTTON', 'BUTTON'], `${label}: list content control must not contain a thumb`)
            assert.equal(visual.icons, 2, `${label}: both content options need icons`)
            assert.deepEqual(visual.groupStyle, visual.layoutStyle, `${label}: list content group should match layout control`)
            assert.deepEqual(visual.selectedStyle, visual.selectedLayoutStyle, `${label}: selected content should match layout control`)
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
      for (const width of [240, 320, 380, 600, 800]) {
        for (const language of ['en', 'zh']) {
          await page.setContent(`<div id="root" style="width:${width}px"></div>`)
          await page.evaluate((value) => { window.fixture = value }, { mode: 'cards', language, section: 'providers' })
          await page.addScriptTag({ content: script })
          const helpName = language === 'en' ? 'About AI provider views' : 'AI 供应商视图说明'
          const help = page.getByRole('button', { name: helpName })
          await help.waitFor()
          const order = await page.evaluate((helpLabel) => {
            const title = document.querySelector('h3')
            const button = document.querySelector(`button[aria-label="${helpLabel}"]`)
            const controls = document.querySelector('[class*="credentialsTitleControls"]')
            const filter = controls.querySelector('label')
            const view = controls.querySelector('[role="group"]')
            const rect = (element) => {
              const { left, right, top, bottom, height } = element.getBoundingClientRect()
              return { left, right, top, bottom, height }
            }
            return {
              sharedHeading: title.parentElement === button.parentElement.parentElement,
              titleBeforeHelp: !!(title.compareDocumentPosition(button) & Node.DOCUMENT_POSITION_FOLLOWING),
              helpBeforeFilter: !!(button.compareDocumentPosition(filter) & Node.DOCUMENT_POSITION_FOLLOWING),
              filterBeforeView: !!(filter.compareDocumentPosition(view) & Node.DOCUMENT_POSITION_FOLLOWING),
              controlChildren: [...controls.children].map((child) => child.className),
              title: rect(title), help: rect(button), filter: rect(filter), view: rect(view),
              rootWidth: document.documentElement.clientWidth, rootScrollWidth: document.documentElement.scrollWidth,
            }
          }, helpName)
          const label = `${engine} ${touch ? 'touch' : 'pointer'} providers width=${width} ${language}`
          assert.ok(order.sharedHeading && order.titleBeforeHelp && order.helpBeforeFilter && order.filterBeforeView, `${label}: incorrect DOM order`)
          assert.ok(Math.abs((order.title.top + order.title.bottom) / 2 - (order.help.top + order.help.bottom) / 2) <= 1, `${label}: help is not alongside the title`)
          assert.ok(order.title.right <= order.help.left, `${label}: title and help overlap`)
          const helpCenter = (order.help.top + order.help.bottom) / 2
          const filterCenter = (order.filter.top + order.filter.bottom) / 2
          assert.ok(helpCenter <= filterCenter + 1, `${label}: filter appears above title help`)
          if (Math.abs(helpCenter - filterCenter) < 2) assert.ok(order.help.right <= order.filter.left, `${label}: filter appears before title help`)
          assert.equal(order.controlChildren.length, 3, `${label}: unexpected title control or divider`)
          assert.ok(order.controlChildren[1].includes('Divider'), `${label}: expected divider between filter and view`)
          assert.ok(Math.abs(order.filter.height - (touch ? 52 : 34)) <= 1 && Math.abs(order.view.height - (touch ? 52 : 34)) <= 1, `${label}: filter and view heights differ`)
          assert.ok(order.rootScrollWidth <= order.rootWidth + 1, `${label}: page overflows`)
          providerChecks += 1
        }
      }
      assert.deepEqual(errors, [], `${engine}: page errors`)
      await context.close()
    }
  } finally {
    await browser.close()
  }
}
console.log(`PASS ${checks} credential control layouts and ${providerChecks} AI title-help layouts across Chromium/WebKit and pointer/touch`)
