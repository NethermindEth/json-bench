package main

import (
	"reflect"
	"testing"
)

func TestGenerateIsDeterministic(t *testing.T) {
	opts := options{maxBlock: 25_900_000, proofSeed: 13, stateSeed: 19, callSeed: 21, traceFrom: 15_123_456, traceCount: 25}

	if !reflect.DeepEqual(generate(opts), generate(opts)) {
		t.Fatal("the same options must give the same corpus")
	}
}

func TestGenerateCounts(t *testing.T) {
	opts := options{maxBlock: 25_900_000, proofSeed: 13, stateSeed: 19, callSeed: 21, traceFrom: 15_123_456, traceCount: 25}
	want := map[string]int{
		"proofs-spread.jsonl": 25 * 3,
		"proofs-random.jsonl": 60 * 5,
		"state-reads.jsonl":   60 * 6,
		"calls.jsonl":         120 * 3 * 2,
		"trace-blocks.jsonl":  25 * 2,
	}

	for name, requests := range generate(opts) {
		if len(requests) != want[name] {
			t.Errorf("%s: %d requests, want %d", name, len(requests), want[name])
		}
	}
}

func TestEvenHeightsSpanTheRange(t *testing.T) {
	heights := evenHeights(15_123_456, 25_900_000, 25)

	if heights[0] != 15_123_456 || heights[len(heights)-1] != 25_900_000 {
		t.Fatalf("heights run %d..%d, want 15123456..25900000", heights[0], heights[len(heights)-1])
	}
}

func TestSeededHeightsStayInRangeAndDistinct(t *testing.T) {
	heights := seededHeights(13, 60, 4_500_000, 25_900_000)
	seen := map[uint64]bool{}

	for _, h := range heights {
		if h < 4_500_000 || h > 25_900_000 {
			t.Fatalf("height %d outside 4500000..25900000", h)
		}
		if seen[h] {
			t.Fatalf("height %d repeated", h)
		}
		seen[h] = true
	}
}

func TestStorageKeyIsFullWidth(t *testing.T) {
	if got := storageKey(0); len(got) != 66 {
		t.Fatalf("storage key %q has %d characters, want 66", got, len(got))
	}
}
