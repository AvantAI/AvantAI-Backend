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
	"strings"
	"sync"
	"time"
)

// Structure to hold the intraday data response
type TimeSeries struct {
	MetaData   map[string]string            `json:"Meta Data"`
	TimeSeries map[string]map[string]string `json:"Time Series (1min)"`
}

// Global slice to store the fetched stock data
var stockDataList []TimeSeries

const apiKey = ""

func main() {
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

	for {

		var symbols []string
		for _, stock := range stocks {
			symbols = append(symbols, stock.Symbol)
		}

		stockData, err := getData(strings.Join(symbols, ","), apiKey)
		if err != nil {
			log.Fatalf("Error getting stock data: %v\n", err)
			os.Exit(1)
		}

		// Use a WaitGroup to wait for all goroutines to complete
		var wg sync.WaitGroup

		for _, stock := range stockData {
			// Start the goroutine
			wg.Add(1)
			go runManagerAgent(&wg, time.Minute.String(), stock)

		}

		// Wait for all goroutines to finish
		wg.Wait()

		// Wait for 1 minute (60 seconds)
		time.Sleep(1 * time.Minute)
	}
}

func getData( symbols string, apiKey string) ([]ep.StockData, error) {
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

	return bulkResponse.Symbols, nil

}

func runManagerAgent(wg *sync.WaitGroup, min string, stock ep.StockData) {
	defer wg.Done() // Decrement the counter when the goroutine completes

	stock_data := fmt.Sprintf(min + " Open: " + fmt.Sprint(stock.Open) + 
	" Close: " + fmt.Sprint(stock.PreviousClose) + " High: " + fmt.Sprint(stock.High) + "Low: " + fmt.Sprint(stock.Low))

	dataDir := "reports"
	stockDir := filepath.Join(dataDir, stock.Symbol)

	// Create or open the file
	file, err := os.Create(filepath.Join(stockDir, "news_report.txt"))
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
	file, err = os.Create(filepath.Join(stockDir, "earnings_report.txt"))
	if err != nil {
		fmt.Println("Error creating file:", err)
	}
	defer file.Close()

	// Read the entire file content
	earnings_report, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Error reading file: %v\n", err)
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
