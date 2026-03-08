package main

// Component 1: HTF Morning Scanner / Pre-Market Filter
//
// This program runs once at the start of each trading day (or pre-market) to
// build the HTF watchlist. It:
//   1. Loads the stock universe from the shared config CSV.
//   2. Fetches historical daily bars from Alpaca for each ticker.
//   3. Applies all HTF filter criteria (flagpole, flag, volume, MAs).
//   4. Saves qualifying candidates to data/htf/htf_YYYYMMDD_results.json.
//
// Usage:
//   go run htf_scanner.go [-date 2025-03-06]
//
// The output JSON is consumed by htf_main to drive intraday monitoring.

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
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

// maxConcurrent limits simultaneous Alpaca API requests to avoid rate-limiting.
const maxConcurrent = 5

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

// ===== Alpaca API client (daily bars) =====

// alpacaDailyBar matches the Alpaca API bar object for any timeframe.
type alpacaDailyBar struct {
	T string  `json:"t"` // RFC3339 timestamp
	O float64 `json:"o"` // Open
	H float64 `json:"h"` // High
	L float64 `json:"l"` // Low
	C float64 `json:"c"` // Close
	V float64 `json:"v"` // Volume
}

type alpacaBarsResponse struct {
	Bars          []alpacaDailyBar `json:"bars"`
	Symbol        string           `json:"symbol"`
	NextPageToken *string          `json:"next_page_token"`
}

// fetchAlpacaDailyBars retrieves historical 1-Day bars from Alpaca for a
// single symbol between startDate and endDate (inclusive, YYYY-MM-DD format).
// It follows pagination to return all available bars in the window.
// Bars are returned sorted oldest-to-newest.
func fetchAlpacaDailyBars(apiKey, apiSecret, symbol, startDate, endDate string) ([]htf.DailyBar, error) {
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

		var alpacaResp alpacaBarsResponse
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

	// Ensure oldest-first ordering for the filter algorithms.
	sort.Slice(allBars, func(i, j int) bool {
		return allBars[i].Date.Before(allBars[j].Date)
	})

	return allBars, nil
}

// ===== Stock universe loader =====

// loadSymbolsFromCSV reads ticker symbols from the shared stock universe file.
// It skips the header row and empty rows, matching the EP strategy's approach.
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
			// Skip header row
			continue
		}
		if len(row) > 0 && strings.TrimSpace(row[0]) != "" {
			symbols = append(symbols, strings.TrimSpace(strings.ToUpper(row[0])))
		}
	}
	return symbols, nil
}

// ===== Concurrent scan worker =====

type scanResult struct {
	symbol    string
	candidate *htf.HTFCandidate
	err       error
}

func main() {
	scanDatePtr := flag.String("date", time.Now().Format("2006-01-02"), "trading date to scan for (YYYY-MM-DD)")
	flag.Parse()
	scanDate := *scanDatePtr

	fmt.Printf("=== HTF Morning Scanner — %s ===\n\n", scanDate)

	loadEnv()

	alpacaKey := os.Getenv("ALPACA_API_KEY")
	alpacaSecret := os.Getenv("ALPACA_SECRET_KEY")
	if alpacaKey == "" || alpacaSecret == "" {
		log.Fatal("ALPACA_API_KEY or ALPACA_SECRET_KEY not set in environment")
	}

	// Load stock universe
	symbols, err := loadSymbolsFromCSV(htf.StockUniverseCSVPath)
	if err != nil {
		log.Fatalf("Failed to load stock universe from %s: %v", htf.StockUniverseCSVPath, err)
	}
	fmt.Printf("Loaded %d symbols from %s\n", len(symbols), htf.StockUniverseCSVPath)

	// Compute the historical lookback date range.
	// We look back HistoricalLookbackDays calendar days from the scan date.
	endDate := scanDate
	scanTime, err := time.Parse("2006-01-02", scanDate)
	if err != nil {
		log.Fatalf("Invalid scan date %q: %v", scanDate, err)
	}
	startDate := scanTime.AddDate(0, 0, -htf.HistoricalLookbackDays).Format("2006-01-02")

	fmt.Printf("Historical data range: %s → %s\n", startDate, endDate)
	fmt.Printf("HTF filter thresholds: pole >= %.0f%% in <= %d days | flag %d–%d days | range <= %.0f%% | pullback %.0f–%.0f%%\n\n",
		htf.FlagpoleMinGainPct, htf.FlagpoleMaxTradingDays,
		htf.FlagMinTradingDays, htf.FlagMaxTradingDays,
		htf.FlagMaxRangePct,
		htf.FlagMinPullbackPct, htf.FlagMaxPullbackPct,
	)

	// Bounded concurrency: semaphore pattern (push to acquire, pop to release).
	semaphore := make(chan struct{}, maxConcurrent)

	resultsCh := make(chan scanResult, len(symbols))
	var wg sync.WaitGroup

	for _, sym := range symbols {
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()

			// Acquire rate-limit slot
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			bars, err := fetchAlpacaDailyBars(alpacaKey, alpacaSecret, symbol, startDate, endDate)
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

	// Close results channel after all goroutines finish.
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Collect results
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

	// Sort qualifying candidates by flagpole gain descending (strongest move first).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Flagpole.GainPct > candidates[j].Flagpole.GainPct
	})

	fmt.Printf("\n=== HTF Scan Complete ===\n")
	fmt.Printf("Symbols scanned : %d\n", scanned)
	fmt.Printf("Errors          : %d\n", errCount)
	fmt.Printf("HTF candidates  : %d\n\n", len(candidates))

	// Build the scan report (mirrors BacktestReport in EP)
	report := htf.HTFScanReport{
		ScanDate:         scanDate,
		GeneratedAt:      time.Now().Format(time.RFC3339),
		TotalCandidates:  len(candidates),
		QualifyingStocks: candidates,
	}

	// Save to data/htf/htf_YYYYMMDD_results.json
	if err := os.MkdirAll(htf.OutputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory %s: %v", htf.OutputDir, err)
	}

	filename := fmt.Sprintf("%s/htf_%s_results.json",
		htf.OutputDir,
		strings.ReplaceAll(scanDate, "-", ""),
	)

	jsonData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal scan report: %v", err)
	}

	if err := os.WriteFile(filename, jsonData, 0644); err != nil {
		log.Fatalf("Failed to write scan report to %s: %v", filename, err)
	}

	fmt.Printf("Scan report saved to: %s\n", filename)

	// Print summary table of qualifying candidates
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
}
