package main

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"

	"avantai/pkg/superperformance"
)

type AnalysisResult struct {
	Symbol  string
	Results []superperformance.Result
	Error   error
}

func main() {
	fmt.Println("🚀 Superperformance Growth Stock Screener")
	fmt.Println("==========================================")

	if err := godotenv.Load(); err != nil {
		fmt.Println("Warning: No .env file found, using environment variables")
	}

	apiKey := os.Getenv("MARKETSTACK_TOKEN")
	if apiKey == "" {
		fmt.Println("❌ ERROR: MarketStack API key is required")
		fmt.Println("Set the MARKETSTACK_TOKEN environment variable")
		os.Exit(1)
	}

	// Initialize API client with conservative settings
	client := &superperformance.MarketStackClient{
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		RateLimit:  make(chan struct{}, 3), // Conservative rate limiting
	}

	// Initialize rate limiter
	for i := 0; i < 3; i++ {
		client.RateLimit <- struct{}{}
	}

	// Load symbols from CSV only
	fmt.Printf("📁 Loading symbols from config.csv\n")
	symbols, err := superperformance.LoadStocksFromCSV("/Users/pranav/projects/avant-ai/AvantAI-Backend/pkg/ep/config.csv")
	if err != nil {
		fmt.Printf("❌ ERROR loading symbols from config.csv: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("📊 Loaded %d symbols from config.csv\n", len(symbols))

	if len(symbols) == 0 {
		fmt.Println("❌ ERROR: No symbols found in config.csv")
		os.Exit(1)
	}

	// Run analysis
	fmt.Printf("\n🔍 Starting analysis of %d stocks...\n", len(symbols))
	fmt.Printf("🎯 Looking for explosive growth patterns:\n")
	fmt.Printf("   • Growth Stock: 100%+ in 64-252 days OR 150%+ in 252-504 days\n")
	fmt.Printf("   • Superperformer: 300%+ in 64-252 days OR 500%+ in 252-504 days\n")
	fmt.Printf("   • Min Volume: 200,000+ shares daily average\n\n")

	startTime := time.Now()
	resultsNASDAQ := analyzeStocks(client, symbols[:4329], "NASDAQ")
	resultsNYSE := analyzeStocks(client, symbols[4329:], "NYSE")
	results := append(resultsNASDAQ, resultsNYSE...)
	duration := time.Since(startTime)

	// Process and display results
	fmt.Printf("\n📈 Analysis completed in %v\n", duration)
	fmt.Println("==========================================")

	allResults := []superperformance.Result{}
	successCount := 0
	errorCount := 0
	growthStocksFound := 0
	superperformersFound := 0
	volumeFailures := 0
	dataFailures := 0
	apiFailures := 0

	for _, result := range results {
		if result.Error != nil {
			errorCount++
			errorStr := result.Error.Error()
			if strings.Contains(errorStr, "volume requirement") {
				volumeFailures++
			} else if strings.Contains(errorStr, "Insufficient data") {
				dataFailures++
			} else {
				apiFailures++
			}
			fmt.Printf("❌ %s: %v\n", result.Symbol, result.Error)
			continue
		}

		successCount++
		if len(result.Results) > 0 {
			growthStocksFound++
			for _, r := range result.Results {
				allResults = append(allResults, r)
				if r.Superperformance == "Yes" {
					superperformersFound++
				}
			}
		}
	}

	// Sort results by total gain (descending)
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].TotalGain > allResults[j].TotalGain
	})

	// Display summary
	fmt.Printf("📊 ANALYSIS SUMMARY\n")
	fmt.Printf("   • Total stocks analyzed: %d\n", len(symbols))
	fmt.Printf("   • Successfully processed: %d\n", successCount)
	fmt.Printf("   • Failed analysis: %d\n", errorCount)
	if errorCount > 0 {
		fmt.Printf("     - Volume failures: %d (< 200k avg volume)\n", volumeFailures)
		fmt.Printf("     - Data failures: %d (insufficient history)\n", dataFailures)
		fmt.Printf("     - API failures: %d (network/API errors)\n", apiFailures)
	}
	fmt.Printf("   • Growth stocks identified: %d\n", growthStocksFound)
	fmt.Printf("   • Superperformers found: %d ⭐\n", superperformersFound)
	fmt.Printf("   • Total explosive growth moves: %d\n", len(allResults))

	if len(allResults) > 0 {
		fmt.Printf("\n🏆 TOP EXPLOSIVE GROWTH PERFORMERS\n")
		fmt.Printf("%-8s %-12s %-12s %-8s %-5s %-15s %s\n",
			"TICKER", "START", "END", "GAIN%", "DAYS", "SUPERPERFORM", "DRAWDOWNS")
		fmt.Println(strings.Repeat("-", 85))

		displayCount := len(allResults)
		if displayCount > 25 {
			displayCount = 25
		}

		for i := 0; i < displayCount; i++ {
			r := allResults[i]
			superIcon := ""
			if r.Superperformance == "Yes" {
				superIcon = " ⭐"
			}
			fmt.Printf("%-8s %-12s %-12s %7.1f %5d %-15s %s%s\n",
				r.Ticker, r.StartDate, r.EndDate, r.TotalGain, r.DurationDays,
				r.Superperformance, r.Drawdowns, superIcon)
		}

		// Save results
		jsonFile := "superperformance_results.json"
		csvFile := "sp_results.csv"

		if err := superperformance.SaveResultsToJSON(allResults, jsonFile); err != nil {
			fmt.Printf("❌ ERROR saving JSON: %v\n", err)
		} else {
			fmt.Printf("\n💾 Results saved to %s\n", jsonFile)
		}

		if err := superperformance.SaveResultsToCSV(allResults, csvFile); err != nil {
			fmt.Printf("❌ ERROR saving CSV: %v\n", err)
		} else {
			fmt.Printf("💾 Results saved to %s\n", csvFile)
		}

		fmt.Printf("\n✅ EXPLOSIVE GROWTH SCREENING COMPLETE!\n")
		fmt.Printf("🎯 Found %d explosive growth moves across %d stocks\n", len(allResults), growthStocksFound)
		if superperformersFound > 0 {
			fmt.Printf("⭐ %d SUPERPERFORMERS identified!\n", superperformersFound)
		}
	} else {
		fmt.Printf("\n❌ No explosive growth stocks found in the analyzed dataset\n")
	}
}

func analyzeStocks(client *superperformance.MarketStackClient, symbols []string, exchange string) []AnalysisResult {
	var wg sync.WaitGroup
	resultsChan := make(chan AnalysisResult, len(symbols))

	for i, symbol := range symbols {
		wg.Add(1)
		go func(sym string, index int) {
			defer wg.Done()

			if index%5 == 0 || index == len(symbols)-1 {
				fmt.Printf("📊 Progress: %d/%d stocks analyzed (%.1f%%)\r",
					index+1, len(symbols), float64(index+1)/float64(len(symbols))*100)
			}

			results, err := client.AnalyzeStock(sym, exchange)
			resultsChan <- AnalysisResult{
				Symbol:  sym,
				Results: results,
				Error:   err,
			}
		}(symbol, i)

		// Spread out API calls
		if i%3 == 0 && i > 0 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var results []AnalysisResult
	for result := range resultsChan {
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Symbol < results[j].Symbol
	})

	return results
}