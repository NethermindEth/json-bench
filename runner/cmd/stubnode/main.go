// Command stubnode runs the JSON-RPC stub as a standalone endpoint, so a real
// `runner benchmark` can be pointed at a node whose latency and failure rates
// are known exactly.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/jsonrpc-bench/runner/internal/stubnode"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18545", "Address to serve JSON-RPC on")
	configPath := flag.String("config", "", "Path to a stub config JSON file (see -print-config for the shape)")
	seed := flag.Int64("seed", 0, "Override the config seed")
	printConfig := flag.Bool("print-config", false, "Print the default config and exit")
	flag.Parse()

	cfg := stubnode.DefaultConfig()
	if *configPath != "" {
		data, err := os.ReadFile(*configPath)
		if err != nil {
			log.Fatalf("stubnode: %v", err)
		}
		cfg = stubnode.Config{}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			log.Fatalf("stubnode: %s: %v", *configPath, err)
		}
	}
	if *seed != 0 {
		cfg.Seed = *seed
	}

	if *printConfig {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(cfg); err != nil {
			log.Fatalf("stubnode: %v", err)
		}
		return
	}

	stub, err := stubnode.New(cfg)
	if err != nil {
		log.Fatalf("stubnode: %v", err)
	}

	fmt.Fprintf(os.Stderr, "stubnode: JSON-RPC on http://%s (seed %d), tally at http://%s/__stats\n",
		*listen, cfg.Seed, *listen)
	if err := http.ListenAndServe(*listen, stub.Handler()); err != nil {
		log.Fatalf("stubnode: %v", err)
	}
}
