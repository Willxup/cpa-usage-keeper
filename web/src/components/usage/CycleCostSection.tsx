import { useCallback, useEffect, useState, type CSSProperties } from 'react';
import { ApiError, fetchCycleCostBreakdown, fetchCycleCostCurrent, fetchCycleCostHistory } from '@/lib/api';
import type { CycleCostBreakdown, CycleCostSummary } from '@/lib/types';

interface CycleCostSectionProps {
  onAuthRequired?: () => void;
}

const PROVIDER = 'codex';
const WEEKLY_WINDOW_SECONDS = 604800;
const NUMBER_FORMAT_USD = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 2 });
const NUMBER_FORMAT_INT = new Intl.NumberFormat('en-US');
const PERCENT_FORMAT = new Intl.NumberFormat('en-US', { maximumFractionDigits: 1 });
const DATETIME_FORMAT = new Intl.DateTimeFormat('en-US', { year: 'numeric', month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit' });

function formatRelative(ms: number): string {
  if (ms <= 0) return 'ended';
  const totalMinutes = Math.floor(ms / 60_000);
  const days = Math.floor(totalMinutes / 1440);
  const hours = Math.floor((totalMinutes % 1440) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return `${days}d ${hours}h left`;
  if (hours > 0) return `${hours}h ${minutes}m left`;
  return `${minutes}m left`;
}

function formatDateTime(value?: string): string {
  if (!value) return '—';
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) return value;
  return DATETIME_FORMAT.format(new Date(parsed));
}

function summaryKey(item: CycleCostSummary): string {
  return `${item.provider}::${item.authIndex}::${item.cycleEnd}`;
}

export function CycleCostSection({ onAuthRequired }: CycleCostSectionProps) {
  const [currentItems, setCurrentItems] = useState<CycleCostSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedAuthIndex, setSelectedAuthIndex] = useState<string | null>(null);
  const [history, setHistory] = useState<CycleCostSummary[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyError, setHistoryError] = useState<string | null>(null);
  const [breakdown, setBreakdown] = useState<CycleCostBreakdown | null>(null);
  const [breakdownLoading, setBreakdownLoading] = useState(false);
  const [breakdownError, setBreakdownError] = useState<string | null>(null);

  const handleApiError = useCallback((err: unknown, fallback: string, setter: (value: string | null) => void) => {
    if (err instanceof ApiError && err.status === 401) {
      onAuthRequired?.();
      return;
    }
    setter(err instanceof Error ? err.message : fallback);
  }, [onAuthRequired]);

  const loadCurrent = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetchCycleCostCurrent(PROVIDER);
      setCurrentItems(response.items);
    } catch (err) {
      handleApiError(err, 'Failed to load current cycle costs', setError);
    } finally {
      setLoading(false);
    }
  }, [handleApiError]);

  const loadHistoryFor = useCallback(async (authIndex: string) => {
    setHistoryLoading(true);
    setHistoryError(null);
    try {
      const response = await fetchCycleCostHistory(authIndex, PROVIDER, 12);
      setHistory(response.items);
    } catch (err) {
      handleApiError(err, 'Failed to load cycle history', setHistoryError);
    } finally {
      setHistoryLoading(false);
    }
  }, [handleApiError]);

  const loadBreakdownFor = useCallback(async (authIndex: string, cycleEnd: string) => {
    setBreakdownLoading(true);
    setBreakdownError(null);
    try {
      const response = await fetchCycleCostBreakdown(authIndex, cycleEnd, PROVIDER);
      setBreakdown(response);
    } catch (err) {
      handleApiError(err, 'Failed to load cycle breakdown', setBreakdownError);
    } finally {
      setBreakdownLoading(false);
    }
  }, [handleApiError]);

  useEffect(() => {
    void loadCurrent();
  }, [loadCurrent]);

  useEffect(() => {
    if (!selectedAuthIndex) return;
    void loadHistoryFor(selectedAuthIndex);
    const current = currentItems.find((item) => item.authIndex === selectedAuthIndex);
    if (current) {
      void loadBreakdownFor(current.authIndex, current.cycleEnd);
    }
  }, [currentItems, loadBreakdownFor, loadHistoryFor, selectedAuthIndex]);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <h2 style={{ margin: 0 }}>Cycle Cost (Codex Weekly)</h2>
        <button type="button" onClick={() => { void loadCurrent(); }} disabled={loading}>
          {loading ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>
      <p style={{ margin: 0, color: '#666', fontSize: 13 }}>
        Each row is one Codex account&apos;s 7-day weekly-limit cycle. The cycle window is taken from the latest
        quota refresh; refresh quotas on the Credentials tab to populate this view. The $ value applies the
        current pricing table to usage events that occurred inside the cycle.
      </p>
      {error && <div style={{ color: '#c00', padding: 8, background: '#fee', borderRadius: 4 }}>{error}</div>}
      <div style={{ overflowX: 'auto' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr style={{ background: '#f5f5f5', textAlign: 'left' }}>
              <th style={cellHeaderStyle}>Identity</th>
              <th style={cellHeaderStyle}>Auth Index</th>
              <th style={cellHeaderStyle}>Cycle Window</th>
              <th style={cellHeaderStyle}>Time Left</th>
              <th style={cellHeaderStyle}>Used %</th>
              <th style={cellHeaderStyle}>Est. $ This Cycle</th>
              <th style={cellHeaderStyle}>Tokens</th>
              <th style={cellHeaderStyle}>Requests</th>
              <th style={cellHeaderStyle}>Last Captured</th>
              <th style={cellHeaderStyle}>Action</th>
            </tr>
          </thead>
          <tbody>
            {currentItems.length === 0 && !loading && (
              <tr>
                <td colSpan={10} style={{ ...cellBodyStyle, color: '#888', fontStyle: 'italic', textAlign: 'center' }}>
                  No cycle snapshots yet. Run a quota refresh on the Credentials tab for any Codex account, then come back here.
                </td>
              </tr>
            )}
            {currentItems.map((item) => {
              const cycleEndMs = Date.parse(item.cycleEnd);
              const timeLeft = Number.isFinite(cycleEndMs) ? formatRelative(cycleEndMs - Date.now()) : '—';
              const selected = selectedAuthIndex === item.authIndex;
              return (
                <tr key={summaryKey(item)} style={{ background: selected ? '#eef6ff' : 'transparent' }}>
                  <td style={cellBodyStyle}>{item.identityName || '—'}</td>
                  <td style={{ ...cellBodyStyle, fontFamily: 'monospace', fontSize: 12 }}>{item.authIndex}</td>
                  <td style={cellBodyStyle}>{formatDateTime(item.cycleStart)} → {formatDateTime(item.cycleEnd)}</td>
                  <td style={cellBodyStyle}>{timeLeft}</td>
                  <td style={cellBodyStyle}>{PERCENT_FORMAT.format(item.usedPercent)}%</td>
                  <td style={{ ...cellBodyStyle, fontWeight: 600 }}>
                    {NUMBER_FORMAT_USD.format(item.totalUsd)}
                    {item.pricingMissing && <span title="Some models in this cycle have no pricing configured" style={{ color: '#cc6600', marginLeft: 4 }}>⚠</span>}
                  </td>
                  <td style={cellBodyStyle}>{NUMBER_FORMAT_INT.format(item.totalTokens)}</td>
                  <td style={cellBodyStyle}>{NUMBER_FORMAT_INT.format(item.requestCount)}</td>
                  <td style={cellBodyStyle}>{formatDateTime(item.lastCapturedAt)}</td>
                  <td style={cellBodyStyle}>
                    <button type="button" onClick={() => setSelectedAuthIndex(item.authIndex)}>
                      {selected ? 'Selected' : 'Details'}
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {selectedAuthIndex && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12, padding: 12, border: '1px solid #e0e0e0', borderRadius: 6 }}>
          <h3 style={{ margin: 0 }}>Detail · {selectedAuthIndex}</h3>
          <section>
            <h4 style={{ margin: '0 0 6px' }}>Current Cycle Breakdown by Model</h4>
            {breakdownLoading && <div style={{ color: '#888' }}>Loading breakdown…</div>}
            {breakdownError && <div style={{ color: '#c00' }}>{breakdownError}</div>}
            {breakdown && !breakdownLoading && (
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                <thead>
                  <tr style={{ background: '#fafafa', textAlign: 'left' }}>
                    <th style={cellHeaderStyle}>Model</th>
                    <th style={cellHeaderStyle}>Input</th>
                    <th style={cellHeaderStyle}>Output</th>
                    <th style={cellHeaderStyle}>Cached</th>
                    <th style={cellHeaderStyle}>Reasoning</th>
                    <th style={cellHeaderStyle}>Requests</th>
                    <th style={cellHeaderStyle}>$ Cost</th>
                  </tr>
                </thead>
                <tbody>
                  {breakdown.models.length === 0 && (
                    <tr><td colSpan={7} style={{ ...cellBodyStyle, color: '#888', textAlign: 'center' }}>No events in this cycle.</td></tr>
                  )}
                  {breakdown.models.map((entry) => (
                    <tr key={entry.model}>
                      <td style={cellBodyStyle}>{entry.model}{entry.modelAlias ? ` (${entry.modelAlias})` : ''}</td>
                      <td style={cellBodyStyle}>{NUMBER_FORMAT_INT.format(entry.inputTokens)}</td>
                      <td style={cellBodyStyle}>{NUMBER_FORMAT_INT.format(entry.outputTokens)}</td>
                      <td style={cellBodyStyle}>{NUMBER_FORMAT_INT.format(entry.cachedTokens + entry.cacheReadTokens)}</td>
                      <td style={cellBodyStyle}>{NUMBER_FORMAT_INT.format(entry.reasoningTokens)}</td>
                      <td style={cellBodyStyle}>{NUMBER_FORMAT_INT.format(entry.requestCount)}</td>
                      <td style={{ ...cellBodyStyle, fontWeight: 600 }}>
                        {NUMBER_FORMAT_USD.format(entry.usdCost)}
                        {entry.pricingMissing && <span title="Pricing not configured for this model" style={{ color: '#cc6600', marginLeft: 4 }}>⚠</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>
          <section>
            <h4 style={{ margin: '0 0 6px' }}>Historical Cycles (most recent first)</h4>
            {historyLoading && <div style={{ color: '#888' }}>Loading history…</div>}
            {historyError && <div style={{ color: '#c00' }}>{historyError}</div>}
            {!historyLoading && !historyError && (
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                <thead>
                  <tr style={{ background: '#fafafa', textAlign: 'left' }}>
                    <th style={cellHeaderStyle}>Cycle Window</th>
                    <th style={cellHeaderStyle}>State</th>
                    <th style={cellHeaderStyle}>$ Cost</th>
                    <th style={cellHeaderStyle}>Tokens</th>
                    <th style={cellHeaderStyle}>Requests</th>
                  </tr>
                </thead>
                <tbody>
                  {history.length === 0 && (
                    <tr><td colSpan={5} style={{ ...cellBodyStyle, color: '#888', textAlign: 'center' }}>No historical cycles yet. The first sealed cycle takes one window ({WEEKLY_WINDOW_SECONDS / 86400} days) to accumulate after first quota refresh.</td></tr>
                  )}
                  {history.map((row) => (
                    <tr key={summaryKey(row)}>
                      <td style={cellBodyStyle}>{formatDateTime(row.cycleStart)} → {formatDateTime(row.cycleEnd)}</td>
                      <td style={cellBodyStyle}>{row.sealed ? 'sealed' : 'current'}</td>
                      <td style={{ ...cellBodyStyle, fontWeight: 600 }}>
                        {NUMBER_FORMAT_USD.format(row.totalUsd)}
                        {row.pricingMissing && <span title="Some models have no pricing" style={{ color: '#cc6600', marginLeft: 4 }}>⚠</span>}
                      </td>
                      <td style={cellBodyStyle}>{NUMBER_FORMAT_INT.format(row.totalTokens)}</td>
                      <td style={cellBodyStyle}>{NUMBER_FORMAT_INT.format(row.requestCount)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>
        </div>
      )}
    </div>
  );
}

const cellHeaderStyle: CSSProperties = {
  padding: '8px 10px',
  borderBottom: '1px solid #ddd',
  fontWeight: 600,
};

const cellBodyStyle: CSSProperties = {
  padding: '8px 10px',
  borderBottom: '1px solid #f0f0f0',
};
