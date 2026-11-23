package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	// ErrInvalidToken is returned when JWT token is invalid
	ErrInvalidToken = errors.New("invalid token")
	// ErrTokenExpired is returned when JWT token is expired
	ErrTokenExpired = errors.New("token expired")
	// ErrWrongTokenType is returned when a refresh token is used as an access token or vice versa
	ErrWrongTokenType = errors.New("wrong token type")
)

// Token types stored in the "typ" claim.
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// Claims represents JWT token claims
type Claims struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Role      string `json:"role,omitempty"`
	TokenType string `json:"typ,omitempty"`
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
	return ts.sign(userID, email, role, TokenTypeAccess, ts.accessTokenDuration)
}

// GenerateRefreshToken generates a JWT refresh token
func (ts *TokenService) GenerateRefreshToken(userID, email, role string) (string, error) {
	return ts.sign(userID, email, role, TokenTypeRefresh, ts.refreshTokenDuration)
}

func (ts *TokenService) sign(userID, email, role, tokenType string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:    userID,
		Email:     email,
		Role:      role,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(ts.secret)
}

// ValidateAccessToken validates a token and requires it to be an access token.
func (ts *TokenService) ValidateAccessToken(tokenString string) (*Claims, error) {
	return ts.validateType(tokenString, TokenTypeAccess)
}

// ValidateRefreshToken validates a token and requires it to be a refresh token.
func (ts *TokenService) ValidateRefreshToken(tokenString string) (*Claims, error) {
	return ts.validateType(tokenString, TokenTypeRefresh)
}

func (ts *TokenService) validateType(tokenString, want string) (*Claims, error) {
	claims, err := ts.ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != want {
		return nil, ErrWrongTokenType
	}
	return claims, nil
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
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
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
