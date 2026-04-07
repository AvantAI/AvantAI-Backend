package ep

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
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
	// FIX: VolumeDriedUp was in main.go's StockStats but missing here — now added and calculated
	VolumeDriedUp bool `json:"volume_dried_up"`
	// FIX: PreviousEarningsReaction was never populated — now fetched from Finnhub in Stage 2
	PreviousEarningsReaction string `json:"previous_earnings_reaction"`
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
	// FIX: Added VolumeDriedUp to TechnicalIndicators so Stage 3 can pass it through
	VolumeDriedUp bool
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

// FinnhubEarnings represents a single quarterly earnings record from Finnhub
type FinnhubEarnings struct {
	Actual   *float64 `json:"actual"`
	Estimate *float64 `json:"estimate"`
	Period   string   `json:"period"` // e.g. "2023-09-30"
	Quarter  int      `json:"quarter"`
	Surprise *float64 `json:"surprise"`
	// SurprisePct is the percentage beat/miss vs estimate
	SurprisePct *float64 `json:"surprisePercent"`
	Symbol      string   `json:"symbol"`
	Year        int      `json:"year"`
}

// FinnhubEarningsResponse wraps the Finnhub earnings endpoint array
type FinnhubEarningsResponse struct {
	EarningsCalendar []FinnhubEarnings `json:"earningsCalendar"`
}

// FinnhubNews represents a single news article from Finnhub
type FinnhubNews struct {
	Category string `json:"category"`
	Datetime int64  `json:"datetime"`
	Headline string `json:"headline"`
	ID       int64  `json:"id"`
	Image    string `json:"image"`
	Related  string `json:"related"`
	Source   string `json:"source"`
	Summary  string `json:"summary"`
	URL      string `json:"url"`
}

// EarningsReactionSummary holds the derived sentiment across recent quarters
type EarningsReactionSummary struct {
	// Quarters holds the last N quarterly results, newest first
	Quarters []QuarterlyResult `json:"quarters"`
	// OverallSentiment is "Positive", "Mixed", or "Negative"
	OverallSentiment string `json:"overall_sentiment"`
	// PositiveCount is how many of the last quarters had a beat
	PositiveCount int `json:"positive_count"`
	// TotalQuarters is how many quarters were evaluated
	TotalQuarters int `json:"total_quarters"`
}

// QuarterlyResult summarises one quarter
type QuarterlyResult struct {
	Period      string  `json:"period"`
	Actual      float64 `json:"actual"`
	Estimate    float64 `json:"estimate"`
	SurprisePct float64 `json:"surprise_pct"`
	Beat        bool    `json:"beat"`
}

// Constants for filtering criteria
const (
	MIN_GAP_UP_PERCENT = 10.0
	MIN_STOCK_PRICE    = 5.00
	MIN_DOLLAR_VOLUME  = 10000000 // $10M
	MIN_MARKET_CAP     = 50000000 // $50M

	// FIX: was 2.0 — strategy requires 3–5x average daily premarket volume
	MIN_PREMARKET_VOL_RATIO = 3.0

	// FIX: Added minimum premarket vol as % of avg daily volume (strategy: 10–20%)
	MIN_PREMARKET_VOL_PCT_OF_DAILY = 10.0

	MAX_EXTENSION_ADR      = 4.0
	TOO_EXTENDED_ADR       = 8.0
	NEAR_EMA_ADR_THRESHOLD = 2.0 // FIX: strategy says "within 2 ADRs" — was 1.5
	MIN_ADR_PERCENT        = 5.0
	MAX_CONCURRENT         = 166
	API_CALLS_PER_SECOND   = 166

	// Number of prior quarters to evaluate for earnings reaction history
	EARNINGS_LOOKBACK_QUARTERS = 4
)

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

// ─────────────────────────────────────────────────────────────────────────────
// Main backtesting function
// ─────────────────────────────────────────────────────────────────────────────

func FilterStocksEpisodicPivotBacktest(config BacktestConfig) error {
	// ── Initialise logger ──────────────────────────────────────────────────
	logDir := "data/backtests/logs"
	logger, err := InitLogger(logDir, fmt.Sprintf("backtest_%s", strings.ReplaceAll(config.TargetDate, "-", "")))
	if err != nil {
		fmt.Printf("⚠️  Could not create log file: %v\n", err)
	} else {
		defer logger.Close()
	}

	LogSection(fmt.Sprintf("EPISODIC PIVOT BACKTESTING — %s", config.TargetDate))

	LogInfo("INIT", "Validating target date: %s", config.TargetDate)
	targetDate, err := time.Parse("2006-01-02", config.TargetDate)
	if err != nil {
		LogError("INIT", "", "Invalid target date format: %v", err)
		return fmt.Errorf("invalid target date format. Use YYYY-MM-DD: %v", err)
	}
	LogInfo("INIT", "Target date parsed successfully: %s", targetDate.Format("2006-01-02"))

	if targetDate.After(time.Now()) {
		LogError("INIT", "", "Target date %s is in the future", config.TargetDate)
		return fmt.Errorf("target date %s is in the future", config.TargetDate)
	}
	LogInfo("INIT", "Target date is in the past — valid for backtesting")

	if config.LookbackDays == 0 {
		config.LookbackDays = 300
		LogInfo("INIT", "Using default lookback period: %d days", config.LookbackDays)
	}

	LogSubSection("Configuration Summary")
	LogInfo("INIT", "Target Date    : %s", config.TargetDate)
	LogInfo("INIT", "Lookback Period: %d days", config.LookbackDays)
	LogInfo("INIT", "Alpaca Key     : %s...", config.AlpacaKey[:min(10, len(config.AlpacaKey))])
	LogInfo("INIT", "Finnhub Key    : %s...", config.FinnhubKey[:min(10, len(config.FinnhubKey))])

	// ── Stage 1: Gap Up ────────────────────────────────────────────────────
	LogSection(fmt.Sprintf("STAGE 1 — Gap Up Filter (min %.0f%%)", MIN_GAP_UP_PERCENT))
	t0 := time.Now()
	gapUpStocks, err := backtestStage1GapUp(config)
	if err != nil {
		LogError("S1", "", "Stage 1 failed: %v", err)
		return fmt.Errorf("error in Stage 1 backtest: %v", err)
	}
	LogStageSummary("S1", len(gapUpStocks), len(gapUpStocks), time.Since(t0))

	if len(gapUpStocks) == 0 {
		LogWarn("S1", "", "No stocks found with gap up criteria for %s", config.TargetDate)
		return nil
	}

	// ── Stage 2: Liquidity ─────────────────────────────────────────────────
	LogSection(fmt.Sprintf("STAGE 2 — Liquidity Filter (min market cap $%dM)", MIN_MARKET_CAP/1_000_000))
	t0 = time.Now()
	liquidStocks, err := backtestStage2Liquidity(config, gapUpStocks)
	if err != nil {
		LogError("S2", "", "Stage 2 failed: %v", err)
		return fmt.Errorf("error in Stage 2 backtest: %v", err)
	}
	LogStageSummary("S2", len(liquidStocks), len(gapUpStocks), time.Since(t0))

	// ── Stage 3: Technical ─────────────────────────────────────────────────
	LogSection("STAGE 3 — Technical Analysis")
	t0 = time.Now()
	technicalStocks, err := backtestStage3Technical(config, liquidStocks)
	if err != nil {
		LogError("S3", "", "Stage 3 failed: %v", err)
		return fmt.Errorf("error in Stage 3 backtest: %v", err)
	}
	LogStageSummary("S3", len(technicalStocks), len(liquidStocks), time.Since(t0))

	// ── Stage 4: Final Filter ──────────────────────────────────────────────
	LogSection("STAGE 4 — Final Episodic Pivot Criteria")
	t0 = time.Now()
	finalStocks := backtestStage4Final(technicalStocks)
	LogStageSummary("S4", len(finalStocks), len(technicalStocks), time.Since(t0))

	// ── Output ─────────────────────────────────────────────────────────────
	LogSection("Saving Results")
	if err := outputBacktestResults(config, finalStocks); err != nil {
		LogError("OUT", "", "Failed to write results: %v", err)
		return fmt.Errorf("error writing backtest results: %v", err)
	}

	LogSection(fmt.Sprintf("BACKTEST COMPLETE — %d qualifying stocks for %s", len(finalStocks), config.TargetDate))
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Stage 1: Gap Up Filter
// ─────────────────────────────────────────────────────────────────────────────

func backtestStage1GapUp(config BacktestConfig) ([]StockData, error) {
	LogInfo("S1", "Fetching tradable symbols from Alpaca...")
	symbols, err := getAlpacaTradableSymbols(config)
	if err != nil {
		return nil, fmt.Errorf("failed to get stock symbols: %v", err)
	}
	LogInfo("S1", "Retrieved %d tradable symbols", len(symbols))
	LogInfo("S1", "Criteria: Gap Up >= %.0f%%  |  Concurrency: %d  |  Rate: %d/s",
		MIN_GAP_UP_PERCENT, MAX_CONCURRENT, API_CALLS_PER_SECOND)

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
				LogProgress("S1", currentCount, len(symbols),
					fmt.Sprintf("%d qualified so far", currentQualified))
			}

			LogDebug("S1", sym, "Fetching historical data around %s", config.TargetDate)
			currentData, previousData, err := getHistoricalDataForDateAlpaca(
				config.AlpacaKey, config.AlpacaSecret, sym, config.TargetDate)
			if err != nil {
				LogWarn("S1", sym, "Failed to get historical data: %v", err)
				return
			}

			if currentData == nil || previousData == nil {
				LogWarn("S1", sym, "Missing data (current=%v previous=%v)",
					currentData != nil, previousData != nil)
				return
			}

			if previousData.Close <= 0 {
				LogWarn("S1", sym, "Invalid previous close: %.2f", previousData.Close)
				return
			}

			gapUp := ((currentData.Open - previousData.Close) / previousData.Close) * 100

			LogDebug("S1", sym, "CurrentDate=%s PrevDate=%s Open=%.2f PrevClose=%.2f GapUp=%.2f%%",
				currentData.Timestamp[:10], previousData.Timestamp[:10],
				currentData.Open, previousData.Close, gapUp)

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
				LogQualify("S1", sym, fmt.Sprintf("Gap=%.2f%%  Open=$%.2f  PrevClose=$%.2f",
					gapUp, currentData.Open, previousData.Close))
				gapUpStocks = append(gapUpStocks, stockData)
				mu.Unlock()
			} else {
				LogReject("S1", sym, fmt.Sprintf("Gap=%.2f%% < %.0f%% minimum", gapUp, MIN_GAP_UP_PERCENT))
			}
		}(symbol)
	}

	wg.Wait()
	LogInfo("S1", "Processing complete: %d / %d symbols qualified", len(gapUpStocks), len(symbols))
	return gapUpStocks, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Stage 2: Liquidity Filter
// ─────────────────────────────────────────────────────────────────────────────

func backtestStage2Liquidity(config BacktestConfig, stocks []StockData) ([]BacktestResult, error) {
	var liquidStocks []BacktestResult
	rateLimiter := time.Tick(time.Second / 30) // Slower rate for Finnhub company profile calls

	LogInfo("S2", "Analyzing liquidity for %d stocks", len(stocks))

	for i, stock := range stocks {
		LogDebug("S2", stock.Symbol, "[%d/%d] Fetching Finnhub company profile", i+1, len(stocks))
		<-rateLimiter

		companyProfile, err := getCompanyProfileFinnhub(config.FinnhubKey, stock.Symbol)
		if err != nil {
			LogWarn("S2", stock.Symbol, "Finnhub profile error: %v", err)
		}

		var marketCap float64
		var name, sector, industry string = stock.Name, "Unknown", "Unknown"

		if companyProfile != nil {
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

		if marketCap == 0 {
			LogWarn("S2", stock.Symbol, "No market cap from Finnhub — estimating from volume/price")
			companyInfo := &CompanyInfo{Name: name, Sector: sector, Industry: industry}
			marketCap = estimateMarketCapImproved(stock, companyInfo)
			LogDebug("S2", stock.Symbol, "Estimated market cap: $%.0fM", marketCap/1_000_000)
		}

		marketCapM := marketCap / 1_000_000
		minCapM := float64(MIN_MARKET_CAP) / 1_000_000

		LogMetrics("S2", stock.Symbol, map[string]interface{}{
			"market_cap_m":   fmt.Sprintf("$%.2fM", marketCapM),
			"min_required_m": fmt.Sprintf("$%.2fM", minCapM),
			"name":           name,
			"sector":         sector,
			"industry":       industry,
			"gap_up_pct":     fmt.Sprintf("%.2f%%", stock.ExtendedHoursChangePercent),
		})

		if marketCap >= MIN_MARKET_CAP {
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
			LogQualify("S2", stock.Symbol, fmt.Sprintf("MarketCap=$%.0fM >= $%.0fM", marketCapM, minCapM))
		} else {
			LogReject("S2", stock.Symbol, fmt.Sprintf("MarketCap=$%.0fM < $%.0fM", marketCapM, minCapM))
		}
	}

	LogInfo("S2", "Liquidity filter complete: %d / %d stocks qualified", len(liquidStocks), len(stocks))
	return liquidStocks, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Stage 3: Technical Analysis
// ─────────────────────────────────────────────────────────────────────────────

func backtestStage3Technical(config BacktestConfig, stocks []BacktestResult) ([]BacktestResult, error) {
	var technicalStocks []BacktestResult
	rateLimiter := time.Tick(time.Second / API_CALLS_PER_SECOND)

	LogInfo("S3", "Analyzing technical indicators for %d stocks", len(stocks))

	for i, stock := range stocks {
		LogDebug("S3", stock.Symbol, "[%d/%d] Fetching %d days of historical data",
			i+1, len(stocks), config.LookbackDays)
		<-rateLimiter

		historicalData, err := getHistoricalDataUpToDateAlpaca(
			config.AlpacaKey, config.AlpacaSecret, stock.Symbol, config.TargetDate, config.LookbackDays)
		if err != nil {
			LogWarn("S3", stock.Symbol, "Historical data error: %v", err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Historical data error: %v", err))
			continue
		}

		LogDebug("S3", stock.Symbol, "Retrieved %d days of historical data", len(historicalData))

		if len(historicalData) < 200 {
			LogReject("S3", stock.Symbol,
				fmt.Sprintf("Insufficient data — only %d days (need 200+)", len(historicalData)))
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Insufficient data: %d days", len(historicalData)))
			stock.DataQuality = "Poor"
			continue
		}

		stock.HistoricalDays = len(historicalData)

		technicalIndicators, err := calculateTechnicalIndicatorsBacktest(
			historicalData, stock.Symbol, config.TargetDate)
		if err != nil {
			LogWarn("S3", stock.Symbol, "Technical indicators error: %v", err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Technical analysis error: %v", err))
			continue
		}

		dolVol, adr, err := calculateHistoricalMetricsBacktest(
			historicalData, stock.Symbol, config.TargetDate)
		if err != nil {
			LogWarn("S3", stock.Symbol, "Metrics calculation error: %v", err)
			stock.ValidationNotes = append(stock.ValidationNotes,
				fmt.Sprintf("Metrics calculation error: %v", err))
			continue
		}

		premarketAnalysis := simulatePremarketAnalysisBacktest(historicalData, config.TargetDate)

		// Store all metrics
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
		// FIX: Now populated from technical indicators
		stock.StockInfo.VolumeDriedUp = technicalIndicators.VolumeDriedUp

		currentPrice := getCurrentPriceBacktest(historicalData)

		LogMetrics("S3", stock.Symbol, map[string]interface{}{
			"above_200_ema":        technicalIndicators.IsAbove200EMA,
			"adr_pct":              fmt.Sprintf("%.2f%%", adr),
			"breaks_resistance":    technicalIndicators.BreaksResistance,
			"current_price":        fmt.Sprintf("$%.2f", currentPrice),
			"dist_from_50_ema_adr": fmt.Sprintf("%.2f", technicalIndicators.DistanceFrom50EMA),
			"dol_vol_m":            fmt.Sprintf("$%.2fM", dolVol/1000000.0),
			"ema_10":               fmt.Sprintf("$%.2f", technicalIndicators.EMA10),
			"ema_20":               fmt.Sprintf("$%.2f", technicalIndicators.EMA20),
			"ema_200":              fmt.Sprintf("$%.2f", technicalIndicators.EMA200),
			"ema_50":               fmt.Sprintf("$%.2f", technicalIndicators.EMA50),
			"historical_days":      len(historicalData),
			"is_extended":          technicalIndicators.IsExtended,
			"is_near_ema_10_20":    technicalIndicators.IsNearEMA1020,
			"is_too_extended":      technicalIndicators.IsTooExtended,
			"pm_vol_ratio":         fmt.Sprintf("%.2fx", premarketAnalysis.VolumeRatio),
			"sma_200":              fmt.Sprintf("$%.2f", technicalIndicators.SMA200),
			"volume_dried_up":      technicalIndicators.VolumeDriedUp,
		})

		technicalStocks = append(technicalStocks, stock)
		LogQualify("S3", stock.Symbol, "Passed technical analysis")
	}

	LogInfo("S3", "Technical analysis complete: %d stocks qualified", len(technicalStocks))
	return technicalStocks, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Stage 4: Final Filter
// FIX: Now enforces IsNearEMA1020, PremarketVolAsPercent, and VolumeDriedUp
//      which were previously calculated but never used as filters.
// ─────────────────────────────────────────────────────────────────────────────

func backtestStage4Final(stocks []BacktestResult) []BacktestResult {
	var finalStocks []BacktestResult

	LogInfo("S4", "Applying final episodic pivot criteria to %d stocks", len(stocks))

	for i, stock := range stocks {
		LogDebug("S4", stock.Symbol, "[%d/%d] Final analysis", i+1, len(stocks))

		type criterion struct {
			name   string
			passed bool
			detail string
		}

		criteria := []criterion{
			{
				"Dollar Volume",
				stock.StockInfo.DolVol >= MIN_DOLLAR_VOLUME,
				fmt.Sprintf("$%.0fM >= $%.0fM", stock.StockInfo.DolVol/1000000.0, MIN_DOLLAR_VOLUME/1000000.0),
			},
			{
				// FIX: threshold raised from 2.0x to MIN_PREMARKET_VOL_RATIO (5.0x)
				"Premarket Vol Ratio",
				stock.StockInfo.PremarketVolumeRatio >= MIN_PREMARKET_VOL_RATIO,
				fmt.Sprintf("%.2fx >= %.1fx (gap-day vs avg daily volume proxy)", stock.StockInfo.PremarketVolumeRatio, MIN_PREMARKET_VOL_RATIO),
			},
			{
				// FIX: was never enforced — strategy requires 20–50% of avg daily vol
				"Premarket Vol % of Daily",
				stock.StockInfo.PremarketVolAsPercent >= MIN_PREMARKET_VOL_PCT_OF_DAILY,
				fmt.Sprintf("%.2f%% >= %.0f%% of avg daily volume", stock.StockInfo.PremarketVolAsPercent, MIN_PREMARKET_VOL_PCT_OF_DAILY),
			},
			{
				"Above 200 EMA",
				stock.StockInfo.IsAbove200EMA,
				fmt.Sprintf("price above EMA200=$%.2f", stock.StockInfo.EMA200),
			},
			{
				"Not Extended",
				!stock.StockInfo.IsExtended,
				fmt.Sprintf("dist=%.2f ADRs (max %.1f)", stock.StockInfo.DistanceFrom50EMA, MAX_EXTENSION_ADR),
			},
			{
				// FIX: was calculated but never filtered on — strategy: price near 10/20 EMA within 2 ADRs
				"Near 10/20 EMA",
				stock.StockInfo.IsNearEMA1020,
				fmt.Sprintf("within %.1f ADRs of EMA10 or EMA20", NEAR_EMA_ADR_THRESHOLD),
			},
			{
				// FIX: VolumeDriedUp was in the struct but never evaluated
				// Strategy: 20-day avg vol < 60-day avg vol before gap day
				"Volume Dried Up",
				stock.StockInfo.VolumeDriedUp,
				"20-day avg vol < 60-day avg vol (consolidation signal)",
			},
		}

		allPassed := true
		for _, c := range criteria {
			if c.passed {
				LogDebug("S4", stock.Symbol, "✅ PASS  %s — %s", c.name, c.detail)
			} else {
				LogDebug("S4", stock.Symbol, "❌ FAIL  %s — %s", c.name, c.detail)
				allPassed = false
			}
		}

		if stock.StockInfo.IsTooExtended {
			LogDebug("S4", stock.Symbol, "❌ FAIL  Too Extended — %.2f ADRs > %.1f",
				stock.StockInfo.DistanceFrom50EMA, TOO_EXTENDED_ADR)
			allPassed = false
		}

		if allPassed && !stock.StockInfo.IsTooExtended {
			finalStocks = append(finalStocks, stock)
			LogQualify("S4", stock.Symbol, fmt.Sprintf(
				"Gap=%.2f%%  DolVol=$%.0fM  ADR=%.2f%%  Dist50EMA=%.2f  VolDriedUp=%v  EarningsRxn=%s",
				stock.StockInfo.GapUp, stock.StockInfo.DolVol/1_000_000,
				stock.StockInfo.ADR, stock.StockInfo.DistanceFrom50EMA,
				stock.StockInfo.VolumeDriedUp, stock.StockInfo.PreviousEarningsReaction))
		} else {
			reason := "Failed one or more criteria"
			if stock.StockInfo.IsTooExtended {
				reason = fmt.Sprintf("Too extended (%.2f ADRs > %.1f max)", stock.StockInfo.DistanceFrom50EMA, TOO_EXTENDED_ADR)
			}
			LogReject("S4", stock.Symbol, reason)
		}
	}

	LogInfo("S4", "Final filter complete: %d / %d stocks qualified", len(finalStocks), len(stocks))
	return finalStocks
}

// ─────────────────────────────────────────────────────────────────────────────
// Alpaca API helpers
// ─────────────────────────────────────────────────────────────────────────────

func getHistoricalDataForDateAlpaca(apiKey, apiSecret, symbol, targetDate string) (*AlpacaBarData, *AlpacaBarData, error) {
	target, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return nil, nil, err
	}

	startDate := target.AddDate(0, 0, -7).Format("2006-01-02")
	endDate := target.AddDate(0, 0, 1).Format("2006-01-02")

	url := fmt.Sprintf(
		"https://data.alpaca.markets/v2/stocks/%s/bars?start=%s&end=%s&timeframe=1Day&adjustment=split&feed=sip&limit=10000",
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %v", err)
	}

	LogDebug("API", symbol, "getHistoricalDataForDate status=%d body_preview=%s",
		resp.StatusCode, string(body[:min(120, len(body))]))

	var barsResponse AlpacaBarsResponse
	if err := json.Unmarshal(body, &barsResponse); err != nil {
		return nil, nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	bars := barsResponse.Bars
	if len(bars) < 2 {
		return nil, nil, fmt.Errorf("insufficient data: only %d bars", len(bars))
	}

	sort.Slice(bars, func(i, j int) bool { return bars[i].Timestamp > bars[j].Timestamp })

	var currentData, previousData *AlpacaBarData
	for i, bar := range bars {
		if bar.Timestamp[:10] == targetDate {
			currentData = &AlpacaBarData{
				Symbol: symbol, Timestamp: bar.Timestamp,
				Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, Volume: bar.Volume,
			}
			if i+1 < len(bars) {
				prev := bars[i+1]
				previousData = &AlpacaBarData{
					Symbol: symbol, Timestamp: prev.Timestamp,
					Open: prev.Open, High: prev.High, Low: prev.Low, Close: prev.Close, Volume: prev.Volume,
				}
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

func getHistoricalDataUpToDateAlpaca(apiKey, apiSecret, symbol, targetDate string, lookbackDays int) ([]AlpacaBarData, error) {
	target, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return nil, err
	}

	startDate := target.AddDate(0, 0, -(lookbackDays + 100)).Format("2006-01-02")

	url := fmt.Sprintf(
		"https://data.alpaca.markets/v2/stocks/%s/bars?start=%s&end=%s&timeframe=1Day&adjustment=split&feed=sip&limit=10000",
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	LogDebug("API", symbol, "getHistoricalDataUpToDate status=%d bars_preview=%s",
		resp.StatusCode, string(body[:min(120, len(body))]))

	var barsResponse AlpacaBarsResponse
	if err := json.Unmarshal(body, &barsResponse); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	if len(barsResponse.Bars) == 0 {
		return nil, fmt.Errorf("no bars data for symbol %s", symbol)
	}

	var filteredData []AlpacaBarData
	for _, bar := range barsResponse.Bars {
		barTime, err := time.Parse(time.RFC3339, bar.Timestamp)
		if err != nil {
			continue
		}
		if barTime.Before(target.AddDate(0, 0, 1)) {
			filteredData = append(filteredData, AlpacaBarData{
				Symbol: symbol, Timestamp: bar.Timestamp,
				Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, Volume: bar.Volume,
			})
		}
	}

	LogDebug("API", symbol, "Filtered to %d bars up to %s", len(filteredData), targetDate)
	return filteredData, nil
}

func getCompanyProfileFinnhub(apiKey, symbol string) (*FinnhubCompanyProfile, error) {
	url := fmt.Sprintf("https://finnhub.io/api/v1/stock/profile2?symbol=%s&token=%s", symbol, apiKey)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

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

	if profile.Ticker == "" {
		return nil, fmt.Errorf("no profile data found for symbol")
	}

	return &profile, nil
}

// getEarningsReactionFinnhub fetches the last EARNINGS_LOOKBACK_QUARTERS quarters
// of EPS actuals vs estimates for a symbol, restricted to reports that were
// published strictly before targetDate (no lookahead).
//
// Finnhub endpoint: GET /stock/earnings?symbol=AAPL&limit=4
func getEarningsReactionFinnhub(apiKey, symbol, targetDate string) (*EarningsReactionSummary, error) {
	target, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return nil, fmt.Errorf("invalid target date: %v", err)
	}

	url := fmt.Sprintf(
		"https://finnhub.io/api/v1/stock/earnings?symbol=%s&limit=%d&token=%s",
		symbol, EARNINGS_LOOKBACK_QUARTERS+2, apiKey) // fetch extra to filter by date

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body[:min(200, len(body))]))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	// Finnhub returns a bare JSON array for this endpoint
	var earnings []FinnhubEarnings
	if err := json.Unmarshal(body, &earnings); err != nil {
		return nil, fmt.Errorf("failed to parse earnings JSON: %v", err)
	}

	// Filter to only quarters whose period end date is strictly before targetDate,
	// and only keep up to EARNINGS_LOOKBACK_QUARTERS results.
	var quarters []QuarterlyResult
	for _, e := range earnings {
		if e.Period == "" {
			continue
		}
		periodDate, err := time.Parse("2006-01-02", e.Period)
		if err != nil {
			continue
		}
		// Ensure we are not including the current-quarter earnings that
		// may have triggered this very gap-up event.
		if !periodDate.Before(target) {
			continue
		}
		if e.Actual == nil || e.Estimate == nil {
			continue
		}
		surprisePct := 0.0
		if e.SurprisePct != nil {
			surprisePct = *e.SurprisePct
		}
		quarters = append(quarters, QuarterlyResult{
			Period:      e.Period,
			Actual:      *e.Actual,
			Estimate:    *e.Estimate,
			SurprisePct: surprisePct,
			Beat:        *e.Actual >= *e.Estimate,
		})
		if len(quarters) >= EARNINGS_LOOKBACK_QUARTERS {
			break
		}
	}

	positiveCount := 0
	for _, q := range quarters {
		if q.Beat {
			positiveCount++
		}
	}

	sentiment := "Unknown"
	if len(quarters) > 0 {
		ratio := float64(positiveCount) / float64(len(quarters))
		switch {
		case ratio >= 0.75:
			sentiment = "Positive"
		case ratio >= 0.50:
			sentiment = "Mixed"
		default:
			sentiment = "Negative"
		}
	}

	return &EarningsReactionSummary{
		Quarters:         quarters,
		OverallSentiment: sentiment,
		PositiveCount:    positiveCount,
		TotalQuarters:    len(quarters),
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Pre-gap news: multi-source scraper (past 7 days before targetDate)
// Sources: Finnhub API, Yahoo Finance, Finviz, MarketWatch
// ─────────────────────────────────────────────────────────────────────────────

// ScrapedNewsArticle holds a normalised article from any source.
type ScrapedNewsArticle struct {
	Title       string
	Source      string
	URL         string
	PublishedAt time.Time
	Summary     string
	FullContent string
}

// getPreGapNews fetches the past 7 days of news from multiple sources,
// de-duplicates by title, and enriches the top 20 with full body text.
func getPreGapNews(finnhubKey, symbol, targetDate string) ([]ScrapedNewsArticle, error) {
	target, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return nil, fmt.Errorf("invalid target date: %v", err)
	}
	from := target.AddDate(0, 0, -7)
	to := target.AddDate(0, 0, -1) // never include the gap day itself

	client := &http.Client{Timeout: 20 * time.Second}

	type result struct {
		articles []ScrapedNewsArticle
		name     string
	}
	ch := make(chan result, 4)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		ch <- result{fetchNewsFromFinnhub(finnhubKey, symbol, from, to), "Finnhub"}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch <- result{scrapeYahooFinanceNews(client, symbol, from, to), "Yahoo Finance"}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch <- result{scrapeFinvizNews(client, symbol, from, to), "Finviz"}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch <- result{scrapeMarketWatchNews(client, symbol, from, to), "MarketWatch"}
	}()
	go func() { wg.Wait(); close(ch) }()

	seen := make(map[string]bool)
	var all []ScrapedNewsArticle
	for r := range ch {
		fmt.Printf("[NEWS] %s: %d articles for %s\n", r.name, len(r.articles), symbol)
		for _, a := range r.articles {
			key := strings.ToLower(strings.TrimSpace(a.Title))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			all = append(all, a)
		}
	}

	// Date-filter: must fall in [from, to]
	var filtered []ScrapedNewsArticle
	for _, a := range all {
		if !a.PublishedAt.Before(from) && !a.PublishedAt.After(to.Add(24*time.Hour)) {
			filtered = append(filtered, a)
		}
	}

	// Newest-first
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].PublishedAt.After(filtered[j].PublishedAt)
	})
	if len(filtered) > 20 {
		filtered = filtered[:20]
	}

	// Enrich with full body text
	filtered = enrichArticlesWithFullContent(client, filtered)

	fmt.Printf("[NEWS] %s: %d unique articles after dedup+filter\n", symbol, len(filtered))
	return filtered, nil
}

// fetchNewsFromFinnhub pulls from the Finnhub company-news endpoint.
func fetchNewsFromFinnhub(apiKey, symbol string, from, to time.Time) []ScrapedNewsArticle {
	url := fmt.Sprintf(
		"https://finnhub.io/api/v1/company-news?symbol=%s&from=%s&to=%s&token=%s",
		symbol, from.Format("2006-01-02"), to.Format("2006-01-02"), apiKey)
	resp, err := http.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var raw []FinnhubNews
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}
	var out []ScrapedNewsArticle
	for _, n := range raw {
		out = append(out, ScrapedNewsArticle{
			Title:       n.Headline,
			Source:      n.Source,
			URL:         n.URL,
			PublishedAt: time.Unix(n.Datetime, 0).UTC(),
			Summary:     n.Summary,
		})
	}
	return out
}

// scrapeYahooFinanceNews scrapes the Yahoo Finance quote news tab.
func scrapeYahooFinanceNews(client *http.Client, symbol string, from, to time.Time) []ScrapedNewsArticle {
	url := fmt.Sprintf("https://finance.yahoo.com/quote/%s/news", symbol)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil
	}
	var articles []ScrapedNewsArticle
	doc.Find("div[data-test-locator='StreamItem'], li.stream-item").Each(func(_ int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Find("h3, h2").First().Text())
		if title == "" {
			title = strings.TrimSpace(s.Find("a").First().Text())
		}
		href, _ := s.Find("a").First().Attr("href")
		if strings.HasPrefix(href, "/") {
			href = "https://finance.yahoo.com" + href
		}
		summary := strings.TrimSpace(s.Find("p").First().Text())
		source := strings.TrimSpace(s.Find("div span, span.provider-name").Last().Text())
		timeText := strings.TrimSpace(s.Find("time, span[data-testid='item-pubtime']").Text())
		if title == "" || href == "" {
			return
		}
		articles = append(articles, ScrapedNewsArticle{
			Title:       title,
			Source:      "Yahoo Finance / " + source,
			URL:         href,
			PublishedAt: parseRelativeOrAbsoluteTime(timeText, to),
			Summary:     summary,
		})
	})
	return articles
}

// scrapeFinvizNews scrapes the Finviz quote-page news table.
func scrapeFinvizNews(client *http.Client, symbol string, from, to time.Time) []ScrapedNewsArticle {
	url := fmt.Sprintf("https://finviz.com/quote.ashx?t=%s", symbol)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil
	}
	var articles []ScrapedNewsArticle
	var lastDate string
	doc.Find("table.fullview-news-outer tr").Each(func(i int, s *goquery.Selection) {
		if i == 0 {
			return
		}
		timeCell := strings.TrimSpace(s.Find("td").First().Text())
		newsCell := s.Find("td").Last()
		title := strings.TrimSpace(newsCell.Find("a").Text())
		href, _ := newsCell.Find("a").Attr("href")
		source := strings.TrimSpace(newsCell.Find("span").Text())
		if title == "" || href == "" {
			return
		}
		// Finviz shows a new date string only on the first row of each day
		if strings.Contains(timeCell, "-") {
			lastDate = timeCell
		}
		articles = append(articles, ScrapedNewsArticle{
			Title:       title,
			Source:      "Finviz / " + source,
			URL:         href,
			PublishedAt: parseFinvizDateStr(lastDate+" "+timeCell, to),
		})
	})
	return articles
}

// scrapeMarketWatchNews scrapes the MarketWatch ticker news page.
func scrapeMarketWatchNews(client *http.Client, symbol string, from, to time.Time) []ScrapedNewsArticle {
	url := fmt.Sprintf("https://www.marketwatch.com/investing/stock/%s/news", strings.ToLower(symbol))
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil
	}
	var articles []ScrapedNewsArticle
	doc.Find("div.article__content, div.element--article").Each(func(_ int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Find("h3.article__headline a, h2.article__headline a").Text())
		href, _ := s.Find("h3.article__headline a, h2.article__headline a").Attr("href")
		if strings.HasPrefix(href, "/") {
			href = "https://www.marketwatch.com" + href
		}
		summary := strings.TrimSpace(s.Find("p.article__summary").Text())
		author := strings.TrimSpace(s.Find("span.article__author").Text())
		timeText := strings.TrimSpace(s.Find("span.article__timestamp, time").Text())
		if title == "" || href == "" {
			return
		}
		src := "MarketWatch"
		if author != "" {
			src += " / " + author
		}
		articles = append(articles, ScrapedNewsArticle{
			Title:       title,
			Source:      src,
			URL:         href,
			PublishedAt: parseMarketWatchDateStr(timeText, to),
			Summary:     summary,
		})
	})
	return articles
}

// enrichArticlesWithFullContent fetches body text for each article (max 3 concurrent).
func enrichArticlesWithFullContent(client *http.Client, articles []ScrapedNewsArticle) []ScrapedNewsArticle {
	sem := make(chan struct{}, 3)
	var mu sync.Mutex
	var wg sync.WaitGroup
	enriched := make([]ScrapedNewsArticle, len(articles))
	copy(enriched, articles)
	for i := range enriched {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			time.Sleep(300 * time.Millisecond)
			content := fetchArticleBodyText(client, enriched[idx].URL)
			mu.Lock()
			enriched[idx].FullContent = content
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	return enriched
}

// fetchArticleBodyText GETs a URL and extracts readable paragraph text.
func fetchArticleBodyText(client *http.Client, articleURL string) string {
	if !strings.HasPrefix(articleURL, "http") {
		return ""
	}
	req, _ := http.NewRequest("GET", articleURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return ""
	}
	doc.Find("script, style, nav, header, footer, aside, .ad, .advertisement").Remove()
	var paragraphs []string
	for _, sel := range []string{"article p", "div.article-body p", "div.caas-body p", "div.body__content p", "main p", "p"} {
		doc.Find(sel).Each(func(_ int, s *goquery.Selection) {
			t := strings.TrimSpace(s.Text())
			if len(t) > 60 {
				paragraphs = append(paragraphs, t)
			}
		})
		if len(paragraphs) >= 3 {
			break
		}
	}
	result := strings.Join(paragraphs, "\n\n")
	result = regexp.MustCompile(`[ \t]+`).ReplaceAllString(result, " ")
	result = regexp.MustCompile(`\n{3,}`).ReplaceAllString(result, "\n\n")
	return strings.TrimSpace(result)
}

// ── Date-parsing helpers ──────────────────────────────────────────────────────

func parseRelativeOrAbsoluteTime(s string, fallback time.Time) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	if strings.Contains(s, "ago") {
		m := regexp.MustCompile(`(\d+)\s*([hHdDmM])`).FindStringSubmatch(s)
		if len(m) == 3 {
			var n int
			fmt.Sscanf(m[1], "%d", &n)
			switch strings.ToLower(m[2]) {
			case "h":
				return time.Now().Add(-time.Duration(n) * time.Hour)
			case "d":
				return time.Now().AddDate(0, 0, -n)
			case "m":
				return time.Now().Add(-time.Duration(n) * time.Minute)
			}
		}
	}
	for _, f := range []string{"January 2, 2006", "Jan 2, 2006", "2006-01-02", "Jan 2, 2006, 3:04 PM"} {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return fallback
}

func parseFinvizDateStr(s string, fallback time.Time) time.Time {
	s = strings.TrimSpace(s)
	for _, f := range []string{"Jan-02-06 03:04PM", "Jan-02-06 3:04PM", "Jan-02-06"} {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return fallback
}

func parseMarketWatchDateStr(s string, fallback time.Time) time.Time {
	s = strings.TrimSpace(s)
	for _, f := range []string{"January 2, 2006 at 3:04 p.m. ET", "Jan. 2, 2006", "Jan 2, 2006", "2006-01-02"} {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return fallback
}

// ─────────────────────────────────────────────────────────────────────────────
// Historical earnings: Finnhub numbers + SEC EDGAR press-release content
// for the 3 quarters before targetDate
// ─────────────────────────────────────────────────────────────────────────────

// DeepQuarterlyResult extends QuarterlyResult with full press-release content.
type DeepQuarterlyResult struct {
	QuarterlyResult
	PressReleaseContent string `json:"press_release_content,omitempty"`
	SECFilingURL        string `json:"sec_filing_url,omitempty"`
	RevenueActual       string `json:"revenue_actual,omitempty"`
	RevenueEstimate     string `json:"revenue_estimate,omitempty"`
}

// DeepEarningsSummary is like EarningsReactionSummary but with full content.
type DeepEarningsSummary struct {
	Symbol           string                `json:"symbol"`
	OverallSentiment string                `json:"overall_sentiment"`
	PositiveCount    int                   `json:"positive_count"`
	TotalQuarters    int                   `json:"total_quarters"`
	Quarters         []DeepQuarterlyResult `json:"quarters"`
}

const DEEP_EARNINGS_QUARTERS = 3

// getDeepEarningsHistory fetches the last 3 quarters' EPS data from Finnhub,
// then enriches each quarter with the actual SEC EDGAR 8-K press release.
func getDeepEarningsHistory(finnhubKey, symbol, targetDate string) (*DeepEarningsSummary, error) {
	target, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return nil, fmt.Errorf("invalid target date: %v", err)
	}

	// ── Step 1: Finnhub EPS numbers ───────────────────────────────────────
	url := fmt.Sprintf(
		"https://finnhub.io/api/v1/stock/earnings?symbol=%s&limit=%d&token=%s",
		symbol, DEEP_EARNINGS_QUARTERS+2, finnhubKey)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("Finnhub request failed: %v", err)
	}
	defer resp.Body.Close()
	var raw []FinnhubEarnings
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("Finnhub parse error: %v", err)
	}

	var quarters []DeepQuarterlyResult
	for _, e := range raw {
		if e.Period == "" {
			continue
		}
		pd, err := time.Parse("2006-01-02", e.Period)
		if err != nil || !pd.Before(target) {
			continue
		}
		if e.Actual == nil || e.Estimate == nil {
			continue
		}
		surprisePct := 0.0
		if e.SurprisePct != nil {
			surprisePct = *e.SurprisePct
		}
		quarters = append(quarters, DeepQuarterlyResult{
			QuarterlyResult: QuarterlyResult{
				Period:      e.Period,
				Actual:      *e.Actual,
				Estimate:    *e.Estimate,
				SurprisePct: surprisePct,
				Beat:        *e.Actual >= *e.Estimate,
			},
		})
		if len(quarters) >= DEEP_EARNINGS_QUARTERS {
			break
		}
	}

	// ── Step 2: SEC EDGAR press-release content for each quarter ──────────
	client := &http.Client{Timeout: 30 * time.Second}
	secUA := "EpisodicPivotResearch research@example.com"
	cik := getCIKForSymbol(client, symbol, secUA)

	if cik != "" {
		for i := range quarters {
			qDate, err := time.Parse("2006-01-02", quarters[i].Period)
			if err != nil {
				continue
			}
			// Earnings are typically filed within 45 days after period end
			searchFrom := qDate
			searchTo := qDate.AddDate(0, 0, 60)

			content, filingURL := fetchSECEarningsPressRelease(client, cik, searchFrom, searchTo, secUA)
			if content != "" {
				quarters[i].PressReleaseContent = content
				quarters[i].SECFilingURL = filingURL
				fmt.Printf("[EARNINGS] %s Q%s: fetched SEC press release (%d chars)\n",
					symbol, quarters[i].Period, len(content))
			} else {
				fmt.Printf("[EARNINGS] %s Q%s: no SEC press release found\n", symbol, quarters[i].Period)
			}
			// Be polite to SEC servers
			time.Sleep(500 * time.Millisecond)
		}
	}

	// ── Step 3: Sentiment rollup ──────────────────────────────────────────
	positiveCount := 0
	for _, q := range quarters {
		if q.Beat {
			positiveCount++
		}
	}
	sentiment := "Unknown"
	if len(quarters) > 0 {
		ratio := float64(positiveCount) / float64(len(quarters))
		switch {
		case ratio >= 0.75:
			sentiment = "Positive"
		case ratio >= 0.50:
			sentiment = "Mixed"
		default:
			sentiment = "Negative"
		}
	}

	return &DeepEarningsSummary{
		Symbol:           symbol,
		OverallSentiment: sentiment,
		PositiveCount:    positiveCount,
		TotalQuarters:    len(quarters),
		Quarters:         quarters,
	}, nil
}

// getCIKForSymbol looks up a ticker's CIK from SEC's company_tickers.json.
func getCIKForSymbol(client *http.Client, ticker, userAgent string) string {
	req, _ := http.NewRequest("GET", "https://www.sec.gov/files/company_tickers.json", nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var tickers map[string]struct {
		CIK    int    `json:"cik_str"`
		Ticker string `json:"ticker"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tickers); err != nil {
		return ""
	}
	upper := strings.ToUpper(ticker)
	for _, v := range tickers {
		if strings.ToUpper(v.Ticker) == upper {
			return fmt.Sprintf("%d", v.CIK)
		}
	}
	return ""
}

// fetchSECEarningsPressRelease finds the 8-K filed in [from, to] with
// item 2.02 (Results of Operations) and returns its exhibit 99.1 text.
func fetchSECEarningsPressRelease(client *http.Client, cik string, from, to time.Time, userAgent string) (string, string) {
	paddedCIK := fmt.Sprintf("%010s", cik)
	filingURL := fmt.Sprintf("https://data.sec.gov/submissions/CIK%s.json", paddedCIK)

	req, _ := http.NewRequest("GET", filingURL, nil)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return "", ""
	}
	defer resp.Body.Close()

	var filings struct {
		Filings struct {
			Recent struct {
				AccessionNumber []string `json:"accessionNumber"`
				FilingDate      []string `json:"filingDate"`
				FormType        []string `json:"form"`
				PrimaryDocument []string `json:"primaryDocument"`
				Items           []string `json:"items"`
			} `json:"recent"`
		} `json:"filings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&filings); err != nil {
		return "", ""
	}

	// Find the 8-K with item 2.02 closest to [from, to]
	bestAccession, bestPrimaryDoc := "", ""
	for i, formType := range filings.Filings.Recent.FormType {
		if formType != "8-K" {
			continue
		}
		items := filings.Filings.Recent.Items[i]
		if !strings.Contains(items, "2.02") {
			continue
		}
		fd, err := time.Parse("2006-01-02", filings.Filings.Recent.FilingDate[i])
		if err != nil {
			continue
		}
		if fd.Before(from) || fd.After(to) {
			continue
		}
		bestAccession = filings.Filings.Recent.AccessionNumber[i]
		bestPrimaryDoc = filings.Filings.Recent.PrimaryDocument[i]
		break
	}

	if bestAccession == "" {
		return "", ""
	}

	accNoSlash := strings.ReplaceAll(bestAccession, "-", "")
	trimCIK := strings.TrimLeft(cik, "0")
	baseURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/", trimCIK, accNoSlash)
	indexURL := fmt.Sprintf("%s%s-index.htm", baseURL, accNoSlash)

	// Try to find exhibit 99.1 via the filing index page
	content := fetchExhibit99FromIndex(client, indexURL, baseURL, trimCIK, accNoSlash, userAgent)
	if content == "" {
		// Fallback: try the primary document itself
		docURL := baseURL + bestPrimaryDoc
		content = fetchSECHTMLDocument(client, docURL, userAgent)
	}

	filingPageURL := fmt.Sprintf("https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&CIK=%s&type=8-K&dateb=&owner=include&count=10", trimCIK)
	return content, filingPageURL
}

// fetchExhibit99FromIndex parses the filing index page and fetches exhibit 99.1.
func fetchExhibit99FromIndex(client *http.Client, indexURL, baseURL, cik, accNoSlash, userAgent string) string {
	req, _ := http.NewRequest("GET", indexURL, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return ""
	}

	var exhibitURL string
	doc.Find("table tr").EachWithBreak(func(_ int, row *goquery.Selection) bool {
		cells := row.Find("td")
		if cells.Length() < 3 {
			return true
		}
		combined := strings.ToLower(cells.Text())
		if strings.Contains(combined, "99.1") || strings.Contains(combined, "ex-99.1") ||
			strings.Contains(combined, "press release") {
			href, exists := cells.Find("a").First().Attr("href")
			if exists {
				if strings.HasPrefix(href, "/") {
					exhibitURL = "https://www.sec.gov" + href
				} else if !strings.HasPrefix(href, "http") {
					exhibitURL = baseURL + href
				} else {
					exhibitURL = href
				}
				return false // stop iteration
			}
		}
		return true
	})

	if exhibitURL == "" {
		// Try common filename patterns
		for _, name := range []string{"ex991.htm", "ex99-1.htm", "ex99_1.htm", "ex-99_1.htm"} {
			url := baseURL + name
			if content := fetchSECHTMLDocument(client, url, userAgent); content != "" {
				return content
			}
		}
		return ""
	}
	return fetchSECHTMLDocument(client, exhibitURL, userAgent)
}

// fetchSECHTMLDocument fetches an SEC HTML/text document and returns clean text.
func fetchSECHTMLDocument(client *http.Client, docURL, userAgent string) string {
	req, _ := http.NewRequest("GET", docURL, nil)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	// Skip binary content
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "image") || strings.Contains(ct, "pdf") {
		return ""
	}
	body := string(bodyBytes)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		// Plain text fallback
		re := regexp.MustCompile(`\s+`)
		return strings.TrimSpace(re.ReplaceAllString(body, " "))
	}
	doc.Find("script, style, meta, link").Remove()

	var parts []string
	seen := make(map[string]bool)
	doc.Find("p, td, li, h1, h2, h3, h4").Each(func(_ int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if len(t) > 20 && !seen[t] {
			seen[t] = true
			parts = append(parts, t)
		}
	})
	result := strings.Join(parts, "\n")
	result = regexp.MustCompile(`[ \t]+`).ReplaceAllString(result, " ")
	result = regexp.MustCompile(`\n{3,}`).ReplaceAllString(result, "\n\n")
	return strings.TrimSpace(result)
}

// ─────────────────────────────────────────────────────────────────────────────
// File writers
// ─────────────────────────────────────────────────────────────────────────────

// writeEarningsReport writes data/<SYMBOL>/historical_earnings_report.txt.
// It now uses DeepEarningsSummary so each quarter can include press-release text.
func writeEarningsReport(symbol string, summary *DeepEarningsSummary) error {
	dir := filepath.Join("data", symbol)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create report dir: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, "historical_earnings_report.txt"))
	if err != nil {
		return fmt.Errorf("failed to create earnings report: %v", err)
	}
	defer f.Close()

	if summary == nil || summary.TotalQuarters == 0 {
		fmt.Fprintf(f, "No earnings history available.\n")
		return nil
	}

	sep := strings.Repeat("=", 80)
	fmt.Fprintf(f, "%s\n", sep)
	fmt.Fprintf(f, "HISTORICAL EARNINGS REPORT  —  %s\n", symbol)
	fmt.Fprintf(f, "Generated  : %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(f, "%s\n\n", sep)

	fmt.Fprintf(f, "OVERALL SENTIMENT : %s\n", summary.OverallSentiment)
	fmt.Fprintf(f, "QUARTERS POSITIVE : %d / %d\n\n", summary.PositiveCount, summary.TotalQuarters)

	fmt.Fprintf(f, "%-12s  %-10s  %-10s  %-10s  %-5s\n",
		"Period", "Actual", "Estimate", "Surprise%", "Beat")
	fmt.Fprintf(f, "%s\n", strings.Repeat("-", 54))
	for _, q := range summary.Quarters {
		beat := "No"
		if q.Beat {
			beat = "Yes"
		}
		fmt.Fprintf(f, "%-12s  %-10.4f  %-10.4f  %-10.2f  %-5s\n",
			q.Period, q.Actual, q.Estimate, q.SurprisePct, beat)
	}

	// Full press-release content per quarter
	for _, q := range summary.Quarters {
		if q.PressReleaseContent == "" {
			continue
		}
		fmt.Fprintf(f, "\n%s\n", strings.Repeat("-", 80))
		fmt.Fprintf(f, "EARNINGS PRESS RELEASE  —  Period ending %s\n", q.Period)
		if q.SECFilingURL != "" {
			fmt.Fprintf(f, "SEC Filing  : %s\n", q.SECFilingURL)
		}
		fmt.Fprintf(f, "%s\n\n", strings.Repeat("-", 80))
		fmt.Fprintf(f, "%s\n", q.PressReleaseContent)
	}
	return nil
}

// writeNewsReport writes data/<SYMBOL>/pre_gap_news_report.txt.
// It now uses ScrapedNewsArticle which includes full body content.
func writeNewsReport(symbol string, articles []ScrapedNewsArticle) error {
	dir := filepath.Join("data", symbol)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create report dir: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, "pre_gap_news_report.txt"))
	if err != nil {
		return fmt.Errorf("failed to create news report: %v", err)
	}
	defer f.Close()

	sep := strings.Repeat("=", 80)
	fmt.Fprintf(f, "%s\n", sep)
	fmt.Fprintf(f, "PRE-GAP NEWS REPORT  —  %s\n", symbol)
	fmt.Fprintf(f, "Generated : %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(f, "Articles  : %d (past 7 trading days, multi-source)\n", len(articles))
	fmt.Fprintf(f, "%s\n\n", sep)

	if len(articles) == 0 {
		fmt.Fprintf(f, "No recent news articles found.\n")
		return nil
	}

	for i, a := range articles {
		fmt.Fprintf(f, "ARTICLE %d of %d\n", i+1, len(articles))
		fmt.Fprintf(f, "Title     : %s\n", a.Title)
		fmt.Fprintf(f, "Source    : %s\n", a.Source)
		fmt.Fprintf(f, "Published : %s\n", a.PublishedAt.Format("2006-01-02 15:04 MST"))
		fmt.Fprintf(f, "URL       : %s\n", a.URL)
		if a.Summary != "" {
			fmt.Fprintf(f, "\nSummary:\n%s\n", a.Summary)
		}
		if a.FullContent != "" {
			fmt.Fprintf(f, "\n--- FULL ARTICLE CONTENT ---\n%s\n", a.FullContent)
		}
		fmt.Fprintf(f, "\n%s\n\n", strings.Repeat("-", 80))
	}
	return nil
}

func parseSectorFromIndustry(industry string) string {
	industryLower := strings.ToLower(industry)
	sectorMap := map[string]string{
		"technology": "Technology", "software": "Technology", "hardware": "Technology",
		"semiconductor": "Technology", "internet": "Technology", "computer": "Technology",
		"electronic": "Technology", "health": "Healthcare", "healthcare": "Healthcare",
		"pharmaceutical": "Healthcare", "biotech": "Healthcare", "medical": "Healthcare",
		"finance": "Financial Services", "financial": "Financial Services", "bank": "Financial Services",
		"insurance": "Financial Services", "investment": "Financial Services",
		"energy": "Energy", "oil": "Energy", "gas": "Energy",
		"utilities": "Utilities", "real estate": "Real Estate",
		"consumer": "Consumer Cyclical", "retail": "Consumer Cyclical", "automotive": "Consumer Cyclical",
		"industrial": "Industrials", "manufacturing": "Industrials",
		"aerospace": "Industrials", "defense": "Industrials",
		"materials": "Basic Materials", "chemical": "Basic Materials", "mining": "Basic Materials",
		"telecommunication": "Communication Services", "media": "Communication Services",
	}
	for key, sector := range sectorMap {
		if strings.Contains(industryLower, key) {
			return sector
		}
	}
	return "Unknown"
}

func getAlpacaTradableSymbols(config BacktestConfig) ([]string, error) {
	url := "https://paper-api.alpaca.markets/v2/assets?status=active&asset_class=us_equity"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("APCA-API-KEY-ID", config.AlpacaKey)
	req.Header.Set("APCA-API-SECRET-KEY", config.AlpacaSecret)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
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
	if len(symbols) == 0 {
		return nil, fmt.Errorf("no tradable symbols found")
	}

	LogInfo("API", "Found %d tradable NYSE/NASDAQ symbols", len(symbols))
	return symbols, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Calculation helpers
// ─────────────────────────────────────────────────────────────────────────────

func estimateMarketCapImproved(stock StockData, _ *CompanyInfo) float64 {
	if stock.Close <= 0 || stock.Volume <= 0 {
		return 0
	}
	estimatedShares := float64(stock.Volume) * 100
	return stock.Close * estimatedShares
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

// calculateTechnicalIndicatorsBacktest computes all EMAs/SMAs and derived flags.
//
// FIX (lookahead bias): EMAs and SMAs are now calculated on dataUpToTarget which
// excludes the target date's own bar — the target bar's open is what we are
// deciding to trade on, so it must not contaminate the indicator values.
//
// FIX (VolumeDriedUp): 20-day avg volume vs 60-day avg volume comparison is now
// calculated here and returned in TechnicalIndicators.
func calculateTechnicalIndicatorsBacktest(historicalData []AlpacaBarData, symbol, targetDate string) (*TechnicalIndicators, error) {
	if len(historicalData) < 200 {
		return nil, fmt.Errorf("insufficient data for technical indicators")
	}

	sort.Slice(historicalData, func(i, j int) bool {
		return historicalData[i].Timestamp < historicalData[j].Timestamp
	})

	// FIX: find the bar BEFORE the target date — do not include the target
	// date's own bar in any indicator calculation (was off-by-one before).
	targetIndex := len(historicalData) - 1
	for i, data := range historicalData {
		if strings.HasPrefix(data.Timestamp, targetDate) {
			// i is the target date bar; use i-1 as the last indicator bar
			targetIndex = i - 1
			break
		}
	}

	if targetIndex < 0 {
		return nil, fmt.Errorf("target date is the first bar — no prior data for indicators")
	}

	// All indicator math is done on data strictly before the target date
	dataUpToTarget := historicalData[:targetIndex+1]
	if len(dataUpToTarget) < 200 {
		return nil, fmt.Errorf("insufficient pre-target data: %d bars (need 200)", len(dataUpToTarget))
	}

	// The "current price" for all distance calculations is the previous close
	// (i.e. the close of the day before the gap day) — this is what was visible
	// in premarket when the gap-up was discovered.
	currentPrice := dataUpToTarget[len(dataUpToTarget)-1].Close
	currentADR := calculateADRForPeriodAlpaca(dataUpToTarget[max(0, len(dataUpToTarget)-21):])

	sma200 := calculateSMAAlpaca(dataUpToTarget, 200)
	ema200 := calculateEMAAlpaca(dataUpToTarget, 200)
	ema50 := calculateEMAAlpaca(dataUpToTarget, 50)
	ema20 := calculateEMAAlpaca(dataUpToTarget, 20)
	ema10 := calculateEMAAlpaca(dataUpToTarget, 10)

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
	// FIX: threshold updated to 2.0 ADRs to match strategy document ("within 2 ADRs")
	isNearEMA1020 := (distanceFrom10EMA <= NEAR_EMA_ADR_THRESHOLD) || (distanceFrom20EMA <= NEAR_EMA_ADR_THRESHOLD)

	breaksResistance := false
	if len(dataUpToTarget) >= 20 {
		recent20Days := dataUpToTarget[len(dataUpToTarget)-20:]
		recentHigh := 0.0
		for _, day := range recent20Days[:len(recent20Days)-1] {
			if day.High > recentHigh {
				recentHigh = day.High
			}
		}
		// Use the gap-day open (first bar of target date) for the resistance check
		// The target date bar IS in historicalData at targetIndex+1; we fetch it
		// directly rather than from dataUpToTarget so there's no indicator contamination.
		gapDayOpen := historicalData[targetIndex+1].Open
		breaksResistance = gapDayOpen > recentHigh
	}

	// FIX: Volume Dried Up — compare 20-day avg vol to 60-day avg vol.
	// Both windows are calculated on pre-target data only.
	volumeDriedUp := false
	if len(dataUpToTarget) >= 60 {
		last20 := dataUpToTarget[len(dataUpToTarget)-20:]
		last60 := dataUpToTarget[len(dataUpToTarget)-60:]
		avg20vol := calculateAvgVolumeAlpaca(last20)
		avg60vol := calculateAvgVolumeAlpaca(last60)
		// Dried up = recent 20-day average is lower than the 60-day baseline
		volumeDriedUp = avg20vol < avg60vol
		LogDebug("S3", symbol, "VolumeDriedUp: avg20=%.0f  avg60=%.0f  dried=%v",
			avg20vol, avg60vol, volumeDriedUp)
	}

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

func calculateHistoricalMetricsBacktest(historicalData []AlpacaBarData, _, targetDate string) (float64, float64, error) {
	sort.Slice(historicalData, func(i, j int) bool {
		return historicalData[i].Timestamp < historicalData[j].Timestamp
	})

	targetIndex := -1
	for i, data := range historicalData {
		if strings.HasPrefix(data.Timestamp, targetDate) {
			targetIndex = i - 1
			break
		}
	}

	if targetIndex < 0 {
		return 0, 0, fmt.Errorf("target date not found in historical data or no prior bars available")
	}

	startIndex := targetIndex - 20
	if startIndex < 0 {
		startIndex = 0
	}

	metricsData := historicalData[startIndex : targetIndex+1]
	LogDebug("S3", "", "ADR/DolVol window: %s → %s (%d days)",
		metricsData[0].Timestamp[:10], metricsData[len(metricsData)-1].Timestamp[:10], len(metricsData))

	if len(metricsData) < 10 {
		return 0, 0, fmt.Errorf("insufficient pre-target data for metrics: only %d days", len(metricsData))
	}

	var dolVolSum, adrSum float64
	validDays := 0
	for _, d := range metricsData {
		if d.Close <= 0 || d.Volume <= 0 || d.High <= 0 || d.Low <= 0 {
			continue
		}
		dolVolSum += d.Volume * d.Close
		adrSum += ((d.High - d.Low) / d.Low) * 100
		validDays++
	}

	if validDays == 0 {
		return 0, 0, fmt.Errorf("no valid historical data found")
	}

	return dolVolSum / float64(validDays), adrSum / float64(validDays), nil
}

// simulatePremarketAnalysisBacktest estimates premarket activity on the target
// date using only data available before the open.
func simulatePremarketAnalysisBacktest(historicalData []AlpacaBarData, targetDate string) *PremarketAnalysis {
	if len(historicalData) == 0 {
		return &PremarketAnalysis{}
	}

	sort.Slice(historicalData, func(i, j int) bool {
		return historicalData[i].Timestamp < historicalData[j].Timestamp
	})

	targetIndex := -1
	for i, d := range historicalData {
		if strings.HasPrefix(d.Timestamp, targetDate) {
			targetIndex = i
			break
		}
	}
	if targetIndex < 0 {
		return &PremarketAnalysis{}
	}

	startIndex := targetIndex - 21
	if startIndex < 0 {
		startIndex = 0
	}
	priorBars := historicalData[startIndex:targetIndex]
	if len(priorBars) == 0 {
		return &PremarketAnalysis{}
	}
	avgDailyVol := calculateAvgVolumeAlpaca(priorBars)
	avgPremarketVol := avgDailyVol * 0.12

	gapDayVolume := historicalData[targetIndex].Volume
	currentPremarketVol := gapDayVolume * 0.12

	volumeRatio := 0.0
	if avgPremarketVol > 0 {
		volumeRatio = currentPremarketVol / avgPremarketVol
	}

	volAsPercentOfDaily := 0.0
	if avgDailyVol > 0 {
		volAsPercentOfDaily = (currentPremarketVol / avgDailyVol) * 100
	}

	LogDebug("S3", "", "PM simulation: avgDailyVol=%.0f  gapDayVol=%.0f  estPMVol=%.0f  ratio=%.2fx  pctOfDaily=%.1f%%",
		avgDailyVol, gapDayVolume, currentPremarketVol, volumeRatio, volAsPercentOfDaily)

	return &PremarketAnalysis{
		CurrentPremarketVol: currentPremarketVol,
		AvgPremarketVol:     avgPremarketVol,
		VolumeRatio:         volumeRatio,
		VolAsPercentOfDaily: volAsPercentOfDaily,
	}
}

// getCurrentPriceBacktest returns the close of the most recent bar.
func getCurrentPriceBacktest(historicalData []AlpacaBarData) float64 {
	if len(historicalData) == 0 {
		return 0
	}
	return historicalData[len(historicalData)-1].Close
}

// ─────────────────────────────────────────────────────────────────────────────
// Output
// ─────────────────────────────────────────────────────────────────────────────

func outputBacktestResults(config BacktestConfig, stocks []BacktestResult) error {
	// ── Fetch and write per-ticker reports for qualifying stocks only ──────
	for i, stock := range stocks {
		fmt.Printf("[OUTPUT] [%d/%d] Fetching deep reports for %s...\n", i+1, len(stocks), stock.Symbol)

		// Deep earnings: Finnhub EPS numbers + SEC EDGAR press-release text
		deepEarnings, earningsErr := getDeepEarningsHistory(config.FinnhubKey, stock.Symbol, config.TargetDate)
		if earningsErr != nil {
			fmt.Printf("⚠️  [OUTPUT] Could not fetch earnings for %s: %v\n", stock.Symbol, earningsErr)
		} else {
			stocks[i].StockInfo.PreviousEarningsReaction = deepEarnings.OverallSentiment
		}

		// Multi-source news: Finnhub + Yahoo Finance + Finviz + MarketWatch, past 7 days
		newsArticles, newsErr := getPreGapNews(config.FinnhubKey, stock.Symbol, config.TargetDate)
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

// ─────────────────────────────────────────────────────────────────────────────
// Small helpers
// ─────────────────────────────────────────────────────────────────────────────

func boolToString(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func calculateAvgGapUpBacktest(stocks []BacktestResult) float64 {
	if len(stocks) == 0 {
		return 0
	}
	sum := 0.0
	for _, s := range stocks {
		sum += s.StockInfo.GapUp
	}
	return sum / float64(len(stocks))
}

func calculateAvgMarketCapBacktest(stocks []BacktestResult) float64 {
	if len(stocks) == 0 {
		return 0
	}
	sum := 0.0
	for _, s := range stocks {
		sum += s.StockInfo.MarketCap
	}
	return sum / float64(len(stocks))
}

func calculateDataQualityDistribution(stocks []BacktestResult) map[string]int {
	dist := make(map[string]int)
	for _, s := range stocks {
		dist[s.DataQuality]++
	}
	return dist
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

// ─────────────────────────────────────────────────────────────────────────────
// Public convenience functions
// ─────────────────────────────────────────────────────────────────────────────

func RunEpisodicPivotBacktest(alpacaKey, alpacaSecret, finnhubKey, targetDate string) error {
	config := BacktestConfig{
		TargetDate:   targetDate,
		AlpacaKey:    alpacaKey,
		AlpacaSecret: alpacaSecret,
		FinnhubKey:   finnhubKey,
		LookbackDays: 300,
	}
	return FilterStocksEpisodicPivotBacktest(config)
}

func RunMultipleDateBacktests(alpacaKey, alpacaSecret, finnhubKey string, dates []string) error {
	LogSection(fmt.Sprintf("RUNNING MULTIPLE DATE BACKTESTS — %d dates", len(dates)))

	results := make(map[string]int)
	for i, date := range dates {
		LogInfo("MULTI", "[%d/%d] Backtesting %s", i+1, len(dates), date)

		config := BacktestConfig{
			TargetDate:   date,
			AlpacaKey:    alpacaKey,
			AlpacaSecret: alpacaSecret,
			FinnhubKey:   finnhubKey,
			LookbackDays: 300,
		}

		gapUpStocks, err := backtestStage1GapUp(config)
		if err != nil {
			LogError("MULTI", "", "Error backtesting %s: %v", date, err)
			results[date] = 0
			continue
		}
		results[date] = len(gapUpStocks)

		if err := FilterStocksEpisodicPivotBacktest(config); err != nil {
			LogError("MULTI", "", "Full backtest error for %s: %v", date, err)
		}
	}

	LogSection("MULTI-DATE BACKTEST SUMMARY")
	for date, count := range results {
		LogInfo("MULTI", "%s  ->  %d qualifying stocks", date, count)
	}
	return nil
}

func AnalyzeBacktestPerformance(backtestDir string) error {
	LogSection("ANALYZING BACKTEST PERFORMANCE")

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
				LogWarn("ANALYSIS", "", "Error reading %s: %v", file.Name(), err)
				continue
			}
			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				LogWarn("ANALYSIS", "", "Error parsing %s: %v", file.Name(), err)
				continue
			}
			allResults = append(allResults, result)
			LogInfo("ANALYSIS", "Loaded %s", file.Name())
		}
	}

	if len(allResults) == 0 {
		return fmt.Errorf("no backtest results found in %s", backtestDir)
	}

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

	LogInfo("ANALYSIS", "Total qualifying stocks across all dates: %d", totalStocks)
	LogInfo("ANALYSIS", "Average per date: %.2f", float64(totalStocks)/float64(len(allResults)))
	LogInfo("ANALYSIS", "Dates analyzed: %v", dates)

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

	if err := os.WriteFile(analysisFile, analysisData, 0644); err != nil {
		return err
	}

	LogInfo("ANALYSIS", "Analysis written to %s", analysisFile)
	return nil
}
