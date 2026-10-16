// Command generate-archive writes the mainnet archive corpus: historical
// eth_getProof, state reads, eth_call and block tracing at fixed or seeded
// heights. It needs no node, so the same flags always give the same files.
//
// Every storage key reads real storage: slot 0 of each token, and the balance
// mapping entry of one holder, under the mapping slot each token uses on
// mainnet. Heights below a token's deployment ask for an account that does not
// exist yet, which an archive must still answer, with an absence proof.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const zeroAddress = "0x0000000000000000000000000000000000000000"

// holder carries a balance in every token below at the time of writing.
const holder = "0x28C6c06298d514Db089934071355E5743bf21d60"

type token struct {
	name        string
	address     string
	balanceSlot int64
}

// Balance mapping slots checked on mainnet: eth_getStorageAt of the derived key
// equals balanceOf(holder) for each token.
var (
	weth = token{"WETH", "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", 3}
	usdc = token{"USDC", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", 9}
	usdt = token{"USDT", "0xdAC17F958D2ee523a2206206994597C13D831ec7", 2}
	dai  = token{"DAI", "0x6B175474E89094C44Da98b954EedeAC495271d0F", 2}
	link = token{"LINK", "0x514910771AF9Ca656af840dff83E8264EcF986CA", 1}
)

type request struct {
	Method string `json:"method"`
	Params []any  `json:"params"`
}

type options struct {
	maxBlock   uint64
	proofSeed  uint64
	stateSeed  uint64
	callSeed   uint64
	traceFrom  uint64
	traceCount int
}

func main() {
	outputDir := flag.String("output-dir", "rpc-calls/archive", "directory the corpus is written to")
	opts := options{}
	flag.Uint64Var(&opts.maxBlock, "max-block", 25_900_000, "highest block a seeded height may take")
	flag.Uint64Var(&opts.proofSeed, "proof-seed", 13, "seed of the random proof heights")
	flag.Uint64Var(&opts.stateSeed, "state-seed", 19, "seed of the state-read heights")
	flag.Uint64Var(&opts.callSeed, "call-seed", 21, "seed of the eth_call heights")
	flag.Uint64Var(&opts.traceFrom, "trace-from", 15_123_456, "lowest traced block")
	flag.IntVar(&opts.traceCount, "trace-blocks", 25, "traced blocks, spread evenly up to --max-block")
	flag.Parse()

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		slog.Error("create output directory", "error", err)
		os.Exit(1)
	}

	for name, requests := range generate(opts) {
		path := filepath.Join(*outputDir, name)
		if err := writeJSONL(path, requests); err != nil {
			slog.Error("write corpus", "file", path, "error", err)
			os.Exit(1)
		}
		slog.Info("wrote corpus", "file", path, "requests", len(requests))
	}
}

func generate(opts options) map[string][]request {
	return map[string][]request{
		"proofs-spread.jsonl": proofs(spreadHeights(), []token{weth, usdc, usdt}),
		"proofs-random.jsonl": proofs(seededHeights(opts.proofSeed, 60, 4_500_000, opts.maxBlock), []token{weth, usdc, usdt, dai, link}),
		"state-reads.jsonl":   stateReads(seededHeights(opts.stateSeed, 60, 4_500_000, opts.maxBlock), []token{weth, usdc, usdt, dai, link}),
		"calls.jsonl":         calls(seededHeights(opts.callSeed, 120, 6_500_000, opts.maxBlock), []token{weth, usdc, usdt}),
		"trace-blocks.jsonl":  traces(evenHeights(opts.traceFrom, opts.maxBlock, opts.traceCount)),
	}
}

// spreadHeights gives x*1,000,000 + 333,333 for x = 1..25: one height per
// million blocks, from 1,333,333 to 25,333,333.
func spreadHeights() []uint64 {
	heights := make([]uint64, 0, 25)
	for x := uint64(1); x <= 25; x++ {
		heights = append(heights, x*1_000_000+333_333)
	}
	return heights
}

func seededHeights(seed uint64, count int, from, to uint64) []uint64 {
	rng := rand.New(rand.NewPCG(seed, seed))
	heights := make([]uint64, 0, count)
	for len(heights) < count {
		height := from + rng.Uint64N(to-from+1)
		if !slices.Contains(heights, height) {
			heights = append(heights, height)
		}
	}
	return heights
}

func evenHeights(from, to uint64, count int) []uint64 {
	heights := make([]uint64, 0, count)
	for i := 0; i < count; i++ {
		heights = append(heights, from+(to-from)*uint64(i)/uint64(count-1))
	}
	return heights
}

func proofs(heights []uint64, tokens []token) []request {
	var requests []request
	for _, height := range heights {
		for _, t := range tokens {
			keys := []string{storageKey(0), balanceKey(t)}
			requests = append(requests, request{"eth_getProof", []any{t.address, keys, blockTag(height)}})
		}
	}
	return requests
}

func stateReads(heights []uint64, tokens []token) []request {
	var requests []request
	for _, height := range heights {
		requests = append(requests, request{"eth_getBalance", []any{holder, blockTag(height)}})
		for _, t := range tokens {
			requests = append(requests, request{"eth_getStorageAt", []any{t.address, balanceKey(t), blockTag(height)}})
		}
	}
	return requests
}

func calls(heights []uint64, tokens []token) []request {
	balanceOf := "0x70a08231" + common.Bytes2Hex(common.LeftPadBytes(common.HexToAddress(holder).Bytes(), 32))
	const totalSupply = "0x18160ddd"
	var requests []request
	for _, height := range heights {
		for _, t := range tokens {
			for _, data := range []string{balanceOf, totalSupply} {
				call := map[string]string{"from": zeroAddress, "to": t.address, "data": data}
				requests = append(requests, request{"eth_call", []any{call, blockTag(height)}})
			}
		}
	}
	return requests
}

func traces(heights []uint64) []request {
	var requests []request
	for _, height := range heights {
		requests = append(requests,
			request{"debug_traceBlockByNumber", []any{blockTag(height), map[string]string{"tracer": "callTracer"}}},
			request{"trace_block", []any{blockTag(height)}})
	}
	return requests
}

// balanceKey is keccak256(pad32(holder) ++ pad32(slot)), the storage key of
// balances[holder] in a Solidity mapping at slot.
func balanceKey(t token) string {
	return crypto.Keccak256Hash(
		common.LeftPadBytes(common.HexToAddress(holder).Bytes(), 32),
		common.LeftPadBytes(big.NewInt(t.balanceSlot).Bytes(), 32)).Hex()
}

// storageKey is a full 32-byte key: some clients reject short hex such as "0x0".
func storageKey(slot int64) string {
	return common.BigToHash(big.NewInt(slot)).Hex()
}

func blockTag(height uint64) string {
	return fmt.Sprintf("0x%x", height)
}

func writeJSONL(path string, requests []request) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, r := range requests {
		if err := encoder.Encode(r); err != nil {
			return err
		}
	}
	return nil
}
