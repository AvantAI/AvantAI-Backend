package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	directoryPath := "data"
	watchlistPath := "cmd\\avantai\\ep\\ep_watchlist\\watchlist.csv"

	// Get all stock folders
	stockFolders := ParseAllStockFolders(directoryPath)

	// Get stocks from watchlist
	watchlistStocks := ParseWatchList(watchlistPath)

	// Compare and find differences
	foldersToDelete := CompareFoldersToWatchList(stockFolders, watchlistStocks)

	// Delete folders not on watchlist
	DeleteFoldersNotOnWatchList(directoryPath, foldersToDelete)
}

func ParseAllStockFolders(directoryPath string) []string {
	folders, err := os.ReadDir(directoryPath)
	if err != nil {
		fmt.Println("Error reading directory:", err)
		return nil
	}

	var stockFolders []string
	for _, folder := range folders {
		if folder.IsDir() {
			stockFolders = append(stockFolders, folder.Name())
			fmt.Println("Found stock folder:", folder.Name())
		}
	}

	return stockFolders
}

func ParseWatchList(watchlistPath string) []string {
	file, err := os.Open(watchlistPath)
	if err != nil {
		fmt.Println("Error opening watchlist:", err)
		return nil
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		fmt.Println("Error reading CSV:", err)
		return nil
	}

	var watchlistStocks []string
	for _, row := range records {
		if len(row) > 0 {
			symbol := row[0]
			watchlistStocks = append(watchlistStocks, symbol)
			fmt.Println("Found watchlist stock:", symbol)
		}
	}

	return watchlistStocks
}

func CompareFoldersToWatchList(folders, watchlist []string) []string {
	var foldersToDelete []string

	for _, folder := range folders {
		found := false
		for _, stock := range watchlist {
			if strings.EqualFold(folder, stock) {
				found = true
				break
			}
		}

		if !found {
			foldersToDelete = append(foldersToDelete, folder)
			fmt.Printf("Stock folder %s not in watchlist, marking for deletion\n", folder)
		}
	}

	return foldersToDelete
}

func DeleteFoldersNotOnWatchList(directoryPath string, foldersToDelete []string) {
	for _, folder := range foldersToDelete {
		folderPath := filepath.Join(directoryPath, folder)
		fmt.Printf("Deleting folder: %s\n", folderPath)

		err := os.RemoveAll(folderPath)
		if err != nil {
			fmt.Printf("Error deleting folder %s: %v\n", folderPath, err)
		} else {
			fmt.Printf("Successfully deleted folder: %s\n", folderPath)
		}
	}
}
