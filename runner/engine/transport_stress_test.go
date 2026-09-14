package engine

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
func TestSocketRunSurvivesTheNodeHangingUp(t *testing.T) {
	m := newMux()
	first, err := m.register([]string{"1"})
	require.NoError(t, err)

	m.fail(errConnectionClosed)
	assert.Nil(t, <-first.done)

	// Reconnecting resets the mux, and requests flow again.
	m.reset()
	second, err := m.register([]string{"1"})
	require.NoError(t, err, "the same id is free again once the connection is re-established")
	m.dispatch([]byte(`{"jsonrpc":"2.0","id":1,"result":"after reconnect"}`))

	select {
	case frame := <-second.done:
		assert.Contains(t, string(frame), "after reconnect")
	case <-time.After(time.Second):
		t.Fatal("no answer after reconnect")
	}
}
