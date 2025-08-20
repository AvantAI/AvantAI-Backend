package ep

// import (
// 	"encoding/json"
// 	"fmt"
// 	"io"
// 	"net/http"
// 	"os"
// 	"path/filepath"
// 	"strings"
// 	"sync"
// 	"time"
// )

// // Tiingo API response structs
// type TiingoNewsResponse []TiingoArticle

// type TiingoArticle struct {
// 	ID            string     `json:"id"`
// 	Title         string     `json:"title"`
// 	URL           string     `json:"url"`
// 	Description   string     `json:"description"`
// 	PublishedDate string     `json:"publishedDate"`
// 	Source        string     `json:"source"`
// 	Tags          []string   `json:"tags"`
// 	Tickers       []string   `json:"tickers"`
// 	CrawlDate     string     `json:"crawlDate"`
// 	Sentiment     *Sentiment `json:"sentiment,omitempty"` // Sentiment may be null
// }

// type Sentiment struct {
// 	Polarity       float64 `json:"polarity"`
// 	Negative       float64 `json:"negative"`
// 	Neutral        float64 `json:"neutral"`
// 	Positive       float64 `json:"positive"`
// 	Compound       float64 `json:"compound"`
// 	SentimentLabel string  `json:"-"` // Calculated field
// }

// func GetNews(wg *sync.WaitGroup, apiKey string, ticker string, timestamp string) {
// 	defer wg.Done() // Decrement the counter when the goroutine completes

// 	timeFrom, timeTo := getTime(timestamp)

// 	fmt.Println("Time from:", timeFrom)
// 	fmt.Println("Time to:", timeTo)

// 	// Format dates for Tiingo API (ISO 8601 format)
// 	tiingoTimeFrom := formatTiingoDate(timeFrom)
// 	tiingoTimeTo := formatTiingoDate(timeTo)

// 	// Fetch data from Tiingo
// 	url := fmt.Sprintf("https://api.tiingo.com/tiingo/news?tickers=%s&startDate=%s&endDate=%s&limit=10&token=%s&sortBy=relevance",
// 		ticker, tiingoTimeFrom, tiingoTimeTo, apiKey)

// 	fmt.Printf("Fetching news data for %s...\n", ticker)
// 	client := &http.Client{Timeout: 10 * time.Second}
// 	req, err := http.NewRequest("GET", url, nil)
// 	if err != nil {
// 		fmt.Printf("Error creating HTTP request: %v\n", err)
// 		os.Exit(1)
// 	}

// 	response, err := client.Do(req)
// 	if err != nil {
// 		fmt.Printf("Error making HTTP request: %v\n", err)
// 		os.Exit(1)
// 	}
// 	defer response.Body.Close()

// 	if response.StatusCode != http.StatusOK {
// 		fmt.Printf("API request failed with status code: %d\n", response.StatusCode)
// 		os.Exit(1)
// 	}

// 	body, err := io.ReadAll(response.Body)
// 	if err != nil {
// 		fmt.Printf("Error reading response body: %v\n", err)
// 		os.Exit(1)
// 	}

// 	var result TiingoNewsResponse
// 	if err := json.Unmarshal(body, &result); err != nil {
// 		fmt.Printf("Error parsing JSON: %v\n", err)
// 		os.Exit(1)
// 	}

// 	// Process sentiment for each article
// 	for i := range result {
// 		if result[i].Sentiment != nil {
// 			result[i].Sentiment.SentimentLabel = categorizeSentiment(result[i].Sentiment.Compound)
// 		}
// 	}

// 	// Create folders
// 	dataDir := "data"
// 	stockDir := filepath.Join(dataDir, ticker)

// 	if err := os.MkdirAll(stockDir, 0755); err != nil {
// 		fmt.Printf("Error creating directories: %v\n", err)
// 		os.Exit(1)
// 	}

// 	// Separate regular news and earnings reports
// 	var newsArticles []TiingoArticle
// 	var earningsReports []TiingoArticle

// 	for _, article := range result {
// 		isEarningsReport := false

// 		// Check tags and tickers for earnings related content
// 		for _, tag := range article.Tags {
// 			if contains(tag, "earnings") || contains(tag, "quarterly") || contains(tag, "financial results") {
// 				isEarningsReport = true
// 				break
// 			}
// 		}

// 		// Also check the title and description for earnings mentions
// 		title := article.Title
// 		description := article.Description
// 		if contains(title, "earnings") || contains(title, "quarterly results") ||
// 			contains(description, "earnings report") || contains(description, "quarterly earnings") {
// 			isEarningsReport = true
// 		}

// 		if isEarningsReport {
// 			earningsReports = append(earningsReports, article)
// 		} else {
// 			newsArticles = append(newsArticles, article)
// 		}
// 	}

// 	// Write news reports to file
// 	newsReportPath := filepath.Join(stockDir, "news_report.txt")
// 	if err := writeArticlesToFile(newsReportPath, newsArticles, ticker); err != nil {
// 		fmt.Printf("Error writing news report: %v\n", err)
// 	} else {
// 		fmt.Printf("News report written to %s\n", newsReportPath)
// 	}

// 	// Write earnings reports to file
// 	earningsReportPath := filepath.Join(stockDir, "earnings_report.txt")
// 	if err := writeArticlesToFile(earningsReportPath, earningsReports, ticker); err != nil {
// 		fmt.Printf("Error writing earnings report: %v\n", err)
// 	} else {
// 		fmt.Printf("Earnings report written to %s\n", earningsReportPath)
// 	}
// }

// // Helper function to categorize sentiment based on compound score
// func categorizeSentiment(score float64) string {
// 	if score >= 0.05 {
// 		return "Bullish"
// 	} else if score <= -0.05 {
// 		return "Bearish"
// 	} else {
// 		return "Neutral"
// 	}
// }

// // Helper function to check if a string contains a substring (case insensitive)
// func contains(s, substr string) bool {
// 	s = strings.ToLower(s)
// 	substr = strings.ToLower(substr)
// 	return strings.Contains(s, substr)
// }

// // Format date from Alpha Vantage format to Tiingo format
// func formatTiingoDate(alphaDate string) string {
// 	// Parse Alpha Vantage format: "20250204T0000"
// 	t, err := time.Parse("20060102T1504", alphaDate)
// 	if err != nil {
// 		fmt.Printf("Error parsing date format: %v\n", err)
// 		return alphaDate // Return original if parsing fails
// 	}

// 	// Return ISO 8601 format for Tiingo: "2025-02-04T00:00:00"
// 	return t.Format("2006-01-02T15:04:05")
// }

// // Write articles to a file
// func writeArticlesToFile(filePath string, articles []TiingoArticle, ticker string) error {
// 	f, err := os.Create(filePath)
// 	if err != nil {
// 		return err
// 	}
// 	defer f.Close()

// 	// Write header
// 	fmt.Fprintf(f, "NEWS REPORT FOR %s\n", ticker)
// 	fmt.Fprintf(f, "Generated on: %s\n", time.Now().Format("2006-01-02 15:04:05"))
// 	fmt.Fprintf(f, "Total articles: %d\n\n", len(articles))
// 	fmt.Fprintf(f, "%s\n\n", strings.Repeat("-", 80))

// 	// Write each article
// 	for i, article := range articles {
// 		fmt.Fprintf(f, "ARTICLE %d\n", i+1)
// 		fmt.Fprintf(f, "Title: %s\n", article.Title)
// 		fmt.Fprintf(f, "Source: %s\n", article.Source)
// 		fmt.Fprintf(f, "Published: %s\n", article.PublishedDate)
// 		fmt.Fprintf(f, "URL: %s\n", article.URL)

// 		// Write sentiment information if available
// 		if article.Sentiment != nil {
// 			fmt.Fprintf(f, "Sentiment: %s (Compound Score: %.2f)\n",
// 				article.Sentiment.SentimentLabel, article.Sentiment.Compound)
// 			fmt.Fprintf(f, "Sentiment Details: Positive: %.2f, Neutral: %.2f, Negative: %.2f\n",
// 				article.Sentiment.Positive, article.Sentiment.Neutral, article.Sentiment.Negative)
// 		} else {
// 			fmt.Fprintf(f, "Sentiment: Not Available\n")
// 		}

// 		// Check if ticker is in the tickers list
// 		isRelevant := false
// 		for _, t := range article.Tickers {
// 			if t == ticker {
// 				isRelevant = true
// 				break
// 			}
// 		}
// 		fmt.Fprintf(f, "Ticker Relevance: %v\n", isRelevant)

// 		fmt.Fprintf(f, "\nDescription:\n%s\n", article.Description)

// 		if len(article.Tags) > 0 {
// 			fmt.Fprintf(f, "\nTags:\n")
// 			for _, tag := range article.Tags {
// 				fmt.Fprintf(f, "- %s\n", tag)
// 			}
// 		}

// 		if len(article.Tickers) > 0 {
// 			fmt.Fprintf(f, "\nRelated Tickers:\n")
// 			for _, t := range article.Tickers {
// 				fmt.Fprintf(f, "- %s\n", t)
// 			}
// 		}

// 		fmt.Fprintf(f, "\n%s\n\n", strings.Repeat("-", 80))
// 	}

// 	return nil
// }

// func getTime(timestamp string) (string, string) {
// 	// Get current and previous day's date
// 	now := time.Now()
// 	yesterday := now.AddDate(0, 0, -1)
// 	timeFrom := yesterday.Format("20060102T0000")
// 	timeTo := now.Format("20060102T2359")

// 	fmt.Println(timestamp)

// 	if timestamp != "" {
// 		// Parse the timestamp
// 		t, err := time.Parse("2006-01-02", timestamp[0:10])
// 		if err != nil {
// 			fmt.Printf("Error parsing timestamp: %v\n", err)
// 			return timeFrom, timeTo
// 		}

// 		// Format the time to the required format
// 		y := t.AddDate(0, 0, -1)
// 		timeFrom = y.Format("20060102T0000")
// 		timeTo = t.Format("20060102T0000")
// 	}

// 	return timeFrom, timeTo
// }
