package engine

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/jsonrpc-bench/runner/config"
)

// reqsCountThresholdFactor pads the generated sequence above the requests a run
// should need, so a run that dispatches slightly ahead of schedule does not
// exhaust it.
const reqsCountThresholdFactor = 0.1

// sequenceColumns is the requests CSV layout: id, name, method, payload.
const sequenceColumns = 4

const (
	columnID = iota
	columnName
	columnMethod
	columnPayload
)

// Request is one pre-rendered JSON-RPC call. The payload is rendered once and
// reused for every client, so all clients issue byte-identical requests.
type Request struct {
	ID      int
	Name    string
	Method  string
	Payload []byte
}

// BuildSequence draws the run's requests by weighted sampling. With a non-zero
// cfg.Seed the sequence is fixed, which is what makes two runs of one config
// comparable; the CSV form is the interchange format that lets separate runs on
// separate hosts replay the same traffic.
func BuildSequence(cfg *config.Config) ([]Request, error) {
	count, err := sequenceLength(cfg)
	if err != nil {
		return nil, err
	}

	totalWeight := 0
	for _, call := range cfg.Calls {
		totalWeight += call.Weight
	}
	if totalWeight <= 0 {
		return nil, errors.New("calls must declare a positive total weight")
	}

	rng := newRNG(cfg.Seed)
	out := make([]Request, 0, count)
	for id := 1; id <= count; id++ {
		draw := rng.Float64() * float64(totalWeight)
		cumulative := 0.0
		for _, call := range cfg.Calls {
			cumulative += float64(call.Weight)
			if draw >= cumulative {
				continue
			}
			rpcCall, err := call.Sample(rng)
			if err != nil {
				return nil, fmt.Errorf("failed to sample call %s: %w", call.Name, err)
			}
			payload, err := json.Marshal(map[string]any{
				"id":      id,
				"jsonrpc": "2.0",
				"method":  rpcCall.Method,
				"params":  rpcCall.Params,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to marshal payload: %w", err)
			}
			out = append(out, Request{ID: id, Name: call.Name, Method: rpcCall.Method, Payload: payload})
			break
		}
	}
	return out, nil
}

func sequenceLength(cfg *config.Config) (int, error) {
	if cfg.RPS <= 0 {
		return cfg.Iterations, nil
	}
	duration, err := time.ParseDuration(cfg.Duration)
	if err != nil {
		return 0, fmt.Errorf("failed to parse config duration: %w", err)
	}
	return int(math.Ceil(float64(cfg.RPS) * duration.Seconds() * (1.0 + reqsCountThresholdFactor))), nil
}

// newRNG returns the generator behind request sampling: fixed by seed when
// non-zero, so two runs of one config issue byte-identical request sequences;
// otherwise seeded from the clock.
func newRNG(seed int64) *rand.Rand {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return rand.New(rand.NewSource(seed))
}

// WriteSequenceCSV renders a sequence to the requests CSV format.
func WriteSequenceCSV(w io.Writer, requests []Request) error {
	writer := csv.NewWriter(w)
	for _, req := range requests {
		row := []string{strconv.Itoa(req.ID), req.Name, req.Method, string(req.Payload)}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("failed to write request %d: %w", req.ID, err)
		}
	}
	writer.Flush()
	return writer.Error()
}

// LoadSequenceCSV reads a pre-generated requests CSV. The method column drives
// the per-method breakdown, so the name column is free to be any label.
func LoadSequenceCSV(path string) ([]Request, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open calls file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = sequenceColumns

	var out []Request
	for row := 1; ; row++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse calls file %s: %w", path, err)
		}
		id, err := strconv.Atoi(record[columnID])
		if err != nil {
			return nil, fmt.Errorf("calls file %s row %d has a non-numeric id %q", path, row, record[columnID])
		}
		if record[columnMethod] == "" {
			return nil, fmt.Errorf("calls file %s row %d has an empty method", path, row)
		}
		out = append(out, Request{
			ID:      id,
			Name:    record[columnName],
			Method:  record[columnMethod],
			Payload: []byte(record[columnPayload]),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("calls file %s contains no requests (expected rows of id,name,method,payload)", path)
	}
	return out, nil
}

// Sequence resolves the run's traffic: a pre-generated file when the config
// names one, otherwise a freshly drawn sequence.
func Sequence(cfg *config.Config) ([]Request, error) {
	if cfg.CallsFile != "" {
		return LoadSequenceCSV(cfg.CallsFile)
	}
	return BuildSequence(cfg)
}
