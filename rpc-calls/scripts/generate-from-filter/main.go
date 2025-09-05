package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"math/rand/v2"
	"os"

	"github.com/ethereum/go-ethereum/common"
)

type FilterQuery struct {
	FromBlock  int64            `json:"fromBlock"`
	ToBlock    int64            `json:"toBlock"`
	Address    []common.Address `json:"address"`
	Topics     [][]common.Hash  `json:"topics"`
	ResultHash *common.Hash     `json:"resultHash,omitempty"`
}

func generateFilter(filterQuery FilterQuery) map[string]any {
	return map[string]any{
		"method": "eth_getLogs",
		"params": []any{
			map[string]any{
				"fromBlock": filterQuery.FromBlock,
				"toBlock":   filterQuery.ToBlock,
				"address":   filterQuery.Address,
				"topics":    filterQuery.Topics,
			},
		},
	}
}

func main() {
	input := flag.String("input", "", "Input file to generate logs queries from. See https://github.com/ethereum/go-ethereum/tree/master/cmd/workload for more details.")
	output := flag.String("output", "", "Output file to save the logs queries to.")
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

	queriesData := [][]FilterQuery{}
	if err := json.Unmarshal(data, &queriesData); err != nil {
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
	for _, filterQueries := range queriesData {
		for _, filterQuery := range filterQueries {
			queries = append(queries, generateFilter(filterQuery))
		}
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
