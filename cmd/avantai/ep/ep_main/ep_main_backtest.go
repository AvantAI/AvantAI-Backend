package main

import (
	"avantai/pkg/ep"
	"avantai/pkg/sapien"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/tidwall/pretty"
)

// ===== Configuration =====
const maxIterations = 15 // Stop after 15 minutes

// ===== Manager Agent Response Structure =====
type ManagerResponse struct {
	Recommendation string `json:"Recommendation"`
	EntryTime      string `json:"Entry Time,omitempty"`
	EntryPrice     string `json:"Entry Price,omitempty"`
	StopLoss       string `json:"Stop-Loss,omitempty"`
	RiskPercent    string `json:"Risk %,omitempty"`
	Reasoning      string `json:"Reasoning"`
}

// ===== Watchlist CSV Structure =====
type WatchlistEntry struct {
	StockSymbol string
	EntryPrice  string
	StopLoss    string
	Shares      string
	Date        string
}

// ===== Strategy helpers (EP / Opening Range / VWAP / Consolidation) =====

type MinuteBar struct {
	T time.Time
	O float64
	H float64
	L float64
	C float64
	V int64
}

type StrategyState struct {
	// Opening Range levels
	OR5High  float64
	OR5Low   float64
	OR15High float64
	OR15Low  float64

	// VWAP running components
	cumPV float64 // Σ(typicalPrice * volume)
	cumV  float64 // Σ(volume)
	VWAP  float64

	// Simple consolidation flags
	IsTight5   bool // last ~5 bars within a tight range
	IsTight10  bool // last ~10 bars within a tight range
	LastUpdate time.Time
}

// Updates VWAP and opening range values given all minute bars since open.
func (s *StrategyState) Recompute(bars []MinuteBar) {
	n := len(bars)
	if n == 0 {
		return
	}

	// Opening range 5 and 15 minutes (from regular session start)
	limit := func(x, max int) int {
		if x > max {
			return max
		}
		return x
	}
	or5 := bars[:limit(n, 5)]
	or15 := bars[:limit(n, 15)]

	s.OR5High, s.OR5Low = rangeHL(or5)
	s.OR15High, s.OR15Low = rangeHL(or15)

	// VWAP (session)
	var pv float64
	var vSum float64
	for _, b := range bars {
		typ := (b.H + b.L + b.C) / 3.0
		pv += typ * float64(b.V)
		vSum += float64(b.V)
	}
	s.cumPV = pv
	s.cumV = vSum
	if s.cumV > 0 {
		s.VWAP = s.cumPV / s.cumV
	}

	// Tight consolidations: check last 5 and 10 bars band% of VWAP
	s.IsTight5 = isTight(bars, 5, s.VWAP, 0.004)   // ~0.4% band
	s.IsTight10 = isTight(bars, 10, s.VWAP, 0.006) // ~0.6% band

	s.LastUpdate = bars[n-1].T
}

func rangeHL(bars []MinuteBar) (hi, lo float64) {
	if len(bars) == 0 {
		return 0, 0
	}
	hi = bars[0].H
	lo = bars[0].L
	for _, b := range bars {
		if b.H > hi {
			hi = b.H
		}
		if b.L < lo {
			lo = b.L
		}
	}
	return
}

func isTight(all []MinuteBar, lookback int, ref float64, band float64) bool {
	n := len(all)
	if n < lookback {
		return false
	}
	seg := all[n-lookback:]
	hi, lo := rangeHL(seg)
	if ref == 0 {
		// Fallback: use segment mid
		ref = (hi + lo) / 2
		if ref == 0 {
			return false
		}
	}
	width := (hi - lo) / ref
	return width >= 0 && width <= band
}

// ===== Marketstack client =====

type MarketstackIntradayResponse struct {
	Pagination struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Count  int `json:"count"`
		Total  int `json:"total"`
	} `json:"pagination"`
	Data []struct {
		Open   float64 `json:"open"`
		High   float64 `json:"high"`
		Low    float64 `json:"low"`
		Close  float64 `json:"close"`
		Volume float64 `json:"volume"`
		Date   string  `json:"date"` // e.g. "2024-04-01T13:31:00+0000" (UTC)
		Symbol string  `json:"symbol"`
	} `json:"data"`
}

func fetchIntradayForDate(apiKey, symbol, dateStr string) (MarketstackIntradayResponse, error) {
	// Use the /intraday/[date] endpoint format for specific historical dates
	url := fmt.Sprintf(
		"https://api.marketstack.com/v2/intraday/%s?access_key=%s&symbols=%s&interval=1min&limit=1000&sort=ASC",
		dateStr, apiKey, symbol,
	)

	fmt.Printf("Making API request: %s\n", url)
	resp, err := http.Get(url)
	if err != nil {
		return MarketstackIntradayResponse{}, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return MarketstackIntradayResponse{}, fmt.Errorf("read body: %w", err)
	}

	fmt.Printf("API Response status: %s, Body length: %d\n", resp.Status, len(body))
	if resp.StatusCode != 200 {
		return MarketstackIntradayResponse{}, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var out MarketstackIntradayResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return MarketstackIntradayResponse{}, fmt.Errorf("unmarshal: %w; body=%s", err, string(body))
	}
	return out, nil
}

// ===== Session utilities =====

var (
	locNY, _ = time.LoadLocation("America/New_York")
)

// Returns regular session open/close for a given YYYY-MM-DD (New York time).
func sessionWindow(date string) (openNY, closeNY time.Time, err error) {
	d, err := time.ParseInLocation("2006-01-02", date, locNY)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	openNY = time.Date(d.Year(), d.Month(), d.Day(), 9, 30, 0, 0, locNY)
	closeNY = time.Date(d.Year(), d.Month(), d.Day(), 16, 0, 0, 0, locNY)
	return
}

func toISOUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05")
}

func parseMarketstackTimeUTC(s string) (time.Time, error) {
	// Marketstack returns e.g. "2025-08-29T13:31:00+0000"
	return time.Parse("2006-01-02T15:04:05-0700", s)
}

// ===== App wiring =====

// Top-level structure that matches your JSON
type BacktestReport struct {
	BacktestConfig   BacktestConfig   `json:"backtest_config"`
	BacktestDate     string           `json:"backtest_date"`
	BacktestSummary  BacktestSummary  `json:"backtest_summary"`
	FilterCriteria   FilterCriteria   `json:"filter_criteria"`
	GeneratedAt      string           `json:"generated_at"`
	QualifyingStocks []BacktestResult `json:"qualifying_stocks"`
}

type BacktestConfig struct {
	LookbackDays int    `json:"lookback_days"`
	TargetDate   string `json:"target_date"`
}

type BacktestSummary struct {
	AvgGapUp                float64        `json:"avg_gap_up"`
	AvgMarketCap            float64        `json:"avg_market_cap"`
	DataQualityDistribution map[string]int `json:"data_quality_distribution"`
	TotalCandidates         int            `json:"total_candidates"`
}

type FilterCriteria struct {
	MaxExtensionAdr         float64 `json:"max_extension_adr"`
	MinDollarVolume         int64   `json:"min_dollar_volume"`
	MinGapUpPercent         float64 `json:"min_gap_up_percent"`
	MinMarketCap            int64   `json:"min_market_cap"`
	MinPremarketVolumeRatio float64 `json:"min_premarket_volume_ratio"`
}

type StockDataList struct {
	Symbol    string         `json:"symbol"`
	StockData []ep.StockData `json:"stock_data"`
}

type StockStats struct {
	Timestamp                string  `json:"timestamp"`
	MarketCap                float64 `json:"market_cap"`
	Dolvol                   float64 `json:"dolvol"`
	GapUp                    float64 `json:"gap_up"`
	Adr                      float64 `json:"adr"`
	Name                     string  `json:"name"`
	Exchange                 string  `json:"exchange"`
	Sector                   string  `json:"sector"`
	Industry                 string  `json:"industry"`
	PremarketVolume          float64 `json:"premarket_volume"`
	AvgPremarketVolume       float64 `json:"avg_premarket_volume"`
	PremarketVolumeRatio     float64 `json:"premarket_volume_ratio"`
	PremarketVolPercent      int     `json:"premarket_vol_percent"`
	Sma200                   float64 `json:"sma_200"`
	Ema200                   float64 `json:"ema_200"`
	Ema50                    float64 `json:"ema_50"`
	Ema20                    float64 `json:"ema_20"`
	Ema10                    float64 `json:"ema_10"`
	IsAbove200Ema            bool    `json:"is_above_200_ema"`
	DistanceFrom50Ema        float64 `json:"distance_from_50_ema"`
	IsExtended               bool    `json:"is_extended"`
	IsTooExtended            bool    `json:"is_too_extended"`
	VolumeDriedUp            bool    `json:"volume_dried_up"`
	IsNearEma1020            bool    `json:"is_near_ema_10_20"`
	BreaksResistance         bool    `json:"breaks_resistance"`
	PreviousEarningsReaction string  `json:"previous_earnings_reaction"`
}

type FilteredStock struct {
	Symbol    string     `json:"symbol"`
	StockInfo StockStats `json:"stock_info"`
}

type BacktestResult struct {
	FilteredStock
	BacktestDate    string   `json:"backtest_date"`
	DataQuality     string   `json:"data_quality"`
	HistoricalDays  int      `json:"historical_days_available"`
	ValidationNotes []string `json:"validation_notes"`
}

// ===== JSON Extraction and CSV Management =====

func extractJSON(response string) (*ManagerResponse, error) {
	// Look for JSON pattern in the response
	jsonPattern := regexp.MustCompile(`\{[^{}]*"Recommendation"[^{}]*\}`)
	match := jsonPattern.FindString(response)

	if match == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var managerResp ManagerResponse
	err := json.Unmarshal([]byte(match), &managerResp)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return &managerResp, nil
}

func saveJSONResponse(symbol string, minute int, response *ManagerResponse) error {
	// Create directory if it doesn't exist
	dir := filepath.Join("responses", symbol)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Save JSON file with prettified formatting
	filename := filepath.Join(dir, fmt.Sprintf("minute_%d_response.json", minute))
	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	// Prettify the JSON
	prettified := pretty.Pretty(data)

	return os.WriteFile(filename, prettified, 0644)
}

func addToWatchlist(entry WatchlistEntry) error {
	filename := "watchlist.csv"

	// Read existing entries to check for duplicates
	existingEntries := make(map[string]WatchlistEntry)
	if file, err := os.Open(filename); err == nil {
		defer file.Close()
		reader := csv.NewReader(file)
		records, err := reader.ReadAll()
		if err != nil {
			return fmt.Errorf("failed to read existing watchlist: %w", err)
		}

		// Skip header row if it exists
		startIdx := 0
		if len(records) > 0 && records[0][0] == "stock_symbol" {
			startIdx = 1
		}

		// Build map of existing entries
		for i := startIdx; i < len(records); i++ {
			if len(records[i]) >= 5 {
				key := fmt.Sprintf("%s|%s|%s|%s|%s", records[i][0], records[i][1], records[i][2], records[i][3], records[i][4])
				existingEntries[key] = WatchlistEntry{
					StockSymbol: records[i][0],
					EntryPrice:  records[i][1],
					StopLoss:    records[i][2],
					Shares:      records[i][3],
					Date:        records[i][4],
				}
			}
		}
	}

	// Check if this entry already exists
	entryKey := fmt.Sprintf("%s|%s|%s|%s|%s", entry.StockSymbol, entry.EntryPrice, entry.StopLoss, entry.Shares, entry.Date)
	if _, exists := existingEntries[entryKey]; exists {
		return fmt.Errorf("duplicate entry: %s with entry price %s, stop loss %s, shares %s already exists in watchlist",
			entry.StockSymbol, entry.EntryPrice, entry.StopLoss, entry.Shares)
	}

	// Check if file exists
	fileExists := true
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		fileExists = false
	}

	// Open file for append or create
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open watchlist file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header if file is new
	if !fileExists {
		if err := writer.Write([]string{"stock_symbol", "entry_price", "stop_loss_price", "shares", "date"}); err != nil {
			return fmt.Errorf("failed to write CSV header: %w", err)
		}
	}

	// Write the entry
	record := []string{entry.StockSymbol, entry.EntryPrice, entry.StopLoss, entry.Shares, entry.Date}
	if err := writer.Write(record); err != nil {
		return fmt.Errorf("failed to write CSV record: %w", err)
	}

	return nil
}

func main() {
	backtestDate := *flag.String("date", "2025-08-01", "a string for the date") // YYYY-MM-DD format - change this to your desired date (must be historical, not future)
	flag.Parse()

	fmt.Printf("=== EP Historical Intraday Collector (Per-Minute Manager Calls) for %s ===\n", backtestDate)
	fmt.Printf("=== Processing will stop after %d minutes ===\n", maxIterations)

	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}
	apiKey := os.Getenv("MARKETSTACK_TOKEN")
	if apiKey == "" {
		log.Fatal("MARKETSTACK_TOKEN not found")
	}

	// Load symbols from backtest results file
	backtestFileName := fmt.Sprintf("data/backtests/backtest_%s_results.json", strings.ReplaceAll(backtestDate, "-", ""))
	raw, err := os.ReadFile(backtestFileName)
	if err != nil {
		log.Fatalf("open %s: %v", backtestFileName, err)
	}

	// Slice to hold the parsed data
	var backtestReport BacktestReport
	if err := json.Unmarshal(raw, &backtestReport); err != nil {
		log.Fatalf("unmarshal backtest results: %v", err)
	}

	var backtestResults []BacktestResult
	for _, stock := range backtestReport.QualifyingStocks {
		backtestResults = append(backtestResults, stock)
	}

	fmt.Printf("Loaded %d symbols from backtest results\n", len(backtestResults))

	// Extract symbols for the specific backtest date
	var symbols []string
	var sentiment []string
	for _, result := range backtestResults {
		// Create sentiment string in the desired format
		sentimentData := map[string]interface{}{
			"Stock_name": result.Symbol,
			"Stock_info": result.FilteredStock.StockInfo,
		}
		sentimentJSON, err := json.Marshal(sentimentData)
		if err != nil {
			log.Printf("Error marshaling sentiment for %s: %v", result.Symbol, err)
			continue
		}
		sentiment = append(sentiment, string(sentimentJSON))
		symbols = append(symbols, result.Symbol)
	}

	var wg sync.WaitGroup
	fmt.Printf("Starting %d historical intraday workers for date %s…\n", len(symbols), backtestDate)
	for i, symbol := range symbols {
		wg.Add(1)
		go func(idx int, sym string) {
			defer wg.Done()
			historicalIntradayWorker(apiKey, sym, backtestDate, sentiment[idx], idx+1, backtestDate)
		}(i, symbol)
	}
	wg.Wait()
	fmt.Println("All workers finished. Done.")
}

// Historical intraday worker: fetches complete day's data and processes each minute sequentially
func historicalIntradayWorker(apiKey, symbol, date string, sentiment string, goroutineId int, backtestDate string) {
	fmt.Printf("[#%d:%s] historical worker started for %s\n", goroutineId, symbol, date)

	openNY, closeNY, err := sessionWindow(date)
	if err != nil {
		log.Printf("[#%d:%s] sessionWindow error: %v", goroutineId, symbol, err)
		return
	}

	fmt.Printf("[#%d:%s] fetching historical data for date %s (session: %s to %s NY time)\n",
		goroutineId, symbol, date, openNY.Format("15:04"), closeNY.Format("15:04"))

	// Use the new date-specific endpoint
	resp, err := fetchIntradayForDate(apiKey, symbol, date)
	if err != nil {
		log.Printf("[#%d:%s] fetchIntradayForDate error: %v", goroutineId, symbol, err)
		return
	}
	if len(resp.Data) == 0 {
		fmt.Printf("[#%d:%s] no intraday data found for %s\n", goroutineId, symbol, date)
		return
	}

	fmt.Printf("[#%d:%s] received %d data points from API\n", goroutineId, symbol, len(resp.Data))

	// Transform + filter to regular session minutes in NY time
	var bars []MinuteBar
	for _, d := range resp.Data {
		tu, err := parseMarketstackTimeUTC(d.Date)
		if err != nil {
			fmt.Printf("[#%d:%s] error parsing time %s: %v\n", goroutineId, symbol, d.Date, err)
			continue
		}
		tNY := tu.In(locNY)
		// keep only [open, close] during regular session hours
		if tNY.Before(openNY) || tNY.After(closeNY) {
			continue
		}
		bars = append(bars, MinuteBar{
			T: tNY, O: d.Open, H: d.High, L: d.Low, C: d.Close, V: int64(d.Volume),
		})
	}

	if len(bars) == 0 {
		fmt.Printf("[#%d:%s] no bars within session hours (%s to %s) for %s\n",
			goroutineId, symbol, openNY.Format("15:04"), closeNY.Format("15:04"), date)
		return
	}

	// Sort ascending by time (API should return in ASC order but let's be safe)
	sortMinuteBarsAsc(bars)

	fmt.Printf("[#%d:%s] filtered to %d bars within session hours\n", goroutineId, symbol, len(bars))

	// Process each minute sequentially, calling manager agent after each bar
	processMinuteByMinute(bars, symbol, sentiment, goroutineId, backtestDate)

	fmt.Printf("[#%d:%s] completed processing all minutes for %s\n", goroutineId, symbol, date)
}

// Process bars minute by minute, calling manager agent after each minute
func processMinuteByMinute(allBars []MinuteBar, symbol string, sentiment string, goroutineId int, backtestDate string) {
	fmt.Printf("[#%d:%s] Starting minute-by-minute processing (limited to %d minutes or until Buy recommendation)\n",
		goroutineId, symbol, maxIterations)

	// Limit iterations to maxIterations
	maxMinutes := len(allBars)
	if maxMinutes > maxIterations {
		maxMinutes = maxIterations
	}

	for i := 1; i <= maxMinutes; i++ {
		// Get bars up to current minute (inclusive)
		currentBars := allBars[:i]
		currentTime := currentBars[i-1].T

		// Compute strategy state up to this point
		state := StrategyState{}
		state.Recompute(currentBars)

		fmt.Printf("[#%d:%s] Minute %d/%d (%s): OR5[%.2f/%.2f] OR15[%.2f/%.2f] VWAP=%.3f Tight5=%t Tight10=%t\n",
			goroutineId, symbol, i, maxMinutes, currentTime.Format("15:04"),
			state.OR5High, state.OR5Low, state.OR15High, state.OR15Low,
			state.VWAP, state.IsTight5, state.IsTight10,
		)

		// Convert current bars to EP format
		epBars := convertToEP(symbol, currentBars)

		// Call manager agent with data up to this minute (synchronously within this stock's goroutine)
		// runManagerAgent now returns true if we should stop processing (Buy recommendation)
		shouldStop := runManagerAgent(epBars, symbol, goroutineId, i, sentiment, maxMinutes, backtestDate)

		if shouldStop {
			fmt.Printf("[#%d:%s] 🛑 Stopping processing after minute %d due to Buy recommendation\n",
				goroutineId, symbol, i)
			break
		}

		// Optional: Add a small delay to simulate real-time processing
		// time.Sleep(100 * time.Millisecond)
	}

	fmt.Printf("[#%d:%s] ✓ Completed minute-by-minute processing\n",
		goroutineId, symbol)
}

func sortMinuteBarsAsc(b []MinuteBar) {
	// simple insertion sort (n is tiny per pull); replace with sort.Slice if you prefer
	for i := 1; i < len(b); i++ {
		j := i
		for j > 0 && b[j-1].T.After(b[j].T) {
			b[j-1], b[j] = b[j], b[j-1]
			j--
		}
	}
}

func convertToEP(symbol string, bars []MinuteBar) []ep.StockData {
	out := make([]ep.StockData, 0, len(bars))
	var prevClose float64
	for i, b := range bars {
		if i == 0 {
			prevClose = b.O
		}
		s := ep.StockData{
			Symbol:    symbol,
			Timestamp: b.T.Format("2006-01-02 15:04:00"),
			Open:      b.O,
			High:      b.H,
			Low:       b.L,
			Close:     b.C,
			Volume:    b.V,
		}
		// If your ep.StockData has PreviousClose, populate it (keeps your runManagerAgent logs consistent).
		// If not, this no-op won't compile, so comment it out.
		s.PreviousClose = prevClose

		out = append(out, s)
		prevClose = b.C
	}
	return out
}

// ===== Modified runManagerAgent with JSON processing and watchlist management =====

func runManagerAgent(stockdata []ep.StockData, symbol string, goroutineId int, currentMinute int, sentiment string, totalMinutes int, backtestDate string) bool {
	fmt.Printf("\n[Goroutine %d] --- Starting runManagerAgent for %s (minute %d/%d) ---\n",
		goroutineId, symbol, currentMinute, totalMinutes)
	defer fmt.Printf("[Goroutine %d] ✓ runManagerAgent completed for %s (minute %d/%d)\n",
		goroutineId, symbol, currentMinute, totalMinutes)

	fmt.Printf("[Goroutine %d] Processing %d stock data points for %s (up to minute %d)\n",
		goroutineId, len(stockdata), symbol, currentMinute)
	stock_data := ""

	for i, stockdataPoint := range stockdata {
		stock_data += fmt.Sprintf("%d min - Open: %v Close: %v High: %v Low: %v\n",
			i+1, stockdataPoint.Open, stockdataPoint.PreviousClose, stockdataPoint.High, stockdataPoint.Low)
		if i < 3 || i == len(stockdata)-1 { // Show first 3 and last data point
			fmt.Printf("[Goroutine %d]   Data %d: O:%.2f C:%.2f H:%.2f L:%.2f\n",
				goroutineId, i+1, stockdataPoint.Open, stockdataPoint.PreviousClose, stockdataPoint.High, stockdataPoint.Low)
		}
	}
	fmt.Printf("[Goroutine %d] ✓ Stock data formatted (%d characters) for minute %d\n",
		goroutineId, len(stock_data), currentMinute)

	dataDir := "reports"
	stockDir := filepath.Join(dataDir, symbol)

	// Read news
	newsFilePath := filepath.Join(stockDir, "news_report.txt")
	file, err := os.Open(newsFilePath)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Error opening news report file: %v\n", goroutineId, err)
	} else {
		defer file.Close()
	}
	var news []byte
	if file != nil {
		news, err = io.ReadAll(file)
		if err != nil {
			fmt.Printf("[Goroutine %d] ❌ Error reading news file: %v\n", goroutineId, err)
		} else {
			fmt.Printf("[Goroutine %d] ✓ News report read (%d bytes)\n", goroutineId, len(news))
		}
	}

	// Read earnings
	earningsFilePath := filepath.Join(stockDir, "earnings_report.txt")
	file, err = os.Open(earningsFilePath)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Error opening earnings report file: %v\n", goroutineId, err)
	} else {
		defer file.Close()
	}
	var earnings []byte
	if file != nil {
		earnings, err = io.ReadAll(file)
		if err != nil {
			fmt.Printf("[Goroutine %d] ❌ Error reading earnings file: %v\n", goroutineId, err)
		} else {
			fmt.Printf("[Goroutine %d] ✓ Earnings report read (%d bytes)\n", goroutineId, len(earnings))
		}
	}

	fmt.Printf("[Goroutine %d] Calling ManagerAgentReqInfo for %s (minute %d/%d)...\n",
		goroutineId, symbol, currentMinute, totalMinutes)

	// sentiment is already a JSON string, so we can pass it directly
	resp, err := sapien.ManagerAgentReqInfo(stock_data, string(news), string(earnings), sentiment)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ ManagerAgentReqInfo error (minute %d): %v\n", goroutineId, currentMinute, err)
		return false // Don't stop processing on API errors
	}
	fmt.Printf("[Goroutine %d] ✓ ManagerAgentReqInfo completed for minute %d. Response (%d chars): %s\n",
		goroutineId, currentMinute, len(resp.Response), resp.Response)

	// Extract JSON from response
	managerResp, err := extractJSON(resp.Response)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Failed to extract JSON from response (minute %d): %v\n",
			goroutineId, currentMinute, err)
		return false // Don't stop processing on JSON extraction errors
	}

	// Save JSON response to file
	if err := saveJSONResponse(symbol, currentMinute, managerResp); err != nil {
		fmt.Printf("[Goroutine %d] ❌ Failed to save JSON response (minute %d): %v\n",
			goroutineId, currentMinute, err)
	} else {
		fmt.Printf("[Goroutine %d] ✓ JSON response saved for minute %d\n", goroutineId, currentMinute)
	}

	// Check if recommendation is "Buy" and add to watchlist
	stringPercent := strings.TrimSpace(strings.TrimSuffix(managerResp.RiskPercent, "%"))
	riskPercent, _ := strconv.ParseFloat(stringPercent, 64)

	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	accSize, _ := strconv.ParseFloat(os.Getenv("ACCOUNT_SIZE"), 64)
	entryPrice, _ := strconv.ParseFloat(strings.ReplaceAll(managerResp.EntryPrice, "$", ""), 64)
	fmt.Println("Entry Price: ", entryPrice)
	stopLoss, _ := strconv.ParseFloat(strings.ReplaceAll(managerResp.StopLoss, "$", ""), 64)
	fmt.Println("Stop Loss: ", stopLoss)

	fmt.Println("Shares: ", (((riskPercent/100)*accSize)*1.0)/(entryPrice-stopLoss))
	fmt.Println("EntryPrice - StopLoss: ", (entryPrice - stopLoss))

	if strings.ToLower(strings.TrimSpace(managerResp.Recommendation)) == "buy" {
		// Only add to watchlist if we have entry price and stop loss
		if managerResp.EntryPrice != "" && managerResp.StopLoss != "" {
			entry := WatchlistEntry{
				StockSymbol: symbol,
				EntryPrice:  managerResp.EntryPrice,
				StopLoss:    managerResp.StopLoss,
				Shares:      strconv.FormatFloat(float64(int(math.Round((((riskPercent/100)*accSize)*1.0)/(entryPrice-stopLoss)))), 'f', 2, 64),
				Date:        backtestDate,
			}

			if err := addToWatchlist(entry); err != nil {
				if strings.Contains(err.Error(), "duplicate entry") {
					fmt.Printf("[Goroutine %d] ⚠️ Duplicate watchlist entry for %s with entry price %s and stop loss %s (minute %d)\n",
						goroutineId, symbol, entry.EntryPrice, entry.StopLoss, currentMinute)
				} else {
					fmt.Printf("[Goroutine %d] ❌ Failed to add %s to watchlist (minute %d): %v\n",
						goroutineId, symbol, currentMinute, err)
				}
			} else {
				fmt.Printf("[Goroutine %d] ✅ Added %s to watchlist with entry price %s and stop loss %s (minute %d)\n",
					goroutineId, symbol, entry.EntryPrice, entry.StopLoss, currentMinute)
			}

			// Return true to signal that processing should stop for this stock
			return true
		} else {
			fmt.Printf("[Goroutine %d] ⚠️ Buy recommendation for %s but missing entry price or stop loss (minute %d)\n",
				goroutineId, symbol, currentMinute)
			// Continue processing even if Buy recommendation is incomplete
			return false
		}
	} else {
		fmt.Printf("[Goroutine %d] 📊 Recommendation for %s: %s (minute %d)\n",
			goroutineId, symbol, managerResp.Recommendation, currentMinute)
		// Continue processing for non-Buy recommendations
		return false
	}
}
