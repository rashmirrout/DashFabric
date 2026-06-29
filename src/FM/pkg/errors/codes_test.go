package errors

import (
	"strings"
	"testing"
)

// Every catalog key must equal the Code field of its Info — guards
// against copy-paste drift between map key and value.
func TestCatalog_KeyMatchesInfoCode(t *testing.T) {
	for k, v := range catalog {
		if k != v.Code {
			t.Errorf("catalog[%q].Code = %q, must equal map key", string(k), string(v.Code))
		}
	}
}

// Spec catalog has 61 codes today (8+8+8+10+8+6+8+5). If the spec
// changes, update this number AND the catalog AND the spec — they
// move together.
func TestCatalog_HasExpectedCount(t *testing.T) {
	const want = 61
	if got := len(catalog); got != want {
		t.Errorf("catalog size = %d, want %d (spec §3 has 61 codes)", got, want)
	}
}

func TestCatalog_NoDuplicateStrings(t *testing.T) {
	seen := map[Code]bool{}
	for k := range catalog {
		if seen[k] {
			t.Errorf("duplicate code %q in catalog", string(k))
		}
		seen[k] = true
	}
}

func TestCatalog_AllCodesHaveMeaning(t *testing.T) {
	for k, v := range catalog {
		if v.Meaning == "" {
			t.Errorf("code %q has empty Meaning", string(k))
		}
	}
}

// Spec §9.2 requirement: every CRITICAL code MUST have a runbook at
// Specs/Runbooks/{code}.md. We don't verify the file exists here
// (Wave 6 doc-lint slice does that) but we enforce the path shape.
func TestCriticalCodes_HaveRunbookPath(t *testing.T) {
	for k, v := range catalog {
		if v.Severity != SeverityCritical {
			continue
		}
		if v.Runbook == "" {
			t.Errorf("CRITICAL code %q has empty Runbook (spec §9.2)", string(k))
			continue
		}
		if !strings.HasPrefix(v.Runbook, "Specs/Runbooks/") {
			t.Errorf("CRITICAL code %q runbook %q must start with Specs/Runbooks/", string(k), v.Runbook)
		}
		if !strings.HasSuffix(v.Runbook, ".md") {
			t.Errorf("CRITICAL code %q runbook %q must end with .md", string(k), v.Runbook)
		}
	}
}

// Non-CRITICAL codes should NOT carry a runbook (generic alerting handles
// them). Catches accidental over-promotion of non-critical errors.
func TestNonCriticalCodes_HaveNoRunbook(t *testing.T) {
	for k, v := range catalog {
		if v.Severity == SeverityCritical {
			continue
		}
		if v.Runbook != "" {
			t.Errorf("non-CRITICAL code %q has Runbook %q (only CRITICAL gets a runbook)", string(k), v.Runbook)
		}
	}
}

func TestClassify_KnownCode(t *testing.T) {
	info := Classify(REG_007_REFCOUNT_UNDERFLOW)
	if info.Severity != SeverityCritical {
		t.Errorf("REG_007 severity = %s, want CRITICAL", info.Severity)
	}
	if info.Recoverability != RecovPermanent {
		t.Errorf("REG_007 recov = %s, want PERMANENT", info.Recoverability)
	}
	if info.Retryable() {
		t.Error("REG_007 Retryable() = true, want false (PERMANENT)")
	}
}

func TestClassify_UnknownCode(t *testing.T) {
	info := Classify(Code("REG_999_NOT_REAL"))
	if info.Code != Unknown {
		t.Errorf("unknown code .Code = %q, want %q", string(info.Code), string(Unknown))
	}
	if info.Severity != SeverityError {
		t.Errorf("unknown code severity = %s, want ERROR", info.Severity)
	}
	if info.Retryable() {
		t.Error("unknown code Retryable() = true, want false")
	}
}

func TestRetryable_OnlyTransient(t *testing.T) {
	for _, c := range All() {
		info := Classify(c)
		wantRetry := info.Recoverability == RecovTransient
		if info.Retryable() != wantRetry {
			t.Errorf("Classify(%q).Retryable()=%v, want %v (recov=%s)",
				string(c), info.Retryable(), wantRetry, info.Recoverability)
		}
	}
}

func TestAll_ReturnsEveryRegisteredCode(t *testing.T) {
	if len(All()) != len(catalog) {
		t.Errorf("All() returned %d codes, catalog has %d", len(All()), len(catalog))
	}
}

func TestSeverity_String(t *testing.T) {
	cases := map[Severity]string{
		SeverityInfo:     "INFO",
		SeverityWarn:     "WARN",
		SeverityError:    "ERROR",
		SeverityCritical: "CRITICAL",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Severity(%d).String() = %q, want %q", int(s), got, want)
		}
	}
}

func TestRecoverability_String(t *testing.T) {
	cases := map[Recoverability]string{
		RecovNA:        "N/A",
		RecovTransient: "TRANSIENT",
		RecovPermanent: "PERMANENT",
		RecovOperator:  "OPERATOR",
	}
	for r, want := range cases {
		if got := r.String(); got != want {
			t.Errorf("Recoverability(%d).String() = %q, want %q", int(r), got, want)
		}
	}
}
