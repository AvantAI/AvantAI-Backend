package ep

// import (
// 	"context"
// 	"fmt"
// 	"log"
// 	"math"
// 	"os"

// 	"github.com/joho/godotenv"
// 	"github.com/shopspring/decimal"

// 	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
// 	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
// )

// var (
// 	ctx      = context.Background()
// 	client   *alpaca.Client
// 	mdClient *marketdata.Client
// )

// func init() {
// 	err := godotenv.Load()
// 	if err != nil {
// 		log.Fatal("Error loading .env file")
// 	}

// 	apiKey := os.Getenv("ALPACA_API_KEY")
// 	apiSecret := os.Getenv("ALPACA_SECRET_KEY")

// 	if apiKey == "" || apiSecret == "" {
// 		log.Fatal("ALPACA_API_KEY or ALPACA_SECRET_KEY not set")
// 	}

// 	client = alpaca.NewClient(alpaca.ClientOpts{
// 		APIKey:    apiKey,
// 		APISecret: apiSecret,
// 		BaseURL:   "https://paper-api.alpaca.markets",
// 	})

// 	mdClient = marketdata.NewClient(marketdata.ClientOpts{
// 		APIKey:    apiKey,
// 		APISecret: apiSecret,
// 	})
// }

// // --------------------------------------------------
// // PLACE ENTRY WITH STOP (BRACKET ORDER)
// // --------------------------------------------------

// func PlaceEntryWithStop(
// 	symbol string,
// 	stopLoss float64,
// 	shares int,
// 	entryPrice *float64,
// ) (*alpaca.Order, error) {

// 	// Verify asset exists
// 	_, err := client.GetAsset(symbol)
// 	if err != nil {
// 		return nil, err
// 	}

// 	var marketPrice float64
// 	var limitPrice float64

// 	if entryPrice == nil {
// 		trade, err := mdClient.GetLatestTrade(
// 			symbol,
// 			marketdata.GetLatestTradeRequest{},
// 		)
// 		if err != nil {
// 			return nil, err
// 		}
// 		marketPrice = trade.Price
// 		limitPrice = marketPrice + 0.10
// 	} else {
// 		limitPrice = *entryPrice
// 		marketPrice = limitPrice - 0.10
// 	}

// 	limitPrice = math.Round(limitPrice*100) / 100
// 	stopLoss = math.Round(stopLoss*100) / 100
// 	takeProfit := math.Round(limitPrice*7*100) / 100

// 	decLimit := decimal.NewFromFloat(limitPrice)
// 	decStop := decimal.NewFromFloat(stopLoss)
// 	decTake := decimal.NewFromFloat(takeProfit)

// 	fmt.Println("\n=== ENTRY ORDER ===")
// 	fmt.Printf("Symbol: %s\n", symbol)
// 	fmt.Printf("Market Price: $%.2f\n", marketPrice)
// 	fmt.Printf("Limit Entry: $%.2f\n", limitPrice)
// 	fmt.Printf("Stop Loss: $%.2f\n", stopLoss)
// 	fmt.Printf("Take Profit: $%.2f\n", takeProfit)

// 	qty := decimal.NewFromInt(int64(shares))

// 	req := alpaca.PlaceOrderRequest{
// 		Symbol:      symbol,
// 		Qty:         &qty,
// 		Side:        alpaca.Buy,
// 		Type:        alpaca.Limit,
// 		TimeInForce: alpaca.Day,
// 		LimitPrice:  &decLimit,
// 		OrderClass:  alpaca.Bracket,
// 		StopLoss: &alpaca.StopLoss{
// 			StopPrice: &decStop,
// 		},
// 		TakeProfit: &alpaca.TakeProfit{
// 			LimitPrice: &decTake,
// 		},
// 	}

// 	order, err := client.PlaceOrder(req)
// 	if err != nil {
// 		return nil, err
// 	}

// 	fmt.Println("Order ID:", order.ID)
// 	fmt.Println("Status:", order.Status)

// 	if len(order.Legs) > 0 {
// 		fmt.Println("Bracket Legs:")
// 		for i, leg := range order.Legs {
// 			fmt.Printf("  Leg %d: %s %s\n", i+1, leg.Side, leg.Type)
// 		}
// 	}

// 	return order, nil
// }

// // --------------------------------------------------
// // PLACE SELL ORDER
// // --------------------------------------------------

// func PlaceSellOrder(
// 	symbol string,
// 	shares int,
// 	sellPrice *float64,
// ) (*alpaca.Order, error) {

// 	_, err := client.GetAsset(symbol)
// 	if err != nil {
// 		return nil, err
// 	}

// 	qty := decimal.NewFromInt(int64(shares))
// 	req := alpaca.PlaceOrderRequest{
// 		Symbol:      symbol,
// 		Qty:         &qty,
// 		Side:        alpaca.Sell,
// 		TimeInForce: alpaca.Day,
// 	}

// 	if sellPrice == nil {
// 		req.Type = alpaca.Market
// 	} else {
// 		price := math.Round(*sellPrice*100) / 100
// 		decPrice := decimal.NewFromFloat(price)
// 		req.Type = alpaca.Limit
// 		req.LimitPrice = &decPrice
// 	}

// 	return client.PlaceOrder(req)
// }

// // --------------------------------------------------
// // MAIN
// // --------------------------------------------------

// // func main() {
// // 	symbol := "AAPL"
// // 	shares := 1

// // 	stopLoss := 150.00
// // 	order, err := PlaceEntryWithStop(symbol, stopLoss, shares, nil)
// // 	if err != nil {
// // 		log.Fatal("Entry order failed:", err)
// // 	}

// // 	fmt.Println("\nBracket order submitted:", order.ID)
// // }
