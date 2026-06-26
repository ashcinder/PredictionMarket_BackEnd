package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"PredictionMarket/internal/aimanaged"
	"PredictionMarket/internal/apiv1"
	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/config"
	"PredictionMarket/internal/database"
	"PredictionMarket/internal/ipfs"
	"PredictionMarket/internal/oracle"
	"PredictionMarket/internal/sentinel"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

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

	ipfsClient := ipfs.NewClient(cfg.IPFSGateway)
	goldOracle := oracle.NewGoldOracle(oracle.Config{
		GoldAPIURL:     cfg.GoldAPIURL,
		SinaURL:        cfg.SinaURL,
		SinaReferer:    cfg.SinaReferer,
		UserAgent:      cfg.OracleUserAgent,
		RequestTimeout: cfg.OracleRequestTimeout,
	})
	watcher := sentinel.NewWatcher(cfg, chainClient, ipfsClient, goldOracle)
	managedStore, err := aimanaged.NewStore()
	if err != nil {
		slog.Error("init ai-managed store failed", "error", err)
		os.Exit(1)
	}
	managedServer := aimanaged.NewServer(managedStore)
	historyHandler := aimanaged.NewHistoryHandler(repository, cfg.AIHistoryMaxPoints, chainClient)
	managedEngine := aimanaged.NewEngine(cfg, managedStore, ipfsClient, goldOracle, repository, repository)
	sampler := aimanaged.NewMarketHistorySampler(chainClient, repository, cfg.ContractAddress, cfg.SamplerPollInterval, cfg.AIHistoryMaxPoints)

	// v1 API cache layer (DApp reads from MySQL, writes sync to chain→IPFS→DB).
	v1Repo := apiv1.NewMySQLRepository(db)
	v1Server := apiv1.NewServer(
		v1Repo, v1Repo, v1Repo, v1Repo, v1Repo,
		managedStore, chainClient, ipfsClient, cfg.ContractAddress, cfg.AIHistoryMaxPoints,
	)

	// Extend the sampler to also keep the v1 cache tables fresh.
	samplerExt := apiv1.NewSamplerCacheExt(v1Repo, v1Repo, v1Repo, cfg.ContractAddress)
	sampler.SetCacheExt(samplerExt)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mux := http.NewServeMux()
	managedServer.Register(mux)
	historyHandler.Register(mux)
	v1Server.Register(mux)
	httpServer := &http.Server{
		Addr:              cfg.HTTPListen,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 4)
	go func() {
		slog.Info("http api server started", "listen", cfg.HTTPListen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	go func() {
		if err := managedEngine.Run(ctx); err != nil && err != context.Canceled {
			errCh <- err
		}
	}()
	go func() {
		if err := watcher.Run(ctx); err != nil && err != context.Canceled {
			errCh <- err
		}
	}()
	go func() {
		if err := sampler.Run(ctx); err != nil && err != context.Canceled {
			errCh <- err
		}
	}()

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
