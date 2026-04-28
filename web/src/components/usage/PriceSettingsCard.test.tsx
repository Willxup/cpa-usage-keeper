import React from 'react';
import { describe, expect, it } from 'vitest';
import { buildPricingModelOptions } from './PriceSettingsCard';

const configuredBadge = <span data-testid="configured" />;

describe('buildPricingModelOptions', () => {
  it('keeps unpriced models selectable before priced models and marks priced models', () => {
    const options = buildPricingModelOptions(
      ['priced-zeta', 'unpriced-beta', 'priced-alpha', 'unpriced-alpha'],
      {
        'priced-zeta': { prompt: 3, completion: 15, cache: 0.3 },
        'priced-alpha': { prompt: 2, completion: 8, cache: 0.2 },
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
});
