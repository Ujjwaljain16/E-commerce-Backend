package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenService_GenerateAccessToken(t *testing.T) {
	ts := NewTokenService("test-secret", 15*time.Minute, 7*24*time.Hour)

	token, err := ts.GenerateAccessToken("user123", "test@example.com", "USER")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if token == "" {
		t.Fatal("expected token, got empty string")
	}

	// Validate the generated token
	claims, err := ts.ValidateToken(token)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}

	if claims.UserID != "user123" {
		t.Errorf("expected UserID 'user123', got '%s'", claims.UserID)
	}

	if claims.Email != "test@example.com" {
		t.Errorf("expected Email 'test@example.com', got '%s'", claims.Email)
	}

	if claims.Role != "USER" {
		t.Errorf("expected Role 'USER', got '%s'", claims.Role)
	}
}

func TestTokenService_GenerateRefreshToken(t *testing.T) {
	ts := NewTokenService("test-secret", 15*time.Minute, 7*24*time.Hour)

	token, err := ts.GenerateRefreshToken("user123", "test@example.com", "ADMIN")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if token == "" {
		t.Fatal("expected token, got empty string")
	}

	// Validate the generated token
	claims, err := ts.ValidateToken(token)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}

	if claims.Role != "ADMIN" {
		t.Errorf("expected Role 'ADMIN', got '%s'", claims.Role)
	}

	// Verify refresh token has longer expiration
	if claims.ExpiresAt.Sub(claims.IssuedAt.Time) < 6*24*time.Hour {
		t.Error("refresh token expiration too short")
	}
}

func TestTokenService_GenerateTokenPair(t *testing.T) {
	ts := NewTokenService("test-secret", 15*time.Minute, 7*24*time.Hour)

	accessToken, refreshToken, err := ts.GenerateTokenPair("user123", "test@example.com", "USER")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if accessToken == "" || refreshToken == "" {
		t.Fatal("expected both tokens, got empty strings")
	}

	// Both should be valid
	_, err = ts.ValidateToken(accessToken)
	if err != nil {
		t.Errorf("access token invalid: %v", err)
	}

	_, err = ts.ValidateToken(refreshToken)
	if err != nil {
		t.Errorf("refresh token invalid: %v", err)
	}
}

func TestTokenService_ValidateToken_Invalid(t *testing.T) {
	ts := NewTokenService("test-secret", 15*time.Minute, 7*24*time.Hour)

	tests := []struct {
		name  string
		token string
	}{
		{"empty token", ""},
		{"invalid format", "invalid.token.string"},
		{"malformed", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ts.ValidateToken(tt.token)
			if err != ErrInvalidToken {
				t.Errorf("expected ErrInvalidToken, got %v", err)
			}
		})
	}
}

func TestTokenService_ValidateToken_WrongSecret(t *testing.T) {
	ts1 := NewTokenService("secret1", 15*time.Minute, 7*24*time.Hour)
	ts2 := NewTokenService("secret2", 15*time.Minute, 7*24*time.Hour)

	token, err := ts1.GenerateAccessToken("user123", "test@example.com", "USER")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Try to validate with different secret
	_, err = ts2.ValidateToken(token)
	if err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken when using wrong secret, got %v", err)
	}
}

func TestTokenService_ValidateToken_Expired(t *testing.T) {
	// Create service with very short expiration
	ts := NewTokenService("test-secret", 1*time.Millisecond, 1*time.Millisecond)

	token, err := ts.GenerateAccessToken("user123", "test@example.com", "USER")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Wait for token to expire
	time.Sleep(10 * time.Millisecond)

	_, err = ts.ValidateToken(token)
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestTokenService_GetClaimsFromToken(t *testing.T) {
	ts := NewTokenService("test-secret", 1*time.Millisecond, 1*time.Millisecond)

	token, err := ts.GenerateAccessToken("user123", "test@example.com", "USER")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Should fail normal validation
	_, err = ts.ValidateToken(token)
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}

	// But should succeed with GetClaimsFromToken
	claims, err := ts.GetClaimsFromToken(token)
	if err != nil {
		t.Fatalf("expected to get claims from expired token, got error: %v", err)
	}

	if claims.UserID != "user123" {
		t.Errorf("expected UserID 'user123', got '%s'", claims.UserID)
	}
}

func TestTokenService_DifferentDurations(t *testing.T) {
	accessDuration := 30 * time.Minute
	refreshDuration := 14 * 24 * time.Hour

	ts := NewTokenService("test-secret", accessDuration, refreshDuration)

	accessToken, refreshToken, err := ts.GenerateTokenPair("user123", "test@example.com", "USER")
	if err != nil {
		t.Fatalf("failed to generate tokens: %v", err)
	}

	accessClaims, _ := ts.ValidateToken(accessToken)
	refreshClaims, _ := ts.ValidateToken(refreshToken)

	// Check access token duration (allow some tolerance)
	accessDiff := accessClaims.ExpiresAt.Sub(accessClaims.IssuedAt.Time)
	if accessDiff < 29*time.Minute || accessDiff > 31*time.Minute {
		t.Errorf("access token duration incorrect: %v", accessDiff)
	}

	// Check refresh token duration
	refreshDiff := refreshClaims.ExpiresAt.Sub(refreshClaims.IssuedAt.Time)
	if refreshDiff < 13*24*time.Hour || refreshDiff > 15*24*time.Hour {
		t.Errorf("refresh token duration incorrect: %v", refreshDiff)
	}
}

func TestTokenService_RoleInClaims(t *testing.T) {
	ts := NewTokenService("test-secret", 15*time.Minute, 7*24*time.Hour)

	roles := []string{"USER", "ADMIN", "GUEST", ""}

	for _, role := range roles {
		token, err := ts.GenerateAccessToken("user123", "test@example.com", role)
		if err != nil {
			t.Fatalf("failed to generate token with role '%s': %v", role, err)
		}

		claims, err := ts.ValidateToken(token)
		if err != nil {
			t.Fatalf("failed to validate token: %v", err)
		}

		if claims.Role != role {
			t.Errorf("expected role '%s', got '%s'", role, claims.Role)
		}
	}
}

func TestTokenService_SigningMethodValidation(t *testing.T) {
	ts := NewTokenService("test-secret", 15*time.Minute, 7*24*time.Hour)

	// Create a token with a different signing method (RS256 instead of HS256)
	claims := &Claims{
		UserID: "user123",
		Email:  "test@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	// Try to create token with wrong method (this will fail at validation)
	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	tokenString, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

	// Should fail validation
	_, err := ts.ValidateToken(tokenString)
	if err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken for wrong signing method, got %v", err)
	}
}
