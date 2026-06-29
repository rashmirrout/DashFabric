package noregistrybypass

import (
	"go/parser"
	"go/token"
	"testing"
)

func violations(t *testing.T, src, pkgPath string) []Violation {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Check(fset, f, pkgPath)
}

// Direct call to registry.New outside pkg/registry/ must be flagged.
func TestCheck_DirectCall_Flagged(t *testing.T) {
	src := `package actor
import "github.com/dashfabric/fm/pkg/registry"
var _ = registry.New[string, int]("vnet")
`
	vs := violations(t, src, "github.com/dashfabric/fm/pkg/actor")
	if len(vs) != 1 {
		t.Fatalf("violations = %d, want 1; got %v", len(vs), vs)
	}
	if vs[0].Message == "" {
		t.Error("violation message is empty")
	}
}

// The same call inside pkg/registry/vnet/ is allowed.
func TestCheck_InsideRegistryPkg_Allowed(t *testing.T) {
	src := `package vnet
import "github.com/dashfabric/fm/pkg/registry"
var _ = registry.New[string, int]("vnet")
`
	vs := violations(t, src, "github.com/dashfabric/fm/pkg/registry/vnet")
	if len(vs) != 0 {
		t.Errorf("violations = %d inside registry subtree, want 0; got %v", len(vs), vs)
	}
}

// Exactly pkg/registry itself is also allowed (the root package).
func TestCheck_RegistryRootPkg_Allowed(t *testing.T) {
	src := `package registry
import "github.com/dashfabric/fm/pkg/registry"
var _ = registry.New[string, int]("x")
`
	vs := violations(t, src, "github.com/dashfabric/fm/pkg/registry")
	if len(vs) != 0 {
		t.Errorf("violations = %d for root registry pkg, want 0", len(vs))
	}
}

// Calling the typed constructor vnet.New() is never flagged.
func TestCheck_TypedConstructor_NotFlagged(t *testing.T) {
	src := `package actor
import "github.com/dashfabric/fm/pkg/registry/vnet"
var _ = vnet.New()
`
	vs := violations(t, src, "github.com/dashfabric/fm/pkg/actor")
	if len(vs) != 0 {
		t.Errorf("violations = %d for typed constructor, want 0; got %v", len(vs), vs)
	}
}

// A file that doesn't import the registry package at all is clean.
func TestCheck_NoRegistryImport_Clean(t *testing.T) {
	src := `package actor
import "context"
var _ = context.Background()
`
	vs := violations(t, src, "github.com/dashfabric/fm/pkg/actor")
	if len(vs) != 0 {
		t.Errorf("violations = %d for no-import file, want 0", len(vs))
	}
}

// Multiple violations in one file are all reported.
func TestCheck_MultipleViolations(t *testing.T) {
	src := `package actor
import "github.com/dashfabric/fm/pkg/registry"
var a = registry.New[string, int]("a")
var b = registry.New[int, string]("b")
`
	vs := violations(t, src, "github.com/dashfabric/fm/pkg/actor")
	if len(vs) != 2 {
		t.Errorf("violations = %d, want 2; got %v", len(vs), vs)
	}
}

// A renamed import alias is still detected.
func TestCheck_RenamedAlias_Flagged(t *testing.T) {
	src := `package actor
import reg "github.com/dashfabric/fm/pkg/registry"
var _ = reg.New[string, int]("x")
`
	vs := violations(t, src, "github.com/dashfabric/fm/pkg/actor")
	if len(vs) != 1 {
		t.Fatalf("violations = %d for renamed alias, want 1; got %v", len(vs), vs)
	}
}

// A blank import (_) is skipped gracefully.
func TestCheck_BlankImport_Skipped(t *testing.T) {
	src := `package actor
import _ "github.com/dashfabric/fm/pkg/registry"
`
	vs := violations(t, src, "github.com/dashfabric/fm/pkg/actor")
	if len(vs) != 0 {
		t.Errorf("violations = %d for blank import, want 0", len(vs))
	}
}

// Non-generic form (registry.New without type args) is also caught.
func TestCheck_NonGenericCall_Flagged(t *testing.T) {
	src := `package actor
import "github.com/dashfabric/fm/pkg/registry"
var _ = registry.New("x")
`
	vs := violations(t, src, "github.com/dashfabric/fm/pkg/actor")
	if len(vs) != 1 {
		t.Errorf("violations = %d for non-generic call, want 1; got %v", len(vs), vs)
	}
}

// A deep pkg/registry/... subdirectory is also in the allowlist.
func TestCheck_DeepRegistrySubpackage_Allowed(t *testing.T) {
	src := `package internal
import "github.com/dashfabric/fm/pkg/registry"
var _ = registry.New[string, int]("x")
`
	vs := violations(t, src, "github.com/dashfabric/fm/pkg/registry/nic/internal")
	if len(vs) != 0 {
		t.Errorf("violations = %d for deep registry subpkg, want 0", len(vs))
	}
}
