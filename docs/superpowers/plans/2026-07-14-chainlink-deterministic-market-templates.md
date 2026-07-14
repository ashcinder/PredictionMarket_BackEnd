# Chainlink Deterministic Market Templates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every newly created template market use a Beijing-day rule that can be resolved from public Chainlink XAU/USD and BTC/USD Ethereum Data Feed rounds, then audited by N-1 AI reviewers and one final AI arbiter.

**Architecture:** Add an independent Chainlink Aggregator V3 reader with RPC failover and a MySQL round cache. Route version 2 rules through a Beijing-boundary resolver and deterministic evaluator, then send the complete evidence package through the existing multi-AI final-arbiter path. Android and Simulator expose the same six versioned templates and reject unsupported or sub-day markets before deployment.

**Tech Stack:** Go 1.24, go-ethereum primitives, MySQL 8, YAML, Android Java 8/SDK 21, JUnit 4, Gradle, Ethereum JSON-RPC.

## Global Constraints

- New rules use `rule_version: 2`, `source: CHAINLINK_DATA_FEED_ETHEREUM`, `timezone: Asia/Shanghai`, `boundary_policy: LAST_AT_OR_BEFORE`, and `max_staleness_sec: 43200`.
- XAU/USD feed is `0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6`; BTC/USD feed is `0xF4030086522a5bEEa4988F8cA5B36dbC97BeE88c`.
- New creation supports only `TYPE_PRICE`, `TYPE_RETURN_THRESHOLD`, `TYPE_PRICE_THRESHOLD`, `TYPE_PRICE_RANGE`, `TYPE_RELATIVE`, and `TYPE_STREAK`.
- Version 2 boundaries are Beijing midnight, at least one calendar day apart, and both boundary weekdays are Tuesday through Saturday.
- Browser scraping, model memory, Gold API, Sina, and Coinbase are not authoritative version 2 settlement evidence.
- Missing, stale, malformed, or unavailable evidence stays pending; it never produces a guessed winner.
- Existing legacy market metadata and behavior remain readable and are not rewritten.
- Preserve unrelated user changes already present in all three repositories.

---

### Task 1: Chainlink Aggregator V3 Reader and Configuration

**Files:**
- Create: `internal/chainlinkfeed/client.go`
- Create: `internal/chainlinkfeed/client_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `config.yaml`
- Modify: `config.example.yaml`

**Interfaces:**
- Produces: `chainlinkfeed.Client`, `chainlinkfeed.Round`, `LatestRound(ctx, feed)`, `Round(ctx, feed, roundID)`, and `RoundAtOrBefore(ctx, feed, boundary)`.
- Produces: `Config.ChainlinkRPCURLs`, `Config.ChainlinkXAUUSDFeed`, `Config.ChainlinkBTCUSDFeed`, `Config.ChainlinkPollInterval`, and `Config.ChainlinkMaxStaleness`.

- [ ] **Step 1: Add failing ABI and failover tests**

```go
func TestClientDecodesLatestRoundData(t *testing.T) {
	server := newRPCFixture(t, map[string]string{
		"0xfeaf968c": encodeRound(5, 8286, 407910500000, 1784033519),
	})
	client := NewClient([]string{server.URL}, time.Second)
	round, err := client.LatestRound(context.Background(), xauFeed)
	if err != nil { t.Fatal(err) }
	if round.ID.String() != "92233720368547766366" || round.Price != 4079.105 {
		t.Fatalf("round = %+v", round)
	}
}

func TestClientFailsOverToSecondRPC(t *testing.T) {
	bad := newHTTPStatusServer(t, http.StatusTooManyRequests)
	good := newRPCFixture(t, map[string]string{"0xfeaf968c": encodeRound(5, 8286, 407910500000, 1784033519)})
	client := NewClient([]string{bad.URL, good.URL}, time.Second)
	if _, err := client.LatestRound(context.Background(), xauFeed); err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: Run the focused test and verify failure**

Run: `go test ./internal/chainlinkfeed -run 'TestClient' -v`

Expected: FAIL because `NewClient` and round decoding do not exist.

- [ ] **Step 3: Implement the reader with strict round validation**

```go
type Round struct {
	Feed            common.Address
	ID              *big.Int
	Answer          *big.Int
	Decimals        uint8
	Price           float64
	StartedAt       time.Time
	UpdatedAt       time.Time
	AnsweredInRound *big.Int
}

type Client struct {
	rpcURLs []string
	client  *http.Client
}

func (c *Client) LatestRound(ctx context.Context, feed common.Address) (Round, error)
func (c *Client) Round(ctx context.Context, feed common.Address, id *big.Int) (Round, error)
func (c *Client) RoundAtOrBefore(ctx context.Context, feed common.Address, boundary time.Time) (Round, error)
```

Use selectors `0xfeaf968c`, `0x9a6fc8f5`, `0x313ce567`, and `0x245a7bfc`. Require five 32-byte words, positive answers, nonzero timestamps, and `answeredInRound >= roundId`. Search the active phase by aggregator-round binary search; when the target predates the active phase, query `phaseAggregators(uint16)` and continue in the previous phase.

- [ ] **Step 4: Add strict YAML defaults and validation**

```yaml
oracle:
  chainlink_rpc_urls:
    - "https://ethereum-rpc.publicnode.com"
    - "https://eth.llamarpc.com"
  chainlink_xau_usd_feed: "0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6"
  chainlink_btc_usd_feed: "0xF4030086522a5bEEa4988F8cA5B36dbC97BeE88c"
  chainlink_poll_interval_seconds: 60
  chainlink_max_staleness_seconds: 43200
```

Reject empty URL lists, non-HTTP URLs, invalid addresses, poll intervals outside 10-3600 seconds, and staleness outside 3600-86400 seconds.

- [ ] **Step 5: Run configuration and reader tests**

Run: `go test ./internal/config ./internal/chainlinkfeed -v`

Expected: PASS.

- [ ] **Step 6: Commit the focused backend change**

```bash
git add internal/chainlinkfeed internal/config/config.go internal/config/config_test.go config.yaml config.example.yaml
git commit -m "feat: add public Chainlink feed reader"
```

### Task 2: Chainlink Round Cache and Recorder

**Files:**
- Create: `internal/database/migrations/017_chainlink_rounds.sql`
- Modify: `internal/database/mysql.go`
- Modify: `internal/database/migrations_test.go`
- Create: `internal/marketdata/chainlink_rounds_mysql.go`
- Create: `internal/marketdata/chainlink_rounds_mysql_test.go`
- Create: `internal/marketdata/chainlink_recorder.go`
- Create: `internal/marketdata/chainlink_recorder_test.go`

**Interfaces:**
- Consumes: `chainlinkfeed.Round` and `chainlinkfeed.Client`.
- Produces: `ChainlinkRoundRepository.Save`, `LatestAtOrBefore`, `ByID`, and `ChainlinkRoundRecorder.Run`.

- [ ] **Step 1: Add failing repository and recorder tests**

```go
func TestMySQLChainlinkRoundRepositoryUpsertsByFeedAndRound(t *testing.T) {
	repo, mock := newRoundRepositoryTest(t)
	mock.ExpectExec("INSERT INTO oracle_chainlink_rounds").
		WithArgs(strings.ToLower(xauFeed.Hex()), "92233720368547766366", "407910500000", 8,
			4079.105, int64(1784033519), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := repo.Save(context.Background(), fixtureRound()); err != nil { t.Fatal(err) }
}

func TestRecorderStoresOnlyNewLatestRounds(t *testing.T) {
	client := &fakeRoundClient{rounds: []chainlinkfeed.Round{fixtureRound(), fixtureRound()}}
	repo := &countingRoundRepository{}
	recorder := NewChainlinkRoundRecorder(client, repo, []common.Address{xauFeed, btcFeed}, time.Millisecond)
	recorder.recordOnce(context.Background())
	recorder.recordOnce(context.Background())
	if repo.uniqueSaves != 1 { t.Fatalf("unique saves = %d", repo.uniqueSaves) }
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./internal/marketdata -run 'ChainlinkRound|Recorder' -v`

Expected: FAIL because the repository and recorder do not exist.

- [ ] **Step 3: Add migration and repository**

```sql
CREATE TABLE IF NOT EXISTS oracle_chainlink_rounds (
  feed_address VARCHAR(42) NOT NULL,
  round_id DECIMAL(30,0) NOT NULL,
  answer_raw DECIMAL(78,0) NOT NULL,
  decimals TINYINT UNSIGNED NOT NULL,
  price_usd DECIMAL(30,8) NOT NULL,
  updated_at_sec BIGINT NOT NULL,
  fetched_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (feed_address, round_id),
  INDEX idx_chainlink_feed_time (feed_address, updated_at_sec DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

`LatestAtOrBefore` must select `updated_at_sec <= ? ORDER BY updated_at_sec DESC LIMIT 1`. Preserve round IDs as decimal strings because composite uint80 IDs exceed `uint64`.

- [ ] **Step 4: Implement recorder lifecycle and logs**

The recorder polls both feeds, saves unseen rounds idempotently, logs feed, round ID, source timestamp, price, and RPC failure, and exits on context cancellation.

- [ ] **Step 5: Run database and recorder tests**

Run: `go test ./internal/database ./internal/marketdata -run 'Migration|ChainlinkRound|Recorder' -v`

Expected: PASS.

- [ ] **Step 6: Commit the cache layer**

```bash
git add internal/database internal/marketdata/chainlink_rounds_mysql.go internal/marketdata/chainlink_rounds_mysql_test.go internal/marketdata/chainlink_recorder.go internal/marketdata/chainlink_recorder_test.go
git commit -m "feat: cache Chainlink settlement rounds"
```

### Task 3: Version 2 Rule Contract and Six Deterministic Evaluators

**Files:**
- Modify: `internal/judge/evidence.go`
- Modify: `internal/judge/evidence_test.go`
- Create: `internal/judge/boundary.go`
- Create: `internal/judge/boundary_test.go`

**Interfaces:**
- Produces: expanded `judge.Rule`, constants `TypeReturnThreshold`, `TypePriceRange`, `TypeStreak`, and `ValidateVersion2Rule`.
- Produces: evaluator support for all six supported types.

- [ ] **Step 1: Add failing rule and evaluator table tests**

```go
func TestEvaluateVersion2Templates(t *testing.T) {
	tests := []struct{name string; rule Rule; evidence Evidence; winner int}{
		{"direction", v2Rule(TypePrice, "UP"), boundaryEvidence(4000, 4040), 0},
		{"return threshold", v2Threshold(TypeReturnThreshold, "GTE", 0.9), boundaryEvidence(4000, 4040), 0},
		{"price threshold", v2Threshold(TypePriceThreshold, "GTE", 4030), deadlineEvidence(4040), 0},
		{"price range", v2Range(4020, 4050), deadlineEvidence(4040), 0},
		{"relative", v2Relative(), relativeEvidence(4000, 4040, 100000, 100500), 0},
		{"streak", v2Streak("UP", 3), dailyEvidence(4000, 4010, 4020, 4030), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateStructured(tt.rule, tt.evidence)
			if !got.Determinate || got.Winner != tt.winner { t.Fatalf("result = %+v", got) }
		})
	}
}
```

- [ ] **Step 2: Verify tests fail**

Run: `go test ./internal/judge -run 'Version2|Beijing' -v`

Expected: FAIL because the new rule fields and types do not exist.

- [ ] **Step 3: Extend the rule without changing legacy semantics**

```go
type Rule struct {
	RuleVersion             int     `json:"rule_version,omitempty"`
	Type                    string  `json:"type"`
	Symbol                  string  `json:"symbol"`
	Source                  string  `json:"source"`
	SourceContract          string  `json:"source_contract,omitempty"`
	Benchmark               string  `json:"benchmark,omitempty"`
	BenchmarkSourceContract string  `json:"benchmark_source_contract,omitempty"`
	Timezone                string  `json:"timezone,omitempty"`
	BoundaryPolicy          string  `json:"boundary_policy,omitempty"`
	MaxStalenessSec         int64   `json:"max_staleness_sec,omitempty"`
	Operator                string  `json:"operator,omitempty"`
	Direction               string  `json:"direction,omitempty"`
	Threshold               float64 `json:"threshold,omitempty"`
	LowerThreshold          float64 `json:"lower_threshold,omitempty"`
	UpperThreshold          float64 `json:"upper_threshold,omitempty"`
	StreakDays              int     `json:"streak_days,omitempty"`
	FlatTolerance           float64 `json:"flat_tolerance_percent,omitempty"`
	StartTimeSec            int64   `json:"start_time_sec"`
	EndTimeSec              int64   `json:"end_time_sec"`
}
```

Add `SourceTime`, `RoundID`, and `SourceContract` to `Candle` so evidence can expose the actual on-chain report while `Time` remains the committed boundary.

- [ ] **Step 4: Implement Beijing boundary validation**

`ValidateVersion2Rule` requires exact midnight in `Asia/Shanghai`, end after start by at least 24 hours, Tuesday-Saturday start/end weekdays, canonical source/contracts/policy, supported type, and type-specific fields. Return field-specific errors.

- [ ] **Step 5: Implement the six calculations**

Use close-to-close percentage return, absolute return for `TYPE_RETURN_THRESHOLD`, inclusive ranges, strict XAU return greater than BTC return, and pairwise streak checks. Keep old `TYPE_VOLATILITY`, `TYPE_TOUCH`, `TYPE_VOLUME`, `TYPE_TECHNICAL`, and `TYPE_EVENT` branches unchanged.

- [ ] **Step 6: Run all judge tests**

Run: `go test ./internal/judge -v`

Expected: PASS.

- [ ] **Step 7: Commit rule support**

```bash
git add internal/judge
git commit -m "feat: add deterministic v2 market rules"
```

### Task 4: Chainlink Boundary Resolver and Historical Recovery

**Files:**
- Create: `internal/marketdata/chainlink_resolver.go`
- Create: `internal/marketdata/chainlink_resolver_test.go`
- Modify: `internal/marketdata/resolver.go`
- Modify: `internal/marketdata/sampled_gold_test.go`

**Interfaces:**
- Consumes: `judge.Rule`, round repository, and Chainlink client.
- Produces: `ChainlinkResolver.Resolve(ctx, rule) judge.Result` and a version-aware composite resolver.

- [ ] **Step 1: Add failing cache, recovery, staleness, relative, and streak tests**

```go
func TestChainlinkResolverRecoversMissingBoundaryRoundAndCachesIt(t *testing.T) {
	repo := newMemoryRoundRepo()
	client := &fakeHistoricalFeedClient{rounds: map[common.Address][]chainlinkfeed.Round{
		xauFeed: {roundAt(start, 4000), roundAt(end, 4040)},
	}}
	resolver := NewChainlinkResolver(client, repo, xauFeed, btcFeed)
	got := resolver.Resolve(context.Background(), v2PriceRule(start, end))
	if !got.Determinate || got.Winner != 0 || repo.saveCount != 2 { t.Fatalf("result=%+v saves=%d", got, repo.saveCount) }
}

func TestChainlinkResolverRejectsTwelveHourOldBoundary(t *testing.T) {
	got := resolverWithRounds(roundAt(start.Add(-13*time.Hour), 4000)).Resolve(context.Background(), rule)
	if got.Determinate || !strings.Contains(got.Summary, "stale") { t.Fatalf("result=%+v", got) }
}
```

- [ ] **Step 2: Verify focused tests fail**

Run: `go test ./internal/marketdata -run 'ChainlinkResolver' -v`

Expected: FAIL because the resolver does not exist.

- [ ] **Step 3: Implement boundary loading and evidence summaries**

For each required boundary, read cache first; if absent, call `RoundAtOrBefore`, save it, then enforce `boundary.Sub(round.UpdatedAt) <= max_staleness`. Build candles with `Time=boundary`, `SourceTime=round.UpdatedAt`, `RoundID=round.ID.String()`, and identical OHLC values equal to the normalized Chainlink answer.

The result summary must list every feed contract, boundary, selected round ID, report timestamp, age, price, formula, intermediate values, and candidate result.

- [ ] **Step 4: Route version 2 rules to Chainlink and legacy rules to existing providers**

```go
type VersionedResolver struct {
	legacy    *StructuredResolver
	chainlink *ChainlinkResolver
}

func (r *VersionedResolver) Resolve(ctx context.Context, rule judge.Rule) judge.Result {
	if rule.RuleVersion >= 2 { return r.chainlink.Resolve(ctx, rule) }
	return r.legacy.Resolve(ctx, rule)
}
```

- [ ] **Step 5: Run market-data tests**

Run: `go test ./internal/marketdata -v`

Expected: PASS, including unchanged legacy sample tests.

- [ ] **Step 6: Commit resolver support**

```bash
git add internal/marketdata
git commit -m "feat: resolve Beijing-day markets from Chainlink"
```

### Task 5: Wire Chainlink Live Quotes, Recorder, and Versioned Resolver

**Files:**
- Create: `internal/oracle/chainlink_gold.go`
- Create: `internal/oracle/chainlink_gold_test.go`
- Modify: `internal/oracle/gold.go`
- Modify: `internal/oracle/gold_test.go`
- Modify: `main.go`

**Interfaces:**
- Consumes: Chainlink reader, config, and round repository.
- Produces: a Chainlink-first gold quote with labeled Gold API/Sina fallback.

- [ ] **Step 1: Add failing primary/fallback/freshness tests**

```go
func TestGoldOraclePrefersFreshChainlinkQuote(t *testing.T) {
	primary := &fakeChainlinkGoldSource{quote: &Quote{PriceUSD: 4079.105, QuoteSource: "Chainlink XAU/USD"}}
	fallback := newGoldHTTPFixture(t, 3999)
	oracle := NewGoldOracleWithPrimary(fallback.config, primary)
	quote, err := oracle.FetchQuote()
	if err != nil { t.Fatal(err) }
	if quote.PriceUSD != 4079.105 || quote.QuoteSource != "Chainlink XAU/USD" { t.Fatalf("quote=%+v", quote) }
}
```

- [ ] **Step 2: Verify oracle tests fail**

Run: `go test ./internal/oracle -run 'Chainlink|Prefers' -v`

Expected: FAIL because the primary source hook does not exist.

- [ ] **Step 3: Implement Chainlink-first quote composition**

`ChainlinkGoldSource.FetchQuote` reads the latest XAU round, rejects timestamps older than configured staleness, and maps feed address, round ID, and report time into `QuoteSource` and `QuoteUpdatedAt`. `GoldOracle.FetchQuote` calls the primary first, then existing Gold API and Sina fallbacks.

- [ ] **Step 4: Wire all backend services in `main.go`**

Construct one Chainlink client, round repository, recorder, Chainlink resolver, versioned resolver, and Chainlink gold source. Start the recorder in its own goroutine and increase `errCh` capacity. Keep the existing local XAU sampler for legacy markets and diagnostics.

- [ ] **Step 5: Run backend package tests and build**

Run: `go test ./internal/oracle ./internal/marketdata ./internal/config ./internal/database -v`

Run: `go build ./...`

Expected: PASS.

- [ ] **Step 6: Commit wiring**

```bash
git add internal/oracle main.go
git commit -m "feat: use Chainlink quotes across backend agents"
```

### Task 6: Send Every Version 2 Settlement Through Multi-AI Final Arbitration

**Files:**
- Modify: `internal/sentinel/watcher.go`
- Modify: `internal/sentinel/watcher_test.go`
- Modify: `internal/aioracle/provider.go`
- Modify: `internal/aioracle/arbiter_test.go`
- Modify: `internal/aioracle/aioracle_test.go`

**Interfaces:**
- Consumes: complete deterministic `judge.Result` summaries.
- Produces: one common AI evidence-audit path for all version 2 quantitative types.

- [ ] **Step 1: Add failing routing and prompt tests**

```go
func TestEveryVersion2RuleRequiresFinalArbiterReview(t *testing.T) {
	for _, typ := range []string{judge.TypePrice, judge.TypeReturnThreshold, judge.TypePriceThreshold,
		judge.TypePriceRange, judge.TypeRelative, judge.TypeStreak} {
		t.Run(typ, func(t *testing.T) {
			watcher, oracle := version2WatcherFixture(t, typ)
			if err := watcher.evaluateAndResolve(context.Background(), expiredGame()); err != nil { t.Fatal(err) }
			if oracle.calls != 1 { t.Fatalf("oracle calls=%d", oracle.calls) }
		})
	}
}
```

Assert peer and final prompts contain round IDs, report timestamps, prices, the exact type-specific formula, and an explicit instruction to return `INDETERMINATE` when evidence cannot be reproduced.

- [ ] **Step 2: Verify tests fail**

Run: `go test ./internal/sentinel ./internal/aioracle -run 'Version2|Quantitative|Arbiter' -v`

Expected: FAIL because only relative markets currently use AI review.

- [ ] **Step 3: Generalize quantitative evidence packaging**

All `rule.RuleVersion >= 2` results call `buildQuantitativeAIEvent` and `oracle.Resolve`. Legacy deterministic rules retain existing direct-settlement behavior. Update `requiresStructuredResolution` with all new types.

- [ ] **Step 4: Generalize reviewer and final-arbiter instructions**

Replace relative-only wording with a rule-type checklist covering direction return, absolute return, threshold, inclusive range, relative returns, and consecutive comparisons. The final arbiter controls YES/NO/INDETERMINATE; no confidence threshold changes its decision.

- [ ] **Step 5: Run AI oracle and watcher tests**

Run: `go test ./internal/aioracle ./internal/sentinel -v`

Expected: PASS.

- [ ] **Step 6: Commit AI settlement routing**

```bash
git add internal/aioracle internal/sentinel
git commit -m "feat: audit all v2 settlements with multi AI"
```

### Task 7: Replace Simulator Templates and Normalize Beijing Boundaries

**Files:**
- Create: `PredictionMarket_Simulator/internal/scenario/boundary.go`
- Create: `PredictionMarket_Simulator/internal/scenario/boundary_test.go`
- Modify: `PredictionMarket_Simulator/internal/scenario/templates.go`
- Modify: `PredictionMarket_Simulator/internal/scenario/templates_test.go`
- Modify: `PredictionMarket_Simulator/internal/config/config.go`
- Modify: `PredictionMarket_Simulator/internal/config/config_test.go`
- Modify: `PredictionMarket_Simulator/config.yaml`
- Modify: `PredictionMarket_Simulator/config.example.yaml`
- Modify: `PredictionMarket_Simulator/README.md`

**Interfaces:**
- Produces: exactly six supported Simulator types and complete version 2 metadata.
- Produces: `NextValidBeijingBoundary` and `NormalizeMarketWindow`.

- [ ] **Step 1: Replace tests with the six-type contract**

```go
func TestSupportedTypesContainOnlyResolvableV2Templates(t *testing.T) {
	want := []string{TypePrice, TypeReturnThreshold, TypePriceThreshold, TypePriceRange, TypeRelative, TypeStreak}
	if diff := cmp.Diff(want, SupportedTypes()); diff != "" { t.Fatal(diff) }
}

func TestEveryGeneratedMarketCommitsVersion2ChainlinkRule(t *testing.T) {
	for index, typ := range SupportedTypes() {
		market, err := BuildMarket(BuildMarketInput{Type: typ, Now: fixtureNow, Duration: 48*time.Hour, TemplateSeed: index})
		if err != nil { t.Fatal(err) }
		var meta struct{ ResolutionRule judgeRule `json:"resolutionRule"` }
		json.Unmarshal([]byte(market.MetadataJSON), &meta)
		if meta.ResolutionRule.RuleVersion != 2 || meta.ResolutionRule.Timezone != "Asia/Shanghai" { t.Fatalf("meta=%s", market.MetadataJSON) }
	}
}
```

- [ ] **Step 2: Verify Simulator tests fail**

Run from `PredictionMarket_Simulator`: `go test ./internal/scenario ./internal/config -v`

Expected: FAIL because legacy templates and arbitrary durations remain.

- [ ] **Step 3: Implement the new catalog and canonical metadata**

Use titles:

```text
黄金价格 上涨 2天
黄金涨跌幅 大于 3%
黄金价格 大于等于 4200USD/盎司
黄金价格 位于 4000-4200USD/盎司
黄金 跑赢 BTC
黄金价格 连续上涨 3天
```

Use only BTC for relative markets and only `GTE`/`LTE` for price thresholds. Repurpose template icon markers to the corresponding new types.

- [ ] **Step 4: Normalize generated windows**

Convert `Now` to `Asia/Shanghai`, choose the next Tuesday-Saturday midnight as observation start, choose a later valid boundary based on the requested whole-day duration, and set contract duration to `end - Now`. Reject configured min/max durations below 24 hours and round accepted values to whole days.

- [ ] **Step 5: Update YAML documentation and mode help**

Default `market.types` to the six types and set duration examples in whole days. Explain that `create_and_trade` creates future Beijing-day pools while `trade_existing` only trades existing IDs.

- [ ] **Step 6: Run the complete Simulator suite**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 7: Commit Simulator changes in its repository**

```bash
git add internal/scenario internal/config config.yaml config.example.yaml README.md
git commit -m "feat: generate resolvable Beijing-day markets"
```

### Task 8: Add Android Template Catalog and Creation Policy

**Files:**
- Create: `brokerwallet-ai-agent/app/src/main/java/com/example/brokerfi/xc/agent/gold/model/logic/GoldMarketTemplateCatalog.java`
- Create: `brokerwallet-ai-agent/app/src/main/java/com/example/brokerfi/xc/agent/gold/model/logic/GoldMarketCreationPolicy.java`
- Create: `brokerwallet-ai-agent/app/src/test/java/com/example/brokerfi/xc/agent/gold/GoldMarketTemplateCatalogTest.java`
- Create: `brokerwallet-ai-agent/app/src/test/java/com/example/brokerfi/xc/agent/gold/GoldMarketCreationPolicyTest.java`
- Modify: `brokerwallet-ai-agent/app/src/main/java/com/example/brokerfi/xc/agent/gold/view/GoldMarketTemplateIcon.java`
- Modify: `brokerwallet-ai-agent/app/src/test/java/com/example/brokerfi/xc/agent/gold/GoldMarketTemplateIconTest.java`

**Interfaces:**
- Produces: one Android source of truth for supported types, display names, hints, icons, canonical contracts, and boundary normalization.
- Produces: pure-Java rule builders testable without Android framework classes.

- [ ] **Step 1: Add failing catalog and boundary tests**

```java
@Test public void catalogContainsOnlySixResolvableTemplates() {
    assertEquals(Arrays.asList("TYPE_PRICE", "TYPE_RETURN_THRESHOLD", "TYPE_PRICE_THRESHOLD",
            "TYPE_PRICE_RANGE", "TYPE_RELATIVE", "TYPE_STREAK"), GoldMarketTemplateCatalog.types());
}

@Test public void normalizesMondayToTuesdayBeijingMidnight() {
    Calendar input = beijing(2026, Calendar.JULY, 13, 15, 40);
    Calendar result = GoldMarketCreationPolicy.nextValidBoundary(input);
    assertEquals(Calendar.TUESDAY, result.get(Calendar.DAY_OF_WEEK));
    assertEquals(0, result.get(Calendar.HOUR_OF_DAY));
}
```

- [ ] **Step 2: Verify Android unit tests fail**

Run from `brokerwallet-ai-agent`: `./gradlew testDebugUnitTest --tests 'com.example.brokerfi.xc.agent.gold.GoldMarketTemplateCatalogTest' --tests 'com.example.brokerfi.xc.agent.gold.GoldMarketCreationPolicyTest'`

Expected: FAIL because the helper classes do not exist.

- [ ] **Step 3: Implement pure Java catalog and policy**

Use `Calendar` plus `TimeZone.getTimeZone("Asia/Shanghai")` for SDK 21 compatibility. Expose constants for rule version, source, policy, staleness, and feed addresses. Build JSON with the exact backend field names and validate each type before returning it.

- [ ] **Step 4: Update icon mapping without breaking legacy cards**

New creation types map to six template XML drawables. Keep legacy inference branches so old volume, technical, event, touch, and volatility markets still display their existing covers.

- [ ] **Step 5: Run focused Android tests**

Run: `./gradlew testDebugUnitTest --tests 'com.example.brokerfi.xc.agent.gold.GoldMarketTemplateCatalogTest' --tests 'com.example.brokerfi.xc.agent.gold.GoldMarketCreationPolicyTest' --tests 'com.example.brokerfi.xc.agent.gold.GoldMarketTemplateIconTest'`

Expected: PASS.

- [ ] **Step 6: Commit Android policy support**

```bash
git add app/src/main/java/com/example/brokerfi/xc/agent/gold/model/logic app/src/main/java/com/example/brokerfi/xc/agent/gold/view/GoldMarketTemplateIcon.java app/src/test/java/com/example/brokerfi/xc/agent/gold
git commit -m "feat: define resolvable Android market templates"
```

### Task 9: Update Android Manual and AI Creation Workflows

**Files:**
- Modify: `brokerwallet-ai-agent/app/src/main/java/com/example/brokerfi/xc/agent/gold/view/GoldCreatePoolFragment.java`
- Modify: `brokerwallet-ai-agent/app/src/main/java/com/example/brokerfi/xc/agent/gold/view/GoldCreateCustomActivity.java`
- Modify: `brokerwallet-ai-agent/app/src/main/res/layout/activity_gold_create_custom.xml`
- Modify: `brokerwallet-ai-agent/app/src/main/java/com/example/brokerfi/xc/agent/gold/model/logic/GoldMarketCardPresenter.java`
- Modify: `brokerwallet-ai-agent/app/src/test/java/com/example/brokerfi/xc/agent/gold/GoldMarketCardPresenterTest.java`
- Create: `brokerwallet-ai-agent/app/src/test/java/com/example/brokerfi/xc/agent/gold/GoldCreatePromptContractTest.java`

**Interfaces:**
- Consumes: Android template catalog and creation policy.
- Produces: six-template manual cards, six-template AI parser, whole-day date selection, and canonical version 2 metadata.

- [ ] **Step 1: Add failing prompt, title, and rule contract tests**

Assert the prompt contains all six supported type names and does not contain `TYPE_VOLUME`, `TYPE_TECHNICAL`, `TYPE_EVENT`, `TYPE_TOUCH`, or `TYPE_VOLATILITY`. Add card-title expectations for return threshold, price range, and streak.

```java
assertEquals("黄金涨跌幅 大于 3%", GoldMarketCardPresenter.displayTitle("黄金涨跌幅 大于 3%", 0));
assertEquals("黄金价格 位于 4000-4200USD/盎司", GoldMarketCardPresenter.displayTitle("黄金价格 位于 4000-4200USD/盎司", 0));
assertEquals("黄金价格 连续上涨 3天", GoldMarketCardPresenter.displayTitle("黄金价格 连续上涨 3天", 0));
```

- [ ] **Step 2: Verify focused tests fail**

Run: `./gradlew testDebugUnitTest --tests 'com.example.brokerfi.xc.agent.gold.GoldCreatePromptContractTest' --tests 'com.example.brokerfi.xc.agent.gold.GoldMarketCardPresenterTest'`

Expected: FAIL because the prompt and presenters still use legacy templates.

- [ ] **Step 3: Replace the AI parser contract**

Move the prompt text to a package-visible builder method. Require output fields `type`, type-specific parameters, `startDaysFromNow`, `durationDays`, `directionIdx`, `operatorIdx`, `liquidity`, and `confidence`. Reject confidence below 0.7 and every unsupported type locally.

- [ ] **Step 4: Replace manual cards and controls**

Build the grid from `GoldMarketTemplateCatalog`. Hide the time picker, set both calendars through the creation policy, show Beijing dates, and display a validation message for adjusted invalid boundaries. Replace technical indicator controls with a second numeric input for price range and a day-count input for streak.

- [ ] **Step 5: Build metadata through the shared policy**

Remove the legacy `buildResolutionRule` switch from the activity. Call `GoldMarketCreationPolicy.buildRule(...)`, and use generated start/end epochs for title, condition, summary, metadata, and contract duration. Show the Chainlink source and boundary policy in the confirmation dialog.

- [ ] **Step 6: Run Android unit tests and compile**

Run: `./gradlew testDebugUnitTest`

Run: `./gradlew assembleDebug`

Expected: PASS and produce `app/build/outputs/apk/debug/app-debug.apk`.

- [ ] **Step 7: Commit Android workflow changes**

```bash
git add app/src/main app/src/test
git commit -m "feat: create only resolvable Chainlink markets"
```

### Task 10: End-to-End Fixtures, Documentation, and Final Verification

**Files:**
- Create: `internal/sentinel/v2_settlement_integration_test.go`
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-07-14-chainlink-deterministic-market-templates-design.md` only if implementation reveals a necessary clarification.
- Modify: `PredictionMarket_Simulator/README.md` only for verified command examples.
- Modify: `brokerwallet-ai-agent/README.md` only for verified backend prerequisites.

**Interfaces:**
- Verifies: six template fixtures travel from canonical metadata through Chainlink evidence, deterministic calculation, final arbiter, and settlement transaction encoding.

- [ ] **Step 1: Add six end-to-end fixture cases**

```go
func TestVersion2TemplatesReachExpectedSettlementOrExplicitPending(t *testing.T) {
	for _, fixture := range version2Fixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			watcher := watcherWithChainlinkRoundsAndFinalArbiter(t, fixture.rounds, fixture.finalDecision)
			err := watcher.evaluateAndResolve(context.Background(), fixture.game)
			if fixture.pendingReason != "" {
				if err == nil || !strings.Contains(err.Error(), fixture.pendingReason) { t.Fatalf("err=%v", err) }
				return
			}
			if err != nil { t.Fatal(err) }
			assertResolveGameCall(t, fixture.expectedWinner)
		})
	}
}
```

- [ ] **Step 2: Run backend tests without starting trading services**

Run from `PredictionMarket`: `go test ./...`

Expected: PASS. If the existing `start.sh` fixture remains absent, run all unaffected packages explicitly and report that pre-existing test gap rather than modifying unrelated startup behavior.

Run: `go vet ./...`

Run: `go build ./...`

Expected: PASS.

- [ ] **Step 3: Run an opt-in public RPC smoke test**

Run: `PREDICTIONMARKET_CHAINLINK_LIVE_TEST=1 go test ./internal/chainlinkfeed -run TestLiveEthereumFeeds -v`

Expected: XAU and BTC return positive prices, nonzero round IDs, and source timestamps not in the future. Do not start `go run .` because it can trade or settle configured markets.

- [ ] **Step 4: Run Simulator verification**

Run from `PredictionMarket_Simulator`: `go test ./...`

Run: `go run . --mode preview --scenario create_and_trade`

Expected: generated plan contains only six version 2 types with Beijing-midnight boundaries; preview sends no transaction.

- [ ] **Step 5: Run Android verification**

Run from `brokerwallet-ai-agent`: `./gradlew testDebugUnitTest assembleDebug`

Expected: PASS.

- [ ] **Step 6: Verify no legacy creation strings remain**

Run from workspace root:

```bash
rg -n "addTemplate\(.*(交易量|技术指标|事件驱动|极值触碰)|TYPE_(VOLUME|TECHNICAL|EVENT|TOUCH|VOLATILITY)" brokerwallet-ai-agent/app/src/main PredictionMarket_Simulator/internal
```

Expected: legacy strings appear only in compatibility rendering/parsing code and legacy constants explicitly documented as non-creatable.

- [ ] **Step 7: Document verified operation and failure states**

Document required YAML fields, public RPC failover, Beijing boundary restrictions, six templates, AI provider prerequisites, `INDETERMINATE` retry behavior, preview command, and the fact that Data Streams subscriptions and browser automation are not required.

- [ ] **Step 8: Review diffs without reverting user changes**

Run separately in all three repositories: `git status --short`, `git diff --check`, and `git diff --stat`.

Expected: no whitespace errors; unrelated build artifacts and pre-existing user edits remain untouched.

- [ ] **Step 9: Commit final verified tests and documentation per repository**

```bash
git add internal/sentinel/v2_settlement_integration_test.go README.md docs
git commit -m "test: verify deterministic Chainlink settlements"
```

Use repository-local commits for Simulator and Android documentation so no repository receives paths owned by another repository.
