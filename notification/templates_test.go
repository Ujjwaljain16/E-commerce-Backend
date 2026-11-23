package notification

import (
	"testing"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderOrderCreated(t *testing.T) {
	subject, body, err := render(events.TopicOrderCreated, messageData{
		Name:    "Asha",
		OrderID: "order-42",
		Total:   formatTotal(12050, "usd"),
		Items:   []events.OrderItem{{ProductName: "Keyboard", Quantity: 2}},
	})
	require.NoError(t, err)
	assert.Equal(t, "We received your order order-42", subject)
	assert.Contains(t, body, "Hi Asha,")
	assert.Contains(t, body, "order order-42 for $120.50")
	assert.Contains(t, body, "2 x Keyboard")
}

func TestRenderOrderConfirmed(t *testing.T) {
	subject, body, err := render(events.TopicOrderConfirmed, messageData{OrderID: "order-42", Total: formatTotal(9900, "")})
	require.NoError(t, err)
	assert.Equal(t, "Your order order-42 is confirmed", subject)
	assert.Contains(t, body, "Hi there,", "falls back when the name is unknown")
	assert.Contains(t, body, "order-42 ($99.00) is confirmed")
}

func TestRenderOrderCancelled(t *testing.T) {
	subject, body, err := render(events.TopicOrderCancelled, messageData{Name: "Asha", OrderID: "order-42", Reason: "payment declined"})
	require.NoError(t, err)
	assert.Equal(t, "Your order order-42 was cancelled", subject)
	assert.Contains(t, body, "has been cancelled (payment declined).")

	_, body, err = render(events.TopicOrderCancelled, messageData{OrderID: "order-42"})
	require.NoError(t, err)
	assert.Contains(t, body, "has been cancelled.")
}

func TestRenderUnknownTopic(t *testing.T) {
	_, _, err := render("order.shipped", messageData{})
	assert.Error(t, err)
}

func TestFormatTotal(t *testing.T) {
	assert.Equal(t, "", formatTotal(0, "USD"))
	assert.Equal(t, "", formatTotal(-5, "USD"))
	assert.Equal(t, "$10.00", formatTotal(1000, ""))
	assert.Equal(t, "$0.07", formatTotal(7, "usd"))
	assert.Equal(t, "$39.98", formatTotal(3998, "USD"))
	assert.Equal(t, "$1234567.89", formatTotal(123456789, "USD"))
	assert.Equal(t, "10.50 EUR", formatTotal(1050, "eur"))
}
