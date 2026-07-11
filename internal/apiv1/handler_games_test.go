package apiv1

import (
	"testing"

	"PredictionMarket/internal/ipfs"
)

type countingMetadataClient struct {
	calls int
}

func (c *countingMetadataClient) DownloadMetadata(string) (*ipfs.Metadata, error) {
	c.calls++
	return &ipfs.Metadata{Desc: "should not be used"}, nil
}

func TestHydrateGameSkipsLegacySimulatorCID(t *testing.T) {
	metadata := &countingMetadataClient{}
	server := NewServer(nil, nil, nil, nil, nil, nil, nil, metadata, "", 0)
	game := &GameMetaDTO{
		GameID:       60,
		IPFSCID:      "sim-7ff46e8ee879669959178615c7d09887",
		Desc:         "黄金价格 上涨",
		Condition:    "黄金价格在 从 2026-07-09 12:00 到 2026-07-10 12:00 相对基准 上涨 (Price Up)",
		DetailedInfo: "模拟用户创建的方向类黄金预测池。",
		OptionYes:    "YES",
		OptionNo:     "NO",
	}

	server.hydrateGameFromIPFS(game)
	if metadata.calls != 0 {
		t.Fatalf("metadata download calls = %d, want 0", metadata.calls)
	}
	if game.Desc != "黄金价格 上涨" {
		t.Fatalf("legacy game metadata changed: %+v", game)
	}
}
