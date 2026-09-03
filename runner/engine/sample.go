package engine

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Outcome classifies what happened to one request. A JSON-RPC error arrives as
// HTTP 200, so HTTP status alone cannot answer whether a call succeeded — which
// is the distinction the whole engine exists to make.
type Outcome string

const (
	OutcomeOK        Outcome = "ok"
	OutcomeRPCError  Outcome = "rpc_error"
	OutcomeRPCNull   Outcome = "rpc_null"
	OutcomeHTTPError Outcome = "http_error"
	OutcomeTruncated Outcome = "truncated"
	OutcomeTimeout   Outcome = "timeout"
	OutcomeTransport Outcome = "transport"
)

// Outcomes lists every class, in report order.
var Outcomes = []Outcome{
	OutcomeOK, OutcomeRPCNull, OutcomeRPCError,
	OutcomeHTTPError, OutcomeTruncated, OutcomeTimeout, OutcomeTransport,
}

// IsError reports whether the outcome counts against the error rate. A null
// result is a successful call that returned nothing, so it is not an error —
// but it is tallied separately, because "fast because it returned nothing" is
// a real archive-node failure mode.
func (o Outcome) IsError() bool {
	switch o {
	case OutcomeOK, OutcomeRPCNull:
		return false
	default:
		return true
	}
}

// Phases is the latency decomposition, mirroring the k6 metric family so the
// numbers remain comparable. Duration deliberately excludes connection setup.
type Phases struct {
	Blocked    time.Duration
	DNS        time.Duration
	Connecting time.Duration
	TLS        time.Duration
	Sending    time.Duration
	Waiting    time.Duration
	Receiving  time.Duration
}

// Duration is sending + waiting + receiving, which is what k6 reports as
// http_req_duration.
func (p Phases) Duration() time.Duration { return p.Sending + p.Waiting + p.Receiving }

// Sample is one request's full record. Retaining these is what makes exact
// percentiles, real second moments and offline re-aggregation possible.
type Sample struct {
	Client     string
	ClientType string
	Name       string
	Method     string
	RequestID  int

	Scheduled time.Time
	Start     time.Time
	Queue     time.Duration

	Phases           Phases
	Status           int
	Outcome          Outcome
	RPCCode          int
	RequestBytes     int
	ResponseBytes    int
	ConnectionReused bool
	Error            string
}

// Service is the time the endpoint took: send to last byte.
func (s Sample) Service() time.Duration { return s.Phases.Duration() }

// Total is the time from when the request was due to when it completed. It
// exceeds Service exactly when the generator could not dispatch on schedule,
// which is the coordinated-omission error that makes a saturated run look fast.
func (s Sample) Total() time.Duration { return s.Queue + s.Service() }

type sampleRecord struct {
	TS               float64 `json:"ts"`
	Client           string  `json:"client"`
	ClientType       string  `json:"client_type,omitempty"`
	Name             string  `json:"name"`
	Method           string  `json:"method"`
	RequestID        int     `json:"req_id"`
	QueueNS          int64   `json:"queue_ns"`
	ServiceNS        int64   `json:"service_ns"`
	BlockedNS        int64   `json:"blocked_ns"`
	DNSNS            int64   `json:"dns_ns"`
	ConnectingNS     int64   `json:"connecting_ns"`
	TLSNS            int64   `json:"tls_ns"`
	SendingNS        int64   `json:"sending_ns"`
	WaitingNS        int64   `json:"waiting_ns"`
	ReceivingNS      int64   `json:"receiving_ns"`
	Status           int     `json:"status"`
	Outcome          Outcome `json:"outcome"`
	RPCCode          int     `json:"rpc_code,omitempty"`
	RequestBytes     int     `json:"req_bytes"`
	ResponseBytes    int     `json:"resp_bytes"`
	ConnectionReused bool    `json:"conn_reused"`
	Error            string  `json:"error,omitempty"`
}

// SampleWriter streams samples to newline-delimited JSON, gzipped.
type SampleWriter struct {
	path string
	file *os.File
	gz   *gzip.Writer
	enc  *json.Encoder
}

// NewSampleWriter creates the sample file. A nil writer is valid and discards,
// so callers need not branch on whether sample capture was requested.
func NewSampleWriter(path string) (*SampleWriter, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create sample directory: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create sample file: %w", err)
	}
	gz := gzip.NewWriter(file)
	return &SampleWriter{path: path, file: file, gz: gz, enc: json.NewEncoder(gz)}, nil
}

func (w *SampleWriter) Write(s Sample) error {
	if w == nil {
		return nil
	}
	return w.enc.Encode(sampleRecord{
		TS:               float64(s.Start.UnixNano()) / 1e9,
		Client:           s.Client,
		ClientType:       s.ClientType,
		Name:             s.Name,
		Method:           s.Method,
		RequestID:        s.RequestID,
		QueueNS:          s.Queue.Nanoseconds(),
		ServiceNS:        s.Service().Nanoseconds(),
		BlockedNS:        s.Phases.Blocked.Nanoseconds(),
		DNSNS:            s.Phases.DNS.Nanoseconds(),
		ConnectingNS:     s.Phases.Connecting.Nanoseconds(),
		TLSNS:            s.Phases.TLS.Nanoseconds(),
		SendingNS:        s.Phases.Sending.Nanoseconds(),
		WaitingNS:        s.Phases.Waiting.Nanoseconds(),
		ReceivingNS:      s.Phases.Receiving.Nanoseconds(),
		Status:           s.Status,
		Outcome:          s.Outcome,
		RPCCode:          s.RPCCode,
		RequestBytes:     s.RequestBytes,
		ResponseBytes:    s.ResponseBytes,
		ConnectionReused: s.ConnectionReused,
		Error:            s.Error,
	})
}

func (w *SampleWriter) Close() error {
	if w == nil {
		return nil
	}
	if err := w.gz.Close(); err != nil {
		w.file.Close()
		return err
	}
	return w.file.Close()
}

// Path is where the samples were written, for logging.
func (w *SampleWriter) Path() string {
	if w == nil {
		return ""
	}
	return w.path
}

// ReadSamples decodes a sample file back into records, which is what offline
// re-aggregation and the A/B comparison read.
func ReadSamples(r io.Reader) ([]Sample, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to open sample stream: %w", err)
	}
	defer gz.Close()

	var out []Sample
	dec := json.NewDecoder(gz)
	for {
		var rec sampleRecord
		if err := dec.Decode(&rec); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode sample: %w", err)
		}
		sec := int64(rec.TS)
		out = append(out, Sample{
			Client:     rec.Client,
			ClientType: rec.ClientType,
			Name:       rec.Name,
			Method:     rec.Method,
			RequestID:  rec.RequestID,
			Start:      time.Unix(sec, int64((rec.TS-float64(sec))*1e9)),
			Queue:      time.Duration(rec.QueueNS),
			Phases: Phases{
				Blocked:    time.Duration(rec.BlockedNS),
				DNS:        time.Duration(rec.DNSNS),
				Connecting: time.Duration(rec.ConnectingNS),
				TLS:        time.Duration(rec.TLSNS),
				Sending:    time.Duration(rec.SendingNS),
				Waiting:    time.Duration(rec.WaitingNS),
				Receiving:  time.Duration(rec.ReceivingNS),
			},
			Status:           rec.Status,
			Outcome:          rec.Outcome,
			RPCCode:          rec.RPCCode,
			RequestBytes:     rec.RequestBytes,
			ResponseBytes:    rec.ResponseBytes,
			ConnectionReused: rec.ConnectionReused,
			Error:            rec.Error,
		})
	}
	return out, nil
}
