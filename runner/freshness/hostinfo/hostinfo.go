// Package hostinfo collects best-effort host facts for the probe manifest:
// clock synchronisation quality, round-trip baseline to the node and the
// probe's own resource use.
package hostinfo

import (
	"context"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/process"

	"github.com/jsonrpc-bench/runner/freshness/ethrpc"
	"github.com/jsonrpc-bench/runner/freshness/schema"
)

// ClockReading is one clock-quality observation. OK is false when no source
// produced an error estimate.
type ClockReading struct {
	Source  string
	ErrorMs float64
	Synced  *bool
	Detail  string
	OK      bool
}

// ReadClock tries chrony, then systemd-timedated. timedatectl only reports
// whether NTP is synchronised, so it yields Synced without an error estimate.
func ReadClock(ctx context.Context) ClockReading {
	if r, ok := readChrony(ctx); ok {
		return r
	}
	if r, ok := readTimedatectl(ctx); ok {
		return r
	}
	return ClockReading{Source: "unavailable", Detail: "neither chronyc nor timedatectl answered"}
}

func runCmd(ctx context.Context, name string, args ...string) (string, bool) {
	if _, err := exec.LookPath(name); err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// readChrony parses `chronyc -c tracking`. The error bound follows chrony's
// own definition: |system offset| + root delay / 2 + root dispersion.
func readChrony(ctx context.Context) (ClockReading, bool) {
	out, ok := runCmd(ctx, "chronyc", "-c", "tracking")
	if !ok {
		return ClockReading{}, false
	}
	f := strings.Split(out, ",")
	if len(f) < 14 {
		return ClockReading{}, false
	}
	num := func(i int) (float64, bool) {
		v, err := strconv.ParseFloat(strings.TrimSpace(f[i]), 64)
		return v, err == nil
	}
	offset, ok1 := num(4)
	delay, ok2 := num(10)
	disp, ok3 := num(11)
	if !ok1 || !ok2 || !ok3 {
		return ClockReading{}, false
	}
	leap := strings.TrimSpace(f[13])
	synced := !strings.EqualFold(leap, "Not synchronised")
	errMs := (math.Abs(offset) + delay/2 + disp) * 1000
	return ClockReading{
		Source:  "chrony",
		ErrorMs: errMs,
		Synced:  &synced,
		Detail:  "offset=" + f[4] + "s root_delay=" + f[10] + "s root_dispersion=" + f[11] + "s leap=" + leap,
		OK:      synced,
	}, true
}

func readTimedatectl(ctx context.Context) (ClockReading, bool) {
	out, ok := runCmd(ctx, "timedatectl", "show", "-p", "NTPSynchronized", "--value")
	if !ok {
		return ClockReading{}, false
	}
	synced := out == "yes"
	return ClockReading{Source: "timedatectl", Synced: &synced, Detail: "NTPSynchronized=" + out}, true
}

// StepDetector flags wall-clock jumps by comparing wall and monotonic
// progress between samples.
type StepDetector struct {
	last      time.Time
	threshold time.Duration
}

func NewStepDetector(threshold time.Duration) *StepDetector {
	return &StepDetector{last: time.Now(), threshold: threshold}
}

// Sample returns the wall-minus-monotonic drift since the previous sample and
// whether it exceeds the threshold.
func (d *StepDetector) Sample() (time.Duration, bool) {
	now := time.Now()
	wall := now.Round(0).Sub(d.last.Round(0))
	mono := now.Sub(d.last)
	d.last = now
	drift := wall - mono
	if drift < 0 {
		return drift, -drift > d.threshold
	}
	return drift, drift > d.threshold
}

// MeasureRTT sends n sequential eth_chainId calls over the probe's warm
// connections.
func MeasureRTT(ctx context.Context, c *ethrpc.Client, n int) *schema.RTTStats {
	st := &schema.RTTStats{}
	var samples []int64
	for i := 0; i < n && ctx.Err() == nil; i++ {
		resp := c.Call(ctx, "eth_chainId")
		if ethrpc.Parse(resp).Class != schema.ClassResult {
			st.Failures++
			continue
		}
		samples = append(samples, int64(resp.Received.Mono-resp.Sent.Mono)/1000)
	}
	st.Samples = len(samples)
	if len(samples) == 0 {
		return st
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	st.P50Micro = Percentile(samples, 50)
	st.P95Micro = Percentile(samples, 95)
	st.MaxMicro = samples[len(samples)-1]
	return st
}

// Percentile uses nearest-rank on an ascending slice.
func Percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

// ResourceSampler reports the probe process's CPU and memory.
type ResourceSampler struct {
	proc *process.Process
}

func NewResourceSampler() *ResourceSampler {
	p, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		return &ResourceSampler{}
	}
	_, _ = p.Percent(0)
	return &ResourceSampler{proc: p}
}

// Sample returns CPU percent since the previous call, RSS, goroutines and the
// host load average where available.
func (s *ResourceSampler) Sample() map[string]any {
	out := map[string]any{"goroutines": runtime.NumGoroutine()}
	if s.proc != nil {
		if pct, err := s.proc.Percent(0); err == nil {
			out["cpu_percent"] = math.Round(pct*100) / 100
		}
		if mem, err := s.proc.MemoryInfo(); err == nil {
			out["rss_bytes"] = mem.RSS
		}
	}
	if avg, err := load.Avg(); err == nil {
		out["load1"] = avg.Load1
	}
	return out
}
