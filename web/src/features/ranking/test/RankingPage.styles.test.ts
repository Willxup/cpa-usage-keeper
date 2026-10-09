import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const styles = readFileSync(new URL('../RankingPage.module.scss', import.meta.url), 'utf8');
const mobile = styles.slice(styles.indexOf('@include mobile'), styles.indexOf('@media (prefers-reduced-motion'));

const rule = (selector: string) => {
  const start = styles.indexOf(selector);
  expect(start).toBeGreaterThanOrEqual(0);
  const open = styles.indexOf('{', start);
  const close = styles.indexOf('\n}', open);
  expect(open).toBeGreaterThan(start);
  expect(close).toBeGreaterThan(open);
  return styles.slice(open + 1, close);
};

describe('Ranking table context styles', () => {
  it('keeps the header and first two columns visible without introducing a fixed table height', () => {
    const tableHeader = rule('.table thead th');
    const rankColumn = rule('.rankColumn');
    const participantColumn = rule('.participantColumn');

    expect(tableHeader).toContain('position: sticky;');
    expect(tableHeader).toContain('top: 0;');
    expect(rankColumn).toContain('position: sticky;');
    expect(rankColumn).toContain('left: 0;');
    expect(participantColumn).toContain('position: sticky;');
    expect(participantColumn).toContain('left: var(--ranking-rank-column-width);');
    expect(rule('.tableScroll')).not.toMatch(/(?:height|max-height):/);
  });

  it('gives editable local avatars visible keyboard focus', () => {
    const trigger = rule('.localProfileAvatarButton');
    expect(trigger).toContain('&:focus-visible');
    expect(trigger).toContain('outline: 2px solid var(--primary-color);');
  });

  it('keeps an accessible focus ring on the otherwise frameless title select', () => {
    const titleMetric = rule('.titleMetricSelect');
    expect(titleMetric).toContain("&[aria-expanded='true']");
    expect(titleMetric).toContain('box-shadow: 0 0 0 3px');
  });

  it('shows the tooltip on hover and focus while keeping hidden content noninteractive', () => {
    const title = rule('.profileModalTitle');
    const help = rule('.profilePrivacyHelp');
    const tooltip = rule('.profilePrivacyTooltip');
    expect(title).toContain('position: relative;');
    expect(help).not.toContain('position: relative;');
    expect(tooltip).toContain('left: 0;');
    expect(tooltip).toContain('max-width: min(340px, calc(100vw - 64px));');
    expect(tooltip).toContain('opacity: 0;');
    expect(tooltip).toContain('pointer-events: none;');
    expect(styles).toContain('.profilePrivacyHelp:hover .profilePrivacyTooltip');
    expect(styles).toContain('.profilePrivacyHelp:focus-within .profilePrivacyTooltip');
    expect(styles).toContain('.profilePrivacyTooltipVisible');
  });

  it('keeps long title controls clipped and lets the title track wrap only when necessary', () => {
    const card = rule('.leaderboardCard:global(.card)');
    const title = rule('.metricTitleHeading');
    const stackedStart = styles.indexOf('@mixin ranking-header-stacked');
    const stackedEnd = styles.indexOf('\n}', stackedStart);
    const stacked = styles.slice(stackedStart, stackedEnd);
    const containerStart = styles.indexOf('@container ranking-card (max-width: 760px)');
    const containerEnd = styles.indexOf('\n}', containerStart);
    const container = styles.slice(containerStart, containerEnd);

    expect(card).toContain('container-name: ranking-card;');
    expect(card).toContain('container-type: inline-size;');
    expect(title).toContain('overflow: hidden;');
    expect(rule('.leaderboardTitle :global(.keeper-card-title-track)')).toContain('flex-wrap: wrap;');
    expect(stacked).toMatch(/\.leaderboardHeader\s*\{[\s\S]*?grid-template-areas:\s*'title profile';/);
    expect(stacked).toMatch(
      /\.leaderboardHeaderActions\s*\{[\s\S]*?justify-content:\s*flex-end;[\s\S]*?justify-self:\s*end;[\s\S]*?margin-right:\s*0;/,
    );
    expect(stacked).not.toContain("'toolbar toolbar'");
    expect(container).toContain('@include ranking-header-stacked;');
  });

  it('keeps only the active profile avatar visible on mobile while preserving the accessible name', () => {
    expect(mobile).toContain('@include ranking-header-stacked;');
    expect(mobile).toMatch(/\.profileActionName\s*\{[\s\S]*?display:\s*none;/);
  });

  it('lets the sticky participant column follow the display name width on mobile', () => {
    expect(mobile).toMatch(
      /\.participantColumn,\s*\.participantCell\s*\{[\s\S]*?min-width:\s*0;/,
    );
  });

  it('allows the avatar list to scroll within the desktop profile modal', () => {
    const modalAvatars = rule('.profileModal .avatarGrid');

    expect(modalAvatars).toContain('overflow-y: auto;');
    const modalBody = rule('.profileModal :global(.modal-body)');
    expect(modalBody).toContain('overflow: hidden;');
    expect(modalBody).toContain('max-height: none;');
  });

  it('restores modal body scrolling on short mobile viewports', () => {
    expect(mobile).toMatch(/\.profileModal\s+:global\(\.modal-body\)\s*\{[\s\S]*?overflow:\s*auto;/);
    expect(mobile).toMatch(/\.profileModal\s+:global\(\.modal-body\)\s*\{[\s\S]*?max-height:\s*min\(60dvh,/);
  });

  it('adapts profile avatar columns to narrow mobile modal widths', () => {
    expect(mobile).toMatch(
      /\.profileModal\s+\.avatarGrid\s*\{[\s\S]*?grid-template-columns:\s*repeat\(auto-fill,\s*minmax\(44px,\s*1fr\)\);/,
    );
  });

  it('separates the destructive action from the normal profile actions and stacks cleanly on mobile', () => {
    const footer = rule('.profileActionFooter');
    expect(footer).toContain('display: flex;');
    expect(footer).toContain('justify-content: space-between;');
    expect(footer).toContain('width: 100%;');
    expect(rule('.profileActionFooterRight')).toContain('display: flex;');
    expect(rule('.profileActionFooterRight')).toContain('flex-wrap: wrap;');

    expect(mobile).toContain('.profileActionFooter');
    expect(mobile).toContain('flex-direction: column-reverse;');
  });
});
