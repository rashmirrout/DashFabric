package types

import (
	"fmt"
	"strings"
)

// MaxIDLen is the upper bound FM accepts on any ID string received from the
// Control Bridge. The cap exists to keep log lines, audit records, and
// map-key memory bounded — not to encode CB's actual ID grammar.
const MaxIDLen = 256

// ENIID identifies a single Elastic Network Interface.
// Source of truth: Control Bridge. FM never mints these.
type ENIID string

// VnetID identifies a VNET. Owns mappings, route-groups, ACL-groups,
// meter-policies by reference (see Specs/me-and-ai/fm-data-model-sync.md §2).
type VnetID string

// RouteGroupID identifies an outbound-routing group shared across many ENIs.
type RouteGroupID string

// AclGroupID identifies a single ACL group. An ENI references up to 6
// (stage1+stage2, v4+v6, in+out).
type AclGroupID string

// MappingID identifies a single VNET-mapping entry (VIP/PA → underlay/PA).
type MappingID string

// MeterPolicyID identifies a meter (rate-limit / billing) policy.
type MeterPolicyID string

// HaScopeID identifies a high-availability scope — the unit over which a
// single FM pod claims an HA lease (Specs/FM/recovery-and-failover-design.md).
type HaScopeID string

// DeviceID identifies a managed device (DPU, host NIC, container vSwitch).
type DeviceID string

// TenantID identifies a tenant. Used for OPA decisions and audit attribution.
type TenantID string

// ValidateID applies FM's universal ID-string contract: non-empty,
// not longer than MaxIDLen, no embedded whitespace or NUL.
//
// FM does not assume CB's ID grammar beyond this. UUID / ULID / opaque-blob
// — all valid as long as they pass this gate.
func ValidateID(s string) error {
	if len(s) == 0 {
		return fmt.Errorf("id: empty")
	}
	if len(s) > MaxIDLen {
		return fmt.Errorf("id: length %d exceeds max %d", len(s), MaxIDLen)
	}
	if strings.ContainsAny(s, " \t\n\r\x00") {
		return fmt.Errorf("id: contains whitespace or NUL")
	}
	return nil
}

// Constructors. Each wraps ValidateID and returns the typed ID.
// Use these at trust boundaries (where a raw string arrives from CB, gRPC,
// fmctl input, etc.). Inside FM, pass the typed value directly.

func NewENIID(s string) (ENIID, error)             { return ENIID(s), ValidateID(s) }
func NewVnetID(s string) (VnetID, error)           { return VnetID(s), ValidateID(s) }
func NewRouteGroupID(s string) (RouteGroupID, error) {
	return RouteGroupID(s), ValidateID(s)
}
func NewAclGroupID(s string) (AclGroupID, error)   { return AclGroupID(s), ValidateID(s) }
func NewMappingID(s string) (MappingID, error)     { return MappingID(s), ValidateID(s) }
func NewMeterPolicyID(s string) (MeterPolicyID, error) {
	return MeterPolicyID(s), ValidateID(s)
}
func NewHaScopeID(s string) (HaScopeID, error) { return HaScopeID(s), ValidateID(s) }
func NewDeviceID(s string) (DeviceID, error)   { return DeviceID(s), ValidateID(s) }
func NewTenantID(s string) (TenantID, error)   { return TenantID(s), ValidateID(s) }

// IsZero reports whether the ID is the empty string. Useful in guard clauses
// where the zero value should never reach a registry or driver.
func (e ENIID) IsZero() bool         { return e == "" }
func (v VnetID) IsZero() bool        { return v == "" }
func (r RouteGroupID) IsZero() bool  { return r == "" }
func (a AclGroupID) IsZero() bool    { return a == "" }
func (m MappingID) IsZero() bool     { return m == "" }
func (m MeterPolicyID) IsZero() bool { return m == "" }
func (h HaScopeID) IsZero() bool     { return h == "" }
func (d DeviceID) IsZero() bool      { return d == "" }
func (t TenantID) IsZero() bool      { return t == "" }
