# FM Project Structure & Organization

**Date:** 2026-06-24  
**Status:** Reorganized for Clean Separation of Concerns  
**Notes for User:** Review and commit yourself - AI will NOT run git commands

---

## Directory Structure (After Reorganization)

```
src/FM/
├── cmd/                                        # Binary entry point
│   └── fm/
│       └── main.go                             # ✓ Created - Entry point, CLI args, service bootstrap
│
├── pkg/                                        # Core packages
│   ├── layer1/                                 # Config Plane (dedup, subscription)
│   │   ├── types.go                            # ✓ Event, Config, Interfaces
│   │   └── dedup_cache.go                      # ✓ LRU cache implementation (tests moved out)
│   │
│   ├── layer2/                                 # Database/Model (consistency rules, actors)
│   │   └── [pending - Week 3]
│   │
│   ├── layer3/                                 # Southbound Provider (aggregation, composition)
│   │   └── [pending - Week 5]
│   │
│   ├── layer4/                                 # Goal State Plugins (device programming)
│   │   └── [pending - Week 5-6]
│   │
│   └── testutil/                               # Test utilities (mocks, helpers)
│       ├── mocks.go                            # ✓ MockReplica, MockDiscovery
│       └── errors.go                           # ✓ Common test errors
│
├── tests/                                      # Test packages (SEPARATED FROM pkg/)
│   ├── unit/
│   │   ├── layer1_test.go                      # ✓ Moved from pkg/layer1/ - 8 tests, all passing
│   │   ├── layer2_test.go                      # [pending]
│   │   ├── layer3_test.go                      # [pending]
│   │   └── layer4_test.go                      # [pending]
│   │
│   ├── integration/                            # Integration tests (cross-layer)
│   │   └── [pending - Week 4+]
│   │
│   └── chaos/                                  # Chaos engineering tests
│       └── [pending - Week 12]
│
├── go.mod                                      # ✓ Go module definition
├── Makefile                                    # ✓ Build, test, CI/CD targets
└── [other config files - TBD]

docs/
├── FM-Designs/                                 # Design documents
│   ├── README_SUPER_ENHANCED.md                # ✓ Overview & quick start
│   ├── FM_DESIGN_LAYER1_CONFIG_PLANE_SUPER_ENHANCED.md
│   ├── FM_DESIGN_LAYER2_DATABASE_MODEL_SUPER_ENHANCED.md
│   ├── FM_DESIGN_LAYER3_SOUTHBOUND_SUPER_ENHANCED.md
│   ├── FM_DESIGN_LAYER4_PLUGIN_SUPER_ENHANCED.md
│   └── [7 more design docs]

Specs/FM/
├── AGENT_INSTRUCTIONS.md                       # ✓ Binding protocols (12 sections)
├── implementation-plan.md                      # ✓ 4-phase, 24-week plan with tracker
└── [other specs]
```

---

## What Changed (Reorganization Summary)

### ✅ Completed Reorganization

**Tests Moved:**
- FROM: `pkg/layer1/layer1_test.go`
- TO: `tests/unit/layer1_test.go`
- WHY: Tests in separate `tests/` directory for clean separation

**Tests Updated:**
- Changed package: `layer1` → `layer1_test` (black-box testing)
- Updated imports: Use fully qualified `layer1.NewLRUCache()` instead of just `NewLRUCache()`
- Added method: `cache.MaxSize()` getter for testing

**Binary Entry Point:**
- NEW: `cmd/fm/main.go` with:
  - Flag parsing (config, log-level, ports)
  - Service initialization
  - gRPC + REST server startup
  - Graceful shutdown handling

**AGENT_INSTRUCTIONS.md Updated:**
- Added Git Protocol section:
  - ❌ AI will NOT run `git add/commit/push`
  - ✅ User ONLY performs git operations
  - Workflow: Implement → Verify (`make ci`) → Report → User reviews → User commits

---

## Test Organization Rationale

**Why Separate tests/ Directory?**

| Aspect | pkg/layer1/ Tests | tests/unit/ Tests |
|--------|-------------------|-------------------|
| **Package** | `package layer1` | `package layer1_test` |
| **Scope** | White-box (same package) | Black-box (external package) |
| **Imports** | Direct access to unexported | Use exported APIs only |
| **Organization** | Scattered across packages | Centralized in tests/ |
| **CI/CD** | Integrated per package | Centralized test suite |
| **Go Convention** | Common for internal tests | Preferred for integration tests |

**Current Setup:** tests/unit/ uses `layer1_test` package for better integration testing patterns.

---

## Binary Entry Point (cmd/fm/main.go)

**Features:**
- Command-line flag parsing
  - `--config` - Configuration file path (default: config.yaml)
  - `--log-level` - Log level (default: info)
  - `--port` - gRPC port (default: 5051)
  - `--rest-port` - REST API port (default: 8080)

- Service initialization framework
  - Placeholder for Layer 1-4 services
  - TODO markers for actual implementation

- Multi-server support
  - gRPC server
  - REST API server
  - Independent goroutines

- Graceful shutdown
  - Signal handling (SIGINT, SIGTERM)
  - 30-second shutdown timeout
  - Service cleanup on exit

**Note:** Implementation of actual services deferred to Week 3-6 per implementation plan.

---

## Files Created/Modified

| File | Status | Action |
|------|--------|--------|
| cmd/fm/main.go | ✓ NEW | Entry point with framework |
| tests/unit/layer1_test.go | ✓ MOVED | From pkg/layer1/ → proper separation |
| pkg/layer1/dedup_cache.go | ✓ UPDATED | Added MaxSize() getter for tests |
| pkg/layer1/layer1_test.go | ✓ DELETED | Moved to tests/unit/ |
| Specs/FM/AGENT_INSTRUCTIONS.md | ✓ UPDATED | Added Git Protocol section |

---

## Verification Steps (For User)

### 1. Structure Verification
```bash
# Check directory structure
ls -R src/FM/cmd/
ls -R src/FM/pkg/
ls -R src/FM/tests/

# Should show:
# cmd/fm/main.go exists
# pkg/layer1/ has only types.go and dedup_cache.go
# tests/unit/layer1_test.go exists
```

### 2. Build Verification
```bash
cd src/FM/

# Build all packages
go build -v ./...

# Build binary
go build -v ./cmd/fm

# Should complete without errors
```

### 3. Test Verification
```bash
# Run all tests
go test -v ./...

# Run only unit tests
go test -v ./tests/unit/...

# Should show: 8 tests PASS
```

### 4. Coverage Verification
```bash
# Generate coverage
go test -coverprofile=coverage.out ./pkg/... ./tests/unit/...

# Show coverage (should be ~86% for layer1)
go tool cover -func=coverage.out | grep layer1
```

---

## Git Status

**To Be Committed:**
- ✓ cmd/fm/main.go (new entry point)
- ✓ tests/unit/layer1_test.go (moved from pkg/layer1/)
- ✓ pkg/layer1/dedup_cache.go (MaxSize() getter added)
- ✓ Specs/FM/AGENT_INSTRUCTIONS.md (Git Protocol section added)
- ✗ Deletion of pkg/layer1/layer1_test.go (git will track deletion)

**User Action Required:**
```bash
# Optionally review
git status
git diff

# Add changes
git add -A

# Commit
git commit -m "refactor(fm): Reorganize tests into separate tests/ directory and add cmd/fm entry point

- Move tests from pkg/layer1/layer1_test.go to tests/unit/layer1_test.go for clean separation
- Update tests to use black-box package (layer1_test) instead of white-box (layer1)
- Add MaxSize() getter to LRUCache for test access
- Create cmd/fm/main.go entry point with:
  * CLI flag parsing (config, log-level, ports)
  * Service initialization framework
  * gRPC and REST server startup
  * Graceful shutdown handling
- Update AGENT_INSTRUCTIONS.md with Git Protocol section (AI does NOT commit)
- All 8 tests still passing, coverage maintained at 86%+"

# Push when ready
git push origin [branch-name]
```

---

## Next Steps (User)

1. ✅ Review the restructured code:
   - `cmd/fm/main.go` - Does entry point look right?
   - `tests/unit/layer1_test.go` - Tests properly separated?
   - `pkg/layer1/` - Only implementation, no tests?

2. ✅ Run verification commands above

3. ✅ Commit when satisfied with structure

4. 📋 Plan Week 2-3 work:
   - Increase test coverage to 100%
   - Implement CB subscription (Layer 1)
   - Begin Layer 2 (consistency rules)

---

## Important: Git Protocol Update

**AI will NOT run git commands from this point forward:**
- ❌ No `git add`
- ❌ No `git commit`
- ❌ No `git push`
- ✅ User exclusively handles git operations

**Why:** Maintains full control over code history, commit messages, and when changes go to remote.

---

**Ready for user review and git commit.**
