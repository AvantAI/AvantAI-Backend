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
	TargetDate     string // Format: "2023-01-15" - the date to backtest
	MarketstackKey string // API key for historical data
	TiingoKey      string // Optional - for more historical data if needed
	LookbackDays   int    // Days of historical data to fetch (default: 300)
}

// BacktestResult extends FilteredStock with additional backtest metrics
type BacktestResult struct {
	FilteredStock
	BacktestDate    string   `json:"backtest_date"`
	DataQuality     string   `json:"data_quality"`
	HistoricalDays  int      `json:"historical_days_available"`
	ValidationNotes []string `json:"validation_notes"`
}

// Main backtesting function
func FilterStocksEpisodicPivotBacktest(config BacktestConfig) error {
	fmt.Printf("=== Episodic Pivot Backtesting for %s ===\n", config.TargetDate)

	// Validate target date
	targetDate, err := time.Parse("2006-01-02", config.TargetDate)
	if err != nil {
		return fmt.Errorf("invalid target date format. Use YYYY-MM-DD: %v", err)
	}

	// Ensure we're not trying to backtest future dates
	if targetDate.After(time.Now()) {
		return fmt.Errorf("target date %s is in the future", config.TargetDate)
	}

	// Set default lookback if not specified
	if config.LookbackDays == 0 {
		config.LookbackDays = 300
	}

	fmt.Printf("📅 Target Date: %s\n", config.TargetDate)
	fmt.Printf("📊 Lookback Period: %d days\n", config.LookbackDays)

	// Stage 1: Filter by gap up on target date
	fmt.Println("\n🔍 Stage 1: Backtesting gap up filter...")
	gapUpStocks, err := backtestStage1GapUp(config)
	if err != nil {
		return fmt.Errorf("error in Stage 1 backtest: %v", err)
	}

	fmt.Printf("✅ Stage 1 complete. Found %d stocks with 8%+ gap up on %s\n",
		len(gapUpStocks), config.TargetDate)

	if len(gapUpStocks) == 0 {
		fmt.Println("⚠️ No stocks found with gap up criteria for target date")
		return nil
	}

	// Stage 2: Market cap and liquidity filter (using target date data)
	fmt.Println("\n💰 Stage 2: Backtesting liquidity filter...")
	liquidStocks, err := backtestStage2Liquidity(config, gapUpStocks)
	if err != nil {
		return fmt.Errorf("error in Stage 2 backtest: %v", err)
	}

	fmt.Printf("✅ Stage 2 complete. Found %d stocks meeting liquidity requirements\n",
		len(liquidStocks))

	// Stage 3: Technical analysis using historical data up to target date
	fmt.Println("\n📊 Stage 3: Backtesting technical analysis...")
	technicalStocks, err := backtestStage3Technical(config, liquidStocks)
	if err != nil {
		return fmt.Errorf("error in Stage 3 backtest: %v", err)
	}

	fmt.Printf("✅ Stage 3 complete. Found %d stocks meeting technical criteria\n",
		len(technicalStocks))

	// Stage 4: Final episodic pivot filter
	fmt.Println("\n🎯 Stage 4: Final backtesting criteria...")
	finalStocks := backtestStage4Final(technicalStocks)

	// Output results
	err = outputBacktestResults(config, finalStocks)
	if err != nil {
		return fmt.Errorf("error writing backtest results: %v", err)
	}

	fmt.Printf("\n🎉 Backtest complete! Found %d qualifying stocks for %s\n",
		len(finalStocks), config.TargetDate)
	fmt.Printf("📁 Results written to backtest_%s_results.json\n",
		strings.ReplaceAll(config.TargetDate, "-", ""))

	return nil
}

// Stage 1: Backtest gap up filter
func backtestStage1GapUp(config BacktestConfig) ([]StockData, error) {
	symbols, err := getStockSymbols()
	if err != nil {
		return nil, fmt.Errorf("failed to get stock symbols: %v", err)
	}

	fmt.Printf("📈 Backtesting %d symbols for gap up on %s...\n", len(symbols), config.TargetDate)

	var gapUpStocks []StockData
	var mu sync.Mutex
	processedCount := 0

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
			if processedCount%25 == 0 {
				fmt.Printf("   Processed %d/%d symbols...\n", processedCount, len(symbols))
			}
			mu.Unlock()

			// Get historical data around target date
			currentData, previousData, err := getHistoricalDataForDate(
				config.MarketstackKey, sym, config.TargetDate)
			if err != nil {
				return
			}

			if currentData == nil || previousData == nil {
				return
			}

			if previousData.Close <= 0 {
				return
			}

			gapUp := ((currentData.Open - previousData.Close) / previousData.Close) * 100
			fmt.Println("currentData.Open for ", sym, " is ", currentData.Open)
			fmt.Println("previousData.Close for ", sym, " is ", previousData.Close)
			fmt.Println("gapUp for ", sym, " is ", gapUp)

			// Filter for 8%+ gap up on target date
			if gapUp >= MIN_GAP_UP_PERCENT {
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
					ChangePercent:              gapUp,
					ExtendedHoursQuote:         currentData.Close,
					ExtendedHoursChange:        currentData.Close - previousData.Close,
					ExtendedHoursChangePercent: gapUp,
					Exchange:                   currentData.Exchange,
					Name:                       currentData.Name,
				}

				mu.Lock()
				fmt.Printf("✅ %s qualified on %s! Gap up: %.2f%%\n",
					sym, config.TargetDate, gapUp)
				gapUpStocks = append(gapUpStocks, stockData)
				mu.Unlock()
			}
		}(symbol)
	}

	wg.Wait()
	return gapUpStocks, nil
}

// Stage 2: Backtest liquidity filter
func backtestStage2Liquidity(config BacktestConfig, stocks []StockData) ([]BacktestResult, error) {
	var liquidStocks []BacktestResult
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	fmt.Printf("💰 Backtesting liquidity for %d stocks...\n", len(stocks))

	for i, stock := range stocks {
		fmt.Printf("🏢 [%d/%d] Processing liquidity for %s...\n", i+1, len(stocks), stock.Symbol)
		<-rateLimiter

		// Get company info (this doesn't change much over time)
		companyInfo, err := getTickerInfoV2(config.MarketstackKey, stock.Symbol)
		if err != nil {
			fmt.Printf("⚠️ Could not get company info for %s: %v\n", stock.Symbol, err)
		}

		estimatedMarketCap := estimateMarketCapImproved(stock, companyInfo)

		// Apply minimum liquidity criteria
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

			backtestResult := BacktestResult{
				FilteredStock: FilteredStock{
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
				},
				BacktestDate:    config.TargetDate,
				DataQuality:     "Good",
				ValidationNotes: []string{},
			}

			liquidStocks = append(liquidStocks, backtestResult)
			fmt.Printf("✅ %s passed liquidity filter\n", stock.Symbol)
		} else {
			fmt.Printf("❌ %s rejected: Market cap $%.0f < $%.0f\n",
				stock.Symbol, estimatedMarketCap, MIN_MARKET_CAP)
		}
	}

	return liquidStocks, nil
}

// Stage 3: Backtest technical analysis
func backtestStage3Technical(config BacktestConfig, stocks []BacktestResult) ([]BacktestResult, error) {
	var technicalStocks []BacktestResult
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	fmt.Printf("📊 Backtesting technical analysis for %d stocks...\n", len(stocks))

	for i, stock := range stocks {
		fmt.Printf("📈 [%d/%d] Analyzing %s...\n", i+1, len(stocks), stock.Symbol)
		<-rateLimiter

		// Get historical data up to target date
		historicalData, err := getHistoricalDataUpToDate(
			config.MarketstackKey, stock.Symbol, config.TargetDate, config.LookbackDays)
		if err != nil {
			fmt.Printf("⚠️ Error getting historical data for %s: %v\n", stock.Symbol, err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Historical data error: %v", err))
			continue
		}

		if len(historicalData) < 200 {
			fmt.Printf("⚠️ Insufficient historical data for %s: only %d days\n",
				stock.Symbol, len(historicalData))
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Insufficient data: %d days", len(historicalData)))
			stock.DataQuality = "Poor"
			continue
		}

		stock.HistoricalDays = len(historicalData)

		// Calculate technical indicators using data up to target date
		technicalIndicators, err := calculateTechnicalIndicatorsBacktest(
			historicalData, stock.Symbol, config.TargetDate)
		if err != nil {
			fmt.Printf("⚠️ Error calculating technical indicators for %s: %v\n",
				stock.Symbol, err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Technical analysis error: %v", err))
			continue
		}

		// Calculate dollar volume and ADR using 21 days before target date
		dolVol, adr, err := calculateHistoricalMetricsBacktest(
			historicalData, stock.Symbol, config.TargetDate)
		if err != nil {
			fmt.Printf("⚠️ Error calculating historical metrics for %s: %v\n",
				stock.Symbol, err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Metrics calculation error: %v", err))
			continue
		}

		// Simulate premarket analysis for backtest (real premarket data usually not available historically)
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
		// stock.StockInfo.VolumeDriedUp = technicalIndicators.VolumeDriedUp
		stock.StockInfo.IsNearEMA1020 = technicalIndicators.IsNearEMA1020
		stock.StockInfo.BreaksResistance = technicalIndicators.BreaksResistance

		// Enhanced logging
		fmt.Printf("📊 %s Backtest Analysis for %s:\n", stock.Symbol, config.TargetDate)
		fmt.Printf("   💰 Dollar Volume: $%.0f (Min: $%.0f) %s\n",
			dolVol, MIN_DOLLAR_VOLUME, checkmark(dolVol >= MIN_DOLLAR_VOLUME))
		fmt.Printf("   📏 ADR: %.2f%% (Min: %.2f%%) %s\n",
			adr, MIN_ADR_PERCENT, checkmark(adr >= MIN_ADR_PERCENT))
		fmt.Printf("   📈 Above 200 EMA: %s (Price: $%.2f, EMA200: $%.2f)\n",
			checkmark(technicalIndicators.IsAbove200EMA),
			getCurrentPriceBacktest(historicalData), technicalIndicators.EMA200)
		fmt.Printf("   📊 Historical Days: %d\n", len(historicalData))

		technicalStocks = append(technicalStocks, stock)
	}

	return technicalStocks, nil
}

// Stage 4: Final backtest filter
func backtestStage4Final(stocks []BacktestResult) []BacktestResult {
	var finalStocks []BacktestResult

	fmt.Printf("🎯 Applying final episodic pivot criteria to %d stocks...\n", len(stocks))

	for _, stock := range stocks {
		fmt.Printf("\n🔍 Final backtest analysis for %s:\n", stock.Symbol)

		// Check all episodic pivot criteria
		criteria := []struct {
			name    string
			passed  bool
			details string
		}{
			{"Dollar Volume", stock.StockInfo.DolVol >= MIN_DOLLAR_VOLUME,
				fmt.Sprintf("$%.0f >= $%.0f", stock.StockInfo.DolVol, MIN_DOLLAR_VOLUME)},
			{"Premarket Volume Ratio", stock.StockInfo.PremarketVolumeRatio >= MIN_PREMARKET_VOL_RATIO,
				fmt.Sprintf("%.1fx >= %.1fx (simulated)", stock.StockInfo.PremarketVolumeRatio, MIN_PREMARKET_VOL_RATIO)},
			{"Above 200 EMA", stock.StockInfo.IsAbove200EMA,
				fmt.Sprintf("Price above 200 EMA")},
			{"Not Extended", !stock.StockInfo.IsExtended,
				fmt.Sprintf("Distance from 50 EMA: %.2f ADRs", stock.StockInfo.DistanceFrom50EMA)},
			// {"Volume Dried Up", stock.StockInfo.VolumeDriedUp,
			// 	fmt.Sprintf("20-day avg vol < 60-day avg vol")},
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

		if allPassed && !stock.StockInfo.IsTooExtended {
			finalStocks = append(finalStocks, stock)
			fmt.Printf("🎉 %s QUALIFIED in backtest!\n", stock.Symbol)
		} else {
			reason := "Failed criteria"
			if stock.StockInfo.IsTooExtended {
				reason = "Too extended (>8 ADRs from 50 EMA)"
			}
			fmt.Printf("❌ %s rejected in backtest: %s\n", stock.Symbol, reason)
		}
	}

	return finalStocks
}

// Helper functions for backtesting

// Get historical data for specific date and previous trading day
func getHistoricalDataForDate(apiKey, symbol, targetDate string) (*MarketstackEODData, *MarketstackEODData, error) {
	// Parse target date
	target, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return nil, nil, err
	}

	// Get a few days around target date to ensure we get the exact date and previous trading day
	startDate := target.AddDate(0, 0, -5).Format("2006-01-02")
	endDate := target.AddDate(0, 0, 1).Format("2006-01-02")

	url := fmt.Sprintf("https://api.marketstack.com/v2/eod?access_key=%s&symbols=%s&date_from=%s&date_to=%s&sort=DESC&exchange=NASDAQ,NYSE",
		apiKey, symbol, startDate, endDate)

	resp, err := http.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %v", err)
	}

	var eodResponse MarketstackEODResponse
	if err := json.Unmarshal(body, &eodResponse); err != nil {
		return nil, nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	if len(eodResponse.Data) < 2 {
		return nil, nil, fmt.Errorf("insufficient data: only %d records", len(eodResponse.Data))
	}

	// Sort by date to find target date and previous trading day
	sort.Slice(eodResponse.Data, func(i, j int) bool {
		return eodResponse.Data[i].Date > eodResponse.Data[j].Date
	})

	var currentData, previousData *MarketstackEODData

	// Find the target date
	for i, data := range eodResponse.Data {
		if strings.HasPrefix(data.Date, targetDate) {
			currentData = &data
			// Previous trading day should be next in sorted array
			if i+1 < len(eodResponse.Data) {
				previousData = &eodResponse.Data[i+1]
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

// Get historical data up to a specific date
func getHistoricalDataUpToDate(apiKey, symbol, targetDate string, lookbackDays int) ([]MarketstackEODData, error) {
	target, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return nil, err
	}

	// Calculate start date with extra buffer for weekends/holidays
	startDate := target.AddDate(0, 0, -(lookbackDays + 50)).Format("2006-01-02")

	url := fmt.Sprintf("https://api.marketstack.com/v2/eod?access_key=%s&symbols=%s&date_from=%s&date_to=%s&limit=1000&sort=DESC&exchange=NASDAQ,NYSE",
		apiKey, symbol, startDate, targetDate)

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

	// Filter data to only include dates up to and including target date
	var filteredData []MarketstackEODData
	for _, data := range eodResponse.Data {
		dataDate, err := time.Parse("2006-01-02T15:04:05+0000", data.Date)
		if err != nil {
			// Try alternative date format
			dataDate, err = time.Parse("2006-01-02", data.Date[:10])
			if err != nil {
				continue
			}
		}

		if dataDate.Before(target.AddDate(0, 0, 1)) { // Include target date
			filteredData = append(filteredData, data)
		}
	}

	return filteredData, nil
}

// Calculate technical indicators for backtesting
func calculateTechnicalIndicatorsBacktest(historicalData []MarketstackEODData, symbol, targetDate string) (*TechnicalIndicators, error) {
	if len(historicalData) < 200 {
		return nil, fmt.Errorf("insufficient data for technical indicators")
	}

	// Sort data by date (oldest first for calculations)
	sort.Slice(historicalData, func(i, j int) bool {
		return historicalData[i].Date < historicalData[j].Date
	})

	// Find the index for target date to use as "current" price
	targetIndex := len(historicalData) - 1 // Default to last day
	for i, data := range historicalData {
		if strings.HasPrefix(data.Date, targetDate) {
			targetIndex = i
			break
		}
	}

	// Use data up to target date for calculations
	dataUpToTarget := historicalData[:targetIndex+1]
	if len(dataUpToTarget) < 200 {
		return nil, fmt.Errorf("insufficient data up to target date")
	}

	currentPrice := dataUpToTarget[len(dataUpToTarget)-1].Close
	currentADR := calculateADRForPeriod(dataUpToTarget[len(dataUpToTarget)-21:])

	// Calculate moving averages
	sma200 := calculateSMA(dataUpToTarget, 200)
	ema200 := calculateEMA(dataUpToTarget, 200)
	ema50 := calculateEMA(dataUpToTarget, 50)
	ema20 := calculateEMA(dataUpToTarget, 20)
	ema10 := calculateEMA(dataUpToTarget, 10)

	// Calculate distance from 50 EMA in ADRs
	distanceFrom50EMA := 0.0
	if currentADR > 0 {
		distanceFrom50EMA = (currentPrice - ema50) / (currentADR * currentPrice / 100)
	}

	// Check if volume has dried up
	// volumeDriedUp := false
	// if len(dataUpToTarget) >= 60 {
	// 	vol20 := calculateAvgVolume(dataUpToTarget[len(dataUpToTarget)-20:])
	// 	vol60 := calculateAvgVolume(dataUpToTarget[len(dataUpToTarget)-60:])
	// 	volumeDriedUp = vol20 < vol60
	// }

	// Check if near 10/20 EMA
	distanceFrom10EMA := 0.0
	distanceFrom20EMA := 0.0
	if currentADR > 0 {
		adrValue := currentADR * currentPrice / 100
		distanceFrom10EMA = abs(currentPrice-ema10) / adrValue
		distanceFrom20EMA = abs(currentPrice-ema20) / adrValue
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
		// VolumeDriedUp:     volumeDriedUp,
		IsNearEMA1020:     isNearEMA1020,
		BreaksResistance:  breaksResistance,
	}

	fmt.Printf("📊 Technical indicators calculated for %s on %s\n", symbol, targetDate)
	return indicators, nil
}

// Calculate historical metrics for backtesting
func calculateHistoricalMetricsBacktest(historicalData []MarketstackEODData, symbol, targetDate string) (float64, float64, error) {
	// Sort by date (newest first)
	sort.Slice(historicalData, func(i, j int) bool {
		return historicalData[i].Date > historicalData[j].Date
	})

	// Find target date index
	targetIndex := -1
	for i, data := range historicalData {
		if strings.HasPrefix(data.Date, targetDate) {
			targetIndex = i
			break
		}
	}

	if targetIndex == -1 {
		return 0, 0, fmt.Errorf("target date not found in historical data")
	}

	// Use 21 days including target date
	endIndex := targetIndex + 21
	if endIndex > len(historicalData) {
		endIndex = len(historicalData)
	}

	metricsData := historicalData[targetIndex:endIndex]

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

	return avgDolVol, avgADR, nil
}

// Get real historical premarket data from Tiingo, with fallback to simulation
func getHistoricalPremarketDataBacktest(tiingoKey, symbol, targetDate string, historicalData []MarketstackEODData, dolVol float64) (*PremarketAnalysis, error) {
	if tiingoKey == "" {
		return nil, fmt.Errorf("no Tiingo API key provided")
	}

	fmt.Printf("🔍 Attempting to get real historical premarket data for %s on %s...\n", symbol, targetDate)

	// Parse target date
	target, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return nil, fmt.Errorf("invalid target date: %v", err)
	}

	// Get premarket data for target date
	currentPremarketVol, err := getPremarketVolumeForDateTiingo(tiingoKey, symbol, targetDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get current premarket volume: %v", err)
	}

	// Get average premarket volumes from previous 20 trading days
	avgPremarketVol, avgDailyVol, err := getHistoricalPremarketAveragesTiingo(tiingoKey, symbol, target.AddDate(0, 0, -30), target.AddDate(0, 0, -1))
	if err != nil {
		return nil, fmt.Errorf("failed to get historical premarket averages: %v", err)
	}

	// Calculate ratios
	volumeRatio := 0.0
	if avgPremarketVol > 0 {
		volumeRatio = currentPremarketVol / avgPremarketVol
	}

	volAsPercentOfDaily := 0.0
	if avgDailyVol > 0 {
		volAsPercentOfDaily = (currentPremarketVol / avgDailyVol) * 100
	}

	analysis := &PremarketAnalysis{
		CurrentPremarketVol: currentPremarketVol,
		AvgPremarketVol:     avgPremarketVol,
		VolumeRatio:         volumeRatio,
		VolAsPercentOfDaily: volAsPercentOfDaily,
	}

	fmt.Printf("📊 Real premarket data for %s:\n", symbol)
	fmt.Printf("   Current: %.0f shares\n", currentPremarketVol)
	fmt.Printf("   Average: %.0f shares\n", avgPremarketVol)
	fmt.Printf("   Ratio: %.2fx\n", volumeRatio)
	fmt.Printf("   As %% Daily: %.2f%%\n", volAsPercentOfDaily)

	return analysis, nil
}

// Get premarket volume for a specific historical date
func getPremarketVolumeForDateTiingo(tiingoKey, symbol, targetDate string) (float64, error) {
	// Parse target date and create date range for API call
	// target, err := time.Parse("2006-01-02", targetDate)
	// if err != nil {
	// 	return 0, err
	// }

	// Request minute-level data for the target date
	url := fmt.Sprintf("https://api.tiingo.com/iex/%s/prices?startDate=%s&endDate=%s&resampleFreq=1min&afterHours=true&columns=open,high,low,close,volume&token=%s",
		symbol, targetDate, targetDate, tiingoKey)

	resp, err := http.Get(url)
	if err != nil {
		return 0, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %v", err)
	}

	// Check for API errors
	if strings.Contains(string(body), "\"detail\":") {
		return 0, fmt.Errorf("API error: %s", string(body))
	}

	var candles []struct {
		Timestamp string  `json:"date"`
		Volume    float64 `json:"volume"`
	}
	if err := json.Unmarshal(body, &candles); err != nil {
		return 0, fmt.Errorf("failed to parse JSON: %v", err)
	}

	if len(candles) == 0 {
		return 0, fmt.Errorf("no intraday data returned for %s", targetDate)
	}

	// Calculate premarket volume (4:00 AM - 9:30 AM ET)
	premarketVol := 0.0
	estLocation, _ := time.LoadLocation("America/New_York")

	for _, candle := range candles {
		timestamp, err := time.Parse(time.RFC3339, candle.Timestamp)
		if err != nil {
			continue
		}

		estTime := timestamp.In(estLocation)
		hour := estTime.Hour()
		minute := estTime.Minute()

		// Premarket hours: 4:00 AM - 9:30 AM ET
		if (hour >= 4 && hour < 9) || (hour == 9 && minute < 30) {
			premarketVol += candle.Volume
		}
	}

	if premarketVol == 0 {
		return 0, fmt.Errorf("no premarket volume found for %s", targetDate)
	}

	return premarketVol, nil
}

// Get historical premarket averages over a date range
func getHistoricalPremarketAveragesTiingo(tiingoKey, symbol string, startDate, endDate time.Time) (float64, float64, error) {
	// Format dates for API
	start := startDate.Format("2006-01-02")
	end := endDate.Format("2006-01-02")

	// Try minute-level data first
	url := fmt.Sprintf("https://api.tiingo.com/iex/%s/prices?startDate=%s&endDate=%s&resampleFreq=1min&afterHours=true&columns=open,high,low,close,volume&token=%s",
		symbol, start, end, tiingoKey)

	resp, err := http.Get(url)
	if err != nil {
		return 0, 0, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// Try hourly data if minute data fails
		return getHistoricalPremarketAveragesHourlyTiingo(tiingoKey, symbol, start, end)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read response: %v", err)
	}

	// Check for API errors and fallback to hourly
	if strings.Contains(string(body), "\"detail\":") {
		return getHistoricalPremarketAveragesHourlyTiingo(tiingoKey, symbol, start, end)
	}

	var candles []struct {
		Timestamp string  `json:"date"`
		Volume    float64 `json:"volume"`
	}
	if err := json.Unmarshal(body, &candles); err != nil {
		return 0, 0, fmt.Errorf("failed to parse minute data: %v", err)
	}

	if len(candles) == 0 {
		return getHistoricalPremarketAveragesHourlyTiingo(tiingoKey, symbol, start, end)
	}

	// Process minute-level data to get daily premarket and total volumes
	return processIntradayDataForPremarket(candles)
}

// Fallback to hourly data if minute data is not available
func getHistoricalPremarketAveragesHourlyTiingo(tiingoKey, symbol, start, end string) (float64, float64, error) {
	url := fmt.Sprintf("https://api.tiingo.com/iex/%s/prices?startDate=%s&endDate=%s&resampleFreq=1hour&afterHours=true&columns=open,high,low,close,volume&token=%s",
		symbol, start, end, tiingoKey)

	resp, err := http.Get(url)
	if err != nil {
		return 0, 0, fmt.Errorf("hourly HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, 0, fmt.Errorf("hourly API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read hourly response: %v", err)
	}

	if strings.Contains(string(body), "\"detail\":") {
		return 0, 0, fmt.Errorf("hourly API error: %s", string(body))
	}

	var candles []struct {
		Timestamp string  `json:"date"`
		Volume    float64 `json:"volume"`
	}
	if err := json.Unmarshal(body, &candles); err != nil {
		return 0, 0, fmt.Errorf("failed to parse hourly data: %v", err)
	}

	if len(candles) == 0 {
		return 0, 0, fmt.Errorf("no hourly data returned")
	}

	return processIntradayDataForPremarket(candles)
}

// Process intraday data to extract premarket volumes
func processIntradayDataForPremarket(candles []struct {
	Timestamp string  `json:"date"`
	Volume    float64 `json:"volume"`
}) (float64, float64, error) {

	dailyPremarketVolumes := make(map[string]float64)
	dailyTotalVolumes := make(map[string]float64)

	estLocation, _ := time.LoadLocation("America/New_York")

	for _, candle := range candles {
		timestamp, err := time.Parse(time.RFC3339, candle.Timestamp)
		if err != nil {
			continue
		}

		estTime := timestamp.In(estLocation)
		dayKey := estTime.Format("2006-01-02")
		hour := estTime.Hour()
		minute := estTime.Minute()

		// Add to daily total
		dailyTotalVolumes[dayKey] += candle.Volume

		// Check if it's premarket (4:00 AM - 9:30 AM ET)
		if (hour >= 4 && hour < 9) || (hour == 9 && minute < 30) {
			dailyPremarketVolumes[dayKey] += candle.Volume
		}
	}

	if len(dailyPremarketVolumes) == 0 {
		return 0, 0, fmt.Errorf("no premarket data found in date range")
	}

	// Calculate averages
	var totalPremarketVol, totalDailyVol float64
	validDays := 0

	for day, premarketVol := range dailyPremarketVolumes {
		if premarketVol > 0 {
			totalPremarketVol += premarketVol
			if dailyVol, exists := dailyTotalVolumes[day]; exists && dailyVol > 0 {
				totalDailyVol += dailyVol
				validDays++
			}
		}
	}

	if validDays == 0 {
		return 0, 0, fmt.Errorf("no valid trading days found")
	}

	avgPremarketVol := totalPremarketVol / float64(validDays)
	avgDailyVol := totalDailyVol / float64(validDays)

	fmt.Printf("✅ Historical premarket analysis over %d trading days:\n", validDays)
	fmt.Printf("   Avg Premarket Volume: %.0f shares\n", avgPremarketVol)
	fmt.Printf("   Avg Daily Volume: %.0f shares\n", avgDailyVol)

	return avgPremarketVol, avgDailyVol, nil
}

// Simulate premarket analysis for backtesting
func simulatePremarketAnalysisBacktest(historicalData []MarketstackEODData, dolVol float64) *PremarketAnalysis {
	// For backtesting, we simulate premarket activity based on historical patterns
	// In reality, historical premarket data is rarely available for backtesting

	if len(historicalData) == 0 {
		return &PremarketAnalysis{}
	}

	// Calculate average daily volume from recent data
	avgDailyVol := calculateAvgVolume(historicalData[:min(20, len(historicalData))])

	// Simulate premarket metrics for gap-up stocks
	// Assume elevated premarket activity for stocks that gap up significantly
	estimatedPremarketVol := avgDailyVol * 0.12              // Typical premarket ~12% of daily
	estimatedCurrentPremarket := estimatedPremarketVol * 6.5 // Simulate 6.5x for strong gap-up

	return &PremarketAnalysis{
		CurrentPremarketVol: estimatedCurrentPremarket,
		AvgPremarketVol:     estimatedPremarketVol,
		VolumeRatio:         6.5,  // Simulated 6.5x ratio for backtest
		VolAsPercentOfDaily: 25.0, // Simulated 25% of daily volume
	}
}

// Output backtest results
func outputBacktestResults(config BacktestConfig, stocks []BacktestResult) error {
	// Create detailed backtest output
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
	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		return err
	}

	// Also create a CSV summary for easy analysis
	err = outputBacktestCSVSummary(stocks, stockDir, dateStr)
	if err != nil {
		fmt.Printf("Warning: Could not create CSV summary: %v\n", err)
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

	// Write header
	headers := []string{
		"Symbol", "Name", "Sector", "Industry", "Gap Up %", "Market Cap",
		"Dollar Volume", "ADR %", "Above 200 EMA", "Extended", "Volume Dried Up",
		"Historical Days", "Data Quality", "Validation Notes",
	}
	writer.Write(headers)

	// Write data
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
			// boolToString(stock.StockInfo.VolumeDriedUp),
			fmt.Sprintf("%d", stock.HistoricalDays),
			stock.DataQuality,
			validationNotes,
		}
		writer.Write(row)
	}

	fmt.Printf("CSV backtest summary written to %s\n", filename)
	return nil
}

// Helper functions for backtest calculations
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

func getCurrentPriceBacktest(historicalData []MarketstackEODData) float64 {
	if len(historicalData) == 0 {
		return 0
	}
	// For backtest, the "current" price is the most recent in the historical data
	return historicalData[0].Close
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Convenience function to run a backtest with default settings
func RunEpisodicPivotBacktest(marketstackKey, targetDate string) error {
	config := BacktestConfig{
		TargetDate:     targetDate,
		MarketstackKey: marketstackKey,
		TiingoKey:      "",  // Optional
		LookbackDays:   300, // Default lookback
	}

	return FilterStocksEpisodicPivotBacktest(config)
}

// Run multiple backtests for different dates
func RunMultipleDateBacktests(marketstackKey string, dates []string) error {
	fmt.Printf("Running backtests for %d dates...\n", len(dates))

	results := make(map[string]int)

	for i, date := range dates {
		fmt.Printf("\n[%d/%d] Backtesting %s...\n", i+1, len(dates), date)

		config := BacktestConfig{
			TargetDate:     date,
			MarketstackKey: marketstackKey,
			TiingoKey:      "",
			LookbackDays:   300,
		}

		// Capture results before filtering
		gapUpStocks, err := backtestStage1GapUp(config)
		if err != nil {
			fmt.Printf("Error backtesting %s: %v\n", date, err)
			results[date] = 0
			continue
		}

		results[date] = len(gapUpStocks)

		// Run full backtest
		err = FilterStocksEpisodicPivotBacktest(config)
		if err != nil {
			fmt.Printf("Error in full backtest for %s: %v\n", date, err)
		}
	}

	// Output summary
	fmt.Printf("\n=== BACKTEST SUMMARY ===\n")
	for date, count := range results {
		fmt.Printf("%s: %d qualifying stocks\n", date, count)
	}

	return nil
}

// Analyze backtest performance by comparing multiple dates
func AnalyzeBacktestPerformance(backtestDir string) error {
	fmt.Println("Analyzing backtest performance across dates...")

	// Read all backtest JSON files
	files, err := os.ReadDir(backtestDir)
	if err != nil {
		return fmt.Errorf("error reading backtest directory: %v", err)
	}

	var allResults []map[string]interface{}

	for _, file := range files {
		if strings.HasPrefix(file.Name(), "backtest_") && strings.HasSuffix(file.Name(), "_results.json") {
			filePath := filepath.Join(backtestDir, file.Name())

			data, err := os.ReadFile(filePath)
			if err != nil {
				fmt.Printf("Error reading %s: %v\n", file.Name(), err)
				continue
			}

			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				fmt.Printf("Error parsing %s: %v\n", file.Name(), err)
				continue
			}

			allResults = append(allResults, result)
		}
	}

	if len(allResults) == 0 {
		return fmt.Errorf("no backtest results found in %s", backtestDir)
	}

	// Analyze results
	fmt.Printf("Analyzed %d backtest results\n", len(allResults))

	// Calculate statistics
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

	fmt.Printf("Total qualifying stocks across all dates: %d\n", totalStocks)
	fmt.Printf("Average per date: %.2f\n", float64(totalStocks)/float64(len(allResults)))

	// Output analysis to file
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

	fmt.Printf("Backtest analysis written to %s\n", analysisFile)
	return nil
}
