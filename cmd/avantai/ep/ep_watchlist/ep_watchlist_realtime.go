package main

// import (
// 	"encoding/csv"
// 	"fmt"
// 	"log"
// 	"math"
// 	"os"
// 	"strconv"
// 	"strings"
// 	"sync"
// 	"time"

// 	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
// 	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
// 	"github.com/joho/godotenv"
// 	"path/filepath"

// 	ep "avantai/pkg/ep"
// )

// // ─────────────────────────────────────────────────────────────────────────────
// // Constants
// // ─────────────────────────────────────────────────────────────────────────────

// const (
// 	POLL_INTERVAL         = 60 * time.Second
// 	MARKET_CHECK_INTERVAL = 1 * time.Minute

// 	BREAKEVEN_TRIGGER_PERCENT = 0.02
// 	BREAKEVEN_TRIGGER_DAYS    = 2

// 	PROFIT_TAKE_1_RR      = 1.5
// 	PROFIT_TAKE_1_PERCENT = 0.25
// 	PROFIT_TAKE_2_RR      = 3.0
// 	PROFIT_TAKE_2_PERCENT = 0.25
// 	PROFIT_TAKE_3_RR      = 5.0
// 	PROFIT_TAKE_3_PERCENT = 0.25

// 	STRONG_EP_GAIN         = 0.15
// 	STRONG_EP_DAYS         = 3
// 	STRONG_EP_TAKE_PERCENT = 0.30

// 	MAX_DAYS_NO_FOLLOWTHROUGH = 8
// 	WEAK_CLOSE_THRESHOLD      = 0.30

// 	MIN_PRICE = 2.0
// 	MAX_PRICE = 200.0

// 	MARKET_OPEN_HOUR  = 9
// 	MARKET_OPEN_MIN   = 30
// 	MARKET_CLOSE_HOUR = 16
// 	MARKET_CLOSE_MIN  = 0
// 	EASTERN_TZ        = "America/New_York"

// 	// How often to re-sync the full position list from Alpaca.
// 	// Runs once at startup, then every POSITION_SYNC_INTERVAL while the market
// 	// is open (catches new fills without any manual watchlist editing).
// 	POSITION_SYNC_INTERVAL = 5 * time.Minute

// 	// For the first EARLY_OPEN_DURATION after market open, sync every
// 	// EARLY_OPEN_SYNC_INTERVAL to catch opening-bell fills quickly.
// 	EARLY_OPEN_DURATION      = 20 * time.Minute
// 	EARLY_OPEN_SYNC_INTERVAL = 1 * time.Minute
// )

// // ─────────────────────────────────────────────────────────────────────────────
// // Data structures
// // ─────────────────────────────────────────────────────────────────────────────

// type RealtimePosition struct {
// 	Symbol          string
// 	EntryPrice      float64
// 	StopLoss        float64
// 	InitialStopLoss float64
// 	InitialRisk     float64
// 	Shares          float64
// 	InitialShares   float64
// 	PurchaseDate    time.Time
// 	DaysHeld        int
// 	LastCheckDate   time.Time

// 	ProfitTaken  bool
// 	ProfitTaken2 bool
// 	ProfitTaken3 bool

// 	TrailingStopMode bool
// 	HighestPrice     float64

// 	CumulativeProfit float64

// 	SessionHigh float64
// 	SessionLow  float64
// 	SessionOpen float64

// 	WeakCloseDetected bool

// 	mu sync.Mutex
// }

// type TradeRecord struct {
// 	Symbol      string
// 	EntryPrice  float64
// 	ExitPrice   float64
// 	Shares      float64
// 	InitialRisk float64
// 	ProfitLoss  float64
// 	RiskReward  float64
// 	EntryDate   string
// 	ExitDate    string
// 	ExitReason  string
// 	IsWinner    bool
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // Global state
// // ─────────────────────────────────────────────────────────────────────────────

// var (
// 	activePositions map[string]*RealtimePosition = make(map[string]*RealtimePosition)
// 	positionsMu     sync.RWMutex
// 	tradeResultsMu  sync.Mutex

// 	alpacaClient *alpaca.Client
// 	mdClient     *marketdata.Client
// 	easternLoc   *time.Location

// 	lastPositionSync time.Time // tracks when we last called syncPositionsFromAlpaca
// 	marketOpenTime   time.Time // set each time the market transitions to open
// )

// // ─────────────────────────────────────────────────────────────────────────────
// // Injectable function variables (tests swap these for stubs)
// // ─────────────────────────────────────────────────────────────────────────────

// type Bar struct {
// 	Open, High, Low, Close float64
// }

// var getAlpacaPositionFn = func(symbol string) (float64, error) {
// 	pos, err := alpacaClient.GetPosition(symbol)
// 	if err != nil {
// 		return 0, err
// 	}
// 	qty, _ := pos.Qty.Float64()
// 	return qty, nil
// }

// var getIntradayBarsFn = func(symbol string, sessionStart, now time.Time) ([]Bar, error) {
// 	raw, err := mdClient.GetBars(symbol, marketdata.GetBarsRequest{
// 		TimeFrame: marketdata.OneMin,
// 		Start:     sessionStart,
// 		End:       now,
// 		Feed:      marketdata.IEX,
// 	})
// 	if err != nil {
// 		return nil, err
// 	}
// 	bars := make([]Bar, len(raw))
// 	for i, b := range raw {
// 		bars[i] = Bar{Open: b.Open, High: b.High, Low: b.Low, Close: b.Close}
// 	}
// 	return bars, nil
// }

// type AccountSnapshot struct {
// 	Equity      float64
// 	Cash        float64
// 	BuyingPower float64
// 	DayPL       float64
// }

// var getAccountFn = func() (*AccountSnapshot, error) {
// 	acct, err := alpacaClient.GetAccount()
// 	if err != nil {
// 		return nil, err
// 	}
// 	equity, _ := acct.Equity.Float64()
// 	cash, _ := acct.Cash.Float64()
// 	bp, _ := acct.BuyingPower.Float64()
// 	lastEquity, _ := acct.LastEquity.Float64()
// 	return &AccountSnapshot{
// 		Equity:      equity,
// 		Cash:        cash,
// 		BuyingPower: bp,
// 		DayPL:       equity - lastEquity,
// 	}, nil
// }

// // getAllAlpacaPositionsFn returns all open positions from Alpaca.
// // Tests replace this with a stub.
// var getAllAlpacaPositionsFn = func() ([]alpaca.Position, error) {
// 	return alpacaClient.GetPositions()
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // main
// // ─────────────────────────────────────────────────────────────────────────────

// func loadEnv() {
// 	dir, err := os.Getwd()
// 	if err != nil {
// 		return
// 	}
// 	for {
// 		candidate := filepath.Join(dir, ".env")
// 		if _, err := os.Stat(candidate); err == nil {
// 			godotenv.Load(candidate)
// 			return
// 		}
// 		parent := filepath.Dir(dir)
// 		if parent == dir {
// 			return
// 		}
// 		dir = parent
// 	}
// }

// func main() {
// 	loadEnv()

// 	apiKey := os.Getenv("ALPACA_API_KEY")
// 	apiSecret := os.Getenv("ALPACA_SECRET_KEY")
// 	baseURL := os.Getenv("ALPACA_PAPER_URL")

// 	if apiKey == "" || apiSecret == "" {
// 		log.Fatal("ALPACA_API_KEY and ALPACA_SECRET_KEY must be set")
// 	}

// 	alpacaClient = alpaca.NewClient(alpaca.ClientOpts{
// 		APIKey:    apiKey,
// 		APISecret: apiSecret,
// 		BaseURL:   baseURL,
// 	})
// 	mdClient = marketdata.NewClient(marketdata.ClientOpts{
// 		APIKey:    apiKey,
// 		APISecret: apiSecret,
// 	})

// 	var err error
// 	easternLoc, err = time.LoadLocation(EASTERN_TZ)
// 	if err != nil {
// 		log.Fatalf("Cannot load timezone %s: %v", EASTERN_TZ, err)
// 	}

// 	initTradeResultsFile()
// 	printAccountStatus("startup")

// 	// ── Immediately sync positions from Alpaca so we start monitoring right away
// 	syncPositionsFromAlpaca()

// 	// If we're starting up while the market is already open, record that open
// 	// time so the early-open fast-sync window is calculated correctly.
// 	if isMarketOpen() {
// 		now := time.Now().In(easternLoc)
// 		marketOpenTime = time.Date(now.Year(), now.Month(), now.Day(),
// 			MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, easternLoc)
// 	}

// 	log.Println("🚀 Real-time EP monitor started (Alpaca-sync mode).")
// 	log.Println("   Positions are fetched directly from your Alpaca account every 5 minutes.")
// 	log.Println("   For the first 20 min after open, positions are synced every 1 minute.")
// 	log.Println("   No watchlist.csv required.")
// 	log.Println("   Press Ctrl+C to stop.")

// 	wasOpen := isMarketOpen()

// 	for {
// 		open := isMarketOpen()

// 		if open {
// 			// Detect the market just transitioning from closed → open.
// 			if !wasOpen {
// 				now := time.Now().In(easternLoc)
// 				marketOpenTime = time.Date(now.Year(), now.Month(), now.Day(),
// 					MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, easternLoc)
// 				log.Printf("🔔 Market just opened — fast-sync active for %s",
// 					EARLY_OPEN_DURATION)
// 				syncPositionsFromAlpaca()
// 			} else {
// 				// Choose sync interval based on how long the market has been open.
// 				sinceOpen := time.Since(marketOpenTime)
// 				syncInterval := POSITION_SYNC_INTERVAL
// 				if sinceOpen < EARLY_OPEN_DURATION {
// 					syncInterval = EARLY_OPEN_SYNC_INTERVAL
// 					remaining := EARLY_OPEN_DURATION - sinceOpen
// 					log.Printf("⏱️  Early-open window: fast-sync every %s (%s remaining)",
// 						EARLY_OPEN_SYNC_INTERVAL, remaining.Round(time.Second))
// 				}

// 				if time.Since(lastPositionSync) >= syncInterval {
// 					syncPositionsFromAlpaca()
// 				}
// 			}

// 			evaluatePositions()
// 			time.Sleep(POLL_INTERVAL)
// 		} else {
// 			nextOpen := nextMarketOpen()
// 			sleepDur := time.Until(nextOpen)
// 			if sleepDur < time.Minute {
// 				sleepDur = MARKET_CHECK_INTERVAL
// 			}
// 			log.Printf("💤 Market closed. Next open ~%s  (sleeping %s)",
// 				nextOpen.In(easternLoc).Format("Mon 2006-01-02 15:04 MST"),
// 				sleepDur.Round(time.Minute))
// 			time.Sleep(sleepDur)

// 			// Sync once when market re-opens so overnight fills are picked up.
// 			if isMarketOpen() {
// 				syncPositionsFromAlpaca()
// 			}
// 		}

// 		wasOpen = open
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // syncPositionsFromAlpaca — the core replacement for checkAndProcessWatchlist
// // ─────────────────────────────────────────────────────────────────────────────

// // syncPositionsFromAlpaca fetches every open position from Alpaca and:
// //   - Adds a new RealtimePosition for any symbol not already monitored.
// //   - Updates share count for positions already being monitored.
// //   - Removes any position from activePositions that is no longer open in Alpaca
// //     (e.g. stopped out by a bracket order while the monitor was sleeping).
// func syncPositionsFromAlpaca() {
// 	lastPositionSync = time.Now()

// 	alpacaPositions, err := getAllAlpacaPositionsFn()
// 	if err != nil {
// 		log.Printf("⚠️  Could not fetch positions from Alpaca: %v", err)
// 		return
// 	}

// 	// Build a quick lookup set of symbols currently open in Alpaca.
// 	alpacaSymbols := make(map[string]alpaca.Position, len(alpacaPositions))
// 	for _, p := range alpacaPositions {
// 		alpacaSymbols[p.Symbol] = p
// 	}

// 	// ── 1. Add or update positions ────────────────────────────────────────────
// 	for _, ap := range alpacaPositions {
// 		qty, _ := ap.Qty.Float64()
// 		if qty <= 0 {
// 			continue // skip short / empty positions
// 		}

// 		avgEntry, _ := ap.AvgEntryPrice.Float64()
// 		if avgEntry <= 0 {
// 			log.Printf("[%s] ⚠️  Avg entry price unavailable — skipping", ap.Symbol)
// 			continue
// 		}

// 		// Filter by price range (same guard as before)
// 		if avgEntry < MIN_PRICE || avgEntry > MAX_PRICE {
// 			log.Printf("[%s] ⚠️  Avg entry $%.2f outside range ($%.0f–$%.0f) — skipping",
// 				ap.Symbol, avgEntry, MIN_PRICE, MAX_PRICE)
// 			continue
// 		}

// 		positionsMu.RLock()
// 		existing, alreadyTracked := activePositions[ap.Symbol]
// 		positionsMu.RUnlock()

// 		if alreadyTracked {
// 			// Just keep share count in sync.
// 			existing.mu.Lock()
// 			if existing.Shares != qty {
// 				log.Printf("[%s] 🔄 Share sync: %.0f → %.0f", ap.Symbol, existing.Shares, qty)
// 				existing.Shares = qty
// 			}
// 			existing.mu.Unlock()
// 			continue
// 		}

// 		// ── New position: build a RealtimePosition from the Alpaca data ───────
// 		//
// 		// Stop loss: derived as DEFAULT_STOP_PCT below avg entry because Alpaca
// 		// does not expose the stop leg of a bracket order via GetPositions.
// 		// Adjust DEFAULT_STOP_PCT to tighten or widen the default.
// 		const DEFAULT_STOP_PCT = 0.05
// 		stopPrice := avgEntry * (1.0 - DEFAULT_STOP_PCT)
// 		initialRisk := avgEntry - stopPrice

// 		// Use today as the purchase date if we can't determine it otherwise.
// 		// For a more accurate date you could cross-reference order history, but
// 		// today is safe for all exit-timing calculations on the first monitoring day.
// 		now := time.Now().In(easternLoc)
// 		purchaseDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, easternLoc)

// 		pos := &RealtimePosition{
// 			Symbol:          ap.Symbol,
// 			EntryPrice:      avgEntry,
// 			StopLoss:        stopPrice,
// 			InitialStopLoss: stopPrice,
// 			InitialRisk:     initialRisk,
// 			Shares:          qty,
// 			InitialShares:   qty,
// 			PurchaseDate:    purchaseDate,
// 			HighestPrice:    avgEntry,
// 			LastCheckDate:   purchaseDate,
// 			SessionHigh:     0,
// 			SessionLow:      math.MaxFloat64,
// 			SessionOpen:     0,
// 		}

// 		positionsMu.Lock()
// 		activePositions[ap.Symbol] = pos
// 		positionsMu.Unlock()

// 		log.Printf("[%s] 🟢 NEW POSITION DETECTED | Entry: $%.2f | Stop: $%.2f | Risk/share: $%.2f | Shares: %.0f (default stop %.0f%% below entry)",
// 			ap.Symbol, avgEntry, stopPrice, initialRisk, qty, DEFAULT_STOP_PCT*100)
// 	}

// 	// ── 2. Remove positions no longer open in Alpaca ──────────────────────────
// 	positionsMu.Lock()
// 	for sym := range activePositions {
// 		if _, stillOpen := alpacaSymbols[sym]; !stillOpen {
// 			log.Printf("[%s] 🔴 No longer open in Alpaca — removing from monitor", sym)
// 			delete(activePositions, sym)
// 		}
// 	}
// 	positionsMu.Unlock()

// 	log.Printf("🔁 Position sync complete — %d position(s) being monitored", func() int {
// 		positionsMu.RLock()
// 		defer positionsMu.RUnlock()
// 		return len(activePositions)
// 	}())
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // Market hours helpers
// // ─────────────────────────────────────────────────────────────────────────────

// func isMarketOpen() bool {
// 	now := time.Now().In(easternLoc)
// 	wd := now.Weekday()
// 	if wd == time.Saturday || wd == time.Sunday {
// 		return false
// 	}
// 	open := time.Date(now.Year(), now.Month(), now.Day(),
// 		MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, easternLoc)
// 	close_ := time.Date(now.Year(), now.Month(), now.Day(),
// 		MARKET_CLOSE_HOUR, MARKET_CLOSE_MIN, 0, 0, easternLoc)
// 	return now.After(open) && now.Before(close_)
// }

// func nextMarketOpen() time.Time {
// 	now := time.Now().In(easternLoc)
// 	candidate := time.Date(now.Year(), now.Month(), now.Day(),
// 		MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, easternLoc)
// 	if now.Before(candidate) && now.Weekday() != time.Saturday && now.Weekday() != time.Sunday {
// 		return candidate
// 	}
// 	for {
// 		candidate = candidate.AddDate(0, 0, 1)
// 		if candidate.Weekday() != time.Saturday && candidate.Weekday() != time.Sunday {
// 			return candidate
// 		}
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // Evaluation loop  (unchanged from original)
// // ─────────────────────────────────────────────────────────────────────────────

// func evaluatePositions() {
// 	positionsMu.RLock()
// 	if len(activePositions) == 0 {
// 		positionsMu.RUnlock()
// 		return
// 	}
// 	symbols := make([]string, 0, len(activePositions))
// 	for sym := range activePositions {
// 		symbols = append(symbols, sym)
// 	}
// 	positionsMu.RUnlock()

// 	log.Printf("📊 Evaluating %d active position(s)...", len(symbols))
// 	printAccountStatus("")

// 	var wg sync.WaitGroup
// 	var removeMu sync.Mutex
// 	toRemove := make(map[string]bool)

// 	for _, sym := range symbols {
// 		positionsMu.RLock()
// 		pos, ok := activePositions[sym]
// 		positionsMu.RUnlock()
// 		if !ok {
// 			continue
// 		}
// 		wg.Add(1)
// 		go func(p *RealtimePosition) {
// 			defer wg.Done()
// 			if closed := evaluatePosition(p); closed {
// 				removeMu.Lock()
// 				toRemove[p.Symbol] = true
// 				removeMu.Unlock()
// 			}
// 		}(pos)
// 	}

// 	wg.Wait()

// 	if len(toRemove) > 0 {
// 		positionsMu.Lock()
// 		for sym := range toRemove {
// 			delete(activePositions, sym)
// 		}
// 		positionsMu.Unlock()
// 		printCurrentStats()
// 		printAccountStatus("after close")
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // Per-position evaluation  (unchanged from original)
// // ─────────────────────────────────────────────────────────────────────────────

// func evaluatePosition(pos *RealtimePosition) bool {
// 	pos.mu.Lock()
// 	defer pos.mu.Unlock()

// 	now := time.Now().In(easternLoc)

// 	qty, err := getAlpacaPositionFn(pos.Symbol)
// 	if err != nil {
// 		log.Printf("[%s] ⚠️  Position no longer found in Alpaca — removing from monitor", pos.Symbol)
// 		return true
// 	}
// 	if qty != pos.Shares && qty > 0 {
// 		log.Printf("[%s] 🔄 Share count updated %.0f → %.0f (Alpaca sync)",
// 			pos.Symbol, pos.Shares, qty)
// 		pos.Shares = qty
// 	}

// 	today := now.Format("2006-01-02")
// 	sessionStart := time.Date(now.Year(), now.Month(), now.Day(),
// 		MARKET_OPEN_HOUR, MARKET_OPEN_MIN, 0, 0, easternLoc)

// 	bars, barsErr := getIntradayBarsFn(pos.Symbol, sessionStart, now)
// 	if barsErr != nil || len(bars) == 0 {
// 		log.Printf("[%s] ⚠️  Cannot fetch intraday bars: %v — skipping tick", pos.Symbol, barsErr)
// 		return false
// 	}

// 	sessionHigh := 0.0
// 	sessionLow := math.MaxFloat64
// 	sessionOpen := bars[0].Open
// 	latestClose := bars[len(bars)-1].Close

// 	for _, b := range bars {
// 		if b.High > sessionHigh {
// 			sessionHigh = b.High
// 		}
// 		if b.Low < sessionLow {
// 			sessionLow = b.Low
// 		}
// 	}

// 	pos.SessionHigh = sessionHigh
// 	pos.SessionLow = sessionLow
// 	pos.SessionOpen = sessionOpen

// 	if sessionHigh > pos.HighestPrice {
// 		pos.HighestPrice = sessionHigh
// 	}

// 	if today != pos.LastCheckDate.Format("2006-01-02") {
// 		pos.DaysHeld++
// 		pos.LastCheckDate = now
// 		pos.SessionHigh = 0
// 		pos.SessionLow = math.MaxFloat64
// 	}

// 	currentPrice := latestClose
// 	currentGain := currentPrice - pos.EntryPrice
// 	currentRR := 0.0
// 	if pos.InitialRisk > 0 {
// 		currentRR = currentGain / pos.InitialRisk
// 	}

// 	log.Printf("[%s] Day %d (%s) | Close: $%.2f | SessionH: $%.2f | SessionL: $%.2f | Gain: $%.2f (%.1f%%) | R/R: %.2fR | Stop: $%.2f | Shares: %.0f",
// 		pos.Symbol, pos.DaysHeld, today,
// 		currentPrice, sessionHigh, sessionLow,
// 		currentGain, (currentGain/pos.EntryPrice)*100,
// 		currentRR, pos.StopLoss, pos.Shares)

// 	if sessionHigh > 0 && !pos.WeakCloseDetected {
// 		closeFromHigh := (sessionHigh - currentPrice) / sessionHigh
// 		if closeFromHigh >= WEAK_CLOSE_THRESHOLD {
// 			log.Printf("[%s] ⚠️  WEAK CLOSE: $%.2f is %.1f%% below session high $%.2f — exiting",
// 				pos.Symbol, currentPrice, closeFromHigh*100, sessionHigh)
// 			pos.WeakCloseDetected = true
// 			return executeExit(pos, currentPrice, now, "Weak Close")
// 		}
// 	}

// 	if sessionLow <= pos.StopLoss || currentPrice <= pos.StopLoss {
// 		return executeStopOut(pos, pos.StopLoss, now)
// 	}

// 	if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS && !pos.ProfitTaken {
// 		pctGain := (sessionHigh - pos.EntryPrice) / pos.EntryPrice
// 		if pctGain >= BREAKEVEN_TRIGGER_PERCENT && pos.StopLoss < pos.EntryPrice {
// 			pos.StopLoss = pos.EntryPrice
// 			log.Printf("[%s] 🔒 BREAKEVEN STOP SET — session touched +%.1f%% on day %d",
// 				pos.Symbol, pctGain*100, pos.DaysHeld)
// 		}
// 	}

// 	if pos.DaysHeld >= MAX_DAYS_NO_FOLLOWTHROUGH && currentRR < 0.5 && !pos.ProfitTaken {
// 		tighter := math.Max(pos.EntryPrice-(pos.InitialRisk*0.3), pos.EntryPrice)
// 		if tighter > pos.StopLoss {
// 			pos.StopLoss = tighter
// 			log.Printf("[%s] ⚠️  No follow-through after %d days — stop tightened to $%.2f",
// 				pos.Symbol, MAX_DAYS_NO_FOLLOWTHROUGH, pos.StopLoss)
// 		}
// 	}

// 	pctGain := (currentPrice - pos.EntryPrice) / pos.EntryPrice
// 	if pos.DaysHeld <= STRONG_EP_DAYS && pctGain >= STRONG_EP_GAIN && !pos.ProfitTaken {
// 		closed := executeStrongEPProfit(pos, currentPrice, now)
// 		if closed {
// 			return true
// 		}
// 		return false
// 	}

// 	if currentRR >= PROFIT_TAKE_1_RR && !pos.ProfitTaken {
// 		executeProfitPartial(pos, currentPrice, now, PROFIT_TAKE_1_PERCENT, 1)
// 		pos.TrailingStopMode = true
// 		pos.ProfitTaken = true
// 		if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS {
// 			pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice)
// 		}
// 		return pos.Shares <= 0
// 	}
// 	if currentRR >= PROFIT_TAKE_2_RR && pos.ProfitTaken && !pos.ProfitTaken2 {
// 		executeProfitPartial(pos, currentPrice, now, PROFIT_TAKE_2_PERCENT, 2)
// 		pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice+(pos.InitialRisk*1.0))
// 		pos.ProfitTaken2 = true
// 		return pos.Shares <= 0
// 	}
// 	if currentRR >= PROFIT_TAKE_3_RR && pos.ProfitTaken2 && !pos.ProfitTaken3 {
// 		executeProfitPartial(pos, currentPrice, now, PROFIT_TAKE_3_PERCENT, 3)
// 		pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice+(pos.InitialRisk*2.0))
// 		pos.ProfitTaken3 = true
// 		return pos.Shares <= 0
// 	}

// 	if pos.TrailingStopMode {
// 		pctFromEntry := (currentPrice - pos.EntryPrice) / pos.EntryPrice
// 		var newStop float64
// 		switch {
// 		case pctFromEntry > 0.20:
// 			newStop = currentPrice * 0.94
// 		case pctFromEntry > 0.10:
// 			newStop = currentPrice * 0.95
// 		case pctFromEntry > 0.05:
// 			newStop = currentPrice * 0.96
// 		default:
// 			newStop = pos.EntryPrice
// 		}
// 		if newStop > pos.StopLoss {
// 			pos.StopLoss = newStop
// 			log.Printf("[%s] 📈 Trailing stop → $%.2f (%.1f%% from entry)",
// 				pos.Symbol, pos.StopLoss, pctFromEntry*100)
// 		}
// 		if pctFromEntry < 0.05 && currentPrice < pos.EntryPrice*0.96 {
// 			exitPrice := math.Max(pos.StopLoss, pos.EntryPrice)
// 			return executeExit(pos, exitPrice, now, "Trailing — retreated below threshold")
// 		}
// 	}

// 	return false
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // Exit helpers  (unchanged from original)
// // ─────────────────────────────────────────────────────────────────────────────

// func executeStopOut(pos *RealtimePosition, stopPrice float64, t time.Time) bool {
// 	shares := int(math.Round(pos.Shares))
// 	if shares < 1 {
// 		return true
// 	}
// 	log.Printf("[%s] 🛑 STOP OUT @ $%.2f — selling %d shares", pos.Symbol, stopPrice, shares)
// 	if _, err := ep.PlaceSellOrder(pos.Symbol, shares, &stopPrice); err != nil {
// 		log.Printf("[%s] ❌ PlaceSellOrder error: %v", pos.Symbol, err)
// 	}
// 	pl := (stopPrice - pos.EntryPrice) * float64(shares)
// 	totalPL := pl + pos.CumulativeProfit
// 	rr := 0.0
// 	if pos.InitialRisk > 0 {
// 		rr = (stopPrice - pos.EntryPrice) / pos.InitialRisk
// 	}
// 	reason := "Stop Loss Hit"
// 	if pos.ProfitTaken {
// 		reason = "Trailing Stop Hit (partial profit protected)"
// 	}
// 	if pos.CumulativeProfit > 0 {
// 		reason = fmt.Sprintf("%s — cumulative P/L incl. partials: $%.2f", reason, totalPL)
// 	}
// 	recordTrade(TradeRecord{
// 		Symbol: pos.Symbol, EntryPrice: pos.EntryPrice, ExitPrice: stopPrice,
// 		Shares: float64(shares), InitialRisk: pos.InitialRisk, ProfitLoss: pl,
// 		RiskReward: rr, EntryDate: pos.PurchaseDate.Format("2006-01-02"),
// 		ExitDate: t.Format("2006-01-02"), ExitReason: reason, IsWinner: totalPL > 0,
// 	})
// 	pos.Shares = 0
// 	return true
// }

// func executeStrongEPProfit(pos *RealtimePosition, currentPrice float64, t time.Time) bool {
// 	sharesToSell := int(math.Floor(pos.Shares * STRONG_EP_TAKE_PERCENT))
// 	if sharesToSell < 1 {
// 		sharesToSell = 1
// 	}
// 	if sharesToSell > int(pos.Shares) {
// 		sharesToSell = int(pos.Shares)
// 	}
// 	log.Printf("[%s] 🚀 STRONG EP! Selling %.0f%% (%d shares) @ $%.2f",
// 		pos.Symbol, STRONG_EP_TAKE_PERCENT*100, sharesToSell, currentPrice)
// 	if _, err := ep.PlaceSellOrder(pos.Symbol, sharesToSell, &currentPrice); err != nil {
// 		log.Printf("[%s] ❌ PlaceSellOrder error: %v", pos.Symbol, err)
// 	}
// 	pl := (currentPrice - pos.EntryPrice) * float64(sharesToSell)
// 	rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk
// 	recordTrade(TradeRecord{
// 		Symbol: pos.Symbol, EntryPrice: pos.EntryPrice, ExitPrice: currentPrice,
// 		Shares: float64(sharesToSell), InitialRisk: pos.InitialRisk, ProfitLoss: pl,
// 		RiskReward: rr, EntryDate: pos.PurchaseDate.Format("2006-01-02"),
// 		ExitDate: t.Format("2006-01-02"),
// 		ExitReason: fmt.Sprintf("Strong EP — %.0f%% sold", STRONG_EP_TAKE_PERCENT*100),
// 		IsWinner: true,
// 	})
// 	pos.CumulativeProfit += pl
// 	pos.Shares -= float64(sharesToSell)
// 	pos.StopLoss = math.Max(pos.EntryPrice, pos.StopLoss)
// 	pos.ProfitTaken = true
// 	pos.TrailingStopMode = true
// 	log.Printf("[%s] ✅ %.0f shares remain | Cumulative P/L: $%.2f | Stop → BE: $%.2f",
// 		pos.Symbol, pos.Shares, pos.CumulativeProfit, pos.StopLoss)
// 	return pos.Shares <= 0
// }

// func executeProfitPartial(pos *RealtimePosition, currentPrice float64, t time.Time, pct float64, level int) {
// 	sharesToSell := int(math.Floor(pos.Shares * pct))
// 	if sharesToSell < 1 {
// 		sharesToSell = 1
// 	}
// 	if sharesToSell > int(pos.Shares) {
// 		sharesToSell = int(pos.Shares)
// 	}
// 	rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk
// 	log.Printf("[%s] 🎯 PROFIT LEVEL %d (%.2fR) — selling %d shares @ $%.2f",
// 		pos.Symbol, level, rr, sharesToSell, currentPrice)
// 	if _, err := ep.PlaceSellOrder(pos.Symbol, sharesToSell, &currentPrice); err != nil {
// 		log.Printf("[%s] ❌ PlaceSellOrder error: %v", pos.Symbol, err)
// 	}
// 	pl := (currentPrice - pos.EntryPrice) * float64(sharesToSell)
// 	recordTrade(TradeRecord{
// 		Symbol: pos.Symbol, EntryPrice: pos.EntryPrice, ExitPrice: currentPrice,
// 		Shares: float64(sharesToSell), InitialRisk: pos.InitialRisk, ProfitLoss: pl,
// 		RiskReward: rr, EntryDate: pos.PurchaseDate.Format("2006-01-02"),
// 		ExitDate: t.Format("2006-01-02"),
// 		ExitReason: fmt.Sprintf("Profit Level %d at %.2fR", level, rr),
// 		IsWinner: true,
// 	})
// 	pos.CumulativeProfit += pl
// 	pos.Shares -= float64(sharesToSell)
// 	log.Printf("[%s] ✅ %.0f shares remain | Cumulative P/L: $%.2f | Stop: $%.2f",
// 		pos.Symbol, pos.Shares, pos.CumulativeProfit, pos.StopLoss)
// }

// func executeExit(pos *RealtimePosition, exitPrice float64, t time.Time, reason string) bool {
// 	shares := int(math.Round(pos.Shares))
// 	if shares < 1 {
// 		return true
// 	}
// 	log.Printf("[%s] 📤 EXIT — %s | Selling %d shares @ $%.2f", pos.Symbol, reason, shares, exitPrice)
// 	if _, err := ep.PlaceSellOrder(pos.Symbol, shares, &exitPrice); err != nil {
// 		log.Printf("[%s] ❌ PlaceSellOrder error: %v", pos.Symbol, err)
// 	}
// 	pl := (exitPrice - pos.EntryPrice) * float64(shares)
// 	totalPL := pl + pos.CumulativeProfit
// 	rr := 0.0
// 	if pos.InitialRisk > 0 {
// 		rr = (exitPrice - pos.EntryPrice) / pos.InitialRisk
// 	}
// 	fullReason := reason
// 	if pos.CumulativeProfit != 0 {
// 		fullReason = fmt.Sprintf("%s (prev. partial P/L: $%.2f, total: $%.2f)", reason, pos.CumulativeProfit, totalPL)
// 	}
// 	recordTrade(TradeRecord{
// 		Symbol: pos.Symbol, EntryPrice: pos.EntryPrice, ExitPrice: exitPrice,
// 		Shares: float64(shares), InitialRisk: pos.InitialRisk, ProfitLoss: pl,
// 		RiskReward: rr, EntryDate: pos.PurchaseDate.Format("2006-01-02"),
// 		ExitDate: t.Format("2006-01-02"), ExitReason: fullReason, IsWinner: totalPL > 0,
// 	})
// 	pos.Shares = 0
// 	return true
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // Trade results CSV  (unchanged)
// // ─────────────────────────────────────────────────────────────────────────────

// func initTradeResultsFile() {
// 	const filename = "trade_results.csv"
// 	if _, err := os.Stat(filename); os.IsNotExist(err) {
// 		f, err := os.Create(filename)
// 		if err != nil {
// 			log.Printf("⚠️  Cannot create trade_results.csv: %v", err)
// 			return
// 		}
// 		defer f.Close()
// 		w := csv.NewWriter(f)
// 		_ = w.Write([]string{
// 			"Symbol", "EntryPrice", "ExitPrice", "Shares", "InitialRisk",
// 			"ProfitLoss", "RiskReward", "EntryDate", "ExitDate", "ExitReason", "IsWinner",
// 		})
// 		w.Flush()
// 		log.Println("📄 Created trade_results.csv")
// 	}
// }

// func recordTrade(r TradeRecord) {
// 	tradeResultsMu.Lock()
// 	defer tradeResultsMu.Unlock()
// 	f, err := os.OpenFile("trade_results.csv", os.O_APPEND|os.O_WRONLY, 0644)
// 	if err != nil {
// 		log.Printf("⚠️  Cannot open trade_results.csv: %v", err)
// 		return
// 	}
// 	defer f.Close()
// 	w := csv.NewWriter(f)
// 	_ = w.Write([]string{
// 		r.Symbol,
// 		fmt.Sprintf("%.2f", r.EntryPrice),
// 		fmt.Sprintf("%.2f", r.ExitPrice),
// 		fmt.Sprintf("%.2f", r.Shares),
// 		fmt.Sprintf("%.2f", r.InitialRisk),
// 		fmt.Sprintf("%.2f", r.ProfitLoss),
// 		fmt.Sprintf("%.2f", r.RiskReward),
// 		r.EntryDate,
// 		r.ExitDate,
// 		r.ExitReason,
// 		fmt.Sprintf("%t", r.IsWinner),
// 	})
// 	w.Flush()
// 	log.Printf("[%s] ✍️  Recorded | P/L: $%.2f | R/R: %.2fR | Winner: %t",
// 		r.Symbol, r.ProfitLoss, r.RiskReward, r.IsWinner)
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // Account status + stats  (unchanged)
// // ─────────────────────────────────────────────────────────────────────────────

// func printAccountStatus(label string) {
// 	snap, err := getAccountFn()
// 	if err != nil {
// 		log.Printf("⚠️  Could not fetch account info: %v", err)
// 		return
// 	}
// 	dayPLSign := "+"
// 	if snap.DayPL < 0 {
// 		dayPLSign = ""
// 	}
// 	sep := strings.Repeat("─", 65)
// 	fmt.Println("\n" + sep)
// 	if label != "" {
// 		fmt.Printf("  💼  ACCOUNT STATUS  (%s)\n", label)
// 	} else {
// 		fmt.Println("  💼  ACCOUNT STATUS")
// 	}
// 	fmt.Println(sep)
// 	fmt.Printf("  Equity:        $%12.2f\n", snap.Equity)
// 	fmt.Printf("  Cash:          $%12.2f\n", snap.Cash)
// 	fmt.Printf("  Buying Power:  $%12.2f\n", snap.BuyingPower)
// 	fmt.Printf("  Day P/L:        %s$%.2f\n", dayPLSign, snap.DayPL)
// 	fmt.Println(sep + "\n")
// }

// func printCurrentStats() {
// 	tradeResultsMu.Lock()
// 	defer tradeResultsMu.Unlock()
// 	f, err := os.Open("trade_results.csv")
// 	if err != nil {
// 		return
// 	}
// 	defer f.Close()
// 	records, err := csv.NewReader(f).ReadAll()
// 	if err != nil || len(records) <= 1 {
// 		return
// 	}
// 	type stat struct {
// 		winners, losers int
// 		totalPL         float64
// 		winRR, lossRR   float64
// 	}
// 	var s stat
// 	for i := 1; i < len(records); i++ {
// 		row := records[i]
// 		if len(row) < 11 {
// 			continue
// 		}
// 		pl, _ := strconv.ParseFloat(row[5], 64)
// 		rr, _ := strconv.ParseFloat(row[6], 64)
// 		isWinner := row[10] == "true"
// 		s.totalPL += pl
// 		if isWinner {
// 			s.winners++
// 			s.winRR += rr
// 		} else {
// 			s.losers++
// 			s.lossRR += rr
// 		}
// 	}
// 	total := s.winners + s.losers
// 	if total == 0 {
// 		return
// 	}
// 	winRate := float64(s.winners) / float64(total) * 100
// 	avgWinRR, avgLossRR := 0.0, 0.0
// 	if s.winners > 0 {
// 		avgWinRR = s.winRR / float64(s.winners)
// 	}
// 	if s.losers > 0 {
// 		avgLossRR = s.lossRR / float64(s.losers)
// 	}
// 	sep := strings.Repeat("─", 65)
// 	fmt.Println("\n" + sep)
// 	fmt.Println("  📈  LIVE TRADING STATS")
// 	fmt.Println(sep)
// 	fmt.Printf("  Trades: %d  |  Winners: %d (%.1f%%)  |  Losers: %d (%.1f%%)\n",
// 		total, s.winners, winRate, s.losers, 100-winRate)
// 	fmt.Printf("  Avg Win R/R: %.2fR  |  Avg Loss R/R: %.2fR\n", avgWinRR, avgLossRR)
// 	fmt.Printf("  Total Realised P/L: $%.2f\n", s.totalPL)
// 	fmt.Println(sep + "\n")
// }