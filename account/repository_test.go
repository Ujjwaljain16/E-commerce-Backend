package account

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/lib/pq"
)

// Note: These are integration tests that require a running PostgreSQL database
// Skip if DATABASE_URL is not set

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	// Skip if no database URL is provided
	dbURL := "postgres://postgres:postgres@localhost:5432/ecommerce_test?sslmode=disable"

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Skip("Skipping integration test: database not available")
	}

	// Test connection
	if err := db.Ping(); err != nil {
		t.Skip("Skipping integration test: cannot connect to database")
	}

	// Cleanup function
	cleanup := func() {
		_, _ = db.Exec("TRUNCATE TABLE accounts CASCADE")
		db.Close()
	}

	return db, cleanup
}

func TestRepository_Create(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	account, err := repo.Create(ctx, "test@example.com", "password123", "Test User", "1234567890")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if account.ID == "" {
		t.Error("Expected non-empty ID")
	}
	if account.Email != "test@example.com" {
		t.Errorf("Expected email test@example.com, got %s", account.Email)
	}
	if account.Name != "Test User" {
		t.Errorf("Expected name Test User, got %s", account.Name)
	}
	if !account.IsActive {
		t.Error("Expected account to be active")
	}
}

func TestRepository_Create_DuplicateEmail(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create first account
	_, err := repo.Create(ctx, "duplicate@example.com", "password123", "User 1", "1111111111")
	if err != nil {
		t.Fatalf("First create failed: %v", err)
	}

	// Try to create with same email
	_, err = repo.Create(ctx, "duplicate@example.com", "password456", "User 2", "2222222222")
	if err != ErrEmailAlreadyExists {
		t.Errorf("Expected ErrEmailAlreadyExists, got %v", err)
	}
}

func TestRepository_GetByID(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create account
	created, err := repo.Create(ctx, "getbyid@example.com", "password123", "Get By ID", "3333333333")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Get by ID
	account, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if account.ID != created.ID {
		t.Errorf("Expected ID %s, got %s", created.ID, account.ID)
	}
	if account.Email != "getbyid@example.com" {
		t.Errorf("Expected email getbyid@example.com, got %s", account.Email)
	}
}

func TestRepository_GetByEmail(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create account
	_, err := repo.Create(ctx, "getbyemail@example.com", "password123", "Get By Email", "4444444444")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Get by email
	account, err := repo.GetByEmail(ctx, "getbyemail@example.com")
	if err != nil {
		t.Fatalf("GetByEmail failed: %v", err)
	}

	if account.Email != "getbyemail@example.com" {
		t.Errorf("Expected email getbyemail@example.com, got %s", account.Email)
	}
}

func TestRepository_Update(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create account
	created, err := repo.Create(ctx, "update@example.com", "password123", "Original Name", "5555555555")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update account
	updated, err := repo.Update(ctx, created.ID, "Updated Name", "6666666666")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Name != "Updated Name" {
		t.Errorf("Expected name Updated Name, got %s", updated.Name)
	}
	if updated.Phone != "6666666666" {
		t.Errorf("Expected phone 6666666666, got %s", updated.Phone)
	}
}

func TestRepository_VerifyPassword(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create account
	_, err := repo.Create(ctx, "verify@example.com", "correctpassword", "Verify User", "7777777777")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Test correct password
	account, err := repo.VerifyPassword(ctx, "verify@example.com", "correctpassword")
	if err != nil {
		t.Fatalf("VerifyPassword with correct password failed: %v", err)
	}
	if account.Email != "verify@example.com" {
		t.Errorf("Expected email verify@example.com, got %s", account.Email)
	}

	// Test wrong password
	_, err = repo.VerifyPassword(ctx, "verify@example.com", "wrongpassword")
	if err != ErrInvalidCredentials {
		t.Errorf("Expected ErrInvalidCredentials, got %v", err)
	}
}

func TestRepository_Delete(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create account
	created, err := repo.Create(ctx, "delete@example.com", "password123", "Delete User", "8888888888")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Delete account
	err = repo.Delete(ctx, created.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Try to get deleted account
	_, err = repo.GetByID(ctx, created.ID)
	if err != ErrAccountNotFound {
		t.Errorf("Expected ErrAccountNotFound for deleted account, got %v", err)
	}
}
