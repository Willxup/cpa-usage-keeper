// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RequestEventsDetailsCard } from '../RequestEventsDetailsCard';
import i18n from '@/i18n';
import type { UsageEvent } from '@/lib/types';

const event: UsageEvent = {
  id: '101',
  timestamp: '2026-04-23T02:00:00.000Z',
  api_key: 'Production Key',
  model: 'claude-sonnet',
  source: 'Provider A',
  source_raw: 'source-a',
  source_type: 'openai',
  auth_index: '1',
  failed: false,
  latency_ms: 120,
  ttft_ms: 45,
  speed_tps: 30,
  tokens: {
    input_tokens: 100,
    output_tokens: 60,
    reasoning_tokens: 20,
    cache_read_tokens: 20,
    cache_creation_tokens: 0,
    total_tokens: 200,
  },
  cost_usd: 0.1234,
  cost_available: true,
  pricing_style: 'claude',
};

type CardProps = React.ComponentProps<typeof RequestEventsDetailsCard>;

const defaultProps: CardProps = {
  events: [event],
  loading: false,
  page: 1,
  pageSize: 20,
  pageSizeOptions: [20, 50, 100, 500, 1000],
  totalCount: 120,
  totalPages: 6,
  modelOptions: ['claude-sonnet', 'claude-opus'],
  sourceOptions: [
    { value: 'source-a', label: 'Provider A' },
    { value: 'source-b', label: 'Provider B' },
  ],
  modelFilter: '__all__',
  sourceFilter: '__all__',
  resultFilter: '__all__',
  onPageChange: () => undefined,
  onPageSizeChange: () => undefined,
  onModelFilterChange: () => undefined,
  onSourceFilterChange: () => undefined,
  onResultFilterChange: () => undefined,
};

const flushPortal = () => new Promise((resolve) => window.setTimeout(resolve, 0));

const mountCard = async (props: Partial<CardProps> = {}) => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);

  await act(async () => {
    root.render(<RequestEventsDetailsCard {...defaultProps} {...props} />);
  });

  return {
    container,
    unmount: async () => {
      await act(async () => root.unmount());
      container.remove();
    },
  };
};

const findButtonByText = (text: string) => Array.from(document.querySelectorAll<HTMLButtonElement>('button'))
  .find((button) => button.textContent?.trim() === text);

beforeEach(async () => {
  await i18n.changeLanguage('en');
});

afterEach(() => {
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

describe('RequestEventsDetailsCard Ant Design controls', () => {
  it('changes server filters through Ant Design Select', async () => {
    const onModelFilterChange = vi.fn();
    const mounted = await mountCard({ onModelFilterChange });

    try {
      const modelInput = mounted.container.querySelector<HTMLInputElement>('input[aria-label="Model"]');
      expect(modelInput).toBeInstanceOf(HTMLElement);

      await act(async () => {
        modelInput?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
        await flushPortal();
      });

      const option = Array.from(document.body.querySelectorAll<HTMLElement>('.ant-select-item-option'))
        .find((item) => item.textContent?.trim() === 'claude-opus');
      expect(option).toBeInstanceOf(HTMLElement);

      await act(async () => option?.click());
      expect(onModelFilterChange).toHaveBeenCalledWith('claude-opus', expect.anything());
    } finally {
      await mounted.unmount();
    }
  });

  it('does not repeat a source type that only differs by letter case', async () => {
    const mounted = await mountCard({
      events: [{ ...event, source: 'kimi', source_raw: 'kimi', source_type: 'KIMI' }],
    });

    try {
      expect(mounted.container.textContent).toContain('kimi');
      expect(mounted.container.textContent).not.toContain('KIMI');
    } finally {
      await mounted.unmount();
    }
  });

  it('clears all active server filters through the existing callbacks', async () => {
    const onModelFilterChange = vi.fn();
    const onSourceFilterChange = vi.fn();
    const onResultFilterChange = vi.fn();
    const mounted = await mountCard({
      modelFilter: 'claude-sonnet',
      sourceFilter: 'source-a',
      resultFilter: 'failed',
      onModelFilterChange,
      onSourceFilterChange,
      onResultFilterChange,
    });

    try {
      const clearButton = findButtonByText('Clear Filters');
      expect(clearButton).toBeInstanceOf(HTMLButtonElement);
      expect(clearButton?.disabled).toBe(false);

      await act(async () => clearButton?.click());

      expect(onModelFilterChange).toHaveBeenCalledWith('__all__');
      expect(onSourceFilterChange).toHaveBeenCalledWith('__all__');
      expect(onResultFilterChange).toHaveBeenCalledWith('__all__');
    } finally {
      await mounted.unmount();
    }
  });

  it('dispatches the selected export format from the Ant Design dropdown', async () => {
    const onExport = vi.fn();
    const mounted = await mountCard({ onExport });

    try {
      const exportButton = findButtonByText('Export');
      expect(exportButton).toBeInstanceOf(HTMLButtonElement);

      await act(async () => {
        exportButton?.click();
        await flushPortal();
      });

      const csvItem = Array.from(document.body.querySelectorAll<HTMLElement>('[role="menuitem"]'))
        .find((item) => item.textContent?.trim() === 'Export CSV');
      expect(csvItem).toBeInstanceOf(HTMLElement);

      await act(async () => csvItem?.click());
      expect(onExport).toHaveBeenCalledWith('csv');
    } finally {
      await mounted.unmount();
    }
  });

  it('toggles visible columns through the Ant Design checkbox dropdown', async () => {
    const onVisibleColumnIdsChange = vi.fn();
    const mounted = await mountCard({
      initialVisibleColumnIds: ['timestamp'],
      onVisibleColumnIdsChange,
    });

    try {
      const columnsButton = mounted.container.querySelector<HTMLButtonElement>('button[aria-label="Columns"]');
      expect(columnsButton).toBeInstanceOf(HTMLButtonElement);

      await act(async () => {
        columnsButton?.click();
        await flushPortal();
      });

      const modelItem = Array.from(document.body.querySelectorAll<HTMLElement>('[role="menuitem"]'))
        .find((item) => item.textContent?.trim() === 'Model');
      expect(modelItem).toBeInstanceOf(HTMLElement);

      await act(async () => modelItem?.click());
      expect(onVisibleColumnIdsChange).toHaveBeenCalledWith(['timestamp', 'model']);
    } finally {
      await mounted.unmount();
    }
  });

  it('keeps server pagination controlled through Ant Design Pagination', async () => {
    const onPageChange = vi.fn();
    const mounted = await mountCard({ onPageChange });

    try {
      const secondPage = mounted.container.querySelector<HTMLElement>('.ant-pagination-item-2');
      expect(secondPage).toBeInstanceOf(HTMLElement);

      await act(async () => secondPage?.click());
      expect(onPageChange).toHaveBeenCalledWith(2);
    } finally {
      await mounted.unmount();
    }
  });

  it('keeps server page-size changes on the existing callback contract', async () => {
    const onPageSizeChange = vi.fn();
    const mounted = await mountCard({ onPageSizeChange });

    try {
      const pageSizeInput = mounted.container.querySelector<HTMLInputElement>('input[aria-label="Page Size"]');
      const pageSizeSelector = pageSizeInput?.closest<HTMLElement>('.ant-select-content');
      expect(pageSizeSelector).toBeInstanceOf(HTMLElement);

      await act(async () => {
        pageSizeSelector?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
        await flushPortal();
      });

      const option = Array.from(document.body.querySelectorAll<HTMLElement>('[role="option"]'))
        .find((item) => item.textContent?.includes('50 / page'));
      expect(option).toBeInstanceOf(HTMLElement);

      await act(async () => option?.click());
      expect(onPageSizeChange).toHaveBeenCalledWith(50);
    } finally {
      await mounted.unmount();
    }
  });
});
