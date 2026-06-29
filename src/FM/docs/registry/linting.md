# Registry Linting — NO_REGISTRY_BYPASS

## What the rule enforces

Code **outside** `pkg/registry/...` must not call `registry.New[...]` directly.
All other packages must use the typed per-object constructors:

| Registry | Constructor |
|---|---|
| VnetRegistry | `vnet.New(state)` |
| NicRegistry | `nic.New(state)` |
| MappingRegistry | `mapping.New(state)` |
| AclGroupRegistry | `acl.New(state)` |
| RouteGroupRegistry | `route.New(state)` |
| MeterPolicyRegistry | `meter.New(state)` |

**Why?** Each typed constructor enforces validation, defensive copies, and the
Acquire/Release refcount contract before touching the inner `registry.Registry[K, V]`.
Bypassing it produces unchecked, shared-mutable registry state.

## Running the linter

```bash
# Build once
go build -o bin/fm-lint ./tools/fm-lint

# Lint current module
./bin/fm-lint ./...

# Lint a specific package tree
./bin/fm-lint ./pkg/actor/...
```

Exit codes:

| Code | Meaning |
|---|---|
| 0 | No violations |
| 1 | One or more violations found |
| 2 | Tool error (bad arguments, parse failure) |

## Example violation output

```
pkg/actor/spawn.go:42:14: registry.New called directly outside pkg/registry/...; \
  use the typed constructor (e.g. vnet.New(), nic.New()) — NO_REGISTRY_BYPASS
```

## CI integration

```makefile
lint:
	go build -o bin/fm-lint ./tools/fm-lint
	./bin/fm-lint ./...
```

Add `lint` as a dependency of `ci` in the Makefile. Any violation exits 1 and
fails the build.

## Detection strategy

The checker is a pure-AST pass (`go/parser` + `go/ast`, no type-checker):

1. Scan `import` declarations for `github.com/dashfabric/fm/pkg/registry`.
2. Record the local alias (default `"registry"`, handles renamed and blank imports).
3. Walk `CallExpr` nodes; flag any call whose `Fun` is rooted at the alias and
   selects `New` — including generic instantiation forms
   `registry.New[K, V](...)` (`IndexListExpr`) and `registry.New[K](...)` (`IndexExpr`).
4. Suppress if the file's import path is exactly `pkg/registry` or has the prefix
   `pkg/registry/` (i.e., the registry subtree itself is allowed).

No `golang.org/x/tools` dependency — stdlib only.

## Adding a new per-object registry

1. Create `pkg/registry/<object>/` with a typed wrapper following the pattern
   in `pkg/registry/vnet/vnet.go`.
2. The new constructor (`<object>.New(state)`) calls `registry.New[K, V]("label")`
   internally — this is fine because the file lives inside `pkg/registry/...`.
3. Callers outside the registry subtree use `<object>.New(state)` and are
   automatically enforced by `fm-lint`.

## Future Scopes

- **`go vet` plugin**: Rewrite as a `go/analysis` pass so it can run via
  `go vet -vettool=$(which fm-lint) ./...` once `golang.org/x/tools` is added
  to the module.
- **Lock-order enforcement**: A second rule detecting cross-registry locking
  outside the canonical order (vnet → nic → mapping → acl → route → meter)
  would require call-graph analysis and is deferred to a later wave.
- **Auto-fix**: Suggest the correct typed constructor based on the key/value
  type arguments of the flagged `registry.New` call.
