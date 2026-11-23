package notification

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
)

// messageData is what the email templates can refer to.
type messageData struct {
	Name    string
	OrderID string
	Total   string
	Reason  string
	Items   []events.OrderItem
}

type messageTemplate struct {
	subject *template.Template
	body    *template.Template
}

func newMessageTemplate(name, subject, body string) messageTemplate {
	return messageTemplate{
		subject: template.Must(template.New(name + ".subject").Parse(subject)),
		body:    template.Must(template.New(name + ".body").Parse(body)),
	}
}

var messageTemplates = map[string]messageTemplate{
	events.TopicOrderCreated: newMessageTemplate(events.TopicOrderCreated,
		`We received your order {{.OrderID}}`,
		`Hi {{.Name}},

Thanks for shopping with us! We've received order {{.OrderID}}{{if .Total}} for {{.Total}}{{end}} and are processing your payment.
{{if .Items}}
Items:
{{range .Items}}  - {{.Quantity}} x {{.ProductName}}
{{end}}{{end}}
We'll email you again as soon as it's confirmed.
`),
	events.TopicOrderConfirmed: newMessageTemplate(events.TopicOrderConfirmed,
		`Your order {{.OrderID}} is confirmed`,
		`Hi {{.Name}},

Good news: your payment went through and order {{.OrderID}}{{if .Total}} ({{.Total}}){{end}} is confirmed.
We'll let you know when it ships.
`),
	events.TopicOrderCancelled: newMessageTemplate(events.TopicOrderCancelled,
		`Your order {{.OrderID}} was cancelled`,
		`Hi {{.Name}},

Your order {{.OrderID}} has been cancelled{{if .Reason}} ({{.Reason}}){{end}}.
If you were charged, the refund is on its way.
`),
}

func render(topic string, data messageData) (subject, body string, err error) {
	t, ok := messageTemplates[topic]
	if !ok {
		return "", "", fmt.Errorf("no template for %s", topic)
	}
	if data.Name == "" {
		data.Name = "there"
	}
	var sb, bb strings.Builder
	if err := t.subject.Execute(&sb, data); err != nil {
		return "", "", fmt.Errorf("render %s subject: %w", topic, err)
	}
	if err := t.body.Execute(&bb, data); err != nil {
		return "", "", fmt.Errorf("render %s body: %w", topic, err)
	}
	return sb.String(), bb.String(), nil
}

// formatTotal renders an amount in cents for humans, e.g. 3998 USD -> "$39.98". Events without a
// currency are in USD. Integer math only, so the email always matches what was charged.
func formatTotal(cents int64, currency string) string {
	if cents <= 0 {
		return ""
	}
	amount := fmt.Sprintf("%d.%02d", cents/100, cents%100)
	currency = strings.ToUpper(currency)
	if currency == "" || currency == "USD" {
		return "$" + amount
	}
	return amount + " " + currency
}
