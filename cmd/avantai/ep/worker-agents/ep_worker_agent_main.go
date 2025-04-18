package main

import (
	"avantai/pkg/ep"
	"avantai/pkg/sapien"
	"encoding/json"
	"io"
	"log"
	"os"
	"sync"
)

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

	// gets the news info for the respective stock
	for _, stock := range stocks {
		// Start the goroutine
		wg.Add(2)

		// TODO: add the agent pipeline
		go sapien.NewsAgentReqInfo(&wg, stock.Symbol)
		go sapien.EarningsReportAgentReqInfo(&wg, stock.Symbol)
	}

	// Wait for all goroutines to finish
	wg.Wait()
}
