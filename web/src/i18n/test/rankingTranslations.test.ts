import { describe, expect, it } from 'vitest';
import i18n from '../index';

describe('Ranking translations', () => {
  it('describes joining as a manual retry state and locks the profile on first submission', () => {
    expect(i18n.t('ranking.status_joining', { lng: 'en' })).toBe('Registration pending');
    expect(i18n.t('ranking.join_retry', { lng: 'en' })).toBe('Retry Registration');
    expect(i18n.t('ranking.join_confirm_body', { lng: 'en' })).toContain('first submission');
    expect(i18n.t('ranking.join_confirm_body', { lng: 'en' })).not.toContain('successful registration');
  });

  it('clearly states that pausing stops ranking data uploads', () => {
    expect(i18n.t('ranking.pause_confirm_body', { lng: 'zh' })).toContain('停止同步排名数据');
  });
});
