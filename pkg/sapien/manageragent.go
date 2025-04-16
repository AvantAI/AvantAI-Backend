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

func ManagerAgentReqInfo(stock_data string, news string, earnings_report string) (*ServeResponse, error) {
	const EpClaudeManagerAgent = "ep-claude-manager-agent"
	const namespace = "pranav"

	sapienApi := NewSapienApi("localhost:8081", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhZG1pbiI6InN5c3RlbV9hZG1pbiIsImF1ZCI6WyJjbWQiXSwiZXhwIjoxNzQ3MzI3MzY5LCJpYXQiOjE3NDQ3MzUzNjksImlzcyI6IlNhcGllbmh1YiIsImp0aSI6IjQ2MjczMzA5LWVkYTctNGNiZC1hNWFkLWE1NjcyZjU0M2IzNCIsInN1YiI6IjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMCIsInRlbmFudCI6IjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMCJ9.eJYaCCoLpyZ6xxvP0M9cdMubYn-sdyhn9VyVPSHGlKw", zap.Must(zap.NewProduction()))	

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