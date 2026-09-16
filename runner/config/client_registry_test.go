package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jsonrpc-bench/runner/types"
)

// The URL decides the transport, so the registry must admit every scheme the
// engine can actually dispatch on. It used to accept only http(s), which made
// every ws:// and ipc:// endpoint unreachable through configuration while the
// engine supported them — a feature that could not be switched on.
func TestClientRegistryAcceptsEveryTransport(t *testing.T) {
	for _, url := range []string{
		"http://127.0.0.1:8545",
		"https://node.example",
		"ws://127.0.0.1:8546",
		"wss://node.example/rpc",
		"ipc:///var/lib/nethermind/nethermind.ipc",
		"/var/lib/nethermind/nethermind.ipc",
	} {
		registry := NewClientRegistry()
		err := registry.LoadFromConfig(types.ClientsConfig{
			Clients: []types.ClientConfig{{Name: "node", URL: url}},
		})
		assert.NoError(t, err, "%s should be configurable", url)
	}

	for _, url := range []string{"", "node.example:8545", "ftp://node"} {
		registry := NewClientRegistry()
		err := registry.LoadFromConfig(types.ClientsConfig{
			Clients: []types.ClientConfig{{Name: "node", URL: url}},
		})
		assert.Error(t, err, "%q should still be refused", url)
	}
}

// Auth reaches the node as request headers, which an HTTP request and a
// WebSocket handshake both carry and a Unix socket does not. Accepting the
// block on an IPC client and then ignoring it means the run benchmarks an
// endpoint it never authenticated to, and reports whatever the node says to an
// unauthenticated caller.
func TestAuthIsRefusedOnATransportThatCannotCarryIt(t *testing.T) {
	auth := &types.AuthConfig{Type: "basic", Username: "user", Password: "secret"}

	for _, url := range []string{"http://127.0.0.1:8545", "wss://node.example/rpc"} {
		registry := NewClientRegistry()
		err := registry.LoadFromConfig(types.ClientsConfig{
			Clients: []types.ClientConfig{{Name: "node", URL: url, Auth: auth}},
		})
		assert.NoError(t, err, "%s carries headers, so auth applies", url)
	}

	for _, url := range []string{"ipc:///var/lib/nethermind/nethermind.ipc", "/var/lib/nethermind/nethermind.ipc"} {
		registry := NewClientRegistry()
		err := registry.LoadFromConfig(types.ClientsConfig{
			Clients: []types.ClientConfig{{Name: "node", URL: url, Auth: auth}},
		})
		if assert.Error(t, err, "%s cannot carry auth, so configuring it must be refused rather than ignored", url) {
			assert.Contains(t, err.Error(), "carries no headers")
		}
	}
}
