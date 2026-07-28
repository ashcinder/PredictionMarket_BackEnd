package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"PredictionMarket/internal/aicenter"
	"PredictionMarket/internal/aimanaged"
	"PredictionMarket/internal/aioracle"
	"PredictionMarket/internal/apiv1"
	appcache "PredictionMarket/internal/cache"
	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/chainlinkfeed"
	"PredictionMarket/internal/config"
	"PredictionMarket/internal/database"
	"PredictionMarket/internal/ipfs"
	"PredictionMarket/internal/localcontent"
	"PredictionMarket/internal/logging"
	"PredictionMarket/internal/marketdata"
	"PredictionMarket/internal/oracle"
	"PredictionMarket/internal/research"
	"PredictionMarket/internal/sentinel"

	"github.com/ethereum/go-ethereum/common"
)

func main() {
	consoleHandler := logging.NewConciseConsoleHandler(os.Stdout)
	markdownLogs, logErr := logging.NewMarkdownRouter(consoleHandler, "logs")
	if logErr != nil {
		slog.SetDefault(slog.New(consoleHandler))
		slog.Warn("initialize markdown logs failed; continuing with console only", "error", logErr)
	} else {
		slog.SetDefault(slog.New(markdownLogs))
		defer markdownLogs.Close()
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}

	if cfg.UseBrokerChain {
		slog.Info("using BrokerChain API", "url", cfg.BrokerChainURL)
	} else {
		slog.Info("using local RPC", "url", cfg.RPCURL)
	}

	var redisStore appcache.Store
	if cfg.RedisEnabled {
		store, cacheErr := appcache.NewRedisStore(context.Background(), appcache.RedisConfig{
			Address:          cfg.RedisAddress,
			Password:         cfg.RedisPassword,
			DB:               cfg.RedisDB,
			KeyPrefix:        cfg.RedisKeyPrefix,
			OperationTimeout: cfg.RedisOperationTimeout,
		})
		if cacheErr != nil {
			slog.Warn("redis unavailable; continuing without cache",
				"address", cfg.RedisAddress,
				"error", cacheErr,
			)
		} else {
			redisStore = store
			defer redisStore.Close()
			slog.Info("redis cache enabled",
				"address", cfg.RedisAddress,
				"prefix", cfg.RedisKeyPrefix,
				"quote_ttl", cfg.RedisQuoteTTL,
				"public_data_ttl", cfg.RedisPublicDataTTL,
				"research_ttl", cfg.RedisResearchTTL,
			)
		}
	}

	httpListener, err := net.Listen("tcp", cfg.HTTPListen)
	if err != nil {
		slog.Error("http api listen failed",
			"listen", cfg.HTTPListen,
			"error", err,
			"hint", "another backend process may already be using this port",
		)
		os.Exit(1)
	}
	defer httpListener.Close()

	db, err := database.OpenMySQL(context.Background(), database.Config{
		DSN:                   cfg.MySQLDSN,
		MaxOpenConnections:    cfg.MySQLMaxOpenConnections,
		MaxIdleConnections:    cfg.MySQLMaxIdleConnections,
		ConnectionMaxLifetime: cfg.MySQLConnectionMaxLifetime,
	})
	if err != nil {
		slog.Error("init mysql failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	repository := aimanaged.NewMySQLRepository(db)

	chainClient, err := chain.NewClient(cfg.PrivateKey, cfg.ContractAddress, cfg.RPCURL, cfg.BrokerChainURL, cfg.UseBrokerChain)
	if err != nil {
		slog.Error("init chain client failed", "error", err)
		os.Exit(1)
	}
	defer chainClient.Close()

	ipfsClient := ipfs.NewClient(cfg.IPFSGateway, cfg.IPFSFallbackGateways)
	chainlinkClient := chainlinkfeed.NewClient(cfg.ChainlinkRPCURLs, cfg.OracleRequestTimeout)
	chainlinkXAUFeed := common.HexToAddress(cfg.ChainlinkXAUUSDFeed)
	chainlinkBTCFeed := common.HexToAddress(cfg.ChainlinkBTCUSDFeed)
	chainlinkETHFeed := common.HexToAddress(cfg.ChainlinkETHUSDFeed)
	chainlinkSOLFeed := common.HexToAddress(cfg.ChainlinkSOLUSDFeed)
	chainlinkBNBFeed := common.HexToAddress(cfg.ChainlinkBNBUSDFeed)
	chainlinkBenchmarkFeeds := map[string]common.Address{
		"BTC": chainlinkBTCFeed,
		"ETH": chainlinkETHFeed,
		"SOL": chainlinkSOLFeed,
		"BNB": chainlinkBNBFeed,
	}
	chainlinkRoundRepository := marketdata.NewMySQLChainlinkRoundRepository(db)
	chainlinkGoldSource := oracle.NewChainlinkGoldSource(
		chainlinkClient, chainlinkXAUFeed, cfg.ChainlinkMaxStaleness)
	goldOracle := oracle.NewGoldOracleWithPrimary(oracle.Config{
		GoldAPIURL:     cfg.GoldAPIURL,
		SinaURL:        cfg.SinaURL,
		SinaReferer:    cfg.SinaReferer,
		UserAgent:      cfg.OracleUserAgent,
		RequestTimeout: cfg.OracleRequestTimeout,
	}, chainlinkGoldSource)
	aiOracle, err := buildAIOracle(cfg, goldOracle)
	if err != nil {
		slog.Error("init AI oracle failed", "error", err)
		os.Exit(1)
	}
	watcher := sentinel.NewWatcher(cfg, chainClient, ipfsClient, aiOracle)
	aiCenterRepository := aicenter.NewRepository(db)
	watcher.SetSettlementAuditRecorder(aiCenterRepository)
	historicalClient := marketdata.NewGoldAPIClient(
		cfg.HistoricalGoldAPIBaseURL, cfg.HistoricalGoldAPIKey, cfg.OracleRequestTimeout)
	goldSampleRepository := marketdata.NewMySQLGoldSampleRepository(db)
	sampledGoldSource := marketdata.NewSampledGoldSource(
		goldSampleRepository, 2*cfg.OracleSampleInterval)
	if !historicalClient.Available() {
		slog.Warn("aioracle: historical gold evidence is not configured",
			"stage", "startup",
			"required_environment", config.GoldAPIKeyEnvName,
			"local_sample_interval", cfg.OracleSampleInterval,
			"logic_summary", "短周期收盘价、涨跌和黄金跑赢 BTC 可回退到本地连续采样；波动、触价和技术指标仍需要历史 OHLC，证据不足时保持未裁决",
		)
	}
	bitcoinHistoricalClient := marketdata.NewCoinbaseClient(
		cfg.HistoricalBitcoinBaseURL, cfg.OracleRequestTimeout)
	legacyResolver := marketdata.NewStructuredResolverWithSamples(
		historicalClient, bitcoinHistoricalClient, sampledGoldSource)
	chainlinkResolver := marketdata.NewChainlinkResolverWithBenchmarks(
		chainlinkClient, chainlinkRoundRepository, chainlinkXAUFeed, chainlinkBenchmarkFeeds)
	watcher.SetQuantitativeResolver(marketdata.NewVersionedResolver(legacyResolver, chainlinkResolver))
	chainlinkRoundRecorder := marketdata.NewChainlinkRoundRecorder(
		chainlinkClient, chainlinkRoundRepository,
		[]common.Address{
			chainlinkXAUFeed, chainlinkBTCFeed, chainlinkETHFeed,
			chainlinkSOLFeed, chainlinkBNBFeed,
		}, cfg.ChainlinkPollInterval)
	goldSampleRecorder := marketdata.NewGoldSampleRecorder(
		goldSampleRepository, goldOracle, cfg.OracleSampleInterval)
	managedStore, err := aimanaged.NewStoreWithSecret(cfg.PrivateKey)
	if err != nil {
		slog.Error("init ai-managed store failed", "error", err)
		os.Exit(1)
	}
	managedStore.SetPersistence(repository)
	if err := managedStore.RestoreFromRepository(context.Background()); err != nil {
		slog.Error("restore ai-managed entries failed", "error", err)
		os.Exit(1)
	}
	slog.Info("ai-managed entries restored", "count", len(managedStore.Entries()))
	managedServer := aimanaged.NewServer(managedStore)
	historyHandler := aimanaged.NewHistoryHandler(repository, cfg.AIHistoryMaxPoints, chainClient)
	managedEngine := aimanaged.NewEngine(cfg, managedStore, ipfsClient, goldOracle, repository, repository)
	managedEngine.SetStructuredSignalSource(chainlinkResolver)
	sampler := aimanaged.NewMarketHistorySampler(chainClient, repository, cfg.ContractAddress, cfg.SamplerPollInterval, cfg.AIHistoryMaxPoints)

	// v1 API cache layer (DApp reads from MySQL, writes sync to chain→IPFS→DB).
	v1Repo := apiv1.NewMySQLRepository(db, cfg.ContractAddress)
	v1Server := apiv1.NewServer(
		v1Repo, v1Repo, v1Repo, v1Repo, v1Repo,
		managedStore, chainClient, ipfsClient, cfg.ContractAddress, cfg.AIHistoryMaxPoints,
	)
	quoteProvider := oracle.QuoteProvider(goldOracle)
	researchProvider := buildResearchClient(cfg)
	if redisStore != nil {
		quoteProvider = oracle.NewCachedQuoteProvider(goldOracle, redisStore, cfg.RedisQuoteTTL)
		researchProvider = research.NewCachedResearcher(
			researchProvider, redisStore, cfg.RedisResearchTTL,
			cfg.AIBaseURL, cfg.AIModel,
		)
	}
	v1Server.SetQuoteProvider(quoteProvider)
	v1Server.SetResearchProvider(researchProvider)
	v1Server.SetRuntimePolicy(cfg.AutoResolveEnabled)
	aiCenterServer := aicenter.NewServer(aiCenterRepository, redisStore)

	// Extend the sampler to also keep the v1 cache tables fresh.
	samplerExt := apiv1.NewSamplerCacheExt(v1Repo, v1Repo, v1Repo, v1Repo, cfg.ContractAddress, managedStore)
	sampler.SetCacheExt(samplerExt)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mux := http.NewServeMux()
	managedServer.Register(mux)
	historyHandler.Register(mux)
	v1Server.Register(mux)
	aiCenterServer.Register(mux)
	localcontent.NewServer("data/local-ipfs").Register(mux)
	var apiHandler http.Handler = mux
	if redisStore != nil {
		apiHandler = apiv1.NewPublicCacheMiddleware(
			redisStore, cfg.RedisPublicDataTTL,
		).Wrap(apiHandler)
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPListen,
		Handler:           withCORS(apiHandler),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 6)
	go func() {
		slog.Info("http api server started", "listen", cfg.HTTPListen)
		if err := httpServer.Serve(httpListener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	go func() {
		if err := managedEngine.Run(ctx); err != nil && err != context.Canceled {
			errCh <- err
		}
	}()
	if cfg.AutoResolveEnabled {
		go func() {
			if err := watcher.Run(ctx); err != nil && err != context.Canceled {
				errCh <- err
			}
		}()
	} else {
		slog.Warn("automatic market resolution disabled",
			"setting", "sentinel.auto_resolve_enabled",
			"immediate_expiry_demo_creation", true,
			"logic_summary", "到期博弈池保持等待裁决；重新启用该开关并重启后端后，sentinel 将扫描并触发多 AI 开奖",
		)
	}
	go func() {
		if err := goldSampleRecorder.Run(ctx); err != nil && err != context.Canceled {
			errCh <- err
		}
	}()
	go chainlinkRoundRecorder.Run(ctx)
	if cfg.SamplerChainSyncEnabled {
		go func() {
			if err := sampler.Run(ctx); err != nil && err != context.Canceled {
				errCh <- err
			}
		}()
	} else {
		slog.Info("chain-to-database sampler disabled by config", "setting", "sampler.chain_sync_enabled")
	}

	select {
	case <-ctx.Done():
	case err := <-errCh:
		slog.Error("service exited with error", "error", err)
		cancel()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("http api shutdown failed", "error", err)
	}
}

func buildResearchClient(cfg *config.Config) research.Researcher {
	providers := make([]research.Researcher, 0, 1+len(cfg.AIOracleProviders))
	seen := make(map[string]bool)
	add := func(baseURL, apiKey, model string, timeout time.Duration) {
		key := baseURL + "\x00" + model + "\x00" + apiKey
		if seen[key] || baseURL == "" || apiKey == "" || model == "" {
			return
		}
		seen[key] = true
		providers = append(providers, research.NewClient(baseURL, apiKey, model, timeout))
	}
	add(cfg.AIBaseURL, cfg.AIAPIKey, cfg.AIModel, 180*time.Second)
	for _, provider := range cfg.AIOracleProviders {
		// Anthropic uses a different request envelope. The configured DeepSeek,
		// GLM, MiniMax and OpenAI providers are OpenAI-compatible.
		if provider.Provider == "anthropic" {
			continue
		}
		timeout := time.Duration(provider.TimeoutSeconds) * time.Second
		if timeout < 180*time.Second {
			timeout = 180 * time.Second
		}
		add(provider.BaseURL, provider.APIKey, provider.Model, timeout)
	}
	return research.NewFailover(providers...)
}

func buildAIOracle(cfg *config.Config, goldOracle *oracle.GoldOracle) (*aioracle.Oracle, error) {
	providerConfigs := make([]aioracle.ProviderConfig, 0, len(cfg.AIOracleProviders))
	for _, p := range cfg.AIOracleProviders {
		providerConfigs = append(providerConfigs, aioracle.ProviderConfig{
			Name:           p.Name,
			Model:          p.Model,
			APIKey:         p.APIKey,
			BaseURL:        p.BaseURL,
			Provider:       p.Provider,
			Weight:         p.Weight,
			TimeoutSeconds: p.TimeoutSeconds,
		})
	}
	providers := aioracle.NewProviders(providerConfigs)
	if len(providers) == 0 {
		return nil, fmt.Errorf("aioracle.providers must contain at least one usable provider")
	}
	if len(providers) != len(providerConfigs) {
		return nil, fmt.Errorf("initialized %d/%d AI oracle providers", len(providers), len(providerConfigs))
	}

	consensus := aioracle.NewConsensusEngine(aioracle.ConsensusConfig{
		FinalArbiter:      cfg.AIOracleConsensus.FinalArbiter,
		MinConsensusRatio: cfg.AIOracleConsensus.MinConsensusRatio,
		MinConfidence:     cfg.AIOracleConsensus.MinConfidence,
		MinModelsRequired: cfg.AIOracleConsensus.MinModelsRequired,
		TiebreakModel:     cfg.AIOracleConsensus.TiebreakModel,
	}, providers)

	newsCfg := aioracle.NewsConfig{
		NewsAPIKey:            cfg.AIOracleNews.NewsAPIKey,
		NewsAPIURL:            cfg.AIOracleNews.NewsAPIURL,
		RSSFeeds:              cfg.AIOracleNews.RSSFeeds,
		MaxArticles:           cfg.AIOracleNews.MaxArticles,
		LookbackHours:         cfg.AIOracleNews.LookbackHours,
		RequestTimeoutSeconds: cfg.AIOracleNews.RequestTimeoutSeconds,
	}
	evidenceFetchers := []aioracle.NewsFetcher{aioracle.NewGoldEvidenceFetcher(goldOracle)}
	if newsFetcher := aioracle.NewNewsFetcher(newsCfg); newsFetcher != nil {
		evidenceFetchers = append(evidenceFetchers, newsFetcher)
	}

	return aioracle.NewOracleWithOptions(
		aioracle.NewCompositeNewsFetcher(evidenceFetchers...),
		consensus,
		aioracle.OracleOptions{
			PollInterval: time.Duration(cfg.AIOraclePollIntervalSeconds) * time.Second,
			NewsLookback: time.Duration(cfg.AIOracleNews.LookbackHours) * time.Hour,
			MaxArticles:  cfg.AIOracleNews.MaxArticles,
		},
	), nil
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")

		// Log every incoming request so we can see if the frontend is
		// reaching the server, even when no handler matches (404).
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"remote", r.RemoteAddr,
		)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
