package main

// import (
// 	"avantai/pkg/superperformance"
// 	"encoding/json"
// 	"fmt"
// 	"log"
// 	"net/http"
// 	"os"
// 	"time"
// )

// // Trading configuration
// type TradingConfig struct {
// 	// Risk Management
// 	MaxRiskPerPosition  float64 `json:"max_risk_per_position"` // 2% default
// 	MaxPortfolioRisk    float64 `json:"max_portfolio_risk"`    // 10% total risk
// 	TrailingStopPercent float64 `json:"trailing_stop_percent"` // 20% for growth stocks
// 	ProfitTakingPercent float64 `json:"profit_taking_percent"` // 25% initial target
// 	MaxPositionSize     float64 `json:"max_position_size"`     // 5% of portfolio max

// 	// Entry Criteria
// 	MinInvestmentScore float64 `json:"min_investment_score"`  // 6.0 minimum
// 	RequireMinervini   bool    `json:"require_minervini"`     // true
// 	RequireVCP         bool    `json:"require_vcp"`           // false (optional)
// 	MaxRiskPercent     float64 `json:"max_risk_percent"`      // 8% max risk per trade
// 	MinRewardRiskRatio float64 `json:"min_reward_risk_ratio"` // 2.5:1 minimum

// 	// Position Management
// 	PartialProfitAt   float64 `json:"partial_profit_at"`   // 20% - take 1/3 off
// 	TrailingStopStart float64 `json:"trailing_stop_start"` // 15% - start trailing
// 	MaxHoldingDays    int     `json:"max_holding_days"`    // 180 days max

// 	// Market Conditions
// 	RequireBullMarket bool    `json:"require_bull_market"` // Require favorable conditions
// 	MinStocksAboveMA  float64 `json:"min_stocks_above_ma"` // 60% of stocks above 200-day MA

// 	// Data Sources
// 	MarketStackAPIKey string `json:"marketstack_api_key"`
// 	UpdateFrequency   string `json:"update_frequency"` // "daily", "weekly"
// 	BackupWatchlist   bool   `json:"backup_watchlist"` // true
// }

// func NewMarketStackClient(apiKey string) (*superperformance.MarketStackClient, error) {
// 	fmt.Printf("[DEBUG] Creating new MarketStack client\n")

// 	// Validate API key
// 	if apiKey == "" {
// 		fmt.Printf("[ERROR] API key is empty\n")
// 		return nil, fmt.Errorf("API key cannot be empty")
// 	}

// 	// Create rate limiter channel
// 	rateLimiter := make(chan struct{}, superperformance.MAX_CONCURRENT)
// 	for i := 0; i < superperformance.MAX_CONCURRENT; i++ {
// 		rateLimiter <- struct{}{}
// 	}
// 	fmt.Printf("[DEBUG] Rate limiter initialized with %d concurrent slots\n", superperformance.MAX_CONCURRENT)

// 	// Create HTTP client with timeout
// 	httpClient := &http.Client{Timeout: 30 * time.Second}
// 	fmt.Printf("[DEBUG] HTTP client created with 30s timeout\n")

// 	client := &superperformance.MarketStackClient{
// 		APIKey:     apiKey,
// 		HTTPClient: httpClient,
// 		RateLimit:  rateLimiter,
// 	}

// 	fmt.Printf("[INFO] MarketStack client successfully created\n")
// 	return client, nil
// }

// // Default configuration
// func NewTradingConfig() *TradingConfig {
// 	return &TradingConfig{
// 		MaxRiskPerPosition:  2.0,
// 		MaxPortfolioRisk:    10.0,
// 		TrailingStopPercent: 20.0,
// 		ProfitTakingPercent: 25.0,
// 		MaxPositionSize:     5.0,
// 		MinInvestmentScore:  6.0,
// 		RequireMinervini:    true,
// 		RequireVCP:          false,
// 		MaxRiskPercent:      8.0,
// 		MinRewardRiskRatio:  2.5,
// 		PartialProfitAt:     20.0,
// 		TrailingStopStart:   15.0,
// 		MaxHoldingDays:      180,
// 		RequireBullMarket:   false,
// 		MinStocksAboveMA:    60.0,
// 		UpdateFrequency:     "daily",
// 		BackupWatchlist:     true,
// 	}
// }

// func (c *TradingConfig) Save(filename string) error {
// 	file, err := os.Create(filename)
// 	if err != nil {
// 		return err
// 	}
// 	defer file.Close()

// 	encoder := json.NewEncoder(file)
// 	encoder.SetIndent("", "  ")
// 	return encoder.Encode(c)
// }

// func LoadTradingConfig(filename string) (*TradingConfig, error) {
// 	file, err := os.Open(filename)
// 	if err != nil {
// 		if os.IsNotExist(err) {
// 			// Create default config
// 			config := NewTradingConfig()
// 			config.Save(filename)
// 			return config, nil
// 		}
// 		return nil, err
// 	}
// 	defer file.Close()

// 	var config TradingConfig
// 	decoder := json.NewDecoder(file)
// 	err = decoder.Decode(&config)
// 	return &config, err
// }

// // Position sizing calculator
// type PositionSizer struct {
// 	PortfolioValue float64
// 	Config         *TradingConfig
// }

// func NewPositionSizer(portfolioValue float64, config *TradingConfig) *PositionSizer {
// 	return &PositionSizer{
// 		PortfolioValue: portfolioValue,
// 		Config:         config,
// 	}
// }

// func (ps *PositionSizer) CalculatePositionSize(entryPrice, stopPrice float64) (shares int, dollarsAtRisk float64, warnings []string) {
// 	var warns []string

// 	// Calculate risk per share
// 	riskPerShare := entryPrice - stopPrice
// 	if riskPerShare <= 0 {
// 		warns = append(warns, "Invalid stop loss - must be below entry price")
// 		return 0, 0, warns
// 	}

// 	// Calculate max dollars to risk (2% of portfolio)
// 	maxDollarsToRisk := ps.PortfolioValue * (ps.Config.MaxRiskPerPosition / 100.0)

// 	// Calculate shares based on risk
// 	sharesBasedOnRisk := int(maxDollarsToRisk / riskPerShare)

// 	// Calculate max shares based on position size limit (5% of portfolio)
// 	maxPositionValue := ps.PortfolioValue * (ps.Config.MaxPositionSize / 100.0)
// 	maxSharesBasedOnSize := int(maxPositionValue / entryPrice)

// 	// Use the smaller of the two
// 	shares = sharesBasedOnRisk
// 	if maxSharesBasedOnSize < sharesBasedOnRisk {
// 		shares = maxSharesBasedOnSize
// 		warns = append(warns, "Position size limited by max position size rule")
// 	}

// 	dollarsAtRisk = float64(shares) * riskPerShare
// 	riskPercent := dollarsAtRisk / ps.PortfolioValue * 100

// 	// Warnings
// 	if riskPercent > ps.Config.MaxRiskPerPosition {
// 		warns = append(warns, fmt.Sprintf("Risk %.1f%% exceeds max %.1f%%", riskPercent, ps.Config.MaxRiskPerPosition))
// 	}

// 	if shares == 0 {
// 		warns = append(warns, "Position size too small - consider larger portfolio or smaller position")
// 	}

// 	return shares, dollarsAtRisk, warns
// }

// // Market condition analyzer
// type MarketAnalyzer struct {
// 	client *superperformance.MarketStackClient
// }

// func NewMarketAnalyzer(client *superperformance.MarketStackClient) *MarketAnalyzer {
// 	return &MarketAnalyzer{client: client}
// }

// func (ma *MarketAnalyzer) AnalyzeMarketConditions(symbols []string) (*MarketCondition, error) {
// 	// Analyze major indices and broad market
// 	indices := []string{"SPY", "QQQ", "DIA", "IWM"} // S&P 500, NASDAQ, Dow, Russell 2000

// 	conditionsScore := 0.0
// 	totalStocks := 0
// 	stocksAbove200MA := 0

// 	for _, symbol := range indices {
// 		dateTo := time.Now().Format("2006-01-02")
// 		dateFrom := time.Now().AddDate(0, -8, 0).Format("2006-01-02") // 8 months

// 		data, err := ma.client.GetStockData(symbol, dateFrom, dateTo)
// 		if err != nil {
// 			continue
// 		}

// 		if len(data) >= 200 {
// 			currentPrice := data[len(data)-1].Close
// 			sma200 := ma.client.CalculateSMA(data, 200)

// 			if currentPrice > sma200 {
// 				conditionsScore += 25.0 // Each index above 200-day MA = 25 points
// 			}
// 		}
// 	}

// 	// Check broader market participation
// 	sampleSize := min(len(symbols), 100) // Check up to 100 stocks
// 	for i := 0; i < sampleSize; i++ {
// 		dateTo := time.Now().Format("2006-01-02")
// 		dateFrom := time.Now().AddDate(0, -8, 0).Format("2006-01-02")

// 		data, err := ma.client.GetStockData(symbols[i], dateFrom, dateTo)
// 		if err != nil {
// 			continue
// 		}

// 		if len(data) >= 200 {
// 			totalStocks++
// 			currentPrice := data[len(data)-1].Close
// 			sma200 := ma.client.CalculateSMA(data, 200)

// 			if currentPrice > sma200 {
// 				stocksAbove200MA++
// 			}
// 		}

// 		if i%10 == 0 {
// 			fmt.Printf("[PROGRESS] Market analysis: %d/%d stocks checked\n", i+1, sampleSize)
// 		}
// 	}

// 	participationRate := 0.0
// 	if totalStocks > 0 {
// 		participationRate = float64(stocksAbove200MA) / float64(totalStocks) * 100
// 	}

// 	// Determine market condition
// 	condition := "NEUTRAL"
// 	if conditionsScore >= 75 && participationRate >= 60 {
// 		condition = "BULL"
// 	} else if conditionsScore <= 25 || participationRate <= 40 {
// 		condition = "BEAR"
// 	}

// 	return &MarketCondition{
// 		Condition:          condition,
// 		ConditionsScore:    conditionsScore,
// 		ParticipationRate:  participationRate,
// 		IndicesAbove200MA:  int(conditionsScore / 25),
// 		StocksAbove200MA:   stocksAbove200MA,
// 		TotalStocksChecked: totalStocks,
// 		LastUpdated:        time.Now(),
// 		RecommendedAction:  ma.getMarketRecommendation(condition, conditionsScore, participationRate),
// 	}, nil
// }

// type MarketCondition struct {
// 	Condition          string    `json:"condition"`           // BULL, BEAR, NEUTRAL
// 	ConditionsScore    float64   `json:"conditions_score"`    // 0-100
// 	ParticipationRate  float64   `json:"participation_rate"`  // % stocks above 200-day MA
// 	IndicesAbove200MA  int       `json:"indices_above_200ma"` // How many indices above 200-day MA
// 	StocksAbove200MA   int       `json:"stocks_above_200ma"`
// 	TotalStocksChecked int       `json:"total_stocks_checked"`
// 	LastUpdated        time.Time `json:"last_updated"`
// 	RecommendedAction  string    `json:"recommended_action"`
// }

// func (ma *MarketAnalyzer) getMarketRecommendation(condition string, score, participation float64) string {
// 	switch condition {
// 	case "BULL":
// 		return "AGGRESSIVE - Increase position sizes, focus on breakouts"
// 	case "BEAR":
// 		return "DEFENSIVE - Reduce exposure, tighten stops, focus on cash"
// 	default:
// 		if score >= 50 && participation >= 50 {
// 			return "CAUTIOUS - Small positions, quick profits"
// 		}
// 		return "WAIT - Preserve capital, minimal new positions"
// 	}
// }

// // Complete trading system example
// func RunCompleteTradingDay() {
// 	fmt.Printf("🌅 DAILY TRADING SYSTEM STARTUP\n")
// 	fmt.Printf("Time: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

// 	// Load configuration
// 	config, err := LoadTradingConfig("trading_config.json")
// 	if err != nil {
// 		log.Printf("Warning: Using default config: %v", err)
// 		config = NewTradingConfig()
// 		config.Save("trading_config.json")
// 	}

// 	// Initialize client
// 	client, err := NewMarketStackClient(config.MarketStackAPIKey)
// 	if err != nil {
// 		log.Fatal("Failed to initialize MarketStack client - check API key")
// 	}

// 	// Step 1: Check market conditions
// 	fmt.Printf("📊 Step 1: Analyzing market conditions\n")
// 	marketAnalyzer := NewMarketAnalyzer(client)

// 	// Use a broader symbol set for market analysis
// 	marketSymbols := []string{
// 		"SPY", "QQQ", "DIA", "IWM", // Major indices
// 		"XLK", "XLF", "XLE", "XLV", "XLI", "XLC", "XLB", "XLRE", "XLU", "XLP", "XLY", // Sector ETFs
// 	}

// 	marketCondition, err := marketAnalyzer.AnalyzeMarketConditions(marketSymbols)
// 	if err != nil {
// 		log.Printf("Market analysis failed: %v", err)
// 	} else {
// 		fmt.Printf("Market Condition: %s (%.0f/100)\n", marketCondition.Condition, marketCondition.ConditionsScore)
// 		fmt.Printf("Participation: %.1f%% stocks above 200-day MA\n", marketCondition.ParticipationRate)
// 		fmt.Printf("Recommendation: %s\n\n", marketCondition.RecommendedAction)
// 	}

// 	// Step 2: Monitor existing positions
// 	fmt.Printf("👁️ Step 2: Monitoring existing positions\n")
// 	if err := client.RunWatchlistMonitor(); err != nil {
// 		log.Printf("Watchlist monitoring failed: %v", err)
// 	}

// 	// Step 3: Screen for new opportunities (if market conditions allow)
// 	if marketCondition == nil || marketCondition.Condition != "BEAR" {
// 		fmt.Printf("\n🔍 Step 3: Screening for new opportunities\n")

// 		// Your full symbol universe
// 		allSymbols := []string{
// 			// Technology Leaders
// 			"AAPL", "MSFT", "GOOGL", "AMZN", "META", "TSLA", "NVDA", "AMD", "CRM", "ADBE",
// 			"NFLX", "PYPL", "SHOP", "SQ", "ROKU", "ZM", "DOCU", "OKTA", "SNOW", "PLTR",

// 			// Biotech/Healthcare
// 			"MRNA", "PFE", "JNJ", "ABBV", "TMO", "DHR", "AMGN", "GILD", "BIIB", "REGN",
// 			"ILMN", "VRTX", "ISRG", "DXCM", "EW", "ZBH", "SYK", "MDT", "ABT", "BAX",

// 			// Growth Stocks
// 			"TSLA", "SQ", "ROKU", "PINS", "SNAP", "TWTR", "UBER", "LYFT", "ABNB", "DASH",
// 			"COIN", "HOOD", "AFRM", "UPST", "SOFI", "OPEN", "RBLX", "U", "DKNG", "PENN",

// 			// Add more sectors as needed...
// 		}

// 		if err := client.QuickDailyScreen(allSymbols); err != nil {
// 			log.Printf("Daily screening failed: %v", err)
// 		}
// 	} else {
// 		fmt.Printf("\n⚠️ Step 3: Skipping new screening due to BEAR market conditions\n")
// 	}

// 	// Step 4: Export results
// 	fmt.Printf("\n📊 Step 4: Exporting results\n")
// 	if err := client.ExportWatchlistToCSV(); err != nil {
// 		log.Printf("Export failed: %v", err)
// 	}

// 	fmt.Printf("\n✅ Daily trading system complete!\n")
// 	fmt.Printf("📁 Check these files for trading decisions:\n")
// 	fmt.Printf("   - watchlist.csv (new opportunities)\n")
// 	fmt.Printf("   - current_positions.csv (position management)\n")
// 	fmt.Printf("   - trading_config.json (system settings)\n")
// }

// // Portfolio manager for calculating position sizes
// type PortfolioManager struct {
// 	TotalValue    float64
// 	CashAvailable float64
// 	Positions     []Position
// 	Config        *TradingConfig
// }

// type Position struct {
// 	Ticker       string
// 	Shares       int
// 	EntryPrice   float64
// 	CurrentPrice float64
// 	StopPrice    float64
// 	Value        float64
// 	PctOfPort    float64
// 	UnrealizedPL float64
// }

// func (pm *PortfolioManager) CalculateOrderSize(ticker string, entryPrice, stopPrice float64) *OrderRecommendation {
// 	// Calculate risk per share
// 	riskPerShare := entryPrice - stopPrice

// 	// Max dollars to risk (2% of portfolio)
// 	maxRisk := pm.TotalValue * (pm.Config.MaxRiskPerPosition / 100.0)

// 	// Calculate shares
// 	shares := int(maxRisk / riskPerShare)

// 	// Check against max position size
// 	maxPositionValue := pm.TotalValue * (pm.Config.MaxPositionSize / 100.0)
// 	maxShares := int(maxPositionValue / entryPrice)

// 	if shares > maxShares {
// 		shares = maxShares
// 	}

// 	totalCost := float64(shares) * entryPrice
// 	actualRisk := float64(shares) * riskPerShare
// 	riskPercent := actualRisk / pm.TotalValue * 100

// 	return &OrderRecommendation{
// 		Ticker:          ticker,
// 		Shares:          shares,
// 		EntryPrice:      entryPrice,
// 		StopPrice:       stopPrice,
// 		TotalCost:       totalCost,
// 		DollarsAtRisk:   actualRisk,
// 		RiskPercent:     riskPercent,
// 		PositionPercent: totalCost / pm.TotalValue * 100,
// 		OrderType:       pm.determineOrderType(entryPrice, stopPrice),
// 		ValidUntil:      time.Now().Add(3 * 24 * time.Hour), // 3 days
// 	}
// }

// type OrderRecommendation struct {
// 	Ticker          string    `json:"ticker"`
// 	Shares          int       `json:"shares"`
// 	EntryPrice      float64   `json:"entry_price"`
// 	StopPrice       float64   `json:"stop_price"`
// 	TotalCost       float64   `json:"total_cost"`
// 	DollarsAtRisk   float64   `json:"dollars_at_risk"`
// 	RiskPercent     float64   `json:"risk_percent"`
// 	PositionPercent float64   `json:"position_percent"`
// 	OrderType       string    `json:"order_type"`
// 	ValidUntil      time.Time `json:"valid_until"`
// }

// func (pm *PortfolioManager) determineOrderType(entryPrice, stopPrice float64) string {
// 	riskPercent := (entryPrice - stopPrice) / entryPrice * 100

// 	if riskPercent <= 5 {
// 		return "MARKET"
// 	} else if riskPercent <= 8 {
// 		return "LIMIT"
// 	} else {
// 		return "WAIT" // Too risky
// 	}
// }

// // Setup script generator
// func GenerateSetupScript() error {
// 	setupScript := `#!/bin/bash

// # Investment System Setup Script
// echo "🎯 Setting up Investment System..."

// # Create directories
// mkdir -p superperformance_data
// mkdir -p backups

// # Create environment file template
// cat > .env << EOF
// MARKETSTACK_API_KEY=your_api_key_here
// PORTFOLIO_VALUE=100000
// EOF

// # Create cron job for daily monitoring
// echo "📅 Setting up daily monitoring (runs at 6 PM Eastern)..."
// (crontab -l 2>/dev/null; echo "0 18 * * 1-5 cd $(pwd) && go run daily_monitor.go") | crontab -

// # Create trading config
// echo "⚙️ Creating default trading configuration..."

// echo "✅ Setup complete!"
// echo ""
// echo "Next steps:"
// echo "1. Add your MarketStack API key to .env file"
// echo "2. Set your portfolio value in .env"
// echo "3. Run: go run main.go"
// echo "4. Import watchlist.csv into your broker"
// echo "5. Daily monitoring will run automatically at 6 PM"
// `

// 	return os.WriteFile("setup.sh", []byte(setupScript), 0755)
// }

// // Daily monitoring script (separate executable)
// func GenerateDailyMonitorScript() error {
// 	monitorScript := `package main

// import (
// 	"fmt"
// 	"log"
// 	"os"
// 	"strconv"
	
// 	"github.com/joho/godotenv"
// )

// func main() {
// 	// Load environment variables
// 	err := godotenv.Load()
// 	if err != nil {
// 		log.Printf("Warning: .env file not found")
// 	}

// 	apiKey := os.Getenv("MARKETSTACK_API_KEY")
// 	if apiKey == "" {
// 		log.Fatal("MARKETSTACK_API_KEY not set")
// 	}

// 	portfolioValue, _ := strconv.ParseFloat(os.Getenv("PORTFOLIO_VALUE"), 64)
// 	if portfolioValue == 0 {
// 		portfolioValue = 100000 // Default $100k
// 	}

// 	client := superperformance.NewMarketStackClient(apiKey)
// 	config, _ := superperformance.LoadTradingConfig("trading_config.json")

// 	fmt.Printf("👁️ Daily Monitoring - Portfolio: $%.0f\n", portfolioValue)

// 	// Monitor existing positions
// 	if err := client.RunWatchlistMonitor(); err != nil {
// 		log.Printf("Monitoring failed: %v", err)
// 	}

// 	// Export updated positions
// 	if err := client.ExportWatchlistToCSV(); err != nil {
// 		log.Printf("Export failed: %v", err)
// 	}

// 	// Create position size recommendations for any new opportunities
// 	portfolio := &superperformance.PortfolioManager{
// 		TotalValue: portfolioValue,
// 		Config:     config,
// 	}

// 	fmt.Printf("✅ Daily monitoring complete - check current_positions.csv\n")
// }
// `

// 	return os.WriteFile("daily_monitor.go", []byte(monitorScript), 0644)
// }

// func init() {
// 	// Generate setup scripts when package is imported
// 	GenerateSetupScript()
// 	GenerateDailyMonitorScript()
// }
