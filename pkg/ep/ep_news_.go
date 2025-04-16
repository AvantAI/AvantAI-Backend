package ep

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// API response structs
type AlphaVantageResponse struct {
	Feed           []Article `json:"feed"`
	Sentiment      string    `json:"overall_sentiment"`
	SentimentScore float64   `json:"overall_sentiment_score"`
}

type Article struct {
	Title         string            `json:"title"`
	URL           string            `json:"url"`
	TimePublished string            `json:"time_published"`
	Summary       string            `json:"summary"`
	Source        string            `json:"source"`
	Sentiment     string            `json:"overall_sentiment"`
	Score         float64           `json:"overall_sentiment_score"`
	Topics        []Topic           `json:"topics"`
	TickerSent    []TickerSentiment `json:"ticker_sentiment"`
}

type Topic struct {
	Topic     string `json:"topic"`
	Relevance string `json:"relevance_score"`
}

type TickerSentiment struct {
	Ticker    string `json:"ticker"`
	Sentiment string `json:"ticker_sentiment"`
	Score     string `json:"ticker_sentiment_score"`
	Relevance string `json:"relevance_score"`
}

func GetNews(wg *sync.WaitGroup, apiKey string, ticker string) {
	defer wg.Done() // Decrement the counter when the goroutine completes
	if len(os.Args) != 3 {
		fmt.Println("Usage: ./stock-news-sentiment <API_KEY> <TICKER>")
		os.Exit(1)
	}

	// Get current and previous day's date
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	timeFrom := yesterday.Format("20060102T0000")
	timeTo := now.Format("20060102T2359")

	// Fetch data from Alpha Vantage
	url := fmt.Sprintf("https://www.alphavantage.co/query?function=NEWS_SENTIMENT&tickers=%s&apikey=%s&limit=10&time_from=%s&time_to=%s",
		ticker, apiKey, timeFrom, timeTo)

	fmt.Printf("Fetching news sentiment data for %s...\n", ticker)
	response, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error making HTTP request: %v\n", err)
		os.Exit(1)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		fmt.Printf("API request failed with status code: %d\n", response.StatusCode)
		os.Exit(1)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		fmt.Printf("Error reading response body: %v\n", err)
		os.Exit(1)
	}

	var result AlphaVantageResponse
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Printf("Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	// Create folders
	dataDir := "data"
	stockDir := filepath.Join(dataDir, ticker)

	if err := os.MkdirAll(stockDir, 0755); err != nil {
		fmt.Printf("Error creating directories: %v\n", err)
		os.Exit(1)
	}

	// Separate regular news and earnings reports
	var newsArticles []Article
	var earningsReports []Article

	for _, article := range result.Feed {
		isEarningsReport := false

		// Check topics for earnings related content
		for _, topic := range article.Topics {
			if topic.Topic == "Earnings" || topic.Topic == "Earnings Report" || topic.Topic == "Earnings Call" {
				isEarningsReport = true
				break
			}
		}

		// Also check the title and summary for earnings mentions
		title := article.Title
		summary := article.Summary
		if contains(title, "earnings") || contains(title, "quarterly results") ||
			contains(summary, "earnings report") || contains(summary, "quarterly earnings") {
			isEarningsReport = true
		}

		if isEarningsReport {
			earningsReports = append(earningsReports, article)
		} else {
			newsArticles = append(newsArticles, article)
		}
	}

	// Write news reports to file
	newsReportPath := filepath.Join(stockDir, "news_report.txt")
	if err := writeArticlesToFile(newsReportPath, newsArticles, ticker); err != nil {
		fmt.Printf("Error writing news report: %v\n", err)
	} else {
		fmt.Printf("News report written to %s\n", newsReportPath)
	}

	// Write earnings reports to file
	earningsReportPath := filepath.Join(stockDir, "earnings_report.txt")
	if err := writeArticlesToFile(earningsReportPath, earningsReports, ticker); err != nil {
		fmt.Printf("Error writing earnings report: %v\n", err)
	} else {
		fmt.Printf("Earnings report written to %s\n", earningsReportPath)
	}
}

// Helper function to check if a string contains a substring (case insensitive)
func contains(s, substr string) bool {
	s = strings.ToLower(s)
	substr = strings.ToLower(substr)
	return strings.Contains(s, substr)
}

// Write articles to a file
func writeArticlesToFile(filePath string, articles []Article, ticker string) error {
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Write header
	fmt.Fprintf(f, "NEWS SENTIMENT REPORT FOR %s\n", ticker)
	fmt.Fprintf(f, "Generated on: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(f, "Total articles: %d\n\n", len(articles))
	fmt.Fprintf(f, "%s\n\n", strings.Repeat("-", 80))

	// Write each article
	for i, article := range articles {
		fmt.Fprintf(f, "ARTICLE %d\n", i+1)
		fmt.Fprintf(f, "Title: %s\n", article.Title)
		fmt.Fprintf(f, "Source: %s\n", article.Source)
		fmt.Fprintf(f, "Published: %s\n", article.TimePublished)
		fmt.Fprintf(f, "URL: %s\n", article.URL)
		fmt.Fprintf(f, "Sentiment: %s (Score: %.2f)\n", article.Sentiment, article.Score)

		// Write ticker-specific sentiment
		for _, ts := range article.TickerSent {
			if ts.Ticker == ticker {
				fmt.Fprintf(f, "%s Sentiment: %s (Score: %s, Relevance: %s)\n",
					ts.Ticker, ts.Sentiment, ts.Score, ts.Relevance)
				break
			}
		}

		fmt.Fprintf(f, "\nSummary:\n%s\n", article.Summary)

		if len(article.Topics) > 0 {
			fmt.Fprintf(f, "\nTopics:\n")
			for _, topic := range article.Topics {
				fmt.Fprintf(f, "- %s (Relevance: %s)\n", topic.Topic, topic.Relevance)
			}
		}

		fmt.Fprintf(f, "\n%s\n\n", strings.Repeat("-", 80))
	}

	return nil
}
