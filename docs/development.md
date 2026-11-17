# Development Guide

[Back to README](../README.md) · [简体中文](./development.zh.md)

See the README's [project structure](../README.md#project-structure) and [local development](../README.md#local-development) sections for layout and setup.

## Tests

Run the full verification baseline from the repository root:

```bash
make verify
```

Or run checks individually:

```bash
go test ./cmd/... ./internal/...
npm --prefix ./web run test
npm --prefix ./web run lint
npm --prefix ./web run typecheck
npm --prefix ./web run build
```

## Chart time handling

### Principles

- Use the response or configured timezone. Calendar days are not always 24 hours; rolling windows use elapsed durations, and daily rates use the actual minutes in the calendar day.
- Identify buckets by an instant or full date and sort by instant. Formatted clock labels are for display only.
- Query fixes must not change stored aggregation boundaries. Preserve Overview and Latency's different hourly anchors, plus Activity's 364-block grid and stored bucket ends, to avoid changing the meaning of historical data.

### Shared Code

- Calendar-day calculations: `internal/timeutil.CivilDate` and `DayBounds`. Read aggregates through repository bucket adapters.
- Raw-event filtering: `internal/repository/usage_event_time_read.go`. Use bounded segments for ordinary queries and the open-ended reader for recent-cache startup/reload; callers retain field selection and cache lifecycle handling.
- Frontend labels: `web/src/utils/usage/timeLabels.ts`. Add timezone offsets only to ambiguous local clock labels, preserving locale, precision, fallback, and empty-point rules.

### Validation

Cover ordinary Shanghai dates, DST gaps and folds, and browser timezones that differ from the response timezone. Confirm that chart styles and lifecycle behavior remain unchanged.

## Admin historical API keys

`GET /usage/api/v1/usage/analysis?range=24h&include_deleted=true` opts into all hourly/daily key usage, including deleted and unregistered historical keys. Registered keys use their decimal Keeper ID in API-key composition and every heatmap identity field; unregistered keys use `legacy:<lowercase SHA-256 hex of api_group_key>`, matching comparisons. Labels remain aliases/display names or masked values. Successful opted-in responses add `include_deleted: true`.

`GET /usage/api/v1/usage/api-keys/settings?include_deleted=true` lists active and deleted rows, adds `isDeleted` to every item and `includeDeleted: true` to the response. Native rows retain `apiKey`; plugin rows still omit it. Treat this admin response as secret material.

Only the literal value `true` opts in. Both routes remain admin-only. Omitted/other values preserve existing responses, including omission of the new fields. Key-viewer routes ignore the parameter and deleted keys cannot authenticate. Comparisons, `api_key_id` filtering, and the schema are unchanged.
