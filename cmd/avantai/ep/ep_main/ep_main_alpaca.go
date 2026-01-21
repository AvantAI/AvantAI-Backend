package main

// import (
// 	"avantai/pkg/ep"
// 	"avantai/pkg/sapien"
// 	"encoding/csv"
// 	"encoding/json"
// 	"fmt"
// 	"io"
// 	"log"
// 	"math"
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
// 	OR5High    float64
// 	OR5Low     float64
// 	OR15High   float64
// 	OR15Low    float64
// 	cumPV      float64
// 	cumV       float64
// 	VWAP       float64
// 	IsTight5   bool
// 	IsTight10  bool
// 	LastUpdate time.Time
// }

// type RealtimeScanResponse struct {
// 	ScanTime         string                `json:"scan_time"`
// 	MarketStatus     string                `json:"market_status"`
// 	FilterCriteria   FilterCriteriaDetails `json:"filter_criteria"`
// 	QualifyingStocks []ep.RealtimeResult   `json:"qualifying_stocks"`
// 	Summary          ScanSummary           `json:"summary"`
// }

// type FilterCriteriaDetails struct {
// 	MinGapUpPercent      float64 `json:"min_gap_up_percent"`
// 	MinDollarVolume      float64 `json:"min_dollar_volume"`
// 	MinMarketCap         float64 `json:"min_market_cap"`
// 	MinPremarketVolRatio float64 `json:"min_premarket_volume_ratio"`
// 	MaxExtensionADR      float64 `json:"max_extension_adr"`
// }

// type ScanSummary struct {
// 	TotalCandidates int     `json:"total_candidates"`
// 	AvgGapUp        float64 `json:"avg_gap_up"`
// }

// func (s *StrategyState) Recompute(bars []MinuteBar) {
// 	n := len(bars)
// 	if n == 0 {
// 		return
// 	}
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
// 	s.IsTight5 = isTight(bars, 5, s.VWAP, 0.004)
// 	s.IsTight10 = isTight(bars, 10, s.VWAP, 0.006)
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
// 		ref = (hi + lo) / 2
// 		if ref == 0 {
// 			return false
// 		}
// 	}
// 	width := (hi - lo) / ref
// 	return width >= 0 && width <= band
// }

// // ===== Alpaca Market Data API =====

// type AlpacaBar struct {
// 	T  string  `json:"t"`
// 	O  float64 `json:"o"`
// 	H  float64 `json:"h"`
// 	L  float64 `json:"l"`
// 	C  float64 `json:"c"`
// 	V  int64   `json:"v"`
// 	N  int     `json:"n"`
// 	VW float64 `json:"vw"`
// }

// type AlpacaBarsResponse struct {
// 	Bars          []AlpacaBar `json:"bars"`
// 	Symbol        string      `json:"symbol"`
// 	NextPageToken *string     `json:"next_page_token"`
// }

// func fetchAlpacaIntraday(apiKey, apiSecret, symbol, startTime, endTime string) ([]AlpacaBar, error) {
// 	url := fmt.Sprintf(
// 		"https://data.alpaca.markets/v2/stocks/%s/bars?timeframe=1Min&start=%s&end=%s&limit=10000&adjustment=raw&feed=sip",
// 		symbol, startTime, endTime,
// 	)
// 	req, err := http.NewRequest("GET", url, nil)
// 	if err != nil {
// 		return nil, fmt.Errorf("create request: %w", err)
// 	}
// 	req.Header.Set("APCA-API-KEY-ID", apiKey)
// 	req.Header.Set("APCA-API-SECRET-KEY", apiSecret)
// 	client := &http.Client{Timeout: 30 * time.Second}
// 	resp, err := client.Do(req)
// 	if err != nil {
// 		return nil, fmt.Errorf("http request: %w", err)
// 	}
// 	defer resp.Body.Close()
// 	if resp.StatusCode != http.StatusOK {
// 		body, _ := io.ReadAll(resp.Body)
// 		return nil, fmt.Errorf("alpaca API error (status %d): %s", resp.StatusCode, string(body))
// 	}
// 	body, err := io.ReadAll(resp.Body)
// 	if err != nil {
// 		return nil, fmt.Errorf("read body: %w", err)
// 	}
// 	var alpacaResp AlpacaBarsResponse
// 	if err := json.Unmarshal(body, &alpacaResp); err != nil {
// 		return nil, fmt.Errorf("unmarshal: %w; body=%s", err, string(body))
// 	}
// 	if len(alpacaResp.Bars) == 0 {
// 		return []AlpacaBar{}, nil
// 	}
// 	return alpacaResp.Bars, nil
// }

// // ===== Session utilities =====

// var (
// 	locNY, _ = time.LoadLocation("America/New_York")
// )

// func sessionWindow(date string) (openNY, closeNY time.Time, err error) {
// 	d, err := time.ParseInLocation("2006-01-02", date, locNY)
// 	if err != nil {
// 		return time.Time{}, time.Time{}, err
// 	}
// 	openNY = time.Date(d.Year(), d.Month(), d.Day(), 9, 30, 0, 0, locNY)
// 	closeNY = time.Date(d.Year(), d.Month(), d.Day(), 16, 0, 0, 0, locNY)
// 	return
// }

// func toRFC3339(t time.Time) string {
// 	return t.Format(time.RFC3339)
// }

// func parseAlpacaTime(s string) (time.Time, error) {
// 	return time.Parse(time.RFC3339, s)
// }

// // ===== App wiring =====

// type StockDataList struct {
// 	Symbol    string         `json:"symbol"`
// 	StockData []ep.StockData `json:"stock_data"`
// }

// func main() {
// 	fmt.Println("=== EP Intraday Collector (1-min) with Alpaca ===")
// 	if err := godotenv.Load(); err != nil {
// 		log.Fatal("Error loading .env file")
// 	}
// 	alpacaKey := os.Getenv("ALPACA_API_KEY")
// 	alpacaSecret := os.Getenv("ALPACA_SECRET_KEY")
// 	if alpacaKey == "" || alpacaSecret == "" {
// 		log.Fatal("ALPACA_API_KEY or ALPACA_SECRET_KEY not found in .env")
// 	}
// 	filePath := "data/stockdata/filtered_stocks_marketstack.json"
// 	raw, err := os.ReadFile(filePath)
// 	if err != nil {
// 		log.Fatalf("open %s: %v", filePath, err)
// 	}
// 	var scanResponse RealtimeScanResponse
// 	err = json.Unmarshal(raw, &scanResponse)
// 	if err != nil {
// 		log.Fatalf("error unmarshalling JSON: %v", err)
// 	}
// 	result := scanResponse.QualifyingStocks
// 	stocks := make([]ep.FilteredStock, len(result))
// 	for i, r := range result {
// 		stocks[i] = r.FilteredStock
// 	}
// 	fmt.Printf("Loaded %d filtered symbols\n", len(stocks))
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
// 			intradayWorker(alpacaKey, alpacaSecret, sym, date, sent, idx+1)
// 		}(i, symbol, dates[i], sentiment[i])
// 	}
// 	wg.Wait()
// 	fmt.Println("All workers finished. Done.")
// }

// func intradayWorker(apiKey, apiSecret, symbol, date string, sentiment string, goroutineId int) {
// 	fmt.Printf("[#%d:%s] worker started for %s\n", goroutineId, symbol, date)
// 	openNY, closeNY, err := sessionWindow(date)
// 	if err != nil {
// 		log.Printf("[#%d:%s] sessionWindow error: %v", goroutineId, symbol, err)
// 		return
// 	}

// 	// Calculate 15-minute cutoff from market open
// 	fifteenMinCutoff := openNY.Add(15 * time.Minute)

// 	state := StrategyState{}
// 	var bars []MinuteBar
// 	var epBars []ep.StockData
// 	ticker := time.NewTicker(1 * time.Minute)
// 	defer ticker.Stop()
// 	immediate := make(chan struct{}, 1)
// 	immediate <- struct{}{}
// 	for {
// 		select {
// 		case <-immediate:
// 		case <-ticker.C:
// 		}
// 		nowNY := time.Now().In(locNY)

// 		// Check if we've exceeded 15 minutes from market open
// 		if nowNY.After(fifteenMinCutoff) {
// 			fmt.Printf("[#%d:%s] ⏱️ 15 minutes elapsed from market open — exiting\n", goroutineId, symbol)
// 			return
// 		}

// 		if nowNY.After(closeNY.Add(1 * time.Minute)) {
// 			fmt.Printf("[#%d:%s] past session close — exiting\n", goroutineId, symbol)
// 			return
// 		}
// 		if nowNY.Before(openNY) {
// 			wait := time.Until(openNY)
// 			if wait > 0 {
// 				fmt.Printf("[#%d:%s] waiting until open (in %s)\n", goroutineId, symbol, wait.Truncate(time.Second))
// 				time.Sleep(wait)
// 			}
// 		}
// 		startTime := toRFC3339(openNY)
// 		endTime := toRFC3339(nowNY)
// 		alpacaBars, err := fetchAlpacaIntraday(apiKey, apiSecret, symbol, startTime, endTime)
// 		if err != nil {
// 			log.Printf("[#%d:%s] fetchAlpacaIntraday error: %v", goroutineId, symbol, err)
// 			continue
// 		}
// 		if len(alpacaBars) == 0 {
// 			fmt.Printf("[#%d:%s] no intraday data yet\n", goroutineId, symbol)
// 			continue
// 		}
// 		newBars := make([]MinuteBar, 0, len(alpacaBars))
// 		for _, d := range alpacaBars {
// 			t, err := parseAlpacaTime(d.T)
// 			if err != nil {
// 				continue
// 			}
// 			tNY := t.In(locNY)
// 			if tNY.Before(openNY) || tNY.After(closeNY) {
// 				continue
// 			}
// 			newBars = append(newBars, MinuteBar{
// 				T: tNY, O: d.O, H: d.H, L: d.L, C: d.C, V: d.V,
// 			})
// 		}
// 		if len(newBars) == 0 {
// 			continue
// 		}

// 		sortMinuteBarsAsc(newBars)
// 		bars = mergeBars(bars, newBars)
// 		state.Recompute(bars)
// 		epBars = convertToEP(symbol, bars)
// 		fmt.Printf("[#%d:%s] t=%s  OR5[H/L]=[%.2f/%.2f]  OR15[H/L]=[%.2f/%.2f]  VWAP=%.3f  Tight5=%t Tight10=%t  bars=%d\n",
// 			goroutineId, symbol, state.LastUpdate.Format("15:04"),
// 			state.OR5High, state.OR5Low, state.OR15High, state.OR15Low, state.VWAP, state.IsTight5, state.IsTight10, len(bars),
// 		)

// 		// Run manager agent and check if we should stop
// 		shouldStop := runManagerAgent(epBars, symbol, sentiment, goroutineId)
// 		if shouldStop {
// 			fmt.Printf("[#%d:%s] 🛑 Buy recommendation received — stopping worker\n", goroutineId, symbol)
// 			return
// 		}
// 	}
// }

// func sortMinuteBarsAsc(b []MinuteBar) {
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
// 		s.PreviousClose = prevClose
// 		out = append(out, s)
// 		prevClose = b.C
// 	}
// 	return out
// }

// const timeFormat = "2006-01-02 15:04:00"

// func getMinute(timestampStr string) (int, error) {
// 	t, err := time.Parse(timeFormat, timestampStr)
// 	if err != nil {
// 		return 0, err
// 	}
// 	return t.Minute(), nil
// }

// type ManagerResponse struct {
// 	Recommendation string `json:"Recommendation"`
// 	EntryTime      string `json:"Entry Time,omitempty"`
// 	EntryPrice     string `json:"Entry Price,omitempty"`
// 	StopLoss       string `json:"Stop-Loss,omitempty"`
// 	RiskPercent    string `json:"Risk %,omitempty"`
// 	Reasoning      string `json:"Reasoning"`
// }

// type WatchlistEntry struct {
// 	StockSymbol string
// 	EntryPrice  string
// 	StopLoss    string
// 	Shares      string
// 	InitialRisk string
// }

// func extractJSON(response string) (*ManagerResponse, error) {
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
// 	dir := filepath.Join("responses", symbol)
// 	if err := os.MkdirAll(dir, 0755); err != nil {
// 		return fmt.Errorf("failed to create directory: %w", err)
// 	}
// 	filename := filepath.Join(dir, fmt.Sprintf("minute_%d_response.json", minute-29))
// 	data, err := json.Marshal(response)
// 	if err != nil {
// 		return fmt.Errorf("failed to marshal response: %w", err)
// 	}
// 	prettified := pretty.Pretty(data)
// 	return os.WriteFile(filename, prettified, 0644)
// }

// func addToWatchlist(entry WatchlistEntry) error {
// 	filename := "watchlist.csv"
// 	existingEntries := make(map[string]WatchlistEntry)
// 	if file, err := os.Open(filename); err == nil {
// 		defer file.Close()
// 		reader := csv.NewReader(file)
// 		records, err := reader.ReadAll()
// 		if err != nil {
// 			return fmt.Errorf("failed to read existing watchlist: %w", err)
// 		}
// 		startIdx := 0
// 		if len(records) > 0 && records[0][0] == "stock_symbol" {
// 			startIdx = 1
// 		}
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
// 	entryKey := fmt.Sprintf("%s|%s|%s|%s", entry.StockSymbol, entry.EntryPrice, entry.StopLoss, entry.Shares)
// 	if _, exists := existingEntries[entryKey]; exists {
// 		return fmt.Errorf("duplicate entry: %s with entry price %s and stop loss %s already exists in watchlist",
// 			entry.StockSymbol, entry.EntryPrice, entry.StopLoss)
// 	}
// 	fileExists := true
// 	if _, err := os.Stat(filename); os.IsNotExist(err) {
// 		fileExists = false
// 	}
// 	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
// 	if err != nil {
// 		return fmt.Errorf("failed to open watchlist file: %w", err)
// 	}
// 	defer file.Close()
// 	writer := csv.NewWriter(file)
// 	defer writer.Flush()
// 	if !fileExists {
// 		if err := writer.Write([]string{"stock_symbol", "entry_price", "stop_loss_price", "shares"}); err != nil {
// 			return fmt.Errorf("failed to write CSV header: %w", err)
// 		}
// 	}
// 	record := []string{entry.StockSymbol, entry.EntryPrice, entry.StopLoss, entry.Shares}
// 	if err := writer.Write(record); err != nil {
// 		return fmt.Errorf("failed to write CSV record: %w", err)
// 	}
// 	return nil
// }

// var riskPerTrade = 0.01

// // Modified runManagerAgent to return bool indicating whether to stop the worker
// func runManagerAgent(stockdata []ep.StockData, symbol string, sentiment string, goroutineId int) bool {
// 	fmt.Printf("\n[Goroutine %d] --- Starting runManagerAgent for %s (Sentiment: %s) ---\n", goroutineId, symbol, sentiment)
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
// 		return false
// 	}
// 	min, err := getMinute(stockdata[len(stockdata)-1].Timestamp)
// 	if err != nil {
// 		fmt.Printf("[Goroutine %d] ❌ Failed to parse minute from timestamp %s: %v\n",
// 			goroutineId, stockdata[len(stockdata)-1].Timestamp, err)
// 		return false
// 	}
// 	currentMinute := min
// 	managerResp, err := extractJSON(resp.Response)
// 	if err != nil {
// 		fmt.Printf("[Goroutine %d] ❌ Failed to extract JSON from response (minute %d): %v\n",
// 			goroutineId, currentMinute, err)
// 		return false
// 	}
// 	if err := saveJSONResponse(symbol, currentMinute, managerResp); err != nil {
// 		fmt.Printf("[Goroutine %d] ❌ Failed to save JSON response (minute %d): %v\n",
// 			goroutineId, currentMinute, err)
// 	} else {
// 		fmt.Printf("[Goroutine %d] ✓ JSON response saved for minute %d\n", goroutineId, currentMinute)
// 	}
// 	if err := godotenv.Load(); err != nil {
// 		log.Fatal("Error loading .env file")
// 	}
// 	stringPercent := strings.TrimSpace(strings.TrimSuffix(managerResp.RiskPercent, "%"))
// 	riskPercent, _ := strconv.ParseFloat(stringPercent, 64)
// 	accSize, _ := strconv.ParseFloat(os.Getenv("ACCOUNT_SIZE"), 64)
// 	entryPrice, _ := strconv.ParseFloat(strings.ReplaceAll(managerResp.EntryPrice, "$", ""), 64)
// 	stopLoss, _ := strconv.ParseFloat(strings.ReplaceAll(managerResp.StopLoss, "$", ""), 64)
// 	initialRisk, _ := strconv.ParseFloat(strings.ReplaceAll(managerResp.RiskPercent, "%", ""), 64)
// 	if strings.ToLower(strings.TrimSpace(managerResp.Recommendation)) == "buy" {
// 		if managerResp.EntryPrice != "" && managerResp.StopLoss != "" {
// 			shares := math.Round((((riskPercent) * (riskPerTrade * accSize)) * 1.0) / (entryPrice - stopLoss))
// 			entry := WatchlistEntry{
// 				StockSymbol: symbol,
// 				EntryPrice:  managerResp.EntryPrice,
// 				StopLoss:    managerResp.StopLoss,
// 				Shares:      strconv.FormatFloat(float64(int(shares)), 'f', 2, 64),
// 				InitialRisk: strconv.FormatFloat(initialRisk, 'f', 2, 64),
// 			}

// 			ep.PlaceEntryWithStop(symbol, stopLoss, int(shares), nil)

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
// 			return true // Stop the worker - buy recommendation received
// 		} else {
// 			fmt.Printf("[Goroutine %d] ⚠️ Buy recommendation for %s but missing entry price or stop loss (minute %d)\n",
// 				goroutineId, symbol, currentMinute)
// 			return false
// 		}
// 	} else {
// 		fmt.Printf("[Goroutine %d] 📊 Recommendation for %s: %s (minute %d)\n",
// 			goroutineId, symbol, managerResp.Recommendation, currentMinute)
// 		return false
// 	}
// }
