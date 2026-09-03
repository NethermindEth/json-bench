package main

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

func mustType(t *testing.T, s string) abi.Type {
	t.Helper()
	ty, err := abi.NewType(s, "", nil)
	if err != nil {
		t.Fatalf("abi.NewType(%q): %v", s, err)
	}
	return ty
}

// TestConvertArgRejectsOutOfRange pins the loud-failure contract: a YAML integer
// the ABI type cannot represent must be an error, never a silently wrapped value
// that encodes cleanly and means something else.
func TestConvertArgRejectsOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name    string
		abiType string
		arg     any
	}{
		{"uint8 overflow", "uint8", 256},
		{"uint8 negative", "uint8", -1},
		{"uint16 overflow", "uint16", 65536},
		{"uint32 overflow", "uint32", 4294967296},
		{"uint256 negative", "uint256", -1},
		{"uint24 overflow", "uint24", 16777216},
		{"int8 overflow", "int8", 128},
		{"int8 underflow", "int8", -129},
		{"int16 overflow", "int16", 32768},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := convertArg(tc.arg, mustType(t, tc.abiType), "ctx")
			if err == nil {
				t.Fatalf("convertArg(%v, %s) should have failed, got nil", tc.arg, tc.abiType)
			}
		})
	}
}

// TestConvertArgNativeWidths checks the boundary values still convert, and to the
// concrete Go type go-ethereum's packer expects for that width.
func TestConvertArgNativeWidths(t *testing.T) {
	for _, tc := range []struct {
		abiType string
		arg     any
		want    any
	}{
		{"uint8", 255, uint8(255)},
		{"uint8", 0, uint8(0)},
		{"uint16", 65535, uint16(65535)},
		{"uint32", 4294967295, uint32(4294967295)},
		{"int8", -128, int8(-128)},
		{"int8", 127, int8(127)},
		{"int16", -32768, int16(-32768)},
	} {
		t.Run(tc.abiType, func(t *testing.T) {
			got, err := convertArg(tc.arg, mustType(t, tc.abiType), "ctx")
			if err != nil {
				t.Fatalf("convertArg: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %#v (%T), want %#v (%T)", got, got, tc.want, tc.want)
			}
		})
	}
}

// TestConvertArgWideWidthsStayBig confirms the widths go-ethereum wants as
// *big.Int are left alone — uint24/uint80/uint192 have no native Go type.
func TestConvertArgWideWidthsStayBig(t *testing.T) {
	for _, ty := range []string{"uint24", "uint80", "uint192", "uint256", "int128"} {
		t.Run(ty, func(t *testing.T) {
			got, err := convertArg("1000", mustType(t, ty), "ctx")
			if err != nil {
				t.Fatalf("convertArg: %v", err)
			}
			n, ok := got.(*big.Int)
			if !ok {
				t.Fatalf("got %T, want *big.Int", got)
			}
			if n.Int64() != 1000 {
				t.Errorf("got %s, want 1000", n)
			}
		})
	}
}

// TestConvertArgAcceptsChainlinkRoundID guards a real corpus value: Chainlink
// phase-encoded round ids exceed int64 and must survive as a *big.Int.
func TestConvertArgAcceptsChainlinkRoundID(t *testing.T) {
	got, err := convertArg("0x30000000000000400", mustType(t, "uint80"), "ctx")
	if err != nil {
		t.Fatalf("convertArg: %v", err)
	}
	n, ok := got.(*big.Int)
	if !ok {
		t.Fatalf("got %T, want *big.Int", got)
	}
	if n.String() != "55340232221128655872" { // 3<<64 | 0x400
		t.Errorf("got %s", n)
	}
}

// TestConvertIntRejectsFloat keeps the existing guard against YAML parsing a
// wide integer as a float and quietly losing precision.
func TestConvertIntRejectsFloat(t *testing.T) {
	_, err := convertArg(1.5, mustType(t, "uint256"), "ctx")
	if err == nil || !strings.Contains(err.Error(), "float") {
		t.Fatalf("want a float-specific error, got %v", err)
	}
}
