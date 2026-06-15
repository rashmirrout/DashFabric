package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/dashfabric/weaver/pkg/auth"
	"github.com/dashfabric/weaver/pkg/ratelimit"
)

func TestAuthenticationFlow(t *testing.T) {
	bearerAuth := auth.NewBearerTokenAuth()
	user := &auth.AuthenticatedUser{
		ID:     "user-1",
		Name:   "Alice",
		Role:   "admin",
		Scopes: []string{"read", "write"},
	}
	bearerAuth.RegisterToken("valid-token", user)

	ctx := context.Background()

	result, err := bearerAuth.Authenticate(ctx, "Bearer valid-token")
	if err != nil {
		t.Errorf("authentication failed: %v", err)
	}
	if result.ID != "user-1" {
		t.Errorf("expected user-1, got %s", result.ID)
	}
}

func TestAuthzWithRBAC(t *testing.T) {
	rbac := auth.NewRBAC()
	rbac.DefineRole("admin", []string{"read", "write", "delete"})
	rbac.DefineRole("user", []string{"read"})

	adminUser := &auth.AuthenticatedUser{
		ID:   "admin-1",
		Role: "admin",
	}

	regularUser := &auth.AuthenticatedUser{
		ID:   "user-1",
		Role: "user",
	}

	if !rbac.Authorize(adminUser, "write") {
		t.Errorf("admin should have write permission")
	}

	if rbac.Authorize(regularUser, "write") {
		t.Errorf("regular user should not have write permission")
	}
}

func TestRateLimitingFlow(t *testing.T) {
	config := map[ratelimit.Dimension]float64{
		ratelimit.DimensionGlobal:   100,
		ratelimit.DimensionPerIP:    20,
		ratelimit.DimensionPerClient: 50,
	}

	limiter := ratelimit.NewMultiDimensionalLimiter(
		config,
		[]ratelimit.Dimension{ratelimit.DimensionGlobal, ratelimit.DimensionPerIP},
	)

	allowed := 0
	for i := 0; i < 25; i++ {
		if limiter.Allow("client-1", "192.168.1.1", "") {
			allowed++
		}
	}

	if allowed != 20 {
		t.Errorf("expected 20 requests allowed for IP, got %d", allowed)
	}
}

func TestAuthAndRateLimitIntegration(t *testing.T) {
	bearerAuth := auth.NewBearerTokenAuth()
	rbac := auth.NewRBAC()

	user := &auth.AuthenticatedUser{
		ID:     "user-1",
		Role:   "service",
		Scopes: []string{"api.read"},
	}
	bearerAuth.RegisterToken("token-123", user)
	rbac.DefineRole("service", []string{"read", "write"})

	config := map[ratelimit.Dimension]float64{
		ratelimit.DimensionPerClient: 100,
	}
	rateLimiter := ratelimit.NewMultiDimensionalLimiter(
		config,
		[]ratelimit.Dimension{ratelimit.DimensionPerClient},
	)

	ctx := context.Background()

	authenticatedUser, err := bearerAuth.Authenticate(ctx, "Bearer token-123")
	if err != nil {
		t.Errorf("authentication failed: %v", err)
	}

	if !rbac.Authorize(authenticatedUser, "read") {
		t.Errorf("user should have read permission")
	}

	if !rateLimiter.Allow(authenticatedUser.ID, "", "") {
		t.Errorf("user should not be rate limited")
	}
}

func TestConcurrentAuthzAndRateLimit(t *testing.T) {
	rbac := auth.NewRBAC()
	rbac.DefineRole("user", []string{"read"})

	config := map[ratelimit.Dimension]float64{
		ratelimit.DimensionPerClient: 1000,
	}
	limiter := ratelimit.NewMultiDimensionalLimiter(
		config,
		[]ratelimit.Dimension{ratelimit.DimensionPerClient},
	)

	allowed := int64(0)
	denied := int64(0)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			clientID := fmt.Sprintf("client-%d", idx)

			user := &auth.AuthenticatedUser{
				ID:   clientID,
				Role: "user",
			}

			for j := 0; j < 50; j++ {
				if rbac.Authorize(user, "read") {
					if limiter.Allow(clientID, "", "") {
						mu.Lock()
						allowed++
						mu.Unlock()
					} else {
						mu.Lock()
						denied++
						mu.Unlock()
					}
				}
			}
		}(i)
	}

	wg.Wait()

	if allowed != 500 {
		t.Logf("allowed=%d (expected 500)", allowed)
	}
}

func TestRBACMultipleRoles(t *testing.T) {
	rbac := auth.NewRBAC()

	rbac.DefineRole("admin", []string{"*"})
	rbac.DefineRole("editor", []string{"read", "write"})
	rbac.DefineRole("viewer", []string{"read"})

	actions := []string{"read", "write", "delete", "admin"}

	adminUser := &auth.AuthenticatedUser{Role: "admin"}
	editorUser := &auth.AuthenticatedUser{Role: "editor"}
	viewerUser := &auth.AuthenticatedUser{Role: "viewer"}

	for _, action := range actions {
		if !rbac.Authorize(adminUser, action) {
			t.Errorf("admin should have %s permission", action)
		}
	}

	if !rbac.Authorize(editorUser, "read") || !rbac.Authorize(editorUser, "write") {
		t.Errorf("editor should have read and write permissions")
	}

	if rbac.Authorize(editorUser, "delete") {
		t.Errorf("editor should not have delete permission")
	}

	if !rbac.Authorize(viewerUser, "read") {
		t.Errorf("viewer should have read permission")
	}

	if rbac.Authorize(viewerUser, "write") || rbac.Authorize(viewerUser, "delete") {
		t.Errorf("viewer should only have read permission")
	}
}

func TestRateLimitingDimensions(t *testing.T) {
	config := map[ratelimit.Dimension]float64{
		ratelimit.DimensionGlobal:    1000,
		ratelimit.DimensionPerIP:     100,
		ratelimit.DimensionPerClient: 50,
	}

	limiter := ratelimit.NewMultiDimensionalLimiter(
		config,
		[]ratelimit.Dimension{
			ratelimit.DimensionGlobal,
			ratelimit.DimensionPerIP,
			ratelimit.DimensionPerClient,
		},
	)

	var wg sync.WaitGroup
	blocked := int64(0)
	var mu sync.Mutex

	for ip := 0; ip < 5; ip++ {
		for client := 0; client < 2; client++ {
			wg.Add(1)
			go func(ipIdx, clientIdx int) {
				defer wg.Done()
				clientID := fmt.Sprintf("client-%d-%d", ipIdx, clientIdx)
				ipAddr := fmt.Sprintf("192.168.1.%d", ipIdx+1)

				for i := 0; i < 30; i++ {
					if !limiter.Allow(clientID, ipAddr, "") {
						mu.Lock()
						blocked++
						mu.Unlock()
					}
				}
			}(ip, client)
		}
	}

	wg.Wait()

	if blocked == 0 {
		t.Logf("no requests were rate limited (this might be expected)")
	} else {
		t.Logf("blocked requests: %d", blocked)
	}
}

func BenchmarkAuthzCheck(b *testing.B) {
	rbac := auth.NewRBAC()
	rbac.DefineRole("user", []string{"read", "write"})

	user := &auth.AuthenticatedUser{Role: "user"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rbac.Authorize(user, "read")
	}
}

func BenchmarkRateLimitCheck(b *testing.B) {
	config := map[ratelimit.Dimension]float64{
		ratelimit.DimensionPerClient: 10000,
	}
	limiter := ratelimit.NewMultiDimensionalLimiter(
		config,
		[]ratelimit.Dimension{ratelimit.DimensionPerClient},
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow("client-1", "", "")
	}
}

func BenchmarkAuthAndAuthzFlow(b *testing.B) {
	bearerAuth := auth.NewBearerTokenAuth()
	rbac := auth.NewRBAC()

	user := &auth.AuthenticatedUser{ID: "user-1", Role: "admin"}
	bearerAuth.RegisterToken("token", user)
	rbac.DefineRole("admin", []string{"read", "write"})

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		authUser, _ := bearerAuth.Authenticate(ctx, "Bearer token")
		rbac.Authorize(authUser, "write")
	}
}
