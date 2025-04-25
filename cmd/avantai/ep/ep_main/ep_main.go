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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

// Structure to hold the intraday data response
type TimeSeries struct {
	MetaData   map[string]string            `json:"Meta Data"`
	TimeSeries map[string]map[string]string `json:"Time Series (1min)"`
}

type StockData struct {
	Symbol    string         `json:"symbol"`
	StockData []ep.StockData `json:"stock_data"`
}

// Global slice to store the fetched stock data
var stockDataList []TimeSeries

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	apiKey := os.Getenv("API_KEY")
	// Navigate to the directory and open the file
	filePath := "data/stockdata/filtered_stocks.json"
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// Read the entire file content
	fileContent, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	// Slice to hold the parsed data
	var stocks []ep.FilteredStock

	// Unmarshal the JSON data into the stocks slice
	err = json.Unmarshal(fileContent, &stocks)
	if err != nil {
		log.Fatalf("Error unmarshalling JSON: %v\n", err)
		os.Exit(1)
	}

	// var stockDataList []StockData

	for i := 1; i <= 5; i++ {

		var symbols []string
		var dates []string
		for _, stock := range stocks {
			symbols = append(symbols, stock.Symbol)
			dates = append(dates, stock.StockInfo.Timestamp[0:10])
		}

		// stockData, err := getData(strings.Join(symbols, ","), apiKey)
		// if err != nil {
		// 	log.Fatalf("Error getting stock data: %v\n", err)
		// 	os.Exit(1)
		// }

		stockData, err := getIntradayData(strings.Join(symbols, ","), dates, i, apiKey)
		if err != nil {
			log.Fatalf("Error getting stock data: %v\n", err)
			os.Exit(1)
		}

		// Use a WaitGroup to wait for all goroutines to complete
		var wg sync.WaitGroup

		// for _, stock := range stockData {
		// 	var curStockData []ep.StockData
		// 	// Loop through stockDataList to find the matching stock by its Symbol
		// 	for _, data := range stockDataList {
		// 		if data.Symbol == stock.Symbol {
		// 			curStockData = data.StockData
		// 			break // Once found, you can break the loop
		// 		}
		// 	}

		// 	stockDataList = append(stockDataList, StockData{
		// 		Symbol:    stock.Symbol,
		// 		StockData: append(curStockData, stock),
		// 	})
		// 	// Start the goroutine
		// 	wg.Add(1)
		// 	go runManagerAgent(&wg, curStockData, stock.Symbol)

		// }

		for _, stock := range stockData {
			// Start the goroutine
			wg.Add(1)
			go runManagerAgent(&wg, stock.StockData, stock.Symbol)

		}

		// Wait for all goroutines to finish
		wg.Wait()

		// Wait for 1 minute (60 seconds)
		time.Sleep(1 * time.Minute)
	}
}

func getData(symbols string, apiKey string) ([]ep.StockData, error) {
	// Send the HTTP GET request
	resp, err := http.Get(fmt.Sprintf("https://www.alphavantage.co/query?function=REALTIME_BULK_QUOTES&symbol=%s&apikey=%s", symbols, apiKey))
	if err != nil {
		fmt.Printf("error fetching data from Alpha Vantage: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("error reading response body: %v", err)
	}

	var bulkResponse ep.BulkQuoteResponse
	err = json.Unmarshal(body, &bulkResponse)
	if err != nil {
		fmt.Printf("error parsing bulk quotes response: %v. Response: %s", err, string(body))
		os.Exit(1)
	}

	return bulkResponse.Data, nil

}

func getIntradayData(symbols string, dates []string, increment int, apiKey string) ([]StockData, error) {

	var stockDataList []StockData

	for i, symbol := range strings.Split(symbols, ",") {
		// Parse the target date to extract month information
		parsedDate, err := time.Parse("2006-01-02", dates[i])
		if err != nil {
			return nil, fmt.Errorf("invalid date format: %v", err)
		}

		// Format the year and month for the API parameter
		yearMonth := parsedDate.Format("2006-01")

		// Format the API URL for Intraday Time Series (1min interval) with month parameter
		apiURL := fmt.Sprintf("https://www.alphavantage.co/query?function=TIME_SERIES_INTRADAY&symbol=%s&interval=1min&month=%s&outputsize=full&apikey=%s",
			symbol, yearMonth, apiKey)

		// Send the HTTP GET request
		resp, err := http.Get(apiURL)
		if err != nil {
			return nil, fmt.Errorf("error fetching data from Alpha Vantage: %v", err)
		}
		defer resp.Body.Close()

		// Read the response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %v", err)
		}

		// Parse the JSON response
		var rawResponse map[string]interface{}
		err = json.Unmarshal(body, &rawResponse)
		if err != nil {
			return nil, fmt.Errorf("error parsing response: %v. Response: %s", err, string(body))
		}

		// Extract the time series data
		timeSeries, ok := rawResponse["Time Series (1min)"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid response format or no data available")
		}

		marketOpenTime := "09:30:00"

		// Create the start time (9:30 AM)
		startTime, err := time.Parse("2006-01-02 15:04:05", dates[i]+" "+marketOpenTime)
		if err != nil {
			return nil, fmt.Errorf("error parsing start time: %v", err)
		}

		// Process the data for the specified date, starting at market open (9:30)
		var result []ep.StockData

		// Loop through each minute with the specified increment
		for i := 1; i <= increment; i++ {
			// Calculate current time stamp
			currentTime := startTime.Add(time.Duration(i) * time.Minute)

			// Stop if we've moved to next day
			if currentTime.Day() != parsedDate.Day() {
				break
			}

			// Format timestamp for API data lookup
			timeKey := currentTime.Format("2006-01-02 15:04:00")

			// Check if we have data for this timestamp
			dataPoint, exists := timeSeries[timeKey]
			if !exists {
				// Skip times when market is closed (after 4:00 PM)
				if currentTime.Hour() >= 16 {
					break
				}
				continue
			}

			// Cast to proper type
			dataMap, ok := dataPoint.(map[string]interface{})
			if !ok {
				continue
			}

			// Create StockData object
			stockData := ep.StockData{
				Symbol:    symbol,
				Timestamp: timeKey,
			}

			// Parse the data values
			if open, ok := dataMap["1. open"].(string); ok {
				stockData.Open, _ = strconv.ParseFloat(open, 64)
			}
			if high, ok := dataMap["2. high"].(string); ok {
				stockData.High, _ = strconv.ParseFloat(high, 64)
			}
			if low, ok := dataMap["3. low"].(string); ok {
				stockData.Low, _ = strconv.ParseFloat(low, 64)
			}
			if close, ok := dataMap["4. close"].(string); ok {
				stockData.Close, _ = strconv.ParseFloat(close, 64)
			}
			if volume, ok := dataMap["5. volume"].(string); ok {
				volumeFloat, _ := strconv.ParseFloat(volume, 64)
				stockData.Volume = int64(volumeFloat)
			}

			result = append(result, stockData)
		}

		stockDataList = append(stockDataList, StockData{
			Symbol:    symbol,
			StockData: result,
		})
	}

	return stockDataList, nil
}

func runManagerAgent(wg *sync.WaitGroup, stockdata []ep.StockData, symbol string) {
	defer wg.Done() // Decrement the counter when the goroutine completes

	stock_data := ""

	for i, stockdata := range stockdata {
		stock_data += fmt.Sprintf(fmt.Sprint(i) + " min - " + " Open: " + fmt.Sprint(stockdata.Open) +
			" Close: " + fmt.Sprint(stockdata.PreviousClose) + " High: " + fmt.Sprint(stockdata.High) + " Low: " + fmt.Sprint(stockdata.Low) + "\n")
	}

	dataDir := "reports"
	stockDir := filepath.Join(dataDir, symbol)

	// Create or open the file
	file, err := os.Open(filepath.Join(stockDir, "news_report.txt"))
	if err != nil {
		fmt.Println("Error creating file:", err)
	}
	defer file.Close()

	// Read the entire file content
	news, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Error reading file: %v\n", err)
	}

	// Create or open the file
	file, err = os.Open(filepath.Join(stockDir, "earnings_report.txt"))
	if err != nil {
		fmt.Println("Error opening file:", err)
	}
	defer file.Close()

	// Read the entire file content
	earnings_report, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Error opening file: %v\n", err)
	}

	resp, err := sapien.ManagerAgentReqInfo(stock_data, string(news), string(earnings_report))
	if err != nil {
		fmt.Printf("Error: %s", err)
		os.Exit(1)
	}

	fmt.Printf(resp.Response)
}

// Function to read all files from the given directory and return the concatenated content as a string
func readFilesFromDirectory(directory string) (string, error) {
	var result string

	// Read the directory contents
	files, err := os.ReadDir(directory)
	if err != nil {
		return "", fmt.Errorf("error reading directory: %v", err)
	}

	// Loop through all the files
	for _, file := range files {
		if !file.IsDir() { // Check if it's not a subdirectory
			filePath := filepath.Join(directory, file.Name())
			content, err := os.ReadFile(filePath)
			if err != nil {
				return "", fmt.Errorf("error reading file %s: %v", file.Name(), err)
			}
			// Append file content to the result string
			result += string(content) + "\n"
		}
	}

	return result, nil
}
