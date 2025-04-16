package sapien

import (
	"fmt"

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

func ManagerAgentReqInfo(min string, stockData StockData, news string, earnings_report string) (*ServeResponse, error) {
	const EpClaudeManagerAgent = "ep-claude-manager-agent"
	const namespace = "pranav"

	stock_data := fmt.Sprintf(min + " Open: " + fmt.Sprint(stockData.Open) + 
	" Price: " + fmt.Sprint(stockData.Price) + " High: " + fmt.Sprint(stockData.High) + "Low: " + fmt.Sprint(stockData.Low))

	sapienApi := NewSapienApi("localhost:8081", "", zap.Must(zap.NewProduction()))	

	statusCode, status, agentRes, err := sapienApi.GenerateCompletion(
    namespace, 
    EpClaudeManagerAgent, 
    &ServeRequest{
                Input: []Field{ 
                        {Name: "stock_data", Value: stock_data},

                        {Name: "news", Value: news},

                        {Name: "earnings_report", Value: earnings_report},
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