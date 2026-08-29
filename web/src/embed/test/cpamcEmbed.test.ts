import { afterEach, describe, expect, it, vi } from 'vitest';
import { cpamcEmbedSearch, isCPAMCEmbed, notifyCPAMCEmbedReady } from '../cpamcEmbed';

describe('CPAMC embed query helpers', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('detects only the CPAMC embed mode', () => {
    expect(isCPAMCEmbed('?embed=cpamc')).toBe(true);
    expect(isCPAMCEmbed('?mode=cpamc')).toBe(true);
    expect(isCPAMCEmbed('embed=cpamc')).toBe(true);
    expect(isCPAMCEmbed('?foo=1&embed=cpamc')).toBe(true);
    expect(isCPAMCEmbed('?embed=iframe&embed=cpamc')).toBe(true);
    expect(isCPAMCEmbed('?mode=normal&mode=cpamc')).toBe(true);
    expect(isCPAMCEmbed('?embed=iframe')).toBe(false);
    expect(isCPAMCEmbed('?embed=CPAMC')).toBe(false);
    expect(isCPAMCEmbed('')).toBe(false);
  });

  it('preserves only the CPAMC embed query for app navigation', () => {
    expect(cpamcEmbedSearch('?embed=cpamc')).toBe('?embed=cpamc');
    expect(cpamcEmbedSearch('?mode=cpamc')).toBe('?embed=cpamc');
    expect(cpamcEmbedSearch('?foo=1&embed=cpamc&bar=2')).toBe('?embed=cpamc');
    expect(cpamcEmbedSearch('?embed=iframe&embed=cpamc')).toBe('?embed=cpamc');
    expect(cpamcEmbedSearch('?embed=iframe')).toBe('');
    expect(cpamcEmbedSearch('')).toBe('');
  });

  it('notifies the parent frame only in CPAMC embed mode', () => {
    const messages: unknown[] = [];
    vi.stubGlobal('window', {
      parent: { postMessage: (message: unknown) => messages.push(message) },
    });

    notifyCPAMCEmbedReady('?embed=iframe');
    notifyCPAMCEmbedReady('?embed=cpamc');

    expect(messages).toEqual([{ type: 'cpa-usage-keeper:ready' }]);
  });
});
