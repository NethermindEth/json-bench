package review

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jsonrpc-bench/runner/freshness/schema"
)

// ProbeRun is one probe output directory loaded into memory.
type ProbeRun struct {
	Dir          string
	Manifest     schema.Manifest
	Capabilities schema.Capabilities
	Targets      map[uint64]*schema.Target
	// Attempts are keyed by probe then block number, ordered by Seq.
	Attempts map[string]map[uint64][]schema.Attempt
	CLEvents map[uint64][]schema.Event
}

func (p *ProbeRun) ID() string { return p.Manifest.PairID }

// ClockErrorMs is the worst clock error the probe reported.
func (p *ProbeRun) ClockErrorMs() float64 {
	if p.Manifest.Clock.MaxErrorMs > p.Manifest.Clock.ErrorMs {
		return p.Manifest.Clock.MaxErrorMs
	}
	return p.Manifest.Clock.ErrorMs
}

func (p *ProbeRun) InRange(n uint64) bool {
	return n >= p.Manifest.FirstBlock && n <= p.Manifest.LastBlock
}

func LoadProbeRun(dir string) (*ProbeRun, error) {
	run := &ProbeRun{
		Dir:      dir,
		Targets:  map[uint64]*schema.Target{},
		Attempts: map[string]map[uint64][]schema.Attempt{},
		CLEvents: map[uint64][]schema.Event{},
	}
	if err := readJSON(filepath.Join(dir, schema.ManifestFile), &run.Manifest); err != nil {
		return nil, err
	}
	if run.Manifest.SchemaVersion != schema.Version {
		return nil, fmt.Errorf("%s: schema_version %d, want %d", dir, run.Manifest.SchemaVersion, schema.Version)
	}
	if err := readJSON(filepath.Join(dir, schema.CapabilitiesFile), &run.Capabilities); err != nil {
		return nil, err
	}
	err := eachLine(filepath.Join(dir, schema.TargetsFile), func(b []byte) error {
		var t schema.Target
		if err := json.Unmarshal(b, &t); err != nil {
			return err
		}
		run.Targets[t.BlockNumber] = &t
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = eachLine(filepath.Join(dir, schema.AttemptsFile), func(b []byte) error {
		var a schema.Attempt
		if err := json.Unmarshal(b, &a); err != nil {
			return err
		}
		byBlock := run.Attempts[a.Probe]
		if byBlock == nil {
			byBlock = map[uint64][]schema.Attempt{}
			run.Attempts[a.Probe] = byBlock
		}
		byBlock[a.BlockNumber] = append(byBlock[a.BlockNumber], a)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, byBlock := range run.Attempts {
		for _, list := range byBlock {
			sort.SliceStable(list, func(i, j int) bool { return list[i].Seq < list[j].Seq })
		}
	}
	err = eachLine(filepath.Join(dir, schema.EventsFile), func(b []byte) error {
		var e schema.Event
		if err := json.Unmarshal(b, &e); err != nil {
			return err
		}
		if e.Type == schema.EventCL && e.Slot != nil {
			run.CLEvents[*e.Slot] = append(run.CLEvents[*e.Slot], e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return run, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func eachLine(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	line := 0
	for sc.Scan() {
		line++
		if len(sc.Bytes()) == 0 {
			continue
		}
		if err := fn(sc.Bytes()); err != nil {
			return fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}
	return sc.Err()
}
