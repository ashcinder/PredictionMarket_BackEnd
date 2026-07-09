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
	if !s.cfg.Runtime.Enabled && s.cfg.Runtime.Mode != config.ModePreview {
		return errors.New("runtime.enabled is false; set it true or run in runtime.mode=preview")
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timing.Timeout)
	defer cancel()

	if s.cfg.Runtime.Mode == config.ModePreview {
		plan, err := s.buildPlan()
		if err != nil {
			return err
		}
		if err := writePlan(s.cfg.Runtime.PlanFile, plan); err != nil {
			return err
		}
		s.logPlan(plan)
		s.logf("simulator: preview plan saved to %s; no database writes or chain transactions were sent", s.cfg.Runtime.PlanFile)
		return nil
	}

	plan, err := readPlan(s.cfg.Runtime.PlanFile)
	if err != nil {
		return err
	}
	return s.executePlan(ctx, plan)
}

func (s *Simulator) buildPlan() (*Plan, error) {
	participants, err := buildParticipants(s.cfg.Scenario.Participants)
	if err != nil {
		return nil, err
	}
	plan := newPlan(s.cfg.Scenario.Type)
	for i, p := range participants {
		plan.Participants = append(plan.Participants, PlanParticipant{
			Index:      i,
			Address:    p.address,
			PrivateKey: p.privateKey,
		})
	}

	switch s.cfg.Scenario.Type {
	case config.ScenarioCreateAndTrade:
		for i := 0; i < s.cfg.Scenario.MarketCount; i++ {
			creatorIndex := i % len(participants)
			creator := participants[creatorIndex]
			market, initialWei, err := s.buildMarket(i, creator.address)
			if err != nil {
				return nil, err
			}
			pm := planMarketFromScenario(i+1, i+1, creatorIndex, market, initialWei)
			pm.Trades, err = s.buildPlannedTrades(participants, creatorIndex)
			if err != nil {
				return nil, err
			}
			plan.Markets = append(plan.Markets, pm)
		}
	case config.ScenarioTradeExisting:
		if len(s.cfg.Scenario.ExistingGameIDs) == 0 {
			return nil, errors.New("scenario.existing_game_ids is required for trade_existing")
		}
		for i, gameID := range s.cfg.Scenario.ExistingGameIDs {
			trades, err := s.buildPlannedTrades(participants, -1)
			if err != nil {
				return nil, err
			}
			plan.Markets = append(plan.Markets, PlanMarket{
				Index:          i + 1,
				ExistingGameID: gameID,
				Trades:         trades,
			})
		}
	default:
		return nil, fmt.Errorf("unsupported scenario %q", s.cfg.Scenario.Type)
	}
	return plan, nil
}

func (s *Simulator) buildPlannedTrades(participants []participant, creatorIndex int) ([]PlanTrade, error) {
	tradeCount := randomIntInRange(s.rng, s.cfg.Scenario.TradesPerMarketMin, s.cfg.Scenario.TradesPerMarketMax)
	minWei, err := amount.ParseBKCToWei(s.cfg.Trade.BuyMinBKC)
	if err != nil {
		return nil, err
	}
	maxWei, err := amount.ParseBKCToWei(s.cfg.Trade.BuyMaxBKC)
	if err != nil {
		return nil, err
	}
	trades := make([]PlanTrade, 0, tradeCount)
	for i := 0; i < tradeCount; i++ {
		userIndex := s.rng.Intn(len(participants))
		if !s.cfg.Trade.CreatorAlsoTrades && creatorIndex >= 0 {
			for userIndex == creatorIndex {
				userIndex = (userIndex + 1) % len(participants)
			}
		}
		optionID := i % 2
		if s.rng.Intn(2) == 1 {
			optionID = 1 - optionID
		}
		amountWei, err := amount.RandomWeiInRange(minWei, maxWei)
		if err != nil {
			return nil, err
		}
		trades = append(trades, PlanTrade{
			Index:     i + 1,
			UserIndex: userIndex,
			User:      participants[userIndex].address,
			OptionID:  optionID,
			Option:    optionName(optionID),
			AmountBKC: weiToDisplayBKC(amountWei),
			AmountWei: amountWei.String(),
		})
	}
	return trades, nil
}

func (s *Simulator) executePlan(ctx context.Context, plan *Plan) error {
	participants, err := participantsFromPlan(plan)
	if err != nil {
		return err
	}
	s.logf("simulator: executing plan=%s participants=%d on_chain=%t scenario=%s",
		s.cfg.Runtime.PlanFile, len(participants), s.cfg.Runtime.OnChain, plan.Scenario)

	writer, err := dbwriter.Open(ctx, s.cfg.MySQL, s.cfg.Chain.ContractAddress)
	if err != nil {
		return err
	}
	defer writer.Close()

	if s.cfg.Runtime.OnChain {
		funder, err := chain.NewClient(
			s.cfg.Chain.PrivateKey,
			s.cfg.Chain.ContractAddress,
			s.cfg.Chain.RPCURL,
			s.cfg.Chain.BrokerChainURL,
			s.cfg.Chain.UseBrokerChain,
		)
		if err != nil {
			return err
		}
		if err := s.fundParticipantsForPlan(ctx, funder, participants, plan); err != nil {
			return err
		}
	}

	switch plan.Scenario {
	case config.ScenarioCreateAndTrade:
		return s.executeCreateAndTradePlan(ctx, writer, participants, plan)
	case config.ScenarioTradeExisting:
		return s.executeExistingTradePlan(ctx, writer, participants, plan)
	default:
		return fmt.Errorf("unsupported plan scenario %q", plan.Scenario)
	}
}

func (s *Simulator) executeCreateAndTradePlan(ctx context.Context, writer *dbwriter.Writer, participants []participant, plan *Plan) error {
	nextOffchainID := 1
	if !s.cfg.Runtime.OnChain {
		id, err := writer.NextGameID(ctx)
		if err != nil {
			return err
		}
		nextOffchainID = id
	}
	for i, plannedMarket := range plan.Markets {
		market, err := scenarioMarketFromPlan(plannedMarket)
		if err != nil {
			return err
		}
		initialWei, err := parseWei(plannedMarket.InitialLiquidityWei, "initial_liquidity_wei")
		if err != nil {
			return err
		}
		creator, err := participantAt(participants, plannedMarket.CreatorIndex)
		if err != nil {
			return err
		}
		gameID := nextOffchainID + i
		var info *chain.GameInfo
		var offchain *offchainMarketState

		s.logf("simulator: create market #%d type=%s creator=%s initial=%s BKC cid=%s",
			plannedMarket.Index, market.Type, creator.address, market.InitialLiquidity, market.IPFSCID)
		if s.cfg.Runtime.OnChain {
			client, err := chain.NewClient(creator.privateKey, s.cfg.Chain.ContractAddress, s.cfg.Chain.RPCURL, s.cfg.Chain.BrokerChainURL, s.cfg.Chain.UseBrokerChain)
			if err != nil {
				return err
			}
			tx, err := client.CreateGame(ctx, market.IPFSCID, market.DurationSeconds, initialWei)
			if err != nil {
				return fmt.Errorf("create market #%d: %w", plannedMarket.Index, err)
			}
			s.logf("simulator: created market tx=%s", tx)
			if s.cfg.Chain.UseBrokerChain {
				if err := sleepWithContext(ctx, 8*time.Second); err != nil {
					return err
				}
			}
			gameID, err = client.GameCount(ctx)
			if err != nil {
				return fmt.Errorf("read gameCount after create: %w", err)
			}
			info, err = client.GetGameInfo(ctx, gameID)
			if err != nil {
				return fmt.Errorf("read created game info: %w", err)
			}
		} else {
			offchain = newOffchainMarketState(gameID, market.IPFSCID, initialWei, time.Now().Add(time.Duration(market.DurationSeconds)*time.Second).Unix())
			info = offchain.info
		}
		if err := writer.SyncCreatedMarket(ctx, gameID, market, info, initialWei, time.Now().Unix()); err != nil {
			return err
		}
		if err := s.executePlannedTradesForMarket(ctx, writer, participants, gameID, offchain, plannedMarket.Trades); err != nil {
			return err
		}
	}
	return nil
}

func (s *Simulator) executeExistingTradePlan(ctx context.Context, writer *dbwriter.Writer, participants []participant, plan *Plan) error {
	if !s.cfg.Runtime.OnChain {
		return errors.New("trade_existing execution requires runtime.on_chain=true")
	}
	for _, plannedMarket := range plan.Markets {
		if plannedMarket.ExistingGameID <= 0 {
			return fmt.Errorf("market #%d missing existing_game_id", plannedMarket.Index)
		}
		if err := s.executePlannedTradesForMarket(ctx, writer, participants, plannedMarket.ExistingGameID, nil, plannedMarket.Trades); err != nil {
			return err
		}
	}
	return nil
}

func (s *Simulator) executePlannedTradesForMarket(ctx context.Context, writer *dbwriter.Writer, participants []participant, gameID int, offchain *offchainMarketState, trades []PlanTrade) error {
	for _, trade := range trades {
		p, err := participantAt(participants, trade.UserIndex)
		if err != nil {
			return err
		}
		if trade.User != "" && !strings.EqualFold(trade.User, p.address) {
			return fmt.Errorf("trade #%d user does not match participant index %d", trade.Index, trade.UserIndex)
		}
		if trade.OptionID != 0 && trade.OptionID != 1 {
			return fmt.Errorf("trade #%d has invalid option_id %d", trade.Index, trade.OptionID)
		}
		amountWei, err := parseWei(trade.AmountWei, "amount_wei")
		if err != nil {
			return err
		}
		s.logf("simulator: game=%d trade #%d user=%s option=%s amount_wei=%s",
			gameID, trade.Index, p.address, optionName(trade.OptionID), amountWei.String())
		var record *dbwriter.TradeRecord
		if s.cfg.Runtime.OnChain {
			record, err = s.executeOnchainTrade(ctx, p, gameID, trade.OptionID, amountWei)
		} else {
			record, err = executeOffchainTrade(offchain, p.address, trade.OptionID, amountWei)
		}
		if err != nil {
			return err
		}
		if err := writer.SyncTrade(ctx, record); err != nil {
			return err
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

func (s *Simulator) fundParticipantsForPlan(ctx context.Context, funder *chain.Client, participants []participant, plan *Plan) error {
	required := make([]*big.Int, len(participants))
	for i := range required {
		required[i] = big.NewInt(0)
	}
	for _, market := range plan.Markets {
		if market.InitialLiquidityWei != "" {
			initialWei, err := parseWei(market.InitialLiquidityWei, "initial_liquidity_wei")
			if err != nil {
				return err
			}
			if market.CreatorIndex < 0 || market.CreatorIndex >= len(required) {
				return fmt.Errorf("market #%d creator index out of range", market.Index)
			}
			required[market.CreatorIndex].Add(required[market.CreatorIndex], initialWei)
		}
		for _, trade := range market.Trades {
			amountWei, err := parseWei(trade.AmountWei, "amount_wei")
			if err != nil {
				return err
			}
			if trade.UserIndex < 0 || trade.UserIndex >= len(required) {
				return fmt.Errorf("trade #%d user index out of range", trade.Index)
			}
			required[trade.UserIndex].Add(required[trade.UserIndex], amountWei)
		}
	}
	buffer, _ := amount.ParseBKCToWei("0.1")
	for i, p := range participants {
		required[i].Add(required[i], buffer)
		if required[i].Sign() <= 0 {
			continue
		}
		s.logf("simulator: funding participant #%d %s with %s wei", i+1, p.address, required[i].String())
		if _, err := funder.SendNativeTransfer(ctx, p.address, required[i]); err != nil {
			return fmt.Errorf("fund participant #%d: %w", i+1, err)
		}
	}
	if s.cfg.Chain.UseBrokerChain {
		return sleepWithContext(ctx, 8*time.Second)
	}
	return nil
}

func participantsFromPlan(plan *Plan) ([]participant, error) {
	if plan == nil {
		return nil, errors.New("plan is nil")
	}
	out := make([]participant, 0, len(plan.Participants))
	for i, item := range plan.Participants {
		if item.Address == "" {
			return nil, fmt.Errorf("participant #%d missing address", i+1)
		}
		if item.PrivateKey == "" {
			return nil, fmt.Errorf("participant #%d missing private_key", i+1)
		}
		out = append(out, participant{
			privateKey: item.PrivateKey,
			address:    item.Address,
		})
	}
	return out, nil
}

func participantAt(participants []participant, index int) (participant, error) {
	if index < 0 || index >= len(participants) {
		return participant{}, fmt.Errorf("participant index %d out of range", index)
	}
	return participants[index], nil
}

func (s *Simulator) logPlan(plan *Plan) {
	s.logf("simulator: preview participants=%d markets=%d scenario=%s target_on_chain=%t",
		len(plan.Participants), len(plan.Markets), plan.Scenario, s.cfg.Runtime.OnChain)
	for _, market := range plan.Markets {
		if market.ExistingGameID > 0 {
			s.logf("simulator: preview existing game=%d trades=%d", market.ExistingGameID, len(market.Trades))
		} else {
			s.logf("simulator: preview create market #%d type=%s creator=%s initial=%s BKC trades=%d cid=%s",
				market.Index, market.Type, market.CreatorAddress, market.InitialLiquidityBKC, len(market.Trades), market.IPFSCID)
		}
		for _, trade := range market.Trades {
			s.logf("simulator: preview trade #%d user=%s option=%s amount=%s BKC",
				trade.Index, trade.User, trade.Option, trade.AmountBKC)
		}
	}
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
