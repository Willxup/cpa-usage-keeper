import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal } from 'antd';
import { fetchVersion } from '@/lib/api';
import type { VersionResponse } from '@/lib/types';
import { IconGithub } from '@/components/ui/icons';
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
      <div className={styles.aboutLine}>
        <span>© 2026</span>
        <a href={GITHUB_REPOSITORY_URL} target="_blank" rel="noreferrer">CPA Usage Keeper</a>
        <span>·</span>
        <a href={`${GITHUB_REPOSITORY_URL}/blob/main/LICENSE`} target="_blank" rel="noreferrer">License</a>
        <span>·</span>
        <a href={CLIPROXYAPI_REPOSITORY_URL} target="_blank" rel="noreferrer">CLIProxyAPI Integration</a>
      </div>
      <div className={styles.aboutLine}>
        <span>Powered By</span>
        <a href={GITHUB_PROFILE_URL} target="_blank" rel="noreferrer" aria-label="Willxup GitHub profile">
          <IconGithub size={16} aria-hidden="true" />
          <span>Willxup</span>
        </a>
        {versionLabel ? (
          <>
            <span className={styles.aboutVersionSeparator} aria-hidden="true">·</span>
            <span className={styles.aboutVersion}>{versionLabel}</span>
          </>
        ) : null}
      </div>
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
      width={420}
    >
      {open ? <ProductAboutContent /> : null}
    </Modal>
  );
}
