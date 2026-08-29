import { useTranslation } from 'react-i18next';
import { formatLeaderboardValue, formatOverallMetricValue } from '../format';
import type {
  RankingDetailMetric,
  RankingLeaderboardEntry,
  RankingMetric,
  RankingScope,
} from '../types';
import styles from '../RankingPage.module.scss';
import { RankingAvatar } from './RankingAvatar';

const OVERALL_METRICS: RankingDetailMetric[] = [
  'total_tokens',
  'request_count',
  'cache_read_rate',
  'ttft_average',
  'latency_average',
  'peak_tpm',
  'peak_rpm',
];

export interface RankingLeaderboardResultsProps {
  scope: RankingScope;
  metric: RankingMetric;
  entries: readonly RankingLeaderboardEntry[];
  onEditLocalProfile?: (entry: RankingLeaderboardEntry) => void;
}

export function RankingLeaderboardResults({
  scope,
  metric,
  entries,
  onEditLocalProfile,
}: RankingLeaderboardResultsProps) {
  const { t } = useTranslation();
  const podium = entries.slice(0, 3);

  return (
    <div className={styles.leaderboardResults} data-ranking-results>
      <div className={styles.podiumGrid} aria-label={`${t('ranking.rank')} 1–3`} data-ranking-podium>
        {podium.map((entry, index) => (
          <PodiumCard
            key={entry.participant_id}
            entry={entry}
            position={index + 1}
            metric={metric}
            scope={scope}
            onEditLocalProfile={onEditLocalProfile}
          />
        ))}
      </div>
      <div className={styles.tableScroll}>
        <table className={styles.table}>
          <thead>
            <tr>
              <th className={styles.rankColumn} data-ranking-rank-column>{t('ranking.rank')}</th>
              <th className={styles.participantColumn} data-ranking-participant-column>
                {t(scope === 'local' ? 'ranking.api_key' : 'ranking.participant')}
              </th>
              {metric === 'overall' ? (
                <>
                  <th className={styles.numberCell}>{t('ranking.score')}</th>
                  {OVERALL_METRICS.map((item) => <th key={item} className={styles.numberCell}>{t(`ranking.metric_short_${item}`)}</th>)}
                </>
              ) : <th className={styles.numberCell}>{t(`ranking.metric_${metric}`)}</th>}
            </tr>
          </thead>
          <tbody>
            {entries.map((entry, index) => (
              <tr key={entry.participant_id} data-ranking-row>
                <td className={styles.rankColumn} data-ranking-rank-column>
                  <span className={styles.rankBadge} data-ranking-position>{index + 1}</span>
                </td>
                <td className={styles.participantColumn} data-ranking-participant-column>
                  <div className={styles.participantCell}>
                    <LeaderboardEntryAvatar
                      entry={entry}
                      scope={scope}
                      className={styles.tableAvatar}
                      onEditLocalProfile={onEditLocalProfile}
                    />
                    <strong>{entry.display_name}</strong>
                  </div>
                </td>
                {metric === 'overall' ? (
                  <>
                    <td className={`${styles.numberCell} ${styles.scoreCell}`.trim()}>{formatLeaderboardValue(metric, entry, scope)}</td>
                    {OVERALL_METRICS.map((item) => (
                      <td key={item} className={styles.numberCell}>{formatOverallMetricValue(item, entry)}</td>
                    ))}
                  </>
                ) : <td className={`${styles.numberCell} ${styles.scoreCell}`.trim()}>{formatLeaderboardValue(metric, entry, scope)}</td>}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function PodiumCard({ entry, position, metric, scope, onEditLocalProfile }: {
  entry: RankingLeaderboardEntry;
  position: number;
  metric: RankingMetric;
  scope: RankingScope;
  onEditLocalProfile?: (entry: RankingLeaderboardEntry) => void;
}) {
  const { t } = useTranslation();
  const value = formatLeaderboardValue(metric, entry, scope);
  const valueSizeClass = value.length >= 11
    ? styles.podiumValueCompact
    : value.length >= 8
      ? styles.podiumValueMedium
      : '';

  return (
    <article
      className={`${styles.podiumCard} ${styles[`podiumCard${position}` as keyof typeof styles]}`.trim()}
      data-ranking-podium-rank={position}
    >
      <div className={styles.podiumRank}>
        <span>{t('ranking.rank')}</span>
        <strong>{String(position).padStart(2, '0')}</strong>
      </div>
      <LeaderboardEntryAvatar
        entry={entry}
        scope={scope}
        className={styles.podiumAvatar}
        onEditLocalProfile={onEditLocalProfile}
      />
      <strong className={styles.podiumName}>{entry.display_name}</strong>
      <span className={`${styles.podiumValue} ${valueSizeClass}`.trim()}>{value}</span>
    </article>
  );
}

function LeaderboardEntryAvatar({ entry, scope, className, onEditLocalProfile }: {
  entry: RankingLeaderboardEntry;
  scope: RankingScope;
  className: string;
  onEditLocalProfile?: (entry: RankingLeaderboardEntry) => void;
}) {
  const { t } = useTranslation();
  if (scope !== 'local' || !onEditLocalProfile) {
    return <RankingAvatar avatarID={entry.avatar_id} name={entry.display_name} className={className} decorative />;
  }
  return (
    <button
      type="button"
      className={`${styles.localProfileAvatarButton} ${className}`.trim()}
      aria-label={t('ranking.local_profile_edit_label', { name: entry.display_name })}
      onClick={() => onEditLocalProfile(entry)}
      data-ranking-local-profile-edit={entry.participant_id}
    >
      <RankingAvatar avatarID={entry.avatar_id} name={entry.display_name} decorative />
    </button>
  );
}
