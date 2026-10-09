import { describe, expect, it } from 'vitest';
import i18n, { SUPPORTED_LANGUAGES } from '../index';

describe('credential health translations', () => {
  it.each(SUPPORTED_LANGUAGES)('distinguishes degraded and unhealthy states in %s', (language) => {
    const labels = i18n.getResourceBundle(language, 'translation').usage_stats;
    expect(labels.credentials_health_status_warning).not.toBe(labels.credentials_health_status_failure);
    expect(labels.credentials_health_summary_degraded).not.toBe(labels.credentials_health_summary_unhealthy);
  });
});
