package main

// import (
// 	"avantai/pkg/ep"
// 	"avantai/pkg/sapien"
// 	"encoding/json"
// 	"io"
// 	"log"
// 	"os"
// 	"sync"
// )

// // RealtimeScanResponse represents the complete JSON output from the real-time scanner
// type RealtimeScanResponse struct {
// 	ScanTime         string                `json:"scan_time"`
// 	MarketStatus     string                `json:"market_status"`
// 	FilterCriteria   FilterCriteriaDetails `json:"filter_criteria"`
// 	QualifyingStocks []ep.RealtimeResult   `json:"qualifying_stocks"`
// 	Summary          ScanSummary           `json:"summary"`
// }

// // FilterCriteriaDetails contains all the filter thresholds used
// type FilterCriteriaDetails struct {
// 	MinGapUpPercent      float64 `json:"min_gap_up_percent"`
// 	MinDollarVolume      float64 `json:"min_dollar_volume"`
// 	MinMarketCap         float64 `json:"min_market_cap"`
// 	MinPremarketVolRatio float64 `json:"min_premarket_volume_ratio"`
// 	MaxExtensionADR      float64 `json:"max_extension_adr"`
// }

// // ScanSummary contains aggregate statistics from the scan
// type ScanSummary struct {
// 	TotalCandidates int     `json:"total_candidates"`
// 	AvgGapUp        float64 `json:"avg_gap_up"`
// }

// func main() {

// 	// Navigate to the directory and open the file
// 	filePath := "data/stockdata/filtered_stocks_marketstack.json"
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

// 	var scanResponse RealtimeScanResponse
// 	err = json.Unmarshal(fileContent, &scanResponse)
// 	if err != nil {
// 		log.Fatalf("error unmarshalling JSON: %v", err)
// 	}

// 	// Slice to hold the parsed data
// 	result := scanResponse.QualifyingStocks

// 	stocks := make([]ep.FilteredStock, len(result))
// 	for i, r := range result {
// 		stocks[i] = r.FilteredStock
// 	}

// 	// Use a WaitGroup to wait for all goroutines to complete
// 	var wg sync.WaitGroup

// 	// gets the news info for the respective stock
// 	for _, stock := range stocks {
// 		// Start the goroutine
// 		wg.Add(2)

// 		// TODO: add the agent pipeline
// 		go sapien.NewsAgentReqInfo(&wg, stock.Symbol)
// 		go sapien.EarningsReportAgentReqInfo(&wg, stock.Symbol)
// 	}

// 	// Wait for all goroutines to finish
// 	wg.Wait()
// }
