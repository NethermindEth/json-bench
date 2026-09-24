// Package capture writes probe records off the measurement path: callers hand
// records to a bounded buffer and never wait on disk. A full buffer drops the
// record and counts it, so data loss is visible in the manifest rather than
// showing up as scheduler delay.
package capture

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// JSONL appends one JSON document per line.
type JSONL struct {
	ch      chan any
	dropped atomic.Int64
	done    chan error
	f       *os.File
}

func NewJSONL(path string, buffer int) (*JSONL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	w := &JSONL{ch: make(chan any, buffer), done: make(chan error, 1), f: f}
	go w.loop()
	return w, nil
}

func (w *JSONL) loop() {
	bw := bufio.NewWriterSize(w.f, 1<<16)
	enc := json.NewEncoder(bw)
	var firstErr error
	for rec := range w.ch {
		if err := enc.Encode(rec); err != nil && firstErr == nil {
			firstErr = err
		}
		if len(w.ch) == 0 {
			if err := bw.Flush(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	if err := bw.Flush(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := w.f.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	w.done <- firstErr
}

// Write enqueues rec without blocking.
func (w *JSONL) Write(rec any) {
	select {
	case w.ch <- rec:
	default:
		w.dropped.Add(1)
	}
}

func (w *JSONL) Dropped() int64 { return w.dropped.Load() }

// Close drains the buffer and closes the file.
func (w *JSONL) Close() error {
	close(w.ch)
	return <-w.done
}

// Store keeps each distinct canonical response once, named by its digest.
type Store struct {
	dir     string
	mu      sync.Mutex
	seen    map[string]struct{}
	ch      chan storeItem
	dropped atomic.Int64
	done    chan error
}

type storeItem struct {
	digest string
	value  any
}

func NewStore(dir string, buffer int) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, seen: map[string]struct{}{}, ch: make(chan storeItem, buffer), done: make(chan error, 1)}
	go s.loop()
	return s, nil
}

func (s *Store) loop() {
	var firstErr error
	for it := range s.ch {
		b, err := json.Marshal(it.value)
		if err == nil {
			err = os.WriteFile(filepath.Join(s.dir, it.digest+".json"), b, 0o644)
		}
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("store %s: %w", it.digest, err)
		}
	}
	s.done <- firstErr
}

// Put stores value under digest unless that digest was already stored.
func (s *Store) Put(digest string, value any) {
	s.mu.Lock()
	if _, ok := s.seen[digest]; ok {
		s.mu.Unlock()
		return
	}
	s.seen[digest] = struct{}{}
	s.mu.Unlock()
	select {
	case s.ch <- storeItem{digest: digest, value: value}:
	default:
		s.dropped.Add(1)
		s.mu.Lock()
		delete(s.seen, digest)
		s.mu.Unlock()
	}
}

func (s *Store) Dropped() int64 { return s.dropped.Load() }

func (s *Store) Close() error {
	close(s.ch)
	return <-s.done
}

// WriteJSON writes an indented JSON file synchronously (manifest,
// capabilities: written outside the measurement window).
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
