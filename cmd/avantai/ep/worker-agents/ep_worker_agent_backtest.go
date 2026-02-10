package main

// import (
// 	"avantai/pkg/sapien"
// 	"encoding/json"
// 	"flag"
// 	"fmt"
// 	"io"
// 	"log"
// 	"os"
// 	"strings"
// )

// // Top-level structure that matches your JSON
// type BacktestReport struct {
// 	BacktestConfig   BacktestConfig   `json:"backtest_config"`
// 	BacktestDate     string           `json:"backtest_date"`
// 	BacktestSummary  BacktestSummary  `json:"backtest_summary"`
// 	FilterCriteria   FilterCriteria   `json:"filter_criteria"`
// 	GeneratedAt      string           `json:"generated_at"`
// 	QualifyingStocks []BacktestResult `json:"qualifying_stocks"`
// }

// type BacktestConfig struct {
// 	LookbackDays int    `json:"lookback_days"`
// 	TargetDate   string `json:"target_date"`
// }

// type BacktestSummary struct {
// 	AvgGapUp                float64        `json:"avg_gap_up"`
// 	AvgMarketCap            float64        `json:"avg_market_cap"`
// 	DataQualityDistribution map[string]int `json:"data_quality_distribution"`
// 	TotalCandidates         int            `json:"total_candidates"`
// }

// type FilterCriteria struct {
// 	MaxExtensionAdr         float64 `json:"max_extension_adr"`
// 	MinDollarVolume         int64   `json:"min_dollar_volume"`
// 	MinGapUpPercent         float64 `json:"min_gap_up_percent"`
// 	MinMarketCap            int64   `json:"min_market_cap"`
// 	MinPremarketVolumeRatio float64 `json:"min_premarket_volume_ratio"`
// }

// type BacktestResult struct {
// 	FilteredStock
// 	BacktestDate    string   `json:"backtest_date"`
// 	DataQuality     string   `json:"data_quality"`
// 	HistoricalDays  int      `json:"historical_days_available"`
// 	ValidationNotes []string `json:"validation_notes"`
// }

// type FilteredStock struct {
// 	Symbol    string     `json:"symbol"`
// 	StockInfo StockStats `json:"stock_info"`
// }

// type StockStats struct {
// 	Timestamp                string  `json:"timestamp"`
// 	MarketCap                float64 `json:"market_cap"`
// 	Dolvol                   float64 `json:"dolvol"`
// 	GapUp                    float64 `json:"gap_up"`
// 	Adr                      float64 `json:"adr"`
// 	Name                     string  `json:"name"`
// 	Exchange                 string  `json:"exchange"`
// 	Sector                   string  `json:"sector"`
// 	Industry                 string  `json:"industry"`
// 	PremarketVolume          float64 `json:"premarket_volume"`
// 	AvgPremarketVolume       float64 `json:"avg_premarket_volume"`
// 	PremarketVolumeRatio     float64 `json:"premarket_volume_ratio"`
// 	PremarketVolPercent      int     `json:"premarket_vol_percent"`
// 	Sma200                   float64 `json:"sma_200"`
// 	Ema200                   float64 `json:"ema_200"`
// 	Ema50                    float64 `json:"ema_50"`
// 	Ema20                    float64 `json:"ema_20"`
// 	Ema10                    float64 `json:"ema_10"`
// 	IsAbove200Ema            bool    `json:"is_above_200_ema"`
// 	DistanceFrom50Ema        float64 `json:"distance_from_50_ema"`
// 	IsExtended               bool    `json:"is_extended"`
// 	IsTooExtended            bool    `json:"is_too_extended"`
// 	VolumeDriedUp            bool    `json:"volume_dried_up"`
// 	IsNearEma1020            bool    `json:"is_near_ema_10_20"`
// 	BreaksResistance         bool    `json:"breaks_resistance"`
// 	PreviousEarningsReaction string  `json:"previous_earnings_reaction"`
// }

// func main() {

// 	backtestDatePtr := flag.String("date", "", "a string for the date")
// 	flag.Parse()
// 	backtestDate := *backtestDatePtr // Now dereference after parsing

// 	filePath := fmt.Sprintf("data/backtests/backtest_%s_results.json", strings.ReplaceAll(backtestDate, "-", ""))
// 	file, err := os.Open(filePath)
// 	if err != nil {
// 		log.Fatalf("Error opening file: %v\n", err)
// 	}
// 	defer file.Close()

// 	// Read the entire file content
// 	fileContent, err := io.ReadAll(file)
// 	if err != nil {
// 		log.Fatalf("Error reading file: %v\n", err)
// 	}

// 	// Slice to hold the parsed data
// 	var report BacktestReport

// 	// Unmarshal the JSON data into the report slice
// 	err = json.Unmarshal(fileContent, &report)
// 	if err != nil {
// 		log.Fatalf("Error unmarshalling JSON: %v\n", err)
// 	}

// 	// Use a WaitGroup to wait for all goroutines to complete
// 	// var wg sync.WaitGroup

// 	// Create a semaphore channel to limit concurrent API calls
// 	// Buffer size of 3 means max 3 concurrent operations
// 	// semaphore := make(chan struct{}, 1)

// 	// gets the news info for the respective stock
// 	for _, stock := range report.QualifyingStocks {

// 		sapien.NewsAgentReqInfo(stock.Symbol)
// 		sapien.EarningsReportAgentReqInfo(stock.Symbol)
// 		// wg.Add(1)

// 		// go func(symbol string) {
// 		// 	defer wg.Done()

// 		// 	// Acquire semaphore (blocks if 3 goroutines are already running)
// 		// 	semaphore <- struct{}{}
// 		// 	defer func() { <-semaphore }() // Release semaphore when done

// 		// 	// Create a WaitGroup for the two agent calls
// 		// 	var agentWg sync.WaitGroup
// 		// 	agentWg.Add(2)

// 		// 	go sapien.NewsAgentReqInfo(&agentWg, symbol)
// 		// 	go sapien.EarningsReportAgentReqInfo(&agentWg, symbol)

// 		// 	agentWg.Wait()
// 		// }(stock.Symbol)
// 	}

// 	// Wait for all goroutines to finish
// 	// wg.Wait()
// }
