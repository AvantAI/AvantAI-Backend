package main

// import (
// 	"avantai/pkg/superperformance"
// 	"fmt"
// 	"log"
// 	"net/http"
// 	"os"
// 	"time"

// 	"github.com/joho/godotenv"
// )

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

// func setupEnvironment() error {
// 	fmt.Printf("[STEP 1] Setting up environment\n")

// 	if err := godotenv.Load(); err != nil {
// 		fmt.Printf("[WARNING] Could not load .env file: %v\n", err)
// 	}

// 	apiKey := os.Getenv("MARKETSTACK_TOKEN")
// 	if apiKey == "" {
// 		return fmt.Errorf("MARKETSTACK_TOKEN environment variable not set")
// 	}

// 	fmt.Printf("[SUCCESS] Environment setup complete\n")
// 	return nil
// }

// func main() {
// 	// Load environment and validate setup
// 	if err := setupEnvironment(); err != nil {
// 		log.Fatalf("[FATAL] Setup failed: %v", err)
// 	}

// 	// Initialize your MarketStack client
// 	apiKey := os.Getenv("MARKETSTACK_TOKEN")
// 	if apiKey == "" {
// 		log.Fatal("MARKETSTACK_TOKEN environment variable not set")
// 	}

// 	client, err := NewMarketStackClient(apiKey)
// 	if err != nil {
// 		log.Fatalf("Failed to create MarketStack client: %v", err)
// 	}

// 	// Define your symbol universe (example symbols - replace with your preferred list)
// 	symbols := []string{
// 		// Technology
// 		"AAPL", "MSFT", "GOOGL", "AMZN", "META", "TSLA", "NVDA", "AMD", "CRM", "ADBE",
// 		"NFLX", "PYPL", "INTC", "CSCO", "ORCL", "IBM", "QCOM", "TXN", "AVGO", "MU",

// 		// Growth/Biotech
// 		"MRNA", "PFE", "JNJ", "ABBV", "TMO", "DHR", "AMGN", "GILD", "BIIB", "REGN",
// 		"ILMN", "VRTX", "CELG", "MYL", "TEVA", "NVO", "RHHBY", "SNY", "GSK", "AZN",

// 		// Consumer/Retail
// 		"WMT", "HD", "NKE", "SBUX", "MCD", "DIS", "CMCSA", "VZ", "T", "NFLX",
// 		"COST", "TGT", "LOW", "TJX", "BBY", "GPS", "M", "JWN", "KSS", "DKS",

// 		// Financial
// 		"JPM", "BAC", "WFC", "C", "GS", "MS", "AXP", "BRK.B", "V", "MA",
// 		"BLK", "SPGI", "CME", "ICE", "NDAQ", "MCO", "MSCI", "TRV", "AIG", "MET",

// 		// Energy/Industrial
// 		"XOM", "CVX", "COP", "EOG", "SLB", "HAL", "BKR", "MPC", "VLO", "PSX",
// 		"BA", "CAT", "DE", "GE", "HON", "MMM", "UTX", "LMT", "RTX", "NOC",

// 		// Add your own symbols here...
// 	}

// 	// Run the complete investment system
// 	fmt.Printf("🎯 Starting Investment Analysis System\n")
// 	fmt.Printf("📊 Analyzing %d symbols for investment opportunities\n\n", len(symbols))

// 	if err := client.RunCompleteInvestmentSystem(symbols); err != nil {
// 		log.Fatalf("Investment system failed: %v", err)
// 	}

// 	fmt.Printf("\n✅ Analysis complete! Check these files:\n")
// 	fmt.Printf("   📋 watchlist.csv - Import into your broker for trading\n")
// 	fmt.Printf("   📊 watchlist.json - Complete monitoring data\n")
// 	fmt.Printf("   📁 superperformance_data/ - Detailed analysis results\n")
// }

// // Daily monitoring script - run this every day after market close
// func runDailyMonitoring() {
// 	apiKey := os.Getenv("MARKETSTACK_TOKEN")
// 	if apiKey == "" {
// 		log.Fatal("MARKETSTACK_TOKEN environment variable not set")
// 	}

// 	client, err := NewMarketStackClient(apiKey)
// 	if err != nil {
// 		log.Fatalf("[FATAL] Failed to create client: %v", err)
// 	}

// 	fmt.Printf("👁️ Running Daily Watchlist Monitor\n")

// 	if err := client.RunWatchlistMonitor(); err != nil {
// 		log.Fatalf("Daily monitoring failed: %v", err)
// 	}

// 	if err := client.ExportWatchlistToCSV(); err != nil {
// 		log.Printf("Warning: Failed to export CSV: %v", err)
// 	}

// 	fmt.Printf("✅ Daily monitoring complete!\n")
// }

// // Quick screen for new opportunities - run this weekly
// func runWeeklyScreen() {
// 	apiKey := os.Getenv("MARKETSTACK_TOKEN")
// 	if apiKey == "" {
// 		log.Fatal("MARKETSTACK_TOKEN environment variable not set")
// 	}

// 	client, err := NewMarketStackClient(apiKey)
// 	if err != nil {
// 		log.Fatalf("[FATAL] Failed to create client: %v", err)
// 	}

// 	// Focused symbol list for weekly screening (top movers, sector leaders, etc.)
// 	weeklySymbols := []string{
// 		// Add symbols from:
// 		// - Sector ETF top holdings
// 		// - Recent earnings winners
// 		// - Breakout alerts from other sources
// 		// - IPOs from last 6 months
// 		"SYMBOL1", "SYMBOL2", // Replace with actual symbols
// 	}

// 	fmt.Printf("🔍 Running Weekly Screen for New Opportunities\n")

// 	if err := client.QuickDailyScreen(weeklySymbols); err != nil {
// 		log.Fatalf("Weekly screening failed: %v", err)
// 	}

// 	fmt.Printf("✅ Weekly screening complete!\n")
// }

// /*
// USAGE INSTRUCTIONS:

// 1. INITIAL SETUP:
//    go run main.go
//    - This runs the complete investment system
//    - Creates watchlist.csv with buy recommendations
//    - Sets up watchlist.json for monitoring

// 2. DAILY MONITORING (run every day after market close):
//    - Uncomment runDailyMonitoring() in main()
//    - This checks all watchlist stocks for sell signals
//    - Updates trailing stops and position management

// 3. WEEKLY SCREENING (run every weekend):
//    - Uncomment runWeeklyScreen() in main()
//    - Add new symbol candidates to weeklySymbols
//    - Finds new investment opportunities

// 4. TRADING WORKFLOW:

//    A. BUY NOW stocks:
//       - Place market orders immediately
//       - Use the provided buy price as reference
//       - Set stop loss orders at specified levels
//       - Set initial profit targets

//    B. BUY BREAKOUT stocks:
//       - Place limit orders above breakout price
//       - Cancel if not filled within 3 days
//       - Use provided max buy price as limit

//    C. WATCH LIST stocks:
//       - Monitor for breakout setups
//       - Wait for proper entry signals
//       - Re-analyze weekly

// 5. SELL SIGNALS (automated monitoring):
//    - Stop loss hit
//    - Trailing stop hit
//    - Minervini template breakdown
//    - Growth move ended
//    - 50-day MA breakdown
//    - Large single-day decline (8%+)
//    - Holding too long without progress

// 6. FILES CREATED:
//    - watchlist.csv: Import into broker for orders
//    - watchlist.json: Complete monitoring data
//    - current_positions.csv: Active position summary
//    - superperformance_data/: Detailed analysis archives

// RISK MANAGEMENT RULES:
// - Never risk more than 2% of portfolio on single position
// - Use proper position sizing based on stop loss distance
// - Always use stop losses - no exceptions
// - Take partial profits at 20-25% gains
// - Trail stops aggressively on big winners
// - Cut losses quickly, let winners run
// */
