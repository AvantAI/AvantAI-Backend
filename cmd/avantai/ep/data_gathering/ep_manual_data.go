package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// AlphaVantageIntradayResponse represents the structure of the Alpha Vantage TIME_SERIES_INTRADAY response
type AlphaVantageIntradayResponse struct {
	MetaData struct {
		Information   string `json:"1. Information"`
		Symbol        string `json:"2. Symbol"`
		LastRefreshed string `json:"3. Last Refreshed"`
		Interval      string `json:"4. Interval"`
		OutputSize    string `json:"5. Output Size"`
		TimeZone      string `json:"6. Time Zone"`
	} `json:"Meta Data"`
	TimeSeries map[string]struct {
		Open   string `json:"1. open"`
		High   string `json:"2. high"`
		Low    string `json:"3. low"`
		Close  string `json:"4. close"`
		Volume string `json:"5. volume"`
	} `json:"Time Series (1min)"`
}

// StockQuote represents our final stock data structure
type StockQuote struct {
	Symbol                     string `json:"symbol"`
	Timestamp                  string `json:"timestamp"`
	Open                       string `json:"open"`
	High                       string `json:"high"`
	Low                        string `json:"low"`
	Close                      string `json:"close"`
	Volume                     string `json:"volume"`
	PreviousClose              string `json:"previous_close"`
	Change                     string `json:"change"`
	ChangePercent              string `json:"change_percent"`
	ExtendedHoursQuote         string `json:"extended_hours_quote"`
	ExtendedHoursChange        string `json:"extended_hours_change"`
	ExtendedHoursChangePercent string `json:"extended_hours_change_percent"`
}

func main() {

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	apiKey := os.Getenv("API_KEY")
	symbol := "MSFT"
	targetDate := "2023-08-22"
	outputFile := "stock_data.json"

	// Calculate previous trading day
	previousDayStr := "2023-08-21"
	previousDayMonth := "2023-08"

	fmt.Println("Target date:", targetDate)
	fmt.Println("Previous trading day:", previousDayStr)

	// Get intraday data from Alpha Vantage
	intradayData, err := getIntradayData(apiKey, targetDate[0:7], symbol)
	if err != nil {
		fmt.Printf("Error fetching intraday data: %v\n", err)
		return
	}

	// Get intraday data from Alpha Vantage for previous day
	previousDayData, err := getIntradayData(apiKey, previousDayMonth, symbol)
	if err != nil {
		fmt.Printf("Error fetching previous day intraday data: %v\n", err)
		return
	}

	// Get previous day's closing price at 4:00 PM
	previousDayClose, err := getPreviousDayCloseAt4PM(previousDayData, previousDayStr)
	if err != nil {
		fmt.Printf("Error getting previous day close: %v\n", err)
		return
	}

	fmt.Println(previousDayClose)

	if previousDayClose == "" {
		fmt.Printf("Couldn't find previous day's closing price at 4:00 PM\n")
		return
	}

	// Get the opening minute quote
	stockQuote, err := getOpeningMinuteQuote(intradayData, symbol, targetDate, previousDayClose)
	if err != nil {
		fmt.Printf("Error processing data: %v\n", err)
		return
	}

	if stockQuote == nil {
		fmt.Printf("No opening minute data found for date: %s\n", targetDate)
		return
	}

	// Load existing data or create new data slice
	var existingData []StockQuote
	loadExistingData(outputFile, &existingData)

	// Add new quote and save
	updatedData := append(existingData, *stockQuote)
	saveStockData(outputFile, updatedData)

	fmt.Printf("Successfully processed opening minute quote for %s on %s and saved to %s\n",
		symbol, targetDate, outputFile)
}

func getIntradayData(apiKey, month string, symbol string) (AlphaVantageIntradayResponse, error) {
	var response AlphaVantageIntradayResponse

	url := fmt.Sprintf(
		"https://www.alphavantage.co/query?function=TIME_SERIES_INTRADAY&symbol=%s&extended_hours=true&interval=1min&month=%s&outputsize=full&apikey=%s",
		symbol, month, apiKey)

	resp, err := http.Get(url)
	if err != nil {
		return response, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return response, err
	}

	err = json.Unmarshal(body, &response)
	if err != nil {
		return response, err
	}

	return response, nil
}

func getPreviousDayCloseAt4PM(data AlphaVantageIntradayResponse, previousDayStr string) (string, error) {
	// Look for the 4:00 PM entry (regular market close time)
	targetTime := previousDayStr + " 16:00:00"

	// Check if we have the exact 4:00 PM timestamp
	if quote, found := data.TimeSeries[targetTime]; found {
		return quote.Close, nil
	}

	// If not found at exact 4:00 PM, find the closest time to 4:00 PM
	var closestTime string
	minDiff := int64(86400) // Initialize with seconds in a day

	for timestamp := range data.TimeSeries {
		if strings.HasPrefix(timestamp, previousDayStr) {
			timestampParts := strings.Split(timestamp, " ")
			if len(timestampParts) != 2 {
				continue
			}

			timeStr := timestampParts[1]
			t1, err := time.Parse("15:04:05", timeStr)
			if err != nil {
				continue
			}

			// Parse 16:00:00
			t2, _ := time.Parse("15:04:05", "16:00:00")

			// Calculate difference in seconds
			diff := t1.Sub(t2).Seconds()
			absDiff := int64(diff)
			if diff < 0 {
				absDiff = -absDiff
			}

			if absDiff < minDiff {
				minDiff = absDiff
				closestTime = timestamp
			}
		}
	}

	if closestTime != "" {
		return data.TimeSeries[closestTime].Close, nil
	}

	return "", fmt.Errorf("no close time found for previous day %s", previousDayStr)
}

func getOpeningMinuteQuote(data AlphaVantageIntradayResponse, symbol, targetDate, previousDayClose string) (*StockQuote, error) {
	// Collect all timestamps for the target date
	var timestamps []string
	for timestamp := range data.TimeSeries {
		if strings.HasPrefix(timestamp, targetDate) {
			timestamps = append(timestamps, timestamp)
		}
	}

	if len(timestamps) == 0 {
		return nil, nil // No data for this date
	}

	// Sort timestamps to find the first minute of trading
	sort.Strings(timestamps)

	// Regular market hours typically start at 9:30 AM ET
	marketOpenTime := " 09:29:00"
	var openingTimestamp string

	// Find the first timestamp at or after market open
	for _, ts := range timestamps {
		timeStr := ts[len(targetDate):]
		if timeStr >= marketOpenTime {
			openingTimestamp = ts
			break
		}
	}

	// If we didn't find a market opening time, try to get the first timestamp of the day
	if openingTimestamp == "" && len(timestamps) > 0 {
		openingTimestamp = timestamps[0]
	}

	if openingTimestamp == "" {
		return nil, nil // No suitable timestamp found
	}

	// Get the quote for the opening minute
	quote := data.TimeSeries[openingTimestamp]

	// Calculate change and change percent (should be 0 as we're using same value for both)
	change := 0.0
	changePercent := 0.0

	stockQuote := &StockQuote{
		Symbol:                     symbol,
		Timestamp:                  openingTimestamp,
		Open:                       quote.Open,
		High:                       quote.High,
		Low:                        quote.Low,
		Close:                      previousDayClose,    // Use previous day's close value
		Volume:                     quote.Volume,
		PreviousClose:              previousDayClose,    // Use previous day's close value
		Change:                     fmt.Sprintf("%.4f", change),
		ChangePercent:              fmt.Sprintf("%.4f", changePercent),
		ExtendedHoursQuote:         "0",
		ExtendedHoursChange:        "0",
		ExtendedHoursChangePercent: "0",
	}

	return stockQuote, nil
}

func parseFloat(s string) (float64, error) {
	var result float64
	_, err := fmt.Sscanf(s, "%f", &result)
	return result, err
}

func loadExistingData(filename string, stockData *[]StockQuote) {
	// Check if file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return // File doesn't exist, so we'll create a new one
	}

	// Read existing file
	fileData, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Warning: Could not read existing file: %v\n", err)
		return
	}

	// Empty file or invalid JSON
	if len(fileData) == 0 {
		return
	}

	// Try to unmarshal
	err = json.Unmarshal(fileData, stockData)
	if err != nil {
		fmt.Printf("Warning: Could not parse existing file: %v\n", err)
	}
}

func saveStockData(filename string, stockData []StockQuote) error {
	jsonData, err := json.MarshalIndent(stockData, "", "    ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, jsonData, 0644)
}
