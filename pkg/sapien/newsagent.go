package sapien

import (
	"avantai/pkg/spec"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func NewsAgentReqInfo(wg *sync.WaitGroup, stock string) {
	defer wg.Done() // Decrement the counter when the goroutine completes
	const EpNewsAgent = "ep-gemma-news-agent"
	// const namespace = "avant"

	logger := zap.Must(zap.NewProduction())

	// Navigate to the directory and open the file
	dirPath := fmt.Sprintf("data/%s", stock)

	file, err := os.Open(filepath.Join(dirPath, "news_report.txt"))
	if err != nil {
		log.Fatalf("Error opening file: %v\n", err)
	}
	defer file.Close()

	// Read the entire file content
	news, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Error reading file: %v\n", err)
	}

	err = godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// apiKey := os.Getenv("SAPIEN_TOKEN")

	// sapienApi := NewSapienApi("http://localhost:4081", apiKey, zap.Must(zap.NewProduction()))

	jsonResp := false
	agentRes, err := spec.Generate(EpNewsAgent, &spec.ServeRequestSpecV3{
		AgentNamespace: "avant",
		AgentName:      EpNewsAgent,
		Input: []spec.NameValueTypeV3{
			{Name: "ep_news", Value: string(news)},
		},
	}, jsonResp, logger)

	// statusCode, status, agentRes, err := sapienApi.GenerateCompletion(
	// 	namespace,
	// 	EpNewsAgent,
	// 	&ServeRequest{
	// 		Input: []Field{
	// 			{Name: "ep_news", Value: string(news)},
	// 		},
	// 	},
	// )

	if err != nil {
		fmt.Printf("err: %s", err)
		os.Exit(1)
	}

	// fmt.Printf("StatusCode: %d status: %s", statusCode, status)

	// Create folders
	dataDir := "reports"
	stockDir := filepath.Join(dataDir, stock)

	if err := os.MkdirAll(stockDir, 0755); err != nil {
		fmt.Printf("Error creating directories: %v\n", err)
		os.Exit(1)
	}

	// Create or open the file
	file, err = os.Create(filepath.Join(stockDir, "news_report.txt"))
	if err != nil {
		fmt.Println("Error creating file:", err)
	}
	defer file.Close()

	fmt.Println("Response:", agentRes)

	_, err = file.WriteString(agentRes)
	if err != nil {
		fmt.Println("Error writing to file:", err)
		os.Exit(1)
	}
}
