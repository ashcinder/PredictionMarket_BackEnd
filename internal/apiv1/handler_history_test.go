package apiv1

import "testing"

func TestPrepareChartHistoryFiltersAndBucketsRequestedRange(t *testing.T) {
	points := make([]PricePointDTO, 0, 181)
	for i := int64(0); i <= 180; i++ {
		points = append(points, PricePointDTO{
			GameID:       7,
			TimestampSec: 1_700_000_000 + i*60,
			YesPrice:     float64(i),
			NoPrice:      100 - float64(i),
		})
	}

	got := prepareChartHistory(points, parseChartRange("1h", 256))
	if len(got) < 55 || len(got) > 63 {
		t.Fatalf("expected about one point per minute for 1h, got %d", len(got))
	}
	if got[0].TimestampSec > points[len(points)-1].TimestampSec-3600 {
		t.Fatalf("expected boundary context point, first timestamp=%d", got[0].TimestampSec)
	}
	if got[len(got)-1].TimestampSec != points[len(points)-1].TimestampSec {
		t.Fatalf("expected latest point to be preserved")
	}
}

func TestPrepareChartHistoryAllDownsamplesLargeSeries(t *testing.T) {
	points := make([]PricePointDTO, 0, 1000)
	for i := int64(0); i < 1000; i++ {
		points = append(points, PricePointDTO{TimestampSec: 1_700_000_000 + i*60})
	}
	got := prepareChartHistory(points, parseChartRange("all", 256))
	if len(got) != 240 {
		t.Fatalf("expected all range to be downsampled to 240 mobile points, got %d", len(got))
	}
	if got[0].TimestampSec != points[0].TimestampSec ||
		got[len(got)-1].TimestampSec != points[len(points)-1].TimestampSec {
		t.Fatalf("expected first and last points to be preserved")
	}
}

func TestLargestTriangleSamplingPreservesShortLivedMove(t *testing.T) {
	points := make([]PricePointDTO, 500)
	for i := range points {
		points[i] = PricePointDTO{TimestampSec: 1_700_000_000 + int64(i*60), YesPrice: 50, NoPrice: 50}
	}
	points[251].YesPrice = 96
	points[251].NoPrice = 4

	got := largestTriangleThreeBuckets(points, 80)
	foundSpike := false
	for _, point := range got {
		if point.TimestampSec == points[251].TimestampSec && point.YesPrice == 96 {
			foundSpike = true
			break
		}
	}
	if !foundSpike {
		t.Fatal("visually significant share spike was lost during downsampling")
	}
}
