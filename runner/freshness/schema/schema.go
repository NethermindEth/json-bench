// Package schema defines the on-disk records shared by the freshness probe and
// review modes. Every file a probe writes is decoded by review, so changes here
// must bump Version.
package schema

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const Version = 1

// Probe output file names.
const (
	ManifestFile     = "run-manifest.json"
	CapabilitiesFile = "capabilities.json"
	EventsFile       = "events.jsonl"
	TargetsFile      = "targets.jsonl"
	AttemptsFile     = "rpc-attempts.jsonl"
	ResponsesDir     = "responses"
)

// Review output file names.
const (
	ReferenceDir     = "reference-data"
	BlockResultsFile = "block-results.jsonl"
	SummaryFile      = "summary.json"
	ReportFile       = "report.md"
)

// Nanos is an integer nanosecond value serialised as a decimal string so
// readers that parse JSON numbers as float64 cannot silently lose precision.
type Nanos int64

func (n Nanos) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatInt(int64(n), 10) + `"`), nil
}

func (n *Nanos) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("nanos must be a decimal string: %w", err)
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*n = Nanos(v)
	return nil
}

// Stamp pairs a wall-clock reading (comparable across hosts within the clock
// error) with a monotonic reading (only comparable within one process).
type Stamp struct {
	Wall Nanos `json:"wall_ns"`
	Mono Nanos `json:"mono_ns"`
}

func (s Stamp) IsZero() bool { return s.Wall == 0 && s.Mono == 0 }

// Clock produces Stamps for one process; Mono is measured from the clock's
// creation using Go's monotonic reading.
type Clock struct {
	start time.Time
}

func NewClock() *Clock { return &Clock{start: time.Now()} }

func (c *Clock) Now() Stamp { return c.At(time.Now()) }

// At converts a time taken with time.Now (so it carries a monotonic reading).
func (c *Clock) At(t time.Time) Stamp {
	return Stamp{Wall: Nanos(t.UnixNano()), Mono: Nanos(t.Sub(c.start))}
}

// Probe kinds.
const (
	ProbeStateNumber        = "state_number"
	ProbeLogsNumber         = "logs_number"
	ProbeStateLatest        = "state_latest"
	ProbeStateHashCanonical = "state_hash_canonical"
	ProbeLogsHash           = "logs_hash"
	ProbeTransactionReceipt = "transaction_receipt"
	ProbeBlockReceipts      = "block_receipts"
)

// AllProbes lists probe kinds in report order.
var AllProbes = []string{
	ProbeStateNumber, ProbeLogsNumber, ProbeStateLatest, ProbeStateHashCanonical,
	ProbeLogsHash, ProbeTransactionReceipt, ProbeBlockReceipts,
}

// Attempt outcome classes.
const (
	ClassResult    = "result"
	ClassNotReady  = "not_ready"
	ClassRPCError  = "rpc_error"
	ClassNull      = "null_result"
	ClassTransport = "transport_error"
	ClassTimeout   = "timeout"
	ClassHTTPError = "http_error"
	ClassBadBody   = "invalid_body"
)

// Local (probe-side) check verdicts for a result.
const (
	LocalPending  = "pending"
	LocalMatch    = "match"
	LocalMismatch = "mismatch"
)

// Probe-side statuses for one probe on one target.
const (
	StatusMatched          = "matched"
	StatusNotApplicable    = "not_applicable"
	StatusDeadlineExceeded = "deadline_exceeded"
	StatusIncorrect        = "incorrect"
	StatusUnsupported      = "unsupported"
	StatusLateArmed        = "late_armed"
	StatusAborted          = "aborted"
)

// Probe-side not-applicable reasons.
const (
	ReasonEmptyLogs         = "not_applicable_empty_logs"
	ReasonEmptyTransactions = "not_applicable_empty_transactions"
)

// Event types written to events.jsonl.
const (
	EventArm          = "target_armed"
	EventPromote      = "target_promoted"
	EventHeader       = "header_observed"
	EventMissedSlot   = "missed_slot"
	EventStall        = "stall"
	EventReorg        = "parent_mismatch"
	EventLateArmed    = "late_armed"
	EventClockSample  = "clock_sample"
	EventClockStep    = "clock_step"
	EventResource     = "resource_sample"
	EventCL           = "cl_event"
	EventCLDisconnect = "cl_stream_disconnected"
	EventRunStart     = "run_started"
	EventRunEnd       = "run_finished"
)

// Envelope is embedded in every JSONL record.
type Envelope struct {
	SchemaVersion int    `json:"schema_version"`
	RunID         string `json:"run_id"`
	PairID        string `json:"pair_id"`
	HostID        string `json:"host_id"`
	ClockDomain   string `json:"clock_domain"`
}

// Event is one line of events.jsonl.
type Event struct {
	Envelope
	Type        string         `json:"type"`
	At          Stamp          `json:"at"`
	BlockNumber *uint64        `json:"block_number,omitempty"`
	BlockHash   string         `json:"block_hash,omitempty"`
	Slot        *uint64        `json:"slot,omitempty"`
	Source      string         `json:"source"`
	Boundary    string         `json:"boundary,omitempty"`
	Status      string         `json:"status,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
}

// Attempt is one line of rpc-attempts.jsonl. By default only outcome
// transitions are recorded: the first attempt of each run of identical
// outcomes and the last one before it changes, which is what bounds
// availability. Repeat counts cover the attempts in between.
type Attempt struct {
	Envelope
	Probe       string `json:"probe"`
	BlockNumber uint64 `json:"block_number"`
	Seq         int    `json:"seq"`
	Edge        string `json:"edge"`
	Repeat      int    `json:"repeat,omitempty"`
	Scheduled   Stamp  `json:"scheduled"`
	Sent        Stamp  `json:"sent"`
	Received    Stamp  `json:"received"`
	Class       string `json:"class"`
	Local       string `json:"local,omitempty"`
	LocalReason string `json:"local_reason,omitempty"`
	Digest      string `json:"digest,omitempty"`
	Bytes       int    `json:"bytes"`
	HTTPStatus  int    `json:"http_status,omitempty"`
	ErrorCode   *int   `json:"error_code,omitempty"`
	Error       string `json:"error,omitempty"`
	WeakMatch   bool   `json:"weak_not_ready_match,omitempty"`
}

// Attempt edges.
const (
	EdgeFirst = "first"
	EdgeLast  = "last"
	EdgeAll   = "all"
)

// ProbeResult is the probe-side outcome of one probe on one target, with the
// counters for attempts that were not written individually.
type ProbeResult struct {
	Status         string         `json:"status"`
	Reason         string         `json:"reason,omitempty"`
	ArmedAt        Stamp          `json:"armed_at"`
	FirstSent      Stamp          `json:"first_sent,omitempty"`
	MatchReceived  *Stamp         `json:"match_received,omitempty"`
	MatchDigest    string         `json:"match_digest,omitempty"`
	LeftCensored   bool           `json:"left_censored,omitempty"`
	Attempts       int            `json:"attempts"`
	SkippedPolls   int            `json:"skipped_polls"`
	Classes        map[string]int `json:"classes,omitempty"`
	LagP50Micros   int64          `json:"scheduler_lag_p50_us"`
	LagMaxMicros   int64          `json:"scheduler_lag_max_us"`
	OrderViolation bool           `json:"order_violation,omitempty"`
}

// Target is one line of targets.jsonl: one execution block number as seen by
// this probe's node.
type Target struct {
	Envelope
	BlockNumber    uint64                  `json:"block_number"`
	BlockHash      string                  `json:"block_hash,omitempty"`
	ParentHash     string                  `json:"parent_hash,omitempty"`
	ExpectedParent string                  `json:"expected_parent,omitempty"`
	ParentMismatch bool                    `json:"parent_mismatch,omitempty"`
	Timestamp      uint64                  `json:"timestamp,omitempty"`
	SlotStart      Nanos                   `json:"slot_start_ns,omitempty"`
	Deadline       Nanos                   `json:"deadline_ns,omitempty"`
	Slot           *uint64                 `json:"slot,omitempty"`
	MissedSlots    int                     `json:"missed_slots_before"`
	TxCount        int                     `json:"tx_count"`
	GasUsed        uint64                  `json:"gas_used"`
	LogsBloomEmpty bool                    `json:"logs_bloom_empty"`
	ReceiptsRoot   string                  `json:"receipts_root,omitempty"`
	HeaderObserved *Stamp                  `json:"header_observed,omitempty"`
	HeaderSource   string                  `json:"header_source,omitempty"`
	ArmedAt        Stamp                   `json:"armed_at"`
	Promoted       *Stamp                  `json:"promoted_at,omitempty"`
	LateArmed      bool                    `json:"late_armed,omitempty"`
	Warmup         bool                    `json:"warmup,omitempty"`
	ClockStep      bool                    `json:"clock_step,omitempty"`
	Probes         map[string]*ProbeResult `json:"probes"`
}

// NotReadySignature is the normalised shape of a node's answer for a block it
// does not have yet, captured at preflight.
type NotReadySignature struct {
	HTTPStatus int    `json:"http_status"`
	Kind       string `json:"kind"`
	Code       *int   `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	RawMessage string `json:"raw_message,omitempty"`
}

// Signature kinds.
const (
	SigError      = "error"
	SigNull       = "null"
	SigEmptyArray = "empty_array"
	SigZeroResult = "zero_result"
)

// ProbeCapability is the preflight verdict for one enabled probe.
type ProbeCapability struct {
	Supported bool               `json:"supported"`
	Reason    string             `json:"reason,omitempty"`
	NotReady  *NotReadySignature `json:"not_ready_signature,omitempty"`
}

type Capabilities struct {
	SchemaVersion int                         `json:"schema_version"`
	RunID         string                      `json:"run_id"`
	PairID        string                      `json:"pair_id"`
	ChainID       uint64                      `json:"chain_id"`
	GenesisHash   string                      `json:"genesis_hash"`
	GenesisError  string                      `json:"genesis_error,omitempty"`
	HistoryCode   bool                        `json:"eip2935_code_present"`
	HistoryCanary bool                        `json:"eip2935_canary_ok"`
	ELSyncing     *bool                       `json:"el_syncing"`
	CLSync        map[string]any              `json:"cl_sync,omitempty"`
	CLSyncChecks  map[string]string           `json:"cl_sync_checks,omitempty"`
	Probes        map[string]*ProbeCapability `json:"probes"`
}

type RTTStats struct {
	Samples  int   `json:"samples"`
	Failures int   `json:"failures"`
	P50Micro int64 `json:"p50_us"`
	P95Micro int64 `json:"p95_us"`
	MaxMicro int64 `json:"max_us"`
}

type ClockQuality struct {
	Mode       string  `json:"mode"`
	Source     string  `json:"source"`
	ErrorMs    float64 `json:"error_ms"`
	MaxErrorMs float64 `json:"max_error_ms"`
	Synced     *bool   `json:"synced,omitempty"`
	Steps      int     `json:"steps"`
	Detail     string  `json:"detail,omitempty"`
}

type Manifest struct {
	SchemaVersion       int               `json:"schema_version"`
	RunID               string            `json:"run_id"`
	PairID              string            `json:"pair_id"`
	HostID              string            `json:"host_id"`
	Hostname            string            `json:"hostname"`
	ClockDomain         string            `json:"clock_domain"`
	Labels              map[string]string `json:"labels,omitempty"`
	StartedAt           Stamp             `json:"started_at"`
	FinishedAt          Stamp             `json:"finished_at"`
	Outcome             string            `json:"outcome"`
	OutcomeReason       string            `json:"outcome_reason,omitempty"`
	Config              any               `json:"config"`
	ELClientVersion     string            `json:"el_client_version,omitempty"`
	CLClientVersion     string            `json:"cl_client_version,omitempty"`
	ChainID             uint64            `json:"chain_id"`
	GenesisHash         string            `json:"genesis_hash"`
	SlotDurationSeconds uint64            `json:"slot_duration_seconds"`
	SlotDurationSource  string            `json:"slot_duration_source"`
	BeaconGenesisTime   *uint64           `json:"beacon_genesis_time,omitempty"`
	SlotsPerEpoch       *uint64           `json:"slots_per_epoch,omitempty"`
	FirstBlock          uint64            `json:"first_block"`
	LastBlock           uint64            `json:"last_block"`
	WarmupBlocks        int               `json:"warmup_blocks"`
	RecordAllAttempts   bool              `json:"record_all_attempts"`
	Clock               ClockQuality      `json:"clock"`
	RTTStart            *RTTStats         `json:"rtt_start,omitempty"`
	RTTEnd              *RTTStats         `json:"rtt_end,omitempty"`
	Dropped             map[string]int64  `json:"dropped_records"`
	Counts              map[string]int    `json:"counts"`
}

// Run outcomes.
const (
	OutcomeCompleted   = "completed"
	OutcomeMaxDuration = "max_duration"
	OutcomeInterrupted = "interrupted"
	OutcomeError       = "error"
)
