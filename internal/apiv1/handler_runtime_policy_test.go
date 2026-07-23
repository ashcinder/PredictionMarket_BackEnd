package apiv1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimePolicyDefaultsToAutomaticResolution(t *testing.T) {
	server := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, "", 0)
	mux := http.NewServeMux()
	server.Register(mux)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
		"/api/v1/gold/runtime-policy", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response runtimePolicyResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.AutoResolveEnabled || response.AllowExpiredMarketCreate {
		t.Fatalf("unexpected default policy: %+v", response)
	}
}

func TestRuntimePolicyDerivesDemoCreationFromDisabledResolution(t *testing.T) {
	server := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, "", 0)
	server.SetRuntimePolicy(false)
	mux := http.NewServeMux()
	server.Register(mux)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
		"/api/v1/gold/runtime-policy", nil))
	var response runtimePolicyResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.AutoResolveEnabled || !response.AllowExpiredMarketCreate ||
		response.DemoDurationSeconds != 1 {
		t.Fatalf("unexpected demo policy: %+v", response)
	}
}
