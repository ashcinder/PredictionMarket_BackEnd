package scenario

import (
	"strings"
	"testing"
	"time"
)

func TestAllCurrentProjectMarketTypesAreSupported(t *testing.T) {
	want := []string{
		TypePrice,
		TypeVolatility,
		TypeVolume,
		TypeTechnical,
		TypeTouch,
		TypeRelative,
		TypePriceThreshold,
		TypeEvent,
	}
	if len(SupportedTypes()) != len(want) {
		t.Fatalf("SupportedTypes length = %d, want %d", len(SupportedTypes()), len(want))
	}
	for _, typ := range want {
		if _, ok := TemplateForType(typ); !ok {
			t.Fatalf("TemplateForType(%q) not supported", typ)
		}
	}
}

func TestBuildMarketProducesTypedMetadata(t *testing.T) {
	market, err := BuildMarket(BuildMarketInput{
		Type:         TypePriceThreshold,
		Index:        3,
		Creator:      "0x0000000000000000000000000000000000000001",
		Duration:     2 * time.Hour,
		InitialBKC:   "4.5",
		Now:          time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		TemplateSeed: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if market.Type != TypePriceThreshold {
		t.Fatalf("market type = %q", market.Type)
	}
	if !strings.Contains(market.Desc, "USD") || !strings.Contains(market.Condition, "USD") {
		t.Fatalf("price threshold metadata did not include USD: %+v", market)
	}
	if market.OptionYes == "" || market.OptionNo == "" || market.CreatorAddress == "" {
		t.Fatalf("missing display metadata: %+v", market)
	}
	if !strings.Contains(market.MetadataJSON, `"type":"TYPE_PRICE_THRESHOLD"`) {
		t.Fatalf("metadata JSON missing type: %s", market.MetadataJSON)
	}
	if !strings.HasPrefix(market.IPFSCID, "sim-") {
		t.Fatalf("simulated CID = %q, want sim-*", market.IPFSCID)
	}
}
