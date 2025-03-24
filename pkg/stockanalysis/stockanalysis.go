package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// StockData holds analyzed metrics for each stock
type StockData struct {
	ADR           float64 `json:"ADR"`           // Average Daily Range
	Dolvol        float64 `json:"Dolvol"`        // Dollar Volume
	Volatility    float64 `json:"Volatility"`    // Price Volatility
	Growth        float64 `json:"Growth"`        // Price Growth
	PERatio       float64 `json:"PERatio"`       // Price-to-Earnings Ratio
	MarketCap     float64 `json:"MarketCap"`     // Market Capitalization
	DividendYield float64 `json:"DividendYield"` // Dividend Yield
}

// Config struct to hold stock symbols from CSV
type config struct {
	StockSymbols []string `csv:"stock_symbols"`
	// Add filter criteria for flexibility
	Filters struct {
		MinADR        float64
		MaxADR        float64
		MinDolvol     float64
		MaxDolvol     float64
		MinVolatility float64
		MaxVolatility float64
		MinGrowth     float64
		MinPERatio    float64
		MaxPERatio    float64
		MinMarketCap  float64
		MinDivYield   float64
	}
}

// AlphaVantageResponse represents the JSON structure returned by Alpha Vantage API
type AlphaVantageResponse struct {
	MetaData   map[string]string            `json:"Meta Data"`
	TimeSeries map[string]map[string]string `json:"Time Series (1min)"`
}

// YahooFinanceResponse represents the structure returned by Yahoo Finance API
type YahooFinanceResponse struct {
	QuoteResponse struct {
		Result []struct {
			Symbol        string  `json:"symbol"`
			TrailingPE    float64 `json:"trailingPE"`
			MarketCap     float64 `json:"marketCap"`
			DividendYield float64 `json:"dividendYield"`
		} `json:"result"`
		Error interface{} `json:"error"`
	} `json:"quoteResponse"`
}

// StockPriceData holds extracted price data from the API response
type StockPriceData struct {
	HighPrices  []float64
	LowPrices   []float64
	ClosePrices []float64
	Volumes     []float64
}

// loadConfig reads stock symbols from a CSV file
func loadConfig() (*config, error) {
	file, err := os.Open("config.csv")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read all stocks from the CSV file
	stocks, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	var config config

	// Skip header row if present
	startIdx := 0
	if len(stocks) > 0 && (stocks[0][0] == "Symbol" || stocks[0][0] == "stock_symbols") {
		startIdx = 1
	}

	for i := startIdx; i < len(stocks); i++ {
		if len(stocks[i]) > 0 { // Make sure the record has the column
			symbol := stocks[i][0]
			fmt.Println("Added stock symbol:", symbol)
			config.StockSymbols = append(config.StockSymbols, symbol)
		}
	}

	// Set default filter values
	config.Filters.MinADR = 4.0         // Minimum Average Daily Range (%)
	config.Filters.MaxADR = 15.0        // Maximum Average Daily Range (%)
	config.Filters.MinDolvol = 10000000 // Minimum Dollar Volume ($)
	config.Filters.MaxDolvol = math.MaxFloat64
	config.Filters.MinVolatility = 5.0  // Minimum Volatility
	config.Filters.MaxVolatility = 10.0 // Maximum Volatility
	config.Filters.MinGrowth = 50.0     // Minimum Growth (%)
	config.Filters.MinPERatio = 0.0     // Minimum P/E Ratio
	config.Filters.MaxPERatio = 100.0   // Maximum P/E Ratio
	config.Filters.MinMarketCap = 0.0   // Minimum Market Cap
	config.Filters.MinDivYield = 0.0    // Minimum Dividend Yield

	return &config, nil
}

// fetchStockData gets intraday data from Alpha Vantage API
func fetchStockData(symbol string, apiKey string) (*AlphaVantageResponse, error) {
	url := fmt.Sprintf("https://www.alphavantage.co/query?function=TIME_SERIES_INTRADAY&symbol=%s&interval=1min&outputsize=full&apikey=%s", symbol, apiKey)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result AlphaVantageResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	// Check if API returned an error message
	if len(result.TimeSeries) == 0 {
		return nil, fmt.Errorf("API error or no data for symbol %s", symbol)
	}

	return &result, nil
}

// fetchYahooFinanceData gets fundamental data from Yahoo Finance API
func fetchYahooFinanceData(symbols []string) (map[string]struct {
	PERatio       float64
	MarketCap     float64
	DividendYield float64
}, error) {
	// Join symbols with comma for the API request
	symbolsStr := strings.Join(symbols, ",")

	url := fmt.Sprintf("https://query1.finance.yahoo.com/v7/finance/quote?symbols=%s&fields=symbol,trailingPE,marketCap,dividendYield", symbolsStr)

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// Set necessary headers for Yahoo Finance API
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result YahooFinanceResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	// Create a map to store results by symbol
	fundamentalData := make(map[string]struct {
		PERatio       float64
		MarketCap     float64
		DividendYield float64
	})

	// Process results
	for _, stock := range result.QuoteResponse.Result {
		fundamentalData[stock.Symbol] = struct {
			PERatio       float64
			MarketCap     float64
			DividendYield float64
		}{
			PERatio:       stock.TrailingPE,
			MarketCap:     stock.MarketCap,
			DividendYield: stock.DividendYield * 100, // Convert to percentage
		}
	}

	return fundamentalData, nil
}

// extractPriceData extracts time series price data from Alpha Vantage response
func extractPriceData(data *AlphaVantageResponse) StockPriceData {
	var priceData StockPriceData

	// Extract time-ordered price data
	for _, entry := range data.TimeSeries {
		high, _ := strconv.ParseFloat(entry["2. high"], 64)
		low, _ := strconv.ParseFloat(entry["3. low"], 64)
		close, _ := strconv.ParseFloat(entry["4. close"], 64)
		volume, _ := strconv.ParseFloat(entry["5. volume"], 64)

		priceData.HighPrices = append(priceData.HighPrices, high)
		priceData.LowPrices = append(priceData.LowPrices, low)
		priceData.ClosePrices = append(priceData.ClosePrices, close)
		priceData.Volumes = append(priceData.Volumes, volume)
	}

	return priceData
}

// calculateADR calculates Average Daily Range
func calculateADR(priceData StockPriceData) float64 {
	if len(priceData.ClosePrices) == 0 {
		return 0
	}

	var dailyRanges []float64
	for i := 0; i < len(priceData.HighPrices); i++ {
		dailyRange := (priceData.HighPrices[i] - priceData.LowPrices[i]) / priceData.ClosePrices[i] * 100
		dailyRanges = append(dailyRanges, dailyRange)
	}

	return average(dailyRanges)
}

// calculateDollarVolume calculates the average Dollar Volume
func calculateDollarVolume(priceData StockPriceData) float64 {
	var dolVols []float64
	for i := 0; i < len(priceData.ClosePrices); i++ {
		dolVol := priceData.ClosePrices[i] * priceData.Volumes[i]
		dolVols = append(dolVols, dolVol)
	}

	return average(dolVols)
}

// calculateVolatility computes price volatility (standard deviation of returns)
func calculateVolatility(priceData StockPriceData) float64 {
	if len(priceData.ClosePrices) < 2 {
		return 0
	}

	var returns []float64
	for i := 1; i < len(priceData.ClosePrices); i++ {
		ret := (priceData.ClosePrices[i] - priceData.ClosePrices[i-1]) / priceData.ClosePrices[i-1]
		returns = append(returns, ret)
	}

	return standardDeviation(returns)
}

// calculateGrowth computes price growth over the time period
func calculateGrowth(priceData StockPriceData) float64 {
	if len(priceData.ClosePrices) < 2 {
		return 0
	}

	firstPrice := priceData.ClosePrices[0]
	lastPrice := priceData.ClosePrices[len(priceData.ClosePrices)-1]

	return (lastPrice - firstPrice) / firstPrice * 100
}

// Helper function to calculate average
func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// Helper function to calculate standard deviation
func standardDeviation(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}

	avg := average(values)
	var sumSquaredDiff float64
	for _, v := range values {
		diff := v - avg
		sumSquaredDiff += diff * diff
	}

	variance := sumSquaredDiff / float64(len(values))
	return math.Sqrt(variance)
}

// calculateStockMetrics computes all metrics for a stock from intraday data
func calculateStockMetrics(data *AlphaVantageResponse) (StockData, error) {
	var stockData StockData

	// Extract price data from response
	priceData := extractPriceData(data)

	// Calculate each metric using its own function
	stockData.ADR = calculateADR(priceData)
	stockData.Dolvol = calculateDollarVolume(priceData)
	stockData.Volatility = calculateVolatility(priceData)
	stockData.Growth = calculateGrowth(priceData)

	// PERatio, MarketCap, and DividendYield will be filled from Yahoo Finance later

	return stockData, nil
}

// Check if stock meets filter criteria
func meetsFilterCriteria(data StockData, cfg *config) bool {
	if data.ADR < cfg.Filters.MinADR || data.ADR > cfg.Filters.MaxADR {
		return false
	}

	if data.Dolvol < cfg.Filters.MinDolvol || data.Dolvol > cfg.Filters.MaxDolvol {
		return false
	}

	if data.Volatility < cfg.Filters.MinVolatility || data.Volatility > cfg.Filters.MaxVolatility {
		return false
	}

	if data.Growth < cfg.Filters.MinGrowth {
		return false
	}

	if data.PERatio < cfg.Filters.MinPERatio || (data.PERatio > cfg.Filters.MaxPERatio && data.PERatio != 0) {
		return false
	}

	if data.MarketCap < cfg.Filters.MinMarketCap {
		return false
	}

	if data.DividendYield < cfg.Filters.MinDivYield {
		return false
	}

	return true
}

// saveToJSON writes the filtered stocks to a JSON file
func saveToJSON(stocks map[string]StockData) error {
	data, err := json.MarshalIndent(stocks, "", "    ")
	if err != nil {
		return err
	}

	return os.WriteFile("filtered_stocks.json", data, 0644)
}

func main() {
	// Load Alpha Vantage API key from environment
	apiKey := "QN0D4W8UM2AYVQFO"
	if apiKey == "" {
		log.Fatal("Please set the ALPHA_VANTAGE_API_KEY environment variable")
	}

	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Create map to store stock data
	stockDataMap := make(map[string]StockData)

	// Process each stock for intraday data
	fmt.Println("Fetching intraday data from Alpha Vantage...")
	count := 0
	for _, symbol := range cfg.StockSymbols {
		if count > 5 {
			break
		}
		fmt.Printf("Processing %s...\n", symbol)

		// Fetch stock data
		data, err := fetchStockData(symbol, apiKey)
		if err != nil {
			log.Printf("Error fetching data for %s: %v", symbol, err)
			continue
		}

		// Calculate metrics
		metrics, err := calculateStockMetrics(data)
		if err != nil {
			log.Printf("Error calculating metrics for %s: %v", symbol, err)
			continue
		}

		stockDataMap[symbol] = metrics

		// Rate limit API calls (Alpha Vantage has limits)
		if len(cfg.StockSymbols) > 5 {
			time.Sleep(15 * time.Second) // Alpha Vantage free tier allows ~5 calls per minute
		}
		count += 1
	}

	// Batch fetch fundamental data from Yahoo Finance
	fmt.Println("Fetching fundamental data from Yahoo Finance...")
	fundamentalData, err := fetchYahooFinanceData(cfg.StockSymbols)
	if err != nil {
		log.Printf("Error fetching Yahoo Finance data: %v", err)
	} else {
		// Update stock data with fundamental metrics
		count = 0
		for symbol, metrics := range stockDataMap {
			if count > 5 {
				break
			}
			if fundData, ok := fundamentalData[symbol]; ok {
				metrics.PERatio = fundData.PERatio
				metrics.MarketCap = fundData.MarketCap
				metrics.DividendYield = fundData.DividendYield
				stockDataMap[symbol] = metrics
			}
			count += 1
		}
	}

	count = 0
	// Filter stocks based on criteria
	filteredStocks := make(map[string]StockData)
	for symbol, metrics := range stockDataMap {
		if count > 5 {
			break
		}
		if meetsFilterCriteria(metrics, cfg) {
			fmt.Printf("Stock %s meets criteria - ADR: %.2f, Dolvol: %.2f, Volatility: %.2f, Growth: %.2f, P/E: %.2f, MarketCap: %.2f, DivYield: %.2f%%\n",
				symbol, metrics.ADR, metrics.Dolvol, metrics.Volatility, metrics.Growth,
				metrics.PERatio, metrics.MarketCap, metrics.DividendYield)
			filteredStocks[symbol] = metrics
		} else {
			fmt.Printf("Stock %s does not meet criteria\n", symbol)
			fmt.Printf("Stock %s meets criteria - ADR: %.2f, Dolvol: %.2f, Volatility: %.2f, Growth: %.2f, P/E: %.2f, MarketCap: %.2f, DivYield: %.2f%%\n",
				symbol, metrics.ADR, metrics.Dolvol, metrics.Volatility, metrics.Growth,
				metrics.PERatio, metrics.MarketCap, metrics.DividendYield)
			fmt.Println()
		}
		count += 1
	}

	// Save results to JSON file
	if len(filteredStocks) > 0 {
		saveToJSON(filteredStocks)
		fmt.Printf("Saved %d stocks to filtered_stocks.json\n", len(filteredStocks))
	} else {
		fmt.Println("No stocks met the filter criteria")
	}
}
