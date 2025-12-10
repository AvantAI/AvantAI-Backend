package main

import (
	"avantai/pkg/ep"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/joho/godotenv"
)

// RealtimeScanResponse represents the complete JSON output from the real-time scanner
type RealtimeScanResponse struct {
	ScanTime       string                `json:"scan_time"`
	MarketStatus   string                `json:"market_status"`
	FilterCriteria FilterCriteriaDetails `json:"filter_criteria"`
	QualifyingStocks []ep.RealtimeResult `json:"qualifying_stocks"`
	Summary        ScanSummary           `json:"summary"`
}

// FilterCriteriaDetails contains all the filter thresholds used
type FilterCriteriaDetails struct {
	MinGapUpPercent        float64 `json:"min_gap_up_percent"`
	MinDollarVolume        float64 `json:"min_dollar_volume"`
	MinMarketCap           float64 `json:"min_market_cap"`
	MinPremarketVolRatio   float64 `json:"min_premarket_volume_ratio"`
	MaxExtensionADR        float64 `json:"max_extension_adr"`
}

// ScanSummary contains aggregate statistics from the scan
type ScanSummary struct {
	TotalCandidates int     `json:"total_candidates"`
	AvgGapUp        float64 `json:"avg_gap_up"`
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	alpacaKey := os.Getenv("ALPACA_API_KEY")
	alpacaSecret := os.Getenv("ALPACA_SECRET_KEY")

	// Run the real-time scanner
	config := ep.AlpacaConfig{
		APIKey:    alpacaKey,
		APISecret: alpacaSecret,
		BaseURL:   "https://paper-api.alpaca.markets",
		DataURL:   "https://data.alpaca.markets",
		IsPaper:   true,
	}

	err = ep.FilterStocksEpisodicPivotRealtime(config)
	if err != nil {
		log.Fatalf("Error running real-time scanner: %v\n", err)
	}

	// Read the real-time scan results (now always at the same location)
	filePath := "data/stockdata/filtered_stocks_marketstack.json"
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Error opening file: %v\n", err)
	}
	defer file.Close()

	// Read the entire file content
	fileContent, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Error reading file: %v\n", err)
	}

	// Unmarshal into the RealtimeScanResponse structure
	var scanResponse RealtimeScanResponse
	err = json.Unmarshal(fileContent, &scanResponse)
	if err != nil {
		log.Fatalf("Error unmarshalling JSON: %v\n", err)
	}

	// Print summary information
	fmt.Printf("\n=== Real-Time Scan Results ===\n")
	fmt.Printf("Scan Time: %s\n", scanResponse.ScanTime)
	fmt.Printf("Market Status: %s\n", scanResponse.MarketStatus)
	fmt.Printf("Total Qualifying Stocks: %d\n", scanResponse.Summary.TotalCandidates)
	fmt.Printf("Average Gap Up: %.2f%%\n\n", scanResponse.Summary.AvgGapUp)

	// Print filter criteria
	fmt.Printf("=== Filter Criteria Used ===\n")
	fmt.Printf("Min Gap Up: %.2f%%\n", scanResponse.FilterCriteria.MinGapUpPercent)
	fmt.Printf("Min Dollar Volume: $%.0f\n", scanResponse.FilterCriteria.MinDollarVolume)
	fmt.Printf("Min Market Cap: $%.0f\n", scanResponse.FilterCriteria.MinMarketCap)
	fmt.Printf("Min Premarket Vol Ratio: %.2fx\n", scanResponse.FilterCriteria.MinPremarketVolRatio)
	fmt.Printf("Max Extension: %.2f ADRs\n\n", scanResponse.FilterCriteria.MaxExtensionADR)

	// Process each qualifying stock
	fmt.Printf("=== Qualifying Stocks ===\n")
	for i, stock := range scanResponse.QualifyingStocks {
		fmt.Printf("\n[%d] %s (%s)\n", i+1, stock.Symbol, stock.StockInfo.Name)
		fmt.Printf("    Exchange: %s | Sector: %s | Industry: %s\n", 
			stock.StockInfo.Exchange, stock.StockInfo.Sector, stock.StockInfo.Industry)
		fmt.Printf("    Gap Up: %.2f%% | Market Cap: $%.0fM\n", 
			stock.StockInfo.GapUp, stock.StockInfo.MarketCap/1_000_000)
		fmt.Printf("    Dollar Volume: $%.0fM | ADR: %.2f%%\n", 
			stock.StockInfo.DolVol/1_000_000, stock.StockInfo.ADR)
		fmt.Printf("    Premarket Volume: %.0f (%.2fx avg, %.2f%% of daily)\n",
			stock.StockInfo.PremarketVolume, 
			stock.StockInfo.PremarketVolumeRatio, 
			stock.StockInfo.PremarketVolAsPercent)
		fmt.Printf("    Technical Indicators:\n")
		fmt.Printf("      SMA200: $%.2f | EMA200: $%.2f\n", 
			stock.StockInfo.SMA200, stock.StockInfo.EMA200)
		fmt.Printf("      EMA50: $%.2f | EMA20: $%.2f | EMA10: $%.2f\n",
			stock.StockInfo.EMA50, stock.StockInfo.EMA20, stock.StockInfo.EMA10)
		fmt.Printf("      Above 200 EMA: %v | Distance from 50 EMA: %.2f ADRs\n",
			stock.StockInfo.IsAbove200EMA, stock.StockInfo.DistanceFrom50EMA)
		fmt.Printf("      Extended: %v | Too Extended: %v\n",
			stock.StockInfo.IsExtended, stock.StockInfo.IsTooExtended)
		fmt.Printf("      Near EMA 10/20: %v | Breaks Resistance: %v\n",
			stock.StockInfo.IsNearEMA1020, stock.StockInfo.BreaksResistance)
		fmt.Printf("    Data Quality: %s\n", stock.DataQuality)
		if len(stock.ValidationNotes) > 0 {
			fmt.Printf("    Validation Notes: %v\n", stock.ValidationNotes)
		}
	}

	// Use a WaitGroup to process stocks concurrently (e.g., fetch news)
	var wg sync.WaitGroup

	// Example: Get news and earnings for each qualifying stock
	for _, stock := range scanResponse.QualifyingStocks {
		wg.Add(1)
		go ep.GetNewsAndEarnings(&wg, stock.Symbol, stock.StockInfo.Timestamp)
	}

	// Wait for all goroutines to finish
	wg.Wait()

	fmt.Println("\n✅ Processing complete!")
}

// Alternative: If you want to work directly with the qualifying stocks slice
func processRealtimeStocks(filePath string) ([]ep.RealtimeResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	fileContent, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %v", err)
	}

	var scanResponse RealtimeScanResponse
	err = json.Unmarshal(fileContent, &scanResponse)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling JSON: %v", err)
	}

	return scanResponse.QualifyingStocks, nil
}

// Helper function to get the most recent scan file
func getMostRecentScanFile(dir string) (string, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var mostRecent string
	var mostRecentTime int64

	for _, file := range files {
		if !file.IsDir() && len(file.Name()) > 5 && file.Name()[:5] == "scan_" {
			info, err := file.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Unix() > mostRecentTime {
				mostRecentTime = info.ModTime().Unix()
				mostRecent = file.Name()
			}
		}
	}

	if mostRecent == "" {
		return "", fmt.Errorf("no scan files found")
	}

	return dir + "/" + mostRecent, nil
}