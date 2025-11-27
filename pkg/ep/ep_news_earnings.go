package ep

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/joho/godotenv"
)

// Scraped data structures
type NewsArticle struct {
	Title       string    `json:"title"`
	Source      string    `json:"source"`
	Author      string    `json:"author,omitempty"`
	PublishedAt time.Time `json:"published_at"`
	URL         string    `json:"url"`
	Summary     string    `json:"summary"`
	Content     string    `json:"content,omitempty"`
	Sentiment   string    `json:"sentiment,omitempty"`
}

type EarningsReport struct {
	Ticker        string            `json:"ticker"`
	CompanyName   string            `json:"company_name"`
	ReportDate    string            `json:"report_date"`
	Period        string            `json:"period"`
	Metrics       map[string]string `json:"metrics"`
	Summary       string            `json:"summary"`
	Guidance      string            `json:"guidance,omitempty"`
	KeyHighlights []string          `json:"key_highlights"`
	Source        string            `json:"source"`
	URL           string            `json:"url"`
	Content       string            `json:"content,omitempty"`
}

type ScrapedData struct {
	Ticker          string           `json:"ticker"`
	Date            string           `json:"date"`
	NewsArticles    []NewsArticle    `json:"news_articles"`
	EarningsReports []EarningsReport `json:"earnings_reports"`
	ScrapedAt       time.Time        `json:"scraped_at"`
}

// SEC EDGAR structures
type SECFilingsResponse struct {
	Filings struct {
		Recent struct {
			AccessionNumber []string `json:"accessionNumber"`
			FilingDate      []string `json:"filingDate"`
			ReportDate      []string `json:"reportDate"`
			FormType        []string `json:"form"`
			PrimaryDocument []string `json:"primaryDocument"`
			Items           []string `json:"items"`
		} `json:"recent"`
	} `json:"filings"`
}

// FinancialModelingPrep API structures
type FMPEarningsResponse struct {
	Symbol           string  `json:"symbol"`
	Date             string  `json:"date"`
	Eps              float64 `json:"eps"`
	EpsEstimated     float64 `json:"epsEstimated"`
	Revenue          int64   `json:"revenue"`
	RevenueEstimated int64   `json:"revenueEstimated"`
	FiscalDateEnding string  `json:"fiscalDateEnding"`
	Time             string  `json:"time"`
	UpdatedFromDate  string  `json:"updatedFromDate"`
}

// Exhibit information
type Exhibit struct {
	name string
	url  string
}

// Main function to get news and earnings data via web scraping
func GetNewsAndEarnings(wg *sync.WaitGroup, ticker string, dateStr string) {
	defer wg.Done()

	fmt.Printf("Starting web scraping for %s on date: %s\n", ticker, dateStr)

	// Parse and validate date
	targetDate, err := parseTargetDate(dateStr)
	if err != nil {
		fmt.Printf("Error parsing date: %v\n", err)
		return
	}

	// Create data structure
	scrapedData := ScrapedData{
		Ticker:          ticker,
		Date:            targetDate.Format("2006-01-02"),
		NewsArticles:    []NewsArticle{},
		EarningsReports: []EarningsReport{},
		ScrapedAt:       time.Now(),
	}

	// Create HTTP client with timeout and user agent
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Scrape from multiple sources concurrently
	var scraperWg sync.WaitGroup
	newsChannel := make(chan []NewsArticle, 3)
	earningsChannel := make(chan []EarningsReport, 3)

	// Yahoo Finance scraping
	scraperWg.Add(1)
	go scrapeYahooFinance(client, ticker, targetDate, newsChannel, earningsChannel, &scraperWg)

	// MarketWatch scraping
	scraperWg.Add(1)
	go scrapeMarketWatch(client, ticker, targetDate, newsChannel, earningsChannel, &scraperWg)

	// Finviz scraping
	scraperWg.Add(1)
	go scrapeFinviz(client, ticker, targetDate, newsChannel, earningsChannel, &scraperWg)

	// Wait for all scrapers to complete
	go func() {
		scraperWg.Wait()
		close(newsChannel)
		close(earningsChannel)
	}()

	// Collect results
	for newsArticles := range newsChannel {
		scrapedData.NewsArticles = append(scrapedData.NewsArticles, newsArticles...)
	}

	for earningsReports := range earningsChannel {
		scrapedData.EarningsReports = append(scrapedData.EarningsReports, earningsReports...)
	}

	// Filter articles by date
	scrapedData.NewsArticles = filterArticlesByDate(scrapedData.NewsArticles, targetDate)
	scrapedData.EarningsReports = filterEarningsByDate(scrapedData.EarningsReports, targetDate)

	fmt.Printf("After date filtering: %d news articles, %d earnings reports\n",
		len(scrapedData.NewsArticles), len(scrapedData.EarningsReports))

	// If no earnings reports found from news scraping, try SEC EDGAR directly
	if len(scrapedData.EarningsReports) == 0 {
		fmt.Printf("No earnings reports found from news sources, fetching directly from SEC EDGAR...\n")
		if content := fetchFromSECEdgar(client, ticker, targetDate); content != "" {
			earningsReport := EarningsReport{
				Ticker:      ticker,
				CompanyName: ticker,
				ReportDate:  targetDate.Format("2006-01-02"),
				Summary:     "SEC EDGAR Filing",
				Source:      "SEC EDGAR",
				URL:         "https://www.sec.gov",
				Metrics:     make(map[string]string),
				Content:     content,
			}
			scrapedData.EarningsReports = append(scrapedData.EarningsReports, earningsReport)
			fmt.Printf("Added SEC EDGAR earnings report\n")
		} else {
			// If SEC EDGAR also fails, try FinancialModelingPrep API
			fmt.Printf("SEC EDGAR returned no results, trying FinancialModelingPrep API...\n")
			if fmpReport := fetchFromFMP(client, ticker, targetDate); fmpReport != nil {
				scrapedData.EarningsReports = append(scrapedData.EarningsReports, *fmpReport)
				fmt.Printf("Added FinancialModelingPrep earnings report\n")
			} else {
				fmt.Printf("No earnings data found from any source\n")
			}
		}
	}

	// Fetch full content for all articles (with rate limiting)
	fmt.Printf("Fetching full content for %d articles...\n", len(scrapedData.NewsArticles))
	scrapedData.NewsArticles = fetchFullArticleContent(client, scrapedData.NewsArticles)

	fmt.Printf("Fetching full content for %d earnings reports...\n", len(scrapedData.EarningsReports))
	scrapedData.EarningsReports = fetchFullEarningsContent(client, scrapedData.EarningsReports)

	// Create output directory
	dataDir := "data"
	stockDir := filepath.Join(dataDir, ticker)
	if err := os.MkdirAll(stockDir, 0755); err != nil {
		fmt.Printf("Error creating directories: %v\n", err)
		return
	}

	// Write results to files
	writeScrapedNewsToFile(filepath.Join(stockDir, "news_report.txt"), scrapedData.NewsArticles, ticker, targetDate)
	writeScrapedEarningsToFile(filepath.Join(stockDir, "earnings_report.txt"), scrapedData.EarningsReports, ticker, targetDate)
	writeJSONReport(filepath.Join(stockDir, "scraped_data.json"), scrapedData)

	fmt.Printf("Scraping completed for %s. Found %d news articles and %d earnings reports\n",
		ticker, len(scrapedData.NewsArticles), len(scrapedData.EarningsReports))
}

// ============================================================================
// CONTENT FETCHING FUNCTIONS
// ============================================================================

// Fetch full content for all news articles
func fetchFullArticleContent(client *http.Client, articles []NewsArticle) []NewsArticle {
	var wg sync.WaitGroup
	articlesChan := make(chan NewsArticle, len(articles))

	// Rate limiter: max 3 concurrent requests
	semaphore := make(chan struct{}, 3)

	for _, article := range articles {
		wg.Add(1)
		go func(a NewsArticle) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			// Add delay between requests
			time.Sleep(500 * time.Millisecond)

			content := fetchArticleContent(client, a.URL, a.Source)
			a.Content = content
			articlesChan <- a
		}(article)
	}

	go func() {
		wg.Wait()
		close(articlesChan)
	}()

	var enrichedArticles []NewsArticle
	for article := range articlesChan {
		enrichedArticles = append(enrichedArticles, article)
	}

	return enrichedArticles
}

// Fetch full content for earnings reports
func fetchFullEarningsContent(client *http.Client, earnings []EarningsReport) []EarningsReport {
	var wg sync.WaitGroup
	earningsChan := make(chan EarningsReport, len(earnings))

	// Rate limiter: max 2 concurrent requests (more conservative for earnings)
	semaphore := make(chan struct{}, 2)

	for _, earning := range earnings {
		wg.Add(1)
		go func(e EarningsReport) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			// Parse report date
			reportDate, err := time.Parse("2006-01-02", e.ReportDate)
			if err != nil {
				reportDate = time.Now()
			}

			// Fetch earnings content from SEC EDGAR or original URL
			content := fetchEarningsReportContent(client, e.Ticker, reportDate, e.URL)
			e.Content = content
			earningsChan <- e

			// Longer delay between earnings requests
			time.Sleep(2 * time.Second)
		}(earning)
	}

	go func() {
		wg.Wait()
		close(earningsChan)
	}()

	var enrichedEarnings []EarningsReport
	for earning := range earningsChan {
		enrichedEarnings = append(enrichedEarnings, earning)
	}

	return enrichedEarnings
}

// Fetch earnings report content with SEC EDGAR and fallback to original URL
func fetchEarningsReportContent(client *http.Client, ticker string, reportDate time.Time, originalURL string) string {
	fmt.Printf("Fetching earnings content for %s on %s\n", ticker, reportDate.Format("2006-01-02"))

	// Method 1: Try SEC EDGAR first (most reliable and FREE)
	if content := fetchFromSECEdgar(client, ticker, reportDate); content != "" {
		fmt.Printf("✓ Successfully fetched earnings from SEC EDGAR\n")
		return content
	}

	// Method 2: Try original URL if it's valid
	if strings.HasPrefix(originalURL, "http://") || strings.HasPrefix(originalURL, "https://") {
		if content := fetchArticleContent(client, originalURL, ""); content != "" {
			fmt.Printf("✓ Successfully fetched earnings from original URL\n")
			return content
		}
	}

	fmt.Printf("✗ Failed to fetch earnings content for %s\n", ticker)
	return "Unable to fetch full earnings report. Please check SEC EDGAR directly."
}

// Fetch and extract content from an article URL
func fetchArticleContent(client *http.Client, url, source string) string {
	// Ensure URL has a valid scheme
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		fmt.Printf("Skipping invalid URL (no protocol): %s\n", url)
		return ""
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("Error creating request for %s: %v\n", url, err)
		return ""
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error fetching %s: %v\n", url, err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Printf("Non-200 status code for %s: %d\n", url, resp.StatusCode)
		return ""
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		fmt.Printf("Error parsing HTML for %s: %v\n", url, err)
		return ""
	}

	// Extract content based on source
	var content string

	if strings.Contains(source, "Yahoo Finance") {
		content = extractYahooContent(doc)
	} else if strings.Contains(source, "MarketWatch") {
		content = extractMarketWatchContent(doc)
	} else if strings.Contains(source, "Finviz") {
		// Finviz links often go to external sources, use generic extraction
		content = extractGenericContent(doc)
	} else {
		content = extractGenericContent(doc)
	}

	return cleanText(content)
}

// ============================================================================
// FINANCIALMODELINGPREP API FUNCTIONS
// ============================================================================

// Fetch earnings from FinancialModelingPrep API
func fetchFromFMP(client *http.Client, ticker string, targetDate time.Time) *EarningsReport {
	// Load API key from environment
	godotenv.Load() // Load .env file if it exists
	apiKey := os.Getenv("FMP_KEY")
	if apiKey == "" {
		fmt.Printf("FMP_KEY not found in environment variables\n")
		return nil
	}

	// Try to get earnings data
	earnings := fetchFMPEarnings(client, ticker, targetDate, apiKey)
	if earnings == nil {
		fmt.Printf("No earnings data found from FMP for %s\n", ticker)
		return nil
	}

	// Build the earnings report
	report := &EarningsReport{
		Ticker:        ticker,
		CompanyName:   ticker,
		ReportDate:    earnings.Date,
		Period:        earnings.FiscalDateEnding,
		Summary:       fmt.Sprintf("Earnings data for %s - %s", ticker, earnings.Date),
		Source:        "FinancialModelingPrep API",
		URL:           fmt.Sprintf("https://financialmodelingprep.com/api/v3/historical/earning_calendar/%s?apikey=%s", ticker, apiKey),
		Metrics:       make(map[string]string),
		KeyHighlights: []string{},
	}

	// Add metrics
	report.Metrics["EPS Actual"] = fmt.Sprintf("%.2f", earnings.Eps)
	report.Metrics["EPS Estimated"] = fmt.Sprintf("%.2f", earnings.EpsEstimated)
	report.Metrics["Revenue"] = fmt.Sprintf("$%d", earnings.Revenue)
	report.Metrics["Revenue Estimated"] = fmt.Sprintf("$%d", earnings.RevenueEstimated)

	// Calculate surprises
	epsSurprise := earnings.Eps - earnings.EpsEstimated
	revenueSurprise := float64(earnings.Revenue-earnings.RevenueEstimated) / float64(earnings.RevenueEstimated) * 100

	report.KeyHighlights = append(report.KeyHighlights,
		fmt.Sprintf("EPS: %.2f (Est: %.2f, Surprise: %.2f)", earnings.Eps, earnings.EpsEstimated, epsSurprise),
		fmt.Sprintf("Revenue: $%d (Est: $%d, Surprise: %.1f%%)", earnings.Revenue, earnings.RevenueEstimated, revenueSurprise),
	)

	// Build content summary
	report.Content = fmt.Sprintf(
		"Earnings Summary for %s\n\n"+
			"Report Date: %s\n"+
			"Fiscal Period Ending: %s\n\n"+
			"Financial Results:\n"+
			"- EPS (Actual): %.2f\n"+
			"- EPS (Estimated): %.2f\n"+
			"- EPS Surprise: %.2f\n\n"+
			"- Revenue (Actual): $%d\n"+
			"- Revenue (Estimated): $%d\n"+
			"- Revenue Surprise: %.1f%%\n\n"+
			"Announcement Time: %s\n",
		ticker, earnings.Date, earnings.FiscalDateEnding,
		earnings.Eps, earnings.EpsEstimated, epsSurprise,
		earnings.Revenue, earnings.RevenueEstimated, revenueSurprise,
		earnings.Time,
	)

	return report
}

// Fetch earnings data from FMP
func fetchFMPEarnings(client *http.Client, ticker string, targetDate time.Time, apiKey string) *FMPEarningsResponse {
	// Get earnings calendar for the specific date range
	// Search within 7 days before and after target date for more precision
	fromDate := targetDate.AddDate(0, 0, -7).Format("2006-01-02")
	toDate := targetDate.AddDate(0, 0, 7).Format("2006-01-02")

	url := fmt.Sprintf(
		"https://financialmodelingprep.com/api/v3/historical/earning_calendar/%s?from=%s&to=%s&apikey=%s",
		ticker, fromDate, toDate, apiKey,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("Error creating FMP request: %v\n", err)
		return nil
	}

	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error fetching from FMP: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Printf("FMP API returned status %d\n", resp.StatusCode)
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("Response: %s\n", string(bodyBytes))
		return nil
	}

	var earnings []FMPEarningsResponse
	if err := json.NewDecoder(resp.Body).Decode(&earnings); err != nil {
		fmt.Printf("Error decoding FMP response: %v\n", err)
		return nil
	}

	if len(earnings) == 0 {
		fmt.Printf("No earnings found in FMP response for date range %s to %s\n", fromDate, toDate)
		return nil
	}

	// Find the earnings report closest to the target date (within 2 days)
	var closestEarnings *FMPEarningsResponse
	minDiff := 365 * 24 * time.Hour

	for i := range earnings {
		earningsDate, err := time.Parse("2006-01-02", earnings[i].Date)
		if err != nil {
			continue
		}

		diff := earningsDate.Sub(targetDate)
		if diff < 0 {
			diff = -diff
		}

		// Only consider earnings within 2 days of target date
		if diff.Hours()/24 > 2 {
			continue
		}

		if diff < minDiff {
			minDiff = diff
			closestEarnings = &earnings[i]
		}
	}

	if closestEarnings != nil {
		daysDiff := minDiff.Hours() / 24
		fmt.Printf("Found FMP earnings for %s on %s (%.0f days from target date %s)\n",
			ticker, closestEarnings.Date, daysDiff, targetDate.Format("2006-01-02"))
	} else {
		fmt.Printf("No earnings found within 2 days of target date %s\n", targetDate.Format("2006-01-02"))
	}

	return closestEarnings
}

// ============================================================================
// SEC EDGAR FUNCTIONS
// ============================================================================

// Fetch from SEC EDGAR
func fetchFromSECEdgar(client *http.Client, ticker string, targetDate time.Time) string {
	// SEC requires a User-Agent with company name and email
	// IMPORTANT: Replace this with your actual contact info
	userAgent := "PersonalProject yourname@youremail.com"

	// First, get the CIK (Central Index Key) for the ticker
	cik := getCIKFromTicker(client, ticker, userAgent)
	if cik == "" {
		fmt.Printf("Could not find CIK for ticker %s\n", ticker)
		return ""
	}

	// Format CIK with leading zeros (10 digits)
	cik = fmt.Sprintf("%010s", cik)

	// Fetch recent filings
	filingURL := fmt.Sprintf("https://data.sec.gov/submissions/CIK%s.json", cik)

	req, err := http.NewRequest("GET", filingURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error fetching SEC filings: %v\n", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Printf("SEC API returned status %d\n", resp.StatusCode)
		return ""
	}

	var filings SECFilingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&filings); err != nil {
		fmt.Printf("Error decoding SEC response: %v\n", err)
		return ""
	}

	// Find relevant earnings filing (8-K or 10-Q) closest to target date
	var bestFiling struct {
		accessionNumber string
		primaryDoc      string
		formType        string
		filingDate      string
	}

	minDiff := 90 * 24 * time.Hour // Max 90 days difference

	for i := 0; i < len(filings.Filings.Recent.FormType); i++ {
		formType := filings.Filings.Recent.FormType[i]

		// Look for earnings-related forms
		if formType != "8-K" && formType != "10-Q" && formType != "10-K" {
			continue
		}

		filingDate, err := time.Parse("2006-01-02", filings.Filings.Recent.FilingDate[i])
		if err != nil {
			continue
		}

		diff := filingDate.Sub(targetDate)
		if diff < 0 {
			diff = -diff
		}

		// For 8-K, check if it's earnings-related
		if formType == "8-K" {
			items := filings.Filings.Recent.Items[i]
			if !strings.Contains(items, "2.02") && !strings.Contains(items, "7.01") {
				// 2.02 = Results of Operations, 7.01 = Regulation FD Disclosure
				continue
			}
		}

		if diff < minDiff {
			minDiff = diff
			bestFiling.accessionNumber = filings.Filings.Recent.AccessionNumber[i]
			bestFiling.primaryDoc = filings.Filings.Recent.PrimaryDocument[i]
			bestFiling.formType = formType
			bestFiling.filingDate = filings.Filings.Recent.FilingDate[i]
		}
	}

	if bestFiling.accessionNumber == "" {
		fmt.Printf("No relevant SEC filings found near %s\n", targetDate.Format("2006-01-02"))
		return ""
	}

	fmt.Printf("Found SEC filing: %s filed on %s\n", bestFiling.formType, bestFiling.filingDate)

	// For 8-K filings, we need to get the exhibits (99.1, 99.2) which contain the actual earnings content
	accessionNoSlash := strings.ReplaceAll(bestFiling.accessionNumber, "-", "")

	if bestFiling.formType == "8-K" {
		// Fetch the index page to find all exhibits
		indexURL := fmt.Sprintf("https://www.sec.gov/cgi-bin/viewer?action=view&cik=%s&accession_number=%s&xbrl_type=v",
			strings.TrimLeft(cik, "0"), bestFiling.accessionNumber)

		exhibits := findExhibits(client, indexURL, userAgent, cik, accessionNoSlash)

		// Fetch content from all exhibits (usually 99.1 is press release, 99.2 is financials)
		var allContent strings.Builder
		successCount := 0

		for i, exhibit := range exhibits {
			fmt.Printf("Attempting to fetch exhibit: %s from %s\n", exhibit.name, exhibit.url)
			content := fetchSECDocument(client, exhibit.url, userAgent)
			if content != "" && len(content) > 100 {
				successCount++
				if successCount > 1 {
					allContent.WriteString("\n\n" + strings.Repeat("=", 80) + "\n")
					allContent.WriteString(fmt.Sprintf("EXHIBIT %s\n", exhibit.name))
					allContent.WriteString(strings.Repeat("=", 80) + "\n\n")
				}
				allContent.WriteString(content)

				// Stop after successfully getting 2-3 exhibits (usually that's all we need)
				if successCount >= 3 {
					break
				}
			}

			// Only try up to 10 exhibit URLs to avoid too many failed requests
			if i >= 9 {
				break
			}
		}

		if allContent.Len() > 0 {
			fmt.Printf("Successfully fetched %d exhibits\n", successCount)
			return allContent.String()
		}

		fmt.Printf("Failed to fetch any exhibits, will try primary document\n")
	}

	// Fallback: fetch the primary document
	docURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/%s",
		strings.TrimLeft(cik, "0"), accessionNoSlash, bestFiling.primaryDoc)

	return fetchSECDocument(client, docURL, userAgent)
}

// Find exhibits in an 8-K filing (like 99.1, 99.2)
func findExhibits(client *http.Client, indexURL, userAgent, cik, accessionNoSlash string) []Exhibit {
	// First, try the more direct approach - fetch the filing's index.htm or -index.htm
	baseURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/",
		strings.TrimLeft(cik, "0"), accessionNoSlash)

	// Try common index file names
	indexFiles := []string{accessionNoSlash + "-index.htm", accessionNoSlash + "-index.html", "index.htm", "index.html"}

	var doc *goquery.Document

	for _, indexFile := range indexFiles {
		tryURL := baseURL + indexFile
		req, err := http.NewRequest("GET", tryURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", userAgent)

		resp, err := client.Do(req)
		if err != nil {
			continue
		}

		if resp.StatusCode == 200 {
			doc, err = goquery.NewDocumentFromReader(resp.Body)
			resp.Body.Close()
			if err == nil {
				// indexFileUsed = tryURL
				fmt.Printf("Successfully fetched index from: %s\n", tryURL)
				break
			}
		} else {
			resp.Body.Close()
		}
	}

	var exhibits []Exhibit

	if doc != nil {
		// Look for exhibit files in the index table
		doc.Find("table").Each(func(i int, table *goquery.Selection) {
			table.Find("tr").Each(func(j int, row *goquery.Selection) {
				cells := row.Find("td")

				// Handle different table formats (3, 4, or 5 columns)
				if cells.Length() >= 3 {
					var typeText, descText string
					var link *goquery.Selection

					// Common format: Seq | Description | Document | Type | Size
					// Or: Type | Description | Document
					if cells.Length() >= 4 {
						// Try format: Seq | Description | Document | Type
						typeText = strings.TrimSpace(cells.Eq(3).Text())
						descText = strings.TrimSpace(cells.Eq(1).Text())
						link = cells.Eq(2).Find("a").First()
					} else {
						// Format: Type | Description | Document
						typeText = strings.TrimSpace(cells.Eq(0).Text())
						descText = strings.TrimSpace(cells.Eq(1).Text())
						link = cells.Eq(2).Find("a").First()
					}

					// If we still don't have a link, search all cells
					if link.Length() == 0 {
						row.Find("a").Each(func(k int, a *goquery.Selection) {
							if link.Length() == 0 {
								link = a
							}
						})
					}

					// Combine type and description for matching
					combined := strings.ToLower(typeText + " " + descText)

					// Look for earnings-related exhibits with very broad matching
					isRelevantExhibit := strings.Contains(combined, "ex-99") ||
						strings.Contains(combined, "exhibit 99") ||
						strings.Contains(combined, "99.1") ||
						strings.Contains(combined, "99.2") ||
						strings.Contains(combined, "press release") ||
						strings.Contains(combined, "letter to shareholders") ||
						strings.Contains(combined, "shareholder letter") ||
						strings.Contains(combined, "earnings") ||
						strings.Contains(combined, "financial results") ||
						(strings.Contains(combined, "exhibit") && strings.Contains(combined, "99"))

					if isRelevantExhibit && link.Length() > 0 {
						if href, exists := link.Attr("href"); exists && href != "" {
							// Make URL absolute
							if !strings.HasPrefix(href, "http") {
								if strings.HasPrefix(href, "/") {
									href = "https://www.sec.gov" + href
								} else {
									href = baseURL + href
								}
							}

							// Create a descriptive name
							exhibitName := descText
							if exhibitName == "" {
								exhibitName = typeText
							}
							if exhibitName == "" {
								exhibitName = "Earnings Exhibit"
							}

							exhibits = append(exhibits, Exhibit{
								name: strings.TrimSpace(exhibitName),
								url:  href,
							})
							fmt.Printf("Found exhibit: '%s' -> %s\n", exhibitName, href)
						}
					}
				}
			})
		})
	}

	if len(exhibits) > 0 {
		fmt.Printf("Found %d exhibits via index parsing\n", len(exhibits))
		return exhibits
	}

	// Fallback to common exhibit construction
	fmt.Printf("No exhibits found via index, falling back to common patterns\n")
	return constructCommonExhibits(cik, accessionNoSlash)
}

// Construct URLs for common exhibits when we can't parse them
func constructCommonExhibits(cik, accessionNoSlash string) []Exhibit {
	baseURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/",
		strings.TrimLeft(cik, "0"), accessionNoSlash)

	// Common exhibit file naming patterns - try many variations
	commonExhibits := []string{
		// Standard patterns
		"ex991.htm",
		"ex99_1.htm",
		"ex-99_1.htm",
		"ex-991.htm",
		"d8k_ex991.htm",
		"ex992.htm",
		"ex99_2.htm",
		"ex-99_2.htm",
		"ex-992.htm",
		"d8k_ex992.htm",
		// With company prefix patterns (common for larger companies)
		"rddt-ex991.htm",
		"rddt-ex992.htm",
		// Numbered variations
		"ex99-1.htm",
		"ex99-2.htm",
		// Text file versions
		"ex991.txt",
		"ex99_1.txt",
		"ex-99_1.txt",
	}

	var exhibits []Exhibit
	for _, filename := range commonExhibits {
		exhibits = append(exhibits, Exhibit{
			name: filename,
			url:  baseURL + filename,
		})
	}

	fmt.Printf("Generated %d potential exhibit URLs to try\n", len(exhibits))
	return exhibits
}

// Get CIK from ticker symbol
func getCIKFromTicker(client *http.Client, ticker string, userAgent string) string {
	// SEC maintains a ticker to CIK mapping
	url := "https://www.sec.gov/files/company_tickers.json"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var tickers map[string]struct {
		CIK    int    `json:"cik_str"`
		Ticker string `json:"ticker"`
		Title  string `json:"title"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tickers); err != nil {
		return ""
	}

	ticker = strings.ToUpper(ticker)
	for _, v := range tickers {
		if strings.ToUpper(v.Ticker) == ticker {
			return fmt.Sprintf("%d", v.CIK)
		}
	}

	return ""
}

// Fetch and parse SEC document
func fetchSECDocument(client *http.Client, docURL, userAgent string) string {
	fmt.Printf("Fetching: %s\n", docURL)

	req, err := http.NewRequest("GET", docURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error fetching SEC document: %v\n", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// For exhibits, a 404 is normal if the filename doesn't match
		// Don't log errors for common exhibit attempts
		if resp.StatusCode != 404 {
			fmt.Printf("SEC document returned status %d for %s\n", resp.StatusCode, docURL)
		}
		return ""
	}

	// Read the body first
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response body: %v\n", err)
		return ""
	}

	// Check if it's actually binary data (images, PDFs, etc) - skip these
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "image") ||
		strings.Contains(contentType, "pdf") ||
		strings.Contains(contentType, "octet-stream") {
		fmt.Printf("Skipping binary content type: %s\n", contentType)
		return ""
	}

	// Check for binary data by looking at first bytes
	if len(bodyBytes) > 4 {
		// Check for common binary file signatures
		if bodyBytes[0] == 0xFF && bodyBytes[1] == 0xD8 { // JPEG
			fmt.Printf("Skipping JPEG image data\n")
			return ""
		}
		if bodyBytes[0] == 0x89 && bodyBytes[1] == 0x50 && bodyBytes[2] == 0x4E && bodyBytes[3] == 0x47 { // PNG
			fmt.Printf("Skipping PNG image data\n")
			return ""
		}
		if bodyBytes[0] == 0x25 && bodyBytes[1] == 0x50 && bodyBytes[2] == 0x44 && bodyBytes[3] == 0x46 { // PDF
			fmt.Printf("Skipping PDF data\n")
			return ""
		}
	}

	// Convert to string and check if it's mostly printable
	content := string(bodyBytes)
	if !isPrintableText(content) {
		fmt.Printf("Skipping non-printable/binary content\n")
		return ""
	}

	// Check if it's HTML
	isHTML := strings.Contains(contentType, "html") || strings.Contains(content[:min(512, len(content))], "<html")

	if !isHTML {
		// Plain text document - clean it up
		return cleanTextContent(content)
	}

	// Parse as HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		// If parsing fails, return cleaned text
		return cleanTextContent(content)
	}

	// Remove script, style, and other non-content elements
	doc.Find("script, style, meta, link, noscript").Remove()

	// Extract text from HTML - prioritize the main content
	var textContent strings.Builder

	// For SEC documents, look for specific content areas
	mainContent := doc.Find("body").First()
	if mainContent.Length() == 0 {
		mainContent = doc.Selection
	}

	// Extract text while preserving some structure
	seenText := make(map[string]bool)

	mainContent.Find("p, div, td, li, h1, h2, h3, h4, span").Each(func(i int, s *goquery.Selection) {
		// Get direct text (not from children)
		text := strings.TrimSpace(s.Contents().Not("script, style").Text())

		// Filter out very short text, HTML artifacts, and duplicates
		if len(text) > 15 && !strings.HasPrefix(text, "<") && !seenText[text] {
			// Avoid repeating the same text if it appears in nested elements
			seenText[text] = true
			textContent.WriteString(text)
			textContent.WriteString("\n\n")
		}
	})

	result := textContent.String()

	// If we didn't get much content, try a simpler approach
	if len(result) < 500 {
		result = strings.TrimSpace(doc.Text())
	}

	// Clean up the result
	result = cleanTextContent(result)

	if len(result) > 100 {
		fmt.Printf("✓ Successfully extracted %d characters of content\n", len(result))
	}

	return result
}

// Check if content is mostly printable text
func isPrintableText(s string) bool {
	if len(s) == 0 {
		return false
	}

	printableCount := 0
	sampleSize := min(1000, len(s))

	for i := 0; i < sampleSize; i++ {
		c := s[i]
		// Allow common printable ASCII, newlines, tabs
		if (c >= 32 && c <= 126) || c == '\n' || c == '\r' || c == '\t' {
			printableCount++
		}
	}

	// If less than 80% is printable, it's probably binary
	return float64(printableCount)/float64(sampleSize) > 0.8
}

// Clean text content - remove weird characters and excessive whitespace
func cleanTextContent(text string) string {
	// Remove null bytes and other control characters except newlines and tabs
	cleaned := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return r
		}
		if r < 32 || r == 127 {
			return -1
		}
		// Remove non-printable Unicode characters
		if r > 127 && r < 160 {
			return -1
		}
		return r
	}, text)

	// Clean up excessive whitespace
	re := regexp.MustCompile(`\s+`)
	cleaned = re.ReplaceAllString(cleaned, " ")

	// Clean up excessive newlines
	re = regexp.MustCompile(`\n\s*\n\s*\n+`)
	cleaned = re.ReplaceAllString(cleaned, "\n\n")

	return strings.TrimSpace(cleaned)
}

// ============================================================================
// CONTENT EXTRACTION FUNCTIONS
// ============================================================================

// Extract content from Yahoo Finance articles
func extractYahooContent(doc *goquery.Document) string {
	var paragraphs []string

	// Yahoo Finance article body selectors
	doc.Find("div.caas-body p, article p, div[data-test-locator='paragraph'] p").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		if len(text) > 50 { // Filter out short snippets
			paragraphs = append(paragraphs, text)
		}
	})

	if len(paragraphs) == 0 {
		// Fallback: try to get all paragraphs
		doc.Find("p").Each(func(i int, s *goquery.Selection) {
			text := s.Text()
			if len(text) > 50 {
				paragraphs = append(paragraphs, text)
			}
		})
	}

	return strings.Join(paragraphs, "\n\n")
}

// Extract content from MarketWatch articles
func extractMarketWatchContent(doc *goquery.Document) string {
	var paragraphs []string

	// MarketWatch article body selectors
	doc.Find("div.article__body p, div.body__content p, article p").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		if len(text) > 50 {
			paragraphs = append(paragraphs, text)
		}
	})

	if len(paragraphs) == 0 {
		// Fallback
		doc.Find("p").Each(func(i int, s *goquery.Selection) {
			text := s.Text()
			if len(text) > 50 {
				paragraphs = append(paragraphs, text)
			}
		})
	}

	return strings.Join(paragraphs, "\n\n")
}

// Generic content extraction for unknown sources
func extractGenericContent(doc *goquery.Document) string {
	var paragraphs []string

	// Remove unwanted elements
	doc.Find("script, style, nav, header, footer, aside, .ad, .advertisement, .sidebar").Remove()

	// Try to find main content area
	mainSelectors := []string{
		"article",
		"main",
		"div.article",
		"div.content",
		"div.post",
		"div.entry-content",
		"div[role='main']",
	}

	var mainContent *goquery.Selection
	for _, selector := range mainSelectors {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			mainContent = content
			break
		}
	}

	// If no main content found, use body
	if mainContent == nil || mainContent.Length() == 0 {
		mainContent = doc.Find("body")
	}

	// Extract all paragraphs from main content
	mainContent.Find("p").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		// Filter out short paragraphs and navigation/menu items
		if len(text) > 50 && !strings.Contains(strings.ToLower(text), "cookie") {
			paragraphs = append(paragraphs, text)
		}
	})

	return strings.Join(paragraphs, "\n\n")
}

// ============================================================================
// WEB SCRAPING FUNCTIONS
// ============================================================================

// Scrape Yahoo Finance
func scrapeYahooFinance(client *http.Client, ticker string, targetDate time.Time, newsChannel chan<- []NewsArticle, earningsChannel chan<- []EarningsReport, wg *sync.WaitGroup) {
	defer wg.Done()

	newsArticles := []NewsArticle{}
	earningsReports := []EarningsReport{}

	// Yahoo Finance news URL
	newsURL := fmt.Sprintf("https://finance.yahoo.com/quote/%s/news", ticker)

	req, err := http.NewRequest("GET", newsURL, nil)
	if err != nil {
		fmt.Printf("Error creating Yahoo Finance request: %v\n", err)
		newsChannel <- newsArticles
		earningsChannel <- earningsReports
		return
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error fetching Yahoo Finance: %v\n", err)
		newsChannel <- newsArticles
		earningsChannel <- earningsReports
		return
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		fmt.Printf("Error parsing Yahoo Finance HTML: %v\n", err)
		newsChannel <- newsArticles
		earningsChannel <- earningsReports
		return
	}

	// Scrape news articles
	doc.Find("div[data-test-locator='StreamItem']").Each(func(i int, s *goquery.Selection) {
		// Limit to 20 news articles total from Yahoo
		if len(newsArticles) >= 20 {
			return
		}

		title := s.Find("h3").Text()
		if title == "" {
			title = s.Find("a").First().Text()
		}

		url, exists := s.Find("a").First().Attr("href")
		if !exists {
			return
		}

		// Make URL absolute
		if strings.HasPrefix(url, "/") {
			url = "https://finance.yahoo.com" + url
		} else if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			// If URL doesn't start with / or http(s)://, it's likely a relative path
			url = "https://finance.yahoo.com/" + url
		}

		summary := s.Find("p").First().Text()
		source := s.Find("div").Last().Text()

		// Try to extract date - Yahoo Finance format varies
		timeText := s.Find("div span").Text()
		publishedAt := parseYahooDate(timeText, targetDate)

		article := NewsArticle{
			Title:       cleanText(title),
			Source:      "Yahoo Finance - " + cleanText(source),
			URL:         url,
			Summary:     cleanText(summary),
			PublishedAt: publishedAt,
		}

		// Check if this is an earnings-related article
		if isEarningsRelated(title, summary) {
			earnings := EarningsReport{
				Ticker:      ticker,
				CompanyName: extractCompanyName(title),
				ReportDate:  targetDate.Format("2006-01-02"),
				Summary:     cleanText(summary),
				Source:      "Yahoo Finance",
				URL:         url,
				Metrics:     make(map[string]string),
			}
			earningsReports = append(earningsReports, earnings)
		} else {
			newsArticles = append(newsArticles, article)
		}
	})

	fmt.Printf("Yahoo Finance: Found %d news articles, %d earnings reports\n", len(newsArticles), len(earningsReports))
	newsChannel <- newsArticles
	earningsChannel <- earningsReports
}

// Scrape MarketWatch
func scrapeMarketWatch(client *http.Client, ticker string, targetDate time.Time, newsChannel chan<- []NewsArticle, earningsChannel chan<- []EarningsReport, wg *sync.WaitGroup) {
	defer wg.Done()

	newsArticles := []NewsArticle{}
	earningsReports := []EarningsReport{}

	// MarketWatch news URL
	newsURL := fmt.Sprintf("https://www.marketwatch.com/investing/stock/%s/news", strings.ToLower(ticker))

	req, err := http.NewRequest("GET", newsURL, nil)
	if err != nil {
		fmt.Printf("Error creating MarketWatch request: %v\n", err)
		newsChannel <- newsArticles
		earningsChannel <- earningsReports
		return
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error fetching MarketWatch: %v\n", err)
		newsChannel <- newsArticles
		earningsChannel <- earningsReports
		return
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		fmt.Printf("Error parsing MarketWatch HTML: %v\n", err)
		newsChannel <- newsArticles
		earningsChannel <- earningsReports
		return
	}

	// Scrape news articles from MarketWatch
	doc.Find("div.article__content").Each(func(i int, s *goquery.Selection) {
		// Limit to 20 news articles total from MarketWatch
		if len(newsArticles) >= 20 {
			return
		}

		title := s.Find("h3.article__headline a").Text()
		if title == "" {
			title = s.Find("h2.article__headline a").Text()
		}

		url, exists := s.Find("h3.article__headline a, h2.article__headline a").Attr("href")
		if !exists {
			return
		}

		// Make URL absolute
		if strings.HasPrefix(url, "/") {
			url = "https://www.marketwatch.com" + url
		} else if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "https://www.marketwatch.com/" + url
		}

		summary := s.Find("p.article__summary").Text()
		author := s.Find("span.article__author").Text()

		// Extract time
		timeText := s.Find("span.article__timestamp").Text()
		publishedAt := parseMarketWatchDate(timeText, targetDate)

		article := NewsArticle{
			Title:       cleanText(title),
			Source:      "MarketWatch",
			Author:      cleanText(author),
			URL:         url,
			Summary:     cleanText(summary),
			PublishedAt: publishedAt,
		}

		// Check if this is an earnings-related article
		if isEarningsRelated(title, summary) {
			earnings := EarningsReport{
				Ticker:      ticker,
				CompanyName: extractCompanyName(title),
				ReportDate:  targetDate.Format("2006-01-02"),
				Summary:     cleanText(summary),
				Source:      "MarketWatch",
				URL:         url,
				Metrics:     make(map[string]string),
			}
			earningsReports = append(earningsReports, earnings)
		} else {
			newsArticles = append(newsArticles, article)
		}
	})

	fmt.Printf("MarketWatch: Found %d news articles, %d earnings reports\n", len(newsArticles), len(earningsReports))
	newsChannel <- newsArticles
	earningsChannel <- earningsReports
}

// Scrape Finviz
func scrapeFinviz(client *http.Client, ticker string, targetDate time.Time, newsChannel chan<- []NewsArticle, earningsChannel chan<- []EarningsReport, wg *sync.WaitGroup) {
	defer wg.Done()

	newsArticles := []NewsArticle{}
	earningsReports := []EarningsReport{}

	// Finviz news URL
	newsURL := fmt.Sprintf("https://finviz.com/quote.ashx?t=%s", ticker)

	req, err := http.NewRequest("GET", newsURL, nil)
	if err != nil {
		fmt.Printf("Error creating Finviz request: %v\n", err)
		newsChannel <- newsArticles
		earningsChannel <- earningsReports
		return
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error fetching Finviz: %v\n", err)
		newsChannel <- newsArticles
		earningsChannel <- earningsReports
		return
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		fmt.Printf("Error parsing Finviz HTML: %v\n", err)
		newsChannel <- newsArticles
		earningsChannel <- earningsReports
		return
	}

	// Scrape news from Finviz news table
	doc.Find("table.fullview-news-outer tr").Each(func(i int, s *goquery.Selection) {
		if i == 0 { // Skip header row
			return
		}

		// Limit to 20 news articles total from Finviz
		if len(newsArticles) >= 20 {
			return
		}

		timeCell := s.Find("td").First()
		newsCell := s.Find("td").Last()

		timeText := timeCell.Text()
		title := newsCell.Find("a").Text()
		url, exists := newsCell.Find("a").Attr("href")

		if !exists || title == "" {
			return
		}

		source := newsCell.Find("span").Text()
		publishedAt := parseFinvizDate(timeText, targetDate)

		article := NewsArticle{
			Title:       cleanText(title),
			Source:      "Finviz - " + cleanText(source),
			URL:         url,
			PublishedAt: publishedAt,
		}

		// Check if this is an earnings-related article
		if isEarningsRelated(title, "") {
			earnings := EarningsReport{
				Ticker:      ticker,
				CompanyName: extractCompanyName(title),
				ReportDate:  targetDate.Format("2006-01-02"),
				Summary:     cleanText(title),
				Source:      "Finviz",
				URL:         url,
				Metrics:     make(map[string]string),
			}
			earningsReports = append(earningsReports, earnings)
		} else {
			newsArticles = append(newsArticles, article)
		}
	})

	fmt.Printf("Finviz: Found %d news articles, %d earnings reports\n", len(newsArticles), len(earningsReports))
	newsChannel <- newsArticles
	earningsChannel <- earningsReports
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func parseTargetDate(dateStr string) (time.Time, error) {
	// Handle MM/DD/YY format
	if len(dateStr) >= 8 && strings.Contains(dateStr, "/") {
		parts := strings.Split(dateStr, "/")
		if len(parts) == 3 {
			month, _ := strconv.Atoi(parts[0])
			day, _ := strconv.Atoi(parts[1])
			year, _ := strconv.Atoi(parts[2])

			// Handle 2-digit years
			if year < 100 {
				if year < 50 {
					year += 2000
				} else {
					year += 1900
				}
			}

			return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), nil
		}
	}

	// Handle YYYY-MM-DD format
	if date, err := time.Parse("2006-01-02", dateStr); err == nil {
		return date, nil
	}

	// Default to yesterday
	return time.Now().AddDate(0, 0, -1), nil
}

func parseYahooDate(timeText string, targetDate time.Time) time.Time {
	// Yahoo Finance often shows relative times like "2h ago", "1d ago"
	if strings.Contains(timeText, "ago") {
		if strings.Contains(timeText, "h") {
			// Hours ago
			re := regexp.MustCompile(`(\d+)h`)
			if matches := re.FindStringSubmatch(timeText); len(matches) > 1 {
				if hours, err := strconv.Atoi(matches[1]); err == nil {
					return time.Now().Add(-time.Duration(hours) * time.Hour)
				}
			}
		} else if strings.Contains(timeText, "d") {
			// Days ago
			re := regexp.MustCompile(`(\d+)d`)
			if matches := re.FindStringSubmatch(timeText); len(matches) > 1 {
				if days, err := strconv.Atoi(matches[1]); err == nil {
					return time.Now().AddDate(0, 0, -days)
				}
			}
		}
	}
	return targetDate
}

func parseMarketWatchDate(timeText string, targetDate time.Time) time.Time {
	// MarketWatch uses formats like "March 12, 2025 at 10:30 a.m. ET"
	if parsed, err := time.Parse("January 2, 2006 at 3:04 p.m. MST", timeText); err == nil {
		return parsed
	}
	if parsed, err := time.Parse("Jan 2, 2006", timeText); err == nil {
		return parsed
	}
	return targetDate
}

func parseFinvizDate(timeText string, targetDate time.Time) time.Time {
	// Finviz uses formats like "Mar-12-25 10:30AM"
	if parsed, err := time.Parse("Jan-02-06 3:04PM", timeText); err == nil {
		return parsed
	}
	if parsed, err := time.Parse("Jan-02-06", timeText); err == nil {
		return parsed
	}
	return targetDate
}

func isEarningsRelated(title, summary string) bool {
	text := strings.ToLower(title + " " + summary)
	earningsKeywords := []string{
		"earnings", "quarterly results", "q1", "q2", "q3", "q4",
		"financial results", "quarterly earnings", "earnings report",
		"earnings call", "quarterly report", "fiscal quarter",
	}

	for _, keyword := range earningsKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func extractCompanyName(title string) string {
	// Simple extraction - this could be enhanced with more sophisticated logic
	words := strings.Fields(title)
	if len(words) > 0 {
		return words[0]
	}
	return ""
}

func cleanText(text string) string {
	// Remove extra whitespace and clean up text
	re := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(re.ReplaceAllString(text, " "))
}

func filterArticlesByDate(articles []NewsArticle, targetDate time.Time) []NewsArticle {
	var filtered []NewsArticle
	for _, article := range articles {
		// Accept articles from the target date or within 1 day
		if article.PublishedAt.Year() == targetDate.Year() &&
			article.PublishedAt.YearDay() >= targetDate.YearDay()-1 &&
			article.PublishedAt.YearDay() <= targetDate.YearDay()+1 {
			filtered = append(filtered, article)
		}
	}
	return filtered
}

func filterEarningsByDate(earnings []EarningsReport, targetDate time.Time) []EarningsReport {
	var filtered []EarningsReport

	for _, earning := range earnings {
		// Parse the report date
		reportDate, err := time.Parse("2006-01-02", earning.ReportDate)
		if err != nil {
			// If date parsing fails, skip it
			fmt.Printf("Skipping earnings report due to date parse error: %s\n", earning.ReportDate)
			continue
		}

		// Accept earnings within 2 days of target date
		daysDiff := reportDate.Sub(targetDate).Hours() / 24
		if daysDiff < 0 {
			daysDiff = -daysDiff
		}

		if daysDiff <= 2 {
			filtered = append(filtered, earning)
			fmt.Printf("Including earnings report from %s (%.0f days from target)\n", earning.ReportDate, daysDiff)
		} else {
			fmt.Printf("Filtering out earnings report from %s (%.0f days from target, exceeds 2 day limit)\n",
				earning.ReportDate, daysDiff)
		}
	}

	return filtered
}

// ============================================================================
// FILE WRITING FUNCTIONS
// ============================================================================

func writeScrapedNewsToFile(filePath string, articles []NewsArticle, ticker string, targetDate time.Time) error {
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "NEWS REPORT FOR %s\n", ticker)
	fmt.Fprintf(f, "Target Date: %s\n", targetDate.Format("2006-01-02"))
	fmt.Fprintf(f, "Generated on: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(f, "Total articles: %d\n\n", len(articles))
	fmt.Fprintf(f, "%s\n\n", strings.Repeat("-", 80))

	for i, article := range articles {
		fmt.Fprintf(f, "ARTICLE %d\n", i+1)
		fmt.Fprintf(f, "Title: %s\n", article.Title)
		fmt.Fprintf(f, "Source: %s\n", article.Source)
		if article.Author != "" {
			fmt.Fprintf(f, "Author: %s\n", article.Author)
		}
		fmt.Fprintf(f, "Published: %s\n", article.PublishedAt.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(f, "URL: %s\n", article.URL)
		if article.Summary != "" {
			fmt.Fprintf(f, "\nSummary:\n%s\n", article.Summary)
		}
		if article.Content != "" {
			fmt.Fprintf(f, "\n--- FULL ARTICLE CONTENT ---\n%s\n", article.Content)
		}
		fmt.Fprintf(f, "\n%s\n\n", strings.Repeat("-", 80))
	}

	return nil
}

func writeScrapedEarningsToFile(filePath string, earnings []EarningsReport, ticker string, targetDate time.Time) error {
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "EARNINGS REPORT FOR %s\n", ticker)
	fmt.Fprintf(f, "Target Date: %s\n", targetDate.Format("2006-01-02"))
	fmt.Fprintf(f, "Generated on: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(f, "Total reports: %d\n\n", len(earnings))
	fmt.Fprintf(f, "%s\n\n", strings.Repeat("-", 80))

	for i, earning := range earnings {
		fmt.Fprintf(f, "EARNINGS REPORT %d\n", i+1)
		fmt.Fprintf(f, "Ticker: %s\n", earning.Ticker)
		fmt.Fprintf(f, "Company: %s\n", earning.CompanyName)
		fmt.Fprintf(f, "Report Date: %s\n", earning.ReportDate)
		fmt.Fprintf(f, "Period: %s\n", earning.Period)
		fmt.Fprintf(f, "Source: %s\n", earning.Source)
		fmt.Fprintf(f, "URL: %s\n", earning.URL)

		if earning.Summary != "" {
			fmt.Fprintf(f, "\nSummary:\n%s\n", earning.Summary)
		}

		if earning.Guidance != "" {
			fmt.Fprintf(f, "\nGuidance:\n%s\n", earning.Guidance)
		}

		if len(earning.KeyHighlights) > 0 {
			fmt.Fprintf(f, "\nKey Highlights:\n")
			for _, highlight := range earning.KeyHighlights {
				fmt.Fprintf(f, "- %s\n", highlight)
			}
		}

		if len(earning.Metrics) > 0 {
			fmt.Fprintf(f, "\nMetrics:\n")
			for key, value := range earning.Metrics {
				fmt.Fprintf(f, "- %s: %s\n", key, value)
			}
		}

		if earning.Content != "" {
			fmt.Fprintf(f, "\n--- FULL EARNINGS REPORT CONTENT ---\n%s\n", earning.Content)
		}

		fmt.Fprintf(f, "\n%s\n\n", strings.Repeat("-", 80))
	}

	return nil
}

func writeJSONReport(filePath string, data ScrapedData) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, jsonData, 0644)
}
