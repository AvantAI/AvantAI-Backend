package ep

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
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
}

type ScrapedData struct {
	Ticker          string           `json:"ticker"`
	Date            string           `json:"date"`
	NewsArticles    []NewsArticle    `json:"news_articles"`
	EarningsReports []EarningsReport `json:"earnings_reports"`
	ScrapedAt       time.Time        `json:"scraped_at"`
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

// Helper functions

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
	targetDateStr := targetDate.Format("2006-01-02")

	for _, earning := range earnings {
		if earning.ReportDate == targetDateStr {
			filtered = append(filtered, earning)
		}
	}
	return filtered
}

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
