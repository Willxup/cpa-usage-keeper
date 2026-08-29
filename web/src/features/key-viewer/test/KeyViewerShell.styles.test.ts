import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const styles = readFileSync(new URL('../KeyViewerShell.module.scss', import.meta.url), 'utf8');

describe('KeyViewerShell loading layer', () => {
  it('keeps the viewer toolbar above the page loading overlay', () => {
    const toolbarBlock = styles.match(/\.toolbarRow\s*\{[\s\S]*?\n\}/)?.[0] ?? '';

    expect(toolbarBlock).toContain('position: relative;');
    expect(toolbarBlock).toContain('z-index: 6;');
  });
});
