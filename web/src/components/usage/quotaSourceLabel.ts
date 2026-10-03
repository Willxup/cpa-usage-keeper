export const quotaSourceLabel = (source?: string) =>
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
