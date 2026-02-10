package main

import (
	"avantai/pkg/ep"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	ALPACA_BASE_URL = "https://data.alpaca.markets/v2"
	CHECK_INTERVAL  = 5 * time.Minute

	// IMPROVED: Wider stop to avoid premature stop-outs
	MAX_STOP_DISTANCE_MULTIPLIER = 2.0 // Was 1.5
	ATR_PERIOD                   = 14

	// IMPROVED: Earlier breakeven protection
	BREAKEVEN_TRIGGER_PERCENT = 0.02 // Move to BE at +2%
	BREAKEVEN_TRIGGER_DAYS    = 2    // Can trigger after day 2

	// IMPROVED: Graduated profit taking
	PROFIT_TAKE_1_RR      = 1.5  // First exit at 1.5R
	PROFIT_TAKE_1_PERCENT = 0.25 // Take 25%
	PROFIT_TAKE_2_RR      = 3.0  // Second exit at 3R
	PROFIT_TAKE_2_PERCENT = 0.25 // Take another 25%
	PROFIT_TAKE_3_RR      = 5.0  // Third exit at 5R
	PROFIT_TAKE_3_PERCENT = 0.25 // Take another 25%

	STRONG_EP_GAIN         = 0.15 // 15% in 3 days
	STRONG_EP_DAYS         = 3
	STRONG_EP_TAKE_PERCENT = 0.30 // Take 30%

	// Trailing stop
	MA_PERIOD = 20

	// Weak close threshold
	WEAK_CLOSE_THRESHOLD = 0.30

	// IMPROVED: More patience
	MAX_DAYS_NO_FOLLOWTHROUGH = 8 // Was 5

	// NEW: Entry filters
	MIN_VOLUME_RATIO = 1.2
	MIN_PRICE        = 2.0
	MAX_PRICE        = 200.0

	// Market hours (ET)
	MARKET_OPEN_HOUR    = 9
	MARKET_OPEN_MIN     = 30
	MARKET_CLOSE_HOUR   = 16
	MARKET_CLOSE_MIN    = 0
	PRE_CLOSE_CHECK_MIN = 5

	// Intraday monitoring for entry day
	INTRADAY_CHECK_INTERVAL = 15 * time.Minute
)

type AlpacaBar struct {
	T string  `json:"t"` // Timestamp
	O float64 `json:"o"` // Open
	H float64 `json:"h"` // High
	L float64 `json:"l"` // Low
	C float64 `json:"c"` // Close
	V float64 `json:"v"` // Volume
}

type AlpacaBarsResponse struct {
	Bars          map[string][]AlpacaBar `json:"bars"`
	NextPageToken string                 `json:"next_page_token,omitempty"`
}

type AlpacaLatestQuote struct {
	Symbol string `json:"symbol"`
	Quote  struct {
		T  string  `json:"t"`
		Ax string  `json:"ax"`
		Ap float64 `json:"ap"`
		As int     `json:"as"`
		Bx string  `json:"bx"`
		Bp float64 `json:"bp"`
		Bs int     `json:"bs"`
	} `json:"quote"`
}

type Position struct {
	Symbol           string
	EntryPrice       float64
	StopLoss         float64
	Shares           float64
	PurchaseDate     time.Time
	Status           string
	ProfitTaken      bool
	ProfitTaken2     bool
	ProfitTaken3     bool
	DaysHeld         int
	LastCheckDate    time.Time
	InitialStopLoss  float64
	InitialRisk      float64
	EPDayLow         float64
	EPDayHigh        float64
	HighestPrice     float64
	TrailingStopMode bool
	CumulativeProfit float64
	InitialShares    float64
	AverageVolume    float64
}

type TradeResult struct {
	Symbol      string
	EntryPrice  float64
	ExitPrice   float64
	Shares      float64
	ProfitLoss  float64
	RiskReward  float64
	EntryDate   string
	ExitDate    string
	ExitReason  string
	IsWinner    bool
	AccountSize float64
	InitialRisk float64
}

var (
	activePositions   = make(map[string]*Position)
	processedSymbols  = make(map[string]bool)
	accountSize       float64
	riskPerTrade      float64
	alpacaKey         string
	alpacaSecret      string
	lastModTime       time.Time
	historicalCache   = make(map[string][]AlpacaBar)
	lastMarketClose   time.Time
	preCloseChecked   bool
	stopAlerts        = make(map[string]bool)
	profitAlerts      = make(map[string]bool)
	watchlistPath     string
	pythonScriptPath  = "AutoStocks/premarket.py"
	autoExecuteTrades = true // Set to false to disable automatic order execution
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, checking for environment variables")
	}

	alpacaKey = os.Getenv("ALPACA_API_KEY")
	alpacaSecret = os.Getenv("ALPACA_SECRET_KEY")

	if alpacaKey == "" || alpacaSecret == "" {
		log.Fatal("ALPACA_API_KEY and ALPACA_SECRET_KEY must be set in environment or .env file")
	}

	// Check if auto-execute is disabled via environment variable
	if os.Getenv("AUTO_EXECUTE_TRADES") == "false" {
		autoExecuteTrades = false
		log.Println("⚠️  AUTO TRADE EXECUTION DISABLED - You will need to manually execute trades")
	} else {
		log.Println("✅ AUTO TRADE EXECUTION ENABLED - Orders will be placed automatically via Python script")
	}

	// Check if Python script path is overridden
	if customPath := os.Getenv("PYTHON_SCRIPT_PATH"); customPath != "" {
		pythonScriptPath = customPath
		log.Printf("Using custom Python script path: %s", pythonScriptPath)
	}

	accountSize = getAccountSize()
	riskPerTrade = getRiskPerTrade()
	log.Printf("Starting Account Size: $%.2f", accountSize)
	log.Printf("Risk Per Trade: %.2f%%", riskPerTrade*100)

	initializeTradeResultsFile()

	log.Println("🕐 Starting ENHANCED LIVE TRADING mode...")
	log.Println("Monitoring watchlist and active positions during market hours")
	log.Println("")
	log.Println("📋 ENHANCED EP TRADING RULES IMPLEMENTED:")
	log.Println("  1. Entry Filters: Volume 1.2x avg, Price $2-$200, Above 20-day MA")
	log.Println("  2. Stop-Loss: Just below EP day low, max 2.0x ATR distance (WIDER)")
	log.Println("  3. Breakeven: Move to BE at +2% gain after Day 2 (EARLIER)")
	log.Println("  4. Graduated Profit Taking:")
	log.Println("     - Level 1: 25% at 1.5R")
	log.Println("     - Level 2: 25% at 3.0R (stop locked at +1R)")
	log.Println("     - Level 3: 25% at 5.0R (stop locked at +2R)")
	log.Println("  5. Strong EP: 30% at 15% gain in 3 days")
	log.Println("  6. Dynamic Trailing Stop: Adjusts based on gain percentage")
	log.Println("     - Wide trailing (6% below) if gain > 20%")
	log.Println("     - Medium trailing (5% below) if gain > 10%")
	log.Println("     - Tight trailing (4% below MA) if gain > 5%")
	log.Println("  7. Time-Frame: Tighten stops if no follow-through within 8 days")
	log.Println("")
	log.Println("📊 Entry day positions: Intraday monitoring every 15 minutes using Alpaca")
	log.Println("⏰ Pre-close check will run at 3:55 PM ET (5 min before close)")
	log.Println("📊 EOD processing will run after 4:00 PM ET")
	log.Println("Press Ctrl+C to stop")

	loadExistingPositions()
	displayPositionSummary()

	lastIntradayCheck := time.Time{}

	for {
		now := time.Now()

		if now.Hour() < MARKET_OPEN_HOUR || (now.Hour() == MARKET_OPEN_HOUR && now.Minute() < MARKET_OPEN_MIN) {
			preCloseChecked = false
		}

		if isMarketHours(now) {
			checkAndProcessWatchlist()

			if time.Since(lastIntradayCheck) >= INTRADAY_CHECK_INTERVAL {
				monitorIntradayEntryPositions()
				lastIntradayCheck = now
			}

			monitorActivePositions()

			if isPreCloseWindow(now) && !preCloseChecked {
				log.Println("⏰ 5 MINUTES TO MARKET CLOSE - Running pre-close position check...")
				performPreCloseCheck()
				preCloseChecked = true
			}
		} else if justAfterMarketClose(now) {
			log.Println("⏰ Market closed. Waiting for EOD data...")
			time.Sleep(10 * time.Minute)

			processEndOfDayPositions()
			lastMarketClose = time.Now()
			preCloseChecked = false
		}

		time.Sleep(CHECK_INTERVAL)
	}
}

func makeAlpacaRequest(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("APCA-API-KEY-ID", alpacaKey)
	req.Header.Set("APCA-API-SECRET-KEY", alpacaSecret)

	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}

func getAccountSize() float64 {
	accountSizeStr := os.Getenv("ACCOUNT_SIZE")
	if accountSizeStr == "" {
		log.Println("ACCOUNT_SIZE not set, using default $10,000")
		return 10000.0
	}
	size, err := strconv.ParseFloat(accountSizeStr, 64)
	if err != nil {
		log.Println("Invalid ACCOUNT_SIZE, using default $10,000")
		return 10000.0
	}
	return size
}

func getRiskPerTrade() float64 {
	riskStr := os.Getenv("RISK_PER_TRADE")
	if riskStr == "" {
		log.Println("RISK_PER_TRADE not set, using default 1%")
		return 0.01
	}
	risk, err := strconv.ParseFloat(riskStr, 64)
	if err != nil {
		log.Println("Invalid RISK_PER_TRADE, using default 1%")
		return 0.01
	}
	return risk
}

func updateAccountSize(amount float64) {
	accountSize += amount
	log.Printf("💰 Account balance: $%.2f (change: $%.2f)", accountSize, amount)
}

func getTotalAccountValue() float64 {
	cashBalance := accountSize
	totalPositionValue := 0.0

	for _, pos := range activePositions {
		currentPrice := getCurrentPrice(pos.Symbol)
		if currentPrice > 0 {
			positionValue := currentPrice * pos.Shares
			totalPositionValue += positionValue
		}
	}

	return cashBalance + totalPositionValue
}

func isMarketHours(t time.Time) bool {
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return false
	}

	hour, min, _ := t.Clock()
	openMinutes := MARKET_OPEN_HOUR*60 + MARKET_OPEN_MIN
	closeMinutes := MARKET_CLOSE_HOUR*60 + MARKET_CLOSE_MIN
	currentMinutes := hour*60 + min

	return currentMinutes >= openMinutes && currentMinutes < closeMinutes
}

func isPreCloseWindow(t time.Time) bool {
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return false
	}

	hour, min, _ := t.Clock()
	closeMinutes := MARKET_CLOSE_HOUR*60 + MARKET_CLOSE_MIN
	preCloseMinutes := closeMinutes - PRE_CLOSE_CHECK_MIN
	currentMinutes := hour*60 + min

	return currentMinutes >= preCloseMinutes && currentMinutes < closeMinutes
}

func justAfterMarketClose(t time.Time) bool {
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return false
	}

	hour, min, _ := t.Clock()
	closeMinutes := MARKET_CLOSE_HOUR*60 + MARKET_CLOSE_MIN
	currentMinutes := hour*60 + min

	if currentMinutes >= closeMinutes && currentMinutes < closeMinutes+30 {
		return lastMarketClose.Day() != t.Day()
	}

	return false
}

func monitorIntradayEntryPositions() {
	today := time.Now().Format("2006-01-02")

	for symbol, pos := range activePositions {
		if pos.PurchaseDate.Format("2006-01-02") == today && pos.DaysHeld <= 1 {
			hasWeakClose, weakClosePrice := checkWeakCloseIntraday(symbol, time.Now())
			if hasWeakClose && weakClosePrice > 0 {
				log.Printf("[%s] ⚠️  Weak close detected intraday, closing position @ $%.2f", symbol, weakClosePrice)

				// Execute sell order
				if err := executeSellOrder(symbol, pos.Shares, weakClosePrice); err != nil {
					log.Printf("[%s] ⚠️  Failed to execute sell order, but recording exit", symbol)
				}

				totalProceeds := weakClosePrice * pos.Shares
				costBasis := pos.EntryPrice * pos.Shares
				profitLoss := totalProceeds - costBasis
				totalProfitLoss := profitLoss + pos.CumulativeProfit

				updateAccountSize(totalProceeds)

				exitReason := "Weak Close Intraday"
				if pos.CumulativeProfit > 0 {
					exitReason = fmt.Sprintf("%s (Previous partial profits: $%.2f)", exitReason, pos.CumulativeProfit)
				}

				recordTradeResult(TradeResult{
					Symbol:      symbol,
					EntryPrice:  pos.EntryPrice,
					ExitPrice:   weakClosePrice,
					Shares:      pos.Shares,
					InitialRisk: pos.InitialRisk,
					ProfitLoss:  profitLoss,
					RiskReward:  (weakClosePrice - pos.EntryPrice) / pos.InitialRisk,
					EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
					ExitDate:    time.Now().Format("2006-01-02"),
					ExitReason:  exitReason,
					IsWinner:    totalProfitLoss > 0,
					AccountSize: accountSize,
				})

				removeFromWatchlist(symbol)
				delete(activePositions, symbol)
			}
		}
	}
}

func checkWeakCloseIntraday(symbol string, date time.Time) (bool, float64) {
	// Get intraday bars (5-minute intervals) for today
	start := date.Format("2006-01-02") + "T09:30:00-05:00"
	end := date.Format("2006-01-02") + "T16:00:00-05:00"

	url := fmt.Sprintf("%s/stocks/%s/bars?timeframe=5Min&start=%s&end=%s&limit=10000",
		ALPACA_BASE_URL, symbol, start, end)

	resp, err := makeAlpacaRequest(url)
	if err != nil {
		log.Printf("[%s] ⚠️  Error fetching intraday data: %v", symbol, err)
		return false, 0
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[%s] ⚠️  Error reading intraday response: %v", symbol, err)
		return false, 0
	}

	var result AlpacaBarsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[%s] ⚠️  Error parsing intraday data: %v", symbol, err)
		return false, 0
	}

	bars, exists := result.Bars[symbol]
	if !exists || len(bars) == 0 {
		return false, 0
	}

	dayHigh := 0.0
	var weakClosePrice float64

	for i, bar := range bars {
		if bar.H > dayHigh {
			dayHigh = bar.H
		}

		if i > 0 && dayHigh > 0 {
			closeFromHigh := (dayHigh - bar.C) / dayHigh
			if closeFromHigh >= WEAK_CLOSE_THRESHOLD {
				weakClosePrice = bar.C
				barTime, _ := time.Parse(time.RFC3339, bar.T)
				log.Printf("[%s] ⚠️  WEAK CLOSE detected at %s | Close: $%.2f (%.1f%% from high $%.2f)",
					symbol, barTime.Format("15:04"), bar.C, closeFromHigh*100, dayHigh)
				return true, weakClosePrice
			}
		}
	}

	return false, 0
}

func loadExistingPositions() {
	filePath := "watchlist.csv"
	file, err := os.Open(filePath)
	if err != nil {
		filePath = "/Users/pranav/projects/avant-ai/AvantAI-Backend/watchlist.csv"
		file, err = os.Open(filePath)
		if err != nil {
			log.Println("No existing watchlist found")
			return
		}
	}
	defer file.Close()
	watchlistPath = filePath

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		log.Printf("Error reading CSV: %v", err)
		return
	}

	if len(records) <= 1 {
		return
	}

	log.Println("\n📥 Loading existing positions from watchlist...")

	for i := 1; i < len(records); i++ {
		pos := parsePosition(records[i])
		if pos != nil && (pos.Status == "HOLDING" || pos.Status == "MONITORING" || strings.Contains(pos.Status, "Strong EP") || strings.Contains(pos.Status, "Lvl")) {
			fetchAllHistoricalData(pos.Symbol, pos.PurchaseDate)

			pos.DaysHeld = int(time.Since(pos.PurchaseDate).Hours() / 24)

			activePositions[pos.Symbol] = pos
			processedSymbols[pos.Symbol] = true

			currentPrice := getCurrentPrice(pos.Symbol)
			gain := currentPrice - pos.EntryPrice

			log.Printf("  [%s] %.2f shares @ $%.2f | Current: $%.2f | Gain: $%.2f (%.1f%%) | Days: %d | Cumulative: $%.2f",
				pos.Symbol, pos.Shares, pos.EntryPrice, currentPrice, gain,
				(gain/pos.EntryPrice)*100, pos.DaysHeld, pos.CumulativeProfit)
		}
	}

	if len(activePositions) > 0 {
		log.Printf("✅ Loaded %d active position(s)\n", len(activePositions))
	} else {
		log.Println("✅ No active positions to load\n")
	}
}

func displayPositionSummary() {
	if len(activePositions) == 0 {
		log.Println("📊 No active positions")
		return
	}

	log.Println("\n" + strings.Repeat("=", 80))
	log.Println("📊 ACTIVE POSITIONS SUMMARY")
	log.Println(strings.Repeat("=", 80))

	totalValue := 0.0
	totalGain := 0.0
	totalCumulativeProfit := 0.0

	for symbol, pos := range activePositions {
		currentPrice := getCurrentPrice(symbol)
		positionValue := currentPrice * pos.Shares
		positionGain := (currentPrice - pos.EntryPrice) * pos.Shares
		rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk

		totalValue += positionValue
		totalGain += positionGain
		totalCumulativeProfit += pos.CumulativeProfit

		log.Printf("[%s] %s | %.0f/%.0f shares @ $%.2f → $%.2f | Value: $%.2f | P/L: $%.2f (%.1f%%) | Cum: $%.2f | R/R: %.2fR | Days: %d",
			symbol, pos.Status, pos.Shares, pos.InitialShares, pos.EntryPrice, currentPrice, positionValue,
			positionGain, (positionGain/(pos.EntryPrice*pos.Shares))*100, pos.CumulativeProfit, rr, pos.DaysHeld)
	}

	log.Println(strings.Repeat("-", 80))
	log.Printf("Total Position Value: $%.2f | Total P/L: $%.2f | Cumulative Profits: $%.2f | Account: $%.2f",
		totalValue, totalGain, totalCumulativeProfit, accountSize)
	log.Println(strings.Repeat("=", 80) + "\n")
}

func checkAndProcessWatchlist() {
	filePath := "watchlist.csv"
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		filePath = "/Users/pranav/projects/avant-ai/AvantAI-Backend/watchlist.csv"
		fileInfo, err = os.Stat(filePath)
		if err != nil {
			return
		}
	}

	watchlistPath = filePath

	if fileInfo.ModTime().After(lastModTime) {
		lastModTime = fileInfo.ModTime()
		log.Println("📋 Watchlist updated, processing new entries...")
		processWatchlist(filePath)
	}
}

func processWatchlist(filePath string) {
	file, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		log.Printf("Error reading CSV: %v", err)
		return
	}

	if len(records) <= 1 {
		return
	}

	var positions []*Position
	for i := 1; i < len(records); i++ {
		pos := parsePosition(records[i])
		if pos != nil && !processedSymbols[pos.Symbol] && activePositions[pos.Symbol] == nil {
			positions = append(positions, pos)
		}
	}

	sort.Slice(positions, func(i, j int) bool {
		return positions[i].PurchaseDate.Before(positions[j].PurchaseDate)
	})

	scaledPositions := scalePositionsToFit(positions)

	for _, pos := range scaledPositions {
		if shouldStartPosition(pos) {
			startPosition(pos)
		}
	}
}

func shouldStartPosition(newPos *Position) bool {
	today := time.Now().Format("2006-01-02")
	posDate := newPos.PurchaseDate.Format("2006-01-02")

	return today == posDate
}

func scalePositionsToFit(positions []*Position) []*Position {
	recalculatedPositions := make([]*Position, len(positions))
	totalAccountValue := getTotalAccountValue()

	for i, pos := range positions {
		recalculatedPos := *pos
		recalculatedPositions[i] = recalculateShares(&recalculatedPos, totalAccountValue)
	}

	return recalculatedPositions
}

func recalculateShares(pos *Position, currentAccountSize float64) *Position {
	riskPercent := 1.0
	dollarRisk := riskPerTrade * currentAccountSize
	riskPerShare := pos.EntryPrice - pos.StopLoss

	if riskPerShare <= 0 {
		log.Printf("[%s] ⚠️  Invalid risk calculation, cannot recalculate shares", pos.Symbol)
		return pos
	}

	newShares := math.Round(((riskPercent * dollarRisk * 1.0) / riskPerShare))

	if newShares != pos.Shares {
		log.Printf("[%s] 📊 Recalculated shares based on current account size: %.0f -> %.0f shares (Account: $%.2f, DollarRisk: $%.2f, RiskPerShare: $%.2f)",
			pos.Symbol, pos.Shares, newShares, currentAccountSize, dollarRisk, riskPerShare)
		pos.Shares = newShares
	}

	return pos
}

func removeFromWatchlist(symbol string) {
	if watchlistPath == "" {
		log.Printf("[%s] ⚠️  Cannot remove from watchlist: path not set", symbol)
		return
	}

	file, err := os.Open(watchlistPath)
	if err != nil {
		log.Printf("[%s] ⚠️  Cannot open watchlist for removal: %v", symbol, err)
		return
	}

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	file.Close()

	if err != nil {
		log.Printf("[%s] ⚠️  Cannot read watchlist: %v", symbol, err)
		return
	}

	var newRecords [][]string
	removed := false
	for i, record := range records {
		if i == 0 {
			newRecords = append(newRecords, record)
		} else if len(record) > 0 && strings.TrimSpace(record[0]) != symbol {
			newRecords = append(newRecords, record)
		} else {
			removed = true
		}
	}

	if !removed {
		return
	}

	file, err = os.Create(watchlistPath)
	if err != nil {
		log.Printf("[%s] ⚠️  Cannot create watchlist file: %v", symbol, err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	err = writer.WriteAll(newRecords)
	if err != nil {
		log.Printf("[%s] ⚠️  Cannot write watchlist: %v", symbol, err)
		return
	}

	log.Printf("[%s] 🗑️  Removed from watchlist", symbol)
}

func fetchAllHistoricalData(symbol string, startDate time.Time) bool {
	if _, exists := historicalCache[symbol]; exists {
		return true
	}

	fromDate := startDate.AddDate(0, 0, -60) // Get 60 days for MA calculations
	toDate := time.Now()

	log.Printf("[%s] 📥 Fetching historical data from %s to %s...",
		symbol, fromDate.Format("2006-01-02"), toDate.Format("2006-01-02"))

	// Alpaca format: RFC3339
	start := fromDate.Format("2006-01-02") + "T00:00:00-05:00"
	end := toDate.Format("2006-01-02") + "T23:59:59-05:00"

	url := fmt.Sprintf("%s/stocks/%s/bars?timeframe=1Day&start=%s&end=%s&limit=10000",
		ALPACA_BASE_URL, symbol, start, end)

	resp, err := makeAlpacaRequest(url)
	if err != nil {
		log.Printf("[%s] ❌ Error fetching historical data: %v", symbol, err)
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[%s] ❌ Error reading response: %v", symbol, err)
		return false
	}

	var result AlpacaBarsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[%s] ❌ Error parsing historical data: %v", symbol, err)
		log.Printf("Response body: %s", string(body))
		return false
	}

	bars, exists := result.Bars[symbol]
	if !exists || len(bars) == 0 {
		log.Printf("[%s] ❌ No historical data returned", symbol)
		return false
	}

	// Sort by timestamp
	sort.Slice(bars, func(i, j int) bool {
		return bars[i].T < bars[j].T
	})

	historicalCache[symbol] = bars
	log.Printf("[%s] ✅ Cached %d days of historical data", symbol, len(bars))
	return true
}

func calculateAverageVolume(symbol string, endDate time.Time, period int) float64 {
	historicalData, exists := historicalCache[symbol]
	if !exists || len(historicalData) < period {
		return 0
	}

	var endIdx int
	targetDate := endDate.Format("2006-01-02")

	for i := range historicalData {
		barTime, _ := time.Parse(time.RFC3339, historicalData[i].T)
		if barTime.Format("2006-01-02") <= targetDate {
			endIdx = i
		}
	}

	if endIdx < period {
		return 0
	}

	sum := 0.0
	for i := endIdx - period; i < endIdx; i++ {
		if i >= 0 {
			sum += historicalData[i].V
		}
	}

	return sum / float64(period)
}

func startPosition(pos *Position) {
	log.Printf("[%s] 📥 Fetching historical data...", pos.Symbol)

	if !fetchAllHistoricalData(pos.Symbol, pos.PurchaseDate) {
		log.Printf("[%s] ⚠️  Cannot start position - failed to fetch historical data", pos.Symbol)
		processedSymbols[pos.Symbol] = true
		return
	}

	historicalData := historicalCache[pos.Symbol]

	var epDayData *AlpacaBar
	targetDate := pos.PurchaseDate.Format("2006-01-02")

	for i := range historicalData {
		barTime, _ := time.Parse(time.RFC3339, historicalData[i].T)
		if barTime.Format("2006-01-02") == targetDate {
			epDayData = &historicalData[i]
			break
		}
	}

	if epDayData == nil {
		log.Printf("[%s] ⚠️  Cannot find EP day data for %s", pos.Symbol, targetDate)
		processedSymbols[pos.Symbol] = true
		return
	}

	// NEW: Price filter
	if epDayData.C < MIN_PRICE || epDayData.C > MAX_PRICE {
		log.Printf("[%s] ⚠️  Price $%.2f outside acceptable range ($%.2f-$%.2f), skipping",
			pos.Symbol, epDayData.C, MIN_PRICE, MAX_PRICE)
		processedSymbols[pos.Symbol] = true
		return
	}

	// NEW: Volume filter
	avgVolume := calculateAverageVolume(pos.Symbol, pos.PurchaseDate, 20)
	if avgVolume > 0 {
		volumeRatio := epDayData.V / avgVolume
		if volumeRatio < MIN_VOLUME_RATIO {
			log.Printf("[%s] ⚠️  Insufficient volume: %.2fx average (need %.2fx), skipping",
				pos.Symbol, volumeRatio, MIN_VOLUME_RATIO)
			processedSymbols[pos.Symbol] = true
			return
		}
		log.Printf("[%s] ✅ Volume: %.2fx average", pos.Symbol, volumeRatio)
		pos.AverageVolume = avgVolume
	}

	// NEW: Trend filter
	ma20 := calculate20DayMA(pos.Symbol, pos.PurchaseDate)
	if ma20 > 0 && epDayData.C < ma20 {
		log.Printf("[%s] ⚠️  Price $%.2f below 20-day MA $%.2f, skipping weak setup",
			pos.Symbol, epDayData.C, ma20)
		processedSymbols[pos.Symbol] = true
		return
	}

	pos.EPDayLow = epDayData.L
	pos.EPDayHigh = epDayData.H

	atr := calculateATR(pos.Symbol, pos.PurchaseDate, ATR_PERIOD)

	var finalStop float64
	if pos.StopLoss > 0 && pos.StopLoss < pos.EntryPrice {
		finalStop = pos.StopLoss
		log.Printf("[%s] ℹ️  Using watchlist stop loss: $%.2f", pos.Symbol, finalStop)
	} else {
		// IMPROVED: Wider stop (0.98 instead of 0.99)
		suggestedStop := pos.EPDayLow * 0.98

		if atr > 0 {
			// IMPROVED: 2.0x ATR instead of 1.5x
			maxStopDistance := atr * MAX_STOP_DISTANCE_MULTIPLIER
			actualStopDistance := pos.EntryPrice - suggestedStop

			if actualStopDistance > maxStopDistance {
				suggestedStop = pos.EntryPrice - maxStopDistance
				log.Printf("[%s] ⚠️  Stop adjusted to respect ATR limit. ATR: $%.2f, Max Distance: $%.2f",
					pos.Symbol, atr, maxStopDistance)
			}
		}
		finalStop = suggestedStop
		log.Printf("[%s] ℹ️  Calculated stop loss: $%.2f (EP Low: $%.2f)", pos.Symbol, finalStop, pos.EPDayLow)
	}

	pos.StopLoss = finalStop
	pos.InitialStopLoss = finalStop
	pos.InitialRisk = pos.EntryPrice - pos.StopLoss
	pos.HighestPrice = pos.EntryPrice

	riskPerShare := pos.InitialRisk
	if riskPerShare <= 0 {
		log.Printf("[%s] ⚠️  Invalid risk calculation. Entry: $%.2f, Stop: $%.2f",
			pos.Symbol, pos.EntryPrice, pos.StopLoss)
		processedSymbols[pos.Symbol] = true
		return
	}

	positionCost := pos.EntryPrice * pos.Shares

	// IMPROVED: Buy what we can afford if insufficient funds
	if positionCost > accountSize {
		availableFunds := accountSize * 0.5
		affordableShares := math.Floor(availableFunds / pos.EntryPrice)

		if affordableShares < 1 {
			log.Printf("[%s] ⚠️  Insufficient funds even for 1 share. Need $%.2f, have $%.2f. Skipping.",
				pos.Symbol, pos.EntryPrice, accountSize)
			processedSymbols[pos.Symbol] = true
			return
		}

		log.Printf("[%s] ⚠️  Insufficient funds for full position. Need $%.2f, have $%.2f",
			pos.Symbol, positionCost, accountSize)
		log.Printf("[%s] 💡 Adjusting: Using 50%% of cash ($%.2f) to buy %.0f shares instead of %.0f shares",
			pos.Symbol, availableFunds, affordableShares, pos.Shares)

		pos.Shares = affordableShares
		positionCost = pos.EntryPrice * pos.Shares
	}

	updateAccountSize(-positionCost)
	pos.LastCheckDate = pos.PurchaseDate
	pos.Status = "HOLDING"
	pos.InitialShares = pos.Shares
	pos.CumulativeProfit = 0.0
	activePositions[pos.Symbol] = pos
	processedSymbols[pos.Symbol] = true

	updatePositionInCSV(pos)

	totalAccountValue := getTotalAccountValue()
	dollarRisk := totalAccountValue * riskPerTrade
	log.Printf("[%s] 🟢 OPENED POSITION | Shares: %.0f @ $%.2f | Total: $%.2f | Stop: $%.2f | Risk: $%.2f (%.2f%%) | ATR: $%.2f | Date: %s",
		pos.Symbol, pos.Shares, pos.EntryPrice, positionCost, pos.StopLoss, dollarRisk, (pos.InitialRisk/pos.EntryPrice)*100,
		atr, pos.PurchaseDate.Format("2006-01-02"))
	log.Printf("[%s] ℹ️  Cash: $%.2f | Total Account Value: $%.2f",
		pos.Symbol, accountSize, totalAccountValue)
}

func calculateATR(symbol string, endDate time.Time, period int) float64 {
	historicalData, exists := historicalCache[symbol]
	if !exists || len(historicalData) < period {
		return 0
	}

	var trueRanges []float64
	var endIdx int

	targetDate := endDate.Format("2006-01-02")
	for i := range historicalData {
		barTime, _ := time.Parse(time.RFC3339, historicalData[i].T)
		if barTime.Format("2006-01-02") <= targetDate {
			endIdx = i
		}
	}

	for i := endIdx - period + 1; i <= endIdx && i < len(historicalData); i++ {
		if i <= 0 {
			continue
		}
		current := historicalData[i]
		previous := historicalData[i-1]

		highLow := current.H - current.L
		highClose := math.Abs(current.H - previous.C)
		lowClose := math.Abs(current.L - previous.C)

		tr := math.Max(highLow, math.Max(highClose, lowClose))
		trueRanges = append(trueRanges, tr)
	}

	if len(trueRanges) == 0 {
		return 0
	}

	sum := 0.0
	for _, tr := range trueRanges {
		sum += tr
	}

	return sum / float64(len(trueRanges))
}

func calculate20DayMA(symbol string, currentDate time.Time) float64 {
	historicalData, exists := historicalCache[symbol]
	if !exists {
		return 0
	}

	var prices []float64
	targetDate := currentDate.Format("2006-01-02")

	for i := range historicalData {
		barTime, _ := time.Parse(time.RFC3339, historicalData[i].T)
		if barTime.Format("2006-01-02") <= targetDate {
			prices = append(prices, historicalData[i].C)
		}
	}

	if len(prices) < MA_PERIOD {
		return 0
	}

	recentPrices := prices[len(prices)-MA_PERIOD:]
	sum := 0.0
	for _, price := range recentPrices {
		sum += price
	}

	return sum / float64(MA_PERIOD)
}

func executeSellOrder(symbol string, shares float64, sellPrice float64) error {
	if !autoExecuteTrades {
		log.Printf("[%s] 📋 MANUAL ACTION REQUIRED: Sell %.0f shares @ $%.2f", symbol, shares, sellPrice)
		return nil
	}

	order, err := ep.PlaceSellOrder(symbol, int(shares), &sellPrice)
	if err != nil {
		log.Printf("[%s] ❌ Error placing sell order: %v", symbol, err)
		return err
	}

	log.Printf("[%s] 🛒 Sell order placed: ID %s | %.0f shares @ $%.2f", symbol, order.ID, shares, sellPrice)

	return nil
}

func getCurrentPrice(symbol string) float64 {
	historicalData, exists := historicalCache[symbol]
	if !exists || len(historicalData) == 0 {
		// Try to fetch latest quote
		url := fmt.Sprintf("%s/stocks/%s/quotes/latest", ALPACA_BASE_URL, symbol)
		resp, err := makeAlpacaRequest(url)
		if err != nil {
			return 0
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var quoteResp struct {
			Quote struct {
				Ap float64 `json:"ap"` // Ask price
				Bp float64 `json:"bp"` // Bid price
			} `json:"quote"`
		}

		if json.Unmarshal(body, &quoteResp) == nil {
			// Use mid-point of bid-ask
			if quoteResp.Quote.Ap > 0 && quoteResp.Quote.Bp > 0 {
				return (quoteResp.Quote.Ap + quoteResp.Quote.Bp) / 2
			}
		}

		return 0
	}

	return historicalData[len(historicalData)-1].C
}

func monitorActivePositions() {
	if len(activePositions) == 0 {
		return
	}

	now := time.Now()
	hour, min, _ := now.Clock()
	currentTime := fmt.Sprintf("%02d:%02d", hour, min)

	log.Printf("📊 [%s] Monitoring %d active position(s)...", currentTime, len(activePositions))

	for symbol, pos := range activePositions {
		fetchAllHistoricalData(pos.Symbol, pos.PurchaseDate)

		currentPrice := getCurrentPrice(pos.Symbol)
		if currentPrice == 0 {
			continue
		}

		currentGain := currentPrice - pos.EntryPrice
		currentRR := currentGain / pos.InitialRisk
		percentGain := (currentPrice - pos.EntryPrice) / pos.EntryPrice

		if currentPrice > pos.HighestPrice {
			pos.HighestPrice = currentPrice
			log.Printf("[%s] 📈 NEW HIGH: $%.2f (was $%.2f)", symbol, currentPrice, pos.HighestPrice)
		}

		log.Printf("[%s] Status: %s | Current: $%.2f | Gain: $%.2f (%.1f%%) | R/R: %.2fR | Cum: $%.2f | Stop: $%.2f | Shares: %.0f/%.0f",
			symbol, pos.Status, currentPrice, currentGain,
			(currentGain/pos.EntryPrice)*100, currentRR, pos.CumulativeProfit, pos.StopLoss, pos.Shares, pos.InitialShares)

		// Stop loss alerts
		if pos.DaysHeld <= 1 {
			if currentPrice <= pos.StopLoss*1.02 {
				if !stopAlerts[symbol] {
					log.Printf("[%s] 🚨🚨 CRITICAL ALERT: Price at/near stop ($%.2f vs stop $%.2f)!",
						symbol, currentPrice, pos.StopLoss)
					log.Printf("[%s] 🚨🚨 ACTION: Consider selling NOW to avoid further losses!", symbol)
					stopAlerts[symbol] = true
				}
			} else {
				delete(stopAlerts, symbol)
			}
		} else {
			if currentPrice <= pos.StopLoss {
				if !stopAlerts[symbol] {
					log.Printf("[%s] 🚨🚨 ALERT: Price at/below stop ($%.2f vs stop $%.2f)!",
						symbol, currentPrice, pos.StopLoss)
					stopAlerts[symbol] = true
				}
			} else {
				delete(stopAlerts, symbol)
			}
		}

		// IMPROVED: Earlier breakeven (Day 2 at 2%)
		if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS && !pos.ProfitTaken {
			percentGainIntraday := (currentPrice - pos.EntryPrice) / pos.EntryPrice
			if percentGainIntraday >= BREAKEVEN_TRIGGER_PERCENT && pos.StopLoss < pos.EntryPrice {
				pos.StopLoss = pos.EntryPrice
				log.Printf("[%s] 🔒 MOVED TO BREAKEVEN - Up %.1f%% (Day %d), protecting capital. Stop: $%.2f",
					pos.Symbol, percentGainIntraday*100, pos.DaysHeld, pos.StopLoss)
				updatePositionInCSV(pos)
			}
		}

		// Profit alerts - Strong EP
		if !pos.ProfitTaken && pos.DaysHeld <= STRONG_EP_DAYS && percentGain >= STRONG_EP_GAIN {
			if !profitAlerts[symbol] {
				log.Printf("[%s] 🎉🎉 STRONG EP ALERT: %.1f%% gain in %d days!",
					symbol, percentGain*100, pos.DaysHeld)
				log.Printf("[%s] 🎉🎉 RECOMMENDATION: Take %.0f%% profit at market close",
					symbol, STRONG_EP_TAKE_PERCENT*100)
				profitAlerts[symbol] = true
			}
		}

		// IMPROVED: Graduated profit alerts
		if currentRR >= PROFIT_TAKE_1_RR && !pos.ProfitTaken {
			if !profitAlerts[symbol+"_L1"] {
				log.Printf("[%s] 🎯 PROFIT LEVEL 1 REACHED: %.2fR!", symbol, currentRR)
				log.Printf("[%s] 🎯 RECOMMENDATION: Take %.0f%% profit at market close",
					symbol, PROFIT_TAKE_1_PERCENT*100)
				profitAlerts[symbol+"_L1"] = true
			}
		}

		if currentRR >= PROFIT_TAKE_2_RR && pos.ProfitTaken && !pos.ProfitTaken2 {
			if !profitAlerts[symbol+"_L2"] {
				log.Printf("[%s] 🎯🎯 PROFIT LEVEL 2 REACHED: %.2fR!", symbol, currentRR)
				log.Printf("[%s] 🎯🎯 RECOMMENDATION: Take %.0f%% profit, lock stop at +1R",
					symbol, PROFIT_TAKE_2_PERCENT*100)
				profitAlerts[symbol+"_L2"] = true
			}
		}

		if currentRR >= PROFIT_TAKE_3_RR && pos.ProfitTaken2 && !pos.ProfitTaken3 {
			if !profitAlerts[symbol+"_L3"] {
				log.Printf("[%s] 🎯🎯🎯 PROFIT LEVEL 3 REACHED: %.2fR!", symbol, currentRR)
				log.Printf("[%s] 🎯🎯🎯 RECOMMENDATION: Take %.0f%% profit, lock stop at +2R",
					symbol, PROFIT_TAKE_3_PERCENT*100)
				profitAlerts[symbol+"_L3"] = true
			}
		}

		// IMPROVED: Dynamic trailing stop monitoring
		if pos.TrailingStopMode {
			ma20 := calculate20DayMA(symbol, time.Now())
			if ma20 > 0 {
				percentGainFromEntry := (currentPrice - pos.EntryPrice) / pos.EntryPrice
				var targetDistance float64

				if percentGainFromEntry > 0.20 {
					targetDistance = 0.06 // 6% wide trailing
				} else if percentGainFromEntry > 0.10 {
					targetDistance = 0.05 // 5% medium trailing
				} else if percentGainFromEntry > 0.05 {
					targetDistance = 0.04 // 4% tight trailing
				}

				if targetDistance > 0 {
					distanceFromMA := ((currentPrice - ma20) / ma20) * 100
					if distanceFromMA < targetDistance*100 && distanceFromMA > -(targetDistance*100/2) {
						log.Printf("[%s] ⚠️  Near 20-day MA: $%.2f (MA: $%.2f, %.1f%% away, trailing at %.0f%%)",
							symbol, currentPrice, ma20, distanceFromMA, targetDistance*100)
					}
				}
			}
		}
	}
}

func performPreCloseCheck() {
	if len(activePositions) == 0 {
		log.Println("✅ No active positions to check")
		return
	}

	log.Println(strings.Repeat("=", 80))
	log.Println("🔔 PRE-CLOSE POSITION EVALUATION (3:55 PM - 5 MINUTES TO CLOSE)")
	log.Println(strings.Repeat("=", 80))

	sellRecommendations := []string{}
	holdPositions := []string{}

	for symbol, pos := range activePositions {
		fetchAllHistoricalData(symbol, pos.PurchaseDate)
		currentPrice := getCurrentPrice(symbol)

		if currentPrice == 0 {
			log.Printf("[%s] ⚠️  Unable to get current price", symbol)
			continue
		}

		currentGain := currentPrice - pos.EntryPrice
		currentRR := currentGain / pos.InitialRisk
		percentGain := (currentPrice - pos.EntryPrice) / pos.EntryPrice
		positionValue := currentPrice * pos.Shares

		log.Printf("\n[%s] Pre-Close Analysis:", symbol)
		log.Printf("  Current Price: $%.2f (Position Value: $%.2f)", currentPrice, positionValue)
		log.Printf("  Entry Price: $%.2f | Stop Loss: $%.2f", pos.EntryPrice, pos.StopLoss)
		log.Printf("  Gain: $%.2f (%.1f%%) | R/R: %.2fR | Cumulative: $%.2f", currentGain, percentGain*100, currentRR, pos.CumulativeProfit)
		log.Printf("  Days Held: %d | Status: %s | Shares: %.0f/%.0f", pos.DaysHeld, pos.Status, pos.Shares, pos.InitialShares)

		shouldSell := false
		sellReason := ""

		// Check stop loss
		if pos.DaysHeld <= 1 {
			if currentPrice <= pos.StopLoss {
				log.Printf("  🛑 ALERT: SELL NOW - Price at/below stop!")
				sellReason = fmt.Sprintf("AT STOP LOSS ($%.2f <= $%.2f) - LOSS: $%.2f",
					currentPrice, pos.StopLoss, currentGain*pos.Shares)
				shouldSell = true
			}
		} else {
			if currentPrice <= pos.StopLoss {
				log.Printf("  🛑 ALERT: SELL AT CLOSE - Price at/below stop!")
				sellReason = fmt.Sprintf("AT STOP LOSS ($%.2f <= $%.2f) - P/L: $%.2f",
					currentPrice, pos.StopLoss, currentGain*pos.Shares)
				shouldSell = true
			}
		}

		// Strong EP check
		if !shouldSell && pos.DaysHeld <= STRONG_EP_DAYS && percentGain >= STRONG_EP_GAIN && !pos.ProfitTaken {
			log.Printf("  🚀 ALERT: Take %.0f%% profit - Strong EP detected!", STRONG_EP_TAKE_PERCENT*100)
			sharesToSell := pos.Shares * STRONG_EP_TAKE_PERCENT
			profitAmount := (currentPrice - pos.EntryPrice) * sharesToSell
			sellReason = fmt.Sprintf("STRONG EP - Sell %.0f shares (%.0f%% of position) for $%.2f profit (%.1f%% gain in %d days)",
				sharesToSell, STRONG_EP_TAKE_PERCENT*100, profitAmount, percentGain*100, pos.DaysHeld)
			shouldSell = true
		}

		// IMPROVED: Graduated profit taking
		if !shouldSell && currentRR >= PROFIT_TAKE_1_RR && !pos.ProfitTaken {
			log.Printf("  🎯 ALERT: Take %.0f%% profit - Hit %.2fR target (Level 1)!", PROFIT_TAKE_1_PERCENT*100, currentRR)
			sharesToSell := pos.Shares * PROFIT_TAKE_1_PERCENT
			profitAmount := (currentPrice - pos.EntryPrice) * sharesToSell
			sellReason = fmt.Sprintf("PROFIT LEVEL 1 - Sell %.0f shares (%.0f%% of position) for $%.2f profit (%.2fR achieved)",
				sharesToSell, PROFIT_TAKE_1_PERCENT*100, profitAmount, currentRR)
			shouldSell = true
		}

		if !shouldSell && currentRR >= PROFIT_TAKE_2_RR && pos.ProfitTaken && !pos.ProfitTaken2 {
			log.Printf("  🎯🎯 ALERT: Take %.0f%% profit - Hit %.2fR target (Level 2)!", PROFIT_TAKE_2_PERCENT*100, currentRR)
			sharesToSell := pos.Shares * PROFIT_TAKE_2_PERCENT
			profitAmount := (currentPrice - pos.EntryPrice) * sharesToSell
			sellReason = fmt.Sprintf("PROFIT LEVEL 2 - Sell %.0f shares (%.0f%% of position) for $%.2f profit (%.2fR achieved)",
				sharesToSell, PROFIT_TAKE_2_PERCENT*100, profitAmount, currentRR)
			shouldSell = true
		}

		if !shouldSell && currentRR >= PROFIT_TAKE_3_RR && pos.ProfitTaken2 && !pos.ProfitTaken3 {
			log.Printf("  🎯🎯🎯 ALERT: Take %.0f%% profit - Hit %.2fR target (Level 3)!", PROFIT_TAKE_3_PERCENT*100, currentRR)
			sharesToSell := pos.Shares * PROFIT_TAKE_3_PERCENT
			profitAmount := (currentPrice - pos.EntryPrice) * sharesToSell
			sellReason = fmt.Sprintf("PROFIT LEVEL 3 - Sell %.0f shares (%.0f%% of position) for $%.2f profit (%.2fR achieved)",
				sharesToSell, PROFIT_TAKE_3_PERCENT*100, profitAmount, currentRR)
			shouldSell = true
		}

		// IMPROVED: Dynamic trailing stop check
		if !shouldSell && pos.TrailingStopMode {
			ma20 := calculate20DayMA(symbol, time.Now())
			percentGainFromEntry := (currentPrice - pos.EntryPrice) / pos.EntryPrice

			if ma20 > 0 {
				var stopThreshold float64
				if percentGainFromEntry > 0.20 {
					stopThreshold = ma20 * 0.94 // 6% below
				} else if percentGainFromEntry > 0.10 {
					stopThreshold = ma20 * 0.95 // 5% below
				} else if percentGainFromEntry > 0.05 {
					stopThreshold = ma20 * 0.96 // 4% below
				}

				if stopThreshold > 0 && currentPrice < stopThreshold {
					log.Printf("  📉 ALERT: SELL - Price below dynamic trailing stop")
					profitAmount := (currentPrice - pos.EntryPrice) * pos.Shares
					sellReason = fmt.Sprintf("TRAILING STOP - Below %.0f%% MA threshold ($%.2f < $%.2f) - P/L: $%.2f",
						(1-(stopThreshold/ma20))*100, currentPrice, stopThreshold, profitAmount)
					shouldSell = true
				}
			}
		}

		// IMPROVED: Weak momentum check (8 days instead of 5)
		if !shouldSell && pos.DaysHeld >= MAX_DAYS_NO_FOLLOWTHROUGH && currentRR < 0.5 && !pos.ProfitTaken {
			log.Printf("  ⚠️  WARNING: No follow-through after %d days (only %.2fR)",
				pos.DaysHeld, currentRR)
			log.Printf("  Consider tightening stop or exiting if no momentum")
			holdPositions = append(holdPositions,
				fmt.Sprintf("%s: WEAK MOMENTUM - %d days, only %.2fR - Consider exit",
					symbol, pos.DaysHeld, currentRR))
		}

		if shouldSell {
			sellRecommendations = append(sellRecommendations, fmt.Sprintf("%s: %s", symbol, sellReason))
			log.Printf("  ❗ ACTION REQUIRED: %s", sellReason)
		} else if len(holdPositions) == 0 || holdPositions[len(holdPositions)-1][:len(symbol)] != symbol {
			log.Printf("  ✅ HOLD - No exit signals at this time")
			holdPositions = append(holdPositions,
				fmt.Sprintf("%s: Hold - P/L: $%.2f (%.1f%%), R/R: %.2fR, Cum: $%.2f",
					symbol, currentGain*pos.Shares, percentGain*100, currentRR, pos.CumulativeProfit))
		}
	}

	log.Println("\n" + strings.Repeat("=", 80))
	if len(sellRecommendations) > 0 {
		log.Println("🔔 IMMEDIATE SELL RECOMMENDATIONS (Execute before 4:00 PM ET):")
		log.Println(strings.Repeat("-", 80))
		for i, rec := range sellRecommendations {
			log.Printf("  %d. %s", i+1, rec)
		}
		log.Println(strings.Repeat("-", 80))
		log.Println("⏰ YOU HAVE 5 MINUTES TO EXECUTE THESE SALES")
	} else {
		log.Println("✅ No immediate sell signals")
	}

	if len(holdPositions) > 0 {
		log.Println("\n📊 POSITIONS TO HOLD:")
		log.Println(strings.Repeat("-", 80))
		for i, pos := range holdPositions {
			log.Printf("  %d. %s", i+1, pos)
		}
	}
	log.Println(strings.Repeat("=", 80) + "\n")
}

func processEndOfDayPositions() {
	if len(activePositions) == 0 {
		return
	}

	log.Println("🌙 Processing end-of-day positions...")

	for symbol := range activePositions {
		fetchAllHistoricalData(symbol, activePositions[symbol].PurchaseDate)
	}

	time.Sleep(30 * time.Second)

	positionsToRemove := []string{}
	positionsUpdated := 0

	for symbol, pos := range activePositions {
		shouldRemove := processEODForPosition(pos)
		if shouldRemove {
			positionsToRemove = append(positionsToRemove, symbol)
		} else {
			positionsUpdated++
		}
	}

	for _, symbol := range positionsToRemove {
		delete(activePositions, symbol)
	}

	log.Printf("✅ EOD processing complete - %d positions updated, %d closed",
		positionsUpdated, len(positionsToRemove))

	if len(activePositions) > 0 {
		displayPositionSummary()
	}
}

func processEODForPosition(pos *Position) bool {
	historicalData, exists := historicalCache[pos.Symbol]
	if !exists {
		return false
	}

	today := time.Now().Format("2006-01-02")
	var todayData *AlpacaBar

	for i := len(historicalData) - 1; i >= 0; i-- {
		barTime, _ := time.Parse(time.RFC3339, historicalData[i].T)
		if barTime.Format("2006-01-02") == today {
			todayData = &historicalData[i]
			break
		}
	}

	if todayData == nil {
		log.Printf("[%s] ⚠️  No EOD data available for today yet", pos.Symbol)
		return false
	}

	pos.LastCheckDate = time.Now()
	pos.DaysHeld++

	currentPrice := todayData.C
	dayLow := todayData.L
	dayHigh := todayData.H
	currentGain := currentPrice - pos.EntryPrice
	currentRR := currentGain / pos.InitialRisk

	if dayHigh > pos.HighestPrice {
		pos.HighestPrice = dayHigh
	}

	log.Printf("[%s] EOD Day %d | Close: $%.2f | Low: $%.2f | High: $%.2f | Gain: $%.2f (%.1f%%) | R/R: %.2fR | Cum: $%.2f | Stop: $%.2f",
		pos.Symbol, pos.DaysHeld, currentPrice, dayLow, dayHigh, currentGain,
		(currentGain/pos.EntryPrice)*100, currentRR, pos.CumulativeProfit, pos.StopLoss)

	// Check stop loss
	if pos.DaysHeld == 1 {
		if dayLow <= pos.StopLoss {
			stopOut(pos, pos.StopLoss, time.Now())
			return true
		}
	} else {
		if currentPrice <= pos.StopLoss {
			stopOut(pos, currentPrice, time.Now())
			return true
		}
	}

	// IMPROVED: Earlier breakeven (Day 2 at 2%)
	if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS && !pos.ProfitTaken {
		percentGain := (dayHigh - pos.EntryPrice) / pos.EntryPrice
		if percentGain >= BREAKEVEN_TRIGGER_PERCENT && pos.StopLoss < pos.EntryPrice {
			pos.StopLoss = pos.EntryPrice
			log.Printf("[%s] 🔒 MOVED TO BREAKEVEN - Up %.1f%% (Day %d), protecting capital. Stop: $%.2f",
				pos.Symbol, percentGain*100, pos.DaysHeld, pos.StopLoss)
			updatePositionInCSV(pos)
		}
	}

	// IMPROVED: Tighten stop if no follow-through (8 days instead of 5)
	if pos.DaysHeld >= MAX_DAYS_NO_FOLLOWTHROUGH && currentRR < 0.5 && !pos.ProfitTaken {
		tightenedStop := pos.EntryPrice - (pos.InitialRisk * 0.3)
		tightenedStop = math.Max(tightenedStop, pos.EntryPrice)
		if tightenedStop > pos.StopLoss {
			pos.StopLoss = tightenedStop
			log.Printf("[%s] ⚠️  Weak follow-through after %d days. Tightening stop to $%.2f",
				pos.Symbol, MAX_DAYS_NO_FOLLOWTHROUGH, pos.StopLoss)
			updatePositionInCSV(pos)
		}
	}

	// Check for strong EP
	percentGain := (currentPrice - pos.EntryPrice) / pos.EntryPrice
	if pos.DaysHeld <= STRONG_EP_DAYS && percentGain >= STRONG_EP_GAIN && !pos.ProfitTaken {
		takeStrongEPProfit(pos, currentPrice, time.Now())
		updatePositionInCSV(pos)
		return false
	}

	// IMPROVED: Graduated profit taking
	if currentRR >= PROFIT_TAKE_1_RR && !pos.ProfitTaken {
		takeProfitPartial(pos, currentPrice, time.Now(), PROFIT_TAKE_1_PERCENT, 1)
		pos.TrailingStopMode = true
		pos.ProfitTaken = true
		if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS {
			pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice)
			log.Printf("[%s] 🔒 Stop at breakeven after Level 1 profit", pos.Symbol)
		}
		updatePositionInCSV(pos)
		return false
	}

	if currentRR >= PROFIT_TAKE_2_RR && pos.ProfitTaken && !pos.ProfitTaken2 {
		takeProfitPartial(pos, currentPrice, time.Now(), PROFIT_TAKE_2_PERCENT, 2)
		newStop := pos.EntryPrice + (pos.InitialRisk * 1.0)
		pos.StopLoss = math.Max(pos.StopLoss, newStop)
		pos.ProfitTaken2 = true
		log.Printf("[%s] 🔒 Stop locked at +1R after Level 2 profit", pos.Symbol)
		updatePositionInCSV(pos)
		return false
	}

	if currentRR >= PROFIT_TAKE_3_RR && pos.ProfitTaken2 && !pos.ProfitTaken3 {
		takeProfitPartial(pos, currentPrice, time.Now(), PROFIT_TAKE_3_PERCENT, 3)
		newStop := pos.EntryPrice + (pos.InitialRisk * 2.0)
		pos.StopLoss = math.Max(pos.StopLoss, newStop)
		pos.ProfitTaken3 = true
		log.Printf("[%s] 🔒 Stop locked at +2R after Level 3 profit", pos.Symbol)
		updatePositionInCSV(pos)
		return false
	}

	// IMPROVED: Dynamic trailing stop
	if pos.TrailingStopMode {
		ma20 := calculate20DayMA(pos.Symbol, time.Now())
		percentGainFromEntry := (currentPrice - pos.EntryPrice) / pos.EntryPrice

		if ma20 > 0 {
			var newStop float64

			if percentGainFromEntry > 0.20 {
				// Wide trailing: 6% below MA or price
				newStop = math.Max(ma20*0.94, currentPrice*0.94)
				newStop = math.Max(newStop, pos.EntryPrice)
				if newStop > pos.StopLoss {
					pos.StopLoss = newStop
					log.Printf("[%s] 📈 Wide trailing (%.1f%% gain): $%.2f (6%% below)",
						pos.Symbol, percentGainFromEntry*100, pos.StopLoss)
					updatePositionInCSV(pos)
				}
			} else if percentGainFromEntry > 0.10 {
				// Medium trailing: 5% below MA or price
				newStop = math.Max(ma20*0.95, currentPrice*0.95)
				newStop = math.Max(newStop, pos.EntryPrice)
				if newStop > pos.StopLoss {
					pos.StopLoss = newStop
					log.Printf("[%s] 📈 Medium trailing (%.1f%% gain): $%.2f (5%% below)",
						pos.Symbol, percentGainFromEntry*100, pos.StopLoss)
					updatePositionInCSV(pos)
				}
			} else if percentGainFromEntry > 0.05 {
				// Tight trailing: 4% below MA
				newStop = ma20 * 0.96
				newStop = math.Max(newStop, pos.EntryPrice)
				if newStop > pos.StopLoss {
					pos.StopLoss = newStop
					log.Printf("[%s] 📈 Tight trailing (%.1f%% gain): $%.2f (4%% below MA)",
						pos.Symbol, percentGainFromEntry*100, pos.StopLoss)
					updatePositionInCSV(pos)
				}
			}

			// Exit if below trailing threshold
			if percentGainFromEntry > 0.20 && currentPrice < ma20*0.94 {
				exitPosition(pos, currentPrice, time.Now(), "Closed 6% below MA (wide trailing)")
				return true
			} else if percentGainFromEntry > 0.10 && currentPrice < ma20*0.95 {
				exitPosition(pos, currentPrice, time.Now(), "Closed 5% below MA (medium trailing)")
				return true
			} else if percentGainFromEntry > 0.05 && currentPrice < ma20*0.96 {
				exitPosition(pos, currentPrice, time.Now(), "Closed 4% below MA (tight trailing)")
				return true
			}
		}
	}

	updatePositionInCSV(pos)
	return false
}

func takeStrongEPProfit(pos *Position, currentPrice float64, date time.Time) {
	sharesToSell := math.Floor(pos.Shares * STRONG_EP_TAKE_PERCENT)
	saleProceeds := sharesToSell * currentPrice
	remainingShares := pos.Shares - sharesToSell
	profit := (currentPrice - pos.EntryPrice) * sharesToSell

	fmt.Printf("\n[%s] 🚀 STRONG EP! Taking %.0f%% profit on %s\n",
		pos.Symbol, STRONG_EP_TAKE_PERCENT*100, date.Format("2006-01-02"))
	fmt.Printf("    Selling %.0f shares @ $%.2f = $%.2f (Profit: $%.2f)\n",
		sharesToSell, currentPrice, saleProceeds, profit)

	// Execute sell order
	if err := executeSellOrder(pos.Symbol, sharesToSell, currentPrice); err != nil {
		log.Printf("[%s] ⚠️  Failed to execute sell order, but recording partial exit", pos.Symbol)
	}

	updateAccountSize(saleProceeds)
	pos.CumulativeProfit += profit

	recordTradeResult(TradeResult{
		Symbol:      pos.Symbol,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   currentPrice,
		Shares:      sharesToSell,
		InitialRisk: pos.InitialRisk,
		ProfitLoss:  profit,
		RiskReward:  (currentPrice - pos.EntryPrice) / pos.InitialRisk,
		EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
		ExitDate:    date.Format("2006-01-02"),
		ExitReason:  fmt.Sprintf("Strong EP - %.0f%% sold", STRONG_EP_TAKE_PERCENT*100),
		IsWinner:    true,
		AccountSize: accountSize,
	})

	pos.Shares = remainingShares
	pos.StopLoss = math.Max(pos.EntryPrice, pos.StopLoss)
	pos.Status = "MONITORING (Strong EP)"
	pos.ProfitTaken = true
	pos.TrailingStopMode = true

	log.Printf("[%s] ✅ Stop at BE: $%.2f | %.0f shares remaining | Cumulative: $%.2f\n",
		pos.Symbol, pos.StopLoss, remainingShares, pos.CumulativeProfit)
}

func takeProfitPartial(pos *Position, currentPrice float64, date time.Time, percent float64, level int) {
	sharesToSell := math.Floor(pos.Shares * percent)
	saleProceeds := sharesToSell * currentPrice
	remainingShares := pos.Shares - sharesToSell
	profit := (currentPrice - pos.EntryPrice) * sharesToSell
	rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk

	fmt.Printf("\n[%s] 🎯 PROFIT LEVEL %d (%.2fR) on %s | Selling %.0f%% (%.0f shares) @ $%.2f = $%.2f (Profit: $%.2f)\n",
		pos.Symbol, level, rr, date.Format("2006-01-02"), percent*100, sharesToSell, currentPrice, saleProceeds, profit)

	// Execute sell order
	if err := executeSellOrder(pos.Symbol, sharesToSell, currentPrice); err != nil {
		log.Printf("[%s] ⚠️  Failed to execute sell order, but recording partial exit", pos.Symbol)
	}

	updateAccountSize(saleProceeds)
	pos.CumulativeProfit += profit

	recordTradeResult(TradeResult{
		Symbol:      pos.Symbol,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   currentPrice,
		Shares:      sharesToSell,
		InitialRisk: pos.InitialRisk,
		ProfitLoss:  profit,
		RiskReward:  rr,
		EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
		ExitDate:    date.Format("2006-01-02"),
		ExitReason:  fmt.Sprintf("Profit Level %d at %.2fR", level, rr),
		IsWinner:    true,
		AccountSize: accountSize,
	})

	pos.Shares = remainingShares
	pos.Status = fmt.Sprintf("MONITORING (Lvl %d)", level)

	log.Printf("[%s] ✅ %.0f shares remaining | Cumulative: $%.2f | Stop: $%.2f\n",
		pos.Symbol, remainingShares, pos.CumulativeProfit, pos.StopLoss)
}

func stopOut(pos *Position, exitPrice float64, date time.Time) {
	totalProceeds := exitPrice * pos.Shares
	costBasis := pos.EntryPrice * pos.Shares
	profitLoss := totalProceeds - costBasis
	totalProfitLoss := profitLoss + pos.CumulativeProfit

	isWinner := totalProfitLoss > 0
	exitReason := "Stop Loss Hit"

	if isWinner && pos.ProfitTaken {
		exitReason = "Trailing Stop Hit (Profit Protected)"
		fmt.Printf("\n[%s] 📊 TRAILING STOP HIT on %s @ $%.2f | Remaining P/L: $%.2f | Total P/L: $%.2f\n",
			pos.Symbol, date.Format("2006-01-02"), exitPrice, profitLoss, totalProfitLoss)
	} else {
		fmt.Printf("\n[%s] 🛑 STOPPED OUT on %s @ $%.2f | Loss: $%.2f\n",
			pos.Symbol, date.Format("2006-01-02"), exitPrice, profitLoss)
	}

	// Execute sell order for all remaining shares
	if err := executeSellOrder(pos.Symbol, pos.Shares, exitPrice); err != nil {
		log.Printf("[%s] ⚠️  Failed to execute sell order, but recording exit", pos.Symbol)
	}

	updateAccountSize(totalProceeds)

	exitReasonDetail := exitReason
	if pos.CumulativeProfit > 0 {
		exitReasonDetail = fmt.Sprintf("%s (Previous partial profits: $%.2f)", exitReason, pos.CumulativeProfit)
	}

	recordTradeResult(TradeResult{
		Symbol:      pos.Symbol,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   exitPrice,
		Shares:      pos.Shares,
		InitialRisk: pos.InitialRisk,
		ProfitLoss:  profitLoss,
		RiskReward:  (exitPrice - pos.EntryPrice) / pos.InitialRisk,
		EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
		ExitDate:    date.Format("2006-01-02"),
		ExitReason:  exitReasonDetail,
		IsWinner:    isWinner,
		AccountSize: accountSize,
	})

	removeFromWatchlist(pos.Symbol)
	printCurrentStats()
}

func exitPosition(pos *Position, currentPrice float64, date time.Time, reason string) {
	totalProceeds := currentPrice * pos.Shares
	costBasis := pos.EntryPrice * pos.Shares
	profit := totalProceeds - costBasis
	totalProfit := profit + pos.CumulativeProfit

	fmt.Printf("\n[%s] 📤 EXITING on %s @ $%.2f | Reason: %s | Remaining P/L: $%.2f | Total P/L: $%.2f\n",
		pos.Symbol, date.Format("2006-01-02"), currentPrice, reason, profit, totalProfit)

	// Execute sell order for all remaining shares
	if err := executeSellOrder(pos.Symbol, pos.Shares, currentPrice); err != nil {
		log.Printf("[%s] ⚠️  Failed to execute sell order, but recording exit", pos.Symbol)
	}

	updateAccountSize(totalProceeds)

	rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk

	reasonDetail := reason
	if pos.CumulativeProfit > 0 {
		reasonDetail = fmt.Sprintf("%s (Previous partial profits: $%.2f)", reason, pos.CumulativeProfit)
	}

	recordTradeResult(TradeResult{
		Symbol:      pos.Symbol,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   currentPrice,
		Shares:      pos.Shares,
		InitialRisk: pos.InitialRisk,
		ProfitLoss:  profit,
		RiskReward:  rr,
		EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
		ExitDate:    date.Format("2006-01-02"),
		ExitReason:  reasonDetail,
		IsWinner:    totalProfit > 0,
		AccountSize: accountSize,
	})

	removeFromWatchlist(pos.Symbol)
	printCurrentStats()
}

func initializeTradeResultsFile() {
	filename := "trade_results.csv"
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		file, err := os.Create(filename)
		if err != nil {
			log.Printf("Error creating trade results file: %v", err)
			return
		}
		defer file.Close()

		writer := csv.NewWriter(file)
		header := []string{"Symbol", "EntryPrice", "ExitPrice", "Shares", "InitialRisk", "ProfitLoss", "RiskReward", "EntryDate", "ExitDate", "ExitReason", "IsWinner", "AccountSize"}
		writer.Write(header)
		writer.Flush()
	}
}

func recordTradeResult(result TradeResult) {
	file, err := os.OpenFile("trade_results.csv", os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening trade results file: %v", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	row := []string{
		result.Symbol,
		fmt.Sprintf("%.2f", result.EntryPrice),
		fmt.Sprintf("%.2f", result.ExitPrice),
		fmt.Sprintf("%.2f", result.Shares),
		fmt.Sprintf("%.2f", result.InitialRisk),
		fmt.Sprintf("%.2f", result.ProfitLoss),
		fmt.Sprintf("%.2f", result.RiskReward),
		result.EntryDate,
		result.ExitDate,
		result.ExitReason,
		fmt.Sprintf("%t", result.IsWinner),
		fmt.Sprintf("%.2f", result.AccountSize),
	}
	writer.Write(row)
	writer.Flush()

	log.Printf("[%s] ✍️  Trade recorded: P/L: $%.2f, R/R: %.2f, Account: $%.2f",
		result.Symbol, result.ProfitLoss, result.RiskReward, result.AccountSize)
}

func updatePositionInCSV(pos *Position) {
	if watchlistPath == "" {
		return
	}

	file, err := os.Open(watchlistPath)
	if err != nil {
		log.Printf("[%s] Error opening CSV: %v", pos.Symbol, err)
		return
	}

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	file.Close()

	if err != nil {
		log.Printf("[%s] Error reading CSV: %v", pos.Symbol, err)
		return
	}

	found := false
	for i := 1; i < len(records); i++ {
		if records[i][0] == pos.Symbol {
			records[i][1] = fmt.Sprintf("%.2f", pos.EntryPrice)
			records[i][2] = fmt.Sprintf("%.2f", pos.StopLoss)
			records[i][3] = fmt.Sprintf("%.2f", pos.Shares)
			if len(records[i]) > 4 {
				records[i][4] = pos.PurchaseDate.Format("2006-01-02")
			} else {
				records[i] = append(records[i], pos.PurchaseDate.Format("2006-01-02"))
			}
			if len(records[i]) > 5 {
				records[i][5] = pos.Status
			} else {
				records[i] = append(records[i], pos.Status)
			}
			if len(records[i]) > 6 {
				records[i][6] = fmt.Sprintf("%d", pos.DaysHeld)
			} else {
				records[i] = append(records[i], fmt.Sprintf("%d", pos.DaysHeld))
			}
			found = true
			break
		}
	}

	if !found {
		log.Printf("[%s] Position not found in CSV", pos.Symbol)
		return
	}

	outFile, err := os.Create(watchlistPath)
	if err != nil {
		log.Printf("[%s] Error creating CSV: %v", pos.Symbol, err)
		return
	}
	defer outFile.Close()

	writer := csv.NewWriter(outFile)
	err = writer.WriteAll(records)
	if err != nil {
		log.Printf("[%s] Error writing CSV: %v", pos.Symbol, err)
	}
	writer.Flush()
}

func printCurrentStats() {
	file, err := os.Open("trade_results.csv")
	if err != nil {
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return
	}

	if len(records) <= 1 {
		return
	}

	totalTrades := len(records) - 1
	winners := 0
	totalPL := 0.0
	winnerRR := 0.0
	loserRR := 0.0
	losers := 0

	for i := 1; i < len(records); i++ {
		row := records[i]
		isWinner := row[10] == "true"
		pl, _ := strconv.ParseFloat(row[5], 64)
		rr, _ := strconv.ParseFloat(row[6], 64)

		totalPL += pl
		if isWinner {
			winners++
			winnerRR += rr
		} else {
			losers++
			loserRR += rr
		}
	}

	winRate := float64(winners) / float64(totalTrades) * 100
	avgWinRR := 0.0
	if winners > 0 {
		avgWinRR = winnerRR / float64(winners)
	}
	avgLossRR := 0.0
	if losers > 0 {
		avgLossRR = loserRR / float64(losers)
	}

	initialAccount := getAccountSize()
	returnPct := (accountSize - initialAccount) / initialAccount * 100

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("📈 CURRENT STATISTICS")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("Total Trades: %d | Winners: %d (%.1f%%) | Losers: %d (%.1f%%)\n",
		totalTrades, winners, winRate, losers, 100-winRate)
	fmt.Printf("Avg Win R/R: %.2fR | Avg Loss R/R: %.2fR\n", avgWinRR, avgLossRR)
	fmt.Printf("Total P/L: $%.2f | Return: %.2f%%\n", totalPL, returnPct)
	fmt.Printf("Starting: $%.2f | Current: $%.2f\n", initialAccount, accountSize)
	fmt.Println(strings.Repeat("=", 70) + "\n")
}

func parsePosition(row []string) *Position {
	if len(row) < 5 {
		return nil
	}

	entryPrice, err1 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[1], "$")), 64)
	stopLoss, err2 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[2], "$")), 64)
	shares, err3 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[3], "$")), 64)

	if err1 != nil || err2 != nil || err3 != nil {
		log.Println("Skipping row with invalid data:", row)
		return nil
	}

	purchaseDate, err := time.Parse("2006-01-02", strings.TrimSpace(row[4]))
	if err != nil {
		log.Printf("Error parsing date for %s: %v", row[0], err)
		return nil
	}

	status := "HOLDING"
	if len(row) > 5 {
		status = strings.TrimSpace(row[5])
	}

	daysHeld := 0
	if len(row) > 6 {
		daysHeld, _ = strconv.Atoi(strings.TrimSpace(row[6]))
	}

	// Parse profit taken flags from status
	profitTaken := false
	profitTaken2 := false
	profitTaken3 := false
	if strings.Contains(status, "Lvl 1") {
		profitTaken = true
	} else if strings.Contains(status, "Lvl 2") {
		profitTaken = true
		profitTaken2 = true
	} else if strings.Contains(status, "Lvl 3") {
		profitTaken = true
		profitTaken2 = true
		profitTaken3 = true
	} else if status == "MONITORING" || strings.Contains(status, "Strong EP") {
		profitTaken = true
	}

	pos := &Position{
		Symbol:        strings.TrimSpace(row[0]),
		EntryPrice:    entryPrice,
		StopLoss:      stopLoss,
		Shares:        shares,
		PurchaseDate:  purchaseDate,
		Status:        status,
		ProfitTaken:   profitTaken,
		ProfitTaken2:  profitTaken2,
		ProfitTaken3:  profitTaken3,
		DaysHeld:      daysHeld,
		InitialShares: shares, // Assume current shares if not tracking separately
	}

	return pos
}
