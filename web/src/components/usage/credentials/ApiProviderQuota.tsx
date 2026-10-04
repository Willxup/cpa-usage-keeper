import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { fetchAdminProviderQuota, type ViewerQuotaAccount } from "@/lib/api";
import { startQuotaPolling } from "@/lib/quotaPolling";
import { QuotaAccounts } from "../QuotaAccounts";

export function ApiProviderQuota() {
  const { t } = useTranslation();
  const [accounts, setAccounts] = useState<ViewerQuotaAccount[]>([]);
  const [error, setError] = useState(false);
  useEffect(
    () =>
      startQuotaPolling(async (signal) => {
        try {
          const data = await fetchAdminProviderQuota(signal);
          if (!signal.aborted) {
            setAccounts(
              data.accounts.filter(
                (account) => account.kind === "api" && account.rows.length > 0,
              ),
            );
            setError(false);
          }
        } catch (failure) {
          if (!signal.aborted) setError(true);
          throw failure;
        }
      }),
    [],
  );
  if (!accounts.length && !error) return null;
  return (
    <details open>
      <summary>{t("key_quota.api_provider_title")}</summary>
      {error && <p role="alert">{t("key_quota.error")}</p>}
      <QuotaAccounts accounts={accounts} />
    </details>
  );
}
