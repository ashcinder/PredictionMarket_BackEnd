package marketdata

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"PredictionMarket/internal/chainlinkfeed"

	"github.com/ethereum/go-ethereum/common"
)

type latestRoundReader interface {
	LatestRound(context.Context, common.Address) (chainlinkfeed.Round, error)
}

type roundSaver interface {
	Save(context.Context, chainlinkfeed.Round) error
}

type ChainlinkRoundRecorder struct {
	client   latestRoundReader
	repo     roundSaver
	feeds    []common.Address
	interval time.Duration

	mu       sync.Mutex
	lastSeen map[common.Address]string
}

func NewChainlinkRoundRecorder(
	client latestRoundReader, repo roundSaver, feeds []common.Address, interval time.Duration,
) *ChainlinkRoundRecorder {
	if interval <= 0 {
		interval = time.Minute
	}
	return &ChainlinkRoundRecorder{
		client: client, repo: repo, feeds: append([]common.Address(nil), feeds...),
		interval: interval, lastSeen: make(map[common.Address]string),
	}
}

func (r *ChainlinkRoundRecorder) Run(ctx context.Context) {
	if r == nil || r.client == nil || r.repo == nil || len(r.feeds) == 0 {
		slog.Warn("Chainlink round recorder is disabled because dependencies are incomplete")
		return
	}
	r.recordOnce(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("Chainlink round recorder stopped")
			return
		case <-ticker.C:
			r.recordOnce(ctx)
		}
	}
}

func (r *ChainlinkRoundRecorder) recordOnce(ctx context.Context) {
	for _, feed := range r.feeds {
		round, err := r.client.LatestRound(ctx, feed)
		if err != nil {
			slog.Warn("read latest Chainlink round failed", "feed", feed.Hex(), "error", err)
			continue
		}
		r.mu.Lock()
		seen := r.lastSeen[feed] == round.ID.String()
		r.mu.Unlock()
		if seen {
			continue
		}
		if err := r.repo.Save(ctx, round); err != nil {
			slog.Warn("save latest Chainlink round failed", "feed", feed.Hex(), "round_id", round.ID, "error", err)
			continue
		}
		r.mu.Lock()
		r.lastSeen[feed] = round.ID.String()
		r.mu.Unlock()
		slog.Info("Chainlink round saved",
			"feed", feed.Hex(), "round_id", round.ID, "source_time", round.UpdatedAt,
			"price_usd", round.Price)
	}
}
