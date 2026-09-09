import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const styles = readFileSync(new URL('../KeyViewerShell.module.scss', import.meta.url), 'utf8');
const source = readFileSync(new URL('../KeyViewerShell.tsx', import.meta.url), 'utf8');

describe('KeyViewerShell loading layer', () => {
  it('keeps the viewer toolbar above the page loading overlay', () => {
    const toolbarBlock = styles.match(/\.toolbarRow\s*\{[\s\S]*?\n\}/)?.[0] ?? '';

    expect(toolbarBlock).toContain('position: relative;');
    expect(toolbarBlock).toContain('z-index: 6;');
  });

  it('uses the same connected tab treatment and toolbar grid as the management page', () => {
    expect(source).toContain('styles.tabBarConnected');
    expect(source).toContain('lang={i18n.resolvedLanguage || i18n.language}');
    expect(styles).toMatch(/\.tabBarConnected\s*\{[\s\S]*?border-radius:\s*999px;/);
    expect(styles).toMatch(/\.tabBarConnected \.tabPill\s*\{[\s\S]*?font-weight:\s*700;/);
    expect(styles).toMatch(/\.toolbarActionsRight\s*\{[\s\S]*?display:\s*grid;[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\) auto;/);
  });
});
