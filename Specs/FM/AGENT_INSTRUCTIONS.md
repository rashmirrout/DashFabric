# Fabric Manager Implementation: Agent Instructions & Protocols

**Version:** 1.0  
**Status:** Active - Binding Document for All Implementation Work  
**Scope:** All FM implementation across Phases 1-4  
**Audience:** AI Agents, Engineers, Code Reviewers

---

## 1. Core Testing Mandate (Non-Negotiable)

### 1.1 Coverage Requirements

**Absolute Minimums:**
- **Line Coverage:** 100% (no exceptions, no excluded files)
- **Branch Coverage:** 100% (all decision paths tested)
- **Mutation Kill Rate:** >90% (verify tests catch bugs)
- **Race Condition Detection:** 100% (Go `-race` flag on all tests)

**Verification Method:**
```bash
make test-coverage              # Fails if coverage < 100%
make test-mutation              # Fails if kill_rate < 90%
make test-race                  # Fails if -race detects races
```

**Coverage Tracking:**
- Coverage report generated per PR
- Coverage dashboard showing trends (must not decline)
- Coverage > 100% considered suspicious (likely over-tested) → review for redundancy

### 1.2 Test Types & Proportions

**Required Test Mix:**
- **Unit Tests:** 60% of suite (fast, isolated, comprehensive)
- **Integration Tests:** 25% of suite (real dependencies, critical flows)
- **Chaos/Property Tests:** 15% of suite (verify invariants, fault tolerance)

**Test Execution & Timing:**
- **Unit tests:** Must run in <30 seconds
- **Integration tests:** Must run in <2 minutes
- **Full suite (unit + integration + chaos):** Must run in <5 minutes
- **Mutation testing:** Separate CI job, allowed to be slower

### 1.3 Test Documentation

**For Each Test File:**
- [ ] Test plan document (`tests/<component>/TEST_PLAN.md`)
  - Test objectives (what invariants are verified?)
  - Test scenarios (happy path, edge cases, error cases)
  - Expected outcomes (pass criteria)
  - Mutation testing notes (which bugs should tests catch?)

**Example Test Plan Structure:**
```markdown
# TEST_PLAN.md: Layer 2 Consistency Rules

## Objectives
- Verify no self-references possible
- Verify no dangling references possible
- Verify no circular dependencies possible
- Verify version monotonicity enforced
- Verify VNET isolation enforced

## Test Scenarios
- Self-reference test (construct references itself → REJECTED)
- Dangling reference test (ref to non-existent construct → REJECTED)
- Circular dependency test (A→B→C→A → REJECTED)
- Version monotonicity test (v5→v3 → REJECTED)
- Cross-VNET reference test (VNET_A references VNET_B → REJECTED)

## Property-Based Tests
- For any sequence of consistency rule tests, system remains in valid state
- For any N concurrent writes, consistency rules still enforced

## Mutation Testing Notes
- If self-ref check removed → 3 tests fail (mutation killed)
- If dangling ref check removed → 5 tests fail (mutation killed)
- If version check removed → 4 tests fail (mutation killed)
```

### 1.4 Chaos & Resilience Testing

**Mandatory Chaos Scenarios:**
- **Replica Crash:** Kill a replica mid-operation → system recovers automatically
- **Network Latency:** Inject 500ms latency → retries work, timeouts enforced
- **Network Partition:** Simulate zone partition → circuit breaker opens, traffic diverted
- **Database Failure:** etcd goes down → graceful degradation, recovery when etcd returns
- **Concurrent Load:** 1000 concurrent requests → no crashes, no deadlocks, latency acceptable

**Chaos Test Documentation:**
```markdown
# CHAOS_PLAN.md: Replica Crash Scenario

## Setup
- Start FM cluster (3 replicas)
- Send 100 concurrent requests (Goal State programs)
- Kill primary replica (simulated crash)

## Expected Behavior
- In-flight requests: 90%+ should complete, <10% may fail
- Pending requests: Should be resubmitted to secondary replicas
- Failover time: <30 seconds (via K8s Lease)
- System remains healthy: Latency p99 < 2s during recovery

## Verification
- Metrics: primary_killed=true, failover_time_ms=<30000, requests_recovered=90%+
- Logs: Clear trace of replica detection, election, recovery
- Traces: Jaeger shows request flow through retry/timeout logic
```

---

## 2. Build System & Compilation (Makefile-Based)

### 2.1 Makefile Requirements

**Mandatory Targets:**
```makefile
# Build
make build              # Compile Go code (go build -race -v ./...)
make clean              # Remove build artifacts

# Testing (all must enforce minimums)
make test               # Run unit + integration tests (-race, fail if <100% coverage)
make test-unit          # Unit tests only
make test-integration   # Integration tests only
make test-chaos         # Chaos tests only
make test-coverage      # Report coverage, fail if <100%
make test-mutation      # Run mutation testing, fail if kill_rate < 90%
make test-race          # Run with -race detector
make test-all           # All tests + mutation + benchmarks

# Code Quality
make lint               # Run golangci-lint (fail on any issues)
make fmt                # Format code (gofmt, goimports)
make vet                # Run go vet
make staticcheck        # Run staticcheck

# Benchmarks & Performance
make benchmark          # Run benchmarks, show baseline vs last run
make profile            # Run pprof profiling (CPU, memory, goroutines)

# Documentation
make docs               # Generate API docs (go doc -html)

# Docker
make docker-build       # Build Docker image
make docker-test        # Run tests in Docker
make docker-run         # Run FM service in Docker

# CI/CD
make ci                 # Run all checks (build + lint + test + coverage + mutation)
make pre-commit         # Pre-commit hook (quick checks)
```

**Example Makefile Snippet:**
```makefile
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -race -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	if (( $$(echo "$$COVERAGE < 100" | bc -l) )); then \
		echo "ERROR: Coverage $$COVERAGE% < 100%"; \
		exit 1; \
	fi; \
	echo "✓ Coverage: $$COVERAGE%"

.PHONY: test-mutation
test-mutation:
	@echo "Running mutation testing..."
	mutate -v ./... > mutation.log 2>&1
	@KILLED=$$(grep "Killed mutations:" mutation.log | awk '{print $$3}'); \
	TOTAL=$$(grep "Total mutations:" mutation.log | awk '{print $$3}'); \
	RATE=$$((KILLED * 100 / TOTAL)); \
	if (( RATE < 90 )); then \
		echo "ERROR: Mutation kill rate $$RATE% < 90%"; \
		exit 1; \
	fi; \
	echo "✓ Mutation kill rate: $$RATE%"

.PHONY: ci
ci: build lint test-race test-coverage test-mutation
	@echo "✓ All CI checks passed"
```

### 2.2 CI/CD Pipeline (GitHub Actions / GitLab CI)

**Required Checks (Blocking PRs):**
1. `go build` without warnings
2. `golangci-lint` with strict config (all linters enabled)
3. Unit tests (70% of all tests)
4. Integration tests (25% of all tests)
5. `go test -race` (no race conditions)
6. Coverage 100% (line + branch)
7. Mutation testing >90% kill rate
8. Docker image builds successfully
9. Security scanning (no hardcoded credentials, OWASP checks)

**Failed Checks → PR blocked until fixed**

---

## 3. Test Planning, Documentation, Execution & Tracking

### 3.1 Test Planning Process

**Before Writing Code:**
1. **Create TEST_PLAN.md** for each component
   - List test objectives (invariants to verify)
   - List test scenarios (happy path, edge cases, errors)
   - List mutation testing expectations (what bugs should tests catch?)
   - Estimate test count needed for 100% coverage

2. **Design Test Data Fixtures** (reusable test data)
   - Create `testutil/fixtures.go` for pre-built test objects
   - Document fixture assumptions
   - Maintain fixture catalog (prevent duplication)

3. **Review Test Plan with Team**
   - Confirm scenarios comprehensive
   - Identify gaps (missing edge cases)
   - Estimate LOC for tests vs implementation

**Example Test Plan (Layer 1 Dedup Cache):**
```markdown
# TEST_PLAN.md: Layer 1 Dedup Cache

## Component Under Test
`pkg/layer1/dedup_cache.go` - SHA256-based deduplication with LRU eviction

## Test Objectives
1. Verify SHA256 fingerprint computed correctly
2. Verify duplicate events detected and not passed to Layer 2
3. Verify LRU eviction working (old entries removed)
4. Verify cache hit/miss ratio tracked
5. Verify cache is thread-safe under concurrent access

## Test Scenarios
### Happy Path
- TC-001: New event → SHA256 computed → cache miss → event forwarded ✓
- TC-002: Duplicate event → same SHA256 → cache hit → event dropped ✓
- TC-003: Cache full (10,000 entries) → LRU evicts oldest entry ✓

### Edge Cases
- TC-004: Very large event (1MB+) → fingerprint computed correctly ✓
- TC-005: Collision resistant (different events same SHA256?) → No ✓
- TC-006: Concurrent writes to cache (100 goroutines) → No data corruption ✓

### Error Cases
- TC-007: Event nil/empty → panic safely ✓
- TC-008: Out of memory → graceful degradation (stop caching) ✓

## Mutation Testing Expectations
- Remove SHA256 computation → 3 tests fail (hash mismatch)
- Remove LRU eviction → 2 tests fail (cache not bounded)
- Change cache size → 1 test fails (hardcoded assumption)
- Remove concurrency lock → -race detector fails

## Test Count Estimate
- Unit tests: 8 (happy path, edge, error)
- Property-based tests: 3 (distribution, consistency, bounds)
- Benchmarks: 2 (cache hit rate, eviction cost)
- **Total: 13 tests for this component**
```

### 3.2 Test Execution Tracking

**Test Execution Spreadsheet** (shared with team):
- Component, Test ID, Status (pending/in-progress/passed/failed)
- Execution date, executed by, duration
- Issues found (if any)
- Mutation testing coverage
- Sign-off

**Example Tracking Row:**
| Component | Test ID | Status | Executed | Duration | Issues | Mutation Coverage | Sign-Off |
|-----------|---------|--------|----------|----------|--------|------------------|----------|
| L1 Dedup | TC-001 | ✓ Passed | 2026-06-24 | 120ms | None | 3/3 mutations killed | A1 |
| L1 Dedup | TC-002 | ✓ Passed | 2026-06-24 | 150ms | None | 3/3 mutations killed | A1 |

### 3.3 Test Reporting & Metrics

**Weekly Test Report (Friday):**
- Total tests written this week: X
- Total tests passing: Y
- Test pass rate: Y/X
- Coverage this week: Z% (absolute coverage %))
- Coverage trend (week over week)
- Mutation kill rate: M%
- Critical bugs found: N
- Issues backlog: P

---

## 4. World-Class Design Patterns & Architecture Principles

### 4.1 Architectural Patterns (Mandatory)

**1. Actor Model (Layer 2)**
- Per-type actors serialize writes (one actor per construct type)
- Concurrent reads allowed (RWMutex protected)
- Enables parallelism while maintaining consistency
- Used in: VnetActor, NicActor, RouteTableActor, ACLActor, MappingActor

**2. Plugin Architecture (Layer 4)**
- Define plugin interface early (do not add methods mid-implementation)
- Support hot-loading (plugins can be added/removed without restart)
- Plugin registry maintains list of active plugins
- Used in: Intel DPU, Nvidia DPU, Custom plugins

**3. Chain of Responsibility (Reliability Patterns)**
- Request flows through: Circuit Breaker → Retry → Timeout → Transport
- Each handler can reject/transform/forward request
- Used in: Request routing (CB → Retry → Timeout → Plugin)

**4. Observer Pattern (Feedback Loops)**
- Device state changes trigger observers
- Reconciliation observes divergence events
- Used in: Watch streams (CB → Layer 3), Divergence detection

**5. Repository Pattern (Storage Abstraction)**
- Storage interface hides T1/T2/T3 backend details
- Easy to swap implementations (etcd → RocksDB for testing)
- Used in: StateStore interface, all DB access

### 4.2 SOLID Principles (All Enforced)

**Single Responsibility**
- One type = one responsibility (LRU cache does caching only, not serialization)
- One function = one task (no god functions >50 lines)
- ✅ Code review will flag violations

**Open/Closed**
- Open for extension (plugin interface, middleware chain)
- Closed for modification (once interface stable, no breaking changes)
- ✅ New vendor plugins added without modifying existing code

**Liskov Substitution**
- All plugin implementations must be substitutable
- All storage backends must be substitutable
- ✅ Tests use mock implementations successfully

**Interface Segregation**
- No fat interfaces (max 5 methods per interface)
- Clients depend on minimal interfaces
- ✅ Example: HealthChecker interface has 1 method (Check())

**Dependency Inversion**
- Depend on abstractions (interfaces), not concretions (structs)
- Inject dependencies at construction time
- ✅ TestUtil provides mock implementations for all interfaces

### 4.3 Code Quality Standards

**Function Length:**
- Maximum 50 lines per function
- If longer: break into helper functions
- Justified exceptions: generated code, table-driven tests

**Cyclomatic Complexity:**
- Maximum 10 per function (golangci-lint enforces via gocyclo)
- If higher: extract sub-functions
- Justified exception: switch statements with many cases

**Naming Conventions:**
- Types: PascalCase (CircuitBreaker, not circuit_breaker)
- Functions: camelCase (isValid, not is_valid)
- Constants: ALL_CAPS (MAX_RETRIES, not max_retries)
- Private: lowercase first letter (circuitBreaker, not CircuitBreaker)
- Unexported helpers: prefixed with _ or inside packages (no public_() functions)

**Documentation:**
- Every exported type: `// Type describes...` doc comment
- Every exported function: `// Func does X and returns Y` doc comment
- Complex logic: inline comments explaining WHY (not WHAT)
- No WHAT comments (WHAT is obvious from code; WHY is not)

**Error Handling:**
- Errors are values, check them
- No silent failures (logging != handling)
- Distinguish retryable vs non-retryable errors explicitly
- Panic only on programmer error (contract violations)

---

## 5. Extensibility, Reliability, Scalability (ERS Principles)

### 5.1 Extensibility Requirements

**Design Must Support:**
- [ ] New vendor plugins (add without modifying existing plugins)
- [ ] New discovery methods (add etcd, Consul, DNS, etc. without changing Layer 1)
- [ ] New health check types (add gRPC, TCP, custom without changing health monitor)
- [ ] New load balancing strategies (add without modifying Layer 3 composition)
- [ ] New authentication methods (add JWT, mTLS, API key without changing existing auth)

**Extensibility Checklist Per Component:**
- [ ] Interface defined before implementation
- [ ] Plugin/strategy/decorator pattern used
- [ ] Registry maintains active components
- [ ] No hardcoded references (all via registry)
- [ ] Configuration allows enabling/disabling components

**Example: Multi-Vendor Plugin Extensibility**
```go
// Define interface FIRST
type DevicePlugin interface {
    Configure(config Config) error
    Program(ctx context.Context, goal *GoalState) error
    Status(ctx context.Context) (*PluginStatus, error)
}

// Create registry for plugins
type PluginRegistry struct {
    plugins map[string]DevicePlugin
}

// New vendor adds plugin WITHOUT modifying existing code
func RegisterIntelPlugin(reg *PluginRegistry) {
    reg.Register("intel_dpu", NewIntelPlugin())
}
```

### 5.2 Reliability Requirements

**Design Must Support:**
- [ ] Automatic failure detection (within 30 seconds)
- [ ] Automatic recovery (90%+ without human intervention)
- [ ] Graceful degradation (keep system running when subsystems fail)
- [ ] Health monitoring (all components expose health status)
- [ ] Observability (all failures logged + traced + metered)

**Reliability Checklist Per Component:**
- [ ] Circuit breaker pattern used (prevent cascading failures)
- [ ] Retry logic with exponential backoff (transient failures recover)
- [ ] Timeout management (no indefinite waits)
- [ ] Error classification (retryable vs terminal)
- [ ] Fallback strategy (when retries exhaust, degrade gracefully)
- [ ] Health check endpoint (readiness + liveness)
- [ ] Observability: errors logged, retries metered, traces recorded

**Example: Reliable Device Programming**
```go
// Design for failure from start
func (plugin *Plugin) Program(ctx context.Context, goal *GoalState) error {
    // 1. Classify error
    err := plugin.send(goal)
    if err == nil { return nil }
    
    if !isRetryable(err) {
        // Circuit breaker will handle (open after N failures)
        return err
    }
    
    // 2. Retry with exponential backoff
    return exponentialBackoff(func() error {
        return plugin.send(goal)
    }, ctx)
}

// Health check exposes circuit breaker state
func (plugin *Plugin) Health() *Health {
    return &Health{
        Status: plugin.circuitBreaker.State(),
        Failures: plugin.failureCount,
        LastError: plugin.lastError,
        ErrorRate: plugin.ErrorRate(),
    }
}
```

### 5.3 Scalability Requirements

**Design Must Support:**
- [ ] Horizontal scaling (add more shards, not more memory per pod)
- [ ] Data sharding (partition by vnet_id, consistent hash ensures stability)
- [ ] Per-shard parallelism (10 workers per plugin, per shard)
- [ ] Concurrent request handling (1000+ concurrent requests per shard)
- [ ] Bounded memory (no unbounded caches; LRU eviction enforced)
- [ ] Minimal state propagation (only changes sent, not full state)

**Scalability Checklist Per Component:**
- [ ] Data structures use O(1) lookups (hash maps, not linked lists)
- [ ] Algorithms are sublinear (O(log N) or better)
- [ ] Memory is bounded (fixed-size caches with eviction)
- [ ] No global locks (use per-shard locks, actor model)
- [ ] Batch processing (collect 100 items, process once, not one-by-one)
- [ ] Async processing (don't block on slow operations)
- [ ] Connection pooling (reuse connections, don't create per-request)

**Example: Scalable Goal State Generation**
```go
// Bad (scales poorly): Generate Goal State sequentially per ENI
for _, eni := range vnets[vnet].enis {
    goal := compose(eni)  // Takes 10ms per ENI
    // 1000 ENIs = 10 seconds (not scalable)
}

// Good (scales well): Compose in parallel with worker pool
type Composer struct {
    workers int  // 10 workers per plugin
    queue   chan *ENI
}

func (c *Composer) Compose(enis []*ENI) {
    // Queue all ENIs
    for _, eni := range enis {
        c.queue <- eni  // Non-blocking send
    }
    
    // Workers compose in parallel
    // 1000 ENIs / 10 workers = 100 iterations, each takes 10ms = 1 second (scalable)
}
```

---

## 6. Production-Grade Software Engineering Principles

### 6.1 Zero Downtime & High Availability

- **Deployment:** Rolling updates (always >1 pod running)
- **Leadership:** K8s Lease coordination (automatic failover <30s)
- **State Management:** Persistent state in etcd + RocksDB (survive pod restart)
- **Connection Draining:** Graceful shutdown (drain in-flight requests before exit)
- **Health Checks:** Readiness (accepting traffic?) + Liveness (still alive?)

### 6.2 Monitoring & Observability

**All Components Must Expose:**
- **Metrics** (Prometheus format)
  - Counters: events_received, events_processed, errors_total
  - Gauges: active_requests, queue_depth, circuit_breaker_state
  - Histograms: latency_ms, program_duration_ms
  - Rate: requests/sec, errors/sec

- **Logs** (structured JSON)
  - Every request: request_id, user, operation, status, latency
  - Every error: error_type, stack_trace, context
  - Correlation: trace_id propagated across layers

- **Traces** (OpenTelemetry)
  - Full request flow (Layer 1 → 2 → 3 → 4 → device)
  - Timing of each layer
  - Errors and retries visible in trace

**Example Observability:**
```go
// Structured logging
logger.Info("goal_state_generated",
    zap.String("request_id", req.ID),
    zap.String("vnet_id", goal.VnetID),
    zap.Int64("duration_ms", elapsed),
    zap.String("fingerprint", goal.Fingerprint),
)

// Prometheus metrics
metricsGoalStatesGenerated.WithLabelValues(vnet.Vendor).Inc()
metricsCompositionDuration.WithLabelValues(vnet.Vendor).Observe(float64(elapsed))

// OpenTelemetry trace
span := tracer.Start(ctx, "compose_goal_state")
defer span.End()
span.SetAttributes(
    attribute.String("vnet_id", goal.VnetID),
    attribute.String("fingerprint", goal.Fingerprint),
)
```

### 6.3 Security (Built-In)

- **Secrets:** Never log secrets, never embed in code (use env vars / K8s secrets)
- **TLS:** All RPC encrypted (TLS 1.3 minimum)
- **Authentication:** Mandatory for all APIs (bearer token, JWT, mTLS)
- **Authorization:** RBAC enforced (who can call what?)
- **Audit Logging:** All writes logged with user, timestamp, changes
- **Input Validation:** All inputs validated (length, format, type)

### 6.4 Performance & Efficiency

**Latency Targets:**
- Device registration: <100ms p99
- Goal State composition: <50ms p99
- Plugin programming: <100ms p99 (per replica)
- Full ingestion-to-program: <1s p99

**Throughput Targets:**
- Events ingested: 50k+ events/sec
- Unique events (post-dedup): 10k+ events/sec
- Goal States composed: 1000+ per second
- Devices programmed: 100+ per second per plugin

**Efficiency Targets:**
- Memory: <1MB per ENI
- CPU: <1 core per 1000 concurrent requests
- Network: Minimal state propagation (deltas, not full state)

---

## 7. Implementation Standards

### 7.1 Code Review Checklist

**Every PR Must Pass:**
- [ ] All tests passing (unit + integration + chaos)
- [ ] 100% coverage maintained (or improved)
- [ ] Mutation kill rate >90%
- [ ] No lint warnings (golangci-lint clean)
- [ ] No race conditions (-race flag clean)
- [ ] No secrets committed (pre-commit hook)
- [ ] Design patterns followed (SOLID, ERS principles)
- [ ] Documentation updated (code comments, test plans, API docs)
- [ ] Performance impact acceptable (benchmarks show no regression >5%)
- [ ] Security reviewed (no hardcoded secrets, input validation, TLS used)

### 7.2 Commit Message Format

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types:** feat, fix, refactor, docs, test, perf, ci, chore  
**Scope:** layer1, layer2, layer3, layer4, feedback, testing, observability, etc.  
**Subject:** Imperative, present tense, lowercase, no period  
**Body:** Explain WHAT and WHY (not HOW)  
**Footer:** Closes #issue_number, Breaking-Change: if applicable

**Example:**
```
feat(layer2): implement 5 consistency rules enforcement

Add validation to Layer 2 to enforce:
- No self-references
- No dangling references
- No circular dependencies
- Version monotonicity
- VNET isolation

Validation happens at write-time in consistency engine.
Tests cover all 5 rules with property-based testing.

Closes #42
```

### 7.3 Git Workflow

1. **Create feature branch:** `git checkout -b feat/layer1-dedup-cache`
2. **Implement + test:** Write tests first, then code
3. **Local verification:** `make ci` must pass
4. **Push to remote:** `git push origin feat/layer1-dedup-cache`
5. **Create PR:** Link to issue, fill out template
6. **Code review:** At least 2 approvals required
7. **Merge:** Squash and merge (one commit per feature)

### 7.4 Documentation Requirements

**Per Component:**
- [ ] README.md (what it does, how to use)
- [ ] TEST_PLAN.md (test objectives, scenarios, mutation expectations)
- [ ] API documentation (exported types/functions with doc comments)
- [ ] Example usage (code examples showing common use cases)
- [ ] Performance characteristics (latency, throughput, memory)

**Per Phase:**
- [ ] Phase completion report (what was delivered, metrics achieved)
- [ ] Metrics summary (coverage, mutation kill rate, performance targets)
- [ ] Known issues (bugs, limitations, workarounds)
- [ ] Next phase notes (what to focus on, lessons learned)

---

## 8. AI Agent Responsibilities & Constraints

### 8.1 AI Agent Execution Rules

**MUST DO:**
1. ✅ Write tests FIRST (before implementation)
2. ✅ Achieve 100% coverage (no exceptions)
3. ✅ Verify mutation kill rate >90% (before declaring done)
4. ✅ Follow SOLID + ERS principles (code reviews will check)
5. ✅ Use Makefile targets (never run `go test` directly; use `make test`)
6. ✅ Document test plans (TEST_PLAN.md before implementation)
7. ✅ Update tracking spreadsheet (test execution log)
8. ✅ Report metrics weekly (coverage, mutation rate, performance)

**MUST NOT DO:**
1. ❌ Commit without `make ci` passing (all checks must pass)
2. ❌ Merge without code review (minimum 2 approvals)
3. ❌ Skip mutation testing (it's not optional)
4. ❌ Ignore race condition warnings (`go test -race` must be clean)
5. ❌ Hardcode values (use configuration, environment variables)
6. ❌ Ignore security concerns (TLS, secrets, auth, audit logs)
7. ❌ Optimize prematurely (measure first, optimize if needed)
8. ❌ Defer testing to end (testing happens during implementation)

### 8.2 AI Agent Decision Authority

**AI Can Autonomously Decide:**
- How to structure code (packages, files, types)
- Which design patterns to use (actor model, repository, etc.)
- What helper functions to create
- Test structure (table-driven, property-based, etc.)
- Refactoring & code cleanup (as long as tests pass)

**AI Must Ask User:**
- Scope changes (adding/removing features)
- Architecture decisions (if multiple valid approaches)
- Phase timeline adjustments (if risks identified)
- Breaking changes (if affects other layers/teams)
- Production decisions (deployment, traffic ramp-up)

### 8.3 AI Reporting Cadence

**Weekly (Friday EOD):**
- What was completed this week (features, tests, metrics)
- What's planned for next week (blockers identified?)
- Metrics snapshot (coverage %, mutation kill rate, latency p99)
- Issues found (bugs, design concerns, dependencies)

**Phase Completion (End of each phase):**
- Full phase report (deliverables, test results, metrics)
- Lessons learned (what went well, what to improve)
- Handoff notes (for next phase team)

---

## 9. Escalation & Review Process

### 9.1 Technical Review Board

**Decisions Requiring Review:**
- Any architectural change (propose → review → approve → implement)
- Any breaking change (affects other layers or teams)
- Any deviation from standards (explain exception → approve → document)
- Any security concern (flag → review → resolve)

**Review Approval Required From:**
- **Architecture:** Team Lead (A1 or equivalent)
- **Testing:** QA/Test Lead (D1 or equivalent)
- **Performance:** Platform Lead (C1 or equivalent)
- **Security:** Security reviewer (external or designated)

### 9.2 Blocker Resolution

**If Blocker Identified:**
1. Document blocker (what's blocked, why, impact)
2. Escalate to team lead (within 1 hour)
3. Team lead facilitates resolution (conference call, design session, etc.)
4. Document resolution (why this approach was chosen)
5. Resume implementation

**Blocked PRs:**
- Merge blocker → requires approval from at least 1 reviewer before merge
- Test blocker → requires all tests passing before merge
- Build blocker → requires `make ci` clean before merge

---

## 10. Exception Policy

**Exceptions to These Standards Require:**
1. Written justification (why exception needed?)
2. Risk assessment (what could go wrong?)
3. Approval from Team Lead (signature)
4. Monitoring plan (how will we verify it's OK?)
5. Cleanup plan (how will we fix it later?)
6. Documentation (explain exception for future maintainers)

**Examples of Valid Exceptions:**
- "Coverage 98% because external dependency not mockable" → Provide wrapper mock, document limitation
- "Latency 1.5s because upstream service slow" → Provide cache, document assumption
- "Mutation kill rate 85% because algorithm too complex to kill all mutants" → Document cases, plan refactoring

**Invalid Exceptions:**
- "Didn't write tests because we're in a hurry" → NO
- "Coverage 80% because it's good enough" → NO
- "Ignored security because it's only for testing" → NO

---

## 11. Continuous Improvement

### 11.1 Metrics Tracking

**Weekly Dashboard:**
- Coverage trend (must not decline)
- Mutation kill rate trend (must stay >90%)
- Test count trend (should increase with features)
- Bug escape rate (bugs found in code review vs production)
- Performance trend (latency p99, throughput)

### 11.2 Retrospectives

**After Each Phase:**
- What went well? (celebrate)
- What could be improved? (identify)
- What will we change next phase? (commit)
- Document lessons learned

**Example Retro Template:**
```markdown
# Phase 1 Retrospective

## What Went Well
- ✓ Dedup cache achieved 80% efficiency (exceeded 75% target)
- ✓ Actor model proven scalable (0 race conditions found)
- ✓ Chaos testing caught 5 bugs early

## What Could Be Improved
- Need better mock device simulation
- Need clearer error messages in Layer 2 validation
- Need more integration test scenarios

## Changes for Phase 2
- Create more realistic mock devices
- Improve error messages with specific guidance
- Add 10 more integration test scenarios
```

---

## 12. Sign-Off

**These protocols are BINDING for all FM implementation work.**

**Acknowledgment Required From:**
- [ ] Team Lead (Architecture owner)
- [ ] QA/Test Lead (Testing enforcement)
- [ ] Platform Lead (Performance & scalability)
- [ ] Security Lead (Security requirements)

**Effective Date:** [Phase 1 Week 1 Monday]  
**Version:** 1.0 (Living document; updates require team consensus)

---

## Appendix: Quick Reference

### Quick Command Reference
```bash
make build              # Compile
make test               # Run all tests (must pass for commit)
make test-coverage      # Show coverage (must be 100%)
make test-mutation      # Mutation kill rate (must be >90%)
make lint               # Code quality
make ci                 # All checks (this is the gate for merge)
make docs               # Generate documentation
```

### Coverage / Mutation / Performance Targets
| Metric | Minimum | Target | How to Measure |
|--------|---------|--------|----------------|
| Line Coverage | 100% | 100% | `make test-coverage` |
| Branch Coverage | 100% | 100% | Coverage tool report |
| Mutation Kill Rate | 90% | >90% | `make test-mutation` |
| Latency p99 | <1s | <500ms | Benchmarks + production metrics |
| Auto-Recovery | 90% | 95%+ | Chaos test results |
| Uptime | 99.9% | 99.95% | Production SLO tracking |

### Test Type Quick Reference
| Type | Count | Duration | Purpose |
|------|-------|----------|---------|
| Unit | 60% | <30s | Fast, isolated validation |
| Integration | 25% | <2m | Real dependency testing |
| Chaos | 15% | <5m | Fault tolerance verification |
| Property-Based | 10% | Varies | Invariant verification |
| Mutation | - | 5-10m | Test quality verification |

---

**Document Version:** 1.0  
**Last Updated:** 2026-06-19  
**Next Review:** End of Phase 1 (Week 6)
