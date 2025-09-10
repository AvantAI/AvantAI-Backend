package superperformance

// import (
// 	"encoding/csv"
// 	"encoding/json"
// 	"fmt"
// 	"math"
// 	"os"
// 	"sort"
// 	"strconv"
// 	"strings"
// 	"time"
// )

// // Investment analysis structures combining your growth analysis with Minervini criteria
// type MinerviniCriteria struct {
// 	PriceAbove50SMA       bool    `json:"price_above_50_sma"`
// 	PriceAbove150SMA      bool    `json:"price_above_150_sma"`
// 	PriceAbove200SMA      bool    `json:"price_above_200_sma"`
// 	SMA150Above200        bool    `json:"sma_150_above_200"`
// 	SMA200TrendingUp      bool    `json:"sma_200_trending_up"`
// 	Within25PercentOfHigh bool    `json:"within_25_percent_of_high"`
// 	RelativeStrength      float64 `json:"relative_strength"`
// 	VolumePattern         string  `json:"volume_pattern"` // ACCUMULATION, DISTRIBUTION, NEUTRAL
// 	SMA50                 float64 `json:"sma_50"`
// 	SMA150                float64 `json:"sma_150"`
// 	SMA200                float64 `json:"sma_200"`
// 	CriteriaScore         int     `json:"criteria_score"`
// 	PassesTemplate        bool    `json:"passes_template"`
// }

// type VCPAnalysis struct {
// 	IsVCP             bool    `json:"is_vcp"`
// 	ContractionWeeks  int     `json:"contraction_weeks"`
// 	BaseDepth         float64 `json:"base_depth"`
// 	TighteningPattern bool    `json:"tightening_pattern"`
// 	BreakoutPrice     float64 `json:"breakout_price"`
// 	ProperEntry       float64 `json:"proper_entry"`
// 	MaxBuyPrice       float64 `json:"max_buy_price"`
// 	StopLossPrice     float64 `json:"stop_loss_price"`
// 	RiskRewardRatio   float64 `json:"risk_reward_ratio"`
// 	VolumeOnBreakout  bool    `json:"volume_on_breakout"`
// 	BaseType          string  `json:"base_type"` // CUP_WITH_HANDLE, FLAT_BASE, HIGH_TIGHT_FLAG
// }

// type InvestmentCandidate struct {
// 	GrowthStock
// 	MinerviniCriteria *MinerviniCriteria `json:"minervini_criteria"`
// 	VCPAnalysis       *VCPAnalysis       `json:"vcp_analysis"`
// 	EntryStrategy     *EntryStrategy     `json:"entry_strategy"`
// 	BuyReason         string             `json:"buy_reason"`
// 	InvestmentScore   float64            `json:"investment_score"`
// 	AddedToWatchlist  time.Time          `json:"added_to_watchlist"`
// 	RecommendedAction string             `json:"recommended_action"` // BUY_NOW, BUY_BREAKOUT, HOLD, SELL, WATCH
// 	Priority          string             `json:"priority"`           // HIGH, MEDIUM, LOW
// 	Notes             string             `json:"notes"`
// }

// type WatchlistEntry struct {
// 	Ticker             string    `json:"ticker"`
// 	EntryDate          time.Time `json:"entry_date"`
// 	EntryPrice         float64   `json:"entry_price"`
// 	CurrentPrice       float64   `json:"current_price"`
// 	StopLoss           float64   `json:"stop_loss"`
// 	TargetPrice        float64   `json:"target_price"`
// 	UnrealizedGainLoss float64   `json:"unrealized_gain_loss"`
// 	UnrealizedPercent  float64   `json:"unrealized_percent"`
// 	DaysHeld           int       `json:"days_held"`
// 	Status             string    `json:"status"` // ACTIVE, STOPPED_OUT, SOLD, WAITING_ENTRY
// 	SellReason         string    `json:"sell_reason,omitempty"`
// 	LastAnalysisDate   time.Time `json:"last_analysis_date"`
// 	MinerviniBreakdown bool      `json:"minervini_breakdown"`
// 	GrowthMoveIntact   bool      `json:"growth_move_intact"`
// 	TrailingStopLoss   float64   `json:"trailing_stop_loss"`
// 	MaxGainPercent     float64   `json:"max_gain_percent"`
// 	BuyTriggerPrice    float64   `json:"buy_trigger_price,omitempty"`
// 	WaitingForBreakout bool      `json:"waiting_for_breakout"`
// }

// // Main investment screening function
// func (c *MarketStackClient) RunInvestmentScreener(symbols []string) error {
// 	fmt.Printf("[INFO] 🎯 STARTING INVESTMENT-GRADE STOCK SCREENER\n")
// 	fmt.Printf("[INFO] Combining Growth Analysis + Minervini Template\n")
// 	fmt.Printf("[INFO] Analyzing %d symbols\n", len(symbols))

// 	// First run growth screening
// 	growthStocks, err := c.ScreenForGrowthStocks(symbols)
// 	if err != nil {
// 		return fmt.Errorf("growth screening failed: %v", err)
// 	}

// 	var investmentCandidates []InvestmentCandidate

// 	// Analyze each growth stock with Minervini criteria and entry strategy
// 	for _, growthStock := range growthStocks {
// 		candidate, err := c.analyzeInvestmentCandidate(growthStock)
// 		if err != nil {
// 			fmt.Printf("[ERROR] Failed to analyze %s: %v\n", growthStock.Ticker, err)
// 			continue
// 		}

// 		if candidate != nil {
// 			investmentCandidates = append(investmentCandidates, *candidate)
// 		}
// 	}

// 	// Sort by investment score
// 	sort.Slice(investmentCandidates, func(i, j int) bool {
// 		return investmentCandidates[i].InvestmentScore > investmentCandidates[j].InvestmentScore
// 	})

// 	// Save results and update watchlist
// 	if err := c.saveInvestmentResults(investmentCandidates); err != nil {
// 		return fmt.Errorf("failed to save results: %v", err)
// 	}

// 	if err := c.updateWatchlist(investmentCandidates); err != nil {
// 		return fmt.Errorf("failed to update watchlist: %v", err)
// 	}

// 	// Print investment recommendations
// 	c.printInvestmentRecommendations(investmentCandidates)

// 	return nil
// }

// func (c *MarketStackClient) analyzeInvestmentCandidate(growthStock GrowthStock) (*InvestmentCandidate, error) {
// 	// Get fresh data for technical analysis
// 	dateTo := time.Now().Format("2006-01-02")
// 	dateFrom := time.Now().AddDate(-1, 0, 0).Format("2006-01-02") // 1 year for moving averages

// 	data, err := c.GetStockData(growthStock.Ticker, dateFrom, dateTo)
// 	if err != nil {
// 		return nil, err
// 	}

// 	if len(data) < 200 {
// 		return nil, fmt.Errorf("insufficient data for moving averages")
// 	}

// 	// Calculate Minervini criteria
// 	minerviniCriteria := c.calculateMinerviniCriteria(data)

// 	// Analyze VCP pattern for entry timing
// 	vcpAnalysis := c.analyzeVCPPattern(data)

// 	// Determine entry strategy
// 	entryStrategy := c.determineEntryStrategy(data, growthStock.CurrentGrowth, vcpAnalysis, minerviniCriteria)

// 	// Calculate investment score
// 	investmentScore := c.calculateInvestmentScore(growthStock, minerviniCriteria, vcpAnalysis, entryStrategy)

// 	// Determine action and priority
// 	action, priority := c.determineInvestmentAction(growthStock, minerviniCriteria, entryStrategy, investmentScore)

// 	// Only include high-quality candidates
// 	if investmentScore < 6.0 {
// 		return nil, nil
// 	}

// 	candidate := &InvestmentCandidate{
// 		GrowthStock:       growthStock,
// 		MinerviniCriteria: minerviniCriteria,
// 		VCPAnalysis:       vcpAnalysis,
// 		EntryStrategy:     entryStrategy,
// 		BuyReason:         c.generateBuyReason(growthStock, minerviniCriteria, vcpAnalysis),
// 		InvestmentScore:   investmentScore,
// 		AddedToWatchlist:  time.Now(),
// 		RecommendedAction: action,
// 		Priority:          priority,
// 		Notes:             c.generateInvestmentNotes(growthStock, minerviniCriteria, entryStrategy),
// 	}

// 	return candidate, nil
// }

// func (c *MarketStackClient) calculateMinerviniCriteria(data []StockData) *MinerviniCriteria {
// 	if len(data) < 200 {
// 		return nil
// 	}

// 	currentData := data[len(data)-1]
// 	currentPrice := currentData.Close

// 	// Calculate moving averages
// 	sma50 := c.CalculateSMA(data, 50)
// 	sma150 := c.CalculateSMA(data, 150)
// 	sma200 := c.CalculateSMA(data, 200)

// 	// Calculate 52-week high
// 	high52Week := 0.0
// 	for i := len(data) - 252; i < len(data); i++ {
// 		if i >= 0 && data[i].High > high52Week {
// 			high52Week = data[i].High
// 		}
// 	}

// 	// Check 200-day SMA trend (rising for at least 1 month)
// 	sma200TrendingUp := c.isSMATrendingUp(data, 200, 22) // 22 trading days ≈ 1 month

// 	// Calculate relative strength vs market (simplified)
// 	relativeStrength := c.calculateRelativeStrength(data)

// 	// Analyze volume pattern
// 	volumePattern := c.analyzeVolumePattern(data, 20)

// 	// Count criteria met
// 	criteriaScore := 0
// 	priceAbove50 := currentPrice > sma50
// 	priceAbove150 := currentPrice > sma150
// 	priceAbove200 := currentPrice > sma200
// 	sma150Above200 := sma150 > sma200
// 	within25Percent := (high52Week-currentPrice)/high52Week <= 0.25

// 	if priceAbove50 {
// 		criteriaScore++
// 	}
// 	if priceAbove150 {
// 		criteriaScore++
// 	}
// 	if priceAbove200 {
// 		criteriaScore++
// 	}
// 	if sma150Above200 {
// 		criteriaScore++
// 	}
// 	if sma200TrendingUp {
// 		criteriaScore++
// 	}
// 	if within25Percent {
// 		criteriaScore++
// 	}
// 	if relativeStrength >= 70 {
// 		criteriaScore++
// 	}

// 	return &MinerviniCriteria{
// 		PriceAbove50SMA:       priceAbove50,
// 		PriceAbove150SMA:      priceAbove150,
// 		PriceAbove200SMA:      priceAbove200,
// 		SMA150Above200:        sma150Above200,
// 		SMA200TrendingUp:      sma200TrendingUp,
// 		Within25PercentOfHigh: within25Percent,
// 		RelativeStrength:      relativeStrength,
// 		VolumePattern:         volumePattern,
// 		SMA50:                 sma50,
// 		SMA150:                sma150,
// 		SMA200:                sma200,
// 		CriteriaScore:         criteriaScore,
// 		PassesTemplate:        criteriaScore >= 6, // Must meet at least 6/7 criteria
// 	}
// }

// func (c *MarketStackClient) analyzeVCPPattern(data []StockData) *VCPAnalysis {
// 	if len(data) < 100 {
// 		return nil
// 	}

// 	// Look for volatility contraction pattern in last 3-8 weeks
// 	currentPrice := data[len(data)-1].Close

// 	// Find recent base/consolidation
// 	baseStart, baseEnd := c.findRecentBase(data)
// 	if baseStart == -1 || baseEnd == -1 {
// 		return &VCPAnalysis{IsVCP: false}
// 	}

// 	// Calculate base characteristics
// 	baseHigh := 0.0
// 	baseLow := math.MaxFloat64

// 	for i := baseStart; i <= baseEnd; i++ {
// 		if data[i].High > baseHigh {
// 			baseHigh = data[i].High
// 		}
// 		if data[i].Low < baseLow {
// 			baseLow = data[i].Low
// 		}
// 	}

// 	baseDepth := (baseHigh - baseLow) / baseHigh * 100
// 	contractionWeeks := (baseEnd - baseStart) / 5 // Approximate weeks

// 	// Check for tightening pattern
// 	tighteningPattern := c.checkTighteningPattern(data, baseStart, baseEnd)

// 	// Calculate breakout levels
// 	breakoutPrice := baseHigh * 1.02 // 2% above base high
// 	properEntry := baseHigh * 1.025  // 2.5% above base high for proper entry
// 	maxBuyPrice := baseHigh * 1.05   // Maximum 5% above breakout
// 	stopLossPrice := baseLow * 0.98  // 2% below base low

// 	// Check volume characteristics
// 	volumeOnBreakout := c.checkBreakoutVolume(data, baseEnd)

// 	// Determine base type
// 	baseType := c.classifyBaseType(data, baseStart, baseEnd, baseDepth)

// 	// Risk/reward calculation
// 	riskAmount := properEntry - stopLossPrice
// 	rewardAmount := properEntry * 0.25 // Conservative 25% target
// 	riskRewardRatio := rewardAmount / riskAmount

// 	isVCP := contractionWeeks >= 3 && contractionWeeks <= 8 &&
// 		baseDepth <= 25 && tighteningPattern &&
// 		currentPrice >= baseLow*1.01 // Not breaking down

// 	return &VCPAnalysis{
// 		IsVCP:             isVCP,
// 		ContractionWeeks:  contractionWeeks,
// 		BaseDepth:         baseDepth,
// 		TighteningPattern: tighteningPattern,
// 		BreakoutPrice:     breakoutPrice,
// 		ProperEntry:       properEntry,
// 		MaxBuyPrice:       maxBuyPrice,
// 		StopLossPrice:     stopLossPrice,
// 		RiskRewardRatio:   riskRewardRatio,
// 		VolumeOnBreakout:  volumeOnBreakout,
// 		BaseType:          baseType,
// 	}
// }

// type EntryStrategy struct {
// 	BuyNow              bool    `json:"buy_now"`
// 	BuyAtBreakout       bool    `json:"buy_at_breakout"`
// 	BuyPrice            float64 `json:"buy_price"`
// 	MaxBuyPrice         float64 `json:"max_buy_price"`
// 	StopLossPrice       float64 `json:"stop_loss_price"`
// 	InitialTargetPrice  float64 `json:"initial_target_price"`
// 	TrailingStopPercent float64 `json:"trailing_stop_percent"`
// 	EntryReason         string  `json:"entry_reason"`
// 	RiskPercent         float64 `json:"risk_percent"`
// 	RewardRiskRatio     float64 `json:"reward_risk_ratio"`
// 	TimeframeDays       int     `json:"timeframe_days"`
// 	BuyCondition        string  `json:"buy_condition"` // MARKET_BUY, BREAKOUT_BUY, PULLBACK_BUY
// }

// func (c *MarketStackClient) determineEntryStrategy(data []StockData, growth *GrowthMove, vcp *VCPAnalysis, minervini *MinerviniCriteria) *EntryStrategy {
// 	currentPrice := data[len(data)-1].Close

// 	strategy := &EntryStrategy{
// 		TrailingStopPercent: 20, // 20% trailing stop for growth stocks
// 		TimeframeDays:       90, // 3-month timeframe for initial targets
// 	}

// 	// Case 1: Stock breaking out of VCP pattern NOW
// 	if vcp != nil && vcp.IsVCP && minervini.PassesTemplate {
// 		if currentPrice >= vcp.BreakoutPrice && currentPrice <= vcp.MaxBuyPrice {
// 			strategy.BuyNow = true
// 			strategy.BuyPrice = currentPrice
// 			strategy.MaxBuyPrice = vcp.MaxBuyPrice
// 			strategy.StopLossPrice = vcp.StopLossPrice
// 			strategy.BuyCondition = "MARKET_BUY"
// 			strategy.EntryReason = fmt.Sprintf("VCP breakout at $%.2f", vcp.BreakoutPrice)
// 		} else if currentPrice < vcp.BreakoutPrice {
// 			strategy.BuyAtBreakout = true
// 			strategy.BuyPrice = vcp.ProperEntry
// 			strategy.MaxBuyPrice = vcp.MaxBuyPrice
// 			strategy.StopLossPrice = vcp.StopLossPrice
// 			strategy.BuyCondition = "BREAKOUT_BUY"
// 			strategy.EntryReason = fmt.Sprintf("Buy on breakout above $%.2f", vcp.BreakoutPrice)
// 		}
// 	}

// 	// Case 2: Strong growth stock at/near support (Minervini template)
// 	if minervini.PassesTemplate && growth != nil && growth.Status == "ACTIVE" {
// 		// Check if near key moving average support
// 		if currentPrice <= minervini.SMA50*1.03 && currentPrice >= minervini.SMA50*0.97 {
// 			strategy.BuyNow = true
// 			strategy.BuyPrice = currentPrice
// 			strategy.MaxBuyPrice = minervini.SMA50 * 1.02
// 			strategy.StopLossPrice = minervini.SMA50 * 0.95
// 			strategy.BuyCondition = "MARKET_BUY"
// 			strategy.EntryReason = "At 50-day MA support in active growth move"
// 		} else if currentPrice <= minervini.SMA150*1.05 && currentPrice >= minervini.SMA150*0.98 {
// 			strategy.BuyNow = true
// 			strategy.BuyPrice = currentPrice
// 			strategy.MaxBuyPrice = minervini.SMA150 * 1.03
// 			strategy.StopLossPrice = minervini.SMA150 * 0.95
// 			strategy.BuyCondition = "MARKET_BUY"
// 			strategy.EntryReason = "At 150-day MA support in active growth move"
// 		}
// 	}

// 	// Case 3: Pullback entry for superperformance stocks
// 	if growth != nil && growth.Status == "ACTIVE" &&
// 		strings.Contains(strategy.EntryReason, "SUPER") {
// 		// More aggressive entry for superperformance
// 		highPrice := growth.HighPrice
// 		pullbackPercent := (highPrice - currentPrice) / highPrice * 100

// 		if pullbackPercent >= 8 && pullbackPercent <= 20 {
// 			strategy.BuyNow = true
// 			strategy.BuyPrice = currentPrice
// 			strategy.MaxBuyPrice = currentPrice * 1.02
// 			strategy.StopLossPrice = math.Max(minervini.SMA50*0.95, currentPrice*0.92)
// 			strategy.BuyCondition = "PULLBACK_BUY"
// 			strategy.EntryReason = fmt.Sprintf("Pullback entry in superperformance stock (%.1f%% from high)", pullbackPercent)
// 		}
// 	}

// 	// Set default stop if not set
// 	if strategy.StopLossPrice == 0 {
// 		if minervini.SMA50 > 0 {
// 			strategy.StopLossPrice = minervini.SMA50 * 0.93 // 7% below 50-day MA
// 		} else {
// 			strategy.StopLossPrice = currentPrice * 0.92 // 8% stop
// 		}
// 	}

// 	// Set default buy price if interested but no immediate entry
// 	if strategy.BuyPrice == 0 && minervini.PassesTemplate {
// 		strategy.BuyPrice = currentPrice * 1.01 // Small premium
// 		strategy.MaxBuyPrice = currentPrice * 1.03
// 	}

// 	// Calculate risk/reward
// 	if strategy.BuyPrice > 0 && strategy.StopLossPrice > 0 {
// 		riskAmount := strategy.BuyPrice - strategy.StopLossPrice
// 		strategy.RiskPercent = riskAmount / strategy.BuyPrice * 100

// 		// Conservative target: 25% gain or next resistance level
// 		strategy.InitialTargetPrice = strategy.BuyPrice * 1.25
// 		if growth != nil && growth.HighPrice > strategy.InitialTargetPrice {
// 			strategy.InitialTargetPrice = growth.HighPrice * 1.10 // 10% above previous high
// 		}

// 		rewardAmount := strategy.InitialTargetPrice - strategy.BuyPrice
// 		strategy.RewardRiskRatio = rewardAmount / riskAmount
// 	}

// 	return strategy
// }

// func (c *MarketStackClient) calculateInvestmentScore(growth GrowthStock, minervini *MinerviniCriteria, vcp *VCPAnalysis, entry *EntryStrategy) float64 {
// 	score := growth.Score // Start with growth score (0-10)

// 	// Minervini template bonus (0-3 points)
// 	if minervini != nil {
// 		score += float64(minervini.CriteriaScore) * 0.3 // Up to 2.1 points
// 		if minervini.PassesTemplate {
// 			score += 1.0 // Bonus for passing full template
// 		}
// 	}

// 	// VCP pattern bonus (0-2 points)
// 	if vcp != nil && vcp.IsVCP {
// 		score += 2.0
// 		if vcp.RiskRewardRatio >= 3.0 {
// 			score += 1.0 // Good risk/reward
// 		}
// 	}

// 	// Entry timing bonus (0-2 points)
// 	if entry != nil {
// 		if entry.BuyNow && entry.RiskPercent <= 8 {
// 			score += 2.0 // Great entry timing with low risk
// 		} else if entry.BuyAtBreakout && entry.RewardRiskRatio >= 2.5 {
// 			score += 1.5 // Good breakout setup
// 		}
// 	}

// 	// Superperformance bonus
// 	if growth.IsSuperperformance {
// 		score += 1.0
// 	}

// 	return math.Min(score, 15.0) // Cap at 15
// }

// func (c *MarketStackClient) determineInvestmentAction(_ GrowthStock, minervini *MinerviniCriteria, entry *EntryStrategy, score float64) (string, string) {
// 	// High priority candidates (score >= 10)
// 	if score >= 10 && entry != nil && entry.BuyNow {
// 		return "BUY_NOW", "HIGH"
// 	}

// 	// Medium-high priority (score >= 8)
// 	if score >= 8 {
// 		if entry != nil && entry.BuyAtBreakout {
// 			return "BUY_BREAKOUT", "HIGH"
// 		}
// 		if entry != nil && entry.BuyNow {
// 			return "BUY_NOW", "MEDIUM"
// 		}
// 	}

// 	// Medium priority (score >= 6)
// 	if score >= 6 && minervini != nil && minervini.PassesTemplate {
// 		return "WATCH", "MEDIUM"
// 	}

// 	return "WATCH", "LOW"
// }

// // Watchlist management functions
// func (c *MarketStackClient) updateWatchlist(candidates []InvestmentCandidate) error {
// 	// Load existing watchlist
// 	watchlist, err := c.loadWatchlist()
// 	if err != nil {
// 		watchlist = []WatchlistEntry{} // Start fresh if file doesn't exist
// 	}

// 	// Add new high-priority candidates
// 	for _, candidate := range candidates {
// 		if candidate.Priority == "HIGH" || candidate.RecommendedAction == "BUY_NOW" {
// 			// Check if already in watchlist
// 			exists := false
// 			for i := range watchlist {
// 				if watchlist[i].Ticker == candidate.Ticker {
// 					// Update existing entry
// 					watchlist[i].CurrentPrice = candidate.CurrentPrice
// 					watchlist[i].LastAnalysisDate = time.Now()
// 					if candidate.EntryStrategy != nil {
// 						watchlist[i].BuyTriggerPrice = candidate.EntryStrategy.BuyPrice
// 						watchlist[i].StopLoss = candidate.EntryStrategy.StopLossPrice
// 						watchlist[i].TargetPrice = candidate.EntryStrategy.InitialTargetPrice
// 					}
// 					exists = true
// 					break
// 				}
// 			}

// 			if !exists {
// 				entry := WatchlistEntry{
// 					Ticker:           candidate.Ticker,
// 					EntryDate:        time.Now(),
// 					CurrentPrice:     candidate.CurrentPrice,
// 					Status:           "WAITING_ENTRY",
// 					LastAnalysisDate: time.Now(),
// 				}

// 				if candidate.EntryStrategy != nil {
// 					entry.BuyTriggerPrice = candidate.EntryStrategy.BuyPrice
// 					entry.StopLoss = candidate.EntryStrategy.StopLossPrice
// 					entry.TargetPrice = candidate.EntryStrategy.InitialTargetPrice
// 					entry.WaitingForBreakout = candidate.EntryStrategy.BuyAtBreakout
// 				}

// 				watchlist = append(watchlist, entry)
// 			}
// 		}
// 	}

// 	return c.saveWatchlist(watchlist)
// }

// func (c *MarketStackClient) RunWatchlistMonitor() error {
// 	fmt.Printf("[INFO] 👁️ MONITORING WATCHLIST FOR SELL SIGNALS\n")

// 	watchlist, err := c.loadWatchlist()
// 	if err != nil {
// 		return fmt.Errorf("failed to load watchlist: %v", err)
// 	}

// 	if len(watchlist) == 0 {
// 		fmt.Printf("[INFO] Watchlist is empty\n")
// 		return nil
// 	}

// 	var updatedWatchlist []WatchlistEntry
// 	sellRecommendations := []WatchlistEntry{}

// 	for _, entry := range watchlist {
// 		if entry.Status == "STOPPED_OUT" || entry.Status == "SOLD" {
// 			continue // Skip already closed positions
// 		}

// 		// Get fresh data
// 		dateTo := time.Now().Format("2006-01-02")
// 		dateFrom := time.Now().AddDate(0, -6, 0).Format("2006-01-02") // 6 months

// 		data, err := c.GetStockData(entry.Ticker, dateFrom, dateTo)
// 		if err != nil {
// 			fmt.Printf("[ERROR] Failed to get data for %s: %v\n", entry.Ticker, err)
// 			updatedWatchlist = append(updatedWatchlist, entry)
// 			continue
// 		}

// 		updatedEntry := c.analyzeWatchlistEntry(entry, data)

// 		if updatedEntry.Status == "STOPPED_OUT" ||
// 			(updatedEntry.Status == "ACTIVE" && strings.Contains(updatedEntry.SellReason, "SELL")) {
// 			sellRecommendations = append(sellRecommendations, updatedEntry)
// 		}

// 		updatedWatchlist = append(updatedWatchlist, updatedEntry)
// 	}

// 	// Save updated watchlist
// 	if err := c.saveWatchlist(updatedWatchlist); err != nil {
// 		return err
// 	}

// 	// Print monitoring results
// 	c.printWatchlistResults(updatedWatchlist, sellRecommendations)

// 	return nil
// }

// func (c *MarketStackClient) analyzeWatchlistEntry(entry WatchlistEntry, data []StockData) WatchlistEntry {
// 	currentData := data[len(data)-1]
// 	currentPrice := currentData.Close

// 	updated := entry
// 	updated.CurrentPrice = currentPrice
// 	updated.LastAnalysisDate = time.Now()

// 	// Calculate P&L if position is active
// 	if entry.Status == "ACTIVE" && entry.EntryPrice > 0 {
// 		updated.UnrealizedGainLoss = currentPrice - entry.EntryPrice
// 		updated.UnrealizedPercent = updated.UnrealizedGainLoss / entry.EntryPrice * 100
// 		updated.DaysHeld = int(time.Since(entry.EntryDate).Hours() / 24)

// 		// Update trailing stop
// 		if updated.UnrealizedPercent > 20 {
// 			newTrailingStop := currentPrice * 0.8 // 20% trailing stop
// 			if newTrailingStop > updated.TrailingStopLoss {
// 				updated.TrailingStopLoss = newTrailingStop
// 			}
// 		}

// 		// Check for max gain tracking
// 		if updated.UnrealizedPercent > updated.MaxGainPercent {
// 			updated.MaxGainPercent = updated.UnrealizedPercent
// 		}
// 	}

// 	// Check sell conditions
// 	sellSignal, reason := c.checkSellConditions(updated, data)
// 	if sellSignal {
// 		updated.SellReason = reason
// 		if strings.Contains(reason, "STOP") {
// 			updated.Status = "STOPPED_OUT"
// 		}
// 	}

// 	// Check buy conditions for waiting entries
// 	if entry.Status == "WAITING_ENTRY" {
// 		buySignal := c.checkBuyConditions(updated, data)
// 		if buySignal {
// 			updated.Status = "ACTIVE"
// 			updated.EntryPrice = currentPrice
// 			updated.EntryDate = time.Now()
// 		}
// 	}

// 	// Check Minervini breakdown
// 	if len(data) >= 50 {
// 		minervini := c.calculateMinerviniCriteria(data)
// 		if minervini != nil {
// 			updated.MinerviniBreakdown = !minervini.PassesTemplate
// 		}
// 	}

// 	return updated
// }

// func (c *MarketStackClient) checkSellConditions(entry WatchlistEntry, data []StockData) (bool, string) {
// 	currentPrice := data[len(data)-1].Close

// 	// Stop loss hit
// 	if currentPrice <= entry.StopLoss {
// 		return true, "STOP LOSS HIT"
// 	}

// 	// Trailing stop hit
// 	if entry.TrailingStopLoss > 0 && currentPrice <= entry.TrailingStopLoss {
// 		return true, "TRAILING STOP HIT"
// 	}

// 	// Minervini template breakdown
// 	if entry.MinerviniBreakdown {
// 		return true, "MINERVINI TEMPLATE BREAKDOWN"
// 	}

// 	// Growth move ended
// 	if !entry.GrowthMoveIntact {
// 		return true, "GROWTH MOVE ENDED"
// 	}

// 	// Calculate moving averages for technical breakdown
// 	if len(data) >= 50 {
// 		sma50 := c.CalculateSMA(data, 50)
// 		if currentPrice < sma50*0.95 { // 5% below 50-day MA
// 			return true, "BROKE BELOW 50-DAY MA"
// 		}
// 	}

// 	// Large single-day decline (8%+ in one day)
// 	if len(data) >= 2 {
// 		previousClose := data[len(data)-2].Close
// 		dailyChange := (currentPrice - previousClose) / previousClose * 100
// 		if dailyChange <= -8 {
// 			return true, "LARGE SINGLE DAY DECLINE"
// 		}
// 	}

// 	// Time-based sell (holding too long without progress)
// 	if entry.DaysHeld > 90 && entry.UnrealizedPercent < 10 {
// 		return true, "HOLDING TOO LONG WITHOUT PROGRESS"
// 	}

// 	return false, ""
// }

// func (c *MarketStackClient) checkBuyConditions(entry WatchlistEntry, data []StockData) bool {
// 	currentPrice := data[len(data)-1].Close

// 	// Check if breakout trigger hit
// 	if entry.WaitingForBreakout && entry.BuyTriggerPrice > 0 {
// 		if currentPrice >= entry.BuyTriggerPrice {
// 			// Confirm with volume
// 			currentVolume := data[len(data)-1].Volume
// 			avgVolume := calculateAverageVolume(data, 20)
// 			if currentVolume > avgVolume*1.2 { // 20% above average volume
// 				return true
// 			}
// 		}
// 	}

// 	// Check if at good support level for entry
// 	if !entry.WaitingForBreakout && entry.BuyTriggerPrice > 0 {
// 		return math.Abs(currentPrice-entry.BuyTriggerPrice)/entry.BuyTriggerPrice <= 0.02 // Within 2%
// 	}

// 	return false
// }

// // Helper functions for technical analysis
// func (c *MarketStackClient) CalculateSMA(data []StockData, period int) float64 {
// 	if len(data) < period {
// 		return 0
// 	}

// 	sum := 0.0
// 	for i := len(data) - period; i < len(data); i++ {
// 		sum += data[i].Close
// 	}
// 	return sum / float64(period)
// }

// func (c *MarketStackClient) isSMATrendingUp(data []StockData, period, trendDays int) bool {
// 	if len(data) < period+trendDays {
// 		return false
// 	}

// 	currentSMA := c.CalculateSMA(data, period)
// 	pastSMA := c.CalculateSMA(data[:len(data)-trendDays], period)

// 	return currentSMA > pastSMA*1.01 // At least 1% increase
// }

// func (c *MarketStackClient) calculateRelativeStrength(data []StockData) float64 {
// 	if len(data) < 252 {
// 		return 0
// 	}

// 	// Simple relative strength calculation (stock performance vs time)
// 	currentPrice := data[len(data)-1].Close
// 	priceYear := data[len(data)-252].Close

// 	stockPerformance := (currentPrice - priceYear) / priceYear * 100

// 	// Normalize to 0-100 scale (simplified)
// 	return math.Min(math.Max(stockPerformance, 0), 100)
// }

// func (c *MarketStackClient) analyzeVolumePattern(data []StockData, days int) string {
// 	if len(data) < days {
// 		return "NEUTRAL"
// 	}

// 	upVolumeSum := 0.0
// 	downVolumeSum := 0.0
// 	upDays := 0
// 	downDays := 0

// 	for i := len(data) - days; i < len(data)-1; i++ {
// 		priceChange := data[i+1].Close - data[i].Close
// 		if priceChange > 0 {
// 			upVolumeSum += data[i+1].Volume
// 			upDays++
// 		} else if priceChange < 0 {
// 			downVolumeSum += data[i+1].Volume
// 			downDays++
// 		}
// 	}

// 	if upDays == 0 || downDays == 0 {
// 		return "NEUTRAL"
// 	}

// 	avgUpVolume := upVolumeSum / float64(upDays)
// 	avgDownVolume := downVolumeSum / float64(downDays)

// 	ratio := avgUpVolume / avgDownVolume
// 	if ratio >= 1.2 {
// 		return "ACCUMULATION"
// 	} else if ratio <= 0.8 {
// 		return "DISTRIBUTION"
// 	}
// 	return "NEUTRAL"
// }

// func (c *MarketStackClient) findRecentBase(data []StockData) (int, int) {
// 	if len(data) < 50 {
// 		return -1, -1
// 	}

// 	// Look for consolidation in last 8 weeks (40 trading days)
// 	endIndex := len(data) - 1
// 	startIndex := len(data) - 40
// 	if startIndex < 0 {
// 		startIndex = 0
// 	}

// 	// Find highest and lowest points in period
// 	highPrice := 0.0
// 	lowPrice := math.MaxFloat64

// 	for i := startIndex; i <= endIndex; i++ {
// 		if data[i].High > highPrice {
// 			highPrice = data[i].High
// 		}
// 		if data[i].Low < lowPrice {
// 			lowPrice = data[i].Low
// 		}
// 	}

// 	// Check if this forms a reasonable base (not too volatile)
// 	volatility := (highPrice - lowPrice) / lowPrice * 100
// 	if volatility <= 25 { // Base depth <= 25%
// 		return startIndex, endIndex
// 	}

// 	return -1, -1
// }

// func (c *MarketStackClient) checkTighteningPattern(data []StockData, start, end int) bool {
// 	if end-start < 15 { // Need at least 3 weeks
// 		return false
// 	}

// 	// Check if volatility is contracting over time
// 	firstHalfVol := c.calculateVolatility(data, start, start+(end-start)/2)
// 	secondHalfVol := c.calculateVolatility(data, start+(end-start)/2, end)

// 	return secondHalfVol < firstHalfVol*0.8 // Second half has 20% less volatility
// }

// func (c *MarketStackClient) calculateVolatility(data []StockData, start, end int) float64 {
// 	if start >= end || end >= len(data) {
// 		return 0
// 	}

// 	high := 0.0
// 	low := math.MaxFloat64

// 	for i := start; i <= end; i++ {
// 		if data[i].High > high {
// 			high = data[i].High
// 		}
// 		if data[i].Low < low {
// 			low = data[i].Low
// 		}
// 	}

// 	return (high - low) / low * 100
// }

// func (c *MarketStackClient) checkBreakoutVolume(data []StockData, baseEnd int) bool {
// 	if baseEnd >= len(data)-1 {
// 		return false
// 	}

// 	// Check if volume increased on breakout day
// 	breakoutVolume := data[baseEnd+1].Volume
// 	avgVolume := calculateAverageVolume(data[:baseEnd], 20)

// 	return breakoutVolume > avgVolume*1.5 // 50% above average
// }

// func (c *MarketStackClient) classifyBaseType(_ []StockData, _ int, _ int, depth float64) string {
// 	if depth <= 12 {
// 		return "HIGH_TIGHT_FLAG"
// 	} else if depth <= 20 {
// 		return "FLAT_BASE"
// 	} else {
// 		return "CUP_WITH_HANDLE"
// 	}
// }

// func (c *MarketStackClient) generateBuyReason(growth GrowthStock, minervini *MinerviniCriteria, vcp *VCPAnalysis) string {
// 	reasons := []string{}

// 	if growth.IsSuperperformance {
// 		reasons = append(reasons, fmt.Sprintf("Superperformance stock (%.1f%% growth)", growth.CurrentGrowth.GrowthPercentage))
// 	} else if growth.IsGrowthStock {
// 		reasons = append(reasons, fmt.Sprintf("Active growth stock (%.1f%% growth)", growth.CurrentGrowth.GrowthPercentage))
// 	}

// 	if minervini != nil && minervini.PassesTemplate {
// 		reasons = append(reasons, "Passes Minervini Template")
// 	}

// 	if vcp != nil && vcp.IsVCP {
// 		reasons = append(reasons, fmt.Sprintf("VCP pattern (%s)", vcp.BaseType))
// 	}

// 	if minervini != nil && minervini.RelativeStrength >= 80 {
// 		reasons = append(reasons, "Strong relative strength")
// 	}

// 	return strings.Join(reasons, " + ")
// }

// func (c *MarketStackClient) generateInvestmentNotes(growth GrowthStock, minervini *MinerviniCriteria, entry *EntryStrategy) string {
// 	notes := []string{}

// 	if growth.CurrentGrowth != nil {
// 		notes = append(notes, fmt.Sprintf("Growth move: %d days, %.1f%% gain",
// 			growth.CurrentGrowth.TradingDays, growth.CurrentGrowth.GrowthPercentage))
// 	}

// 	if minervini != nil {
// 		notes = append(notes, fmt.Sprintf("Minervini score: %d/7", minervini.CriteriaScore))
// 	}

// 	if entry != nil && entry.RiskPercent > 0 {
// 		notes = append(notes, fmt.Sprintf("Risk: %.1f%%, R:R %.1f:1",
// 			entry.RiskPercent, entry.RewardRiskRatio))
// 	}

// 	if growth.HasActiveDrawdown {
// 		notes = append(notes, fmt.Sprintf("In %.1f%% drawdown", growth.CurrentDrawdown.DrawdownPercent))
// 	}

// 	return strings.Join(notes, " | ")
// }

// // File I/O functions
// func (c *MarketStackClient) loadWatchlist() ([]WatchlistEntry, error) {
// 	filename := "watchlist.json"

// 	file, err := os.Open(filename)
// 	if err != nil {
// 		if os.IsNotExist(err) {
// 			return []WatchlistEntry{}, nil
// 		}
// 		return nil, err
// 	}
// 	defer file.Close()

// 	var watchlist []WatchlistEntry
// 	decoder := json.NewDecoder(file)
// 	err = decoder.Decode(&watchlist)
// 	return watchlist, err
// }

// func (c *MarketStackClient) saveWatchlist(watchlist []WatchlistEntry) error {
// 	filename := "watchlist.json"

// 	file, err := os.Create(filename)
// 	if err != nil {
// 		return err
// 	}
// 	defer file.Close()

// 	encoder := json.NewEncoder(file)
// 	encoder.SetIndent("", "  ")
// 	return encoder.Encode(watchlist)
// }

// func (c *MarketStackClient) saveWatchlistCSV(candidates []InvestmentCandidate) error {
// 	filename := "watchlist.csv"

// 	file, err := os.Create(filename)
// 	if err != nil {
// 		return err
// 	}
// 	defer file.Close()

// 	writer := csv.NewWriter(file)
// 	defer writer.Flush()

// 	// Write header
// 	header := []string{
// 		"Ticker", "Current_Price", "Buy_Price", "Max_Buy_Price", "Stop_Loss",
// 		"Target_Price", "Risk_Percent", "Reward_Risk_Ratio", "Action", "Priority",
// 		"Growth_Percent", "Growth_Days", "Minervini_Score", "Investment_Score",
// 		"Buy_Reason", "Notes", "Date_Added",
// 	}
// 	writer.Write(header)

// 	// Write data
// 	for _, candidate := range candidates {
// 		if candidate.Priority == "HIGH" || candidate.RecommendedAction == "BUY_NOW" || candidate.RecommendedAction == "BUY_BREAKOUT" {
// 			row := []string{
// 				candidate.Ticker,
// 				fmt.Sprintf("%.2f", candidate.CurrentPrice),
// 				fmt.Sprintf("%.2f", candidate.EntryStrategy.BuyPrice),
// 				fmt.Sprintf("%.2f", candidate.EntryStrategy.MaxBuyPrice),
// 				fmt.Sprintf("%.2f", candidate.EntryStrategy.StopLossPrice),
// 				fmt.Sprintf("%.2f", candidate.EntryStrategy.InitialTargetPrice),
// 				fmt.Sprintf("%.1f", candidate.EntryStrategy.RiskPercent),
// 				fmt.Sprintf("%.1f", candidate.EntryStrategy.RewardRiskRatio),
// 				candidate.RecommendedAction,
// 				candidate.Priority,
// 				fmt.Sprintf("%.1f", candidate.CurrentGrowth.GrowthPercentage),
// 				fmt.Sprintf("%d", candidate.CurrentGrowth.TradingDays),
// 				fmt.Sprintf("%d", candidate.MinerviniCriteria.CriteriaScore),
// 				fmt.Sprintf("%.1f", candidate.InvestmentScore),
// 				candidate.BuyReason,
// 				candidate.Notes,
// 				candidate.AddedToWatchlist.Format("2006-01-02"),
// 			}
// 			writer.Write(row)
// 		}
// 	}

// 	return nil
// }

// func (c *MarketStackClient) saveInvestmentResults(candidates []InvestmentCandidate) error {
// 	timestamp := time.Now().Format("2006-01-02_15-04-05")

// 	// Save JSON results
// 	jsonFilename := fmt.Sprintf("superperformance_data/investment_candidates_%s.json", timestamp)
// 	if err := os.MkdirAll("superperformance_data", 0755); err != nil {
// 		return err
// 	}

// 	file, err := os.Create(jsonFilename)
// 	if err != nil {
// 		return err
// 	}
// 	defer file.Close()

// 	encoder := json.NewEncoder(file)
// 	encoder.SetIndent("", "  ")
// 	if err := encoder.Encode(candidates); err != nil {
// 		return err
// 	}

// 	// Save CSV watchlist
// 	return c.saveWatchlistCSV(candidates)
// }

// // Results printing functions
// func (c *MarketStackClient) printInvestmentRecommendations(candidates []InvestmentCandidate) {
// 	fmt.Printf("\n" + "🎯" + strings.Repeat("=", 78) + "🎯\n")
// 	fmt.Printf("                    INVESTMENT RECOMMENDATIONS\n")
// 	fmt.Printf("🎯" + strings.Repeat("=", 78) + "🎯\n")

// 	buyNow := []InvestmentCandidate{}
// 	buyBreakout := []InvestmentCandidate{}
// 	watchList := []InvestmentCandidate{}

// 	for _, candidate := range candidates {
// 		switch candidate.RecommendedAction {
// 		case "BUY_NOW":
// 			buyNow = append(buyNow, candidate)
// 		case "BUY_BREAKOUT":
// 			buyBreakout = append(buyBreakout, candidate)
// 		case "WATCH":
// 			if candidate.Priority == "HIGH" || candidate.Priority == "MEDIUM" {
// 				watchList = append(watchList, candidate)
// 			}
// 		}
// 	}

// 	if len(buyNow) > 0 {
// 		fmt.Printf("\n🚀 BUY NOW (Market Orders) - %d stocks:\n", len(buyNow))
// 		for i, stock := range buyNow {
// 			fmt.Printf("  %d. %s - $%.2f | Buy: $%.2f | Stop: $%.2f | Target: $%.2f\n",
// 				i+1, stock.Ticker, stock.CurrentPrice,
// 				stock.EntryStrategy.BuyPrice, stock.EntryStrategy.StopLossPrice,
// 				stock.EntryStrategy.InitialTargetPrice)
// 			fmt.Printf("     Risk: %.1f%% | R:R %.1f:1 | Score: %.1f | %s\n",
// 				stock.EntryStrategy.RiskPercent, stock.EntryStrategy.RewardRiskRatio,
// 				stock.InvestmentScore, stock.BuyReason)
// 			if stock.VCPAnalysis != nil && stock.VCPAnalysis.IsVCP {
// 				fmt.Printf("     🎯 VCP Pattern: %s (%.1f%% base depth)\n",
// 					stock.VCPAnalysis.BaseType, stock.VCPAnalysis.BaseDepth)
// 			}
// 			fmt.Printf("\n")
// 		}
// 	}

// 	if len(buyBreakout) > 0 {
// 		fmt.Printf("\n⚡ BUY ON BREAKOUT (Limit Orders) - %d stocks:\n", len(buyBreakout))
// 		for i, stock := range buyBreakout {
// 			fmt.Printf("  %d. %s - $%.2f | Buy above: $%.2f | Max: $%.2f | Stop: $%.2f\n",
// 				i+1, stock.Ticker, stock.CurrentPrice,
// 				stock.EntryStrategy.BuyPrice, stock.EntryStrategy.MaxBuyPrice,
// 				stock.EntryStrategy.StopLossPrice)
// 			fmt.Printf("     Risk: %.1f%% | R:R %.1f:1 | Score: %.1f\n",
// 				stock.EntryStrategy.RiskPercent, stock.EntryStrategy.RewardRiskRatio,
// 				stock.InvestmentScore)
// 			fmt.Printf("     Reason: %s\n\n", stock.BuyReason)
// 		}
// 	}

// 	if len(watchList) > 0 {
// 		fmt.Printf("\n👀 WATCH LIST (Monitor for Entry) - %d stocks:\n", len(watchList))
// 		for i, stock := range watchList[:min(10, len(watchList))] {
// 			fmt.Printf("  %d. %s - $%.2f | Score: %.1f | %s\n",
// 				i+1, stock.Ticker, stock.CurrentPrice, stock.InvestmentScore, stock.Priority)
// 			fmt.Printf("     %s\n", stock.BuyReason)
// 		}
// 		if len(watchList) > 10 {
// 			fmt.Printf("     ... and %d more (see CSV file)\n", len(watchList)-10)
// 		}
// 	}

// 	if len(buyNow) == 0 && len(buyBreakout) == 0 {
// 		fmt.Printf("\n📊 No immediate buy recommendations found.\n")
// 		fmt.Printf("💡 Focus on watchlist stocks and wait for proper setups.\n")
// 	}

// 	fmt.Printf("\n📁 Results saved to:\n")
// 	fmt.Printf("   - watchlist.csv (for trading)\n")
// 	fmt.Printf("   - watchlist.json (for monitoring)\n")
// 	fmt.Printf("   - superperformance_data/investment_candidates_*.json (detailed analysis)\n")

// 	fmt.Printf("\n" + strings.Repeat("=", 80) + "\n")
// }

// func (c *MarketStackClient) printWatchlistResults(watchlist []WatchlistEntry, sellRecs []WatchlistEntry) {
// 	fmt.Printf("\n" + "👁️" + strings.Repeat("=", 78) + "👁️\n")
// 	fmt.Printf("                    WATCHLIST MONITORING RESULTS\n")
// 	fmt.Printf("👁️" + strings.Repeat("=", 78) + "👁️\n")

// 	activePositions := []WatchlistEntry{}
// 	waitingEntries := []WatchlistEntry{}

// 	for _, entry := range watchlist {
// 		if entry.Status == "ACTIVE" {
// 			activePositions = append(activePositions, entry)
// 		} else if entry.Status == "WAITING_ENTRY" {
// 			waitingEntries = append(waitingEntries, entry)
// 		}
// 	}

// 	if len(sellRecs) > 0 {
// 		fmt.Printf("\n🚨 SELL RECOMMENDATIONS - %d stocks:\n", len(sellRecs))
// 		for i, entry := range sellRecs {
// 			fmt.Printf("  %d. %s - $%.2f | P&L: %.1f%% | Reason: %s\n",
// 				i+1, entry.Ticker, entry.CurrentPrice,
// 				entry.UnrealizedPercent, entry.SellReason)
// 			if entry.MaxGainPercent > 0 {
// 				fmt.Printf("     Max gain was: %.1f%% | Days held: %d\n",
// 					entry.MaxGainPercent, entry.DaysHeld)
// 			}
// 		}
// 	}

// 	if len(activePositions) > 0 {
// 		fmt.Printf("\n📈 ACTIVE POSITIONS - %d stocks:\n", len(activePositions))
// 		totalPL := 0.0
// 		for i, entry := range activePositions {
// 			fmt.Printf("  %d. %s - $%.2f | P&L: %.1f%% ($%.2f) | Days: %d\n",
// 				i+1, entry.Ticker, entry.CurrentPrice,
// 				entry.UnrealizedPercent, entry.UnrealizedGainLoss, entry.DaysHeld)
// 			if entry.TrailingStopLoss > 0 {
// 				fmt.Printf("     Trailing Stop: $%.2f | Target: $%.2f\n",
// 					entry.TrailingStopLoss, entry.TargetPrice)
// 			}
// 			totalPL += entry.UnrealizedPercent
// 		}
// 		fmt.Printf("\n📊 Portfolio Average P&L: %.1f%%\n", totalPL/float64(len(activePositions)))
// 	}

// 	if len(waitingEntries) > 0 {
// 		fmt.Printf("\n⏳ WAITING FOR ENTRY - %d stocks:\n", len(waitingEntries))
// 		for i, entry := range waitingEntries {
// 			fmt.Printf("  %d. %s - $%.2f | Buy trigger: $%.2f | Stop: $%.2f\n",
// 				i+1, entry.Ticker, entry.CurrentPrice,
// 				entry.BuyTriggerPrice, entry.StopLoss)
// 		}
// 	}

// 	fmt.Printf("\n" + strings.Repeat("=", 80) + "\n")
// }

// // Utility functions
// func min(a, b int) int {
// 	if a < b {
// 		return a
// 	}
// 	return b
// }

// // Main command functions to run the system
// func (c *MarketStackClient) RunCompleteInvestmentSystem(symbols []string) error {
// 	fmt.Printf("[INFO] 🎯 RUNNING COMPLETE INVESTMENT SYSTEM\n")
// 	fmt.Printf("[INFO] Phase 1: Screening for investment candidates\n")

// 	if err := c.RunInvestmentScreener(symbols); err != nil {
// 		return fmt.Errorf("investment screening failed: %v", err)
// 	}

// 	fmt.Printf("\n[INFO] Phase 2: Monitoring existing watchlist\n")

// 	if err := c.RunWatchlistMonitor(); err != nil {
// 		return fmt.Errorf("watchlist monitoring failed: %v", err)
// 	}

// 	fmt.Printf("\n[INFO] ✅ Investment system analysis complete!\n")
// 	return nil
// }

// // Export watchlist management functions
// func (c *MarketStackClient) ExportWatchlistToCSV() error {
// 	watchlist, err := c.loadWatchlist()
// 	if err != nil {
// 		return err
// 	}

// 	filename := "current_positions.csv"
// 	file, err := os.Create(filename)
// 	if err != nil {
// 		return err
// 	}
// 	defer file.Close()

// 	writer := csv.NewWriter(file)
// 	defer writer.Flush()

// 	// Header
// 	header := []string{
// 		"Ticker", "Status", "Entry_Date", "Entry_Price", "Current_Price",
// 		"Stop_Loss", "Target_Price", "Unrealized_PL_Percent", "Days_Held",
// 		"Max_Gain_Percent", "Trailing_Stop", "Buy_Trigger", "Last_Analysis",
// 	}
// 	writer.Write(header)

// 	// Data
// 	for _, entry := range watchlist {
// 		row := []string{
// 			entry.Ticker,
// 			entry.Status,
// 			entry.EntryDate.Format("2006-01-02"),
// 			fmt.Sprintf("%.2f", entry.EntryPrice),
// 			fmt.Sprintf("%.2f", entry.CurrentPrice),
// 			fmt.Sprintf("%.2f", entry.StopLoss),
// 			fmt.Sprintf("%.2f", entry.TargetPrice),
// 			fmt.Sprintf("%.1f", entry.UnrealizedPercent),
// 			strconv.Itoa(entry.DaysHeld),
// 			fmt.Sprintf("%.1f", entry.MaxGainPercent),
// 			fmt.Sprintf("%.2f", entry.TrailingStopLoss),
// 			fmt.Sprintf("%.2f", entry.BuyTriggerPrice),
// 			entry.LastAnalysisDate.Format("2006-01-02"),
// 		}
// 		writer.Write(row)
// 	}

// 	fmt.Printf("[INFO] Watchlist exported to %s\n", filename)
// 	return nil
// }

// // Quick screening function for daily use
// func (c *MarketStackClient) QuickDailyScreen(symbols []string) error {
// 	fmt.Printf("[INFO] 🔍 QUICK DAILY INVESTMENT SCREEN\n")

// 	// Run just the investment screener (not full system)
// 	if err := c.RunInvestmentScreener(symbols); err != nil {
// 		return err
// 	}

// 	// Monitor existing positions
// 	if err := c.RunWatchlistMonitor(); err != nil {
// 		return err
// 	}

// 	// Export current state
// 	return c.ExportWatchlistToCSV()
// }
