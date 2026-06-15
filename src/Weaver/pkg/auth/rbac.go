package auth

import (
	"fmt"
	"sync"
)

type RBAC struct {
	mu    sync.RWMutex
	roles map[string][]string
}

func NewRBAC() *RBAC {
	return &RBAC{
		roles: make(map[string][]string),
	}
}

func (r *RBAC) DefineRole(role string, permissions []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roles[role] = permissions
}

func (r *RBAC) Authorize(user *AuthenticatedUser, action string) bool {
	if user == nil {
		return false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	permissions, ok := r.roles[user.Role]
	if !ok {
		return false
	}

	for _, perm := range permissions {
		if perm == action || perm == "*" {
			return true
		}
	}

	return false
}

func (r *RBAC) AuthorizeScope(user *AuthenticatedUser, scope string) bool {
	if user == nil {
		return false
	}

	for _, userScope := range user.Scopes {
		if userScope == scope || userScope == "*" {
			return true
		}
	}

	return false
}

func (r *RBAC) GetPermissions(role string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	permissions, ok := r.roles[role]
	if !ok {
		return nil, fmt.Errorf("role %s not found", role)
	}

	result := make([]string, len(permissions))
	copy(result, permissions)
	return result, nil
}
