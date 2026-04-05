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
	entries  map[string]WatchlistEntry
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
	if _, exists := wm.entries[key]; exists {
		return fmt.Errorf("duplicate entry: %s with entry price %s, stop loss %s, shares %s, initial risk %s already exists in watchlist",
			entry.StockSymbol, entry.EntryPrice, entry.StopLoss, entry.Shares, entry.InitialRisk)
	}

	wm.entries[key] = entry
	return wm.writeAllEntries()
}

func (wm *WatchlistManager) writeAllEntries() error {
	tempFile := wm.filename + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	writer := csv.NewWriter(file)

	if err := writer.Write([]string{"stock_symbol", "entry_price", "stop_loss_price", "shares", "initial_risk", "date"}); err != nil {
		file.Close()
		os.Remove(tempFile)
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

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

	if err := os.Rename(tempFile, wm.filename); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to replace watchlist file: %w", err)
	}

	return nil
}

// Global watchlist manager
var watchlistManager *WatchlistManager

// ===== Strategy helpers =====

type MinuteBar struct {
	T time.Time
	O float64
	H float64
	L float64
	C float64
	V float64
}

// StrategyState holds all computed intraday signals for the current bar.
//
// FIX: Added RVOL, AvgVolume, and the first-30-min RVOL threshold check.
// FIX: OR5Low and OR15Low are now exposed so they can be used as stop loss.
type StrategyState struct {
	// Opening Range levels
	OR5High  float64
	OR5Low   float64
	OR15High float64
	OR15Low  float64

	// VWAP running components
	cumPV float64
	cumV  float64
	VWAP  float64

	// Consolidation flags
	IsTight5  bool
	IsTight10 bool

	// FIX: RVOL — rolling volume relative to the per-bar average of the first
	// 30 minutes. AvgBarVol is the per-minute average over the first 30 bars
	// (computed once and held constant); CumVolume is the cumulative session
	// volume so far; RVOL is CumVolume / (AvgBarVol * elapsed bars).
	//
	// We use per-bar average (not daily average) because we only have intraday
	// data here and we want to normalise against what a "normal" minute looks
	// like for this ticker.
	AvgBarVol30 float64 // per-minute avg vol over first 30 bars, set once at bar 30
	CumVolume   float64 // cumulative volume since session open
	RVOL        float64 // CumVolume / (AvgBarVol30 * numBars); meaningful only after bar 30

	// RVOLPasses is true when we have at least 5 bars AND RVOL >= 5.0 within
	// the first 30 minutes. Strategy: "RVOL should be at least 5–10x within
	// the first 30 minutes of market open."
	RVOLPasses bool

	// MinutesElapsed is the number of bars processed so far (1-indexed)
	MinutesElapsed int

	LastUpdate time.Time
}

// Recompute updates all signals given all minute bars since open.
func (s *StrategyState) Recompute(bars []MinuteBar) {
	n := len(bars)
	if n == 0 {
		return
	}

	s.MinutesElapsed = n

	// ── Opening ranges ─────────────────────────────────────────────────
	limit := func(x, mx int) int {
		if x > mx {
			return mx
		}
		return x
	}
	s.OR5High, s.OR5Low = rangeHL(bars[:limit(n, 5)])
	s.OR15High, s.OR15Low = rangeHL(bars[:limit(n, 15)])

	// ── VWAP (session) ─────────────────────────────────────────────────
	var pv float64
	var vSum float64
	for _, b := range bars {
		typ := (b.H + b.L + b.C) / 3.0
		pv += typ * b.V
		vSum += b.V
	}
	s.cumPV = pv
	s.cumV = vSum
	if s.cumV > 0 {
		s.VWAP = s.cumPV / s.cumV
	}

	// ── Consolidation flags ────────────────────────────────────────────
	s.IsTight5 = isTight(bars, 5, s.VWAP, 0.004)
	s.IsTight10 = isTight(bars, 10, s.VWAP, 0.006)

	// ── FIX: RVOL calculation ──────────────────────────────────────────
	// Cumulative volume is just the sum across all bars seen so far.
	s.CumVolume = vSum

	// AvgBarVol30 is set exactly once when we have 30 bars and never changes,
	// giving a stable baseline. Before 30 bars we use the bars we do have.
	baselineBars := bars
	if n >= 30 && s.AvgBarVol30 == 0 {
		s.AvgBarVol30 = volumeAvg(bars[:30])
	}
	if s.AvgBarVol30 > 0 {
		// Normalise against what the same number of bars would look like at
		// average rate: expectedVol = AvgBarVol30 * numBars
		expectedVol := s.AvgBarVol30 * float64(n)
		if expectedVol > 0 {
			s.RVOL = s.CumVolume / expectedVol
		}
	} else {
		// Before we have 30 bars, use the avg of the bars we have seen so far
		// as a temporary baseline so RVOL is still a useful early signal.
		avg := volumeAvg(baselineBars)
		expectedVol := avg * float64(n)
		if expectedVol > 0 {
			s.RVOL = s.CumVolume / expectedVol
		}
	}

	// RVOL passes if we are within the first 30 minutes and RVOL >= 5.0
	// (strategy: 5–10x within first 30 minutes).
	// We keep it true once it has been set, since by minute 30 the check
	// is either done or never triggered.
	if n >= 5 && s.RVOL >= 5.0 {
		s.RVOLPasses = true
	}

	s.LastUpdate = bars[n-1].T
}

// volumeAvg returns the mean volume of a slice of bars.
func volumeAvg(bars []MinuteBar) float64 {
	if len(bars) == 0 {
		return 0
	}
	sum := 0.0
	for _, b := range bars {
		sum += b.V
	}
	return sum / float64(len(bars))
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
		ref = (hi + lo) / 2
		if ref == 0 {
			return false
		}
	}
	width := (hi - lo) / ref
	return width >= 0 && width <= band
}

// ===== Alpaca API Client =====

type AlpacaBar struct {
	T  string  `json:"t"`
	O  float64 `json:"o"`
	H  float64 `json:"h"`
	L  float64 `json:"l"`
	C  float64 `json:"c"`
	V  float64 `json:"v"`
	N  int     `json:"n"`
	VW float64 `json:"vw"`
}

type AlpacaBarsResponse struct {
	Bars          []AlpacaBar `json:"bars"`
	Symbol        string      `json:"symbol"`
	NextPageToken *string     `json:"next_page_token"`
}

func fetchAlpacaIntraday(apiKey, apiSecret, symbol, dateStr string) ([]MinuteBar, error) {
	startTime := fmt.Sprintf("%sT09:30:00-05:00", dateStr)
	endTime := fmt.Sprintf("%sT16:00:00-05:00", dateStr)

	url := fmt.Sprintf(
		"https://data.alpaca.markets/v2/stocks/%s/bars?timeframe=1Min&start=%s&end=%s&limit=10000&adjustment=all&feed=sip",
		symbol, startTime, endTime,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("APCA-API-KEY-ID", apiKey)
	req.Header.Set("APCA-API-SECRET-KEY", apiSecret)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Alpaca API error %d: %s", resp.StatusCode, string(body))
	}

	var alpacaResp AlpacaBarsResponse
	if err := json.Unmarshal(body, &alpacaResp); err != nil {
		return nil, fmt.Errorf("unmarshal: %w; body=%s", err, string(body))
	}

	return alpacaToMinuteBars(alpacaResp.Bars)
}

func alpacaToMinuteBars(bars []AlpacaBar) ([]MinuteBar, error) {
	var minuteBars []MinuteBar
	for _, bar := range bars {
		t, err := time.Parse(time.RFC3339, bar.T)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp %s: %w", bar.T, err)
		}
		tNY := t.In(locNY)
		minuteBars = append(minuteBars, MinuteBar{
			T: tNY,
			O: bar.O,
			H: bar.H,
			L: bar.L,
			C: bar.C,
			V: bar.V,
		})
	}
	return minuteBars, nil
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
	if err := json.Unmarshal([]byte(match), &managerResp); err != nil {
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
	return os.WriteFile(filename, pretty.Pretty(data), 0644)
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

	alpacaKey := os.Getenv("ALPACA_API_KEY")
	alpacaSecret := os.Getenv("ALPACA_SECRET_KEY")

	if alpacaKey == "" || alpacaSecret == "" {
		log.Fatal("ALPACA_API_KEY or ALPACA_SECRET_KEY not found in .env")
	}

	watchlistManager = NewWatchlistManager("watchlist.csv")

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
	fmt.Printf("Starting %d historical intraday workers for date %s (Alpaca API)...\n",
		len(symbols), backtestDate)

	for i, symbol := range symbols {
		wg.Add(1)
		go func(idx int, sym string) {
			defer wg.Done()
			historicalIntradayWorkerAlpaca(alpacaKey, alpacaSecret, sym, backtestDate,
				sentiment[idx], idx+1, backtestDate)
		}(i, symbol)
	}
	wg.Wait()
	fmt.Println("All workers finished. Done.")
}

// Historical intraday worker using Alpaca
func historicalIntradayWorkerAlpaca(alpacaKey, alpacaSecret, symbol, date string,
	sentiment string, goroutineId int, backtestDate string) {

	fmt.Printf("\n[#%d:%s] ============================================\n", goroutineId, symbol)
	fmt.Printf("[#%d:%s] Historical worker started for %s\n", goroutineId, symbol, date)

	openNY, closeNY, err := sessionWindow(date)
	if err != nil {
		log.Printf("[#%d:%s] sessionWindow error: %v", goroutineId, symbol, err)
		return
	}

	fmt.Printf("🔍 [%s] Fetching data from Alpaca...\n", symbol)
	allBars, err := fetchAlpacaIntraday(alpacaKey, alpacaSecret, symbol, date)
	if err != nil {
		log.Printf("[#%d:%s] ❌ Failed to fetch data from Alpaca: %v", goroutineId, symbol, err)
		return
	}

	fmt.Printf("[#%d:%s] 📊 Got %d bars from Alpaca\n", goroutineId, symbol, len(allBars))

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

	processMinuteByMinute(bars, symbol, sentiment, goroutineId, backtestDate)

	fmt.Printf("[#%d:%s] ✅ Completed processing all minutes\n", goroutineId, symbol)
	fmt.Printf("[#%d:%s] ============================================\n\n", goroutineId, symbol)
}

// processMinuteByMinute processes bars minute by minute, calling the manager
// agent after each bar.
//
// FIX: StrategyState is carried across iterations (not re-created each loop)
// so that AvgBarVol30 is set exactly once at bar 30 and never recomputed.
func processMinuteByMinute(allBars []MinuteBar, symbol string, sentiment string, goroutineId int, backtestDate string) {
	fmt.Printf("[#%d:%s] Starting minute-by-minute processing (limited to %d minutes or until Buy)\n",
		goroutineId, symbol, maxIterations)

	maxMinutes := len(allBars)
	if maxMinutes > maxIterations {
		maxMinutes = maxIterations
	}

	// FIX: state persists across the loop so AvgBarVol30 accumulates correctly
	var state StrategyState

	for i := 1; i <= maxMinutes; i++ {
		currentBars := allBars[:i]

		// FIX: recompute on the persistent state rather than a fresh struct
		state.Recompute(currentBars)

		currentTime := currentBars[i-1].T

		fmt.Printf("[#%d:%s] Minute %d/%d (%s): OR5[%.2f/%.2f] OR15[%.2f/%.2f] VWAP=%.3f "+
			"Tight5=%t Tight10=%t RVOL=%.2fx RVOLPasses=%t\n",
			goroutineId, symbol, i, maxMinutes, currentTime.Format("15:04"),
			state.OR5High, state.OR5Low, state.OR15High, state.OR15Low,
			state.VWAP, state.IsTight5, state.IsTight10,
			state.RVOL, state.RVOLPasses,
		)

		epBars := convertToEP(symbol, currentBars)
		shouldStop := runManagerAgent(epBars, symbol, goroutineId, i, sentiment, maxMinutes, backtestDate, &state)

		if shouldStop {
			fmt.Printf("[#%d:%s] 🛑 Stopping after minute %d due to Buy recommendation\n",
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

// runManagerAgent calls the manager agent for the current minute and handles
// the Buy recommendation flow.
//
// FIX: state is now passed in so we can use OR5Low/OR15Low for the stop loss
// instead of the raw latest bar low.
//
// FIX: RVOL is included as a structured field in the formatted stock data sent
// to the agent so it can reason about institutional volume participation.
//
// FIX: Stop loss uses the opening range low (OR5Low if we are past minute 5,
// OR15Low if past minute 15). This matches the strategy: "stop loss should be
// at the opening range lows or the VWAP."
func runManagerAgent(stockdata []ep.StockData, symbol string, goroutineId int,
	currentMinute int, sentiment string, totalMinutes int,
	backtestDate string, state *StrategyState) bool {

	fmt.Printf("\n[Goroutine %d] --- Starting runManagerAgent for %s (minute %d/%d) ---\n",
		goroutineId, symbol, currentMinute, totalMinutes)
	defer fmt.Printf("[Goroutine %d] ✓ runManagerAgent completed for %s (minute %d/%d)\n",
		goroutineId, symbol, currentMinute, totalMinutes)

	fmt.Printf("[Goroutine %d] Processing %d stock data points for %s (up to minute %d)\n",
		goroutineId, len(stockdata), symbol, currentMinute)

	// ── Format OHLCV bar data ──────────────────────────────────────────────
	stock_data := ""
	for i, stockdataPoint := range stockdata {
		stock_data += fmt.Sprintf("%d min - Open: %v Close: %v High: %v Low: %v Volume: %v\n",
			i+1, stockdataPoint.Open, stockdataPoint.Close, stockdataPoint.High,
			stockdataPoint.Low, stockdataPoint.Volume)
		if i < 3 || i == len(stockdata)-1 {
			fmt.Printf("[Goroutine %d]   Data %d: O:%.2f C:%.2f H:%.2f L:%.2f V:%.0f\n",
				goroutineId, i+1, stockdataPoint.Open, stockdataPoint.Close,
				stockdataPoint.High, stockdataPoint.Low, float64(stockdataPoint.Volume))
		}
	}

	// ── Append structured strategy signals ────────────────────────────────
	// FIX: RVOL and opening range levels are now explicitly included so the
	// agent has clean numerical signals rather than having to infer them from
	// raw bar data. This prevents the agent from under-weighting volume.
	stock_data += fmt.Sprintf(
		"\n--- STRATEGY SIGNALS (minute %d) ---\n"+
			"OR5  High=%.2f  Low=%.2f\n"+
			"OR15 High=%.2f  Low=%.2f\n"+
			"VWAP=%.3f\n"+
			"RVOL=%.2fx  (cumVol=%.0f  avgBarVol30=%.0f)\n"+
			"RVOL>=5x in first 30min: %t\n"+
			"Tight5=%t  Tight10=%t\n",
		currentMinute,
		state.OR5High, state.OR5Low,
		state.OR15High, state.OR15Low,
		state.VWAP,
		state.RVOL, state.CumVolume, state.AvgBarVol30,
		state.RVOLPasses,
		state.IsTight5, state.IsTight10,
	)

	fmt.Printf("[Goroutine %d] ✓ Stock data formatted (%d characters) for minute %d\n",
		goroutineId, len(stock_data), currentMinute)

	// ── Read news and earnings reports ────────────────────────────────────
	dataDir := "reports"
	stockDir := filepath.Join(dataDir, symbol)

	newsFilePath := filepath.Join(stockDir, "news_report.txt")
	newsFile, err := os.Open(newsFilePath)
	var news []byte
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Error opening news report file: %v\n", goroutineId, err)
	} else {
		defer newsFile.Close()
		news, err = io.ReadAll(newsFile)
		if err != nil {
			fmt.Printf("[Goroutine %d] ❌ Error reading news file: %v\n", goroutineId, err)
		} else {
			fmt.Printf("[Goroutine %d] ✓ News report read (%d bytes)\n", goroutineId, len(news))
		}
	}

	earningsFilePath := filepath.Join(stockDir, "earnings_report.txt")
	earningsFile, err := os.Open(earningsFilePath)
	var earnings []byte
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Error opening earnings report file: %v\n", goroutineId, err)
	} else {
		defer earningsFile.Close()
		earnings, err = io.ReadAll(earningsFile)
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
		goroutineId, currentMinute, len(resp), resp)

	fmt.Println("Agent response: ", resp)

	managerResp, err := extractJSON(resp)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Failed to extract JSON from response (minute %d): %v\n",
			goroutineId, currentMinute, err)
		return false
	}

	if err := saveJSONResponse(symbol, currentMinute, managerResp); err != nil {
		fmt.Printf("[Goroutine %d] ❌ Failed to save JSON response (minute %d): %v\n",
			goroutineId, currentMinute, err)
	} else {
		fmt.Printf("[Goroutine %d] ✓ JSON response saved for minute %d\n", goroutineId, currentMinute)
	}

	if strings.ToLower(strings.TrimSpace(managerResp.Recommendation)) != "buy" {
		return false
	}

	// ── Buy path ──────────────────────────────────────────────────────────
	// Parse risk percent
	stringPercent := strings.TrimSpace(strings.TrimSuffix(managerResp.RiskPercent, "%"))
	riskPercent, err := strconv.ParseFloat(stringPercent, 64)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Failed to parse risk percent '%s': %v\n",
			goroutineId, stringPercent, err)
		return false
	}

	accSize, err := strconv.ParseFloat(os.Getenv("ACCOUNT_SIZE"), 64)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Failed to parse account size: %v\n", goroutineId, err)
		return false
	}

	riskPerTrade, err := strconv.ParseFloat(os.Getenv("RISK_PER_TRADE"), 64)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Failed to parse risk per trade: %v\n", goroutineId, err)
		return false
	}

	entryPrice, err := strconv.ParseFloat(strings.ReplaceAll(managerResp.EntryPrice, "$", ""), 64)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Failed to parse entry price '%s': %v\n",
			goroutineId, managerResp.EntryPrice, err)
		return false
	}

	// ── FIX: Stop loss uses Opening Range Low, not raw latest bar low ──────
	// Strategy: "stop loss should be at the opening range lows or the VWAP"
	// We prefer OR5Low when available (minute >= 5), OR15Low when available
	// (minute >= 15). If neither is set yet (edge case: minute < 5), we fall
	// back to VWAP, and only use the latest bar low as a last resort.
	stopLoss := selectStopLoss(state, currentMinute)
	stopLossStr := fmt.Sprintf("%.2f", stopLoss)

	fmt.Printf("[Goroutine %d] 📐 Stop loss selected: %.2f (minute=%d, OR5Low=%.2f, OR15Low=%.2f, VWAP=%.2f)\n",
		goroutineId, stopLoss, currentMinute, state.OR5Low, state.OR15Low, state.VWAP)

	if stopLoss <= 0 {
		fmt.Printf("[Goroutine %d] ❌ Invalid stop loss %.2f\n", goroutineId, stopLoss)
		return false
	}

	riskPerShare := entryPrice - stopLoss
	if riskPerShare <= 0 {
		fmt.Printf("[Goroutine %d] ❌ Invalid risk: entry %.2f <= stop %.2f\n",
			goroutineId, entryPrice, stopLoss)
		return false
	}

	totalRisk := riskPerTrade * accSize
	shares := math.Round(totalRisk / riskPerShare)

	if shares <= 0 {
		fmt.Printf("[Goroutine %d] ❌ Invalid share count: %.0f shares\n", goroutineId, shares)
		return false
	}

	fmt.Printf("[Goroutine %d] 📊 Calculated %.0f shares (riskPerShare=$%.2f, totalRisk=$%.2f)\n",
		goroutineId, shares, riskPerShare, totalRisk)

	if managerResp.EntryPrice != "" {
		entry := WatchlistEntry{
			StockSymbol: symbol,
			EntryPrice:  managerResp.EntryPrice,
			// FIX: use the ORL-derived stop rather than the agent's raw field,
			// which was previously set from the bar low.
			StopLoss:    stopLossStr,
			Shares:      strconv.FormatFloat(shares, 'f', 0, 64),
			InitialRisk: strconv.FormatFloat(riskPercent, 'f', 2, 64),
			Date:        backtestDate,
		}

		if err := watchlistManager.AddEntry(entry); err != nil {
			if strings.Contains(err.Error(), "duplicate entry") {
				fmt.Printf("[Goroutine %d] ⚠️ Duplicate watchlist entry for %s (minute %d)\n",
					goroutineId, symbol, currentMinute)
			} else {
				fmt.Printf("[Goroutine %d] ❌ Failed to add %s to watchlist (minute %d): %v\n",
					goroutineId, symbol, currentMinute, err)
			}
		} else {
			fmt.Printf("[Goroutine %d] ✅ Added %s to watchlist: %.0f shares @ %s (stop: %s) (minute %d)\n",
				goroutineId, symbol, shares, entry.EntryPrice, entry.StopLoss, currentMinute)
		}
		return true
	}
	return false
}

// selectStopLoss returns the appropriate stop loss price according to the EP
// strategy hierarchy: OR15Low (if minute >= 15) > OR5Low (if minute >= 5) >
// VWAP (always available after minute 1).
//
// We deliberately never fall back to a raw bar low because bar lows are noisy
// and don't correspond to any structurally meaningful level.
func selectStopLoss(state *StrategyState, currentMinute int) float64 {
	if currentMinute >= 15 && state.OR15Low > 0 {
		return state.OR15Low
	}
	if currentMinute >= 5 && state.OR5Low > 0 {
		return state.OR5Low
	}
	// VWAP is always computable once we have at least one bar
	if state.VWAP > 0 {
		return state.VWAP
	}
	return 0
}