package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type AuthMethod string

const (
	Bearer  AuthMethod = "bearer"
	JWT     AuthMethod = "jwt"
	APIKey  AuthMethod = "apikey"
	MTLS    AuthMethod = "mtls"
)

type AuthenticatedUser struct {
	ID    string
	Name  string
	Role  string
	Scopes []string
}

type BearerTokenAuth struct {
	validTokens map[string]*AuthenticatedUser
}

func NewBearerTokenAuth() *BearerTokenAuth {
	return &BearerTokenAuth{
		validTokens: make(map[string]*AuthenticatedUser),
	}
}

func (b *BearerTokenAuth) RegisterToken(token string, user *AuthenticatedUser) {
	b.validTokens[token] = user
}

func (b *BearerTokenAuth) Authenticate(ctx context.Context, authHeader string) (*AuthenticatedUser, error) {
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, fmt.Errorf("invalid bearer token format")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	user, ok := b.validTokens[token]
	if !ok {
		return nil, fmt.Errorf("invalid or expired token")
	}

	return user, nil
}

type JWTClaims struct {
	UserID string   `json:"user_id"`
	Name   string   `json:"name"`
	Role   string   `json:"role"`
	Scopes []string `json:"scopes"`
	Exp    int64    `json:"exp"`
}

type JWTAuth struct {
	publicKey string
}

func NewJWTAuth(publicKeyPEM string) *JWTAuth {
	return &JWTAuth{
		publicKey: publicKeyPEM,
	}
}

func (j *JWTAuth) Authenticate(ctx context.Context, tokenString string) (*AuthenticatedUser, error) {
	if tokenString == "" {
		return nil, fmt.Errorf("empty token")
	}

	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	var claims JWTClaims
	if err := json.Unmarshal([]byte(parts[1]), &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	if time.Unix(claims.Exp, 0).Before(time.Now()) {
		return nil, fmt.Errorf("token expired")
	}

	return &AuthenticatedUser{
		ID:     claims.UserID,
		Name:   claims.Name,
		Role:   claims.Role,
		Scopes: claims.Scopes,
	}, nil
}

type APIKeyAuth struct {
	validKeys map[string]*AuthenticatedUser
}

func NewAPIKeyAuth() *APIKeyAuth {
	return &APIKeyAuth{
		validKeys: make(map[string]*AuthenticatedUser),
	}
}

func (a *APIKeyAuth) RegisterKey(key string, user *AuthenticatedUser) {
	a.validKeys[key] = user
}

func (a *APIKeyAuth) Authenticate(ctx context.Context, apiKey string) (*AuthenticatedUser, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("empty API key")
	}

	user, ok := a.validKeys[apiKey]
	if !ok {
		return nil, fmt.Errorf("invalid API key")
	}

	return user, nil
}

type MTLSAuth struct {
	clientCAs *x509.CertPool
}

func NewMTLSAuth(clientCACert string) (*MTLSAuth, error) {
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM([]byte(clientCACert)) {
		return nil, fmt.Errorf("failed to parse client CA certificate")
	}

	return &MTLSAuth{
		clientCAs: certPool,
	}, nil
}

func (m *MTLSAuth) Authenticate(ctx context.Context, clientCert *tls.Certificate) (*AuthenticatedUser, error) {
	if clientCert == nil {
		return nil, fmt.Errorf("client certificate required")
	}

	if len(clientCert.Certificate) == 0 {
		return nil, fmt.Errorf("client certificate empty")
	}

	subject := "client"
	if clientCert.Leaf != nil {
		subject = clientCert.Leaf.Subject.String()
	}

	return &AuthenticatedUser{
		ID:   subject,
		Name: subject,
		Role: "default",
	}, nil
}
