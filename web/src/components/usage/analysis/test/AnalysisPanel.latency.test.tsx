import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import type { AnalysisResponse } from '@/lib/types';

vi.mock('react-chartjs-2', () => ({
  Bar: () => React.createElement('div'),
  Doughnut: () => React.createElement('div'),
  Scatter: () => React.createElement('div'),
}));

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => {} },
  useTranslation: () => ({ t: (key: string) => key }),
}));

import { AnalysisPanel } from '../AnalysisPanel';

const analysis: AnalysisResponse = {
  granularity: 'hourly',
  timezone: 'UTC',
  token_usage: [],
  api_key_composition: [],
  model_composition: [],
  auth_files_composition: [],
  ai_provider_composition: [],
  cost_breakdown: {
    uncached_input_cost_usd: 0,
    cache_read_cost_usd: 0,
    cache_write_cost_usd: 0,
    output_cost_usd: 0,
    total_cost_usd: 0,
    cost_available: true,
  },
  model_efficiency: [],
  heatmap: { api_keys: [], api_key_labels: {}, models: [], cells: [] },
};

describe('AnalysisPanel latency loading boundary', () => {
  it('keeps core cards settled while only the latency card is loading', () => {
    const markup = renderToStaticMarkup(
      <AnalysisPanel
        analysis={analysis}
        loading={false}
        latencyDiagnostics={null}
        latencyLoading
        latencyError=""
        isDark={false}
        isMobile={false}
      />,
    );

    const latencyStart = markup.indexOf('usage_stats.analysis_latency_title');
    const compositionStart = markup.indexOf('usage_stats.analysis_composition_title', latencyStart);
    const latencyMarkup = markup.slice(latencyStart, compositionStart);
    expect(latencyStart).toBeGreaterThan(-1);
    expect(latencyMarkup).toContain('common.loading');
    expect(markup.slice(0, latencyStart)).not.toContain('common.loading');
  });

  it('shows a latency-only error without replacing the core panel', () => {
    const markup = renderToStaticMarkup(
      <AnalysisPanel
        analysis={analysis}
        loading={false}
        latencyDiagnostics={null}
        latencyLoading={false}
        latencyError="latency failed"
        isDark={false}
        isMobile={false}
      />,
    );

    expect(markup).toContain('usage_stats.analysis_token_usage_title');
    expect(markup).toContain('usage_stats.analysis_latency_title');
    expect(markup).toContain('latency failed');
  });
});
