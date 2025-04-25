package main

import (
	"avantai/pkg/ep"
	"encoding/json"
	"io"
	"log"
	"os"
	"sync"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// apiKey := os.Getenv("API_KEY")
	apiKey := os.Getenv("TIINGO_KEY")
	// Filters out stocks that don't match the given criteria
	ep.FilterStocks(apiKey)
	// url := fmt.Sprintf("https://www.alphavantage.co/query?function=REALTIME_BULK_QUOTES&symbol=%sentitlement=realtime&apikey=%s",
	// 	"AAPL,NVDA,IBM", apiKey)

	// resp, err := http.Get(url)
	// if err != nil {
	// 	os.Exit(1)
	// }

	// body, err := io.ReadAll(resp.Body)
	// resp.Body.Close()
	// if err != nil {
	// 	os.Exit(1)
	// }

	// var bulkResponse ep.BulkQuoteResponse
	// err = json.Unmarshal(body, &bulkResponse)
	// if err != nil {
	// 	fmt.Printf("error parsing bulk quotes response: %v. Response: %s", err, string(body))
	// 	os.Exit(1)
	// }

	// fmt.Printf("bulk response: %+v", bulkResponse.Data)

	// Navigate to the directory and open the file
	filePath := "data/stockdata/filtered_stocks.json"
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Error opening file: %v\n", err)
	}
	defer file.Close()

	// Read the entire file content
	fileContent, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Error reading file: %v\n", err)
	}

	// Slice to hold the parsed data
	var stocks []ep.FilteredStock

	// Unmarshal the JSON data into the stocks slice
	err = json.Unmarshal(fileContent, &stocks)
	if err != nil {
		log.Fatalf("Error unmarshalling JSON: %v\n", err)
	}

	// Use a WaitGroup to wait for all goroutines to complete
	var wg sync.WaitGroup

	// gets the news info for the respective stock
	for _, stock := range stocks {
		// Start the goroutine
		wg.Add(1)
		go ep.GetNews(&wg, apiKey, stock.Symbol, stock.StockInfo.Timestamp)
	}

	// Wait for all goroutines to finish
	wg.Wait()
}
