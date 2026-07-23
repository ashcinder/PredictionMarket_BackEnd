package apiv1

import "net/http"

const immediateExpiryDemoDurationSeconds int64 = 1

type runtimePolicyResponse struct {
	AutoResolveEnabled       bool  `json:"auto_resolve_enabled"`
	AllowExpiredMarketCreate bool  `json:"allow_expired_market_creation"`
	DemoDurationSeconds      int64 `json:"demo_duration_seconds"`
}

func (s *Server) handleRuntimePolicy(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, runtimePolicyResponse{
		AutoResolveEnabled:       s.autoResolveEnabled,
		AllowExpiredMarketCreate: !s.autoResolveEnabled,
		DemoDurationSeconds:      immediateExpiryDemoDurationSeconds,
	})
}
