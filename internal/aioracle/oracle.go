package aioracle

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Oracle is the top-level AI oracle that coordinates news fetching, multi-model
// querying, and consensus to produce verdicts for real-world events.
//
// It can be used in two modes:
//  1. Standalone: call Resolve(event) to get a Verdict for a single event.
//  2. Continuous: call Run(ctx, events) to poll a list of events until their
//     deadlines, issuing verdicts as soon as consensus confidence is reached.
type Oracle struct {
	newsFetcher  NewsFetcher
	consensus    *ConsensusEngine
	pollTick     time.Duration
	newsLookback time.Duration
	maxArticles  int

	mu       sync.RWMutex
	verdicts map[string]*Verdict // eventID -> latest verdict
}

// NewOracle creates an AI oracle with the given news fetcher, consensus engine,
// and polling interval (for continuous mode).
func NewOracle(news NewsFetcher, consensus *ConsensusEngine, pollInterval time.Duration) *Oracle {
	return NewOracleWithOptions(news, consensus, OracleOptions{
		PollInterval: pollInterval,
	})
}

// OracleOptions controls polling and evidence collection.
type OracleOptions struct {
	PollInterval time.Duration
	NewsLookback time.Duration
	MaxArticles  int
}

// NewOracleWithOptions creates an oracle whose news window and article limit
// come from configuration instead of being hard-coded in Resolve.
func NewOracleWithOptions(news NewsFetcher, consensus *ConsensusEngine, opts OracleOptions) *Oracle {
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = 5 * time.Minute
	}
	newsLookback := opts.NewsLookback
	if newsLookback <= 0 {
		newsLookback = 72 * time.Hour
	}
	maxArticles := opts.MaxArticles
	if maxArticles <= 0 {
		maxArticles = 10
	}
	return &Oracle{
		newsFetcher:  news,
		consensus:    consensus,
		pollTick:     pollInterval,
		newsLookback: newsLookback,
		maxArticles:  maxArticles,
		verdicts:     make(map[string]*Verdict),
	}
}

// Resolve evaluates a single event: fetches news, queries all models, and
// returns a consensus verdict. This is a synchronous call — use ResolveAsync
// for concurrent resolution of multiple events.
func (o *Oracle) Resolve(ctx context.Context, event Event) *Verdict {
	// Fetch news.
	var articles []NewsArticle
	if o.newsFetcher != nil {
		since := time.Now().Add(-o.newsLookback)
		var err error
		articles, err = o.newsFetcher.Fetch(ctx, event.Keywords, since, o.maxArticles)
		if err != nil {
			slog.Warn("aioracle: news fetch failed, proceeding without news",
				"event_id", event.ID, "error", err)
		}
	}

	slog.Info("aioracle: resolving event",
		"event_id", event.ID,
		"title", event.Title,
		"articles", len(articles),
		"models", o.consensus.ProviderCount(),
	)

	verdict := o.consensus.Judge(ctx, event, articles)
	if len(articles) == 0 && verdict.Resolved {
		candidateSummary := verdict.Summary
		verdict.Occurred = false
		verdict.Decision = DecisionIndeterminate
		verdict.Resolved = false
		verdict.Summary = "indeterminate: no external evidence was available; candidate consensus was: " + candidateSummary
	}

	o.mu.Lock()
	o.verdicts[event.ID] = verdict
	o.mu.Unlock()

	slog.Info("aioracle: verdict reached",
		"event_id", event.ID,
		"occurred", verdict.Occurred,
		"decision", verdict.Decision,
		"resolved", verdict.Resolved,
		"confidence", verdict.Confidence,
		"consensus_ratio", verdict.ConsensusRatio,
		"agreeing_models", fmt.Sprintf("%d/%d", verdict.AgreeingModels, len(verdict.Opinions)),
	)

	return verdict
}

// ResolveAsync resolves multiple events concurrently and returns verdicts
// in the same order as the input events (nil for events that failed to resolve).
func (o *Oracle) ResolveAsync(ctx context.Context, events []Event) []*Verdict {
	results := make([]*Verdict, len(events))
	var wg sync.WaitGroup

	for i := range events {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = o.Resolve(ctx, events[idx])
		}(i)
	}
	wg.Wait()
	return results
}

// LatestVerdict returns the most recent verdict for an event (or nil).
func (o *Oracle) LatestVerdict(eventID string) *Verdict {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.verdicts[eventID]
}

// AllVerdicts returns a copy of all verdicts produced so far.
func (o *Oracle) AllVerdicts() map[string]*Verdict {
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make(map[string]*Verdict, len(o.verdicts))
	for k, v := range o.verdicts {
		out[k] = v
	}
	return out
}

// Run is the continuous resolution loop. It polls events periodically and
// issues verdicts when their deadline is reached or consensus confidence
// crosses the threshold. It stops when ctx is cancelled.
//
// Verdicts are delivered through the returned channel. The caller should
// range over this channel to receive verdicts as they become available.
func (o *Oracle) Run(ctx context.Context, events []Event) <-chan *Verdict {
	out := make(chan *Verdict, 16)

	go func() {
		defer close(out)

		if len(events) == 0 {
			slog.Info("aioracle: no events to monitor, run loop exiting")
			return
		}

		// Track which events still need resolution.
		pending := make(map[string]Event, len(events))
		for _, ev := range events {
			pending[ev.ID] = ev
		}

		slog.Info("aioracle: continuous resolution started",
			"events", len(pending),
			"poll_interval", o.pollTick.String(),
			"models", o.consensus.ProviderCount(),
		)

		ticker := time.NewTicker(o.pollTick)
		defer ticker.Stop()

		// Resolve immediately on start.
		o.resolvePending(ctx, pending, out)

		for {
			if len(pending) == 0 {
				slog.Info("aioracle: all events resolved, run loop exiting")
				return
			}

			select {
			case <-ctx.Done():
				slog.Info("aioracle: run loop stopped by context")
				return
			case <-ticker.C:
				o.resolvePending(ctx, pending, out)
			}
		}
	}()

	return out
}

// resolvePending evaluates all pending events once. Events that produce a
// confident verdict (or are past deadline) are removed from pending and
// emitted to the output channel.
func (o *Oracle) resolvePending(ctx context.Context, pending map[string]Event, out chan<- *Verdict) {
	now := time.Now()
	var toResolve []Event

	for _, ev := range pending {
		toResolve = append(toResolve, ev)
	}

	if len(toResolve) == 0 {
		return
	}

	verdicts := o.ResolveAsync(ctx, toResolve)

	for _, v := range verdicts {
		if v == nil {
			continue
		}

		ev, ok := pending[v.EventID]
		if !ok {
			continue
		}

		// A resolved NO is just as final as a resolved YES. An indeterminate
		// verdict may be emitted at the deadline for audit/manual review, but
		// Resolved remains false so callers cannot mistake it for NO.
		isPastDeadline := now.After(ev.Deadline) || now.Equal(ev.Deadline)

		if v.Resolved || isPastDeadline {
			if isPastDeadline && !v.Resolved {
				slog.Info("aioracle: event past deadline — emitting indeterminate verdict for manual review",
					"event_id", v.EventID,
					"confidence", v.Confidence,
					"decision", v.Decision,
				)
			}
			delete(pending, v.EventID)
			out <- v
		} else {
			slog.Info("aioracle: event still pending — confidence insufficient",
				"event_id", v.EventID,
				"confidence", v.Confidence,
				"min_confidence", o.consensus.ConsensusConfig().MinConfidence,
				"deadline", ev.Deadline.Format(time.RFC3339),
			)
		}
	}
}

// =============================================================================
// Integration helpers — making the AI oracle usable alongside existing judges
// =============================================================================

// ResolvedEvent carries the oracle's verdict back to the caller (e.g., sentinel).
type ResolvedEvent struct {
	Event   Event
	Verdict *Verdict
}

// SimpleResolve is a convenience function for one-shot resolution with a fully
// configured oracle. It's the simplest way to use the AI oracle from external
// code like the sentinel watcher.
func SimpleResolve(ctx context.Context, oracle *Oracle, eventID, title, description string, keywords []string, deadline time.Time) *Verdict {
	return oracle.Resolve(ctx, Event{
		ID:          eventID,
		Title:       title,
		Description: description,
		Keywords:    keywords,
		Deadline:    deadline,
	})
}
