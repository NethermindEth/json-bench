// Package clientinfo queries metadata (web3_clientVersion) from each
// configured RPC endpoint at run startup so the resulting HistoricRun can be
// pinned to a specific client build. Failures are tolerated — a client that
// doesn't respond is recorded as "unknown" and the benchmark continues.
package clientinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jsonrpc-bench/runner/types"
)

// Unknown is the placeholder recorded when web3_clientVersion fails (timeout,
// non-200, missing field, etc.). Stored verbatim in the DB so the UI can
// distinguish "the client didn't answer" from "we never asked".
const Unknown = "unknown"

const defaultTimeout = 5 * time.Second

// FetchVersions queries web3_clientVersion against each client in parallel and
// returns a map of client name → version string. Always returns a non-nil map.
func FetchVersions(ctx context.Context, clients []*types.ClientConfig) map[string]string {
	result := make(map[string]string, len(clients))
	if len(clients) == 0 {
		return result
	}

	type pair struct {
		name    string
		version string
	}
	ch := make(chan pair, len(clients))
	for _, c := range clients {
		go func(c *types.ClientConfig) {
			v, err := fetchOne(ctx, c)
			if err != nil || v == "" {
				ch <- pair{c.Name, Unknown}
				return
			}
			ch <- pair{c.Name, v}
		}(c)
	}
	for range clients {
		p := <-ch
		result[p.name] = p.version
	}
	return result
}

func fetchOne(parent context.Context, c *types.ClientConfig) (string, error) {
	if c == nil || c.URL == "" {
		return "", errors.New("missing URL")
	}
	ctx, cancel := context.WithTimeout(parent, defaultTimeout)
	defer cancel()

	body := []byte(`{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.GetBasicAuthURL(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	var envelope struct {
		Result string `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", err
	}
	if envelope.Error != nil {
		return "", fmt.Errorf("rpc error: %s", envelope.Error.Message)
	}
	return envelope.Result, nil
}
