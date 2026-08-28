import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const drawerStyles = readFileSync(new URL('../CredentialDetailDrawer.module.scss', import.meta.url), 'utf8')

const scssRule = (selector: string) => {
  const start = drawerStyles.indexOf(selector)
  expect(start).toBeGreaterThanOrEqual(0)

  const openingBrace = drawerStyles.indexOf('{', start + selector.length)
  expect(openingBrace).toBeGreaterThan(start)
  let depth = 1
  for (let index = openingBrace + 1; index < drawerStyles.length; index += 1) {
    if (drawerStyles[index] === '{') {
      depth += 1
    } else if (drawerStyles[index] === '}' && --depth === 0) {
      return drawerStyles.slice(start, index + 1)
    }
  }

  throw new Error(`Unclosed SCSS rule: ${selector}`)
}

const topLevelDeclarations = (selector: string) => {
  const rule = scssRule(selector)
  const openingBrace = rule.indexOf('{')
  const declarations: string[] = []
  let nestedDepth = 0
  let declarationStart = openingBrace + 1

  for (let index = declarationStart; index < rule.length; index += 1) {
    if (rule[index] === '{') {
      nestedDepth += 1
    } else if (rule[index] === '}') {
      if (nestedDepth === 0) {
        break
      }
      nestedDepth -= 1
      if (nestedDepth === 0) {
        declarationStart = index + 1
      }
    } else if (rule[index] === ';' && nestedDepth === 0) {
      declarations.push(rule.slice(declarationStart, index + 1).trim())
      declarationStart = index + 1
    }
  }

  return declarations
}

describe('CredentialDetailDrawer styles', () => {
  it('uses the shared Keeper radius for every Overview card surface', () => {
    for (const selector of ['.summaryMetric', '.overviewSection']) {
      const radiusDeclarations = topLevelDeclarations(selector).filter((declaration) => /^border-radius\s*:/.test(declaration))

      expect(radiusDeclarations).toEqual(['border-radius: var(--keeper-card-radius);'])
    }
  })

  it('keeps neutral cards unchanged while allowing list tone classes in request details', () => {
    const summaryDeclarations = topLevelDeclarations('.summaryMetric')

    expect(summaryDeclarations).toContain('border: 1px solid var(--border-color);')
    expect(summaryDeclarations).toContain('background: var(--bg-secondary);')
    expect(scssRule('.summaryMetric')).toContain('> span,\n  > strong,\n  > small')
    expect(drawerStyles).not.toContain('#3b82f6')
    expect(drawerStyles).not.toContain('#8b5cf6')
  })

  it('stacks identity and quota cards in the mobile Overview layout', () => {
    const mobileRules = scssRule('@include mobile')

    expect(mobileRules).toMatch(/\.overviewGrid\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
  })
})
