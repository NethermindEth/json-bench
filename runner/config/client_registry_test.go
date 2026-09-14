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
