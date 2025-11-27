package main

import (
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
	"sync"
	"time"

	"github.com/joho/godotenv"
)

const (
	MARKETSTACK_BASE_URL = "http://api.marketstack.com/v2"
	POLYGON_BASE_URL     = "https://api.polygon.io/v2"
	CSV_CHECK_INTERVAL   = 10 * time.Second
	SIMULATION_MODE      = true
	
	MAX_STOP_DISTANCE_MULTIPLIER = 1.5
	ATR_PERIOD                   = 14
	
	PROFIT_TAKE_MIN_RR    = 2.0
	PROFIT_TAKE_MAX_RR    = 4.0
	PROFIT_TAKE_PERCENT   = 0.40
	STRONG_EP_GAIN        = 1.00
	STRONG_EP_DAYS        = 3
	STRONG_EP_TAKE_PERCENT = 0.75
	
	MA_PERIOD = 20
	
	WEAK_CLOSE_THRESHOLD = 0.30
	
	MAX_DAYS_NO_FOLLOWTHROUGH = 5
)

type MarketstackEODResponse struct {
	Data       []MarketstackEODData `json:"data"`
	Pagination struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Count  int `json:"count"`
		Total  int `json:"total"`
	} `json:"pagination"`
}

type MarketstackEODData struct {
	Symbol string  `json:"symbol"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
	Date   string  `json:"date"`
}

type PolygonIntradayResponse struct {
	Results []PolygonBar `json:"results"`
	Status  string       `json:"status"`
}

type PolygonBar struct {
	Close  float64 `json:"c"`
	High   float64 `json:"h"`
	Low    float64 `json:"l"`
	Open   float64 `json:"o"`
	Time   int64   `json:"t"`
	Volume float64 `json:"v"`
}

type Position struct {
	Symbol           string
	EntryPrice       float64
	StopLoss         float64
	Shares           float64
	PurchaseDate     time.Time
	Status           string
	ProfitTaken      bool
	DaysHeld         int
	LastCheckDate    time.Time
	InitialStopLoss  float64
	InitialRisk      float64
	EPDayLow         float64
	EPDayHigh        float64
	HighestPrice     float64
	TrailingStopMode bool
	mu               sync.Mutex
}

type TradeResult struct {
	Symbol       string
	EntryPrice   float64
	ExitPrice    float64
	Shares       float64
	InitialRisk  float64
	ProfitLoss   float64
	RiskReward   float64
	EntryDate    string
	ExitDate     string
	ExitReason   string
	IsWinner     bool
	AccountSize  float64
}

var (
	activePositions  = make(map[string]*Position)
	processedSymbols = make(map[string]bool)
	accountSize      float64
	riskPerTrade     float64
	apiToken         string
	polygonToken     string
	lastModTime      time.Time
	historicalCache  = make(map[string][]MarketstackEODData)
	watchlistPath    string
	currentSimDate   time.Time
	
	accountMu       sync.Mutex
	positionsMu     sync.RWMutex
	processedMu     sync.Mutex
	tradeResultsMu  sync.Mutex
	cacheMu         sync.RWMutex
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, checking for environment variable")
	}

	apiToken = os.Getenv("MARKETSTACK_TOKEN")
	if apiToken == "" {
		log.Fatal("MARKETSTACK_TOKEN not found in environment or .env file")
	}

	polygonToken = os.Getenv("POLYGON_KEY")
	if polygonToken == "" {
		log.Println("⚠️  POLYGON_TOKEN not found, intraday weak close detection disabled")
	}

	accountSize = getAccountSize()
	riskPerTrade = getRiskPerTrade()
	log.Printf("Starting Account Size: $%.2f", accountSize)
	log.Printf("Risk Per Trade: %.2f%%", riskPerTrade*100)

	initializeTradeResultsFile()

	if SIMULATION_MODE {
		log.Println("⚡ Starting FAST BACKTEST mode... Watching for CSV updates")
	} else {
		log.Println("🕐 Starting REAL-TIME mode... Watching for CSV updates")
	}
	log.Println("Press Ctrl+C to stop")

	for {
		checkAndProcessWatchlist()

		if SIMULATION_MODE {
			runFullSimulation()
		} else {
			simulateActivePositions()
		}

		time.Sleep(CSV_CHECK_INTERVAL)
	}
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
	accountMu.Lock()
	defer accountMu.Unlock()
	accountSize += amount
	log.Printf("💰 Account balance: $%.2f (change: $%.2f)", accountSize, amount)
}

func getAccountSize_Safe() float64 {
	accountMu.Lock()
	defer accountMu.Unlock()
	return accountSize
}

func getTotalAccountValue() float64 {
	accountMu.Lock()
	cashBalance := accountSize
	accountMu.Unlock()
	
	// Add the current value of all open positions
	positionsMu.RLock()
	defer positionsMu.RUnlock()
	
	totalPositionValue := 0.0
	for _, pos := range activePositions {
		// Use entry price as current value (we'll update with actual prices in real-time)
		positionValue := pos.EntryPrice * pos.Shares
		totalPositionValue += positionValue
	}
	
	return cashBalance + totalPositionValue
}

func checkAndProcessWatchlist() {
	filePath := "watchlist.csv"
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		filePath = "cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv"
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
	processedMu.Lock()
	positionsMu.RLock()
	for i := 1; i < len(records); i++ {
		pos := parsePosition(records[i])
		if pos != nil && !processedSymbols[pos.Symbol] && activePositions[pos.Symbol] == nil {
			positions = append(positions, pos)
		}
	}
	positionsMu.RUnlock()
	processedMu.Unlock()

	sort.Slice(positions, func(i, j int) bool {
		return positions[i].PurchaseDate.Before(positions[j].PurchaseDate)
	})

	// Group positions by date
	dateGroups := make(map[string][]*Position)
	for _, pos := range positions {
		dateKey := pos.PurchaseDate.Format("2006-01-02")
		dateGroups[dateKey] = append(dateGroups[dateKey], pos)
	}

	// Process each date group
	for _, datePositions := range dateGroups {
		if len(datePositions) == 0 {
			continue
		}

		if !shouldStartPositionGroup(datePositions[0]) {
			continue
		}

		// SEQUENTIAL: Calculate shares for all positions based on current account
		scaledPositions := scalePositionsToFit(datePositions)

		// SEQUENTIAL: Start all positions (subtract from account)
		var validPositions []*Position
		for _, pos := range scaledPositions {
			if startPosition(pos) {
				validPositions = append(validPositions, pos)
			}
		}

		// CONCURRENT: Simulate all positions concurrently
		if len(validPositions) > 0 {
			log.Printf("🚀 Starting concurrent simulation for %d positions on %s", 
				len(validPositions), validPositions[0].PurchaseDate.Format("2006-01-02"))
		}
	}
}

func shouldStartPositionGroup(newPos *Position) bool {
	positionsMu.RLock()
	defer positionsMu.RUnlock()
	
	if len(activePositions) == 0 {
		return true
	}

	for _, activePos := range activePositions {
		daysDiff := int(newPos.PurchaseDate.Sub(activePos.PurchaseDate).Hours() / 24)
		if daysDiff >= -1 && daysDiff <= 1 {
			return true
		}
	}

	return false
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

func scalePositionsToFit(positions []*Position) []*Position {
	recalculatedPositions := make([]*Position, len(positions))
	
	// Use TOTAL account value (cash + existing positions) for share calculation
	totalAccountValue := getTotalAccountValue()
	
	for i, pos := range positions {
		recalculatedPos := *pos
		recalculatedPositions[i] = recalculateShares(&recalculatedPos, totalAccountValue)
	}
	
	return recalculatedPositions
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
	cacheMu.RLock()
	_, exists := historicalCache[symbol]
	cacheMu.RUnlock()
	
	if exists {
		return true
	}

	fromDate := startDate.AddDate(0, 0, -30)
	toDate := time.Now()

	log.Printf("[%s] 📥 Fetching ALL historical data from %s to %s...", 
		symbol, fromDate.Format("2006-01-02"), toDate.Format("2006-01-02"))

	allData := []MarketstackEODData{}
	offset := 0
	limit := 1000

	for {
		url := fmt.Sprintf("%s/eod?access_key=%s&symbols=%s&date_from=%s&date_to=%s&limit=%d&offset=%d",
			MARKETSTACK_BASE_URL, apiToken, symbol,
			fromDate.Format("2006-01-02"),
			toDate.Format("2006-01-02"),
			limit, offset)

		resp, err := http.Get(url)
		if err != nil {
			log.Printf("[%s] ❌ Error fetching historical data: %v", symbol, err)
			return false
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Printf("[%s] ❌ Error reading response: %v", symbol, err)
			return false
		}

		var result MarketstackEODResponse
		if err := json.Unmarshal(body, &result); err != nil {
			log.Printf("[%s] ❌ Error parsing historical data: %v", symbol, err)
			return false
		}

		allData = append(allData, result.Data...)

		if len(result.Data) < limit || result.Pagination.Offset+result.Pagination.Count >= result.Pagination.Total {
			break
		}

		offset += limit
		time.Sleep(100 * time.Millisecond)
	}

	sort.Slice(allData, func(i, j int) bool {
		dateI, _ := time.Parse("2006-01-02T15:04:05-0700", allData[i].Date)
		dateJ, _ := time.Parse("2006-01-02T15:04:05-0700", allData[j].Date)
		return dateI.Before(dateJ)
	})

	cacheMu.Lock()
	historicalCache[symbol] = allData
	cacheMu.Unlock()
	
	log.Printf("[%s] ✅ Cached %d days of historical data", symbol, len(allData))
	return true
}

func checkWeakCloseIntraday(symbol string, date time.Time) (bool, float64) {
	if polygonToken == "" {
		return false, 0
	}

	dateStr := date.Format("2006-01-02")
	url := fmt.Sprintf("%s/aggs/ticker/%s/range/5/minute/%s/%s?adjusted=true&sort=asc&apiKey=%s",
		POLYGON_BASE_URL, symbol, dateStr, dateStr, polygonToken)

	resp, err := http.Get(url)
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

	var result PolygonIntradayResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[%s] ⚠️  Error parsing intraday data: %v", symbol, err)
		return false, 0
	}

	if result.Status != "OK" || len(result.Results) == 0 {
		return false, 0
	}

	dayHigh := 0.0
	var weakClosePrice float64
	// var weakCloseTime time.Time

	for i, bar := range result.Results {
		if bar.High > dayHigh {
			dayHigh = bar.High
		}

		barTime := time.Unix(bar.Time/1000, 0)
		if i > 0 && dayHigh > 0 {
			closeFromHigh := (dayHigh - bar.Close) / dayHigh
			if closeFromHigh >= WEAK_CLOSE_THRESHOLD {
				weakClosePrice = bar.Close
				// weakCloseTime = barTime
				log.Printf("[%s] ⚠️  WEAK CLOSE detected at %s | Close: $%.2f (%.1f%% from high $%.2f)",
					symbol, barTime.Format("15:04"), bar.Close, closeFromHigh*100, dayHigh)
				return true, weakClosePrice
			}
		}
	}

	return false, 0
}

func calculateATR(symbol string, endDate time.Time, period int) float64 {
	cacheMu.RLock()
	historicalData, exists := historicalCache[symbol]
	cacheMu.RUnlock()
	
	if !exists || len(historicalData) < period {
		return 0
	}

	var trueRanges []float64
	var endIdx int

	for i := range historicalData {
		dataDate, _ := time.Parse("2006-01-02T15:04:05-0700", historicalData[i].Date)
		if !dataDate.After(endDate) {
			endIdx = i
		}
	}

	for i := endIdx - period + 1; i <= endIdx && i < len(historicalData); i++ {
		if i <= 0 {
			continue
		}
		current := historicalData[i]
		previous := historicalData[i-1]

		highLow := current.High - current.Low
		highClose := math.Abs(current.High - previous.Close)
		lowClose := math.Abs(current.Low - previous.Close)

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

func startPosition(pos *Position) bool {
	log.Printf("[%s] 📥 Fetching historical data...", pos.Symbol)
	
	if !fetchAllHistoricalData(pos.Symbol, pos.PurchaseDate) {
		log.Printf("[%s] ⚠️  Cannot start position - failed to fetch historical data", pos.Symbol)
		processedMu.Lock()
		processedSymbols[pos.Symbol] = true
		processedMu.Unlock()
		return false
	}

	cacheMu.RLock()
	historicalData := historicalCache[pos.Symbol]
	cacheMu.RUnlock()

	var epDayData *MarketstackEODData
	for i := range historicalData {
		dataDate, _ := time.Parse("2006-01-02T15:04:05-0700", historicalData[i].Date)
		if dataDate.Format("2006-01-02") == pos.PurchaseDate.Format("2006-01-02") {
			epDayData = &historicalData[i]
			break
		}
	}

	if epDayData == nil {
		log.Printf("[%s] ⚠️  Cannot find EP day data", pos.Symbol)
		processedMu.Lock()
		processedSymbols[pos.Symbol] = true
		processedMu.Unlock()
		return false
	}

	pos.EPDayLow = epDayData.Low
	pos.EPDayHigh = epDayData.High
	
	// Calculate ATR for validation
	atr := calculateATR(pos.Symbol, pos.PurchaseDate, ATR_PERIOD)
	
	// Use the stop loss from watchlist if provided, otherwise calculate
	var finalStop float64
	if pos.StopLoss > 0 && pos.StopLoss < pos.EntryPrice {
		// User provided a stop loss in the watchlist, use it
		finalStop = pos.StopLoss
		log.Printf("[%s] ℹ️  Using watchlist stop loss: $%.2f", pos.Symbol, finalStop)
	} else {
		// Calculate stop just below EP day low
		suggestedStop := pos.EPDayLow * 0.99

		if atr > 0 {
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

	// Use TOTAL account value (cash + existing positions) for share calculation
	totalAccountValue := getTotalAccountValue()
	pos = recalculateShares(pos, totalAccountValue)

	riskPerShare := pos.InitialRisk
	if riskPerShare <= 0 {
		log.Printf("[%s] ⚠️  Invalid risk calculation. Entry: $%.2f, Stop: $%.2f",
			pos.Symbol, pos.EntryPrice, pos.StopLoss)
		processedMu.Lock()
		processedSymbols[pos.Symbol] = true
		processedMu.Unlock()
		return false
	}

	positionCost := pos.EntryPrice * pos.Shares
	currentCash := getAccountSize_Safe()

	if positionCost > currentCash {
		log.Printf("[%s] ⚠️  Insufficient funds. Need $%.2f, have $%.2f. Skipping.",
			pos.Symbol, positionCost, currentCash)
		processedMu.Lock()
		processedSymbols[pos.Symbol] = true
		processedMu.Unlock()
		return false
	}

	updateAccountSize(-positionCost)
	pos.LastCheckDate = pos.PurchaseDate
	
	positionsMu.Lock()
	activePositions[pos.Symbol] = pos
	positionsMu.Unlock()
	
	processedMu.Lock()
	processedSymbols[pos.Symbol] = true
	processedMu.Unlock()

	totalAccountValue = getTotalAccountValue()
	dollarRisk := totalAccountValue * riskPerTrade
	log.Printf("[%s] 🟢 OPENED POSITION | Shares: %.0f @ $%.2f | Total: $%.2f | Stop: $%.2f | Risk: $%.2f (%.2f%%) | ATR: $%.2f | Date: %s",
		pos.Symbol, pos.Shares, pos.EntryPrice, positionCost, pos.StopLoss, dollarRisk, (pos.InitialRisk/pos.EntryPrice)*100,
		atr, pos.PurchaseDate.Format("2006-01-02"))
	log.Printf("[%s] ℹ️  Cash: $%.2f | Total Account Value: $%.2f | Initial stop allows risk below entry.", 
		pos.Symbol, currentCash-positionCost, totalAccountValue)
	
	hasWeakClose, weakClosePrice := checkWeakCloseIntraday(pos.Symbol, pos.PurchaseDate)
	if hasWeakClose && weakClosePrice > 0 {
		log.Printf("[%s] ⚠️  Weak close detected on EP day, closing position @ $%.2f", pos.Symbol, weakClosePrice)
		
		pos.mu.Lock()
		totalProceeds := weakClosePrice * pos.Shares
		costBasis := pos.EntryPrice * pos.Shares
		profitLoss := totalProceeds - costBasis
		pos.mu.Unlock()

		updateAccountSize(totalProceeds)

		recordTradeResult(TradeResult{
			Symbol:      pos.Symbol,
			EntryPrice:  pos.EntryPrice,
			ExitPrice:   weakClosePrice,
			Shares:      pos.Shares,
			InitialRisk: pos.InitialRisk,
			ProfitLoss:  profitLoss,
			RiskReward:  (weakClosePrice - pos.EntryPrice) / pos.InitialRisk,
			EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
			ExitDate:    pos.PurchaseDate.Format("2006-01-02"),
			ExitReason:  "Weak Close on EP Day",
			IsWinner:    profitLoss > 0,
			AccountSize: getAccountSize_Safe(),
		})

		removeFromWatchlist(pos.Symbol)
		
		positionsMu.Lock()
		delete(activePositions, pos.Symbol)
		positionsMu.Unlock()
		
		return false
	}
	
	return true
}

func calculate20DayMA(symbol string, currentDate time.Time) float64 {
	cacheMu.RLock()
	historicalData, exists := historicalCache[symbol]
	cacheMu.RUnlock()
	
	if !exists {
		return 0
	}

	var prices []float64
	for i := range historicalData {
		dataDate, _ := time.Parse("2006-01-02T15:04:05-0700", historicalData[i].Date)
		if dataDate.Before(currentDate) || dataDate.Equal(currentDate) {
			prices = append(prices, historicalData[i].Close)
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

func runFullSimulation() {
	positionsMu.RLock()
	posCount := len(activePositions)
	positionsMu.RUnlock()
	
	if posCount == 0 {
		return
	}

	log.Printf("⚡ Fast simulating %d position(s) concurrently...", posCount)

	positionsMu.RLock()
	currentSimDate = time.Time{}
	for _, pos := range activePositions {
		if currentSimDate.IsZero() || pos.PurchaseDate.Before(currentSimDate) {
			currentSimDate = pos.PurchaseDate
		}
	}
	positionsMu.RUnlock()

	maxIterations := 10000
	iterations := 0

	for {
		positionsMu.RLock()
		posCount = len(activePositions)
		positionsMu.RUnlock()
		
		if posCount == 0 || iterations >= maxIterations {
			break
		}
		
		iterations++

		checkAndProcessWatchlist()

		positionsMu.RLock()
		var wg sync.WaitGroup
		positionsToRemove := make(map[string]bool)
		var removeMu sync.Mutex
		
		for symbol, pos := range activePositions {
			if !pos.LastCheckDate.After(currentSimDate) || pos.LastCheckDate.Equal(currentSimDate) {
				wg.Add(1)
				go func(p *Position, sym string) {
					defer wg.Done()
					shouldRemove := simulateNextDay(p)
					if shouldRemove {
						removeMu.Lock()
						positionsToRemove[sym] = true
						removeMu.Unlock()
					}
				}(pos, symbol)
			}
		}
		positionsMu.RUnlock()

		wg.Wait()

		if len(positionsToRemove) > 0 {
			positionsMu.Lock()
			for symbol := range positionsToRemove {
				delete(activePositions, symbol)
			}
			positionsMu.Unlock()
		}

		currentSimDate = currentSimDate.AddDate(0, 0, 1)

		if iterations%100 == 0 {
			positionsMu.RLock()
			activeCount := len(activePositions)
			positionsMu.RUnlock()
			if activeCount > 0 {
				time.Sleep(50 * time.Millisecond)
			}
		}
	}

	if iterations >= maxIterations {
		log.Println("⚠️  Max iterations reached, stopping simulation")
		positionsMu.Lock()
		for symbol, pos := range activePositions {
			log.Printf("[%s] ⚠️  Force closing position at last known price", symbol)
			cacheMu.RLock()
			historicalData := historicalCache[symbol]
			cacheMu.RUnlock()
			if len(historicalData) > 0 {
				lastPrice := historicalData[len(historicalData)-1].Close
				lastDate, _ := time.Parse("2006-01-02T15:04:05-0700", historicalData[len(historicalData)-1].Date)
				exitPosition(pos, lastPrice, lastDate, "Force closed - end of data")
			}
			delete(activePositions, symbol)
		}
		positionsMu.Unlock()
	}

	log.Println("✅ All positions closed!")
}

func simulateActivePositions() {
	positionsMu.RLock()
	posCount := len(activePositions)
	positionsMu.RUnlock()
	
	if posCount == 0 {
		return
	}

	log.Printf("📊 Simulating %d active position(s)...", posCount)

	var wg sync.WaitGroup
	positionsMu.RLock()
	positionsToRemove := make(map[string]bool)
	var removeMu sync.Mutex

	for symbol, pos := range activePositions {
		wg.Add(1)
		go func(p *Position, sym string) {
			defer wg.Done()
			shouldRemove := simulateNextDay(p)
			if shouldRemove {
				removeMu.Lock()
				positionsToRemove[sym] = true
				removeMu.Unlock()
			}
		}(pos, symbol)
	}
	positionsMu.RUnlock()

	wg.Wait()

	if len(positionsToRemove) > 0 {
		positionsMu.Lock()
		for symbol := range positionsToRemove {
			delete(activePositions, symbol)
		}
		positionsMu.Unlock()
	}
}

func simulateNextDay(pos *Position) bool {
	pos.mu.Lock()
	defer pos.mu.Unlock()

	cacheMu.RLock()
	historicalData, exists := historicalCache[pos.Symbol]
	cacheMu.RUnlock()
	
	if !exists {
		log.Printf("[%s] ⚠️  No historical data available", pos.Symbol)
		return false
	}

	var todayData *MarketstackEODData
	searchDate := pos.LastCheckDate.AddDate(0, 0, 1)

	for attempts := 0; attempts < 7; attempts++ {
		for i := range historicalData {
			dataDate, _ := time.Parse("2006-01-02T15:04:05-0700", historicalData[i].Date)
			if dataDate.Format("2006-01-02") == searchDate.Format("2006-01-02") {
				todayData = &historicalData[i]
				break
			}
		}
		if todayData != nil {
			break
		}
		searchDate = searchDate.AddDate(0, 0, 1)
	}

	if todayData == nil {
		return false
	}

	pos.LastCheckDate = searchDate
	pos.DaysHeld++

	currentPrice := todayData.Close
	dayLow := todayData.Low
	dayHigh := todayData.High
	currentGain := currentPrice - pos.EntryPrice
	currentRR := currentGain / pos.InitialRisk

	if dayHigh > pos.HighestPrice {
		pos.HighestPrice = dayHigh
	}

	log.Printf("[%s] Day %d (%s) | Status: %s | Close: $%.2f | Low: $%.2f | High: $%.2f | Gain: $%.2f (%.1f%%) | R/R: %.2fR | Stop: $%.2f",
		pos.Symbol, pos.DaysHeld, searchDate.Format("2006-01-02"), pos.Status,
		currentPrice, dayLow, dayHigh, currentGain, (currentGain/pos.EntryPrice)*100, currentRR, pos.StopLoss)

	// RULE 1: Check stop loss
	// Day 1 (entry day): Check intraday low vs stop
	// Day 2+: Only check closing price vs stop (end-of-day management)
	if pos.DaysHeld == 1 {
		// Entry day: Check intraday low
		if dayLow <= pos.StopLoss {
			stopOut(pos, pos.StopLoss, searchDate)
			return true
		}
	} else {
		// Subsequent days: Only check closing price
		if currentPrice <= pos.StopLoss {
			stopOut(pos, pos.StopLoss, searchDate)
			return true
		}
	}

	// AUTOMATIC BREAKEVEN: If we're in profit and haven't taken profit yet, move stop to breakeven
	// BUT only after the first day to avoid getting stopped out too early
	if pos.DaysHeld > 3 && !pos.ProfitTaken && dayHigh > pos.EntryPrice && pos.StopLoss < pos.EntryPrice {
		percentGain := (dayHigh - pos.EntryPrice) / pos.EntryPrice
		if percentGain >= 0.03 {
			pos.StopLoss = pos.EntryPrice
			log.Printf("[%s] 🔒 MOVED TO BREAKEVEN - Up %.1f%% intraday (Day %d), protecting capital. Stop: $%.2f",
				pos.Symbol, percentGain*100, pos.DaysHeld, pos.StopLoss)
		}
	}

	// RULE 5: Tighten stop if no follow-through
	if pos.DaysHeld >= MAX_DAYS_NO_FOLLOWTHROUGH && currentPrice < pos.EntryPrice*1.05 && !pos.ProfitTaken {
		tightenedStop := pos.EntryPrice - (pos.InitialRisk * 0.5)
		tightenedStop = math.Max(tightenedStop, pos.EntryPrice)
		if tightenedStop > pos.StopLoss {
			pos.StopLoss = tightenedStop
			log.Printf("[%s] ⚠️  No follow-through after %d days. Tightening stop to $%.2f",
				pos.Symbol, MAX_DAYS_NO_FOLLOWTHROUGH, pos.StopLoss)
		}
	}

	// RULE 2: Check for strong EP
	percentGain := (currentPrice - pos.EntryPrice) / pos.EntryPrice
	if pos.DaysHeld <= STRONG_EP_DAYS && percentGain >= STRONG_EP_GAIN && !pos.ProfitTaken {
		takeStrongEPProfit(pos, currentPrice, searchDate)
		return false
	}

	// RULE 2: Take profit at 2R-4R
	if currentRR >= PROFIT_TAKE_MIN_RR && currentRR <= PROFIT_TAKE_MAX_RR && !pos.ProfitTaken {
		takeProfitPartial(pos, currentPrice, searchDate, PROFIT_TAKE_PERCENT)
		pos.TrailingStopMode = true
		// Move stop to breakeven ONLY if we're past day 3
		if pos.DaysHeld > 3 {
			pos.StopLoss = math.Max(pos.StopLoss, pos.EntryPrice)
			log.Printf("[%s] 🔒 Stop moved to breakeven after profit taking", pos.Symbol)
		}
		return false
	}

	// RULE 3: Trailing stop using 20-day MA
	if pos.TrailingStopMode {
		ma20 := calculate20DayMA(pos.Symbol, searchDate)
		
		percentGainFromEntry := (currentPrice - pos.EntryPrice) / pos.EntryPrice
		
		if percentGainFromEntry > 0.10 {
			// For gains > 10%, use loose trailing stop (4% below MA)
			var newStop float64
			if ma20 > 0 {
				// 4% below MA
				newStop = ma20 * 0.96
			} else {
				// Fallback: 8% below current price
				newStop = currentPrice * 0.92
			}
			
			// CRITICAL: Never allow stop below entry price
			newStop = math.Max(newStop, pos.EntryPrice)
			
			// Only raise the stop, never lower it
			if newStop > pos.StopLoss {
				pos.StopLoss = newStop
				log.Printf("[%s] 📈 Loose trailing stop (%.1f%% gain): $%.2f (4%% below 20-day MA)", 
					pos.Symbol, percentGainFromEntry*100, pos.StopLoss)
			}
			
			// Exit if price closes 4% below MA
			if ma20 > 0 && currentPrice < ma20 * 0.96 {
				exitPrice := math.Max(pos.StopLoss, pos.EntryPrice)
				exitPosition(pos, exitPrice, searchDate, "Closed 4%+ below 20-day MA")
				return true
			}
		} else if percentGainFromEntry > 0.05 {
			// For gains 5-10%, use moderate trailing stop (4% below MA)
			var newStop float64
			if ma20 > 0 {
				// 4% below MA
				newStop = ma20 * 0.96
			} else {
				// Fallback: 6% below current price
				newStop = currentPrice * 0.94
			}
			
			// CRITICAL: Never allow stop below entry price
			newStop = math.Max(newStop, pos.EntryPrice)
			
			// Only raise the stop, never lower it
			if newStop > pos.StopLoss {
				pos.StopLoss = newStop
				log.Printf("[%s] 📈 Moderate trailing stop (%.1f%% gain): $%.2f (4%% below 20-day MA)", 
					pos.Symbol, percentGainFromEntry*100, pos.StopLoss)
			}
			
			// Exit if closes 4% below MA
			if ma20 > 0 && currentPrice < ma20 * 0.96 {
				exitPrice := math.Max(pos.StopLoss, pos.EntryPrice)
				exitPosition(pos, exitPrice, searchDate, "Closed 4%+ below 20-day MA")
				return true
			}
		} else {
			// For gains < 5%, use tight trailing stop (4% below MA or at breakeven)
			if ma20 > 0 && currentPrice < ma20 * 0.96 {
				exitPrice := math.Max(pos.StopLoss, pos.EntryPrice)
				exitPosition(pos, exitPrice, searchDate, "Closed 4%+ below 20-day MA")
				return true
			}

			// Update stop to 4% below MA if it's above entry price and above current stop
			if ma20 > 0 {
				newStop := ma20 * 0.96
				if newStop > pos.StopLoss && newStop >= pos.EntryPrice {
					pos.StopLoss = newStop
					log.Printf("[%s] 📈 Tight trailing stop updated: $%.2f (4%% below 20-day MA)", pos.Symbol, pos.StopLoss)
				} else if pos.StopLoss < pos.EntryPrice {
					// If somehow stop is below entry, fix it immediately
					pos.StopLoss = pos.EntryPrice
					log.Printf("[%s] 🔒 Stop corrected to breakeven: $%.2f", pos.Symbol, pos.StopLoss)
				}
			}
		}
	}

	return false
}

func parsePosition(row []string) *Position {
	if len(row) < 6 {
		return nil
	}

	entryPrice, err1 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[1], "$")), 64)
	stopLoss, err2 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[2], "$")), 64)
	shares, err3 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[3], "$")), 64)
	initialRisk, err4 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[4], "$")), 64)

	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		log.Println("Skipping row with invalid data:", row)
		return nil
	}

	purchaseDate, err := time.Parse("2006-01-02", strings.TrimSpace(row[5]))
	if err != nil {
		log.Printf("Error parsing date for %s: %v", row[0], err)
		return nil
	}

	return &Position{
		Symbol:       strings.TrimSpace(row[0]),
		EntryPrice:   entryPrice,
		StopLoss:     stopLoss,
		Shares:       shares,
		PurchaseDate: purchaseDate,
		InitialRisk:  initialRisk,
		Status:       "HOLDING",
		ProfitTaken:  false,
		DaysHeld:     0,
	}
}

func takeStrongEPProfit(pos *Position, currentPrice float64, date time.Time) {
	sharesToSell := math.Floor(pos.Shares * STRONG_EP_TAKE_PERCENT)
	saleProceeds := sharesToSell * currentPrice
	remainingShares := pos.Shares - sharesToSell
	profit := (currentPrice - pos.EntryPrice) * sharesToSell

	fmt.Printf("\n[%s] 🚀 STRONG EP DETECTED! Taking %.0f%% profit on %s\n",
		pos.Symbol, STRONG_EP_TAKE_PERCENT*100, date.Format("2006-01-02"))
	fmt.Printf("    Selling %.0f shares @ $%.2f = $%.2f (Profit: $%.2f)\n",
		sharesToSell, currentPrice, saleProceeds, profit)

	updateAccountSize(saleProceeds)

	pos.Shares = remainingShares
	pos.StopLoss = math.Max(pos.EntryPrice, pos.StopLoss)
	pos.Status = "MONITORING (Strong EP)"
	pos.ProfitTaken = true
	pos.TrailingStopMode = true

	log.Printf("[%s] ✅ Stop moved to breakeven @ $%.2f | %.0f shares remaining\n",
		pos.Symbol, pos.StopLoss, remainingShares)
}

func takeProfitPartial(pos *Position, currentPrice float64, date time.Time, percent float64) {
	sharesToSell := math.Floor(pos.Shares * percent)
	saleProceeds := sharesToSell * currentPrice
	remainingShares := pos.Shares - sharesToSell
	profit := (currentPrice - pos.EntryPrice) * sharesToSell
	rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk

	fmt.Printf("\n[%s] 🎯 TAKING PROFIT (%.2fR) on %s | Selling %.0f%% (%.0f shares) @ $%.2f = $%.2f (Profit: $%.2f)\n",
		pos.Symbol, rr, date.Format("2006-01-02"), percent*100, sharesToSell, currentPrice, saleProceeds, profit)

	updateAccountSize(saleProceeds)

	pos.Shares = remainingShares
	pos.StopLoss = math.Max(pos.EntryPrice, pos.StopLoss)
	pos.Status = "MONITORING"
	pos.ProfitTaken = true

	log.Printf("[%s] ✅ Stop moved to breakeven @ $%.2f | %.0f shares remaining\n",
		pos.Symbol, pos.StopLoss, remainingShares)
}

func stopOut(pos *Position, exitPrice float64, date time.Time) {
	totalProceeds := exitPrice * pos.Shares
	costBasis := pos.EntryPrice * pos.Shares
	profitLoss := totalProceeds - costBasis

	isWinner := exitPrice >= pos.EntryPrice || profitLoss > 0
	exitReason := "Stop Loss Hit"
	
	if isWinner && pos.ProfitTaken {
		exitReason = "Trailing Stop Hit (Profit Protected)"
		fmt.Printf("\n[%s] 📊 TRAILING STOP HIT on %s @ $%.2f | Remaining Position P/L: $%.2f\n",
			pos.Symbol, date.Format("2006-01-02"), exitPrice, profitLoss)
	} else {
		fmt.Printf("\n[%s] 🛑 STOPPED OUT on %s @ $%.2f | Loss: $%.2f\n",
			pos.Symbol, date.Format("2006-01-02"), exitPrice, profitLoss)
	}

	updateAccountSize(totalProceeds)

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
		ExitReason:  exitReason,
		IsWinner:    isWinner,
		AccountSize: getAccountSize_Safe(),
	})

	removeFromWatchlist(pos.Symbol)
	printCurrentStats()
}

func exitPosition(pos *Position, currentPrice float64, date time.Time, reason string) {
	totalProceeds := currentPrice * pos.Shares
	costBasis := pos.EntryPrice * pos.Shares
	profit := totalProceeds - costBasis

	fmt.Printf("\n[%s] 📤 EXITING on %s @ $%.2f | Reason: %s | P/L: $%.2f\n",
		pos.Symbol, date.Format("2006-01-02"), currentPrice, reason, profit)

	updateAccountSize(totalProceeds)

	rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk

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
		ExitReason:  reason,
		IsWinner:    profit > 0,
		AccountSize: getAccountSize_Safe(),
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
	tradeResultsMu.Lock()
	defer tradeResultsMu.Unlock()

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

	log.Printf("[%s] ✍️  Trade recorded: P/L: $%.2f, R/R: %.2f, Account: $%.2f", result.Symbol, result.ProfitLoss, result.RiskReward, result.AccountSize)
}

func printCurrentStats() {
	tradeResultsMu.Lock()
	defer tradeResultsMu.Unlock()

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
	currentAccount := getAccountSize_Safe()
	returnPct := (currentAccount - initialAccount) / initialAccount * 100

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("📈 CURRENT STATISTICS")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("Total Trades: %d | Winners: %d (%.1f%%) | Losers: %d (%.1f%%)\n", 
		totalTrades, winners, winRate, losers, 100-winRate)
	fmt.Printf("Avg Win R/R: %.2fR | Avg Loss R/R: %.2fR\n", avgWinRR, avgLossRR)
	fmt.Printf("Total P/L: $%.2f | Return: %.2f%%\n", totalPL, returnPct)
	fmt.Printf("Starting: $%.2f | Current: $%.2f\n", initialAccount, currentAccount)
	fmt.Println(strings.Repeat("=", 70) + "\n")
}