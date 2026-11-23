package graph

import (
	"context"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/auth"
)

const roleAdmin = "ADMIN"

type claimsKey struct{}

// WithClaims returns a context carrying the authenticated caller.
func WithClaims(ctx context.Context, claims *auth.Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

// ClaimsFromContext returns the authenticated caller, if any.
func ClaimsFromContext(ctx context.Context) (*auth.Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(*auth.Claims)
	return claims, ok && claims != nil && claims.UserID != ""
}

func requireUser(ctx context.Context) (*auth.Claims, error) {
	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		return nil, errUnauthenticated
	}
	return claims, nil
}

func requireAdmin(ctx context.Context) (*auth.Claims, error) {
	claims, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	if claims.Role != roleAdmin {
		return nil, errForbidden
	}
	return claims, nil
}

func isAdmin(claims *auth.Claims) bool {
	return claims.Role == roleAdmin
}
