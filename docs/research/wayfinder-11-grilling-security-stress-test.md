# Wayfinder Decision #11: Grilling Security & Robustness Stress Test

## Design Tree & Frontier Interview

### Round 1: PostgreSQL Failure & Rollback

❓ **Q1 - PostgreSQL Outage & Failover**: If the external PostgreSQL container crashes or encounters `too many connections`, how does CPA Usage Keeper avoid halting proxy observability?
➡️ **Recommended Answer**: Provide a dual-write or local SQLite spool fallback buffer. If PostgreSQL is unreachable on startup or during operation, queue incoming events in an append-only local SQLite buffer (`data/spool.db`), retry PostgreSQL with exponential backoff, and flush the spool once connectivity recovers.

---

❓ **Q2 - Migration Rollback Boundary**: If a live migration from SQLite to PostgreSQL fails at 85% data transfer, how is data consistency guaranteed?
➡️ **Recommended Answer**: Execute the migration inside an isolated transaction with checksum comparison. The SQLite database remains the authoritative primary until the final verification gate passes and the operator confirms the cutover. Zero destructive writes to SQLite during migration.

---

### Round 2: Malformed Catalog Ingestion

❓ **Q3 - Malformed or Poisoned Model Catalogs**: What prevents an external `model.json` or DP Agents endpoint from injecting absurd context sizes (e.g. 100M tokens) or corrupting pricing tables?
➡️ **Recommended Answer**: Enforce a strict JSON Schema validation boundary:
- `context_window_tokens` bounded between 1,000 and 2,000,000.
- `pricing` fields must be non-negative finite floats bounded by sanity limits ($0.00 to $1,000.00 / 1M tokens).
- Reject duplicate model IDs and strip unknown metadata fields.

---

### Round 3: Automated Quota Priority Oscillation

❓ **Q4 - Throttling Hysteresis & Route Flapping**: What prevents the quota balancing engine from flapping priorities back and forth between two accounts every 10 seconds?
➡️ **Recommended Answer**: Implement a minimum priority hold duration (hysteresis cooldown of at least 5 minutes) and require a minimum delta in remaining capacity (e.g. 15% headroom difference) before triggering an automated priority adjustment.

---

## Verification Gates
1. Unit tests for PostgreSQL GORM dialect initialization and schema auto-migration.
2. Invariant tests proving SQLite spooling during simulated PostgreSQL connection drops.
3. Negative schema tests asserting rejection of malformed or poison `model.json` payloads.
4. Hysteresis simulation proving zero priority flapping under high-frequency synthetic traffic.
