import { useEffect, useId, useState } from "react";
import { IconRefreshCw } from "@/components/ui/icons";

const quotaSourceLabel = (source?: string) =>
  ({
    api_response_headers: "API response headers",
    usage_header: "API response headers",
    websocket_event: "WebSocket event",
    scheduled: "Scheduled provider query",
    scheduled_provider_query: "Scheduled provider query",
    manual: "Manual provider query",
    manual_provider_query: "Manual provider query",
    inspection: "Manual provider query",
    cache_backfill: "Scheduled provider query",
  })[source ?? ""] ?? "Unknown source";

export function QuotaFreshness({
  capturedAt,
  source,
  account,
  resetAt,
  stale,
}: {
  capturedAt?: string;
  source?: string;
  account: string;
  resetAt?: string;
  stale?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [now, setNow] = useState(Date.now);
  const tooltipId = useId();
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 10_000);
    return () => clearInterval(timer);
  }, []);
  const at = capturedAt ? Date.parse(capturedAt) : NaN;
  const seconds = Math.max(0, Math.floor((now - at) / 1000));
  const age = Number.isFinite(at)
    ? seconds < 60
      ? `${seconds}s ago`
      : seconds < 3600
        ? `${Math.floor(seconds / 60)}m ago`
        : `${Math.floor(seconds / 3600)}h ago`
    : "Unknown age";
  const old = Boolean(
    stale ||
    !Number.isFinite(at) ||
    seconds > 900 ||
    (resetAt && Date.parse(resetAt) <= now),
  );
  const details = `${account}\n${quotaSourceLabel(source)}\n${Number.isFinite(at) ? new Date(at).toLocaleString() : "Capture time unavailable"}\n${old ? "Stale observation" : "Cached observation"}`;
  return (
    <span
      style={{
        display: "inline-flex",
        position: "relative",
        marginInlineStart: 6,
        verticalAlign: "baseline",
      }}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
    >
      <button
        type="button"
        aria-label={`Last quota refresh: ${age}${old ? ", stale" : ""}`}
        aria-describedby={open ? tooltipId : undefined}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onClick={() => setOpen(true)}
        onKeyDown={(event) => {
          if (event.key === "Escape") setOpen(false);
        }}
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: 3,
          font: "inherit",
          fontSize: "0.8em",
          fontWeight: 400,
          color: old ? "#b45309" : "inherit",
          opacity: old ? 1 : 0.65,
          background: "transparent",
          border: 0,
          padding: 0,
          cursor: "pointer",
          whiteSpace: "nowrap",
        }}
      >
        <IconRefreshCw size={12} aria-hidden="true" />
        <span>{age}</span>
      </button>
      {open && (
        <span
          id={tooltipId}
          role="tooltip"
          style={{
            display: "block",
            whiteSpace: "pre-line",
            position: "absolute",
            zIndex: 30,
            bottom: "100%",
            insetInlineEnd: 0,
            minWidth: 210,
            maxWidth: "min(300px, 80vw)",
            padding: 10,
            borderRadius: 6,
            background: "#172033",
            color: "white",
            fontSize: 12,
            fontWeight: 400,
            boxShadow: "0 2px 10px #0005",
          }}
        >
          {details}
        </span>
      )}
    </span>
  );
}
