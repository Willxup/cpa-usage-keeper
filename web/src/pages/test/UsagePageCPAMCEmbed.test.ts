import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const usagePageSource = readFileSync(new URL('../UsagePage.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n');

describe('UsagePage CPAMC embed behavior', () => {
  it('does not render the Back to CPA link in CPAMC embed mode', () => {
    expect(usagePageSource).toContain("import { isCPAMCEmbed } from '@/embed/cpamcEmbed';");
    expect(usagePageSource).toMatch(/const isEmbeddedInCPAMC = isCPAMCEmbed\(\);/);
    expect(usagePageSource).toMatch(/\{!viewerMode && !isEmbeddedInCPAMC && cpaManagementURL && \(/);
  });
});
