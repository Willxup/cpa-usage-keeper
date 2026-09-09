// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { RequestEventsDetailsCard } from '../RequestEventsDetailsCard';

describe('RequestEventsDetailsCard model filter ordering', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    await i18n.changeLanguage('en');
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  it.each(['__all__', 'gpt-5.9', 'openai/gpt-5.9'])('sorts models and preserves the selected filter %s', async (selectedModel) => {
    const models = ['qwen-3', 'gpt-5.6-terra', 'gemini-2.5-pro', 'team/anthropic/claude-opus-4.6',
      'gpt-5.10', 'deepseek-v3', 'gpt-5.6', 'team/openai/gpt-5.6-sol', 'openai/gpt-4o'];
    const originalModels = [...models];
    const onModelFilterChange = vi.fn();
    await act(async () => root.render(
      <RequestEventsDetailsCard
        events={[]}
        loading={false}
        totalCount={0}
        modelOptions={models}
        apiKeyOptions={[]}
        sourceOptions={[]}
        modelFilter={selectedModel}
        apiKeyFilter=""
        sourceFilter="__all__"
        resultFilter="__all__"
        onModelFilterChange={onModelFilterChange}
        onApiKeyFilterChange={() => undefined}
        onSourceFilterChange={() => undefined}
        onResultFilterChange={() => undefined}
      />,
    ));

    const trigger = container.querySelector<HTMLInputElement>('input[role="combobox"][aria-label="Model"]');
    expect(trigger).not.toBeNull();
    await act(async () => trigger?.focus());
    await act(async () => trigger?.click());
    const options = Array.from(document.body.querySelectorAll<HTMLButtonElement>('[role="listbox"][aria-label="Model"] [role="option"]'));
    expect(options.map((option) => option.textContent)).toEqual([
      'All',
      'gpt-5.10',
      ...(selectedModel === '__all__' ? [] : [selectedModel]),
      'gpt-5.6',
      'team/openai/gpt-5.6-sol',
      'gpt-5.6-terra',
      'openai/gpt-4o',
      'team/anthropic/claude-opus-4.6',
      'gemini-2.5-pro',
      'deepseek-v3',
      'qwen-3',
    ]);
    expect(options.filter((option) => option.getAttribute('aria-selected') === 'true')
      .map((option) => option.textContent)).toEqual([selectedModel === '__all__' ? 'All' : selectedModel]);
    expect(models).toEqual(originalModels);

    await act(async () => options.find((option) => option.textContent === 'team/openai/gpt-5.6-sol')?.click());
    expect(onModelFilterChange).toHaveBeenCalledExactlyOnceWith('team/openai/gpt-5.6-sol');
  });
});
