package ep

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"
)

var (
	apiClient    *alpaca.Client
	marketClient *marketdata.Client
)

func init() {
	loadEnv()
	initClients()
}

// loadEnv walks up the directory tree from the current working directory
// until it finds a .env file and loads it.  This means tests running from
// a sub-package directory (e.g. cmd/avantai/ep/ep_watchlist/) will still
// find the .env at the project root.
func loadEnv() {
	dir, err := os.Getwd()
	if err != nil {
		log.Println("Warning: cannot determine working directory")
		return
	}

	for {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			if err := godotenv.Load(candidate); err != nil {
				log.Printf("Warning: found .env at %s but could not load it: %v", candidate, err)
			}
			return
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			log.Println("Warning: .env file not found in any parent directory")
			return
		}
		dir = parent
	}
}

// initClients builds the Alpaca clients from environment variables.
// Separated from init() so that missing credentials during tests do not
// call log.Fatal and kill the test binary at import time.
func initClients() {
	apiKey := os.Getenv("ALPACA_API_KEY")
	apiSecret := os.Getenv("ALPACA_SECRET_KEY")
	paperURL := os.Getenv("ALPACA_PAPER_URL")

	if apiKey == "" || apiSecret == "" {
		// Not fatal — tests run without real credentials.
		// Any real call through apiClient/marketClient will panic,
		// which is the correct signal that keys are missing in production.
		log.Println("⚠️  ALPACA_API_KEY / ALPACA_SECRET_KEY not set — Alpaca client calls will fail")
		return
	}

	apiClient = alpaca.NewClient(alpaca.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
		BaseURL:   paperURL,
	})

	marketClient = marketdata.NewClient(marketdata.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Account & Position helpers
// ─────────────────────────────────────────────────────────────────────────────

// AccountInfo holds key account information.
type AccountInfo struct {
	Status         string
	Cash           string
	BuyingPower    string
	PortfolioValue string
}

// GetAccountInfo retrieves key account information.
func GetAccountInfo() (*AccountInfo, error) {
	account, err := apiClient.GetAccount()
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	return &AccountInfo{
		Status:         string(account.Status),
		Cash:           account.Cash.StringFixed(2),
		BuyingPower:    account.BuyingPower.StringFixed(2),
		PortfolioValue: account.PortfolioValue.StringFixed(2),
	}, nil
}

// GetAsset retrieves the Alpaca Asset object for a given symbol and ensures it is tradable.
func GetAsset(symbol string) (*alpaca.Asset, error) {
	asset, err := apiClient.GetAsset(symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get asset %s: %w", symbol, err)
	}
	if !asset.Tradable {
		return nil, fmt.Errorf("%s is not tradable", symbol)
	}
	return asset, nil
}

// CheckAllOrders retrieves and logs all open orders.
func CheckAllOrders() ([]alpaca.Order, error) {
	orders, err := apiClient.GetOrders(alpaca.GetOrdersRequest{Status: "open"})
	if err != nil {
		return nil, fmt.Errorf("failed to get orders: %w", err)
	}

	fmt.Printf("\n=== All Open Orders (%d) ===\n", len(orders))
	for _, order := range orders {
		fmt.Printf("\nOrder ID: %s\n", order.ID)
		fmt.Printf("  Symbol: %s | Side: %s | Type: %s | Qty: %s | Status: %s\n",
			order.Symbol, order.Side, order.Type, order.Qty, order.Status)
		if order.LimitPrice != nil {
			fmt.Printf("  Limit Price: $%s\n", order.LimitPrice.StringFixed(2))
		}
		if order.StopPrice != nil {
			fmt.Printf("  Stop Price: $%s\n", order.StopPrice.StringFixed(2))
		}
	}
	return orders, nil
}

// CancelAllOrders cancels all open orders.
func CancelAllOrders() error {
	orders, err := apiClient.GetOrders(alpaca.GetOrdersRequest{Status: "open"})
	if err != nil {
		return fmt.Errorf("failed to get orders: %w", err)
	}
	for _, order := range orders {
		if err := apiClient.CancelOrder(order.ID); err != nil {
			log.Printf("Failed to cancel order %s: %v", order.ID, err)
		}
	}
	fmt.Printf("Cancelled %d open orders.\n", len(orders))
	return nil
}

// LiquidateAllPositions sells all positions at market price.
func LiquidateAllPositions() error {
	positions, err := apiClient.GetPositions()
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}
	for _, pos := range positions {
		qty, _ := pos.Qty.Float64()
		absQty := pos.Qty.Abs()
		side := alpaca.Sell
		if qty < 0 {
			side = alpaca.Buy
		}
		_, err := apiClient.PlaceOrder(alpaca.PlaceOrderRequest{
			Symbol:      pos.Symbol,
			Qty:         &absQty,
			Side:        side,
			Type:        alpaca.Market,
			TimeInForce: alpaca.Day,
		})
		if err != nil {
			log.Printf("Failed to liquidate %s: %v", pos.Symbol, err)
		} else {
			fmt.Printf("Liquidating %s: %s shares\n", pos.Symbol, pos.Qty.String())
		}
	}
	fmt.Printf("Liquidated %d positions.\n", len(positions))
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Order placement
// ─────────────────────────────────────────────────────────────────────────────

// PlaceEntryWithStop places a bracket limit buy order.
// If entryPrice is nil, it uses market price + $0.10.
// takeProfit is set to 7× entry by default — override as needed.
func PlaceEntryWithStop(symbol string, stopLoss float64, shares int, entryPrice *float64) (*alpaca.Order, error) {
	if _, err := GetAsset(symbol); err != nil {
		return nil, err
	}

	var entry float64
	if entryPrice == nil {
		trade, err := marketClient.GetLatestTrade(symbol, marketdata.GetLatestTradeRequest{})
		if err != nil {
			return nil, fmt.Errorf("failed to get latest trade for %s: %w", symbol, err)
		}
		entry = roundCents(trade.Price + 0.10)
	} else {
		entry = roundCents(*entryPrice)
	}

	stopLoss = roundCents(stopLoss)
	takeProfitPrice := roundCents(entry * 7)

	if stopLoss >= entry {
		return nil, fmt.Errorf("stop loss $%.2f must be below entry $%.2f", stopLoss, entry)
	}

	fmt.Printf("[%s] Entry: $%.2f | Stop: $%.2f | TP: $%.2f | Shares: %d\n",
		symbol, entry, stopLoss, takeProfitPrice, shares)

	qty := decimal.NewFromInt(int64(shares))
	limitPrice := decimal.NewFromFloat(entry)
	stopLossPrice := decimal.NewFromFloat(stopLoss)
	takeProfitLimit := decimal.NewFromFloat(takeProfitPrice)

	order, err := apiClient.PlaceOrder(alpaca.PlaceOrderRequest{
		Symbol:      symbol,
		Qty:         &qty,
		Side:        alpaca.Buy,
		Type:        alpaca.Limit,
		TimeInForce: alpaca.Day,
		LimitPrice:  &limitPrice,
		OrderClass:  alpaca.Bracket,
		StopLoss: &alpaca.StopLoss{
			StopPrice: &stopLossPrice,
		},
		TakeProfit: &alpaca.TakeProfit{
			LimitPrice: &takeProfitLimit,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to place bracket order for %s: %w", symbol, err)
	}

	fmt.Printf("[%s] ✅ Bracket order placed (ID: %s, status: %s)\n", symbol, order.ID, order.Status)
	return order, nil
}

// PlaceSellOrder places a sell order for a given symbol.
// If sellPrice is nil, a market order is placed; otherwise a limit order.
func PlaceSellOrder(symbol string, shares int, sellPrice *float64) (*alpaca.Order, error) {
	if _, err := GetAsset(symbol); err != nil {
		return nil, err
	}

	if shares <= 0 {
		return nil, fmt.Errorf("shares must be positive, got %d", shares)
	}

	qty := decimal.NewFromInt(int64(shares))

	orderReq := alpaca.PlaceOrderRequest{
		Symbol:      symbol,
		Qty:         &qty,
		Side:        alpaca.Sell,
		TimeInForce: alpaca.Day,
	}

	if sellPrice == nil {
		orderReq.Type = alpaca.Market
		log.Printf("[%s] Placing MARKET sell for %d shares", symbol, shares)
	} else {
		rounded := roundCents(*sellPrice)
		limitPrice := decimal.NewFromFloat(rounded)
		orderReq.Type = alpaca.Limit
		orderReq.LimitPrice = &limitPrice
		log.Printf("[%s] Placing LIMIT sell for %d shares @ $%.2f", symbol, shares, rounded)
	}

	order, err := apiClient.PlaceOrder(orderReq)
	if err != nil {
		return nil, fmt.Errorf("failed to place sell order for %s: %w", symbol, err)
	}

	log.Printf("[%s] ✅ Sell order placed (ID: %s, status: %s)", symbol, order.ID, order.Status)
	return order, nil
}

// CancelOrder cancels a single order by ID.
func CancelOrder(orderID string) error {
	if err := apiClient.CancelOrder(orderID); err != nil {
		return fmt.Errorf("failed to cancel order %s: %w", orderID, err)
	}
	log.Printf("Cancelled order %s", orderID)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

// roundCents rounds a float to 2 decimal places (avoids sub-penny issues).
func roundCents(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

// ─────────────────────────────────────────────────────────────────────────────
// main (manual test / smoke-test only — remove or gate before production)
// ─────────────────────────────────────────────────────────────────────────────

func main() {
	fmt.Println("=== Account Info ===")
	info, err := GetAccountInfo()
	if err != nil {
		log.Fatalf("Failed to get account info: %v", err)
	}
	fmt.Printf("Status: %s | Cash: $%s | Buying Power: $%s | Portfolio: $%s\n",
		info.Status, info.Cash, info.BuyingPower, info.PortfolioValue)

	fmt.Println("\n=== Cleaning Up ===")
	_ = CancelAllOrders()
	_ = LiquidateAllPositions()
	time.Sleep(3 * time.Second)

	fmt.Println("\n=== Test Orders ===")
	symbols := []string{"AAPL", "NVDA", "AMD"}
	for _, sym := range symbols {
		trade, err := marketClient.GetLatestTrade(sym, marketdata.GetLatestTradeRequest{})
		if err != nil {
			log.Printf("Skip %s — price fetch error: %v", sym, err)
			continue
		}
		stopLoss := roundCents(trade.Price * 0.95)
		order, err := PlaceEntryWithStop(sym, stopLoss, 10, nil)
		if err != nil {
			log.Printf("✗ %s order error: %v", sym, err)
			continue
		}
		fmt.Printf("✓ %s order placed (ID: %s)\n", sym, order.ID)
	}

	time.Sleep(2 * time.Second)
	_, _ = CheckAllOrders()
}
