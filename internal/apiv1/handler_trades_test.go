package apiv1

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Mock implementations for testing trade sync and chain-state write paths
// ---------------------------------------------------------------------------

// mockChainStateRepo implements ChainStateRepository for handler-level tests.
// It captures all upsert calls and can return a pre-set existing state with
// non-zero status values (to detect regressions where zero-value defaults
// mask missing field copies).
type mockChainStateRepo struct {
	existing          *chainStateRow
	poolCalls         []poolCall
	deadlineCalls     []deadlineCall
	fullCalls         []*chainStateRow
	getChainErr       error
	upsertPoolErr     error
	upsertDeadlineErr error
}

type poolCall struct {
	gameID                           int
	totalPool, reserveYes, reserveNo *big.Int
}

type deadlineCall struct {
	gameID      int
	deadlineSec int64
}

func (m *mockChainStateRepo) GetChainState(_ context.Context, _ int) (*chainStateRow, error) {
	return m.existing, m.getChainErr
}

func (m *mockChainStateRepo) ListAllChainStates(_ context.Context) ([]chainStateRow, error) {
	if m.existing != nil {
		return []chainStateRow{*m.existing}, nil
	}
	return nil, nil
}

func (m *mockChainStateRepo) UpsertChainState(_ context.Context, state *chainStateRow) error {
	m.fullCalls = append(m.fullCalls, state)
	return nil
}

func (m *mockChainStateRepo) UpsertChainStatePool(_ context.Context, gameID int, totalPool, reserveYes, reserveNo *big.Int) error {
	m.poolCalls = append(m.poolCalls, poolCall{
		gameID:     gameID,
		totalPool:  cloneBig(totalPool),
		reserveYes: cloneBig(reserveYes),
		reserveNo:  cloneBig(reserveNo),
	})
	return m.upsertPoolErr
}

func (m *mockChainStateRepo) UpsertChainStateDeadline(_ context.Context, gameID int, deadlineSec int64) error {
	m.deadlineCalls = append(m.deadlineCalls, deadlineCall{
		gameID:      gameID,
		deadlineSec: deadlineSec,
	})
	return m.upsertDeadlineErr
}

// mockTradeRepo implements TradeRepository for handler-level tests.
type mockTradeRepo struct {
	records []*tradeRow
	trades  []TradeRecordDTO
}

type mockPositionRepo struct{}

func (*mockPositionRepo) GetUserPosition(context.Context, string, int) (*userPositionRow, error) {
	return nil, nil
}
func (*mockPositionRepo) ListUserPositions(context.Context, string) ([]userPositionRow, error) {
	return nil, nil
}
func (*mockPositionRepo) UpsertUserPosition(context.Context, *userPositionRow) error {
	return nil
}

func (m *mockTradeRepo) RecordTrade(_ context.Context, trade *tradeRow) error {
	m.records = append(m.records, trade)
	return nil
}

func (m *mockTradeRepo) ListTradesByGameAndUser(_ context.Context, _ int, _ string) ([]TradeRecordDTO, error) {
	return m.trades, nil
}

func TestGetTradesReturnsPurchaseTimeSharesAndPositionSnapshots(t *testing.T) {
	tradeMock := &mockTradeRepo{trades: []TradeRecordDTO{{
		TradeType:        "BUY",
		OptionID:         0,
		AmountWei:        "1000000000000000000",
		ShareAmountWei:   "12500000000000000000",
		MySharesYesAfter: "12500000000000000000",
		MySharesNoAfter:  "0",
		IsSuccess:        true,
		IsAiManaged:      false,
		TxHash:           "0xabc",
		CreatedAt:        "2026-06-29 14:30:00",
	}}}
	srv := NewServer(nil, nil, nil, nil, tradeMock, nil, nil, nil, "0xContract", 256)
	mux := http.NewServeMux()
	srv.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/gold/trades?game_id=1&user_address=0x1234567890123456789012345678901234567890", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Trades []TradeHistoryItemDTO `json:"trades"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Trades) != 1 {
		t.Fatalf("expected one trade, got %d", len(response.Trades))
	}
	got := response.Trades[0]
	if got.ShareAmountWei != "12500000000000000000" ||
		got.MySharesYesAfter != "12500000000000000000" ||
		got.MySharesNoAfter != "0" {
		t.Fatalf("unexpected share fields: %+v", got)
	}
	if got.CreatedAt != "2026-06-29 14:30:00" {
		t.Fatalf("expected database purchase time, got %q", got.CreatedAt)
	}
}

func TestSyncTradeStoresPositionDetailFields(t *testing.T) {
	tradeMock := &mockTradeRepo{}
	srv := NewServer(nil, &mockChainStateRepo{}, &mockPositionRepo{}, nil, tradeMock, nil, nil, nil, "0xContract", 256)
	mux := http.NewServeMux()
	srv.Register(mux)

	body := `{
		"game_id":1,
		"contract_address":"0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
		"user_address":"0x1234567890123456789012345678901234567890",
		"trade_type":"BUY",
		"option_id":0,
		"amount_wei":"100",
		"share_amount_wei":"1250",
		"my_shares_yes_after":"1250",
		"my_shares_no_after":"0",
		"tx_hash":"0xabc",
		"is_success":true
	}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gold/trades/sync", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(tradeMock.records) != 1 {
		t.Fatalf("expected one recorded trade, got %d", len(tradeMock.records))
	}
	got := tradeMock.records[0]
	if got.ShareAmountWei != "1250" || got.MySharesYesAfter != "1250" || got.MySharesNoAfter != "0" {
		t.Fatalf("position detail fields not recorded: %+v", got)
	}
}

// mockGameRepo implements GameMetadataRepository for handler-level tests.
type mockGameRepo struct {
	upsertCalls []*gameRow
}

func (m *mockGameRepo) ListAllGames(_ context.Context) ([]GameMetaDTO, error)      { return nil, nil }
func (m *mockGameRepo) GetGameByID(_ context.Context, _ int) (*GameMetaDTO, error) { return nil, nil }
func (m *mockGameRepo) UpsertGame(_ context.Context, game *gameRow) (int, error) {
	m.upsertCalls = append(m.upsertCalls, game)
	return game.GameID, nil
}
func (m *mockGameRepo) InsertGameStub(_ context.Context, _ int, _, _ string) error { return nil }

func cloneBig(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}

// ---------------------------------------------------------------------------
// Tests: /trades/sync — cascade chain-state write preserves deadline/status
// ---------------------------------------------------------------------------

// TestSyncTradePoolOnlyPreservesDeadline verifies that when /trades/sync
// cascades a chain-state write, it uses UpsertChainStatePool (not the full
// UpsertChainState), so that deadline_sec and status fields are NOT
// overwritten with Go zero values.
func TestSyncTradePoolOnlyPreservesDeadline(t *testing.T) {
	chainMock := &mockChainStateRepo{}
	tradeMock := &mockTradeRepo{}

	srv := NewServer(nil, chainMock, nil, nil, tradeMock, nil, nil, nil, "0xContract", 256)
	mux := http.NewServeMux()
	srv.Register(mux)

	body := `{
		"game_id": 1,
		"contract_address": "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
		"user_address": "0x1234567890123456789012345678901234567890",
		"trade_type": "BUY",
		"option_id": 1,
		"amount_wei": "1000000000000000000",
		"tx_hash": "0xabc",
		"is_success": true,
		"total_pool_after": "5000000000000000000",
		"reserve_yes_after": "2000000000000000000",
		"reserve_no_after": "3000000000000000000",
		"timestamp_sec": 1700000000
	}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gold/trades/sync", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	// The handler must call UpsertChainStatePool, not UpsertChainState.
	if len(chainMock.fullCalls) != 0 {
		t.Fatalf("expected 0 full UpsertChainState calls (which would overwrite deadline/status), got %d", len(chainMock.fullCalls))
	}
	if len(chainMock.poolCalls) != 1 {
		t.Fatalf("expected 1 UpsertChainStatePool call, got %d", len(chainMock.poolCalls))
	}

	call := chainMock.poolCalls[0]
	if call.gameID != 1 {
		t.Errorf("expected gameID=1, got %d", call.gameID)
	}
	if call.totalPool.String() != "5000000000000000000" {
		t.Errorf("expected totalPool=5000000000000000000, got %s", call.totalPool)
	}
	if call.reserveYes.String() != "2000000000000000000" {
		t.Errorf("expected reserveYes=2000000000000000000, got %s", call.reserveYes)
	}
	if call.reserveNo.String() != "3000000000000000000" {
		t.Errorf("expected reserveNo=3000000000000000000, got %s", call.reserveNo)
	}
}

// TestSyncTradeNoPoolFieldsSkipsChainState verifies that when the request
// has no post-trade pool fields, the chain-state update is skipped entirely.
func TestSyncTradeNoPoolFieldsSkipsChainState(t *testing.T) {
	chainMock := &mockChainStateRepo{}
	tradeMock := &mockTradeRepo{}

	srv := NewServer(nil, chainMock, nil, nil, tradeMock, nil, nil, nil, "0xContract", 256)
	mux := http.NewServeMux()
	srv.Register(mux)

	body := `{
		"game_id": 1,
		"contract_address": "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
		"user_address": "0x1234567890123456789012345678901234567890",
		"trade_type": "BUY",
		"option_id": 1,
		"amount_wei": "1000000000000000000",
		"tx_hash": "0xabc",
		"is_success": true,
		"timestamp_sec": 1700000000
	}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gold/trades/sync", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(chainMock.fullCalls) != 0 {
		t.Errorf("expected 0 full UpsertChainState calls, got %d", len(chainMock.fullCalls))
	}
	if len(chainMock.poolCalls) != 0 {
		t.Errorf("expected 0 UpsertChainStatePool calls (no pool fields), got %d", len(chainMock.poolCalls))
	}
}

// TestSyncTradePartialPoolFieldsRejected verifies that partial pool fields
// are rejected with 400 BEFORE any DB writes (RecordTrade not called,
// UpsertChainStatePool not called). Per the atomic three-field contract.
func TestSyncTradePartialPoolFieldsRejected(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			"only total_pool_after",
			`{"game_id":1,"contract_address":"0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c","user_address":"0x1234567890123456789012345678901234567890","trade_type":"BUY","option_id":1,"amount_wei":"1","tx_hash":"0xabc","is_success":true,"total_pool_after":"100","timestamp_sec":1}`,
		},
		{
			"only reserve_yes_after",
			`{"game_id":1,"contract_address":"0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c","user_address":"0x1234567890123456789012345678901234567890","trade_type":"BUY","option_id":1,"amount_wei":"1","tx_hash":"0xabc","is_success":true,"reserve_yes_after":"100","timestamp_sec":1}`,
		},
		{
			"missing reserve_no_after",
			`{"game_id":1,"contract_address":"0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c","user_address":"0x1234567890123456789012345678901234567890","trade_type":"BUY","option_id":1,"amount_wei":"1","tx_hash":"0xabc","is_success":true,"total_pool_after":"100","reserve_yes_after":"200","timestamp_sec":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chainMock := &mockChainStateRepo{}
			tradeMock := &mockTradeRepo{}

			srv := NewServer(nil, chainMock, nil, nil, tradeMock, nil, nil, nil, "0xContract", 256)
			mux := http.NewServeMux()
			srv.Register(mux)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/gold/trades/sync", strings.NewReader(tt.body))
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for partial pool fields, got %d body=%s", rec.Code, rec.Body.String())
			}
			// Must not call any chain-state upsert.
			if len(chainMock.poolCalls) != 0 {
				t.Errorf("expected 0 UpsertChainStatePool calls, got %d", len(chainMock.poolCalls))
			}
			if len(chainMock.fullCalls) != 0 {
				t.Errorf("expected 0 full UpsertChainState calls, got %d", len(chainMock.fullCalls))
			}
			// Must not record any trade — rejection must happen before DB writes.
			if len(tradeMock.records) != 0 {
				t.Errorf("expected 0 RecordTrade calls (rejection before DB write), got %d", len(tradeMock.records))
			}
		})
	}
}

// TestSyncTradeIllegalPoolValuesRejected verifies that non-parseable pool
// field values (e.g. "abc", "1.2") are rejected with 400 before any DB
// writes. This guards against pool fields that pass the non-empty check
// but fail decimal parse, which would otherwise produce SQL NULL.
func TestSyncTradeIllegalPoolValuesRejected(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value string
	}{
		{"non-numeric total_pool_after", "total_pool_after", "abc"},
		{"float total_pool_after", "total_pool_after", "1.5"},
		{"non-numeric reserve_yes_after", "reserve_yes_after", "xyz"},
		{"hex reserve_no_after", "reserve_no_after", "0x1a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chainMock := &mockChainStateRepo{}
			tradeMock := &mockTradeRepo{}

			srv := NewServer(nil, chainMock, nil, nil, tradeMock, nil, nil, nil, "0xContract", 256)
			mux := http.NewServeMux()
			srv.Register(mux)

			// Build a valid 3-field body, then replace one field with an illegal value.
			tmpl := `{"game_id":1,"contract_address":"0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c","user_address":"0x1234567890123456789012345678901234567890","trade_type":"BUY","option_id":1,"amount_wei":"1","tx_hash":"0xabc","is_success":true,"total_pool_after":"100","reserve_yes_after":"200","reserve_no_after":"300","timestamp_sec":1}`
			var body string
			switch tt.field {
			case "total_pool_after":
				body = strings.Replace(tmpl, `"total_pool_after":"100"`, `"total_pool_after":"`+tt.value+`"`, 1)
			case "reserve_yes_after":
				body = strings.Replace(tmpl, `"reserve_yes_after":"200"`, `"reserve_yes_after":"`+tt.value+`"`, 1)
			case "reserve_no_after":
				body = strings.Replace(tmpl, `"reserve_no_after":"300"`, `"reserve_no_after":"`+tt.value+`"`, 1)
			}

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/gold/trades/sync", strings.NewReader(body))
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for illegal pool value, got %d body=%s", rec.Code, rec.Body.String())
			}
			// Zero DB side effects.
			if len(tradeMock.records) != 0 {
				t.Errorf("expected 0 RecordTrade calls, got %d", len(tradeMock.records))
			}
			// Must not call any chain-state upsert.
			if len(chainMock.poolCalls) != 0 {
				t.Errorf("expected 0 UpsertChainStatePool calls, got %d", len(chainMock.poolCalls))
			}
			if len(chainMock.fullCalls) != 0 {
				t.Errorf("expected 0 full UpsertChainState calls, got %d", len(chainMock.fullCalls))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SQL contract tests — verify UPDATE clauses don't touch protected columns
// ---------------------------------------------------------------------------

// TestUpsertChainStatePoolSQLContract verifies the pool SQL UPDATE clause
// does not reference deadline_sec or status columns.
func TestUpsertChainStatePoolSQLContract(t *testing.T) {
	sql := upsertChainStatePoolSQL
	if !strings.Contains(sql, "INSERT INTO") {
		t.Error("pool SQL missing INSERT INTO")
	}

	updateClause := sql[strings.Index(sql, "ON DUPLICATE KEY UPDATE"):]
	forbidden := []string{"is_resolved", "is_refunded", "winning_option", "deadline_sec"}
	for _, field := range forbidden {
		if strings.Contains(updateClause, field) {
			t.Errorf("pool SQL UPDATE clause must not contain %q (it would overwrite existing values)", field)
		}
	}
	required := []string{"total_pool", "reserve_yes", "reserve_no"}
	for _, field := range required {
		if !strings.Contains(updateClause, field) {
			t.Errorf("pool SQL UPDATE clause must contain %q", field)
		}
	}
}

// TestUpsertChainStateDeadlineSQLContract verifies the deadline SQL UPDATE
// clause only touches deadline_sec and does not reference pool or status columns.
func TestUpsertChainStateDeadlineSQLContract(t *testing.T) {
	sql := upsertChainStateDeadlineSQL
	if !strings.Contains(sql, "INSERT INTO") {
		t.Error("deadline SQL missing INSERT INTO")
	}

	updateClause := sql[strings.Index(sql, "ON DUPLICATE KEY UPDATE"):]

	// Must touch deadline_sec.
	if !strings.Contains(updateClause, "deadline_sec=VALUES(deadline_sec)") {
		t.Error("deadline SQL UPDATE clause must set deadline_sec")
	}

	// Must NOT touch pool or status columns.
	forbidden := []string{"total_pool", "reserve_yes", "reserve_no", "is_resolved", "is_refunded", "winning_option"}
	for _, field := range forbidden {
		if strings.Contains(updateClause, field) {
			t.Errorf("deadline SQL UPDATE clause must not contain %q (it would overwrite existing values)", field)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: /games/sync — deadline-only sparse upsert preserves all other fields
// ---------------------------------------------------------------------------

// TestSyncGameDeadlineUsesSparseUpsert verifies that /games/sync uses
// UpsertChainStateDeadline (sparse) instead of a full UpsertChainState
// for deadline-only writes. This eliminates the read-merge-write race
// and guarantees pool/status fields are not overwritten even if the
// existing state has non-zero status values.
func TestSyncGameDeadlineUsesSparseUpsert(t *testing.T) {
	// Use non-zero status values — if the code accidentally does a full
	// upsert with zero defaults, these assertions will catch it.
	chainMock := &mockChainStateRepo{
		existing: &chainStateRow{
			GameID:        1,
			TotalPool:     big.NewInt(5000),
			IsResolved:    true, // non-zero — must survive
			IsRefunded:    true, // non-zero — must survive
			WinningOption: 1,    // non-zero — must survive
			DeadlineSec:   1700000000,
			ReserveYes:    big.NewInt(2000),
			ReserveNo:     big.NewInt(3000),
		},
	}
	gamesMock := &mockGameRepo{}

	srv := NewServer(gamesMock, chainMock, nil, nil, nil, nil, nil, nil, "0xContract", 256)
	mux := http.NewServeMux()
	srv.Register(mux)

	body := `{
		"game_id": 1,
		"contract_address": "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
		"ipfs_cid": "QmTest",
		"deadline_sec": 1800000000
	}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gold/games/sync", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Must use UpsertChainStateDeadline (sparse), NOT full UpsertChainState.
	if len(chainMock.fullCalls) != 0 {
		t.Fatalf("expected 0 full UpsertChainState calls (sparse deadline upsert should be used instead), got %d", len(chainMock.fullCalls))
	}
	if len(chainMock.deadlineCalls) != 1 {
		t.Fatalf("expected 1 UpsertChainStateDeadline call, got %d", len(chainMock.deadlineCalls))
	}

	dc := chainMock.deadlineCalls[0]
	if dc.gameID != 1 {
		t.Errorf("expected gameID=1, got %d", dc.gameID)
	}
	if dc.deadlineSec != 1800000000 {
		t.Errorf("expected deadlineSec=1800000000, got %d", dc.deadlineSec)
	}
}

// TestSyncGameNoDeadlineSkipsChainState verifies that when deadline_sec is
// not provided in /games/sync, no chain-state upsert is performed at all.
func TestSyncGameNoDeadlineSkipsChainState(t *testing.T) {
	chainMock := &mockChainStateRepo{}
	gamesMock := &mockGameRepo{}

	srv := NewServer(gamesMock, chainMock, nil, nil, nil, nil, nil, nil, "0xContract", 256)
	mux := http.NewServeMux()
	srv.Register(mux)

	body := `{
		"game_id": 1,
		"contract_address": "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
		"ipfs_cid": "QmTest"
	}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gold/games/sync", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(chainMock.fullCalls) != 0 {
		t.Errorf("expected 0 full UpsertChainState calls, got %d", len(chainMock.fullCalls))
	}
	if len(chainMock.deadlineCalls) != 0 {
		t.Errorf("expected 0 UpsertChainStateDeadline calls, got %d", len(chainMock.deadlineCalls))
	}
}

// ---------------------------------------------------------------------------
// request validation
// ---------------------------------------------------------------------------

// TestSyncTradeInvalidGameID verifies game_id validation.
func TestSyncTradeInvalidGameID(t *testing.T) {
	chainMock := &mockChainStateRepo{}
	tradeMock := &mockTradeRepo{}

	srv := NewServer(nil, chainMock, nil, nil, tradeMock, nil, nil, nil, "0xContract", 256)
	mux := http.NewServeMux()
	srv.Register(mux)

	tests := []struct {
		name string
		body string
	}{
		{"zero game_id", `{"game_id":0,"contract_address":"0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c","user_address":"0x1234567890123456789012345678901234567890","trade_type":"BUY","option_id":1,"amount_wei":"1","tx_hash":"0xabc","is_success":true}`},
		{"negative game_id", `{"game_id":-1,"contract_address":"0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c","user_address":"0x1234567890123456789012345678901234567890","trade_type":"BUY","option_id":1,"amount_wei":"1","tx_hash":"0xabc","is_success":true}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/gold/trades/sync", strings.NewReader(tt.body))
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestSyncTradeInvalidBody verifies JSON parse errors return 400.
func TestSyncTradeInvalidBody(t *testing.T) {
	chainMock := &mockChainStateRepo{}
	tradeMock := &mockTradeRepo{}

	srv := NewServer(nil, chainMock, nil, nil, tradeMock, nil, nil, nil, "0xContract", 256)
	mux := http.NewServeMux()
	srv.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gold/trades/sync", bytes.NewReader([]byte("not json")))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestSyncTradeCreatedAtDocumentsFormat verifies timestamp_sec is parsed
// correctly (documents the existing created_at format — secondary scope).
func TestSyncTradeCreatedAtDocumentsFormat(t *testing.T) {
	body := `{
		"game_id": 1,
		"contract_address": "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
		"user_address": "0x1234567890123456789012345678901234567890",
		"trade_type": "BUY",
		"option_id": 1,
		"amount_wei": "1000000000000000000",
		"tx_hash": "0xabc",
		"is_success": true,
		"timestamp_sec": 1700000000
	}`

	var req SyncTradeRequest
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.TimestampSec != 1700000000 {
		t.Errorf("expected timestamp_sec=1700000000, got %d", req.TimestampSec)
	}
	// The created_at format in handleGetTrades is "2006-01-02 15:04:05" UTC
	// without timezone suffix — secondary scope, documented here for awareness.
}
