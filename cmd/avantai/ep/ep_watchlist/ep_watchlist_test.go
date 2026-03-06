package main

// import (
// 	"encoding/csv"
// 	"fmt"
// 	"math"
// 	"os"
// 	"path/filepath"
// 	"strconv"
// 	"strings"
// 	"sync"
// 	"sync/atomic"
// 	"testing"
// 	"time"
// )

// // ─────────────────────────────────────────────────────────────────────────────
// // Test infrastructure
// //
// // evaluatePosition uses two injectable function variables defined in
// // realtime_monitor.go (added via injectable_functions.go):
// //
// //   var getAlpacaPositionFn  — wraps alpacaClient.GetPosition
// //   var getIntradayBarsFn    — wraps mdClient.GetBars
// //   var openPositionFn       — wraps tryOpenPosition (used by processWatchlist)
// //
// // Tests patch these vars via stubDeps() / stubOpenPosition() and restore them
// // in t.Cleanup / defer, so no real network calls are ever made.
// // ─────────────────────────────────────────────────────────────────────────────

// // ── Fake bar type used by tests ───────────────────────────────────────────────

// type fakeBar struct {
// 	Open, High, Low, Close float64
// }

// // ── Test helpers ──────────────────────────────────────────────────────────────

// func setupEasternLoc(t *testing.T) {
// 	t.Helper()
// 	loc, err := time.LoadLocation(EASTERN_TZ)
// 	if err != nil {
// 		t.Fatalf("Cannot load timezone: %v", err)
// 	}
// 	easternLoc = loc
// }

// // newPos returns a clean position for test use.
// func newPos(symbol string, entry, stop, shares float64) *RealtimePosition {
// 	risk := entry - stop
// 	now := time.Now()
// 	return &RealtimePosition{
// 		Symbol:        symbol,
// 		EntryPrice:    entry,
// 		StopLoss:      stop,
// 		InitialStopLoss: stop,
// 		InitialRisk:   risk,
// 		Shares:        shares,
// 		InitialShares: shares,
// 		HighestPrice:  entry,
// 		PurchaseDate:  now,
// 		LastCheckDate: now,
// 		SessionLow:    math.MaxFloat64,
// 	}
// }

// // tmpWatchlist writes rows to a temp CSV and returns the path.
// func tmpWatchlist(t *testing.T, rows [][]string) string {
// 	t.Helper()
// 	dir := t.TempDir()
// 	path := filepath.Join(dir, "watchlist.csv")
// 	f, err := os.Create(path)
// 	if err != nil {
// 		t.Fatalf("create watchlist: %v", err)
// 	}
// 	w := csv.NewWriter(f)
// 	_ = w.WriteAll(rows)
// 	f.Close()
// 	return path
// }

// // tmpTradeResults creates a fresh trade_results.csv in the test's working dir
// // and returns a cleanup func that deletes it.
// func tmpTradeResults(t *testing.T) func() {
// 	t.Helper()
// 	dir := t.TempDir()
// 	old, _ := os.Getwd()
// 	if err := os.Chdir(dir); err != nil {
// 		t.Fatalf("chdir: %v", err)
// 	}
// 	initTradeResultsFile()
// 	return func() { _ = os.Chdir(old) }
// }

// // readTradeCSV reads all data rows (skips header) from trade_results.csv.
// func readTradeCSV(t *testing.T) [][]string {
// 	t.Helper()
// 	f, err := os.Open("trade_results.csv")
// 	if err != nil {
// 		t.Fatalf("open trade_results.csv: %v", err)
// 	}
// 	defer f.Close()
// 	rows, err := csv.NewReader(f).ReadAll()
// 	if err != nil {
// 		t.Fatalf("read trade_results.csv: %v", err)
// 	}
// 	if len(rows) <= 1 {
// 		return nil
// 	}
// 	return rows[1:]
// }

// // resetGlobals clears global state between tests.
// func resetGlobals() {
// 	positionsMu.Lock()
// 	activePositions = make(map[string]*RealtimePosition)
// 	positionsMu.Unlock()

// 	processedMu.Lock()
// 	processedSymbols = make(map[string]bool)
// 	processedMu.Unlock()

// 	lastWatchlistMod = time.Time{}
// 	watchlistPath = ""
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 1. parsePosition
// // ─────────────────────────────────────────────────────────────────────────────

// func TestParsePosition_ValidRow(t *testing.T) {
// 	row := []string{"AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"}
// 	pos := parsePosition(row)
// 	if pos == nil {
// 		t.Fatal("expected non-nil position")
// 	}
// 	if pos.Symbol != "AAPL" {
// 		t.Errorf("symbol: got %q want %q", pos.Symbol, "AAPL")
// 	}
// 	if pos.EntryPrice != 150.00 {
// 		t.Errorf("entry: got %.2f want 150.00", pos.EntryPrice)
// 	}
// 	if pos.StopLoss != 140.00 {
// 		t.Errorf("stop: got %.2f want 140.00", pos.StopLoss)
// 	}
// 	if pos.Shares != 100 {
// 		t.Errorf("shares: got %.0f want 100", pos.Shares)
// 	}
// 	if pos.InitialRisk != 10.00 {
// 		t.Errorf("initialRisk: got %.2f want 10.00", pos.InitialRisk)
// 	}
// 	if pos.PurchaseDate.Format("2006-01-02") != "2024-01-15" {
// 		t.Errorf("date: got %s", pos.PurchaseDate.Format("2006-01-02"))
// 	}
// 	// InitialShares should match Shares
// 	if pos.InitialShares != pos.Shares {
// 		t.Errorf("InitialShares %.0f != Shares %.0f", pos.InitialShares, pos.Shares)
// 	}
// 	// HighestPrice should be seeded to entry
// 	if pos.HighestPrice != pos.EntryPrice {
// 		t.Errorf("HighestPrice %.2f != EntryPrice %.2f", pos.HighestPrice, pos.EntryPrice)
// 	}
// }

// func TestParsePosition_DollarSignStripped(t *testing.T) {
// 	row := []string{"TSLA", "$200.00", "$180.00", "50", "$20.00", "2024-03-01"}
// 	pos := parsePosition(row)
// 	if pos == nil {
// 		t.Fatal("expected non-nil position")
// 	}
// 	if pos.EntryPrice != 200.00 {
// 		t.Errorf("entry with $ prefix: got %.2f", pos.EntryPrice)
// 	}
// }

// func TestParsePosition_TooFewColumns(t *testing.T) {
// 	row := []string{"AAPL", "150.00", "140.00"}
// 	if pos := parsePosition(row); pos != nil {
// 		t.Error("expected nil for row with too few columns")
// 	}
// }

// func TestParsePosition_InvalidNumbers(t *testing.T) {
// 	row := []string{"AAPL", "not-a-number", "140.00", "100", "10.00", "2024-01-15"}
// 	if pos := parsePosition(row); pos != nil {
// 		t.Error("expected nil for row with invalid number")
// 	}
// }

// func TestParsePosition_InvalidDate(t *testing.T) {
// 	row := []string{"AAPL", "150.00", "140.00", "100", "10.00", "01/15/2024"}
// 	if pos := parsePosition(row); pos != nil {
// 		t.Error("expected nil for row with bad date format")
// 	}
// }

// func TestParsePosition_WhitespaceHandled(t *testing.T) {
// 	row := []string{"  NVDA  ", " 500.00 ", " 480.00 ", " 20 ", " 20.00 ", "2024-06-10"}
// 	pos := parsePosition(row)
// 	if pos == nil {
// 		t.Fatal("expected non-nil position")
// 	}
// 	if pos.Symbol != "NVDA" {
// 		t.Errorf("symbol not trimmed: %q", pos.Symbol)
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 2. Market hours
// // ─────────────────────────────────────────────────────────────────────────────

// func TestIsMarketOpen_Weekday_During_Session(t *testing.T) {
// 	setupEasternLoc(t)
// 	// Pick a known weekday at 10:00 ET
// 	loc := easternLoc
// 	// Monday 2024-01-08 10:00 ET
// 	ts := time.Date(2024, 1, 8, 10, 0, 0, 0, loc)
// 	// We can't override time.Now() without the function variable pattern,
// 	// so we test the logic directly by reproducing it.
// 	wd := ts.Weekday()
// 	if wd == time.Saturday || wd == time.Sunday {
// 		t.Skip("Unexpected weekend")
// 	}
// 	open := time.Date(ts.Year(), ts.Month(), ts.Day(), MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, loc)
// 	close_ := time.Date(ts.Year(), ts.Month(), ts.Day(), MARKET_CLOSE_HOUR, MARKET_CLOSE_MIN, 0, 0, loc)
// 	if !(ts.After(open) && ts.Before(close_)) {
// 		t.Error("10:00 ET weekday should be inside session")
// 	}
// }

// func TestIsMarketOpen_Weekend(t *testing.T) {
// 	setupEasternLoc(t)
// 	loc := easternLoc
// 	// Saturday 2024-01-06
// 	ts := time.Date(2024, 1, 6, 11, 0, 0, 0, loc)
// 	if ts.Weekday() != time.Saturday {
// 		t.Skip("date is not Saturday")
// 	}
// 	// Weekend check — must be closed
// 	if ts.Weekday() == time.Saturday || ts.Weekday() == time.Sunday {
// 		// correct — closed
// 	} else {
// 		t.Error("Saturday should be closed")
// 	}
// }

// func TestIsMarketOpen_BeforeOpen(t *testing.T) {
// 	setupEasternLoc(t)
// 	loc := easternLoc
// 	ts := time.Date(2024, 1, 8, 9, 0, 0, 0, loc) // 09:00, before 09:30
// 	open := time.Date(ts.Year(), ts.Month(), ts.Day(), MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, loc)
// 	if ts.After(open) {
// 		t.Error("09:00 should be before market open")
// 	}
// }

// func TestIsMarketOpen_AfterClose(t *testing.T) {
// 	setupEasternLoc(t)
// 	loc := easternLoc
// 	ts := time.Date(2024, 1, 8, 16, 30, 0, 0, loc) // 16:30, after 16:00
// 	close_ := time.Date(ts.Year(), ts.Month(), ts.Day(), MARKET_CLOSE_HOUR, MARKET_CLOSE_MIN, 0, 0, loc)
// 	if ts.Before(close_) {
// 		t.Error("16:30 should be after market close")
// 	}
// }

// func TestNextMarketOpen_ReturnsWeekday(t *testing.T) {
// 	setupEasternLoc(t)
// 	next := nextMarketOpen()
// 	if next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
// 		t.Errorf("nextMarketOpen returned a weekend day: %s", next.Weekday())
// 	}
// 	if next.Hour() != MARKET_OPEN_HOUR || next.Minute() != MARKET_OPEN_MIN {
// 		t.Errorf("nextMarketOpen time wrong: %02d:%02d", next.Hour(), next.Minute())
// 	}
// }

// func TestNextMarketOpen_IsInFuture(t *testing.T) {
// 	setupEasternLoc(t)
// 	next := nextMarketOpen()
// 	// next should always be after the last call to isMarketOpen
// 	// (it might be today if we're before open on a weekday, or tomorrow otherwise)
// 	if next.IsZero() {
// 		t.Error("nextMarketOpen returned zero time")
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 3. tryOpenPosition — validation rules (no network calls needed)
// // ─────────────────────────────────────────────────────────────────────────────

// func TestTryOpenPosition_PriceTooLow(t *testing.T) {
// 	resetGlobals()
// 	pos := newPos("PENNY", 1.50, 1.00, 100) // below MIN_PRICE=2.0
// 	tryOpenPosition(pos)

// 	positionsMu.RLock()
// 	_, registered := activePositions["PENNY"]
// 	positionsMu.RUnlock()

// 	if registered {
// 		t.Error("position below MIN_PRICE should not be registered")
// 	}
// }

// func TestTryOpenPosition_PriceTooHigh(t *testing.T) {
// 	resetGlobals()
// 	pos := newPos("BIGSTOCK", 300.00, 280.00, 10) // above MAX_PRICE=200
// 	tryOpenPosition(pos)

// 	positionsMu.RLock()
// 	_, registered := activePositions["BIGSTOCK"]
// 	positionsMu.RUnlock()

// 	if registered {
// 		t.Error("position above MAX_PRICE should not be registered")
// 	}
// }

// func TestTryOpenPosition_StopAboveEntry(t *testing.T) {
// 	resetGlobals()
// 	pos := newPos("BAD", 100.00, 110.00, 50) // stop > entry — invalid
// 	tryOpenPosition(pos)

// 	positionsMu.RLock()
// 	_, registered := activePositions["BAD"]
// 	positionsMu.RUnlock()

// 	if registered {
// 		t.Error("position with stop > entry should not be registered")
// 	}
// }

// func TestTryOpenPosition_ZeroStop(t *testing.T) {
// 	resetGlobals()
// 	pos := newPos("ZS", 50.00, 0.00, 100)
// 	tryOpenPosition(pos)

// 	positionsMu.RLock()
// 	_, registered := activePositions["ZS"]
// 	positionsMu.RUnlock()

// 	if registered {
// 		t.Error("position with zero stop should not be registered")
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 4. executeStopOut
// // ─────────────────────────────────────────────────────────────────────────────

// func TestExecuteStopOut_RecordsTrade_FullLoss(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	pos := newPos("TSLA", 100.00, 90.00, 100)
// 	now := time.Now()

// 	result := executeStopOut(pos, 90.00, now)

// 	if !result {
// 		t.Error("executeStopOut should return true (position closed)")
// 	}
// 	if pos.Shares != 0 {
// 		t.Errorf("shares should be 0 after stop, got %.0f", pos.Shares)
// 	}

// 	rows := readTradeCSV(t)
// 	if len(rows) != 1 {
// 		t.Fatalf("expected 1 trade row, got %d", len(rows))
// 	}
// 	row := rows[0]
// 	if row[0] != "TSLA" {
// 		t.Errorf("symbol: %s", row[0])
// 	}
// 	if row[10] != "false" {
// 		t.Errorf("IsWinner should be false for a full stop-out, got %s", row[10])
// 	}
// 	// P/L = (90-100)*100 = -1000
// 	if row[5] != "-1000.00" {
// 		t.Errorf("P/L: got %s want -1000.00", row[5])
// 	}
// }

// func TestExecuteStopOut_WithCumulativeProfit_IsWinner(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	pos := newPos("NVDA", 100.00, 90.00, 50)
// 	pos.CumulativeProfit = 800.00 // partial profits already banked
// 	pos.ProfitTaken = true
// 	now := time.Now()

// 	executeStopOut(pos, 90.00, now)

// 	rows := readTradeCSV(t)
// 	if len(rows) != 1 {
// 		t.Fatalf("expected 1 row, got %d", len(rows))
// 	}
// 	// Total P/L = (90-100)*50 + 800 = -500 + 800 = +300 → winner
// 	if rows[0][10] != "true" {
// 		t.Errorf("should be winner when cumulative profit offsets stop loss, got %s", rows[0][10])
// 	}
// }

// func TestExecuteStopOut_ZeroShares_ReturnsTrue(t *testing.T) {
// 	pos := newPos("AAPL", 150.00, 140.00, 0) // 0 shares
// 	result := executeStopOut(pos, 140.00, time.Now())
// 	if !result {
// 		t.Error("should return true (nothing to sell)")
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 5. executeStrongEPProfit
// // ─────────────────────────────────────────────────────────────────────────────

// func TestExecuteStrongEPProfit_SellsCorrectPercent(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	pos := newPos("AMD", 100.00, 90.00, 100)
// 	now := time.Now()
// 	exitPrice := 115.00 // 15% gain

// 	closed := executeStrongEPProfit(pos, exitPrice, now)

// 	expectedSell := math.Floor(100 * STRONG_EP_TAKE_PERCENT) // 30 shares
// 	expectedRemaining := 100 - expectedSell

// 	if pos.Shares != expectedRemaining {
// 		t.Errorf("shares remaining: got %.0f want %.0f", pos.Shares, expectedRemaining)
// 	}
// 	if closed {
// 		t.Error("should not be fully closed after partial strong EP exit")
// 	}
// 	if !pos.ProfitTaken {
// 		t.Error("ProfitTaken flag should be set")
// 	}
// 	if !pos.TrailingStopMode {
// 		t.Error("TrailingStopMode should be enabled")
// 	}
// 	// Stop should be at least breakeven
// 	if pos.StopLoss < pos.EntryPrice {
// 		t.Errorf("stop %.2f should be >= entry %.2f after strong EP", pos.StopLoss, pos.EntryPrice)
// 	}
// }

// func TestExecuteStrongEPProfit_CumulativeProfitAccumulates(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	pos := newPos("META", 200.00, 180.00, 100)
// 	exitPrice := 230.00 // +15%

// 	executeStrongEPProfit(pos, exitPrice, time.Now())

// 	expectedShares := math.Floor(100 * STRONG_EP_TAKE_PERCENT) // 30 shares sold
// 	expectedPL := (exitPrice - 200.00) * expectedShares        // 30*30=900
// 	if math.Abs(pos.CumulativeProfit-expectedPL) > 0.01 {
// 		t.Errorf("CumulativeProfit: got %.2f want %.2f", pos.CumulativeProfit, expectedPL)
// 	}
// }

// func TestExecuteStrongEPProfit_FullyClosesWhenAllSharesSold(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	// Only 1 share — selling 30% rounds to 1, so position should fully close
// 	pos := newPos("SMALL", 100.00, 90.00, 1)
// 	closed := executeStrongEPProfit(pos, 115.00, time.Now())

// 	if !closed {
// 		t.Error("should be fully closed when all shares sold")
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 6. executeProfitPartial
// // ─────────────────────────────────────────────────────────────────────────────

// func TestExecuteProfitPartial_Level1(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	pos := newPos("GOOG", 100.00, 90.00, 100)
// 	exitPrice := 115.00 // 1.5R

// 	executeProfitPartial(pos, exitPrice, time.Now(), PROFIT_TAKE_1_PERCENT, 1)

// 	expectedSold := math.Floor(100 * PROFIT_TAKE_1_PERCENT) // 25
// 	expectedRemaining := 100 - expectedSold

// 	if pos.Shares != expectedRemaining {
// 		t.Errorf("shares: got %.0f want %.0f", pos.Shares, expectedRemaining)
// 	}

// 	rows := readTradeCSV(t)
// 	if len(rows) != 1 {
// 		t.Fatalf("expected 1 trade row, got %d", len(rows))
// 	}
// 	if rows[0][10] != "true" {
// 		t.Error("profit take should be a winner")
// 	}
// 	// ExitReason should mention level 1
// 	if !strings.Contains(rows[0][9], "1") {
// 		t.Errorf("exit reason should mention level 1: %s", rows[0][9])
// 	}
// }

// func TestExecuteProfitPartial_NeverSellsMoreThanOwned(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	pos := newPos("SMALL", 100.00, 90.00, 2) // only 2 shares
// 	executeProfitPartial(pos, 115.00, time.Now(), PROFIT_TAKE_1_PERCENT, 1)

// 	if pos.Shares < 0 {
// 		t.Errorf("shares went negative: %.0f", pos.Shares)
// 	}
// }

// func TestExecuteProfitPartial_RRCalculated(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	// Entry 100, stop 90 → InitialRisk = 10. Exit at 115 → (115-100)/10 = 1.5R
// 	pos := newPos("RR", 100.00, 90.00, 100)
// 	executeProfitPartial(pos, 115.00, time.Now(), 0.25, 1)

// 	rows := readTradeCSV(t)
// 	if len(rows) != 1 {
// 		t.Fatalf("expected 1 row")
// 	}
// 	rr, _ := parseFloat(rows[0][6])
// 	if math.Abs(rr-1.5) > 0.01 {
// 		t.Errorf("R/R: got %.2f want 1.50", rr)
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 7. executeExit
// // ─────────────────────────────────────────────────────────────────────────────

// func TestExecuteExit_RecordsTrade(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	watchlistPath = "" // prevent file writes in removeFromWatchlist

// 	pos := newPos("SPY", 400.00, 380.00, 50)
// 	result := executeExit(pos, 395.00, time.Now(), "Test exit reason")

// 	if !result {
// 		t.Error("executeExit should return true")
// 	}
// 	if pos.Shares != 0 {
// 		t.Errorf("shares should be 0, got %.0f", pos.Shares)
// 	}

// 	rows := readTradeCSV(t)
// 	if len(rows) != 1 {
// 		t.Fatalf("expected 1 row, got %d", len(rows))
// 	}
// 	// (395-400)*50 = -250 → loss, no cumulative profit
// 	if rows[0][10] != "false" {
// 		t.Errorf("should be false (loser), got %s", rows[0][10])
// 	}
// }

// func TestExecuteExit_WithCumulativeProfit_ReasonContainsPartials(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	watchlistPath = ""

// 	pos := newPos("QQQ", 300.00, 280.00, 50)
// 	pos.CumulativeProfit = 500.00
// 	executeExit(pos, 295.00, time.Now(), "Trailing exit")

// 	rows := readTradeCSV(t)
// 	if len(rows) != 1 {
// 		t.Fatalf("expected 1 row")
// 	}
// 	// ExitReason should mention the partial profit
// 	if !strings.Contains(rows[0][9], "500") {
// 		t.Errorf("exit reason should mention cumulative profit: %s", rows[0][9])
// 	}
// }

// func TestExecuteExit_ZeroShares_ReturnsTrueNoRow(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	watchlistPath = ""

// 	pos := newPos("ZERO", 100.00, 90.00, 0)
// 	result := executeExit(pos, 95.00, time.Now(), "empty")

// 	if !result {
// 		t.Error("should return true for 0-share position")
// 	}
// 	rows := readTradeCSV(t)
// 	if len(rows) != 0 {
// 		t.Errorf("no trade should be recorded for 0 shares, got %d rows", len(rows))
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 8. Exit rule logic — tested directly on position state
// //    (mirrors simulateNextDay tests from backtest but using the RT position)
// // ─────────────────────────────────────────────────────────────────────────────

// func TestBreakevenTrigger_SetsStop(t *testing.T) {
// 	pos := newPos("AAPL", 100.00, 90.00, 100)
// 	pos.DaysHeld = BREAKEVEN_TRIGGER_DAYS
// 	pos.ProfitTaken = false

// 	// Simulate: sessionHigh touched +3% (above BREAKEVEN_TRIGGER_PERCENT=2%)
// 	sessionHigh := pos.EntryPrice * 1.03
// 	pctGain := (sessionHigh - pos.EntryPrice) / pos.EntryPrice

// 	if pctGain >= BREAKEVEN_TRIGGER_PERCENT && pos.StopLoss < pos.EntryPrice {
// 		pos.StopLoss = pos.EntryPrice
// 	}

// 	if pos.StopLoss != pos.EntryPrice {
// 		t.Errorf("stop should be at breakeven %.2f, got %.2f", pos.EntryPrice, pos.StopLoss)
// 	}
// }

// func TestBreakevenTrigger_DoesNotTriggerBeforeDay2(t *testing.T) {
// 	pos := newPos("AAPL", 100.00, 90.00, 100)
// 	pos.DaysHeld = 1 // before BREAKEVEN_TRIGGER_DAYS
// 	originalStop := pos.StopLoss

// 	sessionHigh := pos.EntryPrice * 1.05
// 	pctGain := (sessionHigh - pos.EntryPrice) / pos.EntryPrice

// 	if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS {
// 		if pctGain >= BREAKEVEN_TRIGGER_PERCENT && pos.StopLoss < pos.EntryPrice {
// 			pos.StopLoss = pos.EntryPrice
// 		}
// 	}

// 	if pos.StopLoss != originalStop {
// 		t.Error("breakeven should not trigger before BREAKEVEN_TRIGGER_DAYS")
// 	}
// }

// func TestNoFollowThrough_TightensStop(t *testing.T) {
// 	pos := newPos("WEAK", 100.00, 90.00, 100)
// 	pos.DaysHeld = MAX_DAYS_NO_FOLLOWTHROUGH
// 	pos.ProfitTaken = false

// 	currentRR := 0.3 // below 0.5 threshold

// 	if pos.DaysHeld >= MAX_DAYS_NO_FOLLOWTHROUGH && currentRR < 0.5 && !pos.ProfitTaken {
// 		tighter := math.Max(pos.EntryPrice-(pos.InitialRisk*0.3), pos.EntryPrice)
// 		if tighter > pos.StopLoss {
// 			pos.StopLoss = tighter
// 		}
// 	}

// 	if pos.StopLoss <= 90.00 {
// 		t.Errorf("stop should have been tightened above original 90.00, got %.2f", pos.StopLoss)
// 	}
// }

// func TestNoFollowThrough_DoesNotTightenIfProfitTaken(t *testing.T) {
// 	pos := newPos("PROFIT", 100.00, 90.00, 100)
// 	pos.DaysHeld = MAX_DAYS_NO_FOLLOWTHROUGH
// 	pos.ProfitTaken = true // already took profit
// 	originalStop := pos.StopLoss

// 	currentRR := 0.3
// 	if pos.DaysHeld >= MAX_DAYS_NO_FOLLOWTHROUGH && currentRR < 0.5 && !pos.ProfitTaken {
// 		tighter := math.Max(pos.EntryPrice-(pos.InitialRisk*0.3), pos.EntryPrice)
// 		if tighter > pos.StopLoss {
// 			pos.StopLoss = tighter
// 		}
// 	}

// 	if pos.StopLoss != originalStop {
// 		t.Error("should not tighten stop when ProfitTaken is true")
// 	}
// }

// func TestWeakClose_ThresholdDetection(t *testing.T) {
// 	// Session high $100, close $69 → 31% below high → should trigger
// 	sessionHigh := 100.0
// 	currentPrice := 69.0
// 	closeFromHigh := (sessionHigh - currentPrice) / sessionHigh

// 	if closeFromHigh < WEAK_CLOSE_THRESHOLD {
// 		t.Errorf("%.1f%% drop should exceed WEAK_CLOSE_THRESHOLD %.0f%%",
// 			closeFromHigh*100, WEAK_CLOSE_THRESHOLD*100)
// 	}
// }

// func TestWeakClose_BelowThresholdNotTriggered(t *testing.T) {
// 	// Session high $100, close $75 → 25% below high → should NOT trigger
// 	sessionHigh := 100.0
// 	currentPrice := 75.0
// 	closeFromHigh := (sessionHigh - currentPrice) / sessionHigh

// 	if closeFromHigh >= WEAK_CLOSE_THRESHOLD {
// 		t.Errorf("%.1f%% drop should NOT exceed WEAK_CLOSE_THRESHOLD %.0f%%",
// 			closeFromHigh*100, WEAK_CLOSE_THRESHOLD*100)
// 	}
// }

// func TestTrailingStop_UpdatesOnBigGain(t *testing.T) {
// 	pos := newPos("RUNNER", 100.00, 90.00, 100)
// 	pos.TrailingStopMode = true
// 	pos.ProfitTaken = true
// 	pos.StopLoss = 100.00 // at breakeven

// 	currentPrice := 125.00 // +25% → uses 94% trail
// 	pctFromEntry := (currentPrice - pos.EntryPrice) / pos.EntryPrice

// 	var newStop float64
// 	switch {
// 	case pctFromEntry > 0.20:
// 		newStop = currentPrice * 0.94
// 	case pctFromEntry > 0.10:
// 		newStop = currentPrice * 0.95
// 	case pctFromEntry > 0.05:
// 		newStop = currentPrice * 0.96
// 	default:
// 		newStop = pos.EntryPrice
// 	}

// 	newStop = math.Max(newStop, pos.EntryPrice)
// 	if newStop > pos.StopLoss {
// 		pos.StopLoss = newStop
// 	}

// 	expectedStop := 125.00 * 0.94
// 	if math.Abs(pos.StopLoss-expectedStop) > 0.01 {
// 		t.Errorf("trailing stop: got %.2f want %.2f", pos.StopLoss, expectedStop)
// 	}
// }

// func TestTrailingStop_NeverMovesBelow_Breakeven(t *testing.T) {
// 	pos := newPos("FLAT", 100.00, 90.00, 100)
// 	pos.TrailingStopMode = true
// 	pos.StopLoss = 100.00

// 	currentPrice := 101.00 // only +1% — below all thresholds
// 	pctFromEntry := (currentPrice - pos.EntryPrice) / pos.EntryPrice

// 	var newStop float64
// 	switch {
// 	case pctFromEntry > 0.20:
// 		newStop = currentPrice * 0.94
// 	case pctFromEntry > 0.10:
// 		newStop = currentPrice * 0.95
// 	case pctFromEntry > 0.05:
// 		newStop = currentPrice * 0.96
// 	default:
// 		newStop = pos.EntryPrice
// 	}
// 	newStop = math.Max(newStop, pos.EntryPrice)

// 	if newStop < pos.EntryPrice {
// 		t.Errorf("trailing stop %.2f fell below entry price %.2f", newStop, pos.EntryPrice)
// 	}
// }

// func TestProfitLevel1_StopMovedToBreakeven(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	pos := newPos("AAPL", 100.00, 90.00, 100)
// 	pos.DaysHeld = 3 // past BREAKEVEN_TRIGGER_DAYS

// 	currentRR := PROFIT_TAKE_1_RR + 0.1 // just past 1.5R
// 	if currentRR >= PROFIT_TAKE_1_RR && !pos.ProfitTaken {
// 		currentPrice := pos.EntryPrice + (pos.InitialRisk * currentRR)
// 		executeProfitPartial(pos, currentPrice, time.Now(), PROFIT_TAKE_1_PERCENT, 1)
// 		pos.TrailingStopMode = true
// 		pos.ProfitTaken = true
// 		if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS {
// 			pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice)
// 		}
// 	}

// 	if pos.StopLoss < pos.EntryPrice {
// 		t.Errorf("stop %.2f should be >= entry %.2f after level 1 profit", pos.StopLoss, pos.EntryPrice)
// 	}
// 	if !pos.TrailingStopMode {
// 		t.Error("TrailingStopMode should be enabled after level 1 profit")
// 	}
// }

// func TestProfitLevel2_StopMovedTo1R(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	pos := newPos("NVDA", 100.00, 90.00, 100)
// 	pos.ProfitTaken = true // level 1 already done
// 	pos.StopLoss = 100.00  // at breakeven

// 	currentRR := PROFIT_TAKE_2_RR + 0.1
// 	if currentRR >= PROFIT_TAKE_2_RR && pos.ProfitTaken && !pos.ProfitTaken2 {
// 		currentPrice := pos.EntryPrice + (pos.InitialRisk * currentRR)
// 		executeProfitPartial(pos, currentPrice, time.Now(), PROFIT_TAKE_2_PERCENT, 2)
// 		newStop := pos.EntryPrice + (pos.InitialRisk * 1.0)
// 		pos.StopLoss = math.Max(pos.StopLoss, newStop)
// 		pos.ProfitTaken2 = true
// 	}

// 	expected1R := 100.00 + 10.00 // entry + 1*risk
// 	if pos.StopLoss < expected1R {
// 		t.Errorf("stop %.2f should be >= +1R (%.2f) after level 2 profit", pos.StopLoss, expected1R)
// 	}
// }

// func TestProfitLevel3_StopMovedTo2R(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	pos := newPos("META", 100.00, 90.00, 100)
// 	pos.ProfitTaken = true
// 	pos.ProfitTaken2 = true
// 	pos.StopLoss = 110.00 // at +1R

// 	currentRR := PROFIT_TAKE_3_RR + 0.1
// 	if currentRR >= PROFIT_TAKE_3_RR && pos.ProfitTaken2 && !pos.ProfitTaken3 {
// 		currentPrice := pos.EntryPrice + (pos.InitialRisk * currentRR)
// 		executeProfitPartial(pos, currentPrice, time.Now(), PROFIT_TAKE_3_PERCENT, 3)
// 		newStop := pos.EntryPrice + (pos.InitialRisk * 2.0)
// 		pos.StopLoss = math.Max(pos.StopLoss, newStop)
// 		pos.ProfitTaken3 = true
// 	}

// 	expected2R := 100.00 + (10.00 * 2.0)
// 	if pos.StopLoss < expected2R {
// 		t.Errorf("stop %.2f should be >= +2R (%.2f) after level 3 profit", pos.StopLoss, expected2R)
// 	}
// }

// func TestProfitLevels_DoNotSkipSequence(t *testing.T) {
// 	// Level 2 should not fire if level 1 hasn't been taken yet
// 	pos := newPos("SEQ", 100.00, 90.00, 100)
// 	pos.ProfitTaken = false // level 1 not taken

// 	currentRR := 3.5 // past level 2 threshold
// 	level2Fired := currentRR >= PROFIT_TAKE_2_RR && pos.ProfitTaken && !pos.ProfitTaken2

// 	if level2Fired {
// 		t.Error("level 2 should not fire when level 1 has not been taken")
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 9. removeFromWatchlist
// // ─────────────────────────────────────────────────────────────────────────────

// func TestRemoveFromWatchlist_RemovesSymbol(t *testing.T) {
// 	rows := [][]string{
// 		{"Symbol", "Entry", "Stop", "Shares", "Risk", "Date"},
// 		{"AAPL", "150", "140", "100", "10", "2024-01-15"},
// 		{"TSLA", "200", "185", "50", "15", "2024-01-15"},
// 		{"NVDA", "500", "480", "20", "20", "2024-01-15"},
// 	}
// 	path := tmpWatchlist(t, rows)
// 	watchlistPath = path

// 	removeFromWatchlist("TSLA")

// 	f, err := os.Open(path)
// 	if err != nil {
// 		t.Fatalf("open: %v", err)
// 	}
// 	defer f.Close()
// 	records, _ := csv.NewReader(f).ReadAll()

// 	for _, rec := range records[1:] {
// 		if rec[0] == "TSLA" {
// 			t.Error("TSLA should have been removed from watchlist")
// 		}
// 	}
// 	// Other symbols should remain
// 	found := map[string]bool{}
// 	for _, rec := range records[1:] {
// 		found[rec[0]] = true
// 	}
// 	if !found["AAPL"] || !found["NVDA"] {
// 		t.Error("AAPL and NVDA should still be in watchlist")
// 	}
// }

// func TestRemoveFromWatchlist_SymbolNotPresent_NoError(t *testing.T) {
// 	rows := [][]string{
// 		{"Symbol", "Entry", "Stop", "Shares", "Risk", "Date"},
// 		{"AAPL", "150", "140", "100", "10", "2024-01-15"},
// 	}
// 	path := tmpWatchlist(t, rows)
// 	watchlistPath = path

// 	// Should not panic or error
// 	removeFromWatchlist("MISSING")

// 	f, _ := os.Open(path)
// 	defer f.Close()
// 	records, _ := csv.NewReader(f).ReadAll()
// 	if len(records) != 2 { // header + AAPL
// 		t.Errorf("watchlist should be unchanged, got %d rows", len(records))
// 	}
// }

// func TestRemoveFromWatchlist_EmptyPath_NoOp(t *testing.T) {
// 	watchlistPath = ""
// 	// Must not panic
// 	removeFromWatchlist("ANYTHING")
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 10. processWatchlist — deduplication
// // ─────────────────────────────────────────────────────────────────────────────

// func TestProcessWatchlist_SkipsAlreadyProcessed(t *testing.T) {
// 	resetGlobals()

// 	rows := [][]string{
// 		{"Symbol", "Entry", "Stop", "Shares", "Risk", "Date"},
// 		{"AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"},
// 	}
// 	path := tmpWatchlist(t, rows)

// 	processedMu.Lock()
// 	processedSymbols["AAPL"] = true
// 	processedMu.Unlock()

// 	processWatchlist(path)

// 	// Give goroutine time if any was mistakenly spawned
// 	time.Sleep(50 * time.Millisecond)

// 	positionsMu.RLock()
// 	_, active := activePositions["AAPL"]
// 	positionsMu.RUnlock()

// 	if active {
// 		t.Error("already-processed symbol should not be added to activePositions")
// 	}
// }

// func TestProcessWatchlist_SkipsAlreadyActive(t *testing.T) {
// 	resetGlobals()

// 	positionsMu.Lock()
// 	activePositions["TSLA"] = newPos("TSLA", 200.00, 185.00, 50)
// 	positionsMu.Unlock()

// 	rows := [][]string{
// 		{"Symbol", "Entry", "Stop", "Shares", "Risk", "Date"},
// 		{"TSLA", "200.00", "185.00", "50", "15.00", "2024-01-15"},
// 	}
// 	path := tmpWatchlist(t, rows)
// 	processWatchlist(path)
// 	time.Sleep(50 * time.Millisecond)

// 	positionsMu.RLock()
// 	count := len(activePositions)
// 	positionsMu.RUnlock()

// 	if count != 1 {
// 		t.Errorf("should still have exactly 1 active position, got %d", count)
// 	}
// }

// func TestProcessWatchlist_EmptyFile_NoOp(t *testing.T) {
// 	resetGlobals()
// 	rows := [][]string{
// 		{"Symbol", "Entry", "Stop", "Shares", "Risk", "Date"},
// 	}
// 	path := tmpWatchlist(t, rows)
// 	processWatchlist(path) // should not panic
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 11. Trade results CSV
// // ─────────────────────────────────────────────────────────────────────────────

// func TestInitTradeResultsFile_CreatesFile(t *testing.T) {
// 	dir := t.TempDir()
// 	old, _ := os.Getwd()
// 	_ = os.Chdir(dir)
// 	defer os.Chdir(old)

// 	initTradeResultsFile()

// 	if _, err := os.Stat("trade_results.csv"); os.IsNotExist(err) {
// 		t.Error("trade_results.csv should have been created")
// 	}
// }

// func TestInitTradeResultsFile_DoesNotOverwriteExisting(t *testing.T) {
// 	dir := t.TempDir()
// 	old, _ := os.Getwd()
// 	_ = os.Chdir(dir)
// 	defer os.Chdir(old)

// 	// Create file with an existing trade row
// 	f, _ := os.Create("trade_results.csv")
// 	w := csv.NewWriter(f)
// 	_ = w.WriteAll([][]string{
// 		{"Symbol", "EntryPrice", "ExitPrice", "Shares", "InitialRisk",
// 			"ProfitLoss", "RiskReward", "EntryDate", "ExitDate", "ExitReason", "IsWinner"},
// 		{"AAPL", "150", "160", "100", "10", "1000", "1.0", "2024-01-01", "2024-01-10", "test", "true"},
// 	})
// 	f.Close()

// 	initTradeResultsFile() // should not overwrite

// 	f2, _ := os.Open("trade_results.csv")
// 	defer f2.Close()
// 	records, _ := csv.NewReader(f2).ReadAll()
// 	if len(records) != 2 {
// 		t.Errorf("existing data should be preserved, got %d rows", len(records))
// 	}
// }

// func TestRecordTrade_AppendsRow(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	r := TradeRecord{
// 		Symbol:      "AMZN",
// 		EntryPrice:  180.00,
// 		ExitPrice:   190.00,
// 		Shares:      50,
// 		InitialRisk: 10.00,
// 		ProfitLoss:  500.00,
// 		RiskReward:  1.0,
// 		EntryDate:   "2024-01-01",
// 		ExitDate:    "2024-01-10",
// 		ExitReason:  "Profit Level 1 at 1.0R",
// 		IsWinner:    true,
// 	}
// 	recordTrade(r)

// 	rows := readTradeCSV(t)
// 	if len(rows) != 1 {
// 		t.Fatalf("expected 1 row, got %d", len(rows))
// 	}
// 	if rows[0][0] != "AMZN" {
// 		t.Errorf("symbol: %s", rows[0][0])
// 	}
// 	if rows[0][5] != "500.00" {
// 		t.Errorf("ProfitLoss: %s", rows[0][5])
// 	}
// 	if rows[0][10] != "true" {
// 		t.Errorf("IsWinner: %s", rows[0][10])
// 	}
// }

// func TestRecordTrade_ConcurrentWrites(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	var wg sync.WaitGroup
// 	n := 20
// 	for i := 0; i < n; i++ {
// 		wg.Add(1)
// 		go func(i int) {
// 			defer wg.Done()
// 			recordTrade(TradeRecord{
// 				Symbol:     "CONCURRENT",
// 				EntryPrice: 100,
// 				ExitPrice:  110,
// 				Shares:     10,
// 				EntryDate:  "2024-01-01",
// 				ExitDate:   "2024-01-02",
// 				ExitReason: "test",
// 				IsWinner:   true,
// 			})
// 		}(i)
// 	}
// 	wg.Wait()

// 	rows := readTradeCSV(t)
// 	if len(rows) != n {
// 		t.Errorf("concurrent writes: got %d rows, want %d", len(rows), n)
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 12. printCurrentStats — smoke test (just verifies it doesn't panic)
// // ─────────────────────────────────────────────────────────────────────────────

// func TestPrintCurrentStats_NoRecords_NoOp(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()
// 	printCurrentStats() // must not panic
// }

// func TestPrintCurrentStats_WithMixedResults(t *testing.T) {
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	trades := []TradeRecord{
// 		{Symbol: "W1", EntryPrice: 100, ExitPrice: 115, Shares: 100, InitialRisk: 10,
// 			ProfitLoss: 1500, RiskReward: 1.5, EntryDate: "2024-01-01", ExitDate: "2024-01-05",
// 			ExitReason: "Profit Level 1", IsWinner: true},
// 		{Symbol: "L1", EntryPrice: 100, ExitPrice: 90, Shares: 100, InitialRisk: 10,
// 			ProfitLoss: -1000, RiskReward: -1.0, EntryDate: "2024-01-02", ExitDate: "2024-01-06",
// 			ExitReason: "Stop Loss Hit", IsWinner: false},
// 		{Symbol: "W2", EntryPrice: 50, ExitPrice: 80, Shares: 200, InitialRisk: 5,
// 			ProfitLoss: 6000, RiskReward: 6.0, EntryDate: "2024-01-03", ExitDate: "2024-01-20",
// 			ExitReason: "Profit Level 3", IsWinner: true},
// 	}
// 	for _, r := range trades {
// 		recordTrade(r)
// 	}

// 	// must not panic
// 	printCurrentStats()
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 13. evaluatePosition — full end-to-end with injected fakes
// //
// // These tests patch getAlpacaPositionFn and getIntradayBarsFn so that
// // evaluatePosition runs all of its exit logic against synthetic price data
// // without making any real network calls.
// // ─────────────────────────────────────────────────────────────────────────────

// // stubDeps patches both injectable function variables and returns a restore func.
// // alpacaShares: qty returned by the fake Alpaca position lookup (0 = position gone).
// // bars: intraday bars the fake market-data feed will return.
// func stubDeps(t *testing.T, alpacaShares float64, bars []Bar) func() {
// 	t.Helper()

// 	origAlpaca := getAlpacaPositionFn
// 	origBars := getIntradayBarsFn

// 	getAlpacaPositionFn = func(symbol string) (float64, error) {
// 		if alpacaShares == 0 {
// 			return 0, fmt.Errorf("position not found")
// 		}
// 		return alpacaShares, nil
// 	}

// 	getIntradayBarsFn = func(symbol string, sessionStart, now time.Time) ([]Bar, error) {
// 		if len(bars) == 0 {
// 			return nil, fmt.Errorf("no bars")
// 		}
// 		return bars, nil
// 	}

// 	return func() {
// 		getAlpacaPositionFn = origAlpaca
// 		getIntradayBarsFn = origBars
// 	}
// }

// // makeBars is a convenience constructor: builds a []Bar slice representing a
// // session where price opens at open, hits sessionHigh and sessionLow intraday,
// // then closes at close.
// func makeBars(open, sessionHigh, sessionLow, close float64) []Bar {
// 	return []Bar{
// 		{Open: open, High: sessionHigh, Low: sessionLow, Close: close},
// 	}
// }

// // TestEvaluatePosition_StopHit verifies that when the session low breaches the
// // stop loss, evaluatePosition returns true and records a trade.
// func TestEvaluatePosition_StopHit(t *testing.T) {
// 	setupEasternLoc(t)
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()
// 	watchlistPath = ""

// 	pos := newPos("STOP", 100.00, 90.00, 100)
// 	// Session low = 89 — below stop of 90
// 	restore := stubDeps(t, 100, makeBars(100, 102, 89, 91))
// 	defer restore()

// 	closed := evaluatePosition(pos)

// 	if !closed {
// 		t.Error("position should be closed when session low breaches stop")
// 	}
// 	if pos.Shares != 0 {
// 		t.Errorf("shares should be 0 after stop, got %.0f", pos.Shares)
// 	}
// 	rows := readTradeCSV(t)
// 	if len(rows) == 0 {
// 		t.Error("trade should be recorded on stop out")
// 	}
// 	if rows[0][10] != "false" {
// 		t.Errorf("stop at loss should be IsWinner=false, got %s", rows[0][10])
// 	}
// }

// // TestEvaluatePosition_CurrentPriceAtStop_Closes verifies that when the
// // current close itself equals or falls below the stop, position closes.
// func TestEvaluatePosition_CurrentPriceAtStop_Closes(t *testing.T) {
// 	setupEasternLoc(t)
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()
// 	watchlistPath = ""

// 	pos := newPos("ATST", 100.00, 90.00, 100)
// 	// Close exactly at stop price
// 	restore := stubDeps(t, 100, makeBars(100, 101, 90, 90))
// 	defer restore()

// 	closed := evaluatePosition(pos)
// 	if !closed {
// 		t.Error("position should close when close == stop")
// 	}
// }

// // TestEvaluatePosition_AlpacaPositionGone_Removes verifies that if Alpaca
// // returns an error (position externally closed), the monitor removes it.
// func TestEvaluatePosition_AlpacaPositionGone_Removes(t *testing.T) {
// 	setupEasternLoc(t)
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()
// 	watchlistPath = ""

// 	pos := newPos("GONE", 100.00, 90.00, 100)
// 	// alpacaShares=0 causes the fake to return an error
// 	restore := stubDeps(t, 0, makeBars(100, 105, 99, 103))
// 	defer restore()

// 	closed := evaluatePosition(pos)
// 	if !closed {
// 		t.Error("position should be removed when Alpaca reports it no longer exists")
// 	}
// }

// // TestEvaluatePosition_WeakClose_Exits verifies that a session close more than
// // WEAK_CLOSE_THRESHOLD below the session high triggers an immediate exit.
// func TestEvaluatePosition_WeakClose_Exits(t *testing.T) {
// 	setupEasternLoc(t)
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()
// 	watchlistPath = ""

// 	pos := newPos("WEAK", 100.00, 90.00, 100)
// 	// Session high = 120, close = 83 → (120-83)/120 = 30.8% > WEAK_CLOSE_THRESHOLD
// 	restore := stubDeps(t, 100, makeBars(100, 120, 82, 83))
// 	defer restore()

// 	closed := evaluatePosition(pos)
// 	if !closed {
// 		t.Error("position should close on weak close detection")
// 	}
// 	rows := readTradeCSV(t)
// 	if len(rows) == 0 {
// 		t.Fatal("trade should be recorded")
// 	}
// 	if !strings.Contains(rows[0][9], "Weak") {
// 		t.Errorf("exit reason should mention Weak Close, got: %s", rows[0][9])
// 	}
// }

// // TestEvaluatePosition_WeakClose_NotTriggeredBelowThreshold verifies that a
// // modest pullback from the high does NOT trigger a weak-close exit.
// func TestEvaluatePosition_WeakClose_NotTriggeredBelowThreshold(t *testing.T) {
// 	setupEasternLoc(t)
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()
// 	watchlistPath = ""

// 	pos := newPos("OK", 100.00, 90.00, 100)
// 	// Session high = 110, close = 108 → (110-108)/110 = 1.8% — well under 30%
// 	restore := stubDeps(t, 100, makeBars(100, 110, 99, 108))
// 	defer restore()

// 	closed := evaluatePosition(pos)
// 	if closed {
// 		t.Error("position should NOT close on a small pullback from high")
// 	}
// }

// // TestEvaluatePosition_BreakevenSet verifies that after BREAKEVEN_TRIGGER_DAYS
// // and a session high >= BREAKEVEN_TRIGGER_PERCENT, the stop is moved to entry.
// func TestEvaluatePosition_BreakevenSet(t *testing.T) {
// 	setupEasternLoc(t)
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()
// 	watchlistPath = ""

// 	pos := newPos("BE", 100.00, 90.00, 100)
// 	pos.DaysHeld = BREAKEVEN_TRIGGER_DAYS     // already at trigger day
// 	pos.LastCheckDate = time.Now()            // same day so DaysHeld won't increment again

// 	// Session high = 103 (+3%) — above BREAKEVEN_TRIGGER_PERCENT (2%)
// 	// Close = 101 (still above stop and above entry)
// 	restore := stubDeps(t, 100, makeBars(100, 103, 99, 101))
// 	defer restore()

// 	evaluatePosition(pos)

// 	if pos.StopLoss != pos.EntryPrice {
// 		t.Errorf("stop should be at breakeven %.2f, got %.2f", pos.EntryPrice, pos.StopLoss)
// 	}
// }

// // TestEvaluatePosition_ProfitLevel1_Triggered verifies that when currentRR
// // crosses PROFIT_TAKE_1_RR, 25% of shares are sold and flags are updated.
// func TestEvaluatePosition_ProfitLevel1_Triggered(t *testing.T) {
// 	setupEasternLoc(t)
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()
// 	watchlistPath = ""

// 	// entry=100, stop=90, risk=10 → 1.5R target = close at 115
// 	// DaysHeld=4: past STRONG_EP_DAYS=3 so Strong EP cannot fire, only L1 can.
// 	pos := newPos("P1", 100.00, 90.00, 100)
// 	pos.DaysHeld = 4
// 	pos.LastCheckDate = time.Now()

// 	restore := stubDeps(t, 100, makeBars(100, 116, 99, 115))
// 	defer restore()

// 	closed := evaluatePosition(pos)

// 	expectedRemaining := 100 - math.Floor(100*PROFIT_TAKE_1_PERCENT) // 75
// 	if pos.Shares != expectedRemaining {
// 		t.Errorf("shares after L1: got %.0f want %.0f", pos.Shares, expectedRemaining)
// 	}
// 	if !pos.ProfitTaken {
// 		t.Error("ProfitTaken flag should be true")
// 	}
// 	if !pos.TrailingStopMode {
// 		t.Error("TrailingStopMode should be true after L1")
// 	}
// 	if closed {
// 		t.Error("position should not be fully closed after L1 partial")
// 	}
// 	rows := readTradeCSV(t)
// 	if len(rows) == 0 {
// 		t.Error("trade should be recorded for L1 partial exit")
// 	}
// }

// // TestEvaluatePosition_SharesSyncedFromAlpaca verifies that when Alpaca
// // reports a different share count (e.g. partial fill), the position is updated.
// func TestEvaluatePosition_SharesSyncedFromAlpaca(t *testing.T) {
// 	setupEasternLoc(t)
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()
// 	watchlistPath = ""

// 	pos := newPos("SYNC", 100.00, 90.00, 100)
// 	pos.LastCheckDate = time.Now()

// 	// Alpaca says 80 shares (partial fill happened externally)
// 	restore := stubDeps(t, 80, makeBars(100, 102, 99, 101))
// 	defer restore()

// 	evaluatePosition(pos)

// 	if pos.Shares != 80 {
// 		t.Errorf("shares should sync to Alpaca value 80, got %.0f", pos.Shares)
// 	}
// }

// // TestEvaluatePosition_StrongEP_PartialSell verifies that a 15%+ gain in the
// // first STRONG_EP_DAYS days triggers the strong EP partial exit.
// func TestEvaluatePosition_StrongEP_PartialSell(t *testing.T) {
// 	setupEasternLoc(t)
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()
// 	watchlistPath = ""

// 	pos := newPos("SEP", 100.00, 90.00, 100)
// 	pos.DaysHeld = 2           // within STRONG_EP_DAYS=3
// 	pos.LastCheckDate = time.Now()

// 	// Close at 116 = +16% gain, above STRONG_EP_GAIN=15%
// 	restore := stubDeps(t, 100, makeBars(100, 117, 99, 116))
// 	defer restore()

// 	evaluatePosition(pos)

// 	expectedRemaining := 100 - math.Floor(100*STRONG_EP_TAKE_PERCENT) // 70
// 	if pos.Shares != expectedRemaining {
// 		t.Errorf("after strong EP: got %.0f shares want %.0f", pos.Shares, expectedRemaining)
// 	}
// 	if !pos.ProfitTaken {
// 		t.Error("ProfitTaken should be set after strong EP")
// 	}
// 	if pos.StopLoss < pos.EntryPrice {
// 		t.Errorf("stop %.2f should be >= entry %.2f after strong EP", pos.StopLoss, pos.EntryPrice)
// 	}
// }

// // TestEvaluatePosition_NoBarsSkipsTick verifies that when the bar feed returns
// // no data (e.g. pre-market, holiday), evaluatePosition returns false (keep alive).
// func TestEvaluatePosition_NoBarsSkipsTick(t *testing.T) {
// 	setupEasternLoc(t)
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()

// 	pos := newPos("NOBARS", 100.00, 90.00, 100)
// 	// Empty bar slice → should skip
// 	restore := stubDeps(t, 100, []Bar{})
// 	defer restore()

// 	closed := evaluatePosition(pos)
// 	if closed {
// 		t.Error("position should stay alive when no bar data is available")
// 	}
// }

// // TestEvaluatePosition_TrailingStop_UpdatesCorrectly verifies that once
// // TrailingStopMode is active, the stop ratchets up as price rises.
// func TestEvaluatePosition_TrailingStop_UpdatesCorrectly(t *testing.T) {
// 	setupEasternLoc(t)
// 	cleanup := tmpTradeResults(t)
// 	defer cleanup()
// 	watchlistPath = ""

// 	pos := newPos("TRAIL", 100.00, 90.00, 100)
// 	pos.ProfitTaken = true
// 	pos.TrailingStopMode = true
// 	pos.StopLoss = 100.00 // at breakeven
// 	pos.LastCheckDate = time.Now()

// 	// Close at 125 (+25%) → should use 94% trail = 117.50
// 	// Session low $101 > stop $100 so stop-out does NOT fire first.
// 	restore := stubDeps(t, 100, makeBars(100, 126, 101, 125))
// 	defer restore()

// 	evaluatePosition(pos)

// 	expectedStop := 125.00 * 0.94
// 	if math.Abs(pos.StopLoss-expectedStop) > 0.01 {
// 		t.Errorf("trailing stop: got %.2f want %.2f", pos.StopLoss, expectedStop)
// 	}
// }

// // TestEvaluatePositions_EmptyMap_NoOp verifies the loop returns immediately
// // when there are no active positions.
// func TestEvaluatePositions_EmptyMap_NoOp(t *testing.T) {
// 	resetGlobals()
// 	setupEasternLoc(t)
// 	evaluatePositions() // must not panic
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 14. RiskReward math
// // ─────────────────────────────────────────────────────────────────────────────

// func TestRiskReward_Calculation(t *testing.T) {
// 	tests := []struct {
// 		entry, stop, exit float64
// 		wantRR            float64
// 	}{
// 		{100, 90, 115, 1.5},  // (115-100)/(100-90) = 1.5R
// 		{100, 90, 130, 3.0},  // 3R
// 		{100, 90, 90, -1.0},  // full stop = -1R
// 		{200, 180, 240, 2.0}, // 2R
// 	}

// 	for _, tc := range tests {
// 		risk := tc.entry - tc.stop
// 		rr := (tc.exit - tc.entry) / risk
// 		if math.Abs(rr-tc.wantRR) > 0.001 {
// 			t.Errorf("entry=%.0f stop=%.0f exit=%.0f: got R/R %.3f want %.3f",
// 				tc.entry, tc.stop, tc.exit, rr, tc.wantRR)
// 		}
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 15. Session OHLC reduction logic
// // ─────────────────────────────────────────────────────────────────────────────

// func TestSessionOHLC_HighLowFromBars(t *testing.T) {
// 	bars := []fakeBar{
// 		{Open: 100, High: 105, Low: 98, Close: 102},
// 		{Open: 102, High: 110, Low: 101, Close: 108},
// 		{Open: 108, High: 112, Low: 106, Close: 109},
// 	}

// 	sessionHigh := 0.0
// 	sessionLow := math.MaxFloat64
// 	for _, b := range bars {
// 		if b.High > sessionHigh {
// 			sessionHigh = b.High
// 		}
// 		if b.Low < sessionLow {
// 			sessionLow = b.Low
// 		}
// 	}

// 	if sessionHigh != 112 {
// 		t.Errorf("sessionHigh: got %.0f want 112", sessionHigh)
// 	}
// 	if sessionLow != 98 {
// 		t.Errorf("sessionLow: got %.0f want 98", sessionLow)
// 	}
// }

// func TestSessionOHLC_SingleBar(t *testing.T) {
// 	bars := []fakeBar{
// 		{Open: 50, High: 55, Low: 48, Close: 52},
// 	}

// 	sessionHigh := 0.0
// 	sessionLow := math.MaxFloat64
// 	for _, b := range bars {
// 		if b.High > sessionHigh {
// 			sessionHigh = b.High
// 		}
// 		if b.Low < sessionLow {
// 			sessionLow = b.Low
// 		}
// 	}

// 	if sessionHigh != 55 {
// 		t.Errorf("single bar high: got %.0f", sessionHigh)
// 	}
// 	if sessionLow != 48 {
// 		t.Errorf("single bar low: got %.0f", sessionLow)
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 16. DaysHeld increment logic
// // ─────────────────────────────────────────────────────────────────────────────

// func TestDaysHeld_IncrementOnce_PerDay(t *testing.T) {
// 	pos := newPos("DAYS", 100, 90, 100)
// 	yesterday := time.Now().AddDate(0, 0, -1)
// 	pos.LastCheckDate = yesterday

// 	today := time.Now().Format("2006-01-02")
// 	if today != pos.LastCheckDate.Format("2006-01-02") {
// 		pos.DaysHeld++
// 		pos.LastCheckDate = time.Now()
// 	}

// 	if pos.DaysHeld != 1 {
// 		t.Errorf("DaysHeld should be 1 after first new-day tick, got %d", pos.DaysHeld)
// 	}

// 	// Call again same day — should not increment
// 	today2 := time.Now().Format("2006-01-02")
// 	if today2 != pos.LastCheckDate.Format("2006-01-02") {
// 		pos.DaysHeld++
// 	}

// 	if pos.DaysHeld != 1 {
// 		t.Errorf("DaysHeld should still be 1 within same day, got %d", pos.DaysHeld)
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 17. StrongEP — only fires within STRONG_EP_DAYS
// // ─────────────────────────────────────────────────────────────────────────────

// func TestStrongEP_DoesNotFireAfterStrongEPDays(t *testing.T) {
// 	pos := newPos("LATE", 100.00, 90.00, 100)
// 	pos.DaysHeld = STRONG_EP_DAYS + 1 // past the window
// 	pos.ProfitTaken = false

// 	pctGain := 0.20 // 20% gain but too late
// 	shouldFire := pos.DaysHeld <= STRONG_EP_DAYS && pctGain >= STRONG_EP_GAIN && !pos.ProfitTaken

// 	if shouldFire {
// 		t.Error("strong EP should not fire past STRONG_EP_DAYS")
// 	}
// }

// func TestStrongEP_FiresWithinWindow(t *testing.T) {
// 	pos := newPos("EARLY", 100.00, 90.00, 100)
// 	pos.DaysHeld = 2 // within STRONG_EP_DAYS=3
// 	pos.ProfitTaken = false

// 	pctGain := 0.16 // 16% > STRONG_EP_GAIN=15%
// 	shouldFire := pos.DaysHeld <= STRONG_EP_DAYS && pctGain >= STRONG_EP_GAIN && !pos.ProfitTaken

// 	if !shouldFire {
// 		t.Error("strong EP should fire within window with sufficient gain")
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // 18. Watchlist lifecycle — create, detect, update, delete
// //
// // These tests exercise the full watchlist pipeline:
// //   writeWatchlist  → checkAndProcessWatchlist detects mod-time change
// //                   → processWatchlist parses rows
// //                   → openPositionFn (stubbed) registers positions
// //
// // One small addition is required in realtime_monitor.go so tests can inject
// // a fake "open position" step without hitting Alpaca:
// //
// //   // Swappable so tests can inject a stub without Alpaca.
// //   var openPositionFn = tryOpenPosition
// //
// // Then in processWatchlist, replace the last line:
// //   go tryOpenPosition(pos)
// // with:
// //   go openPositionFn(pos)
// //
// // That one-line change is the only edit needed in the monitor.
// // ─────────────────────────────────────────────────────────────────────────────

// // ── Watchlist test helpers ────────────────────────────────────────────────────

// // watchlistHeader is the standard CSV header row.
// var watchlistHeader = []string{"Symbol", "EntryPrice", "StopLoss", "Shares", "InitialRisk", "Date"}

// // makeRow builds a valid watchlist data row.
// func makeRow(symbol, entry, stop, shares, risk, date string) []string {
// 	return []string{symbol, entry, stop, shares, risk, date}
// }

// // writeWatchlist writes (or rewrites) a CSV file at path with the given rows
// // (header is prepended automatically).  It also bumps the file's mtime by
// // sleeping 5ms so that consecutive writes in the same test are always detected
// // as "newer" by os.Stat.
// func writeWatchlist(t *testing.T, path string, rows [][]string) {
// 	t.Helper()
// 	f, err := os.Create(path)
// 	if err != nil {
// 		t.Fatalf("writeWatchlist create: %v", err)
// 	}
// 	w := csv.NewWriter(f)
// 	all := append([][]string{watchlistHeader}, rows...)
// 	if err := w.WriteAll(all); err != nil {
// 		t.Fatalf("writeWatchlist write: %v", err)
// 	}
// 	f.Close()
// 	// Tiny sleep so mod-time strictly increases between writes in the same test.
// 	time.Sleep(10 * time.Millisecond)
// }

// // stubOpenPosition replaces openPositionFn with a stub that directly registers
// // the position in activePositions without touching Alpaca.  It returns a
// // restore function that must be deferred.
// //
// // NOTE: add `var openPositionFn = tryOpenPosition` to realtime_monitor.go and
// // change `go tryOpenPosition(pos)` → `go openPositionFn(pos)` in processWatchlist.
// func stubOpenPosition(t *testing.T) func() {
// 	t.Helper()
// 	orig := openPositionFn
// 	openPositionFn = func(pos *RealtimePosition) {
// 		// Apply the same validation logic as tryOpenPosition but skip Alpaca.
// 		if pos.EntryPrice < MIN_PRICE || pos.EntryPrice > MAX_PRICE {
// 			return
// 		}
// 		if pos.StopLoss <= 0 || pos.StopLoss >= pos.EntryPrice {
// 			return
// 		}
// 		pos.InitialStopLoss = pos.StopLoss
// 		pos.InitialRisk = pos.EntryPrice - pos.StopLoss
// 		pos.SessionLow = math.MaxFloat64

// 		positionsMu.Lock()
// 		activePositions[pos.Symbol] = pos
// 		positionsMu.Unlock()
// 	}
// 	return func() { openPositionFn = orig }
// }

// // waitForPositions polls activePositions until wantCount symbols are present
// // or the timeout elapses.  Returns false on timeout.
// func waitForPositions(t *testing.T, wantCount int, timeout time.Duration) bool {
// 	t.Helper()
// 	deadline := time.Now().Add(timeout)
// 	for time.Now().Before(deadline) {
// 		positionsMu.RLock()
// 		n := len(activePositions)
// 		positionsMu.RUnlock()
// 		if n >= wantCount {
// 			return true
// 		}
// 		time.Sleep(5 * time.Millisecond)
// 	}
// 	return false
// }

// // watchlistDir creates a temp directory, writes an initial watchlist.csv,
// // and changes the working directory to it (so checkAndProcessWatchlist finds
// // the file at the default "watchlist.csv" path).  Returns the path and a
// // restore function.
// func watchlistDir(t *testing.T, rows [][]string) (path string, restore func()) {
// 	t.Helper()
// 	dir := t.TempDir()
// 	path = filepath.Join(dir, "watchlist.csv")
// 	writeWatchlist(t, path, rows)

// 	old, _ := os.Getwd()
// 	if err := os.Chdir(dir); err != nil {
// 		t.Fatalf("chdir: %v", err)
// 	}
// 	restore = func() { _ = os.Chdir(old) }
// 	return path, restore
// }

// // ── Tests ─────────────────────────────────────────────────────────────────────

// // TestWatchlist_InitialLoad verifies that creating a watchlist.csv with one
// // symbol causes checkAndProcessWatchlist to pick it up and register the position.
// func TestWatchlist_InitialLoad(t *testing.T) {
// 	resetGlobals()
// 	restore := stubOpenPosition(t)
// 	defer restore()

// 	_, dirRestore := watchlistDir(t, [][]string{
// 		makeRow("AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"),
// 	})
// 	defer dirRestore()

// 	checkAndProcessWatchlist()

// 	if !waitForPositions(t, 1, 500*time.Millisecond) {
// 		t.Fatal("AAPL should have been registered after initial watchlist load")
// 	}

// 	positionsMu.RLock()
// 	pos, ok := activePositions["AAPL"]
// 	positionsMu.RUnlock()

// 	if !ok {
// 		t.Fatal("AAPL not found in activePositions")
// 	}
// 	if pos.EntryPrice != 150.00 {
// 		t.Errorf("entry: got %.2f want 150.00", pos.EntryPrice)
// 	}
// 	if pos.StopLoss != 140.00 {
// 		t.Errorf("stop: got %.2f want 140.00", pos.StopLoss)
// 	}
// 	if pos.InitialRisk != 10.00 {
// 		t.Errorf("risk: got %.2f want 10.00", pos.InitialRisk)
// 	}
// }

// // TestWatchlist_MultipleSymbolsOnInitialLoad verifies all rows are processed
// // when a watchlist is created with several symbols at once.
// func TestWatchlist_MultipleSymbolsOnInitialLoad(t *testing.T) {
// 	resetGlobals()
// 	restore := stubOpenPosition(t)
// 	defer restore()

// 	_, dirRestore := watchlistDir(t, [][]string{
// 		makeRow("AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"),
// 		makeRow("NVDA", "90.00",  "80.00",  "20",  "10.00", "2024-01-15"),
// 		makeRow("TSLA", "200.00", "185.00", "50",  "15.00", "2024-01-15"),
// 	})
// 	defer dirRestore()

// 	checkAndProcessWatchlist()

// 	if !waitForPositions(t, 3, 1*time.Second) {
// 		positionsMu.RLock()
// 		have := len(activePositions)
// 		positionsMu.RUnlock()
// 		t.Fatalf("expected 3 positions, got %d", have)
// 	}

// 	for _, sym := range []string{"AAPL", "NVDA", "TSLA"} {
// 		positionsMu.RLock()
// 		_, ok := activePositions[sym]
// 		positionsMu.RUnlock()
// 		if !ok {
// 			t.Errorf("%s not registered", sym)
// 		}
// 	}
// }

// // TestWatchlist_AddNewSymbol simulates a user adding a row to an existing
// // watchlist.  The first call loads the original rows; the second call (after
// // a file update) should detect the new entry and register only it.
// func TestWatchlist_AddNewSymbol(t *testing.T) {
// 	resetGlobals()
// 	restore := stubOpenPosition(t)
// 	defer restore()

// 	path, dirRestore := watchlistDir(t, [][]string{
// 		makeRow("AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"),
// 	})
// 	defer dirRestore()

// 	// First pass — load AAPL
// 	checkAndProcessWatchlist()
// 	if !waitForPositions(t, 1, 500*time.Millisecond) {
// 		t.Fatal("initial AAPL load failed")
// 	}

// 	// Update: add NVDA
// 	writeWatchlist(t, path, [][]string{
// 		makeRow("AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"),
// 		makeRow("NVDA",  "90.00",  "80.00",  "20",  "10.00", "2024-01-16"),
// 	})

// 	checkAndProcessWatchlist()

// 	if !waitForPositions(t, 2, 500*time.Millisecond) {
// 		t.Fatal("NVDA should have been added after watchlist update")
// 	}

// 	positionsMu.RLock()
// 	_, nvdaOk := activePositions["NVDA"]
// 	positionsMu.RUnlock()

// 	if !nvdaOk {
// 		t.Error("NVDA not found in activePositions after update")
// 	}
// }

// // TestWatchlist_NoChangeDetected verifies that calling checkAndProcessWatchlist
// // twice without modifying the file does NOT re-process already registered symbols.
// func TestWatchlist_NoChangeDetected(t *testing.T) {
// 	resetGlobals()

// 	var callCount int32 // atomic — written by goroutine, read by test goroutine
// 	origFn := openPositionFn
// 	openPositionFn = func(pos *RealtimePosition) {
// 		atomic.AddInt32(&callCount, 1)
// 		if pos.EntryPrice < MIN_PRICE || pos.EntryPrice > MAX_PRICE { return }
// 		if pos.StopLoss <= 0 || pos.StopLoss >= pos.EntryPrice { return }
// 		pos.InitialStopLoss = pos.StopLoss
// 		pos.InitialRisk = pos.EntryPrice - pos.StopLoss
// 		positionsMu.Lock()
// 		activePositions[pos.Symbol] = pos
// 		positionsMu.Unlock()
// 	}
// 	defer func() { openPositionFn = origFn }()

// 	_, dirRestore := watchlistDir(t, [][]string{
// 		makeRow("AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"),
// 	})
// 	defer dirRestore()

// 	checkAndProcessWatchlist() // first call — should process
// 	time.Sleep(50 * time.Millisecond)
// 	firstCount := atomic.LoadInt32(&callCount)

// 	checkAndProcessWatchlist() // second call — file unchanged, should be a no-op
// 	time.Sleep(50 * time.Millisecond)

// 	if atomic.LoadInt32(&callCount) != firstCount {
// 		t.Errorf("openPositionFn called %d times on second check (should be 0 new calls)",
// 			atomic.LoadInt32(&callCount)-firstCount)
// 	}
// }

// // TestWatchlist_SymbolAlreadyActive_NotReprocessed verifies that a symbol which
// // is already in activePositions is not registered a second time even if the
// // watchlist file is updated.
// func TestWatchlist_SymbolAlreadyActive_NotReprocessed(t *testing.T) {
// 	resetGlobals()

// 	var callCount int32
// 	origFn := openPositionFn
// 	openPositionFn = func(pos *RealtimePosition) {
// 		atomic.AddInt32(&callCount, 1)
// 		positionsMu.Lock()
// 		activePositions[pos.Symbol] = pos
// 		positionsMu.Unlock()
// 	}
// 	defer func() { openPositionFn = origFn }()

// 	// Pre-seed AAPL as already active
// 	positionsMu.Lock()
// 	activePositions["AAPL"] = newPos("AAPL", 150, 140, 100)
// 	positionsMu.Unlock()

// 	path, dirRestore := watchlistDir(t, [][]string{
// 		makeRow("AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"),
// 	})
// 	defer dirRestore()

// 	writeWatchlist(t, path, [][]string{
// 		makeRow("AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"),
// 	})
// 	checkAndProcessWatchlist()
// 	time.Sleep(50 * time.Millisecond)

// 	if atomic.LoadInt32(&callCount) > 0 {
// 		t.Errorf("openPositionFn should not be called for already-active symbol, called %d times",
// 			atomic.LoadInt32(&callCount))
// 	}
// }

// // TestWatchlist_InvalidRowSkipped verifies that a row with bad data does not
// // block valid rows from being registered.
// func TestWatchlist_InvalidRowSkipped(t *testing.T) {
// 	resetGlobals()
// 	restore := stubOpenPosition(t)
// 	defer restore()

// 	_, dirRestore := watchlistDir(t, [][]string{
// 		{"BROKEN", "not-a-price", "140.00", "100", "10.00", "2024-01-15"}, // invalid
// 		makeRow("NVDA", "90.00",  "80.00",  "20", "10.00", "2024-01-15"),   // valid
// 	})
// 	defer dirRestore()

// 	checkAndProcessWatchlist()

// 	if !waitForPositions(t, 1, 500*time.Millisecond) {
// 		t.Fatal("NVDA should have been registered despite invalid preceding row")
// 	}

// 	positionsMu.RLock()
// 	_, brokenOk := activePositions["BROKEN"]
// 	_, nvdaOk := activePositions["NVDA"]
// 	positionsMu.RUnlock()

// 	if brokenOk {
// 		t.Error("BROKEN should not have been registered")
// 	}
// 	if !nvdaOk {
// 		t.Error("NVDA should have been registered")
// 	}
// }

// // TestWatchlist_PriceFilterRejectsOutOfRange verifies that a symbol whose
// // price is outside [MIN_PRICE, MAX_PRICE] is silently skipped.
// func TestWatchlist_PriceFilterRejectsOutOfRange(t *testing.T) {
// 	resetGlobals()
// 	restore := stubOpenPosition(t)
// 	defer restore()

// 	_, dirRestore := watchlistDir(t, [][]string{
// 		makeRow("PENNY",   "1.50",   "1.00", "1000", "0.50", "2024-01-15"), // too cheap
// 		makeRow("BIGCO",   "250.00", "230.00", "10", "20.00", "2024-01-15"), // too expensive
// 		makeRow("GOODONE", "50.00",  "45.00",  "50", "5.00",  "2024-01-15"), // valid
// 	})
// 	defer dirRestore()

// 	checkAndProcessWatchlist()

// 	if !waitForPositions(t, 1, 500*time.Millisecond) {
// 		t.Fatal("GOODONE should be registered")
// 	}

// 	positionsMu.RLock()
// 	_, pennyOk := activePositions["PENNY"]
// 	_, bigOk   := activePositions["BIGCO"]
// 	_, goodOk  := activePositions["GOODONE"]
// 	positionsMu.RUnlock()

// 	if pennyOk   { t.Error("PENNY (price too low) should be filtered out") }
// 	if bigOk     { t.Error("BIGCO (price too high) should be filtered out") }
// 	if !goodOk   { t.Error("GOODONE should pass the price filter") }
// }

// // TestWatchlist_StopAboveEntryRejected verifies that a row where StopLoss >= EntryPrice
// // is rejected by the validation inside openPositionFn.
// func TestWatchlist_StopAboveEntryRejected(t *testing.T) {
// 	resetGlobals()
// 	restore := stubOpenPosition(t)
// 	defer restore()

// 	_, dirRestore := watchlistDir(t, [][]string{
// 		makeRow("INVSTOP", "100.00", "110.00", "50", "10.00", "2024-01-15"), // stop > entry
// 		makeRow("VALID",   "100.00",  "90.00", "50", "10.00", "2024-01-15"),
// 	})
// 	defer dirRestore()

// 	checkAndProcessWatchlist()
// 	if !waitForPositions(t, 1, 500*time.Millisecond) {
// 		t.Fatal("VALID should be registered")
// 	}

// 	positionsMu.RLock()
// 	_, invOk   := activePositions["INVSTOP"]
// 	_, validOk := activePositions["VALID"]
// 	positionsMu.RUnlock()

// 	if invOk   { t.Error("INVSTOP (stop > entry) should be rejected") }
// 	if !validOk { t.Error("VALID should be registered") }
// }

// // TestWatchlist_RemoveSymbolFromFile verifies that removeFromWatchlist
// // correctly strips a closed position from the CSV while leaving others intact,
// // and that a subsequent checkAndProcessWatchlist does not re-add the removed symbol.
// func TestWatchlist_RemoveSymbolFromFile(t *testing.T) {
// 	resetGlobals()
// 	restore := stubOpenPosition(t)
// 	defer restore()

// 	path, dirRestore := watchlistDir(t, [][]string{
// 		makeRow("AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"),
// 		makeRow("TSLA", "200.00", "185.00",  "50", "15.00", "2024-01-15"),
// 	})
// 	defer dirRestore()

// 	watchlistPath = path
// 	checkAndProcessWatchlist()
// 	if !waitForPositions(t, 2, 500*time.Millisecond) {
// 		t.Fatal("initial load of 2 positions failed")
// 	}

// 	// Simulate AAPL being stopped out — remove it from the file
// 	removeFromWatchlist("AAPL")

// 	// Read back the file and assert AAPL is gone
// 	f, _ := os.Open(path)
// 	records, _ := csv.NewReader(f).ReadAll()
// 	f.Close()

// 	for _, rec := range records[1:] {
// 		if rec[0] == "AAPL" {
// 			t.Error("AAPL should have been removed from watchlist.csv")
// 		}
// 	}

// 	// TSLA should still be present
// 	found := false
// 	for _, rec := range records[1:] {
// 		if rec[0] == "TSLA" {
// 			found = true
// 		}
// 	}
// 	if !found {
// 		t.Error("TSLA should remain in watchlist.csv after removing AAPL")
// 	}

// 	// A subsequent check should not re-register AAPL (it's in processedSymbols)
// 	positionsMu.Lock()
// 	delete(activePositions, "AAPL") // simulate it being removed from active map
// 	positionsMu.Unlock()

// 	writeWatchlist(t, path, [][]string{
// 		makeRow("TSLA", "200.00", "185.00", "50", "15.00", "2024-01-15"),
// 	})
// 	checkAndProcessWatchlist()
// 	time.Sleep(50 * time.Millisecond)

// 	positionsMu.RLock()
// 	_, aaplBack := activePositions["AAPL"]
// 	positionsMu.RUnlock()

// 	if aaplBack {
// 		t.Error("AAPL should not be re-registered after being removed from watchlist")
// 	}
// }

// // TestWatchlist_SequentialUpdates adds three symbols one at a time, verifying
// // the monitor picks up each update independently without duplicating entries.
// func TestWatchlist_SequentialUpdates(t *testing.T) {
// 	resetGlobals()
// 	restore := stubOpenPosition(t)
// 	defer restore()

// 	path, dirRestore := watchlistDir(t, [][]string{
// 		makeRow("AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"),
// 	})
// 	defer dirRestore()

// 	checkAndProcessWatchlist()
// 	if !waitForPositions(t, 1, 500*time.Millisecond) {
// 		t.Fatal("step 1: AAPL not loaded")
// 	}

// 	// Step 2: add NVDA
// 	writeWatchlist(t, path, [][]string{
// 		makeRow("AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"),
// 		makeRow("NVDA", "90.00",  "80.00",   "20", "10.00", "2024-01-16"),
// 	})
// 	checkAndProcessWatchlist()
// 	if !waitForPositions(t, 2, 500*time.Millisecond) {
// 		t.Fatal("step 2: NVDA not loaded")
// 	}

// 	// Step 3: add META
// 	writeWatchlist(t, path, [][]string{
// 		makeRow("AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"),
// 		makeRow("NVDA", "90.00",  "80.00",   "20", "10.00", "2024-01-16"),
// 		makeRow("META", "80.00",  "70.00",   "30", "10.00", "2024-01-17"),
// 	})
// 	checkAndProcessWatchlist()
// 	if !waitForPositions(t, 3, 500*time.Millisecond) {
// 		t.Fatal("step 3: META not loaded")
// 	}

// 	// Final state check — no duplicates, all three present
// 	positionsMu.RLock()
// 	total := len(activePositions)
// 	_, aaplOk := activePositions["AAPL"]
// 	_, nvdaOk := activePositions["NVDA"]
// 	_, metaOk := activePositions["META"]
// 	positionsMu.RUnlock()

// 	if total != 3 {
// 		t.Errorf("expected exactly 3 positions, got %d", total)
// 	}
// 	if !aaplOk { t.Error("AAPL missing") }
// 	if !nvdaOk { t.Error("NVDA missing") }
// 	if !metaOk { t.Error("META missing") }
// }

// // TestWatchlist_ClearAndReload verifies that clearing the watchlist file
// // (leaving only the header) and then adding new symbols works correctly.
// func TestWatchlist_ClearAndReload(t *testing.T) {
// 	resetGlobals()
// 	restore := stubOpenPosition(t)
// 	defer restore()

// 	path, dirRestore := watchlistDir(t, [][]string{
// 		makeRow("AAPL", "150.00", "140.00", "100", "10.00", "2024-01-15"),
// 	})
// 	defer dirRestore()

// 	watchlistPath = path
// 	checkAndProcessWatchlist()
// 	if !waitForPositions(t, 1, 500*time.Millisecond) {
// 		t.Fatal("initial load failed")
// 	}

// 	// Clear the watchlist (header only)
// 	writeWatchlist(t, path, [][]string{}) // empty rows → only header written
// 	checkAndProcessWatchlist()
// 	time.Sleep(30 * time.Millisecond)

// 	// Now add a brand new symbol
// 	writeWatchlist(t, path, [][]string{
// 		makeRow("AMD", "120.00", "112.00", "80", "8.00", "2024-02-01"),
// 	})
// 	checkAndProcessWatchlist()
// 	if !waitForPositions(t, 2, 500*time.Millisecond) {
// 		// 2 = AAPL (still active) + AMD (new)
// 		positionsMu.RLock()
// 		n := len(activePositions)
// 		positionsMu.RUnlock()
// 		t.Fatalf("expected 2 total positions (AAPL + AMD), got %d", n)
// 	}

// 	positionsMu.RLock()
// 	_, amdOk := activePositions["AMD"]
// 	positionsMu.RUnlock()

// 	if !amdOk {
// 		t.Error("AMD should be registered after re-adding to cleared watchlist")
// 	}
// }

// // TestWatchlist_PositionFieldsCorrectlyParsed validates that every field from
// // the CSV row lands correctly on the registered RealtimePosition.
// func TestWatchlist_PositionFieldsCorrectlyParsed(t *testing.T) {
// 	resetGlobals()
// 	restore := stubOpenPosition(t)
// 	defer restore()

// 	_, dirRestore := watchlistDir(t, [][]string{
// 		makeRow("GOOG", "175.50", "165.00", "40", "10.50", "2024-03-20"),
// 	})
// 	defer dirRestore()

// 	checkAndProcessWatchlist()
// 	if !waitForPositions(t, 1, 500*time.Millisecond) {
// 		t.Fatal("GOOG not registered")
// 	}

// 	positionsMu.RLock()
// 	pos := activePositions["GOOG"]
// 	positionsMu.RUnlock()

// 	if pos == nil {
// 		t.Fatal("GOOG position is nil")
// 	}

// 	tests := []struct {
// 		field string
// 		got   float64
// 		want  float64
// 	}{
// 		{"EntryPrice",    pos.EntryPrice,    175.50},
// 		{"StopLoss",      pos.StopLoss,      165.00},
// 		{"Shares",        pos.Shares,        40.00},
// 		{"InitialShares", pos.InitialShares, 40.00},
// 		{"InitialRisk",   pos.InitialRisk,   10.50},
// 		{"HighestPrice",  pos.HighestPrice,  175.50},
// 	}

// 	for _, tc := range tests {
// 		if math.Abs(tc.got-tc.want) > 0.001 {
// 			t.Errorf("%s: got %.4f want %.4f", tc.field, tc.got, tc.want)
// 		}
// 	}

// 	if pos.PurchaseDate.Format("2006-01-02") != "2024-03-20" {
// 		t.Errorf("PurchaseDate: got %s want 2024-03-20", pos.PurchaseDate.Format("2006-01-02"))
// 	}
// 	if pos.Symbol != "GOOG" {
// 		t.Errorf("Symbol: got %s", pos.Symbol)
// 	}
// 	// Profit flags start false
// 	if pos.ProfitTaken || pos.ProfitTaken2 || pos.ProfitTaken3 {
// 		t.Error("profit flags should all be false on a fresh position")
// 	}
// 	if pos.TrailingStopMode {
// 		t.Error("TrailingStopMode should be false on a fresh position")
// 	}
// }

// // TestWatchlist_DollarSignsInCSV ensures the parser handles prices written
// // with a leading $ (common when copy-pasting from broker UIs).
// func TestWatchlist_DollarSignsInCSV(t *testing.T) {
// 	resetGlobals()
// 	restore := stubOpenPosition(t)
// 	defer restore()

// 	_, dirRestore := watchlistDir(t, [][]string{
// 		{"MSFT", "$150.00", "$140.00", "25", "$10.00", "2024-04-01"},
// 	})
// 	defer dirRestore()

// 	checkAndProcessWatchlist()
// 	if !waitForPositions(t, 1, 500*time.Millisecond) {
// 		t.Fatal("MSFT with $ prices not registered")
// 	}

// 	positionsMu.RLock()
// 	pos := activePositions["MSFT"]
// 	positionsMu.RUnlock()

// 	if pos == nil {
// 		t.Fatal("MSFT position nil")
// 	}
// 	if pos.EntryPrice != 150.00 {
// 		t.Errorf("entry price with $ prefix: got %.2f want 150.00", pos.EntryPrice)
// 	}
// }

// // TestWatchlist_ConcurrentUpdates verifies that evaluatePositions handles
// // multiple positions concurrently without data races. Five positions are
// // registered up-front and then evaluated simultaneously.
// func TestWatchlist_ConcurrentUpdates(t *testing.T) {
// 	resetGlobals()
// 	setupEasternLoc(t)
// 	restore := stubOpenPosition(t)
// 	defer restore()

// 	symbols := []string{"S1", "S2", "S3", "S4", "S5"}
// 	rows := make([][]string, len(symbols))
// 	for i, sym := range symbols {
// 		rows[i] = makeRow(sym, "50.00", "45.00", "100", "5.00", "2024-05-01")
// 	}

// 	// Write all 5 symbols in one atomic file write, then process once.
// 	path, dirRestore := watchlistDir(t, rows)
// 	defer dirRestore()
// 	watchlistPath = path

// 	checkAndProcessWatchlist()
// 	if !waitForPositions(t, len(symbols), 2*time.Second) {
// 		positionsMu.RLock()
// 		n := len(activePositions)
// 		positionsMu.RUnlock()
// 		t.Fatalf("expected %d positions after bulk load, got %d", len(symbols), n)
// 	}

// 	// Now exercise evaluatePositions concurrently — stub out Alpaca + bars so
// 	// it runs purely in-process and the race detector can catch any locking gap.
// 	restoreDeps := stubDeps(t, 100, makeBars(50, 55, 48, 52))
// 	defer restoreDeps()

// 	// Run three concurrent evaluation passes.
// 	var wg sync.WaitGroup
// 	for pass := 0; pass < 3; pass++ {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			evaluatePositions()
// 		}()
// 	}
// 	wg.Wait()

// 	// All positions should still be alive (price well above stop, no exit triggered).
// 	positionsMu.RLock()
// 	n := len(activePositions)
// 	positionsMu.RUnlock()
// 	if n != len(symbols) {
// 		t.Errorf("expected %d positions after concurrent evaluation, got %d", len(symbols), n)
// 	}
// }

// // TestWatchlist_WatchlistPathSetOnFirstDetection verifies that
// // checkAndProcessWatchlist sets the global watchlistPath the first time it
// // finds the file, so that removeFromWatchlist can use it later.
// func TestWatchlist_WatchlistPathSetOnFirstDetection(t *testing.T) {
// 	resetGlobals()

// 	// Start in a dir that has watchlist.csv
// 	dir := t.TempDir()
// 	path := filepath.Join(dir, "watchlist.csv")
// 	writeWatchlist(t, path, [][]string{})

// 	old, _ := os.Getwd()
// 	_ = os.Chdir(dir)
// 	defer os.Chdir(old)

// 	checkAndProcessWatchlist()

// 	if watchlistPath == "" {
// 		t.Error("watchlistPath should be set after first successful detection")
// 	}
// }

// // TestWatchlist_MissingFile_NoOp verifies that checkAndProcessWatchlist
// // returns gracefully when no watchlist.csv exists at either search path.
// func TestWatchlist_MissingFile_NoOp(t *testing.T) {
// 	resetGlobals()

// 	// Use a temp dir that has NO watchlist.csv
// 	dir := t.TempDir()
// 	old, _ := os.Getwd()
// 	_ = os.Chdir(dir)
// 	defer os.Chdir(old)

// 	// Must not panic
// 	checkAndProcessWatchlist()

// 	positionsMu.RLock()
// 	n := len(activePositions)
// 	positionsMu.RUnlock()

// 	if n != 0 {
// 		t.Errorf("no positions should be registered when file is missing, got %d", n)
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // Helpers
// // ─────────────────────────────────────────────────────────────────────────────

// func parseFloat(s string) (float64, error) {
// 	return strconv.ParseFloat(s, 64)
// }