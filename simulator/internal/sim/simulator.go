package sim

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	mrand "math/rand"
	"strings"
	"time"

	"predictionmarket-simulator/internal/amount"
	"predictionmarket-simulator/internal/chain"
	"predictionmarket-simulator/internal/config"
	dbwriter "predictionmarket-simulator/internal/db"
	"predictionmarket-simulator/internal/scenario"

	"github.com/ethereum/go-ethereum/crypto"
)

type Logger interface {
	Printf(format string, args ...any)
}

type Simulator struct {
	cfg *config.Config
	log Logger
	rng *mrand.Rand
}

type participant struct {
	privateKey string
	address    string
}

type offchainMarketState struct {
	info         *chain.GameInfo
	reserveYes   *big.Int
	reserveNo    *big.Int
	userShares   map[string][]*big.Int
	nextTradeSeq int
}

func New(cfg *config.Config, logger Logger) *Simulator {
	return &Simulator{
		cfg: cfg,
		log: logger,
		rng: mrand.New(mrand.NewSource(time.Now().UnixNano())),
	}
}

func (s *Simulator) Run(ctx context.Context) error {
	if !s.cfg.Runtime.Enabled && !s.cfg.Runtime.DryRun {
		return errors.New("runtime.enabled is false; set it true or run with runtime.dry_run=true")
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timing.Timeout)
	defer cancel()

	participants, err := buildParticipants(s.cfg.Scenario.Participants)
	if err != nil {
		return err
	}
	s.logf("simulator: participants=%d on_chain=%t dry_run=%t scenario=%s",
		len(participants), s.cfg.Runtime.OnChain, s.cfg.Runtime.DryRun, s.cfg.Scenario.Type)

	var writer *dbwriter.Writer
	if !s.cfg.Runtime.DryRun {
		writer, err = dbwriter.Open(ctx, s.cfg.MySQL, s.cfg.Chain.ContractAddress)
		if err != nil {
			return err
		}
		defer writer.Close()
	}

	var funder *chain.Client
	if s.cfg.Runtime.OnChain {
		funder, err = chain.NewClient(
			s.cfg.Chain.PrivateKey,
			s.cfg.Chain.ContractAddress,
			s.cfg.Chain.RPCURL,
			s.cfg.Chain.BrokerChainURL,
			s.cfg.Chain.UseBrokerChain,
		)
		if err != nil {
			return err
		}
		if !s.cfg.Runtime.DryRun {
			if err := s.fundParticipants(ctx, funder, participants); err != nil {
				return err
			}
		}
	}

	switch s.cfg.Scenario.Type {
	case config.ScenarioCreateAndTrade:
		return s.runCreateAndTrade(ctx, writer, participants)
	case config.ScenarioTradeExisting:
		return s.runTradeExisting(ctx, writer, participants)
	default:
		return fmt.Errorf("unsupported scenario %q", s.cfg.Scenario.Type)
	}
}

func (s *Simulator) runCreateAndTrade(ctx context.Context, writer *dbwriter.Writer, participants []participant) error {
	nextOffchainID := 1
	if writer != nil && !s.cfg.Runtime.OnChain {
		id, err := writer.NextGameID(ctx)
		if err != nil {
			return err
		}
		nextOffchainID = id
	}
	for i := 0; i < s.cfg.Scenario.MarketCount; i++ {
		creator := participants[i%len(participants)]
		market, initialWei, err := s.buildMarket(i, creator.address)
		if err != nil {
			return err
		}
		gameID := nextOffchainID + i
		var info *chain.GameInfo
		var offchain *offchainMarketState

		s.logf("simulator: create market #%d type=%s creator=%s initial=%s BKC cid=%s",
			i+1, market.Type, creator.address, market.InitialLiquidity, market.IPFSCID)
		if s.cfg.Runtime.OnChain {
			if s.cfg.Runtime.DryRun {
				gameID = i + 1
			} else {
				client, err := chain.NewClient(creator.privateKey, s.cfg.Chain.ContractAddress, s.cfg.Chain.RPCURL, s.cfg.Chain.BrokerChainURL, s.cfg.Chain.UseBrokerChain)
				if err != nil {
					return err
				}
				tx, err := client.CreateGame(ctx, market.IPFSCID, market.DurationSeconds, initialWei)
				if err != nil {
					return fmt.Errorf("create market #%d: %w", i+1, err)
				}
				s.logf("simulator: created market tx=%s", tx)
				gameID, err = client.GameCount(ctx)
				if err != nil {
					return fmt.Errorf("read gameCount after create: %w", err)
				}
				info, err = client.GetGameInfo(ctx, gameID)
				if err != nil {
					return fmt.Errorf("read created game info: %w", err)
				}
			}
		} else {
			offchain = newOffchainMarketState(gameID, market.IPFSCID, initialWei, time.Now().Add(time.Duration(market.DurationSeconds)*time.Second).Unix())
			info = offchain.info
		}
		if writer != nil {
			if err := writer.SyncCreatedMarket(ctx, gameID, market, info, initialWei, time.Now().Unix()); err != nil {
				return err
			}
		}
		if err := s.runTradesForMarket(ctx, writer, participants, creator.address, gameID, offchain); err != nil {
			return err
		}
	}
	return nil
}

func (s *Simulator) runTradeExisting(ctx context.Context, writer *dbwriter.Writer, participants []participant) error {
	if len(s.cfg.Scenario.ExistingGameIDs) == 0 {
		return errors.New("scenario.existing_game_ids is required for trade_existing")
	}
	for _, gameID := range s.cfg.Scenario.ExistingGameIDs {
		if err := s.runTradesForMarket(ctx, writer, participants, "", gameID, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *Simulator) runTradesForMarket(ctx context.Context, writer *dbwriter.Writer, participants []participant, creatorAddress string, gameID int, offchain *offchainMarketState) error {
	trades := randomIntInRange(s.rng, s.cfg.Scenario.TradesPerMarketMin, s.cfg.Scenario.TradesPerMarketMax)
	minWei, err := amount.ParseBKCToWei(s.cfg.Trade.BuyMinBKC)
	if err != nil {
		return err
	}
	maxWei, err := amount.ParseBKCToWei(s.cfg.Trade.BuyMaxBKC)
	if err != nil {
		return err
	}
	for i := 0; i < trades; i++ {
		p := participants[s.rng.Intn(len(participants))]
		if !s.cfg.Trade.CreatorAlsoTrades && creatorAddress != "" && equalAddress(p.address, creatorAddress) {
			p = participants[(i+1)%len(participants)]
		}
		optionID := i % 2
		if s.rng.Intn(2) == 1 {
			optionID = 1 - optionID
		}
		amountWei, err := amount.RandomWeiInRange(minWei, maxWei)
		if err != nil {
			return err
		}
		s.logf("simulator: game=%d trade #%d user=%s option=%s amount_wei=%s",
			gameID, i+1, p.address, optionName(optionID), amountWei.String())
		if s.cfg.Runtime.DryRun {
			continue
		}
		var record *dbwriter.TradeRecord
		if s.cfg.Runtime.OnChain {
			record, err = s.executeOnchainTrade(ctx, p, gameID, optionID, amountWei)
		} else {
			record, err = executeOffchainTrade(offchain, p.address, optionID, amountWei)
		}
		if err != nil {
			return err
		}
		if writer != nil {
			if err := writer.SyncTrade(ctx, record); err != nil {
				return err
			}
		}
		if err := sleepWithContext(ctx, s.cfg.Timing.Pause); err != nil {
			return err
		}
	}
	return nil
}

func (s *Simulator) executeOnchainTrade(ctx context.Context, p participant, gameID int, optionID int, amountWei *big.Int) (*dbwriter.TradeRecord, error) {
	client, err := chain.NewClient(p.privateKey, s.cfg.Chain.ContractAddress, s.cfg.Chain.RPCURL, s.cfg.Chain.BrokerChainURL, s.cfg.Chain.UseBrokerChain)
	if err != nil {
		return nil, err
	}
	before, err := client.GetGameExtraData(ctx, gameID, p.address)
	if err != nil {
		return nil, fmt.Errorf("read pre-trade shares: %w", err)
	}
	tx, err := client.BuyShares(ctx, gameID, optionID, amountWei)
	if err != nil {
		return nil, fmt.Errorf("buy shares: %w", err)
	}
	info, err := client.GetGameInfo(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("read post-trade game info: %w", err)
	}
	after, err := client.GetGameExtraData(ctx, gameID, p.address)
	if err != nil {
		return nil, fmt.Errorf("read post-trade shares: %w", err)
	}
	shareDelta := shareDeltaForOption(before, after, optionID)
	yes, no := dbwriter.PricesFromReserves(dbwriter.ShareAt(after.VirtualReservesNOYES, 0), dbwriter.ShareAt(after.VirtualReservesNOYES, 1))
	return &dbwriter.TradeRecord{
		GameID:           gameID,
		UserAddress:      p.address,
		OptionID:         optionID,
		AmountWei:        new(big.Int).Set(amountWei),
		ShareAmountWei:   shareDelta,
		TxHash:           tx,
		TimestampSec:     time.Now().Unix(),
		Info:             info,
		Extra:            after,
		YesPrice:         yes,
		NoPrice:          no,
		PriceAtTrade:     dbwriter.PriceForOption(optionID, yes, no),
		MySharesYesAfter: dbwriter.BigIntString(dbwriter.ShareAt(after.MySharesYESNO, 0)),
		MySharesNoAfter:  dbwriter.BigIntString(dbwriter.ShareAt(after.MySharesYESNO, 1)),
	}, nil
}

func executeOffchainTrade(state *offchainMarketState, userAddress string, optionID int, amountWei *big.Int) (*dbwriter.TradeRecord, error) {
	if state == nil {
		return nil, errors.New("offchain market state is required")
	}
	state.nextTradeSeq++
	state.info.TotalPool = new(big.Int).Add(state.info.TotalPool, amountWei)
	sharesToUser := new(big.Int).Set(amountWei)
	k := new(big.Int).Mul(state.reserveYes, state.reserveNo)
	if optionID == 0 {
		state.reserveNo.Add(state.reserveNo, amountWei)
		newReserveYes := new(big.Int).Div(k, state.reserveNo)
		sharesToUser.Add(sharesToUser, new(big.Int).Sub(state.reserveYes, newReserveYes))
		state.reserveYes = newReserveYes
	} else {
		state.reserveYes.Add(state.reserveYes, amountWei)
		newReserveNo := new(big.Int).Div(k, state.reserveYes)
		sharesToUser.Add(sharesToUser, new(big.Int).Sub(state.reserveNo, newReserveNo))
		state.reserveNo = newReserveNo
	}
	shares := state.userShares[userAddress]
	if shares == nil {
		shares = []*big.Int{big.NewInt(0), big.NewInt(0)}
		state.userShares[userAddress] = shares
	}
	shares[optionID].Add(shares[optionID], sharesToUser)
	yes, no := dbwriter.PricesFromReserves(state.reserveNo, state.reserveYes)
	extra := &chain.GameExtraData{
		VirtualReservesNOYES: []*big.Int{new(big.Int).Set(state.reserveNo), new(big.Int).Set(state.reserveYes)},
		MySharesYESNO:        []*big.Int{new(big.Int).Set(shares[0]), new(big.Int).Set(shares[1])},
	}
	return &dbwriter.TradeRecord{
		GameID:           state.info.ID,
		UserAddress:      userAddress,
		OptionID:         optionID,
		AmountWei:        new(big.Int).Set(amountWei),
		ShareAmountWei:   sharesToUser,
		TxHash:           fmt.Sprintf("sim-%d-%d", state.info.ID, state.nextTradeSeq),
		TimestampSec:     time.Now().Unix(),
		Info:             cloneInfo(state.info),
		Extra:            extra,
		YesPrice:         yes,
		NoPrice:          no,
		PriceAtTrade:     dbwriter.PriceForOption(optionID, yes, no),
		MySharesYesAfter: dbwriter.BigIntString(shares[0]),
		MySharesNoAfter:  dbwriter.BigIntString(shares[1]),
	}, nil
}

func (s *Simulator) buildMarket(index int, creator string) (*scenario.Market, *big.Int, error) {
	initialMin, err := amount.ParseBKCToWei(s.cfg.Market.InitialLiquidityMinBKC)
	if err != nil {
		return nil, nil, err
	}
	initialMax, err := amount.ParseBKCToWei(s.cfg.Market.InitialLiquidityMaxBKC)
	if err != nil {
		return nil, nil, err
	}
	initialWei, err := amount.RandomWeiInRange(initialMin, initialMax)
	if err != nil {
		return nil, nil, err
	}
	duration := randomDurationInRange(s.rng, s.cfg.Market.DurationMin, s.cfg.Market.DurationMax)
	typ := s.cfg.Market.Types[index%len(s.cfg.Market.Types)]
	market, err := scenario.BuildMarket(scenario.BuildMarketInput{
		Type:         typ,
		Index:        index + 1,
		Creator:      creator,
		Duration:     duration,
		InitialBKC:   weiToDisplayBKC(initialWei),
		Now:          time.Now().UTC(),
		TemplateSeed: s.rng.Int(),
	})
	if err != nil {
		return nil, nil, err
	}
	return market, initialWei, nil
}

func (s *Simulator) fundParticipants(ctx context.Context, funder *chain.Client, participants []participant) error {
	initialMax, err := amount.ParseBKCToWei(s.cfg.Market.InitialLiquidityMaxBKC)
	if err != nil {
		return err
	}
	buyMax, err := amount.ParseBKCToWei(s.cfg.Trade.BuyMaxBKC)
	if err != nil {
		return err
	}
	buffer, _ := amount.ParseBKCToWei("0.1")
	perAccount := new(big.Int).Set(initialMax)
	perAccount.Add(perAccount, new(big.Int).Mul(buyMax, big.NewInt(int64(s.cfg.Scenario.TradesPerMarketMax+1))))
	perAccount.Add(perAccount, buffer)
	for i, p := range participants {
		s.logf("simulator: funding participant #%d %s with %s wei", i+1, p.address, perAccount.String())
		if _, err := funder.SendNativeTransfer(ctx, p.address, perAccount); err != nil {
			return fmt.Errorf("fund participant #%d: %w", i+1, err)
		}
	}
	if s.cfg.Chain.UseBrokerChain {
		return sleepWithContext(ctx, 8*time.Second)
	}
	return nil
}

func buildParticipants(count int) ([]participant, error) {
	if count <= 0 {
		return nil, errors.New("participant count must be positive")
	}
	out := make([]participant, 0, count)
	seen := map[string]struct{}{}
	for len(out) < count {
		key, err := crypto.GenerateKey()
		if err != nil {
			return nil, err
		}
		privateKey := hex.EncodeToString(crypto.FromECDSA(key))
		address := crypto.PubkeyToAddress(key.PublicKey).Hex()
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		out = append(out, participant{privateKey: privateKey, address: address})
	}
	return out, nil
}

func newOffchainMarketState(gameID int, cid string, initialWei *big.Int, deadlineSec int64) *offchainMarketState {
	return &offchainMarketState{
		info: &chain.GameInfo{
			ID:          gameID,
			IPFSCID:     cid,
			TotalPool:   new(big.Int).Set(initialWei),
			DeadlineRaw: deadlineSec,
		},
		reserveYes: new(big.Int).Set(initialWei),
		reserveNo:  new(big.Int).Set(initialWei),
		userShares: map[string][]*big.Int{},
	}
}

func cloneInfo(info *chain.GameInfo) *chain.GameInfo {
	out := *info
	out.TotalPool = new(big.Int).Set(info.TotalPool)
	return &out
}

func shareDeltaForOption(before *chain.GameExtraData, after *chain.GameExtraData, optionID int) *big.Int {
	beforeShares := big.NewInt(0)
	afterShares := big.NewInt(0)
	if before != nil {
		beforeShares = dbwriter.ShareAt(before.MySharesYESNO, optionID)
	}
	if after != nil {
		afterShares = dbwriter.ShareAt(after.MySharesYESNO, optionID)
	}
	delta := new(big.Int).Sub(afterShares, beforeShares)
	if delta.Sign() < 0 {
		return big.NewInt(0)
	}
	return delta
}

func randomDurationInRange(rng *mrand.Rand, min time.Duration, max time.Duration) time.Duration {
	if min >= max {
		return min
	}
	delta := int64(max - min)
	return min + time.Duration(rng.Int63n(delta+1))
}

func randomIntInRange(rng *mrand.Rand, min int, max int) int {
	if min >= max {
		return min
	}
	return min + rng.Intn(max-min+1)
}

func weiToDisplayBKC(wei *big.Int) string {
	if wei == nil {
		return "0"
	}
	rat := new(big.Rat).SetFrac(wei, big.NewInt(1_000_000_000_000_000_000))
	return rat.FloatString(4)
}

func optionName(optionID int) string {
	if optionID == 0 {
		return "YES"
	}
	return "NO"
}

func equalAddress(a string, b string) bool {
	return strings.EqualFold(a, b)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Simulator) logf(format string, args ...any) {
	if s.log != nil {
		s.log.Printf(format, args...)
	}
}
