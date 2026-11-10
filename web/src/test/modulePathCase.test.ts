import { readdirSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, it } from 'vitest'

it('keeps extensionless module paths unique on case-insensitive filesystems', () => {
  const root = fileURLToPath(new URL('..', import.meta.url))
  const modules = new Map<string, string[]>()

  function visit(directory: string) {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name)
      if (entry.isDirectory()) {
        visit(path)
      } else if (/\.[cm]?[jt]sx?$/.test(entry.name) && !entry.name.endsWith('.d.ts')) {
        // 扩展名解析顺序会让仅大小写不同的 .ts/.tsx 模块在 macOS/Windows 上互相遮蔽。
        const key = relative(root, join(dirname(path), entry.name.replace(/\.[^.]+$/, ''))).toLowerCase()
        modules.set(key, [...(modules.get(key) ?? []), relative(root, path)])
      }
    }
  }

  visit(root)
  expect([...modules.values()].filter((paths) => paths.length > 1)).toEqual([])
})
