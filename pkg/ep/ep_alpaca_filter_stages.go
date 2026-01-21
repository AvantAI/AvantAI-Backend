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

// AlpacaConfig holds the configuration for real-time scanning
type AlpacaConfig struct {
	APIKey    string
	APISecret string
	BaseURL   string // "https://paper-api.alpaca.markets" or "https://api.alpaca.markets"
	DataURL   string // "https://data.alpaca.markets"
	IsPaper   bool
}

// AlpacaBar represents a single bar from Alpaca
type AlpacaBar struct {
	Timestamp  string  `json:"t"`
	Open       float64 `json:"o"`
	High       float64 `json:"h"`
	Low        float64 `json:"l"`
	Close      float64 `json:"c"`
	Volume     float64 `json:"v"`
	VWAP       float64 `json:"vw"`
	TradeCount int     `json:"n"`
}

// AlpacaSingleSymbolBarsResponse for single symbol endpoint
type AlpacaSingleSymbolBarsResponse struct {
	Bars          []AlpacaBar `json:"bars"`
	Symbol        string      `json:"symbol"`
	NextPageToken string      `json:"next_page_token"`
}

// RealtimeStockData holds real-time stock information
type RealtimeStockData struct {
	Symbol          string
	CurrentPrice    float64
	PremarketOpen   float64
	PremarketHigh   float64
	PremarketLow    float64
	PremarketClose  float64
	PremarketVolume float64
	PreviousClose   float64
	GapUpPercent    float64
	Exchange        string
	Name            string
}

// RealtimeResult extends FilteredStock with real-time metrics
type RealtimeResult struct {
	FilteredStock
	ScanTime        string   `json:"scan_time"`
	MarketStatus    string   `json:"market_status"`
	DataQuality     string   `json:"data_quality"`
	ValidationNotes []string `json:"validation_notes"`
}

// Main real-time scanning function
func FilterStocksEpisodicPivotRealtime(config AlpacaConfig) error {
	fmt.Println("=== Episodic Pivot Real-Time Scanner ===")

	est, err := time.LoadLocation("America/New_York")
	if err != nil {
		return fmt.Errorf("failed to load EST timezone: %v", err)
	}

	now := time.Now().In(est)
	fmt.Printf("🕒 Scan Time: %s EST\n", now.Format("2006-01-02 15:04:05"))

	// Check if we're in premarket hours (4:00 AM - 9:30 AM EST)
	marketStatus := getMarketStatus(now)
	fmt.Printf("📊 Market Status: %s\n", marketStatus)

	if marketStatus != "PREMARKET" && marketStatus != "OPEN" {
		fmt.Printf("⚠️  Warning: Running outside premarket hours. Current time: %s EST\n",
			now.Format("15:04:05"))
	}

	// Stage 1: Filter by gap up using premarket data
	fmt.Println("\n🔍 Stage 1: Scanning for gap ups in premarket...")
	gapUpStocks, err := realtimeStage1GapUp(config)
	if err != nil {
		return fmt.Errorf("error in Stage 1: %v", err)
	}

	fmt.Printf("✅ Stage 1 complete. Found %d stocks with 8%+ gap up\n", len(gapUpStocks))

	if len(gapUpStocks) == 0 {
		fmt.Println("⚠️  No stocks found with gap up criteria")
		return nil
	}

	// Stage 2: Market cap and liquidity filter
	fmt.Println("\n💰 Stage 2: Filtering by liquidity...")
	liquidStocks, err := realtimeStage2Liquidity(config, gapUpStocks)
	if err != nil {
		return fmt.Errorf("error in Stage 2: %v", err)
	}

	fmt.Printf("✅ Stage 2 complete. Found %d stocks meeting liquidity requirements\n",
		len(liquidStocks))

	// Stage 3: Technical analysis
	fmt.Println("\n📊 Stage 3: Technical analysis...")
	technicalStocks, err := realtimeStage3Technical(config, liquidStocks)
	if err != nil {
		return fmt.Errorf("error in Stage 3: %v", err)
	}

	fmt.Printf("✅ Stage 3 complete. Found %d stocks meeting technical criteria\n",
		len(technicalStocks))

	// Stage 4: Final episodic pivot filter
	fmt.Println("\n🎯 Stage 4: Final criteria...")
	finalStocks := realtimeStage4Final(technicalStocks)

	// Output results
	err = outputRealtimeResults(finalStocks, now)
	if err != nil {
		return fmt.Errorf("error writing results: %v", err)
	}

	fmt.Printf("\n🎉 Scan complete! Found %d qualifying stocks\n", len(finalStocks))
	fmt.Printf("📁 Results written to data/stockdata/filtered_stocks_marketstack.json\n")

	return nil
}

// Stage 1: Real-time gap up filter
func realtimeStage1GapUp(config AlpacaConfig) ([]RealtimeStockData, error) {
	symbols, err := getAlpacaTradableSymbolsMain(config)
	if err != nil {
		return nil, fmt.Errorf("failed to get symbols: %v", err)
	}

	fmt.Printf("📈 Scanning %d symbols for gap ups...\n", len(symbols))

	var gapUpStocks []RealtimeStockData
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
			if processedCount%50 == 0 {
				fmt.Printf("   Processed %d/%d symbols...\n", processedCount, len(symbols))
			}
			mu.Unlock()

			// Get previous close
			fmt.Printf("   [%s] Fetching previous close...\n", sym)
			previousClose, err := getAlpacaPreviousClose(config, sym)
			if err != nil {
				fmt.Printf("   [%s] ❌ Failed to get previous close: %v\n", sym, err)
				return
			}
			if previousClose <= 0 {
				fmt.Printf("   [%s] ❌ Invalid previous close: $%.2f\n", sym, previousClose)
				return
			}
			fmt.Printf("   [%s] ✓ Previous close: $%.2f\n", sym, previousClose)

			// Get premarket data
			fmt.Printf("   [%s] Fetching premarket data...\n", sym)
			premarketData, err := getAlpacaPremarketData(config, sym)
			if err != nil {
				fmt.Printf("   [%s] ❌ Failed to get premarket data: %v\n", sym, err)
				return
			}
			if premarketData == nil {
				fmt.Printf("   [%s] ❌ No premarket data available\n", sym)
				return
			}
			fmt.Printf("   [%s] ✓ Premarket Open: $%.2f, Volume: %.0f\n",
				sym, premarketData.PremarketOpen, premarketData.PremarketVolume)

			// Calculate gap up
			if premarketData.PremarketOpen <= 0 || previousClose <= 0 {
				fmt.Printf("   [%s] ❌ Invalid prices for gap calculation\n", sym)
				return
			}

			gapUp := ((premarketData.PremarketOpen - previousClose) / previousClose) * 100
			fmt.Printf("   [%s] Gap Up: %.2f%%\n", sym, gapUp)

			// Filter for 8%+ gap up
			if gapUp >= MIN_GAP_UP_PERCENT {
				stockData := RealtimeStockData{
					Symbol:          sym,
					CurrentPrice:    premarketData.PremarketClose,
					PremarketOpen:   premarketData.PremarketOpen,
					PremarketHigh:   premarketData.PremarketHigh,
					PremarketLow:    premarketData.PremarketLow,
					PremarketClose:  premarketData.PremarketClose,
					PremarketVolume: premarketData.PremarketVolume,
					PreviousClose:   previousClose,
					GapUpPercent:    gapUp,
				}

				mu.Lock()
				fmt.Printf("   [%s] ✅✅✅ QUALIFIED! Gap up: %.2f%% (Prev: $%.2f, PM Open: $%.2f) ✅✅✅\n",
					sym, gapUp, previousClose, premarketData.PremarketOpen)
				gapUpStocks = append(gapUpStocks, stockData)
				mu.Unlock()
			} else {
				fmt.Printf("   [%s] ❌ Gap up %.2f%% < %.2f%% minimum\n", sym, gapUp, MIN_GAP_UP_PERCENT)
			}
		}(symbol)
	}

	wg.Wait()
	return gapUpStocks, nil
}

// Stage 2: Real-time liquidity filter
func realtimeStage2Liquidity(config AlpacaConfig, stocks []RealtimeStockData) ([]RealtimeResult, error) {
	var liquidStocks []RealtimeResult
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	fmt.Printf("💰 Checking liquidity for %d stocks...\n", len(stocks))

	for i, stock := range stocks {
		fmt.Printf("\n🏢 [%d/%d] ========== Processing %s ==========\n", i+1, len(stocks), stock.Symbol)
		fmt.Printf("   Current Price: $%.2f\n", stock.CurrentPrice)
		fmt.Printf("   Premarket Volume: %.0f shares\n", stock.PremarketVolume)
		<-rateLimiter

		// Get company info from Alpaca
		fmt.Printf("   Fetching company info from Alpaca...\n")
		asset, err := getAlpacaAssetInfo(config, stock.Symbol)
		if err != nil {
			fmt.Printf("   ⚠️  Could not get asset info for %s: %v\n", stock.Symbol, err)
		} else {
			fmt.Printf("   ✓ Company: %s | Exchange: %s\n", asset.Name, asset.Exchange)
		}

		// Estimate market cap based on current price and volume
		fmt.Printf("   Estimating market cap...\n")
		estimatedMarketCap := estimateMarketCapFromRealtime(stock)
		fmt.Printf("   Estimated Market Cap: $%.0fM\n", estimatedMarketCap/1_000_000)
		fmt.Printf("   Minimum Required: $%.0fM\n", MIN_MARKET_CAP/1_000_000)

		// Apply minimum liquidity criteria
		if estimatedMarketCap >= MIN_MARKET_CAP {
			name := stock.Symbol
			exchange := "Unknown"

			if asset != nil {
				name = asset.Name
				exchange = asset.Exchange
			}

			result := RealtimeResult{
				FilteredStock: FilteredStock{
					Symbol: stock.Symbol,
					StockInfo: StockStats{
						Timestamp: time.Now().Format(time.RFC3339),
						MarketCap: estimatedMarketCap,
						GapUp:     stock.GapUpPercent,
						Name:      name,
						Exchange:  exchange,
					},
				},
				ScanTime:        time.Now().Format(time.RFC3339),
				MarketStatus:    "PREMARKET",
				DataQuality:     "Good",
				ValidationNotes: []string{},
			}

			liquidStocks = append(liquidStocks, result)
			fmt.Printf("   ✅ %s PASSED LIQUIDITY FILTER (Market Cap: $%.0fM)\n",
				stock.Symbol, estimatedMarketCap/1_000_000)
		} else {
			fmt.Printf("   ❌ %s REJECTED: Market cap $%.0fM < $%.0fM minimum\n",
				stock.Symbol, estimatedMarketCap/1_000_000, MIN_MARKET_CAP/1_000_000)
		}
	}

	return liquidStocks, nil
}

// Stage 3: Real-time technical analysis
func realtimeStage3Technical(config AlpacaConfig, stocks []RealtimeResult) ([]RealtimeResult, error) {
	var technicalStocks []RealtimeResult
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	fmt.Printf("\n📊 Analyzing technical indicators for %d stocks...\n", len(stocks))

	for i, stock := range stocks {
		fmt.Printf("\n📈 [%d/%d] ========== TECHNICAL ANALYSIS: %s ==========\n", i+1, len(stocks), stock.Symbol)
		<-rateLimiter

		// Get historical data (last 300 days)
		fmt.Printf("   Step 1: Fetching historical data (300 days)...\n")
		historicalData, err := getAlpacaHistoricalBars(config, stock.Symbol, 300)
		if err != nil {
			fmt.Printf("   ❌ Error getting historical data for %s: %v\n", stock.Symbol, err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Historical data error: %v", err))
			continue
		}
		fmt.Printf("   ✓ Retrieved %d days of historical data\n", len(historicalData))

		if len(historicalData) < 200 {
			fmt.Printf("   ❌ Insufficient historical data: only %d days (need 200)\n", len(historicalData))
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Insufficient data: %d days", len(historicalData)))
			stock.DataQuality = "Poor"
			continue
		}

		// Calculate technical indicators
		fmt.Printf("   Step 2: Calculating technical indicators...\n")
		technicalIndicators, err := calculateTechnicalIndicatorsRealtime(
			historicalData, stock.Symbol)
		if err != nil {
			fmt.Printf("   ❌ Error calculating indicators for %s: %v\n", stock.Symbol, err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Technical analysis error: %v", err))
			continue
		}
		fmt.Printf("   ✓ Technical indicators calculated\n")
		fmt.Printf("      SMA200: $%.2f | EMA200: $%.2f\n", technicalIndicators.SMA200, technicalIndicators.EMA200)
		fmt.Printf("      EMA50: $%.2f | EMA20: $%.2f | EMA10: $%.2f\n",
			technicalIndicators.EMA50, technicalIndicators.EMA20, technicalIndicators.EMA10)

		// Calculate dollar volume and ADR
		fmt.Printf("   Step 3: Calculating dollar volume and ADR (21-day average)...\n")
		dolVol, adr, err := calculateHistoricalMetricsRealtime(historicalData)
		if err != nil {
			fmt.Printf("   ❌ Error calculating metrics for %s: %v\n", stock.Symbol, err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Metrics calculation error: %v", err))
			continue
		}
		fmt.Printf("   ✓ Dollar Volume: $%.0fM (Min: $%.0fM)\n", dolVol/1_000_000, MIN_DOLLAR_VOLUME/1_000_000)
		fmt.Printf("   ✓ ADR: %.2f%% (Min: %.2f%%)\n", adr, MIN_ADR_PERCENT)

		// Get premarket volume metrics
		fmt.Printf("   Step 4: Calculating premarket volume metrics...\n")
		premarketMetrics, err := getAlpacaPremarketMetrics(config, stock.Symbol, dolVol)
		if err != nil {
			fmt.Printf("   ⚠️  Error getting premarket metrics for %s: %v\n", stock.Symbol, err)
		} else {
			fmt.Printf("   ✓ Current PM Volume: %.0f shares\n", premarketMetrics.CurrentPremarketVol)
			fmt.Printf("   ✓ Avg PM Volume: %.0f shares\n", premarketMetrics.AvgPremarketVol)
			fmt.Printf("   ✓ PM Volume Ratio: %.2fx (Min: %.2fx)\n",
				premarketMetrics.VolumeRatio, MIN_PREMARKET_VOL_RATIO)
		}

		// Update stock with all metrics
		stock.StockInfo.DolVol = dolVol
		stock.StockInfo.ADR = adr

		if premarketMetrics != nil {
			stock.StockInfo.PremarketVolume = premarketMetrics.CurrentPremarketVol
			stock.StockInfo.AvgPremarketVolume = premarketMetrics.AvgPremarketVol
			stock.StockInfo.PremarketVolumeRatio = premarketMetrics.VolumeRatio
			stock.StockInfo.PremarketVolAsPercent = premarketMetrics.VolAsPercentOfDaily
		}

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

		// Summary of technical checks
		fmt.Printf("\n   📊 TECHNICAL SUMMARY for %s:\n", stock.Symbol)
		fmt.Printf("   ════════════════════════════════════════════════\n")
		fmt.Printf("   💰 Dollar Volume: $%.0fM %s (Min: $%.0fM)\n",
			dolVol/1_000_000, checkmark(dolVol >= MIN_DOLLAR_VOLUME), MIN_DOLLAR_VOLUME/1_000_000)
		fmt.Printf("   📏 ADR: %.2f%% %s (Min: %.2f%%)\n",
			adr, checkmark(adr >= MIN_ADR_PERCENT), MIN_ADR_PERCENT)
		fmt.Printf("   📈 Above 200 EMA: %s\n",
			checkmark(technicalIndicators.IsAbove200EMA))
		fmt.Printf("   📐 Distance from 50 EMA: %.2f ADRs\n", technicalIndicators.DistanceFrom50EMA)
		fmt.Printf("   📊 Extended: %v | Too Extended: %v\n",
			technicalIndicators.IsExtended, technicalIndicators.IsTooExtended)
		if premarketMetrics != nil {
			fmt.Printf("   🔥 Premarket Vol Ratio: %.2fx %s (Min: %.2fx)\n",
				premarketMetrics.VolumeRatio, checkmark(premarketMetrics.VolumeRatio >= MIN_PREMARKET_VOL_RATIO),
				MIN_PREMARKET_VOL_RATIO)
		}
		fmt.Printf("   ════════════════════════════════════════════════\n")

		technicalStocks = append(technicalStocks, stock)
		fmt.Printf("   ✅ %s completed technical analysis\n", stock.Symbol)
	}

	return technicalStocks, nil
}

// Stage 4: Final real-time filter
func realtimeStage4Final(stocks []RealtimeResult) []RealtimeResult {
	var finalStocks []RealtimeResult

	fmt.Printf("\n\n🎯 ========== STAGE 4: FINAL EPISODIC PIVOT CRITERIA ==========\n")
	fmt.Printf("🎯 Applying final filter to %d stocks...\n\n", len(stocks))

	for _, stock := range stocks {
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("🔍 FINAL ANALYSIS FOR: %s (%s)\n", stock.Symbol, stock.StockInfo.Name)
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

		// Check all episodic pivot criteria
		criteria := []struct {
			name    string
			passed  bool
			details string
		}{
			{"Dollar Volume", stock.StockInfo.DolVol >= MIN_DOLLAR_VOLUME,
				fmt.Sprintf("$%.0fM >= $%.0fM", stock.StockInfo.DolVol/1_000_000, MIN_DOLLAR_VOLUME/1_000_000)},
			{"Premarket Volume Ratio", stock.StockInfo.PremarketVolumeRatio >= MIN_PREMARKET_VOL_RATIO,
				fmt.Sprintf("%.2fx >= %.2fx", stock.StockInfo.PremarketVolumeRatio, MIN_PREMARKET_VOL_RATIO)},
			{"Above 200 EMA", stock.StockInfo.IsAbove200EMA,
				fmt.Sprintf("Price above 200 EMA ($%.2f)", stock.StockInfo.EMA200)},
			{"Not Extended", !stock.StockInfo.IsExtended,
				fmt.Sprintf("Distance from 50 EMA: %.2f ADRs (Max: %.2f)", stock.StockInfo.DistanceFrom50EMA, MAX_EXTENSION_ADR)},
		}

		allPassed := true
		passedCount := 0

		fmt.Printf("   Criteria Checklist:\n")
		fmt.Printf("   ═══════════════════════════════════════════\n")
		for i, criterion := range criteria {
			status := "❌ FAIL"
			if criterion.passed {
				status = "✅ PASS"
				passedCount++
			} else {
				allPassed = false
			}
			fmt.Printf("   [%d] %s %s\n", i+1, status, criterion.name)
			fmt.Printf("       → %s\n", criterion.details)
		}
		fmt.Printf("   ═══════════════════════════════════════════\n")
		fmt.Printf("   Passed: %d/4 criteria\n\n", passedCount)

		// Additional check for "too extended"
		if stock.StockInfo.IsTooExtended {
			fmt.Printf("   ⚠️  WARNING: Too Extended (%.2f ADRs > %.2f max)\n\n",
				stock.StockInfo.DistanceFrom50EMA, TOO_EXTENDED_ADR)
		}

		// Print detailed metrics
		fmt.Printf("   📊 Detailed Metrics:\n")
		fmt.Printf("   ═══════════════════════════════════════════\n")
		fmt.Printf("   Gap Up:           %.2f%%\n", stock.StockInfo.GapUp)
		fmt.Printf("   Market Cap:       $%.0fM\n", stock.StockInfo.MarketCap/1_000_000)
		fmt.Printf("   Dollar Volume:    $%.0fM\n", stock.StockInfo.DolVol/1_000_000)
		fmt.Printf("   ADR:              %.2f%%\n", stock.StockInfo.ADR)
		fmt.Printf("   PM Volume:        %.0f shares\n", stock.StockInfo.PremarketVolume)
		fmt.Printf("   PM Vol Ratio:     %.2fx\n", stock.StockInfo.PremarketVolumeRatio)
		fmt.Printf("   SMA200:           $%.2f\n", stock.StockInfo.SMA200)
		fmt.Printf("   EMA200:           $%.2f\n", stock.StockInfo.EMA200)
		fmt.Printf("   EMA50:            $%.2f\n", stock.StockInfo.EMA50)
		fmt.Printf("   Distance 50 EMA:  %.2f ADRs\n", stock.StockInfo.DistanceFrom50EMA)
		fmt.Printf("   Near EMA 10/20:   %v\n", stock.StockInfo.IsNearEMA1020)
		fmt.Printf("   Breaks Resistance: %v\n", stock.StockInfo.BreaksResistance)
		fmt.Printf("   ═══════════════════════════════════════════\n\n")

		if allPassed && !stock.StockInfo.IsTooExtended {
			finalStocks = append(finalStocks, stock)
			fmt.Printf("   ✅✅✅ %s QUALIFIED FOR EPISODIC PIVOT! ✅✅✅\n", stock.Symbol)
			fmt.Printf("   🎉 This stock meets ALL criteria!\n\n")
		} else {
			reason := "Failed one or more criteria"
			if stock.StockInfo.IsTooExtended {
				reason = fmt.Sprintf("Too extended (%.2f ADRs > %.2f max)",
					stock.StockInfo.DistanceFrom50EMA, TOO_EXTENDED_ADR)
			}
			fmt.Printf("   ❌ %s REJECTED: %s\n\n", stock.Symbol, reason)
		}
	}

	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("🎯 FINAL RESULTS: %d stocks qualified out of %d analyzed\n", len(finalStocks), len(stocks))
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	return finalStocks
}

// Helper functions for Alpaca API

// Get tradable symbols from Alpaca
func getAlpacaTradableSymbolsMain(config AlpacaConfig) ([]string, error) {
	url := fmt.Sprintf("%s/v2/assets?status=active&asset_class=us_equity", config.BaseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("APCA-API-KEY-ID", config.APIKey)
	req.Header.Set("APCA-API-SECRET-KEY", config.APISecret)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var assets []struct {
		Symbol   string `json:"symbol"`
		Exchange string `json:"exchange"`
		Tradable bool   `json:"tradable"`
		Status   string `json:"status"`
	}

	if err := json.Unmarshal(body, &assets); err != nil {
		return nil, fmt.Errorf("failed to parse assets: %v", err)
	}

	var symbols []string
	for _, asset := range assets {
		if asset.Tradable && asset.Status == "active" && (asset.Exchange == "NASDAQ" || asset.Exchange == "NYSE") {
			symbols = append(symbols, asset.Symbol)
		}
	}

	return symbols, nil
}

// Get previous day's close from Alpaca
func getAlpacaPreviousClose(config AlpacaConfig, symbol string) (float64, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -5)

	// Use single-symbol endpoint
	url := fmt.Sprintf("%s/v2/stocks/%s/bars?timeframe=1Day&start=%s&end=%s&limit=5&adjustment=raw&feed=sip",
		config.DataURL, symbol, start.Format("2006-01-02"), end.Format("2006-01-02"))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("APCA-API-KEY-ID", config.APIKey)
	req.Header.Set("APCA-API-SECRET-KEY", config.APISecret)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	// Parse single-symbol response
	var barsResp AlpacaSingleSymbolBarsResponse
	if err := json.Unmarshal(body, &barsResp); err != nil {
		return 0, fmt.Errorf("failed to parse response: %v. Body: %s", err, string(body))
	}

	if len(barsResp.Bars) < 2 {
		return 0, fmt.Errorf("insufficient data: only %d bars", len(barsResp.Bars))
	}

	// Return the second-to-last bar's close (previous day)
	return barsResp.Bars[len(barsResp.Bars)-2].Close, nil
}

// Get premarket data from Alpaca
func getAlpacaPremarketData(config AlpacaConfig, symbol string) (*RealtimeStockData, error) {
	est, _ := time.LoadLocation("America/New_York")
	now := time.Now().In(est)

	// Start at 4 AM today
	start := time.Date(now.Year(), now.Month(), now.Day(), 4, 0, 0, 0, est)

	// If it's before 4 AM, use yesterday
	if now.Hour() < 4 {
		start = start.AddDate(0, 0, -1)
	}

	// End at 9:30 AM or current time if before 9:30 AM
	end := time.Date(now.Year(), now.Month(), now.Day(), 9, 30, 0, 0, est)
	if now.Before(end) {
		end = now
	}

	// Format times exactly like the curl example: 2025-01-23T04:00:00-05:00
	startStr := start.Format("2006-01-02T15:04:05-07:00")
	endStr := end.Format("2006-01-02T15:04:05-07:00")

	// Use single-symbol endpoint matching the curl command exactly
	url := fmt.Sprintf("%s/v2/stocks/%s/bars?timeframe=1Min&adjustment=raw&start=%s&end=%s",
		config.DataURL, symbol, startStr, endStr)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// Use exact header names from curl example
	req.Header.Set("Apca-Api-Key-Id", config.APIKey)
	req.Header.Set("Apca-Api-Secret-Key", config.APISecret)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse single-symbol response
	var barsResp AlpacaSingleSymbolBarsResponse
	if err := json.Unmarshal(body, &barsResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v. Body: %s", err, string(body))
	}

	if len(barsResp.Bars) == 0 {
		return nil, fmt.Errorf("no premarket data")
	}

	// Calculate premarket metrics
	pmOpen := barsResp.Bars[0].Open
	pmHigh := barsResp.Bars[0].High
	pmLow := barsResp.Bars[0].Low
	pmClose := barsResp.Bars[len(barsResp.Bars)-1].Close
	pmVolume := 0.0

	for _, bar := range barsResp.Bars {
		if bar.High > pmHigh {
			pmHigh = bar.High
		}
		if bar.Low < pmLow {
			pmLow = bar.Low
		}
		pmVolume += bar.Volume
	}

	return &RealtimeStockData{
		PremarketOpen:   pmOpen,
		PremarketHigh:   pmHigh,
		PremarketLow:    pmLow,
		PremarketClose:  pmClose,
		PremarketVolume: pmVolume,
	}, nil
}

// Get asset info from Alpaca
func getAlpacaAssetInfo(config AlpacaConfig, symbol string) (*struct {
	Name     string `json:"name"`
	Exchange string `json:"exchange"`
}, error) {
	url := fmt.Sprintf("%s/v2/assets/%s", config.BaseURL, symbol)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("APCA-API-KEY-ID", config.APIKey)
	req.Header.Set("APCA-API-SECRET-KEY", config.APISecret)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var asset struct {
		Name     string `json:"name"`
		Exchange string `json:"exchange"`
	}

	if err := json.Unmarshal(body, &asset); err != nil {
		return nil, err
	}

	return &asset, nil
}

// Get historical bars from Alpaca
func getAlpacaHistoricalBars(config AlpacaConfig, symbol string, daysBack int) ([]AlpacaBar, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -(daysBack + 50)) // Extra buffer

	// Use single-symbol endpoint
	url := fmt.Sprintf("%s/v2/stocks/%s/bars?timeframe=1Day&start=%s&end=%s&limit=1000&adjustment=raw&feed=sip",
		config.DataURL, symbol, start.Format("2006-01-02"), end.Format("2006-01-02"))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("APCA-API-KEY-ID", config.APIKey)
	req.Header.Set("APCA-API-SECRET-KEY", config.APISecret)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse single-symbol response
	var barsResp AlpacaSingleSymbolBarsResponse
	if err := json.Unmarshal(body, &barsResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v. Body: %s", err, string(body))
	}

	if len(barsResp.Bars) == 0 {
		return nil, fmt.Errorf("no historical data")
	}

	return barsResp.Bars, nil
}

// Get premarket volume metrics
func getAlpacaPremarketMetrics(config AlpacaConfig, symbol string, avgDolVol float64) (*PremarketAnalysis, error) {
	premarketData, err := getAlpacaPremarketData(config, symbol)
	if err != nil {
		return nil, err
	}

	// Get average daily volume from historical data
	bars, err := getAlpacaHistoricalBars(config, symbol, 21)
	if err != nil {
		return nil, err
	}

	avgDailyVol := 0.0
	for _, bar := range bars {
		avgDailyVol += bar.Volume
	}
	avgDailyVol /= float64(len(bars))

	// Estimate average premarket volume (typically 12% of daily)
	avgPremarketVol := avgDailyVol * 0.12

	volumeRatio := 0.0
	if avgPremarketVol > 0 {
		volumeRatio = premarketData.PremarketVolume / avgPremarketVol
	}

	volAsPercentOfDaily := 0.0
	if avgDailyVol > 0 {
		volAsPercentOfDaily = (premarketData.PremarketVolume / avgDailyVol) * 100
	}

	return &PremarketAnalysis{
		CurrentPremarketVol: premarketData.PremarketVolume,
		AvgPremarketVol:     avgPremarketVol,
		VolumeRatio:         volumeRatio,
		VolAsPercentOfDaily: volAsPercentOfDaily,
	}, nil
}

// Calculate technical indicators for real-time
func calculateTechnicalIndicatorsRealtime(bars []AlpacaBar, symbol string) (*TechnicalIndicators, error) {
	if len(bars) < 200 {
		return nil, fmt.Errorf("insufficient data")
	}

	// Sort by timestamp (oldest first)
	sort.Slice(bars, func(i, j int) bool {
		return bars[i].Timestamp < bars[j].Timestamp
	})

	// Extract close prices
	closes := make([]float64, len(bars))
	for i, bar := range bars {
		closes[i] = bar.Close
	}

	currentPrice := closes[len(closes)-1]

	// Calculate ADR for current period
	currentADR := 0.0
	if len(bars) >= 21 {
		recent := bars[len(bars)-21:]
		adrSum := 0.0
		for _, bar := range recent {
			if bar.Low > 0 {
				adrSum += ((bar.High - bar.Low) / bar.Low) * 100
			}
		}
		currentADR = adrSum / float64(len(recent))
	}

	// Calculate moving averages
	sma200 := calculateSMAFromSlice(closes, 200)
	ema200 := calculateEMAFromSlice(closes, 200)
	ema50 := calculateEMAFromSlice(closes, 50)
	ema20 := calculateEMAFromSlice(closes, 20)
	ema10 := calculateEMAFromSlice(closes, 10)

	// Calculate distance from 50 EMA in ADRs
	distanceFrom50EMA := 0.0
	if currentADR > 0 {
		distanceFrom50EMA = (currentPrice - ema50) / (currentADR * currentPrice / 100)
	}

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
	if len(bars) >= 20 {
		recent20 := bars[len(bars)-20:]
		recentHigh := 0.0
		for i, bar := range recent20 {
			if i < len(recent20)-1 && bar.High > recentHigh {
				recentHigh = bar.High
			}
		}
		currentOpen := bars[len(bars)-1].Open
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
		IsNearEMA1020:     isNearEMA1020,
		BreaksResistance:  breaksResistance,
	}

	return indicators, nil
}

// Calculate historical metrics for real-time
func calculateHistoricalMetricsRealtime(bars []AlpacaBar) (float64, float64, error) {
	if len(bars) < 21 {
		return 0, 0, fmt.Errorf("insufficient data: need at least 21 days")
	}

	// Use last 21 days
	recentBars := bars[len(bars)-21:]

	var dolVolSum float64 = 0
	var adrSum float64 = 0
	validDays := 0

	for _, bar := range recentBars {
		if bar.Close <= 0 || bar.Volume <= 0 || bar.High <= 0 || bar.Low <= 0 {
			continue
		}

		dolVol := bar.Volume * bar.Close
		dolVolSum += dolVol

		dailyRange := ((bar.High - bar.Low) / bar.Low) * 100
		adrSum += dailyRange

		validDays++
	}

	if validDays == 0 {
		return 0, 0, fmt.Errorf("no valid data")
	}

	avgDolVol := dolVolSum / float64(validDays)
	avgADR := adrSum / float64(validDays)

	return avgDolVol, avgADR, nil
}

// Helper: Calculate SMA from slice
func calculateSMAFromSlice(data []float64, period int) float64 {
	if len(data) < period {
		return 0
	}

	sum := 0.0
	for i := len(data) - period; i < len(data); i++ {
		sum += data[i]
	}
	return sum / float64(period)
}

// Helper: Calculate EMA from slice
func calculateEMAFromSlice(data []float64, period int) float64 {
	if len(data) < period {
		return 0
	}

	multiplier := 2.0 / float64(period+1)

	// Start with SMA
	ema := calculateSMAFromSlice(data[:period], period)

	// Calculate EMA for remaining data
	for i := period; i < len(data); i++ {
		ema = (data[i]-ema)*multiplier + ema
	}

	return ema
}

// Estimate market cap from real-time data
func estimateMarketCapFromRealtime(stock RealtimeStockData) float64 {
	// Conservative estimation based on price and volume
	// This is a rough estimate - ideally you'd fetch actual shares outstanding

	currentPrice := stock.PremarketClose
	if currentPrice <= 0 {
		currentPrice = stock.PreviousClose
	}

	volume := stock.PremarketVolume
	if volume <= 0 {
		volume = 1000000 // Default assumption
	}

	// Estimate shares outstanding from volume patterns
	// Typically daily volume is 0.5-2% of shares outstanding
	estimatedShares := volume * 100 // Assume 1% turnover

	return currentPrice * estimatedShares
}

// Get market status based on current time
func getMarketStatus(t time.Time) string {
	hour := t.Hour()
	minute := t.Minute()

	// Market hours in EST
	if hour < 4 {
		return "CLOSED"
	} else if hour < 9 || (hour == 9 && minute < 30) {
		return "PREMARKET"
	} else if hour < 16 {
		return "OPEN"
	} else if hour < 20 {
		return "AFTERHOURS"
	}
	return "CLOSED"
}

// Output real-time results
func outputRealtimeResults(stocks []RealtimeResult, scanTime time.Time) error {
	output := map[string]interface{}{
		"scan_time":     scanTime.Format(time.RFC3339),
		"market_status": getMarketStatus(scanTime),
		"filter_criteria": map[string]interface{}{
			"min_gap_up_percent":         MIN_GAP_UP_PERCENT,
			"min_dollar_volume":          MIN_DOLLAR_VOLUME,
			"min_market_cap":             MIN_MARKET_CAP,
			"min_premarket_volume_ratio": MIN_PREMARKET_VOL_RATIO,
			"max_extension_adr":          MAX_EXTENSION_ADR,
		},
		"qualifying_stocks": stocks,
		"summary": map[string]interface{}{
			"total_candidates": len(stocks),
			"avg_gap_up":       calculateAvgGapUpRealtime(stocks),
		},
	}

	jsonData, err := json.MarshalIndent(output, "", "  ")
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

	// Also create CSV summary
	err = outputRealtimeCSVSummary(stocks, stockDir)
	if err != nil {
		fmt.Printf("Warning: Could not create CSV summary: %v\n", err)
	}

	return nil
}

// Output CSV summary for real-time scan
func outputRealtimeCSVSummary(stocks []RealtimeResult, dir string) error {
	filename := filepath.Join(dir, "filtered_stocks_marketstack_summary.csv")
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write comprehensive header with all metrics
	headers := []string{
		"Symbol", "Name", "Exchange", "Sector", "Industry",
		"Gap Up %", "Market Cap", "Dollar Volume", "ADR %",
		"Premarket Volume", "Avg Premarket Volume", "Premarket Vol Ratio", "Premarket Vol as % of Daily",
		"SMA200", "EMA200", "EMA50", "EMA20", "EMA10",
		"Above 200 EMA", "Distance from 50 EMA (ADRs)", "Is Extended", "Is Too Extended",
		"Is Near EMA 10/20", "Breaks Resistance",
		"Data Quality", "Validation Notes",
	}
	writer.Write(headers)

	// Write data with all metrics
	for _, stock := range stocks {
		validationNotes := strings.Join(stock.ValidationNotes, "; ")
		if validationNotes == "" {
			validationNotes = "None"
		}

		row := []string{
			stock.Symbol,
			stock.StockInfo.Name,
			stock.StockInfo.Exchange,
			stock.StockInfo.Sector,
			stock.StockInfo.Industry,
			fmt.Sprintf("%.2f", stock.StockInfo.GapUp),
			fmt.Sprintf("%.0f", stock.StockInfo.MarketCap),
			fmt.Sprintf("%.0f", stock.StockInfo.DolVol),
			fmt.Sprintf("%.2f", stock.StockInfo.ADR),
			fmt.Sprintf("%.0f", stock.StockInfo.PremarketVolume),
			fmt.Sprintf("%.0f", stock.StockInfo.AvgPremarketVolume),
			fmt.Sprintf("%.2f", stock.StockInfo.PremarketVolumeRatio),
			fmt.Sprintf("%.2f", stock.StockInfo.PremarketVolAsPercent),
			fmt.Sprintf("%.2f", stock.StockInfo.SMA200),
			fmt.Sprintf("%.2f", stock.StockInfo.EMA200),
			fmt.Sprintf("%.2f", stock.StockInfo.EMA50),
			fmt.Sprintf("%.2f", stock.StockInfo.EMA20),
			fmt.Sprintf("%.2f", stock.StockInfo.EMA10),
			boolToString(stock.StockInfo.IsAbove200EMA),
			fmt.Sprintf("%.2f", stock.StockInfo.DistanceFrom50EMA),
			boolToString(stock.StockInfo.IsExtended),
			boolToString(stock.StockInfo.IsTooExtended),
			boolToString(stock.StockInfo.IsNearEMA1020),
			boolToString(stock.StockInfo.BreaksResistance),
			stock.DataQuality,
			validationNotes,
		}
		writer.Write(row)
	}

	fmt.Printf("CSV summary written to %s\n", filename)
	return nil
}

// Helper functions
func calculateAvgGapUpRealtime(stocks []RealtimeResult) float64 {
	if len(stocks) == 0 {
		return 0
	}

	sum := 0.0
	for _, stock := range stocks {
		sum += stock.StockInfo.GapUp
	}
	return sum / float64(len(stocks))
}

// Convenience function to run real-time scanner
func RunEpisodicPivotRealtimeScanner(apiKey, apiSecret string, isPaper bool) error {
	baseURL := "https://api.alpaca.markets"
	if isPaper {
		baseURL = "https://paper-api.alpaca.markets"
	}

	config := AlpacaConfig{
		APIKey:    apiKey,
		APISecret: apiSecret,
		BaseURL:   baseURL,
		DataURL:   "https://data.alpaca.markets",
		IsPaper:   isPaper,
	}

	return FilterStocksEpisodicPivotRealtime(config)
}

// Run scanner continuously with specified interval
func RunContinuousScanner(apiKey, apiSecret string, isPaper bool, intervalMinutes int) error {
	fmt.Printf("🔄 Starting continuous scanner (every %d minutes)...\n", intervalMinutes)

	ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
	defer ticker.Stop()

	// Run once immediately
	err := RunEpisodicPivotRealtimeScanner(apiKey, apiSecret, isPaper)
	if err != nil {
		fmt.Printf("❌ Error in initial scan: %v\n", err)
	}

	// Then run on interval
	for range ticker.C {
		est, _ := time.LoadLocation("America/New_York")
		now := time.Now().In(est)

		// Only run during premarket hours (4 AM - 9:30 AM EST)
		hour := now.Hour()
		if hour >= 4 && hour < 10 {
			fmt.Printf("\n⏰ Running scheduled scan at %s EST\n", now.Format("15:04:05"))
			err := RunEpisodicPivotRealtimeScanner(apiKey, apiSecret, isPaper)
			if err != nil {
				fmt.Printf("❌ Error in scan: %v\n", err)
			}
		} else {
			fmt.Printf("⏸️  Outside premarket hours, skipping scan\n")
		}
	}

	return nil
}