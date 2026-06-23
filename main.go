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
	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/config"
	"PredictionMarket/internal/database"
	"PredictionMarket/internal/gameapi"
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	gameRepo := gameapi.NewGameRepository(db)
	gameAPI := gameapi.NewHandler(gameRepo, chainClient, cfg.ContractAddress, historyHandler)

	mux := http.NewServeMux()
	managedServer.Register(mux)
	historyHandler.Register(mux) // /api/gold/market-history (old path, compat)
	gameAPI.Register(mux)         // /api/gold/games, /api/gold/health, etc.
	httpServer := &http.Server{
		Addr:              cfg.HTTPListen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
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
	go func() {
		// Periodic game cache sync: pull all games + reserves from chain
		// into gold_games so the list/detail/positions APIs are fast.
		slog.Info("game cache syncer started", "interval", "30s")
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		// Initial sync.
		syncCtx, syncCancel := context.WithTimeout(ctx, 120*time.Second)
		gameAPI.SyncAllGames(syncCtx)
		syncCancel()
		for {
			select {
			case <-ctx.Done():
				slog.Info("game cache syncer stopped")
				return
			case <-ticker.C:
				syncCtx, syncCancel := context.WithTimeout(ctx, 120*time.Second)
				gameAPI.SyncAllGames(syncCtx)
				syncCancel()
			}
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
