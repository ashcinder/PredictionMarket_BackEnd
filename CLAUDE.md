# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Run the server (reads config.yaml)
go run main.go

# Or use the start script which validates config first
./start.sh

# Run all tests (uses in-memory stores, no real network)
go test ./...

# Run with race detector
go test -race ./...

# Run a single package test
go test ./internal/config/ -v

# Optional MySQL integration test (requires a _test database, auto-skipped otherwise)
MYSQL_TEST_DSN='prediction:dev-password@tcp(127.0.0.1:3306)/prediction_market_test?parseTime=true' \
  go test -tags=integration ./internal/aimanaged -run TestMySQLIntegration -v
```

## Project architecture

PredictionMarket is a Go backend that drives a blockchain-based prediction market for gold prices. It uses an Ethereum-compatible smart contract managed through either direct RPC or a BrokerChain HTTP API (`chain.use_broker_chain`).

### Startup flow (`main.go`)

1. Load and validate `config.yaml` (strict — unknown fields are rejected)
2. Open MySQL connection, ping, run embedded SQL migrations
3. Create four main components:
   - **Sentinel watcher** — polls chain for expired games and resolves them via the judge
   - **AI-managed engine** — polls AI-managed entries, calls LLM API for buy/hold decisions, executes trades
   - **Market history sampler** — records YES/NO price percentages from chain reserves into MySQL
   - **HTTP API server** — serves `/api/gold/ai-managed` (CRUD for AI-managed entries) and `/api/gold/market-history` (query price chart data)
4. All four run as goroutines; the process exits on SIGTERM/SIGINT or if any service crashes

### Package map

| Package | Role |
| --- | --- |
| `internal/config` | YAML config parsing with strict validation (private key, URLs, DSN, bounds checks) |
| `internal/chain` | Contract interaction: ABI encoding/decoding, RPC JSON-RPC calls, BrokerChain HTTP API with ECDSA signing. Contract functions: `getAllGames`, `getGameInfo`, `getGameExtraData`, `buyShares`, `resolveGame` |
| `internal/database` | MySQL connection pool setup + embedded migration runner with named advisory locks |
| `internal/ipfs` | Fetches game metadata JSON from IPFS gateway. Supports `inline-v1:<hex>` encoded CIDs. Parses normalized history points |
| `internal/oracle` | Gold price oracle: tries Gold API first, falls back to Sina finance for current price and 24h change |
| `internal/judge` | Evaluates game conditions (price up/down, volatility, volume, technical indicators, one-touch barriers, outperformance) against real gold data. Mirrors `agent/gold/model/logic/GoldGameJudge.java` |
| `internal/sentinel` | Polls the chain for games past deadline, fetches game conditions from IPFS, fetches gold price, evaluates winner via judge, and submits `resolveGame` transactions |
| `internal/aimanaged` | **Largest package** — AI-managed auto-trading system with encrypted in-memory key store, HTTP CRUD API, LLM-backed decision engine, MySQL-persisted market history and decision audit trail |

### `aimanaged` sub-package internals

- **`manager.go`** — Core: `Store` (AES-GCM encrypted private key storage in memory), `Server` (HTTP handlers for enabling/disabling AI management), `Engine` (polling loop that fetches chain data + gold price, calls LLM, applies confidence/cooldown rules, executes trades), `AIClient` (sends market data + untrusted IPFS metadata to LLM for decision)
- **`persistence.go`** — Interface definitions: `HistoryRepository` (market history CRUD), `DecisionRepository` (AI decision audit trail), plus `HistoryObservation` and decision record types
- **`mysql_repository.go`** — MySQL implementation of history + decision persistence via the `market_history` and `ai_decisions` tables
- **`history.go`** — In-memory `HistoryObservation` computation from virtual reserves, timestamp bucketing, merge/dedup logic
- **`history_handler.go`** — HTTP handler for `/api/gold/market-history` — returns YES/NO percentage history. Falls back to on-demand chain sampling when the database has no data yet for a new pool
- **`sampler.go`** — Background worker that calls `getAllGames` → `getGameExtraData` for every active game and persists price data points to MySQL

### Database schema

Two tables created by embedded migration `001_market_persistence.sql`:
- **`market_history`** — time-series chart data: `(contract_address, game_id, observed_at)` PK, YES/NO percentages, raw reserves. Sources: `chain` (sampler) or `ipfs` (historical seed data)
- **`ai_decisions`** — full audit trail of AI trading decisions with enum outcomes: `pending`, `history_insufficient`, `invalid_reserves`, `hold`, `low_confidence`, `cooldown`, `traded`, `trade_failed`

Migrations use `GET_LOCK`/`RELEASE_LOCK` named advisory locks to serialize schema changes.

### `agent/` directory

Android (Java) frontend — the mobile client. Key reference files:
- `agent/gold/model/logic/GoldGameJudge.java` — game resolution logic that `internal/judge/game.go` mirrors
- `agent/ai/` — AI-related Java classes that the Go backend reimplements

### Key design patterns

- **Interface-based testing**: `managedChain`, `metadataSource`, `quoteSource`, `decisionSource`, `historyFetcher`, `samplerChain` interfaces allow tests to use mock implementations without real network calls
- **Defensive validation**: Every boundary validates inputs (reserves non-negative, percentages 0-100 sum to 100, addresses are hex, amounts parse without NaN/Inf)
- **Cooldown**: AI engine enforces a 1-hour cooldown between trades for the same user on the same option
- **IPFS as untrusted data**: The LLM prompt explicitly labels IPFS metadata as "untrusted" to prevent prompt injection
- **Private keys never leave memory**: AI-managed keys are AES-GCM encrypted in memory with a per-process random key, decrypted only per-tick for signing
