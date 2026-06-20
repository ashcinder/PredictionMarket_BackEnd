# AI Managed YAML Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move every Go-backend runtime setting into one ignored `config.yaml` and verify the AI-managed enable/decide/trade path without external traffic or real transactions.

**Architecture:** Parse a strict, grouped YAML document into the existing flat runtime `Config`, with no XML or environment-variable fallback. Introduce narrow AI-managed dependency interfaces so production keeps using the existing chain/IPFS/oracle/AI clients while tests use in-memory doubles.

**Tech Stack:** Go 1.22, `gopkg.in/yaml.v3`, `net/http/httptest`, Go Ethereum crypto helpers, standard `testing` package.

---

## File map

- Modify `internal/config/config.go`: strict YAML parsing, validation, and `AIAPIKey` runtime field.
- Create `internal/config/config_test.go`: YAML loading and validation tests.
- Modify `go.mod` and `go.sum`: add `gopkg.in/yaml.v3`.
- Create `config.example.yaml`: committed complete configuration template.
- Create local ignored `config.yaml`: safe placeholder copy for entering real secrets locally.
- Modify `.gitignore`: ignore `config.yaml` and retain protection for legacy `config.xml`.
- Modify `start.sh`: check and launch from YAML.
- Modify `SETUP.md`: document YAML-only setup and simulated tests.
- Modify `internal/aimanaged/manager.go`: read AI key from `Config` and add injectable engine boundaries.
- Create `internal/aimanaged/manager_test.go`: endpoint, AI HTTP, hold/low-confidence, and simulated trade tests.
- Modify `internal/judge/game.go`: correctly parse bilingual comparison thresholds.
- Modify `internal/judge/game_test.go`: isolate comparison parsing and preserve the two failing template regressions.

### Task 0: Repair baseline comparison-threshold parsing

**Files:**
- Modify: `internal/judge/game_test.go`
- Modify: `internal/judge/game.go`

- [ ] **Step 1: Add a focused failing parser test**

Append this test to `internal/judge/game_test.go`:

```go
func TestParseComparisonThreshold(t *testing.T) {
    tests := []struct {
        name string
        condition string
        want float64
        ok bool
    }{
        {"bilingual volume", "成交量 大于 (Above) 100 吨", 100, true},
        {"bilingual RSI", "指标 RSI (14) 大于 (Above) 60 (Indicator Option)", 60, true},
        {"Chinese only", "指标 RSI (14) 小于 30", 30, true},
        {"missing threshold", "指标 RSI (14) 大于 (Above)", 0, false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, ok := parseComparisonThreshold(tt.condition)
            if ok != tt.ok || got != tt.want {
                t.Fatalf("parseComparisonThreshold() = (%v, %v), want (%v, %v)", got, ok, tt.want, tt.ok)
            }
        })
    }
}
```

- [ ] **Step 2: Run parser and existing template tests and verify RED**

Run: `go test ./internal/judge -run 'TestParseComparisonThreshold|TestEvaluateWinner_Template(3_Volume|4_Indicator)' -v`

Expected: build failure because `parseComparisonThreshold` does not exist; the two existing template cases remain known failures.

- [ ] **Step 3: Implement one comparison-threshold parser**

Add `regexp` to the imports in `internal/judge/game.go` and define:

```go
var comparisonThresholdPattern = regexp.MustCompile(`(?:大于|小于|等于)\s*(?:\([^)]*\))?\s*(-?\d+(?:\.\d+)?)`)

func parseComparisonThreshold(condition string) (float64, bool) {
    match := comparisonThresholdPattern.FindStringSubmatch(condition)
    if len(match) != 2 {
        return 0, false
    }
    value, err := strconv.ParseFloat(match[1], 64)
    if err != nil {
        return 0, false
    }
    return value, true
}
```

Use `parseComparisonThreshold(cond)` in both the `成交量` and `指标` branches. Remove `parseThresholdFromIndicator`; retain the existing greater-than, less-than, equal, and current simulated-value comparisons without changing their semantics.

- [ ] **Step 4: Verify the focused regression tests are GREEN**

Run: `go test ./internal/judge -run 'TestParseComparisonThreshold|TestEvaluateWinner_Template(3_Volume|4_Indicator)' -v`

Expected: all focused parser, volume, and indicator cases pass.

- [ ] **Step 5: Run the complete baseline package**

Run: `go test ./internal/judge -v`

Expected: every judge test passes.

- [ ] **Step 6: Commit the root-cause fix**

```bash
git add internal/judge/game.go internal/judge/game_test.go
git commit -m "fix: parse bilingual market thresholds"
```

### Task 1: Strict YAML configuration loader

**Files:**
- Create: `internal/config/config_test.go`
- Modify: `internal/config/config.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Write the failing YAML loader tests**

Create `internal/config/config_test.go` with a valid fixture and table-driven validation checks:

```go
package config

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

const validYAML = `chain:
  private_key: "0000000000000000000000000000000000000000000000000000000000000001"
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
  rpc_url: ""
  broker_chain_url: "https://dash.broker-chain.com:443/"
  use_broker_chain: true
server:
  http_listen: ":8081"
ipfs:
  gateway: "http://127.0.0.1:8080/ipfs"
sentinel:
  poll_interval_seconds: 30
  resolve_delay_seconds: 5
ai:
  api_key: "test-ai-key"
  base_url: "https://api.deepseek.com/chat/completions"
  model: "deepseek-chat"
  poll_interval_seconds: 120
  buy_amount_bkc: "10"
  confidence_min: 0.70
`

func writeTestConfig(t *testing.T, body string) string {
    t.Helper()
    path := filepath.Join(t.TempDir(), "config.yaml")
    if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
        t.Fatal(err)
    }
    return path
}

func TestLoadFileReadsCompleteYAML(t *testing.T) {
    cfg, err := LoadFile(writeTestConfig(t, validYAML))
    if err != nil {
        t.Fatal(err)
    }
    if cfg.AIAPIKey != "test-ai-key" || cfg.AIModel != "deepseek-chat" {
        t.Fatalf("unexpected AI config: %+v", cfg)
    }
    if cfg.IPFSGateway != "http://127.0.0.1:8080/ipfs/" {
        t.Fatalf("IPFS gateway was not normalized: %q", cfg.IPFSGateway)
    }
    if cfg.PollInterval != 30*time.Second || cfg.AIPollInterval != 120*time.Second {
        t.Fatalf("unexpected intervals: poll=%s ai=%s", cfg.PollInterval, cfg.AIPollInterval)
    }
}

func TestLoadFileRejectsInvalidConfiguration(t *testing.T) {
    tests := map[string]string{
        "wallet key": strings.Replace(validYAML,
            "0000000000000000000000000000000000000000000000000000000000000001",
            "replace-with-wallet-private-key", 1),
        "AI key": strings.Replace(validYAML, "test-ai-key", "replace-with-ai-api-key", 1),
        "contract": strings.Replace(validYAML,
            "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c", "not-an-address", 1),
        "RPC mode without URL": strings.Replace(validYAML,
            "use_broker_chain: true", "use_broker_chain: false", 1),
        "AI URL": strings.Replace(validYAML,
            "https://api.deepseek.com/chat/completions", "ftp://invalid", 1),
        "poll interval": strings.Replace(validYAML,
            "poll_interval_seconds: 30", "poll_interval_seconds: 0", 1),
        "confidence": strings.Replace(validYAML,
            "confidence_min: 0.70", "confidence_min: 1.1", 1),
        "buy amount": strings.Replace(validYAML,
            `buy_amount_bkc: "10"`, `buy_amount_bkc: "0"`, 1),
        "unknown field": validYAML + "unexpected: true\n",
    }

    for name, body := range tests {
        t.Run(name, func(t *testing.T) {
            if _, err := LoadFile(writeTestConfig(t, body)); err == nil {
                t.Fatal("expected validation error")
            }
        })
    }
}

func TestLoadFileReportsMissingFile(t *testing.T) {
    _, err := LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))
    if err == nil || !strings.Contains(err.Error(), "read config") {
        t.Fatalf("unexpected error: %v", err)
    }
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run: `go test ./internal/config -run 'TestLoadFile' -v`

Expected: build failure because `LoadFile` and `AIAPIKey` do not exist.

- [ ] **Step 3: Add the YAML dependency**

Run: `go get gopkg.in/yaml.v3@v3.0.1`

Expected: `go.mod` gains a direct YAML dependency and `go.sum` is updated.

- [ ] **Step 4: Replace XML parsing with strict YAML parsing**

In `internal/config/config.go`, remove `encoding/xml`, `XMLConfig`, `loadFromXML`, `firstNonEmpty`, and the code-level default contract address. Define `ConfigFileName = "config.yaml"`, add `AIAPIKey string` to `Config`, and implement the following grouped input and loader:

```go
type fileConfig struct {
    Chain struct {
        PrivateKey      string `yaml:"private_key"`
        ContractAddress string `yaml:"contract_address"`
        RPCURL           string `yaml:"rpc_url"`
        BrokerChainURL   string `yaml:"broker_chain_url"`
        UseBrokerChain   bool   `yaml:"use_broker_chain"`
    } `yaml:"chain"`
    Server struct {
        HTTPListen string `yaml:"http_listen"`
    } `yaml:"server"`
    IPFS struct {
        Gateway string `yaml:"gateway"`
    } `yaml:"ipfs"`
    Sentinel struct {
        PollIntervalSeconds int `yaml:"poll_interval_seconds"`
        ResolveDelaySeconds int `yaml:"resolve_delay_seconds"`
    } `yaml:"sentinel"`
    AI struct {
        APIKey              string  `yaml:"api_key"`
        BaseURL             string  `yaml:"base_url"`
        Model               string  `yaml:"model"`
        PollIntervalSeconds int     `yaml:"poll_interval_seconds"`
        BuyAmountBKC        string  `yaml:"buy_amount_bkc"`
        ConfidenceMin       float64 `yaml:"confidence_min"`
    } `yaml:"ai"`
}

func Load() (*Config, error) {
    slog.Info("loading config from YAML file", "file", ConfigFileName)
    return LoadFile(ConfigFileName)
}

func LoadFile(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("read config %s: %w", path, err)
    }

    var raw fileConfig
    decoder := yaml.NewDecoder(bytes.NewReader(data))
    decoder.KnownFields(true)
    if err := decoder.Decode(&raw); err != nil {
        return nil, fmt.Errorf("parse YAML config %s: %w", path, err)
    }

    privateKey := strings.TrimSpace(raw.Chain.PrivateKey)
    if privateKey == "" || strings.HasPrefix(privateKey, "replace-with-") {
        return nil, errors.New("chain.private_key is required")
    }
    if _, err := crypto.HexToECDSA(strings.TrimPrefix(privateKey, "0x")); err != nil {
        return nil, errors.New("chain.private_key is invalid")
    }
    if !common.IsHexAddress(raw.Chain.ContractAddress) {
        return nil, errors.New("chain.contract_address is invalid")
    }

    apiKey := strings.TrimSpace(raw.AI.APIKey)
    if apiKey == "" || strings.HasPrefix(apiKey, "replace-with-") {
        return nil, errors.New("ai.api_key is required")
    }
    if strings.TrimSpace(raw.AI.Model) == "" {
        return nil, errors.New("ai.model is required")
    }
    if strings.TrimSpace(raw.Server.HTTPListen) == "" {
        return nil, errors.New("server.http_listen is required")
    }
    if raw.Sentinel.PollIntervalSeconds <= 0 {
        return nil, errors.New("sentinel.poll_interval_seconds must be positive")
    }
    if raw.Sentinel.ResolveDelaySeconds < 0 {
        return nil, errors.New("sentinel.resolve_delay_seconds must not be negative")
    }
    if raw.AI.PollIntervalSeconds <= 0 {
        return nil, errors.New("ai.poll_interval_seconds must be positive")
    }
    if raw.AI.ConfidenceMin < 0 || raw.AI.ConfidenceMin > 1 {
        return nil, errors.New("ai.confidence_min must be between 0 and 1")
    }
    amount, err := strconv.ParseFloat(strings.TrimSpace(raw.AI.BuyAmountBKC), 64)
    if err != nil || amount <= 0 {
        return nil, errors.New("ai.buy_amount_bkc must be positive")
    }

    brokerURL, err := requireHTTPURL("chain.broker_chain_url", raw.Chain.BrokerChainURL)
    if err != nil {
        return nil, err
    }
    rpcURL := strings.TrimSpace(raw.Chain.RPCURL)
    if !raw.Chain.UseBrokerChain {
        if rpcURL, err = requireHTTPURL("chain.rpc_url", rpcURL); err != nil {
            return nil, err
        }
    } else if rpcURL != "" {
        if rpcURL, err = requireHTTPURL("chain.rpc_url", rpcURL); err != nil {
            return nil, err
        }
    }
    ipfsGateway, err := requireHTTPURL("ipfs.gateway", raw.IPFS.Gateway)
    if err != nil {
        return nil, err
    }
    aiBaseURL, err := requireHTTPURL("ai.base_url", raw.AI.BaseURL)
    if err != nil {
        return nil, err
    }
    if !strings.HasSuffix(ipfsGateway, "/") {
        ipfsGateway += "/"
    }

    return &Config{
        PrivateKey:      privateKey,
        ContractAddress: common.HexToAddress(raw.Chain.ContractAddress).Hex(),
        RPCURL:          rpcURL,
        BrokerChainURL:  brokerURL,
        IPFSGateway:     ipfsGateway,
        PollInterval:    time.Duration(raw.Sentinel.PollIntervalSeconds) * time.Second,
        ResolveDelay:    time.Duration(raw.Sentinel.ResolveDelaySeconds) * time.Second,
        UseBrokerChain:  raw.Chain.UseBrokerChain,
        HTTPListen:      strings.TrimSpace(raw.Server.HTTPListen),
        AIAPIKey:        apiKey,
        AIBaseURL:       aiBaseURL,
        AIModel:         strings.TrimSpace(raw.AI.Model),
        AIPollInterval:  time.Duration(raw.AI.PollIntervalSeconds) * time.Second,
        AIBuyAmountBKC:  strings.TrimSpace(raw.AI.BuyAmountBKC),
        AIConfidenceMin: raw.AI.ConfidenceMin,
    }, nil
}

func requireHTTPURL(field, value string) (string, error) {
    value = strings.TrimSpace(value)
    parsed, err := url.ParseRequestURI(value)
    if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
        return "", fmt.Errorf("%s must be an HTTP(S) URL", field)
    }
    return value, nil
}
```

Add imports for `bytes`, `errors`, `net/url`, `github.com/ethereum/go-ethereum/common`, `github.com/ethereum/go-ethereum/crypto`, and `gopkg.in/yaml.v3` while retaining the existing runtime `Config` fields.

Run: `go mod tidy`

Expected: YAML is a direct dependency and dependencies used only by deleted code are removed if no package references them.

- [ ] **Step 5: Run the focused and package tests and verify GREEN**

Run: `go test ./internal/config -v`

Expected: all configuration tests pass.

- [ ] **Step 6: Commit the configuration loader**

```bash
git add internal/config/config.go internal/config/config_test.go go.mod go.sum
git commit -m "feat: load backend configuration from yaml"
```

### Task 2: Safe YAML artifacts and setup instructions

**Files:**
- Create: `config.example.yaml`
- Create locally: `config.yaml` (ignored)
- Modify: `.gitignore`
- Modify: `start.sh`
- Modify: `SETUP.md`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Add a failing repository-artifact test**

Append this test to `internal/config/config_test.go`:

```go
func TestRepositoryConfigurationArtifactsUseYAML(t *testing.T) {
    root := filepath.Clean(filepath.Join("..", ".."))
    example, err := os.ReadFile(filepath.Join(root, "config.example.yaml"))
    if err != nil {
        t.Fatal(err)
    }
    usable := strings.ReplaceAll(string(example),
        "replace-with-wallet-private-key",
        "0000000000000000000000000000000000000000000000000000000000000001")
    usable = strings.ReplaceAll(usable, "replace-with-ai-api-key", "test-ai-key")
    if _, err := LoadFile(writeTestConfig(t, usable)); err != nil {
        t.Fatalf("example config is not valid after inserting secrets: %v", err)
    }

    for _, name := range []string{"start.sh", "SETUP.md"} {
        body, err := os.ReadFile(filepath.Join(root, name))
        if err != nil {
            t.Fatal(err)
        }
        if strings.Contains(string(body), "config.example.xml") ||
            strings.Contains(string(body), "读取 config.xml") {
            t.Fatalf("%s still instructs users to configure XML", name)
        }
        if !strings.Contains(string(body), "config.yaml") {
            t.Fatalf("%s does not mention config.yaml", name)
        }
    }
}
```

- [ ] **Step 2: Run the artifact test and verify RED**

Run: `go test ./internal/config -run TestRepositoryConfigurationArtifactsUseYAML -v`

Expected: failure because `config.example.yaml` does not exist.

- [ ] **Step 3: Create the complete committed template**

Create `config.example.yaml` with exactly the grouped document from the approved design:

```yaml
chain:
  private_key: "replace-with-wallet-private-key"
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
  rpc_url: ""
  broker_chain_url: "https://dash.broker-chain.com:443/"
  use_broker_chain: true

server:
  http_listen: ":8081"

ipfs:
  gateway: "http://127.0.0.1:8080/ipfs/"

sentinel:
  poll_interval_seconds: 30
  resolve_delay_seconds: 5

ai:
  api_key: "replace-with-ai-api-key"
  base_url: "https://api.deepseek.com/chat/completions"
  model: "deepseek-chat"
  poll_interval_seconds: 120
  buy_amount_bkc: "10"
  confidence_min: 0.70
```

Create local `config.yaml` with the same safe content so the user has one known location to enter the two real secrets. Do not stage this file.

- [ ] **Step 4: Update ignore and startup behavior**

Keep `config.xml` ignored for leak prevention and add `config.yaml` to `.gitignore`. Replace `start.sh` with:

```bash
#!/bin/bash
set -euo pipefail

if [ ! -f config.yaml ]; then
    echo "错误：未找到 config.yaml"
    echo "请先执行：cp config.example.yaml config.yaml"
    exit 1
fi

if grep -Eq 'replace-with-(wallet-private-key|ai-api-key)' config.yaml; then
    echo "错误：请在 config.yaml 中填写钱包私钥和 AI API Key"
    exit 1
fi

echo "配置文件已准备好，启动 PredictionMarket 后端"
go run main.go
```

- [ ] **Step 5: Rewrite setup documentation around YAML and safe simulation**

Update `SETUP.md` so its quick start is `cp config.example.yaml config.yaml`, describes every grouped key, states that `config.yaml` is ignored, removes XML/environment-variable instructions, and documents `go test ./...` as the no-network AI-managed verification. Include explicit warnings that the automated tests never broadcast a transaction and that `go run main.go` uses real configured services.

- [ ] **Step 6: Verify artifacts and ignore protection**

Run: `go test ./internal/config -run TestRepositoryConfigurationArtifactsUseYAML -v && git check-ignore config.yaml`

Expected: test passes and `git check-ignore` prints `config.yaml`.

- [ ] **Step 7: Commit the safe configuration artifacts**

```bash
git add .gitignore config.example.yaml start.sh SETUP.md internal/config/config_test.go
git commit -m "docs: centralize backend settings in yaml"
```

### Task 3: Make the AI client consume the YAML API key

**Files:**
- Create: `internal/aimanaged/manager_test.go`
- Modify: `internal/aimanaged/manager.go`

- [ ] **Step 1: Write a failing mock-AI HTTP test**

Create `internal/aimanaged/manager_test.go`:

```go
package aimanaged

import (
    "context"
    "encoding/json"
    "math/big"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "PredictionMarket/internal/chain"
    "PredictionMarket/internal/config"
    "PredictionMarket/internal/ipfs"
    "PredictionMarket/internal/oracle"
)

func TestAIClientUsesConfiguredKeyAndModel(t *testing.T) {
    var authorization string
    var model string
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        authorization = r.Header.Get("Authorization")
        var body struct {
            Model string `json:"model"`
        }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
            t.Error(err)
        }
        model = body.Model
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"action\":\"hold\",\"confidence\":0.4,\"reason\":\"test\"}"}}]}`))
    }))
    defer server.Close()

    t.Setenv("DEEPSEEK_API_KEY", "environment-key-must-not-win")
    client := NewAIClient(&config.Config{
        AIAPIKey:  "yaml-key",
        AIBaseURL: server.URL,
        AIModel:   "yaml-model",
    })
    decision, err := client.Decide(context.Background(),
        &chain.GameInfo{ID: 1, TotalPool: big.NewInt(0), DeadlineRaw: time.Now().Add(time.Hour).UnixMilli()},
        &chain.GameExtraData{VirtualReservesNOYES: []*big.Int{big.NewInt(0), big.NewInt(0)}},
        &ipfs.Metadata{},
        &oracle.Quote{PriceUSD: 2300, QuoteSource: "test"},
    )
    if err != nil {
        t.Fatal(err)
    }
    if authorization != "Bearer yaml-key" || model != "yaml-model" {
        t.Fatalf("AI request used wrong config: auth=%q model=%q", authorization, model)
    }
    if decision.Action != "hold" {
        t.Fatalf("unexpected decision: %+v", decision)
    }
}
```

- [ ] **Step 2: Run the AI client test and verify RED**

Run: `go test ./internal/aimanaged -run TestAIClientUsesConfiguredKeyAndModel -v`

Expected: failure because `NewAIClient` uses `DEEPSEEK_API_KEY` instead of `Config.AIAPIKey`.

- [ ] **Step 3: Read the key only from Config**

Replace `NewAIClient` with:

```go
func NewAIClient(cfg *config.Config) *AIClient {
    return &AIClient{
        baseURL:    cfg.AIBaseURL,
        model:      cfg.AIModel,
        apiKey:     strings.TrimSpace(cfg.AIAPIKey),
        httpClient: &http.Client{Timeout: 45 * time.Second},
    }
}
```

Change the empty-key error in `Decide` to `ai.api_key is required`. Remove the now-unused `os` import from `manager.go` if no other code needs it.

- [ ] **Step 4: Run the focused package tests and verify GREEN**

Run: `go test ./internal/aimanaged -run TestAIClientUsesConfiguredKeyAndModel -v`

Expected: test passes and the mock server receives `Bearer yaml-key` and `yaml-model`.

- [ ] **Step 5: Commit the YAML-backed AI client**

```bash
git add internal/aimanaged/manager.go internal/aimanaged/manager_test.go
git commit -m "fix: source ai credentials from yaml config"
```

### Task 4: Characterize AI-managed enable and disable API behavior

**Files:**
- Modify: `internal/aimanaged/manager_test.go`

- [ ] **Step 1: Add the endpoint characterization test**

Append a test that uses only an in-memory store and generated test key:

```go
func TestAIManagedEndpointEnablesQueriesAndDisables(t *testing.T) {
    key, err := crypto.GenerateKey()
    if err != nil {
        t.Fatal(err)
    }
    privateKey := hexutil.Encode(crypto.FromECDSA(key))
    user := crypto.PubkeyToAddress(key.PublicKey).Hex()
    contract := "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"

    store, err := NewStore()
    if err != nil {
        t.Fatal(err)
    }
    mux := http.NewServeMux()
    NewServer(store).Register(mux)

    post := func(enabled bool, key string) *httptest.ResponseRecorder {
        t.Helper()
        payload, err := json.Marshal(SetRequest{
            GameID: 1, UserAddress: user, Enabled: enabled,
            ContractAddress: contract, PrivateKey: key,
        })
        if err != nil {
            t.Fatal(err)
        }
        recorder := httptest.NewRecorder()
        mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost,
            "/api/gold/ai-managed", bytes.NewReader(payload)))
        return recorder
    }
    get := func() bool {
        t.Helper()
        recorder := httptest.NewRecorder()
        target := "/api/gold/ai-managed?game_id=1&user_address=" + url.QueryEscape(user)
        mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
        var response map[string]bool
        if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
            t.Fatal(err)
        }
        return response["enabled"]
    }

    if response := post(true, privateKey); response.Code != http.StatusOK {
        t.Fatalf("enable failed: status=%d body=%s", response.Code, response.Body.String())
    }
    if !get() {
        t.Fatal("managed entry was not enabled")
    }
    if response := post(false, ""); response.Code != http.StatusOK {
        t.Fatalf("disable failed: status=%d body=%s", response.Code, response.Body.String())
    }
    if get() {
        t.Fatal("managed entry was not disabled")
    }
}
```

Add imports for `bytes`, `net/url`, `github.com/ethereum/go-ethereum/common/hexutil`, and `github.com/ethereum/go-ethereum/crypto`.

- [ ] **Step 2: Run the characterization test**

Run: `go test ./internal/aimanaged -run TestAIManagedEndpointEnablesQueriesAndDisables -v`

Expected: PASS, documenting the existing endpoint contract before engine refactoring.

- [ ] **Step 3: Commit the endpoint coverage**

```bash
git add internal/aimanaged/manager_test.go
git commit -m "test: cover ai managed endpoint lifecycle"
```

### Task 5: Simulate hold, low-confidence, and successful AI trades

**Files:**
- Modify: `internal/aimanaged/manager.go`
- Modify: `internal/aimanaged/manager_test.go`

- [ ] **Step 1: Add failing engine tests with in-memory doubles**

Append these doubles and tests to `internal/aimanaged/manager_test.go`:

```go
type fakeManagedChain struct {
    wallet    string
    info      *chain.GameInfo
    extra     *chain.GameExtraData
    sendCount int
    option    int
    value     *big.Int
}

func (f *fakeManagedChain) WalletAddress() string { return f.wallet }
func (f *fakeManagedChain) Close()                {}
func (f *fakeManagedChain) GetGameInfo(context.Context, int) (*chain.GameInfo, error) {
    return f.info, nil
}
func (f *fakeManagedChain) GetGameExtraData(context.Context, int, string) (*chain.GameExtraData, error) {
    return f.extra, nil
}
func (f *fakeManagedChain) BuyShares(_ context.Context, _ int, option int, value *big.Int) (string, error) {
    f.sendCount++
    f.option = option
    f.value = new(big.Int).Set(value)
    return "0xtest", nil
}

type staticMetadata struct{ value *ipfs.Metadata }
func (s staticMetadata) DownloadMetadata(string) (*ipfs.Metadata, error) { return s.value, nil }

type staticQuote struct{ value *oracle.Quote }
func (s staticQuote) FetchQuote() (*oracle.Quote, error) { return s.value, nil }

type staticDecision struct{ value *Decision }
func (s staticDecision) Decide(context.Context, *chain.GameInfo, *chain.GameExtraData, *ipfs.Metadata, *oracle.Quote) (*Decision, error) {
    return s.value, nil
}

func newManagedTestEntry(t *testing.T) (*Store, EntrySnapshot, string) {
    t.Helper()
    key, err := crypto.GenerateKey()
    if err != nil {
        t.Fatal(err)
    }
    user := crypto.PubkeyToAddress(key.PublicKey).Hex()
    store, err := NewStore()
    if err != nil {
        t.Fatal(err)
    }
    if err := store.Enable(SetRequest{
        GameID: 1, UserAddress: user, Enabled: true,
        ContractAddress: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
        PrivateKey: hexutil.Encode(crypto.FromECDSA(key)),
    }); err != nil {
        t.Fatal(err)
    }
    return store, store.Entries()[0], user
}

func newTestEngine(store *Store, client *fakeManagedChain, decision *Decision) *Engine {
    return &Engine{
        cfg: &config.Config{AIConfidenceMin: 0.70, AIBuyAmountBKC: "2.5"},
        store: store,
        newChain: func(string, string) (managedChain, error) { return client, nil },
        metadata: staticMetadata{value: &ipfs.Metadata{}},
        quotes: staticQuote{value: &oracle.Quote{PriceUSD: 2300, QuoteSource: "test"}},
        decisions: staticDecision{value: decision},
    }
}

func TestEngineDoesNotTradeHoldOrLowConfidence(t *testing.T) {
    tests := map[string]*Decision{
        "hold": {Action: "hold", Confidence: 1, Reason: "wait"},
        "low confidence": {Action: "buy_yes", Confidence: 0.69, Reason: "weak"},
    }
    for name, decision := range tests {
        t.Run(name, func(t *testing.T) {
            store, snapshot, user := newManagedTestEntry(t)
            client := &fakeManagedChain{
                wallet: user,
                info: &chain.GameInfo{ID: 1, TotalPool: big.NewInt(0), DeadlineRaw: time.Now().Add(time.Hour).UnixMilli()},
                extra: &chain.GameExtraData{VirtualReservesNOYES: []*big.Int{big.NewInt(0), big.NewInt(0)}},
            }
            if err := newTestEngine(store, client, decision).process(context.Background(), snapshot); err != nil {
                t.Fatal(err)
            }
            if client.sendCount != 0 {
                t.Fatalf("unexpected transactions: %d", client.sendCount)
            }
        })
    }
}

func TestEngineSendsAndRecordsOneSimulatedTrade(t *testing.T) {
    store, snapshot, user := newManagedTestEntry(t)
    client := &fakeManagedChain{
        wallet: user,
        info: &chain.GameInfo{ID: 1, TotalPool: big.NewInt(0), DeadlineRaw: time.Now().Add(time.Hour).UnixMilli()},
        extra: &chain.GameExtraData{VirtualReservesNOYES: []*big.Int{big.NewInt(0), big.NewInt(0)}},
    }
    engine := newTestEngine(store, client, &Decision{Action: "buy_yes", Confidence: 0.91, Reason: "strong"})
    if err := engine.process(context.Background(), snapshot); err != nil {
        t.Fatal(err)
    }
    if client.sendCount != 1 || client.option != 0 {
        t.Fatalf("unexpected simulated sends: count=%d option=%d", client.sendCount, client.option)
    }
    expected := new(big.Int).Mul(big.NewInt(25), big.NewInt(100000000000000000))
    if client.value == nil || client.value.Cmp(expected) != 0 {
        t.Fatalf("unexpected trade value: %v", client.value)
    }
    entries := store.Entries()
    if len(entries) != 1 || entries[0].LastTradeTx != "0xtest" {
        t.Fatalf("trade was not recorded: %+v", entries)
    }
}
```

- [ ] **Step 2: Run the engine tests and verify RED**

Run: `go test ./internal/aimanaged -run 'TestEngine' -v`

Expected: build failure because `managedChain`, `newChain`, `metadata`, `quotes`, and `decisions` do not exist.

- [ ] **Step 3: Add narrow injectable production interfaces**

In `internal/aimanaged/manager.go`, replace concrete engine dependencies with:

```go
type managedChain interface {
    WalletAddress() string
    Close()
    GetGameInfo(context.Context, int) (*chain.GameInfo, error)
    GetGameExtraData(context.Context, int, string) (*chain.GameExtraData, error)
    BuyShares(context.Context, int, int, *big.Int) (string, error)
}

type metadataSource interface {
    DownloadMetadata(string) (*ipfs.Metadata, error)
}

type quoteSource interface {
    FetchQuote() (*oracle.Quote, error)
}

type decisionSource interface {
    Decide(context.Context, *chain.GameInfo, *chain.GameExtraData, *ipfs.Metadata, *oracle.Quote) (*Decision, error)
}

type managedChainFactory func(privateKey, contractAddress string) (managedChain, error)

type Engine struct {
    cfg       *config.Config
    store     *Store
    newChain  managedChainFactory
    metadata  metadataSource
    quotes    quoteSource
    decisions decisionSource
}

type productionManagedChain struct{ client *chain.Client }

func (p *productionManagedChain) WalletAddress() string { return p.client.WalletAddress() }
func (p *productionManagedChain) Close()                { p.client.Close() }
func (p *productionManagedChain) GetGameInfo(ctx context.Context, gameID int) (*chain.GameInfo, error) {
    data, err := chain.EncodeGetGameInfo(gameID)
    if err != nil {
        return nil, err
    }
    encoded, err := p.client.EthCall(ctx, data)
    if err != nil {
        return nil, err
    }
    return chain.DecodeGetGameInfo(gameID, encoded)
}
func (p *productionManagedChain) GetGameExtraData(ctx context.Context, gameID int, user string) (*chain.GameExtraData, error) {
    data, err := chain.EncodeGetGameExtraData(gameID, user)
    if err != nil {
        return nil, err
    }
    encoded, err := p.client.EthCall(ctx, data)
    if err != nil {
        return nil, err
    }
    if encoded == "" || encoded == "0x" {
        return nil, errors.New("empty game extra data")
    }
    return chain.DecodeGetGameExtraData(encoded)
}
func (p *productionManagedChain) BuyShares(ctx context.Context, gameID, option int, value *big.Int) (string, error) {
    data, err := chain.EncodeBuyShares(gameID, option)
    if err != nil {
        return "", err
    }
    return p.client.SendTransaction(ctx, data, value)
}
```

Update the production constructor:

```go
func NewEngine(cfg *config.Config, store *Store, ipfsClient *ipfs.Client, goldOracle *oracle.GoldOracle) *Engine {
    return &Engine{
        cfg: cfg,
        store: store,
        newChain: func(privateKey, contractAddress string) (managedChain, error) {
            client, err := chain.NewClient(privateKey, contractAddress,
                cfg.RPCURL, cfg.BrokerChainURL, cfg.UseBrokerChain)
            if err != nil {
                return nil, err
            }
            return &productionManagedChain{client: client}, nil
        },
        metadata: ipfsClient,
        quotes: goldOracle,
        decisions: NewAIClient(cfg),
    }
}
```

- [ ] **Step 4: Route process through the interfaces**

Replace `process` with the interface-backed version below:

```go
func (e *Engine) process(ctx context.Context, snapshot EntrySnapshot) error {
    privateKey, err := e.store.DecryptPrivateKey(snapshot)
    if err != nil {
        return fmt.Errorf("decrypt private key: %w", err)
    }
    client, err := e.newChain(privateKey, snapshot.ContractAddress)
    if err != nil {
        return fmt.Errorf("init user chain client: %w", err)
    }
    defer client.Close()
    if !strings.EqualFold(client.WalletAddress(), snapshot.UserAddress) {
        e.store.Disable(snapshot.GameID, snapshot.UserAddress)
        return errors.New("private key no longer matches managed user")
    }

    info, err := client.GetGameInfo(ctx, snapshot.GameID)
    if err != nil {
        return fmt.Errorf("get game info: %w", err)
    }
    if info.IsResolved || info.IsRefunded || chain.IsDeadlinePassed(info.DeadlineRaw, time.Now().UnixMilli()) {
        e.store.Disable(snapshot.GameID, snapshot.UserAddress)
        slog.Info("ai-managed task removed inactive game", "game_id", snapshot.GameID, "user", snapshot.UserAddress)
        return nil
    }

    meta, err := e.metadata.DownloadMetadata(info.IPFSCID)
    if err != nil {
        slog.Warn("ai-managed metadata unavailable", "game_id", snapshot.GameID, "cid", info.IPFSCID, "error", err)
        meta = &ipfs.Metadata{}
    }

    extra := &chain.GameExtraData{VirtualReservesNOYES: []*big.Int{big.NewInt(0), big.NewInt(0)}}
    if decoded, extraErr := client.GetGameExtraData(ctx, snapshot.GameID, snapshot.UserAddress); extraErr == nil && decoded != nil {
        extra = decoded
    }

    quote, err := e.quotes.FetchQuote()
    if err != nil {
        return fmt.Errorf("fetch gold quote: %w", err)
    }
    decision, err := e.decisions.Decide(ctx, info, extra, meta, quote)
    if err != nil {
        return fmt.Errorf("ai decide: %w", err)
    }
    option, ok := decision.Option()
    if !ok || decision.Confidence < e.cfg.AIConfidenceMin {
        slog.Info("ai-managed skipped trade",
            "game_id", snapshot.GameID,
            "user", snapshot.UserAddress,
            "action", decision.Action,
            "confidence", decision.Confidence,
            "reason", decision.Reason,
        )
        return nil
    }
    if !e.store.CanTrade(snapshot.GameID, snapshot.UserAddress, option, time.Now()) {
        slog.Info("ai-managed skipped by cooldown", "game_id", snapshot.GameID, "user", snapshot.UserAddress, "option", option)
        return nil
    }

    value, err := parseBKCToWei(e.cfg.AIBuyAmountBKC)
    if err != nil {
        return fmt.Errorf("invalid ai buy amount: %w", err)
    }
    tx, err := client.BuyShares(ctx, snapshot.GameID, option, value)
    if err != nil {
        return fmt.Errorf("send buyShares tx: %w", err)
    }
    e.store.RecordTrade(snapshot.GameID, snapshot.UserAddress, option, tx)
    slog.Info("ai-managed buyShares sent",
        "game_id", snapshot.GameID,
        "user", snapshot.UserAddress,
        "option", option,
        "amount_bkc", e.cfg.AIBuyAmountBKC,
        "confidence", decision.Confidence,
        "tx", tx,
    )
    return nil
}
```

- [ ] **Step 5: Run AI-managed tests and verify GREEN**

Run: `go test ./internal/aimanaged -v`

Expected: endpoint, mock AI HTTP, hold/low-confidence, and one simulated trade tests all pass without external traffic.

- [ ] **Step 6: Run the race detector on the concurrent package**

Run: `go test -race ./internal/aimanaged`

Expected: PASS with no race reports.

- [ ] **Step 7: Commit the testable engine boundary**

```bash
git add internal/aimanaged/manager.go internal/aimanaged/manager_test.go
git commit -m "test: simulate ai managed trading flow"
```

### Task 6: Full verification and configuration safety audit

**Files:**
- Verify all changed files

- [ ] **Step 1: Format Go code**

Run: `gofmt -w internal/config/config.go internal/config/config_test.go internal/aimanaged/manager.go internal/aimanaged/manager_test.go`

Expected: command exits successfully.

- [ ] **Step 2: Run the entire test suite**

Run: `go test ./...`

Expected: all packages pass with zero failures.

- [ ] **Step 3: Run static analysis**

Run: `go vet ./...`

Expected: command exits with status 0 and no diagnostics.

- [ ] **Step 4: Verify the local placeholder config is rejected safely**

Run: `go run main.go`

Expected: non-zero exit with `chain.private_key is required`; output must not contain any private key or API Key value.

- [ ] **Step 5: Verify secrets and legacy configuration are not tracked**

Run: `git check-ignore config.yaml config.xml && git ls-files config.yaml config.xml`

Expected: both names are ignored and `git ls-files` prints neither file.

- [ ] **Step 6: Inspect the final change set**

Run: `git status --short && git diff --check e01658e..HEAD`

Expected: only the intentionally ignored local `config.yaml` remains outside commits; diff check reports no whitespace errors.
