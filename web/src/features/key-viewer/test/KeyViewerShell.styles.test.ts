import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const styles = readFileSync(new URL('../KeyViewerShell.module.scss', import.meta.url), 'utf8');
const toolbarStyles = readFileSync(new URL('../../../components/dashboard/DashboardToolbar.module.scss', import.meta.url), 'utf8');

describe('KeyViewerShell loading layer', () => {
  it('keeps the viewer toolbar above the page loading overlay', () => {
    const toolbarBlock = toolbarStyles.match(/\.host\s*\{[\s\S]*?\n\}/)?.[0] ?? '';

    expect(toolbarBlock).toContain('position: sticky;');
    expect(toolbarBlock).toContain('z-index: 30;');
    expect(styles).toMatch(/\.loadingOverlay\s*\{[\s\S]*?z-index:\s*5;/);
  });

});
