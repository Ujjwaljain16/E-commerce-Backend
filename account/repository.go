package account

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrAccountNotFound is returned when an account is not found
	ErrAccountNotFound = errors.New("account not found")
	// ErrEmailAlreadyExists is returned when email is already registered
	ErrEmailAlreadyExists = errors.New("email already exists")
	// ErrInvalidCredentials is returned when login credentials are invalid
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// Account represents a user account in the system
type Account struct {
	ID           string
	Email        string
	PasswordHash string
	Name         string
	Phone        string
	IsVerified   bool
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Repository defines the interface for account data operations
type Repository interface {
	Create(ctx context.Context, email, password, name, phone string) (*Account, error)
	GetByID(ctx context.Context, id string) (*Account, error)
	GetByEmail(ctx context.Context, email string) (*Account, error)
	Update(ctx context.Context, id, name, phone string) (*Account, error)
	UpdatePassword(ctx context.Context, id, newPasswordHash string) error
	Delete(ctx context.Context, id string) error
	VerifyPassword(ctx context.Context, email, password string) (*Account, error)
	Close() error
}

type repository struct {
	db *sql.DB
}

// NewRepository creates a new account repository
func NewRepository(db *sql.DB) Repository {
	return &repository{db: db}
}

// Create creates a new account with hashed password
func (r *repository) Create(ctx context.Context, email, password, name, phone string) (*Account, error) {
	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	account := &Account{
		ID:           uuid.New().String(),
		Email:        email,
		PasswordHash: string(hashedPassword),
		Name:         name,
		Phone:        phone,
		IsVerified:   false,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	query := `
		INSERT INTO accounts (id, email, password_hash, name, phone, is_verified, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err = r.db.ExecContext(ctx, query,
		account.ID,
		account.Email,
		account.PasswordHash,
		account.Name,
		account.Phone,
		account.IsVerified,
		account.IsActive,
		account.CreatedAt,
		account.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violation
		if err.Error() == "pq: duplicate key value violates unique constraint \"accounts_email_key\"" {
			return nil, ErrEmailAlreadyExists
		}
		return nil, err
	}

	return account, nil
}

// GetByID retrieves an account by ID
func (r *repository) GetByID(ctx context.Context, id string) (*Account, error) {
	account := &Account{}

	query := `
		SELECT id, email, password_hash, name, phone, is_verified, is_active, created_at, updated_at
		FROM accounts
		WHERE id = $1 AND is_active = TRUE
	`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&account.ID,
		&account.Email,
		&account.PasswordHash,
		&account.Name,
		&account.Phone,
		&account.IsVerified,
		&account.IsActive,
		&account.CreatedAt,
		&account.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}

	return account, nil
}

// GetByEmail retrieves an account by email
func (r *repository) GetByEmail(ctx context.Context, email string) (*Account, error) {
	account := &Account{}

	query := `
		SELECT id, email, password_hash, name, phone, is_verified, is_active, created_at, updated_at
		FROM accounts
		WHERE email = $1 AND is_active = TRUE
	`

	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&account.ID,
		&account.Email,
		&account.PasswordHash,
		&account.Name,
		&account.Phone,
		&account.IsVerified,
		&account.IsActive,
		&account.CreatedAt,
		&account.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}

	return account, nil
}

// Update updates account profile information
func (r *repository) Update(ctx context.Context, id, name, phone string) (*Account, error) {
	query := `
		UPDATE accounts
		SET name = $2, phone = $3, updated_at = $4
		WHERE id = $1 AND is_active = TRUE
		RETURNING id, email, password_hash, name, phone, is_verified, is_active, created_at, updated_at
	`

	account := &Account{}
	err := r.db.QueryRowContext(ctx, query, id, name, phone, time.Now()).Scan(
		&account.ID,
		&account.Email,
		&account.PasswordHash,
		&account.Name,
		&account.Phone,
		&account.IsVerified,
		&account.IsActive,
		&account.CreatedAt,
		&account.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}

	return account, nil
}

// UpdatePassword updates the account password
func (r *repository) UpdatePassword(ctx context.Context, id, newPasswordHash string) error {
	query := `
		UPDATE accounts
		SET password_hash = $2, updated_at = $3
		WHERE id = $1 AND is_active = TRUE
	`

	result, err := r.db.ExecContext(ctx, query, id, newPasswordHash, time.Now())
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrAccountNotFound
	}

	return nil
}

// Delete soft-deletes an account by setting is_active to false
func (r *repository) Delete(ctx context.Context, id string) error {
	query := `
		UPDATE accounts
		SET is_active = FALSE, updated_at = $2
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query, id, time.Now())
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrAccountNotFound
	}

	return nil
}

// VerifyPassword verifies email and password combination
func (r *repository) VerifyPassword(ctx context.Context, email, password string) (*Account, error) {
	account, err := r.GetByEmail(ctx, email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	err = bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password))
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	return account, nil
}

// Close closes the database connection
func (r *repository) Close() error {
	return r.db.Close()
}
