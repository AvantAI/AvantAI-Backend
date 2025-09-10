package superperformance

// import (
// 	"encoding/json"
// 	"fmt"
// 	"math"
// 	"os"
// 	"sort"
// 	"strings"
// 	"time"
// )

// // Growth tracking structures
// type GrowthMove struct {
// 	StartDate        time.Time  `json:"start_date"`
// 	EndDate          *time.Time `json:"end_date,omitempty"`
// 	LOD              float64    `json:"lod"`
// 	LODDate          time.Time  `json:"lod_date"`
// 	HighPrice        float64    `json:"high_price"`
// 	HighPriceDate    time.Time  `json:"high_price_date"`
// 	CurrentPrice     float64    `json:"current_price"`
// 	TradingDays      int        `json:"trading_days"`
// 	GrowthPercentage float64    `json:"growth_percentage"`
// 	Status           string     `json:"status"` // ACTIVE, ENDED, DRAWDOWN
// 	EndReason        string     `json:"end_reason,omitempty"`
// 	IsConfirmed      bool       `json:"is_confirmed"`
// 	ConfirmationDate *time.Time `json:"confirmation_date,omitempty"`
// 	LastNewHighDate  time.Time  `json:"last_new_high_date"`
// 	DaysSinceNewHigh int        `json:"days_since_new_high"`
// }

// type Drawdown struct {
// 	StartDate       time.Time  `json:"start_date"`
// 	EndDate         *time.Time `json:"end_date,omitempty"`
// 	HighPrice       float64    `json:"high_price"`
// 	LowPrice        float64    `json:"low_price"`
// 	LowPriceDate    time.Time  `json:"low_price_date"`
// 	DrawdownPercent float64    `json:"drawdown_percent"`
// 	Status          string     `json:"status"` // ACTIVE, RECOVERED
// 	NewLOD          float64    `json:"new_lod,omitempty"`
// 	NewLODDate      *time.Time `json:"new_lod_date,omitempty"`
// }

// type GrowthStock struct {
// 	Ticker         string  `json:"ticker"`
// 	CurrentPrice   float64 `json:"current_price"`
// 	AvgVolume20Day float64 `json:"avg_volume_20day"`
// 	CurrentVolume  float64 `json:"current_volume"`

// 	// Current Growth Move
// 	CurrentGrowth *GrowthMove `json:"current_growth,omitempty"`

// 	// Growth Classification
// 	IsGrowthStock      bool   `json:"is_growth_stock"`
// 	IsSuperperformance bool   `json:"is_superperformance"`
// 	GrowthCategory     string `json:"growth_category"` // GROWTH_100, GROWTH_150, SUPER_300, SUPER_500

// 	// Drawdown Information
// 	CurrentDrawdown   *Drawdown `json:"current_drawdown,omitempty"`
// 	HasActiveDrawdown bool      `json:"has_active_drawdown"`

// 	// Historical Performance
// 	AllTimeHigh     float64   `json:"all_time_high"`
// 	AllTimeHighDate time.Time `json:"all_time_high_date"`

// 	// Screening Metrics
// 	QualifiesForScreen bool    `json:"qualifies_for_screen"`
// 	ScreenReason       string  `json:"screen_reason"`
// 	AlertLevel         string  `json:"alert_level"`
// 	Score              float64 `json:"score"`
// }

// type GrowthScreenerConfig struct {
// 	MinAvgVolume        float64 `json:"min_avg_volume"`
// 	RequireActiveGrowth bool    `json:"require_active_growth"`
// 	MinGrowthPercent    float64 `json:"min_growth_percent"`
// 	MaxTradingDays      int     `json:"max_trading_days"`
// }

// // Main screening function for growth stocks
// func (c *MarketStackClient) ScreenForGrowthStocks(symbols []string) ([]GrowthStock, error) {
// 	fmt.Printf("[INFO] Starting growth stock screening for %d symbols\n", len(symbols))

// 	config := GrowthScreenerConfig{
// 		MinAvgVolume:        200000, // 200k average volume
// 		RequireActiveGrowth: true,   // Only active growth moves
// 		MinGrowthPercent:    100,    // Minimum 100% growth
// 		MaxTradingDays:      504,    // Maximum 504 trading days
// 	}

// 	var growthStocks []GrowthStock
// 	processed := 0
// 	errors := 0

// 	for _, symbol := range symbols {
// 		fmt.Printf("[DEBUG] Analyzing growth for %s\n", symbol)

// 		growthStock, err := c.analyzeGrowthStock(symbol, config)
// 		if err != nil {
// 			fmt.Printf("[ERROR] Failed to analyze %s: %v\n", symbol, err)
// 			errors++
// 			continue
// 		}

// 		if growthStock != nil && growthStock.QualifiesForScreen {
// 			growthStocks = append(growthStocks, *growthStock)
// 			fmt.Printf("[GROWTH] Found %s: %s (%.1f%% in %d days)\n",
// 				growthStock.GrowthCategory, growthStock.Ticker,
// 				growthStock.CurrentGrowth.GrowthPercentage,
// 				growthStock.CurrentGrowth.TradingDays)
// 		}

// 		processed++
// 		if processed%25 == 0 {
// 			fmt.Printf("[PROGRESS] Analyzed %d/%d symbols (%d growth stocks, %d errors)\n",
// 				processed, len(symbols), len(growthStocks), errors)
// 		}

// 		// Rate limiting
// 		time.Sleep(100 * time.Millisecond)
// 	}

// 	// Sort by growth percentage (highest first)
// 	sort.Slice(growthStocks, func(i, j int) bool {
// 		if growthStocks[i].IsSuperperformance != growthStocks[j].IsSuperperformance {
// 			return growthStocks[i].IsSuperperformance
// 		}
// 		return growthStocks[i].CurrentGrowth.GrowthPercentage > growthStocks[j].CurrentGrowth.GrowthPercentage
// 	})

// 	fmt.Printf("[INFO] Growth screening complete: %d qualifying stocks found\n", len(growthStocks))
// 	return growthStocks, nil
// }

// func (c *MarketStackClient) analyzeGrowthStock(symbol string, config GrowthScreenerConfig) (*GrowthStock, error) {
// 	// Get extended historical data (2+ years for proper growth tracking)
// 	dateTo := time.Now().Format("2006-01-02")
// 	dateFrom := time.Now().AddDate(-2, 0, 0).Format("2006-01-02") // 2 years back

// 	data, err := c.GetStockData(symbol, dateFrom, dateTo)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get data: %v", err)
// 	}

// 	if len(data) < 100 {
// 		return nil, fmt.Errorf("insufficient historical data")
// 	}

// 	// Check volume requirement first
// 	avgVolume := calculateAverageVolume(data, 20)
// 	if avgVolume < config.MinAvgVolume {
// 		return nil, nil // Doesn't meet volume requirement
// 	}

// 	currentData := data[len(data)-1]

// 	// Find and analyze growth moves
// 	growthMove := c.findCurrentGrowthMove(data)
// 	if growthMove == nil {
// 		return nil, nil // No active growth move
// 	}

// 	// Check if growth move qualifies
// 	if !c.qualifiesAsGrowthStock(growthMove) {
// 		return nil, nil
// 	}

// 	// Analyze drawdowns
// 	drawdown := c.analyzeCurrentDrawdown(data, growthMove)

// 	// Determine classifications
// 	isGrowth, isSuperperformance, category := c.classifyGrowthStock(growthMove)

// 	// Calculate screening score
// 	score := c.calculateGrowthScore(growthMove, avgVolume, currentData.Volume)

// 	// Determine alert level
// 	alertLevel := c.getGrowthAlertLevel(growthMove, isSuperperformance, drawdown)

// 	growthStock := &GrowthStock{
// 		Ticker:             symbol,
// 		CurrentPrice:       currentData.Close,
// 		AvgVolume20Day:     avgVolume,
// 		CurrentVolume:      currentData.Volume,
// 		CurrentGrowth:      growthMove,
// 		IsGrowthStock:      isGrowth,
// 		IsSuperperformance: isSuperperformance,
// 		GrowthCategory:     category,
// 		CurrentDrawdown:    drawdown,
// 		HasActiveDrawdown:  drawdown != nil && drawdown.Status == "ACTIVE",
// 		AllTimeHigh:        growthMove.HighPrice,
// 		AllTimeHighDate:    growthMove.HighPriceDate,
// 		QualifiesForScreen: true,
// 		ScreenReason:       c.getScreenReason(growthMove, isSuperperformance),
// 		AlertLevel:         alertLevel,
// 		Score:              score,
// 	}

// 	return growthStock, nil
// }

// func (c *MarketStackClient) findCurrentGrowthMove(data []StockData) *GrowthMove {
// 	// Look for the most recent growth move that could still be active
// 	// Start from the end and work backwards

// 	// currentPrice := data[len(data)-1].Close

// 	// Find potential LOD candidates (look back up to 504 trading days)
// 	maxLookback := 504
// 	if len(data) < maxLookback {
// 		maxLookback = len(data)
// 	}

// 	startIndex := len(data) - maxLookback

// 	for i := startIndex; i < len(data)-5; i++ { // Need at least 5 days for confirmation
// 		potentialLOD := data[i].Low
// 		lodDate, _ := time.Parse("2006-01-02T15:04:05-0700", data[i].Date)

// 		// Check if this could be start of a growth move
// 		growthMove := c.analyzeGrowthFromLOD(data, i, potentialLOD, lodDate)
// 		if growthMove != nil && growthMove.Status == "ACTIVE" {
// 			return growthMove
// 		}
// 	}

// 	return nil
// }

// func (c *MarketStackClient) analyzeGrowthFromLOD(data []StockData, lodIndex int, lod float64, lodDate time.Time) *GrowthMove {
// 	if lodIndex >= len(data)-5 {
// 		return nil // Need at least 5 days after LOD
// 	}

// 	// Track the growth move from this LOD
// 	highPrice := lod
// 	highPriceDate := lodDate
// 	currentLOD := lod
// 	currentLODDate := lodDate
// 	isConfirmed := false
// 	var confirmationDate *time.Time
// 	lastNewHighDate := lodDate

// 	// Analyze day by day from LOD
// 	for i := lodIndex; i < len(data); i++ {
// 		currentData := data[i]
// 		currentDate, _ := time.Parse("2006-01-02T15:04:05-0700", currentData.Date)

// 		// Check for new highs
// 		if currentData.High > highPrice {
// 			highPrice = currentData.High
// 			highPriceDate = currentDate
// 			lastNewHighDate = currentDate
// 		}

// 		// Check if LOD is broken (end condition)
// 		if currentData.Low < currentLOD {
// 			// Check if this is a 30%+ drop that might continue as growth
// 			dropPercent := (highPrice - currentData.Low) / highPrice * 100
// 			if dropPercent >= 30 {
// 				// This could be a continuation if new high within 90 days
// 				newHighFound := false
// 				for j := i; j < len(data) && j < i+90; j++ {
// 					if data[j].High > highPrice {
// 						// New high found, update LOD and continue
// 						currentLOD = currentData.Low
// 						currentLODDate = currentDate
// 						newHighFound = true
// 						break
// 					}
// 				}
// 				if !newHighFound {
// 					// Growth ended
// 					break
// 				}
// 			} else {
// 				// LOD broken without 30% drop = end of growth
// 				break
// 			}
// 		}

// 		// Check for 5% confirmation within 5 days
// 		if !isConfirmed && i <= lodIndex+5 {
// 			gainPercent := (currentData.Close - lod) / lod * 100
// 			if gainPercent >= 5 {
// 				isConfirmed = true
// 				confirmationDate = &currentDate
// 			}
// 		}

// 		// Check for 30% reduction (drawdown/end condition)
// 		if highPrice > 0 {
// 			reductionPercent := (highPrice - currentData.Close) / highPrice * 100
// 			if reductionPercent >= 30 {
// 				// Check if new high within 90 days
// 				newHighFound := false
// 				for j := i; j < len(data) && j < i+90; j++ {
// 					if data[j].High > highPrice {
// 						newHighFound = true
// 						break
// 					}
// 				}
// 				if !newHighFound {
// 					// Growth ended
// 					endDate := currentDate
// 					return &GrowthMove{
// 						StartDate:        lodDate,
// 						EndDate:          &endDate,
// 						LOD:              currentLOD,
// 						LODDate:          currentLODDate,
// 						HighPrice:        highPrice,
// 						HighPriceDate:    highPriceDate,
// 						CurrentPrice:     currentData.Close,
// 						TradingDays:      i - lodIndex + 1,
// 						GrowthPercentage: (highPrice - lod) / lod * 100,
// 						Status:           "ENDED",
// 						EndReason:        "30% REDUCTION",
// 						IsConfirmed:      isConfirmed,
// 						ConfirmationDate: confirmationDate,
// 						LastNewHighDate:  lastNewHighDate,
// 						DaysSinceNewHigh: c.calculateTradingDaysBetween(lastNewHighDate, currentDate),
// 					}
// 				}
// 			}
// 		}
// 	}

// 	// If we get here, check current status
// 	currentData := data[len(data)-1]
// 	currentDate, _ := time.Parse("2006-01-02T15:04:05-0700", currentData.Date)
// 	tradingDays := len(data) - lodIndex
// 	daysSinceNewHigh := c.calculateTradingDaysBetween(lastNewHighDate, currentDate)

// 	// Check end conditions
// 	status := "ACTIVE"
// 	var endReason string
// 	var endDate *time.Time

// 	// 504 trading days limit
// 	if tradingDays > 504 {
// 		status = "ENDED"
// 		endReason = "TIME LIMIT (504 DAYS)"
// 		endDate = &currentDate
// 	}

// 	// 30 days without new high
// 	if daysSinceNewHigh > 30 {
// 		status = "ENDED"
// 		endReason = "NO NEW HIGH (30 DAYS)"
// 		endDate = &currentDate
// 	}

// 	// Must be confirmed to be valid
// 	if !isConfirmed {
// 		return nil
// 	}

// 	return &GrowthMove{
// 		StartDate:        lodDate,
// 		EndDate:          endDate,
// 		LOD:              currentLOD,
// 		LODDate:          currentLODDate,
// 		HighPrice:        highPrice,
// 		HighPriceDate:    highPriceDate,
// 		CurrentPrice:     currentData.Close,
// 		TradingDays:      tradingDays,
// 		GrowthPercentage: (highPrice - lod) / lod * 100,
// 		Status:           status,
// 		EndReason:        endReason,
// 		IsConfirmed:      isConfirmed,
// 		ConfirmationDate: confirmationDate,
// 		LastNewHighDate:  lastNewHighDate,
// 		DaysSinceNewHigh: daysSinceNewHigh,
// 	}
// }

// func (c *MarketStackClient) analyzeCurrentDrawdown(data []StockData, growth *GrowthMove) *Drawdown {
// 	if growth == nil || len(data) == 0 {
// 		return nil
// 	}

// 	currentPrice := data[len(data)-1].Close
// 	reductionPercent := (growth.HighPrice - currentPrice) / growth.HighPrice * 100

// 	// Check if currently in drawdown (15-29.9%)
// 	if reductionPercent >= 15 && reductionPercent < 30 {
// 		// Find the lowest point of this drawdown
// 		lowPrice := currentPrice
// 		lowPriceDate, _ := time.Parse("2006-01-02T15:04:05-0700", data[len(data)-1].Date)

// 		// Look back from high to find lowest point
// 		for i := len(data) - 1; i >= 0; i-- {
// 			if data[i].High >= growth.HighPrice {
// 				// Found the high point, now track forward for lowest
// 				for j := i; j < len(data); j++ {
// 					if data[j].Low < lowPrice {
// 						lowPrice = data[j].Low
// 						lowPriceDate, _ = time.Parse("2006-01-02T15:04:05-0700", data[j].Date)
// 					}
// 				}
// 				break
// 			}
// 		}

// 		return &Drawdown{
// 			StartDate:       growth.HighPriceDate,
// 			HighPrice:       growth.HighPrice,
// 			LowPrice:        lowPrice,
// 			LowPriceDate:    lowPriceDate,
// 			DrawdownPercent: reductionPercent,
// 			Status:          "ACTIVE",
// 		}
// 	}

// 	return nil
// }

// func (c *MarketStackClient) qualifiesAsGrowthStock(growth *GrowthMove) bool {
// 	if growth == nil || !growth.IsConfirmed {
// 		return false
// 	}

// 	// Check growth percentage and time requirements
// 	if growth.TradingDays >= 64 && growth.TradingDays <= 252 {
// 		return growth.GrowthPercentage >= 100 // 100%+ in 64-252 days
// 	}

// 	if growth.TradingDays >= 252 && growth.TradingDays <= 504 {
// 		return growth.GrowthPercentage >= 150 // 150%+ in 252-504 days
// 	}

// 	return false
// }

// func (c *MarketStackClient) classifyGrowthStock(growth *GrowthMove) (bool, bool, string) {
// 	if growth == nil {
// 		return false, false, ""
// 	}

// 	isGrowth := c.qualifiesAsGrowthStock(growth)
// 	if !isGrowth {
// 		return false, false, ""
// 	}

// 	// Check for superperformance
// 	if growth.TradingDays >= 64 && growth.TradingDays <= 252 {
// 		if growth.GrowthPercentage >= 300 {
// 			return true, true, "SUPER_300"
// 		}
// 		return true, false, "GROWTH_100"
// 	}

// 	if growth.TradingDays >= 252 && growth.TradingDays <= 504 {
// 		if growth.GrowthPercentage >= 500 {
// 			return true, true, "SUPER_500"
// 		}
// 		return true, false, "GROWTH_150"
// 	}

// 	return false, false, ""
// }

// func (c *MarketStackClient) calculateTradingDaysBetween(start, end time.Time) int {
// 	days := int(end.Sub(start).Hours() / 24)
// 	// Simple approximation: remove weekends (about 2/7 of days)
// 	return int(float64(days) * 5.0 / 7.0)
// }

// func (c *MarketStackClient) calculateGrowthScore(growth *GrowthMove, avgVol, currentVol float64) float64 {
// 	if growth == nil {
// 		return 0
// 	}

// 	score := 0.0

// 	// Growth percentage component (0-4 points)
// 	score += math.Min(growth.GrowthPercentage/100, 4.0)

// 	// Time efficiency bonus (0-2 points, better for shorter timeframes)
// 	timeScore := math.Max(0, 2.0-(float64(growth.TradingDays)/252.0))
// 	score += timeScore

// 	// Volume component (0-2 points)
// 	if avgVol > 0 {
// 		volumeRatio := currentVol / avgVol
// 		score += math.Min(volumeRatio/2.0, 2.0)
// 	}

// 	// Active status bonus (0-2 points)
// 	if growth.Status == "ACTIVE" {
// 		score += 2.0
// 		// Recent new high bonus
// 		if growth.DaysSinceNewHigh <= 5 {
// 			score += 1.0
// 		}
// 	}

// 	return math.Min(score, 10.0)
// }

// func (c *MarketStackClient) getGrowthAlertLevel(growth *GrowthMove, isSuperperformance bool, drawdown *Drawdown) string {
// 	if growth == nil {
// 		return "📋 TRACK"
// 	}

// 	if isSuperperformance && growth.Status == "ACTIVE" {
// 		if drawdown != nil {
// 			return "🔥 SUPERPERFORMANCE (DRAWDOWN)"
// 		}
// 		return "🔥 SUPERPERFORMANCE"
// 	}

// 	if growth.Status == "ACTIVE" && growth.GrowthPercentage >= 200 {
// 		return "🚀 HIGH GROWTH"
// 	}

// 	if growth.Status == "ACTIVE" && growth.GrowthPercentage >= 100 {
// 		return "📈 ACTIVE GROWTH"
// 	}

// 	return "👀 MONITOR"
// }

// func (c *MarketStackClient) getScreenReason(growth *GrowthMove, isSuperperformance bool) string {
// 	if growth == nil {
// 		return ""
// 	}

// 	if isSuperperformance {
// 		return fmt.Sprintf("Superperformance: %.1f%% in %d days",
// 			growth.GrowthPercentage, growth.TradingDays)
// 	}

// 	return fmt.Sprintf("Growth stock: %.1f%% in %d days",
// 		growth.GrowthPercentage, growth.TradingDays)
// }

// // Main runner for growth screening
// func (c *MarketStackClient) RunGrowthScreener(symbols []string) error {
// 	fmt.Printf("[INFO] 🚀 STARTING GROWTH STOCK & SUPERPERFORMANCE SCREENER\n")
// 	fmt.Printf("[INFO] Analyzing %d symbols for growth patterns\n", len(symbols))

// 	growthStocks, err := c.ScreenForGrowthStocks(symbols)
// 	if err != nil {
// 		return fmt.Errorf("growth screening failed: %v", err)
// 	}

// 	// Categorize results
// 	superperformanceStocks := []GrowthStock{}
// 	activeGrowthStocks := []GrowthStock{}
// 	drawdownStocks := []GrowthStock{}

// 	for _, stock := range growthStocks {
// 		if stock.IsSuperperformance {
// 			superperformanceStocks = append(superperformanceStocks, stock)
// 		} else if stock.CurrentGrowth != nil && stock.CurrentGrowth.Status == "ACTIVE" {
// 			activeGrowthStocks = append(activeGrowthStocks, stock)
// 		}

// 		if stock.HasActiveDrawdown {
// 			drawdownStocks = append(drawdownStocks, stock)
// 		}
// 	}

// 	// Save results
// 	timestamp := time.Now().Format("2006-01-02_15-04-05")
// 	filename := fmt.Sprintf("superperformance_data/growth_stocks_%s.json", timestamp)

// 	if err := saveGrowthStocks(growthStocks, filename); err != nil {
// 		return fmt.Errorf("failed to save results: %v", err)
// 	}

// 	// Print results
// 	printGrowthResults(superperformanceStocks, activeGrowthStocks, drawdownStocks)

// 	fmt.Printf("\n[INFO] Growth screening complete. Results saved to: %s\n", filename)
// 	return nil
// }

// func saveGrowthStocks(stocks []GrowthStock, filename string) error {
// 	if err := os.MkdirAll("superperformance_data", 0755); err != nil {
// 		return err
// 	}

// 	file, err := os.Create(filename)
// 	if err != nil {
// 		return err
// 	}
// 	defer file.Close()

// 	encoder := json.NewEncoder(file)
// 	encoder.SetIndent("", "  ")
// 	return encoder.Encode(stocks)
// }

// // Utility functions
// func calculateAverageVolume(data []StockData, days int) float64 {
// 	if len(data) < days {
// 		days = len(data)
// 	}

// 	total := 0.0
// 	count := 0

// 	for i := len(data) - days; i < len(data); i++ {
// 		if data[i].Volume > 0 {
// 			total += data[i].Volume
// 			count++
// 		}
// 	}

// 	if count == 0 {
// 		return 0
// 	}
// 	return total / float64(count)
// }

// func printGrowthResults(superperformance, activeGrowth, drawdown []GrowthStock) {
// 	fmt.Printf("\n" + "🔥" + strings.Repeat("=", 78) + "🔥\n")
// 	fmt.Printf("                    GROWTH STOCK & SUPERPERFORMANCE SCREENING\n")
// 	fmt.Printf("🔥" + strings.Repeat("=", 78) + "🔥\n")

// 	if len(superperformance) > 0 {
// 		fmt.Printf("\n🔥 SUPERPERFORMANCE STOCKS (%d):\n", len(superperformance))
// 		for i, stock := range superperformance {
// 			fmt.Printf("  %d. %s - $%.2f (%s: %.1f%% in %d days)\n",
// 				i+1, stock.Ticker, stock.CurrentPrice, stock.GrowthCategory,
// 				stock.CurrentGrowth.GrowthPercentage, stock.CurrentGrowth.TradingDays)
// 			fmt.Printf("     LOD: $%.2f on %s | High: $%.2f on %s\n",
// 				stock.CurrentGrowth.LOD, stock.CurrentGrowth.LODDate.Format("Jan 2"),
// 				stock.CurrentGrowth.HighPrice, stock.CurrentGrowth.HighPriceDate.Format("Jan 2"))
// 			if stock.HasActiveDrawdown {
// 				fmt.Printf("     ⚠️ In drawdown: %.1f%% from high\n", stock.CurrentDrawdown.DrawdownPercent)
// 			}
// 		}
// 	}

// 	if len(activeGrowth) > 0 {
// 		fmt.Printf("\n🚀 ACTIVE GROWTH STOCKS (%d):\n", len(activeGrowth))
// 		for i, stock := range activeGrowth {
// 			if i < 10 { // Show top 10
// 				fmt.Printf("  %d. %s - $%.2f (%.1f%% in %d days)\n",
// 					i+1, stock.Ticker, stock.CurrentPrice,
// 					stock.CurrentGrowth.GrowthPercentage, stock.CurrentGrowth.TradingDays)
// 			}
// 		}
// 		if len(activeGrowth) > 10 {
// 			fmt.Printf("  ... and %d more (see JSON file)\n", len(activeGrowth)-10)
// 		}
// 	}

// 	if len(drawdown) > 0 {
// 		fmt.Printf("\n⚠️ STOCKS IN DRAWDOWN (%d):\n", len(drawdown))
// 		for i, stock := range drawdown {
// 			if i < 5 { // Show top 5
// 				fmt.Printf("  %d. %s - %.1f%% drawdown from $%.2f high\n",
// 					i+1, stock.Ticker, stock.CurrentDrawdown.DrawdownPercent, stock.CurrentDrawdown.HighPrice)
// 			}
// 		}
// 	}

// 	if len(superperformance) == 0 && len(activeGrowth) == 0 {
// 		fmt.Printf("\n📊 No qualifying growth stocks found in current scan.\n")
// 		fmt.Printf("💡 Consider expanding symbol universe or adjusting criteria.\n")
// 	}

// 	fmt.Printf("\n" + strings.Repeat("=", 80) + "\n")
// }
