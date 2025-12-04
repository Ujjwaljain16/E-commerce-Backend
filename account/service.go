package account

import (
	"context"
	"errors"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	jwt.RegisteredClaims
}

// Service implements the AccountService gRPC interface
type Service struct {
	pb.UnimplementedAccountServiceServer
	repo      Repository
	jwtSecret []byte
}

// NewService creates a new account service
func NewService(repo Repository, jwtSecret string) *Service {
	return &Service{
		repo:      repo,
		jwtSecret: []byte(jwtSecret),
	}
}

// generateTokens generates access and refresh JWT tokens
func (s *Service) generateTokens(userID, email string) (string, string, error) {
	// Access token (15 minutes)
	accessClaims := &Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString(s.jwtSecret)
	if err != nil {
		return "", "", err
	}

	// Refresh token (7 days)
	refreshClaims := &Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString(s.jwtSecret)
	if err != nil {
		return "", "", err
	}

	return accessTokenString, refreshTokenString, nil
}

// parseToken parses and validates a JWT token
func (s *Service) parseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	})

	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	if claims.ExpiresAt.Before(time.Now()) {
		return nil, ErrTokenExpired
	}

	return claims, nil
}

// Register creates a new user account
func (s *Service) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	// Validate input
	if req.Email == "" || req.Password == "" || req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "email, password, and name are required")
	}

	// Create account
	account, err := s.repo.Create(ctx, req.Email, req.Password, req.Name, req.Phone)
	if err != nil {
		if errors.Is(err, ErrEmailAlreadyExists) {
			return nil, status.Error(codes.AlreadyExists, "email already exists")
		}
		return nil, status.Error(codes.Internal, "failed to create account")
	}

	// Generate tokens
	accessToken, refreshToken, err := s.generateTokens(account.ID, account.Email)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate tokens")
	}

	return &pb.RegisterResponse{
		User: &pb.User{
			Id:         account.ID,
			Email:      account.Email,
			Name:       account.Name,
			Phone:      account.Phone,
			CreatedAt:  timestamppb.New(account.CreatedAt),
			UpdatedAt:  timestamppb.New(account.UpdatedAt),
			IsVerified: account.IsVerified,
			IsActive:   account.IsActive,
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

// Login authenticates a user and returns tokens
func (s *Service) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	if req.Email == "" || req.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "email and password are required")
	}

	// Verify credentials
	account, err := s.repo.VerifyPassword(ctx, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			return nil, status.Error(codes.Unauthenticated, "invalid credentials")
		}
		return nil, status.Error(codes.Internal, "failed to verify credentials")
	}

	// Generate tokens
	accessToken, refreshToken, err := s.generateTokens(account.ID, account.Email)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate tokens")
	}

	return &pb.LoginResponse{
		User: &pb.User{
			Id:         account.ID,
			Email:      account.Email,
			Name:       account.Name,
			Phone:      account.Phone,
			CreatedAt:  timestamppb.New(account.CreatedAt),
			UpdatedAt:  timestamppb.New(account.UpdatedAt),
			IsVerified: account.IsVerified,
			IsActive:   account.IsActive,
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

// GetProfile retrieves user profile
func (s *Service) GetProfile(ctx context.Context, req *pb.GetProfileRequest) (*pb.GetProfileResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	account, err := s.repo.GetByID(ctx, req.UserId)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return nil, status.Error(codes.NotFound, "account not found")
		}
		return nil, status.Error(codes.Internal, "failed to get account")
	}

	return &pb.GetProfileResponse{
		User: &pb.User{
			Id:         account.ID,
			Email:      account.Email,
			Name:       account.Name,
			Phone:      account.Phone,
			CreatedAt:  timestamppb.New(account.CreatedAt),
			UpdatedAt:  timestamppb.New(account.UpdatedAt),
			IsVerified: account.IsVerified,
			IsActive:   account.IsActive,
		},
	}, nil
}

// UpdateProfile updates user profile information
func (s *Service) UpdateProfile(ctx context.Context, req *pb.UpdateProfileRequest) (*pb.UpdateProfileResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	account, err := s.repo.Update(ctx, req.UserId, req.Name, req.Phone)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return nil, status.Error(codes.NotFound, "account not found")
		}
		return nil, status.Error(codes.Internal, "failed to update account")
	}

	return &pb.UpdateProfileResponse{
		User: &pb.User{
			Id:         account.ID,
			Email:      account.Email,
			Name:       account.Name,
			Phone:      account.Phone,
			CreatedAt:  timestamppb.New(account.CreatedAt),
			UpdatedAt:  timestamppb.New(account.UpdatedAt),
			IsVerified: account.IsVerified,
			IsActive:   account.IsActive,
		},
	}, nil
}

// ChangePassword changes user password
func (s *Service) ChangePassword(ctx context.Context, req *pb.ChangePasswordRequest) (*pb.ChangePasswordResponse, error) {
	if req.UserId == "" || req.OldPassword == "" || req.NewPassword == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id, old_password, and new_password are required")
	}

	// Get account
	account, err := s.repo.GetByID(ctx, req.UserId)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return nil, status.Error(codes.NotFound, "account not found")
		}
		return nil, status.Error(codes.Internal, "failed to get account")
	}

	// Verify old password
	err = bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(req.OldPassword))
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid old password")
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to hash password")
	}

	// Update password
	err = s.repo.UpdatePassword(ctx, req.UserId, string(hashedPassword))
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to update password")
	}

	return &pb.ChangePasswordResponse{
		Success: true,
		Message: "password changed successfully",
	}, nil
}

// DeleteAccount soft-deletes a user account
func (s *Service) DeleteAccount(ctx context.Context, req *pb.DeleteAccountRequest) (*pb.DeleteAccountResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	err := s.repo.Delete(ctx, req.UserId)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return nil, status.Error(codes.NotFound, "account not found")
		}
		return nil, status.Error(codes.Internal, "failed to delete account")
	}

	return &pb.DeleteAccountResponse{
		Success: true,
		Message: "account deleted successfully",
	}, nil
}

// VerifyToken validates a JWT token
func (s *Service) VerifyToken(ctx context.Context, req *pb.VerifyTokenRequest) (*pb.VerifyTokenResponse, error) {
	if req.Token == "" {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}

	claims, err := s.parseToken(req.Token)
	if err != nil {
		return &pb.VerifyTokenResponse{
			Valid: false,
		}, nil
	}

	return &pb.VerifyTokenResponse{
		Valid:     true,
		UserId:    claims.UserID,
		ExpiresAt: timestamppb.New(claims.ExpiresAt.Time),
	}, nil
}

// RefreshToken generates new tokens from refresh token
func (s *Service) RefreshToken(ctx context.Context, req *pb.RefreshTokenRequest) (*pb.RefreshTokenResponse, error) {
	if req.RefreshToken == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}

	claims, err := s.parseToken(req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrTokenExpired) {
			return nil, status.Error(codes.Unauthenticated, "refresh token expired")
		}
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}

	// Generate new tokens
	accessToken, refreshToken, err := s.generateTokens(claims.UserID, claims.Email)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate tokens")
	}

	return &pb.RefreshTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}
