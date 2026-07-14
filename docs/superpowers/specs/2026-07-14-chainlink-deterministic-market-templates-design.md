# Chainlink Deterministic Market Templates Design

## Objective

Make every newly created template market highly likely to resolve automatically without paid Chainlink Data Streams, browser scraping, or AI-generated price facts. Android, the Go backend, and the Simulator must create and interpret exactly the same machine-readable rules.

The system remains an AI Agent prediction market: AI performs research, parses creation intent, manages positions, independently audits settlement evidence, and produces the final oracle verdict. Quantitative facts and arithmetic come from reproducible on-chain evidence rather than model memory.

## Feasibility Evidence

Chainlink publishes standard XAU/USD and BTC/USD Data Feed contracts on Ethereum mainnet:

- XAU/USD: `0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6`
- BTC/USD: `0xF4030086522a5bEEa4988F8cA5B36dbC97BeE88c`

Read-only calls to `latestRoundData()` and `getRoundData(uint80)` were tested successfully through `https://ethereum-rpc.publicnode.com`. Returned records include a composite round ID, price answer, and immutable update timestamp. These calls do not submit transactions and do not require gas.

Standard Data Feeds update on heartbeat or deviation triggers, not at minute precision. Therefore this design deliberately supports Beijing-calendar-day markets and rejects minute-scale markets. Public RPC availability is mitigated with configurable endpoint failover, local round caching, and historical round recovery.

## Supported Creation Templates

Only the following six templates are available for new manual, AI-created, and simulated markets:

| Template | Rule type | Deterministic calculation |
| --- | --- | --- |
| Price direction | `TYPE_PRICE` | End XAU price versus start XAU price, with explicit flat tolerance |
| Return threshold | `TYPE_RETURN_THRESHOLD` | Absolute XAU close-to-close return compared with a percentage threshold |
| Price threshold | `TYPE_PRICE_THRESHOLD` | End XAU price compared with a USD/oz threshold using `GTE` or `LTE` |
| Price range | `TYPE_PRICE_RANGE` | End XAU price is inside or outside an inclusive USD/oz range |
| Outperform BTC | `TYPE_RELATIVE` | XAU return compared with BTC return over identical boundaries |
| Consecutive direction | `TYPE_STREAK` | XAU rises or falls at every consecutive Beijing-day boundary |

`TYPE_VOLUME`, `TYPE_TECHNICAL`, `TYPE_EVENT`, `TYPE_TOUCH`, and the old intraperiod-amplitude meaning of `TYPE_VOLATILITY` are hidden from all new-creation paths. Existing markets of those types remain readable and keep their legacy settlement behavior; metadata semantics are never rewritten after creation.

Exact equality is not offered for price thresholds because a continuously varying quoted price is extremely unlikely to equal a decimal target exactly.

## Time Contract

All new template markets use `Asia/Shanghai` calendar boundaries:

- Observation start: `00:00:00` on a selected Beijing date.
- Observation end: `00:00:00` on a later Beijing date.
- Minimum interval: one calendar day.
- Start and end must be active XAU trading boundaries. The creation validator permits Tuesday through Saturday at Beijing midnight and rejects Sunday and Monday boundaries.
- AI creation and Simulator generation automatically move a requested boundary forward to the next valid boundary and display the resulting actual dates before deployment.
- The displayed day count is the calendar-day difference between committed start and end, including any weekend crossed inside the interval.

The selected boundary observation is the last Chainlink Data Feed round whose `updatedAt` is less than or equal to the committed boundary. Its age at the boundary must not exceed 12 hours. This `LAST_AT_OR_BEFORE` policy avoids using facts published after the committed time and allows immediate, reproducible settlement.

## Versioned Resolution Rule

Every new market commits a version 2 rule in IPFS metadata:

```json
{
  "rule_version": 2,
  "type": "TYPE_RELATIVE",
  "symbol": "XAU",
  "source": "CHAINLINK_DATA_FEED_ETHEREUM",
  "source_contract": "0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6",
  "benchmark": "BTC",
  "benchmark_source_contract": "0xF4030086522a5bEEa4988F8cA5B36dbC97BeE88c",
  "timezone": "Asia/Shanghai",
  "boundary_policy": "LAST_AT_OR_BEFORE",
  "max_staleness_sec": 43200,
  "start_time_sec": 1783958400,
  "end_time_sec": 1784044800
}
```

Template-specific fields add direction, tolerance, threshold, range bounds, or streak length. Android and Simulator use the same canonical constants and validation rules as the backend contract. The backend rejects malformed version 2 metadata rather than inferring missing fields from human-readable titles.

## Backend Components

### Chainlink Feed Client

A focused market-data client reads Aggregator V3 contracts through a configured ordered list of Ethereum RPC URLs. It validates:

- JSON-RPC response and ABI length.
- Positive answer and configured feed decimals.
- Nonzero `updatedAt` and `answeredInRound >= roundId`.
- Contract address and feed identity.
- Requested round belongs to the expected phase.

The client retries the next RPC only for transport, rate-limit, timeout, or malformed-response failures. A valid on-chain answer is never replaced with a web quote.

### Round Repository and Recorder

MySQL stores feed address, composite round ID, raw answer, decimals, normalized price, update time, and fetch time under a uniqueness key of feed address plus round ID. A background recorder polls `latestRoundData()` and inserts only unseen rounds.

For a missing boundary, the resolver searches historical Chainlink rounds with `getRoundData`. It caches recovered rounds before evaluation. Local samples from Gold API or Sina remain useful for UI/research diagnostics but are not authoritative version 2 settlement evidence.

### Boundary Resolver

The resolver loads the newest valid round at or before each boundary. It rejects the evidence as indeterminate when no round exists, price validation fails, the record is more than 12 hours old at the boundary, or every configured RPC fails before the evidence can be recovered.

For streak markets it resolves one price at every Beijing-midnight boundary. For relative markets it resolves XAU and BTC independently at the same committed boundaries and records each source timestamp and age.

### Structured Evaluator

The evaluator performs only deterministic arithmetic:

- Return: `(end - start) / start * 100`.
- Absolute return: `abs(return)`.
- Relative outcome: `xau_return > btc_return`; a configured tie tolerance resolves a tie to NO.
- Inclusive range: `lower <= end && end <= upper`.
- Streak: every adjacent pair satisfies the committed `UP` or `DOWN` direction; flat is failure unless explicitly configured.

It emits a complete evidence summary containing round IDs, timestamps, normalized prices, formulas, intermediate values, and preliminary YES/NO.

## AI Oracle Flow

Quantitative settlement uses an evidence-audit workflow:

1. The backend builds an immutable evidence package from Chainlink rounds and deterministic calculations.
2. The first N-1 configured AI providers independently check rule interpretation, source timestamps, arithmetic, and preliminary outcome.
3. The Nth configured arbiter receives the original rule, complete evidence package, and every independent opinion.
4. The arbiter returns structured `YES`, `NO`, or `INDETERMINATE`, plus a concise audit explanation.
5. The chain transaction is sent only for `YES` or `NO`. `INDETERMINATE`, provider failure, conflicting unresolvable evidence, or missing evidence keeps the market pending for retry.

No backend confidence threshold may convert an indeterminate AI result into a winner. AI cannot introduce an external price, replace a Chainlink round, or alter deterministic arithmetic. Settlement logs retain the complete evidence package, every independent opinion, the final arbiter prompt summary, verdict, and transaction result.

## AI Research and Managed Trading

The live gold quote provider reads Chainlink XAU/USD first. Gold API and Sina remain ordered display fallbacks and are labeled as such. The quote includes source, feed round, source update time, and freshness.

AI research and managed trading receive the same canonical title, rule, current quote, virtual reserves, time remaining, position, and evidence freshness. Managed trading skips a market when its rule is unsupported, quote is stale, or observation window is invalid. AI provider authentication and account balance remain operational prerequisites and are logged separately from market-data errors.

## Android Creation Changes

The manual grid shows only the six supported templates. All six use date selection without a time picker; selected times are fixed to Beijing midnight and validated before the confirmation dialog.

The AI parser prompt exposes only the six types. It returns a start-day offset and duration in whole days. Parsed output is validated locally; unsupported types, equality thresholds, unsupported benchmarks, Sunday/Monday boundaries, nonnumeric limits, and sub-day intervals cannot reach deployment.

Titles and condition text continue using the established colored token presenter. The detailed condition also states the Chainlink Data Feed source and Beijing boundary policy without changing the concise card title.

## Simulator Changes

The Simulator uses the same six types, version 2 metadata, Chainlink contract addresses, boundary policy, and Beijing valid-boundary helper. Create mode schedules future whole-day markets so the backend can cache data before settlement. Existing-market trade mode does not alter market metadata.

Random parameters are constrained to meaningful ranges around current XAU prices when a quote is available. Static fallback ranges are used only to generate a valid test market, never to settle it.

## Failure Handling

- One RPC fails: retry the next configured endpoint.
- All RPCs fail: use already cached authoritative Chainlink rounds; otherwise keep pending.
- Boundary round is stale beyond 12 hours: keep pending and log the exact age.
- AI provider fails or lacks balance: preserve deterministic evidence, log provider failure, and retry without settling.
- Metadata is malformed or uses an unsupported version 2 type: keep pending and identify the invalid field.
- Legacy hard-to-resolve market: retain the old path; never silently reinterpret it as a new template.

No browser automation, DOM scraping, delayed chart text, or unconstrained AI web answer may be used as sole settlement evidence.

## Verification

Backend tests cover ABI decoding, RPC failover, historical round lookup, cache uniqueness, Beijing boundary validation, stale evidence, all six evaluators, legacy compatibility, and N-1 plus final-arbiter flow. Network smoke tests are opt-in and verify both official feed contracts through a public RPC.

Simulator tests assert that every generated market uses a supported type, valid Beijing boundaries, complete version 2 metadata, and a title consistent with its rule.

Android verification covers AI parser validation, manual date normalization, all six metadata builders, title rendering, and a Gradle compilation. End-to-end verification creates representative markets for every template, supplies fixture Chainlink rounds, runs the watcher, and confirms the expected on-chain settlement call or explicit pending reason.

## Success Criteria

- Every newly created template market is one of the six deterministic types.
- Android, backend, and Simulator emit and consume identical rule semantics.
- All six types settle from cached or historically recovered Chainlink rounds when fresh evidence and AI providers are available.
- Backend downtime during a boundary does not destroy settlement evidence because historical rounds remain queryable on Ethereum.
- Missing evidence or AI availability never produces a guessed winner.
- Minute-scale and unsupported-source markets are rejected before deployment.
