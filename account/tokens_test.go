package account

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// memTokenStore mirrors postgresTokenStore's rules in memory for service tests.
type memTokenStore struct {
	mu     sync.Mutex
	tokens map[string]*memToken
}

type memToken struct {
	user          string
	used, revoked bool
}

func newMemTokenStore() *memTokenStore { return &memTokenStore{tokens: map[string]*memToken{}} }

func (m *memTokenStore) Save(_ context.Context, jti, userID string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[jti] = &memToken{user: userID}
	return nil
}

func (m *memTokenStore) Consume(ctx context.Context, jti string) (string, error) {
	m.mu.Lock()
	t, ok := m.tokens[jti]
	m.mu.Unlock()
	switch {
	case !ok || t.revoked:
		return "", ErrRefreshTokenUnknown
	case t.used:
		_ = m.RevokeAll(ctx, t.user)
		return "", ErrRefreshTokenReused
	}
	t.used = true
	return t.user, nil
}

func (m *memTokenStore) Revoke(_ context.Context, jti string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tokens[jti]; ok {
		t.revoked = true
	}
	return nil
}

func (m *memTokenStore) RevokeAll(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.user == userID {
			t.revoked = true
		}
	}
	return nil
}

func activeAccountRepo() *mockRepository {
	acct := &Account{ID: "u1", Email: "u@example.com", Role: "USER", IsActive: true}
	return &mockRepository{
		verifyPasswordFunc: func(context.Context, string, string) (*Account, error) { return acct, nil },
		getByIDFunc:        func(context.Context, string) (*Account, error) { return acct, nil },
		updatePasswordFunc: func(context.Context, string, string) error { return nil },
	}
}

func login(t *testing.T, svc *Service) *pb.LoginResponse {
	t.Helper()
	resp, err := svc.Login(context.Background(), &pb.LoginRequest{Email: "u@example.com", Password: "password123"})
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestRefreshToken_RotatesAndRejectsReuse(t *testing.T) {
	svc := NewService(activeAccountRepo(), newMemTokenStore(), "test-secret")
	ctx := context.Background()
	first := login(t, svc)

	second, err := svc.RefreshToken(ctx, &pb.RefreshTokenRequest{RefreshToken: first.RefreshToken})
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("refresh must rotate the refresh token")
	}

	// Presenting the old token again looks like theft: reject it and sign out the whole family.
	if _, err := svc.RefreshToken(ctx, &pb.RefreshTokenRequest{RefreshToken: first.RefreshToken}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("reused token: want Unauthenticated, got %v", err)
	}
	if _, err := svc.RefreshToken(ctx, &pb.RefreshTokenRequest{RefreshToken: second.RefreshToken}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("after reuse the newest token must be revoked too, got %v", err)
	}
}

func TestLogout_RevokesRefreshToken(t *testing.T) {
	svc := NewService(activeAccountRepo(), newMemTokenStore(), "test-secret")
	ctx := context.Background()
	s := login(t, svc)

	if _, err := svc.Logout(ctx, &pb.LogoutRequest{RefreshToken: s.RefreshToken}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefreshToken(ctx, &pb.RefreshTokenRequest{RefreshToken: s.RefreshToken}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("logged-out token must not refresh, got %v", err)
	}
	if _, err := svc.Logout(ctx, &pb.LogoutRequest{RefreshToken: "garbage"}); err != nil {
		t.Fatalf("logout with a bad token should still succeed, got %v", err)
	}
}

func TestChangePassword_SignsOutOtherSessions(t *testing.T) {
	repo := activeAccountRepo()
	svc := NewService(repo, newMemTokenStore(), "test-secret")
	ctx := context.Background()
	s := login(t, svc)

	hash, _ := bcryptHash("password123")
	repo.getByIDFunc = func(context.Context, string) (*Account, error) {
		return &Account{ID: "u1", PasswordHash: hash, IsActive: true}, nil
	}
	if _, err := svc.ChangePassword(ctx, &pb.ChangePasswordRequest{UserId: "u1", OldPassword: "password123", NewPassword: "new-password-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefreshToken(ctx, &pb.RefreshTokenRequest{RefreshToken: s.RefreshToken}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("old session must be revoked after a password change, got %v", err)
	}
}

func TestRefreshToken_DeactivatedAccountIsRejected(t *testing.T) {
	repo := activeAccountRepo()
	svc := NewService(repo, newMemTokenStore(), "test-secret")
	s := login(t, svc)
	repo.getByIDFunc = func(context.Context, string) (*Account, error) { return nil, ErrAccountNotFound }
	if _, err := svc.RefreshToken(context.Background(), &pb.RefreshTokenRequest{RefreshToken: s.RefreshToken}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("deleted account must not refresh, got %v", err)
	}
}

func bcryptHash(p string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.MinCost)
	return string(h), err
}
