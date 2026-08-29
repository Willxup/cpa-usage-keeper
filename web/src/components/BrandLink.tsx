import { GITHUB_REPOSITORY_URL } from '@/utils/constants';
import keeperIconUrl from '@/assets/keeper-icon.svg';
import styles from './BrandLink.module.scss';

type BrandLinkProps = {
  className?: string;
};

export function BrandLink({ className = '' }: BrandLinkProps) {
  const linkClassName = `${styles.brandLink} ${className}`.trim();

  return (
    <a className={linkClassName} href={GITHUB_REPOSITORY_URL} target="_blank" rel="noreferrer">
      <img className={styles.brandMark} src={keeperIconUrl} alt="" aria-hidden="true" />
      <span className={styles.brandWord}>KEEPER</span>
    </a>
  );
}
