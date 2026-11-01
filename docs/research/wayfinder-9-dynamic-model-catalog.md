# Wayfinder Decision #9: Dynamic Model Catalog & Token Context Ingestion

## 1. Context & Motivation
LLM capabilities and context windows evolve continuously (e.g. GPT-5.6 Luna with 272K-300K context and 128K output, Claude 3.5/3.7 Sonnet, DeepSeek V3). Hardcoding model context limits and pricing in static tables causes immediate drift and inaccurate cost/token estimates.

## 2. Ingestion Pipeline & Architecture
Create an automated catalog synchronization worker in `internal/pricingmetadata/`:

### Data Sources
1. **Local Configuration (`model.json` / `models.json`)**: Highest precedence for local overrides and private routing aliases.
2. **CLIProxyAPI Runtime Models (`/v8/management/models`)**: Official models available in the active proxy routing pool.
3. **DP Agents / External Provider Catalogs**: Periodically pulled metadata covering context tokens, max generation tokens, vision/tool capabilities, and token pricing.

### Normalized Model Definition Schema
```json
{
  "model_id": "gpt-5.6-luna",
  "display_name": "GPT-5.6 Luna",
  "provider": "openai",
  "context_window_tokens": 300000,
  "max_output_tokens": 128000,
  "supports_vision": true,
  "supports_tools": true,
  "supports_streaming": true,
  "pricing": {
    "prompt_per_1m": 2.50,
    "completion_per_1m": 10.00,
    "cache_read_per_1m": 0.25
  },
  "source": "model.json",
  "updated_at": "2026-10-26T21:40:00Z"
}
```

## 3. Conflict Resolution & Validation Gates
- Strict validation: Reject models where `context_window_tokens <= 0` or negative pricing.
- Layered precedence: `User Override (DB)` > `model.json` > `CLIProxyAPI Registry` > `Static Fallback`.
- Stale detection: Warn in the dashboard if the catalog has not been updated in > 14 days.
