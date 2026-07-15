package ipfs

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDownloadMetadataFallsBackFromLocalContentGatewayToKuboGateway(t *testing.T) {
	client := NewClient("http://backend.local/api/v1/ipfs/", []string{"http://kubo.local/ipfs/"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "http://backend.local/api/v1/ipfs/QmWi2JmtA2T5vNU6SGkhdCm7h7EmH31v45CqKJfrxp6WDf":
			return stringResponse(http.StatusBadRequest, "invalid cid"), nil
		case "http://kubo.local/ipfs/QmWi2JmtA2T5vNU6SGkhdCm7h7EmH31v45CqKJfrxp6WDf":
			return stringResponse(http.StatusOK, `{"type":"TYPE_RELATIVE","desc":"gold market","condition":"gold closes above 2500","optionYES":"YES","optionNO":"NO","resolutionRule":{"type":"TYPE_RELATIVE","symbol":"XAU","benchmark":"BTC","source":"GOLD_API","start_time_sec":10,"end_time_sec":20}}`), nil
		default:
			t.Fatalf("unexpected request URL: %s", req.URL.String())
			return nil, nil
		}
	})}

	meta, err := client.DownloadMetadata("QmWi2JmtA2T5vNU6SGkhdCm7h7EmH31v45CqKJfrxp6WDf")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Condition != "gold closes above 2500" {
		t.Fatalf("fallback metadata was not returned: %+v", meta)
	}
	if meta.Type != "TYPE_RELATIVE" || len(meta.ResolutionRule) == 0 {
		t.Fatalf("structured settlement metadata was lost: %+v", meta)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func stringResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
