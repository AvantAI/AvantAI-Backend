package main

import (
	"avantai/pkg/ep"
	"avantai/pkg/sapien"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

// Constants for volume analysis
const (
	MIN_PREMARKET_VOLUME_INCREASE_PERCENT = 100.0 // Minimum volume increase percentage overnight
)

// Marketstack Intraday Response Structure
type MarketstackIntradayResponse struct {
	Pagination struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Count  int `json:"count"`
		Total  int `json:"total"`
	} `json:"pagination"`
	Data []MarketstackIntradayData `json:"data"`
}

type MarketstackIntradayData struct {
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   float64 `json:"volume"`
	Date     string  `json:"date"`
	Symbol   string  `json:"symbol"`
	Exchange string  `json:"exchange"`
}

type StockDataList struct {
	Symbol    string         `json:"symbol"`
	StockData []ep.StockData `json:"stock_data"`
}

// VolumeAnalysis holds volume comparison data
type VolumeAnalysis struct {
	Symbol                    string  `json:"symbol"`
	Date                     string  `json:"date"`
	PreviousCloseVolume      int64   `json:"previous_close_volume"`
	PremarketVolume          int64   `json:"premarket_volume"`
	MarketOpenVolume         int64   `json:"market_open_volume"`
	VolumeIncreasePercent    float64 `json:"volume_increase_percent"`
	MeetsVolumeThreshold     bool    `json:"meets_volume_threshold"`
	HasPremarketData         bool    `json:"has_premarket_data"`
	PremarketStartTime       string  `json:"premarket_start_time,omitempty"`
	MarketOpenTime           string  `json:"market_open_time,omitempty"`
}

func main() {
	fmt.Println("=== STARTING STOCK DATA PROCESSOR WITH VOLUME ANALYSIS ===")
	
	fmt.Println("Loading .env file...")
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	fmt.Println("✓ .env file loaded successfully")

	apiKey := os.Getenv("MARKETSTACK_TOKEN")
	if apiKey == "" {
		log.Fatal("MARKETSTACK_TOKEN not found in environment variables")
	}
	fmt.Printf("✓ API Key loaded (length: %d characters)\n", len(apiKey))

	// Navigate to the directory and open the file
	filePath := "data/stockdata/filtered_stocks_marketstack.json"
	fmt.Printf("Opening file: %s\n", filePath)
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()
	fmt.Println("✓ File opened successfully")

	// Read the entire file content
	fmt.Println("Reading file content...")
	fileContent, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Error reading file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ File content read successfully (%d bytes)\n", len(fileContent))

	// Slice to hold the parsed data
	var stocks []ep.FilteredStock

	// Unmarshal the JSON data into the stocks slice
	fmt.Println("Parsing JSON data...")
	err = json.Unmarshal(fileContent, &stocks)
	if err != nil {
		log.Fatalf("Error unmarshalling JSON: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ JSON parsed successfully. Found %d stocks\n", len(stocks))

	fmt.Println("\n=== STARTING MAIN PROCESSING LOOP ===")
	for i := 1; i <= 5; i++ {
		fmt.Printf("\n--- ITERATION %d/5 ---\n", i)

		fmt.Println("Extracting symbols and dates from stocks...")
		var symbols []string
		var dates []string
		for j, stock := range stocks {
			symbols = append(symbols, stock.Symbol)
			dates = append(dates, stock.StockInfo.Timestamp[0:10])
			if j < 5 { // Only show first 5 for brevity
				fmt.Printf("  Stock %d: %s (Date: %s)\n", j+1, stock.Symbol, stock.StockInfo.Timestamp[0:10])
			}
		}
		if len(stocks) > 5 {
			fmt.Printf("  ... and %d more stocks\n", len(stocks)-5)
		}
		fmt.Printf("✓ Extracted %d symbols and %d dates\n", len(symbols), len(dates))

		// Perform volume analysis before fetching intraday data
		fmt.Printf("Performing volume analysis for increment %d...\n", i)
		volumeAnalyses, err := performVolumeAnalysis(symbols, dates, apiKey)
		if err != nil {
			log.Printf("Error performing volume analysis: %v", err)
		} else {
			fmt.Printf("✓ Volume analysis completed for %d symbols\n", len(volumeAnalyses))
			
			// Filter stocks that meet volume threshold
			qualifiedStocks := filterStocksByVolume(volumeAnalyses, symbols, dates)
			fmt.Printf("✓ %d stocks meet the volume threshold of %.1f%%\n", len(qualifiedStocks), MIN_PREMARKET_VOLUME_INCREASE_PERCENT)
			
			// Update symbols and dates to only include qualified stocks
			if len(qualifiedStocks) > 0 {
				symbols = make([]string, len(qualifiedStocks))
				dates = make([]string, len(qualifiedStocks))
				for idx, stock := range qualifiedStocks {
					symbols[idx] = stock.Symbol
					dates[idx] = stock.Date
				}
			} else {
				fmt.Println("⚠️  No stocks meet the volume threshold. Proceeding with original list.")
			}
		}

		fmt.Printf("Fetching intraday data for increment %d...\n", i)
		stockData, err := getIntradayData(symbols, dates, i, apiKey)
		if err != nil {
			log.Fatalf("Error getting stock data: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Retrieved stock data for %d symbols\n", len(stockData))

		// Use a WaitGroup to wait for all goroutines to complete
		var wg sync.WaitGroup
		fmt.Printf("Starting %d goroutines for manager agents...\n", len(stockData))

		for j, stock := range stockData {
			// Start the goroutine
			fmt.Printf("  Starting goroutine %d for symbol: %s (with %d data points)\n", j+1, stock.Symbol, len(stock.StockData))
			wg.Add(1)
			go runManagerAgent(&wg, stock.StockData, stock.Symbol, j+1)
		}

		fmt.Println("Waiting for all goroutines to complete...")
		// Wait for all goroutines to finish
		wg.Wait()
		fmt.Println("✓ All goroutines completed")

		if i < 5 {
			fmt.Println("Waiting 1 minute before next iteration...")
			// Wait for 1 minute (60 seconds)
			time.Sleep(1 * time.Minute)
			fmt.Println("✓ Wait complete")
		}
	}
	
	fmt.Println("\n=== STOCK DATA PROCESSOR COMPLETED ===")
}

// performVolumeAnalysis checks premarket volume increases for all stocks
func performVolumeAnalysis(symbols []string, dates []string, apiKey string) ([]VolumeAnalysis, error) {
	fmt.Printf("\n--- performVolumeAnalysis: Analyzing %d stocks ---\n", len(symbols))
	var analyses []VolumeAnalysis

	for i, symbol := range symbols {
		fmt.Printf("\nAnalyzing volume for symbol %d/%d: %s (Date: %s)\n", i+1, len(symbols), symbol, dates[i])
		
		analysis, err := analyzeStockVolume(symbol, dates[i], apiKey)
		if err != nil {
			fmt.Printf("  ❌ Error analyzing volume for %s: %v\n", symbol, err)
			log.Printf("Error analyzing volume for %s: %v", symbol, err)
			continue
		}
		
		analyses = append(analyses, analysis)
		
		if analysis.MeetsVolumeThreshold {
			fmt.Printf("  ✅ %s meets volume threshold (%.1f%% increase)\n", symbol, analysis.VolumeIncreasePercent)
		} else {
			fmt.Printf("  ⚠️  %s does not meet volume threshold (%.1f%% increase)\n", symbol, analysis.VolumeIncreasePercent)
		}

		// Add a small delay between API calls to respect rate limits
		time.Sleep(200 * time.Millisecond)
	}

	// Save volume analysis results
	err := saveVolumeAnalysis(analyses)
	if err != nil {
		fmt.Printf("⚠️  Error saving volume analysis: %v\n", err)
	}

	fmt.Printf("✓ Volume analysis completed for %d stocks\n", len(analyses))
	return analyses, nil
}

// analyzeStockVolume performs volume analysis for a single stock
func analyzeStockVolume(symbol, date, apiKey string) (VolumeAnalysis, error) {
	analysis := VolumeAnalysis{
		Symbol: symbol,
		Date:   date,
	}

	// Get previous trading day for baseline volume
	targetDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return analysis, fmt.Errorf("error parsing date: %v", err)
	}
	
	// Get previous business day (skip weekends)
	prevDate := targetDate.AddDate(0, 0, -1)
	for prevDate.Weekday() == time.Saturday || prevDate.Weekday() == time.Sunday {
		prevDate = prevDate.AddDate(0, 0, -1)
	}
	prevDateStr := prevDate.Format("2006-01-02")

	// Fetch previous day's EOD volume for baseline
	fmt.Printf("    Fetching previous day volume (%s)...\n", prevDateStr)
	prevVolume, err := getPreviousDayVolume(symbol, prevDateStr, apiKey)
	if err != nil {
		fmt.Printf("    ⚠️  Could not get previous day volume: %v\n", err)
		prevVolume = 0 // Continue with analysis even without baseline
	}
	analysis.PreviousCloseVolume = prevVolume

	// Fetch premarket data (4:00 AM - 9:30 AM)
	fmt.Printf("    Fetching premarket volume data...\n")
	premarketVolume, premarketStartTime, err := getPremarketVolume(symbol, date, apiKey)
	if err != nil {
		fmt.Printf("    ⚠️  Could not get premarket volume: %v\n", err)
		analysis.HasPremarketData = false
	} else {
		analysis.PremarketVolume = premarketVolume
		analysis.PremarketStartTime = premarketStartTime
		analysis.HasPremarketData = true
	}

	// Fetch market open volume (9:30 AM onwards)
	fmt.Printf("    Fetching market open volume data...\n")
	marketOpenVolume, marketOpenTime, err := getMarketOpenVolume(symbol, date, apiKey)
	if err != nil {
		fmt.Printf("    ⚠️  Could not get market open volume: %v\n", err)
	} else {
		analysis.MarketOpenVolume = marketOpenVolume
		analysis.MarketOpenTime = marketOpenTime
	}

	// Calculate volume increase percentage
	if prevVolume > 0 {
		totalCurrentVolume := analysis.PremarketVolume + analysis.MarketOpenVolume
		analysis.VolumeIncreasePercent = ((float64(totalCurrentVolume) - float64(prevVolume)) / float64(prevVolume)) * 100
		analysis.MeetsVolumeThreshold = analysis.VolumeIncreasePercent >= MIN_PREMARKET_VOLUME_INCREASE_PERCENT
	} else {
		// If no baseline, check if current volume is significantly high
		analysis.VolumeIncreasePercent = 0
		analysis.MeetsVolumeThreshold = (analysis.PremarketVolume + analysis.MarketOpenVolume) > 1000000 // 1M volume threshold
		fmt.Printf("    ⚠️  No baseline volume, using absolute threshold\n")
	}

	fmt.Printf("    Volume Analysis Results:\n")
	fmt.Printf("      Previous Close Volume: %d\n", analysis.PreviousCloseVolume)
	fmt.Printf("      Premarket Volume: %d\n", analysis.PremarketVolume)
	fmt.Printf("      Market Open Volume: %d\n", analysis.MarketOpenVolume)
	fmt.Printf("      Volume Increase: %.2f%%\n", analysis.VolumeIncreasePercent)
	fmt.Printf("      Meets Threshold: %t\n", analysis.MeetsVolumeThreshold)

	return analysis, nil
}

// getPreviousDayVolume fetches the previous trading day's volume
func getPreviousDayVolume(symbol, date, apiKey string) (int64, error) {
	apiURL := fmt.Sprintf("https://api.marketstack.com/v2/eod?access_key=%s&symbols=%s&date_from=%s&date_to=%s",
		apiKey, symbol, date, date)

	resp, err := http.Get(apiURL)
	if err != nil {
		return 0, fmt.Errorf("error fetching previous day data: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("error reading response: %v", err)
	}

	var eodResponse ep.MarketstackEODResponse
	err = json.Unmarshal(body, &eodResponse)
	if err != nil {
		return 0, fmt.Errorf("error parsing JSON: %v", err)
	}

	if len(eodResponse.Data) == 0 {
		return 0, fmt.Errorf("no EOD data available")
	}

	return int64(eodResponse.Data[0].Volume), nil
}

// getPremarketVolume fetches premarket volume (4:00 AM - 9:30 AM)
func getPremarketVolume(symbol, date, apiKey string) (int64, string, error) {
	// Premarket hours: 4:00 AM to 9:30 AM
	premarketStart := date + "T04:00:00"
	premarketEnd := date + "T09:30:00"

	apiURL := fmt.Sprintf("https://api.marketstack.com/v2/intraday?access_key=%s&symbols=%s&interval=1min&date_from=%s&date_to=%s&limit=1000",
		apiKey, symbol, premarketStart, premarketEnd)

	resp, err := http.Get(apiURL)
	if err != nil {
		return 0, "", fmt.Errorf("error fetching premarket data: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", fmt.Errorf("error reading response: %v", err)
	}

	var intradayResponse MarketstackIntradayResponse
	err = json.Unmarshal(body, &intradayResponse)
	if err != nil {
		return 0, "", fmt.Errorf("error parsing JSON: %v", err)
	}

	if len(intradayResponse.Data) == 0 {
		return 0, "", fmt.Errorf("no premarket data available")
	}

	// Sum up all premarket volume
	var totalVolume int64
	var startTime string
	for i, dataPoint := range intradayResponse.Data {
		totalVolume += int64(dataPoint.Volume)
		if i == 0 {
			startTime = dataPoint.Date
		}
	}

	return totalVolume, startTime, nil
}

// getMarketOpenVolume fetches market open volume (9:30 AM onwards, first hour)
func getMarketOpenVolume(symbol, date, apiKey string) (int64, string, error) {
	// Market open hours: 9:30 AM to 10:30 AM (first hour)
	marketStart := date + "T09:30:00"
	marketEnd := date + "T10:30:00"

	apiURL := fmt.Sprintf("https://api.marketstack.com/v2/intraday?access_key=%s&symbols=%s&interval=1min&date_from=%s&date_to=%s&limit=1000",
		apiKey, symbol, marketStart, marketEnd)

	resp, err := http.Get(apiURL)
	if err != nil {
		return 0, "", fmt.Errorf("error fetching market open data: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", fmt.Errorf("error reading response: %v", err)
	}

	var intradayResponse MarketstackIntradayResponse
	err = json.Unmarshal(body, &intradayResponse)
	if err != nil {
		return 0, "", fmt.Errorf("error parsing JSON: %v", err)
	}

	if len(intradayResponse.Data) == 0 {
		return 0, "", fmt.Errorf("no market open data available")
	}

	// Sum up first hour volume
	var totalVolume int64
	var startTime string
	for i, dataPoint := range intradayResponse.Data {
		totalVolume += int64(dataPoint.Volume)
		if i == 0 {
			startTime = dataPoint.Date
		}
	}

	return totalVolume, startTime, nil
}

// filterStocksByVolume filters stocks that meet the volume threshold
func filterStocksByVolume(analyses []VolumeAnalysis, symbols []string, dates []string) []struct{ Symbol, Date string } {
	var qualified []struct{ Symbol, Date string }
	
	for _, analysis := range analyses {
		if analysis.MeetsVolumeThreshold {
			qualified = append(qualified, struct{ Symbol, Date string }{
				Symbol: analysis.Symbol,
				Date:   analysis.Date,
			})
		}
	}
	
	return qualified
}

// saveVolumeAnalysis saves the volume analysis results to a file
func saveVolumeAnalysis(analyses []VolumeAnalysis) error {
	// Create reports directory if it doesn't exist
	reportsDir := "reports/volume_analysis"
	err := os.MkdirAll(reportsDir, 0755)
	if err != nil {
		return fmt.Errorf("error creating reports directory: %v", err)
	}

	// Generate filename with timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := filepath.Join(reportsDir, fmt.Sprintf("volume_analysis_%s.json", timestamp))

	// Convert to JSON
	jsonData, err := json.MarshalIndent(analyses, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %v", err)
	}

	// Write to file
	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("error writing file: %v", err)
	}

	fmt.Printf("✓ Volume analysis saved to: %s\n", filename)
	return nil
}

// getIntradayData fetches intraday data from Marketstack API for individual stocks
func getIntradayData(symbols []string, dates []string, increment int, apiKey string) ([]StockDataList, error) {
	fmt.Printf("\n--- getIntradayData: Processing %d symbols with increment %d ---\n", len(symbols), increment)
	var stockDataList []StockDataList

	for i, symbol := range symbols {
		fmt.Printf("\nProcessing symbol %d/%d: %s (Date: %s)\n", i+1, len(symbols), symbol, dates[i])

		// For Marketstack, we need to make individual API calls per symbol
		// Use intraday endpoint with 1min interval
		apiURL := fmt.Sprintf("https://api.marketstack.com/v2/intraday?access_key=%s&symbols=%s&interval=1min&date_from=%s&date_to=%s&limit=1000",
			apiKey, symbol, dates[i], dates[i])
		
		fmt.Printf("  API URL: %s\n", apiURL[:100]+"...") // Show first 100 chars for security

		// Send the HTTP GET request
		fmt.Println("  Sending HTTP GET request...")
		resp, err := http.Get(apiURL)
		if err != nil {
			fmt.Printf("  ❌ Error fetching data: %v\n", err)
			return nil, fmt.Errorf("error fetching data from Marketstack: %v", err)
		}
		defer resp.Body.Close()
		fmt.Printf("  ✓ HTTP response received (Status: %s)\n", resp.Status)

		// Read the response body
		fmt.Println("  Reading response body...")
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("  ❌ Error reading response: %v\n", err)
			return nil, fmt.Errorf("error reading response body: %v", err)
		}
		fmt.Printf("  ✓ Response body read (%d bytes)\n", len(body))

		// Parse the JSON response
		fmt.Println("  Parsing JSON response...")
		var intradayResponse MarketstackIntradayResponse
		err = json.Unmarshal(body, &intradayResponse)
		if err != nil {
			fmt.Printf("  ❌ Error parsing JSON: %v\n", err)
			return nil, fmt.Errorf("error parsing response: %v. Response: %s", err, string(body))
		}
		fmt.Printf("  ✓ JSON parsed successfully. Found %d data points\n", len(intradayResponse.Data))
		fmt.Printf("  Pagination info - Limit: %d, Offset: %d, Count: %d, Total: %d\n", 
			intradayResponse.Pagination.Limit, intradayResponse.Pagination.Offset, 
			intradayResponse.Pagination.Count, intradayResponse.Pagination.Total)

		// Check if we have data
		if len(intradayResponse.Data) == 0 {
			fmt.Printf("  ⚠️  No intraday data available for symbol %s on date %s\n", symbol, dates[i])
			log.Printf("No intraday data available for symbol %s on date %s", symbol, dates[i])
			continue
		}

		// Convert Marketstack data to ep.StockData format
		fmt.Println("  Converting data to internal format...")
		var result []ep.StockData

		// Market opens at 9:30 AM, we want to get data for specified increment of minutes
		marketOpenTime := "09:30:00"
		startTime, err := time.Parse("2006-01-02 15:04:05", dates[i]+" "+marketOpenTime)
		if err != nil {
			fmt.Printf("  ❌ Error parsing start time: %v\n", err)
			return nil, fmt.Errorf("error parsing start time: %v", err)
		}
		fmt.Printf("  Market start time: %s\n", startTime.Format("2006-01-02 15:04:05"))

		// Process the available data from Marketstack and filter by time
		fmt.Printf("  Processing %d minutes of data...\n", increment)
		for j := 1; j <= increment && j <= len(intradayResponse.Data); j++ {
			// Calculate the expected timestamp for this minute
			expectedTime := startTime.Add(time.Duration(j) * time.Minute)
			fmt.Printf("    Minute %d: Looking for data around %s\n", j, expectedTime.Format("15:04:05"))

			// Find the closest data point to our expected time
			var closestData *MarketstackIntradayData
			minTimeDiff := time.Hour * 24 // Start with a large time difference

			for k, dataPoint := range intradayResponse.Data {
				// Parse the timestamp from Marketstack (ISO format)
				dataTime, err := time.Parse("2006-01-02T15:04:05+0000", dataPoint.Date)
				if err != nil {
					if k < 3 { // Only show first few errors to avoid spam
						fmt.Printf("      ⚠️  Error parsing timestamp for data point %d: %v\n", k, err)
					}
					continue
				}

				// Check if this data point is close to our expected time
				timeDiff := expectedTime.Sub(dataTime)
				if timeDiff < 0 {
					timeDiff = -timeDiff
				}

				if timeDiff < minTimeDiff {
					minTimeDiff = timeDiff
					closestData = &dataPoint
				}
			}

			// If we found a close enough data point (within 5 minutes), use it
			if closestData != nil && minTimeDiff <= 5*time.Minute {
				fmt.Printf("      ✓ Found data point (time diff: %s)\n", minTimeDiff)
				stockData := ep.StockData{
					Symbol:    symbol,
					Timestamp: expectedTime.Format("2006-01-02 15:04:00"),
					Open:      closestData.Open,
					High:      closestData.High,
					Low:       closestData.Low,
					Close:     closestData.Close,
					Volume:    int64(closestData.Volume),
				}

				result = append(result, stockData)
				fmt.Printf("      Data: O:%.2f H:%.2f L:%.2f C:%.2f V:%d\n", 
					stockData.Open, stockData.High, stockData.Low, stockData.Close, stockData.Volume)
			} else {
				fmt.Printf("      ❌ No suitable data point found (min diff: %s)\n", minTimeDiff)
			}
		}

		fmt.Printf("  ✓ Converted %d data points for %s\n", len(result), symbol)
		stockDataList = append(stockDataList, StockDataList{
			Symbol:    symbol,
			StockData: result,
		})

		// Add a small delay between API calls to respect rate limits
		fmt.Println("  Waiting 200ms for rate limiting...")
		time.Sleep(200 * time.Millisecond)
	}

	fmt.Printf("✓ getIntradayData completed. Returning %d stock data lists\n", len(stockDataList))
	return stockDataList, nil
}

// Alternative function using EOD data if intraday is not available
func getEODData(symbols []string, dates []string, apiKey string) ([]StockDataList, error) {
	fmt.Printf("\n--- getEODData: Processing %d symbols ---\n", len(symbols))
	var stockDataList []StockDataList

	for i, symbol := range symbols {
		fmt.Printf("\nProcessing EOD data for symbol %d/%d: %s (Date: %s)\n", i+1, len(symbols), symbol, dates[i])

		// Use EOD endpoint for end-of-day data
		apiURL := fmt.Sprintf("https://api.marketstack.com/v2/eod?access_key=%s&symbols=%s&date_from=%s&date_to=%s",
			apiKey, symbol, dates[i], dates[i])
		
		fmt.Printf("  EOD API URL: %s\n", apiURL[:100]+"...")

		// Send the HTTP GET request
		fmt.Println("  Sending HTTP GET request for EOD data...")
		resp, err := http.Get(apiURL)
		if err != nil {
			fmt.Printf("  ❌ Error fetching EOD data: %v\n", err)
			return nil, fmt.Errorf("error fetching EOD data from Marketstack: %v", err)
		}
		defer resp.Body.Close()
		fmt.Printf("  ✓ EOD HTTP response received (Status: %s)\n", resp.Status)

		// Read the response body
		fmt.Println("  Reading EOD response body...")
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("  ❌ Error reading EOD response: %v\n", err)
			return nil, fmt.Errorf("error reading response body: %v", err)
		}
		fmt.Printf("  ✓ EOD Response body read (%d bytes)\n", len(body))

		// Parse the JSON response
		fmt.Println("  Parsing EOD JSON response...")
		var eodResponse ep.MarketstackEODResponse
		err = json.Unmarshal(body, &eodResponse)
		if err != nil {
			fmt.Printf("  ❌ Error parsing EOD JSON: %v\n", err)
			return nil, fmt.Errorf("error parsing EOD response: %v. Response: %s", err, string(body))
		}
		fmt.Printf("  ✓ EOD JSON parsed successfully. Found %d data points\n", len(eodResponse.Data))

		// Check if we have data
		if len(eodResponse.Data) == 0 {
			fmt.Printf("  ⚠️  No EOD data available for symbol %s on date %s\n", symbol, dates[i])
			log.Printf("No EOD data available for symbol %s on date %s", symbol, dates[i])
			continue
		}

		// Convert to ep.StockData format
		fmt.Println("  Converting EOD data to internal format...")
		var result []ep.StockData
		for j, dataPoint := range eodResponse.Data {
			// Parse the date from Marketstack
			parsedTime, err := time.Parse("2006-01-02T15:04:05+0000", dataPoint.Date)
			if err != nil {
				fmt.Printf("  ⚠️  Error parsing date for %s (point %d): %v\n", symbol, j, err)
				log.Printf("Error parsing date for %s: %v", symbol, err)
				continue
			}

			stockData := ep.StockData{
				Symbol:    symbol,
				Timestamp: parsedTime.Format("2006-01-02 15:04:00"),
				Open:      dataPoint.Open,
				High:      dataPoint.High,
				Low:       dataPoint.Low,
				Close:     dataPoint.Close,
				Volume:    int64(dataPoint.Volume),
			}

			result = append(result, stockData)
			fmt.Printf("    EOD Data point %d: O:%.2f H:%.2f L:%.2f C:%.2f V:%d\n", 
				j+1, stockData.Open, stockData.High, stockData.Low, stockData.Close, stockData.Volume)
		}

		fmt.Printf("  ✓ Converted %d EOD data points for %s\n", len(result), symbol)
		stockDataList = append(stockDataList, StockDataList{
			Symbol:    symbol,
			StockData: result,
		})

		// Add a small delay between API calls to respect rate limits
		fmt.Println("  Waiting 200ms for rate limiting...")
		time.Sleep(200 * time.Millisecond)
	}

	fmt.Printf("✓ getEODData completed. Returning %d stock data lists\n", len(stockDataList))
	return stockDataList, nil
}

func runManagerAgent(wg *sync.WaitGroup, stockdata []ep.StockData, symbol string, goroutineId int) {
	fmt.Printf("\n[Goroutine %d] --- Starting runManagerAgent for %s ---\n", goroutineId, symbol)
	defer wg.Done() // Decrement the counter when the goroutine completes
	defer fmt.Printf("[Goroutine %d] ✓ runManagerAgent completed for %s\n", goroutineId, symbol)

	fmt.Printf("[Goroutine %d] Processing %d stock data points for %s\n", goroutineId, len(stockdata), symbol)
	stock_data := ""

	for i, stockdataPoint := range stockdata {
		stock_data += fmt.Sprintf(fmt.Sprint(i) + " min - " + " Open: " + fmt.Sprint(stockdataPoint.Open) +
			" Close: " + fmt.Sprint(stockdataPoint.PreviousClose) + " High: " + fmt.Sprint(stockdataPoint.High) + " Low: " + fmt.Sprint(stockdataPoint.Low) + "\n")
		
		if i < 3 { // Show first 3 data points for debugging
			fmt.Printf("[Goroutine %d]   Data %d: O:%.2f C:%.2f H:%.2f L:%.2f\n", 
				goroutineId, i, stockdataPoint.Open, stockdataPoint.PreviousClose, stockdataPoint.High, stockdataPoint.Low)
		}
	}
	fmt.Printf("[Goroutine %d] ✓ Stock data formatted (%d characters)\n", goroutineId, len(stock_data))

	dataDir := "reports"
	stockDir := filepath.Join(dataDir, symbol)
	fmt.Printf("[Goroutine %d] Looking for reports in directory: %s\n", goroutineId, stockDir)

	// Create or open the news report file
	newsFilePath := filepath.Join(stockDir, "news_report.txt")
	fmt.Printf("[Goroutine %d] Opening news report: %s\n", goroutineId, newsFilePath)
	file, err := os.Open(newsFilePath)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Error opening news report file: %v\n", goroutineId, err)
		fmt.Println("Error creating file:", err)
	}
	defer file.Close()

	// Read the entire file content
	fmt.Printf("[Goroutine %d] Reading news report content...\n", goroutineId)
	news, err := io.ReadAll(file)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Error reading news file: %v\n", goroutineId, err)
		log.Fatalf("Error reading file: %v\n", err)
	}
	fmt.Printf("[Goroutine %d] ✓ News report read (%d bytes)\n", goroutineId, len(news))

	// Create or open the earnings report file
	earningsFilePath := filepath.Join(stockDir, "earnings_report.txt")
	fmt.Printf("[Goroutine %d] Opening earnings report: %s\n", goroutineId, earningsFilePath)
	file, err = os.Open(earningsFilePath)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Error opening earnings report file: %v\n", goroutineId, err)
		fmt.Println("Error opening file:", err)
	}
	defer file.Close()

	// Read the entire file content
	fmt.Printf("[Goroutine %d] Reading earnings report content...\n", goroutineId)
	earnings_report, err := io.ReadAll(file)
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Error reading earnings file: %v\n", goroutineId, err)
		log.Fatalf("Error opening file: %v\n", err)
	}
	fmt.Printf("[Goroutine %d] ✓ Earnings report read (%d bytes)\n", goroutineId, len(earnings_report))

	fmt.Printf("[Goroutine %d] Calling ManagerAgentReqInfo for %s...\n", goroutineId, symbol)
	resp, err := sapien.ManagerAgentReqInfo(stock_data, string(news), string(earnings_report))
	if err != nil {
		fmt.Printf("[Goroutine %d] ❌ Error calling ManagerAgentReqInfo: %v\n", goroutineId, err)
		fmt.Printf("Error: %s", err)
		os.Exit(1)
	}
	fmt.Printf("[Goroutine %d] ✓ ManagerAgentReqInfo completed successfully\n", goroutineId)

	fmt.Printf("[Goroutine %d] Response received (%d characters):\n", goroutineId, len(resp.Response))
	fmt.Printf("[Goroutine %d] Response: %s\n", goroutineId, resp.Response)
}

// Function to read all files from the given directory and return the concatenated content as a string
func readFilesFromDirectory(directory string) (string, error) {
	fmt.Printf("\n--- readFilesFromDirectory: %s ---\n", directory)
	var result string

	// Read the directory contents
	fmt.Println("Reading directory contents...")
	files, err := os.ReadDir(directory)
	if err != nil {
		fmt.Printf("❌ Error reading directory: %v\n", err)
		return "", fmt.Errorf("error reading directory: %v", err)
	}
	fmt.Printf("✓ Found %d items in directory\n", len(files))

	// Loop through all the files
	fileCount := 0
	for i, file := range files {
		if !file.IsDir() { // Check if it's not a subdirectory
			fileCount++
			fmt.Printf("  Processing file %d: %s\n", i+1, file.Name())
			filePath := filepath.Join(directory, file.Name())
			content, err := os.ReadFile(filePath)
			if err != nil {
				fmt.Printf("  ❌ Error reading file %s: %v\n", file.Name(), err)
				return "", fmt.Errorf("error reading file %s: %v", file.Name(), err)
			}
			// Append file content to the result string
			result += string(content) + "\n"
			fmt.Printf("  ✓ File %s read (%d bytes)\n", file.Name(), len(content))
		} else {
			fmt.Printf("  Skipping subdirectory: %s\n", file.Name())
		}
	}

	fmt.Printf("✓ readFilesFromDirectory completed. Processed %d files, total content: %d characters\n", fileCount, len(result))
	return result, nil
}