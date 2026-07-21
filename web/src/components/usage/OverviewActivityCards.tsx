import type { UsageActivityResponse } from '@/lib/types';
import { ServiceHealthCard } from './ServiceHealthCard';
import { TokenActivityCard } from './TokenActivityCard';
import styles from '@/pages/UsagePage.module.scss';

export interface OverviewActivityCardsProps {
  activity: UsageActivityResponse | null;
  loading: boolean;
  requestIdentity: string;
}

export function OverviewActivityCards({ activity, loading, requestIdentity }: OverviewActivityCardsProps) {
  // DOM 顺序固定 Token 在前，桌面对应左侧，移动端自然对应上方。
  return (
    <div className={styles.recentActivityGrid}>
      <TokenActivityCard activity={activity} loading={loading} requestIdentity={requestIdentity} />
      <ServiceHealthCard activity={activity} loading={loading} requestIdentity={requestIdentity} />
    </div>
  );
}

