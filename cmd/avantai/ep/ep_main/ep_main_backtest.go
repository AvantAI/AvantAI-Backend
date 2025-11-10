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
	InitialRisk string
	Date        string
}

// ===== Watchlist Manager with Mutex Protection =====
type WatchlistManager struct {
	mu       sync.Mutex
	filename string
	entries  map[string]WatchlistEntry // key: "symbol|entryPrice|stopLoss|shares|initialRisk|date"
}

func NewWatchlistManager(filename string) *WatchlistManager {
	wm := &WatchlistManager{
		filename: filename,
		entries:  make(map[string]WatchlistEntry),
	}
	wm.loadExistingEntries()
	return wm
}

func (wm *WatchlistManager) loadExistingEntries() {
	file, err := os.Open(wm.filename)
	if err != nil {
		// File doesn't exist yet, that's okay
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		log.Printf("⚠️  Warning: Failed to read existing watchlist: %v", err)
		return
	}

	startIdx := 0
	if len(records) > 0 && records[0][0] == "stock_symbol" {
		startIdx = 1
	}

	for i := startIdx; i < len(records); i++ {
		if len(records[i]) >= 6 {
			entry := WatchlistEntry{
				StockSymbol: records[i][0],
				EntryPrice:  records[i][1],
				StopLoss:    records[i][2],
				Shares:      records[i][3],
				InitialRisk: records[i][4],
				Date:        records[i][5],
			}
			key := wm.makeKey(entry)
			wm.entries[key] = entry
		}
	}

	fmt.Printf("📋 Loaded %d existing watchlist entries\n", len(wm.entries))
}

func (wm *WatchlistManager) makeKey(entry WatchlistEntry) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s",
		entry.StockSymbol, entry.EntryPrice, entry.StopLoss, entry.Shares, entry.InitialRisk, entry.Date)
}

func (wm *WatchlistManager) AddEntry(entry WatchlistEntry) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	key := wm.makeKey(entry)

	// Check for duplicate
	if _, exists := wm.entries[key]; exists {
		return fmt.Errorf("duplicate entry: %s with entry price %s, stop loss %s, shares %s, initial risk %s already exists in watchlist",
			entry.StockSymbol, entry.EntryPrice, entry.StopLoss, entry.Shares, entry.InitialRisk)
	}

	// Add to in-memory map
	wm.entries[key] = entry

	// Write entire file atomically
	return wm.writeAllEntries()
}

func (wm *WatchlistManager) writeAllEntries() error {
	// Create a temporary file
	tempFile := wm.filename + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	writer := csv.NewWriter(file)

	// Write header
	if err := writer.Write([]string{"stock_symbol", "entry_price", "stop_loss_price", "shares", "initial_risk", "date"}); err != nil {
		file.Close()
		os.Remove(tempFile)
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Write all entries
	for _, entry := range wm.entries {
		record := []string{entry.StockSymbol, entry.EntryPrice, entry.StopLoss, entry.Shares, entry.InitialRisk, entry.Date}
		if err := writer.Write(record); err != nil {
			file.Close()
			os.Remove(tempFile)
			return fmt.Errorf("failed to write CSV record: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		file.Close()
		os.Remove(tempFile)
		return fmt.Errorf("CSV writer error: %w", err)
	}

	file.Close()

	// Atomically replace the old file with the new one
	if err := os.Rename(tempFile, wm.filename); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to replace watchlist file: %w", err)
	}

	return nil
}

// Global watchlist manager
var watchlistManager *WatchlistManager

// ===== Strategy helpers (EP / Opening Range / VWAP / Consolidation) =====

type MinuteBar struct {
	T time.Time
	O float64
	H float64
	L float64
	C float64
	V float64
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
	url := fmt.Sprintf(
		"https://api.marketstack.com/v2/intraday?access_key=%s&symbols=%s&date_from=%s&date_to=%s&interval=1min&limit=1000&sort=ASC",
		apiKey, symbol, dateStr, dateStr,
	)

	resp, err := http.Get(url)
	if err != nil {
		return MarketstackIntradayResponse{}, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return MarketstackIntradayResponse{}, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return MarketstackIntradayResponse{}, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var out MarketstackIntradayResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return MarketstackIntradayResponse{}, fmt.Errorf("unmarshal: %w; body=%s", err, string(body))
	}

	return out, nil
}

// ===== Polygon.io client =====

type PolygonAggregatesResponse struct {
	Ticker       string       `json:"ticker"`
	QueryCount   int          `json:"queryCount"`
	ResultsCount int          `json:"resultsCount"`
	Adjusted     bool         `json:"adjusted"`
	Results      []PolygonBar `json:"results"`
	Status       string       `json:"status"`
	RequestID    string       `json:"request_id"`
}

type PolygonBar struct {
	V  float64 `json:"v"`  // Volume
	VW float64 `json:"vw"` // Volume weighted average price
	O  float64 `json:"o"`  // Open
	C  float64 `json:"c"`  // Close
	H  float64 `json:"h"`  // High
	L  float64 `json:"l"`  // Low
	T  int64   `json:"t"`  // Timestamp (milliseconds)
	N  int     `json:"n"`  // Number of transactions
}

// ===== Rate Limiter =====

type RateLimiter struct {
	mu        sync.Mutex
	maxPerMin int
	callCount int
	lastReset time.Time
}

func NewRateLimiter(maxPerMin int) *RateLimiter {
	return &RateLimiter{
		maxPerMin: maxPerMin,
		callCount: 0,
		lastReset: time.Now(),
	}
}

func (rl *RateLimiter) Wait() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Reset counter every minute
	if time.Since(rl.lastReset) >= time.Minute {
		rl.callCount = 0
		rl.lastReset = time.Now()
	}

	// If we've hit the limit, wait until next minute
	if rl.callCount >= rl.maxPerMin {
		waitTime := time.Until(rl.lastReset.Add(time.Minute))
		fmt.Printf("⚠️  Polygon rate limit reached (%d/%d). Waiting %v...\n",
			rl.callCount, rl.maxPerMin, waitTime)
		time.Sleep(waitTime)
		rl.callCount = 0
		rl.lastReset = time.Now()
	}

	rl.callCount++
}

func fetchPolygonIntraday(apiKey, symbol, dateStr string, rateLimiter *RateLimiter) (PolygonAggregatesResponse, error) {
	rateLimiter.Wait()

	url := fmt.Sprintf(
		"https://api.polygon.io/v2/aggs/ticker/%s/range/1/minute/%s/%s?adjusted=true&sort=asc&limit=50000&apiKey=%s",
		symbol, dateStr, dateStr, apiKey,
	)

	resp, err := http.Get(url)
	if err != nil {
		return PolygonAggregatesResponse{}, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return PolygonAggregatesResponse{}, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return PolygonAggregatesResponse{}, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var out PolygonAggregatesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return PolygonAggregatesResponse{}, fmt.Errorf("unmarshal: %w", err)
	}

	return out, nil
}

func polygonToMinuteBars(resp PolygonAggregatesResponse) []MinuteBar {
	var bars []MinuteBar

	for _, bar := range resp.Results {
		t := time.Unix(bar.T/1000, (bar.T%1000)*1000000).In(locNY)
		bars = append(bars, MinuteBar{
			T: t,
			O: bar.O,
			H: bar.H,
			L: bar.L,
			C: bar.C,
			V: bar.V,
		})
	}

	return bars
}

// ===== Unified Data Fetcher with Fallback =====

func fetchIntradayWithFallback(marketstackKey, polygonKey, symbol, dateStr string, rateLimiter *RateLimiter) ([]MinuteBar, string, error) {
	var bars []MinuteBar

	// Try Marketstack first
	fmt.Printf("🔍 [%s] Trying Marketstack first...\n", symbol)
	msResp, err := fetchIntradayForDate(marketstackKey, symbol, dateStr)
	if err == nil && len(msResp.Data) > 0 {
		fmt.Printf("✅ [%s] Got %d bars from Marketstack\n", symbol, len(msResp.Data))

		for _, d := range msResp.Data {
			tu, err := parseMarketstackTimeUTC(d.Date)
			if err != nil {
				continue
			}
			tNY := tu.In(locNY)
			bars = append(bars, MinuteBar{
				T: tNY,
				O: d.Open,
				H: d.High,
				L: d.Low,
				C: d.Close,
				V: d.Volume,
			})
		}
		return bars, "Marketstack", nil
	}

	// Marketstack failed or returned no data, try Polygon
	if polygonKey == "" {
		return nil, "", fmt.Errorf("no data from Marketstack and no Polygon API key configured")
	}

	fmt.Printf("⚠️  [%s] Marketstack returned no data. Falling back to Polygon...\n", symbol)
	polyResp, err := fetchPolygonIntraday(polygonKey, symbol, dateStr, rateLimiter)
	if err != nil {
		return nil, "", fmt.Errorf("both Marketstack and Polygon failed: %w", err)
	}

	if len(polyResp.Results) == 0 {
		return nil, "", fmt.Errorf("no data available from either Marketstack or Polygon")
	}

	fmt.Printf("✅ [%s] Got %d bars from Polygon (fallback)\n", symbol, len(polyResp.Results))
	bars = polygonToMinuteBars(polyResp)
	return bars, "Polygon", nil
}

// ===== Session utilities =====

var (
	locNY, _ = time.LoadLocation("America/New_York")
)

func sessionWindow(date string) (openNY, closeNY time.Time, err error) {
	d, err := time.ParseInLocation("2006-01-02", date, locNY)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	openNY = time.Date(d.Year(), d.Month(), d.Day(), 9, 30, 0, 0, locNY)
	closeNY = time.Date(d.Year(), d.Month(), d.Day(), 16, 0, 0, 0, locNY)
	return
}

func parseMarketstackTimeUTC(s string) (time.Time, error) {
	return time.Parse("2006-01-02T15:04:05-0700", s)
}

// ===== Backtest structures =====

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
	dir := filepath.Join("responses", symbol)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	filename := filepath.Join(dir, fmt.Sprintf("minute_%d_response.json", minute))
	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	prettified := pretty.Pretty(data)
	return os.WriteFile(filename, prettified, 0644)
}

func main() {
	backtestDatePtr := flag.String("date", "", "a string for the date")
	flag.Parse()
	backtestDate := *backtestDatePtr

	fmt.Printf("=== EP Historical Intraday Collector (Per-Minute Manager Calls) for %s ===\n", backtestDate)
	fmt.Printf("=== Processing will stop after %d minutes ===\n", maxIterations)

	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	marketstackKey := os.Getenv("MARKETSTACK_TOKEN")
	polygonKey := os.Getenv("POLYGON_KEY")

	if marketstackKey == "" {
		log.Fatal("MARKETSTACK_TOKEN not found")
	}

	if polygonKey == "" {
		log.Println("⚠️  WARNING: POLYGON_API_KEY not found. Fallback will not be available.")
	}

	// Initialize global watchlist manager
	watchlistManager = NewWatchlistManager("watchlist.csv")

	// Create rate limiter for Polygon (5 calls/minute on free tier)
	polygonRateLimiter := NewRateLimiter(5)

	// Load symbols from backtest results file
	backtestFileName := fmt.Sprintf("data/backtests/backtest_%s_results.json", strings.ReplaceAll(backtestDate, "-", ""))
	raw, err := os.ReadFile(backtestFileName)
	if err != nil {
		log.Fatalf("open %s: %v", backtestFileName, err)
	}

	var backtestReport BacktestReport
	if err := json.Unmarshal(raw, &backtestReport); err != nil {
		log.Fatalf("unmarshal backtest results: %v", err)
	}

	var backtestResults []BacktestResult
	for _, stock := range backtestReport.QualifyingStocks {
		backtestResults = append(backtestResults, stock)
	}

	fmt.Printf("Loaded %d symbols from backtest results\n", len(backtestResults))

	var symbols []string
	var sentiment []string
	for _, result := range backtestResults {
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
	fmt.Printf("Starting %d historical intraday workers for date %s (Marketstack with Polygon fallback)...\n",
		len(symbols), backtestDate)

	for i, symbol := range symbols {
		wg.Add(1)
		go func(idx int, sym string) {
			defer wg.Done()
			historicalIntradayWorkerWithFallback(marketstackKey, polygonKey, sym, backtestDate,
				sentiment[idx], idx+1, backtestDate, polygonRateLimiter)
		}(i, symbol)
	}
	wg.Wait()
	fmt.Println("All workers finished. Done.")
}

// Historical intraday worker with fallback
func historicalIntradayWorkerWithFallback(marketstackKey, polygonKey, symbol, date string,
	sentiment string, goroutineId int, backtestDate string, rateLimiter *RateLimiter) {

	fmt.Printf("\n[#%d:%s] ============================================\n", goroutineId, symbol)
	fmt.Printf("[#%d:%s] Historical worker started for %s\n", goroutineId, symbol, date)

	openNY, closeNY, err := sessionWindow(date)
	if err != nil {
		log.Printf("[#%d:%s] sessionWindow error: %v", goroutineId, symbol, err)
		return
	}

	// Fetch data with automatic fallback
	allBars, source, err := fetchIntradayWithFallback(marketstackKey, polygonKey, symbol, date, rateLimiter)
	if err != nil {
		log.Printf("[#%d:%s] ❌ Failed to fetch data: %v", goroutineId, symbol, err)
		return
	}

	fmt.Printf("[#%d:%s] 📊 Using data from: %s\n", goroutineId, symbol, source)

	// Filter to regular session hours
	var bars []MinuteBar
	for _, bar := range allBars {
		if bar.T.Before(openNY) || bar.T.After(closeNY) {
			continue
		}
		bars = append(bars, bar)
	}

	if len(bars) == 0 {
		fmt.Printf("[#%d:%s] no bars within session hours (%s to %s) for %s\n",
			goroutineId, symbol, openNY.Format("15:04"), closeNY.Format("15:04"), date)
		return
	}

	sortMinuteBarsAsc(bars)
	fmt.Printf("[#%d:%s] ✓ Filtered to %d bars within session hours\n", goroutineId, symbol, len(bars))

	// Process minute by minute
	processMinuteByMinute(bars, symbol, sentiment, goroutineId, backtestDate)

	fmt.Printf("[#%d:%s] ✅ Completed processing all minutes\n", goroutineId, symbol)
	fmt.Printf("[#%d:%s] ============================================\n\n", goroutineId, symbol)
}

// Process bars minute by minute, calling manager agent after each minute
func processMinuteByMinute(allBars []MinuteBar, symbol string, sentiment string, goroutineId int, backtestDate string) {
	fmt.Printf("[#%d:%s] Starting minute-by-minute processing (limited to %d minutes or until Buy recommendation)\n",
		goroutineId, symbol, maxIterations)

	maxMinutes := len(allBars)
	if maxMinutes > maxIterations {
		maxMinutes = maxIterations
	}

	for i := 1; i <= maxMinutes; i++ {
		currentBars := allBars[:i]
		currentTime := currentBars[i-1].T

		state := StrategyState{}
		state.Recompute(currentBars)

		fmt.Printf("[#%d:%s] Minute %d/%d (%s): OR5[%.2f/%.2f] OR15[%.2f/%.2f] VWAP=%.3f Tight5=%t Tight10=%t\n",
			goroutineId, symbol, i, maxMinutes, currentTime.Format("15:04"),
			state.OR5High, state.OR5Low, state.OR15High, state.OR15Low,
			state.VWAP, state.IsTight5, state.IsTight10,
		)

		epBars := convertToEP(symbol, currentBars)
		shouldStop := runManagerAgent(epBars, symbol, goroutineId, i, sentiment, maxMinutes, backtestDate)

		if shouldStop {
			fmt.Printf("[#%d:%s] 🛑 Stopping processing after minute %d due to Buy recommendation\n",
				goroutineId, symbol, i)
			break
		}
	}

	fmt.Printf("[#%d:%s] ✓ Completed minute-by-minute processing\n", goroutineId, symbol)
}

func sortMinuteBarsAsc(b []MinuteBar) {
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
			Symbol:        symbol,
			Timestamp:     b.T.Format("2006-01-02 15:04:00"),
			Open:          b.O,
			High:          b.H,
			Low:           b.L,
			Close:         b.C,
			Volume:        int64(b.V),
			PreviousClose: prevClose,
		}

		out = append(out, s)
		prevClose = b.C
	}
	return out
}

// ===== Manager Agent Integration =====

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
		if i < 3 || i == len(stockdata)-1 {
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

	resp, err := sapien.ManagerAgentReqInfo(stock_data, string(news), string(earnings), sentiment)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ ManagerAgentReqInfo error (minute %d): %v\n", goroutineId, currentMinute, err)
		return false
	}
	fmt.Printf("[Goroutine %d] ✓ ManagerAgentReqInfo completed for minute %d. Response (%d chars): %s\n",
		goroutineId, currentMinute, len(resp.Response), resp.Response)

	// Extract JSON from response
	managerResp, err := extractJSON(resp.Response)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Failed to extract JSON from response (minute %d): %v\n",
			goroutineId, currentMinute, err)
		return false
	}

	fmt.Println("Response:", resp.Response)

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

	accSize, _ := strconv.ParseFloat(os.Getenv("ACCOUNT_SIZE"), 64)
	riskPerTrade, _ := strconv.ParseFloat(os.Getenv("RISK_PER_TRADE"), 64)
	entryPrice, _ := strconv.ParseFloat(strings.ReplaceAll(managerResp.EntryPrice, "$", ""), 64)
	stopLoss, _ := strconv.ParseFloat(strings.ReplaceAll(managerResp.StopLoss, "$", ""), 64)
	initialRisk, _ := strconv.ParseFloat(strings.ReplaceAll(managerResp.RiskPercent, "%", ""), 64)

	fmt.Println("Shares (rounded): ", math.Round((((riskPercent)*(riskPerTrade*accSize))*1.0)/(entryPrice-stopLoss)))
	fmt.Println("Shares: ", (((riskPercent)*(riskPerTrade*accSize))*1.0)/(entryPrice-stopLoss))

	if strings.ToLower(strings.TrimSpace(managerResp.Recommendation)) == "buy" {
		if managerResp.EntryPrice != "" && managerResp.StopLoss != "" {
			shares := math.Round(((riskPerTrade * accSize) * 1.0) / (entryPrice - stopLoss))

			entry := WatchlistEntry{
				StockSymbol: symbol,
				EntryPrice:  managerResp.EntryPrice,
				StopLoss:    managerResp.StopLoss,
				Shares:      strconv.FormatFloat(shares, 'f', 0, 64),
				InitialRisk: strconv.FormatFloat(initialRisk, 'f', 2, 64),
				Date:        backtestDate,
			}

			// Use the thread-safe watchlist manager
			if err := watchlistManager.AddEntry(entry); err != nil {
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

			return true
		} else {
			fmt.Printf("[Goroutine %d] ⚠️ Buy recommendation for %s but missing entry price or stop loss (minute %d)\n",
				goroutineId, symbol, currentMinute)
			return false
		}
	} else {
		fmt.Printf("[Goroutine %d] 📊 Recommendation for %s: %s (minute %d)\n",
			goroutineId, symbol, managerResp.Recommendation, currentMinute)
		return false
	}
}
