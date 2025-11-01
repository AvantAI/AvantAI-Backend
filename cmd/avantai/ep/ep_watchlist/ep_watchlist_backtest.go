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
	"time"

	"github.com/joho/godotenv"
)

const (
	MARKETSTACK_BASE_URL = "http://api.marketstack.com/v2"
	CSV_CHECK_INTERVAL   = 10 * time.Second
	SIMULATION_MODE      = true
	
	// Stop loss configuration
	MAX_STOP_DISTANCE_MULTIPLIER = 1.5 // Max stop distance as multiple of ATR
	ATR_PERIOD                   = 14  // Period for ATR calculation
	
	// Profit taking thresholds
	PROFIT_TAKE_MIN_RR    = 2.0  // Minimum 2R before taking profit
	PROFIT_TAKE_MAX_RR    = 4.0  // Maximum 4R before taking profit
	PROFIT_TAKE_PERCENT   = 0.40 // Take 40% profit (1/3 to 1/2)
	STRONG_EP_GAIN        = 1.00 // 100% gain threshold for strong EP
	STRONG_EP_DAYS        = 3    // Days to achieve strong EP
	STRONG_EP_TAKE_PERCENT = 0.75 // Take 75% on strong EP
	
	// Trailing stop
	MA_PERIOD = 10 // 10-day moving average
	
	// Weak close threshold
	WEAK_CLOSE_THRESHOLD = 0.30 // If close is 30% below high, it's weak
	
	// Time-based exit
	MAX_DAYS_NO_FOLLOWTHROUGH = 5
)

type MarketstackEODResponse struct {
	Data []MarketstackEODData `json:"data"`
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
	lastModTime      time.Time
	historicalCache  = make(map[string][]MarketstackEODData)
	watchlistPath    string
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
	accountSize += amount
	log.Printf("💰 Account balance: $%.2f (change: $%.2f)", accountSize, amount)
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
	for i := 1; i < len(records); i++ {
		pos := parsePosition(records[i])
		if pos != nil && !processedSymbols[pos.Symbol] && activePositions[pos.Symbol] == nil {
			positions = append(positions, pos)
		}
	}

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

		// Check if any position in this group should start
		shouldStart := shouldStartPositionGroup(datePositions[0])
		if !shouldStart {
			continue
		}

		// Scale positions to fit account if needed
		scaledPositions := scalePositionsToFit(datePositions)

		// Start all positions in the group
		for _, pos := range scaledPositions {
			startPosition(pos)
		}
	}
}

func shouldStartPositionGroup(newPos *Position) bool {
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
	// Recalculate shares based on current account size and risk parameters
	// Formula: round((riskPercent * (riskPerTrade * accSize) * 1.0) / (entryPrice - stopLoss))
	
	riskPercent := pos.InitialRisk
	dollarRisk := riskPerTrade * currentAccountSize
	riskPerShare := pos.EntryPrice - pos.StopLoss
	
	if riskPerShare <= 0 {
		log.Printf("[%s] ⚠️  Invalid risk calculation, cannot recalculate shares", pos.Symbol)
		return pos
	}
	
	// Your exact formula: round((riskPercent * (riskPerTrade * accSize) * 1.0) / (entryPrice - stopLoss))
	newShares := math.Round(((riskPercent * dollarRisk * 1.0) / riskPerShare))
	
	if newShares != pos.Shares {
		log.Printf("[%s] 📊 Recalculated shares based on current account size: %.0f -> %.0f shares (Account: $%.2f, DollarRisk: $%.2f, RiskPerShare: $%.2f, RiskPercent: %.2f)",
			pos.Symbol, pos.Shares, newShares, currentAccountSize, dollarRisk, riskPerShare, riskPercent)
		pos.Shares = newShares
	}
	
	return pos
}

func scalePositionsToFit(positions []*Position) []*Position {
	// Recalculate shares for each position based on current account size
	recalculatedPositions := make([]*Position, len(positions))
	
	for i, pos := range positions {
		recalculatedPos := *pos // Create a copy
		recalculatedPositions[i] = recalculateShares(&recalculatedPos, accountSize)
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

	// Filter out the symbol
	var newRecords [][]string
	removed := false
	for i, record := range records {
		if i == 0 {
			// Keep header
			newRecords = append(newRecords, record)
		} else if len(record) > 0 && strings.TrimSpace(record[0]) != symbol {
			// Keep all other records
			newRecords = append(newRecords, record)
		} else {
			removed = true
		}
	}

	if !removed {
		return
	}

	// Write back to file
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

func calculateATR(symbol string, endDate time.Time, period int) float64 {
	historicalData, exists := historicalCache[symbol]
	if !exists || len(historicalData) < period {
		return 0
	}

	var trueRanges []float64
	var endIdx int

	// Find the end date index
	for i := range historicalData {
		dataDate, _ := time.Parse("2006-01-02T15:04:05-0700", historicalData[i].Date)
		if !dataDate.After(endDate) {
			endIdx = i
		}
	}

	// Calculate True Range for the period before entry
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

func startPosition(pos *Position) {
	log.Printf("[%s] 📥 Fetching historical data...", pos.Symbol)
	fetchHistoricalData(pos.Symbol, pos.PurchaseDate)

	historicalData, exists := historicalCache[pos.Symbol]
	if !exists {
		log.Printf("[%s] ⚠️  Cannot start position - no historical data", pos.Symbol)
		processedSymbols[pos.Symbol] = true
		return
	}

	// Find EP day data
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
		processedSymbols[pos.Symbol] = true
		return
	}

	// RULE 1: Set stop just below EP day low
	pos.EPDayLow = epDayData.Low
	pos.EPDayHigh = epDayData.High
	suggestedStop := pos.EPDayLow * 0.99 // Just below the low

	// Calculate ATR to validate stop distance
	atr := calculateATR(pos.Symbol, pos.PurchaseDate, ATR_PERIOD)
	if atr > 0 {
		maxStopDistance := atr * MAX_STOP_DISTANCE_MULTIPLIER
		actualStopDistance := pos.EntryPrice - suggestedStop

		if actualStopDistance > maxStopDistance {
			suggestedStop = pos.EntryPrice - maxStopDistance
			log.Printf("[%s] ⚠️  Stop adjusted to respect ATR limit. ATR: $%.2f, Max Distance: $%.2f",
				pos.Symbol, atr, maxStopDistance)
		}
	}

	pos.StopLoss = suggestedStop
	pos.InitialStopLoss = suggestedStop
	pos.InitialRisk = pos.EntryPrice - pos.StopLoss
	pos.HighestPrice = pos.EntryPrice

	// RULE 4: Check for weak close on EP day
	closeFromHigh := (epDayData.High - epDayData.Close) / (epDayData.High - epDayData.Low)
	if closeFromHigh > WEAK_CLOSE_THRESHOLD {
		log.Printf("[%s] ⚠️  WEAK CLOSE detected on EP day. Close: $%.2f, High: $%.2f (%.1f%% off high). SKIPPING TRADE.",
			pos.Symbol, epDayData.Close, epDayData.High, closeFromHigh*100)
		processedSymbols[pos.Symbol] = true
		return
	}

	// Recalculate shares based on CURRENT account size before purchase
	pos = recalculateShares(pos, accountSize)

	// Calculate position size with recalculated shares
	dollarRisk := accountSize * riskPerTrade
	riskPerShare := pos.InitialRisk

	if riskPerShare <= 0 {
		log.Printf("[%s] ⚠️  Invalid risk calculation. Entry: $%.2f, Stop: $%.2f",
			pos.Symbol, pos.EntryPrice, pos.StopLoss)
		processedSymbols[pos.Symbol] = true
		return
	}

	positionCost := pos.EntryPrice * pos.Shares

	if positionCost > accountSize {
		log.Printf("[%s] ⚠️  Insufficient funds. Need $%.2f, have $%.2f. Skipping.",
			pos.Symbol, positionCost, accountSize)
		processedSymbols[pos.Symbol] = true
		return
	}

	updateAccountSize(-positionCost)
	pos.LastCheckDate = pos.PurchaseDate
	activePositions[pos.Symbol] = pos
	processedSymbols[pos.Symbol] = true

	log.Printf("[%s] 🟢 OPENED POSITION | Shares: %.0f @ $%.2f | Total: $%.2f | Stop: $%.2f | Risk: $%.2f (%.2f%%) | ATR: $%.2f | Date: %s",
		pos.Symbol, pos.Shares, pos.EntryPrice, positionCost, pos.StopLoss, dollarRisk, (pos.InitialRisk/pos.EntryPrice)*100,
		atr, pos.PurchaseDate.Format("2006-01-02"))
}

func fetchHistoricalData(symbol string, startDate time.Time) {
	if _, exists := historicalCache[symbol]; exists {
		return
	}

	fromDate := startDate.AddDate(0, 0, -30)
	toDate := time.Now()

	url := fmt.Sprintf("%s/eod?access_key=%s&symbols=%s&date_from=%s&date_to=%s&limit=1000",
		MARKETSTACK_BASE_URL, apiToken, symbol,
		fromDate.Format("2006-01-02"),
		toDate.Format("2006-01-02"))

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("[%s] ❌ Error fetching historical data: %v", symbol, err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[%s] ❌ Error reading response: %v", symbol, err)
		return
	}

	var result MarketstackEODResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[%s] ❌ Error parsing historical data: %v", symbol, err)
		return
	}

	sort.Slice(result.Data, func(i, j int) bool {
		dateI, _ := time.Parse("2006-01-02T15:04:05-0700", result.Data[i].Date)
		dateJ, _ := time.Parse("2006-01-02T15:04:05-0700", result.Data[j].Date)
		return dateI.Before(dateJ)
	})

	historicalCache[symbol] = result.Data
	log.Printf("[%s] ✅ Cached %d days of historical data", symbol, len(result.Data))
}

func calculate10DayMA(symbol string, currentDate time.Time) float64 {
	historicalData, exists := historicalCache[symbol]
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
	if len(activePositions) == 0 {
		return
	}

	log.Printf("⚡ Fast simulating %d position(s)...", len(activePositions))

	maxIterations := 10000
	iterations := 0

	for len(activePositions) > 0 && iterations < maxIterations {
		iterations++

		positionsToRemove := []string{}

		for symbol, pos := range activePositions {
			shouldRemove := simulateNextDay(pos)
			if shouldRemove {
				positionsToRemove = append(positionsToRemove, symbol)
			}
		}

		for _, symbol := range positionsToRemove {
			delete(activePositions, symbol)
		}

		if iterations%100 == 0 && len(activePositions) > 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	if iterations >= maxIterations {
		log.Println("⚠️  Max iterations reached, stopping simulation")
		for symbol, pos := range activePositions {
			log.Printf("[%s] ⚠️  Force closing position at last known price", symbol)
			historicalData := historicalCache[symbol]
			if len(historicalData) > 0 {
				lastPrice := historicalData[len(historicalData)-1].Close
				lastDate, _ := time.Parse("2006-01-02T15:04:05-0700", historicalData[len(historicalData)-1].Date)
				exitPosition(pos, lastPrice, lastDate, "Force closed - end of data")
			}
			delete(activePositions, symbol)
		}
	}

	log.Println("✅ All positions closed!")
}

func simulateActivePositions() {
	if len(activePositions) == 0 {
		return
	}

	log.Printf("📊 Simulating %d active position(s)...", len(activePositions))

	positionsToRemove := []string{}

	for symbol, pos := range activePositions {
		shouldRemove := simulateNextDay(pos)
		if shouldRemove {
			positionsToRemove = append(positionsToRemove, symbol)
		}
	}

	for _, symbol := range positionsToRemove {
		delete(activePositions, symbol)
	}
}

func simulateNextDay(pos *Position) bool {
	historicalData, exists := historicalCache[pos.Symbol]
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
	currentGain := currentPrice - pos.EntryPrice
	currentRR := currentGain / pos.InitialRisk

	// Track highest price
	if currentPrice > pos.HighestPrice {
		pos.HighestPrice = currentPrice
	}

	log.Printf("[%s] Day %d (%s) | Status: %s | Close: $%.2f | Gain: $%.2f (%.1f%%) | R/R: %.2fR | Stop: $%.2f",
		pos.Symbol, pos.DaysHeld, searchDate.Format("2006-01-02"), pos.Status,
		currentPrice, currentGain, (currentGain/pos.EntryPrice)*100, currentRR, pos.StopLoss)

	// RULE 1: Check stop loss (always first)
	if currentPrice <= pos.StopLoss {
		stopOut(pos, currentPrice, searchDate)
		return true
	}

	// RULE 5: Tighten stop if no follow-through within 3-5 days
	if pos.DaysHeld >= MAX_DAYS_NO_FOLLOWTHROUGH && currentPrice < pos.EntryPrice*1.05 && !pos.ProfitTaken {
		// Tighten stop to 50% of initial risk (halfway between entry and original stop)
		tightenedStop := pos.EntryPrice - (pos.InitialRisk * 0.5)
		if tightenedStop > pos.StopLoss {
			pos.StopLoss = tightenedStop
			log.Printf("[%s] ⚠️  No follow-through after %d days. Tightening stop to $%.2f (50%% of initial risk)",
				pos.Symbol, MAX_DAYS_NO_FOLLOWTHROUGH, pos.StopLoss)
		}
	}

	// RULE 2: Check for strong EP (100% gain in 3 days)
	percentGain := (currentPrice - pos.EntryPrice) / pos.EntryPrice
	if pos.DaysHeld <= STRONG_EP_DAYS && percentGain >= STRONG_EP_GAIN && !pos.ProfitTaken {
		takeStrongEPProfit(pos, currentPrice, searchDate)
		return false
	}

	// RULE 2: Take profit at 2R-4R
	if currentRR >= PROFIT_TAKE_MIN_RR && currentRR <= PROFIT_TAKE_MAX_RR && !pos.ProfitTaken {
		takeProfitPartial(pos, currentPrice, searchDate, PROFIT_TAKE_PERCENT)
		pos.TrailingStopMode = true
		return false
	}

	// RULE 3: Trailing stop using 10-day MA
	if pos.TrailingStopMode {
		ma10 := calculate10DayMA(pos.Symbol, searchDate)
		
		// If gain is > 5%, allow more breathing room by using a looser trailing stop
		percentGainFromEntry := (currentPrice - pos.EntryPrice) / pos.EntryPrice
		
		if percentGainFromEntry > 0.05 {
			// For gains > 5%, use a wider trailing stop (e.g., 3-5% below current price or MA minus buffer)
			// This gives the position room to breathe during normal pullbacks
			bufferPercent := 0.05 // 5% buffer
			loosestStop := currentPrice * (1 - bufferPercent)
			
			// Use the lower of: (MA - buffer) or (current price - 5%)
			// But never go below breakeven
			var newStop float64
			if ma10 > 0 {
				maWithBuffer := ma10 * 0.95 // 5% below MA
				newStop = math.Max(loosestStop, maWithBuffer)
			} else {
				newStop = loosestStop
			}
			
			// Never lower stop, and don't go below breakeven
			newStop = math.Max(newStop, pos.EntryPrice)
			
			if newStop > pos.StopLoss {
				pos.StopLoss = newStop
				log.Printf("[%s] 📈 Looser trailing stop (%.1f%% gain): $%.2f (%.1f%% below current)", 
					pos.Symbol, percentGainFromEntry*100, pos.StopLoss, 
					((currentPrice-pos.StopLoss)/currentPrice)*100)
			}
			
			// Still exit if price closes below MA (momentum break)
			if ma10 > 0 && currentPrice < ma10 {
				exitPosition(pos, currentPrice, searchDate, "Closed below 10-day MA")
				return true
			}
		} else {
			// For gains < 5%, use tighter stop (standard 10-day MA)
			if ma10 > 0 && currentPrice < ma10 {
				exitPosition(pos, currentPrice, searchDate, "Closed below 10-day MA")
				return true
			}

			// Update trailing stop to 10-day MA if higher than current stop
			if ma10 > pos.StopLoss {
				pos.StopLoss = ma10
				log.Printf("[%s] 📈 Trailing stop updated to 10-day MA: $%.2f", pos.Symbol, pos.StopLoss)
			}
		}
	}

	return false
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

	return &Position{
		Symbol:       strings.TrimSpace(row[0]),
		EntryPrice:   entryPrice,
		StopLoss:     stopLoss,
		Shares:       shares,
		PurchaseDate: purchaseDate,
		Status:       "HOLDING",
		ProfitTaken:  false,
		DaysHeld:     0,
	}
}

func takeStrongEPProfit(pos *Position, currentPrice float64, date time.Time) {
	sharesToSell := pos.Shares * STRONG_EP_TAKE_PERCENT
	saleProceeds := sharesToSell * currentPrice
	remainingShares := pos.Shares - sharesToSell
	profit := (currentPrice - pos.EntryPrice) * sharesToSell

	fmt.Printf("\n[%s] 🚀 STRONG EP DETECTED! Taking %.0f%% profit on %s\n",
		pos.Symbol, STRONG_EP_TAKE_PERCENT*100, date.Format("2006-01-02"))
	fmt.Printf("    Selling %.2f shares @ $%.2f = $%.2f (Profit: $%.2f)\n",
		sharesToSell, currentPrice, saleProceeds, profit)

	updateAccountSize(saleProceeds)

	pos.Shares = remainingShares
	pos.StopLoss = pos.EntryPrice
	pos.Status = "MONITORING (Strong EP)"
	pos.ProfitTaken = true
	pos.TrailingStopMode = true

	log.Printf("[%s] ✅ Stop moved to breakeven @ $%.2f | %.2f shares remaining\n",
		pos.Symbol, pos.StopLoss, remainingShares)
}

func takeProfitPartial(pos *Position, currentPrice float64, date time.Time, percent float64) {
	sharesToSell := pos.Shares * percent
	saleProceeds := sharesToSell * currentPrice
	remainingShares := pos.Shares - sharesToSell
	profit := (currentPrice - pos.EntryPrice) * sharesToSell
	rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk

	fmt.Printf("\n[%s] 🎯 TAKING PROFIT (%.2fR) on %s | Selling %.0f%% (%.2f shares) @ $%.2f = $%.2f (Profit: $%.2f)\n",
		pos.Symbol, rr, date.Format("2006-01-02"), percent*100, sharesToSell, currentPrice, saleProceeds, profit)

	updateAccountSize(saleProceeds)

	pos.Shares = remainingShares
	pos.StopLoss = pos.EntryPrice
	pos.Status = "MONITORING"
	pos.ProfitTaken = true

	log.Printf("[%s] ✅ Stop moved to breakeven @ $%.2f | %.2f shares remaining\n",
		pos.Symbol, pos.StopLoss, remainingShares)
}

func stopOut(pos *Position, currentPrice float64, date time.Time) {
	totalProceeds := currentPrice * pos.Shares
	costBasis := pos.EntryPrice * pos.Shares
	profitLoss := totalProceeds - costBasis

	// Check if this is actually a winner (stop hit at breakeven or above after taking profit)
	isWinner := currentPrice >= pos.EntryPrice || profitLoss > 0
	exitReason := "Stop Loss Hit"
	
	if isWinner && pos.ProfitTaken {
		exitReason = "Trailing Stop Hit (Profit Protected)"
		fmt.Printf("\n[%s] 📊 TRAILING STOP HIT on %s @ $%.2f | Remaining Position P/L: $%.2f\n",
			pos.Symbol, date.Format("2006-01-02"), currentPrice, profitLoss)
	} else {
		fmt.Printf("\n[%s] 🛑 STOPPED OUT on %s @ $%.2f | Loss: $%.2f\n",
			pos.Symbol, date.Format("2006-01-02"), currentPrice, profitLoss)
	}

	updateAccountSize(totalProceeds)

	recordTradeResult(TradeResult{
		Symbol:      pos.Symbol,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   currentPrice,
		Shares:      pos.Shares,
		InitialRisk: pos.InitialRisk,
		ProfitLoss:  profitLoss,
		RiskReward:  (currentPrice - pos.EntryPrice) / pos.InitialRisk,
		EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
		ExitDate:    date.Format("2006-01-02"),
		ExitReason:  exitReason,
		IsWinner:    isWinner,
		AccountSize: accountSize,
	})

	// Remove from watchlist after closing
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
		AccountSize: accountSize,
	})

	// Remove from watchlist after closing
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

	log.Printf("[%s] ✍️  Trade recorded: P/L: $%.2f, R/R: %.2f, Account: $%.2f", result.Symbol, result.ProfitLoss, result.RiskReward, result.AccountSize)
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