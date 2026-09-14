// Command promsink stands in for Prometheus' remote-write endpoint, recording
// what a load generator pushes and writing it out as a golden capture. It is
// how the k6 series set was pinned before the engine replaced it, and how the
// engine's own output is checked against that set.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jsonrpc-bench/runner/internal/promsink"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:19090", "Address to accept remote-write pushes on")
	golden := flag.String("golden", "", "Write the capture here on shutdown (default: stdout)")
	source := flag.String("source", "", "Annotation recorded in the capture, e.g. \"k6 v2.2.0\"")
	flag.Parse()

	rec := promsink.NewRecorder()
	srv := &http.Server{
		Addr:              *listen,
		Handler:           rec.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("promsink: %v", err)
		}
	}()
	fmt.Fprintf(os.Stderr, "promsink: accepting remote-write on http://%s%s — Ctrl-C to write the capture\n",
		*listen, promsink.WritePath)

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	if err := rec.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "promsink: warning: a push failed to decode: %v\n", err)
	}

	out := os.Stdout
	if *golden != "" {
		f, err := os.Create(*golden)
		if err != nil {
			log.Fatalf("promsink: %v", err)
		}
		defer f.Close()
		out = f
	}
	if err := rec.WriteGolden(out, *source); err != nil {
		log.Fatalf("promsink: %v", err)
	}

	fmt.Fprintf(os.Stderr, "promsink: %d push(es), %d series, %d metric names\n",
		rec.Pushes(), len(rec.Keys()), len(rec.MetricNames()))
}
