package main

// import (
// 	"encoding/csv"
// 	"encoding/json"
// 	"fmt"
// 	"io"
// 	"log"
// 	"math"
// 	"net/http"
// 	"os"
// 	"path/filepath"
// 	"sort"
// 	"strconv"
// 	"strings"
// 	"sync"
// 	"time"

// 	"github.com/joho/godotenv"
// )

// const (
// 	CSV_CHECK_INTERVAL = 10 * time.Second
// 	SIMULATION_MODE    = true

// 	MAX_STOP_DISTANCE_MULTIPLIER = 2.0
// 	ATR_PERIOD                   = 14

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

// 	MA_PERIOD = 20

// 	WEAK_CLOSE_THRESHOLD = 0.30

// 	MAX_DAYS_NO_FOLLOWTHROUGH = 8

// 	MIN_VOLUME_RATIO = 1.2
// 	MIN_PRICE        = 2.0
// 	MAX_PRICE        = 200.0
// )

// // ---------------------------------------------------------------------------
// // Alpaca response structs
// // ---------------------------------------------------------------------------

// // AlpacaBarsResponse is returned by GET /v2/stocks/{symbol}/bars
// type AlpacaBarsResponse struct {
// 	Bars          []AlpacaBar `json:"bars"`
// 	Symbol        string      `json:"symbol"`
// 	NextPageToken string      `json:"next_page_token"`
// }

// // AlpacaBar represents a single OHLCV bar from Alpaca.
// type AlpacaBar struct {
// 	// Alpaca uses "t" for timestamp (RFC3339), "o","h","l","c","v"
// 	Timestamp string  `json:"t"`
// 	Open      float64 `json:"o"`
// 	High      float64 `json:"h"`
// 	Low       float64 `json:"l"`
// 	Close     float64 `json:"c"`
// 	Volume    float64 `json:"v"`
// }

// // ---------------------------------------------------------------------------
// // Internal types (unchanged from original)
// // ---------------------------------------------------------------------------

// // MarketstackEODData is kept as the internal EOD representation so the rest
// // of the logic doesn't need to change — we just populate it from Alpaca bars.
// type MarketstackEODData struct {
// 	Symbol string
// 	Open   float64
// 	High   float64
// 	Low    float64
// 	Close  float64
// 	Volume float64
// 	Date   string // stored as "2006-01-02T00:00:00+0000" to keep compat
// }

// type Position struct {
// 	Symbol           string
// 	EntryPrice       float64
// 	StopLoss         float64
// 	Shares           float64
// 	PurchaseDate     time.Time
// 	Status           string
// 	ProfitTaken      bool
// 	ProfitTaken2     bool
// 	ProfitTaken3     bool
// 	DaysHeld         int
// 	LastCheckDate    time.Time
// 	InitialStopLoss  float64
// 	InitialRisk      float64
// 	EPDayLow         float64
// 	EPDayHigh        float64
// 	HighestPrice     float64
// 	TrailingStopMode bool
// 	CumulativeProfit float64
// 	InitialShares    float64
// 	AverageVolume    float64
// 	mu               sync.Mutex
// }

// type TradeResult struct {
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
// 	AccountSize float64
// }

// var (
// 	activePositions  = make(map[string]*Position)
// 	processedSymbols = make(map[string]bool)
// 	accountSize      float64
// 	riskPerTrade     float64

// 	alpacaKeyID     string
// 	alpacaSecretKey string
// 	alpacaBaseURL   string

// 	lastModTime     time.Time
// 	historicalCache = make(map[string][]MarketstackEODData)
// 	watchlistPath   string
// 	currentSimDate  time.Time

// 	accountMu      sync.Mutex
// 	positionsMu    sync.RWMutex
// 	processedMu    sync.Mutex
// 	tradeResultsMu sync.Mutex
// 	cacheMu        sync.RWMutex
// )

// // ---------------------------------------------------------------------------
// // main
// // ---------------------------------------------------------------------------

// func main() {
// 	loadEnv()

// 	alpacaKeyID = os.Getenv("ALPACA_API_KEY")
// 	alpacaSecretKey = os.Getenv("ALPACA_SECRET_KEY")
// 	alpacaBaseURL = os.Getenv("ALPACA_BASE_URL")

// 	if alpacaKeyID == "" || alpacaSecretKey == "" {
// 		log.Fatal("ALPACA_API_KEY and ALPACA_SECRET_KEY must be set")
// 	}
// 	if alpacaBaseURL == "" {
// 		alpacaBaseURL = "https://data.alpaca.markets/v2"
// 		log.Println("ALPACA_BASE_URL not set, defaulting to", alpacaBaseURL)
// 	}

// 	accountSize = getAccountSize()
// 	riskPerTrade = getRiskPerTrade()
// 	log.Printf("Starting Account Size: $%.2f", accountSize)
// 	log.Printf("Risk Per Trade: %.2f%%", riskPerTrade*100)

// 	initializeTradeResultsFile()

// 	if SIMULATION_MODE {
// 		log.Println("⚡ Starting FAST BACKTEST mode... Watching for CSV updates")
// 	} else {
// 		log.Println("🕐 Starting REAL-TIME mode... Watching for CSV updates")
// 	}
// 	log.Println("Press Ctrl+C to stop")

// 	for {
// 		checkAndProcessWatchlist()

// 		if SIMULATION_MODE {
// 			runFullSimulation()
// 		} else {
// 			simulateActivePositions()
// 		}

// 		time.Sleep(CSV_CHECK_INTERVAL)
// 	}
// }

// // ---------------------------------------------------------------------------
// // Env / config helpers
// // ---------------------------------------------------------------------------

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

// func getAccountSize() float64 {
// 	s := os.Getenv("ACCOUNT_SIZE")
// 	if s == "" {
// 		log.Println("ACCOUNT_SIZE not set, using default $10,000")
// 		return 10000.0
// 	}
// 	v, err := strconv.ParseFloat(s, 64)
// 	if err != nil {
// 		log.Println("Invalid ACCOUNT_SIZE, using default $10,000")
// 		return 10000.0
// 	}
// 	return v
// }

// func getRiskPerTrade() float64 {
// 	s := os.Getenv("RISK_PER_TRADE")
// 	if s == "" {
// 		log.Println("RISK_PER_TRADE not set, using default 1%")
// 		return 0.01
// 	}
// 	v, err := strconv.ParseFloat(s, 64)
// 	if err != nil {
// 		log.Println("Invalid RISK_PER_TRADE, using default 1%")
// 		return 0.01
// 	}
// 	return v
// }

// // ---------------------------------------------------------------------------
// // Account helpers (unchanged)
// // ---------------------------------------------------------------------------

// func updateAccountSize(amount float64) {
// 	accountMu.Lock()
// 	defer accountMu.Unlock()
// 	accountSize += amount
// 	log.Printf("💰 Account balance: $%.2f (change: $%.2f)", accountSize, amount)
// }

// func getAccountSize_Safe() float64 {
// 	accountMu.Lock()
// 	defer accountMu.Unlock()
// 	return accountSize
// }

// func getTotalAccountValue() float64 {
// 	accountMu.Lock()
// 	cash := accountSize
// 	accountMu.Unlock()

// 	positionsMu.RLock()
// 	defer positionsMu.RUnlock()

// 	total := cash
// 	for _, pos := range activePositions {
// 		total += pos.EntryPrice * pos.Shares
// 	}
// 	return total
// }

// // ---------------------------------------------------------------------------
// // Alpaca HTTP helper — adds auth headers automatically
// // ---------------------------------------------------------------------------

// func alpacaGet(url string) ([]byte, error) {
// 	req, err := http.NewRequest("GET", url, nil)
// 	if err != nil {
// 		return nil, err
// 	}
// 	req.Header.Set("APCA-API-KEY-ID", alpacaKeyID)
// 	req.Header.Set("APCA-API-SECRET-KEY", alpacaSecretKey)

// 	resp, err := http.DefaultClient.Do(req)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer resp.Body.Close()
// 	return io.ReadAll(resp.Body)
// }

// // ---------------------------------------------------------------------------
// // fetchAllHistoricalData — replaces Marketstack EOD with Alpaca daily bars
// // ---------------------------------------------------------------------------

// func fetchAllHistoricalData(symbol string, startDate time.Time) bool {
// 	cacheMu.RLock()
// 	_, exists := historicalCache[symbol]
// 	cacheMu.RUnlock()
// 	if exists {
// 		return true
// 	}

// 	fromDate := startDate.AddDate(0, 0, -30)
// 	toDate := time.Now()

// 	log.Printf("[%s] 📥 Fetching ALL historical data from %s to %s via Alpaca...",
// 		symbol, fromDate.Format("2006-01-02"), toDate.Format("2006-01-02"))

// 	var allBars []AlpacaBar
// 	pageToken := ""

// 	for {
// 		url := fmt.Sprintf(
// 			"%s/stocks/%s/bars?timeframe=1Day&start=%s&end=%s&limit=1000&adjustment=raw&feed=iex",
// 			alpacaBaseURL,
// 			symbol,
// 			fromDate.Format("2006-01-02"),
// 			toDate.Format("2006-01-02"),
// 		)
// 		if pageToken != "" {
// 			url += "&page_token=" + pageToken
// 		}

// 		body, err := alpacaGet(url)
// 		if err != nil {
// 			log.Printf("[%s] ❌ Error fetching historical data: %v", symbol, err)
// 			return false
// 		}

// 		var result AlpacaBarsResponse
// 		if err := json.Unmarshal(body, &result); err != nil {
// 			log.Printf("[%s] ❌ Error parsing historical data: %v\nBody: %s", symbol, err, string(body))
// 			return false
// 		}

// 		allBars = append(allBars, result.Bars...)

// 		if result.NextPageToken == "" {
// 			break
// 		}
// 		pageToken = result.NextPageToken
// 		time.Sleep(100 * time.Millisecond)
// 	}

// 	// Convert AlpacaBar → internal MarketstackEODData
// 	converted := make([]MarketstackEODData, 0, len(allBars))
// 	for _, b := range allBars {
// 		// Alpaca timestamps are RFC3339, e.g. "2024-01-15T00:00:00Z"
// 		// Normalise to "2006-01-02T00:00:00+0000" so existing parsing works.
// 		t, err := time.Parse(time.RFC3339, b.Timestamp)
// 		if err != nil {
// 			// Try date-only fallback
// 			t, err = time.Parse("2006-01-02", b.Timestamp[:10])
// 			if err != nil {
// 				continue
// 			}
// 		}
// 		converted = append(converted, MarketstackEODData{
// 			Symbol: symbol,
// 			Open:   b.Open,
// 			High:   b.High,
// 			Low:    b.Low,
// 			Close:  b.Close,
// 			Volume: b.Volume,
// 			// Store in the format the rest of the code parses
// 			Date: t.UTC().Format("2006-01-02T15:04:05+0000"),
// 		})
// 	}

// 	sort.Slice(converted, func(i, j int) bool {
// 		di, _ := time.Parse("2006-01-02T15:04:05+0000", converted[i].Date)
// 		dj, _ := time.Parse("2006-01-02T15:04:05+0000", converted[j].Date)
// 		return di.Before(dj)
// 	})

// 	cacheMu.Lock()
// 	historicalCache[symbol] = converted
// 	cacheMu.Unlock()

// 	log.Printf("[%s] ✅ Cached %d days of historical data", symbol, len(converted))
// 	return true
// }

// // ---------------------------------------------------------------------------
// // checkWeakCloseIntraday — replaces Polygon with Alpaca 5-min bars
// // ---------------------------------------------------------------------------

// func checkWeakCloseIntraday(symbol string, date time.Time) (bool, float64) {
// 	dateStr := date.Format("2006-01-02")
// 	// end = next calendar day so we capture the full session
// 	endStr := date.AddDate(0, 0, 1).Format("2006-01-02")

// 	url := fmt.Sprintf(
// 		"%s/stocks/%s/bars?timeframe=5Min&start=%s&end=%s&limit=1000&adjustment=raw&feed=iex",
// 		alpacaBaseURL, symbol, dateStr, endStr,
// 	)

// 	body, err := alpacaGet(url)
// 	if err != nil {
// 		log.Printf("[%s] ⚠️  Error fetching intraday data: %v", symbol, err)
// 		return false, 0
// 	}

// 	var result AlpacaBarsResponse
// 	if err := json.Unmarshal(body, &result); err != nil {
// 		log.Printf("[%s] ⚠️  Error parsing intraday data: %v", symbol, err)
// 		return false, 0
// 	}

// 	if len(result.Bars) == 0 {
// 		return false, 0
// 	}

// 	dayHigh := 0.0
// 	for i, bar := range result.Bars {
// 		if bar.High > dayHigh {
// 			dayHigh = bar.High
// 		}
// 		if i > 0 && dayHigh > 0 {
// 			closeFromHigh := (dayHigh - bar.Close) / dayHigh
// 			if closeFromHigh >= WEAK_CLOSE_THRESHOLD {
// 				t, _ := time.Parse(time.RFC3339, bar.Timestamp)
// 				log.Printf("[%s] ⚠️  WEAK CLOSE detected at %s | Close: $%.2f (%.1f%% from high $%.2f)",
// 					symbol, t.Format("15:04"), bar.Close, closeFromHigh*100, dayHigh)
// 				return true, bar.Close
// 			}
// 		}
// 	}

// 	return false, 0
// }

// // ---------------------------------------------------------------------------
// // The rest of the file is unchanged — all logic reuses MarketstackEODData
// // ---------------------------------------------------------------------------

// func calculateATR(symbol string, endDate time.Time, period int) float64 {
// 	cacheMu.RLock()
// 	historicalData, exists := historicalCache[symbol]
// 	cacheMu.RUnlock()

// 	if !exists || len(historicalData) < period {
// 		return 0
// 	}

// 	endIdx := 0
// 	for i := range historicalData {
// 		dataDate, _ := time.Parse("2006-01-02T15:04:05+0000", historicalData[i].Date)
// 		if !dataDate.After(endDate) {
// 			endIdx = i
// 		}
// 	}

// 	var trueRanges []float64
// 	for i := endIdx - period + 1; i <= endIdx && i < len(historicalData); i++ {
// 		if i <= 0 {
// 			continue
// 		}
// 		cur := historicalData[i]
// 		prev := historicalData[i-1]

// 		tr := math.Max(cur.High-cur.Low,
// 			math.Max(math.Abs(cur.High-prev.Close), math.Abs(cur.Low-prev.Close)))
// 		trueRanges = append(trueRanges, tr)
// 	}

// 	if len(trueRanges) == 0 {
// 		return 0
// 	}
// 	sum := 0.0
// 	for _, v := range trueRanges {
// 		sum += v
// 	}
// 	return sum / float64(len(trueRanges))
// }

// func calculateAverageVolume(symbol string, endDate time.Time, period int) float64 {
// 	cacheMu.RLock()
// 	historicalData, exists := historicalCache[symbol]
// 	cacheMu.RUnlock()

// 	if !exists || len(historicalData) < period {
// 		return 0
// 	}

// 	endIdx := 0
// 	for i := range historicalData {
// 		dataDate, _ := time.Parse("2006-01-02T15:04:05+0000", historicalData[i].Date)
// 		if !dataDate.After(endDate) {
// 			endIdx = i
// 		}
// 	}

// 	if endIdx < period {
// 		return 0
// 	}

// 	sum := 0.0
// 	for i := endIdx - period; i < endIdx; i++ {
// 		if i >= 0 {
// 			sum += historicalData[i].Volume
// 		}
// 	}
// 	return sum / float64(period)
// }

// func calculate20DayMA(symbol string, currentDate time.Time) float64 {
// 	cacheMu.RLock()
// 	historicalData, exists := historicalCache[symbol]
// 	cacheMu.RUnlock()

// 	if !exists {
// 		return 0
// 	}

// 	var prices []float64
// 	for i := range historicalData {
// 		dataDate, _ := time.Parse("2006-01-02T15:04:05+0000", historicalData[i].Date)
// 		if !dataDate.After(currentDate) {
// 			prices = append(prices, historicalData[i].Close)
// 		}
// 	}

// 	if len(prices) < MA_PERIOD {
// 		return 0
// 	}

// 	recent := prices[len(prices)-MA_PERIOD:]
// 	sum := 0.0
// 	for _, p := range recent {
// 		sum += p
// 	}
// 	return sum / float64(MA_PERIOD)
// }

// // ---------------------------------------------------------------------------
// // Watchlist / position management (unchanged)
// // ---------------------------------------------------------------------------

// func checkAndProcessWatchlist() {
// 	filePath := "watchlist.csv"
// 	fileInfo, err := os.Stat(filePath)
// 	if err != nil {
// 		filePath = "cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv"
// 		fileInfo, err = os.Stat(filePath)
// 		if err != nil {
// 			return
// 		}
// 	}

// 	watchlistPath = filePath

// 	if fileInfo.ModTime().After(lastModTime) {
// 		lastModTime = fileInfo.ModTime()
// 		log.Println("📋 Watchlist updated, processing new entries...")
// 		processWatchlist(filePath)
// 	}
// }

// func processWatchlist(filePath string) {
// 	file, err := os.Open(filePath)
// 	if err != nil {
// 		return
// 	}
// 	defer file.Close()

// 	reader := csv.NewReader(file)
// 	records, err := reader.ReadAll()
// 	if err != nil {
// 		log.Printf("Error reading CSV: %v", err)
// 		return
// 	}

// 	if len(records) <= 1 {
// 		return
// 	}

// 	var positions []*Position
// 	processedMu.Lock()
// 	positionsMu.RLock()
// 	for i := 1; i < len(records); i++ {
// 		pos := parsePosition(records[i])
// 		if pos != nil && !processedSymbols[pos.Symbol] && activePositions[pos.Symbol] == nil {
// 			positions = append(positions, pos)
// 		}
// 	}
// 	positionsMu.RUnlock()
// 	processedMu.Unlock()

// 	sort.Slice(positions, func(i, j int) bool {
// 		return positions[i].PurchaseDate.Before(positions[j].PurchaseDate)
// 	})

// 	dateGroups := make(map[string][]*Position)
// 	for _, pos := range positions {
// 		dateGroups[pos.PurchaseDate.Format("2006-01-02")] = append(dateGroups[pos.PurchaseDate.Format("2006-01-02")], pos)
// 	}

// 	for _, datePositions := range dateGroups {
// 		if len(datePositions) == 0 {
// 			continue
// 		}
// 		if !shouldStartPositionGroup(datePositions[0]) {
// 			continue
// 		}
// 		scaled := scalePositionsToFit(datePositions)
// 		var valid []*Position
// 		for _, pos := range scaled {
// 			if startPosition(pos) {
// 				valid = append(valid, pos)
// 			}
// 		}
// 		if len(valid) > 0 {
// 			log.Printf("🚀 Starting concurrent simulation for %d positions on %s",
// 				len(valid), valid[0].PurchaseDate.Format("2006-01-02"))
// 		}
// 	}
// }

// func shouldStartPositionGroup(newPos *Position) bool {
// 	positionsMu.RLock()
// 	defer positionsMu.RUnlock()

// 	if len(activePositions) == 0 {
// 		return true
// 	}
// 	for _, ap := range activePositions {
// 		days := int(newPos.PurchaseDate.Sub(ap.PurchaseDate).Hours() / 24)
// 		if days >= -1 && days <= 1 {
// 			return true
// 		}
// 	}
// 	return false
// }

// func recalculateShares(pos *Position, currentAccountSize float64) *Position {
// 	dollarRisk := riskPerTrade * currentAccountSize
// 	riskPerShare := pos.EntryPrice - pos.StopLoss
// 	if riskPerShare <= 0 {
// 		log.Printf("[%s] ⚠️  Invalid risk calculation, cannot recalculate shares", pos.Symbol)
// 		return pos
// 	}
// 	newShares := math.Round(dollarRisk / riskPerShare)
// 	if newShares != pos.Shares {
// 		log.Printf("[%s] 📊 Recalculated shares: %.0f -> %.0f (Account: $%.2f, DollarRisk: $%.2f, RPS: $%.2f)",
// 			pos.Symbol, pos.Shares, newShares, currentAccountSize, dollarRisk, riskPerShare)
// 		pos.Shares = newShares
// 	}
// 	return pos
// }

// func scalePositionsToFit(positions []*Position) []*Position {
// 	total := getTotalAccountValue()
// 	out := make([]*Position, len(positions))
// 	for i, pos := range positions {
// 		cp := *pos
// 		out[i] = recalculateShares(&cp, total)
// 	}
// 	return out
// }

// func removeFromWatchlist(symbol string) {
// 	if watchlistPath == "" {
// 		return
// 	}
// 	file, err := os.Open(watchlistPath)
// 	if err != nil {
// 		return
// 	}
// 	reader := csv.NewReader(file)
// 	records, err := reader.ReadAll()
// 	file.Close()
// 	if err != nil {
// 		return
// 	}

// 	var newRecords [][]string
// 	removed := false
// 	for i, record := range records {
// 		if i == 0 {
// 			newRecords = append(newRecords, record)
// 		} else if len(record) > 0 && strings.TrimSpace(record[0]) != symbol {
// 			newRecords = append(newRecords, record)
// 		} else {
// 			removed = true
// 		}
// 	}
// 	if !removed {
// 		return
// 	}

// 	file, err = os.Create(watchlistPath)
// 	if err != nil {
// 		return
// 	}
// 	defer file.Close()
// 	w := csv.NewWriter(file)
// 	w.WriteAll(newRecords)
// 	log.Printf("[%s] 🗑️  Removed from watchlist", symbol)
// }

// func startPosition(pos *Position) bool {
// 	log.Printf("[%s] 📥 Fetching historical data...", pos.Symbol)

// 	if !fetchAllHistoricalData(pos.Symbol, pos.PurchaseDate) {
// 		log.Printf("[%s] ⚠️  Cannot start position - failed to fetch historical data", pos.Symbol)
// 		processedMu.Lock()
// 		processedSymbols[pos.Symbol] = true
// 		processedMu.Unlock()
// 		return false
// 	}

// 	cacheMu.RLock()
// 	historicalData := historicalCache[pos.Symbol]
// 	cacheMu.RUnlock()

// 	var epDayData *MarketstackEODData
// 	for i := range historicalData {
// 		dataDate, _ := time.Parse("2006-01-02T15:04:05+0000", historicalData[i].Date)
// 		if dataDate.Format("2006-01-02") == pos.PurchaseDate.Format("2006-01-02") {
// 			epDayData = &historicalData[i]
// 			break
// 		}
// 	}

// 	if epDayData == nil {
// 		log.Printf("[%s] ⚠️  Cannot find EP day data", pos.Symbol)
// 		processedMu.Lock()
// 		processedSymbols[pos.Symbol] = true
// 		processedMu.Unlock()
// 		return false
// 	}

// 	if epDayData.Close < MIN_PRICE || epDayData.Close > MAX_PRICE {
// 		log.Printf("[%s] ⚠️  Price $%.2f outside acceptable range ($%.2f-$%.2f), skipping",
// 			pos.Symbol, epDayData.Close, MIN_PRICE, MAX_PRICE)
// 		processedMu.Lock()
// 		processedSymbols[pos.Symbol] = true
// 		processedMu.Unlock()
// 		return false
// 	}

// 	avgVolume := calculateAverageVolume(pos.Symbol, pos.PurchaseDate, 20)
// 	if avgVolume > 0 {
// 		volumeRatio := epDayData.Volume / avgVolume
// 		if volumeRatio < MIN_VOLUME_RATIO {
// 			log.Printf("[%s] ⚠️  Insufficient volume: %.2fx average (need %.2fx), skipping",
// 				pos.Symbol, volumeRatio, MIN_VOLUME_RATIO)
// 			processedMu.Lock()
// 			processedSymbols[pos.Symbol] = true
// 			processedMu.Unlock()
// 			return false
// 		}
// 		log.Printf("[%s] ✅ Volume: %.2fx average", pos.Symbol, volumeRatio)
// 		pos.AverageVolume = avgVolume
// 	}

// 	ma20 := calculate20DayMA(pos.Symbol, pos.PurchaseDate)
// 	if ma20 > 0 && epDayData.Close < ma20 {
// 		log.Printf("[%s] ⚠️  Price $%.2f below 20-day MA $%.2f, skipping weak setup",
// 			pos.Symbol, epDayData.Close, ma20)
// 		processedMu.Lock()
// 		processedSymbols[pos.Symbol] = true
// 		processedMu.Unlock()
// 		return false
// 	}

// 	pos.EPDayLow = epDayData.Low
// 	pos.EPDayHigh = epDayData.High

// 	atr := calculateATR(pos.Symbol, pos.PurchaseDate, ATR_PERIOD)

// 	var finalStop float64
// 	if pos.StopLoss > 0 && pos.StopLoss < pos.EntryPrice {
// 		finalStop = pos.StopLoss
// 		log.Printf("[%s] ℹ️  Using watchlist stop loss: $%.2f", pos.Symbol, finalStop)
// 	} else {
// 		suggestedStop := pos.EPDayLow * 0.98
// 		if atr > 0 {
// 			maxDist := atr * MAX_STOP_DISTANCE_MULTIPLIER
// 			if pos.EntryPrice-suggestedStop > maxDist {
// 				suggestedStop = pos.EntryPrice - maxDist
// 				log.Printf("[%s] ⚠️  Stop adjusted to ATR limit. ATR: $%.2f, MaxDist: $%.2f", pos.Symbol, atr, maxDist)
// 			}
// 		}
// 		finalStop = suggestedStop
// 		log.Printf("[%s] ℹ️  Calculated stop: $%.2f (EP Low: $%.2f)", pos.Symbol, finalStop, pos.EPDayLow)
// 	}

// 	pos.StopLoss = finalStop
// 	pos.InitialStopLoss = finalStop
// 	pos.InitialRisk = pos.EntryPrice - pos.StopLoss
// 	pos.HighestPrice = pos.EntryPrice

// 	total := getTotalAccountValue()
// 	pos = recalculateShares(pos, total)

// 	if pos.InitialRisk <= 0 {
// 		log.Printf("[%s] ⚠️  Invalid risk. Entry: $%.2f, Stop: $%.2f", pos.Symbol, pos.EntryPrice, pos.StopLoss)
// 		processedMu.Lock()
// 		processedSymbols[pos.Symbol] = true
// 		processedMu.Unlock()
// 		return false
// 	}

// 	positionCost := pos.EntryPrice * pos.Shares
// 	currentCash := getAccountSize_Safe()

// 	if positionCost > currentCash {
// 		available := currentCash * 0.5
// 		affordable := math.Floor(available / pos.EntryPrice)
// 		if affordable < 1 {
// 			log.Printf("[%s] ⚠️  Insufficient funds. Need $%.2f, have $%.2f. Skipping.", pos.Symbol, pos.EntryPrice, currentCash)
// 			processedMu.Lock()
// 			processedSymbols[pos.Symbol] = true
// 			processedMu.Unlock()
// 			return false
// 		}
// 		log.Printf("[%s] ⚠️  Adjusting to 50%% cash: %.0f shares instead of %.0f", pos.Symbol, affordable, pos.Shares)
// 		pos.Shares = affordable
// 		positionCost = pos.EntryPrice * pos.Shares
// 	}

// 	updateAccountSize(-positionCost)
// 	pos.LastCheckDate = pos.PurchaseDate
// 	pos.InitialShares = pos.Shares
// 	pos.CumulativeProfit = 0.0

// 	positionsMu.Lock()
// 	activePositions[pos.Symbol] = pos
// 	positionsMu.Unlock()

// 	processedMu.Lock()
// 	processedSymbols[pos.Symbol] = true
// 	processedMu.Unlock()

// 	totalVal := getTotalAccountValue()
// 	dollarRisk := totalVal * riskPerTrade
// 	log.Printf("[%s] 🟢 OPENED | Shares: %.0f @ $%.2f | Total: $%.2f | Stop: $%.2f | Risk: $%.2f (%.2f%%) | ATR: $%.2f | Date: %s",
// 		pos.Symbol, pos.Shares, pos.EntryPrice, positionCost, pos.StopLoss, dollarRisk,
// 		(pos.InitialRisk/pos.EntryPrice)*100, atr, pos.PurchaseDate.Format("2006-01-02"))

// 	hasWeakClose, weakClosePrice := checkWeakCloseIntraday(pos.Symbol, pos.PurchaseDate)
// 	if hasWeakClose && weakClosePrice > 0 {
// 		log.Printf("[%s] ⚠️  Weak close on EP day, closing @ $%.2f", pos.Symbol, weakClosePrice)

// 		proceeds := weakClosePrice * pos.Shares
// 		pl := proceeds - pos.EntryPrice*pos.Shares
// 		updateAccountSize(proceeds)

// 		recordTradeResult(TradeResult{
// 			Symbol: pos.Symbol, EntryPrice: pos.EntryPrice, ExitPrice: weakClosePrice,
// 			Shares: pos.Shares, InitialRisk: pos.InitialRisk, ProfitLoss: pl,
// 			RiskReward:  (weakClosePrice - pos.EntryPrice) / pos.InitialRisk,
// 			EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
// 			ExitDate:    pos.PurchaseDate.Format("2006-01-02"),
// 			ExitReason:  "Weak Close on EP Day",
// 			IsWinner:    pl > 0,
// 			AccountSize: getAccountSize_Safe(),
// 		})

// 		removeFromWatchlist(pos.Symbol)
// 		positionsMu.Lock()
// 		delete(activePositions, pos.Symbol)
// 		positionsMu.Unlock()
// 		return false
// 	}

// 	return true
// }

// // ---------------------------------------------------------------------------
// // Simulation runners (unchanged)
// // ---------------------------------------------------------------------------

// func runFullSimulation() {
// 	positionsMu.RLock()
// 	posCount := len(activePositions)
// 	positionsMu.RUnlock()
// 	if posCount == 0 {
// 		return
// 	}

// 	log.Printf("⚡ Fast simulating %d position(s) concurrently...", posCount)

// 	positionsMu.RLock()
// 	currentSimDate = time.Time{}
// 	for _, pos := range activePositions {
// 		if currentSimDate.IsZero() || pos.PurchaseDate.Before(currentSimDate) {
// 			currentSimDate = pos.PurchaseDate
// 		}
// 	}
// 	positionsMu.RUnlock()

// 	maxIterations := 10000
// 	for iter := 0; iter < maxIterations; iter++ {
// 		positionsMu.RLock()
// 		posCount = len(activePositions)
// 		positionsMu.RUnlock()
// 		if posCount == 0 {
// 			break
// 		}

// 		checkAndProcessWatchlist()

// 		positionsMu.RLock()
// 		var wg sync.WaitGroup
// 		toRemove := make(map[string]bool)
// 		var removeMu sync.Mutex

// 		for symbol, pos := range activePositions {
// 			if !pos.LastCheckDate.After(currentSimDate) {
// 				wg.Add(1)
// 				go func(p *Position, sym string) {
// 					defer wg.Done()
// 					if simulateNextDay(p) {
// 						removeMu.Lock()
// 						toRemove[sym] = true
// 						removeMu.Unlock()
// 					}
// 				}(pos, symbol)
// 			}
// 		}
// 		positionsMu.RUnlock()
// 		wg.Wait()

// 		if len(toRemove) > 0 {
// 			positionsMu.Lock()
// 			for sym := range toRemove {
// 				delete(activePositions, sym)
// 			}
// 			positionsMu.Unlock()
// 		}

// 		currentSimDate = currentSimDate.AddDate(0, 0, 1)

// 		if iter%100 == 0 {
// 			positionsMu.RLock()
// 			active := len(activePositions)
// 			positionsMu.RUnlock()
// 			if active > 0 {
// 				time.Sleep(50 * time.Millisecond)
// 			}
// 		}
// 	}

// 	// Force-close any remaining
// 	positionsMu.Lock()
// 	for symbol, pos := range activePositions {
// 		log.Printf("[%s] ⚠️  Force closing at last known price", symbol)
// 		cacheMu.RLock()
// 		hd := historicalCache[symbol]
// 		cacheMu.RUnlock()
// 		if len(hd) > 0 {
// 			last := hd[len(hd)-1]
// 			lastDate, _ := time.Parse("2006-01-02T15:04:05+0000", last.Date)
// 			exitPosition(pos, last.Close, lastDate, "Force closed - end of data")
// 		}
// 		delete(activePositions, symbol)
// 	}
// 	positionsMu.Unlock()

// 	log.Println("✅ All positions closed!")
// }

// func simulateActivePositions() {
// 	positionsMu.RLock()
// 	if len(activePositions) == 0 {
// 		positionsMu.RUnlock()
// 		return
// 	}
// 	log.Printf("📊 Simulating %d active position(s)...", len(activePositions))

// 	var wg sync.WaitGroup
// 	toRemove := make(map[string]bool)
// 	var removeMu sync.Mutex

// 	for symbol, pos := range activePositions {
// 		wg.Add(1)
// 		go func(p *Position, sym string) {
// 			defer wg.Done()
// 			if simulateNextDay(p) {
// 				removeMu.Lock()
// 				toRemove[sym] = true
// 				removeMu.Unlock()
// 			}
// 		}(pos, symbol)
// 	}
// 	positionsMu.RUnlock()
// 	wg.Wait()

// 	if len(toRemove) > 0 {
// 		positionsMu.Lock()
// 		for sym := range toRemove {
// 			delete(activePositions, sym)
// 		}
// 		positionsMu.Unlock()
// 	}
// }

// func simulateNextDay(pos *Position) bool {
// 	pos.mu.Lock()
// 	defer pos.mu.Unlock()

// 	cacheMu.RLock()
// 	historicalData, exists := historicalCache[pos.Symbol]
// 	cacheMu.RUnlock()
// 	if !exists {
// 		return false
// 	}

// 	var todayData *MarketstackEODData
// 	searchDate := pos.LastCheckDate.AddDate(0, 0, 1)
// 	for attempts := 0; attempts < 7; attempts++ {
// 		for i := range historicalData {
// 			dataDate, _ := time.Parse("2006-01-02T15:04:05+0000", historicalData[i].Date)
// 			if dataDate.Format("2006-01-02") == searchDate.Format("2006-01-02") {
// 				todayData = &historicalData[i]
// 				break
// 			}
// 		}
// 		if todayData != nil {
// 			break
// 		}
// 		searchDate = searchDate.AddDate(0, 0, 1)
// 	}
// 	if todayData == nil {
// 		return false
// 	}

// 	pos.LastCheckDate = searchDate
// 	pos.DaysHeld++

// 	currentPrice := todayData.Close
// 	dayLow := todayData.Low
// 	dayHigh := todayData.High
// 	currentGain := currentPrice - pos.EntryPrice
// 	currentRR := currentGain / pos.InitialRisk

// 	if dayHigh > pos.HighestPrice {
// 		pos.HighestPrice = dayHigh
// 	}

// 	log.Printf("[%s] Day %d (%s) | %s | Close: $%.2f | Low: $%.2f | High: $%.2f | Gain: $%.2f (%.1f%%) | R/R: %.2fR | Stop: $%.2f",
// 		pos.Symbol, pos.DaysHeld, searchDate.Format("2006-01-02"), pos.Status,
// 		currentPrice, dayLow, dayHigh, currentGain, (currentGain/pos.EntryPrice)*100, currentRR, pos.StopLoss)

// 	// Stop loss check
// 	checkPrice := currentPrice
// 	if pos.DaysHeld == 1 {
// 		checkPrice = dayLow
// 	}
// 	if checkPrice <= pos.StopLoss {
// 		stopOut(pos, pos.StopLoss, searchDate)
// 		return true
// 	}

// 	// Breakeven
// 	if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS && !pos.ProfitTaken {
// 		pctGain := (dayHigh - pos.EntryPrice) / pos.EntryPrice
// 		if pctGain >= BREAKEVEN_TRIGGER_PERCENT && pos.StopLoss < pos.EntryPrice {
// 			pos.StopLoss = pos.EntryPrice
// 			log.Printf("[%s] 🔒 MOVED TO BREAKEVEN - Up %.1f%% (Day %d). Stop: $%.2f",
// 				pos.Symbol, pctGain*100, pos.DaysHeld, pos.StopLoss)
// 		}
// 	}

// 	// Tighten if no follow-through
// 	if pos.DaysHeld >= MAX_DAYS_NO_FOLLOWTHROUGH && currentRR < 0.5 && !pos.ProfitTaken {
// 		tight := math.Max(pos.EntryPrice-(pos.InitialRisk*0.3), pos.EntryPrice)
// 		if tight > pos.StopLoss {
// 			pos.StopLoss = tight
// 			log.Printf("[%s] ⚠️  Weak follow-through after %d days. Tightening to $%.2f",
// 				pos.Symbol, MAX_DAYS_NO_FOLLOWTHROUGH, pos.StopLoss)
// 		}
// 	}

// 	// Strong EP
// 	pctGain := (currentPrice - pos.EntryPrice) / pos.EntryPrice
// 	if pos.DaysHeld <= STRONG_EP_DAYS && pctGain >= STRONG_EP_GAIN && !pos.ProfitTaken {
// 		takeStrongEPProfit(pos, currentPrice, searchDate)
// 		return false
// 	}

// 	// Graduated profit taking
// 	if currentRR >= PROFIT_TAKE_1_RR && !pos.ProfitTaken {
// 		takeProfitPartial(pos, currentPrice, searchDate, PROFIT_TAKE_1_PERCENT, 1)
// 		pos.TrailingStopMode = true
// 		pos.ProfitTaken = true
// 		if pos.DaysHeld >= BREAKEVEN_TRIGGER_DAYS {
// 			pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice)
// 		}
// 		return false
// 	}
// 	if currentRR >= PROFIT_TAKE_2_RR && pos.ProfitTaken && !pos.ProfitTaken2 {
// 		takeProfitPartial(pos, currentPrice, searchDate, PROFIT_TAKE_2_PERCENT, 2)
// 		pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice+(pos.InitialRisk*1.0))
// 		pos.ProfitTaken2 = true
// 		return false
// 	}
// 	if currentRR >= PROFIT_TAKE_3_RR && pos.ProfitTaken2 && !pos.ProfitTaken3 {
// 		takeProfitPartial(pos, currentPrice, searchDate, PROFIT_TAKE_3_PERCENT, 3)
// 		pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice+(pos.InitialRisk*2.0))
// 		pos.ProfitTaken3 = true
// 		return false
// 	}

// 	// Trailing stops
// 	if pos.TrailingStopMode {
// 		ma20 := calculate20DayMA(pos.Symbol, searchDate)
// 		pctFromEntry := (currentPrice - pos.EntryPrice) / pos.EntryPrice

// 		var newStop float64
// 		switch {
// 		case pctFromEntry > 0.20:
// 			if ma20 > 0 {
// 				newStop = math.Max(ma20*0.94, currentPrice*0.94)
// 			} else {
// 				newStop = currentPrice * 0.94
// 			}
// 			newStop = math.Max(newStop, pos.EntryPrice)
// 			if newStop > pos.StopLoss {
// 				pos.StopLoss = newStop
// 				log.Printf("[%s] 📈 Wide trailing (%.1f%% gain): $%.2f", pos.Symbol, pctFromEntry*100, pos.StopLoss)
// 			}
// 		case pctFromEntry > 0.10:
// 			if ma20 > 0 {
// 				newStop = ma20 * 0.95
// 			} else {
// 				newStop = currentPrice * 0.95
// 			}
// 			newStop = math.Max(newStop, pos.EntryPrice)
// 			if newStop > pos.StopLoss {
// 				pos.StopLoss = newStop
// 				log.Printf("[%s] 📈 Medium trailing (%.1f%% gain): $%.2f", pos.Symbol, pctFromEntry*100, pos.StopLoss)
// 			}
// 		case pctFromEntry > 0.05:
// 			if ma20 > 0 {
// 				newStop = math.Max(ma20*0.96, pos.EntryPrice)
// 				if newStop > pos.StopLoss {
// 					pos.StopLoss = newStop
// 					log.Printf("[%s] 📈 Tight trailing: $%.2f", pos.Symbol, pos.StopLoss)
// 				}
// 			}
// 		}

// 		if pctFromEntry < 0.10 && ma20 > 0 && currentPrice < ma20*0.96 {
// 			exitPrice := math.Max(pos.StopLoss, pos.EntryPrice)
// 			exitPosition(pos, exitPrice, searchDate, "Closed below 20-MA")
// 			return true
// 		}
// 	}

// 	return false
// }

// // ---------------------------------------------------------------------------
// // Profit / exit helpers (unchanged)
// // ---------------------------------------------------------------------------

// func takeStrongEPProfit(pos *Position, currentPrice float64, date time.Time) {
// 	sharesToSell := math.Floor(pos.Shares * STRONG_EP_TAKE_PERCENT)
// 	proceeds := sharesToSell * currentPrice
// 	profit := (currentPrice - pos.EntryPrice) * sharesToSell

// 	fmt.Printf("\n[%s] 🚀 STRONG EP! Taking %.0f%% profit on %s\n",
// 		pos.Symbol, STRONG_EP_TAKE_PERCENT*100, date.Format("2006-01-02"))
// 	fmt.Printf("    Selling %.0f shares @ $%.2f = $%.2f (Profit: $%.2f)\n",
// 		sharesToSell, currentPrice, proceeds, profit)

// 	updateAccountSize(proceeds)
// 	pos.CumulativeProfit += profit

// 	recordTradeResult(TradeResult{
// 		Symbol: pos.Symbol, EntryPrice: pos.EntryPrice, ExitPrice: currentPrice,
// 		Shares: sharesToSell, InitialRisk: pos.InitialRisk, ProfitLoss: profit,
// 		RiskReward: (currentPrice - pos.EntryPrice) / pos.InitialRisk,
// 		EntryDate:  pos.PurchaseDate.Format("2006-01-02"), ExitDate: date.Format("2006-01-02"),
// 		ExitReason: fmt.Sprintf("Strong EP - %.0f%% sold", STRONG_EP_TAKE_PERCENT*100),
// 		IsWinner:   true, AccountSize: getAccountSize_Safe(),
// 	})

// 	pos.Shares = pos.Shares - sharesToSell
// 	pos.StopLoss = math.Max(pos.EntryPrice, pos.StopLoss)
// 	pos.Status = "MONITORING (Strong EP)"
// 	pos.ProfitTaken = true
// 	pos.TrailingStopMode = true
// 	log.Printf("[%s] ✅ Stop at BE: $%.2f | %.0f shares remaining | Cumulative: $%.2f",
// 		pos.Symbol, pos.StopLoss, pos.Shares, pos.CumulativeProfit)
// }

// func takeProfitPartial(pos *Position, currentPrice float64, date time.Time, percent float64, level int) {
// 	sharesToSell := math.Floor(pos.Shares * percent)
// 	proceeds := sharesToSell * currentPrice
// 	profit := (currentPrice - pos.EntryPrice) * sharesToSell
// 	rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk

// 	fmt.Printf("\n[%s] 🎯 PROFIT LEVEL %d (%.2fR) on %s | Selling %.0f%% (%.0f shares) @ $%.2f = $%.2f (Profit: $%.2f)\n",
// 		pos.Symbol, level, rr, date.Format("2006-01-02"), percent*100, sharesToSell, currentPrice, proceeds, profit)

// 	updateAccountSize(proceeds)
// 	pos.CumulativeProfit += profit

// 	recordTradeResult(TradeResult{
// 		Symbol: pos.Symbol, EntryPrice: pos.EntryPrice, ExitPrice: currentPrice,
// 		Shares: sharesToSell, InitialRisk: pos.InitialRisk, ProfitLoss: profit,
// 		RiskReward: rr, EntryDate: pos.PurchaseDate.Format("2006-01-02"),
// 		ExitDate:   date.Format("2006-01-02"),
// 		ExitReason: fmt.Sprintf("Profit Level %d at %.2fR", level, rr),
// 		IsWinner:   true, AccountSize: getAccountSize_Safe(),
// 	})

// 	pos.Shares = pos.Shares - sharesToSell
// 	pos.Status = fmt.Sprintf("MONITORING (Lvl %d)", level)
// 	log.Printf("[%s] ✅ %.0f shares remaining | Cumulative: $%.2f | Stop: $%.2f",
// 		pos.Symbol, pos.Shares, pos.CumulativeProfit, pos.StopLoss)
// }

// func stopOut(pos *Position, exitPrice float64, date time.Time) {
// 	proceeds := exitPrice * pos.Shares
// 	pl := proceeds - pos.EntryPrice*pos.Shares
// 	totalPL := pl + pos.CumulativeProfit
// 	isWinner := totalPL > 0
// 	reason := "Stop Loss Hit"

// 	if isWinner && pos.ProfitTaken {
// 		reason = "Trailing Stop Hit (Profit Protected)"
// 		fmt.Printf("\n[%s] 📊 TRAILING STOP HIT on %s @ $%.2f | Remaining P/L: $%.2f | Total P/L: $%.2f\n",
// 			pos.Symbol, date.Format("2006-01-02"), exitPrice, pl, totalPL)
// 	} else {
// 		fmt.Printf("\n[%s] 🛑 STOPPED OUT on %s @ $%.2f | Loss: $%.2f\n",
// 			pos.Symbol, date.Format("2006-01-02"), exitPrice, pl)
// 	}

// 	updateAccountSize(proceeds)

// 	detail := reason
// 	if pos.CumulativeProfit > 0 {
// 		detail = fmt.Sprintf("%s (Previous partial profits: $%.2f)", reason, pos.CumulativeProfit)
// 	}

// 	recordTradeResult(TradeResult{
// 		Symbol: pos.Symbol, EntryPrice: pos.EntryPrice, ExitPrice: exitPrice,
// 		Shares: pos.Shares, InitialRisk: pos.InitialRisk, ProfitLoss: pl,
// 		RiskReward: (exitPrice - pos.EntryPrice) / pos.InitialRisk,
// 		EntryDate:  pos.PurchaseDate.Format("2006-01-02"), ExitDate: date.Format("2006-01-02"),
// 		ExitReason: detail, IsWinner: isWinner, AccountSize: getAccountSize_Safe(),
// 	})

// 	removeFromWatchlist(pos.Symbol)
// 	printCurrentStats()
// }

// func exitPosition(pos *Position, currentPrice float64, date time.Time, reason string) {
// 	proceeds := currentPrice * pos.Shares
// 	pl := proceeds - pos.EntryPrice*pos.Shares
// 	totalPL := pl + pos.CumulativeProfit

// 	fmt.Printf("\n[%s] 📤 EXITING on %s @ $%.2f | Reason: %s | Remaining P/L: $%.2f | Total P/L: $%.2f\n",
// 		pos.Symbol, date.Format("2006-01-02"), currentPrice, reason, pl, totalPL)

// 	updateAccountSize(proceeds)

// 	detail := reason
// 	if pos.CumulativeProfit > 0 {
// 		detail = fmt.Sprintf("%s (Previous partial profits: $%.2f)", reason, pos.CumulativeProfit)
// 	}

// 	recordTradeResult(TradeResult{
// 		Symbol: pos.Symbol, EntryPrice: pos.EntryPrice, ExitPrice: currentPrice,
// 		Shares: pos.Shares, InitialRisk: pos.InitialRisk, ProfitLoss: pl,
// 		RiskReward: (currentPrice - pos.EntryPrice) / pos.InitialRisk,
// 		EntryDate:  pos.PurchaseDate.Format("2006-01-02"), ExitDate: date.Format("2006-01-02"),
// 		ExitReason: detail, IsWinner: totalPL > 0, AccountSize: getAccountSize_Safe(),
// 	})

// 	removeFromWatchlist(pos.Symbol)
// 	printCurrentStats()
// }

// // ---------------------------------------------------------------------------
// // CSV / stats helpers (unchanged)
// // ---------------------------------------------------------------------------

// func parsePosition(row []string) *Position {
// 	if len(row) < 6 {
// 		return nil
// 	}
// 	entryPrice, e1 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[1], "$")), 64)
// 	stopLoss, e2 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[2], "$")), 64)
// 	shares, e3 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[3], "$")), 64)
// 	initialRisk, e4 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[4], "$")), 64)
// 	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
// 		log.Println("Skipping row with invalid data:", row)
// 		return nil
// 	}
// 	purchaseDate, err := time.Parse("2006-01-02", strings.TrimSpace(row[5]))
// 	if err != nil {
// 		log.Printf("Error parsing date for %s: %v", row[0], err)
// 		return nil
// 	}
// 	return &Position{
// 		Symbol:       strings.TrimSpace(row[0]),
// 		EntryPrice:   entryPrice,
// 		StopLoss:     stopLoss,
// 		Shares:       shares,
// 		PurchaseDate: purchaseDate,
// 		InitialRisk:  initialRisk,
// 		Status:       "HOLDING",
// 	}
// }

// func initializeTradeResultsFile() {
// 	filename := "trade_results.csv"
// 	if _, err := os.Stat(filename); os.IsNotExist(err) {
// 		f, err := os.Create(filename)
// 		if err != nil {
// 			return
// 		}
// 		defer f.Close()
// 		w := csv.NewWriter(f)
// 		w.Write([]string{"Symbol", "EntryPrice", "ExitPrice", "Shares", "InitialRisk",
// 			"ProfitLoss", "RiskReward", "EntryDate", "ExitDate", "ExitReason", "IsWinner", "AccountSize"})
// 		w.Flush()
// 	}
// }

// func recordTradeResult(result TradeResult) {
// 	tradeResultsMu.Lock()
// 	defer tradeResultsMu.Unlock()

// 	f, err := os.OpenFile("trade_results.csv", os.O_APPEND|os.O_WRONLY, 0644)
// 	if err != nil {
// 		return
// 	}
// 	defer f.Close()

// 	w := csv.NewWriter(f)
// 	w.Write([]string{
// 		result.Symbol,
// 		fmt.Sprintf("%.2f", result.EntryPrice),
// 		fmt.Sprintf("%.2f", result.ExitPrice),
// 		fmt.Sprintf("%.2f", result.Shares),
// 		fmt.Sprintf("%.2f", result.InitialRisk),
// 		fmt.Sprintf("%.2f", result.ProfitLoss),
// 		fmt.Sprintf("%.2f", result.RiskReward),
// 		result.EntryDate,
// 		result.ExitDate,
// 		result.ExitReason,
// 		fmt.Sprintf("%t", result.IsWinner),
// 		fmt.Sprintf("%.2f", result.AccountSize),
// 	})
// 	w.Flush()
// 	log.Printf("[%s] ✍️  Trade recorded: P/L: $%.2f, R/R: %.2f, Account: $%.2f",
// 		result.Symbol, result.ProfitLoss, result.RiskReward, result.AccountSize)
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

// 	total := len(records) - 1
// 	winners, losers := 0, 0
// 	totalPL, winRR, lossRR := 0.0, 0.0, 0.0

// 	for _, row := range records[1:] {
// 		pl, _ := strconv.ParseFloat(row[5], 64)
// 		rr, _ := strconv.ParseFloat(row[6], 64)
// 		totalPL += pl
// 		if row[10] == "true" {
// 			winners++
// 			winRR += rr
// 		} else {
// 			losers++
// 			lossRR += rr
// 		}
// 	}

// 	winRate := float64(winners) / float64(total) * 100
// 	avgWin, avgLoss := 0.0, 0.0
// 	if winners > 0 {
// 		avgWin = winRR / float64(winners)
// 	}
// 	if losers > 0 {
// 		avgLoss = lossRR / float64(losers)
// 	}

// 	initial := 10000.0
// 	current := getAccountSize_Safe()
// 	ret := (current - initial) / initial * 100

// 	fmt.Println("\n" + strings.Repeat("=", 70))
// 	fmt.Println("📈 CURRENT STATISTICS")
// 	fmt.Println(strings.Repeat("=", 70))
// 	fmt.Printf("Total Trades: %d | Winners: %d (%.1f%%) | Losers: %d (%.1f%%)\n",
// 		total, winners, winRate, losers, 100-winRate)
// 	fmt.Printf("Avg Win R/R: %.2fR | Avg Loss R/R: %.2fR\n", avgWin, avgLoss)
// 	fmt.Printf("Total P/L: $%.2f | Return: %.2f%%\n", totalPL, ret)
// 	fmt.Printf("Starting: $%.2f | Current: $%.2f\n", initial, current)
// 	fmt.Println(strings.Repeat("=", 70) + "\n")
// }
