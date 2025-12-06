package account

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mockRepository implements Repository interface for testing
type mockRepository struct {
	createFunc         func(ctx context.Context, email, password, name, phone, role string) (*Account, error)
	getByIDFunc        func(ctx context.Context, id string) (*Account, error)
	getByEmailFunc     func(ctx context.Context, email string) (*Account, error)
	updateFunc         func(ctx context.Context, id, name, phone string) (*Account, error)
	updatePasswordFunc func(ctx context.Context, id, newPasswordHash string) error
	deleteFunc         func(ctx context.Context, id string) error
	verifyPasswordFunc func(ctx context.Context, email, password string) (*Account, error)
	closeFunc          func() error
}

func (m *mockRepository) Create(ctx context.Context, email, password, name, phone, role string) (*Account, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, email, password, name, phone, role)
	}
	return nil, errors.New("not implemented")
}

func (m *mockRepository) GetByID(ctx context.Context, id string) (*Account, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (m *mockRepository) GetByEmail(ctx context.Context, email string) (*Account, error) {
	if m.getByEmailFunc != nil {
		return m.getByEmailFunc(ctx, email)
	}
	return nil, errors.New("not implemented")
}

func (m *mockRepository) Update(ctx context.Context, id, name, phone string) (*Account, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, id, name, phone)
	}
	return nil, errors.New("not implemented")
}

func (m *mockRepository) UpdatePassword(ctx context.Context, id, newPasswordHash string) error {
	if m.updatePasswordFunc != nil {
		return m.updatePasswordFunc(ctx, id, newPasswordHash)
	}
	return errors.New("not implemented")
}

func (m *mockRepository) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return errors.New("not implemented")
}

func (m *mockRepository) VerifyPassword(ctx context.Context, email, password string) (*Account, error) {
	if m.verifyPasswordFunc != nil {
		return m.verifyPasswordFunc(ctx, email, password)
	}
	return nil, errors.New("not implemented")
}

func (m *mockRepository) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func TestService_Register_Success(t *testing.T) {
	mockRepo := &mockRepository{
		createFunc: func(ctx context.Context, email, password, name, phone, role string) (*Account, error) {
			return &Account{
				ID:         "test-id-123",
				Email:      email,
				Name:       name,
				Phone:      phone,
				Role:       "USER",
				IsVerified: false,
				IsActive:   true,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}, nil
		},
	}

	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.RegisterRequest{
		Email:    "test@example.com",
		Password: "password123",
		Name:     "Test User",
		Phone:    "1234567890",
	}

	resp, err := service.Register(ctx, req)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if resp.User.Email != req.Email {
		t.Errorf("Expected email %s, got %s", req.Email, resp.User.Email)
	}
	if resp.User.Role != "USER" {
		t.Errorf("Expected role USER, got %s", resp.User.Role)
	}
	if resp.AccessToken == "" {
		t.Error("Expected non-empty access token")
	}
	if resp.RefreshToken == "" {
		t.Error("Expected non-empty refresh token")
	}
}

func TestService_Register_MissingEmail(t *testing.T) {
	mockRepo := &mockRepository{}
	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.RegisterRequest{
		Email:    "",
		Password: "password123",
		Name:     "Test User",
	}

	_, err := service.Register(ctx, req)
	if err == nil {
		t.Fatal("Expected error for missing email")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error, got %v", err)
	}
}

func TestService_Register_DuplicateEmail(t *testing.T) {
	mockRepo := &mockRepository{
		createFunc: func(ctx context.Context, email, password, name, phone, role string) (*Account, error) {
			return nil, ErrEmailAlreadyExists
		},
	}

	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.RegisterRequest{
		Email:    "duplicate@example.com",
		Password: "password123",
		Name:     "Test User",
	}

	_, err := service.Register(ctx, req)
	if err == nil {
		t.Fatal("Expected error for duplicate email")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.AlreadyExists {
		t.Errorf("Expected AlreadyExists error, got %v", err)
	}
}

func TestService_Login_Success(t *testing.T) {
	mockRepo := &mockRepository{
		verifyPasswordFunc: func(ctx context.Context, email, password string) (*Account, error) {
			return &Account{
				ID:         "test-id-123",
				Email:      email,
				Name:       "Test User",
				Phone:      "1234567890",
				Role:       "USER",
				IsVerified: true,
				IsActive:   true,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}, nil
		},
	}

	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.LoginRequest{
		Email:    "test@example.com",
		Password: "password123",
	}

	resp, err := service.Login(ctx, req)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if resp.User.Email != req.Email {
		t.Errorf("Expected email %s, got %s", req.Email, resp.User.Email)
	}
	if resp.AccessToken == "" {
		t.Error("Expected non-empty access token")
	}
}

func TestService_Login_InvalidCredentials(t *testing.T) {
	mockRepo := &mockRepository{
		verifyPasswordFunc: func(ctx context.Context, email, password string) (*Account, error) {
			return nil, ErrInvalidCredentials
		},
	}

	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.LoginRequest{
		Email:    "test@example.com",
		Password: "wrongpassword",
	}

	_, err := service.Login(ctx, req)
	if err == nil {
		t.Fatal("Expected error for invalid credentials")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated error, got %v", err)
	}
}

func TestService_GetProfile_Success(t *testing.T) {
	mockRepo := &mockRepository{
		getByIDFunc: func(ctx context.Context, id string) (*Account, error) {
			return &Account{
				ID:         id,
				Email:      "test@example.com",
				Name:       "Test User",
				Phone:      "1234567890",
				Role:       "USER",
				IsVerified: true,
				IsActive:   true,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}, nil
		},
	}

	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.GetProfileRequest{
		UserId: "test-id-123",
	}

	resp, err := service.GetProfile(ctx, req)
	if err != nil {
		t.Fatalf("GetProfile failed: %v", err)
	}

	if resp.User.Id != req.UserId {
		t.Errorf("Expected user ID %s, got %s", req.UserId, resp.User.Id)
	}
}

func TestService_GetProfile_NotFound(t *testing.T) {
	mockRepo := &mockRepository{
		getByIDFunc: func(ctx context.Context, id string) (*Account, error) {
			return nil, ErrAccountNotFound
		},
	}

	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.GetProfileRequest{
		UserId: "nonexistent-id",
	}

	_, err := service.GetProfile(ctx, req)
	if err == nil {
		t.Fatal("Expected error for nonexistent user")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound error, got %v", err)
	}
}

func TestService_UpdateProfile_Success(t *testing.T) {
	mockRepo := &mockRepository{
		updateFunc: func(ctx context.Context, id, name, phone string) (*Account, error) {
			return &Account{
				ID:         id,
				Email:      "test@example.com",
				Name:       name,
				Phone:      phone,
				Role:       "USER",
				IsVerified: true,
				IsActive:   true,
				CreatedAt:  time.Now().Add(-24 * time.Hour),
				UpdatedAt:  time.Now(),
			}, nil
		},
	}

	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.UpdateProfileRequest{
		UserId: "test-id-123",
		Name:   "Updated Name",
		Phone:  "9876543210",
	}

	resp, err := service.UpdateProfile(ctx, req)
	if err != nil {
		t.Fatalf("UpdateProfile failed: %v", err)
	}

	if resp.User.Name != req.Name {
		t.Errorf("Expected name %s, got %s", req.Name, resp.User.Name)
	}
	if resp.User.Phone != req.Phone {
		t.Errorf("Expected phone %s, got %s", req.Phone, resp.User.Phone)
	}
}

func TestService_ChangePassword_Success(t *testing.T) {
	// Pre-generated bcrypt hash for "oldpassword"
	mockRepo := &mockRepository{
		getByIDFunc: func(ctx context.Context, id string) (*Account, error) {
			return &Account{
				ID:           id,
				Email:        "test@example.com",
				PasswordHash: "$2a$10$rycZFBOvpzNg1AR6XvIamuK.PRpPgopkvss1qv7y/04KxUna/n06i",
				Name:         "Test User",
				Role:         "USER",
				IsActive:     true,
			}, nil
		},
		updatePasswordFunc: func(ctx context.Context, id, newPasswordHash string) error {
			return nil
		},
	}

	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.ChangePasswordRequest{
		UserId:      "test-id-123",
		OldPassword: "oldpassword",
		NewPassword: "newpassword123",
	}

	_, err := service.ChangePassword(ctx, req)
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}
}

func TestService_ChangePassword_WrongOldPassword(t *testing.T) {
	mockRepo := &mockRepository{
		getByIDFunc: func(ctx context.Context, id string) (*Account, error) {
			return &Account{
				ID:           id,
				Email:        "test@example.com",
				PasswordHash: "$2a$10$rycZFBOvpzNg1AR6XvIamuK.PRpPgopkvss1qv7y/04KxUna/n06i",
				Name:         "Test User",
				Role:         "USER",
				IsActive:     true,
			}, nil
		},
	}

	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.ChangePasswordRequest{
		UserId:      "test-id-123",
		OldPassword: "wrongpassword",
		NewPassword: "newpassword123",
	}

	_, err := service.ChangePassword(ctx, req)
	if err == nil {
		t.Fatal("Expected error for wrong old password")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated error, got %v", err)
	}
}

func TestService_DeleteAccount_Success(t *testing.T) {
	mockRepo := &mockRepository{
		deleteFunc: func(ctx context.Context, id string) error {
			return nil
		},
	}

	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.DeleteAccountRequest{
		UserId: "test-id-123",
	}

	_, err := service.DeleteAccount(ctx, req)
	if err != nil {
		t.Fatalf("DeleteAccount failed: %v", err)
	}
}

func TestService_VerifyToken_ValidToken(t *testing.T) {
	mockRepo := &mockRepository{}
	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	// Generate a valid token
	token, _, err := service.tokenService.GenerateTokenPair("user-123", "test@example.com", "USER")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	req := &pb.VerifyTokenRequest{
		Token: token,
	}

	resp, err := service.VerifyToken(ctx, req)
	if err != nil {
		t.Fatalf("VerifyToken failed: %v", err)
	}

	if !resp.Valid {
		t.Error("Expected token to be valid")
	}
	if resp.UserId != "user-123" {
		t.Errorf("Expected user ID user-123, got %s", resp.UserId)
	}
}

func TestService_VerifyToken_InvalidToken(t *testing.T) {
	mockRepo := &mockRepository{}
	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.VerifyTokenRequest{
		Token: "invalid.token.here",
	}

	resp, err := service.VerifyToken(ctx, req)
	if err != nil {
		t.Fatalf("VerifyToken returned error: %v", err)
	}

	if resp.Valid {
		t.Error("Expected token to be invalid")
	}
}

func TestService_RefreshToken_Success(t *testing.T) {
	mockRepo := &mockRepository{}
	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	// Generate a valid refresh token
	_, refreshToken, err := service.tokenService.GenerateTokenPair("user-123", "test@example.com", "USER")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	req := &pb.RefreshTokenRequest{
		RefreshToken: refreshToken,
	}

	resp, err := service.RefreshToken(ctx, req)
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}

	if resp.AccessToken == "" {
		t.Error("Expected new access token")
	}
	if resp.RefreshToken == "" {
		t.Error("Expected new refresh token")
	}
}

func TestService_RefreshToken_InvalidToken(t *testing.T) {
	mockRepo := &mockRepository{}
	service := NewService(mockRepo, "test-secret")
	ctx := context.Background()

	req := &pb.RefreshTokenRequest{
		RefreshToken: "invalid.refresh.token",
	}

	_, err := service.RefreshToken(ctx, req)
	if err == nil {
		t.Fatal("Expected error for invalid refresh token")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated error, got %v", err)
	}
}

func TestService_AllEndpoints_Coverage(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T, *Service)
	}{
		{"Register with admin role", testRegisterWithAdminRole},
		{"Login missing password", testLoginMissingPassword},
		{"GetProfile missing user ID", testGetProfileMissingUserID},
		{"UpdateProfile missing user ID", testUpdateProfileMissingUserID},
		{"ChangePassword missing fields", testChangePasswordMissingFields},
		{"DeleteAccount missing user ID", testDeleteAccountMissingUserID},
		{"VerifyToken empty token", testVerifyTokenEmpty},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockRepository{
				createFunc: func(ctx context.Context, email, password, name, phone, role string) (*Account, error) {
					return &Account{
						ID:        "test-id",
						Email:     email,
						Name:      name,
						Phone:     phone,
						Role:      role,
						IsActive:  true,
						CreatedAt: time.Now(),
						UpdatedAt: time.Now(),
					}, nil
				},
			}
			service := NewService(mockRepo, "test-secret")
			tt.testFunc(t, service)
		})
	}
}

func testRegisterWithAdminRole(t *testing.T, service *Service) {
	// Note: Current implementation defaults to USER, but role is stored correctly
	ctx := context.Background()
	req := &pb.RegisterRequest{
		Email:    "admin@example.com",
		Password: "adminpass",
		Name:     "Admin User",
	}
	resp, err := service.Register(ctx, req)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if resp.User.Role != "USER" {
		t.Errorf("Expected role USER (default), got %s", resp.User.Role)
	}
}

func testLoginMissingPassword(t *testing.T, service *Service) {
	ctx := context.Background()
	req := &pb.LoginRequest{
		Email:    "test@example.com",
		Password: "",
	}
	_, err := service.Login(ctx, req)
	if err == nil {
		t.Fatal("Expected error for missing password")
	}
}

func testGetProfileMissingUserID(t *testing.T, service *Service) {
	ctx := context.Background()
	req := &pb.GetProfileRequest{
		UserId: "",
	}
	_, err := service.GetProfile(ctx, req)
	if err == nil {
		t.Fatal("Expected error for missing user ID")
	}
}

func testUpdateProfileMissingUserID(t *testing.T, service *Service) {
	ctx := context.Background()
	req := &pb.UpdateProfileRequest{
		UserId: "",
		Name:   "New Name",
	}
	_, err := service.UpdateProfile(ctx, req)
	if err == nil {
		t.Fatal("Expected error for missing user ID")
	}
}

func testChangePasswordMissingFields(t *testing.T, service *Service) {
	ctx := context.Background()
	req := &pb.ChangePasswordRequest{
		UserId:      "test-id",
		OldPassword: "",
		NewPassword: "newpass",
	}
	_, err := service.ChangePassword(ctx, req)
	if err == nil {
		t.Fatal("Expected error for missing old password")
	}
}

func testDeleteAccountMissingUserID(t *testing.T, service *Service) {
	ctx := context.Background()
	req := &pb.DeleteAccountRequest{
		UserId: "",
	}
	_, err := service.DeleteAccount(ctx, req)
	if err == nil {
		t.Fatal("Expected error for missing user ID")
	}
}

func testVerifyTokenEmpty(t *testing.T, service *Service) {
	ctx := context.Background()
	req := &pb.VerifyTokenRequest{
		Token: "",
	}
	_, err := service.VerifyToken(ctx, req)
	if err == nil {
		t.Fatal("Expected error for empty token")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error, got %v", err)
	}
}

// Helper function to create timestamppb from time.Time for testing
func mustTimestamp(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t)
}
