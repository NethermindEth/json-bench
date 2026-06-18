package main

import (
	"os"

	"github.com/jsonrpc-bench/runner/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
