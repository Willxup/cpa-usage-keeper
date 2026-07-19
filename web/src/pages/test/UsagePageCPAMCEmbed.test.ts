import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const usagePageSource = readFileSync(new URL('../UsagePage.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n');

describe('UsagePage CPAMC embed behavior', () => {
  it('no longer uses embed-specific filter transitions now that filters are conditionally mounted', () => {
    expect(usagePageSource).not.toContain('isCPAMCEmbed')
    expect(usagePageSource).not.toContain('isEmbeddedInCPAMC')
    expect(usagePageSource).not.toContain('styles.usageFilterTransitionImmediate')
    expect(usagePageSource).not.toContain('styles.toolbarActionsRightAnimated')
  });
});
