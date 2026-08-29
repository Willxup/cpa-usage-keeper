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

const compositionItem = {
  key: 'item',
  label: 'Item',
  total_tokens: 10,
  requests: 1,
  percent: 100,
  input_tokens: 10,
  output_tokens: 0,
  cache_read_tokens: 0,
  cache_creation_tokens: 0,
  reasoning_tokens: 0,
  cost_usd: 0,
  cost_available: true,
};

const analysis: AnalysisResponse = {
  granularity: 'hourly',
  timezone: 'UTC',
  token_usage: [],
  api_key_composition: [compositionItem],
  model_composition: [compositionItem],
  auth_files_composition: [compositionItem],
  ai_provider_composition: [compositionItem],
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

describe('AnalysisPanel composition dimensions', () => {
  it('keeps all administrator dimensions by default', () => {
    const markup = renderToStaticMarkup(
      <AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />,
    );

    expect(markup).toContain('usage_stats.analysis_composition_api_key_tab');
    expect(markup).toContain('usage_stats.analysis_composition_model_tab');
    expect(markup).toContain('usage_stats.analysis_composition_auth_files_tab');
    expect(markup).toContain('usage_stats.analysis_composition_ai_provider_tab');
  });

  it('renders only explicitly selected viewer dimensions', () => {
    const markup = renderToStaticMarkup(
      <AnalysisPanel
        analysis={analysis}
        loading={false}
        isDark={false}
        isMobile={false}
        compositionDimensions={['api_key', 'model']}
      />,
    );

    expect(markup).toContain('usage_stats.analysis_composition_api_key_tab');
    expect(markup).toContain('usage_stats.analysis_composition_model_tab');
    expect(markup).not.toContain('usage_stats.analysis_composition_auth_files_tab');
    expect(markup).not.toContain('usage_stats.analysis_composition_ai_provider_tab');
  });
});
