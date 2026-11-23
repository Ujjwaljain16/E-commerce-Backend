//go:build e2e

// Package e2e drives the running stack (docker compose up) through the full checkout saga.
// Run with: go test -count=1 -tags=e2e ./tests/e2e/...
package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	accountpb "github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	catalogpb "github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	notificationpb "github.com/Ujjwaljain16/E-commerce-Backend/notification/pb"
	orderpb "github.com/Ujjwaljain16/E-commerce-Backend/order/pb"
	paymentpb "github.com/Ujjwaljain16/E-commerce-Backend/payment/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type clients struct {
	account      accountpb.AccountServiceClient
	catalog      catalogpb.CatalogServiceClient
	order        orderpb.OrderServiceClient
	payment      paymentpb.PaymentServiceClient
	notification notificationpb.NotificationServiceClient
}

func dial(t *testing.T, env, def string) *grpc.ClientConn {
	t.Helper()
	addr := os.Getenv(env)
	if addr == "" {
		addr = def
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func setup(t *testing.T) clients {
	return clients{
		account:      accountpb.NewAccountServiceClient(dial(t, "ACCOUNT_ADDR", "localhost:50051")),
		catalog:      catalogpb.NewCatalogServiceClient(dial(t, "CATALOG_ADDR", "localhost:50052")),
		order:        orderpb.NewOrderServiceClient(dial(t, "ORDER_ADDR", "localhost:50053")),
		payment:      paymentpb.NewPaymentServiceClient(dial(t, "PAYMENT_ADDR", "localhost:50054")),
		notification: notificationpb.NewNotificationServiceClient(dial(t, "NOTIFICATION_ADDR", "localhost:50055")),
	}
}

func eventually(t *testing.T, what string, check func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	last := ""
	for time.Now().Before(deadline) {
		ok, state := check()
		if ok {
			return
		}
		last = state
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s (last state: %s)", what, last)
}

func TestCheckoutSaga(t *testing.T) {
	c := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	suffix := time.Now().UnixNano()

	reg, err := c.account.Register(ctx, &accountpb.RegisterRequest{
		Email: fmt.Sprintf("e2e-%d@example.com", suffix), Password: "password123", Name: "E2E Buyer",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	userID := reg.User.Id

	prod, err := c.catalog.CreateProduct(ctx, &catalogpb.CreateProductRequest{
		Name: "E2E Espresso Machine", Description: "Dual boiler espresso machine for testing",
		Price: 19.99, Sku: fmt.Sprintf("E2E-%d", suffix), Stock: 5, Category: "kitchen",
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	productID := prod.Product.Id

	stock := func() int32 {
		p, err := c.catalog.GetProduct(ctx, &catalogpb.GetProductRequest{Id: productID})
		if err != nil {
			t.Fatalf("get product: %v", err)
		}
		return p.Product.Stock
	}
	orderStatus := func(id string) orderpb.OrderStatus {
		o, err := c.order.GetOrder(ctx, &orderpb.GetOrderRequest{Id: id})
		if err != nil {
			t.Fatalf("get order: %v", err)
		}
		return o.Order.Status
	}
	paymentStatus := func(orderID string) paymentpb.PaymentStatus {
		p, err := c.payment.GetPaymentByOrder(ctx, &paymentpb.GetPaymentByOrderRequest{OrderId: orderID})
		if status.Code(err) == codes.NotFound {
			return paymentpb.PaymentStatus_PAYMENT_STATUS_UNSPECIFIED
		}
		if err != nil {
			t.Fatalf("get payment: %v", err)
		}
		return p.Payment.Status
	}
	placeOrder := func(qty int32, method string) *orderpb.Order {
		resp, err := c.order.CreateOrder(ctx, &orderpb.CreateOrderRequest{
			UserId:          userID,
			Items:           []*orderpb.OrderItemInput{{ProductId: productID, Quantity: qty, Price: 0.01}},
			ShippingAddress: "1 Test Street",
			PaymentMethod:   method,
		})
		if err != nil {
			t.Fatalf("create order: %v", err)
		}
		return resp.Order
	}

	t.Run("successful payment confirms the order", func(t *testing.T) {
		o := placeOrder(2, "card")
		if o.Status != orderpb.OrderStatus_PENDING {
			t.Fatalf("new order should be PENDING, got %v", o.Status)
		}
		if o.TotalAmount != 39.98 || o.Items[0].Price != 19.99 {
			t.Fatalf("order must use catalog prices, got total=%v price=%v", o.TotalAmount, o.Items[0].Price)
		}
		if got := stock(); got != 3 {
			t.Fatalf("stock should be reserved: want 3, got %d", got)
		}
		eventually(t, "order CONFIRMED", func() (bool, string) {
			s := orderStatus(o.Id)
			return s == orderpb.OrderStatus_CONFIRMED, s.String()
		})
		if s := paymentStatus(o.Id); s != paymentpb.PaymentStatus_PAYMENT_STATUS_SUCCEEDED {
			t.Fatalf("payment should have succeeded, got %v", s)
		}
	})

	t.Run("declined card cancels the order and releases stock", func(t *testing.T) {
		o := placeOrder(1, "card_declined")
		eventually(t, "order CANCELLED", func() (bool, string) {
			s := orderStatus(o.Id)
			return s == orderpb.OrderStatus_CANCELLED, s.String()
		})
		eventually(t, "stock released", func() (bool, string) {
			got := stock()
			return got == 3, fmt.Sprint(got)
		})
		if s := paymentStatus(o.Id); s != paymentpb.PaymentStatus_PAYMENT_STATUS_FAILED {
			t.Fatalf("payment should have failed, got %v", s)
		}
	})

	t.Run("cancelling a paid order refunds it and restocks", func(t *testing.T) {
		o := placeOrder(1, "card")
		eventually(t, "order CONFIRMED", func() (bool, string) {
			s := orderStatus(o.Id)
			return s == orderpb.OrderStatus_CONFIRMED, s.String()
		})
		if _, err := c.order.CancelOrder(ctx, &orderpb.CancelOrderRequest{Id: o.Id, Reason: "changed my mind"}); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		eventually(t, "payment REFUNDED", func() (bool, string) {
			s := paymentStatus(o.Id)
			return s == paymentpb.PaymentStatus_PAYMENT_STATUS_REFUNDED, s.String()
		})
		eventually(t, "stock released", func() (bool, string) {
			got := stock()
			return got == 3, fmt.Sprint(got)
		})
		if s := orderStatus(o.Id); s != orderpb.OrderStatus_CANCELLED {
			t.Fatalf("refund must not overwrite CANCELLED, got %v", s)
		}
	})

	t.Run("ordering more than is in stock is rejected", func(t *testing.T) {
		_, err := c.order.CreateOrder(ctx, &orderpb.CreateOrderRequest{
			UserId: userID, Items: []*orderpb.OrderItemInput{{ProductId: productID, Quantity: 999}},
			ShippingAddress: "1 Test Street", PaymentMethod: "card",
		})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("expected FailedPrecondition, got %v", err)
		}
		if got := stock(); got != 3 {
			t.Fatalf("failed order must not change stock, got %d", got)
		}
	})

	t.Run("customer is notified about each step", func(t *testing.T) {
		eventually(t, "notifications", func() (bool, string) {
			resp, err := c.notification.ListNotifications(ctx, &notificationpb.ListNotificationsRequest{UserId: userID, PageSize: 50})
			if err != nil {
				return false, err.Error()
			}
			seen := map[string]int{}
			for _, n := range resp.Notifications {
				seen[n.Type]++
			}
			// 3 orders placed, 2 confirmed, 2 cancelled (declined + customer).
			ok := seen["order.created"] == 3 && seen["order.confirmed"] == 2 && seen["order.cancelled"] == 2
			return ok, fmt.Sprint(seen)
		})
	})

	t.Run("search finds the product by description words", func(t *testing.T) {
		eventually(t, "search hit", func() (bool, string) {
			resp, err := c.catalog.SearchProducts(ctx, &catalogpb.SearchProductsRequest{Query: "dual boiler espresso"})
			if err != nil {
				return false, err.Error()
			}
			for _, p := range resp.Products {
				if p.Id == productID {
					return true, ""
				}
			}
			return false, fmt.Sprintf("%d results", resp.Total)
		})
	})
}
