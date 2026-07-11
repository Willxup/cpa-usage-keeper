import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
} from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { IconCheck, IconChevronDown, IconEye, IconEyeOff } from '@/components/ui/icons';
import type { CpaApiKeySettingsItem, UsageIdentity } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

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

export function normalizeApiKeyAuthFileScopeNames(names: readonly string[]): string[] {
  const seen = new Set<string>();
  return names.reduce<string[]>((normalizedNames, rawName) => {
    const name = rawName.trim();
    if (!name || seen.has(name)) return normalizedNames;
    seen.add(name);
    normalizedNames.push(name);
    return normalizedNames;
  }, []);
}

type AuthFileScopeIdentity = Pick<UsageIdentity, 'auth_type' | 'file_name' | 'is_deleted'>;

// 设置范围只允许选择当前仍有效的 OAuth 认证文件；禁用文件仍可配置，符合后端的有效身份定义。
export function getActiveAuthFileScopeNames(identities: readonly AuthFileScopeIdentity[]): string[] {
  return normalizeApiKeyAuthFileScopeNames(
    identities
      .filter((identity) => identity.auth_type === 1 && !identity.is_deleted)
      .map((identity) => identity.file_name ?? ''),
  );
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
      </div>
      <p className={styles.sectionSubtitle}>{subtitle}</p>
    </div>
  );
}

const AUTH_FILE_SCOPE_DROPDOWN_MARGIN = 8;
const AUTH_FILE_SCOPE_DROPDOWN_OFFSET = 6;
const AUTH_FILE_SCOPE_DROPDOWN_MAX_HEIGHT = 260;
const AUTH_FILE_SCOPE_DROPDOWN_MIN_WIDTH = 260;
const AUTH_FILE_SCOPE_DROPDOWN_Z_INDEX = 2010;

const clampAuthFileScopeDropdownPosition = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);

const resolveAuthFileScopeDropdownStyle = (element: HTMLElement): CSSProperties => {
  const rect = element.getBoundingClientRect();
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;
  const availableWidth = Math.max(0, viewportWidth - AUTH_FILE_SCOPE_DROPDOWN_MARGIN * 2);
  const width = Math.min(Math.max(rect.width, AUTH_FILE_SCOPE_DROPDOWN_MIN_WIDTH), availableWidth);
  const left = clampAuthFileScopeDropdownPosition(
    rect.left - (width - rect.width) / 2,
    AUTH_FILE_SCOPE_DROPDOWN_MARGIN,
    Math.max(AUTH_FILE_SCOPE_DROPDOWN_MARGIN, viewportWidth - width - AUTH_FILE_SCOPE_DROPDOWN_MARGIN),
  );
  const spaceBelow = viewportHeight - rect.bottom - AUTH_FILE_SCOPE_DROPDOWN_MARGIN - AUTH_FILE_SCOPE_DROPDOWN_OFFSET;
  const spaceAbove = rect.top - AUTH_FILE_SCOPE_DROPDOWN_MARGIN - AUTH_FILE_SCOPE_DROPDOWN_OFFSET;
  const direction = spaceBelow >= AUTH_FILE_SCOPE_DROPDOWN_MAX_HEIGHT || spaceBelow >= spaceAbove ? 'down' : 'up';
  const maxHeight = Math.max(
    0,
    Math.min(AUTH_FILE_SCOPE_DROPDOWN_MAX_HEIGHT, direction === 'down' ? spaceBelow : spaceAbove),
  );

  return direction === 'down'
    ? {
        position: 'fixed',
        top: rect.bottom + AUTH_FILE_SCOPE_DROPDOWN_OFFSET,
        left,
        width,
        maxHeight,
        zIndex: AUTH_FILE_SCOPE_DROPDOWN_Z_INDEX,
      }
    : {
        position: 'fixed',
        bottom: viewportHeight - rect.top + AUTH_FILE_SCOPE_DROPDOWN_OFFSET,
        left,
        width,
        maxHeight,
        zIndex: AUTH_FILE_SCOPE_DROPDOWN_Z_INDEX,
      };
};

interface AuthFileScopeSelectorProps {
  availableNames: readonly string[];
  value: readonly string[];
  onChange: (names: string[]) => void;
  disabled: boolean;
  ariaLabel: string;
}

function AuthFileScopeSelector({ availableNames, value, onChange, disabled, ariaLabel }: AuthFileScopeSelectorProps) {
  const { t } = useTranslation();
  const selectId = useId();
  const menuId = `${selectId}-menu`;
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const dropdownRef = useRef<HTMLDivElement | null>(null);
  const rafRef = useRef<number | null>(null);
  const [dropdownStyle, setDropdownStyle] = useState<CSSProperties | null>(null);
  const normalizedAvailableNames = useMemo(() => normalizeApiKeyAuthFileScopeNames(availableNames), [availableNames]);
  const selectedNames = useMemo(() => normalizeApiKeyAuthFileScopeNames(value), [value]);
  const selectedNameSet = useMemo(() => new Set(selectedNames), [selectedNames]);
  const availableNameSet = useMemo(() => new Set(normalizedAvailableNames), [normalizedAvailableNames]);
  const optionNames = useMemo(
    () => [
      ...normalizedAvailableNames,
      ...selectedNames.filter((name) => !availableNameSet.has(name)),
    ],
    [availableNameSet, normalizedAvailableNames, selectedNames],
  );
  const isOpen = open && !disabled;
  const summary = selectedNames.length > 0
    ? t('usage_stats.api_key_auth_file_scope_selected', { count: selectedNames.length })
    : t('usage_stats.api_key_auth_file_scope_placeholder');

  useEffect(() => {
    if (!isOpen) return;
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node;
      if (wrapRef.current?.contains(target) || dropdownRef.current?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen || !dropdownStyle) return;
    dropdownRef.current?.querySelector<HTMLButtonElement>('button')?.focus();
  }, [dropdownStyle, isOpen]);

  const updateDropdownStyle = useCallback(() => {
    if (!wrapRef.current) return;
    setDropdownStyle(resolveAuthFileScopeDropdownStyle(wrapRef.current));
  }, []);

  const scheduleDropdownStyleUpdate = useCallback(() => {
    if (typeof window === 'undefined') return;
    if (rafRef.current !== null) {
      window.cancelAnimationFrame(rafRef.current);
    }
    rafRef.current = window.requestAnimationFrame(() => {
      rafRef.current = null;
      updateDropdownStyle();
    });
  }, [updateDropdownStyle]);

  useLayoutEffect(() => {
    if (!isOpen) {
      if (rafRef.current !== null && typeof window !== 'undefined') {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
      return;
    }

    updateDropdownStyle();
    window.addEventListener('resize', scheduleDropdownStyleUpdate);
    window.addEventListener('scroll', scheduleDropdownStyleUpdate, true);

    return () => {
      window.removeEventListener('resize', scheduleDropdownStyleUpdate);
      window.removeEventListener('scroll', scheduleDropdownStyleUpdate, true);
      if (rafRef.current !== null) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
  }, [isOpen, scheduleDropdownStyleUpdate, updateDropdownStyle]);

  const toggleName = useCallback((name: string) => {
    onChange(
      selectedNameSet.has(name)
        ? selectedNames.filter((selectedName) => selectedName !== name)
        : [...selectedNames, name],
    );
  }, [onChange, selectedNameSet, selectedNames]);

  const handleTriggerKeyDown = useCallback((event: KeyboardEvent<HTMLButtonElement>) => {
    if (disabled) return;
    if (event.key === 'Escape' && isOpen) {
      event.preventDefault();
      setOpen(false);
      return;
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp' || event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      setOpen(true);
    }
  }, [disabled, isOpen]);

  const handleDropdownKeyDown = useCallback((event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      setOpen(false);
      triggerRef.current?.focus();
      return;
    }
    if (event.key === 'Tab') {
      setOpen(false);
      return;
    }
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp' && event.key !== 'Home' && event.key !== 'End') {
      return;
    }

    const optionButtons = Array.from(dropdownRef.current?.querySelectorAll<HTMLButtonElement>('button') ?? []);
    if (optionButtons.length === 0) return;
    const currentIndex = optionButtons.findIndex((button) => button === document.activeElement);
    const nextIndex = event.key === 'Home'
      ? 0
      : event.key === 'End'
        ? optionButtons.length - 1
        : event.key === 'ArrowDown'
          ? (currentIndex + 1 + optionButtons.length) % optionButtons.length
          : (currentIndex - 1 + optionButtons.length) % optionButtons.length;
    event.preventDefault();
    optionButtons[nextIndex]?.focus();
  }, []);

  const dropdown = isOpen && dropdownStyle
    ? (
        <div
          ref={dropdownRef}
          className={styles.apiKeyAuthFileScopeDropdown}
          id={menuId}
          role="menu"
          aria-label={ariaLabel}
          style={dropdownStyle}
          onKeyDown={handleDropdownKeyDown}
        >
          {optionNames.length === 0 ? (
            <div className={styles.apiKeyAuthFileScopeEmpty}>{t('usage_stats.api_key_auth_file_scope_empty')}</div>
          ) : optionNames.map((name) => {
            const selected = selectedNameSet.has(name);
            const unavailable = !availableNameSet.has(name);
            return (
              <button
                key={name}
                type="button"
                role="menuitemcheckbox"
                aria-checked={selected}
                className={`${styles.apiKeyAuthFileScopeOption} ${selected ? styles.apiKeyAuthFileScopeOptionSelected : ''}`.trim()}
                onClick={() => toggleName(name)}
              >
                <span className={styles.apiKeyAuthFileScopeOptionLabel}>{name}</span>
                {unavailable && <span className={styles.apiKeyAuthFileScopeOptionUnavailable}>{t('usage_stats.api_key_auth_file_scope_unavailable')}</span>}
                {selected ? (
                  <span className={styles.apiKeyAuthFileScopeCheck} aria-hidden="true">
                    <IconCheck size={12} />
                  </span>
                ) : (
                  <span className={styles.apiKeyAuthFileScopeCheckPlaceholder} aria-hidden="true" />
                )}
              </button>
            );
          })}
        </div>
      )
    : null;

  return (
    <>
      <div className={styles.apiKeyAuthFileScopePicker} ref={wrapRef}>
        <button
          ref={triggerRef}
          type="button"
          className={styles.apiKeyAuthFileScopeTrigger}
          aria-haspopup="menu"
          aria-expanded={isOpen}
          aria-controls={isOpen ? menuId : undefined}
          aria-label={ariaLabel}
          title={selectedNames.length > 0 ? selectedNames.join(', ') : undefined}
          disabled={disabled}
          onClick={() => setOpen((currentOpen) => !currentOpen)}
          onKeyDown={handleTriggerKeyDown}
        >
          <span className={`${styles.apiKeyAuthFileScopeTriggerText} ${selectedNames.length === 0 ? styles.apiKeyAuthFileScopeTriggerPlaceholder : ''}`.trim()}>{summary}</span>
          <span className={styles.apiKeyAuthFileScopeTriggerIcon} aria-hidden="true">
            <IconChevronDown size={14} />
          </span>
        </button>
      </div>
      {dropdown && (typeof document === 'undefined' ? dropdown : createPortal(dropdown, document.body))}
    </>
  );
}

export interface ApiKeySettingsCardProps {
  apiKeys: CpaApiKeySettingsItem[];
  loading?: boolean;
  savingId?: string | null;
  onSaveAlias: (id: string, keyAlias: string) => void | Promise<void>;
  authFileScopes?: Record<string, string[]>;
  authFileScopesLoading?: boolean;
  authFileScopeOptions?: string[];
  authFileScopeOptionsLoading?: boolean;
  authFileScopeSavingId?: string | null;
  onSaveAuthFileScopes?: (id: string, authFileNames: string[]) => void | Promise<void>;
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void;
}

export function ApiKeySettingsCard({ apiKeys, loading = false, savingId = null, onSaveAlias, authFileScopes = {}, authFileScopesLoading = false, authFileScopeOptions = [], authFileScopeOptionsLoading = false, authFileScopeSavingId = null, onSaveAuthFileScopes, onNotice }: ApiKeySettingsCardProps) {
  const { t } = useTranslation();
  const [showFullApiKeys, setShowFullApiKeys] = useState(false);
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const copyResetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const initialAliases = useMemo(
    () => Object.fromEntries(apiKeys.map((item) => [item.id, item.keyAlias])),
    [apiKeys],
  );
  const [draftAliases, setDraftAliases] = useState<Record<string, string>>(initialAliases);
  const initialAuthFileScopes = useMemo(
    () => Object.fromEntries(apiKeys.map((item) => [item.id, normalizeApiKeyAuthFileScopeNames(authFileScopes[item.id] ?? [])])),
    [apiKeys, authFileScopes],
  );
  const [draftAuthFileScopes, setDraftAuthFileScopes] = useState<Record<string, string[]>>(initialAuthFileScopes);

  useEffect(() => {
    setDraftAliases(initialAliases);
  }, [initialAliases]);

  useEffect(() => {
    setDraftAuthFileScopes(initialAuthFileScopes);
  }, [initialAuthFileScopes]);

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

  return (
    <Card
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
      className={`${styles.detailsFixedCard} ${styles.apiKeySettingsCard}`}
    >
      <div className={styles.apiKeySettingsBody}>
        {loading && apiKeys.length === 0 ? (
          <div className={styles.hint}>{t('common.loading')}</div>
        ) : apiKeys.length === 0 ? (
          <div className={styles.hint}>{t('usage_stats.api_key_settings_empty')}</div>
        ) : (
          <div className={styles.apiKeySettingsList}>
            {apiKeys.map((item) => {
              const draftAlias = draftAliases[item.id] ?? '';
              const disabled = savingId === item.id;
              const scopeSaving = authFileScopeSavingId === item.id;
              const scopeDisabled = authFileScopesLoading || authFileScopeOptionsLoading || scopeSaving;
              const draftAuthFileScope = draftAuthFileScopes[item.id] ?? [];
              const apiKey = getApiKeySettingsVisibleKey(item, showFullApiKeys);
              const copyLabel = copiedId === item.id ? t('usage_stats.api_key_settings_copied') : t('usage_stats.api_key_settings_copy');
              return (
                <div key={item.id} className={styles.apiKeySettingsItem}>
                  <div className={styles.apiKeySettingsTopRow}>
                    <div className={styles.apiKeySettingsSummary}>
                      <span className={styles.apiKeyFieldLabel}>{t('usage_stats.api_key_settings_display_key')}</span>
                      <div className={styles.apiKeySettingsKeyRow}>
                        <span className={styles.apiKeySettingsName} title={apiKey}>{apiKey}</span>
                        <Button
                          variant="secondary"
                          size="sm"
                          className={`${styles.usagePillAction} ${styles.settingsCompactAction} ${styles.apiKeySettingsCopyButton}`.trim()}
                          onClick={() => void handleCopyApiKey(item)}
                          disabled={!item.apiKey}
                        >
                          {copyLabel}
                        </Button>
                      </div>
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
                  {onSaveAuthFileScopes && (
                    <div className={`${styles.apiKeySettingsForm} ${styles.apiKeySettingsScopeForm}`.trim()}>
                      <label className={styles.apiKeyAliasField}>
                        <span className={styles.apiKeyAliasLabel}>{t('usage_stats.api_key_auth_file_scope_label')}</span>
                        <AuthFileScopeSelector
                          key={`${item.id}-${scopeDisabled ? 'disabled' : 'enabled'}`}
                          availableNames={authFileScopeOptions}
                          value={draftAuthFileScope}
                          onChange={(names) => setDraftAuthFileScopes((current) => ({ ...current, [item.id]: names }))}
                          ariaLabel={`${t('usage_stats.api_key_auth_file_scope_label')} ${apiKey}`}
                          disabled={scopeDisabled}
                        />
                        <span className={styles.apiKeyAuthFileScopeHint}>{t('usage_stats.api_key_auth_file_scope_hint')}</span>
                      </label>
                      <div className={styles.apiKeySettingsActions}>
                        <Button
                          variant="primary"
                          size="sm"
                          className={`${styles.usagePillAction} ${styles.settingsCompactAction} ${styles.apiKeySettingsSaveButton}`.trim()}
                          onClick={() => onSaveAuthFileScopes(item.id, draftAuthFileScope)}
                          disabled={scopeDisabled}
                        >
                          {scopeSaving ? t('usage_stats.api_key_auth_file_scope_saving') : t('usage_stats.api_key_auth_file_scope_save')}
                        </Button>
                      </div>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </Card>
  );
}
