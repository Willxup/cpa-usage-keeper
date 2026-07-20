import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal } from 'antd';
import { fetchVersion } from '@/lib/api';
import type { VersionResponse } from '@/lib/types';
import keeperIconUrl from '@/assets/keeper-icon.svg';
import { IconCode, IconExternalLink, IconFileText, IconGithub } from '@/components/ui/icons';
import { CLIPROXYAPI_REPOSITORY_URL, GITHUB_PROFILE_URL, GITHUB_REPOSITORY_URL } from '@/utils/constants';
import styles from './ProductAbout.module.scss';

type AboutVersionLoader = (signal: AbortSignal) => Promise<Pick<VersionResponse, 'version'>>;

export function aboutVersionLabel(version?: string): string | undefined {
  const trimmed = version?.trim();
  return trimmed ? `Version: ${trimmed}` : undefined;
}

export async function loadAboutVersion(loadVersion: AboutVersionLoader, signal: AbortSignal): Promise<string> {
  try {
    const versionInfo = await loadVersion(signal);
    return versionInfo.version ?? '';
  } catch {
    return '';
  }
}

export function ProductAboutContent({ version: fixedVersion, loadVersion = true }: { version?: string; loadVersion?: boolean }) {
  const [version, setVersion] = useState('');

  useEffect(() => {
    if (fixedVersion !== undefined) return;
    if (!loadVersion) return;

    const requestController = new AbortController();
    void loadAboutVersion(fetchVersion, requestController.signal)
      .then((nextVersion) => {
        if (!requestController.signal.aborted) {
          setVersion(nextVersion);
        }
      });

    return () => {
      requestController.abort();
    };
  }, [fixedVersion, loadVersion]);

  const versionLabel = aboutVersionLabel(fixedVersion ?? (loadVersion ? version : ''));

  return (
    <div className={styles.about}>
      <header className={styles.productHeader}>
        <div className={styles.brandMark} aria-hidden="true">
          <img src={keeperIconUrl} alt="" />
        </div>
        <div>
          <h2 className={styles.productName}>CPA Usage Keeper</h2>
          <p className={styles.copyright}>© 2026</p>
        </div>
      </header>

      <nav className={styles.resourceList} aria-label="Project resources">
        <a className={styles.resourceLink} href={GITHUB_REPOSITORY_URL} target="_blank" rel="noreferrer">
          <span className={styles.resourceIcon} aria-hidden="true"><IconGithub size={20} /></span>
          <span className={styles.resourceCopy}>
            <strong>CPA Usage Keeper</strong>
            <span>github.com/Willxup/cpa-usage-keeper</span>
          </span>
          <IconExternalLink className={styles.externalIcon} size={17} aria-hidden="true" />
        </a>
        <a className={styles.resourceLink} href={`${GITHUB_REPOSITORY_URL}/blob/main/LICENSE`} target="_blank" rel="noreferrer">
          <span className={styles.resourceIcon} aria-hidden="true"><IconFileText size={20} /></span>
          <span className={styles.resourceCopy}>
            <strong>License</strong>
            <span>MIT License</span>
          </span>
          <IconExternalLink className={styles.externalIcon} size={17} aria-hidden="true" />
        </a>
        <a className={styles.resourceLink} href={CLIPROXYAPI_REPOSITORY_URL} target="_blank" rel="noreferrer">
          <span className={styles.resourceIcon} aria-hidden="true"><IconCode size={20} /></span>
          <span className={styles.resourceCopy}>
            <strong>CLIProxyAPI Integration</strong>
            <span>github.com/router-for-me/CLIProxyAPI</span>
          </span>
          <IconExternalLink className={styles.externalIcon} size={17} aria-hidden="true" />
        </a>
      </nav>

      <footer className={styles.productMeta}>
        <div className={styles.metaItem}>
          <span className={styles.metaLabel}>Powered By</span>
          <a className={styles.authorLink} href={GITHUB_PROFILE_URL} target="_blank" rel="noreferrer" aria-label="Willxup GitHub profile">
            <IconGithub size={16} aria-hidden="true" />
            <span>Willxup</span>
          </a>
        </div>
        {versionLabel ? (
          <div className={`${styles.metaItem} ${styles.versionMeta}`}>
            <span className={styles.metaLabel}>Version</span>
            <span className={styles.aboutVersion} aria-label={versionLabel}>
              {versionLabel.replace(/^Version:\s*/, '')}
            </span>
          </div>
        ) : null}
      </footer>
    </div>
  );
}

export interface ProductAboutProps {
  open: boolean;
  onClose: () => void;
}

export function ProductAbout({ open, onClose }: ProductAboutProps) {
  const { t } = useTranslation();

  return (
    <Modal
      title={t('common.about_title')}
      open={open}
      onCancel={onClose}
      footer={null}
      width={560}
      centered
      className={styles.aboutModal}
    >
      {open ? <ProductAboutContent /> : null}
    </Modal>
  );
}
