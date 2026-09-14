package types

import (
	"fmt"
	"net/url"
	"strings"
)

// TransportKind is how a target is reached. It is recorded in the run manifest,
// because latency over a multiplexed socket and latency over pooled HTTP
// exchanges are not the same measurement.
type TransportKind string

const (
	TransportHTTP      TransportKind = "http"
	TransportWebSocket TransportKind = "websocket"
	TransportIPC       TransportKind = "ipc"
)

// TransportKindFor reads the transport out of a client URL.
//
// Nethermind, Geth, Erigon and Reth all expose the same JSON-RPC methods over
// three transports, and the choice belongs to the endpoint rather than to a
// flag: a config naming an `ipc://` path has already said what it means.
// Transports lists every transport a target can be reached over, for error
// messages and for the client registry's URL validation.
var Transports = []TransportKind{TransportHTTP, TransportWebSocket, TransportIPC}

func TransportKindFor(raw string) (TransportKind, error) {
	if raw == "" {
		return "", fmt.Errorf("no URL")
	}

	// A bare filesystem path is the shape a node's IPC endpoint is usually
	// written in, and it has no scheme to parse.
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "~/") {
		return TransportIPC, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%q is not a usable URL: %w", raw, err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return TransportHTTP, nil
	case "ws", "wss":
		return TransportWebSocket, nil
	case "ipc", "unix":
		return TransportIPC, nil
	case "":
		return "", fmt.Errorf("%q has no scheme; use http://, ws:// or ipc:// (or an absolute path for IPC)", raw)
	default:
		return "", fmt.Errorf("%q uses the unsupported scheme %q; the transports are http, ws and ipc", raw, parsed.Scheme)
	}
}

// IPCPath extracts the socket path from an IPC URL, accepting both the
// `ipc:///run/node.ipc` form and a bare path.
func IPCPath(raw string) string {
	for _, prefix := range []string{"ipc://", "unix://"} {
		if strings.HasPrefix(raw, prefix) {
			return strings.TrimPrefix(raw, prefix)
		}
	}
	return raw
}
