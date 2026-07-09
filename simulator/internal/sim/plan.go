package sim

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"predictionmarket-simulator/internal/scenario"
)

const planVersion = 1

type Plan struct {
	Version      int               `json:"version"`
	GeneratedAt  string            `json:"generated_at"`
	Scenario     string            `json:"scenario"`
	Participants []PlanParticipant `json:"participants"`
	Markets      []PlanMarket      `json:"markets"`
}

type PlanParticipant struct {
	Index      int    `json:"index"`
	Address    string `json:"address"`
	PrivateKey string `json:"private_key"`
}

type PlanMarket struct {
	Index               int         `json:"index"`
	PreviewGameID       int         `json:"preview_game_id,omitempty"`
	ExistingGameID      int         `json:"existing_game_id,omitempty"`
	Type                string      `json:"type,omitempty"`
	IPFSCID             string      `json:"ipfs_cid,omitempty"`
	Desc                string      `json:"desc,omitempty"`
	Condition           string      `json:"condition,omitempty"`
	AvatarURL           string      `json:"avatar_url,omitempty"`
	DetailedInfo        string      `json:"detailed_info,omitempty"`
	OptionYes           string      `json:"option_yes,omitempty"`
	OptionNo            string      `json:"option_no,omitempty"`
	CreatorIndex        int         `json:"creator_index"`
	CreatorAddress      string      `json:"creator_address,omitempty"`
	DurationSeconds     int64       `json:"duration_seconds,omitempty"`
	InitialLiquidityBKC string      `json:"initial_liquidity_bkc,omitempty"`
	InitialLiquidityWei string      `json:"initial_liquidity_wei,omitempty"`
	MetadataJSON        string      `json:"metadata_json,omitempty"`
	Trades              []PlanTrade `json:"trades"`
}

type PlanTrade struct {
	Index     int    `json:"index"`
	UserIndex int    `json:"user_index"`
	User      string `json:"user"`
	OptionID  int    `json:"option_id"`
	Option    string `json:"option"`
	AmountBKC string `json:"amount_bkc"`
	AmountWei string `json:"amount_wei"`
}

func writePlan(path string, plan *Plan) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	if path == "" {
		return fmt.Errorf("plan path is empty")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create plan dir: %w", err)
		}
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("encode plan: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write plan: %w", err)
	}
	return nil
}

func readPlan(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan %s: %w", path, err)
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("parse plan %s: %w", path, err)
	}
	if plan.Version != planVersion {
		return nil, fmt.Errorf("unsupported plan version %d", plan.Version)
	}
	if len(plan.Participants) == 0 {
		return nil, fmt.Errorf("plan has no participants")
	}
	if len(plan.Markets) == 0 {
		return nil, fmt.Errorf("plan has no markets")
	}
	return &plan, nil
}

func newPlan(scenarioType string) *Plan {
	return &Plan{
		Version:     planVersion,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Scenario:    scenarioType,
	}
}

func planMarketFromScenario(index int, previewGameID int, creatorIndex int, market *scenario.Market, initialWei *big.Int) PlanMarket {
	return PlanMarket{
		Index:               index,
		PreviewGameID:       previewGameID,
		Type:                market.Type,
		IPFSCID:             market.IPFSCID,
		Desc:                market.Desc,
		Condition:           market.Condition,
		AvatarURL:           market.AvatarURL,
		DetailedInfo:        market.DetailedInfo,
		OptionYes:           market.OptionYes,
		OptionNo:            market.OptionNo,
		CreatorIndex:        creatorIndex,
		CreatorAddress:      market.CreatorAddress,
		DurationSeconds:     market.DurationSeconds,
		InitialLiquidityBKC: market.InitialLiquidity,
		InitialLiquidityWei: initialWei.String(),
		MetadataJSON:        market.MetadataJSON,
	}
}

func scenarioMarketFromPlan(pm PlanMarket) (*scenario.Market, error) {
	if pm.IPFSCID == "" {
		return nil, fmt.Errorf("market #%d missing ipfs_cid", pm.Index)
	}
	return &scenario.Market{
		Type:             pm.Type,
		IPFSCID:          pm.IPFSCID,
		Desc:             pm.Desc,
		Condition:        pm.Condition,
		AvatarURL:        pm.AvatarURL,
		DetailedInfo:     pm.DetailedInfo,
		OptionYes:        pm.OptionYes,
		OptionNo:         pm.OptionNo,
		CreatorAddress:   pm.CreatorAddress,
		DurationSeconds:  pm.DurationSeconds,
		InitialLiquidity: pm.InitialLiquidityBKC,
		MetadataJSON:     pm.MetadataJSON,
	}, nil
}

func parseWei(raw string, label string) (*big.Int, error) {
	value := new(big.Int)
	if _, ok := value.SetString(raw, 10); !ok || value.Sign() < 0 {
		return nil, fmt.Errorf("%s must be a non-negative integer wei string", label)
	}
	return value, nil
}
