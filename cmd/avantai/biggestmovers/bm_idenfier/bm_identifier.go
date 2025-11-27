package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Configuration
const (
	BaseURL           = "https://api.marketstack.com/v2"
	ConfigFilePath    = "pkg/ep/config.csv"
	MinMarketCap      = 300_000_000.0 // $300M
	MinDollarVolume   = 5_000_000.0   // $5M
	MinReturnPct      = 100.0         // 100%
	MinDurationDays   = 21
	MaxDurationDays   = 315                    // ~15 months
	LiquidityWindow   = 21                     // Days for liquidity calculation
	MinTradingDaysIPO = 126                    // ~6 months before high
	RateLimitDelay    = 220 * time.Millisecond // ~4.5 requests/sec
	MaxRetries        = 3                      // Maximum retry attempts for rate limit errors
	RetryBaseDelay    = 5 * time.Second        // Base delay for exponential backoff
)

// API Response structures
type TickersResponse struct {
	Data []struct {
		Symbol string `json:"symbol"`
		Name   string `json:"name"`
	} `json:"data"`
	Pagination struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Count  int `json:"count"`
		Total  int `json:"total"`
	} `json:"pagination"`
}

type EODResponse struct {
	Data       []EODData `json:"data"`
	Pagination struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Count  int `json:"count"`
		Total  int `json:"total"`
	} `json:"pagination"`
}

type EODData struct {
	Date        string  `json:"date"`
	Open        float64 `json:"open"`
	High        float64 `json:"high"`
	Low         float64 `json:"low"`
	Close       float64 `json:"close"`
	AdjClose    float64 `json:"adj_close"`
	AdjHigh     float64 `json:"adj_high"`
	AdjLow      float64 `json:"adj_low"`
	AdjOpen     float64 `json:"adj_open"`
	AdjVolume   float64 `json:"adj_volume"`
	Volume      float64 `json:"volume"`
	Symbol      string  `json:"symbol"`
	Exchange    string  `json:"exchange"`
	SplitFactor float64 `json:"split_factor"`
}

type QualifyingMove struct {
	Ticker          string    `json:"ticker"`
	StartYear       int       `json:"start_year"`
	LowDate         time.Time `json:"low_date"`
	LowPrice        float64   `json:"low_price"`
	HighDate        time.Time `json:"high_date"`
	HighPrice       float64   `json:"high_price"`
	PercentIncrease float64   `json:"percent_increase"`
	DurationDays    int       `json:"duration_days"`
	MarketCapAtLow  float64   `json:"market_cap_at_low"`
	AvgDollarVolume float64   `json:"avg_dollar_volume"`
}

type Scanner struct {
	apiKey string
	client *http.Client
}

func NewScanner(apiKey string) *Scanner {
	return &Scanner{
		apiKey: apiKey,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Load tickers from config CSV file
func (s *Scanner) LoadTickersFromCSV(filepath string) ([]string, error) {
	fmt.Printf("\n=== Loading tickers from %s ===\n", filepath)

	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read header
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}
	fmt.Printf("  CSV Header: %v\n", header)

	// Read all records
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV records: %w", err)
	}

	tickers := []string{}
	for i, record := range records {
		if len(record) > 0 && record[0] != "" {
			ticker := strings.TrimSpace(record[0])
			if ticker != "" {
				tickers = append(tickers, ticker)
			}
		} else {
			fmt.Printf("  WARNING: Skipping empty row %d\n", i+1)
		}
	}

	fmt.Printf("✓ Loaded %d tickers from config file\n\n", len(tickers))

	// Print first 10 tickers as sample
	sampleCount := 10
	if len(tickers) < sampleCount {
		sampleCount = len(tickers)
	}
	fmt.Printf("Sample tickers: %v\n", tickers[:sampleCount])
	if len(tickers) > sampleCount {
		fmt.Printf("... and %d more\n", len(tickers)-sampleCount)
	}

	return tickers, nil
}

// Fetch all historical EOD data for a ticker with retry logic
func (s *Scanner) GetEODData(ticker string) ([]EODData, error) {
	fmt.Printf("    Fetching EOD data for %s...\n", ticker)
	allData := []EODData{}
	offset := 0
	limit := 100

	for {
		var eodResp EODResponse
		var err error

		// Retry logic for rate limiting
		for attempt := 0; attempt <= MaxRetries; attempt++ {
			// Wait for rate limiter
			time.Sleep(RateLimitDelay)

			url := fmt.Sprintf("%s/eod?access_key=%s&symbols=%s&limit=%d&offset=%d",
				BaseURL, s.apiKey, ticker, limit, offset)

			resp, httpErr := s.client.Get(url)
			if httpErr != nil {
				err = httpErr
				break
			}

			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				err = readErr
				break
			}

			// Check for rate limit error
			if resp.StatusCode == 429 {
				if attempt < MaxRetries {
					// Exponential backoff
					backoffDelay := RetryBaseDelay * time.Duration(1<<uint(attempt))
					fmt.Printf("    Rate limit hit for %s (attempt %d/%d), waiting %v...\n",
						ticker, attempt+1, MaxRetries+1, backoffDelay)
					time.Sleep(backoffDelay)
					continue
				}
				return nil, fmt.Errorf("rate limit exceeded after %d retries for %s", MaxRetries, ticker)
			}

			if resp.StatusCode != 200 {
				fmt.Printf("    ERROR: API returned status %s for %s\n", resp.Status, ticker)
				return nil, fmt.Errorf("API error for %s: %s", ticker, resp.Status)
			}

			if unmarshalErr := json.Unmarshal(body, &eodResp); unmarshalErr != nil {
				return nil, fmt.Errorf("failed to parse EOD response: %w", unmarshalErr)
			}

			// Success - break retry loop
			err = nil
			break
		}

		if err != nil {
			return nil, err
		}

		fmt.Printf("    Received %d data points (offset=%d, total=%d)\n", len(eodResp.Data), offset, len(allData)+len(eodResp.Data))
		allData = append(allData, eodResp.Data...)

		if len(eodResp.Data) < limit {
			break
		}

		offset += limit
	}

	fmt.Printf("    Total data points for %s: %d\n", ticker, len(allData))

	// Sort by date ascending
	sort.Slice(allData, func(i, j int) bool {
		ti, _ := time.Parse("2006-01-02T15:04:05-0700", allData[i].Date)
		tj, _ := time.Parse("2006-01-02T15:04:05-0700", allData[j].Date)
		return ti.Before(tj)
	})

	if len(allData) > 0 {
		firstDate, _ := time.Parse("2006-01-02T15:04:05-0700", allData[0].Date)
		lastDate, _ := time.Parse("2006-01-02T15:04:05-0700", allData[len(allData)-1].Date)
		fmt.Printf("    Date range: %s to %s\n", firstDate.Format("2006-01-02"), lastDate.Format("2006-01-02"))
	}

	return allData, nil
}

// Calculate average dollar volume around a date
func calculateAvgDollarVolume(data []EODData, centerIdx int) float64 {
	start := centerIdx - LiquidityWindow/2
	end := centerIdx + LiquidityWindow/2 + 1

	if start < 0 {
		start = 0
	}
	if end > len(data) {
		end = len(data)
	}

	sum := 0.0
	count := 0
	for i := start; i < end && i <= centerIdx; i++ {
		dollarVol := data[i].Volume * data[i].AdjClose
		sum += dollarVol
		count++
	}

	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// Estimate market cap (simplified - using price * volume as proxy)
func estimateMarketCap(data EODData) float64 {
	return data.AdjClose * data.Volume * 10
}

// Analyze stock for qualifying moves
func (s *Scanner) AnalyzeStock(ticker string, data []EODData) []QualifyingMove {
	if len(data) < MinDurationDays {
		fmt.Printf("    SKIP: %s has insufficient data (%d days < %d minimum)\n", ticker, len(data), MinDurationDays)
		return nil
	}

	fmt.Printf("    Analyzing %s with %d data points...\n", ticker, len(data))
	moves := []QualifyingMove{}

	// Group data by year
	yearData := make(map[int][]int)
	for i, d := range data {
		t, err := time.Parse("2006-01-02T15:04:05-0700", d.Date)
		if err != nil {
			continue
		}
		year := t.Year()
		yearData[year] = append(yearData[year], i)
	}

	fmt.Printf("    Found data spanning %d years\n", len(yearData))

	// Process each year from 2024 backwards
	years := []int{}
	for year := range yearData {
		years = append(years, year)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))

	for _, year := range years {
		if year > 2024 {
			continue
		}

		fmt.Printf("\n    --- Analyzing Year %d for %s ---\n", year, ticker)

		indices := yearData[year]
		if len(indices) == 0 {
			fmt.Printf("    SKIP: No data for year %d\n", year)
			continue
		}

		fmt.Printf("    Year %d has %d trading days\n", year, len(indices))

		// Find the low in this year
		lowIdx := -1
		lowPrice := 999999999.0

		for _, idx := range indices {
			if data[idx].AdjClose < lowPrice && data[idx].AdjClose > 0 {
				lowPrice = data[idx].AdjClose
				lowIdx = idx
			}
		}

		if lowIdx == -1 {
			fmt.Printf("    SKIP: Could not find valid low for year %d\n", year)
			continue
		}

		lowDate, _ := time.Parse("2006-01-02T15:04:05-0700", data[lowIdx].Date)
		fmt.Printf("    Low found: $%.2f on %s (index %d)\n", lowPrice, lowDate.Format("2006-01-02"), lowIdx)

		// Check market cap at low
		marketCap := estimateMarketCap(data[lowIdx])
		fmt.Printf("    Market cap at low: $%.2fM\n", marketCap/1_000_000)
		if marketCap < MinMarketCap {
			fmt.Printf("    FAIL: Market cap $%.2fM < $%.2fM minimum\n", marketCap/1_000_000, MinMarketCap/1_000_000)
			continue
		}

		// Check liquidity at low
		avgDollarVol := calculateAvgDollarVolume(data, lowIdx)
		fmt.Printf("    Avg dollar volume (21-day): $%.2fM\n", avgDollarVol/1_000_000)
		if avgDollarVol < MinDollarVolume {
			fmt.Printf("    FAIL: Dollar volume $%.2fM < $%.2fM minimum\n", avgDollarVol/1_000_000, MinDollarVolume/1_000_000)
			continue
		}

		// Search for high after low in Year Y and entire Year Y+1
		maxSearchDate := time.Date(year+2, 1, 1, 0, 0, 0, 0, time.UTC)
		maxSearchIdx := len(data) - 1
		for i := lowIdx; i < len(data); i++ {
			t, _ := time.Parse("2006-01-02T15:04:05-0700", data[i].Date)
			if t.After(maxSearchDate) {
				maxSearchIdx = i - 1
				break
			}
		}

		fmt.Printf("    Searching for high from index %d to %d (up to %s)\n", lowIdx, maxSearchIdx, maxSearchDate.Format("2006-01-02"))

		// Find the high
		highIdx := -1
		highPrice := 0.0

		for i := lowIdx; i <= maxSearchIdx && i < len(data); i++ {
			if data[i].AdjClose > highPrice {
				highPrice = data[i].AdjClose
				highIdx = i
			}
		}

		if highIdx == -1 || highIdx == lowIdx {
			fmt.Printf("    FAIL: Could not find valid high after low\n")
			continue
		}

		highDate, _ := time.Parse("2006-01-02T15:04:05-0700", data[highIdx].Date)
		fmt.Printf("    High found: $%.2f on %s (index %d)\n", highPrice, highDate.Format("2006-01-02"), highIdx)

		// Check duration constraints
		durationDays := int(highDate.Sub(lowDate).Hours() / 24)
		fmt.Printf("    Duration: %d days\n", durationDays)
		if durationDays < MinDurationDays {
			fmt.Printf("    FAIL: Duration %d days < %d minimum\n", durationDays, MinDurationDays)
			continue
		}
		if durationDays > MaxDurationDays {
			fmt.Printf("    FAIL: Duration %d days > %d maximum\n", durationDays, MaxDurationDays)
			continue
		}

		// Check IPO rule (6 months before high)
		if lowIdx < MinTradingDaysIPO {
			fmt.Printf("    FAIL: IPO rule - only %d trading days before high (need %d)\n", lowIdx, MinTradingDaysIPO)
			continue
		}

		// Check return percentage
		returnPct := ((highPrice - lowPrice) / lowPrice) * 100
		fmt.Printf("    Return: %.2f%% ($%.2f -> $%.2f)\n", returnPct, lowPrice, highPrice)
		if returnPct < MinReturnPct {
			fmt.Printf("    FAIL: Return %.2f%% < %.2f%% minimum\n", returnPct, MinReturnPct)
			continue
		}

		// Qualifying move found!
		fmt.Printf("    ✓ QUALIFYING MOVE FOUND! %.2f%% gain over %d days\n", returnPct, durationDays)
		moves = append(moves, QualifyingMove{
			Ticker:          ticker,
			StartYear:       year,
			LowDate:         lowDate,
			LowPrice:        lowPrice,
			HighDate:        highDate,
			HighPrice:       highPrice,
			PercentIncrease: returnPct,
			DurationDays:    durationDays,
			MarketCapAtLow:  marketCap,
			AvgDollarVolume: avgDollarVol,
		})
	}

	if len(moves) > 0 {
		fmt.Printf("\n    ✓✓✓ Total qualifying moves for %s: %d\n", ticker, len(moves))
	} else {
		fmt.Printf("\n    No qualifying moves found for %s\n", ticker)
	}

	return moves
}

// Scan all stocks sequentially
func (s *Scanner) ScanAllStocks() ([]QualifyingMove, error) {
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("STARTING SEQUENTIAL SCAN")
	fmt.Println(strings.Repeat("=", 70))

	tickers, err := s.LoadTickersFromCSV(ConfigFilePath)
	if err != nil {
		return nil, err
	}

	allMoves := []QualifyingMove{}
	successCount := 0
	errorCount := 0
	startTime := time.Now()

	for i, ticker := range tickers {
		elapsed := time.Since(startTime)
		fmt.Printf("\n[%d/%d] %.1f%% complete (elapsed: %s) =================\n",
			i+1, len(tickers), float64(i+1)/float64(len(tickers))*100, elapsed.Round(time.Second))
		fmt.Printf("Processing: %s\n", ticker)
		fmt.Printf("Progress: %d success, %d errors, %d qualifying moves found\n", successCount, errorCount, len(allMoves))

		data, err := s.GetEODData(ticker)
		if err != nil {
			log.Printf("  ERROR fetching data for %s: %v\n", ticker, err)
			errorCount++
			continue
		}

		successCount++
		moves := s.AnalyzeStock(ticker, data)
		if len(moves) > 0 {
			fmt.Printf("\n  ★★★ FOUND %d QUALIFYING MOVE(S) FOR %s ★★★\n", len(moves), ticker)
			for _, move := range moves {
				fmt.Printf("    → Year %d: %.2f%% gain ($%.2f to $%.2f) over %d days\n",
					move.StartYear, move.PercentIncrease, move.LowPrice, move.HighPrice, move.DurationDays)
			}
			allMoves = append(allMoves, moves...)
		}
	}

	totalTime := time.Since(startTime)
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("SCAN COMPLETE!")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("Total time: %s\n", totalTime.Round(time.Second))
	fmt.Printf("Stocks processed: %d\n", successCount)
	fmt.Printf("Errors: %d\n", errorCount)
	fmt.Printf("Total qualifying moves: %d\n", len(allMoves))

	// Group by year
	yearCounts := make(map[int]int)
	for _, move := range allMoves {
		yearCounts[move.StartYear]++
	}

	years := []int{}
	for year := range yearCounts {
		years = append(years, year)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))

	fmt.Println("\nMoves by year:")
	for _, year := range years {
		fmt.Printf("  %d: %d moves\n", year, yearCounts[year])
	}

	return allMoves, nil
}

// Scan for a specific year
func (s *Scanner) ScanForYear(year int) ([]QualifyingMove, error) {
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("STARTING SCAN FOR YEAR %d\n", year)
	fmt.Println(strings.Repeat("=", 70))

	tickers, err := s.LoadTickersFromCSV(ConfigFilePath)
	if err != nil {
		return nil, err
	}

	allMoves := []QualifyingMove{}
	successCount := 0
	errorCount := 0
	startTime := time.Now()

	for i, ticker := range tickers {
		elapsed := time.Since(startTime)
		fmt.Printf("\n[%d/%d] %.1f%% complete (elapsed: %s) =================\n",
			i+1, len(tickers), float64(i+1)/float64(len(tickers))*100, elapsed.Round(time.Second))
		fmt.Printf("Processing: %s for year %d\n", ticker, year)

		data, err := s.GetEODData(ticker)
		if err != nil {
			log.Printf("  ERROR fetching data for %s: %v\n", ticker, err)
			errorCount++
			continue
		}

		successCount++
		moves := s.AnalyzeStock(ticker, data)

		// Filter for the specific year
		for _, move := range moves {
			if move.StartYear == year {
				fmt.Printf("\n  ★★★ FOUND QUALIFYING MOVE FOR %s IN YEAR %d ★★★\n", ticker, year)
				fmt.Printf("    → %.2f%% gain ($%.2f to $%.2f) over %d days\n",
					move.PercentIncrease, move.LowPrice, move.HighPrice, move.DurationDays)
				allMoves = append(allMoves, move)
			}
		}
	}

	totalTime := time.Since(startTime)
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("SCAN COMPLETE FOR YEAR %d!\n", year)
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("Total time: %s\n", totalTime.Round(time.Second))
	fmt.Printf("Stocks processed: %d\n", successCount)
	fmt.Printf("Errors: %d\n", errorCount)
	fmt.Printf("Qualifying moves for %d: %d\n", year, len(allMoves))

	return allMoves, nil
}

// Save results to JSON
func SaveResults(moves []QualifyingMove, filename string) error {
	fmt.Printf("\nSaving results to %s...\n", filename)
	data, err := json.MarshalIndent(moves, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return err
	}

	fmt.Printf("✓ Successfully saved %d moves to %s (%.2f KB)\n", len(moves), filename, float64(len(data))/1024)
	return nil
}

func main() {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║      BIGGEST MOVERS DATASET SCANNER v2.0 (SEQUENTIAL)          ║")
	fmt.Println("║           Using Marketstack API v2                              ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝\n")

	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, checking for environment variable")
	}

	apiKey := os.Getenv("MARKETSTACK_TOKEN")
	if apiKey == "" {
		log.Fatal("ERROR: Please set MARKETSTACK_TOKEN environment variable")
	}
	fmt.Printf("✓ API Key loaded: %s...%s\n", apiKey[:8], apiKey[len(apiKey)-4:])

	scanner := NewScanner(apiKey)

	fmt.Println("\nConfiguration:")
	fmt.Printf("  Config File: %s\n", ConfigFilePath)
	fmt.Printf("  API Version: v2 (https://api.marketstack.com/v2)\n")
	fmt.Printf("  Rate Limit: ~4.5 requests/sec (API limit: 5 req/sec)\n")
	fmt.Printf("  Rate Limit Delay: %v\n", RateLimitDelay)
	fmt.Printf("  Max Retries: %d\n", MaxRetries)
	fmt.Printf("  Retry Base Delay: %v\n", RetryBaseDelay)
	fmt.Printf("  Min Return: %.0f%%\n", MinReturnPct)
	fmt.Printf("  Min Duration: %d days\n", MinDurationDays)
	fmt.Printf("  Max Duration: %d days (~15 months)\n", MaxDurationDays)
	fmt.Printf("  Min Market Cap: $%.0fM\n", MinMarketCap/1_000_000)
	fmt.Printf("  Min Dollar Volume: $%.0fM\n", MinDollarVolume/1_000_000)
	fmt.Printf("  Liquidity Window: %d days\n", LiquidityWindow)
	fmt.Printf("  Min Trading Days (IPO): %d days (~6 months)\n", MinTradingDaysIPO)

	// Sequential scan for all years
	fmt.Println("\n>>> Starting sequential full historical scan...")
	moves, err := scanner.ScanAllStocks()
	if err != nil {
		log.Fatalf("❌ Scan failed: %v", err)
	}

	fmt.Printf("\n╔══════════════════════════════════════════════════════════════════╗")
	fmt.Printf("\n║ FINAL RESULTS: Found %d qualifying moves                        ", len(moves))
	fmt.Printf("\n╚══════════════════════════════════════════════════════════════════╝\n")

	// Save results
	if err := SaveResults(moves, "biggest_movers_all.json"); err != nil {
		log.Printf("❌ Failed to save results: %v", err)
	}

	// Print top 10 by percentage
	if len(moves) > 0 {
		sort.Slice(moves, func(i, j int) bool {
			return moves[i].PercentIncrease > moves[j].PercentIncrease
		})

		fmt.Println("\n🏆 Top 10 Biggest Movers by Percentage:")
		limit := 10
		if len(moves) < limit {
			limit = len(moves)
		}
		for i := 0; i < limit; i++ {
			m := moves[i]
			fmt.Printf("%2d. %s (%d): %.2f%% ($%.2f → $%.2f) in %d days\n",
				i+1, m.Ticker, m.StartYear, m.PercentIncrease, m.LowPrice, m.HighPrice, m.DurationDays)
		}
	}

	fmt.Println("\n✓ Scan complete!")

	// Example: Scan for specific year (uncomment to use)
	// year := 2023
	// fmt.Printf("\n>>> Scanning for year %d...\n", year)
	// moves, err := scanner.ScanForYear(year)
	// if err != nil {
	//     log.Fatalf("❌ Scan failed: %v", err)
	// }
	// fmt.Printf("\n✓ Found %d qualifying moves for %d\n", len(moves), year)
	// SaveResults(moves, fmt.Sprintf("biggest_movers_%d.json", year))
}
