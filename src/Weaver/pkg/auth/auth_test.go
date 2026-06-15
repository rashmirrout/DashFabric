package auth

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBearerTokenAuth(t *testing.T) {
	auth := NewBearerTokenAuth()
	user := &AuthenticatedUser{
		ID:    "user-1",
		Name:  "Alice",
		Role:  "admin",
		Scopes: []string{"read", "write"},
	}
	auth.RegisterToken("valid-token", user)

	tests := []struct {
		name        string
		authHeader  string
		shouldError bool
		expectedID  string
	}{
		{"valid token", "Bearer valid-token", false, "user-1"},
		{"invalid token", "Bearer invalid-token", true, ""},
		{"no bearer prefix", "token", true, ""},
		{"empty bearer", "Bearer ", true, ""},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := auth.Authenticate(ctx, tt.authHeader)
			if (err != nil) != tt.shouldError {
				t.Errorf("expected error %v, got %v", tt.shouldError, err != nil)
			}
			if !tt.shouldError && result.ID != tt.expectedID {
				t.Errorf("expected ID %s, got %s", tt.expectedID, result.ID)
			}
		})
	}
}

func TestJWTAuth(t *testing.T) {
	auth := NewJWTAuth("test-public-key")

	tests := []struct {
		name        string
		token       string
		shouldError bool
	}{
		{"empty token", "", true},
		{"invalid format", "invalid", true},
		{"wrong parts", "a.b", true},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.Authenticate(ctx, tt.token)
			if (err != nil) != tt.shouldError {
				t.Errorf("expected error %v, got %v", tt.shouldError, err != nil)
			}
		})
	}
}

func TestAPIKeyAuth(t *testing.T) {
	auth := NewAPIKeyAuth()
	user := &AuthenticatedUser{
		ID:   "api-user-1",
		Name: "API User",
		Role: "service",
	}
	auth.RegisterKey("secret-key-123", user)

	tests := []struct {
		name        string
		apiKey      string
		shouldError bool
		expectedID  string
	}{
		{"valid key", "secret-key-123", false, "api-user-1"},
		{"invalid key", "wrong-key", true, ""},
		{"empty key", "", true, ""},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := auth.Authenticate(ctx, tt.apiKey)
			if (err != nil) != tt.shouldError {
				t.Errorf("expected error %v, got %v", tt.shouldError, err != nil)
			}
			if !tt.shouldError && result.ID != tt.expectedID {
				t.Errorf("expected ID %s, got %s", tt.expectedID, result.ID)
			}
		})
	}
}

func TestRBAC(t *testing.T) {
	rbac := NewRBAC()

	adminPerms := []string{"read", "write", "delete", "admin"}
	userPerms := []string{"read"}
	rbac.DefineRole("admin", adminPerms)
	rbac.DefineRole("user", userPerms)

	tests := []struct {
		name      string
		role      string
		action    string
		authorized bool
	}{
		{"admin read", "admin", "read", true},
		{"admin write", "admin", "write", true},
		{"admin delete", "admin", "delete", true},
		{"user read", "user", "read", true},
		{"user write", "user", "write", false},
		{"user delete", "user", "delete", false},
		{"unknown role", "unknown", "read", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := &AuthenticatedUser{
				ID:   "test-user",
				Role: tt.role,
			}
			authorized := rbac.Authorize(user, tt.action)
			if authorized != tt.authorized {
				t.Errorf("expected %v, got %v", tt.authorized, authorized)
			}
		})
	}
}

func TestRBACScopes(t *testing.T) {
	rbac := NewRBAC()

	user := &AuthenticatedUser{
		ID:     "user-1",
		Role:   "user",
		Scopes: []string{"read:orders", "write:profile"},
	}

	tests := []struct {
		name       string
		scope      string
		authorized bool
	}{
		{"allowed scope 1", "read:orders", true},
		{"allowed scope 2", "write:profile", true},
		{"disallowed scope", "delete:user", false},
		{"wildcard", "*", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authorized := rbac.AuthorizeScope(user, tt.scope)
			if authorized != tt.authorized {
				t.Errorf("expected %v, got %v", tt.authorized, authorized)
			}
		})
	}
}

func TestRBACGetPermissions(t *testing.T) {
	rbac := NewRBAC()
	adminPerms := []string{"*"}
	rbac.DefineRole("admin", adminPerms)

	perms, err := rbac.GetPermissions("admin")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(perms) != 1 || perms[0] != "*" {
		t.Errorf("expected ['*'], got %v", perms)
	}

	_, err = rbac.GetPermissions("nonexistent")
	if err == nil {
		t.Errorf("expected error for nonexistent role")
	}
}

func TestBearerTokenConcurrent(t *testing.T) {
	auth := NewBearerTokenAuth()

	for i := 0; i < 10; i++ {
		token := "token-" + string(rune(i+'0'))
		user := &AuthenticatedUser{
			ID:   "user-" + string(rune(i+'0')),
			Role: "user",
		}
		auth.RegisterToken(token, user)
	}

	ctx := context.Background()
	var completed int64
	const goroutines = 100
	const iterations = 100

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			for j := 0; j < iterations; j++ {
				token := "token-" + string(rune((idx%10)+'0'))
				auth.Authenticate(ctx, "Bearer "+token)
			}
			atomic.AddInt64(&completed, 1)
		}(i)
	}

	for {
		if atomic.LoadInt64(&completed) == goroutines {
			break
		}
		time.Sleep(time.Millisecond)
	}
}

func BenchmarkBearerTokenAuth(b *testing.B) {
	auth := NewBearerTokenAuth()
	user := &AuthenticatedUser{ID: "test", Role: "user"}
	auth.RegisterToken("token", user)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		auth.Authenticate(ctx, "Bearer token")
	}
}

func BenchmarkRBACAuthorize(b *testing.B) {
	rbac := NewRBAC()
	rbac.DefineRole("admin", []string{"read", "write", "delete"})
	user := &AuthenticatedUser{ID: "test", Role: "admin"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rbac.Authorize(user, "write")
	}
}
