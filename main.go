package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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
	goldOracle := oracle.NewGoldOracle()
	watcher := sentinel.NewWatcher(cfg, chainClient, ipfsClient, goldOracle)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := watcher.Run(ctx); err != nil && err != context.Canceled {
		slog.Error("watcher exited with error", "error", err)
		os.Exit(1)
	}
}
