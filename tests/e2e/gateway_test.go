//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

type gqlError struct {
	Message    string                 `json:"message"`
	Extensions map[string]interface{} `json:"extensions"`
}

type gqlResponse struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []gqlError                 `json:"errors"`
}

func (r gqlResponse) code() string {
	if len(r.Errors) == 0 {
		return ""
	}
	c, _ := r.Errors[0].Extensions["code"].(string)
	return c
}

func gql(t *testing.T, token, query string, vars map[string]interface{}) gqlResponse {
	t.Helper()
	url := os.Getenv("GATEWAY_URL")
	if url == "" {
		url = "http://localhost:8000/graphql"
	}
	body, _ := json.Marshal(map[string]interface{}{"query": query, "variables": vars})
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("gateway request: %v", err)
	}
	defer resp.Body.Close()
	var out gqlResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode (HTTP %d): %v", resp.StatusCode, err)
	}
	return out
}

func field(t *testing.T, r gqlResponse, name string, into interface{}) {
	t.Helper()
	if len(r.Errors) > 0 {
		t.Fatalf("%s failed: %+v", name, r.Errors)
	}
	if err := json.Unmarshal(r.Data[name], into); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
}

type authPayload struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	User         struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	} `json:"user"`
}

const registerMutation = `mutation($in: RegisterInput!) { register(input: $in) { accessToken refreshToken user { id role } } }`

func register(t *testing.T, email string) authPayload {
	var a authPayload
	field(t, gql(t, "", registerMutation, map[string]interface{}{
		"in": map[string]interface{}{"email": email, "password": "password123", "name": "Gateway Buyer"},
	}), "register", &a)
	return a
}

func TestGatewayCheckout(t *testing.T) {
	suffix := time.Now().UnixNano()
	alice := register(t, fmt.Sprintf("alice-%d@example.com", suffix))
	bob := register(t, fmt.Sprintf("bob-%d@example.com", suffix))
	if alice.User.Role != "USER" {
		t.Fatalf("self-registered users must be USER, got %s", alice.User.Role)
	}

	var admin authPayload
	field(t, gql(t, "", `mutation { login(email: "admin@example.com", password: "admin-password-123") { accessToken user { id role } } }`, nil), "login", &admin)
	if admin.User.Role != "ADMIN" {
		t.Fatalf("bootstrap admin should be ADMIN, got %s", admin.User.Role)
	}

	t.Run("anonymous and refresh-token callers are rejected", func(t *testing.T) {
		if c := gql(t, "", `{ me { id } }`, nil).code(); c != "UNAUTHENTICATED" {
			t.Fatalf("anonymous me: want UNAUTHENTICATED, got %q", c)
		}
		if c := gql(t, alice.RefreshToken, `{ me { id } }`, nil).code(); c != "UNAUTHENTICATED" {
			t.Fatalf("refresh token as bearer: want UNAUTHENTICATED, got %q", c)
		}
	})

	productInput := map[string]interface{}{
		"name": "Gateway Grinder", "description": "Conical burr grinder", "priceCents": 4950,
		"sku": fmt.Sprintf("GW-%d", suffix), "stock": 10, "category": "kitchen",
	}
	const createProduct = `mutation($in: CreateProductInput!) { createProduct(input: $in) { id stock } }`

	t.Run("only admins create products", func(t *testing.T) {
		if c := gql(t, alice.AccessToken, createProduct, map[string]interface{}{"in": productInput}).code(); c != "FORBIDDEN" {
			t.Fatalf("USER createProduct: want FORBIDDEN, got %q", c)
		}
	})

	var product struct {
		ID    string `json:"id"`
		Stock int    `json:"stock"`
	}
	field(t, gql(t, admin.AccessToken, createProduct, map[string]interface{}{"in": productInput}), "createProduct", &product)

	var order struct {
		ID               string `json:"id"`
		Status           string `json:"status"`
		TotalAmountCents int64  `json:"totalAmountCents"`
		UserID           string `json:"userId"`
	}
	field(t, gql(t, alice.AccessToken, `mutation($in: CreateOrderInput!) { createOrder(input: $in) { id status totalAmountCents userId } }`,
		map[string]interface{}{"in": map[string]interface{}{
			"items":           []map[string]interface{}{{"productId": product.ID, "quantity": 2}},
			"shippingAddress": "221B Baker Street", "billingAddress": "221B Baker Street", "paymentMethod": "card",
		}}), "createOrder", &order)
	if order.TotalAmountCents != 9900 || order.UserID != alice.User.ID {
		t.Fatalf("unexpected order %+v", order)
	}

	t.Run("order is confirmed once payment succeeds", func(t *testing.T) {
		eventually(t, "CONFIRMED via gateway", func() (bool, string) {
			var o struct {
				Status string `json:"status"`
			}
			field(t, gql(t, alice.AccessToken, `query($id: ID!) { order(id: $id) { status } }`, map[string]interface{}{"id": order.ID}), "order", &o)
			return o.Status == "CONFIRMED", o.Status
		})
		var p struct {
			Status      string `json:"status"`
			AmountCents int64  `json:"amountCents"`
		}
		field(t, gql(t, alice.AccessToken, `query($id: ID!) { paymentForOrder(orderId: $id) { status amountCents } }`, map[string]interface{}{"id": order.ID}), "paymentForOrder", &p)
		if p.Status != "SUCCEEDED" || p.AmountCents != 9900 {
			t.Fatalf("unexpected payment %+v", p)
		}
	})

	t.Run("other users cannot see the order", func(t *testing.T) {
		if c := gql(t, bob.AccessToken, `query($id: ID!) { order(id: $id) { id } }`, map[string]interface{}{"id": order.ID}).code(); c != "NOT_FOUND" {
			t.Fatalf("bob reading alice's order: want NOT_FOUND, got %q", c)
		}
		if c := gql(t, bob.AccessToken, `mutation($id: ID!) { cancelOrder(id: $id) }`, map[string]interface{}{"id": order.ID}).code(); c != "NOT_FOUND" {
			t.Fatalf("bob cancelling alice's order: want NOT_FOUND, got %q", c)
		}
	})

	t.Run("notifications are visible to the owner", func(t *testing.T) {
		eventually(t, "notifications via gateway", func() (bool, string) {
			var page struct {
				Total int `json:"total"`
			}
			field(t, gql(t, alice.AccessToken, `{ myNotifications { total } }`, nil), "myNotifications", &page)
			return page.Total >= 2, fmt.Sprint(page.Total)
		})
	})
}

func TestGatewaySessions(t *testing.T) {
	user := register(t, fmt.Sprintf("sessions-%d@example.com", time.Now().UnixNano()))
	const refresh = `mutation($t: String!) { refreshToken(refreshToken: $t) { accessToken refreshToken } }`

	var rotated authPayload
	field(t, gql(t, "", refresh, map[string]interface{}{"t": user.RefreshToken}), "refreshToken", &rotated)
	if rotated.RefreshToken == user.RefreshToken {
		t.Fatal("refresh should rotate the refresh token")
	}

	t.Run("reusing a rotated refresh token signs out every session", func(t *testing.T) {
		if c := gql(t, "", refresh, map[string]interface{}{"t": user.RefreshToken}).code(); c != "UNAUTHENTICATED" {
			t.Fatalf("reuse: want UNAUTHENTICATED, got %q", c)
		}
		if c := gql(t, "", refresh, map[string]interface{}{"t": rotated.RefreshToken}).code(); c != "UNAUTHENTICATED" {
			t.Fatalf("after reuse the newest token must be revoked, got %q", c)
		}
	})

	t.Run("logout revokes the refresh token", func(t *testing.T) {
		var a authPayload
		field(t, gql(t, "", `mutation { login(email: "admin@example.com", password: "admin-password-123") { accessToken refreshToken } }`, nil), "login", &a)
		var ok bool
		field(t, gql(t, a.AccessToken, `mutation($t: String!) { logout(refreshToken: $t) }`, map[string]interface{}{"t": a.RefreshToken}), "logout", &ok)
		if !ok {
			t.Fatal("logout returned false")
		}
		if c := gql(t, "", refresh, map[string]interface{}{"t": a.RefreshToken}).code(); c != "UNAUTHENTICATED" {
			t.Fatalf("refresh after logout: want UNAUTHENTICATED, got %q", c)
		}
	})
}
