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
	APIKey     string
	APISecret  string
	BaseURL    string // "https://paper-api.alpaca.markets" or "https://api.alpaca.markets"
	DataURL    string // "https://data.alpaca.markets"
	IsPaper    bool
	FinnhubKey string
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
	Symbol             string
	CurrentPrice       float64
	PremarketOpen      float64
	PremarketHigh      float64
	PremarketLow       float64
	PremarketClose     float64
	PremarketVolume    float64
	RegularSessionOpen float64
	PreviousClose      float64
	GapUpPercent       float64
	GapUsedPrice       float64 // the actual price used for gap calculation — carried through to Stage 3
	Exchange           string
	Name               string
}

// RealtimeResult extends FilteredStock with real-time metrics
type RealtimeResult struct {
	FilteredStock
	ScanTime        string   `json:"scan_time"`
	MarketStatus    string   `json:"market_status"`
	DataQuality     string   `json:"data_quality"`
	ValidationNotes []string `json:"validation_notes"`
	// GapUsedPrice carried from Stage 1 so Stage 3 can use it as currentPrice
	// for EMA distance calculations instead of yesterday's close.
	GapUsedPrice float64 `json:"gap_used_price"`
	// Status is "confident" for 7/7 criteria, "questionable" for 6/7.
	Status string `json:"status"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Main real-time scanning function
// ─────────────────────────────────────────────────────────────────────────────

func FilterStocksEpisodicPivotRealtime(config AlpacaConfig) error {
	logDir := "data/stockdata/logs"
	logger, err := InitLogger(logDir, "realtime_scan")
	if err != nil {
		fmt.Printf("⚠️  Could not create log file: %v\n", err)
	} else {
		defer logger.Close()
	}

	est, err := time.LoadLocation("America/New_York")
	if err != nil {
		return fmt.Errorf("failed to load EST timezone: %v", err)
	}

	now := time.Now().In(est)
	marketStatus := getMarketStatus(now)

	LogSection("EPISODIC PIVOT REAL-TIME SCANNER")
	LogInfo("INIT", "Scan Time     : %s EST", now.Format("2006-01-02 15:04:05"))
	LogInfo("INIT", "Market Status : %s", marketStatus)

	if marketStatus != "PREMARKET" {
		LogWarn("INIT", "", "Running outside the 4:00–9:00am EST scan window — current time is %s EST",
			now.Format("15:04:05"))
	}

	LogSection(fmt.Sprintf("STAGE 1 — Gap Up Filter (min %.0f%%)", MIN_GAP_UP_PERCENT))
	t0 := time.Now()
	gapUpStocks, err := realtimeStage1GapUp(config)
	if err != nil {
		LogError("S1", "", "Stage 1 failed: %v", err)
		return fmt.Errorf("error in Stage 1: %v", err)
	}
	LogStageSummary("S1", len(gapUpStocks), len(gapUpStocks), time.Since(t0))

	if len(gapUpStocks) == 0 {
		LogWarn("S1", "", "No stocks found with gap up criteria")
		return nil
	}

	LogSection(fmt.Sprintf("STAGE 2 — Liquidity Filter (min market cap $%dM)", MIN_MARKET_CAP/1_000_000))
	t0 = time.Now()
	liquidStocks, err := realtimeStage2Liquidity(config, gapUpStocks)
	if err != nil {
		LogError("S2", "", "Stage 2 failed: %v", err)
		return fmt.Errorf("error in Stage 2: %v", err)
	}
	LogStageSummary("S2", len(liquidStocks), len(gapUpStocks), time.Since(t0))

	LogSection("STAGE 3 — Technical Analysis")
	t0 = time.Now()
	technicalStocks, err := realtimeStage3Technical(config, liquidStocks)
	if err != nil {
		LogError("S3", "", "Stage 3 failed: %v", err)
		return fmt.Errorf("error in Stage 3: %v", err)
	}
	LogStageSummary("S3", len(technicalStocks), len(liquidStocks), time.Since(t0))

	LogSection("STAGE 4 — Final Episodic Pivot Criteria")
	t0 = time.Now()
	finalStocks := realtimeStage4Final(technicalStocks)
	// Count by status for the summary log
	confidentCount, questionableCount := 0, 0
	for _, s := range finalStocks {
		if s.Status == "confident" {
			confidentCount++
		} else {
			questionableCount++
		}
	}
	LogStageSummary("S4", len(finalStocks), len(technicalStocks), time.Since(t0))
	LogInfo("S4", "  confident=%d  questionable=%d", confidentCount, questionableCount)

	LogSection("Saving Results")
	if err := outputRealtimeResults(config, finalStocks, now); err != nil {
		LogError("OUT", "", "Failed to write results: %v", err)
		return fmt.Errorf("error writing results: %v", err)
	}

	LogSection(fmt.Sprintf("SCAN COMPLETE — %d qualifying stocks (%d confident, %d questionable)",
		len(finalStocks), confidentCount, questionableCount))
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Stage 1: Gap Up Filter
// ─────────────────────────────────────────────────────────────────────────────

func realtimeStage1GapUp(config AlpacaConfig) ([]RealtimeStockData, error) {
	symbols, err := getAlpacaTradableSymbolsMain(config)
	if err != nil {
		return nil, fmt.Errorf("failed to get symbols: %v", err)
	}
	LogInfo("S1", "Scanning %d symbols for gap ups (NYSE + NASDAQ only)", len(symbols))

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
			current := processedCount
			mu.Unlock()

			if current%50 == 0 {
				LogProgress("S1", current, len(symbols), "")
			}

			previousClose, err := getAlpacaPreviousClose(config, sym)
			if err != nil {
				LogDebug("S1", sym, "Failed to get previous close: %v", err)
				return
			}
			if previousClose <= 0 {
				LogDebug("S1", sym, "Invalid previous close: $%.2f", previousClose)
				return
			}

			premarketData, err := getAlpacaPremarketData(config, sym)
			if err != nil {
				LogDebug("S1", sym, "No premarket data: %v", err)
				return
			}
			if premarketData == nil {
				return
			}

			// Prefer the regular-session open once the market is open.
			// Before 9:30am fall back to the premarket session high.
			regularOpen, err := getAlpacaRegularSessionOpen(config, sym)
			var gapPrice float64
			var gapPriceLabel string
			if err == nil && regularOpen > 0 {
				gapPrice = regularOpen
				gapPriceLabel = fmt.Sprintf("RegularOpen=$%.2f", regularOpen)
				premarketData.RegularSessionOpen = regularOpen
			} else {
				gapPrice = premarketData.PremarketHigh
				gapPriceLabel = fmt.Sprintf("PMHigh=$%.2f (regular session not yet open)", premarketData.PremarketHigh)
			}

			if gapPrice <= 0 {
				return
			}

			gapUp := ((gapPrice - previousClose) / previousClose) * 100
			premarketData.GapUsedPrice = gapPrice

			LogDebug("S1", sym, "PrevClose=$%.2f  %s  PMVol=%.0f  GapUp=%.2f%%",
				previousClose, gapPriceLabel, premarketData.PremarketVolume, gapUp)

			if gapUp >= MIN_GAP_UP_PERCENT && gapPrice >= MIN_STOCK_PRICE {
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
					GapUsedPrice:    gapPrice,
				}

				mu.Lock()
				LogQualify("S1", sym, fmt.Sprintf("Gap=%.2f%%  PrevClose=$%.2f  GapPrice=$%.2f  PMVol=%.0f",
					gapUp, previousClose, gapPrice, premarketData.PremarketVolume))
				gapUpStocks = append(gapUpStocks, stockData)
				mu.Unlock()
			} else {
				LogReject("S1", sym, fmt.Sprintf("Gap=%.2f%% < %.0f%% minimum", gapUp, MIN_GAP_UP_PERCENT))
				LogReject("S1", sym, fmt.Sprintf("Stock price=$%.2f < $%.2f minimum", gapPrice, MIN_STOCK_PRICE))
			}
		}(symbol)
	}

	wg.Wait()
	return gapUpStocks, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Stage 2: Liquidity Filter
// ─────────────────────────────────────────────────────────────────────────────

func realtimeStage2Liquidity(config AlpacaConfig, stocks []RealtimeStockData) ([]RealtimeResult, error) {
	var liquidStocks []RealtimeResult
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	LogInfo("S2", "Checking liquidity for %d stocks", len(stocks))

	for i, stock := range stocks {
		LogDebug("S2", stock.Symbol, "[%d/%d] Fetching Finnhub company profile", i+1, len(stocks))
		<-rateLimiter

		var marketCap float64
		var name, sector, industry string = stock.Symbol, "Unknown", "Unknown"

		if config.FinnhubKey != "" {
			companyProfile, err := getCompanyProfileFinnhub(config.FinnhubKey, stock.Symbol)
			if err != nil {
				LogWarn("S2", stock.Symbol, "Finnhub profile error: %v", err)
			} else if companyProfile != nil {
				marketCap = companyProfile.MarketCap * 1_000_000
				if companyProfile.Name != "" {
					name = companyProfile.Name
				}
				if companyProfile.FinnhubIndustry != "" {
					industry = companyProfile.FinnhubIndustry
					sector = parseSectorFromIndustry(companyProfile.FinnhubIndustry)
				}
				LogDebug("S2", stock.Symbol, "Finnhub: Name=%s  MarketCap=$%.0fM  Sector=%s  Industry=%s",
					name, marketCap/1_000_000, sector, industry)
			}
		} else {
			LogWarn("S2", stock.Symbol, "No FinnhubKey configured — falling back to volume estimate")
		}

		if marketCap == 0 {
			LogWarn("S2", stock.Symbol, "No Finnhub market cap — estimating from volume/price")
			marketCap = estimateMarketCapFromRealtime(stock)
			LogDebug("S2", stock.Symbol, "Estimated market cap: $%.0fM", marketCap/1_000_000)
		}

		asset, err := getAlpacaAssetInfo(config, stock.Symbol)
		if err != nil {
			LogWarn("S2", stock.Symbol, "Could not get asset info: %v", err)
		} else {
			if name == stock.Symbol && asset.Name != "" {
				name = asset.Name
			}
		}

		exchange := "Unknown"
		if asset != nil {
			exchange = asset.Exchange
		}

		LogMetrics("S2", stock.Symbol, map[string]interface{}{
			"current_price":  fmt.Sprintf("$%.2f", stock.CurrentPrice),
			"gap_used_price": fmt.Sprintf("$%.2f", stock.GapUsedPrice),
			"market_cap_m":   fmt.Sprintf("$%.2fM", marketCap/1_000_000),
			"min_required_m": fmt.Sprintf("$%.2fM", float64(MIN_MARKET_CAP)/1_000_000),
			"pm_volume":      fmt.Sprintf("%.0f", stock.PremarketVolume),
			"gap_up_pct":     fmt.Sprintf("%.2f%%", stock.GapUpPercent),
			"sector":         sector,
			"industry":       industry,
		})

		if marketCap >= MIN_MARKET_CAP {
			result := RealtimeResult{
				FilteredStock: FilteredStock{
					Symbol: stock.Symbol,
					StockInfo: StockStats{
						Timestamp:       time.Now().Format(time.RFC3339),
						MarketCap:       marketCap,
						GapUp:           stock.GapUpPercent,
						Name:            name,
						Exchange:        exchange,
						Sector:          sector,
						Industry:        industry,
						PremarketVolume: stock.PremarketVolume,
					},
				},
				ScanTime:        time.Now().Format(time.RFC3339),
				MarketStatus:    "PREMARKET",
				DataQuality:     "Good",
				ValidationNotes: []string{},
				// Carry GapUsedPrice through so Stage 3 can use it as currentPrice
				GapUsedPrice: stock.GapUsedPrice,
			}

			liquidStocks = append(liquidStocks, result)
			LogQualify("S2", stock.Symbol, fmt.Sprintf("MarketCap=$%.0fM >= $%dM",
				marketCap/1_000_000, MIN_MARKET_CAP/1_000_000))
		} else {
			LogReject("S2", stock.Symbol, fmt.Sprintf("MarketCap=$%.0fM < $%dM",
				marketCap/1_000_000, MIN_MARKET_CAP/1_000_000))
		}
	}

	return liquidStocks, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Stage 3: Technical Analysis
// ─────────────────────────────────────────────────────────────────────────────

func realtimeStage3Technical(config AlpacaConfig, stocks []RealtimeResult) ([]RealtimeResult, error) {
	var technicalStocks []RealtimeResult
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	LogInfo("S3", "Analyzing technical indicators for %d stocks", len(stocks))

	for i, stock := range stocks {
		LogDebug("S3", stock.Symbol, "[%d/%d] Fetching 300-day historical data", i+1, len(stocks))
		<-rateLimiter

		historicalData, err := getAlpacaHistoricalBars(config, stock.Symbol, 300)
		if err != nil {
			LogWarn("S3", stock.Symbol, "Historical data error: %v", err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Historical data error: %v", err))
			continue
		}
		LogDebug("S3", stock.Symbol, "Retrieved %d days of historical data", len(historicalData))

		// +1 headroom because today's bar may be present and will be stripped
		// inside the calculation functions, potentially dropping below 200
		if len(historicalData) < 201 {
			LogReject("S3", stock.Symbol,
				fmt.Sprintf("Insufficient data — %d days (need 201+)", len(historicalData)))
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Insufficient data: %d days", len(historicalData)))
			stock.DataQuality = "Poor"
			continue
		}

		// Pass GapUsedPrice so EMA distances are calculated against the actual
		// gap price rather than yesterday's close.
		technicalIndicators, err := calculateTechnicalIndicatorsRealtime(historicalData, stock.Symbol, stock.GapUsedPrice)
		if err != nil {
			LogWarn("S3", stock.Symbol, "Technical indicators error: %v", err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Technical analysis error: %v", err))
			continue
		}

		dolVol, adr, err := calculateHistoricalMetricsRealtime(historicalData)
		if err != nil {
			LogWarn("S3", stock.Symbol, "Metrics error: %v", err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Metrics calculation error: %v", err))
			continue
		}

		// Pass the already-fetched historical bars — avoids a redundant API call
		premarketMetrics, err := getAlpacaPremarketMetrics(config, stock.Symbol, historicalData)
		if err != nil {
			LogWarn("S3", stock.Symbol, "Premarket metrics error: %v", err)
		}

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
		stock.StockInfo.VolumeDriedUp = technicalIndicators.VolumeDriedUp

		pmRatio := 0.0
		if premarketMetrics != nil {
			pmRatio = premarketMetrics.VolumeRatio
		}

		LogMetrics("S3", stock.Symbol, map[string]interface{}{
			"gap_used_price":       fmt.Sprintf("$%.2f", stock.GapUsedPrice),
			"above_200_ema":        technicalIndicators.IsAbove200EMA,
			"adr_pct":              fmt.Sprintf("%.2f%%", adr),
			"breaks_resistance":    technicalIndicators.BreaksResistance,
			"dist_from_50_ema_adr": fmt.Sprintf("%.2f", technicalIndicators.DistanceFrom50EMA),
			"dol_vol_m":            fmt.Sprintf("$%.2fM", dolVol/1_000_000),
			"ema_10":               fmt.Sprintf("$%.2f", technicalIndicators.EMA10),
			"ema_20":               fmt.Sprintf("$%.2f", technicalIndicators.EMA20),
			"ema_200":              fmt.Sprintf("$%.2f", technicalIndicators.EMA200),
			"ema_50":               fmt.Sprintf("$%.2f", technicalIndicators.EMA50),
			"is_extended":          technicalIndicators.IsExtended,
			"is_near_ema_10_20":    technicalIndicators.IsNearEMA1020,
			"is_too_extended":      technicalIndicators.IsTooExtended,
			"pm_vol_ratio":         fmt.Sprintf("%.2fx", pmRatio),
			"sma_200":              fmt.Sprintf("$%.2f", technicalIndicators.SMA200),
			"volume_dried_up":      technicalIndicators.VolumeDriedUp,
		})

		technicalStocks = append(technicalStocks, stock)
		LogQualify("S3", stock.Symbol, "Passed technical analysis")
	}

	return technicalStocks, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Stage 4: Final Filter
// Stocks passing all 7 criteria are marked "confident".
// Stocks passing exactly 5 of 7 criteria are marked "questionable".
// Stocks passing fewer than 5 are rejected entirely.
// IsTooExtended is treated as an automatic hard disqualifier regardless of
// the 6/7 threshold — a stock that is too extended is always rejected.
// ─────────────────────────────────────────────────────────────────────────────

func realtimeStage4Final(stocks []RealtimeResult) []RealtimeResult {
	var finalStocks []RealtimeResult

	LogInfo("S4", "Applying final episodic pivot criteria to %d stocks", len(stocks))

	for _, stock := range stocks {
		type criterion struct {
			name   string
			passed bool
			detail string
		}

		criteria := []criterion{
			{
				"Dollar Volume",
				stock.StockInfo.DolVol >= MIN_DOLLAR_VOLUME,
				fmt.Sprintf("$%.0fM >= $%.0fM", stock.StockInfo.DolVol/1_000_000, float64(MIN_DOLLAR_VOLUME)/1_000_000),
			},
			{
				"Premarket Vol Ratio",
				stock.StockInfo.PremarketVolumeRatio >= MIN_PREMARKET_VOL_RATIO,
				fmt.Sprintf("%.2fx >= %.1fx", stock.StockInfo.PremarketVolumeRatio, MIN_PREMARKET_VOL_RATIO),
			},
			{
				"Premarket Vol % of Daily",
				stock.StockInfo.PremarketVolAsPercent >= MIN_PREMARKET_VOL_PCT_OF_DAILY,
				fmt.Sprintf("%.2f%% >= %.0f%% of avg daily volume",
					stock.StockInfo.PremarketVolAsPercent, MIN_PREMARKET_VOL_PCT_OF_DAILY),
			},
			{
				"Above 200 EMA",
				stock.StockInfo.IsAbove200EMA,
				fmt.Sprintf("GapPrice=$%.2f vs EMA200=$%.2f", stock.GapUsedPrice, stock.StockInfo.EMA200),
			},
			{
				"Not Extended",
				!stock.StockInfo.IsExtended,
				fmt.Sprintf("dist=%.2f ADRs (max %.1f)", stock.StockInfo.DistanceFrom50EMA, MAX_EXTENSION_ADR),
			},
			{
				"Near 10/20 EMA",
				stock.StockInfo.IsNearEMA1020,
				fmt.Sprintf("within %.1f ADRs of EMA10 or EMA20", NEAR_EMA_ADR_THRESHOLD),
			},
			{
				"Volume Dried Up",
				stock.StockInfo.VolumeDriedUp,
				"20-day avg vol < 60-day avg vol (consolidation signal)",
			},
		}

		// Hard disqualifier — checked independently of the 6/7 threshold.
		if stock.StockInfo.IsTooExtended {
			LogDebug("S4", stock.Symbol, "❌ HARD REJECT  Too Extended — %.2f ADRs > %.1f",
				stock.StockInfo.DistanceFrom50EMA, TOO_EXTENDED_ADR)
			LogReject("S4", stock.Symbol, fmt.Sprintf("Too extended (%.2f ADRs > %.1f max)",
				stock.StockInfo.DistanceFrom50EMA, TOO_EXTENDED_ADR))
			continue
		}

		passedCount := 0
		for _, c := range criteria {
			if c.passed {
				passedCount++
				LogDebug("S4", stock.Symbol, "✅ PASS  %s — %s", c.name, c.detail)
			} else {
				LogDebug("S4", stock.Symbol, "❌ FAIL  %s — %s", c.name, c.detail)
			}
		}

		totalCriteria := len(criteria) // 7

		switch {
		case passedCount == totalCriteria:
			stock.Status = "confident"
			finalStocks = append(finalStocks, stock)
			LogQualify("S4", stock.Symbol, fmt.Sprintf(
				"[confident 7/7]  Gap=%.2f%%  GapPrice=$%.2f  DolVol=$%.0fM  ADR=%.2f%%  PMRatio=%.2fx  Dist50EMA=%.2f  VolDriedUp=%v",
				stock.StockInfo.GapUp, stock.GapUsedPrice, stock.StockInfo.DolVol/1_000_000,
				stock.StockInfo.ADR, stock.StockInfo.PremarketVolumeRatio,
				stock.StockInfo.DistanceFrom50EMA, stock.StockInfo.VolumeDriedUp))

		case passedCount >= totalCriteria-2 && stock.StockInfo.PremarketVolumeRatio >= 1.5:
			stock.Status = "questionable"
			// Record which criterion was missed for transparency
			for _, c := range criteria {
				if !c.passed {
					stock.ValidationNotes = append(stock.ValidationNotes,
						fmt.Sprintf("missed: %s (%s)", c.name, c.detail))
				}
			}
			finalStocks = append(finalStocks, stock)
			LogQualify("S4", stock.Symbol, fmt.Sprintf(
				"[questionable 5/7]  Gap=%.2f%%  GapPrice=$%.2f  DolVol=$%.0fM  ADR=%.2f%%  PMRatio=%.2fx  Dist50EMA=%.2f  VolDriedUp=%v",
				stock.StockInfo.GapUp, stock.GapUsedPrice, stock.StockInfo.DolVol/1_000_000,
				stock.StockInfo.ADR, stock.StockInfo.PremarketVolumeRatio,
				stock.StockInfo.DistanceFrom50EMA, stock.StockInfo.VolumeDriedUp))

		default:
			LogReject("S4", stock.Symbol,
				fmt.Sprintf("Only %d/%d criteria passed (need at least 6)", passedCount, totalCriteria))
		}
	}

	LogInfo("S4", "Final filter complete: %d / %d stocks qualified", len(finalStocks), len(stocks))
	return finalStocks
}

// ─────────────────────────────────────────────────────────────────────────────
// Alpaca API helpers
// ─────────────────────────────────────────────────────────────────────────────

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
		if asset.Tradable && asset.Status == "active" &&
			(asset.Exchange == "NASDAQ" || asset.Exchange == "NYSE") {
			symbols = append(symbols, asset.Symbol)
		}
	}
	return symbols, nil
}

func getAlpacaPreviousClose(config AlpacaConfig, symbol string) (float64, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -6)

	url := fmt.Sprintf("%s/v2/stocks/%s/bars?timeframe=1Day&start=%s&end=%s&limit=5&adjustment=split&feed=sip",
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

	var barsResp AlpacaSingleSymbolBarsResponse
	if err := json.Unmarshal(body, &barsResp); err != nil {
		return 0, fmt.Errorf("failed to parse response: %v. Body: %s", err, string(body))
	}

	if len(barsResp.Bars) == 0 {
		return 0, fmt.Errorf("no bars returned for %s", symbol)
	}

	sort.Slice(barsResp.Bars, func(i, j int) bool {
		return barsResp.Bars[i].Timestamp < barsResp.Bars[j].Timestamp
	})

	est, _ := time.LoadLocation("America/New_York")
	today := time.Now().In(est).Format("2006-01-02")

	for i := len(barsResp.Bars) - 1; i >= 0; i-- {
		if !strings.HasPrefix(barsResp.Bars[i].Timestamp, today) {
			return barsResp.Bars[i].Close, nil
		}
	}

	return 0, fmt.Errorf("no previous close found for %s", symbol)
}

// getAlpacaRegularSessionOpen fetches today's 9:30am daily bar open.
// Returns an error before 9:30am EST when the bar doesn't exist yet.
func getAlpacaRegularSessionOpen(config AlpacaConfig, symbol string) (float64, error) {
	est, _ := time.LoadLocation("America/New_York")
	now := time.Now().In(est)
	today := now.Format("2006-01-02")

	url := fmt.Sprintf("%s/v2/stocks/%s/bars?timeframe=1Day&start=%s&end=%s&limit=1&adjustment=split&feed=sip",
		config.DataURL, symbol, today, today)

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

	var barsResp AlpacaSingleSymbolBarsResponse
	if err := json.Unmarshal(body, &barsResp); err != nil {
		return 0, err
	}

	if len(barsResp.Bars) == 0 {
		return 0, fmt.Errorf("no daily bar yet for %s today", symbol)
	}

	if !strings.HasPrefix(barsResp.Bars[0].Timestamp, today) {
		return 0, fmt.Errorf("daily bar is not for today")
	}

	return barsResp.Bars[0].Open, nil
}

// getAlpacaPremarketData fetches premarket session data for a symbol.
// Two calls:
//   - /bars/latest  → open/high/low/close of the most recent bar
//   - 1-min bars 4am–9:30am → summed for true cumulative premarket volume
type AlpacaLatestBarResponse struct {
	Bar AlpacaBar `json:"bar"`
}

func getAlpacaPremarketData(config AlpacaConfig, symbol string) (*RealtimeStockData, error) {
	est, _ := time.LoadLocation("America/New_York")
	today := time.Now().In(est).Format("2006-01-02")

	// ── Call 1: latest bar for price snapshot ────────────────────────────
	latestURL := fmt.Sprintf("%s/v2/stocks/%s/bars/latest?feed=sip", config.DataURL, symbol)
	latestReq, err := http.NewRequest("GET", latestURL, nil)
	if err != nil {
		return nil, err
	}
	latestReq.Header.Set("APCA-API-KEY-ID", config.APIKey)
	latestReq.Header.Set("APCA-API-SECRET-KEY", config.APISecret)

	client := &http.Client{Timeout: 10 * time.Second}
	latestResp, err := client.Do(latestReq)
	if err != nil {
		return nil, err
	}
	defer latestResp.Body.Close()

	latestBody, err := io.ReadAll(latestResp.Body)
	if err != nil {
		return nil, err
	}

	if latestResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("alpaca API error %d: %s", latestResp.StatusCode, string(latestBody))
	}

	var latestBarsResp AlpacaLatestBarResponse
	if err := json.Unmarshal(latestBody, &latestBarsResp); err != nil {
		return nil, fmt.Errorf("failed to parse latest bar: %v", err)
	}

	if latestBarsResp.Bar.Volume == 0 {
		return nil, fmt.Errorf("no premarket data for %s", symbol)
	}

	latestBar := latestBarsResp.Bar

	// ── Call 2: 1-min bars for cumulative premarket volume ───────────────
	volURL := fmt.Sprintf(
		"%s/v2/stocks/%s/bars?timeframe=1Min&start=%sT04:00:00-05:00&end=%sT09:30:00-05:00&limit=1000&feed=sip",
		config.DataURL, symbol, today, today)

	volReq, err := http.NewRequest("GET", volURL, nil)
	if err != nil {
		return nil, err
	}
	volReq.Header.Set("APCA-API-KEY-ID", config.APIKey)
	volReq.Header.Set("APCA-API-SECRET-KEY", config.APISecret)

	volResp, err := client.Do(volReq)
	if err != nil {
		return nil, err
	}
	defer volResp.Body.Close()

	volBody, err := io.ReadAll(volResp.Body)
	if err != nil {
		return nil, err
	}

	var volBarsResp AlpacaSingleSymbolBarsResponse
	if volResp.StatusCode == http.StatusOK {
		_ = json.Unmarshal(volBody, &volBarsResp)
	}

	var totalVol float64
	for _, bar := range volBarsResp.Bars {
		totalVol += bar.Volume
	}
	if totalVol == 0 {
		// Fallback: use latest bar volume rather than returning zero
		totalVol = latestBar.Volume
	}

	LogDebug("S1", symbol, "PM bars fetched=%d  totalVol=%.0f  latestClose=$%.2f",
		len(volBarsResp.Bars), totalVol, latestBar.Close)

	return &RealtimeStockData{
		PremarketOpen:   latestBar.Open,
		PremarketHigh:   latestBar.High,
		PremarketLow:    latestBar.Low,
		PremarketClose:  latestBar.Close,
		PremarketVolume: totalVol,
	}, nil
}

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

func getAlpacaHistoricalBars(config AlpacaConfig, symbol string, daysBack int) ([]AlpacaBar, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -(daysBack + 50))

	url := fmt.Sprintf("%s/v2/stocks/%s/bars?timeframe=1Day&start=%s&end=%s&limit=1000&adjustment=split&feed=sip",
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

	var barsResp AlpacaSingleSymbolBarsResponse
	if err := json.Unmarshal(body, &barsResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v. Body: %s", err, string(body))
	}

	if len(barsResp.Bars) == 0 {
		return nil, fmt.Errorf("no historical data")
	}

	return barsResp.Bars, nil
}

// getAlpacaPremarketMetrics computes premarket volume ratio using the
// already-fetched historical bars — no extra API call needed.
func getAlpacaPremarketMetrics(config AlpacaConfig, symbol string, historicalBars []AlpacaBar) (*PremarketAnalysis, error) {
	premarketData, err := getAlpacaPremarketData(config, symbol)
	if err != nil {
		return nil, err
	}

	est, _ := time.LoadLocation("America/New_York")
	today := time.Now().In(est).Format("2006-01-02")

	var priorBars []AlpacaBar
	for _, b := range historicalBars {
		if !strings.HasPrefix(b.Timestamp, today) {
			priorBars = append(priorBars, b)
		}
	}
	if len(priorBars) == 0 {
		return nil, fmt.Errorf("no prior bars available for %s", symbol)
	}

	// Use only the last 21 confirmed daily bars for the average
	if len(priorBars) > 21 {
		priorBars = priorBars[len(priorBars)-21:]
	}

	avgDailyVol := calcAvgVolFromBars(priorBars)
	avgPremarketVol := avgDailyVol * 0.12

	volumeRatio := 0.0
	if avgPremarketVol > 0 {
		volumeRatio = premarketData.PremarketVolume / avgPremarketVol
	}

	volAsPercentOfDaily := 0.0
	if avgDailyVol > 0 {
		volAsPercentOfDaily = (premarketData.PremarketVolume / avgDailyVol) * 100
	}

	LogDebug("S3", symbol, "PMMetrics: bars=%d  avgDailyVol=%.0f  pmVol=%.0f  ratio=%.2fx  pctOfDaily=%.1f%%",
		len(priorBars), avgDailyVol, premarketData.PremarketVolume, volumeRatio, volAsPercentOfDaily)

	return &PremarketAnalysis{
		CurrentPremarketVol: premarketData.PremarketVolume,
		AvgPremarketVol:     avgPremarketVol,
		VolumeRatio:         volumeRatio,
		VolAsPercentOfDaily: volAsPercentOfDaily,
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Technical calculation helpers
// ─────────────────────────────────────────────────────────────────────────────

// calculateTechnicalIndicatorsRealtime computes EMAs/SMAs and derived flags.
//
// currentPremarketPrice is the gap-up price from Stage 1 (PremarketHigh or
// RegularOpen). Using it instead of yesterday's close ensures IsAbove200EMA
// and DistanceFrom50EMA reflect where the stock actually is right now.
func calculateTechnicalIndicatorsRealtime(bars []AlpacaBar, symbol string, currentPremarketPrice float64) (*TechnicalIndicators, error) {
	sort.Slice(bars, func(i, j int) bool { return bars[i].Timestamp < bars[j].Timestamp })

	// Strip today's partial bar if present
	est, _ := time.LoadLocation("America/New_York")
	today := time.Now().In(est).Format("2006-01-02")
	if len(bars) > 0 && strings.HasPrefix(bars[len(bars)-1].Timestamp, today) {
		bars = bars[:len(bars)-1]
	}

	if len(bars) < 200 {
		return nil, fmt.Errorf("insufficient data after stripping today: %d bars", len(bars))
	}

	closes := make([]float64, len(bars))
	for i, bar := range bars {
		closes[i] = bar.Close
	}

	// Use the actual premarket/gap price for all distance calculations.
	// Fall back to yesterday's close if no premarket price was available.
	currentPrice := currentPremarketPrice
	if currentPrice <= 0 {
		currentPrice = closes[len(closes)-1]
	}

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

	sma200 := calculateSMAFromSlice(closes, 200)
	ema200 := calculateEMAFromSlice(closes, 200)
	ema50 := calculateEMAFromSlice(closes, 50)
	ema20 := calculateEMAFromSlice(closes, 20)
	ema10 := calculateEMAFromSlice(closes, 10)

	distanceFrom50EMA := 0.0
	if currentADR > 0 {
		distanceFrom50EMA = (currentPrice - ema50) / (currentADR * currentPrice / 100)
	}

	distanceFrom10EMA, distanceFrom20EMA := 0.0, 0.0
	if currentADR > 0 {
		adrValue := currentADR * currentPrice / 100
		distanceFrom10EMA = abs(currentPrice-ema10) / adrValue
		distanceFrom20EMA = abs(currentPrice-ema20) / adrValue
	}
	isNearEMA1020 := distanceFrom10EMA <= NEAR_EMA_ADR_THRESHOLD || distanceFrom20EMA <= NEAR_EMA_ADR_THRESHOLD

	breaksResistance := false
	if len(bars) >= 20 {
		recent20 := bars[len(bars)-20:]
		recentHigh := 0.0
		for i, bar := range recent20 {
			if i < len(recent20)-1 && bar.High > recentHigh {
				recentHigh = bar.High
			}
		}
		breaksResistance = currentPrice > recentHigh
	}

	volumeDriedUp := false
	if len(bars) >= 60 {
		last20 := bars[len(bars)-20:]
		last60 := bars[len(bars)-60:]
		avg20vol := calcAvgVolFromBars(last20)
		avg60vol := calcAvgVolFromBars(last60)
		volumeDriedUp = avg20vol < avg60vol
		LogDebug("S3", symbol, "VolumeDriedUp: avg20=%.0f  avg60=%.0f  dried=%v",
			avg20vol, avg60vol, volumeDriedUp)
	}

	LogDebug("S3", symbol, "EMA calc: currentPrice=$%.2f  EMA200=$%.2f  EMA50=$%.2f  above200=%v  dist50=%.2f",
		currentPrice, ema200, ema50, currentPrice > ema200, distanceFrom50EMA)

	return &TechnicalIndicators{
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
		VolumeDriedUp:     volumeDriedUp,
	}, nil
}

// calcAvgVolFromBars computes mean volume across a slice of AlpacaBar.
func calcAvgVolFromBars(bars []AlpacaBar) float64 {
	if len(bars) == 0 {
		return 0
	}
	sum := 0.0
	for _, b := range bars {
		sum += b.Volume
	}
	return sum / float64(len(bars))
}

func calculateHistoricalMetricsRealtime(bars []AlpacaBar) (float64, float64, error) {
	sort.Slice(bars, func(i, j int) bool {
		return bars[i].Timestamp < bars[j].Timestamp
	})

	est, _ := time.LoadLocation("America/New_York")
	today := time.Now().In(est).Format("2006-01-02")

	if len(bars) > 0 && strings.HasPrefix(bars[len(bars)-1].Timestamp, today) {
		bars = bars[:len(bars)-1]
	}

	if len(bars) < 21 {
		return 0, 0, fmt.Errorf("insufficient data: need at least 21 days")
	}

	recentBars := bars[len(bars)-21:]
	var dolVolSum, adrSum float64
	validDays := 0

	for _, bar := range recentBars {
		if bar.Close <= 0 || bar.Volume <= 0 || bar.High <= 0 || bar.Low <= 0 {
			continue
		}
		dolVolSum += bar.Volume * bar.Close
		adrSum += ((bar.High - bar.Low) / bar.Low) * 100
		validDays++
	}

	if validDays == 0 {
		return 0, 0, fmt.Errorf("no valid data")
	}

	return dolVolSum / float64(validDays), adrSum / float64(validDays), nil
}

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

func calculateEMAFromSlice(data []float64, period int) float64 {
	if len(data) < period {
		return 0
	}
	multiplier := 2.0 / float64(period+1)
	ema := calculateSMAFromSlice(data[:period], period)
	for i := period; i < len(data); i++ {
		ema = (data[i]-ema)*multiplier + ema
	}
	return ema
}

func estimateMarketCapFromRealtime(stock RealtimeStockData) float64 {
	currentPrice := stock.PremarketClose
	if currentPrice <= 0 {
		currentPrice = stock.PreviousClose
	}
	volume := stock.PremarketVolume
	if volume <= 0 {
		volume = 1_000_000
	}
	return currentPrice * volume * 100
}

func getMarketStatus(t time.Time) string {
	hour, minute := t.Hour(), t.Minute()
	switch {
	case hour < 4:
		return "CLOSED"
	case hour < 9:
		return "PREMARKET"
	case hour == 9 && minute < 30:
		return "LATE_PREMARKET"
	case hour < 16:
		return "OPEN"
	case hour < 20:
		return "AFTERHOURS"
	default:
		return "CLOSED"
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Output
// ─────────────────────────────────────────────────────────────────────────────

func outputRealtimeResults(config AlpacaConfig, stocks []RealtimeResult, scanTime time.Time) error {
	stockDir := "data/stockdata"
	if err := os.MkdirAll(stockDir, 0755); err != nil {
		return fmt.Errorf("error creating directories: %v", err)
	}

	todayDate := scanTime.Format("2006-01-02")
	for i, stock := range stocks {
		fmt.Printf("[OUTPUT] [%d/%d] Fetching deep reports for %s...\n", i+1, len(stocks), stock.Symbol)

		deepEarnings, earningsErr := getDeepEarningsHistory(config.FinnhubKey, stock.Symbol, todayDate)
		if earningsErr != nil {
			fmt.Printf("⚠️  [OUTPUT] Could not fetch earnings for %s: %v\n", stock.Symbol, earningsErr)
		} else {
			stocks[i].StockInfo.PreviousEarningsReaction = deepEarnings.OverallSentiment
		}

		newsArticles, newsErr := getPreGapNews(config.FinnhubKey, stock.Symbol, todayDate)
		if newsErr != nil {
			fmt.Printf("⚠️  [OUTPUT] Could not fetch news for %s: %v\n", stock.Symbol, newsErr)
		}

		if err := writeEarningsReport(stock.Symbol, deepEarnings); err != nil {
			fmt.Printf("⚠️  [OUTPUT] Could not write earnings report for %s: %v\n", stock.Symbol, err)
		}
		if err := writeNewsReport(stock.Symbol, newsArticles); err != nil {
			fmt.Printf("⚠️  [OUTPUT] Could not write news report for %s: %v\n", stock.Symbol, err)
		}
	}

	dateStr := scanTime.Format("20060102_150405")
	jsonFile := filepath.Join(stockDir, fmt.Sprintf("scan_%s_results.json", dateStr))

	// Split stocks into confident and questionable for the summary counts
	confidentCount, questionableCount := 0, 0
	for _, s := range stocks {
		if s.Status == "confident" {
			confidentCount++
		} else {
			questionableCount++
		}
	}

	output := map[string]interface{}{
		"scan_time":     scanTime.Format(time.RFC3339),
		"market_status": getMarketStatus(scanTime),
		"filter_criteria": map[string]interface{}{
			"min_gap_up_percent":             MIN_GAP_UP_PERCENT,
			"min_dollar_volume":              MIN_DOLLAR_VOLUME,
			"min_market_cap":                 MIN_MARKET_CAP,
			"min_premarket_volume_ratio":     MIN_PREMARKET_VOL_RATIO,
			"min_premarket_vol_pct_of_daily": MIN_PREMARKET_VOL_PCT_OF_DAILY,
			"max_extension_adr":              MAX_EXTENSION_ADR,
			"near_ema_adr_threshold":         NEAR_EMA_ADR_THRESHOLD,
		},
		"qualifying_stocks": stocks,
		"summary": map[string]interface{}{
			"total_candidates":   len(stocks),
			"confident_count":    confidentCount,
			"questionable_count": questionableCount,
			"avg_gap_up":         calculateAvgGapUpRealtime(stocks),
		},
	}

	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonFile, jsonData, 0644); err != nil {
		return err
	}
	LogInfo("OUT", "JSON results written to %s", jsonFile)

	latestFile := filepath.Join(stockDir, "filtered_stocks_latest.json")
	if err := os.WriteFile(latestFile, jsonData, 0644); err != nil {
		LogWarn("OUT", "", "Could not write latest file: %v", err)
	}

	if err := outputRealtimeCSVSummary(stocks, stockDir, dateStr); err != nil {
		LogWarn("OUT", "", "Could not create CSV summary: %v", err)
	}

	return nil
}

func outputRealtimeCSVSummary(stocks []RealtimeResult, dir, dateStr string) error {
	filename := filepath.Join(dir, fmt.Sprintf("scan_%s_summary.csv", dateStr))
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	headers := []string{
		"Symbol", "Name", "Exchange", "Sector", "Industry",
		"Status",
		"Gap Up %", "Gap Used Price", "Market Cap", "Dollar Volume", "ADR %",
		"Premarket Volume", "Avg Premarket Volume", "Premarket Vol Ratio", "Premarket Vol % Daily",
		"SMA200", "EMA200", "EMA50", "EMA20", "EMA10",
		"Above 200 EMA", "Dist 50 EMA (ADRs)", "Extended", "Too Extended",
		"Near EMA 10/20", "Breaks Resistance", "Volume Dried Up",
		"Data Quality", "Validation Notes",
	}
	writer.Write(headers)

	for _, stock := range stocks {
		notes := strings.Join(stock.ValidationNotes, "; ")
		if notes == "" {
			notes = "None"
		}
		s := stock.StockInfo
		row := []string{
			stock.Symbol, s.Name, s.Exchange, s.Sector, s.Industry,
			stock.Status,
			fmt.Sprintf("%.2f", s.GapUp),
			fmt.Sprintf("%.2f", stock.GapUsedPrice),
			fmt.Sprintf("%.0f", s.MarketCap),
			fmt.Sprintf("%.0f", s.DolVol),
			fmt.Sprintf("%.2f", s.ADR),
			fmt.Sprintf("%.0f", s.PremarketVolume),
			fmt.Sprintf("%.0f", s.AvgPremarketVolume),
			fmt.Sprintf("%.2f", s.PremarketVolumeRatio),
			fmt.Sprintf("%.2f", s.PremarketVolAsPercent),
			fmt.Sprintf("%.2f", s.SMA200),
			fmt.Sprintf("%.2f", s.EMA200),
			fmt.Sprintf("%.2f", s.EMA50),
			fmt.Sprintf("%.2f", s.EMA20),
			fmt.Sprintf("%.2f", s.EMA10),
			boolToString(s.IsAbove200EMA),
			fmt.Sprintf("%.2f", s.DistanceFrom50EMA),
			boolToString(s.IsExtended),
			boolToString(s.IsTooExtended),
			boolToString(s.IsNearEMA1020),
			boolToString(s.BreaksResistance),
			boolToString(s.VolumeDriedUp),
			stock.DataQuality,
			notes,
		}
		writer.Write(row)
	}

	LogInfo("OUT", "CSV summary written to %s", filename)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

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

// ─────────────────────────────────────────────────────────────────────────────
// Public convenience functions
// ─────────────────────────────────────────────────────────────────────────────

func RunEpisodicPivotRealtimeScanner(apiKey, apiSecret, finnhubKey string, isPaper bool) error {
	baseURL := "https://api.alpaca.markets"
	if isPaper {
		baseURL = "https://paper-api.alpaca.markets"
	}
	config := AlpacaConfig{
		APIKey:     apiKey,
		APISecret:  apiSecret,
		BaseURL:    baseURL,
		DataURL:    "https://data.alpaca.markets",
		IsPaper:    isPaper,
		FinnhubKey: finnhubKey,
	}
	return FilterStocksEpisodicPivotRealtime(config)
}

func RunContinuousScanner(apiKey, apiSecret, finnhubKey string, isPaper bool, intervalMinutes int) error {
	LogInfo("CONTINUOUS", "Starting continuous scanner (every %d minutes)", intervalMinutes)

	ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
	defer ticker.Stop()

	if err := RunEpisodicPivotRealtimeScanner(apiKey, apiSecret, finnhubKey, isPaper); err != nil {
		LogError("CONTINUOUS", "", "Error in initial scan: %v", err)
	}

	for range ticker.C {
		est, _ := time.LoadLocation("America/New_York")
		now := time.Now().In(est)
		hour := now.Hour()

		if hour >= 4 && hour < 9 {
			LogInfo("CONTINUOUS", "Running scheduled scan at %s EST", now.Format("15:04:05"))
			if err := RunEpisodicPivotRealtimeScanner(apiKey, apiSecret, finnhubKey, isPaper); err != nil {
				LogError("CONTINUOUS", "", "Error in scan: %v", err)
			}
		} else {
			LogInfo("CONTINUOUS", "Outside 4:00–9:00am scan window (%s EST) — skipping",
				now.Format("15:04:05"))
		}
	}

	return nil
}
