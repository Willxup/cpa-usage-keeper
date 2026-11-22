import { describe, expect, it } from 'vitest';
import i18n, { SUPPORTED_LANGUAGES } from '../index';

type Messages = { [key: string]: string | Messages };

const entries = (messages: Messages, prefix = ''): [string, string][] => (
  Object.entries(messages).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return typeof value === 'string' ? [[path, value]] : entries(value, path);
  })
);
const placeholders = (message: string) => (message.match(/\{\{[^{}]+\}\}/g) ?? []).sort();
const english = Object.fromEntries(entries(i18n.getResourceBundle('en', 'translation')));

const criticalMessages = [
  {
    key: 'usage_stats.credentials_quota_reset_message_prompt',
    en: 'Consume 1 credit to reset now?',
    zh: '是否消耗 1 次立即重置？',
    'zh-TW': '是否消耗 1 次立即重置？',
  },
  {
    key: 'usage_stats.credentials_quota_reset_recovery_failed',
    en: 'Quota was reset, but CPA account recovery failed. Recover the account in CPA; do not reset quota again.',
    zh: '限额已重置，但 CPA 账号恢复失败。请在 CPA 中恢复账号，勿重复重置限额。',
    'zh-TW': '限額已重置，但 CPA 帳號恢復失敗。請在 CPA 中恢復帳號，勿重複重置限額。',
  },
  {
    key: 'auth.session_expired',
    en: 'Your session expired. Please sign in again.',
    zh: '登录状态已失效，请重新登录。',
    'zh-TW': '登入狀態已失效，請重新登入。',
  },
] as const;

describe('i18n resources', () => {
  it.each(SUPPORTED_LANGUAGES)('provides nonempty messages and matching interpolation parameters in %s', (language) => {
    const translated = Object.fromEntries(entries(i18n.getResourceBundle(language, 'translation')));
    expect(Object.keys(translated).sort()).toEqual(Object.keys(english).sort());
    for (const [key, message] of Object.entries(translated)) {
      expect(message.trim(), `${language}:${key}`).not.toBe('');
      expect(placeholders(message), `${language}:${key}`).toEqual(placeholders(english[key]));
    }
  });

  it.each(criticalMessages)('preserves the safety meaning of $key', ({ key, ...expected }) => {
    expect(Object.fromEntries(SUPPORTED_LANGUAGES.map((language) => [
      language,
      i18n.getResource(language, 'translation', key),
    ]))).toEqual(expected);
  });
});
