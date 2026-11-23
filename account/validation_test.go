package account

import (
	"context"
	"testing"

	"github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRegister_RejectsBadEmailAndShortPassword(t *testing.T) {
	svc := NewService(&mockRepository{}, newMemTokenStore(), "test-secret")
	cases := []*pb.RegisterRequest{
		{Email: "not-an-email", Password: "password123", Name: "A"},
		{Email: "a@example.com", Password: "short", Name: "A"},
	}
	for _, req := range cases {
		if _, err := svc.Register(context.Background(), req); status.Code(err) != codes.InvalidArgument {
			t.Errorf("%+v: expected InvalidArgument, got %v", req, err)
		}
	}
}

func TestRegister_NormalizesEmailAndForcesUserRole(t *testing.T) {
	var gotEmail, gotRole string
	repo := &mockRepository{createFunc: func(_ context.Context, email, _, name, phone, role string) (*Account, error) {
		gotEmail, gotRole = email, role
		return &Account{ID: "id", Email: email, Name: name, Role: role}, nil
	}}
	svc := NewService(repo, newMemTokenStore(), "test-secret")
	if _, err := svc.Register(context.Background(), &pb.RegisterRequest{Email: "  Bob@Example.COM ", Password: "password123", Name: "Bob"}); err != nil {
		t.Fatal(err)
	}
	if gotEmail != "bob@example.com" || gotRole != "USER" {
		t.Fatalf("got email=%q role=%q", gotEmail, gotRole)
	}
}

func TestEnsureAdmin_IsIdempotent(t *testing.T) {
	calls := 0
	repo := &mockRepository{createFunc: func(_ context.Context, _, _, _, _, role string) (*Account, error) {
		calls++
		if role != "ADMIN" {
			t.Fatalf("expected ADMIN role, got %s", role)
		}
		if calls > 1 {
			return nil, ErrEmailAlreadyExists
		}
		return &Account{ID: "id", Role: role}, nil
	}}
	svc := NewService(repo, newMemTokenStore(), "test-secret")
	for i := 0; i < 2; i++ {
		if err := svc.EnsureAdmin(context.Background(), "admin@example.com", "admin-password"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}
