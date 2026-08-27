package config

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
)

// callsFileColumns is the column count of a requests CSV: id, name, method,
// payload (see generator.GenerateK6Requests, which writes it).
const callsFileColumns = 4

const (
	callsFileNameColumn   = 1
	callsFileMethodColumn = 2
)

// LoadCallsFileMethods reads a pre-generated requests CSV and returns the
// distinct RPC methods it exercises, in first-seen order. The methods come from
// the `method` column, which the k6 script tags every request with as
// `rpc_method`; that is what the per-method breakdown is keyed on, so the `name`
// column is free to be any label.
func LoadCallsFileMethods(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open calls file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = callsFileColumns
	reader.ReuseRecord = true

	methods := make([]string, 0)
	seen := make(map[string]struct{})
	for row := 1; ; row++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse calls file %s: %w", path, err)
		}
		method := record[callsFileMethodColumn]
		if method == "" {
			return nil, fmt.Errorf("calls file %s row %d has an empty method (column %d)", path, row, callsFileMethodColumn+1)
		}
		if _, dup := seen[method]; dup {
			continue
		}
		seen[method] = struct{}{}
		methods = append(methods, method)
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("calls file %s contains no requests (expected rows of id,name,method,payload)", path)
	}
	return methods, nil
}

// MethodKeys reports the tag and the identifiers that the per-method breakdown
// is keyed on. A calls-file run keys on the `rpc_method` tag over the distinct
// methods in that file, because its request names are arbitrary labels that need
// not match the declared calls. Otherwise it keys on `req_name` over the
// declared call names.
//
// Both the k6 threshold registration (which is what makes k6 emit the
// tag-filtered submetrics at all) and the metrics collection must agree on this,
// or the per-method breakdown comes back empty.
func (c *Config) MethodKeys() (tag string, identifiers []string) {
	if c.UsesCallsFile() {
		return "rpc_method", c.CallsFileMethods
	}
	identifiers = make([]string, 0, len(c.Calls))
	for _, call := range c.Calls {
		identifier := call.Name
		if identifier == "" {
			identifier = call.Method
		}
		identifiers = append(identifiers, identifier)
	}
	return "req_name", identifiers
}
