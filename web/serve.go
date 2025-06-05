package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	// Parse command-line flags
	port := flag.Int("port", 8080, "Port to serve the reports on")
	resultsDir := flag.String("results", "results", "Directory containing the benchmark results")
	flag.Parse()

	// Check if results directory exists
	if _, err := os.Stat(*resultsDir); os.IsNotExist(err) {
		log.Fatalf("Results directory does not exist: %s", *resultsDir)
	}

	// Create a file server handler
	fs := http.FileServer(http.Dir(*resultsDir))

	// Set up routes
	http.Handle("/", fs)
	http.HandleFunc("/report/latest", func(w http.ResponseWriter, r *http.Request) {
		// Find the latest report
		latestReport, err := findLatestReport(*resultsDir)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to find latest report: %v", err), http.StatusInternalServerError)
			return
		}

		// Redirect to the latest report
		http.Redirect(w, r, "/"+latestReport, http.StatusFound)
	})

	// Start the server
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting report server on http://localhost%s", addr)
	log.Printf("Latest report available at http://localhost%s/report/latest", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// findLatestReport finds the most recently modified HTML report in the results directory
func findLatestReport(resultsDir string) (string, error) {
	var latestFile string
	var latestTime int64

	// Walk the results directory
	err := filepath.Walk(resultsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Only consider HTML files
		if filepath.Ext(path) != ".html" {
			return nil
		}

		// Check if this file is newer than the current latest
		modTime := info.ModTime().Unix()
		if modTime > latestTime {
			latestTime = modTime
			latestFile = path
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	if latestFile == "" {
		return "", fmt.Errorf("no HTML reports found in %s", resultsDir)
	}

	// Return the path relative to the results directory
	relPath, err := filepath.Rel(resultsDir, latestFile)
	if err != nil {
		return "", err
	}

	return relPath, nil
}
