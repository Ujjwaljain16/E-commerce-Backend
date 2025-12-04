package account

import (
	"context"
	"errors"
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
	repo        Repository
	tokenService *auth.TokenService
}

// NewService creates a new account service
func NewService(repo Repository, jwtSecret string) *Service {
	return &Service{
		repo:        repo,
		tokenService: auth.NewTokenService(jwtSecret, 15*time.Minute, 7*24*time.Hour),
	}
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

	// Generate tokens using auth package
	accessToken, refreshToken, err := s.tokenService.GenerateTokenPair(account.ID, account.Email, "USER")
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

	// Generate tokens using auth package
	accessToken, refreshToken, err := s.tokenService.GenerateTokenPair(account.ID, account.Email, "USER")
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

	claims, err := s.tokenService.ValidateToken(req.Token)
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

	claims, err := s.tokenService.ValidateToken(req.RefreshToken)
	if err != nil {
		if errors.Is(err, auth.ErrTokenExpired) {
			return nil, status.Error(codes.Unauthenticated, "refresh token expired")
		}
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}

	// Generate new tokens using auth package
	accessToken, refreshToken, err := s.tokenService.GenerateTokenPair(claims.UserID, claims.Email, claims.Role)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate tokens")
	}

	return &pb.RefreshTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}
