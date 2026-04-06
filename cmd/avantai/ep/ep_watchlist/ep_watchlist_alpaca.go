package main

import (
	"encoding/csv"
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

	ep "avantai/pkg/ep" // ← update to match your go.mod module path
)

// ─────────────────────────────────────────────────────────────────────────────
// Constants
// ─────────────────────────────────────────────────────────────────────────────

const (
	// How often to poll while market is open
	POLL_INTERVAL = 60 * time.Second

	// How often to re-check market open/close status when sleeping
	MARKET_CHECK_INTERVAL = 1 * time.Minute

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
	lastWatchlistMod time.Time
	watchlistPath    string

	positionsMu    sync.RWMutex
	processedMu    sync.Mutex
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

// getIntradayBarsFn returns 1-minute bars for symbol from sessionStart to now.
var getIntradayBarsFn = func(symbol string, sessionStart, now time.Time) ([]Bar, error) {
	raw, err := mdClient.GetBars(symbol, marketdata.GetBarsRequest{
		TimeFrame: marketdata.OneMin,
		Start:     sessionStart,
		End:       now,
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

// openPositionFn is called by processWatchlist for each new symbol.
// Tests replace it with a stub that skips the Alpaca API call.
var openPositionFn = tryOpenPosition

// AccountSnapshot holds the key numbers we care about from Alpaca.
type AccountSnapshot struct {
	Equity      float64 // total account value (cash + positions)
	Cash        float64 // settled cash available
	BuyingPower float64 // overnight buying power
	DayPL       float64 // today's P/L = equity minus yesterday's close equity
}

// getAccountFn fetches account figures from Alpaca.
// Tests replace it with a stub.
var getAccountFn = func() (*AccountSnapshot, error) {
	acct, err := alpacaClient.GetAccount()
	if err != nil {
		return nil, err
	}
	equity, _ := acct.Equity.Float64()
	cash, _ := acct.Cash.Float64()
	bp, _ := acct.BuyingPower.Float64()
	lastEquity, _ := acct.LastEquity.Float64()
	// DayPL = how much equity has changed since yesterday's close.
	dayPL := equity - lastEquity
	return &AccountSnapshot{
		Equity:      equity,
		Cash:        cash,
		BuyingPower: bp,
		DayPL:       dayPL,
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// main
// ─────────────────────────────────────────────────────────────────────────────

// loadEnv walks up the directory tree from the current working directory
// until it finds a .env file and loads it.  This means the binary and tests
// running from any sub-directory will still find the .env at the project root.
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
	baseURL := os.Getenv("ALPACA_PAPER_URL") // e.g. https://paper-api.alpaca.markets

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

	// Print account status immediately so the operator knows the starting equity.
	printAccountStatus("startup")

	log.Println("🚀 Real-time EP monitor started.")
	log.Println("   Drop symbols into watchlist.csv (same columns as backtest) to begin tracking.")
	log.Println("   Process sleeps automatically outside NYSE market hours.")
	log.Println("   Press Ctrl+C to stop.")

	// ── Main loop ─────────────────────────────────────────────────────────────
	for {
		if isMarketOpen() {
			checkAndProcessWatchlist()
			evaluatePositions()
			time.Sleep(POLL_INTERVAL)
		} else {
			nextOpen := nextMarketOpen()
			sleepDur := time.Until(nextOpen)
			if sleepDur < time.Minute {
				sleepDur = MARKET_CHECK_INTERVAL
			}
			log.Printf("💤 Market closed. Next open ~%s  (sleeping %s)",
				nextOpen.In(easternLoc).Format("Mon 2006-01-02 15:04 MST"),
				sleepDur.Round(time.Minute))
			time.Sleep(sleepDur)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Market hours helpers
// ─────────────────────────────────────────────────────────────────────────────

func isMarketOpen() bool {
	now := time.Now().In(easternLoc)
	wd := now.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return false
	}
	open := time.Date(now.Year(), now.Month(), now.Day(),
		MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, easternLoc)
	close_ := time.Date(now.Year(), now.Month(), now.Day(),
		MARKET_CLOSE_HOUR, MARKET_CLOSE_MIN, 0, 0, easternLoc)
	return now.After(open) && now.Before(close_)
}

// nextMarketOpen returns the next weekday 09:30 ET after the current moment.
func nextMarketOpen() time.Time {
	now := time.Now().In(easternLoc)
	candidate := time.Date(now.Year(), now.Month(), now.Day(),
		MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, easternLoc)

	// If today's open is still in the future and today is a weekday, use today.
	if now.Before(candidate) && now.Weekday() != time.Saturday && now.Weekday() != time.Sunday {
		return candidate
	}
	// Otherwise advance day-by-day until we land on a weekday.
	for {
		candidate = candidate.AddDate(0, 0, 1)
		if candidate.Weekday() != time.Saturday && candidate.Weekday() != time.Sunday {
			return candidate
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Watchlist processing
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

		if already || active {
			continue
		}

		// Mark processed immediately to prevent duplicate goroutines
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
// Opening / registering a position
// ─────────────────────────────────────────────────────────────────────────────

// tryOpenPosition validates the position, syncs share count from Alpaca,
// and registers it for ongoing monitoring.
func tryOpenPosition(pos *RealtimePosition) {
	// Basic sanity filters
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

	// Try to sync actual share count from the live Alpaca position.
	// If the position doesn't exist yet (order pending), we keep the CSV shares
	// and will re-sync on the first evaluation tick.
	if alpacaPos, err := alpacaClient.GetPosition(pos.Symbol); err == nil {
		qty, _ := alpacaPos.Qty.Float64()
		if qty > 0 {
			pos.Shares = qty
			pos.InitialShares = qty
			entryAvg, _ := alpacaPos.AvgEntryPrice.Float64()
			// Trust Alpaca's avg entry price when available
			if entryAvg > 0 {
				pos.EntryPrice = entryAvg
				pos.InitialRisk = pos.EntryPrice - pos.StopLoss
			}
			log.Printf("[%s] 🔄 Synced from Alpaca: %.0f shares @ avg $%.2f",
				pos.Symbol, qty, entryAvg)
		}
	} else {
		log.Printf("[%s] ℹ️  No open Alpaca position yet (order may be pending). Monitoring with CSV values.", pos.Symbol)
	}

	// Reset per-session OHLC — will be populated on first bar fetch
	pos.SessionHigh = 0
	pos.SessionLow = math.MaxFloat64
	pos.SessionOpen = 0

	positionsMu.Lock()
	activePositions[pos.Symbol] = pos
	positionsMu.Unlock()

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
	// Snapshot symbol list so we can release the read-lock while goroutines run
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

// ─────────────────────────────────────────────────────────────────────────────
// Per-position evaluation — mirrors simulateNextDay() from the backtest
// ─────────────────────────────────────────────────────────────────────────────

func evaluatePosition(pos *RealtimePosition) bool {
	pos.mu.Lock()
	defer pos.mu.Unlock()

	now := time.Now().In(easternLoc)

	// ── 1. Re-sync shares from Alpaca (handles partial fills, etc.) ──────────
	qty, err := getAlpacaPositionFn(pos.Symbol)
	if err != nil {
		// Position no longer exists in Alpaca — stopped out by bracket leg or
		// closed manually. Remove from monitoring.
		log.Printf("[%s] ⚠️  Position no longer found in Alpaca — removing from monitor", pos.Symbol)
		removeFromWatchlist(pos.Symbol)
		return true
	}
	if qty != pos.Shares && qty > 0 {
		log.Printf("[%s] 🔄 Share count updated %.0f → %.0f (Alpaca sync)",
			pos.Symbol, pos.Shares, qty)
		pos.Shares = qty
	}

	// ── 2. Fetch today's intraday bars to build session OHLC ─────────────────
	today := now.Format("2006-01-02")
	sessionStart := time.Date(now.Year(), now.Month(), now.Day(),
		MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, easternLoc)

	bars, barsErr := getIntradayBarsFn(pos.Symbol, sessionStart, now)
	if barsErr != nil || len(bars) == 0 {
		log.Printf("[%s] ⚠️  Cannot fetch intraday bars: %v — skipping tick", pos.Symbol, barsErr)
		return false
	}

	// Build session OHLC from all bars since open
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

	// ── 3. Increment DaysHeld once per calendar day ───────────────────────────
	if today != pos.LastCheckDate.Format("2006-01-02") {
		pos.DaysHeld++
		pos.LastCheckDate = now
		// Reset session OHLC tracking at the start of a new day
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
		pos.Symbol, pos.DaysHeld, today,
		currentPrice, sessionHigh, sessionLow,
		currentGain, (currentGain/pos.EntryPrice)*100,
		currentRR, pos.StopLoss, pos.Shares)

	// ── 4. Weak close detection (mirrors backtest checkWeakCloseIntraday) ─────
	// On any day, if the session closes more than WEAK_CLOSE_THRESHOLD below
	// the session high, exit immediately (same logic as the backtest).
	if sessionHigh > 0 && !pos.WeakCloseDetected {
		closeFromHigh := (sessionHigh - currentPrice) / sessionHigh
		if closeFromHigh >= WEAK_CLOSE_THRESHOLD {
			log.Printf("[%s] ⚠️  WEAK CLOSE: $%.2f is %.1f%% below session high $%.2f — exiting",
				pos.Symbol, currentPrice, closeFromHigh*100, sessionHigh)
			pos.WeakCloseDetected = true
			return executeExit(pos, currentPrice, now, "Weak Close")
		}
	}

	// ── 5. Stop loss ──────────────────────────────────────────────────────────
	// Use session low (not just latest close) so intraday wicks are caught.
	if sessionLow <= pos.StopLoss || currentPrice <= pos.StopLoss {
		return executeStopOut(pos, pos.StopLoss, now)
	}

	// ── 6. Move to breakeven ──────────────────────────────────────────────────
	if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS && !pos.ProfitTaken {
		pctGain := (sessionHigh - pos.EntryPrice) / pos.EntryPrice
		if pctGain >= BREAKEVEN_TRIGGER_PERCENT && pos.StopLoss < pos.EntryPrice {
			pos.StopLoss = pos.EntryPrice
			log.Printf("[%s] 🔒 BREAKEVEN STOP SET — session touched +%.1f%% on day %d",
				pos.Symbol, pctGain*100, pos.DaysHeld)
		}
	}

	// ── 7. Tighten stop if no follow-through after N days ────────────────────
	if pos.DaysHeld >= MAX_DAYS_NO_FOLLOWTHROUGH && currentRR < 0.5 && !pos.ProfitTaken {
		tighter := math.Max(pos.EntryPrice-(pos.InitialRisk*0.3), pos.EntryPrice)
		if tighter > pos.StopLoss {
			pos.StopLoss = tighter
			log.Printf("[%s] ⚠️  No follow-through after %d days — stop tightened to $%.2f",
				pos.Symbol, MAX_DAYS_NO_FOLLOWTHROUGH, pos.StopLoss)
		}
	}

	// ── 8. Strong EP — big gain in first few days ────────────────────────────
	pctGain := (currentPrice - pos.EntryPrice) / pos.EntryPrice
	if pos.DaysHeld <= STRONG_EP_DAYS && pctGain >= STRONG_EP_GAIN && !pos.ProfitTaken {
		closed := executeStrongEPProfit(pos, currentPrice, now)
		if closed {
			return true
		}
		return false
	}

	// ── 9. Graduated profit taking ────────────────────────────────────────────
	if currentRR >= PROFIT_TAKE_1_RR && !pos.ProfitTaken {
		executeProfitPartial(pos, currentPrice, now, PROFIT_TAKE_1_PERCENT, 1)
		pos.TrailingStopMode = true
		pos.ProfitTaken = true
		if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS {
			pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice)
			log.Printf("[%s] 🔒 Stop locked at breakeven after Level 1 profit", pos.Symbol)
		}
		return pos.Shares <= 0
	}

	if currentRR >= PROFIT_TAKE_2_RR && pos.ProfitTaken && !pos.ProfitTaken2 {
		executeProfitPartial(pos, currentPrice, now, PROFIT_TAKE_2_PERCENT, 2)
		newStop := pos.EntryPrice + (pos.InitialRisk * 1.0)
		pos.StopLoss = math.Max(pos.StopLoss, newStop)
		pos.ProfitTaken2 = true
		log.Printf("[%s] 🔒 Stop locked at +1R ($%.2f) after Level 2 profit", pos.Symbol, pos.StopLoss)
		return pos.Shares <= 0
	}

	if currentRR >= PROFIT_TAKE_3_RR && pos.ProfitTaken2 && !pos.ProfitTaken3 {
		executeProfitPartial(pos, currentPrice, now, PROFIT_TAKE_3_PERCENT, 3)
		newStop := pos.EntryPrice + (pos.InitialRisk * 2.0)
		pos.StopLoss = math.Max(pos.StopLoss, newStop)
		pos.ProfitTaken3 = true
		log.Printf("[%s] 🔒 Stop locked at +2R ($%.2f) after Level 3 profit", pos.Symbol, pos.StopLoss)
		return pos.Shares <= 0
	}

	// ── 10. Trailing stop (active after first profit taken) ───────────────────
	if pos.TrailingStopMode {
		pctFromEntry := (currentPrice - pos.EntryPrice) / pos.EntryPrice

		var newStop float64
		switch {
		case pctFromEntry > 0.20:
			// Wide trail — give big runners room
			newStop = currentPrice * 0.94
		case pctFromEntry > 0.10:
			newStop = currentPrice * 0.95
		case pctFromEntry > 0.05:
			newStop = currentPrice * 0.96
		default:
			newStop = pos.EntryPrice // floor at breakeven
		}

		if newStop > pos.StopLoss {
			pos.StopLoss = newStop
			log.Printf("[%s] 📈 Trailing stop → $%.2f (%.1f%% from entry)",
				pos.Symbol, pos.StopLoss, pctFromEntry*100)
		}

		// If price slips back below the session's 20-MA proxy (entry + 10% band),
		// and we're not yet in meaningful profit, close the position.
		if pctFromEntry < 0.05 && currentPrice < pos.EntryPrice*0.96 {
			exitPrice := math.Max(pos.StopLoss, pos.EntryPrice)
			return executeExit(pos, exitPrice, now, "Trailing — retreated below threshold")
		}
	}

	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Exit execution helpers — each calls ep.PlaceSellOrder
// ─────────────────────────────────────────────────────────────────────────────

// executeStopOut sells all remaining shares at the stop price.
func executeStopOut(pos *RealtimePosition, stopPrice float64, t time.Time) bool {
	shares := int(math.Round(pos.Shares))
	if shares < 1 {
		return true
	}

	log.Printf("[%s] 🛑 STOP OUT @ $%.2f — selling %d shares", pos.Symbol, stopPrice, shares)

	if _, err := ep.PlaceSellOrder(pos.Symbol, shares, &stopPrice); err != nil {
		log.Printf("[%s] ❌ PlaceSellOrder error: %v — position removed from monitor anyway", pos.Symbol, err)
	}

	pl := (stopPrice - pos.EntryPrice) * float64(shares)
	totalPL := pl + pos.CumulativeProfit
	rr := 0.0
	if pos.InitialRisk > 0 {
		rr = (stopPrice - pos.EntryPrice) / pos.InitialRisk
	}

	reason := "Stop Loss Hit"
	if pos.ProfitTaken {
		reason = "Trailing Stop Hit (partial profit protected)"
	}
	if pos.CumulativeProfit > 0 {
		reason = fmt.Sprintf("%s — cumulative P/L incl. partials: $%.2f", reason, totalPL)
	}

	recordTrade(TradeRecord{
		Symbol:      pos.Symbol,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   stopPrice,
		Shares:      float64(shares),
		InitialRisk: pos.InitialRisk,
		ProfitLoss:  pl,
		RiskReward:  rr,
		EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
		ExitDate:    t.Format("2006-01-02"),
		ExitReason:  reason,
		IsWinner:    totalPL > 0,
	})

	removeFromWatchlist(pos.Symbol)
	pos.Shares = 0
	return true
}

// executeStrongEPProfit sells STRONG_EP_TAKE_PERCENT of shares on a strong move.
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

	if _, err := ep.PlaceSellOrder(pos.Symbol, sharesToSell, &currentPrice); err != nil {
		log.Printf("[%s] ❌ PlaceSellOrder error: %v", pos.Symbol, err)
	}

	pl := (currentPrice - pos.EntryPrice) * float64(sharesToSell)
	rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk

	recordTrade(TradeRecord{
		Symbol:      pos.Symbol,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   currentPrice,
		Shares:      float64(sharesToSell),
		InitialRisk: pos.InitialRisk,
		ProfitLoss:  pl,
		RiskReward:  rr,
		EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
		ExitDate:    t.Format("2006-01-02"),
		ExitReason:  fmt.Sprintf("Strong EP — %.0f%% sold", STRONG_EP_TAKE_PERCENT*100),
		IsWinner:    true,
	})

	pos.CumulativeProfit += pl
	pos.Shares -= float64(sharesToSell)
	pos.StopLoss = math.Max(pos.EntryPrice, pos.StopLoss)
	pos.ProfitTaken = true
	pos.TrailingStopMode = true

	log.Printf("[%s] ✅ %.0f shares remain | Cumulative P/L: $%.2f | Stop moved to BE: $%.2f",
		pos.Symbol, pos.Shares, pos.CumulativeProfit, pos.StopLoss)

	return pos.Shares <= 0
}

// executeProfitPartial sells pct of remaining shares at a profit level.
func executeProfitPartial(pos *RealtimePosition, currentPrice float64, t time.Time, pct float64, level int) {
	sharesToSell := int(math.Floor(pos.Shares * pct))
	if sharesToSell < 1 {
		sharesToSell = 1
	}
	if sharesToSell > int(pos.Shares) {
		sharesToSell = int(pos.Shares)
	}

	rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk
	log.Printf("[%s] 🎯 PROFIT LEVEL %d (%.2fR) — selling %d shares @ $%.2f",
		pos.Symbol, level, rr, sharesToSell, currentPrice)

	if _, err := ep.PlaceSellOrder(pos.Symbol, sharesToSell, &currentPrice); err != nil {
		log.Printf("[%s] ❌ PlaceSellOrder error: %v", pos.Symbol, err)
	}

	pl := (currentPrice - pos.EntryPrice) * float64(sharesToSell)

	recordTrade(TradeRecord{
		Symbol:      pos.Symbol,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   currentPrice,
		Shares:      float64(sharesToSell),
		InitialRisk: pos.InitialRisk,
		ProfitLoss:  pl,
		RiskReward:  rr,
		EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
		ExitDate:    t.Format("2006-01-02"),
		ExitReason:  fmt.Sprintf("Profit Level %d at %.2fR", level, rr),
		IsWinner:    true,
	})

	pos.CumulativeProfit += pl
	pos.Shares -= float64(sharesToSell)

	log.Printf("[%s] ✅ %.0f shares remain | Cumulative P/L: $%.2f | Stop: $%.2f",
		pos.Symbol, pos.Shares, pos.CumulativeProfit, pos.StopLoss)
}

// executeExit closes the full remaining position for a given reason.
func executeExit(pos *RealtimePosition, exitPrice float64, t time.Time, reason string) bool {
	shares := int(math.Round(pos.Shares))
	if shares < 1 {
		return true
	}

	log.Printf("[%s] 📤 EXIT — %s | Selling %d shares @ $%.2f", pos.Symbol, reason, shares, exitPrice)

	if _, err := ep.PlaceSellOrder(pos.Symbol, shares, &exitPrice); err != nil {
		log.Printf("[%s] ❌ PlaceSellOrder error: %v — removing from monitor anyway", pos.Symbol, err)
	}

	pl := (exitPrice - pos.EntryPrice) * float64(shares)
	totalPL := pl + pos.CumulativeProfit
	rr := 0.0
	if pos.InitialRisk > 0 {
		rr = (exitPrice - pos.EntryPrice) / pos.InitialRisk
	}

	fullReason := reason
	if pos.CumulativeProfit != 0 {
		fullReason = fmt.Sprintf("%s (prev. partial P/L: $%.2f, total: $%.2f)", reason, pos.CumulativeProfit, totalPL)
	}

	recordTrade(TradeRecord{
		Symbol:      pos.Symbol,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   exitPrice,
		Shares:      float64(shares),
		InitialRisk: pos.InitialRisk,
		ProfitLoss:  pl,
		RiskReward:  rr,
		EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
		ExitDate:    t.Format("2006-01-02"),
		ExitReason:  fullReason,
		IsWinner:    totalPL > 0,
	})

	removeFromWatchlist(pos.Symbol)
	pos.Shares = 0
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

// printAccountStatus fetches live account figures from Alpaca and prints them.
// Called at startup and after every position closes.
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
// Stats summary — printed after every position closes
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
