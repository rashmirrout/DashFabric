// Package noregistrybypass implements the NO_REGISTRY_BYPASS lint rule.
//
// Rule: code outside pkg/registry/... must not call registry.New[...]
// directly. All callers must use the typed per-object constructors
// (vnet.New(), nic.New(), etc.) so the type-wrapper invariants
// (validation, defensive copies, Acquire/Release contract) are always
// enforced.
//
// Detection strategy (AST-only, no type-checker required):
//  1. Scan import declarations for the registry package path.
//  2. Record the local alias (default "registry").
//  3. Walk CallExpr nodes; flag any call whose function expression is
//     a SelectorExpr or IndexExpr/IndexListExpr rooted at the alias.
//  4. Suppress if the file's package path is inside pkg/registry/.
package noregistrybypass

import (
	"go/ast"
	"go/token"
)

const registryPkgPath = "github.com/dashfabric/fm/pkg/registry"
const registryPkgPrefix = "github.com/dashfabric/fm/pkg/registry/"

// Violation describes a single NO_REGISTRY_BYPASS finding.
type Violation struct {
	Pos     token.Position
	Message string
}

// Check inspects f for direct calls to registry.New when pkgPath is
// not inside the registry subtree. Returns one Violation per offending
// call site. fset is used only to resolve positions for the report.
func Check(fset *token.FileSet, f *ast.File, pkgPath string) []Violation {
	// Packages inside pkg/registry/... are allowed to call New directly.
	if pkgPath == registryPkgPath || len(pkgPath) > len(registryPkgPrefix) &&
		pkgPath[:len(registryPkgPrefix)] == registryPkgPrefix {
		return nil
	}

	// Find the local alias for the registry import (usually "registry").
	alias := localAlias(f)
	if alias == "" {
		return nil // file doesn't import the registry package
	}

	var violations []Violation
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isRegistryNewCall(call.Fun, alias) {
			violations = append(violations, Violation{
				Pos: fset.Position(call.Pos()),
				Message: "registry.New called directly outside pkg/registry/...; " +
					"use the typed constructor (e.g. vnet.New(), nic.New()) — NO_REGISTRY_BYPASS",
			})
		}
		return true
	})
	return violations
}

// localAlias returns the local name under which the registry package is
// imported in f, or "" if it is not imported.
func localAlias(f *ast.File) string {
	for _, imp := range f.Imports {
		path := imp.Path.Value // quoted string, e.g. `"github.com/dashfabric/fm/pkg/registry"`
		if path != `"`+registryPkgPath+`"` {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				return "" // blank or dot import — skip
			}
			return imp.Name.Name
		}
		return "registry" // default alias is last path element
	}
	return ""
}

// isRegistryNewCall reports whether expr is a call to <alias>.New,
// including generic instantiation forms <alias>.New[K, V].
func isRegistryNewCall(expr ast.Expr, alias string) bool {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		// registry.New (non-generic, unlikely but handle it)
		return selectorMatches(e, alias, "New")

	case *ast.IndexExpr:
		// registry.New[K] — single type argument
		return isRegistryNewCall(e.X, alias)

	case *ast.IndexListExpr:
		// registry.New[K, V] — multiple type arguments (Go 1.18+)
		return isRegistryNewCall(e.X, alias)
	}
	return false
}

func selectorMatches(sel *ast.SelectorExpr, pkgAlias, funcName string) bool {
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == pkgAlias && sel.Sel.Name == funcName
}
