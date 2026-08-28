import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { Modal } from '@/components/ui/Modal';
import { IconCheck, IconPencil, IconX } from '@/components/ui/icons';
import { useScrollBoundaryContainment } from '@/hooks/useScrollBoundaryContainment';
import type { AuthManagedSessionItem } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

export interface SessionSettingsCardProps {
  sessions: AuthManagedSessionItem[];
  loading?: boolean;
  revokingId?: string | null;
  aliasSavingId?: string | null;
  onLogout: (session: AuthManagedSessionItem) => void | Promise<void>;
  onSaveAlias?: (id: string, alias: string) => Promise<void>;
}

export function getSessionLogoutConfirmationKeys(session: AuthManagedSessionItem) {
  if (session.kind === 'admin') {
    return {
      titleKey: 'usage_stats.session_settings_admin_logout_title',
      bodyKey: 'usage_stats.session_settings_admin_logout_body',
      confirmKey: 'usage_stats.session_settings_logout_confirm',
    };
  }
  return {
    titleKey: 'usage_stats.session_settings_api_key_logout_title',
    bodyKey: 'usage_stats.session_settings_api_key_logout_body',
    confirmKey: 'usage_stats.session_settings_logout_confirm',
  };
}

function getSessionDisplayName(session: AuthManagedSessionItem, t: (key: string) => string) {
  if (session.kind === 'admin') {
    return session.alias || t('usage_stats.session_settings_admin_label');
  }
  return session.label || session.displayKey || t('usage_stats.session_settings_unknown_api_key');
}

function getSessionClientLabel(session: AuthManagedSessionItem, t: (key: string) => string) {
  return session.userAgent || t('usage_stats.session_settings_unknown_value');
}

interface AdminSessionAliasEditorProps {
  session: AuthManagedSessionItem;
  displayName: string;
  saving: boolean;
  disabled: boolean;
  onSaveAlias: (id: string, alias: string) => Promise<void>;
}

function AdminSessionAliasEditor({ session, displayName, saving, disabled, onSaveAlias }: AdminSessionAliasEditorProps) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);
  const [draftAlias, setDraftAlias] = useState(session.alias ?? '');
  const editButtonRef = useRef<HTMLButtonElement | null>(null);
  const restoreFocusRef = useRef(false);
  const currentAlias = session.alias ?? '';
  const canEdit = !disabled && session.id.trim() !== '';
  const canSave = !saving && !disabled && draftAlias.trim() !== currentAlias.trim();

  useEffect(() => {
    if (!editing && restoreFocusRef.current) {
      restoreFocusRef.current = false;
      editButtonRef.current?.focus();
    }
  }, [editing]);

  const finishEditing = () => {
    restoreFocusRef.current = true;
    setEditing(false);
  };

  const startEditing = () => {
    if (!canEdit) return;
    setDraftAlias(currentAlias);
    setEditing(true);
  };
  const cancelEditing = () => {
    if (saving || disabled) return;
    setDraftAlias(currentAlias);
    finishEditing();
  };
  const saveAlias = async () => {
    if (!canSave) return;
    try {
      await onSaveAlias(session.id, draftAlias);
      finishEditing();
    } catch {
      // 保存失败时保留输入内容，方便直接修正或重试。
    }
  };

  if (editing) {
    return (
      <span className={`${styles.sessionSettingsAliasEditor} ${styles.sessionSettingsAliasEditorEditing}`}>
        <span className={styles.sessionSettingsAliasEditLayout}>
          <input
            className={styles.sessionSettingsAliasInput}
            value={draftAlias}
            placeholder={t('usage_stats.session_settings_alias_placeholder')}
            disabled={saving || disabled}
            maxLength={50}
            onChange={(event) => setDraftAlias(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault();
                void saveAlias();
              }
              if (event.key === 'Escape') {
                event.preventDefault();
                cancelEditing();
              }
            }}
            aria-label={t('usage_stats.session_settings_alias_placeholder')}
            autoFocus
          />
          <span className={styles.sessionSettingsAliasEditActions}>
            <button
              type="button"
              className={styles.sessionSettingsAliasIconButton}
              onClick={() => void saveAlias()}
              disabled={!canSave}
              title={saving ? t('usage_stats.session_settings_alias_saving') : t('usage_stats.session_settings_alias_save')}
              aria-label={saving ? t('usage_stats.session_settings_alias_saving') : t('usage_stats.session_settings_alias_save')}
              aria-busy={saving}
            >
              {saving ? <LoadingSpinner size={12} /> : <IconCheck size={13} />}
            </button>
            <button
              type="button"
              className={styles.sessionSettingsAliasIconButton}
              onClick={cancelEditing}
              disabled={saving || disabled}
              title={t('usage_stats.session_settings_alias_cancel')}
              aria-label={t('usage_stats.session_settings_alias_cancel')}
            >
              <IconX size={13} />
            </button>
          </span>
        </span>
      </span>
    );
  }

  return (
    <span className={styles.sessionSettingsAliasEditor}>
      <span className={styles.sessionSettingsAliasDisplayLayout}>
        <span className={styles.sessionSettingsName} title={displayName}>{displayName}</span>
        <button
          type="button"
          className={styles.sessionSettingsAliasEditButton}
          ref={editButtonRef}
          onClick={startEditing}
          disabled={!canEdit}
          title={t('usage_stats.session_settings_alias_edit')}
          aria-label={t('usage_stats.session_settings_alias_edit')}
        >
          <IconPencil size={12} />
        </button>
      </span>
    </span>
  );
}

export function SessionSettingsCard({ sessions, loading = false, revokingId = null, aliasSavingId = null, onLogout, onSaveAlias }: SessionSettingsCardProps) {
  const { t } = useTranslation();
  const [confirmingSession, setConfirmingSession] = useState<AuthManagedSessionItem | null>(null);
  const sessionSettingsBodyRef = useRef<HTMLDivElement | null>(null);
  useScrollBoundaryContainment(sessionSettingsBodyRef);
  const confirmationKeys = confirmingSession ? getSessionLogoutConfirmationKeys(confirmingSession) : null;
  const confirmingLabel = confirmingSession ? getSessionDisplayName(confirmingSession, t) : '';
  const confirmingRevoking = confirmingSession ? revokingId === confirmingSession.id : false;

  const handleConfirmLogout = useCallback(async () => {
    if (!confirmingSession) {
      return;
    }
    await onLogout(confirmingSession);
    setConfirmingSession(null);
  }, [confirmingSession, onLogout]);

  return (
    <Card
      title={t('usage_stats.session_settings_title')}
      subtitle={t('usage_stats.session_settings_subtitle')}
      className={`${styles.detailsFixedCard} ${styles.sessionSettingsCard}`}
    >
      <div ref={sessionSettingsBodyRef} className={styles.sessionSettingsBody}>
        {loading && sessions.length === 0 ? (
          <div className={styles.hint}>{t('common.loading')}</div>
        ) : sessions.length === 0 ? (
          <div className={styles.hint}>{t('usage_stats.session_settings_empty')}</div>
        ) : (
          <div className={styles.sessionSettingsList}>
            {sessions.map((session) => {
              const isAdmin = session.kind === 'admin';
              const displayName = getSessionDisplayName(session, t);
              const clientLabel = getSessionClientLabel(session, t);
              const sourceLabel = session.source === 'embed'
                ? t('usage_stats.session_settings_source_embed')
                : t('usage_stats.session_settings_source_standard');
              const disabled = revokingId === session.id;
              const aliasSaving = aliasSavingId === session.id;
              const aliasDisabled = Boolean(aliasSavingId && !aliasSaving);
              // 详情项交给 CSS Grid 按可用宽度铺开，额外的最近 IP 不占用固定列。
              const details = [
                {
                  key: 'login-ip',
                  label: t('usage_stats.session_settings_login_ip'),
                  value: session.loginIp || t('usage_stats.session_settings_unknown_value'),
                },
                ...(session.lastSeenIp && session.lastSeenIp !== session.loginIp
                  ? [{ key: 'last-seen-ip', label: t('usage_stats.session_settings_last_seen_ip'), value: session.lastSeenIp }]
                  : []),
                {
                  key: 'last-seen-at',
                  label: t('usage_stats.session_settings_last_seen_at'),
                  value: session.lastSeenAt ?? session.loginAt ?? '-',
                },
                {
                  key: 'login-at',
                  label: t('usage_stats.session_settings_login_at'),
                  value: session.loginAt ?? '-',
                },
                {
                  key: 'expires-at',
                  label: t('usage_stats.session_settings_expires_at'),
                  value: session.expiresAt ?? '-',
                },
              ];
              return (
                <div key={session.id} className={styles.sessionSettingsItem}>
                  <div className={styles.sessionSettingsSummary}>
                    <div className={styles.sessionSettingsBadges}>
                      <span className={styles.sessionSettingsType}>
                        {isAdmin ? t('usage_stats.session_settings_type_admin') : t('usage_stats.session_settings_type_api_key')}
                      </span>
                      <span
                        className={`${styles.sessionSettingsSource} ${session.source === 'embed' ? styles.sessionSettingsSourceEmbed : styles.sessionSettingsSourceStandard}`}
                        data-session-source={session.source === 'embed' ? 'embed' : 'standard'}
                      >
                        {sourceLabel}
                      </span>
                    </div>
                    <div className={styles.sessionSettingsNameRow}>
                      {isAdmin && onSaveAlias ? (
                        <AdminSessionAliasEditor
                          session={session}
                          displayName={displayName}
                          saving={aliasSaving}
                          disabled={aliasDisabled}
                          onSaveAlias={onSaveAlias}
                        />
                      ) : (
                        <span className={styles.sessionSettingsName} title={displayName}>{displayName}</span>
                      )}
                    </div>
                  </div>
                  <div className={styles.sessionSettingsClient}>
                    <span className={styles.sessionSettingsClientLabel}>{t('usage_stats.session_settings_user_agent')}</span>
                    <span className={styles.sessionSettingsClientValue}>{clientLabel}</span>
                  </div>
                  <dl className={styles.sessionSettingsDetails}>
                    {details.map((detail) => (
                      <div key={detail.key} className={styles.sessionSettingsDetailItem}>
                        <dt className={styles.sessionSettingsDetailLabel}>{detail.label}</dt>
                        <dd className={styles.sessionSettingsDetailValue}>{detail.value}</dd>
                      </div>
                    ))}
                  </dl>
                  <div className={styles.sessionSettingsActions}>
                    {session.current ? (
                      <span className={styles.sessionSettingsCurrent} data-session-current="true">
                        <span
                          className={styles.sessionSettingsCurrentDot}
                          data-session-current-dot="true"
                          aria-hidden="true"
                        />
                        <span>{t('usage_stats.session_settings_current')}</span>
                      </span>
                    ) : (
                      <Button
                        type="button"
                        variant="danger"
                        size="sm"
                        appearance="action"
                        className={styles.sessionSettingsLogoutButton}
                        onClick={() => setConfirmingSession(session)}
                        disabled={disabled}
                        aria-label={t('usage_stats.session_settings_logout_one')}
                      >
                        {disabled ? t('usage_stats.session_settings_logging_out') : t('common.logout')}
                      </Button>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
      {confirmationKeys && confirmingSession && (
        <Modal
          open={Boolean(confirmingSession)}
          title={t(confirmationKeys.titleKey)}
          onClose={() => setConfirmingSession(null)}
          closeDisabled={confirmingRevoking}
          footer={
            <>
              <Button type="button" variant="secondary" appearance="action" onClick={() => setConfirmingSession(null)} disabled={confirmingRevoking}>
                {t('common.cancel')}
              </Button>
              <Button type="button" variant="danger" appearance="action" onClick={() => void handleConfirmLogout()} loading={confirmingRevoking}>
                {confirmingRevoking ? t('usage_stats.session_settings_logging_out') : t(confirmationKeys.confirmKey)}
              </Button>
            </>
          }
        >
          <p className={styles.sessionSettingsConfirmText}>{t(confirmationKeys.bodyKey, { label: confirmingLabel })}</p>
        </Modal>
      )}
    </Card>
  );
}
