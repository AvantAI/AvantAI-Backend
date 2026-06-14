package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"path/filepath"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"

	ep "avantai/pkg/ep" // ← update to match your go.mod module path
)

// ─────────────────────────────────────────────────────────────────────────────
// Constants
// ─────────────────────────────────────────────────────────────────────────────

const (
	// How often to poll DURING the opening window (first OPENING_WINDOW_MINUTES).
	POLL_INTERVAL = 60 * time.Second

	// Polling schedule (Eastern Time, relative to the regular session):
	//   • First OPENING_WINDOW_MINUTES after the open → poll every POLL_INTERVAL.
	//   • Then only two checkpoints: open + MIDDAY_CHECK_MINUTES, and
	//     close - CLOSING_CHECK_MINUTES.
	OPENING_WINDOW_MINUTES = 15 // 09:30–09:45: watch every minute for new fills
	MIDDAY_CHECK_MINUTES   = 30 // 10:00 checkpoint
	CLOSING_CHECK_MINUTES  = 5  // 15:55 checkpoint

	// Stop-loss for an Alpaca-discovered position = the lowest low of the
	// STOP_LOOKBACK_MINUTES of 1-minute bars immediately before the fill.
	STOP_LOOKBACK_MINUTES = 15

	// When a sell is rejected because the shares are reserved by a working exit
	// order (e.g. an existing bracket/GTC stop):
	//   • false (default, safe) → defer to that order; reconcile its real fill
	//     into trade_results.csv if it has already filled, otherwise stop
	//     monitoring without fabricating a record.
	//   • true → cancel the working order, wait RESERVED_RETRY_DELAY for the
	//     shares to free up, and retry the sell once. WARNING: this removes the
	//     protective stop; if the retry also fails the position is left
	//     unprotected. Only enable if you are confident the retry will fill.
	FORCE_EXIT_ON_RESERVED = false
	RESERVED_RETRY_DELAY   = 1 * time.Second

	// Exit / stop logic (mirrors backtest)
	BREAKEVEN_TRIGGER_PERCENT = 0.02
	BREAKEVEN_TRIGGER_DAYS    = 2

	PROFIT_TAKE_1_RR      = 1.5
	PROFIT_TAKE_1_PERCENT = 0.25
	PROFIT_TAKE_2_RR      = 3.0
	PROFIT_TAKE_2_PERCENT = 0.25
	PROFIT_TAKE_3_RR      = 5.0
	PROFIT_TAKE_3_PERCENT = 0.25

	STRONG_EP_GAIN         = 0.15
	STRONG_EP_DAYS         = 3
	STRONG_EP_TAKE_PERCENT = 0.30

	MAX_DAYS_NO_FOLLOWTHROUGH = 8

	// Weak close: if close is more than 30% below session high, exit
	WEAK_CLOSE_THRESHOLD = 0.30

	// Entry filters
	MIN_PRICE = 2.0
	MAX_PRICE = 200.0

	// NYSE / NASDAQ regular session (Eastern Time)
	MARKET_OPEN_HOUR  = 9
	MARKET_OPEN_MIN   = 30
	MARKET_CLOSE_HOUR = 16
	MARKET_CLOSE_MIN  = 0
	EASTERN_TZ        = "America/New_York"

	// Alpaca rejects a second sell while shares are committed to another order.
	ALPACA_INSUFFICIENT_QTY_CODE = 40310000
)

// ─────────────────────────────────────────────────────────────────────────────
// Data structures
// ─────────────────────────────────────────────────────────────────────────────

// RealtimePosition tracks a live position and all of its exit-rule state.
type RealtimePosition struct {
	Symbol          string
	EntryPrice      float64
	StopLoss        float64
	InitialStopLoss float64
	InitialRisk     float64
	Shares          float64 // decremented on partial exits
	InitialShares   float64
	PurchaseDate    time.Time
	DaysHeld        int
	LastCheckDate   time.Time // tracks last calendar date we incremented DaysHeld

	// Profit-taking flags
	ProfitTaken  bool
	ProfitTaken2 bool
	ProfitTaken3 bool

	// Trailing / advanced stop state
	TrailingStopMode bool
	HighestPrice     float64 // highest intraday high seen since entry

	// Running tally of realised profit from partial exits
	CumulativeProfit float64

	// Per-session daily OHLC — reset each morning at open
	SessionHigh float64
	SessionLow  float64
	SessionOpen float64

	// Set true on EP day if a weak close was detected
	WeakCloseDetected bool

	StopOrderID string // tracks the current GTC stop order in Alpaca

	mu sync.Mutex
}

// TradeRecord is appended to trade_results.csv on every full or partial exit.
type TradeRecord struct {
	Symbol      string
	EntryPrice  float64
	ExitPrice   float64
	Shares      float64
	InitialRisk float64
	ProfitLoss  float64
	RiskReward  float64
	EntryDate   string
	ExitDate    string
	ExitReason  string
	IsWinner    bool
}

// ─────────────────────────────────────────────────────────────────────────────
// Global state
// ─────────────────────────────────────────────────────────────────────────────

var (
	activePositions  = make(map[string]*RealtimePosition)
	processedSymbols = make(map[string]bool)
	finalizedSymbols = make(map[string]bool) // symbols retired for the trading day
	lastWatchlistMod time.Time
	watchlistPath    string
	lastResetDay     string

	positionsMu    sync.RWMutex
	processedMu    sync.Mutex
	finalizedMu    sync.Mutex
	tradeResultsMu sync.Mutex

	alpacaClient *alpaca.Client
	mdClient     *marketdata.Client

	easternLoc *time.Location
)

// ─────────────────────────────────────────────────────────────────────────────
// Injectable function variables
// Tests swap these for fakes so no real network calls are made.
// ─────────────────────────────────────────────────────────────────────────────

// Bar is a single OHLCV bar.  Mirrors marketdata.Bar so tests need not import
// the Alpaca SDK.
type Bar struct {
	Open, High, Low, Close float64
}

// getAlpacaPositionFn returns the share quantity held in Alpaca for symbol,
// or an error if the position does not exist.
var getAlpacaPositionFn = func(symbol string) (float64, error) {
	pos, err := alpacaClient.GetPosition(symbol)
	if err != nil {
		return 0, err
	}
	qty, _ := pos.Qty.Float64()
	return qty, nil
}

// getAllAlpacaPositionsFn returns every open position (symbol, qty, avg entry).
var getAllAlpacaPositionsFn = func() ([]AlpacaPosition, error) {
	raw, err := alpacaClient.GetPositions()
	if err != nil {
		return nil, err
	}
	out := make([]AlpacaPosition, 0, len(raw))
	for _, p := range raw {
		qty, _ := p.Qty.Float64()
		entry, _ := p.AvgEntryPrice.Float64()
		out = append(out, AlpacaPosition{Symbol: p.Symbol, Qty: qty, AvgEntry: entry})
	}
	return out, nil
}

// AlpacaPosition is a thin, SDK-free view of a held position.
type AlpacaPosition struct {
	Symbol   string
	Qty      float64
	AvgEntry float64
}

// getIntradayBarsFn returns 1-minute bars for symbol from start to end.
var getIntradayBarsFn = func(symbol string, start, end time.Time) ([]Bar, error) {
	raw, err := mdClient.GetBars(symbol, marketdata.GetBarsRequest{
		TimeFrame: marketdata.OneMin,
		Start:     start,
		End:       end,
		Feed:      marketdata.IEX,
	})
	if err != nil {
		return nil, err
	}
	bars := make([]Bar, len(raw))
	for i, b := range raw {
		bars[i] = Bar{Open: b.Open, High: b.High, Low: b.Low, Close: b.Close}
	}
	return bars, nil
}

// getEntryTimeFn returns the fill time of the most recent filled BUY for symbol.
var getEntryTimeFn = func(symbol string) (time.Time, error) {
	orders, err := alpacaClient.GetOrders(alpaca.GetOrdersRequest{
		Status:    "closed",
		Symbols:   []string{symbol},
		Until:     time.Now(),
		Limit:     50,
		Direction: "desc",
	})
	if err != nil {
		return time.Time{}, err
	}
	for _, o := range orders {
		if o.Side == alpaca.Buy && o.FilledAt != nil && !o.FilledAt.IsZero() {
			return *o.FilledAt, nil
		}
	}
	return time.Time{}, fmt.Errorf("no filled buy order for %s", symbol)
}

// getClosedSellFillFn returns the most recent filled SELL (price + time) for a
// symbol, used to reconcile an exit that Alpaca executed via a working order.
var getClosedSellFillFn = func(symbol string) (price float64, filledAt time.Time, ok bool, err error) {
	orders, err := alpacaClient.GetOrders(alpaca.GetOrdersRequest{
		Status:    "closed",
		Symbols:   []string{symbol},
		Until:     time.Now(),
		Limit:     50,
		Direction: "desc",
	})
	if err != nil {
		return 0, time.Time{}, false, err
	}
	for _, o := range orders {
		if o.Side == alpaca.Sell && o.FilledAt != nil && !o.FilledAt.IsZero() && o.FilledAvgPrice != nil {
			fp, _ := o.FilledAvgPrice.Float64()
			return fp, *o.FilledAt, true, nil
		}
	}
	return 0, time.Time{}, false, nil
}

// placeSellOrderFn places a market sell. Wraps ep.PlaceSellOrder so tests can fake it.
var placeSellOrderFn = func(symbol string, shares int, price *float64) error {
	_, err := ep.PlaceSellOrder(symbol, shares, price)
	return err
}

// cancelOpenOrdersFn cancels every open order for a symbol (frees reserved shares).
var cancelOpenOrdersFn = func(symbol string) error {
	orders, err := alpacaClient.GetOrders(alpaca.GetOrdersRequest{
		Status:  "open",
		Symbols: []string{symbol},
		Limit:   100,
	})
	if err != nil {
		return err
	}
	var firstErr error
	for _, o := range orders {
		if err := alpacaClient.CancelOrder(o.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// openPositionFn is called by processWatchlist for each new symbol.
var openPositionFn = tryOpenPosition

// AccountSnapshot holds the key numbers we care about from Alpaca.
type AccountSnapshot struct {
	Equity      float64
	Cash        float64
	BuyingPower float64
	DayPL       float64
}

// getAccountFn fetches account figures from Alpaca.
var getAccountFn = func() (*AccountSnapshot, error) {
	acct, err := alpacaClient.GetAccount()
	if err != nil {
		return nil, err
	}
	equity, _ := acct.Equity.Float64()
	cash, _ := acct.Cash.Float64()
	bp, _ := acct.BuyingPower.Float64()
	lastEquity, _ := acct.LastEquity.Float64()
	dayPL := equity - lastEquity
	return &AccountSnapshot{Equity: equity, Cash: cash, BuyingPower: bp, DayPL: dayPL}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// main
// ─────────────────────────────────────────────────────────────────────────────

// loadEnv walks up from the working directory until it finds a .env file.
func loadEnv() {
	dir, err := os.Getwd()
	if err != nil {
		log.Println("Warning: cannot determine working directory")
		return
	}
	for {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			if err := godotenv.Load(candidate); err != nil {
				log.Printf("Warning: found .env at %s but could not load it: %v", candidate, err)
			}
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			log.Println("Warning: .env file not found in any parent directory")
			return
		}
		dir = parent
	}
}

func main() {
	loadEnv()

	apiKey := os.Getenv("ALPACA_API_KEY")
	apiSecret := os.Getenv("ALPACA_SECRET_KEY")
	baseURL := os.Getenv("ALPACA_PAPER_URL")

	if apiKey == "" || apiSecret == "" {
		log.Fatal("ALPACA_API_KEY and ALPACA_SECRET_KEY must be set")
	}

	alpacaClient = alpaca.NewClient(alpaca.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
		BaseURL:   baseURL,
	})
	mdClient = marketdata.NewClient(marketdata.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
	})

	var err error
	easternLoc, err = time.LoadLocation(EASTERN_TZ)
	if err != nil {
		log.Fatalf("Cannot load timezone %s: %v", EASTERN_TZ, err)
	}

	initTradeResultsFile()
	printAccountStatus("startup")

	log.Println("🚀 Real-time EP monitor started.")
	log.Printf("   Schedule: every minute for the first %d min, then at open+%dm and close-%dm.",
		OPENING_WINDOW_MINUTES, MIDDAY_CHECK_MINUTES, CLOSING_CHECK_MINUTES)
	log.Println("   Process sleeps automatically outside those windows.")
	log.Println("   Press Ctrl+C to stop.")

	// ── Main loop ─────────────────────────────────────────────────────────────
	for {
		maybeResetDaily(time.Now())

		process, sleep := planNextCycle(time.Now())
		if process {
			scanAlpacaPositions()      // discover new Alpaca fills with a real stop
			checkAndProcessWatchlist() // optional CSV-driven discovery
			evaluatePositions()
		}

		if sleep < time.Second {
			sleep = time.Second
		}
		log.Printf("⏳ Next check in %s", sleep.Round(time.Second))
		time.Sleep(sleep)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Schedule
// ─────────────────────────────────────────────────────────────────────────────

func marketOpen(t time.Time) time.Time {
	t = t.In(easternLoc)
	return time.Date(t.Year(), t.Month(), t.Day(), MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, easternLoc)
}

func marketClose(t time.Time) time.Time {
	t = t.In(easternLoc)
	return time.Date(t.Year(), t.Month(), t.Day(), MARKET_CLOSE_HOUR, MARKET_CLOSE_MIN, 0, 0, easternLoc)
}

func isMarketOpen() bool {
	now := time.Now().In(easternLoc)
	if wd := now.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	return now.After(marketOpen(now)) && now.Before(marketClose(now))
}

// nextMarketOpen returns the next weekday 09:30 ET after now.
func nextMarketOpen() time.Time { return nextMarketOpenFrom(time.Now()) }

func nextMarketOpenFrom(now time.Time) time.Time {
	now = now.In(easternLoc)
	candidate := time.Date(now.Year(), now.Month(), now.Day(),
		MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, easternLoc)
	if now.Before(candidate) && now.Weekday() != time.Saturday && now.Weekday() != time.Sunday {
		return candidate
	}
	for {
		candidate = candidate.AddDate(0, 0, 1)
		if candidate.Weekday() != time.Saturday && candidate.Weekday() != time.Sunday {
			return candidate
		}
	}
}

// planNextCycle decides whether to run a check/evaluate pass now and how long to
// sleep afterward.
//   - Closed / weekend / pre-open / post-close → don't process; sleep to next open.
//   - First OPENING_WINDOW_MINUTES → process every POLL_INTERVAL.
//   - Rest of session → process only at open+MIDDAY and close-CLOSING.
func planNextCycle(now time.Time) (process bool, sleep time.Duration) {
	now = now.In(easternLoc)

	if wd := now.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false, nextMarketOpenFrom(now).Sub(now)
	}

	open := marketOpen(now)
	close_ := marketClose(now)
	if now.Before(open) || !now.Before(close_) {
		return false, nextMarketOpenFrom(now).Sub(now)
	}

	openingEnd := open.Add(OPENING_WINDOW_MINUTES * time.Minute) // 09:45
	midday := open.Add(MIDDAY_CHECK_MINUTES * time.Minute)       // 10:00
	closing := close_.Add(-CLOSING_CHECK_MINUTES * time.Minute)  // 15:55

	// Phase 1: opening window — poll every minute.
	if now.Before(openingEnd) {
		return true, POLL_INTERVAL
	}

	// Phase 2: only the two checkpoints. tol "catches" the checkpoint we woke on.
	const tol = 90 * time.Second
	if !now.Before(midday) && now.Before(midday.Add(tol)) {
		return true, closing.Sub(now)
	}
	if !now.Before(closing) && now.Before(closing.Add(tol)) {
		return true, nextMarketOpenFrom(now).Sub(now)
	}

	switch {
	case now.Before(midday):
		return false, midday.Sub(now)
	case now.Before(closing):
		return false, closing.Sub(now)
	default:
		return false, nextMarketOpenFrom(now).Sub(now)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Symbol-retirement helpers (kill the re-discovery / duplicate-record loop)
// ─────────────────────────────────────────────────────────────────────────────

func markFinalized(symbol string) {
	finalizedMu.Lock()
	finalizedSymbols[symbol] = true
	finalizedMu.Unlock()
}

func isFinalized(symbol string) bool {
	finalizedMu.Lock()
	defer finalizedMu.Unlock()
	return finalizedSymbols[symbol]
}

// finalize retires a symbol for the rest of the trading day: removed from the
// watchlist, never re-added, never re-evaluated.
func finalize(pos *RealtimePosition) {
	removeFromWatchlist(pos.Symbol)
	markFinalized(pos.Symbol)
	pos.Shares = 0
}

// maybeResetDaily clears the retired/processed sets at the start of each trading
// day so a name exited yesterday can be re-entered today.
func maybeResetDaily(now time.Time) {
	day := now.In(easternLoc).Format("2006-01-02")
	if day == lastResetDay {
		return
	}
	lastResetDay = day
	finalizedMu.Lock()
	finalizedSymbols = make(map[string]bool)
	finalizedMu.Unlock()
	processedMu.Lock()
	processedSymbols = make(map[string]bool)
	processedMu.Unlock()
	log.Printf("🔄 New trading day %s — cleared retired/processed symbols", day)
}

// isInsufficientQty reports the "shares reserved by a working order" condition.
func isInsufficientQty(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *alpaca.APIError
	if errors.As(err, &apiErr) && apiErr.Code == ALPACA_INSUFFICIENT_QTY_CODE {
		return true
	}
	return strings.Contains(err.Error(), "insufficient qty available")
}

// ─────────────────────────────────────────────────────────────────────────────
// Discovery: Alpaca positions
// ─────────────────────────────────────────────────────────────────────────────

// scanAlpacaPositions registers any open Alpaca position not already being
// monitored, deriving the stop from the pre-entry low. This is the loop that
// prints "MONITORING STARTED (from Alpaca)".
func scanAlpacaPositions() {
	positions, err := getAllAlpacaPositionsFn()
	if err != nil {
		log.Printf("⚠️  Could not fetch Alpaca positions: %v", err)
		return
	}

	for _, p := range positions {
		sym := p.Symbol

		if isFinalized(sym) {
			continue // exited already today — never re-add, never re-loop
		}
		positionsMu.RLock()
		_, active := activePositions[sym]
		positionsMu.RUnlock()
		if active {
			continue // already monitoring
		}
		if p.Qty <= 0 {
			continue // short or flat
		}

		pos := &RealtimePosition{
			Symbol:        sym,
			EntryPrice:    p.AvgEntry,
			Shares:        p.Qty,
			InitialShares: p.Qty,
			HighestPrice:  p.AvgEntry,
			SessionLow:    math.MaxFloat64,
			PurchaseDate:  time.Now().In(easternLoc), // overwritten by setStopFromEntry
			LastCheckDate: time.Now().In(easternLoc),
		}

		// Real stop = low of the X minutes before the buy. No assumed value.
		if err := setStopFromEntry(pos); err != nil {
			log.Printf("[%s] ⚠️  Could not derive stop (%v) — not registering yet", sym, err)
			continue
		}

		positionsMu.Lock()
		activePositions[sym] = pos
		positionsMu.Unlock()

		// Mirror the CSV-side processed flag so the two paths don't double-add.
		processedMu.Lock()
		processedSymbols[sym] = true
		processedMu.Unlock()

		log.Printf("[%s] 🟢 MONITORING STARTED (from Alpaca) | Entry: $%.2f | Stop: $%.2f | Risk/share: $%.2f | Shares: %.0f | Since: %s",
			sym, pos.EntryPrice, pos.StopLoss, pos.InitialRisk, pos.Shares,
			pos.PurchaseDate.Format("2006-01-02 15:04"))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Stop derivation from the pre-entry bars
// ─────────────────────────────────────────────────────────────────────────────

// stopFromPriorBars returns the lowest low across the STOP_LOOKBACK_MINUTES of
// 1-minute bars immediately preceding buyTime.
func stopFromPriorBars(symbol string, buyTime time.Time) (float64, error) {
	start := buyTime.Add(-STOP_LOOKBACK_MINUTES * time.Minute)
	bars, err := getIntradayBarsFn(symbol, start, buyTime)
	if err != nil {
		return 0, err
	}
	if len(bars) == 0 {
		return 0, fmt.Errorf("no bars in the %d min before %s",
			STOP_LOOKBACK_MINUTES, buyTime.Format(time.RFC3339))
	}
	low := math.MaxFloat64
	for _, b := range bars {
		if b.Low < low {
			low = b.Low
		}
	}
	return low, nil
}

// setStopFromEntry sets the stop to the pre-entry low and anchors the position's
// entry timestamp to the real fill time.
func setStopFromEntry(pos *RealtimePosition) error {
	entryTime, err := getEntryTimeFn(pos.Symbol)
	if err != nil {
		return fmt.Errorf("entry time: %w", err)
	}
	stop, err := stopFromPriorBars(pos.Symbol, entryTime)
	if err != nil {
		return fmt.Errorf("pre-entry low: %w", err)
	}
	if stop <= 0 || stop >= pos.EntryPrice {
		return fmt.Errorf("derived stop $%.2f not below entry $%.2f", stop, pos.EntryPrice)
	}
	pos.StopLoss = stop
	pos.InitialStopLoss = stop
	pos.InitialRisk = pos.EntryPrice - stop
	pos.PurchaseDate = entryTime
	pos.LastCheckDate = entryTime
	log.Printf("[%s] 🎯 Stop = %d-min pre-entry low $%.2f | entry $%.2f | risk/share $%.2f",
		pos.Symbol, STOP_LOOKBACK_MINUTES, stop, pos.EntryPrice, pos.InitialRisk)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Watchlist processing (CSV)
// ─────────────────────────────────────────────────────────────────────────────

func checkAndProcessWatchlist() {
	path := "watchlist.csv"
	info, err := os.Stat(path)
	if err != nil {
		path = "cmd/avantai/ep/ep_watchlist/watchlist.csv"
		info, err = os.Stat(path)
		if err != nil {
			return
		}
	}
	watchlistPath = path

	if info.ModTime().After(lastWatchlistMod) {
		lastWatchlistMod = info.ModTime()
		log.Println("📋 Watchlist file updated — scanning for new entries...")
		processWatchlist(path)
	}
}

func processWatchlist(path string) {
	file, err := os.Open(path)
	if err != nil {
		log.Printf("⚠️  Cannot open watchlist: %v", err)
		return
	}
	defer file.Close()

	records, err := csv.NewReader(file).ReadAll()
	if err != nil || len(records) <= 1 {
		return
	}

	for i := 1; i < len(records); i++ {
		pos := parsePosition(records[i])
		if pos == nil {
			continue
		}

		processedMu.Lock()
		already := processedSymbols[pos.Symbol]
		processedMu.Unlock()

		positionsMu.RLock()
		active := activePositions[pos.Symbol] != nil
		positionsMu.RUnlock()

		if already || active || isFinalized(pos.Symbol) {
			continue
		}

		processedMu.Lock()
		processedSymbols[pos.Symbol] = true
		processedMu.Unlock()

		go openPositionFn(pos)
	}
}

// parsePosition parses a CSV row into a RealtimePosition.
// Expected columns: Symbol, EntryPrice, StopLoss, Shares, InitialRisk, PurchaseDate
func parsePosition(row []string) *RealtimePosition {
	if len(row) < 6 {
		return nil
	}
	trim := func(s string) string {
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "$"))
	}

	entryPrice, e1 := strconv.ParseFloat(trim(row[1]), 64)
	stopLoss, e2 := strconv.ParseFloat(trim(row[2]), 64)
	shares, e3 := strconv.ParseFloat(trim(row[3]), 64)
	initialRisk, e4 := strconv.ParseFloat(trim(row[4]), 64)

	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		log.Printf("⚠️  Skipping row with parse error: %v", row)
		return nil
	}

	purchaseDate, err := time.Parse("2006-01-02", strings.TrimSpace(row[5]))
	if err != nil {
		log.Printf("⚠️  Cannot parse date '%s': %v", row[5], err)
		return nil
	}

	return &RealtimePosition{
		Symbol:        strings.TrimSpace(row[0]),
		EntryPrice:    entryPrice,
		StopLoss:      stopLoss,
		InitialRisk:   initialRisk,
		Shares:        shares,
		InitialShares: shares,
		PurchaseDate:  purchaseDate,
		HighestPrice:  entryPrice,
		LastCheckDate: purchaseDate,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Opening / registering a position (CSV path)
// ─────────────────────────────────────────────────────────────────────────────

func tryOpenPosition(pos *RealtimePosition) {
	if isFinalized(pos.Symbol) {
		return
	}

	if pos.EntryPrice < MIN_PRICE || pos.EntryPrice > MAX_PRICE {
		log.Printf("[%s] ⚠️  Price $%.2f outside range ($%.0f–$%.0f), skipping",
			pos.Symbol, pos.EntryPrice, MIN_PRICE, MAX_PRICE)
		return
	}
	if pos.StopLoss <= 0 || pos.StopLoss >= pos.EntryPrice {
		log.Printf("[%s] ⚠️  Invalid stop loss $%.2f vs entry $%.2f, skipping",
			pos.Symbol, pos.StopLoss, pos.EntryPrice)
		return
	}

	pos.InitialStopLoss = pos.StopLoss
	pos.InitialRisk = pos.EntryPrice - pos.StopLoss

	// Sync share count + entry price from the live Alpaca position when present,
	// and derive the real stop from the pre-entry low (overriding the CSV stop).
	if alpacaPos, err := alpacaClient.GetPosition(pos.Symbol); err == nil {
		qty, _ := alpacaPos.Qty.Float64()
		if qty > 0 {
			pos.Shares = qty
			pos.InitialShares = qty
			entryAvg, _ := alpacaPos.AvgEntryPrice.Float64()
			if entryAvg > 0 {
				pos.EntryPrice = entryAvg
			}
			if err := setStopFromEntry(pos); err != nil {
				pos.InitialRisk = pos.EntryPrice - pos.StopLoss // keep CSV stop
				log.Printf("[%s] ⚠️  Stop not derived (%v); keeping CSV stop $%.2f",
					pos.Symbol, err, pos.StopLoss)
			}
			log.Printf("[%s] 🔄 Synced from Alpaca: %.0f shares @ avg $%.2f",
				pos.Symbol, qty, entryAvg)
		}
	} else {
		log.Printf("[%s] ℹ️  No open Alpaca position yet (order may be pending). Monitoring with CSV values.", pos.Symbol)
	}

	pos.SessionHigh = 0
	pos.SessionLow = math.MaxFloat64
	pos.SessionOpen = 0

	positionsMu.Lock()
	activePositions[pos.Symbol] = pos
	positionsMu.Unlock()

	if err := replaceStopOrder(pos); err != nil {
		log.Printf("[%s] ❌ Failed to place initial stop: %v", pos.Symbol, err)
	}

	log.Printf("[%s] 🟢 MONITORING STARTED | Entry: $%.2f | Stop: $%.2f | Risk/share: $%.2f | Shares: %.0f | Since: %s",
		pos.Symbol, pos.EntryPrice, pos.StopLoss, pos.InitialRisk,
		pos.Shares, pos.PurchaseDate.Format("2006-01-02"))
}

// ─────────────────────────────────────────────────────────────────────────────
// Evaluation loop
// ─────────────────────────────────────────────────────────────────────────────

func evaluatePositions() {
	positionsMu.RLock()
	if len(activePositions) == 0 {
		positionsMu.RUnlock()
		return
	}
	symbols := make([]string, 0, len(activePositions))
	for sym := range activePositions {
		symbols = append(symbols, sym)
	}
	positionsMu.RUnlock()

	log.Printf("📊 Evaluating %d active position(s)...", len(symbols))
	printAccountStatus("")

	var wg sync.WaitGroup
	var removeMu sync.Mutex
	toRemove := make(map[string]bool)

	for _, sym := range symbols {
		positionsMu.RLock()
		pos, ok := activePositions[sym]
		positionsMu.RUnlock()
		if !ok {
			continue
		}

		wg.Add(1)
		go func(p *RealtimePosition) {
			defer wg.Done()
			if closed := evaluatePosition(p); closed {
				removeMu.Lock()
				toRemove[p.Symbol] = true
				removeMu.Unlock()
			}
		}(pos)
	}

	wg.Wait()

	if len(toRemove) > 0 {
		positionsMu.Lock()
		for sym := range toRemove {
			delete(activePositions, sym)
		}
		positionsMu.Unlock()
		printCurrentStats()
		printAccountStatus("after close")
	}
}

// evaluatePosition mirrors simulateNextDay() from the backtest.
func evaluatePosition(pos *RealtimePosition) bool {
	pos.mu.Lock()
	defer pos.mu.Unlock()

	now := time.Now().In(easternLoc)

	// 1. Re-sync shares from Alpaca.
	qty, err := getAlpacaPositionFn(pos.Symbol)
	if err != nil {
		log.Printf("[%s] ⚠️  Position no longer found in Alpaca — removing from monitor", pos.Symbol)
		removeFromWatchlist(pos.Symbol)
		markFinalized(pos.Symbol)
		return true
	}
	if qty != pos.Shares && qty > 0 {
		log.Printf("[%s] 🔄 Share count updated %.0f → %.0f (Alpaca sync)", pos.Symbol, pos.Shares, qty)
		pos.Shares = qty
	}

	// 2. Fetch today's intraday bars to build session OHLC.
	today := now.Format("2006-01-02")
	sessionStart := time.Date(now.Year(), now.Month(), now.Day(),
		MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, easternLoc)

	bars, barsErr := getIntradayBarsFn(pos.Symbol, sessionStart, now)
	if barsErr != nil || len(bars) == 0 {
		log.Printf("[%s] ⚠️  Cannot fetch intraday bars: %v — skipping tick", pos.Symbol, barsErr)
		return false
	}

	sessionHigh := 0.0
	sessionLow := math.MaxFloat64
	sessionOpen := bars[0].Open
	latestClose := bars[len(bars)-1].Close
	for _, b := range bars {
		if b.High > sessionHigh {
			sessionHigh = b.High
		}
		if b.Low < sessionLow {
			sessionLow = b.Low
		}
	}

	pos.SessionHigh = sessionHigh
	pos.SessionLow = sessionLow
	pos.SessionOpen = sessionOpen
	if sessionHigh > pos.HighestPrice {
		pos.HighestPrice = sessionHigh
	}

	// 3. Increment DaysHeld once per calendar day.
	if today != pos.LastCheckDate.Format("2006-01-02") {
		pos.DaysHeld++
		pos.LastCheckDate = now
		pos.SessionHigh = 0
		pos.SessionLow = math.MaxFloat64
	}

	currentPrice := latestClose
	currentGain := currentPrice - pos.EntryPrice
	currentRR := 0.0
	if pos.InitialRisk > 0 {
		currentRR = currentGain / pos.InitialRisk
	}

	log.Printf("[%s] Day %d (%s) | Close: $%.2f | SessionH: $%.2f | SessionL: $%.2f | Gain: $%.2f (%.1f%%) | R/R: %.2fR | Stop: $%.2f | Shares: %.0f",
		pos.Symbol, pos.DaysHeld, today, currentPrice, sessionHigh, sessionLow,
		currentGain, (currentGain/pos.EntryPrice)*100, currentRR, pos.StopLoss, pos.Shares)

	// 4. Weak close detection.
	if sessionHigh > 0 && !pos.WeakCloseDetected {
		closeFromHigh := (sessionHigh - currentPrice) / sessionHigh
		if closeFromHigh >= WEAK_CLOSE_THRESHOLD {
			log.Printf("[%s] ⚠️  WEAK CLOSE: $%.2f is %.1f%% below session high $%.2f — exiting",
				pos.Symbol, currentPrice, closeFromHigh*100, sessionHigh)
			pos.WeakCloseDetected = true
			return executeExit(pos, currentPrice, now, "Weak Close")
		}
	}

	// 5. Stop loss (session low catches intraday wicks).
	if sessionLow <= pos.StopLoss || currentPrice <= pos.StopLoss {
		return executeStopOut(pos, pos.StopLoss, now)
	}

	// 6. Move to breakeven.
	if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS && !pos.ProfitTaken {
		pctGain := (sessionHigh - pos.EntryPrice) / pos.EntryPrice
		if pctGain >= BREAKEVEN_TRIGGER_PERCENT && pos.StopLoss < pos.EntryPrice {
			pos.StopLoss = pos.EntryPrice
			log.Printf("[%s] 🔒 BREAKEVEN STOP SET", pos.Symbol)
			if err := replaceStopOrder(pos); err != nil {
				log.Printf("[%s] ❌ Failed to update stop in Alpaca: %v", pos.Symbol, err)
			}
		}
	}

	// 7. Tighten stop if no follow-through.
	if pos.DaysHeld >= MAX_DAYS_NO_FOLLOWTHROUGH && currentRR < 0.5 && !pos.ProfitTaken {
		tighter := math.Max(pos.EntryPrice-(pos.InitialRisk*0.3), pos.EntryPrice)
		if tighter > pos.StopLoss {
			pos.StopLoss = tighter
			log.Printf("[%s] ⚠️  No follow-through — stop tightened to $%.2f", pos.Symbol, pos.StopLoss)
			if err := replaceStopOrder(pos); err != nil {
				log.Printf("[%s] ❌ Failed to update stop in Alpaca: %v", pos.Symbol, err)
			}
		}
	}

	// 8. Strong EP — big gain in first few days.
	pctGain := (currentPrice - pos.EntryPrice) / pos.EntryPrice
	if pos.DaysHeld <= STRONG_EP_DAYS && pctGain >= STRONG_EP_GAIN && !pos.ProfitTaken {
		if executeStrongEPProfit(pos, currentPrice, now) {
			return true
		}
		return false
	}

	// 9. Graduated profit taking.
	if currentRR >= PROFIT_TAKE_1_RR && !pos.ProfitTaken {
		if executeProfitPartial(pos, currentPrice, now, PROFIT_TAKE_1_PERCENT, 1) {
			pos.TrailingStopMode = true
			pos.ProfitTaken = true
			if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS {
				pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice)
				log.Printf("[%s] 🔒 Stop locked at breakeven after Level 1 profit", pos.Symbol)
				if err := replaceStopOrder(pos); err != nil {
					log.Printf("[%s] ❌ Failed to update stop in Alpaca: %v", pos.Symbol, err)
				}
			}
			if pos.Shares <= 0 {
				finalize(pos)
				return true
			}
		}
		return false
	}

	if currentRR >= PROFIT_TAKE_2_RR && pos.ProfitTaken && !pos.ProfitTaken2 {
		if executeProfitPartial(pos, currentPrice, now, PROFIT_TAKE_2_PERCENT, 2) {
			pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice+pos.InitialRisk)
			if err := replaceStopOrder(pos); err != nil {
				log.Printf("[%s] ❌ Failed to update stop in Alpaca: %v", pos.Symbol, err)
			}
			pos.ProfitTaken2 = true
			log.Printf("[%s] 🔒 Stop locked at +1R ($%.2f) after Level 2 profit", pos.Symbol, pos.StopLoss)
			if pos.Shares <= 0 {
				finalize(pos)
				return true
			}
		}
		return false
	}

	if currentRR >= PROFIT_TAKE_3_RR && pos.ProfitTaken2 && !pos.ProfitTaken3 {
		if executeProfitPartial(pos, currentPrice, now, PROFIT_TAKE_3_PERCENT, 3) {
			pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice+pos.InitialRisk*2.0)
			if err := replaceStopOrder(pos); err != nil {
				log.Printf("[%s] ❌ Failed to update stop in Alpaca: %v", pos.Symbol, err)
			}
			pos.ProfitTaken3 = true
			log.Printf("[%s] 🔒 Stop locked at +2R ($%.2f) after Level 3 profit", pos.Symbol, pos.StopLoss)
			if pos.Shares <= 0 {
				finalize(pos)
				return true
			}
		}
		return false
	}

	// 10. Trailing stop (active after first profit taken).
	if pos.TrailingStopMode {
		pctFromEntry := (currentPrice - pos.EntryPrice) / pos.EntryPrice

		var newStop float64
		switch {
		case pctFromEntry > 0.20:
			newStop = currentPrice * 0.94
		case pctFromEntry > 0.10:
			newStop = currentPrice * 0.95
		case pctFromEntry > 0.05:
			newStop = currentPrice * 0.96
		default:
			newStop = pos.EntryPrice
		}

		if newStop > pos.StopLoss {
			pos.StopLoss = newStop
			log.Printf("[%s] 📈 Trailing stop → $%.2f", pos.Symbol, pos.StopLoss)
			if err := replaceStopOrder(pos); err != nil {
				log.Printf("[%s] ❌ Failed to update stop in Alpaca: %v", pos.Symbol, err)
			}
		}

		if pctFromEntry < 0.05 && currentPrice < pos.EntryPrice*0.96 {
			exitPrice := math.Max(pos.StopLoss, pos.EntryPrice)
			return executeExit(pos, exitPrice, now, "Trailing — retreated below threshold")
		}
	}

	return false
}

func replaceStopOrder(pos *RealtimePosition) error {
	if pos.StopOrderID != "" {
		if err := alpacaClient.CancelOrder(pos.StopOrderID); err != nil {
			log.Printf("[%s] ⚠️  Could not cancel stop order %s: %v", pos.Symbol, pos.StopOrderID, err)
		} else {
			log.Printf("[%s] 🗑️  Cancelled stop order %s", pos.Symbol, pos.StopOrderID)
		}
		pos.StopOrderID = ""
	}

	qty := int64(math.Round(pos.Shares))
	if qty < 1 {
		return nil
	}

	stopDec := decimal.NewFromFloat(pos.StopLoss)
	qtyDec := decimal.NewFromInt(qty)

	order, err := alpacaClient.PlaceOrder(alpaca.PlaceOrderRequest{
		Symbol:      pos.Symbol,
		Qty:         &qtyDec,
		Side:        alpaca.Sell,
		Type:        alpaca.Stop,
		TimeInForce: alpaca.GTC,
		StopPrice:   &stopDec,
	})
	if err != nil {
		return fmt.Errorf("PlaceOrder (stop): %w", err)
	}

	pos.StopOrderID = order.ID
	log.Printf("[%s] ✅ Stop order placed @ $%.2f (ID: %s)", pos.Symbol, pos.StopLoss, order.ID)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Exit execution
// ─────────────────────────────────────────────────────────────────────────────

// sellOutcome reports how a liquidation attempt resolved.
type sellOutcome int

const (
	sellDone       sellOutcome = iota // our order was accepted at the intended price
	sellReconciled                    // our order failed but a real fill already exists
	sellDeferred                      // shares held by a working order, no fill yet
	sellFailed                        // unexpected error
)

// liquidate attempts to sell `shares`. It handles the "shares reserved by a
// working order" case per FORCE_EXIT_ON_RESERVED, and reconciles the true fill
// from Alpaca when the existing order has already executed.
func liquidate(symbol string, shares int, intendedPrice float64) (sellOutcome, float64, time.Time) {
	p := intendedPrice
	err := placeSellOrderFn(symbol, shares, &p)
	if err == nil {
		return sellDone, intendedPrice, time.Now().In(easternLoc)
	}
	if !isInsufficientQty(err) {
		log.Printf("[%s] ❌ PlaceSellOrder error: %v", symbol, err)
		return sellFailed, 0, time.Time{}
	}

	// Shares are committed to a working exit order (e.g. an existing bracket/GTC stop).
	if FORCE_EXIT_ON_RESERVED {
		log.Printf("[%s] ⚠️  Shares reserved — cancelling open orders to force exit", symbol)
		if cErr := cancelOpenOrdersFn(symbol); cErr != nil {
			log.Printf("[%s] ⚠️  Cancel failed: %v", symbol, cErr)
		}
		time.Sleep(RESERVED_RETRY_DELAY) // let the cancel settle so shares free up
		p = intendedPrice
		if rErr := placeSellOrderFn(symbol, shares, &p); rErr == nil {
			return sellDone, intendedPrice, time.Now().In(easternLoc)
		} else {
			log.Printf("[%s] ❌ Retry after cancel failed: %v — position may be UNPROTECTED, verify!", symbol, rErr)
			// fall through to reconcile
		}
	}

	// Reconcile the real fill from the working order so the record is accurate.
	if fp, ft, ok, rErr := getClosedSellFillFn(symbol); rErr != nil {
		log.Printf("[%s] ⚠️  Could not reconcile fill: %v", symbol, rErr)
	} else if ok {
		log.Printf("[%s] 🔁 Reconciled real exit fill: $%.2f at %s",
			symbol, fp, ft.In(easternLoc).Format("15:04:05"))
		return sellReconciled, fp, ft
	}

	log.Printf("[%s] 🧹 Shares held by a working exit order, no fill yet — not recording", symbol)
	return sellDeferred, 0, time.Time{}
}

// recordExit writes a single trade record for a completed full exit.
func recordExit(pos *RealtimePosition, exitPrice float64, exitTime time.Time, baseReason string, reconciled bool) {
	shares := pos.InitialShares - 0 // record the shares actually being closed below
	shares = math.Round(pos.Shares)
	pl := (exitPrice - pos.EntryPrice) * shares
	totalPL := pl + pos.CumulativeProfit
	rr := 0.0
	if pos.InitialRisk > 0 {
		rr = (exitPrice - pos.EntryPrice) / pos.InitialRisk
	}

	reason := baseReason
	if reconciled {
		reason += " (filled by existing order)"
	}
	if pos.CumulativeProfit != 0 {
		reason = fmt.Sprintf("%s (prev. partial P/L: $%.2f, total: $%.2f)", reason, pos.CumulativeProfit, totalPL)
	}

	recordTrade(TradeRecord{
		Symbol:      pos.Symbol,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   exitPrice,
		Shares:      shares,
		InitialRisk: pos.InitialRisk,
		ProfitLoss:  pl,
		RiskReward:  rr,
		EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
		ExitDate:    exitTime.Format("2006-01-02"),
		ExitReason:  reason,
		IsWinner:    totalPL > 0,
	})
}

// executeStopOut sells all remaining shares; records only on a confirmed fill.
func executeStopOut(pos *RealtimePosition, stopPrice float64, t time.Time) bool {
	shares := int(math.Round(pos.Shares))
	if shares < 1 {
		finalize(pos)
		return true
	}

	log.Printf("[%s] 🛑 STOP OUT @ $%.2f — selling %d shares", pos.Symbol, stopPrice, shares)

	reason := "Stop Loss Hit"
	if pos.ProfitTaken {
		reason = "Trailing Stop Hit (partial profit protected)"
	}

	outcome, fillPrice, fillTime := liquidate(pos.Symbol, shares, stopPrice)
	switch outcome {
	case sellDone:
		recordExit(pos, fillPrice, t, reason, false)
	case sellReconciled:
		recordExit(pos, fillPrice, fillTime, reason, true)
	default:
		log.Printf("[%s] 🧹 Stop-out not recorded (no fresh fill) — retiring symbol", pos.Symbol)
	}

	finalize(pos)
	return true
}

// executeExit closes the full remaining position for a given reason.
func executeExit(pos *RealtimePosition, exitPrice float64, t time.Time, reason string) bool {
	shares := int(math.Round(pos.Shares))
	if shares < 1 {
		finalize(pos)
		return true
	}

	log.Printf("[%s] 📤 EXIT — %s | Selling %d shares @ $%.2f", pos.Symbol, reason, shares, exitPrice)

	outcome, fillPrice, fillTime := liquidate(pos.Symbol, shares, exitPrice)
	switch outcome {
	case sellDone:
		recordExit(pos, fillPrice, t, reason, false)
	case sellReconciled:
		recordExit(pos, fillPrice, fillTime, reason, true)
	default:
		log.Printf("[%s] 🧹 Exit not recorded (no fresh fill) — retiring symbol", pos.Symbol)
	}

	finalize(pos)
	return true
}

// executeStrongEPProfit sells STRONG_EP_TAKE_PERCENT on a strong move.
// Returns true if the position is fully closed. Records/adjusts only on a fill.
func executeStrongEPProfit(pos *RealtimePosition, currentPrice float64, t time.Time) bool {
	sharesToSell := int(math.Floor(pos.Shares * STRONG_EP_TAKE_PERCENT))
	if sharesToSell < 1 {
		sharesToSell = 1
	}
	if sharesToSell > int(pos.Shares) {
		sharesToSell = int(pos.Shares)
	}

	log.Printf("[%s] 🚀 STRONG EP! Selling %.0f%% (%d shares) @ $%.2f",
		pos.Symbol, STRONG_EP_TAKE_PERCENT*100, sharesToSell, currentPrice)

	p := currentPrice
	if err := placeSellOrderFn(pos.Symbol, sharesToSell, &p); err != nil {
		log.Printf("[%s] ❌ Strong-EP partial failed: %v — not recording, leaving position intact", pos.Symbol, err)
		return false
	}

	pl := (currentPrice - pos.EntryPrice) * float64(sharesToSell)
	rr := 0.0
	if pos.InitialRisk > 0 {
		rr = (currentPrice - pos.EntryPrice) / pos.InitialRisk
	}

	recordTrade(TradeRecord{
		Symbol: pos.Symbol, EntryPrice: pos.EntryPrice, ExitPrice: currentPrice,
		Shares: float64(sharesToSell), InitialRisk: pos.InitialRisk, ProfitLoss: pl, RiskReward: rr,
		EntryDate: pos.PurchaseDate.Format("2006-01-02"), ExitDate: t.Format("2006-01-02"),
		ExitReason: fmt.Sprintf("Strong EP — %.0f%% sold", STRONG_EP_TAKE_PERCENT*100), IsWinner: true,
	})

	pos.CumulativeProfit += pl
	pos.Shares -= float64(sharesToSell)
	pos.StopLoss = math.Max(pos.EntryPrice, pos.StopLoss)
	pos.ProfitTaken = true
	pos.TrailingStopMode = true

	log.Printf("[%s] ✅ %.0f shares remain | Cumulative P/L: $%.2f | Stop moved to BE: $%.2f",
		pos.Symbol, pos.Shares, pos.CumulativeProfit, pos.StopLoss)

	if pos.Shares <= 0 {
		finalize(pos)
		return true
	}
	return false
}

// executeProfitPartial sells pct of remaining shares at a profit level.
// Returns true on a confirmed fill; the caller flips its flags only then.
func executeProfitPartial(pos *RealtimePosition, currentPrice float64, t time.Time, pct float64, level int) bool {
	sharesToSell := int(math.Floor(pos.Shares * pct))
	if sharesToSell < 1 {
		sharesToSell = 1
	}
	if sharesToSell > int(pos.Shares) {
		sharesToSell = int(pos.Shares)
	}

	rr := 0.0
	if pos.InitialRisk > 0 {
		rr = (currentPrice - pos.EntryPrice) / pos.InitialRisk
	}
	log.Printf("[%s] 🎯 PROFIT LEVEL %d (%.2fR) — selling %d shares @ $%.2f",
		pos.Symbol, level, rr, sharesToSell, currentPrice)

	p := currentPrice
	if err := placeSellOrderFn(pos.Symbol, sharesToSell, &p); err != nil {
		log.Printf("[%s] ❌ Profit-level-%d partial failed: %v — not recording, leaving position intact", pos.Symbol, level, err)
		return false
	}

	pl := (currentPrice - pos.EntryPrice) * float64(sharesToSell)

	recordTrade(TradeRecord{
		Symbol: pos.Symbol, EntryPrice: pos.EntryPrice, ExitPrice: currentPrice,
		Shares: float64(sharesToSell), InitialRisk: pos.InitialRisk, ProfitLoss: pl, RiskReward: rr,
		EntryDate: pos.PurchaseDate.Format("2006-01-02"), ExitDate: t.Format("2006-01-02"),
		ExitReason: fmt.Sprintf("Profit Level %d at %.2fR", level, rr), IsWinner: true,
	})

	pos.CumulativeProfit += pl
	pos.Shares -= float64(sharesToSell)

	log.Printf("[%s] ✅ %.0f shares remain | Cumulative P/L: $%.2f | Stop: $%.2f",
		pos.Symbol, pos.Shares, pos.CumulativeProfit, pos.StopLoss)
	return true
}

// ─────────────────────────────────────────────────────────────────────────────
// Watchlist file helper
// ─────────────────────────────────────────────────────────────────────────────

func removeFromWatchlist(symbol string) {
	if watchlistPath == "" {
		return
	}

	f, err := os.Open(watchlistPath)
	if err != nil {
		return
	}
	records, err := csv.NewReader(f).ReadAll()
	f.Close()
	if err != nil {
		return
	}

	var kept [][]string
	removed := false
	for i, rec := range records {
		if i == 0 || len(rec) == 0 || strings.TrimSpace(rec[0]) != symbol {
			kept = append(kept, rec)
		} else {
			removed = true
		}
	}

	if !removed {
		return
	}

	f, err = os.Create(watchlistPath)
	if err != nil {
		log.Printf("[%s] ⚠️  Cannot rewrite watchlist: %v", symbol, err)
		return
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.WriteAll(kept); err != nil {
		log.Printf("[%s] ⚠️  Cannot write watchlist: %v", symbol, err)
	}

	log.Printf("[%s] 🗑️  Removed from watchlist", symbol)
}

// ─────────────────────────────────────────────────────────────────────────────
// Trade results CSV
// ─────────────────────────────────────────────────────────────────────────────

func initTradeResultsFile() {
	const filename = "trade_results.csv"
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		f, err := os.Create(filename)
		if err != nil {
			log.Printf("⚠️  Cannot create trade_results.csv: %v", err)
			return
		}
		defer f.Close()
		w := csv.NewWriter(f)
		_ = w.Write([]string{
			"Symbol", "EntryPrice", "ExitPrice", "Shares", "InitialRisk",
			"ProfitLoss", "RiskReward", "EntryDate", "ExitDate", "ExitReason", "IsWinner",
		})
		w.Flush()
		log.Println("📄 Created trade_results.csv")
	}
}

func recordTrade(r TradeRecord) {
	tradeResultsMu.Lock()
	defer tradeResultsMu.Unlock()

	f, err := os.OpenFile("trade_results.csv", os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("⚠️  Cannot open trade_results.csv: %v", err)
		return
	}
	defer f.Close()

	w := csv.NewWriter(f)
	_ = w.Write([]string{
		r.Symbol,
		fmt.Sprintf("%.2f", r.EntryPrice),
		fmt.Sprintf("%.2f", r.ExitPrice),
		fmt.Sprintf("%.2f", r.Shares),
		fmt.Sprintf("%.2f", r.InitialRisk),
		fmt.Sprintf("%.2f", r.ProfitLoss),
		fmt.Sprintf("%.2f", r.RiskReward),
		r.EntryDate,
		r.ExitDate,
		r.ExitReason,
		fmt.Sprintf("%t", r.IsWinner),
	})
	w.Flush()

	log.Printf("[%s] ✍️  Recorded | P/L: $%.2f | R/R: %.2fR | Winner: %t",
		r.Symbol, r.ProfitLoss, r.RiskReward, r.IsWinner)
}

// ─────────────────────────────────────────────────────────────────────────────
// Account status
// ─────────────────────────────────────────────────────────────────────────────

func printAccountStatus(label string) {
	snap, err := getAccountFn()
	if err != nil {
		log.Printf("⚠️  Could not fetch account info: %v", err)
		return
	}

	dayPLSign := "+"
	if snap.DayPL < 0 {
		dayPLSign = ""
	}

	sep := strings.Repeat("─", 65)
	fmt.Println("\n" + sep)
	if label != "" {
		fmt.Printf("  💼  ACCOUNT STATUS  (%s)\n", label)
	} else {
		fmt.Println("  💼  ACCOUNT STATUS")
	}
	fmt.Println(sep)
	fmt.Printf("  Equity:        $%12.2f\n", snap.Equity)
	fmt.Printf("  Cash:          $%12.2f\n", snap.Cash)
	fmt.Printf("  Buying Power:  $%12.2f\n", snap.BuyingPower)
	fmt.Printf("  Day P/L:        %s$%.2f\n", dayPLSign, snap.DayPL)
	fmt.Println(sep + "\n")
}

// ─────────────────────────────────────────────────────────────────────────────
// Stats summary
// ─────────────────────────────────────────────────────────────────────────────

func printCurrentStats() {
	tradeResultsMu.Lock()
	defer tradeResultsMu.Unlock()

	f, err := os.Open("trade_results.csv")
	if err != nil {
		return
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil || len(records) <= 1 {
		return
	}

	type stat struct {
		winners, losers int
		totalPL         float64
		winRR, lossRR   float64
	}
	var s stat

	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 11 {
			continue
		}
		pl, _ := strconv.ParseFloat(row[5], 64)
		rr, _ := strconv.ParseFloat(row[6], 64)
		isWinner := row[10] == "true"

		s.totalPL += pl
		if isWinner {
			s.winners++
			s.winRR += rr
		} else {
			s.losers++
			s.lossRR += rr
		}
	}

	total := s.winners + s.losers
	if total == 0 {
		return
	}

	winRate := float64(s.winners) / float64(total) * 100
	avgWinRR, avgLossRR := 0.0, 0.0
	if s.winners > 0 {
		avgWinRR = s.winRR / float64(s.winners)
	}
	if s.losers > 0 {
		avgLossRR = s.lossRR / float64(s.losers)
	}

	sep := strings.Repeat("─", 65)
	fmt.Println("\n" + sep)
	fmt.Println("  📈  LIVE TRADING STATS")
	fmt.Println(sep)
	fmt.Printf("  Trades: %d  |  Winners: %d (%.1f%%)  |  Losers: %d (%.1f%%)\n",
		total, s.winners, winRate, s.losers, 100-winRate)
	fmt.Printf("  Avg Win R/R: %.2fR  |  Avg Loss R/R: %.2fR\n", avgWinRR, avgLossRR)
	fmt.Printf("  Total Realised P/L: $%.2f\n", s.totalPL)
	fmt.Println(sep + "\n")
}
