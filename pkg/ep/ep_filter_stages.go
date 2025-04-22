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

// Overview response structure
type OverviewResponse struct {
	Symbol        string `json:"Symbol"`
	MarketCapStr  string `json:"MarketCapitalization"`
	Name          string `json:"Name"`
	Exchange      string `json:"Exchange"`
	Sector        string `json:"Sector"`
	Industry      string `json:"Industry"`
	MarketCap     float64 `json:"-"` // Calculated field
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
	Timestamp  string  `json:"timestamp"`
	MarketCap  float64 `json:"market_cap"`
	DolVol     float64 `json:"dolvol"`     // Average dollar volume over 21 days
	GapUp      float64 `json:"gap-up"`     // Gap up percentage
	ADR        float64 `json:"adr"`        // Average daily range
	Name       string  `json:"name"`
	Exchange   string  `json:"exchange"`
	Sector     string  `json:"sector"`
	Industry   string  `json:"industry"`
}

// Configuration
const (
	MIN_DOLLAR_VOLUME  = 10000000.0  // Minimum Dollar Volume
	MIN_GAP_UP_PERCENT = 10.0        // Minimum Gap Up in percent
	MIN_ADR_PERCENT    = 4.0         // Minimum Average Daily Range in percent
	MIN_MARKET_CAP     = 200000000.0 // Minimum Market Cap in dollars
	HISTORY_DAYS       = 21          // Number of days for historical analysis
)

func FilterStocks(apiKey string) {
	fmt.Println("Starting 3-stage stock filter program...")

	// Stage 1: Get bulk quotes data and filter by gap up
	fmt.Println("Stage 1: Fetching bulk quotes and filtering by gap up...")
	gapUpStocks, err := stage1FilterByGapUp(apiKey)
	if err != nil {
		log.Fatalf("Error in Stage 1: %v", err)
	}

	fmt.Printf("Stage 1 complete. Found %d stocks with significant gap up.\n", len(gapUpStocks))

	// Stage 2: Filter by market cap
	fmt.Println("Stage 2: Filtering by market cap...")
	marketCapStocks, err := stage2FilterByMarketCap(apiKey, gapUpStocks)
	if err != nil {
		log.Fatalf("Error in Stage 2: %v", err)
	}

	fmt.Printf("Stage 2 complete. Found %d stocks with sufficient market cap.\n", len(marketCapStocks))

	// Stage 3: Calculate and filter by dollar volume and ADR
	fmt.Println("Stage 3: Calculating historical metrics (DolVol and ADR)...")
	finalFilteredStocks, err := stage3FilterByHistoricalMetrics(apiKey, marketCapStocks)
	if err != nil {
		log.Fatalf("Error in Stage 3: %v", err)
	}

	// Output results to JSON file
	err = outputToJSON(finalFilteredStocks)
	if err != nil {
		log.Fatalf("Error writing to JSON: %v", err)
	}

	fmt.Printf("Filter complete. Found %d stocks matching all criteria.\n", len(finalFilteredStocks))
}

// Stage 1: Filter by gap up
func stage1FilterByGapUp(apiKey string) ([]StockData, error) {
	// Get stock symbols from config file
	symbols, err := getStockSymbols()
	if err != nil {
		return nil, fmt.Errorf("failed to get stock symbols: %v", err)
	}

	// Process in batches of 100 symbols
	const batchSize = 100
	var gapUpStocks []StockData

	var wg sync.WaitGroup
	resultCh := make(chan []StockData, (len(symbols)/batchSize)+1) // Channel to collect results

	// Process symbols in batches
	for i := 0; i < len(symbols); i += batchSize {
		end := i + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}

		// Create a batch of symbols
		batch := symbols[i:end]
		batchSymbols := strings.Join(batch, ",")

		wg.Add(1) // Increment the counter for each goroutine
		go func(symbols string) {
			defer wg.Done()
			// Get bulk quotes for this batch
			quotes, err := getBulkQuotesReq(apiKey, symbols)
			if err != nil {
				fmt.Printf("Error fetching bulk quotes for batch: %v\n", err)
				return
			}
			
			// Filter by gap up
			var batchResults []StockData
			for _, stock := range quotes {
				if (stock.ExtendedHoursQuote == 0 || stock.Close == 0) {
					continue // Skip if data is not available
				}
				// Calculate gap up percentage: ((Open - Close)/Close) * 100
				gapUp := ((stock.ExtendedHoursQuote - stock.Close) / stock.Close) * 100
				
				if gapUp >= MIN_GAP_UP_PERCENT {
					fmt.Printf("Stock %s has gap up of %.2f%%\n", stock.Symbol, gapUp)
					batchResults = append(batchResults, stock)
				}
			}
			
			resultCh <- batchResults
		}(batchSymbols)
		
		// Add a delay to avoid hitting API rate limits
		time.Sleep(250 * time.Millisecond)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(resultCh) // Close the channel after all goroutines are done

	// Collect all results
	for result := range resultCh {
		gapUpStocks = append(gapUpStocks, result...)
	}

	return gapUpStocks, nil
}

// Stage 2: Filter by market cap
func stage2FilterByMarketCap(apiKey string, stocks []StockData) ([]FilteredStock, error) {
	var marketCapStocks []FilteredStock

	for _, stock := range stocks {
		fmt.Printf("Checking market cap for %s...\n", stock.Symbol)
		
		// Get company overview
		overview, err := getCompanyOverview(apiKey, stock.Symbol)
		if err != nil {
			fmt.Printf("Error getting overview for %s: %v\n", stock.Symbol, err)
			continue
		}
		
		// Calculate gap up for output consistency
		gapUp := ((stock.ExtendedHoursQuote - stock.Close) / stock.Close) * 100
		
		// Check market cap threshold
		if overview.MarketCap >= MIN_MARKET_CAP {
			marketCapStocks = append(marketCapStocks, FilteredStock{
				Symbol: stock.Symbol,
				StockInfo: StockStats{
					Timestamp: stock.Timestamp,
					MarketCap: overview.MarketCap,
					GapUp:     gapUp,
					Name:      overview.Name,
					Exchange:  overview.Exchange,
					Sector:    overview.Sector,
					Industry:  overview.Industry,
				},
			})
			fmt.Printf("Added %s to market cap filtered list (Cap: $%.2f)\n", stock.Symbol, overview.MarketCap)
		}
	}

	return marketCapStocks, nil
}

// Stage 3: Calculate and filter by historical metrics (DolVol and ADR)
func stage3FilterByHistoricalMetrics(apiKey string, stocks []FilteredStock) ([]FilteredStock, error) {
	var finalStocks []FilteredStock

	for _, stock := range stocks {
		fmt.Printf("Calculating historical metrics for %s...\n", stock.Symbol)
		
		// Get historical data
		timeSeriesData, err := getTimeSeriesDaily(apiKey, stock.Symbol)
		if err != nil {
			fmt.Printf("Error getting time series for %s: %v\n", stock.Symbol, err)
			continue
		}
		
		// Calculate 21-day average metrics
		dolVol, adr, err := calculateHistoricalMetrics(timeSeriesData)
		if err != nil {
			fmt.Printf("Error calculating metrics for %s: %v\n", stock.Symbol, err)
			continue
		}
		
		// Update stock with calculated metrics
		stock.StockInfo.DolVol = dolVol
		stock.StockInfo.ADR = adr
		
		// Apply final filtering criteria
		if dolVol >= MIN_DOLLAR_VOLUME && adr >= MIN_ADR_PERCENT {
			finalStocks = append(finalStocks, stock)
			fmt.Printf("Added %s to final list (DolVol: $%.2f, ADR: %.2f%%)\n", 
				stock.Symbol, dolVol, adr)
		}
	}

	return finalStocks, nil
}

// Helper function to get stock symbols from config file
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

	// Parse stock symbols (assuming first column)
	var symbols []string
	for _, row := range rows {
		if len(row) > 0 && row[0] != "Symbol" && row[0] != "" {
			symbols = append(symbols, row[0])
		}
	}

	return symbols, nil
}

// Function to get bulk quotes
func getBulkQuotesReq(apiKey string, batchSymbols string) ([]StockData, error) {
	url := fmt.Sprintf("https://www.alphavantage.co/query?function=REALTIME_BULK_QUOTES&entitlement=realtime&symbol=%s&apikey=%s",
		batchSymbols, apiKey)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var bulkResponse BulkQuoteResponse
	err = json.Unmarshal(body, &bulkResponse)
	if err != nil {
		return nil, fmt.Errorf("error parsing bulk quotes response: %v", err)
	}

	fmt.Println(string(body))

	return bulkResponse.Data, nil
}

// Function to get company overview
func getCompanyOverview(apiKey string, symbol string) (*OverviewResponse, error) {
	url := fmt.Sprintf("https://www.alphavantage.co/query?function=OVERVIEW&symbol=%s&apikey=%s", 
		symbol, apiKey)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var overview OverviewResponse
	err = json.Unmarshal(body, &overview)
	if err != nil {
		return nil, fmt.Errorf("error parsing overview response: %v", err)
	}

	// Convert market cap string to float
	marketCap, err := strconv.ParseFloat(overview.MarketCapStr, 64)
	if err != nil {
		fmt.Printf("Warning: Could not parse market cap for %s: %v. Using 0.\n", symbol, err)
		marketCap = 0
	}
	overview.MarketCap = marketCap

	return &overview, nil
}

// Function to get time series daily data
func getTimeSeriesDaily(apiKey string, symbol string) (*TimeSeriesResponse, error) {
	url := fmt.Sprintf("https://www.alphavantage.co/query?function=TIME_SERIES_DAILY&symbol=%s&outputsize=compact&apikey=%s", 
		symbol, apiKey)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var timeSeriesResp TimeSeriesResponse
	err = json.Unmarshal(body, &timeSeriesResp)
	if err != nil {
		return nil, fmt.Errorf("error parsing time series response: %v", err)
	}

	return &timeSeriesResp, nil
}

// Calculate historical metrics: DolVol and ADR
func calculateHistoricalMetrics(timeSeriesData *TimeSeriesResponse) (float64, float64, error) {
	// Extract and sort dates
	var dates []string
	for date := range timeSeriesData.TimeSeries {
		dates = append(dates, date)
	}
	
	// Sort dates in descending order (newest first)
	sort.Slice(dates, func(i, j int) bool {
		return dates[i] > dates[j]
	})

	// Check if we have enough data
	if len(dates) < HISTORY_DAYS {
		return 0, 0, fmt.Errorf("not enough historical data, only %d days available", len(dates))
	}

	var dolVolSum float64 = 0
	var adrSum float64 = 0
	var daysCount int = 0

	// Process the last HISTORY_DAYS days
	for i := 0; i < HISTORY_DAYS && i < len(dates); i++ {
		dataItem := timeSeriesData.TimeSeries[dates[i]]

		// Parse values
		high, err := parseFloat(dataItem.High)
		if err != nil {
			continue
		}

		low, err := parseFloat(dataItem.Low)
		if err != nil {
			continue
		}

		close, err := parseFloat(dataItem.Close)
		if err != nil {
			continue
		}

		volume, err := parseFloat(dataItem.Volume)
		if err != nil {
			continue
		}

		// Calculate dollar volume: Volume * Close
		dolVol := volume * close
		dolVolSum += dolVol

		// Calculate daily range: 100 * (High/Low)
		dailyRange := 100 * (high / low)
		adrSum += dailyRange

		daysCount++
	}

	// Calculate averages
	var avgDolVol, avgADR float64 = 0, 0
	if daysCount > 0 {
		avgDolVol = dolVolSum / float64(daysCount)
		avgADR = adrSum / float64(daysCount)
	}

	return avgDolVol, avgADR, nil
}

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

// Optional: For testing without making API calls
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