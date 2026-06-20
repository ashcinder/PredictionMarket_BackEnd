package aimanaged

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/config"
	"PredictionMarket/internal/ipfs"
	"PredictionMarket/internal/oracle"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	maxWorkerConcurrency = 8
	tradeCooldown        = time.Hour
)

type Store struct {
	mu      sync.RWMutex
	aead    cipher.AEAD
	entries map[string]*entry
}

type entry struct {
	GameID           int
	UserAddress      string
	ContractAddress  string
	KeyNonce         []byte
	KeyCiphertext    []byte
	EnabledAt        time.Time
	LastTradeAt      time.Time
	LastTradeOption  int
	LastTradeTx      string
	LastError        string
	LastDecisionAt   time.Time
	LastDecisionText string
}

type EntrySnapshot struct {
	GameID          int
	UserAddress     string
	ContractAddress string
	EnabledAt       time.Time
	LastTradeAt     time.Time
	LastTradeOption int
	LastTradeTx     string
	LastError       string
	nonce           []byte
	ciphertext      []byte
}

type SetRequest struct {
	GameID          int    `json:"game_id"`
	UserAddress     string `json:"user_address"`
	Enabled         bool   `json:"enabled"`
	ContractAddress string `json:"contract_address"`
	PrivateKey      string `json:"private_key"`
}

type Server struct {
	store *Store
}

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

type productionManagedChain struct {
	client *chain.Client
}

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

func NewStore() (*Store, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{aead: aead, entries: make(map[string]*entry)}, nil
}

func NewServer(store *Store) *Server {
	return &Server{store: store}
}

func NewEngine(cfg *config.Config, store *Store, ipfsClient *ipfs.Client, goldOracle *oracle.GoldOracle) *Engine {
	return &Engine{
		cfg:   cfg,
		store: store,
		newChain: func(privateKey, contractAddress string) (managedChain, error) {
			client, err := chain.NewClient(privateKey, contractAddress, cfg.RPCURL, cfg.BrokerChainURL, cfg.UseBrokerChain)
			if err != nil {
				return nil, err
			}
			return &productionManagedChain{client: client}, nil
		},
		metadata:  ipfsClient,
		quotes:    goldOracle,
		decisions: NewAIClient(cfg),
	}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/gold/ai-managed", s.handleAIManaged)
}

func (s *Server) handleAIManaged(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodPost:
		s.handleSet(w, r)
	case http.MethodGet:
		s.handleGet(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSet(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil {
		slog.Warn("ai-managed private key received over non-TLS HTTP; use HTTPS in production")
	}

	var req SetRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.GameID <= 0 || !common.IsHexAddress(req.UserAddress) {
		writeJSONError(w, http.StatusBadRequest, "invalid game_id or user_address")
		return
	}

	if req.Enabled {
		if !common.IsHexAddress(req.ContractAddress) {
			writeJSONError(w, http.StatusBadRequest, "invalid contract_address")
			return
		}
		wallet, err := walletAddressFromPrivateKey(req.PrivateKey)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid private_key")
			return
		}
		if !strings.EqualFold(wallet, req.UserAddress) {
			writeJSONError(w, http.StatusBadRequest, "private_key does not match user_address")
			return
		}
		if err := s.store.Enable(req); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		s.store.Disable(req.GameID, req.UserAddress)
	}

	_ = json.NewEncoder(w).Encode(map[string]bool{"enabled": req.Enabled})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	gameID, ok := parsePositiveInt(r.URL.Query().Get("game_id"))
	if !ok || !common.IsHexAddress(r.URL.Query().Get("user_address")) {
		writeJSONError(w, http.StatusBadRequest, "invalid game_id or user_address")
		return
	}
	enabled := s.store.IsEnabled(gameID, r.URL.Query().Get("user_address"))
	_ = json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled})
}

func (s *Store) Enable(req SetRequest) error {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := s.aead.Seal(nil, nonce, []byte(strings.TrimSpace(req.PrivateKey)), nil)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[storeKey(req.GameID, req.UserAddress)] = &entry{
		GameID:          req.GameID,
		UserAddress:     common.HexToAddress(req.UserAddress).Hex(),
		ContractAddress: common.HexToAddress(req.ContractAddress).Hex(),
		KeyNonce:        nonce,
		KeyCiphertext:   ciphertext,
		EnabledAt:       time.Now(),
		LastTradeOption: -1,
	}
	return nil
}

func (s *Store) Disable(gameID int, userAddress string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, storeKey(gameID, userAddress))
}

func (s *Store) IsEnabled(gameID int, userAddress string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.entries[storeKey(gameID, userAddress)]
	return ok
}

func (s *Store) Entries() []EntrySnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EntrySnapshot, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, EntrySnapshot{
			GameID:          e.GameID,
			UserAddress:     e.UserAddress,
			ContractAddress: e.ContractAddress,
			EnabledAt:       e.EnabledAt,
			LastTradeAt:     e.LastTradeAt,
			LastTradeOption: e.LastTradeOption,
			LastTradeTx:     e.LastTradeTx,
			LastError:       e.LastError,
			nonce:           append([]byte(nil), e.KeyNonce...),
			ciphertext:      append([]byte(nil), e.KeyCiphertext...),
		})
	}
	return out
}

func (s *Store) DecryptPrivateKey(snapshot EntrySnapshot) (string, error) {
	plain, err := s.aead.Open(nil, snapshot.nonce, snapshot.ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *Store) CanTrade(gameID int, userAddress string, option int, now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[storeKey(gameID, userAddress)]
	if !ok {
		return false
	}
	return e.LastTradeOption != option || e.LastTradeAt.IsZero() || now.Sub(e.LastTradeAt) >= tradeCooldown
}

func (s *Store) RecordTrade(gameID int, userAddress string, option int, tx string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[storeKey(gameID, userAddress)]; ok {
		e.LastTradeAt = time.Now()
		e.LastTradeOption = option
		e.LastTradeTx = tx
		e.LastError = ""
	}
}

func (s *Store) RecordError(gameID int, userAddress string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[storeKey(gameID, userAddress)]; ok {
		e.LastError = err.Error()
	}
}

func (e *Engine) Run(ctx context.Context) error {
	slog.Info("ai-managed engine started",
		"http_poll_interval", e.cfg.AIPollInterval.String(),
		"buy_amount_bkc", e.cfg.AIBuyAmountBKC,
		"confidence_min", e.cfg.AIConfidenceMin,
		"model", e.cfg.AIModel,
	)

	ticker := time.NewTicker(e.cfg.AIPollInterval)
	defer ticker.Stop()

	e.scanOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			slog.Info("ai-managed engine stopped")
			return ctx.Err()
		case <-ticker.C:
			e.scanOnce(ctx)
		}
	}
}

func (e *Engine) scanOnce(ctx context.Context) {
	entries := e.store.Entries()
	if len(entries) == 0 {
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxWorkerConcurrency)
	for _, snapshot := range entries {
		snapshot := snapshot
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			childCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			if err := e.process(childCtx, snapshot); err != nil {
				e.store.RecordError(snapshot.GameID, snapshot.UserAddress, err)
				slog.Warn("ai-managed task failed", "game_id", snapshot.GameID, "user", snapshot.UserAddress, "error", err)
			}
		}()
	}
	wg.Wait()
}

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

type AIClient struct {
	baseURL    string
	model      string
	apiKey     string
	httpClient *http.Client
}

type Decision struct {
	Action     string  `json:"action"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

func NewAIClient(cfg *config.Config) *AIClient {
	return &AIClient{
		baseURL:    cfg.AIBaseURL,
		model:      cfg.AIModel,
		apiKey:     strings.TrimSpace(cfg.AIAPIKey),
		httpClient: &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *AIClient) Decide(ctx context.Context, info *chain.GameInfo, extra *chain.GameExtraData, meta *ipfs.Metadata, quote *oracle.Quote) (*Decision, error) {
	if c.apiKey == "" {
		return nil, errors.New("ai.api_key is required")
	}

	prompt := fmt.Sprintf(`你是 BrokerFi 黄金博弈自动下单风控代理。只能输出 JSON，不要输出 Markdown。
根据黄金现价和池子参数判断是否值得下单。action 只能是 buy_yes、buy_no、hold。
要求：只有信号明确时才买入；confidence 必须是 0 到 1。

数据：
game_id=%d
title=%s
condition=%s
option_yes=%s
option_no=%s
gold_price_usd=%.4f
gold_change_24h_percent=%.4f
quote_source=%s
deadline_raw=%d
total_pool_wei=%s
virtual_reserve_no_wei=%s
virtual_reserve_yes_wei=%s

返回格式：
{"action":"hold","confidence":0.0,"reason":"简短原因"}`,
		info.ID,
		emptyDefault(meta.Desc, fmt.Sprintf("博弈池 #%d", info.ID)),
		emptyDefault(meta.Condition, "未提供"),
		emptyDefault(meta.OptionYES, "YES"),
		emptyDefault(meta.OptionNO, "NO"),
		quote.PriceUSD,
		quote.Change24h,
		quote.QuoteSource,
		info.DeadlineRaw,
		intString(info.TotalPool),
		intString(extra.VirtualReservesNOYES[0]),
		intString(extra.VirtualReservesNOYES[1]),
	)

	payload := map[string]interface{}{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": "你是谨慎的链上黄金预测市场交易代理，只返回 JSON。"},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.2,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai api HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Choices) == 0 {
		return nil, errors.New("ai api returned no choices")
	}
	return parseDecision(envelope.Choices[0].Message.Content)
}

func (d *Decision) Option() (int, bool) {
	switch strings.ToLower(strings.TrimSpace(d.Action)) {
	case "buy_yes", "yes":
		return 0, true
	case "buy_no", "no":
		return 1, true
	default:
		return 0, false
	}
}

func parseDecision(content string) (*Decision, error) {
	content = strings.TrimSpace(content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
	}
	var decision Decision
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		return nil, fmt.Errorf("decode ai decision: %w", err)
	}
	decision.Action = strings.ToLower(strings.TrimSpace(decision.Action))
	if decision.Action == "" {
		decision.Action = "hold"
	}
	if decision.Confidence < 0 {
		decision.Confidence = 0
	}
	if decision.Confidence > 1 {
		decision.Confidence = 1
	}
	return &decision, nil
}

func parsePositiveInt(raw string) (int, bool) {
	n, ok := new(big.Int).SetString(strings.TrimSpace(raw), 10)
	if !ok || !n.IsInt64() || n.Sign() <= 0 {
		return 0, false
	}
	return int(n.Int64()), true
}

func parseBKCToWei(raw string) (*big.Int, error) {
	rat, ok := new(big.Rat).SetString(strings.TrimSpace(raw))
	if !ok || rat.Sign() <= 0 {
		return nil, fmt.Errorf("invalid decimal amount %q", raw)
	}
	wei := new(big.Rat).Mul(rat, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)))
	out := new(big.Int).Quo(wei.Num(), wei.Denom())
	if out.Sign() <= 0 {
		return nil, fmt.Errorf("amount rounds to zero wei")
	}
	return out, nil
}

func walletAddressFromPrivateKey(privateKeyHex string) (string, error) {
	keyHex := strings.TrimPrefix(strings.TrimSpace(privateKeyHex), "0x")
	key, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		return "", err
	}
	return crypto.PubkeyToAddress(key.PublicKey).Hex(), nil
}

func storeKey(gameID int, userAddress string) string {
	return fmt.Sprintf("%d:%s", gameID, strings.ToLower(common.HexToAddress(userAddress).Hex()))
}

func intString(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}

func emptyDefault(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
