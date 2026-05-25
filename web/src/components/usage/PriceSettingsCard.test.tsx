import React from 'react';
import '@/i18n';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { buildPricingModelOptions, buildPricingProviderOptions, normalizePricingModelOptions, PriceSettingsCard } from './PriceSettingsCard';

const configuredBadge = <span data-testid="configured" />;

describe('PriceSettingsCard', () => {
  it('uses the model pricing settings title', () => {
    const html = renderToStaticMarkup(
      <PriceSettingsCard
        modelNames={[]}
        modelPrices={{}}
        onPricesChange={() => undefined}
        loading={false}
      />,
    );

    expect(html).toContain('Model Pricing Settings');
    expect(html).toContain('Pricing Settings');
    expect(html).not.toContain('Model Pricing Table');
  });
});

describe('buildPricingModelOptions', () => {
  it('keeps unpriced models selectable before priced models and marks priced models', () => {
    const options = buildPricingModelOptions(
      normalizePricingModelOptions([], [
        { value: 'provider-a/priced-zeta', source: 'provider-a', model: 'priced-zeta' },
        { value: 'provider-a/unpriced-beta', source: 'provider-a', model: 'unpriced-beta' },
        { value: 'provider-a/priced-alpha', source: 'provider-a', model: 'priced-alpha' },
        { value: 'provider-a/unpriced-alpha', source: 'provider-a', model: 'unpriced-alpha' },
        { value: 'unpriced-alpha', source: '', model: 'unpriced-alpha' },
      ]),
      'provider-a',
      {
        'provider-a/priced-zeta': { prompt: 3, completion: 15, cache: 0.3 },
        'provider-a/priced-alpha': { prompt: 2, completion: 8, cache: 0.2 },
      },
      'Select model',
      configuredBadge,
      'Configured',
    );

    expect(options.map((option) => option.value)).toEqual([
      '',
      'unpriced-alpha',
      'unpriced-beta',
      'priced-alpha',
      'priced-zeta',
    ]);
    expect(options.find((option) => option.value === 'unpriced-alpha')?.suffix).toBeUndefined();
    expect(options.find((option) => option.value === 'priced-alpha')?.suffix).toBe(configuredBadge);
    expect(options.find((option) => option.value === 'priced-alpha')?.suffixAriaLabel).toBe('Configured');
  });

  it('shows only fallback model names when provider is empty', () => {
    const options = buildPricingModelOptions(
      normalizePricingModelOptions([], [
        { value: 'claude-sonnet', source: '', model: 'claude-sonnet' },
        { value: 'provider-a/claude-sonnet', source: 'provider-a', model: 'claude-sonnet' },
        { value: 'provider-a/claude-opus', source: 'provider-a', model: 'claude-opus' },
      ]),
      '',
      {},
      'Select model',
      configuredBadge,
      'Configured',
    );

    expect(options.map((option) => option.value)).toEqual(['', 'claude-sonnet']);
  });
});

describe('buildPricingProviderOptions', () => {
  it('keeps an empty provider option for fallback pricing', () => {
    const options = buildPricingProviderOptions(
      normalizePricingModelOptions([], [
        { value: 'provider-b/claude-sonnet', source: 'provider-b', model: 'claude-sonnet' },
        { value: 'claude-sonnet', source: '', model: 'claude-sonnet' },
        { value: 'provider-a/claude-sonnet', source: 'provider-a', model: 'claude-sonnet' },
      ]),
      'Default pricing',
    );

    expect(options.map((option) => option.value)).toEqual(['', 'provider-a', 'provider-b']);
    expect(options[0].label).toBe('Default pricing');
  });
});
