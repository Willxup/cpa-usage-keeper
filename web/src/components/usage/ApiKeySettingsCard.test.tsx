import React from 'react';
import '@/i18n';
import { describe, expect, it, vi } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { ApiKeySettingsCard, copyApiKeyToClipboard, getApiKeySettingsVisibleKey } from './ApiKeySettingsCard';
import type { CpaApiKeySettingsItem } from '@/lib/types';

const apiKeys: CpaApiKeySettingsItem[] = [
  { id: '9007199254740993', apiKey: 'sk-alpha123456', keyAlias: 'Primary', displayKey: 'sk-*********123456', label: 'Primary', lastSyncedAt: '2026-05-13T00:00:00Z' },
  { id: '9007199254740994', apiKey: 'sk-beta654321', keyAlias: '', displayKey: 'sk-*********654321', label: 'sk-*********654321', lastSyncedAt: null },
];

const renderCard = (props: Partial<React.ComponentProps<typeof ApiKeySettingsCard>> = {}) => renderToStaticMarkup(
  <ApiKeySettingsCard
    apiKeys={apiKeys}
    loading={false}
    savingId={null}
    onSaveAlias={() => undefined}
    {...props}
  />,
);

const countOccurrences = (text: string, value: string) => text.split(value).length - 1;

describe('ApiKeySettingsCard', () => {
  it('renders alias, masked key, and string ids without local ids by default', () => {
    const html = renderCard();

    expect(html).toContain('API Key Settings');
    expect(countOccurrences(html, 'API Key Settings')).toBe(1);
    expect(html).toContain('Primary');
    expect(html).toContain('sk-*********123456');
    expect(html).toContain('sk-*********654321');
    expect(html).not.toContain('sk-alpha123456');
    expect(html).not.toContain('sk-beta654321');
    expect(html).not.toContain('placeholder="sk-alpha123456"');
    expect(html).toContain('aria-label="Show full API keys"');
    expect(html).toContain('m2 2 20 20');
    expect(countOccurrences(html, '>Copy<')).toBe(2);
    expect(html).toContain('title="sk-*********123456"');
    expect(html).not.toContain('9007199254740993');
    expect(html).not.toContain('Local ID');
    expect(html).not.toContain('sk-target-secret-value');
    expect(html).not.toContain('api_key');
  });

  it('uses the title eye toggle state to choose masked or raw keys', () => {
    expect(getApiKeySettingsVisibleKey(apiKeys[0], false)).toBe('sk-*********123456');
    expect(getApiKeySettingsVisibleKey(apiKeys[0], true)).toBe('sk-alpha123456');
  });

  it('copies the raw key value', async () => {
    const writes: string[] = [];

    await copyApiKeyToClipboard(apiKeys[0].apiKey, { clipboard: { writeText: async (value) => { writes.push(value); } } });

    expect(writes).toEqual(['sk-alpha123456']);
  });

  it('falls back to textarea copy when the Clipboard API is unavailable', async () => {
    const textarea = {
      value: '',
      readOnly: false,
      style: {},
      setAttribute: vi.fn(),
      focus: vi.fn(),
      select: vi.fn(),
      remove: vi.fn(),
    };
    const documentRef = {
      body: { appendChild: vi.fn() },
      createElement: vi.fn(() => textarea),
      execCommand: vi.fn(() => true),
    };

    await copyApiKeyToClipboard(apiKeys[0].apiKey, { document: documentRef });

    expect(textarea.value).toBe('sk-alpha123456');
    expect(documentRef.body.appendChild).toHaveBeenCalledWith(textarea);
    expect(documentRef.execCommand).toHaveBeenCalledWith('copy');
    expect(textarea.remove).toHaveBeenCalledTimes(1);
  });

  it('falls back to textarea copy when Clipboard API writes are blocked', async () => {
    const textarea = {
      value: '',
      readOnly: false,
      style: {},
      setAttribute: vi.fn(),
      focus: vi.fn(),
      select: vi.fn(),
      remove: vi.fn(),
    };
    const documentRef = {
      body: { appendChild: vi.fn() },
      createElement: vi.fn(() => textarea),
      execCommand: vi.fn(() => true),
    };
    const clipboard = { writeText: vi.fn(async () => { throw new Error('blocked'); }) };

    await copyApiKeyToClipboard(apiKeys[0].apiKey, { clipboard, document: documentRef });

    expect(clipboard.writeText).toHaveBeenCalledWith('sk-alpha123456');
    expect(documentRef.execCommand).toHaveBeenCalledWith('copy');
  });

  it('renders empty and loading states', () => {
    expect(renderCard({ apiKeys: [], loading: true })).toContain('Loading...');
    expect(renderCard({ apiKeys: [], loading: false })).toContain('No CPA API keys synced yet.');
  });
});
