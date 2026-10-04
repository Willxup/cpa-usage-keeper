import { QuotaAccounts } from "@/components/usage/QuotaAccounts";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { KeyViewerShell } from "@/features/key-viewer/KeyViewerShell";
import type { KeyViewerPath } from "@/features/key-viewer/navigation";
import type { AuthSessionAPIKeySummary } from "@/lib/types";
import {
  ApiError,
  fetchKeyQuota,
  fetchReadOnlyQuota,
  type ViewerQuotaAccount,
} from "@/lib/api";
import { startQuotaPolling } from "@/lib/quotaPolling";

export { quotaRemaining } from "@/components/usage/QuotaAccounts";

export function KeyQuotaPage({
  readOnly = false,
  apiKey,
  onNavigate,
  onAuthRequired,
}: {
  readOnly?: boolean;
  apiKey?: AuthSessionAPIKeySummary;
  onNavigate: (path: KeyViewerPath) => void;
  onAuthRequired?: () => void;
}) {
  const { t } = useTranslation();
  const [accounts, setAccounts] = useState<ViewerQuotaAccount[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);
  const inFlight = useRef(false);
  const refresh = useCallback(
    async (signal?: AbortSignal) => {
      if (inFlight.current) return;
      inFlight.current = true;
      setLoading(true);
      try {
        const data = await (readOnly
          ? fetchReadOnlyQuota(signal)
          : fetchKeyQuota(signal));
        if (!signal?.aborted) {
          setAccounts(data.accounts);
          setError(false);
        }
      } catch (err) {
        if (!signal?.aborted) {
          if (err instanceof ApiError && err.status === 401) onAuthRequired?.();
          setError(true);
        }
      } finally {
        inFlight.current = false;
        setLoading(false);
      }
    },
    [onAuthRequired, readOnly],
  );
  useEffect(() => startQuotaPolling(refresh), [refresh]);
  return (
    <KeyViewerShell
      readOnly={readOnly}
      activePage="quota"
      apiKey={apiKey}
      onNavigate={onNavigate}
      onAuthRequired={onAuthRequired}
      onRefresh={() => void refresh()}
      refreshing={loading}
      refreshDisabled={loading}
    >
      <p>{t("key_quota.shared")}</p>
      {error && <p role="alert">{t("key_quota.error")}</p>}
      {!loading && !error && accounts.length === 0 && (
        <p>{t("key_quota.empty")}</p>
      )}
      <QuotaAccounts accounts={accounts} />
    </KeyViewerShell>
  );
}
