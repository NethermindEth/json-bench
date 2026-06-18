package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"math/rand/v2"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/tracers"
)

type TraceQueries struct {
	BlockHashes  []common.Hash          `json:"blockHashes"`
	TraceConfigs []*tracers.TraceConfig `json:"traceConfigs"`
	ResultHashes []common.Hash          `json:"resultHashes"`
}

func generateTrace(blockHash common.Hash, traceConfig *tracers.TraceConfig) map[string]any {
	return map[string]any{
		"method": "debug_traceBlockByHash",
		"params": []any{
			blockHash.String(),
			traceConfig,
		},
	}
}

func main() {
	input := flag.String("input", "", "Input file to generate historical blocks queries from. See https://github.com/ethereum/go-ethereum/tree/master/cmd/workload for more details.")
	output := flag.String("output", "", "Output file to save the historical blocks queries to.")
	flag.Parse()

	if input == nil || *input == "" || output == nil || *output == "" {
		slog.Error("Input and output flags are required")
		flag.Usage()
		os.Exit(1)
	}

	data, err := os.ReadFile(*input)
	if err != nil {
		slog.Error("Failed to read file", "error", err)
		os.Exit(1)
	}

	traceQueries := TraceQueries{}
	if err := json.Unmarshal(data, &traceQueries); err != nil {
		slog.Error("Failed to unmarshal data", "error", err)
		os.Exit(1)
	}

	outputFile, err := os.Create(*output)
	if err != nil {
		slog.Error("Failed to create output file", "error", err)
		os.Exit(1)
	}
	defer outputFile.Close()
	encoder := json.NewEncoder(outputFile)

	queries := []map[string]any{}
	for index, blockHash := range traceQueries.BlockHashes {
		queries = append(queries, generateTrace(blockHash, traceQueries.TraceConfigs[index]))
	}

	rand.Shuffle(len(queries), func(i, j int) {
		queries[i], queries[j] = queries[j], queries[i]
	})

	for _, query := range queries {
		if err := encoder.Encode(query); err != nil {
			slog.Error("Failed to encode data", "error", err)
			os.Exit(1)
		}
	}
}
