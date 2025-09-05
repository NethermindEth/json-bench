package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"math/rand/v2"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type HistoryQueries struct {
	BlockNumbers   []uint64       `json:"blockNumbers"`
	BlockHashes    []common.Hash  `json:"blockHashes"`
	TxCount        []int          `json:"txCount,omitempty"`
	TxHashIndex    []int          `json:"txHashIndex,omitempty"`
	TxHashes       []*common.Hash `json:"txHashes,omitempty"`
	ReceiptsHashes []common.Hash  `json:"blockReceiptsHashes,omitempty"`
}

func generateBlockByNumber(blockNumber uint64, hydrated bool) map[string]any {
	return map[string]any{
		"method": "eth_getBlockByNumber",
		"params": []any{hexutil.Uint64(blockNumber).String(), hydrated},
	}
}
func generateBlockByHash(blockHash common.Hash, hydrated bool) map[string]any {
	return map[string]any{
		"method": "eth_getBlockByHash",
		"params": []any{blockHash.String(), hydrated},
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

	historyQueries := HistoryQueries{}
	if err := json.Unmarshal(data, &historyQueries); err != nil {
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
	for index, blockNumber := range historyQueries.BlockNumbers {
		r := rand.IntN(4)
		if r == 0 {
			queries = append(queries, generateBlockByNumber(blockNumber, false))
		} else if r == 1 {
			queries = append(queries, generateBlockByHash(historyQueries.BlockHashes[index], false))
		} else if r == 2 {
			queries = append(queries, generateBlockByNumber(blockNumber, true))
		} else {
			queries = append(queries, generateBlockByHash(historyQueries.BlockHashes[index], true))
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
