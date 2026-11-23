package graph

import (
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	accountpb "github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	catalogpb "github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	notificationpb "github.com/Ujjwaljain16/E-commerce-Backend/notification/pb"
	orderpb "github.com/Ujjwaljain16/E-commerce-Backend/order/pb"
	paymentpb "github.com/Ujjwaljain16/E-commerce-Backend/payment/pb"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// pagination validates paging arguments up front so a bad request fails
// with BAD_USER_INPUT instead of being silently clamped by a backend.
func pagination(page, pageSize *int32) (int32, int32, error) {
	p, size := int32(1), int32(defaultPageSize)
	if page != nil {
		p = *page
	}
	if pageSize != nil {
		size = *pageSize
	}
	if p < 1 {
		return 0, 0, badInput("page must be at least 1")
	}
	if size < 1 || size > maxPageSize {
		return 0, 0, badInput("pageSize must be between 1 and %d", maxPageSize)
	}
	return p, size, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toTime(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime()
	return &t
}

func toUser(u *accountpb.User) *User {
	if u == nil {
		return nil
	}
	return &User{
		ID:         u.GetId(),
		Email:      u.GetEmail(),
		Name:       u.GetName(),
		Phone:      u.GetPhone(),
		Role:       u.GetRole(),
		IsVerified: u.GetIsVerified(),
		IsActive:   u.GetIsActive(),
		CreatedAt:  toTime(u.GetCreatedAt()),
		UpdatedAt:  toTime(u.GetUpdatedAt()),
	}
}

func toProduct(p *catalogpb.Product) *Product {
	if p == nil {
		return nil
	}
	images := p.GetImages()
	if images == nil {
		images = []string{}
	}
	return &Product{
		ID:          p.GetId(),
		Name:        p.GetName(),
		Description: p.GetDescription(),
		PriceCents:  p.GetPriceCents(),
		Sku:         p.GetSku(),
		Stock:       p.GetStock(),
		Images:      images,
		Category:    p.GetCategory(),
		CreatedAt:   toTime(p.GetCreatedAt()),
		UpdatedAt:   toTime(p.GetUpdatedAt()),
	}
}

func toProducts(in []*catalogpb.Product) []*Product {
	out := make([]*Product, 0, len(in))
	for _, p := range in {
		out = append(out, toProduct(p))
	}
	return out
}

// Generated enum values match the proto names, so the mapping is by name.
func toOrderStatus(s orderpb.OrderStatus) OrderStatus {
	return OrderStatus(s.String())
}

func toProtoOrderStatus(s OrderStatus) orderpb.OrderStatus {
	return orderpb.OrderStatus(orderpb.OrderStatus_value[string(s)])
}

func toOrder(o *orderpb.Order) *Order {
	if o == nil {
		return nil
	}
	items := make([]*OrderItem, 0, len(o.GetItems()))
	for _, it := range o.GetItems() {
		items = append(items, &OrderItem{
			ID:            it.GetId(),
			ProductID:     it.GetProductId(),
			ProductName:   it.GetProductName(),
			Quantity:      it.GetQuantity(),
			PriceCents:    it.GetPriceCents(),
			SubtotalCents: it.GetSubtotalCents(),
		})
	}
	return &Order{
		ID:               o.GetId(),
		UserID:           o.GetUserId(),
		Status:           toOrderStatus(o.GetStatus()),
		Items:            items,
		TotalAmountCents: o.GetTotalAmountCents(),
		ShippingAddress:  o.GetShippingAddress(),
		BillingAddress:   o.GetBillingAddress(),
		PaymentMethod:    o.GetPaymentMethod(),
		CreatedAt:        toTime(o.GetCreatedAt()),
		UpdatedAt:        toTime(o.GetUpdatedAt()),
	}
}

func toOrders(in []*orderpb.Order) []*Order {
	out := make([]*Order, 0, len(in))
	for _, o := range in {
		out = append(out, toOrder(o))
	}
	return out
}

func toPaymentStatus(s paymentpb.PaymentStatus) PaymentStatus {
	return PaymentStatus(strings.TrimPrefix(s.String(), "PAYMENT_STATUS_"))
}

func toPayment(p *paymentpb.Payment) *Payment {
	if p == nil {
		return nil
	}
	return &Payment{
		ID:              p.GetId(),
		OrderID:         p.GetOrderId(),
		UserID:          p.GetUserId(),
		AmountCents:     p.GetAmountCents(),
		Currency:        p.GetCurrency(),
		Status:          toPaymentStatus(p.GetStatus()),
		Method:          p.GetMethod(),
		ProviderRef:     p.GetProviderRef(),
		FailureReason:   p.GetFailureReason(),
		Provider:        p.GetProvider(),
		ProviderOrderID: p.GetProviderOrderId(),
		CreatedAt:       toTime(p.GetCreatedAt()),
		UpdatedAt:       toTime(p.GetUpdatedAt()),
	}
}

func toNotifications(in []*notificationpb.Notification) []*Notification {
	out := make([]*Notification, 0, len(in))
	for _, n := range in {
		out = append(out, &Notification{
			ID:        n.GetId(),
			Type:      n.GetType(),
			Channel:   n.GetChannel(),
			Recipient: n.GetRecipient(),
			Subject:   n.GetSubject(),
			Body:      n.GetBody(),
			Status:    n.GetStatus(),
			CreatedAt: toTime(n.GetCreatedAt()),
		})
	}
	return out
}
