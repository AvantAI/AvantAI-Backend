package sapien

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
)

func EarningsReportAgentReqInfo(wg *sync.WaitGroup, stock string) {
	defer wg.Done() // Decrement the counter when the goroutine completes
	const EpClaudeManagerAgent = "ep-earnings-report-agent"
	const namespace = "pranav"

	// Navigate to the directory and open the file
	dirPath := fmt.Sprintf("data/%s", stock)

	file, err := os.Open(filepath.Join(dirPath, "earnings_report.txt"))
	if err != nil {
		log.Fatalf("Error opening file: %v\n", err)
	}
	defer file.Close()

	// Read the entire file content
	earnings_report, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Error reading file: %v\n", err)
	}

	sapienApi := NewSapienApi("localhost:8081", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhZG1pbiI6InN5c3RlbV9hZG1pbiIsImF1ZCI6WyJjbWQiXSwiZXhwIjoxNzQ3MzI3MzY5LCJpYXQiOjE3NDQ3MzUzNjksImlzcyI6IlNhcGllbmh1YiIsImp0aSI6IjQ2MjczMzA5LWVkYTctNGNiZC1hNWFkLWE1NjcyZjU0M2IzNCIsInN1YiI6IjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMCIsInRlbmFudCI6IjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMCJ9.eJYaCCoLpyZ6xxvP0M9cdMubYn-sdyhn9VyVPSHGlKw", zap.Must(zap.NewProduction()))

	statusCode, status, agentRes, err := sapienApi.GenerateCompletion(
		namespace,
		EpClaudeManagerAgent,
		&ServeRequest{
			Input: []Field{
				{Name: "ep_earnings", Value: string(earnings_report)},
			},
		},
	)

	if err != nil {
		fmt.Printf("StatusCode: %d status: %s err: %s", statusCode, status, err)
		os.Exit(1)
	}

	fmt.Printf("StatusCode: %d status: %s", statusCode, status)

	// Create folders
	dataDir := "reports"
	stockDir := filepath.Join(dataDir, stock)

	if err := os.MkdirAll(stockDir, 0755); err != nil {
		fmt.Printf("Error creating directories: %v\n", err)
		os.Exit(1)
	}

	// Create or open the file
	file, err = os.Create(filepath.Join(stockDir, "earnings_report.txt"))
	if err != nil {
		fmt.Println("Error creating file:", err)
	}
	defer file.Close()

	_, err = file.WriteString(agentRes.Response)
	if err != nil {
		fmt.Println("Error writing to file:", err)
		os.Exit(1)
	}
}
