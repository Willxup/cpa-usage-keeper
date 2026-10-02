import React, { type ComponentProps } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { AnalysisPanel } from '../AnalysisPanel';
import type { AnalysisResponse } from '@/lib/types';

export const emptyAnalysis: AnalysisResponse = {
  granularity: 'hourly',
  timezone: 'UTC',
  token_usage: [],
  api_key_composition: [],
  model_composition: [],
  auth_files_composition: [],
  ai_provider_composition: [],
  cost_summary: {
    total_cost_usd: 0,
    cost_available: true,
  },
  model_efficiency: [],
  heatmap: {
    api_keys: [],
    api_key_labels: {},
    models: [],
    cells: [],
  },
};

export function AnalysisTestPanel(props: Partial<ComponentProps<typeof AnalysisPanel>>) {
  return <AnalysisPanel analysis={emptyAnalysis} loading={false} isDark={false} isMobile={false} {...props} />;
}

export function renderAnalysisPanel(props: Partial<ComponentProps<typeof AnalysisPanel>> = {}) {
  const container = document.createElement('div');
  container.innerHTML = renderToStaticMarkup(<AnalysisTestPanel {...props} />);
  return container;
}
