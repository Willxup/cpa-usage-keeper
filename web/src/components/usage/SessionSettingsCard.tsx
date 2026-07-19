import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Card, Popconfirm, Table, Tag, Typography, type TableColumnsType } from 'antd';
import { SectionHeader } from '@/components/layout';
import type { AuthManagedSessionItem } from '@/lib/types';
import styles from './UsageSettings.module.scss';

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

function getSessionRowKey(session: AuthManagedSessionItem): string {
  let hash = 2166136261;
  for (let index = 0; index < session.id.length; index += 1) {
    hash ^= session.id.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return `session-${(hash >>> 0).toString(36)}`;
}

export function SessionSettingsCard({ sessions, loading = false, revokingId = null, onLogout }: SessionSettingsCardProps) {
  const { t } = useTranslation();
  const [confirmingSession, setConfirmingSession] = useState<AuthManagedSessionItem | null>(null);

  const handleConfirmLogout = useCallback(async () => {
    if (!confirmingSession) {
      return;
    }
    await onLogout(confirmingSession);
    setConfirmingSession(null);
  }, [confirmingSession, onLogout]);

  const columns = useMemo<TableColumnsType<AuthManagedSessionItem>>(() => [
    {
      key: 'session',
      title: t('usage_stats.session_settings_session_column'),
      width: 240,
      render: (_value, session) => {
        const displayName = getSessionDisplayName(session, t);
        return (
          <Typography.Text strong ellipsis={{ tooltip: displayName }}>
            {displayName}
          </Typography.Text>
        );
      },
    },
    {
      key: 'source',
      title: t('usage_stats.session_settings_source_column'),
      width: 150,
      render: (_value, session) => {
        const sourceLabel = session.source === 'embed'
          ? t('usage_stats.session_settings_source_embed')
          : t('usage_stats.session_settings_source_standard');
        return (
          <Typography.Text type="secondary">
            {sourceLabel}
          </Typography.Text>
        );
      },
    },
    {
      key: 'loginAt',
      title: t('usage_stats.session_settings_login_column'),
      width: 190,
      responsive: ['md'],
      render: (_value, session) => (
        <Typography.Text className={styles.sessionSettingsTimestamp}>
          {session.loginAt ?? '-'}
        </Typography.Text>
      ),
    },
    {
      key: 'expiresAt',
      title: t('usage_stats.session_settings_expires_column'),
      width: 190,
      responsive: ['lg'],
      render: (_value, session) => (
        <Typography.Text className={styles.sessionSettingsTimestamp}>
          {session.expiresAt ?? '-'}
        </Typography.Text>
      ),
    },
    {
      key: 'status',
      title: t('usage_stats.session_settings_status_column'),
      width: 104,
      render: (_value, session) => session.current ? (
        <Tag color="processing">{t('usage_stats.session_settings_current')}</Tag>
      ) : '-',
    },
    {
      key: 'actions',
      title: t('usage_stats.session_settings_actions_column'),
      align: 'right',
      width: 124,
      render: (_value, session) => {
        if (session.current) {
          return null;
        }
        const confirmationKeys = getSessionLogoutConfirmationKeys(session);
        const displayName = getSessionDisplayName(session, t);
        const isRevoking = revokingId === session.id;
        const isOpen = confirmingSession?.id === session.id;
        return (
          <Popconfirm
            open={isOpen}
            title={t(confirmationKeys.titleKey)}
            description={t(confirmationKeys.bodyKey, { label: displayName })}
            okText={t(confirmationKeys.confirmKey)}
            cancelText={t('common.cancel')}
            okButtonProps={{ danger: true, loading: isRevoking }}
            cancelButtonProps={{ disabled: isRevoking }}
            onOpenChange={(open) => {
              if (isRevoking) {
                return;
              }
              setConfirmingSession(open ? session : null);
            }}
            onConfirm={() => handleConfirmLogout()}
          >
            <Button
              danger
              size="small"
              loading={isRevoking}
              disabled={Boolean(revokingId) && !isRevoking}
              aria-label={t('usage_stats.session_settings_logout_one')}
            >
              {isRevoking ? t('usage_stats.session_settings_logging_out') : t('common.logout')}
            </Button>
          </Popconfirm>
        );
      },
    },
  ], [confirmingSession, handleConfirmLogout, revokingId, t]);

  return (
    <Card
      variant="outlined"
      title={
        <SectionHeader
          headingLevel={2}
          title={t('usage_stats.session_settings_title')}
        />
      }
      className={styles.sessionSettingsCard}
    >
      <Table<AuthManagedSessionItem>
        className={styles.sessionSettingsTable}
        columns={columns}
        dataSource={sessions}
        rowKey={getSessionRowKey}
        pagination={false}
        size="small"
        scroll={{ x: 800 }}
        locale={{
          emptyText: loading
            ? t('common.loading')
            : t('usage_stats.session_settings_empty'),
        }}
      />
    </Card>
  );
}
