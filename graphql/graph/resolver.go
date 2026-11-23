// Package graph holds the GraphQL schema implementation for the API gateway.
// Every resolver is a thin translation layer over the backend gRPC services.
package graph

//go:generate go -C .. run github.com/99designs/gqlgen generate

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accountpb "github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	catalogpb "github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	notificationpb "github.com/Ujjwaljain16/E-commerce-Backend/notification/pb"
	orderpb "github.com/Ujjwaljain16/E-commerce-Backend/order/pb"
	paymentpb "github.com/Ujjwaljain16/E-commerce-Backend/payment/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/auth"
)

// TokenValidator checks access tokens. *auth.TokenService satisfies it.
type TokenValidator interface {
	ValidateAccessToken(token string) (*auth.Claims, error)
}

// Resolver wires the schema to the backend services.
type Resolver struct {
	AccountSvc      accountpb.AccountServiceClient
	CatalogSvc      catalogpb.CatalogServiceClient
	OrderSvc        orderpb.OrderServiceClient
	PaymentSvc      paymentpb.PaymentServiceClient
	NotificationSvc notificationpb.NotificationServiceClient
	Tokens          TokenValidator
}

// ownedOrder loads an order the caller is allowed to see. Orders belonging to
// someone else look exactly like missing ones so their existence never leaks.
func (r *Resolver) ownedOrder(ctx context.Context, id string) (*orderpb.Order, error) {
	claims, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := r.OrderSvc.GetOrder(ctx, &orderpb.GetOrderRequest{Id: id})
	if status.Code(err) == codes.NotFound {
		return nil, errOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	o := resp.GetOrder()
	if o == nil || (o.GetUserId() != claims.UserID && !isAdmin(claims)) {
		return nil, errOrderNotFound
	}
	return o, nil
}
