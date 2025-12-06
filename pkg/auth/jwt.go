package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// ErrInvalidToken is returned when JWT token is invalid
	ErrInvalidToken = errors.New("invalid token")
	// ErrTokenExpired is returned when JWT token is expired
	ErrTokenExpired = errors.New("token expired")
)

// Claims represents JWT token claims
type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role,omitempty"` // For future RBAC
	jwt.RegisteredClaims
}

// TokenService handles JWT token generation and validation
type TokenService struct {
	secret               []byte
	accessTokenDuration  time.Duration
	refreshTokenDuration time.Duration
}

// NewTokenService creates a new JWT token service
func NewTokenService(secret string, accessDuration, refreshDuration time.Duration) *TokenService {
	return &TokenService{
		secret:               []byte(secret),
		accessTokenDuration:  accessDuration,
		refreshTokenDuration: refreshDuration,
	}
}

// GenerateAccessToken generates a JWT access token
func (ts *TokenService) GenerateAccessToken(userID, email, role string) (string, error) {
	claims := &Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ts.accessTokenDuration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(ts.secret)
}

// GenerateRefreshToken generates a JWT refresh token
func (ts *TokenService) GenerateRefreshToken(userID, email, role string) (string, error) {
	claims := &Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ts.refreshTokenDuration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(ts.secret)
}

// GenerateTokenPair generates both access and refresh tokens
func (ts *TokenService) GenerateTokenPair(userID, email, role string) (accessToken, refreshToken string, err error) {
	accessToken, err = ts.GenerateAccessToken(userID, email, role)
	if err != nil {
		return "", "", err
	}

	refreshToken, err = ts.GenerateRefreshToken(userID, email, role)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

// ValidateToken parses and validates a JWT token
func (ts *TokenService) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return ts.secret, nil
	})

	if err != nil {
		// Check if it's an expiration error
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// GetClaimsFromToken extracts claims without full validation (useful for expired token info)
func (ts *TokenService) GetClaimsFromToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return ts.secret, nil
	}, jwt.WithoutClaimsValidation())

	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidToken
	}

	return claims, nil
}
