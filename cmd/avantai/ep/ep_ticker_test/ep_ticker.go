package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
)

type MarketstackIntradayResponse struct {
	Pagination struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Count  int `json:"count"`
		Total  int `json:"total"`
	} `json:"pagination"`
	Data []struct {
		Open   float64 `json:"open"`
		High   float64 `json:"high"`
		Low    float64 `json:"low"`
		Close  float64 `json:"close"`
		Volume float64 `json:"volume"`
		Date   string  `json:"date"` // e.g. "2024-04-01T13:31:00+0000" (UTC)
		Symbol string  `json:"symbol"`
	} `json:"data"`
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	marketstackKey := os.Getenv("MARKETSTACK_TOKEN")

	url := fmt.Sprintf(
		"https://api.marketstack.com/v2/intraday?access_key=%s&symbols=%s&date_from=%s&date_to=%s&interval=1min&limit=1000&sort=ASC",
		marketstackKey, "MMSI", "2018-07-24", "2018-07-25",
	)
	fmt.Println("Marketstack URL:", url)

	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("http get: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("read body: %v", err)
	}

	if resp.StatusCode != 200 {
		log.Fatalf("API error %d: %s", resp.StatusCode, string(body))
	}

	var out MarketstackIntradayResponse
	if err := json.Unmarshal(body, &out); err != nil {
		log.Fatalf("unmarshal: %v; body=%s", err, string(body))
	}

	fmt.Printf("Intraday data: %+v\n", out)
}
