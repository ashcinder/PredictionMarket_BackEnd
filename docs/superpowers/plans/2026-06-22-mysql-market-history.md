# MySQL Market History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the AI-managed engine's process-local market history with required MySQL 8 persistence, audit every rule/model decision, and expose a read-only market-history API.

**Architecture:** Keep SQL behind repository interfaces in `internal/aimanaged`; production uses a MySQL repository while unit tests use the existing in-memory implementation/fakes. A small `internal/database` package owns connection setup and versioned embedded migrations. Startup fails if MySQL cannot connect or migrate, and no model call or trade proceeds after a required persistence operation fails.

**Tech Stack:** Go 1.22, `database/sql`, `github.com/go-sql-driver/mysql` v1.9.3, `github.com/DATA-DOG/go-sqlmock` v1.5.2, MySQL 8, embedded SQL migrations, YAML, `net/http`.

---

## File map

- `internal/config/config.go`, `config_test.go`: strict MySQL YAML fields and validation.
- `internal/database/mysql.go`, `mysql_test.go`: open/configure/ping MySQL without leaking the DSN.
- `internal/database/migrations.go`, `migrations_test.go`: advisory-locked embedded migrations.
- `internal/database/migrations/001_market_persistence.sql`: `market_history` and `ai_decisions` schema.
- `internal/aimanaged/persistence.go`: history and decision repository contracts plus domain records.
- `internal/aimanaged/history.go`, `history_test.go`: in-memory repository used by tests and reserve-to-observation conversion.
- `internal/aimanaged/mysql_repository.go`, `mysql_repository_test.go`: production history/decision SQL.
- `internal/aimanaged/manager.go`, `manager_test.go`: persistence gates and decision audit orchestration.
- `internal/aimanaged/history_handler.go`, `history_handler_test.go`: read-only history endpoint.
- `main.go`: fail-fast database wiring and clean shutdown.
- `config.example.yaml`, ignored `config.yaml`, `SETUP.md`, `docker-compose.mysql.yml`: local/runtime setup.
- `internal/aimanaged/mysql_integration_test.go`: optional real-MySQL validation guarded by build tag and a `_test` database-name check.

Do not modify, format, stage, or commit any `agent/` file. The eight existing `agent/` modifications belong to the user.

---

### Task 1: Add strict MySQL configuration and dependencies

**Files:**

- Modify: `internal/config/config_test.go`
- Modify: `internal/config/config.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Write failing configuration tests**

Add this block to `validYAML`:

```yaml
mysql:
  dsn: "prediction:secret@tcp(127.0.0.1:3306)/prediction_market?charset=utf8mb4&parseTime=true&loc=UTC"
  max_open_connections: 10
  max_idle_connections: 5
  connection_max_lifetime_seconds: 300
```

Assert the loaded runtime fields:

```go
if cfg.MySQLDSN == "" || cfg.MySQLMaxOpenConnections != 10 ||
	 cfg.MySQLMaxIdleConnections != 5 || cfg.MySQLConnectionMaxLifetime != 300*time.Second {
	t.Fatalf("unexpected MySQL config: %+v", cfg)
}
```

Add invalid fixtures for an empty/placeholder DSN, malformed DSN, non-positive pool settings, idle connections greater than open connections, and `ai.history_max_points: 1001`. Assert returned errors mention `mysql.dsn` or the specific field but never contain `prediction:secret`.

- [ ] **Step 2: Run the red test**

Run:

```bash
go test ./internal/config
```

Expected: compile failure because `Config` has no MySQL fields.

- [ ] **Step 3: Add pinned dependencies**

Run:

```bash
go get github.com/go-sql-driver/mysql@v1.9.3
go get github.com/DATA-DOG/go-sqlmock@v1.5.2
```

The driver version is the latest official GitHub release verified while writing this plan. `sqlmock` is test-only even though Go records it in the module graph.

- [ ] **Step 4: Implement strict config parsing**

Add raw YAML fields and runtime fields:

```go
MySQLDSN                       string
MySQLMaxOpenConnections        int
MySQLMaxIdleConnections        int
MySQLConnectionMaxLifetime     time.Duration
```

Use `mysql.ParseDSN` to reject malformed DSNs. Reject blank values and values beginning with `replace-with-`; never wrap the parser error with the raw DSN. Validate all pool values as positive and `max_idle_connections <= max_open_connections`. Extend the existing AI history validation to require `history_max_points <= 1000`, matching the API limit.

- [ ] **Step 5: Run the green test and commit**

Run:

```bash
gofmt -w internal/config/config.go internal/config/config_test.go
go test ./internal/config
```

Expected: PASS.

Commit only config and module files:

```bash
git add internal/config/config.go internal/config/config_test.go go.mod go.sum
git commit -m "feat: configure mysql persistence"
```

---

### Task 2: Open MySQL and run embedded migrations

**Files:**

- Create: `internal/database/mysql.go`
- Create: `internal/database/mysql_test.go`
- Create: `internal/database/migrations.go`
- Create: `internal/database/migrations_test.go`
- Create: `internal/database/migrations/001_market_persistence.sql`

- [ ] **Step 1: Write failing connection helper tests**

Define the desired API in tests:

```go
type Config struct {
	DSN                   string
	MaxOpenConnections    int
	MaxIdleConnections    int
	ConnectionMaxLifetime time.Duration
}

func OpenMySQL(ctx context.Context, cfg Config) (*sql.DB, error)
func RunMigrations(ctx context.Context, db *sql.DB) error
```

Use `sqlmock` to prove migration failures are returned without DSN text and `db.Stats().MaxOpenConnections` receives the configured maximum. Test `splitMigrationStatements` with the exact `-- migration:split` delimiter and assert blank/comment-only fragments are removed.

- [ ] **Step 2: Run the red database test**

Run:

```bash
go test ./internal/database
```

Expected: package/functions do not exist.

- [ ] **Step 3: Add the first migration**

Create `001_market_persistence.sql` with:

```sql
CREATE TABLE IF NOT EXISTS market_history (
  contract_address VARCHAR(42) NOT NULL,
  game_id BIGINT UNSIGNED NOT NULL,
  observed_at BIGINT UNSIGNED NOT NULL,
  yes_percent DECIMAL(9,6) NOT NULL,
  no_percent DECIMAL(9,6) NOT NULL,
  reserve_no VARBINARY(32) NULL,
  reserve_yes VARBINARY(32) NULL,
  source VARCHAR(16) NOT NULL,
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (contract_address, game_id, observed_at),
  INDEX idx_market_history_latest (contract_address, game_id, observed_at DESC),
  CONSTRAINT chk_market_history_source CHECK (source IN ('chain','ipfs')),
  CONSTRAINT chk_market_history_percent CHECK (
    yes_percent BETWEEN 0 AND 100 AND no_percent BETWEEN 0 AND 100
  )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- migration:split
CREATE TABLE IF NOT EXISTS ai_decisions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  contract_address VARCHAR(42) NOT NULL,
  game_id BIGINT UNSIGNED NOT NULL,
  user_address VARCHAR(42) NOT NULL,
  observed_at BIGINT UNSIGNED NOT NULL,
  decision_source VARCHAR(16) NOT NULL,
  action VARCHAR(16) NOT NULL,
  confidence DECIMAL(7,6) NOT NULL,
  reason TEXT NOT NULL,
  history_points INT UNSIGNED NOT NULL,
  outcome VARCHAR(32) NOT NULL,
  tx_hash VARCHAR(80) NOT NULL DEFAULT '',
  error_summary VARCHAR(512) NOT NULL DEFAULT '',
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  INDEX idx_ai_decisions_market (contract_address, game_id, observed_at DESC),
  INDEX idx_ai_decisions_user (user_address, observed_at DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

- [ ] **Step 4: Implement fail-fast open and migrations**

`OpenMySQL` must parse the DSN, construct a connector with `mysql.NewConnector`, configure the pool, ping with a 10-second child context, and call `RunMigrations`. Errors use fixed messages such as `ping mysql: %w`; they never include the DSN.

Embed migrations with `//go:embed migrations/*.sql`. `RunMigrations` must:

1. Acquire `GET_LOCK('prediction_market_schema_migrations', 10)` and require result `1`.
2. Create `schema_migrations(version BIGINT PRIMARY KEY, applied_at TIMESTAMP(6) ...)`.
3. Read applied versions.
4. Execute each unapplied migration statement in order.
5. Insert the version only after every statement succeeds.
6. Always attempt `RELEASE_LOCK`.

Do not pretend MySQL DDL is transactionally rollbackable; migration SQL must be idempotent and safe to retry.

- [ ] **Step 5: Run tests and commit**

Run:

```bash
gofmt -w internal/database/*.go
go test ./internal/database
```

Expected: PASS.

```bash
git add internal/database
git commit -m "feat: initialize mysql schema"
```

---

### Task 3: Define persistence contracts and adapt the in-memory test repository

**Files:**

- Create: `internal/aimanaged/persistence.go`
- Modify: `internal/aimanaged/history.go`
- Modify: `internal/aimanaged/history_test.go`

- [ ] **Step 1: Write failing domain/repository tests**

Use these exact domain types:

```go
type MarketIdentity struct {
	ContractAddress string
	GameID          int
}

type HistoryObservation struct {
	Time       int64   `json:"time"`
	YesPercent float64 `json:"yes_percent"`
	NoPercent  float64 `json:"no_percent"`
	ReserveNO  *big.Int `json:"-"`
	ReserveYES *big.Int `json:"-"`
	Source     string  `json:"source"`
}

type HistoryRepository interface {
	MergeAndList(context.Context, MarketIdentity, []HistoryObservation, HistoryObservation, int) ([]HistoryObservation, error)
	List(context.Context, MarketIdentity, int) ([]HistoryObservation, error)
}
```

Define `RuleDecisionRecord`, `ModelDecisionRecord`, and:

```go
type DecisionRepository interface {
	RecordRule(context.Context, RuleDecisionRecord) error
	CreatePending(context.Context, ModelDecisionRecord) (int64, error)
	Finalize(context.Context, int64, string, string, string) error
}
```

Update reserve tests to assert source `chain` and copied raw reserves. Update in-memory history tests to call context-aware `MergeAndList` and `List`.

- [ ] **Step 2: Run the red history test**

Run:

```bash
go test ./internal/aimanaged -run 'Test(PointFromReserves|MarketHistory)'
```

Expected: compile failure for missing persistence types/new signatures.

- [ ] **Step 3: Implement contracts and test repository**

Change `pointFromReserves` to return `HistoryObservation` while preserving the current `big.Rat` percentage calculation. Copy both `big.Int` values so callers cannot mutate chain response memory.

Make `marketHistoryStore` implement `HistoryRepository`; keep it only for tests. It must preserve sorting, time-bucket dedupe, max-size behavior, copied slices, and copied reserve integers.

Add a converter that maps valid IPFS points to `HistoryObservation{Source:"ipfs"}` with nil reserves.

- [ ] **Step 4: Run tests and commit**

Run:

```bash
gofmt -w internal/aimanaged/persistence.go internal/aimanaged/history.go internal/aimanaged/history_test.go
go test ./internal/aimanaged -run 'Test(PointFromReserves|MarketHistory)'
```

Expected: PASS.

```bash
git add internal/aimanaged/persistence.go internal/aimanaged/history.go internal/aimanaged/history_test.go
git commit -m "refactor: define ai persistence contracts"
```

---

### Task 4: Implement MySQL history and decision repositories

**Files:**

- Create: `internal/aimanaged/mysql_repository.go`
- Create: `internal/aimanaged/mysql_repository_test.go`

- [ ] **Step 1: Write failing history SQL tests**

Using `sqlmock`, assert `MergeAndList`:

- begins a transaction;
- inserts each IPFS point with `INSERT IGNORE` and nil reserves;
- upserts the chain point with `ON DUPLICATE KEY UPDATE` including raw reserves and `source='chain'`;
- queries `ORDER BY observed_at DESC LIMIT ?`;
- reverses rows to ascending order;
- commits only after scanning succeeds;
- rolls back on any insert/query/scan error.

Test `List` rejects limits outside `1..1000`, parses nullable `VARBINARY(32)` into `big.Int`, and rejects stored values longer than 32 bytes.

- [ ] **Step 2: Write failing decision SQL tests**

Assert:

```go
RecordRule(ctx, record)
CreatePending(ctx, record) // inserts outcome=pending and returns LastInsertId
Finalize(ctx, id, outcome, txHash, errorSummary)
```

`Finalize` must require exactly one affected row. `sanitizeErrorSummary` removes line breaks and truncates to 512 UTF-8 bytes without splitting a rune.

- [ ] **Step 3: Run the red repository tests**

Run:

```bash
go test ./internal/aimanaged -run TestMySQLRepository
```

Expected: `NewMySQLRepository` and methods do not exist.

- [ ] **Step 4: Implement the repository**

Create:

```go
type MySQLRepository struct { db *sql.DB }

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}
```

Normalize addresses with `strings.ToLower(common.HexToAddress(address).Hex())`. Pass reserves as unsigned big-endian `big.Int.Bytes()` values and reject negative or longer-than-32-byte inputs; never convert uint256 values through `int64` or `float64`. Format percentages with six decimal places.

Each public repository method creates a bounded child context (10 seconds) unless the caller already has an earlier deadline.

- [ ] **Step 5: Run tests and commit**

Run:

```bash
gofmt -w internal/aimanaged/mysql_repository.go internal/aimanaged/mysql_repository_test.go
go test ./internal/aimanaged -run TestMySQLRepository
```

Expected: PASS.

```bash
git add internal/aimanaged/mysql_repository.go internal/aimanaged/mysql_repository_test.go
git commit -m "feat: persist market history in mysql"
```

---

### Task 5: Gate AI/trades on persistence and audit every decision

**Files:**

- Modify: `internal/aimanaged/manager.go`
- Modify: `internal/aimanaged/manager_test.go`

- [ ] **Step 1: Write failing engine persistence-gate tests**

Add configurable fake history and decision repositories. Cover:

1. History merge error: quote calls 0, model calls 0, sends 0.
2. Invalid reserves: `RecordRule` receives `rule/hold/invalid_reserves`; model/sends 0.
3. Insufficient history: `rule/hold/history_insufficient`; quote/model/sends 0.
4. Rule audit failure: process returns error and sends 0.
5. `CreatePending` failure after a model buy: sends 0.
6. Model HOLD finalizes `hold`.
7. Low confidence finalizes `low_confidence`.
8. Cooldown finalizes `cooldown`.
9. Buy success finalizes `traded` with tx hash.
10. Buy failure finalizes `trade_failed` and returns the transaction error.
11. Finalize failure after a successful broadcast returns an audit error but send count remains exactly 1.

- [ ] **Step 2: Run the red engine tests**

Run:

```bash
go test ./internal/aimanaged -run 'TestEngine.*(Persistence|Audit|History|Trade|Confidence|Hold)'
```

Expected: current engine has no injected repositories or audit calls.

- [ ] **Step 3: Refactor Engine dependencies**

Change constructor and fields:

```go
func NewEngine(cfg *config.Config, store *Store, ipfsClient *ipfs.Client,
	goldOracle *oracle.GoldOracle, histories HistoryRepository,
	decisions DecisionRepository) *Engine
```

Keep the AI model dependency under its existing `decisionSource` name; name the audit dependency `audits` to avoid ambiguity.

Implement the exact ordering from the approved design: repository history first; rule audits before returning; model pending row before confidence/cooldown/trade; final outcome after each branch. A failed required persistence call returns an error and prevents every not-yet-executed external side effect.

Use `HistoryObservation` values to build `ResearchContext`; keep the model JSON shape `{time,yes_percent,no_percent}` so this change does not inflate the prompt with reserves.

- [ ] **Step 4: Run all AI-managed tests and commit**

Run:

```bash
gofmt -w internal/aimanaged/manager.go internal/aimanaged/manager_test.go
go test ./internal/aimanaged
```

Expected: PASS, with no external calls.

```bash
git add internal/aimanaged/manager.go internal/aimanaged/manager_test.go
git commit -m "feat: audit ai managed decisions"
```

---

### Task 6: Add the read-only market-history API

**Files:**

- Create: `internal/aimanaged/history_handler.go`
- Create: `internal/aimanaged/history_handler_test.go`

- [ ] **Step 1: Write failing HTTP tests**

Define:

```go
type HistoryHandler struct {
	histories    HistoryRepository
	defaultLimit int
}

func NewHistoryHandler(histories HistoryRepository, defaultLimit int) *HistoryHandler
func (h *HistoryHandler) Register(mux *http.ServeMux)
```

Test `GET /api/gold/market-history` for:

- valid contract/game/default limit and ascending JSON;
- explicit limits 1 and 1000;
- invalid address, missing/non-positive game ID, and limits 0/1001 return 400;
- repository error returns 503 without database details;
- OPTIONS returns 204 and CORS headers;
- other methods return 405;
- reserves serialize as decimal strings or `null`, never JSON numbers.

- [ ] **Step 2: Run the red handler test**

Run:

```bash
go test ./internal/aimanaged -run TestMarketHistoryHandler
```

Expected: handler does not exist.

- [ ] **Step 3: Implement the handler**

Use response points:

```go
type historyResponsePoint struct {
	Time       int64   `json:"time"`
	YesPercent float64 `json:"yes_percent"`
	NoPercent  float64 `json:"no_percent"`
	ReserveNO  *string `json:"reserve_no"`
	ReserveYES *string `json:"reserve_yes"`
	Source     string  `json:"source"`
}
```

Validate with `common.IsHexAddress`, the existing positive integer parser, and `strconv.Atoi`. Log the repository error server-side; return only `{"error":"market history unavailable"}` to the client.

- [ ] **Step 4: Run tests and commit**

Run:

```bash
gofmt -w internal/aimanaged/history_handler.go internal/aimanaged/history_handler_test.go
go test ./internal/aimanaged -run TestMarketHistoryHandler
go test ./internal/aimanaged
```

Expected: PASS.

```bash
git add internal/aimanaged/history_handler.go internal/aimanaged/history_handler_test.go
git commit -m "feat: expose market history api"
```

---

### Task 7: Wire startup, local MySQL, and documentation

**Files:**

- Modify: `main.go`
- Modify: `config.example.yaml`
- Modify but never stage: `config.yaml`
- Modify: `SETUP.md`
- Create: `docker-compose.mysql.yml`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write failing repository-artifact tests**

Extend `TestRepositoryConfigurationArtifactsUseYAML` to require MySQL fields in the example and documentation, and require `docker-compose.mysql.yml` to exist with image `mysql:8`. Ensure substituting placeholder wallet, AI, and MySQL secrets produces a valid config.

- [ ] **Step 2: Run the red artifact test**

Run:

```bash
go test ./internal/config -run TestRepositoryConfigurationArtifactsUseYAML
```

Expected: example/docs/Compose assertions fail.

- [ ] **Step 3: Wire production startup**

After YAML load, call:

```go
db, err := database.OpenMySQL(context.Background(), database.Config{
	DSN: cfg.MySQLDSN,
	MaxOpenConnections: cfg.MySQLMaxOpenConnections,
	MaxIdleConnections: cfg.MySQLMaxIdleConnections,
	ConnectionMaxLifetime: cfg.MySQLConnectionMaxLifetime,
})
if err != nil {
	slog.Error("init mysql failed", "error", err)
	os.Exit(1)
}
defer db.Close()
repository := aimanaged.NewMySQLRepository(db)
```

Inject `repository` into `NewEngine`. Register both the existing managed endpoint and `NewHistoryHandler(repository, cfg.AIHistoryMaxPoints)`.

- [ ] **Step 4: Add local setup artifacts**

`docker-compose.mysql.yml` uses MySQL 8, healthcheck, named volume, database `prediction_market`, user `prediction`, and explicit development-only passwords. `SETUP.md` explains:

```bash
docker compose -f docker-compose.mysql.yml up -d
docker compose -f docker-compose.mysql.yml ps
go run main.go
```

Update `config.example.yaml` with placeholder DSN and pool defaults. Update ignored `config.yaml` with local development values without printing or staging it.

- [ ] **Step 5: Run tests and commit**

Run:

```bash
gofmt -w main.go internal/config/config_test.go
go test ./internal/config
go test ./...
```

Expected: PASS.

Stage exact paths only:

```bash
git add main.go config.example.yaml SETUP.md docker-compose.mysql.yml internal/config/config_test.go
git commit -m "feat: require mysql at startup"
```

---

### Task 8: Add optional real-MySQL integration coverage

**Files:**

- Create: `internal/aimanaged/mysql_integration_test.go`
- Modify: `SETUP.md`

- [ ] **Step 1: Write the integration test**

Add `//go:build integration`. Read `MYSQL_TEST_DSN`, parse it with the official driver, and require the database name to end in `_test`; otherwise call `t.Skip` for missing DSN or `t.Fatal` for an unsafe database name.

The test must run migrations, clear only `market_history` and `ai_decisions` in the dedicated test schema, and verify:

- the maximum uint256 value (`2^256-1`) round-trips exactly through `VARBINARY(32)`;
- IPFS insert cannot overwrite a chain row at the same key;
- concurrent same-bucket chain upserts produce one row;
- a pending decision can finalize to `traded` with a tx hash.

- [ ] **Step 2: Compile the integration test without a database**

Run:

```bash
go test -tags=integration ./internal/aimanaged -run TestMySQLIntegration
```

Expected without `MYSQL_TEST_DSN`: SKIP, package PASS.

- [ ] **Step 3: Document the optional command and commit**

Document a dedicated DSN example and:

```bash
MYSQL_TEST_DSN='prediction:dev-password@tcp(127.0.0.1:3306)/prediction_market_test?parseTime=true' \
  go test -tags=integration ./internal/aimanaged -run TestMySQLIntegration -v
```

```bash
git add internal/aimanaged/mysql_integration_test.go SETUP.md
git commit -m "test: cover mysql persistence integration"
```

---

### Task 9: Final verification, scope audit, and master push

**Files:** all files listed above, plus this plan and the approved design.

- [ ] **Step 1: Re-index the knowledge graph**

Run codebase-memory moderate indexing for the repository so new database and API symbols are discoverable. Search for `MySQLRepository`, `OpenMySQL`, and `HistoryHandler`, then trace outbound calls from `Engine.process` to confirm persistence gates precede AI/trade calls.

- [ ] **Step 2: Run fresh verification**

Run independently:

```bash
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./internal/aimanaged
go test -tags=integration ./internal/aimanaged -run TestMySQLIntegration
```

Expected: all exit 0; integration test skips only when `MYSQL_TEST_DSN` is absent.

- [ ] **Step 3: Audit secrets and scope**

Confirm:

- `config.yaml` is ignored and unstaged;
- staged diff contains no real DSN, private key, API key, GitHub token, or production password;
- no `agent/` path is staged;
- only placeholder/development credentials appear in public artifacts;
- `git diff --cached --check` exits 0.

- [ ] **Step 4: Commit any final plan/docs updates**

```bash
git add docs/superpowers/plans/2026-06-22-mysql-market-history.md docs/superpowers/specs/2026-06-22-mysql-market-history-design.md
git commit -m "docs: plan mysql market persistence"
```

If those documents are already committed and unchanged, do not create an empty commit.

- [ ] **Step 5: Push master**

```bash
git push origin master
```

After push, verify `git rev-parse master origin/master` prints the same SHA and report the remaining unstaged `agent/` files separately as user-owned work.

---

## Acceptance checklist

- MySQL 8 is a required startup dependency with strict, secret-safe YAML configuration.
- Migrations are embedded, advisory-locked, versioned, idempotent, and restart-safe under MySQL DDL semantics.
- Market history survives process restart and is shared by users/instances through a unique database key.
- IPFS seeds cannot overwrite a chain observation.
- AI never runs below the history threshold and a trade never starts before its pending audit record exists.
- Every rule/model outcome is auditable without storing wallet private keys.
- The history API is read-only, validated, bounded, and returns ascending data.
- Default tests need no network, MySQL, AI, or chain; optional integration tests are safe-gated to a `_test` schema.
- User-owned `agent/` changes and ignored `config.yaml` secrets remain outside every commit.
