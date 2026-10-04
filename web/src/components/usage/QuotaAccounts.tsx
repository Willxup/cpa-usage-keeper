import { useTranslation } from "react-i18next";
import { Card } from "@/components/ui/Card";
import type { ViewerQuotaAccount, ViewerQuotaRow } from "@/lib/api";
import { QuotaFreshness } from "./QuotaFreshness";
import styles from "./QuotaAccounts.module.scss";
export function quotaRemaining(row: ViewerQuotaRow): string {
  if (row.remaining_percent != null)
    return `${Math.max(0, Math.min(100, row.remaining_percent)).toLocaleString(undefined, { maximumFractionDigits: 1 })}%`;
  if (row.remaining != null)
    return row.remaining.toLocaleString(undefined, {
      maximumFractionDigits: 2,
    });
  return "—";
}

export function QuotaAccounts({
  accounts,
}: {
  accounts: ViewerQuotaAccount[];
}) {
  const { t } = useTranslation();
  return (
    <div className={styles.accounts}>
      {accounts.map((account) => (
        <Card key={account.label}>
          <h2>{account.label}</h2>
          <p
            className={
              account.status === "available" ? undefined : styles.notice
            }
          >
            {t(`key_quota.${account.status}`)}
          </p>
          {account.updated_at && (
            <p>
              {t("key_quota.updated")}:{" "}
              {new Date(account.updated_at).toLocaleString()}
            </p>
          )}
          {account.rows.map((row, index) => (
            <div className={styles.row} key={index}>
              <div>
                <strong>{row.label}</strong>
                {row.metric && (
                  <span className={styles.metric}>{row.metric}</span>
                )}
              </div>
              <div>
                {t("key_quota.remaining")}:{" "}
                <strong>{quotaRemaining(row)}</strong>
                {row.remaining_percent == null &&
                  row.limit != null &&
                  ` / ${row.limit.toLocaleString()}`}
                <QuotaFreshness
                  capturedAt={row.captured_at ?? account.updated_at}
                  source={row.source}
                  account={account.label}
                  resetAt={row.reset_at}
                  stale={row.stale}
                />
              </div>
              {row.remaining_percent != null && (
                <progress
                  max={100}
                  value={Math.max(0, Math.min(100, row.remaining_percent))}
                  aria-label={`${row.label}: ${t("key_quota.remaining")}`}
                />
              )}
              {row.limit_reached && (
                <p className={styles.notice}>{t("key_quota.exhausted")}</p>
              )}
              {row.reset_at && (
                <p>
                  {t("key_quota.resets")}:{" "}
                  {new Date(row.reset_at).toLocaleString()}
                </p>
              )}
            </div>
          ))}
        </Card>
      ))}
    </div>
  );
}
