import { useEffect, useState } from 'react';
import { fetchVersion } from '@/lib/api';
import type { VersionResponse } from '@/lib/types';
import { IconGithub } from '@/components/ui/icons';
import { CLIPROXYAPI_REPOSITORY_URL, GITHUB_PROFILE_URL, GITHUB_REPOSITORY_URL } from '@/utils/constants';

type FooterVersionLoader = (signal: AbortSignal) => Promise<Pick<VersionResponse, 'version'>>;

export function footerVersionLabel(version?: string): string | undefined {
  const trimmed = version?.trim();
  return trimmed ? `Version: ${trimmed}` : undefined;
}

export async function loadFooterVersion(loadVersion: FooterVersionLoader, signal: AbortSignal): Promise<string> {
  try {
    const versionInfo = await loadVersion(signal);
    return versionInfo.version ?? '';
  } catch {
    return '';
  }
}

export function AppFooter({ version: fixedVersion, loadVersion = true }: { version?: string; loadVersion?: boolean }) {
  const [version, setVersion] = useState('');

  useEffect(() => {
    if (fixedVersion !== undefined) return;
    if (!loadVersion) return;

    const requestController = new AbortController();
    void loadFooterVersion(fetchVersion, requestController.signal)
      .then((nextVersion) => {
        if (!requestController.signal.aborted) {
          setVersion(nextVersion);
        }
      });

    return () => {
      requestController.abort();
    };
  }, [fixedVersion, loadVersion]);

  const versionLabel = footerVersionLabel(fixedVersion ?? (loadVersion ? version : ''));

  return (
    <footer className="app-footer">
      <div className="app-footer-line app-footer-meta">
        <span>© 2026</span>
        <a href={GITHUB_REPOSITORY_URL} target="_blank" rel="noreferrer">CPA Usage Keeper</a>
        <span>·</span>
        <a href={`${GITHUB_REPOSITORY_URL}/blob/main/LICENSE`} target="_blank" rel="noreferrer">License</a>
        <span>·</span>
        <a href={CLIPROXYAPI_REPOSITORY_URL} target="_blank" rel="noreferrer">CLIProxyAPI Integration</a>
      </div>
      <div className="app-footer-line app-footer-powered">
        <span>Powered By</span>
        <a href={GITHUB_PROFILE_URL} target="_blank" rel="noreferrer" aria-label="Willxup GitHub profile">
          <IconGithub size={16} aria-hidden="true" />
          <span>Willxup</span>
        </a>
        {versionLabel ? (
          <>
            <span className="app-footer-version-separator" aria-hidden="true">·</span>
            <span className="app-footer-version">{versionLabel}</span>
          </>
        ) : null}
      </div>
    </footer>
  );
}
