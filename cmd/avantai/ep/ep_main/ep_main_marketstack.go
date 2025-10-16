package main

// import (
// 	"avantai/pkg/ep"
// 	"avantai/pkg/sapien"
// 	"encoding/csv"
// 	"encoding/json"
// 	"fmt"
// 	"io"
//  "math"
// 	"log"
// 	"net/http"
// 	"os"
// 	"path/filepath"
// 	"regexp"
// 	"strconv"
// 	"strings"
// 	"sync"
// 	"time"

// 	"github.com/joho/godotenv"
// 	"github.com/tidwall/pretty"
// )

// // ===== Strategy helpers (EP / Opening Range / VWAP / Consolidation) =====

// type MinuteBar struct {
// 	T time.Time
// 	O float64
// 	H float64
// 	L float64
// 	C float64
// 	V int64
// }

// type StrategyState struct {
// 	// Opening Range levels
// 	OR5High  float64
// 	OR5Low   float64
// 	OR15High float64
// 	OR15Low  float64

// 	// VWAP running components
// 	cumPV float64 // Σ(typicalPrice * volume)
// 	cumV  float64 // Σ(volume)
// 	VWAP  float64

// 	// Simple consolidation flags
// 	IsTight5   bool // last ~5 bars within a tight range
// 	IsTight10  bool // last ~10 bars within a tight range
// 	LastUpdate time.Time
// }

// // Updates VWAP and opening range values given all minute bars since open.
// func (s *StrategyState) Recompute(bars []MinuteBar) {
// 	n := len(bars)
// 	if n == 0 {
// 		return
// 	}

// 	// Opening range 5 and 15 minutes (from regular session start)
// 	limit := func(x, max int) int {
// 		if x > max {
// 			return max
// 		}
// 		return x
// 	}
// 	or5 := bars[:limit(n, 5)]
// 	or15 := bars[:limit(n, 15)]

// 	s.OR5High, s.OR5Low = rangeHL(or5)
// 	s.OR15High, s.OR15Low = rangeHL(or15)

// 	// VWAP (session)
// 	var pv float64
// 	var vSum float64
// 	for _, b := range bars {
// 		typ := (b.H + b.L + b.C) / 3.0
// 		pv += typ * float64(b.V)
// 		vSum += float64(b.V)
// 	}
// 	s.cumPV = pv
// 	s.cumV = vSum
// 	if s.cumV > 0 {
// 		s.VWAP = s.cumPV / s.cumV
// 	}

// 	// Tight consolidations: check last 5 and 10 bars band% of VWAP
// 	s.IsTight5 = isTight(bars, 5, s.VWAP, 0.004)   // ~0.4% band
// 	s.IsTight10 = isTight(bars, 10, s.VWAP, 0.006) // ~0.6% band

// 	s.LastUpdate = bars[n-1].T
// }

// func rangeHL(bars []MinuteBar) (hi, lo float64) {
// 	if len(bars) == 0 {
// 		return 0, 0
// 	}
// 	hi = bars[0].H
// 	lo = bars[0].L
// 	for _, b := range bars {
// 		if b.H > hi {
// 			hi = b.H
// 		}
// 		if b.L < lo {
// 			lo = b.L
// 		}
// 	}
// 	return
// }

// func isTight(all []MinuteBar, lookback int, ref float64, band float64) bool {
// 	n := len(all)
// 	if n < lookback {
// 		return false
// 	}
// 	seg := all[n-lookback:]
// 	hi, lo := rangeHL(seg)
// 	if ref == 0 {
// 		// Fallback: use segment mid
// 		ref = (hi + lo) / 2
// 		if ref == 0 {
// 			return false
// 		}
// 	}
// 	width := (hi - lo) / ref
// 	return width >= 0 && width <= band
// }

// // ===== Marketstack client =====

// type MarketstackIntradayResponse struct {
// 	Pagination struct {
// 		Limit  int `json:"limit"`
// 		Offset int `json:"offset"`
// 		Count  int `json:"count"`
// 		Total  int `json:"total"`
// 	} `json:"pagination"`
// 	Data []struct {
// 		Open   float64 `json:"open"`
// 		High   float64 `json:"high"`
// 		Low    float64 `json:"low"`
// 		Close  float64 `json:"close"`
// 		Volume float64 `json:"volume"`
// 		Date   string  `json:"date"` // e.g. "2024-04-01T13:31:00+0000" (UTC)
// 		Symbol string  `json:"symbol"`
// 	} `json:"data"`
// }

// func fetchIntraday(apiKey, symbol string) (MarketstackIntradayResponse, error) {
// 	url := fmt.Sprintf(
// 		"https://api.marketstack.com/v2/intraday?access_key=%s&symbols=%s&interval=1min&limit=1000",
// 		apiKey, symbol,
// 	)
// 	resp, err := http.Get(url)
// 	if err != nil {
// 		return MarketstackIntradayResponse{}, fmt.Errorf("http get: %w", err)
// 	}
// 	defer resp.Body.Close()
// 	body, err := io.ReadAll(resp.Body)
// 	if err != nil {
// 		return MarketstackIntradayResponse{}, fmt.Errorf("read body: %w", err)
// 	}
// 	var out MarketstackIntradayResponse
// 	if err := json.Unmarshal(body, &out); err != nil {
// 		return MarketstackIntradayResponse{}, fmt.Errorf("unmarshal: %w; body=%s", err, string(body))
// 	}
// 	return out, nil
// }

// // ===== Session utilities =====

// var (
// 	locNY, _ = time.LoadLocation("America/New_York")
// )

// // Returns regular session open/close for a given YYYY-MM-DD (New York time).
// func sessionWindow(date string) (openNY, closeNY time.Time, err error) {
// 	d, err := time.ParseInLocation("2006-01-02", date, locNY)
// 	if err != nil {
// 		return time.Time{}, time.Time{}, err
// 	}
// 	openNY = time.Date(d.Year(), d.Month(), d.Day(), 9, 30, 0, 0, locNY)
// 	closeNY = time.Date(d.Year(), d.Month(), d.Day(), 16, 0, 0, 0, locNY)
// 	return
// }

// func toISOUTC(t time.Time) string {
// 	return t.UTC().Format("2006-01-02T15:04:05")
// }

// func parseMarketstackTimeUTC(s string) (time.Time, error) {
// 	// Marketstack returns e.g. "2025-08-29T13:31:00+0000"
// 	return time.Parse("2006-01-02T15:04:05-0700", s)
// }

// // ===== App wiring =====

// type StockDataList struct {
// 	Symbol    string         `json:"symbol"`
// 	StockData []ep.StockData `json:"stock_data"`
// }

// func main() {
// 	fmt.Println("=== EP Intraday Collector (1-min) ===")

// 	if err := godotenv.Load(); err != nil {
// 		log.Fatal("Error loading .env file")
// 	}
// 	apiKey := os.Getenv("MARKETSTACK_TOKEN")
// 	if apiKey == "" {
// 		log.Fatal("MARKETSTACK_TOKEN not found")
// 	}

// 	// Load filtered symbols list
// 	filePath := "data/stockdata/filtered_stocks_marketstack.json"
// 	raw, err := os.ReadFile(filePath)
// 	if err != nil {
// 		log.Fatalf("open %s: %v", filePath, err)
// 	}
// 	var stocks []ep.FilteredStock
// 	if err := json.Unmarshal(raw, &stocks); err != nil {
// 		log.Fatalf("unmarshal filtered stocks: %v", err)
// 	}
// 	fmt.Printf("Loaded %d filtered symbols\n", len(stocks))

// 	// Extract symbols and their session date (YYYY-MM-DD from StockInfo.Timestamp)
// 	var symbols []string
// 	var dates []string
// 	var sentiment []string
// 	for _, s := range stocks {
// 		symbols = append(symbols, s.Symbol)
// 		dates = append(dates, s.StockInfo.Timestamp[0:10])
// 		sentimentData := map[string]interface{}{
// 			"Stock_name": s.Symbol,
// 			"Stock_info": s.StockInfo,
// 		}
// 		sentimentJSON, err := json.Marshal(sentimentData)
// 		if err != nil {
// 			log.Printf("Error marshaling sentiment for %s: %v", s.Symbol, err)
// 			continue
// 		}
// 		sentiment = append(sentiment, string(sentimentJSON))
// 	}

// 	var wg sync.WaitGroup
// 	fmt.Printf("Starting %d intraday workers…\n", len(symbols))
// 	for i, symbol := range symbols {
// 		wg.Add(1)
// 		go func(idx int, sym, date string, sent string) {
// 			defer wg.Done()
// 			intradayWorker(apiKey, sym, date, sent, idx+1)
// 		}(i, symbol, dates[i], sentiment[i])
// 	}
// 	wg.Wait()
// 	fmt.Println("All workers finished. Done.")
// }

// // Per-symbol minute worker: every minute, refresh bars since session open, recompute strategy metrics,
// // and call runManagerAgent(&wg, bars, symbol, goroutineId)
// func intradayWorker(apiKey, symbol, date string, sentiment string, goroutineId int) {
// 	fmt.Printf("[#%d:%s] worker started for %s\n", goroutineId, symbol, date)

// 	openNY, closeNY, err := sessionWindow(date)
// 	if err != nil {
// 		log.Printf("[#%d:%s] sessionWindow error: %v", goroutineId, symbol, err)
// 		return
// 	}

// 	// We’ll loop from open to close, waking roughly every minute.
// 	// If you run this mid-session, it still works (pulls accumulated bars up to "now").
// 	state := StrategyState{}
// 	var bars []MinuteBar
// 	var epBars []ep.StockData

// 	ticker := time.NewTicker(1 * time.Minute)
// 	defer ticker.Stop()

// 	// Do an immediate tick first
// 	immediate := make(chan struct{}, 1)
// 	immediate <- struct{}{}

// 	for {
// 		select {
// 		case <-immediate:
// 		case <-ticker.C:
// 		}

// 		// Stop past close (NY time)
// 		nowNY := time.Now().In(locNY)
// 		if nowNY.After(closeNY.Add(1 * time.Minute)) {
// 			fmt.Printf("[#%d:%s] past session close — exiting\n", goroutineId, symbol)
// 			return
// 		}

// 		// If before open, wait until open
// 		if nowNY.Before(openNY) {
// 			wait := time.Until(openNY)
// 			if wait > 0 {
// 				fmt.Printf("[#%d:%s] waiting until open (in %s)\n", goroutineId, symbol, wait.Truncate(time.Second))
// 				time.Sleep(wait)
// 			}
// 		}

// 		resp, err := fetchIntraday(apiKey, symbol)
// 		if err != nil {
// 			log.Printf("[#%d:%s] fetchIntraday error: %v", goroutineId, symbol, err)
// 			continue
// 		}
// 		if len(resp.Data) == 0 {
// 			fmt.Printf("[#%d:%s] no intraday data yet\n", goroutineId, symbol)
// 			continue
// 		}

// 		// Transform + filter to regular session minutes in NY time
// 		newBars := make([]MinuteBar, 0, len(resp.Data))
// 		for _, d := range resp.Data {
// 			tu, err := parseMarketstackTimeUTC(d.Date)
// 			if err != nil {
// 				continue
// 			}
// 			tNY := tu.In(locNY)
// 			// keep only [open, close]
// 			if tNY.Before(openNY) || tNY.After(closeNY) {
// 				continue
// 			}
// 			newBars = append(newBars, MinuteBar{
// 				T: tNY, O: d.Open, H: d.High, L: d.Low, C: d.Close, V: int64(d.Volume),
// 			})
// 		}

// 		// Sort ascending by time (Marketstack often returns newest first)
// 		if len(newBars) == 0 {
// 			continue
// 		}
// 		sortMinuteBarsAsc(newBars)

// 		// Deduplicate by time against existing bars
// 		bars = mergeBars(bars, newBars)

// 		// Recompute strategy state from bars since open
// 		state.Recompute(bars)

// 		// Convert to []ep.StockData for ManagerAgent
// 		epBars = convertToEP(symbol, bars)

// 		// Log EP-relevant levels (for your own visibility)
// 		fmt.Printf("[#%d:%s] t=%s  OR5[H/L]=[%.2f/%.2f]  OR15[H/L]=[%.2f/%.2f]  VWAP=%.3f  Tight5=%t Tight10=%t  bars=%d\n",
// 			goroutineId, symbol, state.LastUpdate.Format("15:04"),
// 			state.OR5High, state.OR5Low, state.OR15High, state.OR15Low, state.VWAP, state.IsTight5, state.IsTight10, len(bars),
// 		)

// 		// Call your manager with the latest slice (no trade logic here)
// 		var wg sync.WaitGroup
// 		wg.Add(1)
// 		go runManagerAgent(&wg, epBars, symbol, sentiment, goroutineId)
// 		wg.Wait()

// 		// Gentle rate limit between symbols is inherent; per-symbol we tick every minute
// 	}
// }

// func sortMinuteBarsAsc(b []MinuteBar) {
// 	// simple insertion sort (n is tiny per pull); replace with sort.Slice if you prefer
// 	for i := 1; i < len(b); i++ {
// 		j := i
// 		for j > 0 && b[j-1].T.After(b[j].T) {
// 			b[j-1], b[j] = b[j], b[j-1]
// 			j--
// 		}
// 	}
// }

// func mergeBars(existing, incoming []MinuteBar) []MinuteBar {
// 	if len(existing) == 0 {
// 		return incoming
// 	}
// 	// Use last timestamp to filter
// 	last := existing[len(existing)-1].T
// 	out := existing
// 	for _, nb := range incoming {
// 		if nb.T.After(last) {
// 			out = append(out, nb)
// 		}
// 	}
// 	return out
// }

// func convertToEP(symbol string, bars []MinuteBar) []ep.StockData {
// 	out := make([]ep.StockData, 0, len(bars))
// 	var prevClose float64
// 	for i, b := range bars {
// 		if i == 0 {
// 			prevClose = b.O
// 		}
// 		s := ep.StockData{
// 			Symbol:    symbol,
// 			Timestamp: b.T.Format("2006-01-02 15:04:00"),
// 			Open:      b.O,
// 			High:      b.H,
// 			Low:       b.L,
// 			Close:     b.C,
// 			Volume:    b.V,
// 		}
// 		// If your ep.StockData has PreviousClose, populate it (keeps your runManagerAgent logs consistent).
// 		// If not, this no-op won't compile, so comment it out.
// 		s.PreviousClose = prevClose

// 		out = append(out, s)
// 		prevClose = b.C
// 	}
// 	return out
// }

// // The format you provided
// const timeFormat = "2006-01-02 15:04:00"

// func getMinute(timestampStr string) (int, error) {
// 	// 1. Parse the string into a time.Time object
// 	t, err := time.Parse(timeFormat, timestampStr)
// 	if err != nil {
// 		return 0, err
// 	}

// 	// 2. Extract the minute component as an int
// 	minute := t.Minute()

// 	return minute, nil
// }

// // ===== Manager Agent Response Structure =====
// type ManagerResponse struct {
// 	Recommendation string `json:"Recommendation"`
// 	EntryTime      string `json:"Entry Time,omitempty"`
// 	EntryPrice     string `json:"Entry Price,omitempty"`
// 	StopLoss       string `json:"Stop-Loss,omitempty"`
// 	RiskPercent    string `json:"Risk %,omitempty"`
// 	Reasoning      string `json:"Reasoning"`
// }

// // ===== Watchlist CSV Structure =====
// type WatchlistEntry struct {
// 	StockSymbol string
// 	EntryPrice  string
// 	StopLoss    string
// 	Shares      string
// }

// func extractJSON(response string) (*ManagerResponse, error) {
// 	// Look for JSON pattern in the response
// 	jsonPattern := regexp.MustCompile(`\{[^{}]*"Recommendation"[^{}]*\}`)
// 	match := jsonPattern.FindString(response)

// 	if match == "" {
// 		return nil, fmt.Errorf("no JSON found in response")
// 	}

// 	var managerResp ManagerResponse
// 	err := json.Unmarshal([]byte(match), &managerResp)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
// 	}

// 	return &managerResp, nil
// }

// func saveJSONResponse(symbol string, minute int, response *ManagerResponse) error {
// 	// Create directory if it doesn't exist
// 	dir := filepath.Join("responses", symbol)
// 	if err := os.MkdirAll(dir, 0755); err != nil {
// 		return fmt.Errorf("failed to create directory: %w", err)
// 	}

// 	// Save JSON file with prettified formatting
// 	filename := filepath.Join(dir, fmt.Sprintf("minute_%d_response.json", minute))
// 	data, err := json.Marshal(response)
// 	if err != nil {
// 		return fmt.Errorf("failed to marshal response: %w", err)
// 	}

// 	// Prettify the JSON
// 	prettified := pretty.Pretty(data)

// 	return os.WriteFile(filename, prettified, 0644)
// }

// func addToWatchlist(entry WatchlistEntry) error {
// 	filename := "watchlist.csv"

// 	// Read existing entries to check for duplicates
// 	existingEntries := make(map[string]WatchlistEntry)
// 	if file, err := os.Open(filename); err == nil {
// 		defer file.Close()
// 		reader := csv.NewReader(file)
// 		records, err := reader.ReadAll()
// 		if err != nil {
// 			return fmt.Errorf("failed to read existing watchlist: %w", err)
// 		}

// 		// Skip header row if it exists
// 		startIdx := 0
// 		if len(records) > 0 && records[0][0] == "stock_symbol" {
// 			startIdx = 1
// 		}

// 		// Build map of existing entries
// 		for i := startIdx; i < len(records); i++ {
// 			if len(records[i]) >= 4 {
// 				key := fmt.Sprintf("%s|%s|%s|%s", records[i][0], records[i][1], records[i][2], records[i][3])
// 				existingEntries[key] = WatchlistEntry{
// 					StockSymbol: records[i][0],
// 					EntryPrice:  records[i][1],
// 					StopLoss:    records[i][2],
// 					Shares:      records[i][3],
// 				}
// 			}
// 		}
// 	}

// 	// Check if this entry already exists
// 	entryKey := fmt.Sprintf("%s|%s|%s|%s", entry.StockSymbol, entry.EntryPrice, entry.StopLoss, entry.Shares)
// 	if _, exists := existingEntries[entryKey]; exists {
// 		return fmt.Errorf("duplicate entry: %s with entry price %s and stop loss %s already exists in watchlist",
// 			entry.StockSymbol, entry.EntryPrice, entry.StopLoss)
// 	}

// 	// Check if file exists
// 	fileExists := true
// 	if _, err := os.Stat(filename); os.IsNotExist(err) {
// 		fileExists = false
// 	}

// 	// Open file for append or create
// 	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
// 	if err != nil {
// 		return fmt.Errorf("failed to open watchlist file: %w", err)
// 	}
// 	defer file.Close()

// 	writer := csv.NewWriter(file)
// 	defer writer.Flush()

// 	// Write header if file is new
// 	if !fileExists {
// 		if err := writer.Write([]string{"stock_symbol", "entry_price", "stop_loss_price", "shares"}); err != nil {
// 			return fmt.Errorf("failed to write CSV header: %w", err)
// 		}
// 	}

// 	// Write the entry
// 	record := []string{entry.StockSymbol, entry.EntryPrice, entry.StopLoss, entry.Shares}
// 	if err := writer.Write(record); err != nil {
// 		return fmt.Errorf("failed to write CSV record: %w", err)
// 	}

// 	return nil
// }

// // ===== Your existing runManagerAgent (unchanged) =====

// func runManagerAgent(wg *sync.WaitGroup, stockdata []ep.StockData, symbol string, sentiment string, goroutineId int) {
// 	fmt.Printf("\n[Goroutine %d] --- Starting runManagerAgent for %s (Sentiment: %s) ---\n", goroutineId, symbol, sentiment)
// 	defer wg.Done()
// 	defer fmt.Printf("[Goroutine %d] ✓ runManagerAgent completed for %s\n", goroutineId, symbol)

// 	fmt.Printf("[Goroutine %d] Processing %d stock data points for %s\n", goroutineId, len(stockdata), symbol)
// 	stock_data := ""

// 	for i, stockdataPoint := range stockdata {
// 		stock_data += fmt.Sprintf("%d min - Open: %v Close: %v High: %v Low: %v\n",
// 			i, stockdataPoint.Open, stockdataPoint.PreviousClose, stockdataPoint.High, stockdataPoint.Low)
// 		if i < 3 {
// 			fmt.Printf("[Goroutine %d]   Data %d: O:%.2f C:%.2f H:%.2f L:%.2f\n",
// 				goroutineId, i, stockdataPoint.Open, stockdataPoint.PreviousClose, stockdataPoint.High, stockdataPoint.Low)
// 		}
// 	}
// 	fmt.Printf("[Goroutine %d] ✓ Stock data formatted (%d characters)\n", goroutineId, len(stock_data))

// 	dataDir := "reports"
// 	stockDir := filepath.Join(dataDir, symbol)

// 	// Read news
// 	newsFilePath := filepath.Join(stockDir, "news_report.txt")
// 	file, err := os.Open(newsFilePath)
// 	if err != nil {
// 		fmt.Printf("[Goroutine %d] ❌ Error opening news report file: %v\n", goroutineId, err)
// 	} else {
// 		defer file.Close()
// 	}
// 	var news []byte
// 	if file != nil {
// 		news, err = io.ReadAll(file)
// 		if err != nil {
// 			fmt.Printf("[Goroutine %d] ❌ Error reading news file: %v\n", goroutineId, err)
// 		} else {
// 			fmt.Printf("[Goroutine %d] ✓ News report read (%d bytes)\n", goroutineId, len(news))
// 		}
// 	}

// 	// Read earnings
// 	earningsFilePath := filepath.Join(stockDir, "earnings_report.txt")
// 	file, err = os.Open(earningsFilePath)
// 	if err != nil {
// 		fmt.Printf("[Goroutine %d] ❌ Error opening earnings report file: %v\n", goroutineId, err)
// 	} else {
// 		defer file.Close()
// 	}
// 	var earnings []byte
// 	if file != nil {
// 		earnings, err = io.ReadAll(file)
// 		if err != nil {
// 			fmt.Printf("[Goroutine %d] ❌ Error reading earnings file: %v\n", goroutineId, err)
// 		} else {
// 			fmt.Printf("[Goroutine %d] ✓ Earnings report read (%d bytes)\n", goroutineId, len(earnings))
// 		}
// 	}

// 	fmt.Printf("[Goroutine %d] Calling ManagerAgentReqInfo for %s (Sentiment: %s)...\n", goroutineId, symbol, sentiment)
// 	resp, err := sapien.ManagerAgentReqInfo(stock_data, string(news), string(earnings), sentiment)
// 	if err != nil {
// 		fmt.Printf("[Goroutine %d] ❌ ManagerAgentReqInfo error: %v\n", goroutineId, err)
// 		return
// 	}

// 	min, err := getMinute(stockdata[len(stockdata)-1].Timestamp)
// 	if err != nil {
// 		fmt.Printf("[Goroutine %d] ❌ Failed to parse minute from timestamp %s: %v\n",
// 			goroutineId, stockdata[len(stockdata)-1].Timestamp, err)
// 		return
// 	}

// 	currentMinute := min
// 	// Extract JSON from response
// 	managerResp, err := extractJSON(resp.Response)
// 	if err != nil {
// 		fmt.Printf("[Goroutine %d] ❌ Failed to extract JSON from response (minute %d): %v\n",
// 			goroutineId, currentMinute, err)
// 		return // Don't stop processing on JSON extraction errors
// 	}

// 	// Save JSON response to file
// 	if err := saveJSONResponse(symbol, currentMinute, managerResp); err != nil {
// 		fmt.Printf("[Goroutine %d] ❌ Failed to save JSON response (minute %d): %v\n",
// 			goroutineId, currentMinute, err)
// 	} else {
// 		fmt.Printf("[Goroutine %d] ✓ JSON response saved for minute %d\n", goroutineId, currentMinute)
// 	}

// if err := godotenv.Load(); err != nil {
// 	log.Fatal("Error loading .env file")
// }

// 	// Check if recommendation is "Buy" and add to watchlist
// 	stringPercent := strings.TrimSpace(strings.TrimSuffix(managerResp.RiskPercent, "%"))
// 	riskPercent, _ := strconv.ParseFloat(stringPercent, 64)

// 	accSize, _ := strconv.ParseFloat(os.Getenv("ACCOUNT_SIZE"), 64)
// 	entryPrice, _ := strconv.ParseFloat(strings.ReplaceAll(managerResp.EntryPrice, "$", ""), 64)
// 	stopLoss, _ := strconv.ParseFloat(strings.ReplaceAll(managerResp.StopLoss, "$", ""), 64)

// 	if strings.ToLower(strings.TrimSpace(managerResp.Recommendation)) == "buy" {
// 		// Only add to watchlist if we have entry price and stop loss
// 		if managerResp.EntryPrice != "" && managerResp.StopLoss != "" {
// 			entry := WatchlistEntry{
// 				StockSymbol: symbol,
// 				EntryPrice:  managerResp.EntryPrice,
// 				StopLoss:    managerResp.StopLoss,
// 				Shares:      strconv.FormatFloat(float64(int(math.Round((((riskPercent/100)*accSize)*1.0)/(entryPrice-stopLoss)))), 'f', 2, 64),
// 			}

// 			if err := addToWatchlist(entry); err != nil {
// 				if strings.Contains(err.Error(), "duplicate entry") {
// 					fmt.Printf("[Goroutine %d] ⚠️ Duplicate watchlist entry for %s with entry price %s and stop loss %s (minute %d)\n",
// 						goroutineId, symbol, entry.EntryPrice, entry.StopLoss, currentMinute)
// 				} else {
// 					fmt.Printf("[Goroutine %d] ❌ Failed to add %s to watchlist (minute %d): %v\n",
// 						goroutineId, symbol, currentMinute, err)
// 				}
// 			} else {
// 				fmt.Printf("[Goroutine %d] ✅ Added %s to watchlist with entry price %s and stop loss %s (minute %d)\n",
// 					goroutineId, symbol, entry.EntryPrice, entry.StopLoss, currentMinute)
// 			}

// 			// Return true to signal that processing should stop for this stock
// 			return
// 		} else {
// 			fmt.Printf("[Goroutine %d] ⚠️ Buy recommendation for %s but missing entry price or stop loss (minute %d)\n",
// 				goroutineId, symbol, currentMinute)
// 			// Continue processing even if Buy recommendation is incomplete
// 			return
// 		}
// 	} else {
// 		fmt.Printf("[Goroutine %d] 📊 Recommendation for %s: %s (minute %d)\n",
// 			goroutineId, symbol, managerResp.Recommendation, currentMinute)
// 		// Continue processing for non-Buy recommendations
// 		return
// 	}
// }
