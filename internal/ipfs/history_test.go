package ipfs

import (
	"encoding/hex"
	"math"
	"testing"
)

func TestDownloadMetadataNormalizesHistoryAliasesAndComplements(t *testing.T) {
	client := NewClient("http://unused.example/ipfs/")
	body := `{
		"desc":"gold closes higher",
		"condition":"close above 2500",
		"detailedInfo":"settled from the official close",
		"history":[
			{"time":20,"yes":55,"no":45},
			{"t":10,"y":60,"n":40},
			{"t":15,"y":70},
			{"time":16,"no":65},
			{"t":10,"y":58,"n":42},
			{"t":18,"y":60.1,"n":40.1}
		]
	}`

	meta, err := client.DownloadMetadata("inline-v1:" + hex.EncodeToString([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Desc != "gold closes higher" || meta.Condition != "close above 2500" ||
		meta.DetailedInfo != "settled from the official close" {
		t.Fatalf("static metadata was not preserved: %+v", meta)
	}
	if len(meta.History) != 5 {
		t.Fatalf("expected 5 normalized history points, got %d: %+v", len(meta.History), meta.History)
	}

	wantTimes := []int64{10, 15, 16, 18, 20}
	for i, want := range wantTimes {
		if meta.History[i].Time != want {
			t.Fatalf("history is not sorted: point %d has time %d, want %d", i, meta.History[i].Time, want)
		}
	}
	if got := meta.History[0]; got.YesPercent != 58 || got.NoPercent != 42 {
		t.Fatalf("duplicate timestamp did not keep the last point: %+v", got)
	}
	if got := meta.History[1]; got.YesPercent != 70 || got.NoPercent != 30 {
		t.Fatalf("missing NO percentage was not complemented: %+v", got)
	}
	if got := meta.History[2]; got.YesPercent != 35 || got.NoPercent != 65 {
		t.Fatalf("missing YES percentage was not complemented: %+v", got)
	}
	if got := meta.History[3]; math.Abs(got.YesPercent+got.NoPercent-100) > 1e-9 {
		t.Fatalf("tolerated percentages were not normalized: %+v", got)
	}
}

func TestDownloadMetadataSkipsInvalidHistoryWithoutDiscardingMetadata(t *testing.T) {
	client := NewClient("http://unused.example/ipfs/")
	body := `{
		"desc":"metadata survives",
		"history":[
			{"t":1,"y":51,"n":49},
			{"t":0,"y":50,"n":50},
			{"t":2,"y":-1,"n":101},
			{"t":3,"y":80,"n":10},
			{"t":4,"y":"NaN","n":50},
			{"t":5,"y":1e999,"n":0},
			{"y":50,"n":50},
			{"t":6}
		]
	}`

	meta, err := client.DownloadMetadata("inline-v1:" + hex.EncodeToString([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Desc != "metadata survives" {
		t.Fatalf("static metadata was discarded: %+v", meta)
	}
	if len(meta.History) != 1 || meta.History[0].Time != 1 {
		t.Fatalf("invalid points were not filtered: %+v", meta.History)
	}
}
