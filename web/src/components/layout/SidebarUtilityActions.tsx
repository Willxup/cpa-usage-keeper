import { useState } from 'react';
import { Badge, Button as AntButton } from 'antd';
import { BellOutlined, InfoCircleOutlined, LogoutOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { PreferencesDropdown } from '@/components/ui/PreferencesDropdown';
import { ProductAbout } from './ProductAbout';
import styles from './SidebarUtilityActions.module.scss';

export interface SidebarUtilityActionsProps {
  showUpdateCheck: boolean;
  hasNewVersion: boolean;
  updateCheckLoading: boolean;
  onCheckUpdates: () => void | Promise<void>;
  loggingOut: boolean;
  onRequestLogout: () => void;
}

export function SidebarUtilityActions({
  showUpdateCheck,
  hasNewVersion,
  updateCheckLoading,
  onCheckUpdates,
  loggingOut,
  onRequestLogout,
}: SidebarUtilityActionsProps) {
  const { t } = useTranslation();
  const [aboutOpen, setAboutOpen] = useState(false);

  return (
    <div className={styles.actions}>
      <div className={styles.preferencesActions}>
        <PreferencesDropdown
          className={styles.utilityButton}
          section="language"
          showLabel
        />
        <PreferencesDropdown
          className={styles.utilityButton}
          section="appearance"
          showLabel
        />
      </div>
      {showUpdateCheck && (
        <Badge dot={hasNewVersion} offset={[-4, 4]} className={styles.actionBadge}>
          <AntButton
            className={styles.utilityButton}
            type="text"
            icon={<BellOutlined />}
            onClick={() => void onCheckUpdates()}
            loading={updateCheckLoading}
            aria-label={t('usage_stats.check_updates')}
            aria-pressed={hasNewVersion}
          >
            {t('usage_stats.check_updates')}
          </AntButton>
        </Badge>
      )}
      <AntButton
        className={styles.utilityButton}
        type="text"
        icon={<InfoCircleOutlined />}
        onClick={() => setAboutOpen(true)}
        aria-label={t('common.about_open')}
      >
        {t('common.about_title')}
      </AntButton>
      <AntButton
        className={styles.utilityButton}
        type="text"
        danger
        icon={<LogoutOutlined />}
        onClick={onRequestLogout}
        loading={loggingOut}
      >
        {t('common.logout')}
      </AntButton>
      <ProductAbout open={aboutOpen} onClose={() => setAboutOpen(false)} />
    </div>
  );
}
