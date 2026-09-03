// Package promsink decodes Prometheus remote-write v1 payloads so a load
// engine's Prometheus output can be captured and asserted against, without
// standing up a Prometheus.
//
// It carries a minimal reader for the three messages remote write v1 uses
// rather than depending on prometheus/prometheus for a protobuf:
//
//	WriteRequest { repeated TimeSeries timeseries = 1 }
//	TimeSeries   { repeated Label labels = 1; repeated Sample samples = 2 }
//	Label        { string name = 1; string value = 2 }
//	Sample       { double value = 1; int64 timestamp = 2 }
package promsink

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"github.com/golang/snappy"
)

// Sample is one observation of a series.
type Sample struct {
	Value       float64
	TimestampMS int64
}

// TimeSeries is one decoded series: its labels and the samples in this payload.
type TimeSeries struct {
	Labels  map[string]string
	Samples []Sample
}

// Name returns the series' metric name.
func (ts TimeSeries) Name() string { return ts.Labels["__name__"] }

// LabelKeys returns the series' label names, sorted, excluding __name__.
func (ts TimeSeries) LabelKeys() []string {
	keys := make([]string, 0, len(ts.Labels))
	for k := range ts.Labels {
		if k != "__name__" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// Decode snappy-decompresses and parses a remote-write request body.
func Decode(body []byte) ([]TimeSeries, error) {
	raw, err := snappy.Decode(nil, body)
	if err != nil {
		return nil, fmt.Errorf("snappy decode: %w", err)
	}

	var out []TimeSeries
	r := &reader{b: raw}
	for !r.done() {
		field, wire, err := r.tag()
		if err != nil {
			return nil, err
		}
		if field != 1 {
			if err := r.skip(wire); err != nil {
				return nil, err
			}
			continue
		}
		chunk, err := r.lengthDelimited(wire)
		if err != nil {
			return nil, err
		}
		ts, err := decodeTimeSeries(chunk)
		if err != nil {
			return nil, err
		}
		out = append(out, ts)
	}
	return out, nil
}

func decodeTimeSeries(b []byte) (TimeSeries, error) {
	ts := TimeSeries{Labels: make(map[string]string, 8)}
	r := &reader{b: b}
	for !r.done() {
		field, wire, err := r.tag()
		if err != nil {
			return ts, err
		}
		switch field {
		case 1:
			chunk, err := r.lengthDelimited(wire)
			if err != nil {
				return ts, err
			}
			name, value, err := decodeLabel(chunk)
			if err != nil {
				return ts, err
			}
			ts.Labels[name] = value
		case 2:
			chunk, err := r.lengthDelimited(wire)
			if err != nil {
				return ts, err
			}
			sample, err := decodeSample(chunk)
			if err != nil {
				return ts, err
			}
			ts.Samples = append(ts.Samples, sample)
		default:
			if err := r.skip(wire); err != nil {
				return ts, err
			}
		}
	}
	return ts, nil
}

func decodeLabel(b []byte) (name, value string, err error) {
	r := &reader{b: b}
	for !r.done() {
		field, wire, err := r.tag()
		if err != nil {
			return "", "", err
		}
		switch field {
		case 1, 2:
			chunk, err := r.lengthDelimited(wire)
			if err != nil {
				return "", "", err
			}
			if field == 1 {
				name = string(chunk)
			} else {
				value = string(chunk)
			}
		default:
			if err := r.skip(wire); err != nil {
				return "", "", err
			}
		}
	}
	return name, value, nil
}

func decodeSample(b []byte) (Sample, error) {
	var s Sample
	r := &reader{b: b}
	for !r.done() {
		field, wire, err := r.tag()
		if err != nil {
			return s, err
		}
		switch {
		case field == 1 && wire == wire64bit:
			bits, err := r.fixed64()
			if err != nil {
				return s, err
			}
			s.Value = math.Float64frombits(bits)
		case field == 2 && wire == wireVarint:
			v, err := r.varint()
			if err != nil {
				return s, err
			}
			s.TimestampMS = int64(v)
		default:
			if err := r.skip(wire); err != nil {
				return s, err
			}
		}
	}
	return s, nil
}

const (
	wireVarint = 0
	wire64bit  = 1
	wireBytes  = 2
	wire32bit  = 5
)

type reader struct {
	b []byte
	i int
}

func (r *reader) done() bool { return r.i >= len(r.b) }

func (r *reader) tag() (field int, wire int, err error) {
	key, err := r.varint()
	if err != nil {
		return 0, 0, err
	}
	return int(key >> 3), int(key & 7), nil
}

func (r *reader) varint() (uint64, error) {
	var v uint64
	var shift uint
	for {
		if r.done() {
			return 0, fmt.Errorf("truncated varint at offset %d", r.i)
		}
		if shift > 63 {
			return 0, fmt.Errorf("varint overflow at offset %d", r.i)
		}
		c := r.b[r.i]
		r.i++
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, nil
		}
		shift += 7
	}
}

func (r *reader) fixed64() (uint64, error) {
	if r.i+8 > len(r.b) {
		return 0, fmt.Errorf("truncated 64-bit value at offset %d", r.i)
	}
	v := binary.LittleEndian.Uint64(r.b[r.i : r.i+8])
	r.i += 8
	return v, nil
}

func (r *reader) lengthDelimited(wire int) ([]byte, error) {
	if wire != wireBytes {
		return nil, fmt.Errorf("expected a length-delimited field, got wire type %d", wire)
	}
	n, err := r.varint()
	if err != nil {
		return nil, err
	}
	if uint64(r.i)+n > uint64(len(r.b)) {
		return nil, fmt.Errorf("length-delimited field of %d bytes overruns the payload", n)
	}
	chunk := r.b[r.i : r.i+int(n)]
	r.i += int(n)
	return chunk, nil
}

func (r *reader) skip(wire int) error {
	switch wire {
	case wireVarint:
		_, err := r.varint()
		return err
	case wire64bit:
		_, err := r.fixed64()
		return err
	case wireBytes:
		_, err := r.lengthDelimited(wire)
		return err
	case wire32bit:
		if r.i+4 > len(r.b) {
			return fmt.Errorf("truncated 32-bit value at offset %d", r.i)
		}
		r.i += 4
		return nil
	default:
		return fmt.Errorf("unsupported wire type %d", wire)
	}
}
