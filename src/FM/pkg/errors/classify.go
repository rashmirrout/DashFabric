package errors

import "fmt"

// Severity is the alert / log priority of a code. Only CRITICAL is
// expected to page a human; everything else is logged / metricked.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarn
	SeverityError
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "INFO"
	case SeverityWarn:
		return "WARN"
	case SeverityError:
		return "ERROR"
	case SeverityCritical:
		return "CRITICAL"
	default:
		return fmt.Sprintf("Severity(%d)", int(s))
	}
}

// Recoverability mirrors the spec's `Recov` column.
//
//   - Transient — auto-retry with backoff
//   - Permanent — stop retrying; quarantine; emit alert
//   - Operator  — stop retrying; emit alert with runbook; await human
//   - NA        — informational only; not actionable
type Recoverability int

const (
	RecovNA Recoverability = iota
	RecovTransient
	RecovPermanent
	RecovOperator
)

func (r Recoverability) String() string {
	switch r {
	case RecovNA:
		return "N/A"
	case RecovTransient:
		return "TRANSIENT"
	case RecovPermanent:
		return "PERMANENT"
	case RecovOperator:
		return "OPERATOR"
	default:
		return fmt.Sprintf("Recoverability(%d)", int(r))
	}
}

// Blast is the maximum scope a single instance of this code can affect.
type Blast int

const (
	BlastSingleENI Blast = iota
	BlastSingleDevice
	BlastShard
	BlastPod
	BlastHaScope
	BlastCluster
)

func (b Blast) String() string {
	switch b {
	case BlastSingleENI:
		return "SINGLE_ENI"
	case BlastSingleDevice:
		return "SINGLE_DEVICE"
	case BlastShard:
		return "SHARD"
	case BlastPod:
		return "POD"
	case BlastHaScope:
		return "HA_SCOPE"
	case BlastCluster:
		return "CLUSTER"
	default:
		return fmt.Sprintf("Blast(%d)", int(b))
	}
}

// Info is the classification record for a single Code.
type Info struct {
	Code           Code
	Severity       Severity
	Recoverability Recoverability
	Blast          Blast
	// Runbook is non-empty for CRITICAL codes only, and points to
	// Specs/Runbooks/<file>. Other severities are handled by generic
	// logging / alerting policy.
	Runbook string
	Meaning string
}

// Retryable returns true iff Recoverability is Transient. Backoff policy
// (100ms → 10s cap, 10% jitter) is universal and applied by the caller,
// not encoded here per-code.
func (i Info) Retryable() bool { return i.Recoverability == RecovTransient }

// catalog is the source-of-truth lookup. Keep in sync with
// Specs/FM/error-handling-design.md §3. Adding or changing a code here
// must accompany the same change in the spec.
var catalog = map[Code]Info{
	// API
	API_001_BAD_REQUEST:        {API_001_BAD_REQUEST, SeverityWarn, RecovOperator, BlastSingleENI, "", "Schema violation in input"},
	API_002_AUTH_FAILED:        {API_002_AUTH_FAILED, SeverityWarn, RecovOperator, BlastSingleENI, "", "mTLS/JWT rejected"},
	API_003_NOT_FOUND:          {API_003_NOT_FOUND, SeverityInfo, RecovNA, BlastSingleENI, "", "Device/ENI doesn't exist"},
	API_004_CONFLICT:           {API_004_CONFLICT, SeverityWarn, RecovTransient, BlastSingleENI, "", "ETag mismatch (concurrent modify)"},
	API_005_RATE_LIMITED:       {API_005_RATE_LIMITED, SeverityInfo, RecovTransient, BlastSingleENI, "", "Client over quota"},
	API_006_ADMISSION_REJECTED: {API_006_ADMISSION_REJECTED, SeverityWarn, RecovOperator, BlastPod, "", "Pod over capacity"},
	API_007_SHARD_NOT_OWNED:    {API_007_SHARD_NOT_OWNED, SeverityInfo, RecovTransient, BlastSingleENI, "", "Request to wrong pod; redirect"},
	API_008_TIMEOUT:            {API_008_TIMEOUT, SeverityError, RecovTransient, BlastSingleENI, "", "Backend slow"},

	// REG
	REG_001_KEY_NOT_FOUND:        {REG_001_KEY_NOT_FOUND, SeverityInfo, RecovNA, BlastSingleENI, "", "Acquire on non-existent key"},
	REG_002_WATCH_LOST:           {REG_002_WATCH_LOST, SeverityWarn, RecovTransient, BlastSingleENI, "", "etcd stream dropped"},
	REG_003_WATCH_FAILED:         {REG_003_WATCH_FAILED, SeverityError, RecovTransient, BlastShard, "", "etcd unreachable after retries"},
	REG_004_ACQUIRE_TIMEOUT:      {REG_004_ACQUIRE_TIMEOUT, SeverityWarn, RecovTransient, BlastSingleENI, "", "Ready not closed within deadline"},
	REG_005_ASSEMBLER_INCOMPLETE: {REG_005_ASSEMBLER_INCOMPLETE, SeverityWarn, RecovTransient, BlastSingleENI, "", "VnetMapping chunks missing"},
	REG_006_VALUE_DECODE_FAILED:  {REG_006_VALUE_DECODE_FAILED, SeverityError, RecovPermanent, BlastSingleENI, "", "T1 has invalid proto"},
	REG_007_REFCOUNT_UNDERFLOW:   {REG_007_REFCOUNT_UNDERFLOW, SeverityCritical, RecovPermanent, BlastPod, "Specs/Runbooks/REG_007_REFCOUNT_UNDERFLOW.md", "Bug: more Releases than Acquires"},
	REG_008_CACHE_OOM:            {REG_008_CACHE_OOM, SeverityCritical, RecovTransient, BlastPod, "Specs/Runbooks/REG_008_CACHE_OOM.md", "Memory budget exceeded"},

	// ACT
	ACT_001_PARENT_NOT_READY:       {ACT_001_PARENT_NOT_READY, SeverityInfo, RecovTransient, BlastSingleENI, "", "NicActor waiting on HDO"},
	ACT_002_INPUTS_NOT_READY:       {ACT_002_INPUTS_NOT_READY, SeverityInfo, RecovTransient, BlastSingleENI, "", "NicActor waiting on registries"},
	ACT_003_COMPOSE_FAILED:         {ACT_003_COMPOSE_FAILED, SeverityError, RecovPermanent, BlastSingleENI, "", "Validation rejected (bad spec)"},
	ACT_004_MAILBOX_DROPPED:        {ACT_004_MAILBOX_DROPPED, SeverityWarn, RecovTransient, BlastSingleENI, "", "Registry burst overflowed"},
	ACT_005_PANIC_RECOVERED:        {ACT_005_PANIC_RECOVERED, SeverityError, RecovPermanent, BlastSingleENI, "", "Goroutine recovered from panic"},
	ACT_006_REPEATED_PANIC:         {ACT_006_REPEATED_PANIC, SeverityCritical, RecovPermanent, BlastSingleENI, "Specs/Runbooks/ACT_006_REPEATED_PANIC.md", "Quarantined (3 panics in 10min)"},
	ACT_007_STATE_INVARIANT_BROKEN: {ACT_007_STATE_INVARIANT_BROKEN, SeverityCritical, RecovPermanent, BlastPod, "Specs/Runbooks/ACT_007_STATE_INVARIANT_BROKEN.md", "State machine corrupted (bug)"},
	ACT_008_DEADLINE_EXCEEDED:      {ACT_008_DEADLINE_EXCEEDED, SeverityWarn, RecovTransient, BlastSingleENI, "", "Compose/program exceeded budget"},

	// DRV
	DRV_001_CONNECTION_LOST:    {DRV_001_CONNECTION_LOST, SeverityWarn, RecovTransient, BlastSingleDevice, "", "gRPC/SAI broken"},
	DRV_002_TIMEOUT:            {DRV_002_TIMEOUT, SeverityWarn, RecovTransient, BlastSingleENI, "", "Per-command deadline"},
	DRV_003_INVALID_PAYLOAD:    {DRV_003_INVALID_PAYLOAD, SeverityError, RecovOperator, BlastSingleENI, "", "Device rejected schema"},
	DRV_004_RESOURCE_EXHAUSTED: {DRV_004_RESOURCE_EXHAUSTED, SeverityError, RecovOperator, BlastSingleDevice, "", "Device tables full"},
	DRV_005_VERSION_MISMATCH:   {DRV_005_VERSION_MISMATCH, SeverityWarn, RecovTransient, BlastSingleENI, "", "Plan stale; recompose"},
	DRV_006_PARTIAL_APPLY:      {DRV_006_PARTIAL_APPLY, SeverityError, RecovTransient, BlastSingleENI, "", "Mid-wave failure"},
	DRV_007_DEVICE_REJECTED:    {DRV_007_DEVICE_REJECTED, SeverityError, RecovOperator, BlastSingleENI, "", "Device-level validation"},
	DRV_008_PERMANENT_FAILURE:  {DRV_008_PERMANENT_FAILURE, SeverityCritical, RecovPermanent, BlastSingleDevice, "Specs/Runbooks/DRV_008_PERMANENT_FAILURE.md", "Quarantine"},
	DRV_009_CAPABILITY_MISSING: {DRV_009_CAPABILITY_MISSING, SeverityError, RecovOperator, BlastSingleDevice, "", "Device lacks required feature"},
	DRV_010_HASH_MISMATCH:      {DRV_010_HASH_MISMATCH, SeverityError, RecovTransient, BlastSingleENI, "", "Post-apply hash ≠ target"},

	// ADP
	ADP_001_LEASE_LOST:         {ADP_001_LEASE_LOST, SeverityWarn, RecovTransient, BlastPod, "", "Adapter pod lost leadership"},
	ADP_002_CAS_CONFLICT:       {ADP_002_CAS_CONFLICT, SeverityInfo, RecovTransient, BlastSingleENI, "", "Concurrent T1 write"},
	ADP_003_SCHEMA_REJECTED:    {ADP_003_SCHEMA_REJECTED, SeverityError, RecovOperator, BlastSingleENI, "", "CB sent invalid event"},
	ADP_004_UNKNOWN_EVENT_TYPE: {ADP_004_UNKNOWN_EVENT_TYPE, SeverityError, RecovOperator, BlastSingleENI, "", "CB plugin/FM version skew"},
	ADP_005_T1_UNREACHABLE:     {ADP_005_T1_UNREACHABLE, SeverityCritical, RecovTransient, BlastCluster, "Specs/Runbooks/ADP_005_T1_UNREACHABLE.md", "etcd down"},
	ADP_006_T2_UNREACHABLE:     {ADP_006_T2_UNREACHABLE, SeverityCritical, RecovTransient, BlastCluster, "Specs/Runbooks/ADP_006_T2_UNREACHABLE.md", "etcd down"},
	ADP_007_DLQ_INSERTED:       {ADP_007_DLQ_INSERTED, SeverityWarn, RecovOperator, BlastSingleENI, "", "Event dead-lettered"},
	ADP_008_WATERMARK_REGRESS:  {ADP_008_WATERMARK_REGRESS, SeverityCritical, RecovPermanent, BlastCluster, "Specs/Runbooks/ADP_008_WATERMARK_REGRESS.md", "Bug: watermark went backwards"},

	// STO
	STO_001_T1_TIMEOUT:             {STO_001_T1_TIMEOUT, SeverityWarn, RecovTransient, BlastShard, "", "etcd slow"},
	STO_002_T1_OOM:                 {STO_002_T1_OOM, SeverityCritical, RecovOperator, BlastCluster, "Specs/Runbooks/STO_002_T1_OOM.md", "etcd memory exhausted"},
	STO_003_T1_CAS_RETRY_EXHAUSTED: {STO_003_T1_CAS_RETRY_EXHAUSTED, SeverityError, RecovTransient, BlastSingleENI, "", "Lost CAS race repeatedly"},
	STO_004_T2_LEASE_EXPIRED:       {STO_004_T2_LEASE_EXPIRED, SeverityWarn, RecovTransient, BlastPod, "", "Lease lost mid-op"},
	STO_005_T3_CORRUPTION:          {STO_005_T3_CORRUPTION, SeverityCritical, RecovPermanent, BlastPod, "Specs/Runbooks/STO_005_T3_CORRUPTION.md", "RocksDB integrity check failed"},
	STO_006_T3_FULL:                {STO_006_T3_FULL, SeverityError, RecovOperator, BlastPod, "", "Local cache full; eviction can't keep up"},

	// REC
	REC_001_DRIFT_TRANSIENT:    {REC_001_DRIFT_TRANSIENT, SeverityInfo, RecovTransient, BlastSingleENI, "", "Cleared on retry"},
	REC_002_DRIFT_STALE_DEVICE: {REC_002_DRIFT_STALE_DEVICE, SeverityWarn, RecovTransient, BlastSingleENI, "", "Re-applying"},
	REC_003_DRIFT_OBJECT:       {REC_003_DRIFT_OBJECT, SeverityWarn, RecovTransient, BlastSingleENI, "", "Targeted fix"},
	REC_004_DRIFT_TOTAL:        {REC_004_DRIFT_TOTAL, SeverityError, RecovTransient, BlastSingleENI, "", "Full re-program"},
	REC_005_DRIFT_UNKNOWN:      {REC_005_DRIFT_UNKNOWN, SeverityCritical, RecovOperator, BlastSingleENI, "Specs/Runbooks/REC_005_DRIFT_UNKNOWN.md", "Quarantined; external edit suspected"},
	REC_006_PEER_DRIFT:         {REC_006_PEER_DRIFT, SeverityInfo, RecovTransient, BlastSingleENI, "", "HA failover artifact"},
	REC_007_HASH_RPC_FAIL:      {REC_007_HASH_RPC_FAIL, SeverityWarn, RecovTransient, BlastSingleDevice, "", "Device unreachable"},
	REC_008_PERSISTENT_DRIFT:   {REC_008_PERSISTENT_DRIFT, SeverityCritical, RecovOperator, BlastSingleENI, "Specs/Runbooks/REC_008_PERSISTENT_DRIFT.md", "5+ drifts in 1hr"},

	// HA
	HA_001_FAILOVER_INITIATED:    {HA_001_FAILOVER_INITIATED, SeverityInfo, RecovNA, BlastSingleENI, "", "Normal HA op"},
	HA_002_FAILOVER_TIMEOUT:      {HA_002_FAILOVER_TIMEOUT, SeverityError, RecovTransient, BlastSingleENI, "", "Peer didn't take over in budget"},
	HA_003_SPLIT_BRAIN_SUSPECTED: {HA_003_SPLIT_BRAIN_SUSPECTED, SeverityCritical, RecovOperator, BlastHaScope, "Specs/Runbooks/HA_003_SPLIT_BRAIN_SUSPECTED.md", "Both peers claim PRIMARY"},
	HA_004_SESSION_REPL_LAG:      {HA_004_SESSION_REPL_LAG, SeverityWarn, RecovTransient, BlastHaScope, "", "Replication lag > 1s"},
	HA_005_ORPHANED_STANDBY:      {HA_005_ORPHANED_STANDBY, SeverityWarn, RecovOperator, BlastHaScope, "", "Standby has no primary"},
}

// unknownInfo is returned by Classify for codes not in the catalog.
// Deliberately ERROR + PERMANENT so a caller cannot accidentally retry.
var unknownInfo = Info{
	Code:           Unknown,
	Severity:       SeverityError,
	Recoverability: RecovPermanent,
	Blast:          BlastSingleENI,
	Runbook:        "",
	Meaning:        "Unrecognised error code",
}

// Classify returns the Info record for c. Unknown codes return a sentinel
// with Severity=ERROR, Recoverability=PERMANENT, Retryable=false.
func Classify(c Code) Info {
	if info, ok := catalog[c]; ok {
		return info
	}
	return unknownInfo
}

// All returns a snapshot of every registered Code. Order is not defined.
// Useful for tests, doc generation, and metric pre-registration.
func All() []Code {
	out := make([]Code, 0, len(catalog))
	for c := range catalog {
		out = append(out, c)
	}
	return out
}
