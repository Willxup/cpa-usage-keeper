import React from 'react';
import '@/i18n';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { buildPricingModelOptions, PriceSettingsCard } from './PriceSettingsCard';

const countOccurrences = (text: string, value: string) => text.split(value).length - 1;

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
    expect(countOccurrences(html, 'Pricing Settings')).toBe(1);
    expect(html).not.toContain('Model Pricing Table');
  });

  it('renders Claude pricing style with cache read and write prices', () => {
    const html = renderToStaticMarkup(
      <PriceSettingsCard
        modelNames={['claude-sonnet']}
        modelPrices={{
          'claude-sonnet': {
            style: 'claude',
            prompt: 3,
            completion: 15,
            cache: 0.3,
            cacheCreation: 3.75,
          },
        }}
        onPricesChange={() => undefined}
        loading={false}
      />,
    );

    expect(html).toContain('Claude');
    expect(html).toContain('Cache Read');
    expect(html).toContain('$0.3000/1M');
    expect(html).toContain('Cache Write');
    expect(html).toContain('$3.7500/1M');
  });
});

describe('buildPricingModelOptions', () => {
  it('keeps only unpriced models selectable', () => {
    const options = buildPricingModelOptions(
      ['priced-zeta', 'unpriced-beta', 'priced-alpha', 'unpriced-alpha'],
      {
        'priced-zeta': { style: 'openai', prompt: 3, completion: 15, cache: 0.3, cacheCreation: 0 },
        'priced-alpha': { style: 'openai', prompt: 2, completion: 8, cache: 0.2, cacheCreation: 0 },
      },
      'Select model',
    );

    expect(options.map((option) => option.value)).toEqual([
      '',
      'unpriced-alpha',
      'unpriced-beta',
    ]);
    expect(options.find((option) => option.value === 'unpriced-alpha')?.suffix).toBeUndefined();
    expect(options.find((option) => option.value === 'priced-alpha')).toBeUndefined();
  });
});
