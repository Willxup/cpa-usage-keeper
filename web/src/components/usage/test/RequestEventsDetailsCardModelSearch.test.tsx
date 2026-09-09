// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { RequestEventsDetailsCard } from '../RequestEventsDetailsCard';

describe('RequestEventsDetailsCard model search', () => {
  let container: HTMLDivElement;
  let root: Root;
  const onModelFilterChange = vi.fn();
  const onApiKeyFilterChange = vi.fn();
  const onSourceFilterChange = vi.fn();
  const modelOptions = ['claude-sonnet-4', 'gpt-5', 'gpt-5-mini', 'gemini-2.5-pro'];
  const sortedModelOptions = ['gpt-5', 'gpt-5-mini', 'claude-sonnet-4', 'gemini-2.5-pro'];

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    await i18n.changeLanguage('en');
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    function TestCard() {
      const [modelFilter, setModelFilter] = React.useState('claude-sonnet-4');
      const [apiKeyFilter, setApiKeyFilter] = React.useState('22');
      const [sourceFilter, setSourceFilter] = React.useState('__all__');
      return <RequestEventsDetailsCard
        events={[]}
        loading={false}
        totalCount={0}
        modelOptions={modelOptions}
        apiKeyOptions={[{ id: '22', label: 'Production Key' }, { id: '33', label: 'Test Key' }]}
        sourceOptions={[{ value: 'auth-1', label: 'fallback-1', displayName: 'Team source' }, { value: 'auth-2', label: 'Other source' }]}
        modelFilter={modelFilter}
        apiKeyFilter={apiKeyFilter}
        sourceFilter={sourceFilter}
        resultFilter="__all__"
        onModelFilterChange={(model) => {
          onModelFilterChange(model);
          setModelFilter(model);
        }}
        onApiKeyFilterChange={(key) => { onApiKeyFilterChange(key); setApiKeyFilter(key); }}
        onSourceFilterChange={(source) => { onSourceFilterChange(source); setSourceFilter(source); }}
        onResultFilterChange={() => undefined}
      />;
    }
    await act(async () => root.render(<TestCard />));
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.clearAllMocks();
  });

  const input = () => container.querySelector<HTMLInputElement>('input[role="combobox"][aria-label="Model"]')!;
  const options = () => Array.from(document.querySelectorAll('[role="option"]')).map((node) => node.textContent);
  const openInput = async () => {
    expect(input()).not.toBeNull();
    await act(async () => input().focus());
    await act(async () => input().click());
    expect(input().getAttribute('aria-expanded')).toBe('true');
  };
  const typeQuery = async (query: string) => {
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input(), query);
      input().dispatchEvent(new Event('input', { bubbles: true }));
    });
  };

  it('filters names locally without changing the query until a model is selected', async () => {
    expect(input()).not.toBeNull();
    expect(input().value).toBe('claude-sonnet-4');
    expect(input().getAttribute('aria-expanded')).toBe('false');
    await openInput();
    expect(document.activeElement).toBe(input());
    expect(document.querySelector('[role="listbox"] input')).toBeNull();
    expect(document.querySelectorAll('input[role="combobox"]')).toHaveLength(3);
    await typeQuery(' GPT-5 ');
    expect(options()).toEqual(['gpt-5', 'gpt-5-mini']);
    expect(onModelFilterChange).not.toHaveBeenCalled();
    expect(input().value).toBe(' GPT-5 ');
    await act(async () => input().click());
    expect(input().value).toBe(' GPT-5 ');

    await act(async () => document.querySelectorAll<HTMLButtonElement>('[role="option"]')[1].click());
    expect(onModelFilterChange).toHaveBeenCalledExactlyOnceWith('gpt-5-mini');
    expect(input().value).toBe('gpt-5-mini');
    expect(document.querySelector('[role="listbox"]')).toBeNull();
    expect(document.activeElement).toBe(input());
  });

  it('shows an empty result, preserves the selection on Escape, and resets search when reopened', async () => {
    await openInput();
    await typeQuery('missing-model');
    expect(options()).toEqual([]);
    expect(document.body.textContent).toContain('No matching models');
    await act(async () => input().dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })));
    expect(onModelFilterChange).not.toHaveBeenCalled();
    await act(async () => input().dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(document.activeElement).toBe(input());
    expect(input().value).toBe('claude-sonnet-4');
    expect(input().getAttribute('aria-expanded')).toBe('false');
    await openInput();
    expect(input().value).toBe('');
    expect(options()).toEqual(['All', ...sortedModelOptions]);
    expect(document.querySelector('[role="option"][aria-selected="true"]')?.textContent).toBe('claude-sonnet-4');
    await act(async () => document.querySelector<HTMLButtonElement>('[role="option"]')!.click());
    expect(onModelFilterChange).toHaveBeenCalledExactlyOnceWith('__all__');
    expect(input().value).toBe('All');
  });

  it('keeps the status dropdown without a search input', async () => {
    for (const label of ['Status']) {
      const button = container.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)!;
      await act(async () => button.click());
      expect(document.querySelector('[role="listbox"]')).not.toBeNull();
      expect(document.querySelector('[role="listbox"] input')).toBeNull();
      expect(input().getAttribute('aria-expanded')).toBe('false');
      await act(async () => button.click());
    }
  });

  it.each([
    ['API Key', ' TEST ', 'Test Key', '33', onApiKeyFilterChange],
    ['Source', ' TEAM ', 'Team source', 'auth-1', onSourceFilterChange],
  ] as const)('searches %s display names locally and commits the option ID', async (label, query, displayName, value, onChange) => {
    const control = container.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!;
    await act(async () => control.click());
    const type = async (text: string) => {
      await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(control, text);
        control.dispatchEvent(new Event('input', { bubbles: true }));
      });
    };
    await type('missing-option');
    expect(options()).toEqual([]);
    expect(document.body.textContent).toContain(label === 'API Key' ? 'No matching API Keys' : 'No matching sources');
    await type(query);
    expect(options()).toEqual([displayName]);
    expect(onChange).not.toHaveBeenCalled();
    await act(async () => control.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true, bubbles: true })));
    expect(onChange).not.toHaveBeenCalled();
    await act(async () => control.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })));
    expect(onChange).toHaveBeenCalledExactlyOnceWith(value);
    expect(control.value).toBe(displayName);
    await act(async () => control.click());
    expect(control.value).toBe('');
    expect(options()).toHaveLength(3);
    await type('draft');
    await act(async () => control.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(control.value).toBe(displayName);
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it('orders the filters as Model, API Key, Source, Status', () => {
    expect(Array.from(container.querySelectorAll('[aria-expanded][aria-label]'))
      .filter((node) => ['Model', 'API Key', 'Source', 'Status'].includes(node.getAttribute('aria-label')!))
      .map((node) => node.getAttribute('aria-label'))).toEqual(['Model', 'API Key', 'Source', 'Status']);
  });

  it.each(['Model', 'API Key', 'Source', 'Status'])('does not open %s from its caption or surrounding space', async (label) => {
    const control = container.querySelector<HTMLInputElement | HTMLButtonElement>(`[aria-label="${label}"][aria-expanded]`)!;
    const caption = Array.from(container.querySelectorAll('span')).find((node) => node.textContent === label)!;

    for (const target of [caption, caption.parentElement!]) {
      await act(async () => target.click());
      expect(control.getAttribute('aria-expanded')).toBe('false');
      expect(document.querySelector('[role="listbox"]')).toBeNull();
    }

    await act(async () => control.click());
    expect(control.getAttribute('aria-expanded')).toBe('true');
    await act(async () => control.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    await act(async () => {
      control.focus();
      control.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    });
    expect(control.getAttribute('aria-expanded')).toBe('true');
    expect(onModelFilterChange).not.toHaveBeenCalled();
  });

  it('supports keyboard selection without intercepting text editing or IME confirmation', async () => {
    await openInput();
    await typeQuery('gpt');
    const keyDown = async (key: string, isComposing = false) => {
      const event = new KeyboardEvent('keydown', { key, isComposing, bubbles: true, cancelable: true });
      await act(async () => input().dispatchEvent(event));
      return event;
    };
    const highlighted = () => document.getElementById(input().getAttribute('aria-activedescendant')!)?.textContent;

    expect(highlighted()).toBe('gpt-5');
    await keyDown('ArrowDown');
    expect(highlighted()).toBe('gpt-5-mini');
    await keyDown('ArrowUp');
    expect(highlighted()).toBe('gpt-5');
    for (const key of [' ', 'Home', 'End']) {
      expect((await keyDown(key)).defaultPrevented).toBe(false);
    }
    await keyDown('Enter', true);
    expect(onModelFilterChange).not.toHaveBeenCalled();
    expect(input()).not.toBeNull();
    await keyDown('Enter');
    expect(onModelFilterChange).toHaveBeenCalledExactlyOnceWith('gpt-5');
    expect(input().getAttribute('aria-expanded')).toBe('false');
    expect(input().value).toBe('gpt-5');

    await keyDown('ArrowDown');
    expect(input().getAttribute('aria-expanded')).toBe('true');
    await typeQuery('gemini');
    expect((await keyDown('Tab')).defaultPrevented).toBe(false);
    expect(input().getAttribute('aria-expanded')).toBe('false');
    expect(input().value).toBe('gpt-5');
    expect(document.activeElement).toBe(input());
  });

  it('restores the selected model on outside click or blur without committing draft text', async () => {
    await openInput();
    await typeQuery('gemini');
    const source = container.querySelector<HTMLInputElement>('input[aria-label="Source"]')!;
    await act(async () => {
      source.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      source.focus();
    });
    expect(input().getAttribute('aria-expanded')).toBe('false');
    expect(input().value).toBe('claude-sonnet-4');
    expect(document.activeElement).toBe(source);
    expect(onModelFilterChange).not.toHaveBeenCalled();

    await openInput();
    await typeQuery('gpt');
    await typeQuery('');
    expect(options()).toEqual(['All', ...sortedModelOptions]);
    await act(async () => input().blur());
    expect(input().getAttribute('aria-expanded')).toBe('false');
    expect(input().value).toBe('claude-sonnet-4');
    expect(onModelFilterChange).not.toHaveBeenCalled();
  });
});
