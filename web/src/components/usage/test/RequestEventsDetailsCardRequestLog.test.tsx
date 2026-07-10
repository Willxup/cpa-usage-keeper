// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { UsageEvent } from '@/lib/types';
import {
  RequestEventsDetailsCard,
  splitRequestLogVirtualChunks,
} from '../RequestEventsDetailsCard';

const baseProps: React.ComponentProps<typeof RequestEventsDetailsCard> = {
  events: [],
  loading: false,
  page: 1,
  pageSize: 20,
  pageSizeOptions: [20, 50, 100],
  totalCount: 0,
  totalPages: 0,
  modelOptions: [],
  sourceOptions: [],
  modelFilter: '__all__',
  sourceFilter: '__all__',
  resultFilter: '__all__',
  onPageChange: () => undefined,
  onPageSizeChange: () => undefined,
  onModelFilterChange: () => undefined,
  onSourceFilterChange: () => undefined,
  onResultFilterChange: () => undefined,
};

const requestLogEvent: UsageEvent = {
  id: '101',
  timestamp: '2026-04-23T02:00:00.000Z',
  api_key: 'Production Key',
  model: 'claude-sonnet',
  endpoint: 'POST /v1/messages',
  source: 'Provider A',
  source_raw: 'source-a',
  source_type: 'openai',
  auth_index: '1',
  request_id: 'req-log-101',
  failed: false,
  tokens: {
    input_tokens: 100,
    output_tokens: 60,
    reasoning_tokens: 20,
    cached_tokens: 20,
    cache_read_tokens: 20,
    cache_creation_tokens: 0,
    total_tokens: 200,
  },
  cost_usd: 0.1234,
  cost_available: true,
  pricing_style: 'claude',
};

const rect = (width: number, height: number): DOMRect => ({
  x: 0,
  y: 0,
  top: 0,
  right: width,
  bottom: height,
  left: 0,
  width,
  height,
  toJSON: () => ({}),
});

class TestResizeObserver implements ResizeObserver {
  private readonly callback: ResizeObserverCallback;

  constructor(callback: ResizeObserverCallback) {
    this.callback = callback;
  }

  observe(target: Element) {
    const contentRect = target.getBoundingClientRect();
    this.callback([{
      target,
      contentRect,
      borderBoxSize: [{ inlineSize: contentRect.width, blockSize: contentRect.height }],
      contentBoxSize: [{ inlineSize: contentRect.width, blockSize: contentRect.height }],
      devicePixelContentBoxSize: [],
    } as unknown as ResizeObserverEntry], this);
  }

  disconnect() {}

  unobserve() {}
}

describe('RequestEventsDetailsCard request log virtualization', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    vi.stubGlobal('ResizeObserver', TestResizeObserver);
    vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined);
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function getBoundingClientRect() {
      if (this.className.includes('requestEventsLogSectionPanelInner')) {
        return rect(800, 360);
      }
      if (this instanceof HTMLPreElement) {
        return rect(800, 18);
      }
      return rect(800, 600);
    });
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    document.body.innerHTML = '';
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('splits oversized logical lines into bounded Unicode-safe chunks', () => {
    const content = `${'a'.repeat(5000)}${'😀'.repeat(2000)}`;
    const chunks = splitRequestLogVirtualChunks(content);

    expect(chunks.length).toBeGreaterThan(1);
    expect(chunks.every((chunk) => chunk.length <= 2048)).toBe(true);
    expect(chunks.join('')).toBe(content);
    for (const chunk of chunks.slice(0, -1)) {
      const lastCodeUnit = chunk.charCodeAt(chunk.length - 1);
      expect(lastCodeUnit < 0xD800 || lastCodeUnit > 0xDBFF).toBe(true);
    }
  });

  it('prefers semantic boundaries and preserves complete grapheme clusters', () => {
    const semanticContent = `${'a'.repeat(1999)},${'b'.repeat(3000)}`;
    const semanticChunks = splitRequestLogVirtualChunks(semanticContent);
    expect(semanticChunks[0].endsWith(',')).toBe(true);
    expect(semanticChunks.join('')).toBe(semanticContent);

    const segmenter = new Intl.Segmenter(undefined, { granularity: 'grapheme' });
    const assertGraphemeSafeChunks = (content: string) => {
      const boundaries = new Set<number>([0, content.length]);
      for (const segment of segmenter.segment(content)) {
        boundaries.add(segment.index);
      }
      let boundary = 0;
      for (const chunk of splitRequestLogVirtualChunks(content).slice(0, -1)) {
        boundary += chunk.length;
        expect(boundaries.has(boundary)).toBe(true);
      }
    };

    assertGraphemeSafeChunks(`${'a'.repeat(2046)}🇺🇸${'b'.repeat(3000)}`);
    assertGraphemeSafeChunks(`${'a'.repeat(2047)}e\u0301${'b'.repeat(3000)}`);
  });

  it('builds collapsed sections lazily and keeps opened content mounted while closing', async () => {
    await act(async () => {
      root.render(
        <RequestEventsDetailsCard
          {...baseProps}
          requestLogResponse={{
            event_id: '101',
            available: true,
            sections: [
              { title: 'REQUEST INFO', content: 'first section' },
              { title: 'API RESPONSE ERROR', content: 'lazy second section' },
            ],
          }}
          onRequestLogClose={() => undefined}
        />,
      );
      await Promise.resolve();
    });

    expect(document.body.textContent).not.toContain('lazy second section');
    const trigger = Array.from(document.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('API Response Error'));
    expect(trigger).toBeDefined();

    await act(async () => trigger?.click());
    expect(document.body.textContent).toContain('lazy second section');

    await act(async () => trigger?.click());
    expect(trigger?.getAttribute('aria-expanded')).toBe('false');
    expect(document.body.textContent).toContain('lazy second section');
  });

  it('copies the complete section content without expanding a collapsed section', async () => {
    const writeText = vi.fn(async () => undefined);
    vi.stubGlobal('navigator', { clipboard: { writeText } });

    await act(async () => {
      root.render(
        <RequestEventsDetailsCard
          {...baseProps}
          requestLogResponse={{
            event_id: '104',
            available: true,
            sections: [
              { title: 'REQUEST INFO', content: 'first section' },
              { title: 'API RESPONSE', content: 'complete response content' },
            ],
          }}
          onRequestLogClose={() => undefined}
        />,
      );
      await Promise.resolve();
    });

    const copyButtons = Array.from(document.querySelectorAll<HTMLButtonElement>('button[aria-label^="Copy "]'));
    expect(copyButtons).toHaveLength(2);

    const responseTrigger = Array.from(document.querySelectorAll<HTMLButtonElement>('button[aria-expanded]')).find((button) =>
      button.textContent?.includes('API Response'));
    const responseCopyButton = copyButtons.find((button) => button.getAttribute('aria-label') === 'Copy API Response');
    expect(responseTrigger?.getAttribute('aria-expanded')).toBe('false');
    expect(responseCopyButton).toBeDefined();

    await act(async () => responseCopyButton?.click());

    expect(writeText).toHaveBeenCalledWith('complete response content');
    expect(responseTrigger?.getAttribute('aria-expanded')).toBe('false');
    expect(responseCopyButton?.getAttribute('aria-label')).toBe('API Response copied');
  });

  it('falls back to a selected textarea when the Clipboard API is blocked', async () => {
    const writeText = vi.fn(async () => {
      throw new Error('clipboard blocked');
    });
    const execCommand = vi.fn(() => true);
    const originalExecCommand = Object.getOwnPropertyDescriptor(document, 'execCommand');
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    Object.defineProperty(document, 'execCommand', {
      configurable: true,
      value: execCommand,
    });

    try {
      await act(async () => {
        root.render(
          <RequestEventsDetailsCard
            {...baseProps}
            requestLogResponse={{
              event_id: '105',
              available: true,
              sections: [{ title: 'HEADERS', content: 'Authorization: Bearer preview' }],
            }}
            onRequestLogClose={() => undefined}
          />,
        );
        await Promise.resolve();
      });

      const copyButton = document.querySelector<HTMLButtonElement>('button[aria-label="Copy Headers"]');
      expect(copyButton).not.toBeNull();
      await act(async () => copyButton?.click());

      expect(writeText).toHaveBeenCalledWith('Authorization: Bearer preview');
      expect(execCommand).toHaveBeenCalledWith('copy');
      expect(copyButton?.getAttribute('aria-label')).toBe('Headers copied');
      expect(document.querySelector('textarea[aria-hidden="true"]')).toBeNull();
    } finally {
      if (originalExecCommand) {
        Object.defineProperty(document, 'execCommand', originalExecCommand);
      } else {
        delete (document as Document & { execCommand?: unknown }).execCommand;
      }
    }
  });

  it('keeps the Result badge static when request log access is disabled', async () => {
    await act(async () => {
      root.render(
        <RequestEventsDetailsCard
          {...baseProps}
          events={[requestLogEvent]}
          totalCount={1}
          totalPages={1}
          requestLogAccessEnabled={false}
          onRequestLogOpen={() => undefined}
        />,
      );
      await Promise.resolve();
    });

    expect(document.body.textContent).toContain('Success');
    expect(document.querySelector('[title="Click to view request log"]')).toBeNull();
    expect(document.querySelector('[class*="requestEventsResultLogButton"]')).toBeNull();
    expect(document.querySelector('[class*="requestEventsResultLogIcon"]')).toBeNull();
  });

  it('renders a bounded window and switches items after scrolling', async () => {
    const content = Array.from({ length: 500 }, (_, index) => `line-${index}`).join('\n');
    await act(async () => {
      root.render(
        <RequestEventsDetailsCard
          {...baseProps}
          requestLogResponse={{
            event_id: '102',
            available: true,
            sections: [{ title: 'REQUEST INFO', content }],
          }}
          onRequestLogClose={() => undefined}
        />,
      );
      await Promise.resolve();
    });

    const scroller = document.querySelector<HTMLElement>('[class*="requestEventsLogSectionPanelInner"]');
    expect(scroller).not.toBeNull();
    const initialIndexes = Array.from(document.querySelectorAll<HTMLElement>('pre[data-index]'), (item) => Number(item.dataset.index));
    expect(initialIndexes.length).toBeGreaterThan(0);
    expect(initialIndexes.length).toBeLessThan(500);

    if (scroller) {
      scroller.scrollTop = 5000;
      await act(async () => {
        scroller.dispatchEvent(new Event('scroll'));
        await new Promise((resolve) => window.setTimeout(resolve, 0));
      });
    }

    const scrolledIndexes = Array.from(document.querySelectorAll<HTMLElement>('pre[data-index]'), (item) => Number(item.dataset.index));
    expect(Math.max(...scrolledIndexes)).toBeGreaterThan(Math.max(...initialIndexes));
  });

  it('keeps a multi-megabyte single-line log bounded in the DOM', async () => {
    const content = `{"payload":"${'x'.repeat(5 * 1024 * 1024 + 512 * 1024)}"}`;
    await act(async () => {
      root.render(
        <RequestEventsDetailsCard
          {...baseProps}
          requestLogResponse={{
            event_id: '103',
            available: true,
            sections: [{ title: 'API RESPONSE', content }],
          }}
          onRequestLogClose={() => undefined}
        />,
      );
      await Promise.resolve();
    });

    const renderedChunks = Array.from(document.querySelectorAll<HTMLPreElement>('pre[data-index]'));
    expect(renderedChunks.length).toBeGreaterThan(0);
    expect(renderedChunks.length).toBeLessThan(100);
    expect(renderedChunks.reduce((total, item) => total + (item.textContent?.length ?? 0), 0)).toBeLessThan(256 * 1024);
  });
});
