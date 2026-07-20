import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Card, Input, Space, Table, Tooltip, Typography, type TableColumnsType } from 'antd';
import { SectionHeader } from '@/components/layout';
import { IconEye, IconEyeOff } from '@/components/ui/icons';
import type { CpaApiKeySettingsItem } from '@/lib/types';
import styles from './UsageSettings.module.scss';

interface ApiKeySettingsTitleProps {
  title: string;
  subtitle: string;
  showFullApiKeys: boolean;
  onToggleFullApiKeys: () => void;
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

function getApiKeySettingsRowKey(item: CpaApiKeySettingsItem): string {
  let hash = 2166136261;
  for (let index = 0; index < item.id.length; index += 1) {
    hash ^= item.id.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return `api-key-${(hash >>> 0).toString(36)}`;
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

function ApiKeySettingsTitle({ title, subtitle, showFullApiKeys, onToggleFullApiKeys, showFullLabel, hideFullLabel }: ApiKeySettingsTitleProps) {
  const toggleLabel = showFullApiKeys ? hideFullLabel : showFullLabel;

  return (
    <SectionHeader
      headingLevel={2}
      title={title}
      description={subtitle}
      meta={(
        <Tooltip title={toggleLabel}>
          <Button
            type="text"
            size="small"
            className={styles.apiKeyVisibilityToggle}
            onClick={onToggleFullApiKeys}
            aria-label={toggleLabel}
            aria-pressed={showFullApiKeys}
          >
            {showFullApiKeys ? <IconEye size={16} /> : <IconEyeOff size={16} />}
          </Button>
        </Tooltip>
      )}
    />
  );
}

export interface ApiKeySettingsCardProps {
  apiKeys: CpaApiKeySettingsItem[];
  loading?: boolean;
  savingId?: string | null;
  onSaveAlias: (id: string, keyAlias: string) => void | Promise<void>;
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void;
}

export function ApiKeySettingsCard({ apiKeys, loading = false, savingId = null, onSaveAlias, onNotice }: ApiKeySettingsCardProps) {
  const { t } = useTranslation();
  const [showFullApiKeys, setShowFullApiKeys] = useState(false);
  const [copiedId, setCopiedId] = useState<string | null>(null);
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

  const columns = useMemo<TableColumnsType<CpaApiKeySettingsItem>>(() => [
    {
      key: 'apiKey',
      title: t('usage_stats.api_key_settings_display_key'),
      width: 300,
      render: (_value, item) => {
        const visibleKey = getApiKeySettingsVisibleKey(item, showFullApiKeys);
        return (
          <Typography.Text
            className={styles.apiKeySettingsKey}
            code
            ellipsis={{ tooltip: visibleKey }}
            title={visibleKey}
          >
            {visibleKey}
          </Typography.Text>
        );
      },
    },
    {
      key: 'alias',
      title: t('usage_stats.api_key_settings_alias'),
      render: (_value, item) => {
        const visibleKey = getApiKeySettingsVisibleKey(item, showFullApiKeys);
        const disabled = savingId === item.id;
        return (
          <Input
            value={draftAliases[item.id] ?? ''}
            onChange={(event) => setDraftAliases((current) => ({
              ...current,
              [item.id]: event.target.value,
            }))}
            placeholder={visibleKey}
            aria-label={`${t('usage_stats.api_key_settings_alias')} ${visibleKey}`}
            disabled={disabled}
          />
        );
      },
    },
    {
      key: 'actions',
      title: t('usage_stats.api_key_settings_actions_column'),
      width: 180,
      align: 'right',
      render: (_value, item) => {
        const disabled = savingId === item.id;
        const copyLabel = copiedId === item.id
          ? t('usage_stats.api_key_settings_copied')
          : t('usage_stats.api_key_settings_copy');
        return (
          <Space size={8}>
            <Button
              size="small"
              onClick={() => void handleCopyApiKey(item)}
              disabled={!item.apiKey}
            >
              {copyLabel}
            </Button>
            <Button
              type="primary"
              size="small"
              loading={disabled}
              onClick={() => onSaveAlias(item.id, draftAliases[item.id] ?? '')}
            >
              {disabled ? t('usage_stats.api_key_settings_saving') : t('common.save')}
            </Button>
          </Space>
        );
      },
    },
  ], [copiedId, draftAliases, handleCopyApiKey, onSaveAlias, savingId, showFullApiKeys, t]);

  return (
    <Card
      variant="outlined"
      title={
        <ApiKeySettingsTitle
          title={t('usage_stats.api_key_settings_title')}
          subtitle={t('usage_stats.api_key_settings_subtitle')}
          showFullApiKeys={showFullApiKeys}
          onToggleFullApiKeys={() => setShowFullApiKeys((current) => !current)}
          showFullLabel={t('usage_stats.api_key_settings_show_full')}
          hideFullLabel={t('usage_stats.api_key_settings_hide_full')}
        />
      }
      className={styles.apiKeySettingsCard}
    >
      <Table<CpaApiKeySettingsItem>
        className={styles.apiKeySettingsTable}
        columns={columns}
        dataSource={apiKeys}
        rowKey={getApiKeySettingsRowKey}
        pagination={false}
        size="small"
        scroll={{ x: 760 }}
        locale={{
          emptyText: loading
            ? t('common.loading')
            : t('usage_stats.api_key_settings_empty'),
        }}
      />
    </Card>
  );
}
