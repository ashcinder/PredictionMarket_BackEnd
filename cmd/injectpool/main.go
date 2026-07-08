package main

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/database"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	mysql "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"
)

const (
	defaultConfigPath         = "config.yaml"
	defaultAmountBKC          = "1"
	defaultTimeout            = 2 * time.Minute
	defaultMySQLMaxOpenConns  = 10
	defaultMySQLMaxIdleConns  = 2
	defaultMySQLConnLifetime  = 5 * time.Minute
	defaultRandomParticipants = 6
	defaultRandomMinBKC       = "0.2"
	defaultRandomMaxBKC       = "2"
	generatedBuyGasLimit      = 8_000_000
	nativeTransferGasLimit    = 21_000

	mysqlDSNEnvName      = "PREDICTIONMARKET_MYSQL_DSN"
	mysqlDatabaseEnvName = "PREDICTIONMARKET_MYSQL_DATABASE"
)

type rawConfig struct {
	Chain struct {
		PrivateKey      string `yaml:"private_key"`
		ContractAddress string `yaml:"contract_address"`
		RPCURL          string `yaml:"rpc_url"`
		BrokerChainURL  string `yaml:"broker_chain_url"`
		UseBrokerChain  bool   `yaml:"use_broker_chain"`
	} `yaml:"chain"`
	MySQL struct {
		DSN                          string `yaml:"dsn"`
		MaxOpenConnections           int    `yaml:"max_open_connections"`
		MaxIdleConnections           int    `yaml:"max_idle_connections"`
		ConnectionMaxLifetimeSeconds int    `yaml:"connection_max_lifetime_seconds"`
	} `yaml:"mysql"`
}

type options struct {
	configPath     string
	gameID         int
	keysRaw        string
	keysFile       string
	amountBKC      string
	randomMinBKC   string
	randomMaxBKC   string
	optionPattern  string
	participants   int
	contractAddr   string
	rpcURL         string
	brokerChainURL string
	mysqlDSN       string
	useBrokerChain bool
	forceBroker    bool
	forceLocalRPC  bool
	dryRun         bool
	pause          time.Duration
	timeout        time.Duration
}

type participant struct {
	privateKey string
	address    string
	optionID   int
	amountWei  *big.Int
	generated  bool
}

type marketState struct {
	info  *chain.GameInfo
	extra *chain.GameExtraData
}

type tradeOutcome struct {
	gameID           int
	contractAddress  string
	userAddress      string
	optionID         int
	amountWei        *big.Int
	shareAmountWei   *big.Int
	txHash           string
	timestampSec     int64
	info             *chain.GameInfo
	extra            *chain.GameExtraData
	yesPrice         float64
	noPrice          float64
	isAIManaged      bool
	priceAtTrade     float64
	mySharesYesAfter string
	mySharesNoAfter  string
}

type dbWriter struct {
	db              *sql.DB
	contractAddress string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "injectpool: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	opts := parseFlags()
	if opts.gameID <= 0 {
		return errors.New("-game-id is required and must be positive")
	}
	if opts.timeout <= 0 {
		opts.timeout = defaultTimeout
	}

	cfg, err := loadRawConfig(opts.configPath)
	if err != nil {
		return err
	}
	if err := applyMySQLEnvOverrides(cfg); err != nil {
		return err
	}
	applyFlagOverrides(cfg, opts)
	if err := validateConfig(cfg); err != nil {
		return err
	}

	amountWei, err := parseBKCToWei(opts.amountBKC)
	if err != nil {
		return fmt.Errorf("invalid -amount-bkc: %w", err)
	}
	randomMinWei, err := parseBKCToWei(opts.randomMinBKC)
	if err != nil {
		return fmt.Errorf("invalid -random-min-bkc: %w", err)
	}
	randomMaxWei, err := parseBKCToWei(opts.randomMaxBKC)
	if err != nil {
		return fmt.Errorf("invalid -random-max-bkc: %w", err)
	}
	if randomMinWei.Cmp(randomMaxWei) > 0 {
		return errors.New("-random-min-bkc must be less than or equal to -random-max-bkc")
	}
	optionPattern, err := parseOptionPattern(opts.optionPattern)
	if err != nil {
		return err
	}
	keys, err := loadParticipantKeys(opts.keysRaw, opts.keysFile)
	if err != nil {
		return err
	}
	if len(keys) > 0 && opts.participants > 0 {
		if opts.participants > len(keys) {
			return fmt.Errorf("-participants=%d but only %d private keys were provided", opts.participants, len(keys))
		}
		keys = keys[:opts.participants]
	}

	var participants []participant
	if len(keys) == 0 {
		count := opts.participants
		if count <= 0 {
			count = defaultRandomParticipants
		}
		participants, err = buildRandomParticipants(count, optionPattern, randomMinWei, randomMaxWei)
		if err != nil {
			return err
		}
	} else {
		participants, err = buildParticipants(keys, optionPattern, amountWei)
		if err != nil {
			return err
		}
	}

	fmt.Printf("injectpool: game_id=%d participants=%d contract=%s\n",
		opts.gameID, len(participants), normalizeAddress(cfg.Chain.ContractAddress))
	for i, p := range participants {
		source := "provided"
		if p.generated {
			source = "generated"
		}
		fmt.Printf("  #%d %s option=%s amount_wei=%s source=%s\n",
			i+1, p.address, optionName(p.optionID), bigIntString(p.amountWei), source)
	}
	if opts.dryRun {
		fmt.Println("injectpool: dry-run enabled; no chain transaction or database write was executed")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	if hasGeneratedParticipants(participants) {
		if cfg.Chain.UseBrokerChain {
			return errors.New("automatic random participants require local RPC funding; use -local-rpc or provide funded keys for BrokerChain mode")
		}
		if err := fundGeneratedParticipants(ctx, cfg, participants); err != nil {
			return err
		}
	}

	db, err := database.OpenMySQL(ctx, database.Config{
		DSN:                   cfg.MySQL.DSN,
		MaxOpenConnections:    cfg.MySQL.MaxOpenConnections,
		MaxIdleConnections:    cfg.MySQL.MaxIdleConnections,
		ConnectionMaxLifetime: time.Duration(cfg.MySQL.ConnectionMaxLifetimeSeconds) * time.Second,
	})
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer db.Close()
	writer := &dbWriter{db: db, contractAddress: normalizeAddress(cfg.Chain.ContractAddress)}

	for i, p := range participants {
		if i > 0 && opts.pause > 0 {
			if err := sleepWithContext(ctx, opts.pause); err != nil {
				return err
			}
		}
		if err := injectParticipant(ctx, cfg, writer, opts.gameID, p, i+1, len(participants)); err != nil {
			return err
		}
	}
	fmt.Printf("injectpool: completed %d participant injections for game_id=%d\n", len(participants), opts.gameID)
	return nil
}

func parseFlags() *options {
	opts := &options{}
	flag.StringVar(&opts.configPath, "config", defaultConfigPath, "path to backend config.yaml")
	flag.IntVar(&opts.gameID, "game-id", 0, "existing on-chain game id to inject participation into")
	flag.StringVar(&opts.keysRaw, "keys", "", "comma-separated participant private keys")
	flag.StringVar(&opts.keysFile, "keys-file", "", "file with one participant private key per line")
	flag.StringVar(&opts.amountBKC, "amount-bkc", defaultAmountBKC, "BKC amount each participant spends")
	flag.StringVar(&opts.randomMinBKC, "random-min-bkc", defaultRandomMinBKC, "minimum random BKC amount when keys are omitted")
	flag.StringVar(&opts.randomMaxBKC, "random-max-bkc", defaultRandomMaxBKC, "maximum random BKC amount when keys are omitted")
	flag.StringVar(&opts.optionPattern, "options", "", "comma-separated option pattern: yes/no or 0/1; default alternates YES,NO")
	flag.IntVar(&opts.participants, "participants", 0, "number of random participants when keys are omitted; otherwise limit provided keys")
	flag.StringVar(&opts.contractAddr, "contract", "", "override chain.contract_address")
	flag.StringVar(&opts.rpcURL, "rpc", "", "override chain.rpc_url")
	flag.StringVar(&opts.brokerChainURL, "broker-url", "", "override chain.broker_chain_url")
	flag.StringVar(&opts.mysqlDSN, "mysql-dsn", "", "override mysql.dsn")
	flag.BoolVar(&opts.forceBroker, "use-broker-chain", false, "force BrokerChain API mode")
	flag.BoolVar(&opts.forceLocalRPC, "local-rpc", false, "force local Ethereum JSON-RPC mode")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "print the injection plan without sending transactions or writing DB")
	flag.DurationVar(&opts.pause, "pause", 0, "pause between participant transactions, e.g. 500ms or 2s")
	flag.DurationVar(&opts.timeout, "timeout", defaultTimeout, "overall timeout")
	flag.Parse()
	return opts
}

func loadRawConfig(path string) (*rawConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg rawConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.MySQL.MaxOpenConnections <= 0 {
		cfg.MySQL.MaxOpenConnections = defaultMySQLMaxOpenConns
	}
	if cfg.MySQL.MaxIdleConnections <= 0 {
		cfg.MySQL.MaxIdleConnections = defaultMySQLMaxIdleConns
	}
	if cfg.MySQL.MaxIdleConnections > cfg.MySQL.MaxOpenConnections {
		cfg.MySQL.MaxIdleConnections = cfg.MySQL.MaxOpenConnections
	}
	if cfg.MySQL.ConnectionMaxLifetimeSeconds <= 0 {
		cfg.MySQL.ConnectionMaxLifetimeSeconds = int(defaultMySQLConnLifetime / time.Second)
	}
	return &cfg, nil
}

func applyMySQLEnvOverrides(cfg *rawConfig) error {
	if dsn := strings.TrimSpace(os.Getenv(mysqlDSNEnvName)); dsn != "" {
		parsed, err := mysql.ParseDSN(dsn)
		if err != nil || strings.TrimSpace(parsed.DBName) == "" {
			return fmt.Errorf("%s is invalid", mysqlDSNEnvName)
		}
		cfg.MySQL.DSN = dsn
	}
	if databaseName := strings.TrimSpace(os.Getenv(mysqlDatabaseEnvName)); databaseName != "" {
		parsed, err := mysql.ParseDSN(cfg.MySQL.DSN)
		if err != nil || strings.ContainsAny(databaseName, "/?@") {
			return fmt.Errorf("%s is invalid", mysqlDatabaseEnvName)
		}
		parsed.DBName = databaseName
		cfg.MySQL.DSN = parsed.FormatDSN()
	}
	return nil
}

func applyFlagOverrides(cfg *rawConfig, opts *options) {
	if strings.TrimSpace(opts.contractAddr) != "" {
		cfg.Chain.ContractAddress = opts.contractAddr
	}
	if strings.TrimSpace(opts.rpcURL) != "" {
		cfg.Chain.RPCURL = opts.rpcURL
	}
	if strings.TrimSpace(opts.brokerChainURL) != "" {
		cfg.Chain.BrokerChainURL = opts.brokerChainURL
	}
	if strings.TrimSpace(opts.mysqlDSN) != "" {
		cfg.MySQL.DSN = opts.mysqlDSN
	}
	if opts.forceBroker {
		cfg.Chain.UseBrokerChain = true
	}
	if opts.forceLocalRPC {
		cfg.Chain.UseBrokerChain = false
	}
}

func validateConfig(cfg *rawConfig) error {
	if !common.IsHexAddress(cfg.Chain.ContractAddress) {
		return errors.New("chain.contract_address is invalid")
	}
	if strings.TrimSpace(cfg.Chain.RPCURL) == "" && !cfg.Chain.UseBrokerChain {
		return errors.New("chain.rpc_url is required in local RPC mode")
	}
	if strings.TrimSpace(cfg.Chain.BrokerChainURL) == "" && cfg.Chain.UseBrokerChain {
		return errors.New("chain.broker_chain_url is required in BrokerChain mode")
	}
	parsedDSN, err := mysql.ParseDSN(strings.TrimSpace(cfg.MySQL.DSN))
	if err != nil || strings.TrimSpace(parsedDSN.DBName) == "" {
		return errors.New("mysql.dsn is invalid")
	}
	return nil
}

func loadParticipantKeys(raw string, file string) ([]string, error) {
	var keys []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			keys = append(keys, item)
		}
	}
	if strings.TrimSpace(file) != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read keys file: %w", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if cut, _, ok := strings.Cut(line, "#"); ok {
				line = cut
			}
			line = strings.TrimSpace(line)
			if line != "" {
				keys = append(keys, line)
			}
		}
	}
	return keys, nil
}

func buildParticipants(keys []string, pattern []int, amountWei *big.Int) ([]participant, error) {
	out := make([]participant, 0, len(keys))
	seen := map[string]struct{}{}
	for i, key := range keys {
		canonicalKey := strings.TrimPrefix(strings.TrimSpace(key), "0x")
		ecdsaKey, err := crypto.HexToECDSA(canonicalKey)
		if err != nil {
			return nil, fmt.Errorf("private key #%d is invalid: %w", i+1, err)
		}
		addr := normalizeAddress(crypto.PubkeyToAddress(ecdsaKey.PublicKey).Hex())
		if _, ok := seen[addr]; ok {
			return nil, fmt.Errorf("private key #%d duplicates participant address %s", i+1, addr)
		}
		seen[addr] = struct{}{}
		out = append(out, participant{
			privateKey: canonicalKey,
			address:    addr,
			optionID:   optionForIndex(i, pattern),
			amountWei:  new(big.Int).Set(amountWei),
		})
	}
	return out, nil
}

func buildRandomParticipants(count int, pattern []int, minAmountWei *big.Int, maxAmountWei *big.Int) ([]participant, error) {
	if count <= 0 {
		return nil, errors.New("random participant count must be positive")
	}
	if minAmountWei == nil || maxAmountWei == nil || minAmountWei.Sign() <= 0 || maxAmountWei.Sign() <= 0 {
		return nil, errors.New("random amount range must be positive")
	}
	if minAmountWei.Cmp(maxAmountWei) > 0 {
		return nil, errors.New("random amount minimum exceeds maximum")
	}

	options, err := randomOptionIDs(count, pattern)
	if err != nil {
		return nil, err
	}
	out := make([]participant, 0, count)
	seen := map[string]struct{}{}
	for i := 0; i < count; i++ {
		key, err := crypto.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("generate participant key #%d: %w", i+1, err)
		}
		privateKeyHex := hex.EncodeToString(crypto.FromECDSA(key))
		address := normalizeAddress(crypto.PubkeyToAddress(key.PublicKey).Hex())
		if _, ok := seen[address]; ok {
			i--
			continue
		}
		seen[address] = struct{}{}

		amountWei, err := randomAmountWeiInRange(minAmountWei, maxAmountWei)
		if err != nil {
			return nil, fmt.Errorf("generate participant amount #%d: %w", i+1, err)
		}
		out = append(out, participant{
			privateKey: privateKeyHex,
			address:    address,
			optionID:   options[i],
			amountWei:  amountWei,
			generated:  true,
		})
	}
	return out, nil
}

func randomOptionIDs(count int, pattern []int) ([]int, error) {
	out := make([]int, count)
	if len(pattern) > 0 {
		for i := range out {
			out[i] = optionForIndex(i, pattern)
		}
		return out, nil
	}
	for i := range out {
		out[i] = i % 2
	}
	for i := len(out) - 1; i > 0; i-- {
		jBig, err := crand.Int(crand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return nil, err
		}
		j := int(jBig.Int64())
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func randomBigIntInRange(min *big.Int, max *big.Int) (*big.Int, error) {
	width := new(big.Int).Sub(max, min)
	width.Add(width, big.NewInt(1))
	offset, err := crand.Int(crand.Reader, width)
	if err != nil {
		return nil, err
	}
	return offset.Add(offset, min), nil
}

func randomAmountWeiInRange(min *big.Int, max *big.Int) (*big.Int, error) {
	step := randomAmountStepWei()
	minUnits := ceilDiv(min, step)
	maxUnits := new(big.Int).Div(max, step)
	if minUnits.Cmp(maxUnits) > 0 {
		return randomBigIntInRange(min, max)
	}
	unit, err := randomBigIntInRange(minUnits, maxUnits)
	if err != nil {
		return nil, err
	}
	return unit.Mul(unit, step), nil
}

func randomAmountStepWei() *big.Int {
	value, _ := parseBKCToWei("0.01")
	return value
}

func ceilDiv(value *big.Int, divisor *big.Int) *big.Int {
	result := new(big.Int).Div(value, divisor)
	remainder := new(big.Int).Mod(value, divisor)
	if remainder.Sign() > 0 {
		result.Add(result, big.NewInt(1))
	}
	return result
}

func optionForIndex(index int, pattern []int) int {
	if len(pattern) == 0 {
		return index % 2
	}
	return pattern[index%len(pattern)]
}

func parseOptionPattern(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "0", "yes", "y":
			out = append(out, 0)
		case "1", "no", "n":
			out = append(out, 1)
		default:
			return nil, fmt.Errorf("invalid -options item %q; use yes/no or 0/1", part)
		}
	}
	return out, nil
}

func parseBKCToWei(raw string) (*big.Int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("amount is empty")
	}
	if strings.HasPrefix(raw, "-") || strings.HasPrefix(raw, "+") {
		return nil, errors.New("amount must be a positive decimal without sign")
	}
	whole, frac, hasFrac := strings.Cut(raw, ".")
	if whole == "" {
		whole = "0"
	}
	if whole == "" || !isDecimalDigits(whole) {
		return nil, errors.New("amount whole part must be decimal digits")
	}
	if hasFrac {
		if frac == "" || !isDecimalDigits(frac) {
			return nil, errors.New("amount fractional part must be decimal digits")
		}
		if len(frac) > 18 {
			return nil, errors.New("amount supports at most 18 decimal places")
		}
	} else {
		frac = ""
	}
	frac = frac + strings.Repeat("0", 18-len(frac))
	combined := strings.TrimLeft(whole+frac, "0")
	if combined == "" {
		return nil, errors.New("amount must be greater than zero")
	}
	value, ok := new(big.Int).SetString(combined, 10)
	if !ok || value.Sign() <= 0 {
		return nil, errors.New("amount must be a positive decimal")
	}
	return value, nil
}

func isDecimalDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func hasGeneratedParticipants(participants []participant) bool {
	for _, p := range participants {
		if p.generated {
			return true
		}
	}
	return false
}

func fundGeneratedParticipants(ctx context.Context, cfg *rawConfig, participants []participant) error {
	funderKey := strings.TrimPrefix(strings.TrimSpace(cfg.Chain.PrivateKey), "0x")
	funder, err := crypto.HexToECDSA(funderKey)
	if err != nil {
		return fmt.Errorf("chain.private_key is required and must be valid to fund generated random participants: %w", err)
	}
	funderAddress := normalizeAddress(crypto.PubkeyToAddress(funder.PublicKey).Hex())
	rpc := &localRPCClient{
		url:    cfg.Chain.RPCURL,
		client: &http.Client{Timeout: 30 * time.Second},
	}
	gasPrice, err := rpc.quantity(ctx, "eth_gasPrice", nil)
	if err != nil {
		return fmt.Errorf("read local gas price for generated participants: %w", err)
	}
	for i, p := range participants {
		if !p.generated {
			continue
		}
		amount := generatedParticipantFunding(p.amountWei, gasPrice)
		fmt.Printf("injectpool: funding generated participant #%d %s with %s wei from %s\n",
			i+1, p.address, amount.String(), funderAddress)
		txHash, err := rpc.sendNativeTransfer(ctx, funderKey, p.address, amount)
		if err != nil {
			return fmt.Errorf("fund generated participant #%d: %w", i+1, err)
		}
		fmt.Printf("injectpool: funded generated participant #%d tx=%s\n", i+1, txHash)
	}
	return nil
}

func generatedParticipantFunding(stakeWei *big.Int, gasPrice *big.Int) *big.Int {
	amount := new(big.Int)
	if stakeWei != nil {
		amount.Set(stakeWei)
	}
	if gasPrice != nil && gasPrice.Sign() > 0 {
		amount.Add(amount, new(big.Int).Mul(gasPrice, big.NewInt(generatedBuyGasLimit)))
	}
	amount.Add(amount, generatedFundingBufferWei())
	return amount
}

func generatedFundingBufferWei() *big.Int {
	value, _ := parseBKCToWei("0.02")
	return value
}

type localRPCClient struct {
	url    string
	client interface {
		Do(*http.Request) (*http.Response, error)
	}
}

type localRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *localRPCClient) call(ctx context.Context, method string, params []interface{}, out interface{}) error {
	payload, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *localRPCError  `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("%s RPC error %d: %s", method, envelope.Error.Code, envelope.Error.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

func (c *localRPCClient) quantity(ctx context.Context, method string, params []interface{}) (*big.Int, error) {
	var encoded string
	if err := c.call(ctx, method, params, &encoded); err != nil {
		return nil, err
	}
	value := new(big.Int)
	if _, ok := value.SetString(strings.TrimPrefix(encoded, "0x"), 16); !ok {
		return nil, fmt.Errorf("%s returned invalid quantity %q", method, encoded)
	}
	return value, nil
}

func (c *localRPCClient) sendNativeTransfer(ctx context.Context, privateKeyHex string, toAddress string, value *big.Int) (string, error) {
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(strings.TrimSpace(privateKeyHex), "0x"))
	if err != nil {
		return "", err
	}
	from := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	chainID, err := c.quantity(ctx, "eth_chainId", nil)
	if err != nil {
		return "", err
	}
	nonce, err := c.quantity(ctx, "eth_getTransactionCount", []interface{}{from, "pending"})
	if err != nil {
		return "", err
	}
	gasPrice, err := c.quantity(ctx, "eth_gasPrice", nil)
	if err != nil {
		return "", err
	}
	tx := types.NewTransaction(
		nonce.Uint64(),
		common.HexToAddress(toAddress),
		value,
		nativeTransferGasLimit,
		gasPrice,
		nil,
	)
	signed, err := types.SignTx(tx, types.NewLondonSigner(chainID), privateKey)
	if err != nil {
		return "", err
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return "", err
	}
	var txHash string
	if err := c.call(ctx, "eth_sendRawTransaction", []interface{}{"0x" + hex.EncodeToString(raw)}, &txHash); err != nil {
		return "", err
	}
	if txHash == "" {
		return "", errors.New("local rpc returned empty transfer tx hash")
	}
	if err := c.waitForReceipt(ctx, txHash); err != nil {
		return txHash, err
	}
	return txHash, nil
}

func (c *localRPCClient) waitForReceipt(ctx context.Context, txHash string) error {
	for i := 0; i < 12; i++ {
		var receipt struct {
			Status string `json:"status"`
		}
		err := c.call(ctx, "eth_getTransactionReceipt", []interface{}{txHash}, &receipt)
		if err == nil && receipt.Status != "" {
			if receipt.Status == "0x0" {
				return errors.New("native transfer reverted on chain")
			}
			return nil
		}
		if err := sleepWithContext(ctx, 2500*time.Millisecond); err != nil {
			return err
		}
	}
	return errors.New("native transfer not confirmed within timeout")
}

func injectParticipant(ctx context.Context, cfg *rawConfig, writer *dbWriter, gameID int, p participant, index int, total int) error {
	client, err := chain.NewClient(
		p.privateKey,
		cfg.Chain.ContractAddress,
		cfg.Chain.RPCURL,
		cfg.Chain.BrokerChainURL,
		cfg.Chain.UseBrokerChain,
	)
	if err != nil {
		return fmt.Errorf("participant #%d: init chain client: %w", index, err)
	}
	defer client.Close()

	fmt.Printf("injectpool: [%d/%d] %s buying %s with %s wei\n",
		index, total, p.address, optionName(p.optionID), p.amountWei.String())

	preState, err := queryMarketState(ctx, client, gameID, p.address)
	if err != nil {
		return fmt.Errorf("participant #%d pre-query failed: %w", index, err)
	}

	data, err := chain.EncodeBuyShares(gameID, p.optionID)
	if err != nil {
		return fmt.Errorf("participant #%d encode buyShares: %w", index, err)
	}
	txHash, err := client.SendTransaction(ctx, data, p.amountWei)
	if err != nil {
		return fmt.Errorf("participant #%d send transaction: %w", index, err)
	}
	fmt.Printf("injectpool: [%d/%d] tx confirmed %s\n", index, total, txHash)

	postState, err := queryMarketState(ctx, client, gameID, p.address)
	if err != nil {
		return fmt.Errorf("participant #%d post-query failed after tx %s: %w", index, txHash, err)
	}
	shareDelta := shareDeltaForOption(preState.extra, postState.extra, p.optionID)
	yesPrice, noPrice, err := pricesFromExtra(postState.extra)
	if err != nil {
		return fmt.Errorf("participant #%d calculate prices: %w", index, err)
	}

	outcome := &tradeOutcome{
		gameID:           gameID,
		contractAddress:  normalizeAddress(cfg.Chain.ContractAddress),
		userAddress:      p.address,
		optionID:         p.optionID,
		amountWei:        new(big.Int).Set(p.amountWei),
		shareAmountWei:   shareDelta,
		txHash:           txHash,
		timestampSec:     time.Now().Unix(),
		info:             postState.info,
		extra:            postState.extra,
		yesPrice:         yesPrice,
		noPrice:          noPrice,
		priceAtTrade:     priceForOption(p.optionID, yesPrice, noPrice),
		mySharesYesAfter: bigIntString(shareAt(postState.extra.MySharesYESNO, 0)),
		mySharesNoAfter:  bigIntString(shareAt(postState.extra.MySharesYESNO, 1)),
	}
	if err := writer.Sync(ctx, outcome); err != nil {
		return fmt.Errorf("participant #%d db sync failed: %w", index, err)
	}
	fmt.Printf("injectpool: [%d/%d] db synced shares=%s yes=%.2f no=%.2f\n",
		index, total, shareDelta.String(), yesPrice, noPrice)
	return nil
}

func queryMarketState(ctx context.Context, client *chain.Client, gameID int, userAddress string) (*marketState, error) {
	infoCall, err := chain.EncodeGetGameInfo(gameID)
	if err != nil {
		return nil, err
	}
	infoHex, err := client.EthCall(ctx, infoCall)
	if err != nil {
		return nil, fmt.Errorf("getGameInfo: %w", err)
	}
	info, err := chain.DecodeGetGameInfo(gameID, infoHex)
	if err != nil {
		return nil, err
	}

	extraCall, err := chain.EncodeGetGameExtraData(gameID, userAddress)
	if err != nil {
		return nil, err
	}
	extraHex, err := client.EthCall(ctx, extraCall)
	if err != nil {
		return nil, fmt.Errorf("getGameExtraData: %w", err)
	}
	extra, err := chain.DecodeGetGameExtraData(extraHex)
	if err != nil {
		return nil, err
	}
	return &marketState{info: info, extra: extra}, nil
}

func (w *dbWriter) Sync(ctx context.Context, outcome *tradeOutcome) error {
	if err := w.ensureGame(ctx, outcome); err != nil {
		return err
	}
	if err := w.upsertChainState(ctx, outcome); err != nil {
		return err
	}
	if err := w.upsertUserPosition(ctx, outcome); err != nil {
		return err
	}
	if err := w.insertTrade(ctx, outcome); err != nil {
		return err
	}
	if err := w.appendGoldPriceHistory(ctx, outcome); err != nil {
		return err
	}
	if err := w.appendMarketHistory(ctx, outcome); err != nil {
		return err
	}
	return nil
}

func (w *dbWriter) ensureGame(ctx context.Context, outcome *tradeOutcome) error {
	ipfsCID := strings.TrimSpace(outcome.info.IPFSCID)
	if ipfsCID == "" {
		ipfsCID = fmt.Sprintf("test-game-%d", outcome.gameID)
	}
	_, err := w.db.ExecContext(ctx, `INSERT IGNORE INTO gold_games
		(game_id, contract_address, ipfs_cid, `+"`desc`"+`, `+"`condition`"+`,
		avatar_url, detailed_info, option_yes, option_no, creator_address, deadline_sec)
		VALUES (?, ?, ?, '', '', '', '', 'YES', 'NO', '', ?)`,
		outcome.gameID,
		w.contractAddress,
		ipfsCID,
		normalizeDeadlineSec(outcome.info.DeadlineRaw),
	)
	if err != nil {
		return fmt.Errorf("ensure gold_games row: %w", err)
	}
	return nil
}

func (w *dbWriter) upsertChainState(ctx context.Context, outcome *tradeOutcome) error {
	_, err := w.db.ExecContext(ctx, `INSERT INTO gold_chain_states
		(contract_address, game_id, total_pool, is_resolved, is_refunded, winning_option,
		deadline_sec, reserve_yes, reserve_no)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		total_pool=VALUES(total_pool), is_resolved=VALUES(is_resolved),
		is_refunded=VALUES(is_refunded), winning_option=VALUES(winning_option),
		deadline_sec=VALUES(deadline_sec), reserve_yes=VALUES(reserve_yes),
		reserve_no=VALUES(reserve_no)`,
		w.contractAddress,
		outcome.gameID,
		bigIntToDB(outcome.info.TotalPool),
		boolToInt(outcome.info.IsResolved),
		boolToInt(outcome.info.IsRefunded),
		outcome.info.WinningOption,
		normalizeDeadlineSec(outcome.info.DeadlineRaw),
		bigIntToDB(shareAt(outcome.extra.VirtualReservesNOYES, 1)),
		bigIntToDB(shareAt(outcome.extra.VirtualReservesNOYES, 0)),
	)
	if err != nil {
		return fmt.Errorf("upsert gold_chain_states: %w", err)
	}
	return nil
}

func (w *dbWriter) upsertUserPosition(ctx context.Context, outcome *tradeOutcome) error {
	_, err := w.db.ExecContext(ctx, `INSERT INTO gold_user_positions
		(user_address, game_id, my_shares_yes, my_shares_no)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		my_shares_yes=VALUES(my_shares_yes), my_shares_no=VALUES(my_shares_no)`,
		normalizeAddress(outcome.userAddress),
		outcome.gameID,
		bigIntToDB(shareAt(outcome.extra.MySharesYESNO, 0)),
		bigIntToDB(shareAt(outcome.extra.MySharesYESNO, 1)),
	)
	if err != nil {
		return fmt.Errorf("upsert gold_user_positions: %w", err)
	}
	return nil
}

func (w *dbWriter) insertTrade(ctx context.Context, outcome *tradeOutcome) error {
	_, err := w.db.ExecContext(ctx, `INSERT INTO gold_trades
		(game_id, contract_address, user_address, trade_type, option_id, amount_wei,
		share_amount_wei, shares_wei, price_at_trade, timestamp_sec, tx_hash, is_success,
		is_ai_managed, my_shares_yes_after, my_shares_no_after)
		VALUES (?, ?, ?, 'BUY', ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		outcome.gameID,
		w.contractAddress,
		normalizeAddress(outcome.userAddress),
		outcome.optionID,
		bigIntToDB(outcome.amountWei),
		bigIntString(outcome.shareAmountWei),
		bigIntToDB(outcome.shareAmountWei),
		outcome.priceAtTrade,
		outcome.timestampSec,
		outcome.txHash,
		boolToInt(outcome.isAIManaged),
		outcome.mySharesYesAfter,
		outcome.mySharesNoAfter,
	)
	if err != nil {
		return fmt.Errorf("insert gold_trades: %w", err)
	}
	return nil
}

func (w *dbWriter) appendGoldPriceHistory(ctx context.Context, outcome *tradeOutcome) error {
	_, err := w.db.ExecContext(ctx, `INSERT INTO gold_price_history
		(game_id, timestamp_sec, yes_price, no_price, total_pool)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		yes_price=VALUES(yes_price), no_price=VALUES(no_price), total_pool=VALUES(total_pool)`,
		outcome.gameID,
		outcome.timestampSec,
		outcome.yesPrice,
		outcome.noPrice,
		bigIntToDB(outcome.info.TotalPool),
	)
	if err != nil {
		return fmt.Errorf("append gold_price_history: %w", err)
	}
	return nil
}

func (w *dbWriter) appendMarketHistory(ctx context.Context, outcome *tradeOutcome) error {
	_, err := w.db.ExecContext(ctx, `INSERT INTO market_history
		(contract_address, game_id, observed_at, yes_percent, no_percent, reserve_no, reserve_yes, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'chain')
		ON DUPLICATE KEY UPDATE
		yes_percent=VALUES(yes_percent), no_percent=VALUES(no_percent),
		reserve_no=VALUES(reserve_no), reserve_yes=VALUES(reserve_yes), source=VALUES(source)`,
		w.contractAddress,
		outcome.gameID,
		outcome.timestampSec,
		outcome.yesPrice,
		outcome.noPrice,
		bigIntToDB(shareAt(outcome.extra.VirtualReservesNOYES, 0)),
		bigIntToDB(shareAt(outcome.extra.VirtualReservesNOYES, 1)),
	)
	if err != nil {
		return fmt.Errorf("append market_history: %w", err)
	}
	return nil
}

func shareDeltaForOption(before *chain.GameExtraData, after *chain.GameExtraData, optionID int) *big.Int {
	beforeShares := shareAt(nil, 0)
	afterShares := shareAt(nil, 0)
	if before != nil {
		beforeShares = shareAt(before.MySharesYESNO, optionID)
	}
	if after != nil {
		afterShares = shareAt(after.MySharesYESNO, optionID)
	}
	delta := new(big.Int).Sub(afterShares, beforeShares)
	if delta.Sign() < 0 {
		return big.NewInt(0)
	}
	return delta
}

func shareAt(values []*big.Int, index int) *big.Int {
	if index < 0 || index >= len(values) || values[index] == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(values[index])
}

func pricesFromExtra(extra *chain.GameExtraData) (float64, float64, error) {
	if extra == nil || len(extra.VirtualReservesNOYES) < 2 {
		return 0, 0, errors.New("missing virtual reserves")
	}
	reserveNO := shareAt(extra.VirtualReservesNOYES, 0)
	reserveYES := shareAt(extra.VirtualReservesNOYES, 1)
	if reserveNO.Sign() < 0 || reserveYES.Sign() < 0 {
		return 0, 0, errors.New("reserves must not be negative")
	}
	total := new(big.Int).Add(reserveNO, reserveYES)
	if total.Sign() <= 0 {
		return 0, 0, errors.New("reserve total must be positive")
	}
	yesRatio := new(big.Rat).SetFrac(reserveNO, total)
	yesPrice, _ := yesRatio.Float64()
	yesPrice *= 100
	noPrice := 100 - yesPrice
	if math.IsNaN(yesPrice) || math.IsInf(yesPrice, 0) ||
		math.IsNaN(noPrice) || math.IsInf(noPrice, 0) {
		return 0, 0, errors.New("calculated prices are not finite")
	}
	return yesPrice, noPrice, nil
}

func priceForOption(optionID int, yesPrice float64, noPrice float64) float64 {
	if optionID == 0 {
		return yesPrice
	}
	return noPrice
}

func optionName(optionID int) string {
	if optionID == 0 {
		return "YES"
	}
	return "NO"
}

func normalizeAddress(value string) string {
	return strings.ToLower(common.HexToAddress(value).Hex())
}

func normalizeDeadlineSec(value int64) int64 {
	if value > 10_000_000_000 {
		return value / 1000
	}
	return value
}

func bigIntToDB(value *big.Int) []byte {
	if value == nil {
		return nil
	}
	return []byte(value.String())
}

func bigIntString(value *big.Int) string {
	if value == nil {
		return "0"
	}
	return value.String()
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
