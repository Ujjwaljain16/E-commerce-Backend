package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accountpb "github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	catalogpb "github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	orderpb "github.com/Ujjwaljain16/E-commerce-Backend/order/pb"
	paymentpb "github.com/Ujjwaljain16/E-commerce-Backend/payment/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/auth"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
)

// Fakes embed the generated client interfaces so any RPC a test did not
// expect panics, which the gateway reports as INTERNAL.

type fakeAccounts struct {
	accountpb.AccountServiceClient
	users     map[string]*accountpb.User
	updateReq *accountpb.UpdateProfileRequest
	refreshed *accountpb.RefreshTokenResponse
}

func (f *fakeAccounts) GetProfile(_ context.Context, in *accountpb.GetProfileRequest, _ ...grpc.CallOption) (*accountpb.GetProfileResponse, error) {
	u, ok := f.users[in.UserId]
	if !ok {
		return nil, status.Error(codes.NotFound, "account not found")
	}
	return &accountpb.GetProfileResponse{User: u}, nil
}

func (f *fakeAccounts) UpdateProfile(_ context.Context, in *accountpb.UpdateProfileRequest, _ ...grpc.CallOption) (*accountpb.UpdateProfileResponse, error) {
	f.updateReq = in
	return &accountpb.UpdateProfileResponse{User: &accountpb.User{Id: in.UserId, Name: in.Name, Phone: in.Phone}}, nil
}

func (f *fakeAccounts) RefreshToken(context.Context, *accountpb.RefreshTokenRequest, ...grpc.CallOption) (*accountpb.RefreshTokenResponse, error) {
	return f.refreshed, nil
}

type fakeCatalog struct {
	catalogpb.CatalogServiceClient
	err       error
	panicMsg  string
	createReq *catalogpb.CreateProductRequest
	updateReq *catalogpb.UpdateProductRequest
	product   *catalogpb.Product
}

func (f *fakeCatalog) GetProduct(_ context.Context, in *catalogpb.GetProductRequest, _ ...grpc.CallOption) (*catalogpb.GetProductResponse, error) {
	if f.panicMsg != "" {
		panic(f.panicMsg)
	}
	if f.err != nil {
		return nil, f.err
	}
	return &catalogpb.GetProductResponse{Product: f.product}, nil
}

func (f *fakeCatalog) CreateProduct(_ context.Context, in *catalogpb.CreateProductRequest, _ ...grpc.CallOption) (*catalogpb.CreateProductResponse, error) {
	f.createReq = in
	return &catalogpb.CreateProductResponse{Product: &catalogpb.Product{Id: "p-new", Name: in.Name}}, nil
}

func (f *fakeCatalog) UpdateProduct(_ context.Context, in *catalogpb.UpdateProductRequest, _ ...grpc.CallOption) (*catalogpb.UpdateProductResponse, error) {
	f.updateReq = in
	return &catalogpb.UpdateProductResponse{Product: &catalogpb.Product{Id: in.Id, Name: in.Name, Price: in.Price}}, nil
}

type fakeOrders struct {
	orderpb.OrderServiceClient
	orders    map[string]*orderpb.Order
	createReq *orderpb.CreateOrderRequest
	listReq   *orderpb.ListOrdersRequest
	cancelled []string
}

func (f *fakeOrders) GetOrder(_ context.Context, in *orderpb.GetOrderRequest, _ ...grpc.CallOption) (*orderpb.GetOrderResponse, error) {
	o, ok := f.orders[in.Id]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "order %s does not exist", in.Id)
	}
	return &orderpb.GetOrderResponse{Order: o}, nil
}

func (f *fakeOrders) CreateOrder(_ context.Context, in *orderpb.CreateOrderRequest, _ ...grpc.CallOption) (*orderpb.CreateOrderResponse, error) {
	f.createReq = in
	return &orderpb.CreateOrderResponse{Order: &orderpb.Order{Id: "o-new", UserId: in.UserId, Status: orderpb.OrderStatus_PENDING}}, nil
}

func (f *fakeOrders) CancelOrder(_ context.Context, in *orderpb.CancelOrderRequest, _ ...grpc.CallOption) (*orderpb.CancelOrderResponse, error) {
	f.cancelled = append(f.cancelled, in.Id)
	return &orderpb.CancelOrderResponse{Success: true}, nil
}

func (f *fakeOrders) ListOrders(_ context.Context, in *orderpb.ListOrdersRequest, _ ...grpc.CallOption) (*orderpb.ListOrdersResponse, error) {
	f.listReq = in
	return &orderpb.ListOrdersResponse{}, nil
}

type fakePayments struct {
	paymentpb.PaymentServiceClient
	byOrderCalls int
}

func (f *fakePayments) GetPaymentByOrder(_ context.Context, in *paymentpb.GetPaymentByOrderRequest, _ ...grpc.CallOption) (*paymentpb.GetPaymentResponse, error) {
	f.byOrderCalls++
	return &paymentpb.GetPaymentResponse{Payment: &paymentpb.Payment{Id: "pay-1", OrderId: in.OrderId, Status: paymentpb.PaymentStatus_PAYMENT_STATUS_SUCCEEDED}}, nil
}

type fakeTokens struct{ claims *auth.Claims }

func (f fakeTokens) ValidateAccessToken(string) (*auth.Claims, error) { return f.claims, nil }

type testEnv struct {
	accounts *fakeAccounts
	catalog  *fakeCatalog
	orders   *fakeOrders
	payments *fakePayments
	resolver *Resolver
}

func newTestEnv() *testEnv {
	e := &testEnv{
		accounts: &fakeAccounts{users: map[string]*accountpb.User{
			"alice": {Id: "alice", Email: "alice@example.com", Name: "Alice", Phone: "111", Role: "USER"},
		}},
		catalog: &fakeCatalog{},
		orders: &fakeOrders{orders: map[string]*orderpb.Order{
			"o-alice": {Id: "o-alice", UserId: "alice", Status: orderpb.OrderStatus_SHIPPED},
			"o-bob":   {Id: "o-bob", UserId: "bob", Status: orderpb.OrderStatus_PENDING},
		}},
		payments: &fakePayments{},
	}
	e.resolver = &Resolver{AccountSvc: e.accounts, CatalogSvc: e.catalog, OrderSvc: e.orders, PaymentSvc: e.payments}
	return e
}

var (
	alice = &auth.Claims{UserID: "alice", Email: "alice@example.com", Role: "USER"}
	admin = &auth.Claims{UserID: "root", Email: "root@example.com", Role: "ADMIN"}
)

type gqlResponse struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []struct {
		Message    string         `json:"message"`
		Extensions map[string]any `json:"extensions"`
	} `json:"errors"`
}

func (r gqlResponse) code() string {
	if len(r.Errors) == 0 {
		return ""
	}
	c, _ := r.Errors[0].Extensions["code"].(string)
	return c
}

func do(t *testing.T, r *Resolver, claims *auth.Claims, query string, vars map[string]any) gqlResponse {
	t.Helper()
	return doWith(t, r, HandlerOptions{ComplexityLimit: 200}, claims, query, vars)
}

func doWith(t *testing.T, r *Resolver, opts HandlerOptions, claims *auth.Claims, query string, vars map[string]any) gqlResponse {
	t.Helper()
	opts.Logger = logger.New("graphql-gateway-test")
	h := NewHandler(r, opts)
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if claims != nil {
		req = req.WithContext(WithClaims(req.Context(), claims))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp gqlResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return resp
}

func TestAuthRequired(t *testing.T) {
	queries := map[string]string{
		"me":              `{ me { id } }`,
		"myOrders":        `{ myOrders { total } }`,
		"myPayments":      `{ myPayments { total } }`,
		"myNotifications": `{ myNotifications { total } }`,
		"order":           `{ order(id: "o-alice") { id } }`,
		"createOrder":     `mutation { createOrder(input: {items: [{productId: "p1", quantity: 1}], shippingAddress: "a", billingAddress: "b", paymentMethod: "card"}) { id } }`,
		"deleteAccount":   `mutation { deleteAccount }`,
	}
	for name, q := range queries {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv()
			resp := do(t, env.resolver, nil, q, nil)
			if resp.code() != CodeUnauthenticated {
				t.Fatalf("code = %q, want %s (errors: %+v)", resp.code(), CodeUnauthenticated, resp.Errors)
			}
			if env.orders.createReq != nil {
				t.Fatal("order service was called without a user")
			}
		})
	}
}

func TestAdminOnlyOperations(t *testing.T) {
	ops := map[string]string{
		"createProduct":     `mutation { createProduct(input: {name: "Lamp", price: 10, sku: "L1", stock: 1}) { id } }`,
		"deleteProduct":     `mutation { deleteProduct(id: "p1") }`,
		"orders":            `{ orders(userId: "bob") { total } }`,
		"updateOrderStatus": `mutation { updateOrderStatus(id: "o-bob", status: SHIPPED) { id } }`,
		"refundPayment":     `mutation { refundPayment(paymentId: "pay-1", reason: "damaged") { id } }`,
	}
	for name, q := range ops {
		t.Run(name, func(t *testing.T) {
			resp := do(t, newTestEnv().resolver, alice, q, nil)
			if resp.code() != CodeForbidden {
				t.Fatalf("code = %q, want %s", resp.code(), CodeForbidden)
			}
		})
	}

	t.Run("admin passes through", func(t *testing.T) {
		env := newTestEnv()
		resp := do(t, env.resolver, admin, `{ orders(userId: "bob", status: PENDING) { total } }`, nil)
		if len(resp.Errors) != 0 {
			t.Fatalf("unexpected errors: %+v", resp.Errors)
		}
		if env.orders.listReq.UserId != "bob" || env.orders.listReq.Status != orderpb.OrderStatus_PENDING {
			t.Fatalf("list request = %+v", env.orders.listReq)
		}
	})
}

func TestOrderOwnership(t *testing.T) {
	t.Run("owner sees order", func(t *testing.T) {
		resp := do(t, newTestEnv().resolver, alice, `{ order(id: "o-alice") { id status } }`, nil)
		if len(resp.Errors) != 0 || !strings.Contains(string(resp.Data["order"]), `"SHIPPED"`) {
			t.Fatalf("got %s %+v", resp.Data["order"], resp.Errors)
		}
	})

	t.Run("admin sees any order", func(t *testing.T) {
		resp := do(t, newTestEnv().resolver, admin, `{ order(id: "o-bob") { id } }`, nil)
		if len(resp.Errors) != 0 {
			t.Fatalf("unexpected errors: %+v", resp.Errors)
		}
	})

	// Someone else's order and a missing order must be indistinguishable.
	other := do(t, newTestEnv().resolver, alice, `{ order(id: "o-bob") { id } }`, nil)
	missing := do(t, newTestEnv().resolver, alice, `{ order(id: "nope") { id } }`, nil)
	for name, resp := range map[string]gqlResponse{"other user's": other, "missing": missing} {
		if resp.code() != CodeNotFound || resp.Errors[0].Message != "order not found" {
			t.Errorf("%s order: got %+v", name, resp.Errors)
		}
	}

	t.Run("cancel someone else's order", func(t *testing.T) {
		env := newTestEnv()
		resp := do(t, env.resolver, alice, `mutation { cancelOrder(id: "o-bob", reason: "x") }`, nil)
		if resp.code() != CodeNotFound {
			t.Fatalf("code = %q, want NOT_FOUND", resp.code())
		}
		if len(env.orders.cancelled) != 0 {
			t.Fatal("cancel reached the order service")
		}
	})

	t.Run("payment for someone else's order", func(t *testing.T) {
		env := newTestEnv()
		resp := do(t, env.resolver, alice, `{ paymentForOrder(orderId: "o-bob") { id } }`, nil)
		if resp.code() != CodeNotFound {
			t.Fatalf("code = %q, want NOT_FOUND", resp.code())
		}
		if env.payments.byOrderCalls != 0 {
			t.Fatal("payment service was queried")
		}
	})

	t.Run("payment for own order", func(t *testing.T) {
		env := newTestEnv()
		resp := do(t, env.resolver, alice, `{ paymentForOrder(orderId: "o-alice") { id status } }`, nil)
		if len(resp.Errors) != 0 || !strings.Contains(string(resp.Data["paymentForOrder"]), `"SUCCEEDED"`) {
			t.Fatalf("got %s %+v", resp.Data["paymentForOrder"], resp.Errors)
		}
	})
}

func TestCreateOrderUsesTokenUser(t *testing.T) {
	env := newTestEnv()
	q := `mutation($input: CreateOrderInput!) { createOrder(input: $input) { id userId status } }`
	vars := map[string]any{"input": map[string]any{
		"items":           []any{map[string]any{"productId": "p1", "quantity": 2}},
		"shippingAddress": "1 Main St",
		"billingAddress":  "1 Main St",
		"paymentMethod":   "card",
	}}
	resp := do(t, env.resolver, alice, q, vars)
	if len(resp.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", resp.Errors)
	}
	req := env.orders.createReq
	if req.UserId != "alice" {
		t.Fatalf("user id = %q, want alice", req.UserId)
	}
	if len(req.Items) != 1 {
		t.Fatalf("items = %+v", req.Items)
	}
	item := req.Items[0]
	if item.ProductId != "p1" || item.Quantity != 2 || item.Price != 0 || item.ProductName != "" {
		t.Fatalf("item = %+v, want only product id and quantity", item)
	}
}

func TestCreateOrderRejectsBadQuantity(t *testing.T) {
	env := newTestEnv()
	resp := do(t, env.resolver, alice, `mutation { createOrder(input: {items: [{productId: "p1", quantity: 0}], shippingAddress: "a", billingAddress: "b", paymentMethod: "card"}) { id } }`, nil)
	if resp.code() != CodeBadUserInput || env.orders.createReq != nil {
		t.Fatalf("code = %q, request sent = %v", resp.code(), env.orders.createReq != nil)
	}
}

func TestErrorCodeMapping(t *testing.T) {
	cases := []struct {
		err         error
		wantCode    string
		wantMessage string
	}{
		{status.Error(codes.InvalidArgument, "id is required"), CodeBadUserInput, "id is required"},
		{status.Error(codes.NotFound, "product not found"), CodeNotFound, "product not found"},
		{status.Error(codes.AlreadyExists, "sku taken"), CodeConflict, "sku taken"},
		{status.Error(codes.Unauthenticated, "bad credentials"), CodeUnauthenticated, "bad credentials"},
		{status.Error(codes.PermissionDenied, "nope"), CodeForbidden, "nope"},
		{status.Error(codes.FailedPrecondition, "out of stock"), CodeFailedPrecondition, "out of stock"},
		{status.Error(codes.Unavailable, "dial tcp 10.0.0.7:50052: refused"), CodeUnavailable, "service temporarily unavailable"},
		{status.Error(codes.DeadlineExceeded, "context deadline exceeded"), CodeUnavailable, "service temporarily unavailable"},
		{status.Error(codes.Internal, "pq: relation \"products\" does not exist"), CodeInternal, internalMessage},
		{context.Canceled, CodeInternal, internalMessage},
	}
	for _, tc := range cases {
		t.Run(tc.wantCode+"/"+tc.err.Error(), func(t *testing.T) {
			env := newTestEnv()
			env.catalog.err = tc.err
			resp := do(t, env.resolver, nil, `{ product(id: "p1") { id } }`, nil)
			if resp.code() != tc.wantCode || resp.Errors[0].Message != tc.wantMessage {
				t.Fatalf("got %+v, want %s %q", resp.Errors, tc.wantCode, tc.wantMessage)
			}
		})
	}
}

func TestPanicIsReportedAsInternal(t *testing.T) {
	env := newTestEnv()
	env.catalog.panicMsg = "nil map write in catalog"
	resp := do(t, env.resolver, nil, `{ product(id: "p1") { id } }`, nil)
	if resp.code() != CodeInternal || resp.Errors[0].Message != internalMessage {
		t.Fatalf("got %+v", resp.Errors)
	}
}

func TestPaginationValidation(t *testing.T) {
	for _, q := range []string{
		`{ products(pageSize: 1000) { total } }`,
		`{ products(page: 0) { total } }`,
		`{ myOrders(pageSize: 0) { total } }`,
	} {
		resp := do(t, newTestEnv().resolver, alice, q, nil)
		if resp.code() != CodeBadUserInput {
			t.Errorf("%s: code = %q, want BAD_USER_INPUT", q, resp.code())
		}
	}
}

func TestUpdateProfileKeepsOmittedFields(t *testing.T) {
	env := newTestEnv()
	resp := do(t, env.resolver, alice, `mutation { updateProfile(name: "Alicia") { name phone } }`, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", resp.Errors)
	}
	if got := env.accounts.updateReq; got.UserId != "alice" || got.Name != "Alicia" || got.Phone != "111" {
		t.Fatalf("update request = %+v", got)
	}
}

func TestUpdateProductMergesPatch(t *testing.T) {
	env := newTestEnv()
	env.catalog.product = &catalogpb.Product{Id: "p1", Name: "Lamp", Description: "warm", Price: 10, Stock: 3, Category: "home"}
	resp := do(t, env.resolver, admin, `mutation { updateProduct(id: "p1", input: {price: 12.5}) { id price } }`, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", resp.Errors)
	}
	got := env.catalog.updateReq
	if got.Name != "Lamp" || got.Description != "warm" || got.Price != 12.5 || got.Stock != 3 || got.Category != "home" {
		t.Fatalf("update request = %+v", got)
	}
}

func TestRefreshTokenLoadsProfile(t *testing.T) {
	env := newTestEnv()
	env.accounts.refreshed = &accountpb.RefreshTokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh"}
	env.resolver.Tokens = fakeTokens{claims: alice}
	resp := do(t, env.resolver, nil, `mutation { refreshToken(refreshToken: "old") { accessToken refreshToken user { email } } }`, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", resp.Errors)
	}
	if !strings.Contains(string(resp.Data["refreshToken"]), "alice@example.com") {
		t.Fatalf("payload = %s", resp.Data["refreshToken"])
	}
}

func TestIntrospectionDisabledByDefault(t *testing.T) {
	q := `{ __schema { queryType { name } } }`
	if resp := do(t, newTestEnv().resolver, nil, q, nil); len(resp.Errors) == 0 {
		t.Fatal("introspection should be rejected when the playground is off")
	}
	resp := doWith(t, newTestEnv().resolver, HandlerOptions{ComplexityLimit: 200, Introspection: true}, nil, q, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("introspection enabled but failed: %+v", resp.Errors)
	}
}

func TestComplexityLimit(t *testing.T) {
	resp := doWith(t, newTestEnv().resolver, HandlerOptions{ComplexityLimit: 3}, nil,
		`{ products { total page pageSize items { id name price } } }`, nil)
	if resp.code() != "COMPLEXITY_LIMIT_EXCEEDED" {
		t.Fatalf("code = %q, errors %+v", resp.code(), resp.Errors)
	}
}
