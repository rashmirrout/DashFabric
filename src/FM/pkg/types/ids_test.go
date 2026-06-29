package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateID_LengthBounds(t *testing.T) {
	cases := []struct {
		name    string
		s       string
		wantErr bool
	}{
		{"empty", "", true},
		{"one", "a", false},
		{"max", strings.Repeat("x", MaxIDLen), false},
		{"max+1", strings.Repeat("x", MaxIDLen+1), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateID(c.s)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidateID(%d bytes) err=%v wantErr=%v", len(c.s), err, c.wantErr)
			}
		})
	}
}

func TestValidateID_RejectsWhitespaceAndNUL(t *testing.T) {
	bad := []string{"a b", "a\tb", "a\nb", "a\rb", "a\x00b"}
	for _, s := range bad {
		if err := ValidateID(s); err == nil {
			t.Errorf("ValidateID(%q) accepted, want error", s)
		}
	}
}

func TestConstructors_RejectInvalid(t *testing.T) {
	if _, err := NewENIID(""); err == nil {
		t.Error("NewENIID(\"\") accepted, want error")
	}
	if _, err := NewVnetID("bad id"); err == nil {
		t.Error("NewVnetID(\"bad id\") accepted, want error")
	}
	if _, err := NewMappingID("ok-id-123"); err != nil {
		t.Errorf("NewMappingID(\"ok-id-123\") err=%v want nil", err)
	}
}

func TestIsZero(t *testing.T) {
	if !(ENIID("").IsZero() && VnetID("").IsZero() && DeviceID("").IsZero()) {
		t.Error("zero-value ID did not report IsZero=true")
	}
	if ENIID("x").IsZero() {
		t.Error("non-empty ENIID reported IsZero=true")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	type wire struct {
		ENI    ENIID    `json:"eni"`
		Vnet   VnetID   `json:"vnet"`
		Device DeviceID `json:"device"`
	}
	in := wire{ENI: "eni-1", Vnet: "vnet-7", Device: "dev-a"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"eni":"eni-1","vnet":"vnet-7","device":"dev-a"}`
	if string(b) != want {
		t.Fatalf("Marshal got %s want %s", b, want)
	}
	var out wire
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip got %+v want %+v", out, in)
	}
}

func TestMapKeyBehavior(t *testing.T) {
	m := map[ENIID]int{}
	m[ENIID("eni-1")] = 1
	m[ENIID("eni-2")] = 2
	if m[ENIID("eni-1")] != 1 || m[ENIID("eni-2")] != 2 {
		t.Fatal("ENIID does not behave as map key")
	}
	// VnetID and ENIID share underlying string but are distinct map key types.
	// This compiles by design; we just confirm they cohabit.
	v := map[VnetID]int{VnetID("eni-1"): 99}
	if v[VnetID("eni-1")] != 99 {
		t.Fatal("VnetID does not behave as map key")
	}
}

func TestSpecRevision_Newer(t *testing.T) {
	if !SpecRevision(2).Newer(SpecRevision(1)) {
		t.Error("2.Newer(1) = false")
	}
	if SpecRevision(1).Newer(SpecRevision(1)) {
		t.Error("1.Newer(1) = true (equal must not be newer)")
	}
	if SpecRevision(1).Newer(SpecRevision(2)) {
		t.Error("1.Newer(2) = true")
	}
}

func TestEpoch_Bumped(t *testing.T) {
	if !Epoch(5).Bumped(Epoch(4)) {
		t.Error("5.Bumped(4) = false")
	}
	if Epoch(4).Bumped(Epoch(5)) {
		t.Error("4.Bumped(5) = true")
	}
}

func TestWatermark_Regressed(t *testing.T) {
	if !Watermark(10).Regressed(Watermark(11)) {
		t.Error("10.Regressed(11) = false (10 < 11, must report regressed)")
	}
	if Watermark(11).Regressed(Watermark(10)) {
		t.Error("11.Regressed(10) = true")
	}
	if Watermark(10).Regressed(Watermark(10)) {
		t.Error("10.Regressed(10) = true (equal is not regressed)")
	}
}
