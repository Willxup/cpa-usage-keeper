import type { CSSProperties } from 'react';
import avatarCatalogURL from '@/assets/ranking/avatar-catalog.webp';
import styles from '../RankingPage.module.scss';

const AVATAR_COLUMNS = 10;
const AVATAR_ROWS = 10;

export interface RankingAvatarProps {
  avatarID: number;
  name: string;
  className?: string;
  decorative?: boolean;
}

const avatarPosition = (avatarID: number) => {
  const index = Math.min(66, Math.max(1, Math.trunc(avatarID))) - 1;
  const column = index % AVATAR_COLUMNS;
  const row = Math.floor(index / AVATAR_COLUMNS);
  return `${column * 100 / (AVATAR_COLUMNS - 1)}% ${row * 100 / (AVATAR_ROWS - 1)}%`;
};

export function RankingAvatar({ avatarID, name, className = '', decorative = false }: RankingAvatarProps) {
  const style: CSSProperties = {
    backgroundImage: `url(${avatarCatalogURL})`,
    backgroundPosition: avatarPosition(avatarID),
    backgroundSize: `${AVATAR_COLUMNS * 100}% ${AVATAR_ROWS * 100}%`,
  };

  return (
    <span
      className={`${styles.avatar} ${className}`.trim()}
      style={style}
      role={decorative ? undefined : 'img'}
      aria-label={decorative ? undefined : name}
      aria-hidden={decorative ? true : undefined}
      data-ranking-avatar-id={avatarID}
    />
  );
}
