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
	"strings"
	"sync"
	"time"
)

// API response structures
type BulkQuoteResponse struct {
	MetaData map[string]string `json:"Meta Data"`
	Data     []StockData       `json:"data"`
}

type StockData struct {
	Symbol                     string  `json:"symbol"`
	Timestamp                  string  `json:"timestamp"`
	Open                       float64 `json:"open,string"`
	High                       float64 `json:"high,string"`
	Low                        float64 `json:"low,string"`
	Close                      float64 `json:"close,string"`
	Volume                     float64 `json:"volume,string"`
	PreviousClose              float64 `json:"previousClose,string"`
	Change                     float64 `json:"change,string"`
	ChangePercent              float64 `json:"changePercent"`
	ExtendedHoursQuote         float64 `json:"extendedhoursquote"`
	ExtendedHoursChange        float64 `json:"extendedhourschange"`
	ExtendedHoursChangePercent string  `json:"extendedhourschangepercent"`
}

// Time series response for historical data
type TimeSeriesResponse struct {
	MetaData   map[string]string             `json:"Meta Data"`
	TimeSeries map[string]TimeSeriesDataItem `json:"Time Series (Daily)"`
}

type TimeSeriesDataItem struct {
	Open   string `json:"1. open"`
	High   string `json:"2. high"`
	Low    string `json:"3. low"`
	Close  string `json:"4. close"`
	Volume string `json:"5. volume"`
}

// Our filtered output structure
type FilteredStock struct {
	Symbol    string     `json:"symbol"`
	StockInfo StockStats `json:"stock_info"`
}

type StockStats struct {
	Timestamp string  `json:"timestamp"`
	DolVol    float64 `json:"dolvol"`
	GapUp     float64 `json:"gap-up"`
}

// Configuration
const (
	MIN_DOLLAR_VOLUME               = 10000000.0 // Minimum Dollar Volume
	MIN_GAP_UP_PERCENT              = 10.0       // Minimum Gap Up in percent
	CONSOLIDATION_DAYS              = 5          // Number of days to check for consolidation
	MAX_CONSOLIDATION_RANGE_PERCENT = 5.0        // Maximum range for consolidation period
)

func FilterStocks(apiKey string) {
	fmt.Println("Starting stock filter program...")

	// // Get bulk quotes data
	// stocks, err := fetchBulkQuotes(apiKey)
	// if err != nil {
	// 	log.Fatalf("Error fetching bulk quotes: %v", err)
	// }

	stocks, err := ReadStockData()
	if err != nil {
		log.Fatalf("Failed to read stock data: %v", err)
	}

	fmt.Printf("Fetched data for %d stocks\n", len(stocks))

	// Filter and process stocks
	var filteredStocks []FilteredStock

	for _, stock := range stocks {
		fmt.Printf("Processing stock: %s\n", stock.Symbol)
		fmt.Printf("Close: %f\n", stock.Close)
		// Calculate dollar volume
		dolVol := stock.Open * stock.Volume
		fmt.Println("Dolvol: ", dolVol)

		// Calculate gap up percentage
		gapUp := ((stock.Open - stock.Close) / stock.Close) * 100
		fmt.Println("gapUp: ", gapUp)

		// Initial filtering based on simple metrics
		if dolVol >= MIN_DOLLAR_VOLUME && gapUp >= MIN_GAP_UP_PERCENT {
			// Check for consolidation
			// isConsolidating, err := checkConsolidation(apiKey, stock.Symbol)
			// if err != nil {
			// 	fmt.Printf("Error checking consolidation for %s: %v\n", stock.Symbol, err)
			// 	continue
			// }

			// if isConsolidating {
			// 	// Add to our filtered results
			// 	filteredStocks = append(filteredStocks, FilteredStock{
			// 		Symbol: stock.Symbol,
			// 		StockInfo: StockStats{
			// 			ADR:    adr,
			// 			DolVol: dolVol,
			// 			GapUp:  gapUp,
			// 		},
			// 	})

			// 	fmt.Printf("Added %s to filtered list\n", stock.Symbol)
			// }

			filteredStocks = append(filteredStocks, FilteredStock{
				Symbol: stock.Symbol,
				StockInfo: StockStats{
					Timestamp: stock.Timestamp,
					DolVol:    dolVol,
					GapUp:     gapUp,
				},
			})

			fmt.Printf("Added %s to filtered list\n", stock.Symbol)
		}

		// Add a small delay to avoid hitting API rate limits
		time.Sleep(250 * time.Millisecond)
	}

	// Output results to JSON file
	outputToJSON(filteredStocks)

	fmt.Printf("Filter complete. Found %d stocks matching criteria.\n", len(filteredStocks))
}

func ReadStockData() ([]StockData, error) {
	// Read the file
	data, err := os.ReadFile("pkg/ep/stock_data.json")
	if err != nil {
		return nil, fmt.Errorf("error reading file: %v", err)
	}

	// Parse the JSON data
	var stockDataList []StockData
	err = json.Unmarshal(data, &stockDataList)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %v", err)
	}

	return stockDataList, nil
}

func fetchBulkQuotes(apiKey string) ([]StockData, error) {
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

	fmt.Print("Content: ", reader)

	// Parse stock symbols (assuming first column)
	var symbols []string
	for _, row := range rows {
		if len(row) > 0 && row[0] != "Symbol" && row[0] != "" {
			symbols = append(symbols, row[0])
		}
	}

	// Process in batches of 100 symbols
	const batchSize = 100
	var allQuotes []StockData

	var wg sync.WaitGroup
	resultCh := make(chan []StockData, len(symbols)/batchSize) // Channel to collect result

	for i := 0; i < 100; i += batchSize {
		end := i + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}

		// Create a batch of symbols
		batch := symbols[i:end]
		batchSymbols := strings.Join(batch, ",")

		fmt.Println("Batch: ", batchSymbols)

		wg.Add(1) // Increment the counter for each goroutine
		go getBulkQuotesReq(&wg, apiKey, batchSymbols, resultCh)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(resultCh) // Close the channel after all goroutines are done

	for result := range resultCh {
		// Add quotes from this batch to the total
		allQuotes = append(allQuotes, result...)
	}

	return allQuotes, nil
}

func getBulkQuotesReq(wg *sync.WaitGroup, apiKey string, batchSymbols string, resultCh chan<- []StockData) {
	defer wg.Done() // Decrement the counter when the goroutine completes
	url := fmt.Sprintf("https://www.alphavantage.co/query?function=REALTIME_BULK_QUOTES&symbol=%sentitlement=realtime&apikey=%s",
		batchSymbols, apiKey)

	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error fetching bulk quotes: %s", err)
		os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		fmt.Printf("Error reading response body: %s", err)
		os.Exit(1)
	}

	var bulkResponse BulkQuoteResponse
	err = json.Unmarshal(body, &bulkResponse)
	if err != nil {
		fmt.Printf("error parsing bulk quotes response: %v. Response: %s", err, string(body))
		os.Exit(1)
	}

	resultCh <- bulkResponse.Data // Send the result back through the channel
}

// func checkConsolidation(apiKey string, symbol string) (bool, error) {
// 	url := fmt.Sprintf("https://www.alphavantage.co/query?function=TIME_SERIES_DAILY&symbol=%s&outputsize=compact&apikey=%s", symbol, apiKey)

// 	resp, err := http.Get(url)
// 	if err != nil {
// 		return false, err
// 	}
// 	defer resp.Body.Close()

// 	body, err := io.ReadAll(resp.Body)
// 	if err != nil {
// 		return false, err
// 	}

// 	var timeSeriesResp TimeSeriesResponse
// 	err = json.Unmarshal(body, &timeSeriesResp)
// 	if err != nil {
// 		return false, fmt.Errorf("error parsing time series response: %v", err)
// 	}

// 	// Extract and sort dates
// 	var dates []string
// 	for date := range timeSeriesResp.TimeSeries {
// 		dates = append(dates, date)
// 	}

// 	// Check if we have enough data
// 	if len(dates) < CONSOLIDATION_DAYS {
// 		return false, fmt.Errorf("not enough historical data")
// 	}

// 	// Analysis for consolidation
// 	var highestPrice, lowestPrice float64 = 0, 99999999
// 	var volumeSum float64 = 0

// 	// Look at the last few days (excluding today)
// 	for i := 0; i < CONSOLIDATION_DAYS; i++ {
// 		if i >= len(dates) {
// 			break
// 		}

// 		dataItem := timeSeriesResp.TimeSeries[dates[i]]

// 		// Parse high and low
// 		high, err := parseFloat(dataItem.High)
// 		if err != nil {
// 			continue
// 		}

// 		low, err := parseFloat(dataItem.Low)
// 		if err != nil {
// 			continue
// 		}

// 		volume, _ := parseFloat(dataItem.Volume)
// 		volumeSum += volume

// 		if high > highestPrice {
// 			highestPrice = high
// 		}

// 		if low < lowestPrice {
// 			lowestPrice = low
// 		}
// 	}

// 	// Calculate price range as a percentage
// 	priceRange := ((highestPrice - lowestPrice) / lowestPrice) * 100

// 	// Determine if stock is consolidating
// 	return priceRange <= MAX_CONSOLIDATION_RANGE_PERCENT, nil
// }

func parseFloat(value string) (float64, error) {
	var result float64
	_, err := fmt.Sscanf(value, "%f", &result)
	return result, err
}

func outputToJSON(stocks []FilteredStock) error {
	jsonData, err := json.MarshalIndent(stocks, "", "  ")
	if err != nil {
		return err
	}

	stockDir := "data/stockdata"

	if err := os.MkdirAll(stockDir, 0755); err != nil {
		fmt.Printf("Error creating directories: %v\n", err)
		os.Exit(1)
	}

	err = os.WriteFile(filepath.Join(stockDir, "filtered_stocks.json"), jsonData, 0644)
	if err != nil {
		return err
	}

	fmt.Println("Results written to filtered_stocks.json")
	return nil
}
