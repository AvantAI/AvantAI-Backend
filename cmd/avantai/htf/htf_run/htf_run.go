package main

// HTF Single Entry Point
//
// Run this instead of htf_scanner and htf_main separately.
// It runs the morning scan first, saves results to JSON, then
// immediately starts the intraday monitor on the qualifying candidates.
//
// Usage:
//   go run htf_run.go                    # uses today's date
//   go run htf_run.go -date 2025-03-06   # specific date
//
// To view confirmed breakout signals afterwards:
//   go run cmd/avantai/htf/htf_watchlist/htf_watchlist.go

import (
	"avantai/pkg/htf"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

// ===== Shared constants =====

const maxConcurrent = 5

var (
	locNY, _ = time.LoadLocation("America/New_York")
)

// ===== Environment loader =====

func loadEnv() {
	dir, err := os.Getwd()
	if err != nil {
		log.Printf("Warning: could not determine working directory: %v", err)
		return
	}
	for {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			if err := godotenv.Load(candidate); err != nil {
				log.Printf("Warning: found .env at %s but could not load it: %v", candidate, err)
			}
			if err := os.Chdir(dir); err != nil {
				log.Printf("Warning: could not chdir to project root %s: %v", dir, err)
			}
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			log.Println("Warning: .env file not found in any parent directory")
			return
		}
		dir = parent
	}
}

// ===== Daily bar types (scanner) =====

type alpacaDailyBar struct {
	T string  `json:"t"`
	O float64 `json:"o"`
	H float64 `json:"h"`
	L float64 `json:"l"`
	C float64 `json:"c"`
	V float64 `json:"v"`
}

type alpacaDailyBarsResponse struct {
	Bars          []alpacaDailyBar `json:"bars"`
	Symbol        string           `json:"symbol"`
	NextPageToken *string          `json:"next_page_token"`
}

// fetchDailyBars retrieves historical 1-Day bars from Alpaca for a single symbol.
// Bars are returned sorted oldest-to-newest.
func fetchDailyBars(apiKey, apiSecret, symbol, startDate, endDate string) ([]htf.DailyBar, error) {
	const baseURL = "https://data.alpaca.markets/v2"

	var allBars []htf.DailyBar
	var pageToken *string

	for {
		url := fmt.Sprintf(
			"%s/stocks/%s/bars?timeframe=1Day&start=%s&end=%s&limit=1000&adjustment=all&feed=sip",
			baseURL, symbol, startDate, endDate,
		)
		if pageToken != nil {
			url = fmt.Sprintf("%s&page_token=%s", url, *pageToken)
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("APCA-API-KEY-ID", apiKey)
		req.Header.Set("APCA-API-SECRET-KEY", apiSecret)

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("http request: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read body: %w", err)
		}

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("Alpaca API error %d for %s: %s", resp.StatusCode, symbol, string(body))
		}

		var alpacaResp alpacaDailyBarsResponse
		if err := json.Unmarshal(body, &alpacaResp); err != nil {
			return nil, fmt.Errorf("unmarshal response for %s: %w", symbol, err)
		}

		for _, bar := range alpacaResp.Bars {
			t, err := time.Parse(time.RFC3339, bar.T)
			if err != nil {
				continue
			}
			allBars = append(allBars, htf.DailyBar{
				Date:   t,
				Open:   bar.O,
				High:   bar.H,
				Low:    bar.L,
				Close:  bar.C,
				Volume: bar.V,
			})
		}

		if alpacaResp.NextPageToken == nil {
			break
		}
		pageToken = alpacaResp.NextPageToken
	}

	sort.Slice(allBars, func(i, j int) bool {
		return allBars[i].Date.Before(allBars[j].Date)
	})

	return allBars, nil
}

// loadSymbolsFromCSV reads ticker symbols from the stock universe CSV.
func loadSymbolsFromCSV(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filePath, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read CSV: %w", err)
	}

	var symbols []string
	for i, row := range records {
		if i == 0 {
			continue
		}
		if len(row) > 0 && strings.TrimSpace(row[0]) != "" {
			sym := strings.TrimSpace(strings.ToUpper(row[0]))
			// Skip preferred shares, warrants, and other non-standard tickers
			// that Alpaca does not support (e.g. ABR$D, ACHR.W)
			if strings.ContainsAny(sym, "$") {
				continue
			}
			symbols = append(symbols, sym)
		}
	}
	return symbols, nil
}

// ===== Scanner =====

type scanResult struct {
	symbol    string
	candidate *htf.HTFCandidate
	err       error
}

// runScanner runs the morning pre-market scan and saves results to JSON.
// Returns the qualifying candidates for the intraday monitor.
func runScanner(apiKey, apiSecret, scanDate string) ([]htf.HTFCandidate, error) {
	fmt.Printf("\n=== HTF Morning Scanner — %s ===\n\n", scanDate)

	symbols, err := loadSymbolsFromCSV(htf.StockUniverseCSVPath)
	if err != nil {
		return nil, fmt.Errorf("load stock universe: %w", err)
	}
	fmt.Printf("Loaded %d symbols from %s\n", len(symbols), htf.StockUniverseCSVPath)

	scanTime, err := time.Parse("2006-01-02", scanDate)
	if err != nil {
		return nil, fmt.Errorf("invalid scan date %q: %w", scanDate, err)
	}
	startDate := scanTime.AddDate(0, 0, -htf.HistoricalLookbackDays).Format("2006-01-02")
	endDate := scanDate

	fmt.Printf("Historical data range: %s → %s\n", startDate, endDate)
	fmt.Printf("HTF thresholds: pole >= %.0f%% in <= %d days | flag %d–%d days | range <= %.0f%% | pullback %.0f–%.0f%%\n\n",
		htf.FlagpoleMinGainPct, htf.FlagpoleMaxTradingDays,
		htf.FlagMinTradingDays, htf.FlagMaxTradingDays,
		htf.FlagMaxRangePct,
		htf.FlagMinPullbackPct, htf.FlagMaxPullbackPct,
	)

	semaphore := make(chan struct{}, maxConcurrent)
	resultsCh := make(chan scanResult, len(symbols))
	var wg sync.WaitGroup

	for _, sym := range symbols {
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			bars, err := fetchDailyBars(apiKey, apiSecret, symbol, startDate, endDate)
			if err != nil {
				resultsCh <- scanResult{symbol: symbol, err: err}
				return
			}

			candidate, qualifies := htf.ScanForHTFCandidate(symbol, bars)
			resultsCh <- scanResult{symbol: symbol, candidate: func() *htf.HTFCandidate {
				if qualifies {
					return candidate
				}
				return nil
			}()}
		}(sym)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var candidates []htf.HTFCandidate
	errCount := 0
	scanned := 0

	for result := range resultsCh {
		scanned++
		if result.err != nil {
			errCount++
			fmt.Printf("[HTF Scanner] ERROR %s: %v\n", result.symbol, result.err)
			continue
		}
		if result.candidate != nil {
			candidates = append(candidates, *result.candidate)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Flagpole.GainPct > candidates[j].Flagpole.GainPct
	})

	fmt.Printf("\n=== Scan Complete ===\n")
	fmt.Printf("Symbols scanned : %d\n", scanned)
	fmt.Printf("Errors          : %d\n", errCount)
	fmt.Printf("HTF candidates  : %d\n\n", len(candidates))

	// Save results JSON
	report := htf.HTFScanReport{
		ScanDate:         scanDate,
		GeneratedAt:      time.Now().Format(time.RFC3339),
		TotalCandidates:  len(candidates),
		QualifyingStocks: candidates,
	}

	if err := os.MkdirAll(htf.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	filename := fmt.Sprintf("%s/htf_%s_results.json",
		htf.OutputDir,
		strings.ReplaceAll(scanDate, "-", ""),
	)

	jsonData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal scan report: %w", err)
	}

	if err := os.WriteFile(filename, jsonData, 0644); err != nil {
		return nil, fmt.Errorf("write scan report: %w", err)
	}

	fmt.Printf("Scan report saved to: %s\n", filename)

	if len(candidates) > 0 {
		fmt.Printf("\nHTF Candidates for %s:\n", scanDate)
		fmt.Printf("%-8s  %-10s  %-9s  %-9s  %-7s  %-10s  %-10s  %-8s\n",
			"TICKER", "POLE_GAIN%", "POLE_DAYS", "FLAG_DAYS", "RANGE%", "RESISTANCE", "SUPPORT", "PRICE")
		fmt.Println(strings.Repeat("-", 80))
		for _, c := range candidates {
			fmt.Printf("%-8s  %9.1f%%  %9d  %9d  %6.1f%%  %10.2f  %10.2f  %8.2f\n",
				c.Symbol,
				c.Flagpole.GainPct,
				c.Flagpole.DurationTradingDays,
				c.Flag.TradingDays,
				c.Flag.RangePct,
				c.ResistanceLevel,
				c.SupportLevel,
				c.CurrentPrice,
			)
		}
	} else {
		fmt.Println("No HTF candidates found for this date.")
	}

	return candidates, nil
}

// ===== Intraday bar types (monitor) =====

type MinuteBar struct {
	T time.Time
	O float64
	H float64
	L float64
	C float64
	V float64
}

type alpacaMinuteBar struct {
	T  string  `json:"t"`
	O  float64 `json:"o"`
	H  float64 `json:"h"`
	L  float64 `json:"l"`
	C  float64 `json:"c"`
	V  float64 `json:"v"`
	N  int     `json:"n"`
	VW float64 `json:"vw"`
}

type alpacaMinuteBarsResponse struct {
	Bars          []alpacaMinuteBar `json:"bars"`
	Symbol        string            `json:"symbol"`
	NextPageToken *string           `json:"next_page_token"`
}

// fetchIntraday retrieves 1-minute bars for a symbol on a given date.
func fetchIntraday(apiKey, apiSecret, symbol, dateStr string) ([]MinuteBar, error) {
	startTime := fmt.Sprintf("%sT09:30:00-05:00", dateStr)
	endTime := fmt.Sprintf("%sT16:00:00-05:00", dateStr)

	url := fmt.Sprintf(
		"https://data.alpaca.markets/v2/stocks/%s/bars?timeframe=1Min&start=%s&end=%s&limit=10000&adjustment=all&feed=sip",
		symbol, startTime, endTime,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("APCA-API-KEY-ID", apiKey)
	req.Header.Set("APCA-API-SECRET-KEY", apiSecret)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Alpaca API error %d: %s", resp.StatusCode, string(body))
	}

	var alpacaResp alpacaMinuteBarsResponse
	if err := json.Unmarshal(body, &alpacaResp); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	var bars []MinuteBar
	for _, bar := range alpacaResp.Bars {
		t, err := time.Parse(time.RFC3339, bar.T)
		if err != nil {
			continue
		}
		bars = append(bars, MinuteBar{
			T: t.In(locNY),
			O: bar.O, H: bar.H, L: bar.L, C: bar.C, V: bar.V,
		})
	}

	sort.Slice(bars, func(i, j int) bool {
		return bars[i].T.Before(bars[j].T)
	})

	return bars, nil
}

func sessionWindow(date string) (openNY, closeNY time.Time, err error) {
	d, err := time.ParseInLocation("2006-01-02", date, locNY)
	if err != nil {
		return
	}
	openNY = time.Date(d.Year(), d.Month(), d.Day(), 9, 30, 0, 0, locNY)
	closeNY = time.Date(d.Year(), d.Month(), d.Day(), 16, 0, 0, 0, locNY)
	return
}

// ===== Watchlist CSV manager =====

type WatchlistManager struct {
	mu       sync.Mutex
	filename string
	entries  map[string]htf.HTFWatchlistEntry
}

func NewWatchlistManager(filename string) *WatchlistManager {
	wm := &WatchlistManager{
		filename: filename,
		entries:  make(map[string]htf.HTFWatchlistEntry),
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
		log.Printf("Warning: failed to read existing HTF watchlist: %v", err)
		return
	}

	startIdx := 0
	if len(records) > 0 && records[0][0] == "symbol" {
		startIdx = 1
	}

	for i := startIdx; i < len(records); i++ {
		if len(records[i]) >= 8 {
			entry := htf.HTFWatchlistEntry{
				Symbol:          records[i][0],
				BreakoutTime:    records[i][1],
				BreakoutPrice:   records[i][2],
				ResistanceLevel: records[i][3],
				VolumeRatio:     records[i][4],
				FlagpoleGainPct: records[i][5],
				FlagRangePct:    records[i][6],
				Date:            records[i][7],
			}
			key := fmt.Sprintf("%s|%s", entry.Symbol, entry.Date)
			wm.entries[key] = entry
		}
	}

	fmt.Printf("[HTF Watchlist] Loaded %d existing entries from %s\n", len(wm.entries), wm.filename)
}

func (wm *WatchlistManager) AddEntry(entry htf.HTFWatchlistEntry) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	key := fmt.Sprintf("%s|%s", entry.Symbol, entry.Date)
	if _, exists := wm.entries[key]; exists {
		return fmt.Errorf("duplicate entry: %s on %s already in watchlist", entry.Symbol, entry.Date)
	}

	wm.entries[key] = entry
	return wm.writeAllEntries()
}

func (wm *WatchlistManager) writeAllEntries() error {
	tempFile := wm.filename + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	writer := csv.NewWriter(file)

	header := []string{"symbol", "breakout_time", "breakout_price", "resistance_level",
		"volume_ratio", "flagpole_gain_pct", "flag_range_pct", "date"}
	if err := writer.Write(header); err != nil {
		file.Close()
		os.Remove(tempFile)
		return fmt.Errorf("write header: %w", err)
	}

	for _, e := range wm.entries {
		record := []string{e.Symbol, e.BreakoutTime, e.BreakoutPrice, e.ResistanceLevel,
			e.VolumeRatio, e.FlagpoleGainPct, e.FlagRangePct, e.Date}
		if err := writer.Write(record); err != nil {
			file.Close()
			os.Remove(tempFile)
			return fmt.Errorf("write record: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		file.Close()
		os.Remove(tempFile)
		return fmt.Errorf("CSV flush error: %w", err)
	}
	file.Close()

	if err := os.Rename(tempFile, wm.filename); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

var watchlistManager *WatchlistManager

// ===== Intraday monitor =====

func handleBreakoutSignal(signal *htf.BreakoutSignal, date string, goroutineID int) {
	fmt.Printf("\n[#%d:%s] *** HTF BREAKOUT SIGNAL ***\n", goroutineID, signal.Symbol)
	fmt.Printf("[#%d:%s]   Time            : %s\n", goroutineID, signal.Symbol, signal.BreakoutTime.Format("15:04"))
	fmt.Printf("[#%d:%s]   Breakout Price  : $%.2f\n", goroutineID, signal.Symbol, signal.BreakoutPrice)
	fmt.Printf("[#%d:%s]   Resistance Level: $%.2f\n", goroutineID, signal.Symbol, signal.ResistanceLevel)
	fmt.Printf("[#%d:%s]   Volume Ratio    : %.1fx average\n", goroutineID, signal.Symbol, signal.VolumeRatio)
	fmt.Printf("[#%d:%s]   Confirm Bars    : %d\n", goroutineID, signal.Symbol, signal.ConfirmationBars)
	fmt.Printf("[#%d:%s]   Flagpole Gain   : %.1f%% in %d trading days\n",
		goroutineID, signal.Symbol, signal.Flagpole.GainPct, signal.Flagpole.DurationTradingDays)
	fmt.Printf("[#%d:%s]   Flag Range      : %.1f%% | Pullback from Peak: %.1f%%\n",
		goroutineID, signal.Symbol, signal.Flag.RangePct, signal.Flag.PullbackFromPeakPct)

	entry := htf.HTFWatchlistEntry{
		Symbol:          signal.Symbol,
		BreakoutTime:    signal.BreakoutTime.Format("15:04"),
		BreakoutPrice:   strconv.FormatFloat(signal.BreakoutPrice, 'f', 2, 64),
		ResistanceLevel: strconv.FormatFloat(signal.ResistanceLevel, 'f', 2, 64),
		VolumeRatio:     strconv.FormatFloat(signal.VolumeRatio, 'f', 2, 64),
		FlagpoleGainPct: strconv.FormatFloat(signal.Flagpole.GainPct, 'f', 2, 64),
		FlagRangePct:    strconv.FormatFloat(signal.Flag.RangePct, 'f', 2, 64),
		Date:            date,
	}

	if err := watchlistManager.AddEntry(entry); err != nil {
		if strings.Contains(err.Error(), "duplicate entry") {
			fmt.Printf("[#%d:%s] Duplicate watchlist entry (already recorded)\n", goroutineID, signal.Symbol)
		} else {
			fmt.Printf("[#%d:%s] Failed to add to watchlist: %v\n", goroutineID, signal.Symbol, err)
		}
		return
	}

	fmt.Printf("[#%d:%s] Signal written to %s\n", goroutineID, signal.Symbol, htf.WatchlistCSVFilename)
}

func intradayWorker(apiKey, apiSecret string, candidate htf.HTFCandidate, date string, goroutineID int) {
	symbol := candidate.Symbol

	fmt.Printf("\n[#%d:%s] ========================================\n", goroutineID, symbol)
	fmt.Printf("[#%d:%s] Monitoring | resistance $%.2f | support $%.2f | pole %.1f%%\n",
		goroutineID, symbol,
		candidate.ResistanceLevel,
		candidate.SupportLevel,
		candidate.Flagpole.GainPct,
	)

	openNY, closeNY, err := sessionWindow(date)
	if err != nil {
		log.Printf("[#%d:%s] sessionWindow error: %v", goroutineID, symbol, err)
		return
	}

	allBars, err := fetchIntraday(apiKey, apiSecret, symbol, date)
	if err != nil {
		log.Printf("[#%d:%s] Failed to fetch intraday data: %v", goroutineID, symbol, err)
		return
	}

	var bars []MinuteBar
	for _, bar := range allBars {
		if !bar.T.Before(openNY) && !bar.T.After(closeNY) {
			bars = append(bars, bar)
		}
	}

	if len(bars) == 0 {
		fmt.Printf("[#%d:%s] No bars within session hours (%s–%s) for %s\n",
			goroutineID, symbol, openNY.Format("15:04"), closeNY.Format("15:04"), date)
		return
	}

	fmt.Printf("[#%d:%s] Got %d session bars from Alpaca\n", goroutineID, symbol, len(bars))

	state := htf.NewIntradayState(candidate)

	for i, bar := range bars {
		fmt.Printf("[#%d:%s] Bar %d/%d (%s): O=%.2f H=%.2f L=%.2f C=%.2f V=%.0f | status=%s\n",
			goroutineID, symbol,
			i+1, len(bars),
			bar.T.Format("15:04"),
			bar.O, bar.H, bar.L, bar.C, bar.V,
			state.Status,
		)

		signal := htf.UpdateState(state, bar.H, bar.C, bar.V, bar.T)

		if signal != nil {
			handleBreakoutSignal(signal, date, goroutineID)
			break
		}

		if state.Status == htf.StatusInvalidated {
			fmt.Printf("[#%d:%s] Pattern invalidated at bar %d (%s) — stopping\n",
				goroutineID, symbol, i+1, bar.T.Format("15:04"))
			break
		}
	}

	fmt.Printf("[#%d:%s] Completed | final status: %s\n", goroutineID, symbol, state.Status)
	fmt.Printf("[#%d:%s] ========================================\n\n", goroutineID, symbol)
}

// runMonitor runs the intraday monitor for all qualifying candidates.
func runMonitor(apiKey, apiSecret string, candidates []htf.HTFCandidate, date string) {
	fmt.Printf("\n=== HTF Intraday Monitor — %s ===\n", date)
	fmt.Printf("=== Confirmation bars: %d | First half only: %v ===\n\n",
		htf.BreakoutConfirmationBars, htf.BreakoutFirstHalfOnly)

	fmt.Printf("Monitoring %d HTF candidates:\n\n", len(candidates))
	for i, c := range candidates {
		fmt.Printf("  %d. %-8s  resistance $%.2f  support $%.2f  pole %.1f%% in %d days\n",
			i+1, c.Symbol, c.ResistanceLevel, c.SupportLevel,
			c.Flagpole.GainPct, c.Flagpole.DurationTradingDays)
	}
	fmt.Println()

	watchlistManager = NewWatchlistManager(htf.WatchlistCSVFilename)

	var wg sync.WaitGroup
	for i, candidate := range candidates {
		wg.Add(1)
		go func(idx int, c htf.HTFCandidate) {
			defer wg.Done()
			intradayWorker(apiKey, apiSecret, c, date, idx+1)
		}(i, candidate)
	}

	wg.Wait()
	fmt.Println("\nAll intraday workers finished.")
}

// ===== Entry point =====

func main() {
	datePtr := flag.String("date", time.Now().Format("2006-01-02"), "trading date (YYYY-MM-DD)")
	flag.Parse()
	date := *datePtr

	fmt.Printf("=== HTF Run — %s ===\n", date)

	loadEnv()

	apiKey := os.Getenv("ALPACA_API_KEY")
	apiSecret := os.Getenv("ALPACA_SECRET_KEY")
	if apiKey == "" || apiSecret == "" {
		log.Fatal("ALPACA_API_KEY or ALPACA_SECRET_KEY not set in environment")
	}

	// Step 1: Morning scan
	candidates, err := runScanner(apiKey, apiSecret, date)
	if err != nil {
		log.Fatalf("Scanner failed: %v", err)
	}

	if len(candidates) == 0 {
		fmt.Println("No HTF candidates found — nothing to monitor.")
		return
	}

	// Step 2: Intraday monitor
	runMonitor(apiKey, apiSecret, candidates, date)
}
