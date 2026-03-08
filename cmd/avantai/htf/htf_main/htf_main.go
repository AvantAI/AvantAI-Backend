package main

// Component 2: HTF Intraday Watchlist Monitor
// Component 3: HTF Pattern Recognition Engine (integration)
//
// This program runs throughout the trading day. It:
//   1. Loads the watchlist produced by htf_scanner (htf_YYYYMMDD_results.json).
//   2. Spawns one goroutine per candidate stock.
//   3. Fetches 1-minute intraday bars from Alpaca for the session date.
//   4. Replays bars minute-by-minute, calling the pattern recognition engine.
//   5. When a breakout signal is detected, logs it and appends to htf_watchlist.csv.
//
// Usage:
//   go run htf_main.go -date 2025-03-06
//
// The program stops processing a stock once a signal is emitted (StatusTriggered)
// or the pattern is invalidated (StatusInvalidated). This matches EP's behaviour
// of stopping after a Buy recommendation.

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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

// ===== Intraday bar types =====

// MinuteBar is a single 1-minute OHLCV bar from the Alpaca API.
type MinuteBar struct {
	T time.Time
	O float64
	H float64
	L float64
	C float64
	V float64
}

// alpacaBar matches the Alpaca API bar JSON object.
type alpacaBar struct {
	T  string  `json:"t"`
	O  float64 `json:"o"`
	H  float64 `json:"h"`
	L  float64 `json:"l"`
	C  float64 `json:"c"`
	V  float64 `json:"v"`
	N  int     `json:"n"`
	VW float64 `json:"vw"`
}

type alpacaBarsResponse struct {
	Bars          []alpacaBar `json:"bars"`
	Symbol        string      `json:"symbol"`
	NextPageToken *string     `json:"next_page_token"`
}

// ===== Alpaca intraday fetcher =====

var (
	locNY, _ = time.LoadLocation("America/New_York")
)

// fetchAlpacaIntraday retrieves 1-minute bars for `symbol` on `dateStr`
// (YYYY-MM-DD) from regular market open to close via the Alpaca API.
// Matches the pattern in ep_main_alpaca_backtest.go.
func fetchAlpacaIntraday(apiKey, apiSecret, symbol, dateStr string) ([]MinuteBar, error) {
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

	var alpacaResp alpacaBarsResponse
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
			O: bar.O,
			H: bar.H,
			L: bar.L,
			C: bar.C,
			V: bar.V,
		})
	}

	// Sort oldest-first
	sortMinuteBarsAsc(bars)
	return bars, nil
}

func sortMinuteBarsAsc(bars []MinuteBar) {
	for i := 1; i < len(bars); i++ {
		j := i
		for j > 0 && bars[j-1].T.After(bars[j].T) {
			bars[j-1], bars[j] = bars[j], bars[j-1]
			j--
		}
	}
}

// sessionWindow returns the regular session open and close times (NY) for a date string.
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
// Mirrors WatchlistManager in ep_main_alpaca_backtest.go.

// WatchlistManager is a thread-safe manager for the HTF watchlist CSV file.
// Multiple goroutines (one per candidate) write to it concurrently.
type WatchlistManager struct {
	mu       sync.Mutex
	filename string
	entries  map[string]htf.HTFWatchlistEntry // keyed by symbol|date to prevent duplicates
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
		return // File doesn't exist yet — that's fine on first run.
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

// AddEntry appends a confirmed breakout signal to the watchlist CSV.
// Duplicate entries (same symbol + date) are silently skipped.
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

// Global watchlist manager (shared across goroutines, like EP).
var watchlistManager *WatchlistManager

// ===== Intraday worker =====

// intradayWorker runs the complete intraday monitoring loop for a single
// HTF candidate. It replays bars minute-by-minute against the pattern
// recognition engine and stops when triggered or invalidated.
func intradayWorker(apiKey, apiSecret string, candidate htf.HTFCandidate, date string, goroutineID int) {
	symbol := candidate.Symbol

	fmt.Printf("\n[#%d:%s] ========================================\n", goroutineID, symbol)
	fmt.Printf("[#%d:%s] Intraday worker started | resistance $%.2f | support $%.2f | pole %.1f%%\n",
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

	// Fetch intraday bars from Alpaca
	allBars, err := fetchAlpacaIntraday(apiKey, apiSecret, symbol, date)
	if err != nil {
		log.Printf("[#%d:%s] Failed to fetch intraday data: %v", goroutineID, symbol, err)
		return
	}

	// Filter to regular session hours
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

	// Initialise pattern recognition state
	state := htf.NewIntradayState(candidate)

	// ---- Minute-by-minute processing loop ----
	// Mirrors processMinuteByMinute in ep_main_alpaca_backtest.go.
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
			// Breakout confirmed — record the signal and stop processing.
			handleBreakoutSignal(signal, date, goroutineID)
			break
		}

		if state.Status == htf.StatusInvalidated {
			fmt.Printf("[#%d:%s] Pattern invalidated at bar %d (%s) — stopping\n",
				goroutineID, symbol, i+1, bar.T.Format("15:04"))
			break
		}
	}

	fmt.Printf("[#%d:%s] Intraday worker completed | final status: %s\n", goroutineID, symbol, state.Status)
	fmt.Printf("[#%d:%s] ========================================\n\n", goroutineID, symbol)
}

// handleBreakoutSignal logs the confirmed HTF breakout and appends it to
// the watchlist CSV. This is the signal emission step — no trades are placed.
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

// loadEnv walks up the directory tree from the current working directory until
// it finds a .env file, loads it, and changes the working directory to that
// root. This ensures all relative paths (CSV, output dirs, etc.) resolve
// correctly regardless of where the binary is invoked from.
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

// ===== Entry point =====

func main() {
	tradingDatePtr := flag.String("date", "", "trading session date (YYYY-MM-DD)")
	flag.Parse()
	tradingDate := *tradingDatePtr

	if tradingDate == "" {
		log.Fatal("Usage: htf_main -date YYYY-MM-DD")
	}

	fmt.Printf("=== HTF Intraday Monitor — %s ===\n", tradingDate)
	fmt.Printf("=== Confirmation bars required: %d | First half only: %v ===\n\n",
		htf.BreakoutConfirmationBars, htf.BreakoutFirstHalfOnly)

	loadEnv()

	alpacaKey := os.Getenv("ALPACA_API_KEY")
	alpacaSecret := os.Getenv("ALPACA_SECRET_KEY")
	if alpacaKey == "" || alpacaSecret == "" {
		log.Fatal("ALPACA_API_KEY or ALPACA_SECRET_KEY not set in environment")
	}

	// Load the morning scan results (produced by htf_scanner)
	scanFile := fmt.Sprintf("%s/htf_%s_results.json",
		htf.OutputDir,
		strings.ReplaceAll(tradingDate, "-", ""),
	)

	raw, err := os.ReadFile(scanFile)
	if err != nil {
		log.Fatalf("Could not read scan results from %s: %v\n(Run htf_scanner -date %s first)",
			scanFile, err, tradingDate)
	}

	var scanReport htf.HTFScanReport
	if err := json.Unmarshal(raw, &scanReport); err != nil {
		log.Fatalf("Failed to parse scan report: %v", err)
	}

	if len(scanReport.QualifyingStocks) == 0 {
		fmt.Printf("No HTF candidates in %s — nothing to monitor.\n", scanFile)
		return
	}

	fmt.Printf("Loaded %d HTF candidates from %s\n\n", len(scanReport.QualifyingStocks), scanFile)
	for i, c := range scanReport.QualifyingStocks {
		fmt.Printf("  %d. %-8s  resistance $%.2f  support $%.2f  pole %.1f%% in %d days\n",
			i+1, c.Symbol, c.ResistanceLevel, c.SupportLevel,
			c.Flagpole.GainPct, c.Flagpole.DurationTradingDays)
	}
	fmt.Println()

	// Initialise the global watchlist manager
	watchlistManager = NewWatchlistManager(htf.WatchlistCSVFilename)

	// Launch one goroutine per candidate — mirrors EP's intraday worker pattern.
	var wg sync.WaitGroup
	for i, candidate := range scanReport.QualifyingStocks {
		wg.Add(1)
		go func(idx int, c htf.HTFCandidate) {
			defer wg.Done()
			intradayWorker(alpacaKey, alpacaSecret, c, tradingDate, idx+1)
		}(i, candidate)
	}

	wg.Wait()
	fmt.Println("All intraday workers finished.")
}
