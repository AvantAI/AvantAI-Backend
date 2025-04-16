package main

import (
	"avantai/pkg/ep"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
)

// StockData struct to hold the OCHLV stock data
type StockData struct {
	Symbol string  `json:"symbol"`
	Open   float64 `json:"open,string"`
	High   float64 `json:"high,string"`
	Low    float64 `json:"low,string"`
	Price  float64 `json:"price,string"`
	Volume float64 `json:"volume,string"`
}

// Global slice to store the fetched stock data
var stockDataList []StockData

func main() {
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

	url := fmt.Sprintf(intradayURL, symbol, apiKey)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("error fetching data: %v", err)
	}
	defer resp.Body.Close()

	// Check if the response is successful
	if resp.StatusCode != 200 {
		return fmt.Errorf("API request failed with status code %d", resp.StatusCode)
	}

	// Create a map to hold the JSON response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("error decoding JSON: %v", err)
	}

	// Get the time series data
	timeSeries, ok := result["Time Series (1min)"].(map[string]interface{})
	if !ok {
		fmt.Printf("invalid data format or missing Time Series (1min)")
	}

	// Assume the most recent data is the first entry (this may need adjustments based on your data)
	for timestamp, data := range timeSeries {
		dataMap, ok := data.(map[string]interface{})
		if !ok {
			fmt.Printf("error parsing time series data")
		}

		// Parse the relevant fields (OCHLV)
		stockData := StockData{
			Symbol: symbol,
			Open:   dataMap["1. open"].(float64),
			High:   dataMap["2. high"].(float64),
			Low:    dataMap["3. low"].(float64),
			Price:  dataMap["4. close"].(float64),
			Volume: dataMap["5. volume"].(float64),
		}

		// Append the stock data to the global list
		stockDataList = append(stockDataList, stockData)
		break // Only take the latest data (if you want all data, remove the break)
	}
}
