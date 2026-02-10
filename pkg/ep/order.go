package ep

import (
	"fmt"
	"log"
	"os"
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
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found")
	}

	apiKey := os.Getenv("ALPACA_API_KEY")
	apiSecret := os.Getenv("ALPACA_SECRET_KEY")
	baseURL := os.Getenv("ALPACA_BASE_URL")

	if apiKey == "" || apiSecret == "" {
		log.Fatal("ALPACA_API_KEY and ALPACA_SECRET_KEY must be set")
	}

	// Initialize Alpaca client
	apiClient = alpaca.NewClient(alpaca.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
		BaseURL:   baseURL,
	})

	// Initialize market data client
	marketClient = marketdata.NewClient(marketdata.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
	})
}

// CheckAllOrders retrieves and displays all open orders with detailed information
func CheckAllOrders() ([]alpaca.Order, error) {
	orders, err := apiClient.GetOrders(alpaca.GetOrdersRequest{
		Status: "open",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get orders: %w", err)
	}

	fmt.Printf("\n=== All Open Orders (%d) ===\n", len(orders))
	for _, order := range orders {
		fmt.Printf("\nOrder ID: %s\n", order.ID)
		fmt.Printf("  Symbol: %s\n", order.Symbol)
		fmt.Printf("  Side: %s\n", order.Side)
		fmt.Printf("  Type: %s\n", order.Type)
		fmt.Printf("  Qty: %s\n", order.Qty)
		fmt.Printf("  Status: %s\n", order.Status)
		fmt.Printf("  Order Class: %s\n", order.OrderClass)

		if order.LimitPrice != nil {
			fmt.Printf("  Limit Price: $%s\n", order.LimitPrice.StringFixed(2))
		}
		if order.StopPrice != nil {
			fmt.Printf("  Stop Price: $%s\n", order.StopPrice.StringFixed(2))
		}
	}

	return orders, nil
}

// GetAsset retrieves the Alpaca Asset object for a given symbol and ensures it is tradable
func GetAsset(symbol string) (*alpaca.Asset, error) {
	asset, err := apiClient.GetAsset(symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get asset: %w", err)
	}

	if !asset.Tradable {
		return nil, fmt.Errorf("%s is not tradable", symbol)
	}

	return asset, nil
}

// CancelAllOrders cancels all open orders in the Alpaca account
func CancelAllOrders() error {
	orders, err := apiClient.GetOrders(alpaca.GetOrdersRequest{
		Status: "open",
	})
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

// LiquidateAllPositions sells all positions in the Alpaca account at market price
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

		fmt.Printf("Liquidating %s: %s shares (side: %s)\n", pos.Symbol, pos.Qty.String(), pos.Side)

		_, err := apiClient.PlaceOrder(alpaca.PlaceOrderRequest{
			Symbol:      pos.Symbol,
			Qty:         &absQty,
			Side:        side,
			Type:        alpaca.Market,
			TimeInForce: alpaca.Day,
		})
		if err != nil {
			log.Printf("Failed to liquidate %s: %v", pos.Symbol, err)
		}
	}

	fmt.Printf("Liquidated %d positions.\n", len(positions))
	return nil
}

// AccountInfo holds key account information
type AccountInfo struct {
	Status         string
	Cash           string
	BuyingPower    string
	PortfolioValue string
}

// GetAccountInfo retrieves key account information
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

// PlaceEntryWithStop places a bracket buy order with entry, stop-loss, and take-profit
func PlaceEntryWithStop(symbol string, stopLoss float64, shares int, entryPrice *float64) (*alpaca.Order, error) {
	// Check if asset is tradable
	_, err := GetAsset(symbol)
	if err != nil {
		return nil, err
	}

	var entry float64
	var marketPrice float64

	// Fetch current market price if entry_price not provided
	if entryPrice == nil {
		trades, err := marketClient.GetLatestTrade(symbol, marketdata.GetLatestTradeRequest{})
		if err != nil {
			return nil, fmt.Errorf("failed to get latest trade: %w", err)
		}
		marketPrice = trades.Price
		entry = marketPrice + 0.1 // Buy at $0.1 above market
	} else {
		entry = *entryPrice
		marketPrice = entry - 0.1
	}

	// Round all prices to 2 decimal places to avoid sub-penny issues
	entry = float64(int(entry*100+0.5)) / 100
	stopLoss = float64(int(stopLoss*100+0.5)) / 100
	takeProfitPrice := float64(int((entry*7)*100+0.5)) / 100

	fmt.Printf("Market price for %s: $%.2f\n", symbol, marketPrice)
	fmt.Printf("Entry price (market + $0.1): $%.2f\n", entry)
	fmt.Printf("Stop loss: $%.2f\n", stopLoss)
	fmt.Printf("Take profit: $%.2f\n", takeProfitPrice)

	fmt.Println("\n=== Submitting Order ===")
	fmt.Printf("Symbol: %s\n", symbol)
	fmt.Printf("Qty: %d\n", shares)
	fmt.Println("Side: buy")
	fmt.Println("Type: limit")
	fmt.Printf("Limit Price: $%.2f\n", entry)
	fmt.Printf("Stop Loss: $%.2f\n", stopLoss)
	fmt.Printf("Take Profit: $%.2f\n", takeProfitPrice)

	// Prepare order request with properly rounded decimals
	qty := decimal.NewFromInt(int64(shares))
	limitPrice := decimal.NewFromFloat(entry).Round(2)
	stopLossPrice := decimal.NewFromFloat(stopLoss).Round(2)
	takeProfitLimitPrice := decimal.NewFromFloat(takeProfitPrice).Round(2)

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
			LimitPrice: &takeProfitLimitPrice,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to place order: %w", err)
	}

	fmt.Println("\n=== Order Response ===")
	fmt.Printf("Order ID: %s\n", order.ID)
	fmt.Printf("Side: %s\n", order.Side)
	fmt.Printf("Symbol: %s\n", order.Symbol)
	fmt.Printf("Status: %s\n", order.Status)
	fmt.Printf("Order Class: %s\n", order.OrderClass)

	if len(order.Legs) > 0 {
		fmt.Println("\n=== Bracket Legs ===")
		for i, leg := range order.Legs {
			fmt.Printf("Leg %d: %s @ %s - %s\n", i+1, leg.Side, leg.Type, leg.OrderClass)
		}
	}

	return order, nil
}

// PlaceSellOrder places a sell order for a given symbol, either market or limit
func PlaceSellOrder(symbol string, shares int, sellPrice *float64) (*alpaca.Order, error) {
	// Check if asset is tradable
	_, err := GetAsset(symbol)
	if err != nil {
		return nil, err
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
	} else {
		orderReq.Type = alpaca.Limit
		limitPrice := decimal.NewFromFloat(*sellPrice)
		orderReq.LimitPrice = &limitPrice
	}

	order, err := apiClient.PlaceOrder(orderReq)
	if err != nil {
		return nil, fmt.Errorf("failed to place sell order: %w", err)
	}

	return order, nil
}

func main() {
	apiKey := os.Getenv("ALPACA_API_KEY")
	apiSecret := os.Getenv("ALPACA_SECRET_KEY")
	baseURL := os.Getenv("ALPACA_BASE_URL")

	fmt.Printf("API_KEY loaded: %v\n", apiKey != "")
	fmt.Printf("API_SECRET loaded: %v\n", apiSecret != "")
	fmt.Printf("BASE_URL: %s\n", baseURL)

	fmt.Println("\n=== Account Info ===")
	accountInfo, err := GetAccountInfo()
	if err != nil {
		log.Fatalf("Failed to get account info: %v", err)
	}
	fmt.Printf("Status: %s\n", accountInfo.Status)
	fmt.Printf("Cash: $%s\n", accountInfo.Cash)
	fmt.Printf("Buying Power: $%s\n", accountInfo.BuyingPower)
	fmt.Printf("Portfolio Value: $%s\n", accountInfo.PortfolioValue)

	// Clean slate for testing
	fmt.Println("\n=== Cleaning Up ===")
	if err := CancelAllOrders(); err != nil {
		log.Printf("Error cancelling orders: %v", err)
	}
	if err := LiquidateAllPositions(); err != nil {
		log.Printf("Error liquidating positions: %v", err)
	}
	time.Sleep(3 * time.Second) // Wait for liquidations to process

	// Check positions are cleared
	fmt.Println("\n=== Checking Positions ===")
	positions, err := apiClient.GetPositions()
	if err != nil {
		log.Printf("Failed to get positions: %v", err)
	} else {
		fmt.Printf("Current positions: %d\n", len(positions))
		for _, pos := range positions {
			fmt.Printf("  %s: %s shares\n", pos.Symbol, pos.Qty.String())
		}
	}

	// Submit buy orders for 3 different stocks
	fmt.Println("\n=== Placing Orders for 3 Stocks ===")
	symbols := []string{"AAPL", "NVDA", "AMD"}
	shares := 10 // Reduced shares for testing multiple

	for _, symbol := range symbols {
		fmt.Printf("\n--- Processing %s ---\n", symbol)

		// 1. Get current price to calculate a valid stop loss
		trade, err := marketClient.GetLatestTrade(symbol, marketdata.GetLatestTradeRequest{})
		if err != nil {
			log.Printf("Failed to get price for %s: %v", symbol, err)
			continue
		}

		currentPrice := trade.Price
		stopLoss := currentPrice * 0.95 // 5% stop loss
		fmt.Printf("Current Price: $%.2f, Calculated Stop Loss: $%.2f\n", currentPrice, stopLoss)

		// 2. Place the order
		buyOrder, err := PlaceEntryWithStop(symbol, stopLoss, shares, nil)
		if err != nil {
			log.Printf("✗ Error submitting buy order for %s: %v", symbol, err)
			continue
		}

		fmt.Printf("✓ Buy order submitted successfully for %s (ID: %s)\n", symbol, buyOrder.ID)
	}

	// Wait a moment for orders to be processed
	time.Sleep(2 * time.Second)

	// Check all orders in the account
	if _, err := CheckAllOrders(); err != nil {
		log.Printf("Error checking orders: %v", err)
	}

	/*
		err = LiquidateAllPositions()
		if err != nil {
			log.Printf("Error liquidating position: %v", err)
		}
	*/
}
