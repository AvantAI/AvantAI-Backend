package ep

import (
	"encoding/csv"
	"encoding/json"
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
)

// Marketstack v2 API Response Structures (based on official documentation)
type MarketstackEODResponse struct {
	Pagination struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Count  int `json:"count"`
		Total  int `json:"total"`
	} `json:"pagination"`
	Data []MarketstackEODData `json:"data"`
}

type MarketstackEODData struct {
	Open         float64 `json:"open"`
	High         float64 `json:"high"`
	Low          float64 `json:"low"`
	Close        float64 `json:"close"`
	Volume       float64 `json:"volume"`
	AdjHigh      float64 `json:"adj_high"`
	AdjLow       float64 `json:"adj_low"`
	AdjClose     float64 `json:"adj_close"`
	AdjOpen      float64 `json:"adj_open"`
	AdjVolume    float64 `json:"adj_volume"`
	SplitFactor  float64 `json:"split_factor"`
	Dividend     float64 `json:"dividend"`
	Name         string  `json:"name"`
	ExchangeCode string  `json:"exchange_code"`
	AssetType    string  `json:"asset_type"`
	PriceCurrency string `json:"price_currency"`
	Symbol       string  `json:"symbol"`
	Exchange     string  `json:"exchange"`
	Date         string  `json:"date"`
}

// Marketstack Ticker Info Response (for company details)
type MarketstackTickerInfoResponse struct {
	Data MarketstackTickerInfo `json:"data"`
}

type MarketstackTickerInfo struct {
	Name                string  `json:"name"`
	Ticker              string  `json:"ticker"`
	ItemType            string  `json:"item_type"`
	Sector              string  `json:"sector"`
	Industry            string  `json:"industry"`
	ExchangeCode        string  `json:"exchange_code"`
	FullTimeEmployees   string  `json:"full_time_employees"`
	IPODate             *string `json:"ipo_date"`
	DateFounded         *string `json:"date_founded"`
}

// Our internal structures (compatible with original algorithm)
type StockData struct {
	Symbol                     string  `json:"symbol"`
	Timestamp                  string  `json:"timestamp"`
	Open                       float64 `json:"open"`
	High                       float64 `json:"high"`
	Low                        float64 `json:"low"`
	Close                      float64 `json:"close"`
	Volume                     int64   `json:"volume"`
	PreviousClose              float64 `json:"previous_close"`
	Change                     float64 `json:"change"`
	ChangePercent              float64 `json:"change_percent"`
	ExtendedHoursQuote         float64 `json:"extended_hours_quote"`
	ExtendedHoursChange        float64 `json:"extended_hours_change"`
	ExtendedHoursChangePercent float64 `json:"extended_hours_change_percent"`
	Exchange                   string  `json:"exchange"`
	Name                       string  `json:"name"`
}

type FilteredStock struct {
	Symbol    string     `json:"symbol"`
	StockInfo StockStats `json:"stock_info"`
}

type StockStats struct {
	Timestamp string  `json:"timestamp"`
	MarketCap float64 `json:"market_cap"`
	DolVol    float64 `json:"dolvol"` // Average dollar volume over 21 days
	GapUp     float64 `json:"gap-up"` // Gap up percentage
	ADR       float64 `json:"adr"`    // Average daily range
	Name      string  `json:"name"`
	Exchange  string  `json:"exchange"`
	Sector    string  `json:"sector"`
	Industry  string  `json:"industry"`
}

// Configuration (same filtering criteria)
const (
	MIN_DOLLAR_VOLUME  = 10000000.0  // Minimum Dollar Volume
	MIN_GAP_UP_PERCENT = 10.0        // Minimum Gap Up in percent
	MIN_ADR_PERCENT    = 4.0         // Minimum Average Daily Range in percent
	MIN_MARKET_CAP     = 200000000.0 // Minimum Market Cap in dollars
	
	
	// Marketstack API rate limiting (conservative for free tier)
	API_CALLS_PER_SECOND = 2     // 5 calls per second max, using 2 to be safe
	MAX_CONCURRENT       = 3     // Limit concurrent requests
)

func FilterStocksMarketstack(apiKey string) {
	fmt.Println("=== Starting 3-stage stock filter program with Marketstack v2 API ===")
	
	// Stage 1: Get individual EOD data and filter by gap up
	fmt.Println("\n🔍 Stage 1: Fetching EOD data and filtering by gap up...")
	gapUpStocks, err := stage1FilterByGapUpMarketstackV2(apiKey)
	if err != nil {
		log.Fatalf("❌ Error in Stage 1: %v", err)
	}

	fmt.Printf("✅ Stage 1 complete. Found %d stocks with significant gap up.\n", len(gapUpStocks))

	if len(gapUpStocks) == 0 {
		fmt.Println("⚠️ No stocks found with gap up criteria. Exiting.")
		return
	}

	// Stage 2: Estimate market cap (Marketstack doesn't provide direct market cap)
	fmt.Println("\n💰 Stage 2: Estimating market cap...")
	marketCapStocks, err := stage2FilterByMarketCapMarketstackV2(apiKey, gapUpStocks)
	if err != nil {
		log.Fatalf("❌ Error in Stage 2: %v", err)
	}

	fmt.Printf("✅ Stage 2 complete. Found %d stocks with sufficient estimated market cap.\n", len(marketCapStocks))

	if len(marketCapStocks) == 0 {
		fmt.Println("⚠️ No stocks found with market cap criteria. Exiting.")
		return
	}

	// Stage 3: Calculate and filter by dollar volume and ADR
	fmt.Println("\n📊 Stage 3: Calculating historical metrics (DolVol and ADR)...")
	finalFilteredStocks, err := stage3FilterByHistoricalMetricsMarketstackV2(apiKey, marketCapStocks)
	if err != nil {
		log.Fatalf("❌ Error in Stage 3: %v", err)
	}

	// Output results to JSON file
	err = outputToJSON(finalFilteredStocks)
	if err != nil {
		log.Fatalf("❌ Error writing to JSON: %v", err)
	}

	fmt.Printf("\n🎉 Filter complete! Found %d stocks matching all criteria.\n", len(finalFilteredStocks))
	fmt.Println("📁 Results written to filtered_stocks_marketstack.json")
}

// Stage 1: Filter by gap up using Marketstack v2 EOD endpoint
func stage1FilterByGapUpMarketstackV2(apiKey string) ([]StockData, error) {
	fmt.Println("📋 Loading stock symbols...")
	symbols, err := getStockSymbols()
	if err != nil {
		return nil, fmt.Errorf("failed to get stock symbols: %v", err)
	}

	fmt.Printf("📈 Processing %d symbols for gap up analysis...\n", len(symbols))

	var gapUpStocks []StockData
	var mu sync.Mutex
	gapUpCount := 0
	processedCount := 0

	// Create a semaphore to limit concurrent requests
	semaphore := make(chan struct{}, MAX_CONCURRENT)
	var wg sync.WaitGroup

	// Rate limiter
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	for _, symbol := range symbols {
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()
			
			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			
			// Wait for rate limiter
			<-rateLimiter

			mu.Lock()
			processedCount++
			if processedCount%50 == 0 {
				fmt.Printf("   Processed %d/%d symbols...\n", processedCount, len(symbols))
			}
			mu.Unlock()

			// Get current and previous day data for gap calculation
			fmt.Printf("🔍 Processing %s...\n", sym)
			
			currentData, previousData, err := getCurrentAndPreviousDataV2(apiKey, sym)
			if err != nil {
				fmt.Printf("⚠️ Error fetching data for %s: %v\n", sym, err)
				return
			}

			if currentData == nil || previousData == nil {
				fmt.Printf("⚠️ Insufficient data for %s (need 2 days)\n", sym)
				return
			}

			// Debug output for gap calculation
			fmt.Printf("📊 %s Data:\n", sym)
			fmt.Printf("   Current: Open=%.2f, Close=%.2f, Date=%s\n", 
				currentData.Open, currentData.Close, currentData.Date)
			fmt.Printf("   Previous: Close=%.2f, Date=%s\n", 
				previousData.Close, previousData.Date)

			// Calculate gap up percentage: ((CurrentOpen - PreviousClose)/PreviousClose) * 100
			if previousData.Close <= 0 {
				fmt.Printf("⚠️ Invalid previous close price for %s: %.2f\n", sym, previousData.Close)
				return
			}

			gapUp := ((currentData.Open - previousData.Close) / previousData.Close) * 100
			gapAmount := currentData.Open - previousData.Close

			fmt.Printf("   Gap Analysis: %.2f%% (Gap Amount: $%.2f)\n", gapUp, gapAmount)

			if gapUp >= MIN_GAP_UP_PERCENT && gapAmount > 0 {
				stockData := StockData{
					Symbol:                     currentData.Symbol,
					Timestamp:                  currentData.Date,
					Open:                       currentData.Open,
					High:                       currentData.High,
					Low:                        currentData.Low,
					Close:                      currentData.Close,
					Volume:                     int64(currentData.Volume),
					PreviousClose:              previousData.Close,
					Change:                     currentData.Close - previousData.Close,
					ChangePercent:              ((currentData.Close - previousData.Close) / previousData.Close) * 100,
					ExtendedHoursQuote:         currentData.Close,
					ExtendedHoursChange:        currentData.Close - previousData.Close,
					ExtendedHoursChangePercent: gapUp,
					Exchange:                   currentData.Exchange,
					Name:                       currentData.Name,
				}

				mu.Lock()
				fmt.Printf("✅ %s qualifies! Gap up: %.2f%% (Min: %.2f%%)\n", sym, gapUp, MIN_GAP_UP_PERCENT)
				gapUpStocks = append(gapUpStocks, stockData)
				gapUpCount++
				mu.Unlock()
			} else {
				fmt.Printf("❌ %s rejected: Gap up %.2f%% < %.2f%% or negative gap\n", 
					sym, gapUp, MIN_GAP_UP_PERCENT)
			}
		}(symbol)
	}

	wg.Wait()
	fmt.Printf("📈 Gap up analysis complete: %d qualifying stocks found\n", gapUpCount)
	return gapUpStocks, nil
}

// Get current and previous day data from Marketstack v2 EOD endpoint
func getCurrentAndPreviousDataV2(apiKey string, symbol string) (*MarketstackEODData, *MarketstackEODData, error) {
	// Get last 3 days of data to ensure we have 2 trading days
	url := fmt.Sprintf("https://api.marketstack.com/v2/eod?access_key=%s&symbols=%s&limit=3&sort=DESC", 
		apiKey, symbol)

	fmt.Printf("🌐 API Call: %s\n", url)

	resp, err := http.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %v", err)
	}

	// Debug: print raw response for first few requests
	if len(body) < 500 {
		fmt.Printf("📄 Raw API Response for %s: %s\n", symbol, string(body))
	}

	var eodResponse MarketstackEODResponse
	if err := json.Unmarshal(body, &eodResponse); err != nil {
		return nil, nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	fmt.Printf("📊 API returned %d records for %s\n", len(eodResponse.Data), symbol)

	if len(eodResponse.Data) < 2 {
		return nil, nil, fmt.Errorf("insufficient data: only %d records", len(eodResponse.Data))
	}

	// Marketstack returns data in DESC order (newest first)
	current := &eodResponse.Data[0]   // Most recent trading day
	previous := &eodResponse.Data[1]  // Previous trading day

	fmt.Printf("📅 Current: %s, Previous: %s\n", current.Date, previous.Date)

	return current, previous, nil
}

// Stage 2: Estimate market cap (Marketstack doesn't provide direct market cap)
func stage2FilterByMarketCapMarketstackV2(apiKey string, stocks []StockData) ([]FilteredStock, error) {
	var marketCapStocks []FilteredStock
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	fmt.Printf("💰 Analyzing market cap for %d stocks...\n", len(stocks))

	for i, stock := range stocks {
		fmt.Printf("🏢 [%d/%d] Processing market cap for %s...\n", i+1, len(stocks), stock.Symbol)
		
		// Rate limiting
		<-rateLimiter

		// Get additional company info
		companyInfo, err := getTickerInfoV2(apiKey, stock.Symbol)
		if err != nil {
			fmt.Printf("⚠️ Could not get company info for %s: %v\n", stock.Symbol, err)
		}

		// Estimate market cap using multiple methods
		estimatedMarketCap := estimateMarketCapImproved(stock, companyInfo)

		fmt.Printf("💰 %s Market Cap Analysis:\n", stock.Symbol)
		fmt.Printf("   Price: $%.2f\n", stock.Close)
		fmt.Printf("   Volume: %d shares\n", stock.Volume)
		fmt.Printf("   Estimated Market Cap: $%.0f\n", estimatedMarketCap)
		fmt.Printf("   Required Minimum: $%.0f\n", MIN_MARKET_CAP)

		if estimatedMarketCap >= MIN_MARKET_CAP {
			sector := "Unknown"
			industry := "Unknown"
			name := stock.Name

			if companyInfo != nil {
				if companyInfo.Sector != "" {
					sector = companyInfo.Sector
				}
				if companyInfo.Industry != "" {
					industry = companyInfo.Industry
				}
				if companyInfo.Name != "" {
					name = companyInfo.Name
				}
			}

			filteredStock := FilteredStock{
				Symbol: stock.Symbol,
				StockInfo: StockStats{
					Timestamp: stock.Timestamp,
					MarketCap: estimatedMarketCap,
					GapUp:     stock.ExtendedHoursChangePercent,
					Name:      name,
					Exchange:  stock.Exchange,
					Sector:    sector,
					Industry:  industry,
				},
			}

			marketCapStocks = append(marketCapStocks, filteredStock)
			fmt.Printf("✅ %s added to market cap filtered list\n", stock.Symbol)
		} else {
			fmt.Printf("❌ %s rejected: Market cap $%.0f < $%.0f\n", 
				stock.Symbol, estimatedMarketCap, MIN_MARKET_CAP)
		}
	}

	return marketCapStocks, nil
}

// Get ticker info from Marketstack v2
func getTickerInfoV2(apiKey string, symbol string) (*MarketstackTickerInfo, error) {
	url := fmt.Sprintf("https://api.marketstack.com/v2/tickerinfo?access_key=%s&ticker=%s", 
		apiKey, symbol)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tickerInfoResponse MarketstackTickerInfoResponse
	if err := json.Unmarshal(body, &tickerInfoResponse); err != nil {
		return nil, err
	}

	return &tickerInfoResponse.Data, nil
}

// Improved market cap estimation
func estimateMarketCapImproved(stock StockData, companyInfo *MarketstackTickerInfo) float64 {
	// Method 1: Use volume and price as a rough indicator
	// High volume often correlates with larger market cap companies
	volumeBasedEstimate := float64(stock.Volume) * stock.Close * 50

	// Method 2: Price-based estimation (higher price often indicates larger companies)
	priceBasedEstimate := stock.Close * 1000000 * 10

	// Method 3: Use employee count if available (rough: $1M market cap per employee)
	employeeBasedEstimate := volumeBasedEstimate
	if companyInfo != nil && companyInfo.FullTimeEmployees != "" {
		if employees, err := strconv.Atoi(companyInfo.FullTimeEmployees); err == nil {
			employeeBasedEstimate = float64(employees) * 1000000
			fmt.Printf("   Employee-based estimate: %d employees -> $%.0f\n", 
				employees, employeeBasedEstimate)
		}
	}

	// Take the maximum of all estimates (conservative approach)
	estimates := []float64{volumeBasedEstimate, priceBasedEstimate, employeeBasedEstimate}
	maxEstimate := estimates[0]
	for _, est := range estimates {
		if est > maxEstimate {
			maxEstimate = est
		}
	}

	fmt.Printf("   Volume-based: $%.0f, Price-based: $%.0f, Final: $%.0f\n", 
		volumeBasedEstimate, priceBasedEstimate, maxEstimate)

	return maxEstimate
}

// Stage 3: Calculate and filter by historical metrics using Marketstack v2
func stage3FilterByHistoricalMetricsMarketstackV2(apiKey string, stocks []FilteredStock) ([]FilteredStock, error) {
	var finalStocks []FilteredStock
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	fmt.Printf("📊 Calculating historical metrics for %d stocks...\n", len(stocks))

	for i, stock := range stocks {
		fmt.Printf("📈 [%d/%d] Calculating metrics for %s...\n", i+1, len(stocks), stock.Symbol)
		
		// Rate limiting
		<-rateLimiter

		// Get 30 days of historical data to ensure we have 21 trading days
		historicalData, err := getHistoricalDataV2(apiKey, stock.Symbol, 30)
		if err != nil {
			fmt.Printf("⚠️ Error getting historical data for %s: %v\n", stock.Symbol, err)
			continue
		}

		// Calculate 21-day average metrics
		dolVol, adr, err := calculateHistoricalMetricsV2(historicalData, stock.Symbol)
		if err != nil {
			fmt.Printf("⚠️ Error calculating metrics for %s: %v\n", stock.Symbol, err)
			continue
		}

		fmt.Printf("📊 %s Historical Metrics:\n", stock.Symbol)
		fmt.Printf("   Dollar Volume (21-day avg): $%.0f (Min: $%.0f)\n", dolVol, MIN_DOLLAR_VOLUME)
		fmt.Printf("   ADR (21-day avg): %.2f%% (Min: %.2f%%)\n", adr, MIN_ADR_PERCENT)

		// Update stock with calculated metrics
		stock.StockInfo.DolVol = dolVol
		stock.StockInfo.ADR = adr

		// Apply final filtering criteria
		if dolVol >= MIN_DOLLAR_VOLUME && adr >= MIN_ADR_PERCENT {
			finalStocks = append(finalStocks, stock)
			fmt.Printf("✅ %s added to final list!\n", stock.Symbol)
		} else {
			fmt.Printf("❌ %s rejected: DolVol=$%.0f<%0.f OR ADR=%.2f%%<%.2f%%\n",
				stock.Symbol, dolVol, MIN_DOLLAR_VOLUME, adr, MIN_ADR_PERCENT)
		}
	}

	return finalStocks, nil
}

// Get historical data from Marketstack v2
func getHistoricalDataV2(apiKey string, symbol string, days int) ([]MarketstackEODData, error) {
	url := fmt.Sprintf("https://api.marketstack.com/v2/eod?access_key=%s&symbols=%s&limit=%d&sort=DESC", 
		apiKey, symbol, days)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var eodResponse MarketstackEODResponse
	if err := json.Unmarshal(body, &eodResponse); err != nil {
		return nil, fmt.Errorf("failed to parse historical data JSON: %v", err)
	}

	fmt.Printf("📊 Retrieved %d days of historical data for %s\n", len(eodResponse.Data), symbol)

	return eodResponse.Data, nil
}

// Calculate historical metrics for Marketstack v2 data
func calculateHistoricalMetricsV2(historicalData []MarketstackEODData, symbol string) (float64, float64, error) {
	// Sort data by date (newest first) - should already be sorted from API
	sort.Slice(historicalData, func(i, j int) bool {
		return historicalData[i].Date > historicalData[j].Date
	})

	// Check if we have enough data (need at least 21 trading days)
	if len(historicalData) < 21 {
		return 0, 0, fmt.Errorf("insufficient historical data: only %d days available, need 21", len(historicalData))
	}

	var dolVolSum float64 = 0
	var adrSum float64 = 0
	validDays := 0

	fmt.Printf("📊 Processing 21 days of historical data for %s:\n", symbol)

	// Process the last 21 days
	for i := 0; i < 21 && i < len(historicalData); i++ {
		dataItem := historicalData[i]

		// Validate data
		if dataItem.Close <= 0 || dataItem.Volume <= 0 || dataItem.High <= 0 || dataItem.Low <= 0 {
			fmt.Printf("   Day %d (%s): Invalid data, skipping\n", i+1, dataItem.Date)
			continue
		}

		// Calculate dollar volume: Volume * Close
		dolVol := dataItem.Volume * dataItem.Close
		dolVolSum += dolVol

		// Calculate daily range: ((High - Low) / Low) * 100
		dailyRange := ((dataItem.High - dataItem.Low) / dataItem.Low) * 100
		adrSum += dailyRange

		if i < 5 { // Show first 5 days as sample
			fmt.Printf("   Day %d (%s): Vol=%.0f, Close=$%.2f, DolVol=$%.0f, Range=%.2f%%\n", 
				i+1, dataItem.Date, dataItem.Volume, dataItem.Close, dolVol, dailyRange)
		}

		validDays++
	}

	if validDays == 0 {
		return 0, 0, fmt.Errorf("no valid historical data found")
	}

	// Calculate averages
	avgDolVol := dolVolSum / float64(validDays)
	avgADR := adrSum / float64(validDays)

	fmt.Printf("   Calculations based on %d valid days\n", validDays)
	fmt.Printf("   Average Dollar Volume: $%.0f\n", avgDolVol)
	fmt.Printf("   Average Daily Range: %.2f%%\n", avgADR)

	return avgDolVol, avgADR, nil
}

// Helper functions
func getStockSymbols() ([]string, error) {
	file, err := os.Open("pkg/ep/config.csv")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	var symbols []string
	for _, row := range rows {
		if len(row) > 0 && row[0] != "Symbol" && row[0] != "" {
			symbols = append(symbols, strings.TrimSpace(row[0]))
		}
	}

	fmt.Printf("📋 Loaded %d symbols from config.csv\n", len(symbols))
	return symbols, nil
}

// Output function
func outputToJSON(stocks []FilteredStock) error {
	jsonData, err := json.MarshalIndent(stocks, "", "  ")
	if err != nil {
		return err
	}

	stockDir := "data/stockdata"
	if err := os.MkdirAll(stockDir, 0755); err != nil {
		return fmt.Errorf("error creating directories: %v", err)
	}

	filename := filepath.Join(stockDir, "filtered_stocks_marketstack.json")
	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		return err
	}

	fmt.Printf("📁 Results written to %s\n", filename)
	return nil
}