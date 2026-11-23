package server

import (
	"errors"
	"io"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	paymentpb "github.com/Ujjwaljain16/E-commerce-Backend/payment/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
)

// maxWebhookBytes caps Razorpay webhook bodies; real ones are a few KB.
const maxWebhookBytes = 256 << 10

// RazorpayWebhook hands Razorpay webhooks to the payment service. The body is forwarded byte for
// byte because the signature covers the exact bytes Razorpay sent. Anything other than 2xx makes
// Razorpay retry, so only temporary failures answer 5xx.
func RazorpayWebhook(payments paymentpb.PaymentServiceClient, log *logger.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signature := r.Header.Get("X-Razorpay-Signature")
		if signature == "" {
			writeStatus(w, http.StatusBadRequest, "missing X-Razorpay-Signature")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeStatus(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeStatus(w, http.StatusBadRequest, "could not read body")
			return
		}

		_, err = payments.HandleProviderWebhook(r.Context(), &paymentpb.HandleProviderWebhookRequest{
			Body: body, Signature: signature,
		})
		switch status.Code(err) {
		case codes.OK:
			writeStatus(w, http.StatusOK, "ok")
		case codes.InvalidArgument:
			writeStatus(w, http.StatusBadRequest, "invalid signature")
		case codes.FailedPrecondition:
			writeStatus(w, http.StatusNotFound, "razorpay payments are not enabled")
		default:
			log.Error(r.Context(), "Razorpay webhook failed", map[string]interface{}{
				"error": err.Error(), "event_id": r.Header.Get("X-Razorpay-Event-Id"),
			})
			writeStatus(w, http.StatusServiceUnavailable, "try again later")
		}
	})
}
