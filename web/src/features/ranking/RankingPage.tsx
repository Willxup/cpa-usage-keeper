import { type KeyboardEvent, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { MainActionButton } from '@/components/ui/MainActionButton';
import { Modal } from '@/components/ui/Modal';
import { QuestionMarkHelp } from '@/components/ui/QuestionMarkHelp';
import { RankingApiError } from './api';
import { RankingAvatar } from './components/RankingAvatar';
import { RankingLeaderboardResults } from './components/RankingLeaderboardResults';
import { RankingMetricSelect, RankingToolbar } from './components/RankingToolbar';
import { normalizeRankingDisplayName, RANKING_DISPLAY_NAME_MAX_LENGTH, type RankingProfileError } from './profile';
import type {
  LocalRankingProfileRequest,
  LocalRankingProfileResponse,
  RankingLeaderboardEntry,
  RankingLeaderboardResponse,
  RankingMetadataResponse,
  RankingMetric,
  RankingPeriod,
  RankingProfileRequest,
  RankingScope,
  RankingStatusResponse,
} from './types';
import styles from './RankingPage.module.scss';

const AVATAR_IDS = Array.from({ length: 66 }, (_, index) => index + 1);

type RankingAction = 'join' | 'sync' | 'pause' | 'resume' | 'exit' | null;
type ProfileAction = Exclude<RankingAction, 'join' | null>;
type ProfileModalStep = 'profile' | 'confirm-join' | 'confirm-pause' | 'confirm-exit';
type AsyncAction = () => Promise<unknown>;

const PROFILE_ACTION_SUCCESS_KEYS: Record<ProfileAction, string> = {
  sync: 'ranking.action_sync_success',
  pause: 'ranking.action_pause_success',
  resume: 'ranking.action_resume_success',
  exit: 'ranking.action_exit_success',
};

export interface RankingPageProps {
  scope: RankingScope;
  period: RankingPeriod;
  metric: RankingMetric;
  status: RankingStatusResponse | null;
  metadata: RankingMetadataResponse | null;
  leaderboard: RankingLeaderboardResponse | null;
  statusLoading: boolean;
  metadataLoading: boolean;
  leaderboardLoading: boolean;
  statusError: unknown;
  metadataError: unknown;
  leaderboardError: unknown;
  action: RankingAction;
  actionError: unknown;
  onClearActionError: () => void;
  onJoin: (profile: RankingProfileRequest) => Promise<unknown>;
  onSync: AsyncAction;
  onPause: AsyncAction;
  onResume: AsyncAction;
  onExit: AsyncAction;
  onRetryStatus: AsyncAction;
  onRetryMetadata: AsyncAction;
  onRetryLeaderboard: AsyncAction;
  onUpdateLocalProfile: (participantID: string, profile: LocalRankingProfileRequest) => Promise<LocalRankingProfileResponse>;
  onPeriodChange: (period: RankingPeriod) => void;
  onMetricChange: (metric: RankingMetric) => void;
}

type Translate = (key: string, params?: Record<string, string | number>) => string;

const formatError = (error: unknown, t: Translate): string => {
  if (!(error instanceof RankingApiError)) return t('ranking.error_generic');
  if (error.status === 429 && error.retryAfter) {
    const seconds = Number.parseInt(error.retryAfter, 10);
    if (Number.isFinite(seconds) && seconds > 0) {
      if (seconds < 60) return t('ranking.error_rate_limited_seconds', { seconds });
      return t('ranking.error_rate_limited_minutes', { minutes: Math.ceil(seconds / 60) });
    }
  }
  if (error.status === 429) return t('ranking.error_rate_limited_generic');
  const knownCodes: Record<string, string> = {
    invalid_ranking_profile: 'ranking.error_invalid_profile',
    ranking_profile_locked: 'ranking.error_profile_locked',
    ranking_participant_deleted: 'ranking.error_deleted',
    ranking_sync_in_progress: 'ranking.error_sync_in_progress',
    ranking_participation_state_conflict: 'ranking.error_participation_state',
    ranking_center_incompatible: 'ranking.error_center_incompatible',
    invalid_local_ranking_profile: 'ranking.local_profile_save_failed',
    local_ranking_key_not_found: 'ranking.local_profile_save_failed',
    local_ranking_profile_update_failed: 'ranking.local_profile_save_failed',
  };
  return t(knownCodes[error.code] ?? 'ranking.error_generic');
};

const profileErrorKey = (error: RankingProfileError) => `ranking.profile_error_${error}`;

const resolveScoreExplanation = (
  board: RankingLeaderboardResponse | null,
  metric: RankingMetric,
  language: string,
): string => {
  if (metric !== 'overall' || board?.metric !== 'overall' || board.score_explanation?.version !== 2) return '';
  const texts = board.score_explanation.texts;
  if (!texts) return '';
  const candidates = [
    texts[language as keyof typeof texts],
    texts.en,
    texts.zh,
    texts['zh-TW'],
  ];
  return candidates.find((text) => text?.trim())?.trim() ?? '';
};

export function RankingPage(props: RankingPageProps) {
  const { t, i18n } = useTranslation();
  const [displayName, setDisplayName] = useState('');
  const [avatarID, setAvatarID] = useState(1);
  const [profileError, setProfileError] = useState<RankingProfileError | null>(null);
  const [pendingProfile, setPendingProfile] = useState<RankingProfileRequest | null>(null);
  const [profileModalStep, setProfileModalStep] = useState<ProfileModalStep | null>(null);
  const [profileActionSuccess, setProfileActionSuccess] = useState<ProfileAction | null>(null);
  const [localProfileEntry, setLocalProfileEntry] = useState<RankingLeaderboardEntry | null>(null);
  const [localProfileAlias, setLocalProfileAlias] = useState('');
  const [localProfileAvatarID, setLocalProfileAvatarID] = useState(1);
  const [localProfileSaving, setLocalProfileSaving] = useState(false);
  const [localProfileError, setLocalProfileError] = useState<unknown>(null);
  const currentBoard = props.leaderboard?.period === props.period && props.leaderboard.metric === props.metric
    ? props.leaderboard
    : null;
  // 榜单内容只接受当前选择；同指标的上一周期响应可暂时维持综合分说明，稳定标题栏布局。
  const scoreExplanationBoard = props.leaderboard?.metric === props.metric ? props.leaderboard : null;
  const periodMetadata = props.metadata?.periods.find((item) => item.period === props.period);

  const handleRequestJoin = () => {
    const normalized = normalizeRankingDisplayName(displayName);
    setDisplayName(normalized.value);
    setProfileError(normalized.error);
    if (normalized.error) return;
    setPendingProfile({ display_name: normalized.value, avatar_id: avatarID });
    setProfileModalStep('confirm-join');
  };

  const showProfileStep = () => {
    setProfileModalStep('profile');
  };

  const openProfileModal = () => {
    setProfileActionSuccess(null);
    showProfileStep();
  };

  const runProfileAction = async (action: ProfileAction, operation: AsyncAction) => {
    setProfileActionSuccess(null);
    const result = await operation();
    if (result !== null && result !== undefined) setProfileActionSuccess(action);
    return result;
  };

  const handleConfirmJoin = async () => {
    if (!pendingProfile) return;
    const profile = pendingProfile;
    await props.onJoin(profile);
    setPendingProfile(null);
    showProfileStep();
  };

  const handleConfirmExit = async () => {
    await runProfileAction('exit', props.onExit);
    showProfileStep();
  };

  const handleConfirmPause = async () => {
    await runProfileAction('pause', props.onPause);
    showProfileStep();
  };

  const closeProfileModal = () => {
    setPendingProfile(null);
    setProfileActionSuccess(null);
    props.onClearActionError();
    setProfileModalStep(null);
  };

  const openLocalProfileModal = (entry: RankingLeaderboardEntry) => {
    if (props.scope !== 'local') return;
    setLocalProfileEntry(entry);
    setLocalProfileAlias(entry.key_alias ?? '');
    setLocalProfileAvatarID(entry.avatar_id);
    setLocalProfileError(null);
  };

  const closeLocalProfileModal = () => {
    if (localProfileSaving) return;
    setLocalProfileEntry(null);
    setLocalProfileError(null);
  };

  const saveLocalProfile = async () => {
    if (!localProfileEntry || localProfileSaving) return;
    setLocalProfileSaving(true);
    setLocalProfileError(null);
    try {
      await props.onUpdateLocalProfile(localProfileEntry.participant_id, {
        key_alias: localProfileAlias.trim(),
        avatar_id: localProfileAvatarID,
      });
      setLocalProfileEntry(null);
    } catch (error) {
      setLocalProfileError(error);
    } finally {
      setLocalProfileSaving(false);
    }
  };

  const modalTitle = profileModalStep === 'confirm-join'
    ? t('ranking.join_confirm_title')
    : profileModalStep === 'confirm-pause'
      ? t('ranking.pause_confirm_title')
      : profileModalStep === 'confirm-exit'
        ? t('ranking.exit_confirm_title')
        : t('ranking.participation_title');
  const modalFooter = profileModalStep === 'confirm-join' ? (
    <>
      <Button variant="secondary" appearance="action" onClick={showProfileStep} disabled={props.action === 'join'}>
        {t('common.cancel')}
      </Button>
      <Button data-ranking-confirm-join appearance="action" onClick={() => void handleConfirmJoin()} loading={props.action === 'join'}>
        {t('ranking.join_confirm_action')}
      </Button>
    </>
  ) : profileModalStep === 'confirm-pause' ? (
    <>
      <Button variant="secondary" appearance="action" onClick={showProfileStep} disabled={props.action === 'pause'}>
        {t('common.cancel')}
      </Button>
      <Button
        data-ranking-confirm-pause
        variant="secondary"
        appearance="action"
        onClick={() => void handleConfirmPause()}
        loading={props.action === 'pause'}
      >
        {t('ranking.pause_confirm_action')}
      </Button>
    </>
  ) : profileModalStep === 'confirm-exit' ? (
    <>
      <Button variant="secondary" appearance="action" onClick={showProfileStep} disabled={props.action === 'exit'}>
        {t('common.cancel')}
      </Button>
      <Button
        data-ranking-confirm-exit
        variant="danger"
        appearance="action"
        onClick={() => void handleConfirmExit()}
        loading={props.action === 'exit'}
      >
        {t('ranking.exit_confirm_action')}
      </Button>
    </>
  ) : profileModalStep === 'profile' && props.status?.status === 'disabled' ? (
    <>
      <Button variant="secondary" appearance="action" onClick={closeProfileModal} disabled={props.action !== null}>
        {t('common.cancel')}
      </Button>
      <Button data-ranking-join appearance="action" onClick={handleRequestJoin} disabled={props.action !== null}>
        {t('ranking.join')}
      </Button>
    </>
  ) : profileModalStep === 'profile' && (props.status?.status === 'active' || props.status?.status === 'paused') ? (
    <div className={styles.profileActionFooter}>
      <Button
        data-ranking-exit
        variant="danger"
        appearance="action"
        onClick={() => {
          setProfileModalStep('confirm-exit');
        }}
        disabled={props.action !== null}
      >
        {t('ranking.exit')}
      </Button>
      <div className={styles.profileActionFooterRight}>
        {props.status.status === 'active' ? (
          <>
            <Button
              data-ranking-sync
              variant="secondary"
              appearance="action"
              onClick={() => void runProfileAction('sync', props.onSync)}
              loading={props.action === 'sync'}
              disabled={props.action !== null && props.action !== 'sync'}
            >
              {t('ranking.sync_now')}
            </Button>
            <Button
              data-ranking-pause
              variant="secondary"
              appearance="action"
              onClick={() => {
                setProfileModalStep('confirm-pause');
              }}
              disabled={props.action !== null}
            >
              {t('ranking.pause')}
            </Button>
          </>
        ) : (
          <Button
            data-ranking-resume
            appearance="action"
            onClick={() => void runProfileAction('resume', props.onResume)}
            loading={props.action === 'resume'}
            disabled={props.action !== null && props.action !== 'resume'}
          >
            {t('ranking.resume')}
          </Button>
        )}
        <Button
          data-ranking-close
          variant="secondary"
          appearance="action"
          onClick={closeProfileModal}
          disabled={props.action !== null}
        >
          {t('common.close')}
        </Button>
      </div>
    </div>
  ) : undefined;

  return (
    <section className={styles.page} aria-label={t('ranking.title')}>
      <LeaderboardCard
        period={props.period}
        metric={props.metric}
        metadata={props.metadata}
        metadataLoading={props.metadataLoading}
        metadataError={props.metadataError}
        periodOnline={periodMetadata?.online}
        board={currentBoard}
        scoreExplanationBoard={scoreExplanationBoard}
        loading={props.leaderboardLoading}
        error={props.leaderboardError}
        onRetryMetadata={props.onRetryMetadata}
        onRetry={props.onRetryLeaderboard}
        status={props.status}
        statusLoading={props.statusLoading}
        onOpenProfile={openProfileModal}
        onEditLocalProfile={openLocalProfileModal}
        onPeriodChange={props.onPeriodChange}
        onMetricChange={props.onMetricChange}
        scope={props.scope}
        t={t}
        language={i18n.language}
      />

      <Modal
        open={profileModalStep !== null}
        title={profileModalStep === 'profile' ? (
          <span className={styles.profileModalTitle}>
            <span>{modalTitle}</span>
            <QuestionMarkHelp
              label={t('ranking.privacy_title')}
              description={t('ranking.privacy_description')}
              portal={false}
              className={styles.profilePrivacyHelp}
              tooltipClassName={styles.profilePrivacyTooltip}
              tooltipVisibleClassName={styles.profilePrivacyTooltipVisible}
              buttonProps={{ 'data-ranking-privacy-hint': true }}
              tooltipProps={{ 'data-ranking-privacy-tooltip': true }}
            >
              {t('ranking.privacy_description')}
            </QuestionMarkHelp>
          </span>
        ) : modalTitle}
        onClose={closeProfileModal}
        closeDisabled={props.action !== null}
        footer={modalFooter}
        width={600}
        className={styles.profileModal}
      >
        {profileModalStep === 'profile' ? (
          <div className={styles.profileModalContent}>
            <ParticipationContent
              {...props}
              displayName={displayName}
              avatarID={avatarID}
              profileError={profileError}
              setDisplayName={(value) => {
                setDisplayName(value);
                if (profileError) setProfileError(null);
              }}
              setAvatarID={setAvatarID}
              t={t}
              language={i18n.language}
            />
            {props.actionError ? (
              <div className={styles.errorBox} role="alert" data-ranking-action-feedback="error">
                {formatError(props.actionError, t)}
              </div>
            ) : profileActionSuccess ? (
              <div className={styles.successBox} role="status" data-ranking-action-feedback="success">
                {t(PROFILE_ACTION_SUCCESS_KEYS[profileActionSuccess])}
              </div>
            ) : null}
          </div>
        ) : profileModalStep === 'confirm-join' && pendingProfile ? (
          <div className={styles.confirmBody}>
            <RankingAvatar avatarID={pendingProfile.avatar_id} name={pendingProfile.display_name} className={styles.confirmAvatar} decorative />
            <div>
              <strong>{pendingProfile.display_name}</strong>
              <p>{t('ranking.join_confirm_body')}</p>
            </div>
          </div>
        ) : profileModalStep === 'confirm-pause' ? (
          <p className={styles.confirmText}>{t('ranking.pause_confirm_body')}</p>
        ) : profileModalStep === 'confirm-exit' ? (
          <p className={styles.confirmText}>{t('ranking.exit_confirm_body')}</p>
        ) : null}
      </Modal>

      <Modal
        open={localProfileEntry !== null}
        title={t('ranking.local_profile_edit')}
        onClose={closeLocalProfileModal}
        closeDisabled={localProfileSaving}
        width={600}
        className={styles.profileModal}
        footer={(
          <>
            <Button variant="secondary" appearance="action" onClick={closeLocalProfileModal} disabled={localProfileSaving}>
              {t('common.cancel')}
            </Button>
            <Button
              appearance="action"
              onClick={() => void saveLocalProfile()}
              loading={localProfileSaving}
              data-ranking-local-profile-save
            >
              {t('ranking.local_profile_save')}
            </Button>
          </>
        )}
      >
        {localProfileEntry ? (
          <div className={styles.profileModalContent}>
            <div className={styles.joinForm}>
              <Input
                name="local-ranking-key-alias"
                value={localProfileAlias}
                onChange={(event) => setLocalProfileAlias(event.target.value)}
                label={t('ranking.local_profile_alias')}
                hint={t('ranking.local_profile_alias_hint')}
                placeholder={localProfileEntry.key_alias ? undefined : localProfileEntry.display_name}
                autoComplete="off"
                disabled={localProfileSaving}
              />
              <div className={styles.avatarField}>
                <span className={styles.fieldLabel}>{t('ranking.avatar')}</span>
                <AvatarPicker
                  value={localProfileAvatarID}
                  onChange={setLocalProfileAvatarID}
                  t={t}
                  disabled={localProfileSaving}
                />
              </div>
            </div>
            {localProfileError ? (
              <div className={styles.errorBox} role="alert" data-ranking-local-profile-error>
                {formatError(localProfileError, t)}
              </div>
            ) : null}
          </div>
        ) : null}
      </Modal>
    </section>
  );
}

interface ParticipationContentProps extends RankingPageProps {
  displayName: string;
  avatarID: number;
  profileError: RankingProfileError | null;
  setDisplayName: (value: string) => void;
  setAvatarID: (value: number) => void;
  t: Translate;
  language: string;
}

function ParticipationContent({
  status,
  statusLoading,
  statusError,
  action,
  displayName,
  avatarID,
  profileError,
  setDisplayName,
  setAvatarID,
  onJoin,
  onRetryStatus,
  t,
  language,
}: ParticipationContentProps) {
  if (statusLoading && !status) {
    return <LoadingState text={t('ranking.status_loading')} />;
  }
  if (!status) {
    return (
      <ErrorState error={statusError} onRetry={onRetryStatus} t={t} />
    );
  }
  if (status.status === 'deleted') {
    return (
      <div className={styles.deletedState}>
        <strong>{t('ranking.deleted_title')}</strong>
        <p>{t('ranking.deleted_description')}</p>
      </div>
    );
  }
  if (status.status === 'joining') {
    const profile = { display_name: status.display_name ?? '', avatar_id: status.avatar_id ?? 0 };
    return (
      <div className={styles.activeState}>
        <ProfileIdentity status={status} />
        <p>{t('ranking.joining_description')}</p>
        <Button
          appearance="action"
          onClick={() => void onJoin(profile)}
          loading={action === 'join'}
          disabled={!profile.display_name || !profile.avatar_id}
        >
          {t('ranking.join_retry')}
        </Button>
      </div>
    );
  }
  if (status.status === 'active' || status.status === 'paused') {
    return (
      <div className={styles.activeState}>
        <ProfileIdentity status={status} />
        {status.status === 'paused' ? <div className={styles.pausedNotice}>{t('ranking.paused_description')}</div> : null}
        <dl className={styles.syncFacts}>
          <div><dt>{t('ranking.last_sync')}</dt><dd>{formatDateTime(status.last_successful_sync_at, language, t)}</dd></div>
          <div><dt>{t('ranking.last_complete_day')}</dt><dd>{status.last_successful_complete_day || '—'}</dd></div>
        </dl>
        {status.last_error && <div className={styles.noticeBox}>{t('ranking.last_error')}: {status.last_error}</div>}
      </div>
    );
  }

  return (
    <div className={styles.joinForm}>
      <Input
        name="ranking-display-name"
        value={displayName}
        onChange={(event) => setDisplayName(event.target.value)}
        label={t('ranking.display_name')}
        hint={t('ranking.display_name_hint')}
        error={profileError ? t(profileErrorKey(profileError)) : undefined}
        maxLength={RANKING_DISPLAY_NAME_MAX_LENGTH}
        autoComplete="off"
      />
      <div className={styles.avatarField}>
        <span className={styles.fieldLabel}>{t('ranking.avatar')}</span>
        <AvatarPicker value={avatarID} onChange={setAvatarID} t={t} />
      </div>
    </div>
  );
}

function AvatarPicker({ value, onChange, t, disabled = false }: {
  value: number;
  onChange: (value: number) => void;
  t: Translate;
  disabled?: boolean;
}) {
  const handleAvatarKeyDown = (event: KeyboardEvent<HTMLButtonElement>, currentID: number) => {
    const currentIndex = AVATAR_IDS.indexOf(currentID);
    let nextIndex = currentIndex;
    switch (event.key) {
      case 'ArrowRight':
      case 'ArrowDown':
        nextIndex = (currentIndex + 1) % AVATAR_IDS.length;
        break;
      case 'ArrowLeft':
      case 'ArrowUp':
        nextIndex = (currentIndex - 1 + AVATAR_IDS.length) % AVATAR_IDS.length;
        break;
      case 'Home':
        nextIndex = 0;
        break;
      case 'End':
        nextIndex = AVATAR_IDS.length - 1;
        break;
      default:
        return;
    }
    event.preventDefault();
    const nextID = AVATAR_IDS[nextIndex];
    onChange(nextID);
    event.currentTarget.parentElement
      ?.querySelector<HTMLButtonElement>(`[data-ranking-avatar-option="${nextID}"]`)
      ?.focus();
  };

  return (
    <div className={styles.avatarGrid} role="radiogroup" aria-label={t('ranking.avatar')}>
      {AVATAR_IDS.map((id) => (
        <button
          key={id}
          type="button"
          role="radio"
          aria-checked={value === id}
          aria-label={t('ranking.avatar_option', { id })}
          tabIndex={value === id ? 0 : -1}
          className={`${styles.avatarOption} ${value === id ? styles.avatarOptionActive : ''}`.trim()}
          onClick={() => onChange(id)}
          onKeyDown={(event) => handleAvatarKeyDown(event, id)}
          data-ranking-avatar-option={id}
          disabled={disabled}
        >
          <RankingAvatar avatarID={id} name={t('ranking.avatar_option', { id })} decorative />
        </button>
      ))}
    </div>
  );
}

function ProfileIdentity({ status }: { status: RankingStatusResponse }) {
  const { t } = useTranslation();
  return (
    <div className={styles.profileIdentity}>
      <RankingAvatar avatarID={status.avatar_id ?? 1} name={status.display_name ?? ''} className={styles.profileAvatar} decorative />
      <div>
        <strong>{status.display_name}</strong>
        <span>{t(`ranking.status_${status.status}`)}</span>
      </div>
    </div>
  );
}

interface LeaderboardCardProps {
  scope: RankingScope;
  period: RankingPeriod;
  metric: RankingMetric;
  metadata: RankingMetadataResponse | null;
  metadataLoading: boolean;
  metadataError: unknown;
  periodOnline?: boolean;
  board: RankingLeaderboardResponse | null;
  scoreExplanationBoard: RankingLeaderboardResponse | null;
  loading: boolean;
  error: unknown;
  onRetryMetadata: AsyncAction;
  onRetry: AsyncAction;
  status: RankingStatusResponse | null;
  statusLoading: boolean;
  onOpenProfile: () => void;
  onEditLocalProfile: (entry: RankingLeaderboardEntry) => void;
  onPeriodChange: (period: RankingPeriod) => void;
  onMetricChange: (metric: RankingMetric) => void;
  t: Translate;
  language: string;
}

function LeaderboardCard({
  scope,
  period,
  metric,
  metadata,
  metadataLoading,
  metadataError,
  periodOnline,
  board,
  scoreExplanationBoard,
  loading,
  error,
  onRetryMetadata,
  onRetry,
  status,
  statusLoading,
  onOpenProfile,
  onEditLocalProfile,
  onPeriodChange,
  onMetricChange,
  t,
  language,
}: LeaderboardCardProps) {
  const rows = useMemo(() => board?.entries.slice(0, 100) ?? [], [board]);
  const scoreExplanation = resolveScoreExplanation(scoreExplanationBoard, metric, language);
  const hasRankingProfile = status?.status === 'active' || status?.status === 'paused';
  const profileActionAriaLabel = hasRankingProfile && status.display_name
    ? `${status.display_name} · ${t('ranking.profile_action')}`
    : hasRankingProfile
      ? t('ranking.profile_action')
      : undefined;
  return (
    <article className={`card ${styles.leaderboardCard}`.trim()} aria-busy={loading}>
      <header className={styles.leaderboardHeader}>
        <div className={styles.leaderboardTitle} data-ranking-header-title>
          <div className="keeper-card-title-track">
            <div
              className={`${styles.metricTitleHeading} keeper-card-title`}
              role="heading"
              aria-level={2}
              data-ranking-metric-title
            >
              <RankingMetricSelect metric={metric} onMetricChange={onMetricChange} />
            </div>
            {scoreExplanation ? (
              <span className={styles.scoreExplanationSlot} data-ranking-score-explanation-slot>
                <QuestionMarkHelp
                  label={t('ranking.score_explanation_label')}
                  description={scoreExplanation}
                  portal={false}
                  className={styles.profilePrivacyHelp}
                  buttonClassName={styles.scoreExplanationHint}
                  tooltipClassName={styles.profilePrivacyTooltip}
                  tooltipVisibleClassName={styles.profilePrivacyTooltipVisible}
                  buttonProps={{ 'data-ranking-score-explanation': true }}
                  tooltipProps={{ 'data-ranking-score-explanation-tooltip': true }}
                >
                  {scoreExplanation}
                </QuestionMarkHelp>
              </span>
            ) : null}
            <div className={styles.leaderboardHeaderToolbar} data-ranking-header-toolbar>
              <RankingToolbar
                period={period}
                onPeriodChange={onPeriodChange}
              />
            </div>
          </div>
          {board && (
            <div className={styles.boardMeta}>
              {board.stale && <span className={styles.staleBadge} data-ranking-stale>{t('ranking.stale')} · {board.period_key}</span>}
              <span>{t('ranking.updated_at', { time: formatDateTime(board.generated_at, language, t) })}</span>
            </div>
          )}
        </div>
        {scope === 'community' ? (
          <div
            className={styles.leaderboardHeaderActions}
            data-ranking-profile-action-shell
          >
            <MainActionButton
              shellClassName={`${styles.profileActionShell} ${hasRankingProfile ? styles.profileActionShellActive : ''}`.trim()}
              onClick={onOpenProfile}
              disabled={statusLoading && !status}
              aria-label={profileActionAriaLabel}
              data-ranking-profile-action
            >
              {hasRankingProfile ? (
                <>
                  <RankingAvatar avatarID={status.avatar_id ?? 1} name={status.display_name ?? ''} className={styles.profileActionAvatar} decorative />
                  <span className={styles.profileActionName} data-ranking-profile-name>{status.display_name || t('ranking.profile_action')}</span>
                </>
              ) : status?.status === 'joining'
                ? t('ranking.join_retry')
                : status?.status === 'deleted'
                  ? t('ranking.status_deleted')
                  : status?.status === 'disabled'
                    ? t('ranking.join')
                    : t('ranking.profile_action')}
            </MainActionButton>
          </div>
        ) : null}
      </header>
      {metadataError && board ? (
        <div className={styles.metadataWarning} role="alert" data-ranking-metadata-warning>
          <span>{t('ranking.refresh_failed')}</span>
          <Button variant="secondary" appearance="action" onClick={() => void onRetryMetadata()}>
            {t('common.retry')}
          </Button>
        </div>
      ) : null}
      {metadataLoading && !metadata && !board ? (
        <LoadingState text={t('ranking.metadata_loading')} />
      ) : metadataError && !metadata && !board ? (
        <ErrorState error={metadataError} onRetry={onRetryMetadata} t={t} />
      ) : periodOnline === false ? (
        <EmptyState title={t('ranking.offline_title')} description={t('ranking.offline_description')} />
      ) : loading && !board ? (
        <LoadingState text={t('ranking.leaderboard_loading')} />
      ) : error && !board ? (
        <ErrorState error={error} onRetry={onRetry} t={t} />
      ) : rows.length === 0 ? (
        <EmptyState
          title={t(scope === 'local' ? 'ranking.local_empty_title' : 'ranking.empty_title')}
          description={t(scope === 'local' ? 'ranking.local_empty_description' : 'ranking.empty_description')}
        />
      ) : (
        <RankingLeaderboardResults
          scope={scope}
          metric={metric}
          entries={rows}
          onEditLocalProfile={onEditLocalProfile}
        />
      )}
    </article>
  );
}

function LoadingState({ text }: { text: string }) {
  return <div className={styles.loadingState}><LoadingSpinner size={20} /><span>{text}</span></div>;
}

function ErrorState({ error, onRetry, t }: { error: unknown; onRetry: AsyncAction; t: Translate }) {
  return (
    <div className={styles.errorState} role="alert">
      <p>{formatError(error, t)}</p>
      <Button variant="secondary" appearance="action" onClick={() => void onRetry()}>{t('common.retry')}</Button>
    </div>
  );
}

function EmptyState({ title, description }: { title: string; description: string }) {
  return <div className={styles.emptyState}><strong>{title}</strong><p>{description}</p></div>;
}

function formatDateTime(value: string | undefined, language: string, t: Translate): string {
  if (!value) return t('ranking.never_synced');
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short', hour12: false }).format(date);
}
