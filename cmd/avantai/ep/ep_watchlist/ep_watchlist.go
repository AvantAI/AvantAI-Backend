package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/joho/godotenv"
)

// response models for Alpha Vantage Global Quote
type GlobalQuoteResponse struct {
	Quote GlobalQuote `json:"Global Quote"`
}

type GlobalQuote struct {
	Symbol           string `json:"01. symbol"`
	Open             string `json:"02. open"`
	High             string `json:"03. high"`
	Low              string `json:"04. low"`
	Price            string `json:"05. price"`
	Volume           string `json:"06. volume"`
	LatestTradingDay string `json:"07. latest trading day"`
	PreviousClose    string `json:"08. previous close"`
	Change           string `json:"09. change"`
	ChangePercent    string `json:"10. change percent"`
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, checking for environment variable")
		// Continue anyway, API_KEY might be set directly in the environment
	}

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatal("API_KEY not found in environment or .env file")
	}

	file, err := os.Open("watchlist.csv")
	if err != nil {
		// Try alternate path if the first one fails
		file, err = os.Open("cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv")
		if err != nil {
			log.Fatal("Error opening watchlist file:", err)
		}
	}
	defer file.Close()

	reader := csv.NewReader(file)
	var wg sync.WaitGroup

	// Skip header row if it exists
	// Uncomment the following if your CSV has a header
	// _, err = reader.Read()
	// if err != nil {
	//     log.Fatal("Error reading header row:", err)
	// }

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error reading CSV: %v", err)
			break
		}

		if len(row) != 3 {
			log.Println("Skipping malformed row:", row)
			continue
		}

		symbol := row[0]
		entry, err1 := strconv.ParseFloat(row[1], 64)
		stop, err2 := strconv.ParseFloat(row[2], 64)
		if err1 != nil || err2 != nil {
			log.Println("Skipping row with invalid numbers:", row)
			continue
		}

		wg.Add(1)
		go ComparePrice(&wg, apiKey, symbol, entry, stop)

	}

	wg.Wait()
}

func CalculateMoveStopLossPrice(entry, stop float64) float64 {
	return entry + (entry-stop)*3
}

func MoveStopLoss(symbol string, entryPrice float64) {
	fmt.Printf("[%s] 🚨 Moving stop loss to entry price!\n", symbol)

	// Set the new stop loss to the current entry price
	newStopLoss := entryPrice

	// Set the new entry price to the new stop loss plus 10%
	newEntryPrice := newStopLoss + (newStopLoss * 0.1)

	// Format to 2 decimal places for better readability in CSV
	newStopLossStr := fmt.Sprintf("%.2f", newStopLoss)
	newEntryPriceStr := fmt.Sprintf("%.2f", newEntryPrice)

	// Update the CSV file
	err := UpdateStockInCSV("cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv", symbol, 1, newEntryPriceStr, 2, newStopLossStr)
	if err != nil {
		log.Printf("[%s] Error updating stop loss in CSV: %v", symbol, err)
	} else {
		log.Printf("[%s] Successfully updated stop loss to %.2f and entry price to %.2f",
			symbol, newStopLoss, newEntryPrice)
	}
}

func UpdateStockInCSV(filePath string, symbol string, entryColumnIndex int, newEntryValue string,
	stopColumnIndex int, newStopValue string) error {

	// Try to locate the CSV file
	file, err := os.Open(filePath)
	if err != nil {
		// Try alternate path if the first one fails
		file, err = os.Open("cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv")
		if err != nil {
			return fmt.Errorf("error opening file: %v", err)
		}
	}
	defer file.Close()

	// Parse the CSV content
	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("error reading CSV: %v", err)
	}

	// Find the row with the matching symbol
	rowFound := false
	for i, row := range records {
		if len(row) > 0 && row[0] == symbol {
			// Check if the columns exist
			if entryColumnIndex >= len(row) || stopColumnIndex >= len(row) {
				return fmt.Errorf("column index out of bounds, row has %d columns", len(row))
			}

			// Update the values
			records[i][entryColumnIndex] = newEntryValue
			records[i][stopColumnIndex] = newStopValue
			rowFound = true
			break
		}
	}

	if !rowFound {
		return fmt.Errorf("symbol %s not found in CSV", symbol)
	}

	// Open the file for writing (this will truncate the file)
	// First try the original path
	file, err = os.Create(filePath)
	if err != nil {
		// If that fails, try the alternate path
		file, err = os.Create("cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv")
		if err != nil {
			return fmt.Errorf("error opening file for writing: %v", err)
		}
	}
	defer file.Close()

	// Write the modified records back to the file
	writer := csv.NewWriter(file)
	err = writer.WriteAll(records)
	if err != nil {
		return fmt.Errorf("error writing CSV: %v", err)
	}
	writer.Flush()

	return nil
}

func ComparePrice(wg *sync.WaitGroup, apiKey, symbol string, entry, stop float64) {
	defer wg.Done()

	// fetch
	url := fmt.Sprintf(
		"https://www.alphavantage.co/query?function=GLOBAL_QUOTE&entitlement=realtime&symbol=%s&apikey=%s",
		symbol, apiKey,
	)
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("[%s] error fetching data: %v", symbol, err)
		return
	}
	defer resp.Body.Close()

	// read & unmarshal
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[%s] error reading response: %v", symbol, err)
		return
	}

	var result GlobalQuoteResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[%s] error parsing JSON: %v", symbol, err)
		return
	}

	// Check if price is empty before parsing
	if result.Quote.Price == "" {
		log.Printf("[%s] price data is empty, API may be rate limited or symbol may be invalid", symbol)
		return
	}

	// convert price to float
	price, err := strconv.ParseFloat(result.Quote.Price, 64)
	if err != nil {
		log.Printf("[%s] invalid price format: %v", symbol, err)
		return
	}

	// Log the current price and target price for debugging
	moveStopPrice := CalculateMoveStopLossPrice(entry, stop)
	log.Printf("[%s] Current price: %.2f, Entry: %.2f, Stop: %.2f, Move Stop Target: %.2f",
		symbol, price, entry, stop, moveStopPrice)

	// compare
	if price >= moveStopPrice {
		MoveStopLoss(symbol, entry)
	}
}
