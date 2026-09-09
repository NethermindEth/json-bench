package config

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
)

// callsFileColumns is the column count of a requests CSV: id, name, method,
// payload (see engine.WriteSequenceCSV, which writes it).
const callsFileColumns = 4

const (
	callsFileNameColumn   = 1
	callsFileMethodColumn = 2
)

// LoadCallsFileMethods reads a pre-generated requests CSV and returns the
// distinct RPC methods it exercises, in first-seen order, from the `method`
// column that every request is tagged with as `rpc_method`.
func LoadCallsFileMethods(path string) ([]string, error) {
	methods, _, err := loadCallsFileLabels(path)
	return methods, err
}

// LoadCallsFileLabels also returns the distinct request names, which is what the
// breakdown is keyed on: one method driven through several parameter shapes is
// told apart only by name.
func LoadCallsFileLabels(path string) (methods, names []string, err error) {
	return loadCallsFileLabels(path)
}

func loadCallsFileLabels(path string) (methodsOut, namesOut []string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open calls file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = callsFileColumns
	reader.ReuseRecord = true

	methods := make([]string, 0)
	names := make([]string, 0)
	seenMethod := make(map[string]struct{})
	seenName := make(map[string]struct{})
	for row := 1; ; row++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse calls file %s: %w", path, err)
		}
		method := record[callsFileMethodColumn]
		if method == "" {
			return nil, nil, fmt.Errorf("calls file %s row %d has an empty method (column %d)", path, row, callsFileMethodColumn+1)
		}
		if _, dup := seenMethod[method]; !dup {
			seenMethod[method] = struct{}{}
			methods = append(methods, method)
		}
		if name := record[callsFileNameColumn]; name != "" {
			if _, dup := seenName[name]; !dup {
				seenName[name] = struct{}{}
				names = append(names, name)
			}
		}
	}

	if len(methods) == 0 {
		return nil, nil, fmt.Errorf("calls file %s contains no requests (expected rows of id,name,method,payload)", path)
	}
	return methods, names, nil
}
