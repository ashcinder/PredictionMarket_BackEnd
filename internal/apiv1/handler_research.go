package apiv1

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

const (
	maxResearchRequestBytes      = 64 << 10
	maxResearchSystemPromptBytes = 12 << 10
	maxResearchUserMessageBytes  = 40 << 10
)

type researchRequest struct {
	SystemPrompt string `json:"system_prompt"`
	UserMessage  string `json:"user_message"`
}

type researchResponse struct {
	Content string `json:"content"`
}

func (s *Server) handleResearch(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "POST,OPTIONS") {
		return
	}
	logRequest(r)
	if s.research == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "AI research service is unavailable")
		return
	}

	var req researchRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxResearchRequestBytes))
	if err := decoder.Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	req.SystemPrompt = strings.TrimSpace(req.SystemPrompt)
	req.UserMessage = strings.TrimSpace(req.UserMessage)
	if req.UserMessage == "" {
		writeJSONError(w, http.StatusBadRequest, "user_message is required")
		return
	}
	if len(req.SystemPrompt) > maxResearchSystemPromptBytes || len(req.UserMessage) > maxResearchUserMessageBytes {
		writeJSONError(w, http.StatusBadRequest, "research prompt is too large")
		return
	}

	content, err := s.research.Research(r.Context(), req.SystemPrompt, req.UserMessage)
	if err != nil {
		slog.Warn("apiv1: AI research upstream failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "AI research is temporarily unavailable")
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		writeJSONError(w, http.StatusBadGateway, "AI research returned an empty response")
		return
	}
	writeJSON(w, http.StatusOK, researchResponse{Content: content})
}
