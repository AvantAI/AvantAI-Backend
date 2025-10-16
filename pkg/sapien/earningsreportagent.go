package sapien

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func EarningsReportAgentReqInfo(wg *sync.WaitGroup, stock string) {
	defer wg.Done() // Decrement the counter when the goroutine completes
	const EpEarningsReportAgent = "ep-earnings-report-agent"
	const namespace = "avant"

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

	err = godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	apiKey := os.Getenv("SAPIEN_TOKEN")

	sapienApi := NewSapienApi("http://localhost:8081", apiKey, zap.Must(zap.NewProduction()))

	statusCode, status, agentRes, err := sapienApi.GenerateCompletion(
		namespace,
		EpEarningsReportAgent,
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

	fmt.Println("Response:", agentRes.Response)

	_, err = file.WriteString(agentRes.Response)
	if err != nil {
		fmt.Println("Error writing to file:", err)
		os.Exit(1)
	}
}
