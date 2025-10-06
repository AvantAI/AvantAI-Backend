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

// // Polygon.io API response structs
// type PolygonNewsResponse struct {
// 	Status     string          `json:"status"`
// 	RequestID  string          `json:"request_id"`
// 	Count      int             `json:"count"`
// 	NextURL    string          `json:"next_url,omitempty"`
// 	Results    []PolygonArticle `json:"results"`
// }

// type PolygonArticle struct {
// 	ID             string            `json:"id"`
// 	Publisher      Publisher         `json:"publisher"`
// 	Title          string            `json:"title"`
// 	Author         string            `json:"author"`
// 	PublishedUTC   string            `json:"published_utc"`
// 	ArticleURL     string            `json:"article_url"`
// 	Tickers        []string          `json:"tickers"`
// 	AmpURL         string            `json:"amp_url,omitempty"`
// 	ImageURL       string            `json:"image_url,omitempty"`
// 	Description    string            `json:"description"`
// 	Keywords       []string          `json:"keywords"`
// 	Insights       []Insight         `json:"insights,omitempty"`
// }

// type PolygonEarningsData struct {
// 	ID             string  `json:"id"`              // Unique identifier
// 	Ticker         string  `json:"ticker"`          // Stock symbol (e.g., "AAPL")
// 	Name           string  `json:"name"`            // Company name
// 	ReportDate     string  `json:"report_date"`     // When earnings will be reported
// 	ReportTime     string  `json:"report_time"`     // Time of day (e.g., "bmo", "amc")
// 	Currency       string  `json:"currency"`        // Currency (usually "USD")
// 	Period         string  `json:"period"`          // Reporting period (e.g., "Q1", "Q2")
// 	CalendarDate   string  `json:"calendar_date"`   // Calendar date
// 	CalendarYear   int     `json:"calendar_year"`   // Year (e.g., 2025)
// 	CalendarQuarter int    `json:"calendar_quarter"` // Quarter number (1, 2, 3, 4)
// 	Updated        string  `json:"updated"`         // Last update timestamp
// }

// type PolygonFinancialsData struct {
// 	CompanyName      string            `json:"company_name"`    // Company name
// 	CIK              string            `json:"cik"`             // SEC Central Index Key
// 	FiscalPeriod     string            `json:"fiscal_period"`   // Fiscal period
// 	FiscalYear       string            `json:"fiscal_year"`     // Fiscal year
// 	EndDate          string            `json:"end_date"`        // Period end date
// 	StartDate        string            `json:"start_date"`      // Period start date
// 	FilingDate       string            `json:"filing_date"`     // When filed with SEC
// 	TimeFrame        string            `json:"timeframe"`       // "annual", "quarterly"
// 	Financials       FinancialMetrics  `json:"financials"`      // Actual financial data
// }

// type FinancialMetrics struct {
// 	IncomeStatement    map[string]FinancialValue `json:"income_statement"`
// 	BalanceSheet       map[string]FinancialValue `json:"balance_sheet"`  
// 	CashFlowStatement  map[string]FinancialValue `json:"cash_flow_statement"`
// 	ComprehensiveIncome map[string]FinancialValue `json:"comprehensive_income"`
// }

// type FinancialValue struct {
// 	Value float64 `json:"value"`  // The actual dollar amount or number
// 	Unit  string  `json:"unit"`   // The unit of measurement 
// 	Label string  `json:"label"`  // Human-readable description
// 	Order int     `json:"order"`  // Display order in the financial statement
// }

// type Publisher struct {
// 	Name        string `json:"name"`
// 	HomepageURL string `json:"homepage_url"`
// 	LogoURL     string `json:"logo_url,omitempty"`
// 	FaviconURL  string `json:"favicon_url,omitempty"`
// }

// type Insight struct {
// 	Ticker    string `json:"ticker"`
// 	Sentiment string `json:"sentiment"`
// 	Score     float64 `json:"sentiment_reasoning_score"`
// }

// func GetNews(wg *sync.WaitGroup, apiKey string, ticker string, timestamp string) {
// 	defer wg.Done() // Decrement the counter when the goroutine completes

// 	// Parse the date and format for Polygon API
// 	targetDate := parseDate(timestamp)
	
// 	fmt.Printf("Fetching news for %s on date: %s\n", ticker, targetDate)

// 	// Build Polygon.io API URL
// 	// Format: https://api.polygon.io/v2/reference/news?ticker=AAPL&published_utc.gte=2025-03-12&published_utc.lt=2025-03-13&limit=50&apikey=YOUR_API_KEY
// 	url := fmt.Sprintf("https://api.polygon.io/v2/reference/news?ticker=%s&published_utc.gte=%s&published_utc.lt=%s&limit=50&order=desc&sort=published_utc&apikey=%s",
// 		ticker, targetDate, getNextDay(targetDate), apiKey)

// 	fmt.Printf("Fetching news data for %s...\n", ticker)
// 	response, err := http.Get(url)
// 	if err != nil {
// 		fmt.Printf("Error making HTTP request: %v\n", err)
// 		return
// 	}
// 	defer response.Body.Close()

// 	if response.StatusCode != http.StatusOK {
// 		fmt.Printf("API request failed with status code: %d\n", response.StatusCode)
// 		body, _ := io.ReadAll(response.Body)
// 		fmt.Printf("Response body: %s\n", string(body))
// 		return
// 	}

// 	body, err := io.ReadAll(response.Body)
// 	if err != nil {
// 		fmt.Printf("Error reading response body: %v\n", err)
// 		return
// 	}

// 	var result PolygonNewsResponse
// 	if err := json.Unmarshal(body, &result); err != nil {
// 		fmt.Printf("Error parsing JSON: %v\n", err)
// 		fmt.Printf("Raw response: %s\n", string(body))
// 		return
// 	}

// 	if result.Status != "OK" {
// 		fmt.Printf("API returned error status: %s\n", result.Status)
// 		return
// 	}

// 	fmt.Printf("Found %d articles for %s\n", result.Count, ticker)

// 	// Create folders
// 	dataDir := "data"
// 	stockDir := filepath.Join(dataDir, ticker)

// 	if err := os.MkdirAll(stockDir, 0755); err != nil {
// 		fmt.Printf("Error creating directories: %v\n", err)
// 		return
// 	}

// 	// Separate regular news and earnings reports
// 	var newsArticles []PolygonArticle
// 	var earningsReports []PolygonArticle

// 	for _, article := range result.Results {
// 		isEarningsReport := false

// 		// Check keywords for earnings related content
// 		for _, keyword := range article.Keywords {
// 			if strings.Contains(strings.ToLower(keyword), "earnings") ||
// 				strings.Contains(strings.ToLower(keyword), "quarterly") ||
// 				strings.Contains(strings.ToLower(keyword), "results") {
// 				isEarningsReport = true
// 				break
// 			}
// 		}

// 		// Also check the title and description for earnings mentions
// 		title := strings.ToLower(article.Title)
// 		description := strings.ToLower(article.Description)
// 		if strings.Contains(title, "earnings") || 
// 			strings.Contains(title, "quarterly results") ||
// 			strings.Contains(title, "q1") || strings.Contains(title, "q2") || 
// 			strings.Contains(title, "q3") || strings.Contains(title, "q4") ||
// 			strings.Contains(description, "earnings report") || 
// 			strings.Contains(description, "quarterly earnings") ||
// 			strings.Contains(description, "financial results") {
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
// 	if err := writePolygonArticlesToFile(newsReportPath, newsArticles, ticker); err != nil {
// 		fmt.Printf("Error writing news report: %v\n", err)
// 	} else {
// 		fmt.Printf("News report written to %s (%d articles)\n", newsReportPath, len(newsArticles))
// 	}

// 	// Write earnings reports to file
// 	earningsReportPath := filepath.Join(stockDir, "earnings_report.txt")
// 	if err := writePolygonArticlesToFile(earningsReportPath, earningsReports, ticker); err != nil {
// 		fmt.Printf("Error writing earnings report: %v\n", err)
// 	} else {
// 		fmt.Printf("Earnings report written to %s (%d articles)\n", earningsReportPath, len(earningsReports))
// 	}
// }

// // Parse date from various formats and return YYYY-MM-DD format
// func parseDate(timestamp string) string {
// 	if timestamp == "" {
// 		// Default to yesterday if no timestamp provided
// 		return time.Now().AddDate(0, 0, -1).Format("2006-01-02")
// 	}

// 	// Handle different input formats
// 	if len(timestamp) >= 10 {
// 		// Extract first 10 characters for date part
// 		dateStr := timestamp[0:10]
		
// 		// Try to parse and validate the date
// 		if t, err := time.Parse("2006-01-02", dateStr); err == nil {
// 			return t.Format("2006-01-02")
// 		}
		
// 		// Try alternative format
// 		if t, err := time.Parse("2006/01/02", dateStr); err == nil {
// 			return t.Format("2006-01-02")
// 		}
// 	}

// 	// If parsing fails, default to yesterday
// 	fmt.Printf("Warning: Could not parse timestamp '%s', using yesterday\n", timestamp)
// 	return time.Now().AddDate(0, 0, -1).Format("2006-01-02")
// }

// // Get the next day in YYYY-MM-DD format for the lt parameter
// func getNextDay(dateStr string) string {
// 	t, err := time.Parse("2006-01-02", dateStr)
// 	if err != nil {
// 		return dateStr // fallback to same date
// 	}
// 	return t.AddDate(0, 0, 1).Format("2006-01-02")
// }

// // Write Polygon articles to a file
// func writePolygonArticlesToFile(filePath string, articles []PolygonArticle, ticker string) error {
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
// 		fmt.Fprintf(f, "Publisher: %s\n", article.Publisher.Name)
// 		fmt.Fprintf(f, "Author: %s\n", article.Author)
// 		fmt.Fprintf(f, "Published: %s\n", article.PublishedUTC)
// 		fmt.Fprintf(f, "URL: %s\n", article.ArticleURL)
		
// 		if len(article.Tickers) > 0 {
// 			fmt.Fprintf(f, "Tickers: %s\n", strings.Join(article.Tickers, ", "))
// 		}

// 		// Write sentiment insights if available
// 		if len(article.Insights) > 0 {
// 			fmt.Fprintf(f, "Sentiment Insights:\n")
// 			for _, insight := range article.Insights {
// 				if insight.Ticker == ticker {
// 					fmt.Fprintf(f, "- %s: %s (Score: %.2f)\n", 
// 						insight.Ticker, insight.Sentiment, insight.Score)
// 				}
// 			}
// 		}

// 		fmt.Fprintf(f, "\nDescription:\n%s\n", article.Description)

// 		if len(article.Keywords) > 0 {
// 			fmt.Fprintf(f, "\nKeywords:\n")
// 			for _, keyword := range article.Keywords {
// 				fmt.Fprintf(f, "- %s\n", keyword)
// 			}
// 		}

// 		fmt.Fprintf(f, "\n%s\n\n", strings.Repeat("-", 80))
// 	}

// 	return nil
// }

// // Write earnings data to file
// func writeEarningsToFile(filePath string, earnings []PolygonEarningsData, ticker string) error {
// 	f, err := os.Create(filePath)
// 	if err != nil {
// 		return err
// 	}
// 	defer f.Close()

// 	// Write header
// 	fmt.Fprintf(f, "EARNINGS SCHEDULE FOR %s\n", ticker)
// 	fmt.Fprintf(f, "Generated on: %s\n", time.Now().Format("2006-01-02 15:04:05"))
// 	fmt.Fprintf(f, "Total records: %d\n\n", len(earnings))
// 	fmt.Fprintf(f, "%s\n\n", strings.Repeat("-", 80))

// 	// Write each earnings record
// 	for i, earning := range earnings {
// 		fmt.Fprintf(f, "EARNINGS RECORD %d\n", i+1)
// 		fmt.Fprintf(f, "Ticker: %s\n", earning.Ticker)
// 		fmt.Fprintf(f, "Company: %s\n", earning.Name)
// 		fmt.Fprintf(f, "Report Date: %s\n", earning.ReportDate)
// 		fmt.Fprintf(f, "Report Time: %s\n", earning.ReportTime)
// 		fmt.Fprintf(f, "Period: %s\n", earning.Period)
// 		fmt.Fprintf(f, "Calendar Date: %s\n", earning.CalendarDate)
// 		fmt.Fprintf(f, "Calendar Year: %d\n", earning.CalendarYear)
// 		fmt.Fprintf(f, "Calendar Quarter: %d\n", earning.CalendarQuarter)
// 		fmt.Fprintf(f, "Currency: %s\n", earning.Currency)
// 		fmt.Fprintf(f, "Last Updated: %s\n", earning.Updated)
// 		fmt.Fprintf(f, "\n%s\n\n", strings.Repeat("-", 80))
// 	}

// 	return nil
// }

// // Write financials data to file
// func writeFinancialsToFile(filePath string, financials []PolygonFinancialsData, ticker string) error {
// 	f, err := os.Create(filePath)
// 	if err != nil {
// 		return err
// 	}
// 	defer f.Close()

// 	// Write header
// 	fmt.Fprintf(f, "FINANCIAL REPORTS FOR %s\n", ticker)
// 	fmt.Fprintf(f, "Generated on: %s\n", time.Now().Format("2006-01-02 15:04:05"))
// 	fmt.Fprintf(f, "Total records: %d\n\n", len(financials))
// 	fmt.Fprintf(f, "%s\n\n", strings.Repeat("-", 80))

// 	// Write each financial record
// 	for i, financial := range financials {
// 		fmt.Fprintf(f, "FINANCIAL REPORT %d\n", i+1)
// 		fmt.Fprintf(f, "Company: %s\n", financial.CompanyName)
// 		fmt.Fprintf(f, "CIK: %s\n", financial.CIK)
// 		fmt.Fprintf(f, "Fiscal Period: %s\n", financial.FiscalPeriod)
// 		fmt.Fprintf(f, "Fiscal Year: %s\n", financial.FiscalYear)
// 		fmt.Fprintf(f, "Time Frame: %s\n", financial.TimeFrame)
// 		fmt.Fprintf(f, "Start Date: %s\n", financial.StartDate)
// 		fmt.Fprintf(f, "End Date: %s\n", financial.EndDate)
// 		fmt.Fprintf(f, "Filing Date: %s\n", financial.FilingDate)
		
// 		// Write Income Statement data
// 		if len(financial.Financials.IncomeStatement) > 0 {
// 			fmt.Fprintf(f, "\nINCOME STATEMENT:\n")
// 			for key, value := range financial.Financials.IncomeStatement {
// 				fmt.Fprintf(f, "- %s (%s): %s %s\n", value.Label, key, formatNumber(value.Value), value.Unit)
// 			}
// 		}
		
// 		// Write Balance Sheet data
// 		if len(financial.Financials.BalanceSheet) > 0 {
// 			fmt.Fprintf(f, "\nBALANCE SHEET:\n")
// 			for key, value := range financial.Financials.BalanceSheet {
// 				fmt.Fprintf(f, "- %s (%s): %s %s\n", value.Label, key, formatNumber(value.Value), value.Unit)
// 			}
// 		}
		
// 		// Write Cash Flow Statement data
// 		if len(financial.Financials.CashFlowStatement) > 0 {
// 			fmt.Fprintf(f, "\nCASH FLOW STATEMENT:\n")
// 			for key, value := range financial.Financials.CashFlowStatement {
// 				fmt.Fprintf(f, "- %s (%s): %s %s\n", value.Label, key, formatNumber(value.Value), value.Unit)
// 			}
// 		}
		
// 		// Write Comprehensive Income data
// 		if len(financial.Financials.ComprehensiveIncome) > 0 {
// 			fmt.Fprintf(f, "\nCOMPREHENSIVE INCOME:\n")
// 			for key, value := range financial.Financials.ComprehensiveIncome {
// 				fmt.Fprintf(f, "- %s (%s): %s %s\n", value.Label, key, formatNumber(value.Value), value.Unit)
// 			}
// 		}

// 		fmt.Fprintf(f, "\n%s\n\n", strings.Repeat("-", 80))
// 	}

// 	return nil
// }

// // Helper function to format numbers with commas
// func formatNumber(num float64) string {
// 	if num == 0 {
// 		return "0"
// 	}
	
// 	// Convert to string with 2 decimal places for currency
// 	str := fmt.Sprintf("%.0f", num)
	
// 	// Add commas for thousands
// 	n := len(str)
// 	if n <= 3 {
// 		return str
// 	}
	
// 	// Insert commas
// 	result := ""
// 	for i, char := range str {
// 		if i > 0 && (n-i)%3 == 0 {
// 			result += ","
// 		}
// 		result += string(char)
// 	}
	
// 	return result
// }