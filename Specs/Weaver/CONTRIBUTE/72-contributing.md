# Weaver: Contributing Guide

> **Read Time:** 15 minutes  
> **Previous:** [71-plugin-development.md](./71-plugin-development.md) | **Next:** [../80-implementation-planner.md](../80-implementation-planner.md)

---

## How to Contribute

Thank you for interest in contributing to Weaver! This guide outlines the process.

---

## Code of Conduct

Weaver community follows the Contributor Covenant Code of Conduct.

**Core principles:**
- Be respectful and inclusive
- Welcome diverse perspectives
- Assume good intent
- Address concerns privately

**Violations:** Report to conduct@example.com

---

## Contribution Types

### Bug Reports

**How to report:**
1. Check if issue already exists (search GitHub issues)
2. Create new issue with template: "Bug Report"
3. Include:
   - Description of bug
   - Steps to reproduce
   - Expected behavior
   - Actual behavior
   - Environment (OS, Go version, Weaver version)
   - Logs/error messages

**Example:**
```
Title: Circuit breaker transitions too frequently in high-latency scenarios

Description:
When latency spikes above 500ms, circuit breaker toggles between CLOSED
and OPEN every 10 seconds, even though replicas eventually recover.

Steps to reproduce:
1. Deploy with high-latency replicas (500ms+ latency)
2. Send requests via Weaver
3. Monitor circuit breaker state

Expected: Circuit breaker stays in HALF_OPEN during recovery
Actual: Flips between OPEN and HALF_OPEN

Environment:
- OS: Linux
- Go: 1.21
- Weaver: v1.0.0
```

### Feature Requests

**How to request:**
1. Check if feature already requested
2. Create issue with template: "Feature Request"
3. Include:
   - Use case / problem it solves
   - Proposed solution
   - Alternatives considered
   - Impact (breaking changes?)

### Pull Requests (Code Contributions)

**Process:**
1. Fork repository
2. Create feature branch: `git checkout -b feature/description`
3. Make changes
4. Test thoroughly
5. Submit PR with description
6. Address review feedback
7. Merge when approved

---

## Development Workflow

### 1. Setup

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/weaver.git
cd weaver

# Add upstream remote
git remote add upstream https://github.com/dashfabric/weaver.git

# Install dependencies
go mod download
```

### 2. Create Branch

```bash
# Update main branch
git fetch upstream
git checkout main
git rebase upstream/main

# Create feature branch
git checkout -b feature/my-feature
```

### 3. Make Changes

```bash
# Edit files
vi pkg/loadbalancer/new_strategy.go

# Run tests
go test -race ./...

# Check formatting
golangci-lint run ./...
make fmt

# Commit changes
git add pkg/loadbalancer/new_strategy.go
git commit -m "feat: add new load balancing strategy"
```

### 4. Push & Create PR

```bash
# Push to your fork
git push origin feature/my-feature

# Create PR on GitHub
# Title: "feat: add new load balancing strategy"
# Body: Clear description of changes
```

### 5. Address Review Feedback

```bash
# Make requested changes
git add .
git commit -m "address review feedback"

# Push updates
git push origin feature/my-feature

# Do NOT force-push unless asked
```

### 6. Squash (if requested)

```bash
# Rebase and squash commits
git rebase -i upstream/main

# Mark commits to squash (s instead of pick)
# Force push after rebase
git push -f origin feature/my-feature
```

---

## Commit Message Guidelines

**Format:**
```
<type>(<scope>): <subject>

<body>

<footer>
```

**Type:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation
- `test`: Test additions
- `refactor`: Code restructuring
- `perf`: Performance improvement
- `chore`: Build, dependencies, etc.

**Scope:** Component affected (loadbalancer, discovery, health, etc.)

**Subject:**
- Imperative mood ("add" not "adds" or "added")
- No capital letter at start
- No period at end
- < 50 characters

**Examples:**
```
feat(loadbalancer): add weighted round-robin strategy

fix(circuitbreaker): prevent state flapping on high latency

docs(api-reference): clarify rate limiting dimensions

test(health): add tests for panic mode detection
```

---

## Testing Requirements

**All PRs must include:**

1. **Unit tests** (test new code)
   ```bash
   go test -coverage ./...
   # Aim for >80% coverage
   ```

2. **Integration tests** (test with real services)
   ```bash
   go test -tags=integration ./tests/integration
   ```

3. **No race conditions**
   ```bash
   go test -race ./...
   ```

4. **Existing tests must pass**
   ```bash
   go test ./...
   ```

**Example test:**
```go
func TestWeightedLoadBalancer(t *testing.T) {
  lb := NewWeightedLB()
  
  replicas := []*Replica{
    {Name: "r1", Weight: 2},
    {Name: "r2", Weight: 1},
  }
  
  distribution := make(map[string]int)
  for i := 0; i < 300; i++ {
    selected, _ := lb.Select(nil, replicas)
    distribution[selected.Name]++
  }
  
  if distribution["r1"] < 150 || distribution["r1"] > 250 {
    t.Errorf("weighted distribution failed")
  }
}
```

---

## Documentation Requirements

**When adding/changing features:**

1. **Update relevant docs:**
   - If new config option: update 30-configuration-reference.md
   - If new metric: update 39-metrics-reference.md
   - If new algorithm: update 31-load-balancing-strategies.md

2. **Add code comments:**
   ```go
   // Select replica using weighted round-robin.
   // Higher weights receive more traffic.
   func (w *WeightedRR) Select(...) {
     // ...
   }
   ```

3. **Update CHANGELOG.md**
   ```markdown
   ## v1.1.0 (2027-03-15)
   
   ### Features
   - Add weighted round-robin load balancing strategy
   
   ### Bug Fixes
   - Fix circuit breaker flapping on high latency
   
   ### Breaking Changes
   None
   ```

---

## Code Review Process

**Reviewers will check:**

1. ✅ **Correctness** - Does the code do what it intends?
2. ✅ **Tests** - Are tests comprehensive? Do they pass?
3. ✅ **Performance** - Any performance regressions?
4. ✅ **Security** - Any security issues?
5. ✅ **Style** - Follows Go conventions?
6. ✅ **Documentation** - Is it clear and complete?

**Feedback Cycles:**

- Author makes changes
- Re-request review
- Reviewers check again
- Repeat until approved
- Maintainer merges when ready

---

## Approval & Merge

**Merge requires:**
- ✅ At least 2 approvals (for core changes)
- ✅ All tests passing
- ✅ No conflicts with main
- ✅ CI/CD checks passing

**Maintainers merge** (contributors don't have permission)

---

## Areas Needing Help

### Priority 1 (High Impact)
- Performance optimizations (reduce latency, memory)
- Plugin ecosystem (discovery, LB, health check plugins)
- Documentation improvements
- Test coverage (aim for >90%)

### Priority 2 (Medium Impact)
- Bug fixes (see GitHub issues)
- Small feature requests (see GitHub issues)
- Example applications
- Language bindings (client libraries)

### Priority 3 (Community)
- Community plugins
- Blog posts / tutorials
- Case studies
- Translations (documentation)

---

## Getting Help

**Questions?**
- GitHub Discussions (Q&A)
- GitHub Issues (bugs/features)
- Email: community@example.com
- Slack: #weaver-dev (if available)

**Office Hours:**
- Wednesdays, 10:00 AM UTC
- Video call: (link in repo)
- Anyone welcome

---

## Contributor Recognition

**We recognize contributors:**

1. GitHub: Listed as contributor
2. CONTRIBUTORS.md: Added to file
3. Releases: Acknowledged in release notes
4. Swag: Stickers/t-shirt for major contributions

---

## License

By contributing, you agree that your contributions are licensed under MIT License.

---

**Navigation:**
- [← Previous](./71-plugin-development.md)
- [Index](../INDEX.md)
- [Next →](../80-implementation-planner.md)
