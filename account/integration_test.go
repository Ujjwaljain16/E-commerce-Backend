package account

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// setupIntegrationTest creates a PostgreSQL container and returns a configured service
func setupIntegrationTest(t *testing.T) (*Service, func()) {
	t.Helper()
	ctx := context.Background()

	// Create PostgreSQL container
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("Failed to start postgres container: %v", err)
	}

	// Get connection string
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get connection string: %v", err)
	}

	// Connect to database
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Run migrations
	if err := runMigrations(db); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Create repository and service
	repo := NewRepository(db)
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "test-secret-key-for-testing-only"
	}
	service := NewService(repo, jwtSecret)

	// Cleanup function
	cleanup := func() {
		db.Close()
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}

	return service, cleanup
}

// runMigrations applies database schema
func runMigrations(db *sql.DB) error {
	// Create accounts table
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS accounts (
			id UUID PRIMARY KEY,
			email VARCHAR(255) UNIQUE NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			name VARCHAR(255) NOT NULL,
			phone VARCHAR(20),
			is_verified BOOLEAN DEFAULT FALSE,
			is_active BOOLEAN DEFAULT TRUE,
			role VARCHAR(20) NOT NULL DEFAULT 'USER',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			deleted_at TIMESTAMP,
			CONSTRAINT accounts_role_check CHECK (role IN ('USER', 'ADMIN'))
		);
	`
	if _, err := db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create accounts table: %w", err)
	}

	// Create index on role
	createIndexSQL := `CREATE INDEX IF NOT EXISTS idx_accounts_role ON accounts(role);`
	if _, err := db.Exec(createIndexSQL); err != nil {
		return fmt.Errorf("failed to create role index: %w", err)
	}

	return nil
}

func TestIntegration_RegisterAndLogin(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Test Register
	registerReq := &pb.RegisterRequest{
		Email:    "integration@test.com",
		Password: "SecurePass123!",
		Name:     "Integration Test",
		Phone:    "1234567890",
	}

	registerResp, err := service.Register(ctx, registerReq)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if registerResp.User == nil {
		t.Fatal("Expected user in register response")
	}
	if registerResp.User.Email != registerReq.Email {
		t.Errorf("Expected email %s, got %s", registerReq.Email, registerResp.User.Email)
	}
	if registerResp.User.Role != "USER" {
		t.Errorf("Expected role USER, got %s", registerResp.User.Role)
	}
	if registerResp.AccessToken == "" {
		t.Error("Expected non-empty access token")
	}
	if registerResp.RefreshToken == "" {
		t.Error("Expected non-empty refresh token")
	}

	// Test Login with same credentials
	loginReq := &pb.LoginRequest{
		Email:    registerReq.Email,
		Password: registerReq.Password,
	}

	loginResp, err := service.Login(ctx, loginReq)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if loginResp.User.Email != registerReq.Email {
		t.Errorf("Expected email %s, got %s", registerReq.Email, loginResp.User.Email)
	}
	if loginResp.AccessToken == "" {
		t.Error("Expected non-empty access token")
	}
}

func TestIntegration_RegisterDuplicateEmail(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Register first user
	registerReq := &pb.RegisterRequest{
		Email:    "duplicate@test.com",
		Password: "Pass123!",
		Name:     "First User",
		Phone:    "1111111111",
	}

	_, err := service.Register(ctx, registerReq)
	if err != nil {
		t.Fatalf("First register failed: %v", err)
	}

	// Try to register with same email
	registerReq.Name = "Second User"
	registerReq.Phone = "2222222222"

	_, err = service.Register(ctx, registerReq)
	if err == nil {
		t.Fatal("Expected error for duplicate email")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.AlreadyExists {
		t.Errorf("Expected code AlreadyExists, got %v", st.Code())
	}
}

func TestIntegration_LoginInvalidCredentials(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Register user
	registerReq := &pb.RegisterRequest{
		Email:    "logintest@test.com",
		Password: "CorrectPass123!",
		Name:     "Login Test",
		Phone:    "3333333333",
	}

	_, err := service.Register(ctx, registerReq)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Test with wrong password
	loginReq := &pb.LoginRequest{
		Email:    registerReq.Email,
		Password: "WrongPassword123!",
	}

	_, err = service.Login(ctx, loginReq)
	if err == nil {
		t.Fatal("Expected error for wrong password")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Errorf("Expected code Unauthenticated, got %v", st.Code())
	}

	// Test with non-existent email
	loginReq.Email = "nonexistent@test.com"
	loginReq.Password = "SomePass123!"

	_, err = service.Login(ctx, loginReq)
	if err == nil {
		t.Fatal("Expected error for non-existent email")
	}
}

func TestIntegration_GetProfile(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Register user
	registerReq := &pb.RegisterRequest{
		Email:    "profile@test.com",
		Password: "Pass123!",
		Name:     "Profile Test",
		Phone:    "4444444444",
	}

	registerResp, err := service.Register(ctx, registerReq)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Get profile
	profileReq := &pb.GetProfileRequest{
		UserId: registerResp.User.Id,
	}

	profileResp, err := service.GetProfile(ctx, profileReq)
	if err != nil {
		t.Fatalf("GetProfile failed: %v", err)
	}

	if profileResp.User.Email != registerReq.Email {
		t.Errorf("Expected email %s, got %s", registerReq.Email, profileResp.User.Email)
	}
	if profileResp.User.Name != registerReq.Name {
		t.Errorf("Expected name %s, got %s", registerReq.Name, profileResp.User.Name)
	}
}

func TestIntegration_UpdateProfile(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Register user
	registerReq := &pb.RegisterRequest{
		Email:    "update@test.com",
		Password: "Pass123!",
		Name:     "Original Name",
		Phone:    "5555555555",
	}

	registerResp, err := service.Register(ctx, registerReq)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Update profile
	updateReq := &pb.UpdateProfileRequest{
		UserId: registerResp.User.Id,
		Name:   "Updated Name",
		Phone:  "6666666666",
	}

	updateResp, err := service.UpdateProfile(ctx, updateReq)
	if err != nil {
		t.Fatalf("UpdateProfile failed: %v", err)
	}

	if updateResp.User.Name != updateReq.Name {
		t.Errorf("Expected name %s, got %s", updateReq.Name, updateResp.User.Name)
	}
	if updateResp.User.Phone != updateReq.Phone {
		t.Errorf("Expected phone %s, got %s", updateReq.Phone, updateResp.User.Phone)
	}
}

func TestIntegration_ChangePassword(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Register user
	oldPassword := "OldPass123!"
	registerReq := &pb.RegisterRequest{
		Email:    "changepass@test.com",
		Password: oldPassword,
		Name:     "Change Pass Test",
		Phone:    "7777777777",
	}

	registerResp, err := service.Register(ctx, registerReq)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Change password
	newPassword := "NewPass456!"
	changeReq := &pb.ChangePasswordRequest{
		UserId:      registerResp.User.Id,
		OldPassword: oldPassword,
		NewPassword: newPassword,
	}

	_, err = service.ChangePassword(ctx, changeReq)
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	// Verify can login with new password
	loginReq := &pb.LoginRequest{
		Email:    registerReq.Email,
		Password: newPassword,
	}

	_, err = service.Login(ctx, loginReq)
	if err != nil {
		t.Fatalf("Login with new password failed: %v", err)
	}

	// Verify cannot login with old password
	loginReq.Password = oldPassword
	_, err = service.Login(ctx, loginReq)
	if err == nil {
		t.Fatal("Expected error when logging in with old password")
	}
}

func TestIntegration_VerifyToken(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Register user
	registerReq := &pb.RegisterRequest{
		Email:    "verifytoken@test.com",
		Password: "Pass123!",
		Name:     "Verify Token Test",
		Phone:    "8888888888",
	}

	registerResp, err := service.Register(ctx, registerReq)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Verify valid token
	verifyReq := &pb.VerifyTokenRequest{
		Token: registerResp.AccessToken,
	}

	verifyResp, err := service.VerifyToken(ctx, verifyReq)
	if err != nil {
		t.Fatalf("VerifyToken failed: %v", err)
	}

	if !verifyResp.Valid {
		t.Error("Expected token to be valid")
	}
	if verifyResp.UserId != registerResp.User.Id {
		t.Errorf("Expected user ID %s, got %s", registerResp.User.Id, verifyResp.UserId)
	}

	// Verify invalid token
	verifyReq.Token = "invalid.token.here"
	verifyResp, err = service.VerifyToken(ctx, verifyReq)
	if err != nil {
		t.Fatalf("VerifyToken with invalid token returned error: %v", err)
	}
	if verifyResp.Valid {
		t.Error("Expected invalid token to be marked as invalid")
	}
}

func TestIntegration_RefreshToken(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Register user
	registerReq := &pb.RegisterRequest{
		Email:    "refreshtoken@test.com",
		Password: "Pass123!",
		Name:     "Refresh Token Test",
		Phone:    "9999999999",
	}

	registerResp, err := service.Register(ctx, registerReq)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Refresh token
	refreshReq := &pb.RefreshTokenRequest{
		RefreshToken: registerResp.RefreshToken,
	}

	refreshResp, err := service.RefreshToken(ctx, refreshReq)
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}

	if refreshResp.AccessToken == "" {
		t.Error("Expected new access token")
	}
	if refreshResp.RefreshToken == "" {
		t.Error("Expected new refresh token")
	}
	// Note: New tokens may be identical if generated within the same second with same user data
}

func TestIntegration_DeleteAccount(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Register user
	registerReq := &pb.RegisterRequest{
		Email:    "delete@test.com",
		Password: "Pass123!",
		Name:     "Delete Test",
		Phone:    "0000000000",
	}

	registerResp, err := service.Register(ctx, registerReq)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Delete account
	deleteReq := &pb.DeleteAccountRequest{
		UserId: registerResp.User.Id,
	}

	_, err = service.DeleteAccount(ctx, deleteReq)
	if err != nil {
		t.Fatalf("DeleteAccount failed: %v", err)
	}

	// Verify cannot login after deletion
	loginReq := &pb.LoginRequest{
		Email:    registerReq.Email,
		Password: registerReq.Password,
	}

	_, err = service.Login(ctx, loginReq)
	if err == nil {
		t.Fatal("Expected error when logging in with deleted account")
	}

	// Verify cannot get profile
	profileReq := &pb.GetProfileRequest{
		UserId: registerResp.User.Id,
	}

	_, err = service.GetProfile(ctx, profileReq)
	if err == nil {
		t.Fatal("Expected error when getting profile of deleted account")
	}
}
