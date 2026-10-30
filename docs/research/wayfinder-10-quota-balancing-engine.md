# Wayfinder Decision #10: Proactive Quota Balancing & Request Optimization Engine

## 1. Context & Motivation
Subscribers frequently hit 5-hour rolling rate limits (e.g. OpenAI Plus/Pro, Claude Pro/Max) during high-intensity coding sessions because requests are dispatched naively to the primary credential until it fails with HTTP 429.

## 2. Quota Intelligence & Smoothing Heuristics
Introduce an active quota balancing layer that interfaces with CLIProxyAPI routing priorities:

### Consumption Velocity Estimation
For each active credential, calculate:
$$\text{BurnRate} = \frac{\Delta \text{UsedPercent}}{\Delta t}$$
If `RemainingPercent < 20%` AND `BurnRate > SafeThreshold`:
1. Mark credential status as `throttle_suggested` or `low_headroom`.
2. Automatically adjust credential priority in CPA (`PATCH /v8/management/credentials/:id/priority`) to shift incoming traffic to an alternative healthy credential with available headroom.
3. Once the rolling window resets (`resetAt` passes), restore default priority.

### Request Optimization Safeguards
- **Zero Token Leakage**: Optimization metrics consume only aggregated counts from `usage_events`, never inspecting request prompts or secrets.
- **Fail-Closed Protection**: If quota metrics are missing or unavailable, fallback to default priority without disruptive route shifting.
- **Operator Overrides**: Provide a manual "Lock Priority" toggle in `CredentialPriorityEditor.tsx` to prevent automated shifting on designated master accounts.
