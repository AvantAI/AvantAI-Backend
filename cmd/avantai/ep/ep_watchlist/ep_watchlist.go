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
// 	"sort"
// 	"strconv"
// 	"strings"
// 	"time"

// 	"github.com/joho/godotenv"
// )

// const (
// 	MARKETSTACK_BASE_URL = "http://api.marketstack.com/v2"
// 	CHECK_INTERVAL       = 5 * time.Minute
	
// 	// Stop loss configuration
// 	MAX_STOP_DISTANCE_MULTIPLIER = 1.5
// 	ATR_PERIOD                   = 14
	
// 	// Profit taking thresholds
// 	PROFIT_TAKE_MIN_RR    = 2.0
// 	PROFIT_TAKE_MAX_RR    = 4.0
// 	PROFIT_TAKE_PERCENT   = 0.40
// 	STRONG_EP_GAIN        = 1.00
// 	STRONG_EP_DAYS        = 3
// 	STRONG_EP_TAKE_PERCENT = 0.75
	
// 	// Trailing stop
// 	MA_PERIOD = 10
	
// 	// Weak close threshold
// 	WEAK_CLOSE_THRESHOLD = 0.30
	
// 	// Time-based exit
// 	MAX_DAYS_NO_FOLLOWTHROUGH = 5
	
// 	// Market hours (ET)
// 	MARKET_OPEN_HOUR     = 9
// 	MARKET_OPEN_MIN      = 30
// 	MARKET_CLOSE_HOUR    = 16
// 	MARKET_CLOSE_MIN     = 0
// 	PRE_CLOSE_CHECK_MIN  = 5
// )

// type MarketstackEODResponse struct {
// 	Data []MarketstackEODData `json:"data"`
// }

// type MarketstackEODData struct {
// 	Symbol string  `json:"symbol"`
// 	Open   float64 `json:"open"`
// 	High   float64 `json:"high"`
// 	Low    float64 `json:"low"`
// 	Close  float64 `json:"close"`
// 	Volume float64 `json:"volume"`
// 	Date   string  `json:"date"`
// }

// type Position struct {
// 	Symbol           string
// 	EntryPrice       float64
// 	StopLoss         float64
// 	Shares           float64
// 	PurchaseDate     time.Time
// 	Status           string
// 	ProfitTaken      bool
// 	DaysHeld         int
// 	LastCheckDate    time.Time
// 	InitialStopLoss  float64
// 	InitialRisk      float64
// 	EPDayLow         float64
// 	EPDayHigh        float64
// 	HighestPrice     float64
// 	TrailingStopMode bool
// }

// type TradeResult struct {
// 	Symbol       string
// 	EntryPrice   float64
// 	ExitPrice    float64
// 	Shares       float64
// 	InitialRisk  float64
// 	ProfitLoss   float64
// 	RiskReward   float64
// 	EntryDate    string
// 	ExitDate     string
// 	ExitReason   string
// 	IsWinner     bool
// 	AccountSize  float64
// }

// var (
// 	activePositions  = make(map[string]*Position)
// 	processedSymbols = make(map[string]bool)
// 	accountSize      float64
// 	riskPerTrade     float64
// 	apiToken         string
// 	lastModTime      time.Time
// 	historicalCache  = make(map[string][]MarketstackEODData)
// 	lastMarketClose  time.Time
// 	preCloseChecked  bool
// 	stopAlerts       = make(map[string]bool)
// 	profitAlerts     = make(map[string]bool)
// )

// func main() {
// 	err := godotenv.Load()
// 	if err != nil {
// 		log.Println("Error loading .env file, checking for environment variable")
// 	}

// 	apiToken = os.Getenv("MARKETSTACK_TOKEN")
// 	if apiToken == "" {
// 		log.Fatal("MARKETSTACK_TOKEN not found in environment or .env file")
// 	}

// 	accountSize = getAccountSize()
// 	riskPerTrade = getRiskPerTrade()
// 	log.Printf("Starting Account Size: $%.2f", accountSize)
// 	log.Printf("Risk Per Trade: %.2f%%", riskPerTrade*100)

// 	initializeTradeResultsFile()

// 	log.Println("🕐 Starting LIVE TRADING mode...")
// 	log.Println("Monitoring watchlist and active positions during market hours")
// 	log.Println("⏰ Pre-close check will run at 3:55 PM ET (5 min before close)")
// 	log.Println("📊 EOD processing will run after 4:00 PM ET")
// 	log.Println("Press Ctrl+C to stop")

// 	loadExistingPositions()
// 	displayPositionSummary()

// 	for {
// 		now := time.Now()
		
// 		if now.Hour() < MARKET_OPEN_HOUR || (now.Hour() == MARKET_OPEN_HOUR && now.Minute() < MARKET_OPEN_MIN) {
// 			preCloseChecked = false
// 		}
		
// 		if isMarketHours(now) {
// 			checkAndProcessWatchlist()
// 			monitorActivePositions()
			
// 			if isPreCloseWindow(now) && !preCloseChecked {
// 				log.Println("⏰ 5 MINUTES TO MARKET CLOSE - Running pre-close position check...")
// 				performPreCloseCheck()
// 				preCloseChecked = true
// 			}
// 		} else if justAfterMarketClose(now) {
// 			log.Println("⏰ Market closed. Waiting for EOD data...")
// 			time.Sleep(10 * time.Minute)
			
// 			processEndOfDayPositions()
// 			lastMarketClose = time.Now()
// 			preCloseChecked = false
// 		}

// 		time.Sleep(CHECK_INTERVAL)
// 	}
// }

// func getAccountSize() float64 {
// 	accountSizeStr := os.Getenv("ACCOUNT_SIZE")
// 	if accountSizeStr == "" {
// 		log.Println("ACCOUNT_SIZE not set, using default $10,000")
// 		return 10000.0
// 	}
// 	size, err := strconv.ParseFloat(accountSizeStr, 64)
// 	if err != nil {
// 		log.Println("Invalid ACCOUNT_SIZE, using default $10,000")
// 		return 10000.0
// 	}
// 	return size
// }

// func getRiskPerTrade() float64 {
// 	riskStr := os.Getenv("RISK_PER_TRADE")
// 	if riskStr == "" {
// 		log.Println("RISK_PER_TRADE not set, using default 1%")
// 		return 0.01
// 	}
// 	risk, err := strconv.ParseFloat(riskStr, 64)
// 	if err != nil {
// 		log.Println("Invalid RISK_PER_TRADE, using default 1%")
// 		return 0.01
// 	}
// 	return risk
// }

// func updateAccountSize(amount float64) {
// 	accountSize += amount
// 	log.Printf("💰 Account balance: $%.2f (change: $%.2f)", accountSize, amount)
// }

// func isMarketHours(t time.Time) bool {
// 	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
// 		return false
// 	}
	
// 	hour, min, _ := t.Clock()
// 	openMinutes := MARKET_OPEN_HOUR*60 + MARKET_OPEN_MIN
// 	closeMinutes := MARKET_CLOSE_HOUR*60 + MARKET_CLOSE_MIN
// 	currentMinutes := hour*60 + min
	
// 	return currentMinutes >= openMinutes && currentMinutes < closeMinutes
// }

// func isPreCloseWindow(t time.Time) bool {
// 	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
// 		return false
// 	}
	
// 	hour, min, _ := t.Clock()
// 	closeMinutes := MARKET_CLOSE_HOUR*60 + MARKET_CLOSE_MIN
// 	preCloseMinutes := closeMinutes - PRE_CLOSE_CHECK_MIN
// 	currentMinutes := hour*60 + min
	
// 	return currentMinutes >= preCloseMinutes && currentMinutes < closeMinutes
// }

// func justAfterMarketClose(t time.Time) bool {
// 	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
// 		return false
// 	}
	
// 	hour, min, _ := t.Clock()
// 	closeMinutes := MARKET_CLOSE_HOUR*60 + MARKET_CLOSE_MIN
// 	currentMinutes := hour*60 + min
	
// 	if currentMinutes >= closeMinutes && currentMinutes < closeMinutes+30 {
// 		return lastMarketClose.Day() != t.Day()
// 	}
	
// 	return false
// }

// func loadExistingPositions() {
// 	filePath := "watchlist.csv"
// 	file, err := os.Open(filePath)
// 	if err != nil {
// 		filePath = "cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv"
// 		file, err = os.Open(filePath)
// 		if err != nil {
// 			log.Println("No existing watchlist found")
// 			return
// 		}
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

// 	log.Println("\n📥 Loading existing positions from watchlist...")

// 	for i := 1; i < len(records); i++ {
// 		pos := parsePosition(records[i])
// 		if pos != nil && (pos.Status == "HOLDING" || pos.Status == "MONITORING" || strings.Contains(pos.Status, "Strong EP")) {
// 			fetchHistoricalData(pos.Symbol, pos.PurchaseDate)
			
// 			pos.DaysHeld = int(time.Since(pos.PurchaseDate).Hours() / 24)
			
// 			activePositions[pos.Symbol] = pos
// 			processedSymbols[pos.Symbol] = true
			
// 			currentPrice := getCurrentPrice(pos.Symbol)
// 			gain := currentPrice - pos.EntryPrice
			
// 			log.Printf("  [%s] %.2f shares @ $%.2f | Current: $%.2f | Gain: $%.2f (%.1f%%) | Days: %d", 
// 				pos.Symbol, pos.Shares, pos.EntryPrice, currentPrice, gain, 
// 				(gain/pos.EntryPrice)*100, pos.DaysHeld)
// 		}
// 	}
	
// 	if len(activePositions) > 0 {
// 		log.Printf("✅ Loaded %d active position(s)\n", len(activePositions))
// 	} else {
// 		log.Println("✅ No active positions to load\n")
// 	}
// }

// func displayPositionSummary() {
// 	if len(activePositions) == 0 {
// 		log.Println("📊 No active positions")
// 		return
// 	}

// 	log.Println("\n" + strings.Repeat("=", 80))
// 	log.Println("📊 ACTIVE POSITIONS SUMMARY")
// 	log.Println(strings.Repeat("=", 80))
	
// 	totalValue := 0.0
// 	totalGain := 0.0
	
// 	for symbol, pos := range activePositions {
// 		currentPrice := getCurrentPrice(symbol)
// 		positionValue := currentPrice * pos.Shares
// 		positionGain := (currentPrice - pos.EntryPrice) * pos.Shares
// 		rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk
		
// 		totalValue += positionValue
// 		totalGain += positionGain
		
// 		log.Printf("[%s] %s | %.0f shares @ $%.2f → $%.2f | Value: $%.2f | P/L: $%.2f (%.1f%%) | R/R: %.2fR | Days: %d",
// 			symbol, pos.Status, pos.Shares, pos.EntryPrice, currentPrice, positionValue, 
// 			positionGain, (positionGain/(pos.EntryPrice*pos.Shares))*100, rr, pos.DaysHeld)
// 	}
	
// 	log.Println(strings.Repeat("-", 80))
// 	log.Printf("Total Position Value: $%.2f | Total P/L: $%.2f | Account: $%.2f",
// 		totalValue, totalGain, accountSize)
// 	log.Println(strings.Repeat("=", 80) + "\n")
// }

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
// 	for i := 1; i < len(records); i++ {
// 		pos := parsePosition(records[i])
// 		if pos != nil && !processedSymbols[pos.Symbol] && activePositions[pos.Symbol] == nil {
// 			positions = append(positions, pos)
// 		}
// 	}

// 	sort.Slice(positions, func(i, j int) bool {
// 		return positions[i].PurchaseDate.Before(positions[j].PurchaseDate)
// 	})

// 	for _, pos := range positions {
// 		if shouldStartPosition(pos) {
// 			startPosition(pos)
// 		}
// 	}
// }

// func shouldStartPosition(newPos *Position) bool {
// 	today := time.Now().Format("2006-01-02")
// 	posDate := newPos.PurchaseDate.Format("2006-01-02")
	
// 	return today == posDate
// }

// func startPosition(pos *Position) {
// 	log.Printf("[%s] 📥 Fetching historical data...", pos.Symbol)
// 	fetchHistoricalData(pos.Symbol, pos.PurchaseDate)

// 	historicalData, exists := historicalCache[pos.Symbol]
// 	if !exists {
// 		log.Printf("[%s] ⚠️  Cannot start position - no historical data", pos.Symbol)
// 		processedSymbols[pos.Symbol] = true
// 		return
// 	}

// 	var epDayData *MarketstackEODData
// 	for i := range historicalData {
// 		dataDate, _ := time.Parse("2006-01-02T15:04:05-0700", historicalData[i].Date)
// 		if dataDate.Format("2006-01-02") == pos.PurchaseDate.Format("2006-01-02") {
// 			epDayData = &historicalData[i]
// 			break
// 		}
// 	}

// 	if epDayData == nil {
// 		log.Printf("[%s] ⚠️  Cannot find EP day data", pos.Symbol)
// 		processedSymbols[pos.Symbol] = true
// 		return
// 	}

// 	pos.EPDayLow = epDayData.Low
// 	pos.EPDayHigh = epDayData.High
// 	suggestedStop := pos.EPDayLow * 0.99

// 	atr := calculateATR(pos.Symbol, pos.PurchaseDate, ATR_PERIOD)
// 	if atr > 0 {
// 		maxStopDistance := atr * MAX_STOP_DISTANCE_MULTIPLIER
// 		actualStopDistance := pos.EntryPrice - suggestedStop

// 		if actualStopDistance > maxStopDistance {
// 			suggestedStop = pos.EntryPrice - maxStopDistance
// 			log.Printf("[%s] ⚠️  Stop adjusted to respect ATR limit", pos.Symbol)
// 		}
// 	}

// 	pos.StopLoss = suggestedStop
// 	pos.InitialStopLoss = suggestedStop
// 	pos.InitialRisk = pos.EntryPrice - pos.StopLoss
// 	pos.HighestPrice = pos.EntryPrice

// 	closeFromHigh := (epDayData.High - epDayData.Close) / (epDayData.High - epDayData.Low)
// 	if closeFromHigh > WEAK_CLOSE_THRESHOLD {
// 		log.Printf("[%s] ⚠️  WEAK CLOSE detected on EP day. SKIPPING TRADE.", pos.Symbol)
// 		processedSymbols[pos.Symbol] = true
// 		return
// 	}

// 	riskPerShare := pos.InitialRisk
// 	if riskPerShare <= 0 {
// 		log.Printf("[%s] ⚠️  Invalid risk calculation", pos.Symbol)
// 		processedSymbols[pos.Symbol] = true
// 		return
// 	}

// 	positionCost := pos.EntryPrice * pos.Shares
// 	if positionCost > accountSize {
// 		log.Printf("[%s] ⚠️  Insufficient funds", pos.Symbol)
// 		processedSymbols[pos.Symbol] = true
// 		return
// 	}

// 	updateAccountSize(-positionCost)
// 	pos.LastCheckDate = pos.PurchaseDate
// 	pos.Status = "HOLDING"
// 	activePositions[pos.Symbol] = pos
// 	processedSymbols[pos.Symbol] = true

// 	updatePositionInCSV(pos)

// 	log.Printf("[%s] 🟢 OPENED POSITION | Shares: %.2f @ $%.2f | Stop: $%.2f | Risk: $%.2f",
// 		pos.Symbol, pos.Shares, pos.EntryPrice, pos.StopLoss, pos.InitialRisk)
// }

// func calculateATR(symbol string, endDate time.Time, period int) float64 {
// 	historicalData, exists := historicalCache[symbol]
// 	if !exists || len(historicalData) < period {
// 		return 0
// 	}

// 	var trueRanges []float64
// 	var endIdx int

// 	for i := range historicalData {
// 		dataDate, _ := time.Parse("2006-01-02T15:04:05-0700", historicalData[i].Date)
// 		if !dataDate.After(endDate) {
// 			endIdx = i
// 		}
// 	}

// 	for i := endIdx - period + 1; i <= endIdx && i < len(historicalData); i++ {
// 		if i <= 0 {
// 			continue
// 		}
// 		current := historicalData[i]
// 		previous := historicalData[i-1]

// 		highLow := current.High - current.Low
// 		highClose := math.Abs(current.High - previous.Close)
// 		lowClose := math.Abs(current.Low - previous.Close)

// 		tr := math.Max(highLow, math.Max(highClose, lowClose))
// 		trueRanges = append(trueRanges, tr)
// 	}

// 	if len(trueRanges) == 0 {
// 		return 0
// 	}

// 	sum := 0.0
// 	for _, tr := range trueRanges {
// 		sum += tr
// 	}

// 	return sum / float64(len(trueRanges))
// }

// func fetchHistoricalData(symbol string, startDate time.Time) {
// 	if _, exists := historicalCache[symbol]; exists {
// 		return
// 	}

// 	fromDate := startDate.AddDate(0, 0, -30)
// 	toDate := time.Now()

// 	url := fmt.Sprintf("%s/eod?access_key=%s&symbols=%s&date_from=%s&date_to=%s&limit=1000",
// 		MARKETSTACK_BASE_URL, apiToken, symbol,
// 		fromDate.Format("2006-01-02"),
// 		toDate.Format("2006-01-02"))

// 	resp, err := http.Get(url)
// 	if err != nil {
// 		log.Printf("[%s] ❌ Error fetching historical data: %v", symbol, err)
// 		return
// 	}
// 	defer resp.Body.Close()

// 	body, err := io.ReadAll(resp.Body)
// 	if err != nil {
// 		log.Printf("[%s] ❌ Error reading response: %v", symbol, err)
// 		return
// 	}

// 	var result MarketstackEODResponse
// 	if err := json.Unmarshal(body, &result); err != nil {
// 		log.Printf("[%s] ❌ Error parsing data: %v", symbol, err)
// 		return
// 	}

// 	sort.Slice(result.Data, func(i, j int) bool {
// 		dateI, _ := time.Parse("2006-01-02T15:04:05-0700", result.Data[i].Date)
// 		dateJ, _ := time.Parse("2006-01-02T15:04:05-0700", result.Data[j].Date)
// 		return dateI.Before(dateJ)
// 	})

// 	historicalCache[symbol] = result.Data
// 	log.Printf("[%s] ✅ Cached %d days of historical data", symbol, len(result.Data))
// }

// func calculate10DayMA(symbol string, currentDate time.Time) float64 {
// 	historicalData, exists := historicalCache[symbol]
// 	if !exists {
// 		return 0
// 	}

// 	var prices []float64
// 	for i := range historicalData {
// 		dataDate, _ := time.Parse("2006-01-02T15:04:05-0700", historicalData[i].Date)
// 		if dataDate.Before(currentDate) || dataDate.Equal(currentDate) {
// 			prices = append(prices, historicalData[i].Close)
// 		}
// 	}

// 	if len(prices) < MA_PERIOD {
// 		return 0
// 	}

// 	recentPrices := prices[len(prices)-MA_PERIOD:]
// 	sum := 0.0
// 	for _, price := range recentPrices {
// 		sum += price
// 	}

// 	return sum / float64(MA_PERIOD)
// }

// func getCurrentPrice(symbol string) float64 {
// 	historicalData, exists := historicalCache[symbol]
// 	if !exists || len(historicalData) == 0 {
// 		return 0
// 	}

// 	return historicalData[len(historicalData)-1].Close
// }

// func monitorActivePositions() {
// 	if len(activePositions) == 0 {
// 		return
// 	}

// 	now := time.Now()
// 	hour, min, _ := now.Clock()
// 	currentTime := fmt.Sprintf("%02d:%02d", hour, min)

// 	log.Printf("📊 [%s] Monitoring %d active position(s)...", currentTime, len(activePositions))

// 	for symbol, pos := range activePositions {
// 		fetchHistoricalData(pos.Symbol, pos.PurchaseDate)
		
// 		currentPrice := getCurrentPrice(pos.Symbol)
// 		if currentPrice == 0 {
// 			continue
// 		}

// 		currentGain := currentPrice - pos.EntryPrice
// 		currentRR := currentGain / pos.InitialRisk
// 		percentGain := (currentPrice - pos.EntryPrice) / pos.EntryPrice

// 		if currentPrice > pos.HighestPrice {
// 			pos.HighestPrice = currentPrice
// 			log.Printf("[%s] 📈 NEW HIGH: $%.2f (was $%.2f)", symbol, currentPrice, pos.HighestPrice)
// 		}

// 		log.Printf("[%s] Status: %s | Current: $%.2f | Gain: $%.2f (%.1f%%) | R/R: %.2fR | Stop: $%.2f",
// 			symbol, pos.Status, currentPrice, currentGain, 
// 			(currentGain/pos.EntryPrice)*100, currentRR, pos.StopLoss)

// 		if currentPrice <= pos.StopLoss*1.02 {
// 			if !stopAlerts[symbol] {
// 				log.Printf("[%s] 🚨🚨 CRITICAL ALERT: Price at/near stop ($%.2f vs stop $%.2f)!", 
// 					symbol, currentPrice, pos.StopLoss)
// 				log.Printf("[%s] 🚨🚨 ACTION: Consider selling NOW to avoid further losses!", symbol)
// 				stopAlerts[symbol] = true
// 			}
// 		} else {
// 			delete(stopAlerts, symbol)
// 		}

// 		if !pos.ProfitTaken {
// 			if pos.DaysHeld <= STRONG_EP_DAYS && percentGain >= STRONG_EP_GAIN {
// 				if !profitAlerts[symbol] {
// 					log.Printf("[%s] 🎉🎉 STRONG EP ALERT: %.1f%% gain in %d days!", 
// 						symbol, percentGain*100, pos.DaysHeld)
// 					log.Printf("[%s] 🎉🎉 RECOMMENDATION: Take %.0f%% profit at market close", 
// 						symbol, STRONG_EP_TAKE_PERCENT*100)
// 					profitAlerts[symbol] = true
// 				}
// 			}
			
// 			if currentRR >= PROFIT_TAKE_MIN_RR && currentRR <= PROFIT_TAKE_MAX_RR {
// 				if !profitAlerts[symbol] {
// 					log.Printf("[%s] 🎯🎯 PROFIT TARGET REACHED: %.2fR!", symbol, currentRR)
// 					log.Printf("[%s] 🎯🎯 RECOMMENDATION: Take %.0f%% profit at market close", 
// 						symbol, PROFIT_TAKE_PERCENT*100)
// 					profitAlerts[symbol] = true
// 				}
// 			}
// 		}

// 		if pos.TrailingStopMode {
// 			ma10 := calculate10DayMA(symbol, time.Now())
// 			if ma10 > 0 {
// 				distanceFromMA := ((currentPrice - ma10) / ma10) * 100
// 				if distanceFromMA < 2.0 && distanceFromMA > -1.0 {
// 					log.Printf("[%s] ⚠️  Near 10-day MA: $%.2f (MA: $%.2f, %.1f%% away)", 
// 						symbol, currentPrice, ma10, distanceFromMA)
// 				}
// 			}
// 		}
// 	}
// }

// func performPreCloseCheck() {
// 	if len(activePositions) == 0 {
// 		log.Println("✅ No active positions to check")
// 		return
// 	}

// 	log.Println(strings.Repeat("=", 80))
// 	log.Println("🔔 PRE-CLOSE POSITION EVALUATION (3:55 PM - 5 MINUTES TO CLOSE)")
// 	log.Println(strings.Repeat("=", 80))

// 	sellRecommendations := []string{}
// 	holdPositions := []string{}

// 	for symbol, pos := range activePositions {
// 		fetchHistoricalData(symbol, pos.PurchaseDate)
// 		currentPrice := getCurrentPrice(symbol)
		
// 		if currentPrice == 0 {
// 			log.Printf("[%s] ⚠️  Unable to get current price", symbol)
// 			continue
// 		}

// 		currentGain := currentPrice - pos.EntryPrice
// 		currentRR := currentGain / pos.InitialRisk
// 		percentGain := (currentPrice - pos.EntryPrice) / pos.EntryPrice
// 		positionValue := currentPrice * pos.Shares

// 		log.Printf("\n[%s] Pre-Close Analysis:", symbol)
// 		log.Printf("  Current Price: $%.2f (Position Value: $%.2f)", currentPrice, positionValue)
// 		log.Printf("  Entry Price: $%.2f | Stop Loss: $%.2f", pos.EntryPrice, pos.StopLoss)
// 		log.Printf("  Gain: $%.2f (%.1f%%) | R/R: %.2fR", currentGain, percentGain*100, currentRR)
// 		log.Printf("  Days Held: %d | Status: %s | Profit Taken: %v", pos.DaysHeld, pos.Status, pos.ProfitTaken)

// 		shouldSell := false
// 		sellReason := ""

// 		if currentPrice <= pos.StopLoss {
// 			log.Printf("  🛑 ALERT: SELL NOW - Price at/below stop!")
// 			sellReason = fmt.Sprintf("AT STOP LOSS ($%.2f <= $%.2f) - LOSS: $%.2f", 
// 				currentPrice, pos.StopLoss, currentGain*pos.Shares)
// 			shouldSell = true
// 		}

// 		if !shouldSell && pos.DaysHeld <= STRONG_EP_DAYS && percentGain >= STRONG_EP_GAIN && !pos.ProfitTaken {
// 			log.Printf("  🚀 ALERT: Take %.0f%% profit - Strong EP detected!", STRONG_EP_TAKE_PERCENT*100)
// 			sharesToSell := pos.Shares * STRONG_EP_TAKE_PERCENT
// 			profitAmount := (currentPrice - pos.EntryPrice) * sharesToSell
// 			sellReason = fmt.Sprintf("STRONG EP - Sell %.0f shares (%.0f%% of position) for $%.2f profit (%.1f%% gain in %d days)", 
// 				sharesToSell, STRONG_EP_TAKE_PERCENT*100, profitAmount, percentGain*100, pos.DaysHeld)
// 			shouldSell = true
// 		}

// 		if !shouldSell && currentRR >= PROFIT_TAKE_MIN_RR && currentRR <= PROFIT_TAKE_MAX_RR && !pos.ProfitTaken {
// 			log.Printf("  🎯 ALERT: Take %.0f%% profit - Hit %.2fR target!", PROFIT_TAKE_PERCENT*100, currentRR)
// 			sharesToSell := pos.Shares * PROFIT_TAKE_PERCENT
// 			profitAmount := (currentPrice - pos.EntryPrice) * sharesToSell
// 			sellReason = fmt.Sprintf("PROFIT TARGET - Sell %.0f shares (%.0f%% of position) for $%.2f profit (%.2fR achieved)", 
// 				sharesToSell, PROFIT_TAKE_PERCENT*100, profitAmount, currentRR)
// 			shouldSell = true
// 		}

// 		if !shouldSell && pos.TrailingStopMode {
// 			ma10 := calculate10DayMA(symbol, time.Now())
// 			if ma10 > 0 && currentPrice < ma10 {
// 				log.Printf("  📉 ALERT: SELL - Price below 10-day MA ($%.2f)", ma10)
// 				profitAmount := (currentPrice - pos.EntryPrice) * pos.Shares
// 				sellReason = fmt.Sprintf("TRAILING STOP - Below 10-day MA ($%.2f < $%.2f) - P/L: $%.2f", 
// 					currentPrice, ma10, profitAmount)
// 				shouldSell = true
// 			}
// 		}

// 		if !shouldSell && pos.DaysHeld >= MAX_DAYS_NO_FOLLOWTHROUGH && currentPrice < pos.EntryPrice*1.05 && !pos.ProfitTaken {
// 			log.Printf("  ⚠️  WARNING: No follow-through after %d days (only %.1f%% gain)", 
// 				pos.DaysHeld, percentGain*100)
// 			log.Printf("  Consider tightening stop or exiting if no momentum")
// 			holdPositions = append(holdPositions, 
// 				fmt.Sprintf("%s: WEAK MOMENTUM - %d days, only %.1f%% gain - Consider exit", 
// 					symbol, pos.DaysHeld, percentGain*100))
// 		}

// 		if shouldSell {
// 			sellRecommendations = append(sellRecommendations, fmt.Sprintf("%s: %s", symbol, sellReason))
// 			log.Printf("  ❗ ACTION REQUIRED: %s", sellReason)
// 		} else if len(holdPositions) == 0 || holdPositions[len(holdPositions)-1][:len(symbol)] != symbol {
// 			log.Printf("  ✅ HOLD - No exit signals at this time")
// 			holdPositions = append(holdPositions, 
// 				fmt.Sprintf("%s: Hold - P/L: $%.2f (%.1f%%), R/R: %.2fR", 
// 					symbol, currentGain*pos.Shares, percentGain*100, currentRR))
// 		}
// 	}

// 	log.Println("\n" + strings.Repeat("=", 80))
// 	if len(sellRecommendations) > 0 {
// 		log.Println("🔔 IMMEDIATE SELL RECOMMENDATIONS (Execute before 4:00 PM ET):")
// 		log.Println(strings.Repeat("-", 80))
// 		for i, rec := range sellRecommendations {
// 			log.Printf("  %d. %s", i+1, rec)
// 		}
// 		log.Println(strings.Repeat("-", 80))
// 		log.Println("⏰ YOU HAVE 5 MINUTES TO EXECUTE THESE SALES")
// 	} else {
// 		log.Println("✅ No immediate sell signals")
// 	}
	
// 	if len(holdPositions) > 0 {
// 		log.Println("\n📊 POSITIONS TO HOLD:")
// 		log.Println(strings.Repeat("-", 80))
// 		for i, pos := range holdPositions {
// 			log.Printf("  %d. %s", i+1, pos)
// 		}
// 	}
// 	log.Println(strings.Repeat("=", 80) + "\n")
// }

// func processEndOfDayPositions() {
// 	if len(activePositions) == 0 {
// 		return
// 	}

// 	log.Println("🌙 Processing end-of-day positions...")

// 	for symbol := range activePositions {
// 		fetchHistoricalData(symbol, activePositions[symbol].PurchaseDate)
// 	}

// 	time.Sleep(30 * time.Second)

// 	positionsToRemove := []string{}
// 	positionsUpdated := 0

// 	for symbol, pos := range activePositions {
// 		shouldRemove := processEODForPosition(pos)
// 		if shouldRemove {
// 			positionsToRemove = append(positionsToRemove, symbol)
// 		} else {
// 			positionsUpdated++
// 		}
// 	}

// 	for _, symbol := range positionsToRemove {
// 		delete(activePositions, symbol)
// 	}

// 	log.Printf("✅ EOD processing complete - %d positions updated, %d closed", 
// 		positionsUpdated, len(positionsToRemove))
	
// 	if len(activePositions) > 0 {
// 		displayPositionSummary()
// 	}
// }

// func processEODForPosition(pos *Position) bool {
// 	historicalData, exists := historicalCache[pos.Symbol]
// 	if !exists {
// 		return false
// 	}

// 	today := time.Now().Format("2006-01-02")
// 	var todayData *MarketstackEODData

// 	for i := len(historicalData) - 1; i >= 0; i-- {
// 		dataDate, _ := time.Parse("2006-01-02T15:04:05-0700", historicalData[i].Date)
// 		if dataDate.Format("2006-01-02") == today {
// 			todayData = &historicalData[i]
// 			break
// 		}
// 	}

// 	if todayData == nil {
// 		log.Printf("[%s] ⚠️  No EOD data available for today yet", pos.Symbol)
// 		return false
// 	}

// 	pos.LastCheckDate = time.Now()
// 	pos.DaysHeld++

// 	currentPrice := todayData.Close
// 	currentGain := currentPrice - pos.EntryPrice
// 	currentRR := currentGain / pos.InitialRisk

// 	if currentPrice > pos.HighestPrice {
// 		pos.HighestPrice = currentPrice
// 	}

// 	log.Printf("[%s] EOD Day %d | Close: $%.2f | Gain: $%.2f (%.1f%%) | R/R: %.2fR",
// 		pos.Symbol, pos.DaysHeld, currentPrice, currentGain, 
// 		(currentGain/pos.EntryPrice)*100, currentRR)

// 	if currentPrice <= pos.StopLoss {
// 		stopOut(pos, currentPrice, time.Now())
// 		return true
// 	}

// 	if pos.DaysHeld >= MAX_DAYS_NO_FOLLOWTHROUGH && currentPrice < pos.EntryPrice*1.05 && !pos.ProfitTaken {
// 		tightenedStop := pos.EntryPrice - (pos.InitialRisk * 0.5)
// 		if tightenedStop > pos.StopLoss {
// 			pos.StopLoss = tightenedStop
// 			log.Printf("[%s] ⚠️  No follow-through. Tightening stop to $%.2f", pos.Symbol, pos.StopLoss)
// 			updatePositionInCSV(pos)
// 		}
// 	}

// 	percentGain := (currentPrice - pos.EntryPrice) / pos.EntryPrice
// 	if pos.DaysHeld <= STRONG_EP_DAYS && percentGain >= STRONG_EP_GAIN && !pos.ProfitTaken {
// 		takeStrongEPProfit(pos, currentPrice, time.Now())
// 		updatePositionInCSV(pos)
// 		return false
// 	}

// 	if currentRR >= PROFIT_TAKE_MIN_RR && currentRR <= PROFIT_TAKE_MAX_RR && !pos.ProfitTaken {
// 		takeProfitPartial(pos, currentPrice, time.Now(), PROFIT_TAKE_PERCENT)
// 		pos.TrailingStopMode = true
// 		updatePositionInCSV(pos)
// 		return false
// 	}

// 	if pos.TrailingStopMode {
// 		ma10 := calculate10DayMA(pos.Symbol, time.Now())
// 		percentGainFromEntry := (currentPrice - pos.EntryPrice) / pos.EntryPrice
		
// 		if percentGainFromEntry > 0.05 {
// 			bufferPercent := 0.05
// 			loosestStop := currentPrice * (1 - bufferPercent)
			
// 			var newStop float64
// 			if ma10 > 0 {
// 				maWithBuffer := ma10 * 0.95
// 				newStop = math.Max(loosestStop, maWithBuffer)
// 			} else {
// 				newStop = loosestStop
// 			}
			
// 			newStop = math.Max(newStop, pos.EntryPrice)
			
// 			if newStop > pos.StopLoss {
// 				pos.StopLoss = newStop
// 				log.Printf("[%s] 📈 Looser trailing stop: $%.2f", pos.Symbol, pos.StopLoss)
// 				updatePositionInCSV(pos)
// 			}
			
// 			if ma10 > 0 && currentPrice < ma10 {
// 				exitPosition(pos, currentPrice, time.Now(), "Closed below 10-day MA")
// 				return true
// 			}
// 		} else {
// 			if ma10 > 0 && currentPrice < ma10 {
// 				exitPosition(pos, currentPrice, time.Now(), "Closed below 10-day MA")
// 				return true
// 			}

// 			if ma10 > pos.StopLoss {
// 				pos.StopLoss = ma10
// 				log.Printf("[%s] 📈 Trailing stop to MA: $%.2f", pos.Symbol, pos.StopLoss)
// 				updatePositionInCSV(pos)
// 			}
// 		}
// 	}

// 	updatePositionInCSV(pos)

// 	return false
// }

// func takeStrongEPProfit(pos *Position, currentPrice float64, date time.Time) {
// 	sharesToSell := pos.Shares * STRONG_EP_TAKE_PERCENT
// 	saleProceeds := sharesToSell * currentPrice
// 	remainingShares := pos.Shares - sharesToSell
// 	profit := (currentPrice - pos.EntryPrice) * sharesToSell

// 	fmt.Printf("\n[%s] 🚀 STRONG EP! Taking %.0f%% profit\n", pos.Symbol, STRONG_EP_TAKE_PERCENT*100)
// 	fmt.Printf("    Selling %.2f shares @ $%.2f = $%.2f (Profit: $%.2f)\n",
// 		sharesToSell, currentPrice, saleProceeds, profit)

// 	updateAccountSize(saleProceeds)

// 	pos.Shares = remainingShares
// 	pos.StopLoss = pos.EntryPrice
// 	pos.Status = "MONITORING (Strong EP)"
// 	pos.ProfitTaken = true
// 	pos.TrailingStopMode = true

// 	log.Printf("[%s] ✅ Stop at breakeven @ $%.2f | %.2f shares remaining\n",
// 		pos.Symbol, pos.StopLoss, remainingShares)
// }

// func takeProfitPartial(pos *Position, currentPrice float64, date time.Time, percent float64) {
// 	sharesToSell := pos.Shares * percent
// 	saleProceeds := sharesToSell * currentPrice
// 	remainingShares := pos.Shares - sharesToSell
// 	profit := (currentPrice - pos.EntryPrice) * sharesToSell
// 	rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk

// 	fmt.Printf("\n[%s] 🎯 TAKING PROFIT (%.2fR) | Selling %.0f%%\n", pos.Symbol, rr, percent*100)
// 	fmt.Printf("    %.2f shares @ $%.2f = $%.2f (Profit: $%.2f)\n",
// 		sharesToSell, currentPrice, saleProceeds, profit)

// 	updateAccountSize(saleProceeds)

// 	pos.Shares = remainingShares
// 	pos.StopLoss = pos.EntryPrice
// 	pos.Status = "MONITORING"
// 	pos.ProfitTaken = true

// 	log.Printf("[%s] ✅ Stop at breakeven @ $%.2f | %.2f shares remaining\n",
// 		pos.Symbol, pos.StopLoss, remainingShares)
// }

// func stopOut(pos *Position, currentPrice float64, date time.Time) {
// 	totalProceeds := currentPrice * pos.Shares
// 	costBasis := pos.EntryPrice * pos.Shares
// 	profitLoss := totalProceeds - costBasis

// 	isWinner := currentPrice >= pos.EntryPrice || profitLoss > 0
// 	exitReason := "Stop Loss Hit"
	
// 	if isWinner && pos.ProfitTaken {
// 		exitReason = "Trailing Stop Hit (Profit Protected)"
// 		fmt.Printf("\n[%s] 📊 TRAILING STOP HIT @ $%.2f | Remaining P/L: $%.2f\n",
// 			pos.Symbol, currentPrice, profitLoss)
// 	} else {
// 		fmt.Printf("\n[%s] 🛑 STOPPED OUT @ $%.2f | Loss: $%.2f\n",
// 			pos.Symbol, currentPrice, profitLoss)
// 	}

// 	updateAccountSize(totalProceeds)

// 	recordTradeResult(TradeResult{
// 		Symbol:      pos.Symbol,
// 		EntryPrice:  pos.EntryPrice,
// 		ExitPrice:   currentPrice,
// 		Shares:      pos.Shares,
// 		InitialRisk: pos.InitialRisk,
// 		ProfitLoss:  profitLoss,
// 		RiskReward:  (currentPrice - pos.EntryPrice) / pos.InitialRisk,
// 		EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
// 		ExitDate:    date.Format("2006-01-02"),
// 		ExitReason:  exitReason,
// 		IsWinner:    isWinner,
// 		AccountSize: accountSize,
// 	})

// 	removeFromCSV(pos.Symbol)
// 	printCurrentStats()
// }

// func exitPosition(pos *Position, currentPrice float64, date time.Time, reason string) {
// 	totalProceeds := currentPrice * pos.Shares
// 	costBasis := pos.EntryPrice * pos.Shares
// 	profit := totalProceeds - costBasis

// 	fmt.Printf("\n[%s] 📤 EXITING @ $%.2f | Reason: %s | P/L: $%.2f\n",
// 		pos.Symbol, currentPrice, reason, profit)

// 	updateAccountSize(totalProceeds)

// 	rr := (currentPrice - pos.EntryPrice) / pos.InitialRisk

// 	recordTradeResult(TradeResult{
// 		Symbol:      pos.Symbol,
// 		EntryPrice:  pos.EntryPrice,
// 		ExitPrice:   currentPrice,
// 		Shares:      pos.Shares,
// 		InitialRisk: pos.InitialRisk,
// 		ProfitLoss:  profit,
// 		RiskReward:  rr,
// 		EntryDate:   pos.PurchaseDate.Format("2006-01-02"),
// 		ExitDate:    date.Format("2006-01-02"),
// 		ExitReason:  reason,
// 		IsWinner:    profit > 0,
// 		AccountSize: accountSize,
// 	})

// 	removeFromCSV(pos.Symbol)
// 	printCurrentStats()
// }

// func initializeTradeResultsFile() {
// 	filename := "trade_results.csv"
// 	if _, err := os.Stat(filename); os.IsNotExist(err) {
// 		file, err := os.Create(filename)
// 		if err != nil {
// 			log.Printf("Error creating trade results file: %v", err)
// 			return
// 		}
// 		defer file.Close()

// 		writer := csv.NewWriter(file)
// 		header := []string{"Symbol", "EntryPrice", "ExitPrice", "Shares", "InitialRisk", "ProfitLoss", "RiskReward", "EntryDate", "ExitDate", "ExitReason", "IsWinner", "AccountSize"}
// 		writer.Write(header)
// 		writer.Flush()
// 	}
// }

// func recordTradeResult(result TradeResult) {
// 	file, err := os.OpenFile("trade_results.csv", os.O_APPEND|os.O_WRONLY, 0644)
// 	if err != nil {
// 		log.Printf("Error opening trade results file: %v", err)
// 		return
// 	}
// 	defer file.Close()

// 	writer := csv.NewWriter(file)
// 	row := []string{
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
// 	}
// 	writer.Write(row)
// 	writer.Flush()

// 	log.Printf("[%s] ✍️  Trade recorded: P/L: $%.2f, R/R: %.2f, Account: $%.2f", 
// 		result.Symbol, result.ProfitLoss, result.RiskReward, result.AccountSize)
// }

// func updatePositionInCSV(pos *Position) {
// 	filePath := "watchlist.csv"
// 	file, err := os.Open(filePath)
// 	if err != nil {
// 		filePath = "cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv"
// 		file, err = os.Open(filePath)
// 		if err != nil {
// 			log.Printf("[%s] Error opening CSV: %v", pos.Symbol, err)
// 			return
// 		}
// 	}
// 	defer file.Close()

// 	reader := csv.NewReader(file)
// 	records, err := reader.ReadAll()
// 	if err != nil {
// 		log.Printf("[%s] Error reading CSV: %v", pos.Symbol, err)
// 		return
// 	}

// 	found := false
// 	for i := 1; i < len(records); i++ {
// 		if records[i][0] == pos.Symbol {
// 			records[i][1] = fmt.Sprintf("%.2f", pos.EntryPrice)
// 			records[i][2] = fmt.Sprintf("%.2f", pos.StopLoss)
// 			records[i][3] = fmt.Sprintf("%.2f", pos.Shares)
// 			records[i][4] = pos.PurchaseDate.Format("2006-01-02")
// 			if len(records[i]) > 5 {
// 				records[i][5] = pos.Status
// 			} else {
// 				records[i] = append(records[i], pos.Status)
// 			}
// 			if len(records[i]) > 6 {
// 				records[i][6] = fmt.Sprintf("%d", pos.DaysHeld)
// 			} else {
// 				records[i] = append(records[i], fmt.Sprintf("%d", pos.DaysHeld))
// 			}
// 			found = true
// 			break
// 		}
// 	}

// 	if !found {
// 		log.Printf("[%s] Position not found in CSV", pos.Symbol)
// 		return
// 	}

// 	outFile, err := os.Create(filePath)
// 	if err != nil {
// 		log.Printf("[%s] Error creating CSV: %v", pos.Symbol, err)
// 		return
// 	}
// 	defer outFile.Close()

// 	writer := csv.NewWriter(outFile)
// 	err = writer.WriteAll(records)
// 	if err != nil {
// 		log.Printf("[%s] Error writing CSV: %v", pos.Symbol, err)
// 	}
// 	writer.Flush()
// }

// func removeFromCSV(symbol string) {
// 	filePath := "watchlist.csv"
// 	file, err := os.Open(filePath)
// 	if err != nil {
// 		filePath = "cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv"
// 		file, err = os.Open(filePath)
// 		if err != nil {
// 			log.Printf("[%s] Error opening CSV: %v", symbol, err)
// 			return
// 		}
// 	}
// 	defer file.Close()

// 	reader := csv.NewReader(file)
// 	records, err := reader.ReadAll()
// 	if err != nil {
// 		log.Printf("[%s] Error reading CSV: %v", symbol, err)
// 		return
// 	}

// 	var newRecords [][]string
// 	for _, row := range records {
// 		if len(row) > 0 && row[0] != symbol {
// 			newRecords = append(newRecords, row)
// 		}
// 	}

// 	outFile, err := os.Create(filePath)
// 	if err != nil {
// 		log.Printf("[%s] Error creating CSV: %v", symbol, err)
// 		return
// 	}
// 	defer outFile.Close()

// 	writer := csv.NewWriter(outFile)
// 	err = writer.WriteAll(newRecords)
// 	if err != nil {
// 		log.Printf("[%s] Error writing CSV: %v", symbol, err)
// 	}
// 	writer.Flush()

// 	log.Printf("[%s] Removed from watchlist", symbol)
// }

// func printCurrentStats() {
// 	file, err := os.Open("trade_results.csv")
// 	if err != nil {
// 		return
// 	}
// 	defer file.Close()

// 	reader := csv.NewReader(file)
// 	records, err := reader.ReadAll()
// 	if err != nil {
// 		return
// 	}

// 	if len(records) <= 1 {
// 		return
// 	}

// 	totalTrades := len(records) - 1
// 	winners := 0
// 	totalPL := 0.0
// 	winnerRR := 0.0
// 	loserRR := 0.0
// 	losers := 0

// 	for i := 1; i < len(records); i++ {
// 		row := records[i]
// 		isWinner := row[10] == "true"
// 		pl, _ := strconv.ParseFloat(row[5], 64)
// 		rr, _ := strconv.ParseFloat(row[6], 64)

// 		totalPL += pl
// 		if isWinner {
// 			winners++
// 			winnerRR += rr
// 		} else {
// 			losers++
// 			loserRR += rr
// 		}
// 	}

// 	winRate := float64(winners) / float64(totalTrades) * 100
// 	avgWinRR := 0.0
// 	if winners > 0 {
// 		avgWinRR = winnerRR / float64(winners)
// 	}
// 	avgLossRR := 0.0
// 	if losers > 0 {
// 		avgLossRR = loserRR / float64(losers)
// 	}

// 	initialAccount := getAccountSize()
// 	returnPct := (accountSize - initialAccount) / initialAccount * 100

// 	fmt.Println("\n" + strings.Repeat("=", 70))
// 	fmt.Println("📈 CURRENT STATISTICS")
// 	fmt.Println(strings.Repeat("=", 70))
// 	fmt.Printf("Total Trades: %d | Winners: %d (%.1f%%) | Losers: %d (%.1f%%)\n", 
// 		totalTrades, winners, winRate, losers, 100-winRate)
// 	fmt.Printf("Avg Win R/R: %.2fR | Avg Loss R/R: %.2fR\n", avgWinRR, avgLossRR)
// 	fmt.Printf("Total P/L: $%.2f | Return: %.2f%%\n", totalPL, returnPct)
// 	fmt.Printf("Starting: $%.2f | Current: $%.2f\n", initialAccount, accountSize)
// 	fmt.Println(strings.Repeat("=", 70) + "\n")
// }

// func parsePosition(row []string) *Position {
// 	if len(row) < 5 {
// 		return nil
// 	}

// 	entryPrice, err1 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[1], "$")), 64)
// 	stopLoss, err2 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[2], "$")), 64)
// 	shares, err3 := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(row[3], "$")), 64)

// 	if err1 != nil || err2 != nil || err3 != nil {
// 		return nil
// 	}

// 	purchaseDate, err := time.Parse("2006-01-02", strings.TrimSpace(row[4]))
// 	if err != nil {
// 		return nil
// 	}

// 	status := "HOLDING"
// 	if len(row) > 5 {
// 		status = strings.TrimSpace(row[5])
// 	}

// 	daysHeld := 0
// 	if len(row) > 6 {
// 		daysHeld, _ = strconv.Atoi(strings.TrimSpace(row[6]))
// 	}

// 	return &Position{
// 		Symbol:       strings.TrimSpace(row[0]),
// 		EntryPrice:   entryPrice,
// 		StopLoss:     stopLoss,
// 		Shares:       shares,
// 		PurchaseDate: purchaseDate,
// 		Status:       status,
// 		ProfitTaken:  status == "MONITORING" || strings.Contains(status, "Strong EP"),
// 		DaysHeld:     daysHeld,
// 	}
// }