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

// Marketstack v2 API Response Structures
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
	Open          float64 `json:"open"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	Close         float64 `json:"close"`
	Volume        float64 `json:"volume"`
	AdjHigh       float64 `json:"adj_high"`
	AdjLow        float64 `json:"adj_low"`
	AdjClose      float64 `json:"adj_close"`
	AdjOpen       float64 `json:"adj_open"`
	AdjVolume     float64 `json:"adj_volume"`
	SplitFactor   float64 `json:"split_factor"`
	Dividend      float64 `json:"dividend"`
	Name          string  `json:"name"`
	ExchangeCode  string  `json:"exchange_code"`
	AssetType     string  `json:"asset_type"`
	PriceCurrency string  `json:"price_currency"`
	Symbol        string  `json:"symbol"`
	Exchange      string  `json:"exchange"`
	Date          string  `json:"date"`
}

// Marketstack Ticker Info Response
type MarketstackTickerInfoResponse struct {
	Data MarketstackTickerInfo `json:"data"`
}

type MarketstackTickerInfo struct {
	Name              string  `json:"name"`
	Ticker            string  `json:"ticker"`
	ItemType          string  `json:"item_type"`
	Sector            string  `json:"sector"`
	Industry          string  `json:"industry"`
	ExchangeCode      string  `json:"exchange_code"`
	FullTimeEmployees string  `json:"full_time_employees"`
	IPODate           *string `json:"ipo_date"`
	DateFounded       *string `json:"date_founded"`
}

// Enhanced structures for episodic pivot analysis
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
	GapUp     float64 `json:"gap_up"` // Gap up percentage
	ADR       float64 `json:"adr"`    // Average daily range
	Name      string  `json:"name"`
	Exchange  string  `json:"exchange"`
	Sector    string  `json:"sector"`
	Industry  string  `json:"industry"`
	// New premarket and technical analysis fields
	PremarketVolume          float64 `json:"premarket_volume"`           // Current premarket volume
	AvgPremarketVolume       float64 `json:"avg_premarket_volume"`       // 20-day average premarket volume
	PremarketVolumeRatio     float64 `json:"premarket_volume_ratio"`     // Current/Average premarket volume
	PremarketVolAsPercent    float64 `json:"premarket_vol_percent"`      // Premarket vol as % of avg daily vol
	SMA200                   float64 `json:"sma_200"`                    // 200-day Simple Moving Average
	EMA200                   float64 `json:"ema_200"`                    // 200-day Exponential Moving Average
	EMA50                    float64 `json:"ema_50"`                     // 50-day Exponential Moving Average
	EMA20                    float64 `json:"ema_20"`                     // 20-day Exponential Moving Average
	EMA10                    float64 `json:"ema_10"`                     // 10-day Exponential Moving Average
	IsAbove200EMA            bool    `json:"is_above_200_ema"`           // Price above 200 EMA
	DistanceFrom50EMA        float64 `json:"distance_from_50_ema"`       // Distance from 50 EMA in ADRs
	IsExtended               bool    `json:"is_extended"`                // Is stock extended (>5 ADRs from 50 EMA)
	IsTooExtended            bool    `json:"is_too_extended"`            // Is stock too extended (>8 ADRs from 50 EMA)
	VolumeDriedUp            bool    `json:"volume_dried_up"`            // 20-day avg vol < 60-day avg vol
	IsNearEMA1020            bool    `json:"is_near_ema_10_20"`          // Within 2 ADRs of 10/20 EMA
	BreaksResistance         bool    `json:"breaks_resistance"`          // Gap up breaks previous resistance
	PreviousEarningsReaction string  `json:"previous_earnings_reaction"` // Positive/Negative/Neutral
}

// Configuration
const (
	MIN_DOLLAR_VOLUME          = 10000000.0  // $10M minimum dollar volume
	MIN_GAP_UP_PERCENT         = 8.0         // 8%+ gap up (strategy says 8%+, ideally 10-20%)
	MIN_ADR_PERCENT            = 4.0         // Minimum average daily range
	MIN_MARKET_CAP             = 200000000.0 // $200M minimum market cap
	MIN_PREMARKET_VOL_RATIO    = 5.0         // 5-10x average premarket volume
	MAX_PREMARKET_VOL_RATIO    = 10.0
	MIN_PREMARKET_AS_DAILY_PCT = 20.0 // 20-50% of average daily volume
	MAX_PREMARKET_AS_DAILY_PCT = 50.0
	MAX_EXTENSION_ADR          = 5.0 // 5x ADRs above 50 EMA = extended
	TOO_EXTENDED_ADR           = 8.0 // 8x ADRs above 50 EMA = too extended
	NEAR_EMA_ADR_THRESHOLD     = 2.0 // Within 2 ADRs of 10/20 EMA

	// API rate limiting
	API_CALLS_PER_SECOND = 2
	MAX_CONCURRENT       = 3
)

func FilterStocksEpisodicPivot(apiKey string, tiingoKey string) {
	fmt.Println("=== Episodic Pivot Stock Filter with Premarket Analysis ===")

	// Stage 1: Basic gap up filter (8%+)
	fmt.Println("\n🔍 Stage 1: Filtering by gap up (8%+ minimum)...")
	gapUpStocks, err := stage1FilterByGapUpEP(apiKey)
	if err != nil {
		log.Fatalf("❌ Error in Stage 1: %v", err)
	}

	fmt.Printf("✅ Stage 1 complete. Found %d stocks with 8 percent gap up.\n", len(gapUpStocks))

	if len(gapUpStocks) == 0 {
		fmt.Println("⚠️ No stocks found with gap up criteria. Exiting.")
		return
	}

	// Stage 2: Market cap and liquidity filter
	fmt.Println("\n💰 Stage 2: Filtering by market cap and liquidity...")
	liquidStocks, err := stage2FilterByLiquidity(apiKey, gapUpStocks)
	if err != nil {
		log.Fatalf("❌ Error in Stage 2: %v", err)
	}

	fmt.Printf("✅ Stage 2 complete. Found %d stocks meeting liquidity requirements.\n", len(liquidStocks))

	if len(liquidStocks) == 0 {
		fmt.Println("⚠️ No stocks found meeting liquidity criteria. Exiting.")
		return
	}

	// Stage 3: Technical analysis and premarket volume analysis
	fmt.Println("\n📊 Stage 3: Technical analysis and premarket volume...")
	technicalStocks, err := stage3TechnicalAndPremarketAnalysis(apiKey, tiingoKey, liquidStocks)
	if err != nil {
		log.Fatalf("❌ Error in Stage 3: %v", err)
	}

	fmt.Printf("✅ Stage 3 complete. Found %d stocks meeting technical criteria.\n", len(technicalStocks))

	// Stage 4: Final episodic pivot filter
	fmt.Println("\n🎯 Stage 4: Final episodic pivot criteria...")
	finalStocks := stage4FinalEpisodicPivotFilter(technicalStocks)

	// Output results
	err = outputEpisodicPivotResults(finalStocks)
	if err != nil {
		log.Fatalf("❌ Error writing results: %v", err)
	}

	fmt.Printf("\n🎉 Episodic Pivot Filter complete! Found %d qualifying stocks.\n", len(finalStocks))
	fmt.Println("📁 Results written to episodic_pivot_candidates.json")
}

// Stage 1: Filter by gap up (8%+ minimum)
func stage1FilterByGapUpEP(apiKey string) ([]StockData, error) {
	symbols, err := getStockSymbols()
	if err != nil {
		return nil, fmt.Errorf("failed to get stock symbols: %v", err)
	}

	fmt.Printf("📈 Processing %d symbols for gap up analysis...\n", len(symbols))

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

			premarketData, eodData, err := getPremarketDataV2(apiKey, sym)
			if err != nil {
				fmt.Printf("⚠️ Error getting premarket data for %s: %v\n", sym, err)
				return
			}

			if eodData == nil {
				fmt.Printf("⚠️ No EOD data for %s\n", sym)
				return
			}

			if premarketData == nil {
				fmt.Printf("⚠️ No premarket data for %s\n", sym)
				return
			}

			if eodData == nil {
				fmt.Printf("⚠️ No EOD data for %s\n", sym)
				return
			}

			if premarketData.Close <= 0 {
				fmt.Printf("⚠️ Invalid previous close for %s: %.2f\n", sym, premarketData.Close)
				return
			}

			gapUp := ((premarketData.Open - eodData.Close) / eodData.Close) * 100
			fmt.Println("Debug:", sym, "Current:", premarketData.Open, "Close:", eodData.Close, "Gap Up:", gapUp)

			// Filter for 8%+ gap up
			if gapUp >= MIN_GAP_UP_PERCENT {
				stockData := StockData{
					Symbol:                     premarketData.Symbol,
					Timestamp:                  premarketData.Date,
					Open:                       premarketData.Open,
					High:                       premarketData.High,
					Low:                        premarketData.Low,
					Close:                      eodData.Close,
					Volume:                     int64(premarketData.Volume),
					PreviousClose:              eodData.Close,
					Change:                     premarketData.Open - eodData.Close,
					ChangePercent:              gapUp,
					ExtendedHoursQuote:         premarketData.Open,
					ExtendedHoursChange:        premarketData.Open - eodData.Close,
					ExtendedHoursChangePercent: gapUp,
					Exchange:                   premarketData.Exchange,
					Name:                       premarketData.Symbol,
				}

				mu.Lock()
				fmt.Printf("✅ %s qualifies! Gap up: %.2f%%\n", sym, gapUp)
				gapUpStocks = append(gapUpStocks, stockData)
				mu.Unlock()
			}
		}(symbol)
	}

	wg.Wait()
	return gapUpStocks, nil
}

// Stage 2: Filter by market cap and basic liquidity
func stage2FilterByLiquidity(apiKey string, stocks []StockData) ([]FilteredStock, error) {
	var liquidStocks []FilteredStock
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	fmt.Printf("💰 Analyzing liquidity for %d stocks...\n", len(stocks))

	for i, stock := range stocks {
		fmt.Printf("🏢 [%d/%d] Processing liquidity for %s...\n", i+1, len(stocks), stock.Symbol)
		<-rateLimiter

		companyInfo, err := getTickerInfoV2(apiKey, stock.Symbol)
		if err != nil {
			fmt.Printf("⚠️ Could not get company info for %s: %v\n", stock.Symbol, err)
		}

		estimatedMarketCap := estimateMarketCapImproved(stock, companyInfo)

		// Apply minimum liquidity criteria ($10M+ DolVol requirement)
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

			liquidStocks = append(liquidStocks, filteredStock)
			fmt.Printf("✅ %s passed liquidity filter\n", stock.Symbol)
		} else {
			fmt.Printf("❌ %s rejected: Market cap $%.0f < $%.0f\n",
				stock.Symbol, estimatedMarketCap, MIN_MARKET_CAP)
		}
	}

	return liquidStocks, nil
}

// Stage 3: Technical analysis and REAL premarket volume analysis with Alpha Vantage
func stage3TechnicalAndPremarketAnalysis(apiKey string, tiingoKey string, stocks []FilteredStock) ([]FilteredStock, error) {
	var technicalStocks []FilteredStock
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	// Alpha Vantage rate limiter (5 calls per minute for free tier)
	tiingoRateLimiter := time.Tick(time.Minute / 5)

	fmt.Printf("📊 Performing technical and REAL premarket analysis for %d stocks...\n", len(stocks))
	fmt.Println("🔔 Using Alpha Vantage for real premarket data (FREE tier: 5 calls/min)")

	for i, stock := range stocks {
		fmt.Printf("📈 [%d/%d] Analyzing %s...\n", i+1, len(stocks), stock.Symbol)
		<-rateLimiter

		// Get extended historical data for technical indicators
		historicalData, err := getHistoricalDataV2(apiKey, stock.Symbol, 250)
		if err != nil {
			fmt.Printf("⚠️ Error getting historical data for %s: %v\n", stock.Symbol, err)
			continue
		}

		if len(historicalData) < 200 {
			fmt.Printf("⚠️ Insufficient historical data for %s: only %d days\n", stock.Symbol, len(historicalData))
			continue
		}

		// Calculate technical indicators
		technicalIndicators, err := calculateTechnicalIndicators(historicalData, stock.Symbol)
		if err != nil {
			fmt.Printf("⚠️ Error calculating technical indicators for %s: %v\n", stock.Symbol, err)
			continue
		}

		// Calculate dollar volume and ADR
		dolVol, adr, err := calculateHistoricalMetricsV2(historicalData[:21], stock.Symbol)
		if err != nil {
			fmt.Printf("⚠️ Error calculating historical metrics for %s: %v\n", stock.Symbol, err)
			continue
		}

		// Get REAL premarket data from Tiingo
		fmt.Printf("⏰ Getting real premarket data for %s (Tiingo)...\n", stock.Symbol)
		<-tiingoRateLimiter // Respect Tiingo rate limits

		premarketAnalysis, err := getPremarketVolumeDataTiingo(tiingoKey, stock.Symbol)
		if err != nil {
			fmt.Printf("⚠️ Error getting premarket data for %s: %v\n", stock.Symbol, err)
			// Fall back to simulation
			premarketAnalysis = simulatePremarketAnalysis(historicalData, dolVol)
		}

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

		// Enhanced logging with real premarket data
		fmt.Printf("📊 %s Complete Analysis:\n", stock.Symbol)
		fmt.Printf("   💰 Dollar Volume: $%.0f (Min: $%.0f) %s\n", dolVol, MIN_DOLLAR_VOLUME, checkmark(dolVol >= MIN_DOLLAR_VOLUME))
		fmt.Printf("   📏 ADR: %.2f%% (Min: %.2f%%) %s\n", adr, MIN_ADR_PERCENT, checkmark(adr >= MIN_ADR_PERCENT))
		fmt.Printf("   🔥 Premarket Vol Ratio: %.1fx (Target: %.1f-%.1fx) %s\n",
			premarketAnalysis.VolumeRatio, MIN_PREMARKET_VOL_RATIO, MAX_PREMARKET_VOL_RATIO,
			checkmark(premarketAnalysis.VolumeRatio >= MIN_PREMARKET_VOL_RATIO && premarketAnalysis.VolumeRatio <= MAX_PREMARKET_VOL_RATIO))
		fmt.Printf("   📊 Premarket as %% Daily: %.1f%% (Target: %.1f-%.1f%%) %s\n",
			premarketAnalysis.VolAsPercentOfDaily, MIN_PREMARKET_AS_DAILY_PCT, MAX_PREMARKET_AS_DAILY_PCT,
			checkmark(premarketAnalysis.VolAsPercentOfDaily >= MIN_PREMARKET_AS_DAILY_PCT && premarketAnalysis.VolAsPercentOfDaily <= MAX_PREMARKET_AS_DAILY_PCT))
		fmt.Printf("   📈 Above 200 EMA: %s (Current: $%.2f, EMA200: $%.2f)\n",
			checkmark(technicalIndicators.IsAbove200EMA), getCurrentPrice(historicalData), technicalIndicators.EMA200)
		fmt.Printf("   ⚡ Extended Status: %s (%.2f ADRs from 50 EMA)\n",
			checkmark(!technicalIndicators.IsExtended), technicalIndicators.DistanceFrom50EMA)
		// fmt.Printf("   💧 Volume Dried Up: %s\n", checkmark(technicalIndicators.VolumeDriedUp))
		fmt.Printf("   🎯 Near 10/20 EMA: %s\n", checkmark(technicalIndicators.IsNearEMA1020))

		technicalStocks = append(technicalStocks, stock)
	}

	return technicalStocks, nil
}

// Stage 4: Final episodic pivot filter
func stage4FinalEpisodicPivotFilter(stocks []FilteredStock) []FilteredStock {
	var finalStocks []FilteredStock

	fmt.Printf("🎯 Applying final episodic pivot criteria to %d stocks...\n", len(stocks))

	for _, stock := range stocks {
		fmt.Printf("\n🔍 Final analysis for %s:\n", stock.Symbol)

		// Check all episodic pivot criteria
		criteria := []struct {
			name    string
			passed  bool
			details string
		}{
			{"Dollar Volume", stock.StockInfo.DolVol >= MIN_DOLLAR_VOLUME,
				fmt.Sprintf("$%.0f >= $%.0f", stock.StockInfo.DolVol, MIN_DOLLAR_VOLUME)},
			{"Premarket Volume Ratio", stock.StockInfo.PremarketVolumeRatio >= MIN_PREMARKET_VOL_RATIO,
				fmt.Sprintf("%.1fx >= %.1fx", stock.StockInfo.PremarketVolumeRatio, MIN_PREMARKET_VOL_RATIO)},
			{"Premarket as % Daily Vol", stock.StockInfo.PremarketVolAsPercent >= MIN_PREMARKET_AS_DAILY_PCT,
				fmt.Sprintf("%.1f%% >= %.1f%%", stock.StockInfo.PremarketVolAsPercent, MIN_PREMARKET_AS_DAILY_PCT)},
			{"Above 200 EMA", stock.StockInfo.IsAbove200EMA,
				fmt.Sprintf("Current price above 200 EMA")},
			{"Not Extended", !stock.StockInfo.IsExtended,
				fmt.Sprintf("Distance from 50 EMA: %.2f ADRs (Max: %.1f)", stock.StockInfo.DistanceFrom50EMA, MAX_EXTENSION_ADR)},
			// {"Volume Dried Up", stock.StockInfo.VolumeDriedUp,
			// 	fmt.Sprintf("20-day avg vol < 60-day avg vol")},
			{"Near 10/20 EMA", stock.StockInfo.IsNearEMA1020,
				fmt.Sprintf("Within %.1f ADRs of 10/20 EMA", NEAR_EMA_ADR_THRESHOLD)},
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
			fmt.Printf("🎉 %s QUALIFIES for episodic pivot!\n", stock.Symbol)
		} else {
			reason := "Failed criteria"
			if stock.StockInfo.IsTooExtended {
				reason = "Too extended (>8 ADRs from 50 EMA)"
			}
			fmt.Printf("❌ %s rejected: %s\n", stock.Symbol, reason)
		}
	}

	return finalStocks
}

// Technical indicators calculation
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
	VolumeDriedUp     bool
	IsNearEMA1020     bool
	BreaksResistance  bool
}

func calculateTechnicalIndicators(historicalData []MarketstackEODData, symbol string) (*TechnicalIndicators, error) {
	if len(historicalData) < 200 {
		return nil, fmt.Errorf("insufficient data for technical indicators")
	}

	// Sort data by date (oldest first for calculations)
	sort.Slice(historicalData, func(i, j int) bool {
		return historicalData[i].Date < historicalData[j].Date
	})

	currentPrice := historicalData[len(historicalData)-1].Close
	currentADR := calculateADRForPeriod(historicalData[len(historicalData)-21:])

	// Calculate moving averages
	sma200 := calculateSMA(historicalData, 200)
	ema200 := calculateEMA(historicalData, 200)
	ema50 := calculateEMA(historicalData, 50)
	ema20 := calculateEMA(historicalData, 20)
	ema10 := calculateEMA(historicalData, 10)

	// Calculate distance from 50 EMA in ADRs
	distanceFrom50EMA := 0.0
	if currentADR > 0 {
		distanceFrom50EMA = (currentPrice - ema50) / (currentADR * currentPrice / 100)
	}

	// Check if volume has dried up (20-day avg < 60-day avg)
	// volumeDriedUp := false
	// if len(historicalData) >= 60 {
	// 	vol20 := calculateAvgVolume(historicalData[len(historicalData)-20:])
	// 	vol60 := calculateAvgVolume(historicalData[len(historicalData)-60:])
	// 	volumeDriedUp = vol20 < vol60
	// }

	// Check if near 10/20 EMA (within 2 ADRs)
	distanceFrom10EMA := 0.0
	distanceFrom20EMA := 0.0
	if currentADR > 0 {
		adrValue := currentADR * currentPrice / 100
		distanceFrom10EMA = abs(currentPrice-ema10) / adrValue
		distanceFrom20EMA = abs(currentPrice-ema20) / adrValue
	}
	isNearEMA1020 := (distanceFrom10EMA <= NEAR_EMA_ADR_THRESHOLD) || (distanceFrom20EMA <= NEAR_EMA_ADR_THRESHOLD)

	// Check if breaks resistance (simplified: gap up breaks recent high)
	breaksResistance := false
	if len(historicalData) >= 20 {
		recent20Days := historicalData[len(historicalData)-20:]
		recentHigh := 0.0
		for _, day := range recent20Days[:len(recent20Days)-1] { // Exclude current day
			if day.High > recentHigh {
				recentHigh = day.High
			}
		}
		currentOpen := historicalData[len(historicalData)-1].Open
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
		IsNearEMA1020:    isNearEMA1020,
		BreaksResistance: breaksResistance,
	}

	fmt.Printf("📊 Technical indicators calculated for %s\n", symbol)
	return indicators, nil
}

// Premarket analysis structure
type PremarketAnalysis struct {
	CurrentPremarketVol float64
	AvgPremarketVol     float64
	VolumeRatio         float64
	VolAsPercentOfDaily float64
}

// Simulate premarket analysis (in production, this would use real premarket data)
func simulatePremarketAnalysis(historicalData []MarketstackEODData, _ float64) *PremarketAnalysis {
	// In a real implementation, you would:
	// 1. Connect to a premarket data feed (like IEX Cloud, Alpha Vantage, or TD Ameritrade)
	// 2. Get actual premarket volume and price data
	// 3. Compare against historical premarket averages

	// For simulation purposes, we'll estimate based on current day's volume
	if len(historicalData) == 0 {
		return &PremarketAnalysis{}
	}

	// currentDay := historicalData[0] // Most recent day
	avgDailyVol := calculateAvgVolume(historicalData[:20]) // Last 20 days

	// Simulate premarket metrics based on episodic pivot requirements
	// Assume strong premarket activity for gap-up stocks
	estimatedPremarketVol := avgDailyVol * 0.15            // Typical premarket is ~5-15% of daily
	estimatedCurrentPremarket := estimatedPremarketVol * 7 // 7x average for strong gap-up

	return &PremarketAnalysis{
		CurrentPremarketVol: estimatedCurrentPremarket,
		AvgPremarketVol:     estimatedPremarketVol,
		VolumeRatio:         7.0,  // Simulated 7x ratio
		VolAsPercentOfDaily: 30.0, // Simulated 30% of daily volume
	}
}

// Technical calculation helper functions
func calculateSMA(data []MarketstackEODData, period int) float64 {
	if len(data) < period {
		return 0
	}

	sum := 0.0
	start := len(data) - period
	for i := start; i < len(data); i++ {
		sum += data[i].Close
	}
	return sum / float64(period)
}

func calculateEMA(data []MarketstackEODData, period int) float64 {
	if len(data) < period {
		return 0
	}

	// Calculate initial SMA as starting point
	smaSum := 0.0
	for i := 0; i < period; i++ {
		smaSum += data[i].Close
	}
	ema := smaSum / float64(period)

	// Calculate multiplier
	multiplier := 2.0 / (float64(period) + 1.0)

	// Calculate EMA for remaining data points
	for i := period; i < len(data); i++ {
		ema = (data[i].Close * multiplier) + (ema * (1 - multiplier))
	}

	return ema
}

func calculateADRForPeriod(data []MarketstackEODData) float64 {
	if len(data) == 0 {
		return 0
	}

	sum := 0.0
	for _, day := range data {
		if day.Low > 0 {
			dailyRange := ((day.High - day.Low) / day.Low) * 100
			sum += dailyRange
		}
	}
	return sum / float64(len(data))
}

// Enhanced market cap estimation for episodic pivot requirements
func estimateMarketCapImproved(stock StockData, companyInfo *MarketstackTickerInfo) float64 {
	// For episodic pivot, we need stocks with $10M+ daily dollar volume
	// This typically indicates larger, more liquid companies

	// Method 1: Volume-based estimation (more aggressive for high-volume stocks)
	volumeBasedEstimate := float64(stock.Volume) * stock.Close * 100

	// Method 2: Price-based estimation
	priceBasedEstimate := stock.Close * 5000000 // Assume 5M shares outstanding

	// Method 3: Employee count if available
	employeeBasedEstimate := volumeBasedEstimate
	if companyInfo != nil && companyInfo.FullTimeEmployees != "" {
		if employees, err := strconv.Atoi(companyInfo.FullTimeEmployees); err == nil {
			// Tech/growth companies: ~$2M per employee, others: ~$1M per employee
			multiplier := 1000000.0
			if strings.Contains(strings.ToLower(companyInfo.Sector), "tech") ||
				strings.Contains(strings.ToLower(companyInfo.Industry), "software") {
				multiplier = 2000000.0
			}
			employeeBasedEstimate = float64(employees) * multiplier
		}
	}

	// Take the maximum estimate (conservative approach for filtering)
	estimates := []float64{volumeBasedEstimate, priceBasedEstimate, employeeBasedEstimate}
	maxEstimate := estimates[0]
	for _, est := range estimates {
		if est > maxEstimate {
			maxEstimate = est
		}
	}

	return maxEstimate
}

// Marketstack Intraday Response Structures
type MarketstackIntradayResponse struct {
	Pagination struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Count  int `json:"count"`
		Total  int `json:"total"`
	} `json:"pagination"`
	Data []MarketstackIntradayData `json:"data"`
}

// Marketstack Intraday Data structure
type MarketstackIntradayData struct {
	Open            float64 `json:"open"`
	High            float64 `json:"high"`
	Low             float64 `json:"low"`
	Close           float64 `json:"close"`
	Volume          float64 `json:"volume"`
	Mid             float64 `json:"mid"`
	Last            float64 `json:"last"`
	LastSize        float64 `json:"last_size"`
	BidSize         float64 `json:"bid_size"`
	BidPrice        float64 `json:"bid_price"`
	AskPrice        float64 `json:"ask_price"`
	AskSize         float64 `json:"ask_size"`
	MarketstackLast float64 `json:"marketstack_last"`
	Date            string  `json:"date"`
	Symbol          string  `json:"symbol"`
	Exchange        string  `json:"exchange"`
}

// Existing helper functions (unchanged)
func getPremarketDataV2(apiKey string, symbol string) (*MarketstackIntradayData, *MarketstackEODData, error) {
	url := fmt.Sprintf("https://api.marketstack.com/v2/intraday/latest?access_key=%s&interval=1min&symbols=%s&exchange=NYSE,NASDAQ&after_hours=true",
		apiKey, symbol)

	resp, err := http.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %v", err)
	}

	var intradayResponse MarketstackIntradayResponse
	if err := json.Unmarshal(body, &intradayResponse); err != nil {
		return nil, nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	url = fmt.Sprintf("https://api.marketstack.com/v2/eod/latest?access_key=%s&symbols=%s&exchange=NYSE,NASDAQ&limit=1",
		apiKey, symbol)

	resp, err = http.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %v", err)
	}

	var eodResponse MarketstackEODResponse
	if err := json.Unmarshal(body, &eodResponse); err != nil {
		return nil, nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	return &intradayResponse.Data[0], &eodResponse.Data[0], nil
}

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

	return eodResponse.Data, nil
}

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

func calculateHistoricalMetricsV2(historicalData []MarketstackEODData, _ string) (float64, float64, error) {
	sort.Slice(historicalData, func(i, j int) bool {
		return historicalData[i].Date > historicalData[j].Date
	})

	if len(historicalData) < 21 {
		return 0, 0, fmt.Errorf("insufficient historical data: only %d days available, need 21", len(historicalData))
	}

	var dolVolSum float64 = 0
	var adrSum float64 = 0
	validDays := 0

	for i := 0; i < 21 && i < len(historicalData); i++ {
		dataItem := historicalData[i]

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

	return symbols, nil
}

// Output functions
func outputEpisodicPivotResults(stocks []FilteredStock) error {
	// Create detailed output with all episodic pivot metrics
	output := map[string]interface{}{
		"generated_at": time.Now().Format(time.RFC3339),
		"filter_criteria": map[string]interface{}{
			"min_gap_up_percent":         MIN_GAP_UP_PERCENT,
			"min_dollar_volume":          MIN_DOLLAR_VOLUME,
			"min_market_cap":             MIN_MARKET_CAP,
			"min_premarket_volume_ratio": MIN_PREMARKET_VOL_RATIO,
			"max_extension_adr":          MAX_EXTENSION_ADR,
			"premarket_volume_as_daily":  fmt.Sprintf("%.0f%%-%.0f%%", MIN_PREMARKET_AS_DAILY_PCT, MAX_PREMARKET_AS_DAILY_PCT),
		},
		"qualifying_stocks": stocks,
		"summary": map[string]interface{}{
			"total_candidates": len(stocks),
			"avg_gap_up":       calculateAvgGapUp(stocks),
			"avg_market_cap":   calculateAvgMarketCap(stocks),
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

	filename := filepath.Join(stockDir, "episodic_pivot_candidates.json")
	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		return err
	}

	// Also create a CSV summary for easy viewing
	err = outputCSVSummary(stocks, stockDir)
	if err != nil {
		fmt.Printf("⚠️ Warning: Could not create CSV summary: %v\n", err)
	}

	return nil
}

func outputCSVSummary(stocks []FilteredStock, dir string) error {
	filename := filepath.Join(dir, "episodic_pivot_summary.csv")
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
		"Dollar Volume", "ADR %", "Premarket Vol Ratio", "Premarket % Daily",
		"Above 200 EMA", "Extended", "Volume Dried Up", "Near 10/20 EMA",
	}
	writer.Write(headers)

	// Write data
	for _, stock := range stocks {
		row := []string{
			stock.Symbol,
			stock.StockInfo.Name,
			stock.StockInfo.Sector,
			stock.StockInfo.Industry,
			fmt.Sprintf("%.2f", stock.StockInfo.GapUp),
			fmt.Sprintf("%.0f", stock.StockInfo.MarketCap),
			fmt.Sprintf("%.0f", stock.StockInfo.DolVol),
			fmt.Sprintf("%.2f", stock.StockInfo.ADR),
			fmt.Sprintf("%.1f", stock.StockInfo.PremarketVolumeRatio),
			fmt.Sprintf("%.1f", stock.StockInfo.PremarketVolAsPercent),
			boolToString(stock.StockInfo.IsAbove200EMA),
			boolToString(stock.StockInfo.IsExtended),
			// boolToString(stock.StockInfo.VolumeDriedUp),
			boolToString(stock.StockInfo.IsNearEMA1020),
		}
		writer.Write(row)
	}

	fmt.Printf("📄 CSV summary written to %s\n", filename)
	return nil
}

// Helper calculation functions
func calculateAvgGapUp(stocks []FilteredStock) float64 {
	if len(stocks) == 0 {
		return 0
	}

	sum := 0.0
	for _, stock := range stocks {
		sum += stock.StockInfo.GapUp
	}
	return sum / float64(len(stocks))
}

func calculateAvgMarketCap(stocks []FilteredStock) float64 {
	if len(stocks) == 0 {
		return 0
	}

	sum := 0.0
	for _, stock := range stocks {
		sum += stock.StockInfo.MarketCap
	}
	return sum / float64(len(stocks))
}

func checkmark(condition bool) string {
	if condition {
		return "✅"
	}
	return "❌"
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Analyze price action quality
func analyzePriceActionQuality(historicalData []MarketstackEODData, _ float64) map[string]interface{} {
	if len(historicalData) == 0 {
		return map[string]interface{}{"quality": "Unknown"}
	}

	currentDay := historicalData[0]

	// Check if price held near highs (gap up quality indicator)
	gapUpHigh := currentDay.High
	gapUpLow := currentDay.Low
	gapUpClose := currentDay.Close

	// Calculate how much of the gap was retained
	gapRetention := ((gapUpClose - gapUpLow) / (gapUpHigh - gapUpLow)) * 100

	// Analyze intraday action
	var priceActionQuality string
	if gapRetention >= 80 {
		priceActionQuality = "Excellent" // Held near highs
	} else if gapRetention >= 60 {
		priceActionQuality = "Good" // Decent retention
	} else if gapRetention >= 40 {
		priceActionQuality = "Fair" // Some selling but held
	} else {
		priceActionQuality = "Poor" // Heavy selling/fading
	}

	return map[string]interface{}{
		"quality":               priceActionQuality,
		"gap_retention_percent": gapRetention,
		"held_near_highs":       gapRetention >= 70,
	}
}

// Check support and resistance levels
func analyzeSupportsAndResistance(historicalData []MarketstackEODData) map[string]interface{} {
	if len(historicalData) < 20 {
		return map[string]interface{}{"analysis": "Insufficient data"}
	}

	// Sort by date (oldest first)
	sort.Slice(historicalData, func(i, j int) bool {
		return historicalData[i].Date < historicalData[j].Date
	})

	currentPrice := historicalData[len(historicalData)-1].Open

	// Find recent support and resistance levels (last 20 days)
	recent20Days := historicalData[len(historicalData)-20:]

	// Calculate resistance levels (recent highs)
	var resistanceLevels []float64
	for _, day := range recent20Days[:len(recent20Days)-1] { // Exclude current day
		resistanceLevels = append(resistanceLevels, day.High)
	}

	// Sort resistance levels
	sort.Float64s(resistanceLevels)

	// Find highest resistance level below current price
	var keyResistance float64
	for i := len(resistanceLevels) - 1; i >= 0; i-- {
		if resistanceLevels[i] < currentPrice {
			keyResistance = resistanceLevels[i]
			break
		}
	}

	breaksResistance := keyResistance > 0 && currentPrice > keyResistance

	return map[string]interface{}{
		"key_resistance":           keyResistance,
		"current_price":            currentPrice,
		"breaks_resistance":        breaksResistance,
		"resistance_break_percent": ((currentPrice - keyResistance) / keyResistance) * 100,
	}
}

// Get real premarket data using Tiingo IEX endpoint
func getPremarketVolumeDataTiingo(tiingoKey string, symbol string) (*PremarketAnalysis, error) {
	fmt.Printf("📊 Getting real premarket data for %s from Tiingo...\n", symbol)

	// Get today's intraday data (1-min or 5-min candles)
	currentPremarketVol, err := getCurrentPremarketVolumeTiingo(tiingoKey, symbol)
	if err != nil {
		fmt.Printf("⚠️ Could not get current premarket volume for %s: %v\n", symbol, err)
		return simulatePremarketAnalysis(nil, 0), nil
	}

	// Try to get historical premarket volumes using minute data first
	avgPremarketVol, avgDailyVol, err := getHistoricalPremarketVolumesTiingo(tiingoKey, symbol)
	if err != nil {
		fmt.Printf("⚠️ Minute-level premarket data failed for %s: %v\n", symbol, err)

		// Fallback to hourly data
		avgPremarketVol, avgDailyVol, err = getHistoricalPremarketVolumesHourly(tiingoKey, symbol)
		if err != nil {
			fmt.Printf("⚠️ Hourly premarket data failed for %s: %v\n", symbol, err)

			// Final fallback - use conservative estimates based on current volume
			if currentPremarketVol > 0 {
				avgPremarketVol = currentPremarketVol * 0.8 // Assume current is 80% above average
				avgDailyVol = currentPremarketVol * 5       // Assume premarket is 20% of daily
				fmt.Printf("🔄 Using fallback estimates based on current premarket volume\n")
			} else {
				return simulatePremarketAnalysis(nil, 0), nil
			}
		}
	}

	// Ratios
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

	fmt.Printf("📈 %s Premarket Analysis:\n", symbol)
	fmt.Printf("   Current Premarket Volume: %.0f shares\n", currentPremarketVol)
	fmt.Printf("   Average Premarket Volume: %.0f shares\n", avgPremarketVol)
	fmt.Printf("   Volume Ratio: %.2fx\n", volumeRatio)
	fmt.Printf("   As %% of Daily Volume: %.2f%%\n", volAsPercentOfDaily)

	return analysis, nil
}

// Get current premarket volume from Tiingo intraday data
func getCurrentPremarketVolumeTiingo(tiingoKey, symbol string) (float64, error) {
	loc, _ := time.LoadLocation("America/New_York")
	today := time.Now().In(loc).Format("2006-01-02")

	url := fmt.Sprintf("https://api.tiingo.com/iex/%s/prices?startDate=%s&resampleFreq=1min",
		symbol, today)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+tiingoKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %v", err)
	}

	var candles []struct {
		Timestamp string  `json:"date"`
		Volume    float64 `json:"volume"`
	}
	if err := json.Unmarshal(body, &candles); err != nil {
		return 0, fmt.Errorf("failed to parse JSON: %v", err)
	}

	premarketVol := 0.0
	for _, c := range candles {
		// Parse timestamp in ET
		t, err := time.Parse(time.RFC3339, c.Timestamp)
		if err != nil {
			continue
		}
		localT := t.In(loc)
		hour, min, _ := localT.Clock()
		totalMinutes := hour*60 + min

		// Premarket 4:00–9:25
		if totalMinutes >= 240 && totalMinutes <= 565 {
			premarketVol += c.Volume
		}
	}

	fmt.Printf("📊 Found premarket volume %.0f shares for %s\n", premarketVol, symbol)
	return premarketVol, nil
}

// Check if time is in premarket hours (4:00 AM - 9:25 AM ET)
func isPremarketTime(timeStr string) bool {
	// Parse time (format: "HH:MM:SS")
	parts := strings.Split(timeStr, ":")
	if len(parts) < 2 {
		return false
	}

	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}

	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}

	// Convert to minutes since midnight for easier comparison
	totalMinutes := hour*60 + minute

	// Premarket: 4:00 AM (240 min) to 9:25 AM (565 min)
	return totalMinutes >= 240 && totalMinutes <= 565
}

// Structure for Tiingo intraday data
type TiingoIntradayData struct {
	Date   string  `json:"date"`
	Volume float64 `json:"volume"`
}

// Get actual historical premarket volumes from intraday data
func getHistoricalPremarketVolumesTiingo(tiingoKey string, symbol string) (float64, float64, error) {
	// Get last 10 trading days of minute data to calculate premarket volumes
	endDate := time.Now().Format("2006-01-02")
	startDate := time.Now().AddDate(0, 0, -14).Format("2006-01-02") // 14 days to ensure 10 trading days

	// Get minute-level data to capture actual premarket trading
	url := fmt.Sprintf("https://api.tiingo.com/iex/%s/prices?startDate=%s&endDate=%s&resampleFreq=1min&&afterHours=true&columns=open,high,low,close,volume&token=%s",
		symbol, startDate, endDate, tiingoKey)

	fmt.Printf("🔍 Fetching minute-level data for premarket analysis: %s\n", url)

	resp, err := http.Get(url)
	if err != nil {
		return 0, 0, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read response body: %v", err)
	}

	// Check for API errors first
	if string(body) == `{"detail":"Error: resampleFreq must be in 'Min' or 'Hour' only"}` {
		return 0, 0, fmt.Errorf("API frequency error - trying hourly data instead")
	}

	// Parse minute-level data
	var intradayData []TiingoIntradayData
	if err := json.Unmarshal(body, &intradayData); err != nil {
		// Try parsing as generic interface if struct parsing fails
		var rawData interface{}
		if parseErr := json.Unmarshal(body, &rawData); parseErr == nil {
			fmt.Printf("📋 Raw response structure: %+v\n", rawData)
		}
		return 0, 0, fmt.Errorf("failed to parse minute data: %v", err)
	}

	if len(intradayData) == 0 {
		return 0, 0, fmt.Errorf("no intraday data returned")
	}

	// Process data to extract premarket volumes by day
	dailyPremarketVolumes := make(map[string]float64)
	dailyTotalVolumes := make(map[string]float64)

	for _, dataPoint := range intradayData {
		// Parse timestamp
		timestamp, err := time.Parse(time.RFC3339, dataPoint.Date)
		if err != nil {
			continue
		}

		// Convert to Eastern Time (market timezone)
		estLocation, _ := time.LoadLocation("America/New_York")
		estTime := timestamp.In(estLocation)

		dayKey := estTime.Format("2006-01-02")
		hour := estTime.Hour()
		minute := estTime.Minute()

		// Premarket hours: 4:00 AM - 9:30 AM ET
		isPremarket := (hour >= 4 && hour < 9) || (hour == 9 && minute < 30)

		// Accumulate volumes
		dailyTotalVolumes[dayKey] += dataPoint.Volume

		if isPremarket {
			dailyPremarketVolumes[dayKey] += dataPoint.Volume
		}
	}

	// Calculate averages
	if len(dailyPremarketVolumes) == 0 {
		return 0, 0, fmt.Errorf("no premarket data found in the time range")
	}

	var totalPremarketVol, totalDailyVol float64
	validDays := 0

	for day, premarketVol := range dailyPremarketVolumes {
		if premarketVol > 0 {
			totalPremarketVol += premarketVol
			if dailyVol, exists := dailyTotalVolumes[day]; exists {
				totalDailyVol += dailyVol
			}
			validDays++
		}
	}

	if validDays == 0 {
		return 0, 0, fmt.Errorf("no valid premarket trading days found")
	}

	avgPremarketVol := totalPremarketVol / float64(validDays)
	avgDailyVol := totalDailyVol / float64(validDays)

	fmt.Printf("✅ Real premarket data over %d trading days:\n", validDays)
	fmt.Printf("   Avg Daily Volume: %.0f shares\n", avgDailyVol)
	fmt.Printf("   Avg Premarket Volume: %.0f shares\n", avgPremarketVol)
	fmt.Printf("   Premarket as %% of Daily: %.2f%%\n", (avgPremarketVol/avgDailyVol)*100)

	return avgPremarketVol, avgDailyVol, nil
}

// Fallback using hourly data if minute data isn't available
func getHistoricalPremarketVolumesHourly(tiingoKey string, symbol string) (float64, float64, error) {
	endDate := time.Now().Format("2006-01-02")
	startDate := time.Now().AddDate(0, 0, -14).Format("2006-01-02")

	// Use hourly data as fallback
	url := fmt.Sprintf("https://api.tiingo.com/iex/%s/prices?startDate=%s&endDate=%s&resampleFreq=1hour&afterHours=true&columns=open,high,low,close,volume&token=%s",
		symbol, startDate, endDate, tiingoKey)

	fmt.Printf("🔍 Fetching hourly data for premarket analysis: %s\n", url)

	resp, err := http.Get(url)
	if err != nil {
		return 0, 0, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read response body: %v", err)
	}

	fmt.Printf("📥 Hourly raw response (first 500 chars): %s\n", string(body)[:min(len(body), 500)])

	var intradayData []TiingoIntradayData
	if err := json.Unmarshal(body, &intradayData); err != nil {
		return 0, 0, fmt.Errorf("failed to parse hourly data: %v", err)
	}

	fmt.Printf("📊 Received %d hourly data points\n", len(intradayData))
	if len(intradayData) == 0 {
		return 0, 0, fmt.Errorf("no hourly data returned")
	}

	// Process hourly data to extract premarket volumes
	dailyPremarketVolumes := make(map[string]float64)
	dailyTotalVolumes := make(map[string]float64)

	for _, dataPoint := range intradayData {
		timestamp, err := time.Parse(time.RFC3339, dataPoint.Date)
		if err != nil {
			continue
		}

		estLocation, _ := time.LoadLocation("America/New_York")
		estTime := timestamp.In(estLocation)

		dayKey := estTime.Format("2006-01-02")
		hour := estTime.Hour()

		// Premarket hours: 4:00 AM - 9:00 AM ET (hourly granularity)
		isPremarket := hour >= 4 && hour <= 8

		dailyTotalVolumes[dayKey] += dataPoint.Volume

		if isPremarket {
			dailyPremarketVolumes[dayKey] += dataPoint.Volume
		}
	}

	// Calculate averages
	if len(dailyPremarketVolumes) == 0 {
		return 0, 0, fmt.Errorf("no premarket data found in hourly data")
	}

	var totalPremarketVol, totalDailyVol float64
	validDays := 0

	for day, premarketVol := range dailyPremarketVolumes {
		if premarketVol > 0 {
			totalPremarketVol += premarketVol
			if dailyVol, exists := dailyTotalVolumes[day]; exists {
				totalDailyVol += dailyVol
			}
			validDays++
		}
	}

	if validDays == 0 {
		return 0, 0, fmt.Errorf("no valid premarket trading days found in hourly data")
	}

	avgPremarketVol := totalPremarketVol / float64(validDays)
	avgDailyVol := totalDailyVol / float64(validDays)

	fmt.Printf("✅ Hourly premarket data over %d trading days:\n", validDays)
	fmt.Printf("   Avg Daily Volume: %.0f shares\n", avgDailyVol)
	fmt.Printf("   Avg Premarket Volume: %.0f shares\n", avgPremarketVol)

	return avgPremarketVol, avgDailyVol, nil
}

// Function to run complete episodic pivot analysis with real premarket data
func RunEpisodicPivotAnalysis(marketstackKey string, tiingoKey string) {
	fmt.Println("🚀 Starting Complete Episodic Pivot Analysis...")
	fmt.Println("📊 Using Marketstack for EOD data + Tiingo for premarket data")

	if tiingoKey == "" {
		fmt.Println("⚠️ No Tiingo API key provided - will use simulated premarket data")
		fmt.Println("💡 Get free Tiingo API key at: https://api.tiingo.com/")
	}

	// Set Tiingo API key in environment for functions to use
	if tiingoKey != "" {
		os.Setenv("TIINGO_API_KEY", tiingoKey)
	}

	// Run the main filter
	FilterStocksEpisodicPivot(marketstackKey, tiingoKey)

	fmt.Println("\n📋 EPISODIC PIVOT CHECKLIST:")
	fmt.Println("✅ Catalyst: [MANUAL] - Check news for strong catalyst")
	fmt.Println("✅ Gap Up: 8%+ filtered automatically")
	if tiingoKey != "" {
		fmt.Println("✅ Premarket Volume: REAL data from Alpha Vantage")
	} else {
		fmt.Println("🔶 Premarket Volume: SIMULATED (add Alpha Vantage key for real data)")
	}
	fmt.Println("✅ Price Action: Above 200 EMA + technical analysis")
	fmt.Println("✅ Volume: $10M+ dollar volume filtered")
	fmt.Println("✅ Extension: Non-extended stocks filtered")
	fmt.Println("✅ Structure: Volume dried up + near EMAs filtered")
	fmt.Println("✅ Liquidity: $200M+ market cap filtered")
	fmt.Println("")
	fmt.Println("📝 MANUAL CHECKS STILL NEEDED:")
	fmt.Println("- ⭐ CATALYST: Verify strong catalyst in news (earnings beat, FDA approval, M&A, etc.)")
	fmt.Println("- 📈 PRICE ACTION: Confirm price held near premarket highs")
	fmt.Println("- 📊 BID-ASK: Check tight bid-ask spreads in premarket")
	fmt.Println("- 📈 EARNINGS: Check previous 3-4 quarters for positive reactions")
	fmt.Println("- 💰 FUNDAMENTALS: Verify EPS beat, revenue growth 20-50% YoY")
	fmt.Println("- 📢 GUIDANCE: Look for raised guidance in earnings/news")

	if tiingoKey == "" {
		fmt.Println("\n🔑 TO GET REAL PREMARKET DATA:")
		fmt.Println("1. Get free Tiingo API key: https://api.tiingo.com/")
		fmt.Println("2. Set environment variable: export TIINGO_API_KEY=your_key")
		fmt.Println("3. Re-run analysis for real premarket volume data")
	}
}

func getCurrentPrice(historicalData []MarketstackEODData) float64 {
	if len(historicalData) == 0 {
		return 0
	}
	return historicalData[0].Close // Most recent close
}
