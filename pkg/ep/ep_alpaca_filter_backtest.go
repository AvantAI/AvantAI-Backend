package ep

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// BacktestConfig holds the configuration for backtesting
type BacktestConfig struct {
	TargetDate    string // Format: "2023-01-15" - the date to backtest
	AlpacaKey     string // Alpaca API key
	AlpacaSecret  string // Alpaca secret key
	FinnhubKey    string // Finnhub API key for company data
	FinnhubSecret string // Finnhub secret key
	TiingoKey     string // Optional - for premarket data if needed
	LookbackDays  int    // Days of historical data to fetch (default: 300)
}

// CompanyInfo holds basic company information
type CompanyInfo struct {
	Name     string
	Sector   string
	Industry string
}

// StockData represents basic stock data
type StockData struct {
	Symbol                     string
	Timestamp                  string
	Open                       float64
	High                       float64
	Low                        float64
	Close                      float64
	Volume                     int64
	PreviousClose              float64
	Change                     float64
	ChangePercent              float64
	ExtendedHoursQuote         float64
	ExtendedHoursChange        float64
	ExtendedHoursChangePercent float64
	Exchange                   string
	Name                       string
}

// FilteredStock represents a filtered stock with all analysis
type FilteredStock struct {
	Symbol    string     `json:"symbol"`
	StockInfo StockStats `json:"stock_info"`
}

// StockStats contains all statistics for a stock
type StockStats struct {
	Timestamp             string  `json:"timestamp"`
	MarketCap             float64 `json:"market_cap"`
	GapUp                 float64 `json:"gap_up"`
	Name                  string  `json:"name"`
	Exchange              string  `json:"exchange"`
	Sector                string  `json:"sector"`
	Industry              string  `json:"industry"`
	DolVol                float64 `json:"dollar_volume"`
	ADR                   float64 `json:"adr"`
	PremarketVolume       float64 `json:"premarket_volume"`
	AvgPremarketVolume    float64 `json:"avg_premarket_volume"`
	PremarketVolumeRatio  float64 `json:"premarket_volume_ratio"`
	PremarketVolAsPercent float64 `json:"premarket_vol_as_percent"`
	SMA200                float64 `json:"sma_200"`
	EMA200                float64 `json:"ema_200"`
	EMA50                 float64 `json:"ema_50"`
	EMA20                 float64 `json:"ema_20"`
	EMA10                 float64 `json:"ema_10"`
	IsAbove200EMA         bool    `json:"is_above_200_ema"`
	DistanceFrom50EMA     float64 `json:"distance_from_50_ema"`
	IsExtended            bool    `json:"is_extended"`
	IsTooExtended         bool    `json:"is_too_extended"`
	IsNearEMA1020         bool    `json:"is_near_ema_10_20"`
	BreaksResistance      bool    `json:"breaks_resistance"`
}

// TechnicalIndicators holds technical analysis indicators
type TechnicalIndicators struct {
	SMA200            float64
	EMA200            float64
	EMA50             float64
	EMA20             float64
	EMA10             float64
	IsAbove200EMA     bool
	DistanceFrom50EMA float64
	IsExtended        bool
	IsTooExtended     bool
	IsNearEMA1020     bool
	BreaksResistance  bool
}

// PremarketAnalysis holds premarket volume analysis
type PremarketAnalysis struct {
	CurrentPremarketVol float64
	AvgPremarketVol     float64
	VolumeRatio         float64
	VolAsPercentOfDaily float64
}

// FinnhubCompanyProfile represents company profile from Finnhub
type FinnhubCompanyProfile struct {
	Country         string  `json:"country"`
	Currency        string  `json:"currency"`
	Exchange        string  `json:"exchange"`
	Name            string  `json:"name"`
	Ticker          string  `json:"ticker"`
	IPO             string  `json:"ipo"`
	MarketCap       float64 `json:"marketCapitalization"`
	SharesOut       float64 `json:"shareOutstanding"`
	Logo            string  `json:"logo"`
	Phone           string  `json:"phone"`
	WebURL          string  `json:"weburl"`
	FinnhubIndustry string  `json:"finnhubIndustry"`
}

// FinnhubQuote represents a quote from Finnhub
type FinnhubQuote struct {
	CurrentPrice  float64 `json:"c"`
	Change        float64 `json:"d"`
	PercentChange float64 `json:"dp"`
	HighPrice     float64 `json:"h"`
	LowPrice      float64 `json:"l"`
	OpenPrice     float64 `json:"o"`
	PrevClose     float64 `json:"pc"`
	Timestamp     int64   `json:"t"`
}

// Constants for filtering criteria
const (
	MIN_GAP_UP_PERCENT      = 8.0
	MIN_DOLLAR_VOLUME       = 10000000 // $10M
	MIN_MARKET_CAP          = 50000000 // $50M
	MIN_PREMARKET_VOL_RATIO = 2.0
	MAX_EXTENSION_ADR       = 4.0
	TOO_EXTENDED_ADR        = 8.0
	NEAR_EMA_ADR_THRESHOLD  = 1.5
	MIN_ADR_PERCENT         = 5.0
	MAX_CONCURRENT          = 200
	API_CALLS_PER_SECOND    = 200
)

// AlpacaBar represents a single bar from Alpaca API
// type AlpacaBar struct {
// 	Timestamp string  `json:"t"`
// 	Open      float64 `json:"o"`
// 	High      float64 `json:"h"`
// 	Low       float64 `json:"l"`
// 	Close     float64 `json:"c"`
// 	Volume    float64 `json:"v"`
// 	VWAP      float64 `json:"vw"`
// }

// AlpacaBarsResponse represents the response from Alpaca bars endpoint
type AlpacaBarsResponse struct {
	Bars      []AlpacaBar `json:"bars"`
	Symbol    string      `json:"symbol"`
	NextToken string      `json:"next_page_token"`
}

// AlpacaBarsResponseMap is an alternative response format (legacy)
type AlpacaBarsResponseMap struct {
	Bars map[string][]AlpacaBar `json:"bars"`
}

// AlpacaSnapshot represents a snapshot from Alpaca API
type AlpacaSnapshot struct {
	Symbol      string `json:"symbol"`
	LatestTrade struct {
		Price float64 `json:"p"`
	} `json:"latestTrade"`
	LatestQuote struct {
		BidPrice float64 `json:"bp"`
		AskPrice float64 `json:"ap"`
	} `json:"latestQuote"`
	DailyBar     AlpacaBar `json:"dailyBar"`
	PrevDailyBar AlpacaBar `json:"prevDailyBar"`
}

// AlpacaSnapshotResponse represents the response from Alpaca snapshots endpoint
type AlpacaSnapshotResponse struct {
	Snapshots map[string]AlpacaSnapshot `json:"snapshots"`
}

// BacktestResult extends FilteredStock with additional backtest metrics
type BacktestResult struct {
	FilteredStock
	BacktestDate    string   `json:"backtest_date"`
	DataQuality     string   `json:"data_quality"`
	HistoricalDays  int      `json:"historical_days_available"`
	ValidationNotes []string `json:"validation_notes"`
}

// AlpacaBarData is a wrapper for bar data with consistent structure
type AlpacaBarData struct {
	Symbol    string
	Timestamp string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}

// Main backtesting function
func FilterStocksEpisodicPivotBacktest(config BacktestConfig) error {
	fmt.Println("================================================================================")
	fmt.Printf("=== EPISODIC PIVOT BACKTESTING for %s ===\n", config.TargetDate)
	fmt.Println("================================================================================")

	// Validate target date
	fmt.Println("\n[INIT] Validating target date...")
	targetDate, err := time.Parse("2006-01-02", config.TargetDate)
	if err != nil {
		return fmt.Errorf("invalid target date format. Use YYYY-MM-DD: %v", err)
	}
	fmt.Printf("✅ [INIT] Target date parsed: %s\n", targetDate.Format("2006-01-02"))

	// Ensure we're not trying to backtest future dates
	if targetDate.After(time.Now()) {
		return fmt.Errorf("target date %s is in the future", config.TargetDate)
	}
	fmt.Printf("✅ [INIT] Target date is in the past (valid for backtesting)\n")

	// Set default lookback if not specified
	if config.LookbackDays == 0 {
		config.LookbackDays = 300
		fmt.Printf("ℹ️  [INIT] Using default lookback period: %d days\n", config.LookbackDays)
	}

	fmt.Printf("\n📋 [CONFIG] Configuration Summary:\n")
	fmt.Printf("   Target Date: %s\n", config.TargetDate)
	fmt.Printf("   Lookback Period: %d days\n", config.LookbackDays)
	fmt.Printf("   Alpaca Key: %s...\n", config.AlpacaKey[:min(10, len(config.AlpacaKey))])
	fmt.Printf("   Finnhub Key: %s...\n", config.FinnhubKey[:min(10, len(config.FinnhubKey))])

	// Stage 1: Filter by gap up on target date
	fmt.Println("\n================================================================================")
	fmt.Println("🔍 STAGE 1: Gap Up Filter")
	fmt.Println("================================================================================")
	startTime := time.Now()
	gapUpStocks, err := backtestStage1GapUp(config)
	if err != nil {
		return fmt.Errorf("error in Stage 1 backtest: %v", err)
	}
	fmt.Printf("\n⏱️  [STAGE 1] Completed in %v\n", time.Since(startTime))
	fmt.Printf("✅ [STAGE 1] Found %d stocks with %.0f%% gap up on %s\n",
		len(gapUpStocks), MIN_GAP_UP_PERCENT, config.TargetDate)

	if len(gapUpStocks) == 0 {
		fmt.Println("⚠️  [STAGE 1] No stocks found with gap up criteria for target date")
		return nil
	}

	// Stage 2: Market cap and liquidity filter (using target date data)
	fmt.Println("\n================================================================================")
	fmt.Println("💰 STAGE 2: Liquidity Filter")
	fmt.Println("================================================================================")
	startTime = time.Now()
	liquidStocks, err := backtestStage2Liquidity(config, gapUpStocks)
	if err != nil {
		return fmt.Errorf("error in Stage 2 backtest: %v", err)
	}
	fmt.Printf("\n⏱️  [STAGE 2] Completed in %v\n", time.Since(startTime))
	fmt.Printf("✅ [STAGE 2] Found %d stocks meeting liquidity requirements\n", len(liquidStocks))

	// Stage 3: Technical analysis using historical data up to target date
	fmt.Println("\n================================================================================")
	fmt.Println("📊 STAGE 3: Technical Analysis")
	fmt.Println("================================================================================")
	startTime = time.Now()
	technicalStocks, err := backtestStage3Technical(config, liquidStocks)
	if err != nil {
		return fmt.Errorf("error in Stage 3 backtest: %v", err)
	}
	fmt.Printf("\n⏱️  [STAGE 3] Completed in %v\n", time.Since(startTime))
	fmt.Printf("✅ [STAGE 3] Found %d stocks meeting technical criteria\n", len(technicalStocks))

	// Stage 4: Final episodic pivot filter
	fmt.Println("\n================================================================================")
	fmt.Println("🎯 STAGE 4: Final Episodic Pivot Criteria")
	fmt.Println("================================================================================")
	startTime = time.Now()
	finalStocks := backtestStage4Final(technicalStocks)
	fmt.Printf("\n⏱️  [STAGE 4] Completed in %v\n", time.Since(startTime))

	// Output results
	fmt.Println("\n================================================================================")
	fmt.Println("💾 Saving Results")
	fmt.Println("================================================================================")
	err = outputBacktestResults(config, finalStocks)
	if err != nil {
		return fmt.Errorf("error writing backtest results: %v", err)
	}

	fmt.Println("\n================================================================================")
	fmt.Printf("🎉 BACKTEST COMPLETE!\n")
	fmt.Printf("📊 Found %d qualifying stocks for %s\n", len(finalStocks), config.TargetDate)
	fmt.Printf("📁 Results written to backtest_%s_results.json\n",
		strings.ReplaceAll(config.TargetDate, "-", ""))
	fmt.Println("================================================================================")

	return nil
}

// Stage 1: Backtest gap up filter
func backtestStage1GapUp(config BacktestConfig) ([]StockData, error) {
	fmt.Printf("\n[STAGE 1] Fetching tradable symbols from Alpaca...\n")
	symbols, err := getAlpacaTradableSymbols(config)
	if err != nil {
		return nil, fmt.Errorf("failed to get stock symbols: %v", err)
	}
	fmt.Printf("✅ [STAGE 1] Retrieved %d tradable symbols\n", len(symbols))

	fmt.Printf("\n[STAGE 1] Starting gap up analysis for %d symbols...\n", len(symbols))
	fmt.Printf("[STAGE 1] Criteria: Gap Up >= %.0f%%\n", MIN_GAP_UP_PERCENT)
	fmt.Printf("[STAGE 1] Concurrency: %d workers\n", MAX_CONCURRENT)
	fmt.Printf("[STAGE 1] Rate limit: %d calls/second\n", API_CALLS_PER_SECOND)

	var gapUpStocks []StockData
	var mu sync.Mutex
	processedCount := 0
	qualifiedCount := 0

	semaphore := make(chan struct{}, MAX_CONCURRENT)
	var wg sync.WaitGroup
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	for _, symbol := range symbols {
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			<-rateLimiter

			mu.Lock()
			processedCount++
			currentCount := processedCount
			currentQualified := qualifiedCount
			mu.Unlock()

			if currentCount%25 == 0 {
				fmt.Printf("   [STAGE 1] Progress: %d/%d symbols processed (%d qualified so far)...\n",
					currentCount, len(symbols), currentQualified)
			}

			// Get historical data around target date using Alpaca
			fmt.Printf("      [DEBUG] Fetching historical data for %s...\n", sym)
			currentData, previousData, err := getHistoricalDataForDateAlpaca(
				config.AlpacaKey, config.AlpacaSecret, sym, config.TargetDate)
			if err != nil {
				fmt.Printf("      ⚠️  [DEBUG] %s: Failed to get historical data: %v\n", sym, err)
				return
			}

			if currentData == nil || previousData == nil {
				fmt.Printf("      ⚠️  [DEBUG] %s: Missing data (current=%v, previous=%v)\n",
					sym, currentData != nil, previousData != nil)
				return
			}

			if previousData.Close <= 0 {
				fmt.Printf("      ⚠️  [DEBUG] %s: Invalid previous close: %.2f\n", sym, previousData.Close)
				return
			}

			gapUp := ((currentData.Open - previousData.Close) / previousData.Close) * 100

			fmt.Printf("      [DEBUG] %s Analysis:\n", sym)
			fmt.Printf("         Current Date: %s\n", currentData.Timestamp[:10])
			fmt.Printf("         Previous Date: %s\n", previousData.Timestamp[:10])
			fmt.Printf("         Current Open: $%.2f\n", currentData.Open)
			fmt.Printf("         Previous Close: $%.2f\n", previousData.Close)
			fmt.Printf("         Gap Up: %.2f%%\n", gapUp)

			// Filter for 8%+ gap up on target date
			if gapUp >= MIN_GAP_UP_PERCENT {
				stockData := StockData{
					Symbol:                     sym,
					Timestamp:                  currentData.Timestamp,
					Open:                       currentData.Open,
					High:                       currentData.High,
					Low:                        currentData.Low,
					Close:                      currentData.Close,
					Volume:                     int64(currentData.Volume),
					PreviousClose:              previousData.Close,
					Change:                     currentData.Close - previousData.Close,
					ChangePercent:              gapUp,
					ExtendedHoursQuote:         currentData.Close,
					ExtendedHoursChange:        currentData.Close - previousData.Close,
					ExtendedHoursChangePercent: gapUp,
					Exchange:                   "US",
					Name:                       sym,
				}

				mu.Lock()
				qualifiedCount++
				fmt.Printf("      ✅ [QUALIFIED] %s - Gap: %.2f%% (Total qualified: %d)\n",
					sym, gapUp, qualifiedCount)
				gapUpStocks = append(gapUpStocks, stockData)
				mu.Unlock()
			} else {
				fmt.Printf("      ❌ [REJECTED] %s - Gap: %.2f%% < %.0f%%\n",
					sym, gapUp, MIN_GAP_UP_PERCENT)
			}
		}(symbol)
	}

	wg.Wait()
	fmt.Printf("\n[STAGE 1] Processing complete: %d/%d symbols qualified\n",
		len(gapUpStocks), len(symbols))
	return gapUpStocks, nil
}

// Stage 2: Backtest liquidity filter
func backtestStage2Liquidity(config BacktestConfig, stocks []StockData) ([]BacktestResult, error) {
	var liquidStocks []BacktestResult
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	fmt.Printf("\n[STAGE 2] Analyzing liquidity for %d stocks...\n", len(stocks))
	fmt.Printf("[STAGE 2] Criteria: Market Cap >= $%.0f\n", float64(MIN_MARKET_CAP))

	for i, stock := range stocks {
		fmt.Printf("\n🏢 [STAGE 2] [%d/%d] Processing %s...\n", i+1, len(stocks), stock.Symbol)
		<-rateLimiter

		// Get company info and market cap from Finnhub
		fmt.Printf("   [DEBUG] Fetching Finnhub profile for %s...\n", stock.Symbol)
		companyProfile, err := getCompanyProfileFinnhub(config.FinnhubKey, stock.Symbol)
		if err != nil {
			fmt.Printf("   ⚠️  [DEBUG] Finnhub profile error for %s: %v\n", stock.Symbol, err)
		} else {
			fmt.Printf("   ✅ [DEBUG] Finnhub profile retrieved for %s\n", stock.Symbol)
		}

		// Get real-time market cap from Finnhub
		var marketCap float64
		var name, sector, industry string = stock.Name, "Unknown", "Unknown"

		if companyProfile != nil {
			// Finnhub returns market cap in millions, so multiply by 1 million
			marketCap = companyProfile.MarketCap * 1000000
			fmt.Printf("   [DEBUG] %s Market Cap from Finnhub: $%.0f M (raw: %.2f)\n",
				stock.Symbol, marketCap/1000000, companyProfile.MarketCap)

			if companyProfile.Name != "" {
				name = companyProfile.Name
				fmt.Printf("   [DEBUG] %s Company Name: %s\n", stock.Symbol, name)
			}
			if companyProfile.FinnhubIndustry != "" {
				industry = companyProfile.FinnhubIndustry
				sector = parseSectorFromIndustry(companyProfile.FinnhubIndustry)
				fmt.Printf("   [DEBUG] %s Industry: %s, Sector: %s\n", stock.Symbol, industry, sector)
			}
		}

		// Fallback: estimate market cap if Finnhub doesn't have it
		if marketCap == 0 {
			fmt.Printf("   ⚠️  [DEBUG] No market cap from Finnhub for %s, estimating...\n", stock.Symbol)
			companyInfo := &CompanyInfo{
				Name:     name,
				Sector:   sector,
				Industry: industry,
			}
			marketCap = estimateMarketCapImproved(stock, companyInfo)
			fmt.Printf("   [DEBUG] %s Estimated Market Cap: $%.0f\n", stock.Symbol, marketCap)
		}

		// Apply minimum liquidity criteria
		marketCapM := marketCap / 1000000
		minCapM := float64(MIN_MARKET_CAP) / 1000000
		passed := marketCap >= MIN_MARKET_CAP

		if passed {
			backtestResult := BacktestResult{
				FilteredStock: FilteredStock{
					Symbol: stock.Symbol,
					StockInfo: StockStats{
						Timestamp: stock.Timestamp,
						MarketCap: marketCap,
						GapUp:     stock.ExtendedHoursChangePercent,
						Name:      name,
						Exchange:  stock.Exchange,
						Sector:    sector,
						Industry:  industry,
					},
				},
				BacktestDate:    config.TargetDate,
				DataQuality:     "Good",
				ValidationNotes: []string{},
			}

			liquidStocks = append(liquidStocks, backtestResult)
			fmt.Printf("   ✅ [QUALIFIED] %s - Market Cap: $%.0fM >= $%.0fM\n",
				stock.Symbol, marketCapM, minCapM)
		} else {
			fmt.Printf("   ❌ [REJECTED] %s - Market Cap: $%.0fM < $%.0fM\n",
				stock.Symbol, marketCapM, minCapM)
		}
	}

	fmt.Printf("\n[STAGE 2] Liquidity filter complete: %d/%d stocks qualified\n",
		len(liquidStocks), len(stocks))
	return liquidStocks, nil
}

// Stage 3: Backtest technical analysis
func backtestStage3Technical(config BacktestConfig, stocks []BacktestResult) ([]BacktestResult, error) {
	var technicalStocks []BacktestResult
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	fmt.Printf("\n[STAGE 3] Analyzing technical indicators for %d stocks...\n", len(stocks))

	for i, stock := range stocks {
		fmt.Printf("\n📈 [STAGE 3] [%d/%d] Analyzing %s...\n", i+1, len(stocks), stock.Symbol)
		<-rateLimiter

		// Get historical data up to target date using Alpaca
		fmt.Printf("   [DEBUG] Fetching %d days of historical data for %s...\n",
			config.LookbackDays, stock.Symbol)
		historicalData, err := getHistoricalDataUpToDateAlpaca(
			config.AlpacaKey, config.AlpacaSecret, stock.Symbol, config.TargetDate, config.LookbackDays)
		if err != nil {
			fmt.Printf("   ⚠️  [DEBUG] Historical data error for %s: %v\n", stock.Symbol, err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Historical data error: %v", err))
			continue
		}

		fmt.Printf("   [DEBUG] %s: Retrieved %d days of historical data\n",
			stock.Symbol, len(historicalData))

		if len(historicalData) < 200 {
			fmt.Printf("   ⚠️  [REJECTED] %s: Insufficient data - only %d days (need 200+)\n",
				stock.Symbol, len(historicalData))
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Insufficient data: %d days", len(historicalData)))
			stock.DataQuality = "Poor"
			continue
		}

		stock.HistoricalDays = len(historicalData)

		// Calculate technical indicators using data up to target date
		fmt.Printf("   [DEBUG] Calculating technical indicators for %s...\n", stock.Symbol)
		technicalIndicators, err := calculateTechnicalIndicatorsBacktest(
			historicalData, stock.Symbol, config.TargetDate)
		if err != nil {
			fmt.Printf("   ⚠️  [DEBUG] Technical indicators error for %s: %v\n",
				stock.Symbol, err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Technical analysis error: %v", err))
			continue
		}

		// Calculate dollar volume and ADR using 21 days before target date
		fmt.Printf("   [DEBUG] Calculating dollar volume and ADR for %s...\n", stock.Symbol)
		dolVol, adr, err := calculateHistoricalMetricsBacktest(
			historicalData, stock.Symbol, config.TargetDate)
		if err != nil {
			fmt.Printf("   ⚠️  [DEBUG] Metrics calculation error for %s: %v\n",
				stock.Symbol, err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Metrics calculation error: %v", err))
			continue
		}

		// Simulate premarket analysis for backtest
		fmt.Printf("   [DEBUG] Simulating premarket analysis for %s...\n", stock.Symbol)
		premarketAnalysis := simulatePremarketAnalysisBacktest(historicalData, dolVol)

		// Update stock with all metrics
		stock.StockInfo.DolVol = dolVol
		stock.StockInfo.ADR = adr
		stock.StockInfo.PremarketVolume = premarketAnalysis.CurrentPremarketVol
		stock.StockInfo.AvgPremarketVolume = premarketAnalysis.AvgPremarketVol
		stock.StockInfo.PremarketVolumeRatio = premarketAnalysis.VolumeRatio
		stock.StockInfo.PremarketVolAsPercent = premarketAnalysis.VolAsPercentOfDaily
		stock.StockInfo.SMA200 = technicalIndicators.SMA200
		stock.StockInfo.EMA200 = technicalIndicators.EMA200
		stock.StockInfo.EMA50 = technicalIndicators.EMA50
		stock.StockInfo.EMA20 = technicalIndicators.EMA20
		stock.StockInfo.EMA10 = technicalIndicators.EMA10
		stock.StockInfo.IsAbove200EMA = technicalIndicators.IsAbove200EMA
		stock.StockInfo.DistanceFrom50EMA = technicalIndicators.DistanceFrom50EMA
		stock.StockInfo.IsExtended = technicalIndicators.IsExtended
		stock.StockInfo.IsTooExtended = technicalIndicators.IsTooExtended
		stock.StockInfo.IsNearEMA1020 = technicalIndicators.IsNearEMA1020
		stock.StockInfo.BreaksResistance = technicalIndicators.BreaksResistance

		// Enhanced logging
		currentPrice := getCurrentPriceBacktest(historicalData)
		fmt.Printf("   📊 [METRICS] %s Analysis for %s:\n", stock.Symbol, config.TargetDate)
		fmt.Printf("      💰 Dollar Volume: $%.0f (Min: $%.0f) %s\n",
			dolVol, float64(MIN_DOLLAR_VOLUME), checkmark(dolVol >= MIN_DOLLAR_VOLUME))
		fmt.Printf("      📏 ADR: %.2f%% (Min: %.2f%%) %s\n",
			adr, MIN_ADR_PERCENT, checkmark(adr >= MIN_ADR_PERCENT))
		fmt.Printf("      📈 Current Price: $%.2f\n", currentPrice)
		fmt.Printf("      📊 SMA 200: $%.2f\n", technicalIndicators.SMA200)
		fmt.Printf("      📊 EMA 200: $%.2f\n", technicalIndicators.EMA200)
		fmt.Printf("      📊 EMA 50: $%.2f\n", technicalIndicators.EMA50)
		fmt.Printf("      📊 EMA 20: $%.2f\n", technicalIndicators.EMA20)
		fmt.Printf("      📊 EMA 10: $%.2f\n", technicalIndicators.EMA10)
		fmt.Printf("      ✓  Above 200 EMA: %s\n", checkmark(technicalIndicators.IsAbove200EMA))
		fmt.Printf("      📏 Distance from 50 EMA: %.2f ADRs\n", technicalIndicators.DistanceFrom50EMA)
		fmt.Printf("      ⚠️  Extended: %s\n", checkmark(technicalIndicators.IsExtended))
		fmt.Printf("      ⚠️  Too Extended: %s\n", checkmark(technicalIndicators.IsTooExtended))
		fmt.Printf("      📊 Historical Days: %d\n", len(historicalData))

		technicalStocks = append(technicalStocks, stock)
		fmt.Printf("   ✅ [STAGE 3] %s passed technical analysis\n", stock.Symbol)
	}

	fmt.Printf("\n[STAGE 3] Technical analysis complete: %d stocks qualified\n",
		len(technicalStocks))
	return technicalStocks, nil
}

// Stage 4: Final backtest filter
func backtestStage4Final(stocks []BacktestResult) []BacktestResult {
	var finalStocks []BacktestResult

	fmt.Printf("\n[STAGE 4] Applying final episodic pivot criteria to %d stocks...\n", len(stocks))

	for i, stock := range stocks {
		fmt.Printf("\n🔍 [STAGE 4] [%d/%d] Final analysis for %s:\n", i+1, len(stocks), stock.Symbol)

		criteria := []struct {
			name    string
			passed  bool
			details string
		}{
			{"Dollar Volume", stock.StockInfo.DolVol >= MIN_DOLLAR_VOLUME,
				fmt.Sprintf("$%.0f >= $%v", stock.StockInfo.DolVol, MIN_DOLLAR_VOLUME)},
			{"Premarket Volume Ratio", stock.StockInfo.PremarketVolumeRatio >= MIN_PREMARKET_VOL_RATIO,
				fmt.Sprintf("%.1fx >= %.1fx (simulated)", stock.StockInfo.PremarketVolumeRatio, MIN_PREMARKET_VOL_RATIO)},
			{"Above 200 EMA", stock.StockInfo.IsAbove200EMA,
				fmt.Sprintf("Price above 200 EMA")},
			{"Not Extended", !stock.StockInfo.IsExtended,
				fmt.Sprintf("Distance from 50 EMA: %.2f ADRs (Max: %.1f)",
					stock.StockInfo.DistanceFrom50EMA, MAX_EXTENSION_ADR)},
		}

		allPassed := true
		for _, criterion := range criteria {
			status := "❌"
			if criterion.passed {
				status = "✅"
			} else {
				allPassed = false
			}
			fmt.Printf("   %s %s: %s\n", status, criterion.name, criterion.details)
		}

		// Check too extended
		if stock.StockInfo.IsTooExtended {
			fmt.Printf("   ❌ Too Extended: Distance from 50 EMA: %.2f ADRs > %.1f\n",
				stock.StockInfo.DistanceFrom50EMA, TOO_EXTENDED_ADR)
			allPassed = false
		}

		if allPassed && !stock.StockInfo.IsTooExtended {
			finalStocks = append(finalStocks, stock)
			fmt.Printf("   🎉 [QUALIFIED] %s PASSED all criteria!\n", stock.Symbol)
		} else {
			reason := "Failed criteria"
			if stock.StockInfo.IsTooExtended {
				reason = fmt.Sprintf("Too extended (%.2f > %.1f ADRs from 50 EMA)",
					stock.StockInfo.DistanceFrom50EMA, TOO_EXTENDED_ADR)
			}
			fmt.Printf("   ❌ [REJECTED] %s: %s\n", stock.Symbol, reason)
		}
	}

	fmt.Printf("\n[STAGE 4] Final filter complete: %d/%d stocks qualified\n",
		len(finalStocks), len(stocks))
	return finalStocks
}

// Get historical data for specific date and previous trading day using Alpaca
func getHistoricalDataForDateAlpaca(apiKey, apiSecret, symbol, targetDate string) (*AlpacaBarData, *AlpacaBarData, error) {
	fmt.Printf("         [API] Fetching bars for %s around %s...\n", symbol, targetDate)

	target, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return nil, nil, err
	}

	// Get a range of days to ensure we capture the target date and previous trading day
	startDate := target.AddDate(0, 0, -7).Format("2006-01-02")
	endDate := target.AddDate(0, 0, 1).Format("2006-01-02")

	url := fmt.Sprintf("https://data.alpaca.markets/v2/stocks/%s/bars?start=%s&end=%s&timeframe=1Day&adjustment=split&feed=sip&limit=10000",
		symbol, startDate, endDate)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Add("APCA-API-KEY-ID", apiKey)
	req.Header.Add("APCA-API-SECRET-KEY", apiSecret)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	fmt.Printf("         [API] Response status: %d\n", resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %v", err)
	}

	fmt.Printf("         [API] Response body preview: %s\n", string(body[:min(200, len(body))]))

	var barsResponse AlpacaBarsResponse
	if err := json.Unmarshal(body, &barsResponse); err != nil {
		return nil, nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	bars := barsResponse.Bars
	if len(bars) < 2 {
		return nil, nil, fmt.Errorf("insufficient data: only %d bars", len(bars))
	}

	fmt.Printf("         [API] Retrieved %d bars for %s\n", len(bars), symbol)

	// Sort bars by timestamp (most recent first)
	sort.Slice(bars, func(i, j int) bool {
		return bars[i].Timestamp > bars[j].Timestamp
	})

	// Find the target date and previous trading day
	var currentData, previousData *AlpacaBarData

	for i, bar := range bars {
		barDate := bar.Timestamp[:10] // Extract YYYY-MM-DD
		if barDate == targetDate {
			currentData = &AlpacaBarData{
				Symbol:    symbol,
				Timestamp: bar.Timestamp,
				Open:      bar.Open,
				High:      bar.High,
				Low:       bar.Low,
				Close:     bar.Close,
				Volume:    bar.Volume,
			}
			fmt.Printf("         [API] Found target date bar: %s\n", barDate)
			// Previous trading day is next in sorted array
			if i+1 < len(bars) {
				prevBar := bars[i+1]
				previousData = &AlpacaBarData{
					Symbol:    symbol,
					Timestamp: prevBar.Timestamp,
					Open:      prevBar.Open,
					High:      prevBar.High,
					Low:       prevBar.Low,
					Close:     prevBar.Close,
					Volume:    prevBar.Volume,
				}
				fmt.Printf("         [API] Found previous trading day bar: %s\n", prevBar.Timestamp[:10])
			}
			break
		}
	}

	if currentData == nil {
		return nil, nil, fmt.Errorf("no data found for target date %s", targetDate)
	}

	if previousData == nil {
		return nil, nil, fmt.Errorf("no previous trading day data found for %s", targetDate)
	}

	return currentData, previousData, nil
}

// Get historical data up to a specific date using Alpaca
func getHistoricalDataUpToDateAlpaca(apiKey, apiSecret, symbol, targetDate string, lookbackDays int) ([]AlpacaBarData, error) {
	fmt.Printf("      [API] Fetching historical data for %s (lookback: %d days)...\n", symbol, lookbackDays)

	target, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return nil, err
	}

	// Calculate start date with buffer for weekends/holidays
	startDate := target.AddDate(0, 0, -(lookbackDays + 100)).Format("2006-01-02")

	url := fmt.Sprintf("https://data.alpaca.markets/v2/stocks/%s/bars?start=%s&end=%s&timeframe=1Day&adjustment=split&feed=sip&limit=10000",
		symbol, startDate, targetDate)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("APCA-API-KEY-ID", apiKey)
	req.Header.Add("APCA-API-SECRET-KEY", apiSecret)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	fmt.Printf("      [API] Response status: %d\n", resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	fmt.Printf("      [API] Response body preview: %s\n", string(body[:min(200, len(body))]))

	var barsResponse AlpacaBarsResponse
	if err := json.Unmarshal(body, &barsResponse); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	bars := barsResponse.Bars
	if len(bars) == 0 {
		return nil, fmt.Errorf("no bars data for symbol %s", symbol)
	}

	fmt.Printf("      [API] Retrieved %d bars\n", len(bars))

	// Convert to consistent format and filter by target date
	var filteredData []AlpacaBarData
	for _, bar := range bars {
		barTime, err := time.Parse(time.RFC3339, bar.Timestamp)
		if err != nil {
			continue
		}

		if barTime.Before(target.AddDate(0, 0, 1)) { // Include target date
			filteredData = append(filteredData, AlpacaBarData{
				Symbol:    symbol,
				Timestamp: bar.Timestamp,
				Open:      bar.Open,
				High:      bar.High,
				Low:       bar.Low,
				Close:     bar.Close,
				Volume:    bar.Volume,
			})
		}
	}

	fmt.Printf("      [API] Filtered to %d bars up to %s\n", len(filteredData), targetDate)

	return filteredData, nil
}

// Get company profile from Finnhub
func getCompanyProfileFinnhub(apiKey, symbol string) (*FinnhubCompanyProfile, error) {
	url := fmt.Sprintf("https://finnhub.io/api/v1/stock/profile2?symbol=%s&token=%s", symbol, apiKey)

	fmt.Printf("      [API] Calling Finnhub for %s...\n", symbol)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	fmt.Printf("      [API] Finnhub response status: %d\n", resp.StatusCode)

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	var profile FinnhubCompanyProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	// Check if we got valid data
	if profile.Ticker == "" {
		return nil, fmt.Errorf("no profile data found for symbol")
	}

	return &profile, nil
}

// Parse sector from Finnhub industry string
func parseSectorFromIndustry(industry string) string {
	industryLower := strings.ToLower(industry)

	sectorMap := map[string]string{
		"technology":        "Technology",
		"software":          "Technology",
		"hardware":          "Technology",
		"semiconductor":     "Technology",
		"internet":          "Technology",
		"computer":          "Technology",
		"electronic":        "Technology",
		"health":            "Healthcare",
		"healthcare":        "Healthcare",
		"pharmaceutical":    "Healthcare",
		"biotech":           "Healthcare",
		"medical":           "Healthcare",
		"finance":           "Financial Services",
		"financial":         "Financial Services",
		"bank":              "Financial Services",
		"insurance":         "Financial Services",
		"investment":        "Financial Services",
		"energy":            "Energy",
		"oil":               "Energy",
		"gas":               "Energy",
		"utilities":         "Utilities",
		"real estate":       "Real Estate",
		"consumer":          "Consumer Cyclical",
		"retail":            "Consumer Cyclical",
		"automotive":        "Consumer Cyclical",
		"industrial":        "Industrials",
		"manufacturing":     "Industrials",
		"aerospace":         "Industrials",
		"defense":           "Industrials",
		"materials":         "Basic Materials",
		"chemical":          "Basic Materials",
		"mining":            "Basic Materials",
		"telecommunication": "Communication Services",
		"media":             "Communication Services",
	}

	for key, sector := range sectorMap {
		if strings.Contains(industryLower, key) {
			return sector
		}
	}

	return "Unknown"
}

// Get tradable symbols from Alpaca
func getAlpacaTradableSymbols(config BacktestConfig) ([]string, error) {
	fmt.Printf("[API] Calling Alpaca assets endpoint...\n")
	url := "https://paper-api.alpaca.markets/v2/assets?status=active&asset_class=us_equity"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("APCA-API-KEY-ID", config.AlpacaKey)
	req.Header.Set("APCA-API-SECRET-KEY", config.AlpacaSecret)

	client := &http.Client{Timeout: 30 * time.Second}
	fmt.Printf("[API] Sending request to Alpaca...\n")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	fmt.Printf("[API] Response status: %d\n", resp.StatusCode)

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	fmt.Printf("[API] Response body size: %d bytes\n", len(body))

	var assets []struct {
		ID       string `json:"id"`
		Symbol   string `json:"symbol"`
		Exchange string `json:"exchange"`
		Tradable bool   `json:"tradable"`
		Status   string `json:"status"`
		Class    string `json:"class"`
	}

	if err := json.Unmarshal(body, &assets); err != nil {
		return nil, fmt.Errorf("failed to parse assets: %v", err)
	}

	fmt.Printf("[API] Parsed %d total assets\n", len(assets))

	var symbols []string
	for _, asset := range assets {
		if asset.Tradable && asset.Status == "active" {
			symbols = append(symbols, asset.Symbol)
		}
	}

	if len(symbols) == 0 {
		return nil, fmt.Errorf("no tradable symbols found")
	}

	fmt.Printf("[API] ✅ Found %d tradable symbols\n", len(symbols))
	return symbols, nil
}

// Helper function to estimate market cap
func estimateMarketCapImproved(stock StockData, _ *CompanyInfo) float64 {
	if stock.Close <= 0 || stock.Volume <= 0 {
		return 0
	}

	estimatedShares := float64(stock.Volume) * 100
	estimated := stock.Close * estimatedShares
	fmt.Printf("      [DEBUG] Estimated market cap: $%.0f (shares: %.0f x price: $%.2f)\n",
		estimated, estimatedShares, stock.Close)
	return estimated
}

// Helper function to check if value meets criteria
func checkmark(condition bool) string {
	if condition {
		return "✅"
	}
	return "❌"
}

// Helper function for absolute value
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Calculate technical indicators for backtesting
func calculateTechnicalIndicatorsBacktest(historicalData []AlpacaBarData, symbol, targetDate string) (*TechnicalIndicators, error) {
	fmt.Printf("      [DEBUG] Calculating technical indicators for %s...\n", symbol)

	if len(historicalData) < 200 {
		return nil, fmt.Errorf("insufficient data for technical indicators")
	}

	// Sort data by date (oldest first)
	sort.Slice(historicalData, func(i, j int) bool {
		return historicalData[i].Timestamp < historicalData[j].Timestamp
	})

	// Find target date index
	targetIndex := len(historicalData) - 1
	for i, data := range historicalData {
		if strings.HasPrefix(data.Timestamp, targetDate) {
			targetIndex = i
			fmt.Printf("      [DEBUG] Found target date at index %d/%d\n", i, len(historicalData))
			break
		}
	}

	dataUpToTarget := historicalData[:targetIndex+1]
	if len(dataUpToTarget) < 200 {
		return nil, fmt.Errorf("insufficient data up to target date")
	}

	fmt.Printf("      [DEBUG] Using %d days of data up to %s\n", len(dataUpToTarget), targetDate)

	currentPrice := dataUpToTarget[len(dataUpToTarget)-1].Close
	currentADR := calculateADRForPeriodAlpaca(dataUpToTarget[max(0, len(dataUpToTarget)-21):])

	fmt.Printf("      [DEBUG] Current price: $%.2f, ADR: %.2f%%\n", currentPrice, currentADR)

	// Calculate moving averages
	sma200 := calculateSMAAlpaca(dataUpToTarget, 200)
	ema200 := calculateEMAAlpaca(dataUpToTarget, 200)
	ema50 := calculateEMAAlpaca(dataUpToTarget, 50)
	ema20 := calculateEMAAlpaca(dataUpToTarget, 20)
	ema10 := calculateEMAAlpaca(dataUpToTarget, 10)

	fmt.Printf("      [DEBUG] SMA200: $%.2f, EMA200: $%.2f\n", sma200, ema200)
	fmt.Printf("      [DEBUG] EMA50: $%.2f, EMA20: $%.2f, EMA10: $%.2f\n", ema50, ema20, ema10)

	// Calculate distance from 50 EMA in ADRs
	distanceFrom50EMA := 0.0
	if currentADR > 0 {
		distanceFrom50EMA = (currentPrice - ema50) / (currentADR * currentPrice / 100)
		fmt.Printf("      [DEBUG] Distance from 50 EMA: %.2f ADRs\n", distanceFrom50EMA)
	}

	// Check if near 10/20 EMA
	distanceFrom10EMA := 0.0
	distanceFrom20EMA := 0.0
	if currentADR > 0 {
		adrValue := currentADR * currentPrice / 100
		distanceFrom10EMA = abs(currentPrice-ema10) / adrValue
		distanceFrom20EMA = abs(currentPrice-ema20) / adrValue
		fmt.Printf("      [DEBUG] Distance from 10 EMA: %.2f ADRs, from 20 EMA: %.2f ADRs\n",
			distanceFrom10EMA, distanceFrom20EMA)
	}
	isNearEMA1020 := (distanceFrom10EMA <= NEAR_EMA_ADR_THRESHOLD) || (distanceFrom20EMA <= NEAR_EMA_ADR_THRESHOLD)

	// Check if breaks resistance
	breaksResistance := false
	if len(dataUpToTarget) >= 20 {
		recent20Days := dataUpToTarget[len(dataUpToTarget)-20:]
		recentHigh := 0.0
		for _, day := range recent20Days[:len(recent20Days)-1] {
			if day.High > recentHigh {
				recentHigh = day.High
			}
		}
		currentOpen := dataUpToTarget[len(dataUpToTarget)-1].Open
		breaksResistance = currentOpen > recentHigh
		fmt.Printf("      [DEBUG] Recent 20-day high: $%.2f, current open: $%.2f, breaks resistance: %v\n",
			recentHigh, currentOpen, breaksResistance)
	}

	indicators := &TechnicalIndicators{
		SMA200:            sma200,
		EMA200:            ema200,
		EMA50:             ema50,
		EMA20:             ema20,
		EMA10:             ema10,
		IsAbove200EMA:     currentPrice > ema200,
		DistanceFrom50EMA: distanceFrom50EMA,
		IsExtended:        distanceFrom50EMA > MAX_EXTENSION_ADR,
		IsTooExtended:     distanceFrom50EMA > TOO_EXTENDED_ADR,
		IsNearEMA1020:     isNearEMA1020,
		BreaksResistance:  breaksResistance,
	}

	fmt.Printf("      [DEBUG] ✅ Technical indicators calculated successfully\n")
	return indicators, nil
}

// Helper calculation functions for Alpaca data
func calculateSMAAlpaca(data []AlpacaBarData, period int) float64 {
	if len(data) < period {
		return 0
	}
	sum := 0.0
	for i := len(data) - period; i < len(data); i++ {
		sum += data[i].Close
	}
	return sum / float64(period)
}

func calculateEMAAlpaca(data []AlpacaBarData, period int) float64 {
	if len(data) < period {
		return 0
	}
	multiplier := 2.0 / float64(period+1)
	ema := calculateSMAAlpaca(data[:period], period)
	for i := period; i < len(data); i++ {
		ema = (data[i].Close-ema)*multiplier + ema
	}
	return ema
}

func calculateADRForPeriodAlpaca(data []AlpacaBarData) float64 {
	if len(data) == 0 {
		return 0
	}
	sum := 0.0
	for _, bar := range data {
		if bar.Low > 0 {
			sum += ((bar.High - bar.Low) / bar.Low) * 100
		}
	}
	return sum / float64(len(data))
}

func calculateAvgVolumeAlpaca(data []AlpacaBarData) float64 {
	if len(data) == 0 {
		return 0
	}
	sum := 0.0
	for _, bar := range data {
		sum += bar.Volume
	}
	return sum / float64(len(data))
}

// Calculate historical metrics for backtesting
func calculateHistoricalMetricsBacktest(historicalData []AlpacaBarData, _, targetDate string) (float64, float64, error) {
	fmt.Printf("      [DEBUG] Calculating historical metrics...\n")

	sort.Slice(historicalData, func(i, j int) bool {
		return historicalData[i].Timestamp > historicalData[j].Timestamp
	})

	targetIndex := -1
	for i, data := range historicalData {
		if strings.HasPrefix(data.Timestamp, targetDate) {
			targetIndex = i
			fmt.Printf("      [DEBUG] Target date index: %d\n", i)
			break
		}
	}

	if targetIndex == -1 {
		return 0, 0, fmt.Errorf("target date not found in historical data")
	}

	endIndex := targetIndex + 21
	if endIndex > len(historicalData) {
		endIndex = len(historicalData)
	}

	metricsData := historicalData[targetIndex:endIndex]
	fmt.Printf("      [DEBUG] Using %d days for metrics calculation\n", len(metricsData))

	if len(metricsData) < 10 {
		return 0, 0, fmt.Errorf("insufficient data for metrics: only %d days available", len(metricsData))
	}

	var dolVolSum float64 = 0
	var adrSum float64 = 0
	validDays := 0

	for _, dataItem := range metricsData {
		if dataItem.Close <= 0 || dataItem.Volume <= 0 || dataItem.High <= 0 || dataItem.Low <= 0 {
			continue
		}

		dolVol := dataItem.Volume * dataItem.Close
		dolVolSum += dolVol

		dailyRange := ((dataItem.High - dataItem.Low) / dataItem.Low) * 100
		adrSum += dailyRange

		validDays++
	}

	if validDays == 0 {
		return 0, 0, fmt.Errorf("no valid historical data found")
	}

	avgDolVol := dolVolSum / float64(validDays)
	avgADR := adrSum / float64(validDays)

	fmt.Printf("      [DEBUG] Calculated from %d valid days: DolVol=$%.0f, ADR=%.2f%%\n",
		validDays, avgDolVol, avgADR)

	return avgDolVol, avgADR, nil
}

// Simulate premarket analysis for backtesting
func simulatePremarketAnalysisBacktest(historicalData []AlpacaBarData, _ float64) *PremarketAnalysis {
	fmt.Printf("      [DEBUG] Simulating premarket analysis...\n")

	if len(historicalData) == 0 {
		return &PremarketAnalysis{}
	}

	avgDailyVol := calculateAvgVolumeAlpaca(historicalData[:min(20, len(historicalData))])

	estimatedPremarketVol := avgDailyVol * 0.12
	estimatedCurrentPremarket := estimatedPremarketVol * 6.5

	fmt.Printf("      [DEBUG] Avg daily volume: %.0f, simulated premarket: %.0f (ratio: 6.5x)\n",
		avgDailyVol, estimatedCurrentPremarket)

	return &PremarketAnalysis{
		CurrentPremarketVol: estimatedCurrentPremarket,
		AvgPremarketVol:     estimatedPremarketVol,
		VolumeRatio:         6.5,
		VolAsPercentOfDaily: 25.0,
	}
}

func getCurrentPriceBacktest(historicalData []AlpacaBarData) float64 {
	if len(historicalData) == 0 {
		return 0
	}
	return historicalData[0].Close
}

// Output backtest results
func outputBacktestResults(config BacktestConfig, stocks []BacktestResult) error {
	fmt.Printf("[OUTPUT] Generating results JSON...\n")

	output := map[string]interface{}{
		"backtest_date": config.TargetDate,
		"generated_at":  time.Now().Format(time.RFC3339),
		"backtest_config": map[string]interface{}{
			"target_date":   config.TargetDate,
			"lookback_days": config.LookbackDays,
		},
		"filter_criteria": map[string]interface{}{
			"min_gap_up_percent":         MIN_GAP_UP_PERCENT,
			"min_dollar_volume":          MIN_DOLLAR_VOLUME,
			"min_market_cap":             MIN_MARKET_CAP,
			"min_premarket_volume_ratio": MIN_PREMARKET_VOL_RATIO,
			"max_extension_adr":          MAX_EXTENSION_ADR,
		},
		"qualifying_stocks": stocks,
		"backtest_summary": map[string]interface{}{
			"total_candidates":          len(stocks),
			"avg_gap_up":                calculateAvgGapUpBacktest(stocks),
			"avg_market_cap":            calculateAvgMarketCapBacktest(stocks),
			"data_quality_distribution": calculateDataQualityDistribution(stocks),
		},
	}

	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}

	stockDir := "data/backtests"
	if err := os.MkdirAll(stockDir, 0755); err != nil {
		return fmt.Errorf("error creating directories: %v", err)
	}

	dateStr := strings.ReplaceAll(config.TargetDate, "-", "")
	filename := filepath.Join(stockDir, fmt.Sprintf("backtest_%s_results.json", dateStr))

	fmt.Printf("[OUTPUT] Writing to %s...\n", filename)
	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		return err
	}
	fmt.Printf("✅ [OUTPUT] JSON file written successfully\n")

	fmt.Printf("[OUTPUT] Generating CSV summary...\n")
	err = outputBacktestCSVSummary(stocks, stockDir, dateStr)
	if err != nil {
		fmt.Printf("⚠️  [OUTPUT] Could not create CSV summary: %v\n", err)
	} else {
		fmt.Printf("✅ [OUTPUT] CSV file written successfully\n")
	}

	return nil
}

// Output CSV summary for backtest
func outputBacktestCSVSummary(stocks []BacktestResult, dir, dateStr string) error {
	filename := filepath.Join(dir, fmt.Sprintf("backtest_%s_summary.csv", dateStr))
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	headers := []string{
		"Symbol", "Name", "Sector", "Industry", "Gap Up %", "Market Cap",
		"Dollar Volume", "ADR %", "Above 200 EMA", "Extended",
		"Historical Days", "Data Quality", "Validation Notes",
	}
	writer.Write(headers)

	for _, stock := range stocks {
		validationNotes := strings.Join(stock.ValidationNotes, "; ")
		if validationNotes == "" {
			validationNotes = "None"
		}

		row := []string{
			stock.Symbol,
			stock.StockInfo.Name,
			stock.StockInfo.Sector,
			stock.StockInfo.Industry,
			fmt.Sprintf("%.2f", stock.StockInfo.GapUp),
			fmt.Sprintf("%.0f", stock.StockInfo.MarketCap),
			fmt.Sprintf("%.0f", stock.StockInfo.DolVol),
			fmt.Sprintf("%.2f", stock.StockInfo.ADR),
			boolToString(stock.StockInfo.IsAbove200EMA),
			boolToString(stock.StockInfo.IsExtended),
			fmt.Sprintf("%d", stock.HistoricalDays),
			stock.DataQuality,
			validationNotes,
		}
		writer.Write(row)
	}

	fmt.Printf("[OUTPUT] CSV backtest summary written to %s\n", filename)
	return nil
}

func boolToString(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func calculateAvgGapUpBacktest(stocks []BacktestResult) float64 {
	if len(stocks) == 0 {
		return 0
	}
	sum := 0.0
	for _, stock := range stocks {
		sum += stock.StockInfo.GapUp
	}
	return sum / float64(len(stocks))
}

func calculateAvgMarketCapBacktest(stocks []BacktestResult) float64 {
	if len(stocks) == 0 {
		return 0
	}
	sum := 0.0
	for _, stock := range stocks {
		sum += stock.StockInfo.MarketCap
	}
	return sum / float64(len(stocks))
}

func calculateDataQualityDistribution(stocks []BacktestResult) map[string]int {
	distribution := make(map[string]int)
	for _, stock := range stocks {
		distribution[stock.DataQuality]++
	}
	return distribution
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Convenience function to run a backtest with default settings
func RunEpisodicPivotBacktest(alpacaKey, alpacaSecret, finnhubKey, targetDate string) error {
	fmt.Printf("\n[MAIN] Running episodic pivot backtest for %s...\n", targetDate)

	config := BacktestConfig{
		TargetDate:   targetDate,
		AlpacaKey:    alpacaKey,
		AlpacaSecret: alpacaSecret,
		FinnhubKey:   finnhubKey,
		TiingoKey:    "",
		LookbackDays: 300,
	}

	return FilterStocksEpisodicPivotBacktest(config)
}

// Run multiple backtests for different dates
func RunMultipleDateBacktests(alpacaKey, alpacaSecret, finnhubKey string, dates []string) error {
	fmt.Printf("\n================================================================================\n")
	fmt.Printf("=== RUNNING MULTIPLE DATE BACKTESTS ===\n")
	fmt.Printf("================================================================================\n")
	fmt.Printf("Running backtests for %d dates...\n", len(dates))

	results := make(map[string]int)

	for i, date := range dates {
		fmt.Printf("\n********************************************************************************\n")
		fmt.Printf("*** [%d/%d] BACKTESTING DATE: %s ***\n", i+1, len(dates), date)
		fmt.Printf("********************************************************************************\n")

		config := BacktestConfig{
			TargetDate:   date,
			AlpacaKey:    alpacaKey,
			AlpacaSecret: alpacaSecret,
			FinnhubKey:   finnhubKey,
			TiingoKey:    "",
			LookbackDays: 300,
		}

		gapUpStocks, err := backtestStage1GapUp(config)
		if err != nil {
			fmt.Printf("❌ Error backtesting %s: %v\n", date, err)
			results[date] = 0
			continue
		}

		results[date] = len(gapUpStocks)

		err = FilterStocksEpisodicPivotBacktest(config)
		if err != nil {
			fmt.Printf("❌ Error in full backtest for %s: %v\n", date, err)
		}
	}

	fmt.Printf("\n================================================================================\n")
	fmt.Printf("=== BACKTEST SUMMARY ACROSS ALL DATES ===\n")
	fmt.Printf("================================================================================\n")
	for date, count := range results {
		fmt.Printf("%s: %d qualifying stocks\n", date, count)
	}

	return nil
}

// Analyze backtest performance
func AnalyzeBacktestPerformance(backtestDir string) error {
	fmt.Printf("\n================================================================================\n")
	fmt.Printf("=== ANALYZING BACKTEST PERFORMANCE ===\n")
	fmt.Printf("================================================================================\n")
	fmt.Println("Analyzing backtest performance across dates...")

	files, err := os.ReadDir(backtestDir)
	if err != nil {
		return fmt.Errorf("error reading backtest directory: %v", err)
	}

	var allResults []map[string]interface{}

	fmt.Printf("\n[ANALYSIS] Scanning directory: %s\n", backtestDir)

	for _, file := range files {
		if strings.HasPrefix(file.Name(), "backtest_") && strings.HasSuffix(file.Name(), "_results.json") {
			filePath := filepath.Join(backtestDir, file.Name())
			fmt.Printf("[ANALYSIS] Reading %s...\n", file.Name())

			data, err := os.ReadFile(filePath)
			if err != nil {
				fmt.Printf("⚠️  [ANALYSIS] Error reading %s: %v\n", file.Name(), err)
				continue
			}

			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				fmt.Printf("⚠️  [ANALYSIS] Error parsing %s: %v\n", file.Name(), err)
				continue
			}

			allResults = append(allResults, result)
			fmt.Printf("✅ [ANALYSIS] Successfully loaded %s\n", file.Name())
		}
	}

	if len(allResults) == 0 {
		return fmt.Errorf("no backtest results found in %s", backtestDir)
	}

	fmt.Printf("\n[ANALYSIS] Analyzed %d backtest results\n", len(allResults))

	totalStocks := 0
	dates := make([]string, 0)

	for _, result := range allResults {
		if summary, ok := result["backtest_summary"].(map[string]interface{}); ok {
			if candidates, ok := summary["total_candidates"].(float64); ok {
				totalStocks += int(candidates)
			}
		}

		if date, ok := result["backtest_date"].(string); ok {
			dates = append(dates, date)
		}
	}

	fmt.Printf("\n[ANALYSIS] Summary Statistics:\n")
	fmt.Printf("   Total qualifying stocks across all dates: %d\n", totalStocks)
	fmt.Printf("   Average per date: %.2f\n", float64(totalStocks)/float64(len(allResults)))
	fmt.Printf("   Dates analyzed: %v\n", dates)

	analysisFile := filepath.Join(backtestDir, "backtest_analysis.json")
	analysis := map[string]interface{}{
		"analysis_date":           time.Now().Format(time.RFC3339),
		"total_backtests":         len(allResults),
		"total_qualifying_stocks": totalStocks,
		"average_per_date":        float64(totalStocks) / float64(len(allResults)),
		"dates_analyzed":          dates,
	}

	analysisData, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(analysisFile, analysisData, 0644)
	if err != nil {
		return err
	}

	fmt.Printf("\n✅ [ANALYSIS] Backtest analysis written to %s\n", analysisFile)
	return nil
}
