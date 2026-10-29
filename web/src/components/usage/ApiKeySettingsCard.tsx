import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { PortalTooltip, usePortalTooltip } from '@/components/ui/PortalTooltip';
import { IconCheck, IconCopy, IconEye, IconEyeOff } from '@/components/ui/icons';
import { CredentialAliasEditor } from './credentials/CredentialAliasEditor';
import { useCredentialEnterSave } from './credentials/useCredentialEnterSave';
import { formatCredentialTimestamp } from './credentials/credentialViewModels';
import { useScrollBoundaryContainment } from '@/hooks/useScrollBoundaryContainment';
import type { CpaApiKeySettingsItem } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

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
  activeElement?: { isConnected: boolean; focus: (options?: FocusOptions) => void } | null;
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

export function getApiKeySettingsVisibleKey(item: CpaApiKeySettingsItem, revealed: boolean) {
  return item.sourceType !== 'plugin' && revealed && item.apiKey ? item.apiKey : item.displayKey;
}

export async function copyApiKeyToClipboard(apiKey: string | undefined, context: CopyContext = {}) {
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
  const previouslyFocused = documentRef.activeElement;
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
    // 兼容复制借用焦点后还给原控件，避免一次复制打断页面的键盘操作。
    if (previouslyFocused?.isConnected) previouslyFocused.focus({ preventScroll: true });
  }
}

export interface ApiKeySettingsCardProps {
  apiKeys: CpaApiKeySettingsItem[];
  loading?: boolean;
  savingId?: string | null;
  onSaveAlias: (id: string, keyAlias: string) => void | Promise<void>;
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void;
}

function ApiKeyAliasModal({ item, displayName, onSaveAlias, onClose }: {
  item: CpaApiKeySettingsItem;
  displayName: string;
  onSaveAlias: ApiKeySettingsCardProps['onSaveAlias'];
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [alias, setAlias] = useState(item.keyAlias);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const busy = useRef(false);
  const dirty = alias.trim() !== item.keyAlias.trim();
  useLayoutEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    // 与 Auth Files 弹框一致：即使直接卸载也恢复到触发铅笔按钮。
    return () => { if (opener?.isConnected) opener.focus(); };
  }, []);
  const save = async () => {
    if (busy.current || !dirty) return;
    busy.current = true;
    setSaving(true);
    setError('');
    try {
      await onSaveAlias(item.id, alias.trim());
      onClose();
    } catch {
      setError(t('usage_stats.api_key_settings_alias_save_failed'));
    } finally {
      busy.current = false;
      setSaving(false);
    }
  };
  const enterSave = useCredentialEnterSave(save);
  return (
    <Modal open title={t('usage_stats.api_key_settings_edit_alias')} width={480} closeDisabled={saving}
      onClose={() => { if (!busy.current) onClose(); }}
      footer={<>
        <Button variant="secondary" disabled={saving} onClick={onClose}>{t('common.cancel')}</Button>
        <Button loading={saving} disabled={!dirty || saving} onClick={() => void save()}>{t('common.save')}</Button>
      </>}>
      <form {...enterSave} onSubmit={(event) => { event.preventDefault(); void save(); }}>
        <p className={styles.apiKeyAliasModalName}>{displayName}</p>
        <Input label={t('usage_stats.api_key_settings_alias')} value={alias} maxLength={128} disabled={saving}
          onChange={(event) => setAlias(event.target.value)} />
        {error && <p className="error-box" role="alert">{error}</p>}
      </form>
    </Modal>
  );
}

function ApiKeyIdentityCard({ item, saving, copied, onCopy, onSaveAlias, renderText }: {
  item: CpaApiKeySettingsItem;
  saving: boolean;
  copied: boolean;
  onCopy: (item: CpaApiKeySettingsItem) => Promise<void>;
  onSaveAlias: ApiKeySettingsCardProps['onSaveAlias'];
  renderText: (value: string, className?: string) => ReactNode;
}) {
  const { t } = useTranslation();
  const [revealed, setRevealed] = useState(false);
  const [editing, setEditing] = useState(false);
  const plugin = item.sourceType === 'plugin';
  const identityName = plugin ? t('usage_stats.api_key_settings_plugin_identity', { id: item.id }) : item.displayKey;
  const name = item.keyAlias || identityName;
  const visibleKey = getApiKeySettingsVisibleKey(item, revealed);
  const copyLabel = t(copied ? 'usage_stats.api_key_settings_copied' : 'usage_stats.api_key_settings_copy');
  const toggleLabel = t(revealed ? 'usage_stats.api_key_settings_hide_key' : 'usage_stats.api_key_settings_show_key');
  return (
    <div className={styles.apiKeySettingsItem}>
      <div className={styles.apiKeySettingsHeader}>
        <span className={`${styles.sessionSettingsSource} ${plugin ? styles.apiKeySettingsPluginBadge : styles.sessionSettingsSourceStandard}`}>{t(plugin ? 'usage_stats.api_key_settings_plugin' : 'usage_stats.api_key_settings_native')}</span>
        {!plugin && <div className={styles.apiKeySettingsKeyActions}>
          <button type="button" className={styles.apiKeySettingsCopyIconButton} aria-label={toggleLabel} aria-pressed={revealed}
            onClick={() => setRevealed((current) => !current)}>
            {revealed ? <IconEye size={14} /> : <IconEyeOff size={14} />}
          </button>
          <button type="button" className={`${styles.apiKeySettingsCopyIconButton} ${copied ? styles.apiKeySettingsCopyIconButtonCopied : ''}`.trim()}
            onClick={() => void onCopy(item)} disabled={!item.apiKey} aria-label={copyLabel}>
            {copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
          </button>
        </div>}
      </div>
      <div className={styles.apiKeySettingsName}>
        <CredentialAliasEditor identityId={item.id} displayName={renderText(name)} disabled={saving}
          onEdit={() => setEditing(true)} />
      </div>
      <div className={styles.apiKeySettingsMeta}>
        <div className={styles.apiKeySettingsKeySpace}>
          {plugin ? renderText(identityName) : revealed
            ? <span className={styles.apiKeySettingsRevealedKey} tabIndex={0}>{visibleKey}</span>
            : renderText(visibleKey, styles.apiKeySettingsKey)}
        </div>
        <div className={styles.apiKeySettingsFooter}>
          {plugin ? <div className={styles.apiKeySettingsSource}>
            <span>{t('usage_stats.api_key_settings_source')}</span>
            {renderText(item.accessProvider.replace(/^plugin:/, ''))}
          </div> : item.lastSyncedAt && <span>{t('usage_stats.api_key_settings_last_synced')} <time dateTime={item.lastSyncedAt}>{(formatCredentialTimestamp(item.lastSyncedAt) ?? item.lastSyncedAt).replace(/ UTC(?:[+-]\d{2}:\d{2})?$/, '')}</time></span>}
        </div>
      </div>
      {editing && <ApiKeyAliasModal item={item} displayName={name} onSaveAlias={onSaveAlias} onClose={() => setEditing(false)} />}
    </div>
  );
}

export function ApiKeySettingsCard({ apiKeys, loading = false, savingId = null, onSaveAlias, onNotice }: ApiKeySettingsCardProps) {
  const { t } = useTranslation();
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const copyResetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const apiKeySettingsBodyRef = useRef<HTMLDivElement | null>(null);
  const tooltipId = useId();
  const tooltip = usePortalTooltip();
  useScrollBoundaryContainment(apiKeySettingsBodyRef);
  useEffect(() => () => {
    if (copyResetTimerRef.current) clearTimeout(copyResetTimerRef.current);
  }, []);
  const handleCopyApiKey = useCallback(async (item: CpaApiKeySettingsItem) => {
    if (item.sourceType === 'plugin') return;
    try {
      await copyApiKeyToClipboard(item.apiKey);
      setCopiedId(item.id);
      onNotice?.('success', t('usage_stats.api_key_settings_copy_success'));
      if (copyResetTimerRef.current) clearTimeout(copyResetTimerRef.current);
      copyResetTimerRef.current = setTimeout(() => setCopiedId(null), 1600);
    } catch {
      setCopiedId(null);
      onNotice?.('error', t('usage_stats.api_key_settings_copy_failed'));
    }
  }, [onNotice, t]);
  const renderText = (value: string, className = '') => (
    <span className={`${styles.apiKeySettingsText} ${className}`.trim()} tabIndex={0}
      aria-describedby={tooltip.tooltip?.lines[0] === value ? tooltipId : undefined}
      onMouseEnter={(event) => tooltip.showOnMouseEnter([value], event.currentTarget)}
      onMouseLeave={(event) => tooltip.hideOnMouseLeave(event.currentTarget)}
      onFocus={(event) => tooltip.showOnFocus([value], event.currentTarget)}
      onBlur={(event) => tooltip.hideOnBlur(event.currentTarget)}>
      {value}
    </span>
  );
  return (
    <Card title={t('usage_stats.api_key_settings_title')}
      className={`${styles.detailsFixedCard} ${styles.apiKeySettingsCard}`}>
      <div ref={apiKeySettingsBodyRef} className={styles.apiKeySettingsBody}>
        {loading && apiKeys.length === 0 ? (
          <div className={styles.hint}>{t('common.loading')}</div>
        ) : apiKeys.length === 0 ? (
          <div className={styles.hint}>{t('usage_stats.api_key_settings_empty')}</div>
        ) : (
          <div className={styles.apiKeySettingsList}>
            {apiKeys.map((item) => <ApiKeyIdentityCard key={item.id} item={item} saving={savingId === item.id}
              copied={copiedId === item.id} onCopy={handleCopyApiKey} onSaveAlias={onSaveAlias} renderText={renderText} />)}
          </div>
        )}
      </div>
      <PortalTooltip tooltip={tooltip.tooltip} id={tooltipId} />
    </Card>
  );
}
