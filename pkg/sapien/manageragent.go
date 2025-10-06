package sapien

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

// StockData struct to hold the OCHLV stock data
type StockData struct {
	Symbol string  `json:"symbol"`
	Open   float64 `json:"open,string"`
	High   float64 `json:"high,string"`
	Low    float64 `json:"low,string"`
	Price  float64 `json:"price,string"`
	Volume float64 `json:"volume,string"`
}

func ManagerAgentReqInfo(stock_data string, news string, earnings_report string, sentiment string) (*ServeResponse, error) {
	const EpCerebrasManagerAgent = "ep-cerebras-manager-agent"
	const namespace = "avant"

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	apiKey := os.Getenv("SAPIEN_TOKEN")

	sapienApi := NewSapienApi("http://localhost:8081", apiKey, zap.Must(zap.NewProduction()))

	statusCode, status, agentRes, err := sapienApi.GenerateCompletion(
		namespace,
		EpCerebrasManagerAgent,
		&ServeRequest{
			Input: []Field{
				{Name: "stock_data", Value: stock_data},

				{Name: "news", Value: news},

				{Name: "earnings_report", Value: earnings_report},

				{Name: "stock_sentiment", Value: sentiment},
			},
		},
	)

	if err != nil {
		fmt.Printf("StatusCode: %d status: %s err: %s", statusCode, status, err)
		return nil, err
	}

	fmt.Printf("StatusCode: %d status: %s", statusCode, status)

	return agentRes, nil
}
