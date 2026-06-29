package apiv1

import (
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// parsePositiveInt parses a decimal string as a positive int.
func parsePositiveInt(raw string) (int, bool) {
	n, ok := new(big.Int).SetString(strings.TrimSpace(raw), 10)
	if !ok || !n.IsInt64() || n.Sign() <= 0 {
		return 0, false
	}
	return int(n.Int64()), true
}

// parsePositiveIntFromPath extracts and validates {id} from a Go 1.22+
// wildcard path value.
func parsePositiveIntFromPath(r *http.Request, name string) (int, bool) {
	raw := r.PathValue(name)
	if raw == "" {
		return 0, false
	}
	return parsePositiveInt(raw)
}

// parsePositiveIntFromQuery extracts and validates an integer query parameter.
func parsePositiveIntFromQuery(r *http.Request, name string) (int, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, false
	}
	return parsePositiveInt(raw)
}

// firstNonEmpty returns the first non-empty string. Returns "" if all are empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// parseBigIntFromDB parses a VARBINARY-stored decimal string back to *big.Int.
func parseBigIntFromDB(data []byte) (*big.Int, error) {
	if len(data) == 0 {
		return nil, nil
	}
	v, ok := new(big.Int).SetString(string(data), 10)
	if !ok {
		return nil, nil
	}
	return v, nil
}

// bigIntToDBBytes converts a *big.Int to a decimal string for VARBINARY storage.
func bigIntToDBBytes(v *big.Int) []byte {
	if v == nil {
		return nil
	}
	return []byte(v.String())
}

// bigIntOrZero returns the decimal string of v, or "0" if v is nil.
func bigIntOrZero(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}

// bigIntStrOrZero returns s if non-empty, otherwise "0".
func bigIntStrOrZero(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "0"
	}
	return s
}

// parseBigIntStr parses a decimal string into *big.Int. Returns nil on empty
// or invalid input.
func parseBigIntStr(s string) *big.Int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil
	}
	return v
}

// parseBigIntStrChecked parses a non-empty decimal string into *big.Int.
// Returns (nil, false) on empty or invalid input — used for pre-validating
// pool fields before any DB writes so that illegal values are rejected with
// a 400 before RecordTrade or UpsertChainState are called.
func parseBigIntStrChecked(s string) (*big.Int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, false
	}
	return v, true
}

// normalizeAddress returns a checksum-normalised lowercase hex address.
func normalizeAddress(value string) string {
	return strings.ToLower(common.HexToAddress(value).Hex())
}

// logRequest logs an incoming HTTP API request.
func logRequest(r *http.Request) {
	slog.Info("api request",
		"method", r.Method,
		"path", r.URL.Path,
		"query", r.URL.RawQuery,
		"remote", r.RemoteAddr,
	)
}

// setCORS sets permissive CORS headers and handles OPTIONS preflight.
// Returns true when the request was an OPTIONS preflight (caller should
// return immediately).
func setCORS(w http.ResponseWriter, r *http.Request, methods string) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", methods)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}
