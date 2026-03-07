package market

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CSVProvider implements the BarsProvider interface using local CSV files for replay testing.
type CSVProvider struct {
	DataDir string
}

// NewCSVProvider creates a new CSVProvider reading from the given directory.
func NewCSVProvider(dataDir string) *CSVProvider {
	return &CSVProvider{
		DataDir: dataDir,
	}
}

// GetBars reads CSV files for the requested symbols and returns historical OHLCV data.
// It assumes CSV files are named "{symbol}.csv" (e.g., "AAPL.csv") inside dataDir.
func (p *CSVProvider) GetBars(symbols []string, interval string, limit int) (map[string][]Kline, error) {
	result := make(map[string][]Kline)

	for _, symbol := range symbols {
		klines, err := p.readCSV(symbol, limit)
		if err != nil {
			// In replay mode, if one file is missing, we might want to just skip or return error
			return nil, fmt.Errorf("failed to read replay data for %s: %w", symbol, err)
		}
		result[symbol] = klines
	}

	return result, nil
}

// readCSV reads a CSV file and parses it into Klines.
func (p *CSVProvider) readCSV(symbol string, limit int) ([]Kline, error) {
	// Remove USDT suffix if it exists for stocks, just in case
	cleanSymbol := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")
	
	filename := filepath.Join(p.DataDir, fmt.Sprintf("%s.csv", cleanSymbol))
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	// We assume a header row exists: e.g. timestamp,open,high,low,close,volume
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) <= 1 {
		return []Kline{}, nil // Only header or empty
	}

	var allKlines []Kline
	
	// Start from 1 to skip header
	for i := 1; i < len(records); i++ {
		record := records[i]
		if len(record) < 6 {
			continue // Skip malformed rows
		}

		// Try parsing timestamp
		openTime, err := parseTimestamp(record[0])
		if err != nil {
			continue 
		}

		open, _ := parseCSVFloat(record[1])
		high, _ := parseCSVFloat(record[2])
		low, _ := parseCSVFloat(record[3])
		close, _ := parseCSVFloat(record[4])
		volume, _ := parseCSVFloat(record[5])

		allKlines = append(allKlines, Kline{
			OpenTime:  openTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			CloseTime: openTime + 60000, // rough estimate (+1 min for 1m bars)
		})
	}

	// Make sure they are sorted by time
	sort.Slice(allKlines, func(i, j int) bool {
		return allKlines[i].OpenTime < allKlines[j].OpenTime
	})

	// In a real replay system, we would filter by a "current virtual time".
	// For now, we simulate fetching the LAST `limit` bars to mimic live testing.
	if len(allKlines) > limit {
		return allKlines[len(allKlines)-limit:], nil
	}

	return allKlines, nil
}

func parseTimestamp(ts string) (int64, error) {
	// First try parse as int (unix ms or sec)
	val, err := strconv.ParseInt(ts, 10, 64)
	if err == nil {
		// heuristic: if it's less than 10^12, it's probably seconds
		if val < 1000000000000 {
			return val * 1000, nil
		}
		return val, nil
	}
	
	// Try parsing specific date formats (e.g. ISO8601)
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	
	for _, layout := range layouts {
		t, err := time.Parse(layout, ts)
		if err == nil {
			return t.UnixMilli(), nil
		}
	}
	
	return 0, fmt.Errorf("could not parse timestamp: %s", ts)
}

func parseCSVFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}
