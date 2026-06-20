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
	managedEngine := aimanaged.NewEngine(cfg, managedStore, ipfsClient, goldOracle)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mux := http.NewServeMux()
	managedServer.Register(mux)
	httpServer := &http.Server{
		Addr:              cfg.HTTPListen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 3)
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
