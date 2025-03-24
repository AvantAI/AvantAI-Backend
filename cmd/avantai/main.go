package main

import (
	"AvantAI-Backend/pkg/sapien/workeragents"
	"encoding/json"
	"log"
	"os"
	"sync"
)

func main() {

	
	// Get the API token from environment variables
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhZG1pbiI6InN5c3RlbV9hZG1pbiIsImF1ZCI6WyJjbWQiXSwiZXhwIjoxNzQ0NTIyNTA1LCJpYXQiOjE3NDE5MzA1MDUsImlzcyI6IlNhcGllbmh1YiIsImp0aSI6Ijk0Mzg0OGE4LWZkMDctNDI3NC1hYmMxLTg1MjY5NzRlYzAwNyIsInN1YiI6IjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMCIsInRlbmFudCI6IjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMCJ9.LRRFajdiuVyskr0AJypIwCrRKUfmMMwuq6k5lf6H4BA"
	if token == "" {
		log.Fatal("SAPIEN_TOKEN environment variable is not set")
		return
	}

	// Define the API URL
	apiURL := "http://localhost:8081/serve/v1/agents/generate/pranav/news-agent?version=0.0.3"

	stockAnalysis()

	content, err := os.ReadFile("./data/news/news.txt")
	if err != nil {
		log.Fatal(err)
	}

	// Unmarshal the JSON content into a map
	var data map[string]interface{}
	err = json.Unmarshal(content, &data)
	if err != nil {
		log.Fatal(err)
	}

	// Use a WaitGroup to wait for all goroutines to complete
	var wg sync.WaitGroup

	// Start the goroutine
	wg.Add(1)
	go sendRequest(&wg, apiURL, token, data)

	// Wait for all goroutines to finish
	wg.Wait()
}

func stockAnalysis() {
	// Load configuration from YAML file
    config, err := loadConfig()
    if err != nil {
        log.Fatal(err)
    }

    stockSymbols := config.StockSymbols

    for _, symbol := range stockSymbols {
        data, err := fetchStockData(symbol, config.TimePeriod)
        if err != nil {
            log.Printf("Error fetching stock data for %s: %v", symbol, err)
            continue
        }

        // Calculate metric values
        metrics, err := calculateMetrics(data)
		if err != nil {
			log.Printf("err: ", err)
			return
		}

        // Convert metrics to JSON format
        jsonMetrics, err := convertToJSON(metrics)
		if err != nil {
			log.Printf("err: ", err)
			return
		}

        // Write JSON output to file
        err = writeOutputFile(jsonMetrics, symbol)
        if err != nil {
            log.Printf("Error writing output to file for %s: %v", symbol, err)
        }
    }
}