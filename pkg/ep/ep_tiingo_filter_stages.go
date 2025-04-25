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
	"sync"
	"time"
)

// TiingoDailyPrice represents the Tiingo API response for daily price data
type TiingoDailyPrice struct {
	Date        string  `json:"date"`
	Open        float64 `json:"open"`
	High        float64 `json:"high"`
	Low         float64 `json:"low"`
	Close       float64 `json:"close"`
	Volume      float64 `json:"volume"`
	AdjOpen     float64 `json:"adjOpen"`
	AdjHigh     float64 `json:"adjHigh"`
	AdjLow      float64 `json:"adjLow"`
	AdjClose    float64 `json:"adjClose"`
	AdjVolume   float64 `json:"adjVolume"`
	DivCash     float64 `json:"divCash"`
	SplitFactor float64 `json:"splitFactor"`
}

// TiingoMetaData represents the Tiingo API response for stock metadata
type TiingoMetaData struct {
	Ticker        string   `json:"ticker"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	StartDate     string   `json:"startDate"`
	EndDate       string   `json:"endDate"`
	ExchangeCode  string   `json:"exchangeCode"`
	PrimaryExch   string   `json:"primaryExch"`
	Sector        string   `json:"sector"`
	Industry      string   `json:"industry"`
	SicCode       int      `json:"sicCode"`
	SicSector     string   `json:"sicSector"`
	SicIndustry   string   `json:"sicIndustry"`
	IsActive      bool     `json:"isActive"`
	Tickers       []string `json:"tickers"`
	PermaTicker   string   `json:"permaTicker"`
	MarketCap     float64  `json:"marketCap"`
	PriceSnapshot float64  `json:"priceSnapshot"`
}

// TiingoEODPrice represents the Tiingo API response for end-of-day price data
type TiingoEODPrice struct {
	Date          string  `json:"date"`
	Close         float64 `json:"close"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	Open          float64 `json:"open"`
	Volume        float64 `json:"volume"`
	AdjClose      float64 `json:"adjClose"`
	AdjHigh       float64 `json:"adjHigh"`
	AdjLow        float64 `json:"adjLow"`
	AdjOpen       float64 `json:"adjOpen"`
	AdjVolume     float64 `json:"adjVolume"`
	DivCash       float64 `json:"divCash"`
	SplitFactor   float64 `json:"splitFactor"`
	PreviousClose float64 `json:"-"` // Not from API, calculated
	Change        float64 `json:"-"` // Not from API, calculated
	ChangePercent float64 `json:"-"` // Not from API, calculated
}

// StockData represents our consolidated data structure for stock information
type StockData struct {
	Symbol        string          `json:"symbol"`
	Timestamp     string          `json:"timestamp"`
	Open          float64         `json:"open"`
	High          float64         `json:"high"`
	Low           float64         `json:"low"`
	Close         float64         `json:"close"`
	Volume        float64         `json:"volume"`
	PreviousClose float64         `json:"previous_close"`
	Change        float64         `json:"change"`
	ChangePercent float64         `json:"change_percent"`
	GapUp         float64         `json:"gap_up"`
	MetaData      *TiingoMetaData `json:"meta_data,omitempty"`
}

// Our filtered output structure
type FilteredStock struct {
	Symbol    string     `json:"symbol"`
	StockInfo StockStats `json:"stock_info"`
}

type StockStats struct {
	Timestamp string  `json:"timestamp"`
	MarketCap float64 `json:"market_cap"`
	DolVol    float64 `json:"dolvol"` // Average dollar volume over 21 days
	GapUp     float64 `json:"gap-up"` // Gap up percentage
	ADR       float64 `json:"adr"`    // Average daily range
	Name      string  `json:"name"`
	Exchange  string  `json:"exchange"`
	Sector    string  `json:"sector"`
	Industry  string  `json:"industry"`
}

// Configuration
const (
	MIN_DOLLAR_VOLUME  = 10000000.0  // Minimum Dollar Volume
	MIN_GAP_UP_PERCENT = 10.0        // Minimum Gap Up in percent
	MIN_ADR_PERCENT    = 4.0         // Minimum Average Daily Range in percent
	MIN_MARKET_CAP     = 200000000.0 // Minimum Market Cap in dollars
	TIINGO_BASE_URL    = "https://api.tiingo.com"
)

func FilterStocks(apiKey string) {
	fmt.Println("Starting 3-stage stock filter program using Tiingo API...")

	// Stage 1: Get daily prices and filter by gap up
	fmt.Println("Stage 1: Fetching daily prices and filtering by gap up...")
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

	// Process in batches of 20 symbols (Tiingo has different rate limits)
	const batchSize = 20
	var gapUpStocks []StockData

	var wg sync.WaitGroup
	resultCh := make(chan []StockData, (len(symbols)/batchSize)+1) // Channel to collect results
	errorCh := make(chan error, len(symbols))                      // Channel to collect errors

	// Get today's date and yesterday's date
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	// Process symbols in batches
	for i := 0; i < len(symbols); i += batchSize {
		end := i + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}

		// Create a batch of symbols
		batch := symbols[i:end]

		wg.Add(1) // Increment the counter for each goroutine
		go func(batchSymbols []string) {
			defer wg.Done()

			var batchResults []StockData

			for _, symbol := range batchSymbols {
				// Get EOD data for this symbol
				stockData, err := getEODData(apiKey, symbol, yesterday, today)
				if err != nil {
					errorCh <- fmt.Errorf("error fetching EOD data for %s: %v", symbol, err)
					continue
				}

				// Need at least 2 days of data to calculate gap up
				if len(stockData) < 2 {
					errorCh <- fmt.Errorf("insufficient data for %s", symbol)
					continue
				}

				// Calculate gap up
				currentDay := stockData[0]
				previousDay := stockData[1]

				gapUp := ((currentDay.Open - previousDay.Close) / previousDay.Close) * 100

				data := StockData{
					Symbol:        symbol,
					Timestamp:     currentDay.Date,
					Open:          currentDay.Open,
					High:          currentDay.High,
					Low:           currentDay.Low,
					Close:         currentDay.Close,
					Volume:        currentDay.Volume,
					PreviousClose: previousDay.Close,
					Change:        currentDay.Close - previousDay.Close,
					ChangePercent: ((currentDay.Close - previousDay.Close) / previousDay.Close) * 100,
					GapUp:         gapUp,
				}

				if gapUp >= MIN_GAP_UP_PERCENT {
					fmt.Printf("Stock %s has gap up of %.2f%%\n", symbol, gapUp)
					batchResults = append(batchResults, data)
				}
			}

			resultCh <- batchResults
		}(batch)

		// Add a delay to avoid hitting API rate limits
		time.Sleep(500 * time.Millisecond)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(resultCh) // Close the channels after all goroutines are done
	close(errorCh)

	// Collect errors
	for err := range errorCh {
		fmt.Printf("Warning: %v\n", err)
	}

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

		// Get metadata for this stock
		metadata, err := getStockMetadata(apiKey, stock.Symbol)
		if err != nil {
			fmt.Printf("Error getting metadata for %s: %v\n", stock.Symbol, err)
			continue
		}

		// Check market cap threshold
		if metadata.MarketCap >= MIN_MARKET_CAP {
			marketCapStocks = append(marketCapStocks, FilteredStock{
				Symbol: stock.Symbol,
				StockInfo: StockStats{
					Timestamp: stock.Timestamp,
					MarketCap: metadata.MarketCap,
					GapUp:     stock.GapUp,
					Name:      metadata.Name,
					Exchange:  metadata.ExchangeCode,
					Sector:    metadata.Sector,
					Industry:  metadata.Industry,
				},
			})
			fmt.Printf("Added %s to market cap filtered list (Cap: $%.2f)\n", stock.Symbol, metadata.MarketCap)
		}
	}

	return marketCapStocks, nil
}

// Stage 3: Calculate and filter by historical metrics (DolVol and ADR)
func stage3FilterByHistoricalMetrics(apiKey string, stocks []FilteredStock) ([]FilteredStock, error) {
	var finalStocks []FilteredStock

	// Calculate end date (today) and start date (21 days ago)
	endDate := time.Now().Format("2006-01-02")
	startDate := time.Now().AddDate(0, 0, -30).Format("2006-01-02") // Get extra days to ensure we have enough data

	for _, stock := range stocks {
		fmt.Printf("Calculating historical metrics for %s...\n", stock.Symbol)

		// Get historical data
		historicalData, err := getHistoricalData(apiKey, stock.Symbol, startDate, endDate)
		if err != nil {
			fmt.Printf("Error getting historical data for %s: %v\n", stock.Symbol, err)
			continue
		}

		// Calculate metrics
		dolVol, adr, err := calculateHistoricalMetrics(historicalData)
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

// Function to get EOD (End-of-Day) data for a specific symbol
func getEODData(apiKey string, symbol string, startDate string, endDate string) ([]TiingoEODPrice, error) {
	url := fmt.Sprintf("%s/tiingo/daily/%s/prices?startDate=%s&endDate=%s&format=json&token=%s",
		TIINGO_BASE_URL, symbol, startDate, endDate, apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

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
		return nil, fmt.Errorf("API error: %s", string(body))
	}

	var eodPrices []TiingoEODPrice
	err = json.Unmarshal(body, &eodPrices)
	if err != nil {
		return nil, fmt.Errorf("error parsing EOD response: %v", err)
	}

	// Sort by date (newest first)
	sort.Slice(eodPrices, func(i, j int) bool {
		return eodPrices[i].Date > eodPrices[j].Date
	})

	return eodPrices, nil
}

// Function to get stock metadata
func getStockMetadata(apiKey string, symbol string) (*TiingoMetaData, error) {
	url := fmt.Sprintf("%s/tiingo/daily/%s?token=%s", TIINGO_BASE_URL, symbol, apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

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
		return nil, fmt.Errorf("API error: %s", string(body))
	}

	var metadata TiingoMetaData
	err = json.Unmarshal(body, &metadata)
	if err != nil {
		return nil, fmt.Errorf("error parsing metadata response: %v", err)
	}

	return &metadata, nil
}

// Function to get historical data for a specific symbol
func getHistoricalData(apiKey string, symbol string, startDate string, endDate string) ([]TiingoDailyPrice, error) {
	url := fmt.Sprintf("%s/tiingo/daily/%s/prices?startDate=%s&endDate=%s&format=json&token=%s",
		TIINGO_BASE_URL, symbol, startDate, endDate, apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

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
		return nil, fmt.Errorf("API error: %s", string(body))
	}

	var historicalData []TiingoDailyPrice
	err = json.Unmarshal(body, &historicalData)
	if err != nil {
		return nil, fmt.Errorf("error parsing historical data response: %v", err)
	}

	// Sort by date (newest first)
	sort.Slice(historicalData, func(i, j int) bool {
		return historicalData[i].Date > historicalData[j].Date
	})

	return historicalData, nil
}

// Calculate historical metrics: DolVol and ADR
func calculateHistoricalMetrics(historicalData []TiingoDailyPrice) (float64, float64, error) {
	// Check if we have enough data
	if len(historicalData) < 21 {
		return 0, 0, fmt.Errorf("not enough historical data, only %d days available", len(historicalData))
	}

	var dolVolSum float64 = 0
	var adrSum float64 = 0
	var daysCount int = 0

	// Process the last 21 days
	for i := 0; i < 21 && i < len(historicalData); i++ {
		dataItem := historicalData[i]

		// Calculate dollar volume: Volume * Close
		dolVol := dataItem.Volume * dataItem.Close
		dolVolSum += dolVol

		// Calculate daily range: 100 * (High/Low - 1)
		dailyRange := 100 * ((dataItem.High / dataItem.Low) - 1)
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
