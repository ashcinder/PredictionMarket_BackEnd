package oracle

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGoldOracleUsesConfiguredEndpointsAndHeaders(t *testing.T) {
	var goldHit bool
	var sinaHit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gold":
			goldHit = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"price":2300,"updatedAt":"test"}`))
		case "/sina":
			sinaHit = true
			if r.Header.Get("Referer") != "https://configured.example/referer" {
				t.Errorf("unexpected Referer: %q", r.Header.Get("Referer"))
			}
			if r.Header.Get("User-Agent") != "configured-agent" {
				t.Errorf("unexpected User-Agent: %q", r.Header.Get("User-Agent"))
			}
			_, _ = w.Write([]byte(`var hq_str_hf_XAU="2200,2100";`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oracle := NewGoldOracle(Config{
		GoldAPIURL:     server.URL + "/gold",
		SinaURL:        server.URL + "/sina",
		SinaReferer:    "https://configured.example/referer",
		UserAgent:      "configured-agent",
		RequestTimeout: time.Second,
	})
	quote, err := oracle.fetchGoldAPI()
	if err != nil {
		t.Fatal(err)
	}
	if quote.PriceUSD != 2300 || !goldHit || !sinaHit {
		t.Fatalf("configured endpoints were not used: quote=%+v gold=%v sina=%v", quote, goldHit, sinaHit)
	}
}
