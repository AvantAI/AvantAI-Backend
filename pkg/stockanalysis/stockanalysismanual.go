package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
)

// Stage 1 Filter Struct
type Stage1Filter struct {
	MinADR    float64
	MaxADR    float64
	MinDolvol float64
	MaxDolvol float64
	MinPrice  float64
	MaxPrice  float64
}

// Stage 2 Filter Struct
type Stage2Filter struct {
	MinSMA10      float64
	MaxSMA10      float64
	MinSMA50      float64
	MaxSMA50      float64
	CrossoverType string
}

// Config struct to hold stock symbols and filter stages
type config struct {
	StockSymbols []string
	Stage1Filter Stage1Filter
	Stage2Filter Stage2Filter
}

// StockData struct with SMA fields
type StockData struct {
	Symbol        string    `json:"Symbol"`
	ADR           float64   `json:"ADR"`
	Dolvol        float64   `json:"Dolvol"`
	Volatility    float64   `json:"Volatility"`
	Growth        float64   `json:"Growth"`
	PERatio       float64   `json:"PERatio"`
	MarketCap     float64   `json:"MarketCap"`
	DividendYield float64   `json:"DividendYield"`
	CurrentPrice  float64   `json:"CurrentPrice"`
	ClosePrices   []float64 `json:"-"`
	SMA           struct {
		SMA10 float64 `json:"SMA10"`
		SMA50 float64 `json:"SMA50"`
	} `json:"SMA"`
}

// StockPriceData holds extracted price data from the API response
type StockPriceData struct {
	HighPrices  []float64
	LowPrices   []float64
	ClosePrices []float64
	Volumes     []float64
}

// loadConfig reads configuration for filter stages
func loadConfig() (*config, error) {
	file, err := os.Open("config.csv")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	var cfg config

	// Parse stock symbols (assuming first column)
	for _, row := range rows {
		if len(row) > 0 && row[0] != "Symbol" && row[0] != "" {
			cfg.StockSymbols = append(cfg.StockSymbols, row[0])
		}
	}

	// Stage 1 Filter Configuration
	cfg.Stage1Filter = Stage1Filter{
		MinADR:    3.0,     // Minimum Average Daily Range (%)
		MaxADR:    15.0,    // Maximum Average Daily Range (%)
		MinDolvol: 5000000, // Minimum Dollar Volume ($)
		MinPrice:  5.0,     // Minimum stock price
		MaxPrice:  500.0,   // Maximum stock price
	}

	// Stage 2 Filter Configuration
	cfg.Stage2Filter = Stage2Filter{
		MinSMA10:      0,
		MaxSMA10:      math.MaxFloat64,
		MinSMA50:      0,
		MaxSMA50:      math.MaxFloat64,
		CrossoverType: "golden_cross", // or "death_cross"
	}

	return &cfg, nil
}

// calculateSMA calculates Simple Moving Average for given periods
func calculateSMA(prices []float64, period int) float64 {
	if len(prices) < period {
		return 0
	}

	// Use the most recent prices for SMA calculation
	startIndex := max(0, len(prices)-period)
	periodPrices := prices[startIndex:]

	var sum float64
	for _, price := range periodPrices {
		sum += price
	}

	return sum / float64(len(periodPrices))
}

// calculateAllSMAs computes multiple SMA periods
func calculateAllSMAs(stockData *StockData) {
	// Ensure we have enough close prices
	if len(stockData.ClosePrices) < 50 {
		return
	}

	// Calculate SMAs
	stockData.SMA.SMA10 = calculateSMA(stockData.ClosePrices, 10)
	stockData.SMA.SMA50 = calculateSMA(stockData.ClosePrices, 50)
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

// Helper function to get max of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// calculateStockMetrics computes all metrics for a stock from price data
func calculateStockMetrics(priceData StockPriceData) StockData {
	return StockData{
		Symbol:        priceData.Symbol,
		ADR:           calculateADR(priceData),
		Dolvol:        calculateDollarVolume(priceData),
		Volatility:    calculateVolatility(priceData),
		Growth:        calculateGrowth(priceData),
		PERatio:       priceData.PERatio,
		MarketCap:     priceData.MarketCap,
		DividendYield: priceData.DivYield,
	}
}

// createDefaultConfig sets up default filter criteria
func createDefaultConfig() *config {
	var cfg config

	cfg.Filters.MinADR = 4.0         // Minimum Average Daily Range (%)
	cfg.Filters.MaxADR = 15.0        // Maximum Average Daily Range (%)
	cfg.Filters.MinDolvol = 10000000 // Minimum Dollar Volume ($)
	cfg.Filters.MaxDolvol = math.MaxFloat64
	cfg.Filters.MinVolatility = 5.0  // Minimum Volatility
	cfg.Filters.MaxVolatility = 10.0 // Maximum Volatility
	cfg.Filters.MinGrowth = 50.0     // Minimum Growth (%)

	return &cfg
}

// Check if stock meets filter criteria
func meetsFilterCriteria(data StockData, cfg *config) bool {
	if data.ADR < cfg.Filters.MinADR || data.ADR > cfg.Filters.MaxADR {
		return false
	}

	if data.Dolvol < cfg.Filters.MinDolvol || data.Dolvol > cfg.Filters.MaxDolvol {
		return false
	}

	// if data.Volatility < cfg.Filters.MinVolatility || data.Volatility > cfg.Filters.MaxVolatility {
	// 	return false
	// }

	// if data.Growth < cfg.Filters.MinGrowth {
	// 	return false
	// }

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
	// Create default configuration
	cfg := createDefaultConfig()

	// Load stock data from CSV
	stockPriceDataList, err := loadStockDataFromCSV("stock_data.csv")
	if err != nil {
		log.Fatalf("Error loading stock data: %v", err)
	}

	// Process and analyze each stock's data
	stockDataMap := make(map[string]StockData)
	filteredStocks := make(map[string]StockData)

	for _, priceData := range stockPriceDataList {
		// Calculate metrics
		metrics := calculateStockMetrics(priceData)
		stockDataMap[priceData.Symbol] = metrics

		// Check if stock meets filter criteria
		if meetsFilterCriteria(metrics, cfg) {
			fmt.Printf("Stock %s meets criteria - ADR: %.2f, Dolvol: %.2f, Volatility: %.2f, Growth: %.2f, P/E: %.2f, MarketCap: %.2f, DivYield: %.2f%%\n",
				metrics.Symbol, metrics.ADR, metrics.Dolvol, metrics.Volatility, metrics.Growth,
				metrics.PERatio, metrics.MarketCap, metrics.DividendYield)
			fmt.Println()
			filteredStocks[priceData.Symbol] = metrics
		} else {
			fmt.Printf("Stock %s does not meet criteria\n", priceData.Symbol)
			fmt.Printf("Stock %s does not meet criteria - ADR: %.2f, Dolvol: %.2f, Volatility: %.2f, Growth: %.2f, P/E: %.2f, MarketCap: %.2f, DivYield: %.2f%%\n",
				metrics.Symbol, metrics.ADR, metrics.Dolvol, metrics.Volatility, metrics.Growth,
				metrics.PERatio, metrics.MarketCap, metrics.DividendYield)
			fmt.Println()
		}
	}

	// Save results to JSON file
	if len(filteredStocks) > 0 {
		saveToJSON(filteredStocks)
		fmt.Printf("Saved %d stocks to filtered_stocks.json\n", len(filteredStocks))
	} else {
		fmt.Println("No stocks met the filter criteria")
	}
}
