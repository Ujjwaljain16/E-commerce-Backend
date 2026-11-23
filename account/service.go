package account

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/auth"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service implements the AccountService gRPC interface
type Service struct {
	pb.UnimplementedAccountServiceServer
	repo         Repository
	tokens       TokenStore
	tokenService *auth.TokenService
}

// NewService creates a new account service
func NewService(repo Repository, tokens TokenStore, jwtSecret string) *Service {
	return &Service{
		repo:         repo,
		tokens:       tokens,
		tokenService: auth.NewTokenService(jwtSecret, 15*time.Minute, 7*24*time.Hour),
	}
}

// issueTokens signs a token pair and records the refresh token so it can be rotated and revoked.
func (s *Service) issueTokens(ctx context.Context, a *Account) (string, string, error) {
	access, refresh, err := s.tokenService.GenerateTokenPair(a.ID, a.Email, a.Role)
	if err != nil {
		return "", "", err
	}
	claims, err := s.tokenService.ValidateRefreshToken(refresh)
	if err != nil {
		return "", "", err
	}
	if err := s.tokens.Save(ctx, claims.ID, a.ID, claims.ExpiresAt.Time); err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

// Register creates a new user account
func (s *Service) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	// Validate input
	if req.Email == "" || req.Password == "" || req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "email, password, and name are required")
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "email is not valid")
	}
	if err := validatePassword(req.Password); err != nil {
		return nil, err
	}

	// Self-registration always gets the USER role; admins come from EnsureAdmin.
	account, err := s.repo.Create(ctx, email, req.Password, req.Name, req.Phone, "USER")
	if err != nil {
		if errors.Is(err, ErrEmailAlreadyExists) {
			return nil, status.Error(codes.AlreadyExists, "email already exists")
		}
		return nil, status.Error(codes.Internal, "failed to create account")
	}

	accessToken, refreshToken, err := s.issueTokens(ctx, account)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate tokens")
	}

	return &pb.RegisterResponse{
		User: &pb.User{
			Id:         account.ID,
			Email:      account.Email,
			Name:       account.Name,
			Phone:      account.Phone,
			Role:       account.Role,
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
	account, err := s.repo.VerifyPassword(ctx, strings.ToLower(strings.TrimSpace(req.Email)), req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			return nil, status.Error(codes.Unauthenticated, "invalid credentials")
		}
		return nil, status.Error(codes.Internal, "failed to verify credentials")
	}

	accessToken, refreshToken, err := s.issueTokens(ctx, account)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate tokens")
	}

	return &pb.LoginResponse{
		User: &pb.User{
			Id:         account.ID,
			Email:      account.Email,
			Name:       account.Name,
			Phone:      account.Phone,
			Role:       account.Role,
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
			Role:       account.Role,
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
			Role:       account.Role,
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

	if err := validatePassword(req.NewPassword); err != nil {
		return nil, err
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
	// A password change should log out every other session.
	if err := s.tokens.RevokeAll(ctx, req.UserId); err != nil {
		return nil, status.Error(codes.Internal, "password changed but sessions could not be revoked")
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
	if err := s.tokens.RevokeAll(ctx, req.UserId); err != nil {
		return nil, status.Error(codes.Internal, "account deleted but sessions could not be revoked")
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

	claims, err := s.tokenService.ValidateAccessToken(req.Token)
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

	claims, err := s.tokenService.ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		if errors.Is(err, auth.ErrTokenExpired) {
			return nil, status.Error(codes.Unauthenticated, "refresh token expired")
		}
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}

	// Refresh tokens are single use: each refresh rotates to a new one.
	owner, err := s.tokens.Consume(ctx, claims.ID)
	switch {
	case errors.Is(err, ErrRefreshTokenReused):
		return nil, status.Error(codes.Unauthenticated, "refresh token already used; all sessions were signed out")
	case errors.Is(err, ErrRefreshTokenUnknown):
		return nil, status.Error(codes.Unauthenticated, "refresh token revoked")
	case err != nil:
		return nil, status.Error(codes.Internal, "failed to check refresh token")
	case owner != claims.UserID:
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}

	// Re-read the account so deactivation and role changes take effect at the next refresh.
	account, err := s.repo.GetByID(ctx, claims.UserID)
	if errors.Is(err, ErrAccountNotFound) {
		return nil, status.Error(codes.Unauthenticated, "account no longer active")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to load account")
	}

	accessToken, refreshToken, err := s.issueTokens(ctx, account)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate tokens")
	}

	return &pb.RefreshTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

// Logout revokes a refresh token. Access tokens stay valid until they expire (15 minutes).
func (s *Service) Logout(ctx context.Context, req *pb.LogoutRequest) (*pb.LogoutResponse, error) {
	if req.RefreshToken == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}
	claims, err := s.tokenService.ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		// Expired or invalid tokens can't be used anyway; logging out is still a success.
		return &pb.LogoutResponse{Success: true}, nil
	}
	if err := s.tokens.Revoke(ctx, claims.ID); err != nil {
		return nil, status.Error(codes.Internal, "failed to log out")
	}
	return &pb.LogoutResponse{Success: true}, nil
}

// EnsureAdmin creates the bootstrap admin account if it does not exist yet. Registration never
// grants ADMIN, so this is the only way to get the first admin in a fresh environment.
func (s *Service) EnsureAdmin(ctx context.Context, email, password string) error {
	normalized, err := normalizeEmail(email)
	if err != nil {
		return errors.New("admin email is not valid")
	}
	if err := validatePassword(password); err != nil {
		return errors.New("admin password must be at least 8 characters")
	}
	_, err = s.repo.Create(ctx, normalized, password, "Administrator", "", "ADMIN")
	if errors.Is(err, ErrEmailAlreadyExists) {
		return nil
	}
	return err
}

func normalizeEmail(raw string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || addr.Name != "" {
		return "", errors.New("invalid email")
	}
	return strings.ToLower(addr.Address), nil
}

func validatePassword(p string) error {
	if len(p) < 8 || len(p) > 72 {
		// bcrypt ignores everything after 72 bytes, so longer passwords would silently be truncated.
		return status.Error(codes.InvalidArgument, "password must be between 8 and 72 characters")
	}
	return nil
}
