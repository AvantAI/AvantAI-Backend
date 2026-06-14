package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ─── Data Models ────────────────────────────────────────────────────────────

type LogLine struct {
	Type      string `json:"type"`
	Raw       string `json:"raw"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Stage     string `json:"stage"`
	Symbol    string `json:"symbol"`
	Message   string `json:"message"`
}

type WatchlistEntry struct {
	Symbol      string  `json:"symbol"`
	EntryPrice  float64 `json:"entry_price"`
	StopLoss    float64 `json:"stop_loss_price"`
	Shares      float64 `json:"shares"`
	InitialRisk float64 `json:"initial_risk"`
	Date        string  `json:"date"`
}

type TradeResult struct {
	Symbol      string  `json:"symbol"`
	EntryPrice  float64 `json:"entry_price"`
	ExitPrice   float64 `json:"exit_price"`
	Shares      float64 `json:"shares"`
	InitialRisk float64 `json:"initial_risk"`
	ProfitLoss  float64 `json:"profit_loss"`
	RiskReward  float64 `json:"risk_reward"`
	EntryDate   string  `json:"entry_date"`
	ExitDate    string  `json:"exit_date"`
	ExitReason  string  `json:"exit_reason"`
	IsWinner    bool    `json:"is_winner"`
}

// StockReport holds all fetched analysis content for a symbol
type StockReport struct {
	Symbol          string `json:"symbol"`
	EarningsReport  string `json:"earnings_report"`  // contents of earnings file
	NewsReport      string `json:"news_report"`       // contents of news_report.txt
	ManagerDecision string `json:"manager_decision"`  // contents of final minute_N_response.json
}

type InitialPayload struct {
	Type         string           `json:"type"`
	Watchlist    []WatchlistEntry `json:"watchlist"`
	Trades       []TradeResult    `json:"trades"`
	Stats        Stats            `json:"stats"`
	StockReports []StockReport    `json:"stock_reports"`
}

type Stats struct {
	TotalPnL        float64 `json:"total_pnl"`
	CompletedTrades int     `json:"completed_trades"`
	WinRate         float64 `json:"win_rate"`
	Winners         int     `json:"winners"`
	Losers          int     `json:"losers"`
	TotalOrders     int     `json:"total_orders"`
}

// ─── Globals ─────────────────────────────────────────────────────────────────

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	clients   = make(map[*websocket.Conn]bool)
	clientsMu sync.Mutex
	broadcast = make(chan []byte, 256)

	cachedWatchlist     []WatchlistEntry
	cachedTrades        []TradeResult
	cachedStats         Stats
	cachedStockReports  []StockReport
	logFilePath         string

	// Paths stored at startup so the tailer can trigger reloads
	watchlistPath string
	tradesPath    string
)

// ─── CSV Parsers ─────────────────────────────────────────────────────────────

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes"
}

func loadWatchlist(path string) ([]WatchlistEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	var entries []WatchlistEntry
	for i, row := range records {
		if i == 0 {
			continue
		}
		if len(row) < 5 {
			continue
		}
		entries = append(entries, WatchlistEntry{
			Symbol:      strings.TrimSpace(row[0]),
			EntryPrice:  parseFloat(row[1]),
			StopLoss:    parseFloat(row[2]),
			Shares:      parseFloat(row[3]),
			InitialRisk: parseFloat(row[4]),
			Date: func() string {
				if len(row) > 5 {
					return strings.TrimSpace(row[5])
				}
				return ""
			}(),
		})
	}
	return entries, nil
}

func loadTrades(path string) ([]TradeResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	var trades []TradeResult
	for i, row := range records {
		if i == 0 {
			continue
		}
		if len(row) < 10 {
			continue
		}
		trades = append(trades, TradeResult{
			Symbol:      strings.TrimSpace(row[0]),
			EntryPrice:  parseFloat(row[1]),
			ExitPrice:   parseFloat(row[2]),
			Shares:      parseFloat(row[3]),
			InitialRisk: parseFloat(row[4]),
			ProfitLoss:  parseFloat(row[5]),
			RiskReward:  parseFloat(row[6]),
			EntryDate:   strings.TrimSpace(row[7]),
			ExitDate:    strings.TrimSpace(row[8]),
			ExitReason:  strings.TrimSpace(row[9]),
			IsWinner: parseBool(func() string {
				if len(row) > 10 {
					return row[10]
				}
				return ""
			}()),
		})
	}
	return trades, nil
}

func computeStats(trades []TradeResult, watchlist []WatchlistEntry) Stats {
	var s Stats
	s.TotalOrders = len(watchlist) * 2
	s.CompletedTrades = len(trades)
	for _, t := range trades {
		s.TotalPnL += t.ProfitLoss
		if t.IsWinner {
			s.Winners++
		} else {
			s.Losers++
		}
	}
	if s.CompletedTrades > 0 {
		s.WinRate = float64(s.Winners) / float64(s.CompletedTrades) * 100
	}
	return s
}

// ─── Stock Report Loader ──────────────────────────────────────────────────────

// readFileIfExists returns the file contents as a string, or empty string if not found.
func readFileIfExists(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// findFinalMinuteResponse scans responses/{symbol}/ for minute_N_response.json
// and returns the contents of the one with the highest N.
func findFinalMinuteResponse(symbol string) string {
	dir := filepath.Join("responses", symbol)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	type numbered struct {
		n    int
		path string
	}
	var found []numbered

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "minute_") || !strings.HasSuffix(name, "_response.json") {
			continue
		}
		// Extract the number between "minute_" and "_response.json"
		inner := strings.TrimPrefix(name, "minute_")
		inner = strings.TrimSuffix(inner, "_response.json")
		n, err := strconv.Atoi(inner)
		if err != nil {
			continue
		}
		found = append(found, numbered{n: n, path: filepath.Join(dir, name)})
	}

	if len(found) == 0 {
		return ""
	}

	// Sort descending, pick the highest
	sort.Slice(found, func(i, j int) bool { return found[i].n > found[j].n })

	data, err := os.ReadFile(found[0].path)
	if err != nil {
		return ""
	}

	// Pretty-print the JSON if possible
	var pretty interface{}
	if err := json.Unmarshal(data, &pretty); err == nil {
		if out, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			return string(out)
		}
	}
	return strings.TrimSpace(string(data))
}

// findEarningsReport looks for any file in reports/{symbol}/ that is NOT news_report.txt.
// Typically named something like "{symbol}_earnings_analysis.txt" or similar.
func findEarningsReport(symbol string) string {
	dir := filepath.Join("reports", symbol)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.EqualFold(name, "news_report.txt") {
			continue
		}
		// Return the first non-news file found (earnings report)
		return readFileIfExists(filepath.Join(dir, name))
	}
	return ""
}

// loadStockReports builds a StockReport for each symbol in the watchlist.
func loadStockReports(watchlist []WatchlistEntry) []StockReport {
	var reports []StockReport
	seen := map[string]bool{}

	for _, w := range watchlist {
		sym := w.Symbol
		if seen[sym] {
			continue
		}
		seen[sym] = true

		report := StockReport{
			Symbol:          sym,
			NewsReport:      readFileIfExists(filepath.Join("reports", sym, "news_report.txt")),
			EarningsReport:  findEarningsReport(sym),
			ManagerDecision: findFinalMinuteResponse(sym),
		}

		// Only include if we found at least some data
		if report.NewsReport != "" || report.EarningsReport != "" || report.ManagerDecision != "" {
			reports = append(reports, report)
			log.Printf("Loaded reports for %s (news=%v, earnings=%v, decision=%v)",
				sym,
				report.NewsReport != "",
				report.EarningsReport != "",
				report.ManagerDecision != "",
			)
		} else {
			log.Printf("No reports found for %s", sym)
		}
	}
	return reports
}

// ─── Watchlist Reload ─────────────────────────────────────────────────────────

// reloadWatchlistAndBroadcast re-reads the watchlist/trades CSVs, recomputes
// everything, and pushes a fresh "init" payload to all connected clients.
// Called whenever the log contains "OK: Main backtest completed for <date>".
func reloadWatchlistAndBroadcast() {
	log.Printf("Backtest completion detected — reloading watchlist from %s", watchlistPath)

	wl, err := loadWatchlist(watchlistPath)
	if err != nil {
		log.Printf("Warning: could not reload watchlist (%s): %v", watchlistPath, err)
		return
	}
	tr, err := loadTrades(tradesPath)
	if err != nil {
		log.Printf("Warning: could not reload trades (%s): %v", tradesPath, err)
		// non-fatal — carry on with whatever we have
	}

	cachedWatchlist = wl
	cachedTrades = tr
	cachedStats = computeStats(tr, wl)
	cachedStockReports = loadStockReports(wl)

	log.Printf("Reload complete: %d watchlist entries, %d trades, %d stock reports",
		len(cachedWatchlist), len(cachedTrades), len(cachedStockReports))

	payload := InitialPayload{
		Type:         "init",
		Watchlist:    cachedWatchlist,
		Trades:       cachedTrades,
		Stats:        cachedStats,
		StockReports: cachedStockReports,
	}
	data, _ := json.Marshal(payload)
	select {
	case broadcast <- data:
	default:
		log.Printf("Warning: broadcast channel full, reload payload dropped")
	}
}

// isBacktestCompletedLine returns true when a log line signals that the main
// backtest has finished, regardless of the date embedded in the message.
func isBacktestCompletedLine(line string) bool {
	return strings.Contains(line, "OK: Main backtest completed for")
}

// ─── Log Parser ───────────────────────────────────────────────────────────────

func parseLogLine(raw string) LogLine {
	ll := LogLine{Type: "log", Raw: raw}
	s := raw

	extractBracket := func() string {
		if len(s) == 0 || s[0] != '[' {
			return ""
		}
		end := strings.Index(s, "]")
		if end < 0 {
			return ""
		}
		val := strings.TrimSpace(s[1:end])
		s = strings.TrimLeft(s[end+1:], " ")
		return val
	}

	ll.Timestamp = extractBracket()
	ll.Level = extractBracket()
	ll.Stage = extractBracket()

	if len(s) > 0 && s[0] == '[' {
		end := strings.Index(s, "]")
		if end > 0 && end <= 8 {
			candidate := strings.TrimSpace(s[1:end])
			if isSymbolLike(candidate) {
				ll.Symbol = candidate
				s = strings.TrimLeft(s[end+1:], " ")
			}
		}
	}

	ll.Message = strings.TrimSpace(s)
	return ll
}

func isSymbolLike(s string) bool {
	if len(s) == 0 || len(s) > 8 {
		return false
	}
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

// ─── WebSocket Hub ────────────────────────────────────────────────────────────

func handleConnections(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}
	defer func() {
		clientsMu.Lock()
		delete(clients, conn)
		clientsMu.Unlock()
		conn.Close()
	}()

	clientsMu.Lock()
	clients[conn] = true
	clientsMu.Unlock()

	init := InitialPayload{
		Type:         "init",
		Watchlist:    cachedWatchlist,
		Trades:       cachedTrades,
		Stats:        cachedStats,
		StockReports: cachedStockReports,
	}
	data, _ := json.Marshal(init)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return
	}

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func broadcastLoop() {
	for msg := range broadcast {
		clientsMu.Lock()
		for conn := range clients {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				conn.Close()
				delete(clients, conn)
			}
		}
		clientsMu.Unlock()
	}
}

// ─── Log Tailer ──────────────────────────────────────────────────────────────

func tailLog(path string) {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("Cannot open log file %s: %v", path, err)
		go func() {
			time.Sleep(2 * time.Second)
			sendLogLine("[INFO] Log file not found — waiting for data...")
		}()
		return
	}

	scanner := bufio.NewScanner(f)
	var existing []string
	for scanner.Scan() {
		existing = append(existing, scanner.Text())
	}

	for _, line := range existing {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Check historical lines too, so we don't miss a completed backtest
		// that was already written before the server started.
		if isBacktestCompletedLine(line) {
			reloadWatchlistAndBroadcast()
		}
		sendLogLine(line)
		time.Sleep(2 * time.Millisecond)
	}

	pos, _ := f.Seek(0, io.SeekCurrent)
	for {
		time.Sleep(500 * time.Millisecond)
		newF, err2 := os.Open(path)
		if err2 != nil {
			continue
		}
		newF.Seek(pos, io.SeekStart)
		scanner2 := bufio.NewScanner(newF)
		for scanner2.Scan() {
			line := scanner2.Text()
			if strings.TrimSpace(line) == "" {
				newF.Close()
				continue
			}
			if isBacktestCompletedLine(line) {
				reloadWatchlistAndBroadcast()
			}
			sendLogLine(line)
		}
		pos, _ = newF.Seek(0, io.SeekCurrent)
		newF.Close()
	}
}

func sendLogLine(raw string) {
	ll := parseLogLine(raw)
	data, err := json.Marshal(ll)
	if err != nil {
		return
	}
	select {
	case broadcast <- data:
	default:
	}
}

// ─── CORS middleware ─────────────────────────────────────────────────────────

func withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		h(w, r)
	}
}

// ─── REST endpoints ───────────────────────────────────────────────────────────

func apiStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cachedStats)
}

func apiTrades(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cachedTrades)
}

func apiWatchlist(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cachedWatchlist)
}

func apiReports(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cachedStockReports)
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	watchlistPath = getEnv("WATCHLIST_CSV", "watchlist.csv")
	tradesPath = getEnv("TRADES_CSV", "traderesults.csv")
	logFilePath = getEnv("LOG_FILE", "backtest_execution.log")
	port := getEnv("PORT", "32768")

	var err error
	cachedWatchlist, err = loadWatchlist(watchlistPath)
	if err != nil {
		log.Printf("Warning: could not load watchlist (%s): %v", watchlistPath, err)
	} else {
		log.Printf("Loaded %d watchlist entries", len(cachedWatchlist))
	}

	cachedTrades, err = loadTrades(tradesPath)
	if err != nil {
		log.Printf("Warning: could not load trade results (%s): %v", tradesPath, err)
	} else {
		log.Printf("Loaded %d trade results", len(cachedTrades))
	}

	cachedStats = computeStats(cachedTrades, cachedWatchlist)

	// Load per-symbol reports from reports/ and responses/ directories
	cachedStockReports = loadStockReports(cachedWatchlist)
	log.Printf("Loaded reports for %d symbols", len(cachedStockReports))

	go broadcastLoop()
	go tailLog(logFilePath)

	http.HandleFunc("/ws", withCORS(handleConnections))
	http.HandleFunc("/api/stats", withCORS(apiStats))
	http.HandleFunc("/api/trades", withCORS(apiTrades))
	http.HandleFunc("/api/watchlist", withCORS(apiWatchlist))
	http.HandleFunc("/api/reports", withCORS(apiReports))

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	fmt.Printf("🚀 Trading dashboard server running on :%s\n", port)
	fmt.Printf("   WebSocket: ws://192.168.68.108:%s/ws\n", port)
	fmt.Printf("   REST API:  http://192.168.68.108:%s/api/stats\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}