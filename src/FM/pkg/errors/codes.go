// Package errors holds the canonical FM error catalog (see
// Specs/FM/error-handling-design.md §3) translated into Go constants,
// plus a Classify lookup for severity / recoverability / runbook.
//
// Import as `fmerrors "github.com/dashfabric/fm/pkg/errors"` to avoid
// shadowing the stdlib errors package.
package errors

// Code is the stable string identifier of an FM error. Strings match the
// spec catalog verbatim so grep across logs, metrics, runbook filenames,
// and Go source resolves to a single identifier.
//
// Constant names mirror the string value (SCREAMING_SNAKE_CASE) for the
// same reason — this is an intentional departure from Go's CamelCase
// convention and applies only to the error-code catalog.
type Code string

// --- API_xxx — gateway, REST/gRPC ---
const (
	API_001_BAD_REQUEST         Code = "API_001_BAD_REQUEST"
	API_002_AUTH_FAILED         Code = "API_002_AUTH_FAILED"
	API_003_NOT_FOUND           Code = "API_003_NOT_FOUND"
	API_004_CONFLICT            Code = "API_004_CONFLICT"
	API_005_RATE_LIMITED        Code = "API_005_RATE_LIMITED"
	API_006_ADMISSION_REJECTED  Code = "API_006_ADMISSION_REJECTED"
	API_007_SHARD_NOT_OWNED     Code = "API_007_SHARD_NOT_OWNED"
	API_008_TIMEOUT             Code = "API_008_TIMEOUT"
)

// --- REG_xxx — registry layer ---
const (
	REG_001_KEY_NOT_FOUND         Code = "REG_001_KEY_NOT_FOUND"
	REG_002_WATCH_LOST            Code = "REG_002_WATCH_LOST"
	REG_003_WATCH_FAILED          Code = "REG_003_WATCH_FAILED"
	REG_004_ACQUIRE_TIMEOUT       Code = "REG_004_ACQUIRE_TIMEOUT"
	REG_005_ASSEMBLER_INCOMPLETE  Code = "REG_005_ASSEMBLER_INCOMPLETE"
	REG_006_VALUE_DECODE_FAILED   Code = "REG_006_VALUE_DECODE_FAILED"
	REG_007_REFCOUNT_UNDERFLOW    Code = "REG_007_REFCOUNT_UNDERFLOW"
	REG_008_CACHE_OOM             Code = "REG_008_CACHE_OOM"
)

// --- ACT_xxx — actor layer ---
const (
	ACT_001_PARENT_NOT_READY        Code = "ACT_001_PARENT_NOT_READY"
	ACT_002_INPUTS_NOT_READY        Code = "ACT_002_INPUTS_NOT_READY"
	ACT_003_COMPOSE_FAILED          Code = "ACT_003_COMPOSE_FAILED"
	ACT_004_MAILBOX_DROPPED         Code = "ACT_004_MAILBOX_DROPPED"
	ACT_005_PANIC_RECOVERED         Code = "ACT_005_PANIC_RECOVERED"
	ACT_006_REPEATED_PANIC          Code = "ACT_006_REPEATED_PANIC"
	ACT_007_STATE_INVARIANT_BROKEN  Code = "ACT_007_STATE_INVARIANT_BROKEN"
	ACT_008_DEADLINE_EXCEEDED       Code = "ACT_008_DEADLINE_EXCEEDED"
)

// --- DRV_xxx — driver layer ---
const (
	DRV_001_CONNECTION_LOST     Code = "DRV_001_CONNECTION_LOST"
	DRV_002_TIMEOUT             Code = "DRV_002_TIMEOUT"
	DRV_003_INVALID_PAYLOAD     Code = "DRV_003_INVALID_PAYLOAD"
	DRV_004_RESOURCE_EXHAUSTED  Code = "DRV_004_RESOURCE_EXHAUSTED"
	DRV_005_VERSION_MISMATCH    Code = "DRV_005_VERSION_MISMATCH"
	DRV_006_PARTIAL_APPLY       Code = "DRV_006_PARTIAL_APPLY"
	DRV_007_DEVICE_REJECTED     Code = "DRV_007_DEVICE_REJECTED"
	DRV_008_PERMANENT_FAILURE   Code = "DRV_008_PERMANENT_FAILURE"
	DRV_009_CAPABILITY_MISSING  Code = "DRV_009_CAPABILITY_MISSING"
	DRV_010_HASH_MISMATCH       Code = "DRV_010_HASH_MISMATCH"
)

// --- ADP_xxx — adapter layer ---
const (
	ADP_001_LEASE_LOST          Code = "ADP_001_LEASE_LOST"
	ADP_002_CAS_CONFLICT        Code = "ADP_002_CAS_CONFLICT"
	ADP_003_SCHEMA_REJECTED     Code = "ADP_003_SCHEMA_REJECTED"
	ADP_004_UNKNOWN_EVENT_TYPE  Code = "ADP_004_UNKNOWN_EVENT_TYPE"
	ADP_005_T1_UNREACHABLE      Code = "ADP_005_T1_UNREACHABLE"
	ADP_006_T2_UNREACHABLE      Code = "ADP_006_T2_UNREACHABLE"
	ADP_007_DLQ_INSERTED        Code = "ADP_007_DLQ_INSERTED"
	ADP_008_WATERMARK_REGRESS   Code = "ADP_008_WATERMARK_REGRESS"
)

// --- STO_xxx — storage layer ---
const (
	STO_001_T1_TIMEOUT              Code = "STO_001_T1_TIMEOUT"
	STO_002_T1_OOM                  Code = "STO_002_T1_OOM"
	STO_003_T1_CAS_RETRY_EXHAUSTED  Code = "STO_003_T1_CAS_RETRY_EXHAUSTED"
	STO_004_T2_LEASE_EXPIRED        Code = "STO_004_T2_LEASE_EXPIRED"
	STO_005_T3_CORRUPTION           Code = "STO_005_T3_CORRUPTION"
	STO_006_T3_FULL                 Code = "STO_006_T3_FULL"
)

// --- REC_xxx — reconciliation ---
const (
	REC_001_DRIFT_TRANSIENT     Code = "REC_001_DRIFT_TRANSIENT"
	REC_002_DRIFT_STALE_DEVICE  Code = "REC_002_DRIFT_STALE_DEVICE"
	REC_003_DRIFT_OBJECT        Code = "REC_003_DRIFT_OBJECT"
	REC_004_DRIFT_TOTAL         Code = "REC_004_DRIFT_TOTAL"
	REC_005_DRIFT_UNKNOWN       Code = "REC_005_DRIFT_UNKNOWN"
	REC_006_PEER_DRIFT          Code = "REC_006_PEER_DRIFT"
	REC_007_HASH_RPC_FAIL       Code = "REC_007_HASH_RPC_FAIL"
	REC_008_PERSISTENT_DRIFT    Code = "REC_008_PERSISTENT_DRIFT"
)

// --- HA_xxx — high-availability layer ---
const (
	HA_001_FAILOVER_INITIATED      Code = "HA_001_FAILOVER_INITIATED"
	HA_002_FAILOVER_TIMEOUT        Code = "HA_002_FAILOVER_TIMEOUT"
	HA_003_SPLIT_BRAIN_SUSPECTED   Code = "HA_003_SPLIT_BRAIN_SUSPECTED"
	HA_004_SESSION_REPL_LAG        Code = "HA_004_SESSION_REPL_LAG"
	HA_005_ORPHANED_STANDBY        Code = "HA_005_ORPHANED_STANDBY"
)

// Unknown is the sentinel returned by Classify for codes not in the
// catalog. Severity is ERROR, Recoverability is PERMANENT, so callers
// won't accidentally retry on an unrecognised code.
const Unknown Code = "UNKNOWN"
