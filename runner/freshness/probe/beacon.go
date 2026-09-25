package probe

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// beaconClient reads the optional CL endpoint. Its data is supplementary:
// Beacon API events are post-import/post-gossip-validation milestones, not
// network-arrival timestamps.
type beaconClient struct {
	base    string
	headers map[string]string
	http    *http.Client
}

func newBeaconClient(cfg CLConfig, timeout time.Duration) *beaconClient {
	return &beaconClient{
		base:    strings.TrimRight(cfg.BeaconURL, "/"),
		headers: cfg.Headers,
		http:    &http.Client{Timeout: timeout},
	}
}

func (b *beaconClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.base+path, nil)
	if err != nil {
		return err
	}
	for k, v := range b.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: http %d", path, resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// syncStatus returns the raw /eth/v1/node/syncing data and a verdict per
// health field. A field the CL does not report is "unknown", never healthy.
func (b *beaconClient) syncStatus(ctx context.Context) (map[string]any, map[string]string, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := b.get(ctx, "/eth/v1/node/syncing", &resp); err != nil {
		return nil, nil, err
	}
	checks := map[string]string{}
	for field, healthy := range map[string]bool{"is_syncing": false, "is_optimistic": false, "el_offline": false} {
		v, ok := resp.Data[field].(bool)
		switch {
		case !ok:
			checks[field] = "unknown"
		case v == healthy:
			checks[field] = "ok"
		default:
			checks[field] = "failed"
		}
	}
	return resp.Data, checks, nil
}

func (b *beaconClient) version(ctx context.Context) string {
	var resp struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if b.get(ctx, "/eth/v1/node/version", &resp) != nil {
		return ""
	}
	return resp.Data.Version
}

func (b *beaconClient) specValue(ctx context.Context, key string) (uint64, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := b.get(ctx, "/eth/v1/config/spec", &resp); err != nil {
		return 0, err
	}
	return anyUint(resp.Data[key])
}

func (b *beaconClient) genesisTime(ctx context.Context) (uint64, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := b.get(ctx, "/eth/v1/beacon/genesis", &resp); err != nil {
		return 0, err
	}
	return anyUint(resp.Data["genesis_time"])
}

func anyUint(v any) (uint64, error) {
	switch x := v.(type) {
	case string:
		return strconv.ParseUint(x, 10, 64)
	case float64:
		return uint64(x), nil
	}
	return 0, fmt.Errorf("unexpected value %v", v)
}

type sseEvent struct {
	topic    string
	data     map[string]any
	received time.Time
}

// stream follows the SSE event stream until ctx ends, reconnecting after a
// drop. Some CLs reject block_gossip, so a 4xx on it falls back to the
// remaining topics.
func (b *beaconClient) stream(ctx context.Context, out func(sseEvent), onDisconnect func(error)) {
	topics := []string{"head", "block", "block_gossip"}
	backoff := time.Second
	for ctx.Err() == nil {
		status, err := b.streamOnce(ctx, topics, out)
		if ctx.Err() != nil {
			return
		}
		if status >= 400 && status < 500 && len(topics) == 3 {
			topics = topics[:2]
			continue
		}
		onDisconnect(err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (b *beaconClient) streamOnce(ctx context.Context, topics []string, out func(sseEvent)) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.base+"/eth/v1/events?topics="+strings.Join(topics, ","), nil)
	if err != nil {
		return 0, err
	}
	for k, v := range b.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("event stream: http %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var topic string
	var data strings.Builder
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if topic != "" && data.Len() > 0 {
				ev := sseEvent{topic: topic, received: time.Now()}
				_ = json.Unmarshal([]byte(data.String()), &ev.data)
				out(ev)
			}
			topic = ""
			data.Reset()
		case strings.HasPrefix(line, "event:"):
			topic = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := sc.Err(); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, io.ErrUnexpectedEOF
}
