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
	MARKETSTACK_API_URL = "https://api.marketstack.com/v2"
	MIN_VOLUME          = 200000
	MAX_CONCURRENT      = 5
	DAYS_LOOKBACK       = 9131 // 25 years of trading days
	MIN_GAIN_SHORT_TERM = 100.0  // 100% minimum for 64-252 days
	MIN_GAIN_LONG_TERM  = 150.0  // 150% minimum for 252-504 days
	MIN_DAYS_SHORT      = 64     // Minimum days for 100% moves
	MAX_DAYS_SHORT      = 252    // Maximum days for 100% moves
	MIN_DAYS_LONG       = 252    // Minimum days for 150% moves
	MAX_DAYS_LONG       = 504    // Maximum days for 150% moves
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
	Open          float64 `json:"open"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	Close         float64 `json:"close"`
	Volume        float64 `json:"volume"`
	AdjHigh       float64 `json:"adj_high"`
	AdjLow        float64 `json:"adj_low"`
	AdjClose      float64 `json:"adj_close"`
	AdjOpen       float64 `json:"adj_open"`
	AdjVolume     float64 `json:"adj_volume"`
	SplitFactor   float64 `json:"split_factor"`
	Dividend      float64 `json:"dividend"`
	Date          string  `json:"date"`
	Symbol        string  `json:"symbol"`
	Exchange      string  `json:"exchange"`
	Name          string  `json:"name"`
	ExchangeCode  string  `json:"exchange_code"`
	AssetType     string  `json:"asset_type"`
	PriceCurrency string  `json:"price_currency"`
}

// Core data structures
type GrowthMove struct {
	Ticker               string
	StartDate            time.Time
	EndDate              time.Time  // This should ALWAYS be the peak date for successful moves
	LOD                  float64
	PeakPrice            float64
	PeakDate             time.Time  // The actual date of highest price
	TotalGain            float64
	DurationDays         int        // Days from start to PEAK, not to decline
	IsGrowthStock        bool
	IsSuperperform       bool
	Drawdowns            []Drawdown
	Continuations        []Continuation
	Status               string
	EndReason            string
	DaysSinceHigh        int
	InDrawdown           bool
	DrawdownStart        time.Time
	DrawdownLow          float64
	PendingContinuation  bool
	ContinuationDeadline time.Time
	ContinuationLOD      float64
	ActualEndDate        time.Time  // When the move actually ended (for tracking)
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
	EndDate          string  // Should always be peak date for successful moves
	Superperformance string
	Drawdowns        string
	Continuation     string
	TotalGain        float64
	DurationDays     int     // Days from start to peak
}

// API Client
type MarketStackClient struct {
	APIKey     string
	HTTPClient *http.Client
	RateLimit  chan struct{}
}

func (c *MarketStackClient) GetStockData(symbol string, dateFrom, dateTo string, exchange string) ([]StockData, error) {
	fmt.Printf("[DEBUG] Starting API request for %s from %s to %s on exchange %s\n", symbol, dateFrom, dateTo, exchange)

	var allData []StockData
	offset := 0
	limit := 1000

	for {
		// Rate limiting: acquire token
		<-c.RateLimit
		fmt.Printf("[DEBUG] Rate limit token acquired for %s (offset: %d)\n", symbol, offset)

		url := fmt.Sprintf("%s/eod?access_key=%s&symbols=%s&date_from=%s&date_to=%s&limit=%d&offset=%d",
				MARKETSTACK_API_URL, c.APIKey, symbol, dateFrom, dateTo, limit, offset)

		// Build API URL with pagination
		if exchange != "" {
			url = fmt.Sprintf("%s/eod?access_key=%s&symbols=%s&date_from=%s&date_to=%s&limit=%d&offset=%d&exchange=%s",
				MARKETSTACK_API_URL, c.APIKey, symbol, dateFrom, dateTo, limit, offset, exchange)
		}

		fmt.Printf("[DEBUG] API URL constructed: %s\n", strings.Replace(url, c.APIKey, "***", -1))

		// Make HTTP request
		resp, err := c.HTTPClient.Get(url)
		if err != nil {
			// Release rate limit token
			c.RateLimit <- struct{}{}
			fmt.Printf("[ERROR] HTTP request failed for %s: %v\n", symbol, err)
			return nil, fmt.Errorf("API request failed: %v", err)
		}

		fmt.Printf("[DEBUG] HTTP response received for %s with status: %d\n", symbol, resp.StatusCode)

		// Check HTTP status
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			c.RateLimit <- struct{}{} // Release rate limit token

			fmt.Printf("[ERROR] API returned non-200 status for %s: %d, body: %s\n", symbol, resp.StatusCode, string(body))

			// Handle specific error cases
			if resp.StatusCode == 429 {
				return nil, fmt.Errorf("API rate limit exceeded for %s", symbol)
			}
			if resp.StatusCode == 404 {
				return nil, fmt.Errorf("symbol %s not found", symbol)
			}

			return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
		}

		// Decode JSON response
		var apiResp MarketStackResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
			resp.Body.Close()
			c.RateLimit <- struct{}{} // Release rate limit token
			fmt.Printf("[ERROR] Failed to decode JSON response for %s: %v\n", symbol, err)
			return nil, fmt.Errorf("failed to decode response: %v", err)
		}
		resp.Body.Close()

		// Release rate limit token after successful request
		c.RateLimit <- struct{}{}
		fmt.Printf("[DEBUG] Rate limit token released for %s\n", symbol)

		fmt.Printf("[DEBUG] Successfully decoded %d data points for %s (offset: %d)\n", len(apiResp.Data), symbol, offset)

		// Add data to our collection
		allData = append(allData, apiResp.Data...)

		// Check if we have more data to fetch
		if len(apiResp.Data) < limit {
			fmt.Printf("[DEBUG] Reached end of data for %s (got %d records, expected %d)\n", symbol, len(apiResp.Data), limit)
			break
		}

		// Check pagination info
		if apiResp.Pagination.Total > 0 && len(allData) >= apiResp.Pagination.Total {
			fmt.Printf("[DEBUG] Fetched all available data for %s (%d records)\n", symbol, len(allData))
			break
		}

		offset += limit
		fmt.Printf("[DEBUG] Fetching next batch for %s (new offset: %d)\n", symbol, offset)
	}

	// Validate data exists
	if len(allData) == 0 {
		fmt.Printf("[WARNING] No data returned for %s\n", symbol)
		return nil, fmt.Errorf("no data returned for symbol %s", symbol)
	}

	// Validate data quality - check for valid prices
	validData := []StockData{}
	for _, data := range allData {
		if data.High > 0 && data.Low > 0 && data.Close > 0 && data.Volume >= 0 {
			validData = append(validData, data)
		} else {
			fmt.Printf("[WARNING] Invalid data point for %s on %s: High=%.2f, Low=%.2f, Close=%.2f\n", 
				symbol, data.Date, data.High, data.Low, data.Close)
		}
	}

	if len(validData) == 0 {
		return nil, fmt.Errorf("no valid price data for symbol %s", symbol)
	}

	// Sort by date (oldest first)
	fmt.Printf("[DEBUG] Sorting %d valid data points by date for %s\n", len(validData), symbol)
	sort.Slice(validData, func(i, j int) bool {
		dateI := parseStockDate(validData[i].Date)
		dateJ := parseStockDate(validData[j].Date)
		if dateI.IsZero() || dateJ.IsZero() {
			return false
		}
		return dateI.Before(dateJ)
	})

	// Log date range of retrieved data
	if len(validData) > 0 {
		firstDate := parseStockDate(validData[0].Date)
		lastDate := parseStockDate(validData[len(validData)-1].Date)
		fmt.Printf("[INFO] Retrieved valid data for %s from %s to %s (%d days)\n",
			symbol, firstDate.Format("2006-01-02"), lastDate.Format("2006-01-02"), len(validData))
	}

	return validData, nil
}

// Helper function to parse various date formats from MarketStack
func parseStockDate(dateStr string) time.Time {
	formats := []string{
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05+0000",
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
func (c *MarketStackClient) AnalyzeStock(symbol string, exchange string) ([]Result, error) {
	fmt.Printf("[INFO] Starting analysis for %s\n", symbol)

	// Validate input
	if strings.TrimSpace(symbol) == "" {
		fmt.Printf("[ERROR] Empty symbol provided\n")
		return nil, fmt.Errorf("symbol cannot be empty")
	}

	// Get historical data - try different date ranges if 2000 fails
	dateTo := time.Now().Format("2006-01-02")

	// Try multiple start dates in case 2000 is too far back
	startDates := []string{
		"2000-01-01", // 25+ years
		"2005-01-01", // 20 years
		"2010-01-01", // 15 years
		"2015-01-01", // 10 years
	}

	var stockData []StockData
	var err error

	for _, dateFrom := range startDates {
		fmt.Printf("[DEBUG] Trying to fetch data for %s from %s to %s\n", symbol, dateFrom, dateTo)
		stockData, err = c.GetStockData(symbol, dateFrom, dateTo, exchange)
		if err == nil && len(stockData) >= 100 { // Increased minimum requirement
			fmt.Printf("[INFO] Successfully retrieved %d days of data for %s starting from %s\n",
				len(stockData), symbol, dateFrom)
			break
		}
		fmt.Printf("[WARNING] Failed to get sufficient data from %s: %v\n", dateFrom, err)
	}

	if err != nil || len(stockData) < 100 { // Increased minimum requirement
		return nil, fmt.Errorf("failed to get sufficient data for %s: %v", symbol, err)
	}

	// Validate stock existed during the time period with meaningful trading
	if !validateStockExistence(symbol, stockData) {
		return nil, fmt.Errorf("%s does not appear to have valid trading history", symbol)
	}

	// Check volume requirement
	fmt.Printf("[DEBUG] Checking volume requirements for %s\n", symbol)
	if !meetsVolumeRequirement(stockData) {
		fmt.Printf("[WARNING] %s does not meet volume requirement (min: %d)\n", symbol, MIN_VOLUME)
		return nil, fmt.Errorf("%s does not meet volume requirement", symbol)
	}
	fmt.Printf("[DEBUG] %s meets volume requirements\n", symbol)

	// Find distinct growth moves
	fmt.Printf("[DEBUG] Searching for distinct growth moves in %s\n", symbol)
	results := findDistinctGrowthMoves(symbol, stockData)
	fmt.Printf("[INFO] Found %d distinct growth moves for %s\n", len(results), symbol)

	return results, nil
}

// Add this near the top with other imports and constants
var knownIPODates = map[string]string{
	// Add known problematic cases that shouldn't have early data
	"POWW": "2004-01-01", // Example - American Outdoor Brands went public around 2004
	// Add more as needed
}

func validateStockExistence(symbol string, data []StockData) bool {
	if len(data) < 100 {
		return false
	}

	// Check against known IPO dates if available
	if ipoDateStr, exists := knownIPODates[symbol]; exists {
		ipoDate, err := time.Parse("2006-01-02", ipoDateStr)
		if err == nil {
			// Find earliest data point
			var earliestDate time.Time
			for _, day := range data {
				dayDate := parseStockDate(day.Date)
				if !dayDate.IsZero() && (earliestDate.IsZero() || dayDate.Before(earliestDate)) {
					earliestDate = dayDate
				}
			}
			
			if !earliestDate.IsZero() && earliestDate.Before(ipoDate) {
				fmt.Printf("[WARNING] %s has data before known IPO date: data from %s, IPO ~%s\n", 
					symbol, earliestDate.Format("2006-01-02"), ipoDate.Format("2006-01-02"))
				return false
			}
		}
	}

	// Check for reasonable price ranges (not just $0.0001 prices)
	totalPrice := 0.0
	validPrices := 0
	firstValidDate := time.Time{}
	
	for _, day := range data {
		if day.Close >= 0.01 { // At least 1 cent
			totalPrice += day.Close
			validPrices++
			
			// Track first valid trading date
			dayDate := parseStockDate(day.Date)
			if !dayDate.IsZero() && (firstValidDate.IsZero() || dayDate.Before(firstValidDate)) {
				firstValidDate = dayDate
			}
		}
	}

	if validPrices < len(data)/2 { // At least 50% of days should have reasonable prices
		fmt.Printf("[WARNING] %s has too many invalid price points\n", symbol)
		return false
	}

	avgPrice := totalPrice / float64(validPrices)
	if avgPrice < 0.01 { // Average price should be at least 1 cent
		fmt.Printf("[WARNING] %s average price too low: %.4f\n", symbol, avgPrice)
		return false
	}

	// Check for suspicious early trading patterns that might indicate pre-IPO data
	if !firstValidDate.IsZero() {
		// Look for signs of legitimate trading activity vs data artifacts
		validTradingDays := 0
		significantVolumedays := 0
		
		for _, day := range data {
			dayDate := parseStockDate(day.Date)
			if dayDate.IsZero() || day.Close < 0.01 {
				continue
			}
			
			validTradingDays++
			
			// Check for meaningful volume (indicates real trading)
			if day.Volume > 10000 { // More than 10k shares traded
				significantVolumedays++
			}
		}
		
		// If less than 25% of days have significant volume, might be pre-IPO or invalid data
		volumeRatio := float64(significantVolumedays) / float64(validTradingDays)
		if volumeRatio < 0.25 {
			fmt.Printf("[WARNING] %s has insufficient trading volume history (%.1f%% meaningful volume days)\n", 
				symbol, volumeRatio*100)
			return false
		}
		
		// Check for realistic price progression - IPO stocks typically don't start at pennies
		// unless they're penny stocks or have had significant splits
		firstYearData := []StockData{}
		oneYearLater := firstValidDate.AddDate(1, 0, 0)
		
		for _, day := range data {
			dayDate := parseStockDate(day.Date)
			if !dayDate.IsZero() && dayDate.After(firstValidDate) && dayDate.Before(oneYearLater) {
				firstYearData = append(firstYearData, day)
			}
		}
		
		if len(firstYearData) > 50 { // At least ~2 months of data in first year
			firstYearAvgPrice := 0.0
			for _, day := range firstYearData {
				if day.Close >= 0.01 {
					firstYearAvgPrice += day.Close
				}
			}
			firstYearAvgPrice /= float64(len(firstYearData))
			
			// If average price in first year is extremely low (< $0.10) with low volume,
			// this might be pre-IPO or invalid data
			if firstYearAvgPrice < 0.10 && volumeRatio < 0.5 {
				fmt.Printf("[WARNING] %s shows suspicious early trading pattern (avg price: $%.4f, volume ratio: %.1f%%)\n", 
					symbol, firstYearAvgPrice, volumeRatio*100)
				return false
			}
		}
	}

	fmt.Printf("[INFO] %s validated: First trading date ~%s, avg price: $%.2f\n", 
		symbol, firstValidDate.Format("2006-01-02"), avgPrice)
	
	return true
}

func meetsVolumeRequirement(data []StockData) bool {
	if len(data) < 20 {
		return false
	}

	// Calculate average volume over last 20 days
	totalVolume := 0.0
	count := 0

	for i := len(data) - 20; i < len(data); i++ {
		if data[i].Volume > 0 {
			totalVolume += data[i].Volume
			count++
		}
	}

	if count == 0 {
		return false
	}

	avgVolume := totalVolume / float64(count)
	fmt.Printf("[DEBUG] Average volume: %.0f, required: %d\n", avgVolume, MIN_VOLUME)

	return avgVolume >= MIN_VOLUME
}

func findDistinctGrowthMoves(symbol string, data []StockData) []Result {
	fmt.Printf("[DEBUG] Starting distinct growth move detection for %s with %d data points\n", symbol, len(data))

	var results []Result
	var activeMove *GrowthMove
	lastMoveEndIndex := -1

	for i := 0; i < len(data); i++ {
		currentDay := data[i]
		currentDate := parseStockDate(currentDay.Date)
		if currentDate.IsZero() {
			continue
		}

		// Process active move if one exists
		if activeMove != nil {
			endMove := processActiveMove(activeMove, currentDay, currentDate, i)
			if endMove {
				if result := finalizeGrowthMove(activeMove); result != nil {
					results = append(results, *result)
				}
				activeMove = nil
				lastMoveEndIndex = i
			}
			continue
		}

		// Look for new growth start (with sufficient gap from last move)
		if activeMove == nil && i > lastMoveEndIndex+30 {
			if newMove := checkForGrowthStart(symbol, data, i); newMove != nil {
				activeMove = newMove
				fmt.Printf("[INFO] New growth move started for %s on %s (LOD: %.2f)\n",
					symbol, currentDate.Format("2006-01-02"), newMove.LOD)
			}
		}
	}

	// Handle any remaining active move at end of data
	if activeMove != nil {
		if len(data) > 0 {
			lastDay := data[len(data)-1]
			lastDate := parseStockDate(lastDay.Date)
			if !lastDate.IsZero() {
				// CRITICAL: For end of data, use peak date as end date
				activeMove.EndDate = activeMove.PeakDate
				activeMove.ActualEndDate = lastDate
				activeMove.EndReason = "End of data"
			}
		}

		if result := finalizeGrowthMove(activeMove); result != nil {
			results = append(results, *result)
		}
	}

	// Sort results by start date
	sort.Slice(results, func(i, j int) bool {
		dateI, _ := time.Parse("Jan 2, 2006", results[i].StartDate)
		dateJ, _ := time.Parse("Jan 2, 2006", results[j].StartDate)
		return dateI.Before(dateJ)
	})

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

	// Ensure the potential LOD is reasonable (not a data artifact)
	if potentialLOD <= 0.001 {
		return nil
	}

	// Check for 5% confirmation within 5 days
	confirmationFound := false
	var confirmationHigh float64
	var confirmationDate time.Time

	for i := 0; i <= 5 && startIndex+i < len(data); i++ {
		checkDay := data[startIndex+i]
		checkDate := parseStockDate(checkDay.Date)
		if checkDate.IsZero() {
			continue
		}

		// Check if LOD is broken (with small tolerance for data noise)
		if checkDay.Low < potentialLOD*0.995 {
			return nil
		}

		// Check for 5% gain confirmation
		gain := (checkDay.High - potentialLOD) / potentialLOD
		if gain >= 0.05 {
			confirmationFound = true
			confirmationHigh = checkDay.High
			confirmationDate = checkDate
			break
		}
	}

	if !confirmationFound {
		return nil
	}

	return &GrowthMove{
		Ticker:        symbol,
		StartDate:     startDate,
		LOD:           potentialLOD,
		PeakPrice:     confirmationHigh,
		PeakDate:      confirmationDate,
		EndDate:       confirmationDate, // Initially set to confirmation date
		Status:        "ACTIVE",
		Drawdowns:     []Drawdown{},
		Continuations: []Continuation{},
	}
}

func processActiveMove(move *GrowthMove, currentDay StockData, currentDate time.Time, dataIndex int) bool {
	// Handle pending continuation check first
	if move.PendingContinuation {
		if currentDay.High > move.PeakPrice {
			// Continuation successful
			move.Continuations = append(move.Continuations, Continuation{
				OldPeak:          move.PeakPrice,
				ContinuationDate: currentDate,
				NewLOD:           move.ContinuationLOD,
			})

			move.LOD = move.ContinuationLOD
			move.PeakPrice = currentDay.High
			move.PeakDate = currentDate
			move.EndDate = currentDate // Update end date to new peak
			move.DaysSinceHigh = 0
			move.PendingContinuation = false
			move.Status = "ACTIVE"

			fmt.Printf("[DEBUG] Continuation successful for %s: New peak %.2f on %s\n",
				move.Ticker, currentDay.High, currentDate.Format("2006-01-02"))

		} else if currentDate.After(move.ContinuationDeadline) {
			// Continuation failed - end date should remain as the previous peak date
			move.ActualEndDate = currentDate
			move.EndReason = "Continuation failed"
			return true
		}
		return false
	}

	// Update peak if new high
	if currentDay.High > move.PeakPrice {
		move.PeakPrice = currentDay.High
		move.PeakDate = currentDate
		move.EndDate = currentDate // CRITICAL: Always update end date to peak date
		move.DaysSinceHigh = 0

		fmt.Printf("[DEBUG] New peak for %s: %.2f on %s\n",
			move.Ticker, currentDay.High, currentDate.Format("2006-01-02"))
	} else {
		move.DaysSinceHigh++
	}

	// Check end conditions
	endReason := checkEndConditions(move, currentDay, currentDate)
	if endReason != "" {
		if endReason == "30% decline" {
			// Set up continuation tracking
			move.PendingContinuation = true
			move.ContinuationDeadline = move.PeakDate.AddDate(0, 0, 90)
			move.ContinuationLOD = currentDay.Low
			move.Status = "PENDING_CONTINUATION"
			// EndDate stays as peak date
			fmt.Printf("[DEBUG] 30%% decline for %s, setting up continuation tracking\n", move.Ticker)
			return false
		}

		// For all other end reasons, end date should be peak date
		// ActualEndDate tracks when the condition was triggered
		move.ActualEndDate = currentDate
		move.EndReason = endReason
		return true
	}

	// Check for drawdowns
	checkDrawdownConditions(move, currentDay, currentDate)
	return false
}

func checkEndConditions(move *GrowthMove, currentDay StockData, currentDate time.Time) string {
	// 30% drop from peak
	declineFromPeak := (move.PeakPrice - currentDay.Low) / move.PeakPrice
	if declineFromPeak >= 0.30 {
		return "30% decline"
	}

	// Break below LOD (with small tolerance for data noise)
	if currentDay.Low < move.LOD*0.995 {
		return "LOD broken"
	}

	// No new high in 30 days
	if move.DaysSinceHigh >= 30 {
		return "No new high in 30 days"
	}

	// Maximum time exceeded (504 days from start to current)
	totalDays := int(currentDate.Sub(move.StartDate).Hours() / 24)
	if totalDays >= 504 {
		return "Maximum time exceeded"
	}

	return ""
}

func checkDrawdownConditions(move *GrowthMove, currentDay StockData, currentDate time.Time) {
	declineFromPeak := (move.PeakPrice - currentDay.Low) / move.PeakPrice

	// Check if in drawdown range (15-29.9%)
	if declineFromPeak >= 0.15 && declineFromPeak < 0.30 {
		if !move.InDrawdown {
			move.InDrawdown = true
			move.DrawdownStart = currentDate
			move.DrawdownLow = currentDay.Low
		} else {
			if currentDay.Low < move.DrawdownLow {
				move.DrawdownLow = currentDay.Low
			}
		}
	} else if move.InDrawdown && declineFromPeak < 0.15 {
		// Drawdown recovery
		move.Drawdowns = append(move.Drawdowns, Drawdown{
			StartDate: move.DrawdownStart,
			EndDate:   currentDate,
			LowPrice:  move.DrawdownLow,
		})

		move.LOD = move.DrawdownLow
		move.InDrawdown = false
	}
}

func finalizeGrowthMove(move *GrowthMove) *Result {
	fmt.Printf("[DEBUG] Finalizing growth move for %s\n", move.Ticker)

	// CRITICAL FIX: End date should ALWAYS be the peak date for successful moves
	// This ensures we measure the successful portion, not the decline
	if move.EndDate.IsZero() || move.EndDate.After(move.PeakDate) {
		move.EndDate = move.PeakDate
		fmt.Printf("[DEBUG] Corrected end date for %s to peak date %s\n",
			move.Ticker, move.PeakDate.Format("2006-01-02"))
	}

	// CRITICAL FIX: Duration should be from start to PEAK, not to decline
	totalDays := int(move.PeakDate.Sub(move.StartDate).Hours() / 24)
	if totalDays <= 0 {
		fmt.Printf("[ERROR] Invalid duration calculated for %s: %d days\n", move.Ticker, totalDays)
		return nil
	}

	// Calculate gain from LOD to peak (the successful portion)
	totalGainPercent := (move.PeakPrice - move.LOD) / move.LOD * 100
	move.TotalGain = totalGainPercent
	move.DurationDays = totalDays

	fmt.Printf("[DEBUG] %s Move Stats: %.2f%% gain over %d days (Start: %s -> Peak: %s)\n",
		move.Ticker, totalGainPercent, totalDays,
		move.StartDate.Format("2006-01-02"),
		move.PeakDate.Format("2006-01-02"))

	// CRITICAL GROWTH STOCK CRITERIA CHECK
	move.IsGrowthStock = false

	if totalDays >= MIN_DAYS_SHORT && totalDays <= MAX_DAYS_SHORT {
		// Short-term criteria: 100%+ gain in 64-252 days
		if totalGainPercent >= MIN_GAIN_SHORT_TERM {
			move.IsGrowthStock = true
			fmt.Printf("[INFO] ✓ %s qualifies as GROWTH STOCK (Short-term): %.2f%% in %d days\n",
				move.Ticker, totalGainPercent, totalDays)
		} else {
			fmt.Printf("[DEBUG] ✗ %s short-term move insufficient: %.2f%% < %.1f%% required\n",
				move.Ticker, totalGainPercent, MIN_GAIN_SHORT_TERM)
		}
	} else if totalDays >= MIN_DAYS_LONG && totalDays <= MAX_DAYS_LONG {
		// Long-term criteria: 150%+ gain in 252-504 days
		if totalGainPercent >= MIN_GAIN_LONG_TERM {
			move.IsGrowthStock = true
			fmt.Printf("[INFO] ✓ %s qualifies as GROWTH STOCK (Long-term): %.2f%% in %d days\n",
				move.Ticker, totalGainPercent, totalDays)
		} else {
			fmt.Printf("[DEBUG] ✗ %s long-term move insufficient: %.2f%% < %.1f%% required\n",
				move.Ticker, totalGainPercent, MIN_GAIN_LONG_TERM)
		}
	} else {
		fmt.Printf("[DEBUG] ✗ %s duration outside criteria: %d days (need %d-%d or %d-%d)\n",
			move.Ticker, totalDays, MIN_DAYS_SHORT, MAX_DAYS_SHORT, MIN_DAYS_LONG, MAX_DAYS_LONG)
	}

	// Check superperformance (300%+ short-term, 500%+ long-term)
	move.IsSuperperform = false
	if totalDays >= MIN_DAYS_SHORT && totalDays <= MAX_DAYS_SHORT && totalGainPercent >= 300 {
		move.IsSuperperform = true
		fmt.Printf("[INFO] 🚀 %s is a SHORT-TERM SUPERPERFORMER! %.2f%% in %d days\n",
			move.Ticker, totalGainPercent, totalDays)
	} else if totalDays >= MIN_DAYS_LONG && totalDays <= MAX_DAYS_LONG && totalGainPercent >= 500 {
		move.IsSuperperform = true
		fmt.Printf("[INFO] 🚀 %s is a LONG-TERM SUPERPERFORMER! %.2f%% in %d days\n",
			move.Ticker, totalGainPercent, totalDays)
	}

	// ONLY RETURN MOVES THAT MEET THE CRITICAL GROWTH CRITERIA
	if !move.IsGrowthStock {
		fmt.Printf("[INFO] Excluding %s from results - does not meet growth stock criteria\n", move.Ticker)
		return nil
	}

	// Format output strings
	drawdownStr := "none"
	if len(move.Drawdowns) > 0 {
		var drawdownDates []string
		for _, d := range move.Drawdowns {
			drawdownDates = append(drawdownDates, d.StartDate.Format("Jan 2, 2006"))
		}
		drawdownStr = strings.Join(drawdownDates, "; ")
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
		EndDate:          move.EndDate.Format("Jan 2, 2006"), // This is now always the peak date
		Superperformance: superperformStr,
		Drawdowns:        drawdownStr,
		Continuation:     continuationStr,
		TotalGain:        totalGainPercent,
		DurationDays:     totalDays,
	}

	fmt.Printf("[INFO] ✓ GROWTH STOCK RESULT: %s gained %.2f%% over %d days (%s to %s) - End reason: %s\n",
		result.Ticker, result.TotalGain, result.DurationDays,
		result.StartDate, result.EndDate, move.EndReason)

	return result
}

// Utility functions remain the same
func LoadStocksFromCSV(filename string) ([]string, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil, fmt.Errorf("CSV file does not exist: %s", filename)
	}

	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open CSV file: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %v", err)
	}

	var symbols []string
	for i, record := range records {
		if i == 0 || len(record) == 0 || record[0] == "" {
			continue
		}
		symbol := strings.TrimSpace(strings.ToUpper(record[0]))
		symbols = append(symbols, symbol)
	}

	return symbols, nil
}

func SaveResultsToJSON(results []Result, filename string) error {
	if err := os.MkdirAll("superperformance_data", 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(results); err != nil {
		return fmt.Errorf("failed to encode JSON: %v", err)
	}

	return nil
}

func SaveResultsToCSV(results []Result, filename string) error {
	if err := os.MkdirAll("superperformance_data", 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer file.Close()

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
			return fmt.Errorf("failed to write record for %s: %v", result.Ticker, err)
		}
	}

	return nil
}