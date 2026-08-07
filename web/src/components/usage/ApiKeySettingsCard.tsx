import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { IconEye, IconEyeOff } from '@/components/ui/icons';
import type { CpaApiKeySettingsItem } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

interface ApiKeySettingsTitleProps {
  title: string;
  subtitle: string;
  showFullApiKeys: boolean;
  onToggleFullApiKeys: () => void;
  onGenerateApiKey?: () => void;
  generateLabel: string;
  generateDisabled: boolean;
  showFullLabel: string;
  hideFullLabel: string;
}

type ClipboardWriter = Pick<Clipboard, 'writeText'>;
type CopyTextArea = {
  value: string;
  readOnly: boolean;
  style: {
    position?: string;
    opacity?: string;
    pointerEvents?: string;
    top?: string;
    left?: string;
  };
  setAttribute: (name: string, value: string) => void;
  focus: () => void;
  select: () => void;
  remove?: () => void;
};
type CopyDocument = {
  body?: {
    appendChild: (node: CopyTextArea) => unknown;
    removeChild?: (node: CopyTextArea) => unknown;
  };
  createElement?: (tagName: 'textarea') => CopyTextArea;
  execCommand?: (command: string) => boolean;
};
type CopyContext = {
  clipboard?: ClipboardWriter;
  document?: CopyDocument;
};

export function getApiKeySettingsVisibleKey(item: CpaApiKeySettingsItem, showFullApiKeys: boolean) {
  return showFullApiKeys && item.apiKey ? item.apiKey : item.displayKey;
}

export async function copyApiKeyToClipboard(apiKey: string, context: CopyContext = {}) {
  if (!apiKey) {
    return;
  }
  const clipboard = context.clipboard ?? globalThis.navigator?.clipboard;
  if (clipboard) {
    try {
      await clipboard.writeText(apiKey);
      return;
    } catch {
      // HTTP LAN pages can block navigator.clipboard; fall back to a selected textarea copy.
    }
  }
  const documentRef = context.document ?? (typeof document !== 'undefined' ? document as unknown as CopyDocument : undefined);
  const textarea = documentRef?.createElement?.('textarea');
  if (!documentRef?.body || !documentRef.execCommand || !textarea) {
    throw new Error('clipboard is not available');
  }
  textarea.value = apiKey;
  textarea.readOnly = true;
  textarea.setAttribute('aria-hidden', 'true');
  textarea.style.position = 'fixed';
  textarea.style.opacity = '0';
  textarea.style.pointerEvents = 'none';
  textarea.style.top = '0';
  textarea.style.left = '0';
  documentRef.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  try {
    if (!documentRef.execCommand('copy')) {
      throw new Error('copy command failed');
    }
  } finally {
    if (textarea.remove) {
      textarea.remove();
    } else {
      documentRef.body.removeChild?.(textarea);
    }
  }
}

function ApiKeySettingsTitle({ title, subtitle, showFullApiKeys, onToggleFullApiKeys, onGenerateApiKey, generateLabel, generateDisabled, showFullLabel, hideFullLabel }: ApiKeySettingsTitleProps) {
  const toggleLabel = showFullApiKeys ? hideFullLabel : showFullLabel;

  return (
    <div className={styles.sectionTitleBlock}>
      <div className={styles.apiKeySettingsTitleRow}>
        <h3 className={styles.sectionTitle}>{title}</h3>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className={`${styles.apiKeyVisibilityToggle} ${showFullApiKeys ? styles.apiKeyVisibilityToggleActive : ''}`.trim()}
          onClick={onToggleFullApiKeys}
          aria-label={toggleLabel}
          aria-pressed={showFullApiKeys}
          title={toggleLabel}
        >
          {showFullApiKeys ? <IconEye size={16} /> : <IconEyeOff size={16} />}
        </Button>
        {onGenerateApiKey && (
          <Button
            type="button"
            variant="secondary"
            size="sm"
            className={`${styles.usagePillAction} ${styles.settingsCompactAction} ${styles.apiKeySettingsGenerateButton}`.trim()}
            onClick={onGenerateApiKey}
            disabled={generateDisabled}
          >
            {generateLabel}
          </Button>
        )}
      </div>
      <p className={styles.sectionSubtitle}>{subtitle}</p>
    </div>
  );
}

export interface ApiKeySettingsCardProps {
  apiKeys: CpaApiKeySettingsItem[];
  loading?: boolean;
  savingId?: string | null;
  onSaveAlias: (id: string, keyAlias: string) => void | Promise<void>;
  onGenerateApiKey?: (keyAlias: string) => void | Promise<CpaApiKeySettingsItem | undefined>;
  generating?: boolean;
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void;
}

export function ApiKeySettingsCard({ apiKeys, loading = false, savingId = null, onSaveAlias, onGenerateApiKey, generating = false, onNotice }: ApiKeySettingsCardProps) {
  const { t } = useTranslation();
  const [showFullApiKeys, setShowFullApiKeys] = useState(false);
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [newAlias, setNewAlias] = useState('');
  const [createdKey, setCreatedKey] = useState<CpaApiKeySettingsItem | null>(null);
  const [generatedCopied, setGeneratedCopied] = useState(false);
  const copyResetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const initialAliases = useMemo(
    () => Object.fromEntries(apiKeys.map((item) => [item.id, item.keyAlias])),
    [apiKeys],
  );
  const [draftAliases, setDraftAliases] = useState<Record<string, string>>(initialAliases);

  useEffect(() => {
    setDraftAliases(initialAliases);
  }, [initialAliases]);

  useEffect(() => () => {
    if (copyResetTimerRef.current) {
      clearTimeout(copyResetTimerRef.current);
    }
  }, []);

  const handleCopyApiKey = useCallback(async (item: CpaApiKeySettingsItem) => {
    try {
      await copyApiKeyToClipboard(item.apiKey);
      setCopiedId(item.id);
      onNotice?.('success', t('usage_stats.api_key_settings_copy_success'));
      if (copyResetTimerRef.current) {
        clearTimeout(copyResetTimerRef.current);
      }
      copyResetTimerRef.current = setTimeout(() => setCopiedId(null), 1600);
    } catch {
      setCopiedId(null);
      onNotice?.('error', t('usage_stats.api_key_settings_copy_failed'));
    }
  }, [onNotice, t]);

  const openCreate = useCallback(() => {
    setCreateOpen(true);
    setNewAlias('');
  }, []);

  const closeCreate = useCallback(() => {
    setCreateOpen(false);
    setNewAlias('');
  }, []);

  const handleGenerate = useCallback(async () => {
    if (!onGenerateApiKey) {
      return;
    }
    try {
      const created = await onGenerateApiKey(newAlias.trim());
      if (!created) {
        return;
      }
      setCreatedKey(created);
      setGeneratedCopied(false);
      setCreateOpen(false);
      setNewAlias('');
    } catch {
      // parent surfaces the failure via notice/error; keep the create form open for retry
    }
  }, [newAlias, onGenerateApiKey]);

  const handleCopyGenerated = useCallback(async () => {
    if (!createdKey?.apiKey) {
      return;
    }
    try {
      await copyApiKeyToClipboard(createdKey.apiKey);
      setGeneratedCopied(true);
      onNotice?.('success', t('usage_stats.api_key_settings_copy_success'));
    } catch {
      setGeneratedCopied(false);
      onNotice?.('error', t('usage_stats.api_key_settings_copy_failed'));
    }
  }, [createdKey, onNotice, t]);

  return (
    <Card
      title={
        <ApiKeySettingsTitle
          title={t('usage_stats.api_key_settings_title')}
          subtitle={t('usage_stats.api_key_settings_subtitle')}
          showFullApiKeys={showFullApiKeys}
          onToggleFullApiKeys={() => setShowFullApiKeys((current) => !current)}
          onGenerateApiKey={openCreate}
          generateLabel={t('usage_stats.api_key_settings_generate')}
          generateDisabled={generating}
          showFullLabel={t('usage_stats.api_key_settings_show_full')}
          hideFullLabel={t('usage_stats.api_key_settings_hide_full')}
        />
      }
      className={`${styles.detailsFixedCard} ${styles.apiKeySettingsCard}`}
    >
      <div className={styles.apiKeySettingsBody}>
        {createdKey && (
          <div className={styles.apiKeySettingsGeneratedBox}>
            <span className={styles.apiKeySettingsGeneratedLabel}>{t('usage_stats.api_key_settings_generate_result')}</span>
            <span className={styles.apiKeySettingsGeneratedValue} title={createdKey.apiKey}>{createdKey.apiKey}</span>
            <div className={styles.apiKeySettingsActions}>
              <Button
                variant="secondary"
                size="sm"
                className={`${styles.usagePillAction} ${styles.settingsCompactAction} ${styles.apiKeySettingsCopyButton}`.trim()}
                onClick={() => void handleCopyGenerated()}
                disabled={!createdKey.apiKey}
              >
                {generatedCopied ? t('usage_stats.api_key_settings_copied') : t('usage_stats.api_key_settings_copy')}
              </Button>
            </div>
            <p className={styles.apiKeySettingsGeneratedHint}>{t('usage_stats.api_key_settings_generate_warning')}</p>
          </div>
        )}

        {createOpen && (
          <div className={styles.apiKeySettingsCreateBox}>
            <label className={styles.apiKeyAliasField}>
              <span className={styles.apiKeyAliasLabel}>{t('usage_stats.api_key_settings_generate_alias')}</span>
              <Input
                value={newAlias}
                onChange={(event) => setNewAlias(event.target.value)}
                placeholder={t('usage_stats.api_key_settings_generate_alias_placeholder')}
                aria-label={t('usage_stats.api_key_settings_generate_alias')}
                className={`${styles.usagePillControl} ${styles.apiKeyAliasInput}`.trim()}
                disabled={generating}
              />
            </label>
            <div className={styles.apiKeySettingsActions}>
              <Button
                variant="secondary"
                size="sm"
                className={`${styles.usagePillAction} ${styles.settingsCompactAction}`.trim()}
                onClick={closeCreate}
                disabled={generating}
              >
                {t('common.cancel')}
              </Button>
              <Button
                variant="primary"
                size="sm"
                className={`${styles.usagePillAction} ${styles.settingsCompactAction} ${styles.apiKeySettingsSaveButton}`.trim()}
                onClick={() => void handleGenerate()}
                disabled={generating}
              >
                {generating ? t('usage_stats.api_key_settings_generating') : t('usage_stats.api_key_settings_generate_submit')}
              </Button>
            </div>
          </div>
        )}

        {loading && apiKeys.length === 0 ? (
          <div className={styles.hint}>{t('common.loading')}</div>
        ) : apiKeys.length === 0 ? (
          <div className={styles.hint}>{t('usage_stats.api_key_settings_empty')}</div>
        ) : (
          <div className={styles.apiKeySettingsList}>
            {apiKeys.map((item) => {
              const draftAlias = draftAliases[item.id] ?? '';
              const disabled = savingId === item.id;
              const apiKey = getApiKeySettingsVisibleKey(item, showFullApiKeys);
              const copyLabel = copiedId === item.id ? t('usage_stats.api_key_settings_copied') : t('usage_stats.api_key_settings_copy');
              return (
                <div key={item.id} className={styles.apiKeySettingsItem}>
                  <div className={styles.apiKeySettingsSummary}>
                    <span className={styles.apiKeyFieldLabel}>{t('usage_stats.api_key_settings_display_key')}</span>
                    <span className={styles.apiKeySettingsName} title={apiKey}>{apiKey}</span>
                  </div>
                  <div className={styles.apiKeySettingsForm}>
                    <label className={styles.apiKeyAliasField}>
                      <span className={styles.apiKeyAliasLabel}>{t('usage_stats.api_key_settings_alias')}</span>
                      <Input
                        value={draftAlias}
                        onChange={(event) => setDraftAliases((current) => ({ ...current, [item.id]: event.target.value }))}
                        placeholder={apiKey}
                        aria-label={`${t('usage_stats.api_key_settings_alias')} ${apiKey}`}
                        className={`${styles.usagePillControl} ${styles.apiKeyAliasInput}`.trim()}
                        disabled={disabled}
                      />
                    </label>
                    <div className={styles.apiKeySettingsActions}>
                      <Button
                        variant="secondary"
                        size="sm"
                        className={`${styles.usagePillAction} ${styles.settingsCompactAction} ${styles.apiKeySettingsCopyButton}`.trim()}
                        onClick={() => void handleCopyApiKey(item)}
                        disabled={!item.apiKey}
                      >
                        {copyLabel}
                      </Button>
                      <Button
                        variant="primary"
                        size="sm"
                        className={`${styles.usagePillAction} ${styles.settingsCompactAction} ${styles.apiKeySettingsSaveButton}`.trim()}
                        onClick={() => onSaveAlias(item.id, draftAlias)}
                        disabled={disabled}
                      >
                        {disabled ? t('usage_stats.api_key_settings_saving') : t('common.save')}
                      </Button>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </Card>
  );
}
