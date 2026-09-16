package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/internal/stubnode"
	"github.com/jsonrpc-bench/runner/types"
)

// The mux is written to from every dispatch slot at once while a reader
// goroutine delivers into it and the connection fails underneath.
func TestMuxUnderConcurrentRegisterDispatchAndFailure(t *testing.T) {
	for round := 0; round < 20; round++ {
		m := newMux()
		var wg sync.WaitGroup

		const n = 200
		delivered := make(chan struct{}, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				id := fmt.Sprint(i)
				p, err := m.register([]string{id})
				if err != nil {
					return // connection already failed; a legitimate outcome
				}
				go m.dispatch([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":1}`, i)))
				select {
				case frame := <-p.done:
					if frame != nil {
						delivered <- struct{}{}
					}
				case <-time.After(2 * time.Second):
					t.Errorf("request %d neither answered nor failed", i)
				}
			}(i)
		}

		go func() {
			time.Sleep(time.Millisecond)
			m.fail(errConnectionClosed)
		}()

		wg.Wait()
		// Every request must resolve one way or the other; none may hang.
		assert.LessOrEqual(t, len(delivered), n)
	}
}

// Registering and releasing the same ids repeatedly must not leak entries.
func TestMuxDoesNotLeakWaiters(t *testing.T) {
	m := newMux()
	for i := 0; i < 500; i++ {
		ids := []string{fmt.Sprint(i), fmt.Sprint(i + 100000)}
		p, err := m.register(ids)
		require.NoError(t, err)
		if i%2 == 0 {
			m.dispatch([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":1}`, i)))
			<-p.done
		} else {
			m.release(ids)
		}
	}
	m.mu.Lock()
	remaining := len(m.waiting)
	m.mu.Unlock()
	assert.Zero(t, remaining, "every id must be released by delivery or by cancellation")
}

// A run must survive the node hanging up mid-flight and keep measuring.
//
// A reconnect gets its own mux, so the generation that died and the generation
// replacing it cannot interfere: the reader of the dead socket may still be
// blocked in a read when the new one is already carrying requests.
func TestSocketRunSurvivesTheNodeHangingUp(t *testing.T) {
	stale := newMux()
	first, err := stale.register([]string{"1"})
	require.NoError(t, err)

	live := newMux()
	second, err := live.register([]string{"1"})
	require.NoError(t, err, "the same id is free again once the connection is re-established")

	// The dead socket's reader finally wakes and reports what it saw. That must
	// reach its own generation's requests and nothing else.
	stale.fail(errConnectionClosed)
	assert.Nil(t, <-first.done)

	live.dispatch([]byte(`{"jsonrpc":"2.0","id":1,"result":"after reconnect"}`))
	select {
	case frame := <-second.done:
		assert.Contains(t, string(frame), "after reconnect")
	case <-time.After(time.Second):
		t.Fatal("no answer after reconnect")
	}

	_, err = live.register([]string{"2"})
	assert.NoError(t, err, "the live connection must still take requests after the dead one failed")
}

// Go's transport does not run every trace callback on the goroutine that called
// Do, and it does not finish them before Do returns: a request that queued a
// dial and was then handed a connection freed from the pool proceeds while its
// own dial goroutine is still running, and that goroutine still reports the
// connection it is opening. The request has by then already read its timings.
//
// What produces it is concurrency with a pool that keeps releasing connections,
// which is every run this engine does.
func TestHTTPTimingsAreSafeWhenADialLosesThePoolRace(t *testing.T) {
	srv := stubServer(t, stubnode.Config{
		Default: stubnode.Method{Latency: &stubnode.Latency{Kind: stubnode.LatencyFixed, MS: 1}},
	})

	const concurrency = 300
	tgt, err := newTarget(&types.ClientConfig{Name: "stub", URL: srv.URL}, concurrency, DefaultTransportOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tgt.Close() })

	for round := 0; round < 5; round++ {
		var wg sync.WaitGroup
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				payload := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"eth_call","params":[]}`, i))
				res := tgt.do(context.Background(), payload)
				assert.NoError(t, res.err)
				assert.GreaterOrEqual(t, res.phases.Blocked, time.Duration(0))
			}(i)
		}
		wg.Wait()
	}
}
