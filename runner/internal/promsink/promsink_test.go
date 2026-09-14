package promsink

import (
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/golang/snappy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The encoder below exists only to exercise the decoder against a payload built
// independently of it. The engine's own remote-write encoder is a separate
// concern; this keeps a bug in one from hiding a bug in the other.
func encodeVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func encodeTag(b []byte, field, wire int) []byte {
	return encodeVarint(b, uint64(field)<<3|uint64(wire))
}

func encodeBytesField(b []byte, field int, payload []byte) []byte {
	b = encodeTag(b, field, wireBytes)
	b = encodeVarint(b, uint64(len(payload)))
	return append(b, payload...)
}

func encodeLabel(name, value string) []byte {
	var b []byte
	b = encodeBytesField(b, 1, []byte(name))
	b = encodeBytesField(b, 2, []byte(value))
	return b
}

func encodeSample(value float64, ts int64) []byte {
	var b []byte
	b = encodeTag(b, 1, wire64bit)
	var bits [8]byte
	binary.LittleEndian.PutUint64(bits[:], math.Float64bits(value))
	b = append(b, bits[:]...)
	b = encodeTag(b, 2, wireVarint)
	return encodeVarint(b, uint64(ts))
}

func encodeWriteRequest(t *testing.T, series []TimeSeries) []byte {
	t.Helper()
	var req []byte
	for _, ts := range series {
		var body []byte
		names := make([]string, 0, len(ts.Labels))
		for name := range ts.Labels {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			body = encodeBytesField(body, 1, encodeLabel(name, ts.Labels[name]))
		}
		for _, s := range ts.Samples {
			body = encodeBytesField(body, 2, encodeSample(s.Value, s.TimestampMS))
		}
		req = encodeBytesField(req, 1, body)
	}
	return snappy.Encode(nil, req)
}

func TestDecodeRoundTrip(t *testing.T) {
	in := []TimeSeries{
		{
			Labels:  map[string]string{"__name__": "bench_http_reqs_total", "scenario": "nethermind", "rpc_method": "eth_call"},
			Samples: []Sample{{Value: 142, TimestampMS: 1730000000000}},
		},
		{
			Labels:  map[string]string{"__name__": "bench_http_req_duration_p99", "scenario": "nethermind"},
			Samples: []Sample{{Value: 0.0429, TimestampMS: 1730000000000}},
		},
	}

	out, err := Decode(encodeWriteRequest(t, in))
	require.NoError(t, err)
	require.Len(t, out, 2)

	assert.Equal(t, "bench_http_reqs_total", out[0].Name())
	assert.Equal(t, []string{"rpc_method", "scenario"}, out[0].LabelKeys())
	assert.EqualValues(t, 142, out[0].Samples[0].Value)
	assert.EqualValues(t, 1730000000000, out[0].Samples[0].TimestampMS)
	assert.InDelta(t, 0.0429, out[1].Samples[0].Value, 1e-9)
}

func TestDecodeRejectsMalformedPayloads(t *testing.T) {
	t.Run("not snappy", func(t *testing.T) {
		_, err := Decode([]byte("plain text"))
		assert.Error(t, err)
	})

	t.Run("truncated protobuf does not panic", func(t *testing.T) {
		full := encodeWriteRequest(t, []TimeSeries{{
			Labels:  map[string]string{"__name__": "x", "a": "b"},
			Samples: []Sample{{Value: 1, TimestampMS: 2}},
		}})
		raw, err := snappy.Decode(nil, full)
		require.NoError(t, err)

		for cut := 1; cut < len(raw); cut++ {
			_, err := Decode(snappy.Encode(nil, raw[:cut]))
			_ = err
		}
	})
}

func TestRecorderCapturesTheContract(t *testing.T) {
	rec := NewRecorder()
	srv := httptest.NewServer(rec.Handler())
	defer srv.Close()

	push := func(value float64) {
		body := encodeWriteRequest(t, []TimeSeries{{
			Labels:  map[string]string{"__name__": "bench_http_reqs_total", "scenario": "nethermind", "status": "200"},
			Samples: []Sample{{Value: value, TimestampMS: 1}},
		}})
		req, err := http.NewRequest(http.MethodPost, srv.URL+WritePath, strings.NewReader(string(body)))
		require.NoError(t, err)
		req.Header.Set("Content-Encoding", "snappy")
		req.Header.Set("Content-Type", "application/x-protobuf")
		req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
		req.Header.Set("User-Agent", "test-writer")
		req.SetBasicAuth("bench", "s3cret")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	}

	push(10)
	push(25)

	require.NoError(t, rec.Err())
	assert.Equal(t, 2, rec.Pushes())
	assert.Equal(t, []string{"bench_http_reqs_total{scenario,status}"}, rec.Keys())
	assert.Equal(t, []float64{10, 25}, rec.Series()[0].Values,
		"successive pushes accumulate so cumulative and per-window metrics can be told apart")

	headers := rec.Headers()
	assert.Equal(t, "snappy", headers["Content-Encoding"])
	assert.Equal(t, "0.1.0", headers["X-Prometheus-Remote-Write-Version"])
	assert.Equal(t, "basic <redacted>", headers["Authorization"], "a capture must be committable without leaking the credential")
	assert.NotContains(t, headers["Authorization"], "s3cret")
}

func TestRecorderRejectsUndecodableBody(t *testing.T) {
	rec := NewRecorder()
	srv := httptest.NewServer(rec.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+WritePath, "application/x-protobuf", strings.NewReader("garbage"))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Error(t, rec.Err())
}

func TestGoldenRoundTrip(t *testing.T) {
	rec := NewRecorder()
	rec.observe(&http.Request{Header: http.Header{
		"Content-Encoding": []string{"snappy"},
		"Authorization":    []string{"Basic dXNlcjpwYXNz"},
	}}, []TimeSeries{
		{Labels: map[string]string{"__name__": "bench_vus", "testid": "t"}, Samples: []Sample{{Value: 1}}},
		{Labels: map[string]string{"__name__": "bench_http_reqs_total", "testid": "t", "scenario": "s"}, Samples: []Sample{{Value: 2}}},
	})

	var out strings.Builder
	require.NoError(t, rec.WriteGolden(&out, "test v1.2.3"))

	assert.Contains(t, out.String(), "# source: test v1.2.3")
	assert.NotContains(t, out.String(), "dXNlcjpwYXNz")

	headers, keys, err := ParseGolden([]byte(out.String()))
	require.NoError(t, err)
	assert.Equal(t, "snappy", headers["Content-Encoding"])
	assert.Equal(t, []string{
		"bench_http_reqs_total{scenario,testid}",
		"bench_vus{testid}",
	}, keys, "series are sorted so a golden file diffs cleanly")
}
