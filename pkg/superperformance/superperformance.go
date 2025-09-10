package superperformance

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Configuration
const (
	MARKETSTACK_API_URL = "http://api.marketstack.com/v1"
	MIN_VOLUME          = 200000
	MAX_CONCURRENT      = 5
	DAYS_LOOKBACK       = 3650 // 3 years of trading days
)

// API Response structures
type MarketStackResponse struct {
	Data       []StockData `json:"data"`
	Pagination struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Count  int `json:"count"`
		Total  int `json:"total"`
	} `json:"pagination"`
}

type StockData struct {
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   float64 `json:"volume"`
	Date     string  `json:"date"`
	Symbol   string  `json:"symbol"`
	Exchange string  `json:"exchange"`
}

// Core data structures
type GrowthMove struct {
	Ticker         string
	StartDate      time.Time
	EndDate        time.Time
	LOD            float64
	PeakPrice      float64
	PeakDate       time.Time
	TotalGain      float64
	DurationDays   int
	IsGrowthStock  bool
	IsSuperperform bool
	Drawdowns      []Drawdown
	Continuations  []Continuation
	Status         string
	EndReason      string
	DaysSinceHigh  int
	InDrawdown     bool
	DrawdownStart  time.Time
	DrawdownLow    float64

	// New fields for continuation tracking
	PendingContinuation  bool
	ContinuationDeadline time.Time
	ContinuationLOD      float64
}

type Drawdown struct {
	StartDate time.Time
	EndDate   time.Time
	LowPrice  float64
}

type Continuation struct {
	OldPeak          float64
	ContinuationDate time.Time
	NewLOD           float64
}

type Result struct {
	Ticker           string
	StartDate        string
	EndDate          string
	Superperformance string
	Drawdowns        string
	Continuation     string
	TotalGain        float64
	DurationDays     int
}

// API Client
type MarketStackClient struct {
	APIKey     string
	HTTPClient *http.Client
	RateLimit  chan struct{}
}

func (c *MarketStackClient) GetStockData(symbol string, dateFrom, dateTo string) ([]StockData, error) {
	fmt.Printf("[DEBUG] Starting API request for %s from %s to %s\n", symbol, dateFrom, dateTo)

	// Rate limiting: acquire token
	<-c.RateLimit
	fmt.Printf("[DEBUG] Rate limit token acquired for %s\n", symbol)
	defer func() {
		c.RateLimit <- struct{}{}
		fmt.Printf("[DEBUG] Rate limit token released for %s\n", symbol)
	}()

	// Build API URL
	url := fmt.Sprintf("%s/eod?access_key=%s&symbols=%s&date_from=%s&date_to=%s&limit=1000",
		MARKETSTACK_API_URL, c.APIKey, symbol, dateFrom, dateTo)
	fmt.Printf("[DEBUG] API URL constructed: %s\n", strings.Replace(url, c.APIKey, "***", -1))

	// Make HTTP request
	fmt.Printf("[DEBUG] Making HTTP GET request for %s\n", symbol)
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		fmt.Printf("[ERROR] HTTP request failed for %s: %v\n", symbol, err)
		return nil, fmt.Errorf("API request failed: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			fmt.Printf("[ERROR] Failed to close response body for %s: %v\n", symbol, closeErr)
		}
	}()

	fmt.Printf("[DEBUG] HTTP response received for %s with status: %d\n", symbol, resp.StatusCode)

	// Check HTTP status
	if resp.StatusCode != 200 {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			fmt.Printf("[ERROR] Failed to read error response body for %s: %v\n", symbol, readErr)
			return nil, fmt.Errorf("API returned status %d and failed to read response", resp.StatusCode)
		}
		fmt.Printf("[ERROR] API returned non-200 status for %s: %d, body: %s\n", symbol, resp.StatusCode, string(body))
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Decode JSON response
	fmt.Printf("[DEBUG] Decoding JSON response for %s\n", symbol)
	var apiResp MarketStackResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		fmt.Printf("[ERROR] Failed to decode JSON response for %s: %v\n", symbol, err)
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	fmt.Printf("[DEBUG] Successfully decoded %d data points for %s\n", len(apiResp.Data), symbol)

	// Validate data
	if len(apiResp.Data) == 0 {
		fmt.Printf("[WARNING] No data returned for %s\n", symbol)
		return nil, fmt.Errorf("no data returned for symbol %s", symbol)
	}

	// Sort by date (oldest first)
	fmt.Printf("[DEBUG] Sorting data by date for %s\n", symbol)
	sort.Slice(apiResp.Data, func(i, j int) bool {
		dateI := parseStockDate(apiResp.Data[i].Date)
		dateJ := parseStockDate(apiResp.Data[j].Date)

		if dateI.IsZero() || dateJ.IsZero() {
			return false
		}

		return dateI.Before(dateJ)
	})

	fmt.Printf("[DEBUG] Successfully retrieved and sorted %d data points for %s\n", len(apiResp.Data), symbol)
	return apiResp.Data, nil
}

// Helper function to parse various date formats from MarketStack
func parseStockDate(dateStr string) time.Time {
	formats := []string{
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t
		}
	}

	fmt.Printf("[ERROR] Failed to parse date: %s\n", dateStr)
	return time.Time{}
}

// Core algorithm functions
func (c *MarketStackClient) AnalyzeStock(symbol string) ([]Result, error) {
	fmt.Printf("[INFO] Starting analysis for %s\n", symbol)

	// Validate input
	if strings.TrimSpace(symbol) == "" {
		fmt.Printf("[ERROR] Empty symbol provided\n")
		return nil, fmt.Errorf("symbol cannot be empty")
	}

	// Get 3 years of historical data
	dateTo := time.Now().Format("2006-01-02")
	dateFrom := time.Now().AddDate(-10, 0, 0).Format("2006-01-02")
	fmt.Printf("[DEBUG] Fetching data for %s from %s to %s\n", symbol, dateFrom, dateTo)

	stockData, err := c.GetStockData(symbol, dateFrom, dateTo)
	if err != nil {
		fmt.Printf("[ERROR] Failed to get data for %s: %v\n", symbol, err)
		return nil, fmt.Errorf("failed to get data for %s: %v", symbol, err)
	}

	// Validate sufficient data
	if len(stockData) < 50 {
		fmt.Printf("[WARNING] Insufficient data for %s: only %d days available\n", symbol, len(stockData))
		return nil, fmt.Errorf("insufficient data for %s: only %d days available", symbol, len(stockData))
	}
	fmt.Printf("[DEBUG] %s has sufficient data: %d days\n", symbol, len(stockData))

	// Check volume requirement
	fmt.Printf("[DEBUG] Checking volume requirements for %s\n", symbol)
	if !meetsVolumeRequirement(stockData) {
		fmt.Printf("[WARNING] %s does not meet volume requirement (min: %d)\n", symbol, MIN_VOLUME)
		return nil, fmt.Errorf("%s does not meet volume requirement", symbol)
	}
	fmt.Printf("[DEBUG] %s meets volume requirements\n", symbol)

	// Find growth moves
	fmt.Printf("[DEBUG] Searching for growth moves in %s\n", symbol)
	results := findGrowthMoves(symbol, stockData)
	fmt.Printf("[INFO] Found %d growth moves for %s\n", len(results), symbol)

	return results, nil
}

func meetsVolumeRequirement(data []StockData) bool {
	fmt.Printf("[DEBUG] Checking volume requirements\n")

	if len(data) < 20 {
		fmt.Printf("[DEBUG] Not enough data for volume check: %d days\n", len(data))
		return false
	}

	// Calculate average volume over last 20 days
	totalVolume := 0.0
	count := 0
	validDays := 0

	for i := len(data) - 20; i < len(data); i++ {
		validDays++
		if data[i].Volume > 0 {
			totalVolume += data[i].Volume
			count++
		}
	}

	fmt.Printf("[DEBUG] Volume check: %d valid days out of %d, %d with volume data\n", count, validDays, count)

	if count == 0 {
		fmt.Printf("[DEBUG] No volume data available in last 20 days\n")
		return false
	}

	avgVolume := totalVolume / float64(count)
	fmt.Printf("[DEBUG] Average volume: %.0f, required: %d\n", avgVolume, MIN_VOLUME)

	return avgVolume >= MIN_VOLUME
}

func findGrowthMoves(symbol string, data []StockData) []Result {
	fmt.Printf("[DEBUG] Starting growth move detection for %s with %d data points\n", symbol, len(data))

	var results []Result
	var activeMove *GrowthMove
	movesFound := 0
	potentialStarts := 0

	for i := 0; i < len(data); i++ {
		currentDay := data[i]
		currentDate := parseStockDate(currentDay.Date)
		if currentDate.IsZero() {
			continue
		}

		// Process active move
		if activeMove != nil {
			fmt.Printf("[DEBUG] Updating active move for %s on %s (Day %d since start)\n",
				symbol, currentDate.Format("2006-01-02"),
				int(currentDate.Sub(activeMove.StartDate).Hours()/24))

			endMove := updateActiveMove(activeMove, currentDay, currentDate)
			if endMove {
				fmt.Printf("[DEBUG] Ending move for %s: %s on %s\n", symbol, activeMove.EndReason, currentDate.Format("2006-01-02"))
				if result := finalizeGrowthMove(activeMove); result != nil {
					results = append(results, *result)
					movesFound++
					fmt.Printf("[INFO] Completed growth move #%d for %s: %.2f%% gain over %d days\n",
						movesFound, symbol, result.TotalGain, result.DurationDays)
				}
				activeMove = nil
			}
			continue
		}

		// Look for new growth start
		if newMove := checkForGrowthStart(symbol, data, i); newMove != nil {
			potentialStarts++
			activeMove = newMove
			fmt.Printf("[INFO] New growth move started for %s on %s (potential start #%d)\n",
				symbol, currentDate.Format("2006-01-02"), potentialStarts)
		}
	}

	// Handle any remaining active move at end of data
	if activeMove != nil {
		fmt.Printf("[DEBUG] Finalizing remaining active move for %s\n", symbol)
		// For moves that are still active at the end of data, use the last data point
		if len(data) > 0 {
			lastDay := data[len(data)-1]
			lastDate := parseStockDate(lastDay.Date)
			if !lastDate.IsZero() {
				activeMove.EndDate = lastDate
				activeMove.EndReason = "End of data"
				// Make sure we have the correct ending state
				fmt.Printf("[DEBUG] Setting end date to last data point: %s\n", lastDate.Format("2006-01-02"))
			}
		}

		if result := finalizeGrowthMove(activeMove); result != nil {
			results = append(results, *result)
			movesFound++
			fmt.Printf("[INFO] Final growth move for %s: %.2f%% gain over %d days\n",
				symbol, result.TotalGain, result.DurationDays)
		}
	}

	fmt.Printf("[DEBUG] Growth move detection complete for %s: %d potential starts, %d valid moves\n",
		symbol, potentialStarts, len(results))
	return results
}

func checkForGrowthStart(symbol string, data []StockData, startIndex int) *GrowthMove {
	if startIndex+5 >= len(data) {
		return nil
	}

	currentDay := data[startIndex]
	potentialLOD := currentDay.Low
	startDate := parseStockDate(currentDay.Date)
	if startDate.IsZero() {
		return nil
	}

	fmt.Printf("[DEBUG] Checking growth start for %s on %s with LOD %.2f\n",
		symbol, startDate.Format("2006-01-02"), potentialLOD)

	// Check for 5% confirmation within 5 days
	for i := 1; i <= 5 && startIndex+i < len(data); i++ {
		nextDay := data[startIndex+i]
		nextDate := parseStockDate(nextDay.Date)
		if nextDate.IsZero() {
			continue
		}

		// Check if LOD is broken
		if nextDay.Low < potentialLOD {
			fmt.Printf("[DEBUG] LOD broken on day %d for %s: %.2f < %.2f\n",
				i, symbol, nextDay.Low, potentialLOD)
			return nil
		}

		// Check for 5% gain confirmation
		gain := (nextDay.High - potentialLOD) / potentialLOD
		if gain >= 0.05 {
			fmt.Printf("[INFO] Growth confirmed for %s on %s: %.2f%% gain in %d days\n",
				symbol, nextDate.Format("2006-01-02"), gain*100, i)

			return &GrowthMove{
				Ticker:        symbol,
				StartDate:     startDate,
				LOD:           potentialLOD,
				PeakPrice:     nextDay.High,
				PeakDate:      nextDate,
				Status:        "ACTIVE",
				Drawdowns:     []Drawdown{},
				Continuations: []Continuation{},
			}
		}
	}

	return nil
}

func updateActiveMove(move *GrowthMove, currentDay StockData, currentDate time.Time) bool {
	// Handle pending continuation check first
	if move.PendingContinuation {
		if currentDay.High > move.PeakPrice {
			// Continuation successful - new high achieved
			fmt.Printf("[INFO] Continuation successful for %s: new high %.2f > %.2f\n",
				move.Ticker, currentDay.High, move.PeakPrice)

			move.Continuations = append(move.Continuations, Continuation{
				OldPeak:          move.PeakPrice,
				ContinuationDate: currentDate,
				NewLOD:           move.ContinuationLOD,
			})

			move.LOD = move.ContinuationLOD
			move.PeakPrice = currentDay.High
			move.PeakDate = currentDate
			move.DaysSinceHigh = 0
			move.PendingContinuation = false
			move.Status = "ACTIVE"

		} else if currentDate.After(move.ContinuationDeadline) {
			// Continuation failed - deadline exceeded
			fmt.Printf("[DEBUG] Continuation failed for %s: deadline exceeded on %s\n", move.Ticker, currentDate.Format("2006-01-02"))
			move.EndDate = currentDate // Use current date, not deadline
			move.EndReason = "Continuation failed"
			return true
		}
		// Continue checking for continuation
		return false
	}

	// Update peak if new high
	if currentDay.High > move.PeakPrice {
		oldPeak := move.PeakPrice
		move.PeakPrice = currentDay.High
		move.PeakDate = currentDate
		move.DaysSinceHigh = 0

		gain := (move.PeakPrice - move.LOD) / move.LOD * 100
		fmt.Printf("[DEBUG] New high for %s: %.2f -> %.2f (%.2f%% total gain)\n",
			move.Ticker, oldPeak, move.PeakPrice, gain)
	} else {
		move.DaysSinceHigh++
	}

	// Check end conditions
	endReason := checkEndConditions(move, currentDay, currentDate)
	if endReason != "" {
		// Special case: 30% drop might lead to continuation
		if endReason == "30% decline" {
			fmt.Printf("[DEBUG] 30%% drop detected for %s - checking for continuation possibility\n", move.Ticker)

			// Set up continuation tracking
			move.PendingContinuation = true
			move.ContinuationDeadline = move.PeakDate.AddDate(0, 0, 90) // 90 trading days from peak, not current date
			move.ContinuationLOD = currentDay.Low
			move.Status = "PENDING_CONTINUATION"

			fmt.Printf("[DEBUG] Continuation setup for %s: deadline %s, new LOD %.2f\n",
				move.Ticker, move.ContinuationDeadline.Format("2006-01-02"), move.ContinuationLOD)

			return false // Don't end yet, wait for continuation
		}

		// For all other end conditions, set the end date to current date
		move.EndDate = currentDate
		move.EndReason = endReason
		fmt.Printf("[DEBUG] Move ended for %s on %s: %s\n", move.Ticker, currentDate.Format("2006-01-02"), endReason)
		return true
	}

	// Check for drawdowns
	checkDrawdownConditions(move, currentDay, currentDate)

	return false
}

func checkEndConditions(move *GrowthMove, currentDay StockData, currentDate time.Time) string {
	// Condition 1: 30% drop from peak
	declineFromPeak := (move.PeakPrice - currentDay.Low) / move.PeakPrice
	if declineFromPeak >= 0.30 {
		fmt.Printf("[DEBUG] End condition check for %s: 30%% decline (%.2f%%)\n",
			move.Ticker, declineFromPeak*100)
		return "30% decline"
	}

	// Condition 2: Break below LOD
	if currentDay.Low < move.LOD {
		fmt.Printf("[DEBUG] End condition met for %s: LOD broken (%.2f < %.2f)\n",
			move.Ticker, currentDay.Low, move.LOD)
		return "LOD broken"
	}

	// Condition 3: No new high in 30 days
	if move.DaysSinceHigh >= 30 {
		fmt.Printf("[DEBUG] End condition met for %s: No new high in %d days\n",
			move.Ticker, move.DaysSinceHigh)
		return "No new high in 30 days"
	}

	// Condition 4: Maximum time exceeded (504 days)
	totalDays := int(currentDate.Sub(move.StartDate).Hours() / 24)
	if totalDays >= 504 {
		fmt.Printf("[DEBUG] End condition met for %s: Maximum time exceeded (%d days)\n",
			move.Ticker, totalDays)
		return "Maximum time exceeded"
	}

	return ""
}

func checkDrawdownConditions(move *GrowthMove, currentDay StockData, currentDate time.Time) {
	declineFromPeak := (move.PeakPrice - currentDay.Low) / move.PeakPrice

	// Check if in drawdown range (15-29.9%)
	if declineFromPeak >= 0.15 && declineFromPeak < 0.30 {
		if !move.InDrawdown {
			// Start new drawdown
			move.InDrawdown = true
			move.DrawdownStart = currentDate
			move.DrawdownLow = currentDay.Low
			fmt.Printf("[DEBUG] Drawdown started for %s on %s: %.2f%% decline\n",
				move.Ticker, currentDate.Format("2006-01-02"), declineFromPeak*100)
		} else {
			// Update drawdown low if necessary
			if currentDay.Low < move.DrawdownLow {
				fmt.Printf("[DEBUG] Drawdown deepened for %s: %.2f -> %.2f\n",
					move.Ticker, move.DrawdownLow, currentDay.Low)
				move.DrawdownLow = currentDay.Low
			}
		}
	} else if move.InDrawdown && declineFromPeak < 0.15 {
		// Drawdown recovery
		fmt.Printf("[DEBUG] Drawdown recovery for %s: lasted from %s to %s\n",
			move.Ticker, move.DrawdownStart.Format("2006-01-02"), currentDate.Format("2006-01-02"))

		move.Drawdowns = append(move.Drawdowns, Drawdown{
			StartDate: move.DrawdownStart,
			EndDate:   currentDate,
			LowPrice:  move.DrawdownLow,
		})

		// Update LOD to drawdown low
		fmt.Printf("[DEBUG] LOD updated for %s: %.2f -> %.2f\n",
			move.Ticker, move.LOD, move.DrawdownLow)
		move.LOD = move.DrawdownLow
		move.InDrawdown = false
	}
}

func finalizeGrowthMove(move *GrowthMove) *Result {
	fmt.Printf("[DEBUG] Finalizing growth move for %s\n", move.Ticker)

	// Ensure end date is properly set - this was the main bug
	if move.EndDate.IsZero() {
		// If no end date was set, use the peak date as fallback
		move.EndDate = move.PeakDate
		move.EndReason = "Peak date used as end (no explicit end condition met)"
		fmt.Printf("[DEBUG] Warning: No end date set for %s, using peak date %s\n", 
			move.Ticker, move.PeakDate.Format("2006-01-02"))
	}

	// Calculate metrics using the correct dates
	totalDays := int(move.EndDate.Sub(move.StartDate).Hours() / 24)
	if totalDays <= 0 {
		// Fallback calculation
		totalDays = int(move.PeakDate.Sub(move.StartDate).Hours() / 24)
		fmt.Printf("[DEBUG] Warning: Invalid duration calculated for %s, using peak-based calculation\n", move.Ticker)
	}

	totalGainPercent := (move.PeakPrice - move.LOD) / move.LOD * 100

	move.TotalGain = totalGainPercent
	move.DurationDays = totalDays

	fmt.Printf("[DEBUG] Move metrics for %s: %.2f%% gain over %d days (Start: %s, End: %s, Peak: %s)\n",
		move.Ticker, totalGainPercent, totalDays, 
		move.StartDate.Format("2006-01-02"), 
		move.EndDate.Format("2006-01-02"),
		move.PeakDate.Format("2006-01-02"))

	// Classify growth type - Fixed criteria
	if (totalDays >= 64 && totalDays <= 252 && totalGainPercent >= 100) ||
		(totalDays >= 252 && totalDays <= 504 && totalGainPercent >= 150) {
		move.IsGrowthStock = true
		fmt.Printf("[DEBUG] %s classified as growth stock\n", move.Ticker)
	} else {
		fmt.Printf("[DEBUG] %s does not qualify as growth stock (%.2f%% in %d days)\n",
			move.Ticker, totalGainPercent, totalDays)
	}

	// Check superperformance - Fixed criteria
	if (totalDays >= 64 && totalDays <= 252 && totalGainPercent >= 300) ||
		(totalDays >= 252 && totalDays <= 504 && totalGainPercent >= 500) {
		move.IsSuperperform = true
		fmt.Printf("[INFO] %s is a SUPERPERFORMER! %.2f%% gain\n", move.Ticker, totalGainPercent)
	}

	// Only return growth stocks
	if !move.IsGrowthStock {
		fmt.Printf("[DEBUG] Excluding %s from results (not a growth stock)\n", move.Ticker)
		return nil
	}

	// Format drawdowns
	drawdownStr := "none"
	if len(move.Drawdowns) > 0 {
		var drawdownDates []string
		for _, d := range move.Drawdowns {
			drawdownDates = append(drawdownDates, d.StartDate.Format("Jan 2, 2006"))
		}
		drawdownStr = strings.Join(drawdownDates, "; ")
		fmt.Printf("[DEBUG] %s had %d drawdowns: %s\n", move.Ticker, len(move.Drawdowns), drawdownStr)
	}

	continuationStr := "none"
	if len(move.Continuations) > 0 {
		continuationStr = "Yes"
	}

	superperformStr := "No"
	if move.IsSuperperform {
		superperformStr = "Yes"
	}

	result := &Result{
		Ticker:           move.Ticker,
		StartDate:        move.StartDate.Format("Jan 2, 2006"),
		EndDate:          move.EndDate.Format("Jan 2, 2006"),
		Superperformance: superperformStr,
		Drawdowns:        drawdownStr,
		Continuation:     continuationStr,
		TotalGain:        totalGainPercent,
		DurationDays:     totalDays,
	}

	fmt.Printf("[INFO] Growth move result created for %s: Start=%s, End=%s, Gain=%.2f%%, Days=%d\n", 
		move.Ticker, result.StartDate, result.EndDate, result.TotalGain, result.DurationDays)
	return result
}

// Stock listing structures
type TickerResponse struct {
	Data       []TickerData `json:"data"`
	Pagination struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Count  int `json:"count"`
		Total  int `json:"total"`
	} `json:"pagination"`
}

type TickerData struct {
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Exchange string `json:"exchange"`
}

// Utility functions - Simplified to only support CSV loading
func LoadStocksFromCSV(filename string) ([]string, error) {
	fmt.Printf("[DEBUG] Loading stock symbols from %s\n", filename)

	// Check if file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		fmt.Printf("[ERROR] CSV file does not exist: %s\n", filename)
		return nil, fmt.Errorf("CSV file does not exist: %s", filename)
	}

	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("[ERROR] Failed to open CSV file %s: %v\n", filename, err)
		return nil, fmt.Errorf("failed to open CSV file: %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			fmt.Printf("[ERROR] Failed to close CSV file: %v\n", closeErr)
		}
	}()

	fmt.Printf("[DEBUG] CSV file opened successfully\n")
	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		fmt.Printf("[ERROR] Failed to read CSV file %s: %v\n", filename, err)
		return nil, fmt.Errorf("failed to read CSV: %v", err)
	}

	fmt.Printf("[DEBUG] Read %d records from CSV\n", len(records))

	var symbols []string
	skippedRows := 0

	for i, record := range records {
		if i == 0 { // Skip header
			fmt.Printf("[DEBUG] Skipping header row: %v\n", record)
			continue
		}

		if len(record) == 0 {
			fmt.Printf("[WARNING] Empty record at row %d\n", i+1)
			skippedRows++
			continue
		}

		if record[0] == "" {
			fmt.Printf("[WARNING] Empty symbol at row %d\n", i+1)
			skippedRows++
			continue
		}

		symbol := strings.TrimSpace(strings.ToUpper(record[0]))
		symbols = append(symbols, symbol)
		fmt.Printf("[DEBUG] Added symbol: %s\n", symbol)
	}

	fmt.Printf("[INFO] Loaded %d symbols from CSV (%d rows skipped)\n", len(symbols), skippedRows)
	return symbols, nil
}

func SaveResultsToJSON(results []Result, filename string) error {
	fmt.Printf("[DEBUG] Saving %d results to %s\n", len(results), filename)

	// Create directory if it doesn't exist
	if err := os.MkdirAll("superperformance_data", 0755); err != nil {
		fmt.Printf("[ERROR] Failed to create output directory: %v\n", err)
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	file, err := os.Create(filename)
	if err != nil {
		fmt.Printf("[ERROR] Failed to create output file %s: %v\n", filename, err)
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			fmt.Printf("[ERROR] Failed to close output file: %v\n", closeErr)
		}
	}()

	fmt.Printf("[DEBUG] Output file created successfully\n")

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ") // Pretty print with 2-space indentation

	if err := encoder.Encode(results); err != nil {
		fmt.Printf("[ERROR] Failed to encode JSON to %s: %v\n", filename, err)
		return fmt.Errorf("failed to encode JSON: %v", err)
	}

	fmt.Printf("[INFO] Successfully saved %d results to %s\n", len(results), filename)
	return nil
}

func SaveResultsToCSV(results []Result, filename string) error {
	fmt.Printf("[DEBUG] Saving %d results to %s\n", len(results), filename)

	// Create directory if it doesn't exist
	if err := os.MkdirAll("superperformance_data", 0755); err != nil {
		fmt.Printf("[ERROR] Failed to create output directory: %v\n", err)
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	file, err := os.Create(filename)
	if err != nil {
		fmt.Printf("[ERROR] Failed to create output file %s: %v\n", filename, err)
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			fmt.Printf("[ERROR] Failed to close output file: %v\n", closeErr)
		}
	}()

	fmt.Printf("[DEBUG] Output file created successfully\n")

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{
		"Ticker",
		"StartDate",
		"EndDate",
		"Superperformance",
		"Drawdowns",
		"Continuation",
		"TotalGain",
		"DurationDays",
	}
	if err := writer.Write(header); err != nil {
		fmt.Printf("[ERROR] Failed to write CSV header: %v\n", err)
		return fmt.Errorf("failed to write CSV header: %v", err)
	}

	// Write data rows
	for _, result := range results {
		record := []string{
			result.Ticker,
			result.StartDate,
			result.EndDate,
			result.Superperformance,
			result.Drawdowns,
			result.Continuation,
			strconv.FormatFloat(result.TotalGain, 'f', 2, 64),
			strconv.Itoa(result.DurationDays),
		}
		if err := writer.Write(record); err != nil {
			fmt.Printf("[ERROR] Failed to write record for %s: %v\n", result.Ticker, err)
			return fmt.Errorf("failed to write record for %s: %v", result.Ticker, err)
		}
	}

	fmt.Printf("[INFO] Successfully saved %d results to %s\n", len(results), filename)
	return nil
}