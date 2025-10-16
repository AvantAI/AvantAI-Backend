package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

const (
	PORTFOLIO_RISK_PERCENT = 0.10 // 10% risk per trade
	PROFIT_TAKE_PERCENT    = 0.30 // Take 30% profit (25-35% range)
	MARKETSTACK_BASE_URL   = "http://api.marketstack.com/v2"
)

// Marketstack API response models
type MarketstackRealtimeResponse struct {
	Data MarketstackData `json:"data"`
}

type MarketstackData struct {
	Symbol    string  `json:"symbol"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Last      float64 `json:"last"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
	Date      string  `json:"date"`
	Exchange  string  `json:"exchange"`
}

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

type MarketstackIntradayResponse struct {
	Data []MarketstackIntradayData `json:"data"`
}

type MarketstackIntradayData struct {
	Symbol string  `json:"symbol"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Last   float64 `json:"last"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
	Date   string  `json:"date"`
}

// Position represents a stock position in our watchlist
// CSV format: Symbol,PurchasePrice,StopLoss,Shares,DaysHeld,Status
type Position struct {
	Symbol        string
	PurchasePrice float64
	StopLoss      float64
	Shares        float64
	DaysHeld      int
	Status        string // "HOLDING", "PROFIT_TAKEN", "MONITORING"
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, checking for environment variable")
	}

	apiToken := os.Getenv("MARKETSTACK_TOKEN")
	if apiToken == "" {
		log.Fatal("MARKETSTACK_TOKEN not found in environment or .env file")
	}

	// Get account size for position sizing
	accountSize := getAccountSize()

	file, err := os.Open("watchlist.csv")
	if err != nil {
		file, err = os.Open("cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv")
		if err != nil {
			log.Fatal("Error opening watchlist file:", err)
		}
	}
	defer file.Close()

	reader := csv.NewReader(file)
	var wg sync.WaitGroup

	// Skip header if exists
	header, err := reader.Read()
	if err != nil {
		log.Fatal("Error reading header:", err)
	}
	log.Println("CSV Header:", header)

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error reading CSV: %v", err)
			break
		}

		position := parsePosition(row)
		if position == nil {
			continue
		}

		wg.Add(1)
		go MonitorPosition(&wg, apiToken, position, accountSize)
		time.Sleep(500 * time.Millisecond) // Rate limiting for free tier
	}

	wg.Wait()
}

func parsePosition(row []string) *Position {
	if len(row) < 6 {
		log.Println("Skipping malformed row:", row)
		return nil
	}

	purchasePrice, err1 := strconv.ParseFloat(row[1], 64)
	stopLoss, err2 := strconv.ParseFloat(row[2], 64)
	shares, err3 := strconv.ParseFloat(row[3], 64)
	daysHeld, err4 := strconv.Atoi(row[4])

	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		log.Println("Skipping row with invalid data:", row)
		return nil
	}

	return &Position{
		Symbol:        row[0],
		PurchasePrice: purchasePrice,
		StopLoss:      stopLoss,
		Shares:        shares,
		DaysHeld:      daysHeld,
		Status:        row[5],
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

// CalculatePositionSize calculates shares to buy based on risk management
func CalculatePositionSize(accountSize, purchasePrice, stopLoss float64) float64 {
	riskPerShare := purchasePrice - stopLoss
	if riskPerShare <= 0 {
		log.Println("Invalid risk calculation: stop loss must be below purchase price")
		return 0
	}

	totalRiskAmount := accountSize * PORTFOLIO_RISK_PERCENT
	shares := totalRiskAmount / riskPerShare

	log.Printf("Position Sizing - Account: $%.2f, Risk: $%.2f, Risk/Share: $%.2f, Shares: %.0f",
		accountSize, totalRiskAmount, riskPerShare, shares)

	return shares
}

func MonitorPosition(wg *sync.WaitGroup, apiToken string, pos *Position, accountSize float64) {
	defer wg.Done()

	// Fetch current price
	currentPrice := fetchCurrentPrice(apiToken, pos.Symbol)
	if currentPrice == 0 {
		return
	}

	log.Printf("[%s] Status: %s | Price: $%.2f | Entry: $%.2f | Stop: $%.2f | Shares: %.0f | Days: %d",
		pos.Symbol, pos.Status, currentPrice, pos.PurchasePrice, pos.StopLoss, pos.Shares, pos.DaysHeld)

	// Check if we should move stop to breakeven (after 3-5 days and profit taken)
	if pos.Status == "HOLDING" && pos.DaysHeld >= 3 {
		if currentPrice > pos.PurchasePrice {
			TakeProfitAndMoveToBreakeven(pos)
		}
	}

	// Check if we hit stop loss
	if currentPrice <= pos.StopLoss {
		StopOut(pos, currentPrice)
		return
	}

	// Check 10 EMA for momentum exit (only in last 10 minutes of trading day)
	if pos.Status == "MONITORING" && isLastTenMinutes() {
		ema10 := calculate10EMA(apiToken, pos.Symbol)
		if ema10 > 0 && currentPrice < ema10 {
			ExitPosition(pos, currentPrice, "10 EMA momentum loss")
		}
	}
}

func fetchCurrentPrice(apiToken, symbol string) float64 {
	// Try realtime endpoint first (for paid plans)
	url := fmt.Sprintf("%s/tickers/%s/realtime?access_key=%s", 
		MARKETSTACK_BASE_URL, symbol, apiToken)
	
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("[%s] error fetching realtime data: %v", symbol, err)
		// Fallback to EOD latest
		return fetchLatestEODPrice(apiToken, symbol)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[%s] error reading response: %v", symbol, err)
		return fetchLatestEODPrice(apiToken, symbol)
	}

	var result MarketstackRealtimeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[%s] error parsing JSON, trying EOD: %v", symbol, err)
		return fetchLatestEODPrice(apiToken, symbol)
	}

	// Use 'last' price if available, otherwise 'close'
	if result.Data.Last > 0 {
		return result.Data.Last
	}
	if result.Data.Close > 0 {
		return result.Data.Close
	}

	return fetchLatestEODPrice(apiToken, symbol)
}

func fetchLatestEODPrice(apiToken, symbol string) float64 {
	url := fmt.Sprintf("%s/eod/latest?access_key=%s&symbols=%s&limit=1",
		MARKETSTACK_BASE_URL, apiToken, symbol)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("[%s] error fetching EOD data: %v", symbol, err)
		return 0
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[%s] error reading EOD response: %v", symbol, err)
		return 0
	}

	var result MarketstackEODResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[%s] error parsing EOD JSON: %v", symbol, err)
		return 0
	}

	if len(result.Data) == 0 || result.Data[0].Close == 0 {
		log.Printf("[%s] no EOD price data available", symbol)
		return 0
	}

	return result.Data[0].Close
}

func TakeProfitAndMoveToBreakeven(pos *Position) {
	fmt.Printf("[%s] 🎯 Taking %.0f%% profit and moving stop to breakeven!\n", 
		pos.Symbol, PROFIT_TAKE_PERCENT*100)

	sharesToSell := pos.Shares * PROFIT_TAKE_PERCENT
	remainingShares := pos.Shares - sharesToSell

	log.Printf("[%s] Selling %.0f shares (%.0f%% of position), keeping %.0f shares",
		pos.Symbol, sharesToSell, PROFIT_TAKE_PERCENT*100, remainingShares)

	// Update position
	newStopLoss := pos.PurchasePrice // Move to breakeven
	newShares := remainingShares
	newStatus := "MONITORING"

	err := UpdatePositionInCSV("watchlist.csv", pos.Symbol, newShares, newStopLoss, pos.DaysHeld, newStatus)
	if err != nil {
		log.Printf("[%s] Error updating CSV: %v", pos.Symbol, err)
	} else {
		log.Printf("[%s] ✅ Stop moved to breakeven at $%.2f, %.0f shares remaining",
			pos.Symbol, newStopLoss, newShares)
	}
}

func StopOut(pos *Position, currentPrice float64) {
	loss := (pos.StopLoss - pos.PurchasePrice) * pos.Shares
	fmt.Printf("[%s] 🛑 STOPPED OUT at $%.2f | Loss: $%.2f\n", 
		pos.Symbol, currentPrice, loss)

	// Remove from watchlist or mark as closed
	err := RemoveFromCSV("watchlist.csv", pos.Symbol)
	if err != nil {
		log.Printf("[%s] Error removing from CSV: %v", pos.Symbol, err)
	}
}

func ExitPosition(pos *Position, currentPrice float64, reason string) {
	profit := (currentPrice - pos.PurchasePrice) * pos.Shares
	fmt.Printf("[%s] 📤 EXITING at $%.2f | Reason: %s | Profit: $%.2f\n",
		pos.Symbol, currentPrice, reason, profit)

	err := RemoveFromCSV("watchlist.csv", pos.Symbol)
	if err != nil {
		log.Printf("[%s] Error removing from CSV: %v", pos.Symbol, err)
	}
}

func calculate10EMA(apiToken, symbol string) float64 {
	// Get intraday data for EMA calculation
	url := fmt.Sprintf("%s/intraday?access_key=%s&symbols=%s&interval=1hour&limit=10",
		MARKETSTACK_BASE_URL, apiToken, symbol)
	
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("[%s] error fetching intraday data: %v", symbol, err)
		return 0
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}

	var result MarketstackIntradayResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[%s] error parsing intraday JSON: %v", symbol, err)
		return 0
	}

	if len(result.Data) < 10 {
		log.Printf("[%s] insufficient data for EMA calculation (got %d periods)", symbol, len(result.Data))
		return 0
	}

	// Get last 10 closing prices
	var closes []float64
	for i := 0; i < 10 && i < len(result.Data); i++ {
		close := result.Data[i].Close
		if close == 0 {
			close = result.Data[i].Last
		}
		if close > 0 {
			closes = append(closes, close)
		}
	}

	if len(closes) < 10 {
		return 0
	}

	// Calculate 10-period EMA
	multiplier := 2.0 / 11.0
	ema := closes[0]
	for i := 1; i < 10; i++ {
		ema = (closes[i] * multiplier) + (ema * (1 - multiplier))
	}

	log.Printf("[%s] 10 EMA: $%.2f", symbol, ema)
	return ema
}

func isLastTenMinutes() bool {
	now := time.Now()
	// Market closes at 4:00 PM ET, check if it's after 3:50 PM
	// Note: Adjust timezone as needed for your location
	closeTime := time.Date(now.Year(), now.Month(), now.Day(), 15, 50, 0, 0, now.Location())
	endTime := time.Date(now.Year(), now.Month(), now.Day(), 16, 0, 0, 0, now.Location())
	
	return now.After(closeTime) && now.Before(endTime)
}

func UpdatePositionInCSV(filePath string, symbol string, newShares, newStopLoss float64, daysHeld int, status string) error {
	file, err := os.Open(filePath)
	if err != nil {
		file, err = os.Open("cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv")
		if err != nil {
			return fmt.Errorf("error opening file: %v", err)
		}
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("error reading CSV: %v", err)
	}

	rowFound := false
	for i, row := range records {
		if len(row) > 0 && row[0] == symbol {
			records[i][2] = fmt.Sprintf("%.2f", newStopLoss)
			records[i][3] = fmt.Sprintf("%.2f", newShares)
			records[i][4] = strconv.Itoa(daysHeld)
			records[i][5] = status
			rowFound = true
			break
		}
	}

	if !rowFound {
		return fmt.Errorf("symbol %s not found", symbol)
	}

	file, err = os.Create(filePath)
	if err != nil {
		file, err = os.Create("cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv")
		if err != nil {
			return fmt.Errorf("error opening file for writing: %v", err)
		}
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	err = writer.WriteAll(records)
	if err != nil {
		return fmt.Errorf("error writing CSV: %v", err)
	}
	writer.Flush()

	return nil
}

func RemoveFromCSV(filePath string, symbol string) error {
	file, err := os.Open(filePath)
	if err != nil {
		file, err = os.Open("cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv")
		if err != nil {
			return fmt.Errorf("error opening file: %v", err)
		}
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("error reading CSV: %v", err)
	}

	var newRecords [][]string
	for _, row := range records {
		if len(row) > 0 && row[0] != symbol {
			newRecords = append(newRecords, row)
		}
	}

	file, err = os.Create(filePath)
	if err != nil {
		file, err = os.Create("cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv")
		if err != nil {
			return fmt.Errorf("error opening file for writing: %v", err)
		}
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	err = writer.WriteAll(newRecords)
	if err != nil {
		return fmt.Errorf("error writing CSV: %v", err)
	}
	writer.Flush()

	return nil
}