import { GITHUB_REPOSITORY_URL } from '@/utils/constants';
import styles from './BrandLink.module.scss';

type BrandLinkProps = {
  className?: string;
};

export function BrandLink({ className = '' }: BrandLinkProps) {
  const linkClassName = `${styles.brandLink} ${className}`.trim();

  return (
    <a className={linkClassName} href={GITHUB_REPOSITORY_URL} target="_blank" rel="noreferrer">
      CPA Usage Keeper
    </a>
  );
}
