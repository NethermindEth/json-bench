// Package promrw writes metrics to a Prometheus remote-write endpoint.
//
// Remote write v1 needs three protobuf messages, so they are encoded here
// rather than pulling prometheus/prometheus into the module for a wire format:
//
//	WriteRequest { repeated TimeSeries timeseries = 1 }
//	TimeSeries   { repeated Label labels = 1; repeated Sample samples = 2 }
//	Label        { string name = 1; string value = 2 }
//	Sample       { double value = 1; int64 timestamp = 2 }
package promrw

import (
	"encoding/binary"
	"math"
	"sort"

	"github.com/golang/snappy"
)

const (
	wireVarint = 0
	wire64bit  = 1
	wireBytes  = 2
)

// Series is one metric sample to write.
type Series struct {
	Name        string
	Labels      map[string]string
	Value       float64
	TimestampMS int64
}

// Encode renders a write request: snappy over the protobuf, which is what the
// Content-Encoding and Content-Type headers on a v1 push declare.
func Encode(series []Series) []byte {
	var req []byte
	for _, s := range series {
		req = appendBytesField(req, 1, encodeTimeSeries(s))
	}
	return snappy.Encode(nil, req)
}

func encodeTimeSeries(s Series) []byte {
	var out []byte

	// Prometheus expects labels sorted by name, with the metric name carried as
	// __name__.
	names := make([]string, 0, len(s.Labels)+1)
	names = append(names, "__name__")
	for name := range s.Labels {
		if name != "__name__" {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		value := s.Labels[name]
		if name == "__name__" {
			value = s.Name
		}
		if value == "" {
			continue
		}
		out = appendBytesField(out, 1, encodeLabel(name, value))
	}

	return appendBytesField(out, 2, encodeSample(s.Value, s.TimestampMS))
}

func encodeLabel(name, value string) []byte {
	var out []byte
	out = appendBytesField(out, 1, []byte(name))
	return appendBytesField(out, 2, []byte(value))
}

func encodeSample(value float64, timestampMS int64) []byte {
	out := appendTag(nil, 1, wire64bit)
	var bits [8]byte
	binary.LittleEndian.PutUint64(bits[:], math.Float64bits(sanitizeValue(value)))
	out = append(out, bits[:]...)
	out = appendTag(out, 2, wireVarint)
	return appendVarint(out, uint64(timestampMS))
}

// sanitizeValue keeps NaN and the infinities off the wire. They arise from
// dividing by a zero count, and Prometheus stores them as real samples that
// then poison every aggregation over the series.
func sanitizeValue(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

func appendTag(b []byte, field, wire int) []byte {
	return appendVarint(b, uint64(field)<<3|uint64(wire))
}

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func appendBytesField(b []byte, field int, payload []byte) []byte {
	b = appendTag(b, field, wireBytes)
	b = appendVarint(b, uint64(len(payload)))
	return append(b, payload...)
}
