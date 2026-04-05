package ep

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// SimulationConfig
// ─────────────────────────────────────────────────────────────────────────────

// SimulationConfig holds parameters for a premarket replay simulation.
// Set SimulateAtTime to replay the scanner as if it were running at that
// exact moment — e.g. "07:30" to see what the 7:30am scan would have found.
type SimulationConfig struct {
	AlpacaKey      string
	AlpacaSecret   string
	FinnhubKey     string
	Date           string // "2026-03-23" — must be today or a past date
	SimulateAtTime string // "07:30" — HH:MM EST, must be between 04:00–09:30
	LookbackDays   int    // days of historical data for technicals (default 300)
}

// SimulatedPremarketSnapshot holds the aggregated premarket state for one
// symbol as it would have appeared at SimulateAtTime.
type SimulatedPremarketSnapshot struct {
	Symbol          string
	PreviousClose   float64
	PremarketOpen   float64 // open of the first 1-min bar after 4:00am
	PremarketHigh   float64 // session high up to SimulateAtTime
	PremarketLow    float64 // session low up to SimulateAtTime
	PremarketClose  float64 // close of the last bar up to SimulateAtTime
	PremarketVolume float64 // cumulative volume up to SimulateAtTime
	GapUsedPrice    float64 // PremarketHigh — same heuristic as live scanner
	GapUpPercent    float64
}

type BacktestReport struct {
	BacktestConfig   BacktestConfig   `json:"backtest_config"`
	BacktestDate     string           `json:"backtest_date"`
	BacktestSummary  BacktestSummary  `json:"backtest_summary"`
	FilterCriteria   FilterCriteria   `json:"filter_criteria"`
	GeneratedAt      string           `json:"generated_at"`
	QualifyingStocks []BacktestResult `json:"qualifying_stocks"`
}

type BacktestSummary struct {
	AvgGapUp                float64        `json:"avg_gap_up"`
	AvgMarketCap            float64        `json:"avg_market_cap"`
	DataQualityDistribution map[string]int `json:"data_quality_distribution"`
	TotalCandidates         int            `json:"total_candidates"`
}

type FilterCriteria struct {
	MaxExtensionAdr         float64 `json:"max_extension_adr"`
	MinDollarVolume         int64   `json:"min_dollar_volume"`
	MinGapUpPercent         float64 `json:"min_gap_up_percent"`
	MinMarketCap            int64   `json:"min_market_cap"`
	MinPremarketVolumeRatio float64 `json:"min_premarket_volume_ratio"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Entry point
// ─────────────────────────────────────────────────────────────────────────────

// RunPremarketSimulation fetches today's actual premarket 1-min bars for every
// symbol, replays them up to SimulateAtTime, then feeds the result through the
// same four filter stages as the live scanner.
func RunPremarketSimulation(simConfig SimulationConfig) error {
	logDir := "data/simulations/logs"
	logger, err := InitLogger(logDir, fmt.Sprintf("sim_%s_%s",
		strings.ReplaceAll(simConfig.Date, "-", ""),
		strings.ReplaceAll(simConfig.SimulateAtTime, ":", "")))
	if err != nil {
		fmt.Printf("⚠️  Could not create log file: %v\n", err)
	} else {
		defer logger.Close()
	}

	if simConfig.LookbackDays == 0 {
		simConfig.LookbackDays = 300
	}

	// Parse and validate the simulated scan time
	simulatedAt, err := parseSimTime(simConfig.Date, simConfig.SimulateAtTime)
	if err != nil {
		return fmt.Errorf("invalid simulation time: %v", err)
	}

	LogSection(fmt.Sprintf("PREMARKET SIMULATION — %s at %s EST",
		simConfig.Date, simConfig.SimulateAtTime))
	LogInfo("SIM", "Simulating scanner state as of %s EST", simulatedAt.Format("2006-01-02 15:04:05"))
	LogInfo("SIM", "Lookback days: %d", simConfig.LookbackDays)

	// Build an AlpacaConfig so we can reuse all existing API helpers
	alpacaConfig := AlpacaConfig{
		APIKey:     simConfig.AlpacaKey,
		APISecret:  simConfig.AlpacaSecret,
		BaseURL:    "https://paper-api.alpaca.markets",
		DataURL:    "https://data.alpaca.markets",
		FinnhubKey: simConfig.FinnhubKey,
	}

	// ── Stage 1: fetch all symbols, replay premarket, find gap-ups ────────
	LogSection(fmt.Sprintf("STAGE 1 — Simulated Gap Up Filter (min %.0f%%)", MIN_GAP_UP_PERCENT))
	t0 := time.Now()
	gapUpStocks, err := simStage1GapUp(alpacaConfig, simConfig, simulatedAt)
	if err != nil {
		return fmt.Errorf("simulation Stage 1 failed: %v", err)
	}
	LogStageSummary("S1", len(gapUpStocks), len(gapUpStocks), time.Since(t0))

	if len(gapUpStocks) == 0 {
		LogWarn("SIM", "", "No gap-up stocks found for %s at %s", simConfig.Date, simConfig.SimulateAtTime)
		return nil
	}

	// ── Stages 2–4: identical logic to the live scanner ──────────────────
	// We convert SimulatedPremarketSnapshot → RealtimeStockData so we can
	// pass directly into the existing stage functions.
	realtimeStocks := snapshotsToRealtimeStockData(gapUpStocks)

	LogSection(fmt.Sprintf("STAGE 2 — Liquidity Filter (min market cap $%dM)", MIN_MARKET_CAP/1_000_000))
	t0 = time.Now()
	liquidStocks, err := simStage2Liquidity(alpacaConfig, realtimeStocks)
	if err != nil {
		return fmt.Errorf("simulation Stage 2 failed: %v", err)
	}
	LogStageSummary("S2", len(liquidStocks), len(realtimeStocks), time.Since(t0))

	LogSection("STAGE 3 — Technical Analysis")
	t0 = time.Now()
	technicalStocks, err := simStage3Technical(alpacaConfig, simConfig, liquidStocks)
	if err != nil {
		return fmt.Errorf("simulation Stage 3 failed: %v", err)
	}
	LogStageSummary("S3", len(technicalStocks), len(liquidStocks), time.Since(t0))

	LogSection("STAGE 4 — Final Episodic Pivot Criteria")
	t0 = time.Now()
	finalStocks := realtimeStage4Final(technicalStocks) // reuse identical Stage 4
	LogStageSummary("S4", len(finalStocks), len(technicalStocks), time.Since(t0))

	LogSection("Saving Simulation Results")
	if err := outputSimulationResults(simConfig, finalStocks, simulatedAt); err != nil {
		return fmt.Errorf("failed to write simulation results: %v", err)
	}

	LogSection(fmt.Sprintf("SIMULATION COMPLETE — %d qualifying stocks at %s EST",
		len(finalStocks), simConfig.SimulateAtTime))
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Simulation Stage 1: replay 1-min bars up to SimulateAtTime
// ─────────────────────────────────────────────────────────────────────────────

func simStage1GapUp(config AlpacaConfig, simConfig SimulationConfig, simulatedAt time.Time) ([]SimulatedPremarketSnapshot, error) {
	symbols, err := getAlpacaTradableSymbolsMain(config)
	if err != nil {
		return nil, fmt.Errorf("failed to get symbols: %v", err)
	}
	LogInfo("S1", "Fetched %d tradable symbols", len(symbols))

	var results []SimulatedPremarketSnapshot
	processed := 0
	qualified := 0

	// Sequential rather than concurrent — simulation is run offline so
	// speed is less critical, and this avoids hammering the API.
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	for _, sym := range symbols {
		<-rateLimiter
		processed++
		if processed%100 == 0 {
			LogProgress("S1", processed, len(symbols),
				fmt.Sprintf("%d qualified so far", qualified))
		}

		snap, err := buildPremarketSnapshot(config, sym, simConfig.Date, simulatedAt)
		if err != nil {
			LogDebug("S1", sym, "Snapshot error: %v", err)
			continue
		}
		if snap == nil {
			continue
		}

		LogDebug("S1", sym, "PrevClose=$%.2f  PMHigh=$%.2f  PMVol=%.0f  Gap=%.2f%%  GapPrice=$%.2f",
    snap.PreviousClose, snap.PremarketHigh, snap.PremarketVolume, snap.GapUpPercent, snap.GapUsedPrice)

		if snap.GapUpPercent >= MIN_GAP_UP_PERCENT {
			qualified++
			LogQualify("S1", sym, fmt.Sprintf("Gap=%.2f%%  PrevClose=$%.2f  PMHigh=$%.2f  PMVol=%.0f",
				snap.GapUpPercent, snap.PreviousClose, snap.PremarketHigh, snap.PremarketVolume))
			results = append(results, *snap)
		} else {
			LogReject("S1", sym, fmt.Sprintf("Gap=%.2f%% < %.0f%%", snap.GapUpPercent, MIN_GAP_UP_PERCENT))
		}
	}

	LogInfo("S1", "Processed %d symbols — %d qualified", processed, qualified)
	return results, nil
}

// buildPremarketSnapshot fetches all 1-min bars between 4:00am and simulatedAt
// for a single symbol on simConfig.Date and aggregates them into a snapshot.
func buildPremarketSnapshot(config AlpacaConfig, symbol, date string, simulatedAt time.Time) (*SimulatedPremarketSnapshot, error) {
	// Previous close: last daily bar strictly before the simulation date
	prevClose, err := simGetPreviousClose(config, symbol, date)
	if err != nil || prevClose <= 0 {
		return nil, fmt.Errorf("no previous close: %v", err)
	}

	// Premarket 1-min bars: 4:00am EST → simulatedAt
	bars, err := fetchPremarketMinuteBars(config, symbol, date, simulatedAt)
	if err != nil || len(bars) == 0 {
		return nil, fmt.Errorf("no premarket bars: %v", err)
	}

	// Aggregate
	open := bars[0].Open
	high := bars[0].High
	low := bars[0].Low
	var totalVol float64
	for _, b := range bars {
		totalVol += b.Volume
		if b.High > high {
			high = b.High
		}
		if b.Low < low {
			low = b.Low
		}
	}
	lastClose := bars[len(bars)-1].Close

	gapPrice := high // same heuristic as the live scanner
	gapUp := ((gapPrice - prevClose) / prevClose) * 100

	return &SimulatedPremarketSnapshot{
		Symbol:          symbol,
		PreviousClose:   prevClose,
		PremarketOpen:   open,
		PremarketHigh:   high,
		PremarketLow:    low,
		PremarketClose:  lastClose,
		PremarketVolume: totalVol,
		GapUsedPrice:    gapPrice,
		GapUpPercent:    gapUp,
	}, nil
}

// fetchPremarketMinuteBars pulls 1-min bars from 4:00am EST to simulatedAt.
func fetchPremarketMinuteBars(config AlpacaConfig, symbol, date string, simulatedAt time.Time) ([]AlpacaBar, error) {
	startStr := fmt.Sprintf("%sT04:00:00-05:00", date)
	endStr := simulatedAt.UTC().Format(time.RFC3339)

	url := fmt.Sprintf(
		"%s/v2/stocks/%s/bars?timeframe=1Min&start=%s&end=%s&limit=1000&feed=sip",
		config.DataURL, symbol, startStr, endStr)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("APCA-API-KEY-ID", config.APIKey)
	req.Header.Set("APCA-API-SECRET-KEY", config.APISecret)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d", resp.StatusCode)
	}

	var barsResp AlpacaSingleSymbolBarsResponse
	if err := json.Unmarshal(body, &barsResp); err != nil {
		return nil, fmt.Errorf("parse error: %v", err)
	}

	return barsResp.Bars, nil
}

// simGetPreviousClose returns the close of the last daily bar strictly before date.
func simGetPreviousClose(config AlpacaConfig, symbol, date string) (float64, error) {
	targetDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0, err
	}

	startDate := targetDate.AddDate(0, 0, -7).Format("2006-01-02")
	// end one day before target so we never pick up the target day itself
	endDate := targetDate.AddDate(0, 0, -1).Format("2006-01-02")

	url := fmt.Sprintf(
		"%s/v2/stocks/%s/bars?timeframe=1Day&start=%s&end=%s&limit=5&adjustment=split&feed=sip",
		config.DataURL, symbol, startDate, endDate)

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
		return 0, fmt.Errorf("no bars found")
	}

	// Return the most recent bar (already filtered to before target date)
	sort.Slice(barsResp.Bars, func(i, j int) bool {
		return barsResp.Bars[i].Timestamp < barsResp.Bars[j].Timestamp
	})
	return barsResp.Bars[len(barsResp.Bars)-1].Close, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Simulation Stage 2: liquidity (reuses live logic)
// ─────────────────────────────────────────────────────────────────────────────

func simStage2Liquidity(config AlpacaConfig, stocks []RealtimeStockData) ([]RealtimeResult, error) {
	// Identical to realtimeStage2Liquidity — reuse it directly.
	return realtimeStage2Liquidity(config, stocks)
}

// ─────────────────────────────────────────────────────────────────────────────
// Simulation Stage 3: technical analysis against historical data up to date
// ─────────────────────────────────────────────────────────────────────────────

func simStage3Technical(config AlpacaConfig, simConfig SimulationConfig, stocks []RealtimeResult) ([]RealtimeResult, error) {
	var technicalStocks []RealtimeResult
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	LogInfo("S3", "Analyzing technical indicators for %d stocks (sim date: %s)", len(stocks), simConfig.Date)

	for i, stock := range stocks {
		LogDebug("S3", stock.Symbol, "[%d/%d] Fetching historical data up to %s",
			i+1, len(stocks), simConfig.Date)
		<-rateLimiter

		// Fetch bars strictly up to (but not including) the simulation date
		// so there is zero lookahead bias — same guarantee as the backtest.
		historicalData, err := simGetHistoricalBarsUpToDate(config, stock.Symbol, simConfig.Date, simConfig.LookbackDays)
		if err != nil {
			LogWarn("S3", stock.Symbol, "Historical data error: %v", err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Historical data error: %v", err))
			continue
		}

		if len(historicalData) < 200 {
			LogReject("S3", stock.Symbol,
				fmt.Sprintf("Insufficient data — %d days (need 200+)", len(historicalData)))
			stock.DataQuality = "Poor"
			continue
		}

		// Convert []AlpacaBarData → []AlpacaBar so we can reuse the realtime helpers
		bars := barDataToBars(historicalData)

		technicalIndicators, err := calculateTechnicalIndicatorsRealtime(bars, stock.Symbol, stock.GapUsedPrice)
		if err != nil {
			LogWarn("S3", stock.Symbol, "Technical indicators error: %v", err)
			stock.ValidationNotes = append(stock.ValidationNotes, err.Error())
			continue
		}

		dolVol, adr, err := calculateHistoricalMetricsRealtime(bars)
		if err != nil {
			LogWarn("S3", stock.Symbol, "Metrics error: %v", err)
			stock.ValidationNotes = append(stock.ValidationNotes, err.Error())
			continue
		}

		// Premarket volume metrics: use the snapshot data already stored on the stock
		premarketVol := stock.StockInfo.PremarketVolume // set during Stage 2 from snapshot
		avgDailyVol := calcAvgVolFromBars(bars)
		avgPremarketVol := avgDailyVol * 0.12
		pmRatio := 0.0
		if avgPremarketVol > 0 {
			pmRatio = premarketVol / avgPremarketVol
		}
		pmPctOfDaily := 0.0
		if avgDailyVol > 0 {
			pmPctOfDaily = (premarketVol / avgDailyVol) * 100
		}

		stock.StockInfo.DolVol = dolVol
		stock.StockInfo.ADR = adr
		stock.StockInfo.AvgPremarketVolume = avgPremarketVol
		stock.StockInfo.PremarketVolumeRatio = pmRatio
		stock.StockInfo.PremarketVolAsPercent = pmPctOfDaily
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

		LogMetrics("S3", stock.Symbol, map[string]interface{}{
			"gap_used_price":       fmt.Sprintf("$%.2f", stock.GapUsedPrice),
			"above_200_ema":        technicalIndicators.IsAbove200EMA,
			"adr_pct":              fmt.Sprintf("%.2f%%", adr),
			"dist_from_50_ema_adr": fmt.Sprintf("%.2f", technicalIndicators.DistanceFrom50EMA),
			"dol_vol_m":            fmt.Sprintf("$%.2fM", dolVol/1_000_000),
			"ema_200":              fmt.Sprintf("$%.2f", technicalIndicators.EMA200),
			"is_extended":          technicalIndicators.IsExtended,
			"is_near_ema_10_20":    technicalIndicators.IsNearEMA1020,
			"pm_vol_ratio":         fmt.Sprintf("%.2fx", pmRatio),
			"volume_dried_up":      technicalIndicators.VolumeDriedUp,
		})

		technicalStocks = append(technicalStocks, stock)
		LogQualify("S3", stock.Symbol, "Passed technical analysis")
	}

	return technicalStocks, nil
}

// simGetHistoricalBarsUpToDate fetches daily bars ending strictly before date.
// This is the simulation equivalent of getHistoricalDataUpToDateAlpaca in the
// backtest, ensuring zero lookahead bias.
func simGetHistoricalBarsUpToDate(config AlpacaConfig, symbol, date string, lookbackDays int) ([]AlpacaBarData, error) {
	target, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, err
	}

	startDate := target.AddDate(0, 0, -(lookbackDays + 100)).Format("2006-01-02")
	// end is the day BEFORE target so we never include the gap day
	endDate := target.AddDate(0, 0, -1).Format("2006-01-02")

	url := fmt.Sprintf(
		"%s/v2/stocks/%s/bars?timeframe=1Day&start=%s&end=%s&limit=1000&adjustment=split&feed=sip",
		config.DataURL, symbol, startDate, endDate)

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

	var barsResp AlpacaBarsResponse
	if err := json.Unmarshal(body, &barsResp); err != nil {
		return nil, fmt.Errorf("parse error: %v", err)
	}

	var out []AlpacaBarData
	for _, b := range barsResp.Bars {
		out = append(out, AlpacaBarData{
			Symbol:    symbol,
			Timestamp: b.Timestamp,
			Open:      b.Open,
			High:      b.High,
			Low:       b.Low,
			Close:     b.Close,
			Volume:    b.Volume,
		})
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Output
// ─────────────────────────────────────────────────────────────────────────────

func outputSimulationResults(simConfig SimulationConfig, stocks []RealtimeResult, simulatedAt time.Time) error {
	outDir := filepath.Join("data", "backtests")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("failed to create output dir: %v", err)
	}

	// Per-ticker reports for qualifying stocks
	for i, stock := range stocks {
		fmt.Printf("[SIM OUTPUT] [%d/%d] Fetching deep reports for %s...\n",
			i+1, len(stocks), stock.Symbol)

		deepEarnings, err := getDeepEarningsHistory(simConfig.FinnhubKey, stock.Symbol, simConfig.Date)
		if err != nil {
			fmt.Printf("⚠️  Could not fetch earnings for %s: %v\n", stock.Symbol, err)
		} else {
			stocks[i].StockInfo.PreviousEarningsReaction = deepEarnings.OverallSentiment
		}

		newsArticles, err := getPreGapNews(simConfig.FinnhubKey, stock.Symbol, simConfig.Date)
		if err != nil {
			fmt.Printf("⚠️  Could not fetch news for %s: %v\n", stock.Symbol, err)
		}

		if deepEarnings != nil {
			_ = writeEarningsReport(stock.Symbol, deepEarnings)
		}
		if newsArticles != nil {
			_ = writeNewsReport(stock.Symbol, newsArticles)
		}
	}

	// Build qualifying stocks in BacktestResult format
	qualifyingStocks := make([]BacktestResult, len(stocks))
	dataQualityDist := make(map[string]int)
	var totalMarketCap float64

	for i, s := range stocks {
		dq := s.DataQuality
		if dq == "" {
			dq = "Good"
		}
		dataQualityDist[dq]++
		totalMarketCap += s.StockInfo.MarketCap

		qualifyingStocks[i] = BacktestResult{
			FilteredStock: FilteredStock{
				Symbol: s.Symbol,
				StockInfo: StockStats{
					Timestamp:                s.StockInfo.Timestamp,
					MarketCap:                s.StockInfo.MarketCap,
					DolVol:                   s.StockInfo.DolVol,
					GapUp:                    s.StockInfo.GapUp,
					ADR:                      s.StockInfo.ADR,
					Name:                     s.StockInfo.Name,
					Exchange:                 s.StockInfo.Exchange,
					Sector:                   s.StockInfo.Sector,
					Industry:                 s.StockInfo.Industry,
					PremarketVolume:          s.StockInfo.PremarketVolume,
					AvgPremarketVolume:       s.StockInfo.AvgPremarketVolume,
					PremarketVolumeRatio:     s.StockInfo.PremarketVolumeRatio,
					PremarketVolAsPercent:    s.StockInfo.PremarketVolAsPercent,
					SMA200:                   s.StockInfo.SMA200,
					EMA200:                   s.StockInfo.EMA200,
					EMA50:                    s.StockInfo.EMA50,
					EMA20:                    s.StockInfo.EMA20,
					EMA10:                    s.StockInfo.EMA10,
					IsAbove200EMA:            s.StockInfo.IsAbove200EMA,
					DistanceFrom50EMA:        s.StockInfo.DistanceFrom50EMA,
					IsExtended:               s.StockInfo.IsExtended,
					IsTooExtended:            s.StockInfo.IsTooExtended,
					VolumeDriedUp:            s.StockInfo.VolumeDriedUp,
					IsNearEMA1020:            s.StockInfo.IsNearEMA1020,
					BreaksResistance:         s.StockInfo.BreaksResistance,
					PreviousEarningsReaction: s.StockInfo.PreviousEarningsReaction,
				},
			},
			BacktestDate:    simConfig.Date,
			DataQuality:     dq,
			HistoricalDays:  simConfig.LookbackDays,
			ValidationNotes: s.ValidationNotes,
		}
	}

	avgMarketCap := 0.0
	if len(stocks) > 0 {
		avgMarketCap = totalMarketCap / float64(len(stocks))
	}

	output := BacktestReport{
		BacktestConfig: BacktestConfig{
			LookbackDays: simConfig.LookbackDays,
			TargetDate:   simConfig.Date,
		},
		BacktestDate: simConfig.Date,
		BacktestSummary: BacktestSummary{
			AvgGapUp:                calculateAvgGapUpRealtime(stocks),
			AvgMarketCap:            avgMarketCap,
			DataQualityDistribution: dataQualityDist,
			TotalCandidates:         len(stocks),
		},
		FilterCriteria: FilterCriteria{
			MaxExtensionAdr:         MAX_EXTENSION_ADR,
			MinDollarVolume:         int64(MIN_DOLLAR_VOLUME),
			MinGapUpPercent:         MIN_GAP_UP_PERCENT,
			MinMarketCap:            int64(MIN_MARKET_CAP),
			MinPremarketVolumeRatio: MIN_PREMARKET_VOL_RATIO,
		},
		GeneratedAt:      time.Now().Format(time.RFC3339),
		QualifyingStocks: qualifyingStocks,
	}

	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}

	backtestDate := strings.ReplaceAll(simConfig.Date, "-", "")
	jsonFile := filepath.Join(outDir, fmt.Sprintf("backtest_%s_results.json", backtestDate))
	if err := os.WriteFile(jsonFile, jsonData, 0644); err != nil {
		return err
	}
	LogInfo("OUT", "Simulation results written to %s", jsonFile)

	if err := outputRealtimeCSVSummary(stocks, outDir, fmt.Sprintf("backtest_%s", backtestDate)); err != nil {
		LogWarn("OUT", "", "Could not write simulation CSV: %v", err)
	} else {
		LogInfo("OUT", "Simulation CSV written to %s",
			filepath.Join(outDir, fmt.Sprintf("backtest_%s_summary.csv", backtestDate)))
	}

	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Conversion helpers
// ─────────────────────────────────────────────────────────────────────────────

// snapshotsToRealtimeStockData converts simulation snapshots into the
// RealtimeStockData type expected by realtimeStage2Liquidity.
func snapshotsToRealtimeStockData(snaps []SimulatedPremarketSnapshot) []RealtimeStockData {
	out := make([]RealtimeStockData, len(snaps))
	for i, s := range snaps {
		out[i] = RealtimeStockData{
			Symbol:          s.Symbol,
			CurrentPrice:    s.PremarketClose,
			PremarketOpen:   s.PremarketOpen,
			PremarketHigh:   s.PremarketHigh,
			PremarketLow:    s.PremarketLow,
			PremarketClose:  s.PremarketClose,
			PremarketVolume: s.PremarketVolume,
			PreviousClose:   s.PreviousClose,
			GapUpPercent:    s.GapUpPercent,
			GapUsedPrice:    s.GapUsedPrice,
		}
	}
	return out
}

// barDataToBars converts []AlpacaBarData (backtest type) to []AlpacaBar
// (realtime type) so the realtime indicator functions can be reused.
func barDataToBars(data []AlpacaBarData) []AlpacaBar {
	out := make([]AlpacaBar, len(data))
	for i, d := range data {
		out[i] = AlpacaBar{
			Timestamp: d.Timestamp,
			Open:      d.Open,
			High:      d.High,
			Low:       d.Low,
			Close:     d.Close,
			Volume:    d.Volume,
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Time helpers
// ─────────────────────────────────────────────────────────────────────────────

// parseSimTime builds a time.Time for date + HH:MM in America/New_York.
func parseSimTime(date, clockTime string) (time.Time, error) {
	est, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.Time{}, err
	}

	combined := fmt.Sprintf("%s %s", date, clockTime)
	t, err := time.ParseInLocation("2006-01-02 15:04", combined, est)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot parse '%s %s': %v", date, clockTime, err)
	}

	h, m := t.Hour(), t.Minute()
	if h < 4 || (h == 9 && m > 30) || h > 9 {
		return time.Time{}, fmt.Errorf("SimulateAtTime %s is outside the 04:00–09:30 premarket window", clockTime)
	}

	return t, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Public convenience function
// ─────────────────────────────────────────────────────────────────────────────

// RunTodaySimulation simulates the premarket scanner for today at the given time.
// Example: RunTodaySimulation(key, secret, fhKey, "07:30")
func RunTodaySimulation(alpacaKey, alpacaSecret, finnhubKey, simulateAtTime string) error {
	est, _ := time.LoadLocation("America/New_York")
	today := time.Now().In(est).Format("2006-01-02")

	return RunPremarketSimulation(SimulationConfig{
		AlpacaKey:      alpacaKey,
		AlpacaSecret:   alpacaSecret,
		FinnhubKey:     finnhubKey,
		Date:           today,
		SimulateAtTime: simulateAtTime,
		LookbackDays:   300,
	})
}

// RunHistoricalSimulation simulates the premarket scanner for a past date.
// Example: RunHistoricalSimulation(key, secret, fhKey, "2026-03-20", "07:30")
func RunHistoricalSimulation(alpacaKey, alpacaSecret, finnhubKey, date, simulateAtTime string) error {
	return RunPremarketSimulation(SimulationConfig{
		AlpacaKey:      alpacaKey,
		AlpacaSecret:   alpacaSecret,
		FinnhubKey:     finnhubKey,
		Date:           date,
		SimulateAtTime: simulateAtTime,
		LookbackDays:   300,
	})
}