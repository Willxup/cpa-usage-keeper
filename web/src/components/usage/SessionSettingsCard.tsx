import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import type { AuthManagedSessionItem } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

export interface SessionSettingsCardProps {
  sessions: AuthManagedSessionItem[];
  loading?: boolean;
  revokingId?: string | null;
  onLogout: (session: AuthManagedSessionItem) => void | Promise<void>;
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
    return t('usage_stats.session_settings_admin_label');
  }
  return session.label || session.displayKey || t('usage_stats.session_settings_unknown_api_key');
}

export function SessionSettingsCard({ sessions, loading = false, revokingId = null, onLogout }: SessionSettingsCardProps) {
  const { t } = useTranslation();
  const [confirmingSession, setConfirmingSession] = useState<AuthManagedSessionItem | null>(null);
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
      title={
        <div className={styles.sectionTitleBlock}>
          <h3 className={styles.sectionTitle}>{t('usage_stats.session_settings_title')}</h3>
          <p className={styles.sectionSubtitle}>{t('usage_stats.session_settings_subtitle')}</p>
        </div>
      }
      className={`${styles.detailsFixedCard} ${styles.sessionSettingsCard}`}
    >
      <div className={styles.sessionSettingsBody}>
        {loading && sessions.length === 0 ? (
          <div className={styles.hint}>{t('common.loading')}</div>
        ) : sessions.length === 0 ? (
          <div className={styles.hint}>{t('usage_stats.session_settings_empty')}</div>
        ) : (
          <div className={styles.sessionSettingsList}>
            {sessions.map((session) => {
              const isAdmin = session.kind === 'admin';
              const displayName = getSessionDisplayName(session, t);
              const disabled = revokingId === session.id;
              return (
                <div key={session.id} className={styles.sessionSettingsItem}>
                  <div className={styles.sessionSettingsSummary}>
                    <div className={styles.sessionSettingsBadges}>
                      <span className={styles.sessionSettingsType}>
                        {isAdmin ? t('usage_stats.session_settings_type_admin') : t('usage_stats.session_settings_type_api_key')}
                      </span>
                      {session.current && (
                        <span className={styles.sessionSettingsCurrent}>{t('usage_stats.session_settings_current')}</span>
                      )}
                    </div>
                    <span className={styles.sessionSettingsName} title={displayName}>{displayName}</span>
                  </div>
                  <div className={styles.sessionSettingsDetails}>
                    <span>{t('usage_stats.session_settings_login_at', { value: session.loginAt ?? '-' })}</span>
                    <span>{t('usage_stats.session_settings_expires_at', { value: session.expiresAt ?? '-' })}</span>
                  </div>
                  <div className={styles.sessionSettingsActions}>
                    {!session.current && (
                      <Button
                        type="button"
                        variant="danger"
                        size="sm"
                        className={`${styles.usagePillAction} ${styles.settingsCompactAction} ${styles.usagePillActionDanger} ${styles.sessionSettingsLogoutButton}`.trim()}
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
              <Button type="button" variant="secondary" onClick={() => setConfirmingSession(null)} disabled={confirmingRevoking}>
                {t('common.cancel')}
              </Button>
              <Button type="button" variant="danger" onClick={() => void handleConfirmLogout()} loading={confirmingRevoking}>
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
