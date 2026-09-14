package engine

import "github.com/jsonrpc-bench/runner/types"

// The transport a target is reached over is decided by its URL, and the client
// registry validates that URL before the engine ever sees it. Both therefore
// have to agree on what is reachable, so the rule lives in types and is aliased
// here rather than stated twice.
type TransportKind = types.TransportKind

const (
	TransportHTTP      = types.TransportHTTP
	TransportWebSocket = types.TransportWebSocket
	TransportIPC       = types.TransportIPC
)

var Transports = types.Transports

func TransportKindFor(raw string) (TransportKind, error) { return types.TransportKindFor(raw) }

func ipcPath(raw string) string { return types.IPCPath(raw) }
