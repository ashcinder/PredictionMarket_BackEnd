package sentinel

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/config"
	"PredictionMarket/internal/ipfs"
	"PredictionMarket/internal/judge"
	"PredictionMarket/internal/oracle"
)

type Watcher struct {
	cfg      *config.Config
	chain    *chain.Client
	ipfs     *ipfs.Client
	oracle   *oracle.GoldOracle
	resolving sync.Map
}

func NewWatcher(cfg *config.Config, chainClient *chain.Client, ipfsClient *ipfs.Client, goldOracle *oracle.GoldOracle) *Watcher {
	return &Watcher{
		cfg:    cfg,
		chain:  chainClient,
		ipfs:   ipfsClient,
		oracle: goldOracle,
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	slog.Info("prediction market sentinel started",
		"contract", w.cfg.ContractAddress,
		"wallet", w.chain.WalletAddress(),
		"poll_interval", w.cfg.PollInterval.String(),
		"use_broker_chain", w.cfg.UseBrokerChain,
	)

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	initCtx, initCancel := context.WithTimeout(ctx, 120*time.Second)
	if err := w.scanOnce(initCtx); err != nil {
		slog.Warn("initial scan failed", "error", err)
	}
	initCancel()

	for {
		select {
		case <-ctx.Done():
			slog.Info("sentinel stopped")
			return ctx.Err()
		case <-ticker.C:
			cycleCtx, cycleCancel := context.WithTimeout(ctx, 120*time.Second)
			if err := w.scanOnce(cycleCtx); err != nil {
				slog.Warn("scan failed", "error", err)
			}
			cycleCancel()
		}
	}
}

func (w *Watcher) scanOnce(ctx context.Context) error {
	data := chain.EncodeGetAllGames()
	hexResult, err := w.chain.EthCall(ctx, data)
	if err != nil {
		return fmt.Errorf("eth_call getAllGames: %w", err)
	}
	games, err := chain.DecodeGetAllGames(hexResult)
	if err != nil {
		return fmt.Errorf("decode getAllGames: %w", err)
	}

	now := time.Now().UnixMilli()
	var pending int
	for _, game := range games {
		if game.IsResolved || game.IsRefunded {
			continue
		}
		if !chain.IsDeadlinePassed(game.DeadlineRaw, now) {
			continue
		}
		pending++
		if err := w.resolveGame(ctx, game); err != nil {
			slog.Error("resolve game failed", "game_id", game.ID, "error", err)
		}
	}
	slog.Info("scan complete", "total_games", len(games), "pending_resolve", pending)
	return nil
}

func (w *Watcher) resolveGame(ctx context.Context, game chain.GameOnChain) error {
	key := fmt.Sprintf("%d", game.ID)
	if _, loaded := w.resolving.LoadOrStore(key, true); loaded {
		return nil
	}
	defer w.resolving.Delete(key)

	meta, err := w.ipfs.DownloadMetadata(game.IPFSCID)
	if err != nil {
		return fmt.Errorf("load ipfs metadata: %w", err)
	}
	condition := meta.Condition
	if condition == "" {
		return fmt.Errorf("game %d has empty condition in ipfs metadata", game.ID)
	}

	quote, err := w.oracle.FetchQuote()
	if err != nil {
		return fmt.Errorf("fetch gold quote: %w", err)
	}

	winner := judge.EvaluateWinner(condition, quote)
	if winner < 0 {
		return fmt.Errorf("game %d: judge returned invalid winner", game.ID)
	}

	optionNames := []string{meta.OptionYES, meta.OptionNO}
	winnerName := judge.OptionName(optionNames, winner)

	slog.Info("game evaluated",
		"game_id", game.ID,
		"condition", condition,
		"winner_index", winner,
		"winner", winnerName,
		"gold_price_usd", quote.PriceUSD,
		"change_24h", quote.Change24h,
		"quote_source", quote.QuoteSource,
	)

	if w.cfg.ResolveDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.cfg.ResolveDelay):
		}
	}

	txData := chain.EncodeResolveGame(game.ID, winner)

	txResult, err := w.chain.SendTransaction(ctx, txData, nil)
	if err != nil {
		return fmt.Errorf("send resolveGame tx: %w", err)
	}

	slog.Info("game resolved on chain",
		"game_id", game.ID,
		"winner", winnerName,
		"tx", txResult,
	)
	return nil
}
