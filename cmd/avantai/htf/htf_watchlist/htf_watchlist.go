package main

// HTF Watchlist Utility
//
// Displays the current HTF breakout signals recorded in htf_watchlist.csv.
// Optionally filters by date. Also supports clearing stale entries.
//
// Usage:
//   go run htf_watchlist.go                    # show all entries
//   go run htf_watchlist.go -date 2025-03-06   # show entries for a specific date
//   go run htf_watchlist.go -clear 2025-03-06  # delete entries for a specific date
//
// Mirrors the spirit of ep_watchlist.go in the EP strategy (post-session data management).

import (
	"avantai/pkg/htf"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

func main() {
	filterDatePtr := flag.String("date", "", "filter entries by date (YYYY-MM-DD)")
	clearDatePtr := flag.String("clear", "", "delete all entries for a date (YYYY-MM-DD)")
	flag.Parse()

	filterDate := *filterDatePtr
	clearDate := *clearDatePtr

	fmt.Printf("=== HTF Watchlist — %s ===\n\n", htf.WatchlistCSVFilename)

	// ---- Load existing entries ----
	entries, err := loadWatchlist(htf.WatchlistCSVFilename)
	if err != nil {
		fmt.Printf("Could not load watchlist: %v\n", err)
		fmt.Println("(File may not exist yet — run htf_main to generate signals.)")
		return
	}

	if len(entries) == 0 {
		fmt.Println("Watchlist is empty.")
		return
	}

	// ---- Clear mode ----
	if clearDate != "" {
		remaining := [][]string{}
		removed := 0
		for _, row := range entries {
			if len(row) >= 8 && row[7] == clearDate {
				removed++
				continue
			}
			remaining = append(remaining, row)
		}
		if err := saveWatchlist(htf.WatchlistCSVFilename, remaining); err != nil {
			fmt.Printf("Failed to save updated watchlist: %v\n", err)
			return
		}
		fmt.Printf("Removed %d entries for %s. %d entries remaining.\n", removed, clearDate, len(remaining))
		return
	}

	// ---- Display mode ----
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SYMBOL\tBREAKOUT_TIME\tBREAKOUT_PRICE\tRESISTANCE\tVOL_RATIO\tPOLE_GAIN%\tFLAG_RANGE%\tDATE")
	fmt.Fprintln(w, strings.Repeat("-", 90))

	displayed := 0
	for _, row := range entries {
		if len(row) < 8 {
			continue
		}
		if filterDate != "" && row[7] != filterDate {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7])
		displayed++
	}

	w.Flush()

	if filterDate != "" {
		fmt.Printf("\n%d signal(s) on %s\n", displayed, filterDate)
	} else {
		fmt.Printf("\n%d total signal(s)\n", displayed)
	}
}

// loadWatchlist reads all data rows (excluding header) from the watchlist CSV.
func loadWatchlist(filename string) ([][]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	// Strip header row
	if len(records) > 0 && len(records[0]) > 0 && records[0][0] == "symbol" {
		records = records[1:]
	}

	return records, nil
}

// saveWatchlist writes a fresh watchlist CSV with the provided data rows.
func saveWatchlist(filename string, rows [][]string) error {
	tempFile := filename + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	writer := csv.NewWriter(file)

	header := []string{"symbol", "breakout_time", "breakout_price", "resistance_level",
		"volume_ratio", "flagpole_gain_pct", "flag_range_pct", "date"}
	if err := writer.Write(header); err != nil {
		file.Close()
		os.Remove(tempFile)
		return err
	}

	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			file.Close()
			os.Remove(tempFile)
			return err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		file.Close()
		os.Remove(tempFile)
		return err
	}
	file.Close()

	return os.Rename(tempFile, filename)
}
