package graph

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
)

// Error codes returned in extensions.code.
const (
	CodeBadUserInput       = "BAD_USER_INPUT"
	CodeNotFound           = "NOT_FOUND"
	CodeConflict           = "CONFLICT"
	CodeUnauthenticated    = "UNAUTHENTICATED"
	CodeForbidden          = "FORBIDDEN"
	CodeFailedPrecondition = "FAILED_PRECONDITION"
	CodeUnavailable        = "UNAVAILABLE"
	CodeInternal           = "INTERNAL"
)

const internalMessage = "internal server error"

// clientError is an error whose message is safe to show to API clients.
type clientError struct {
	code    string
	message string
}

func (e *clientError) Error() string { return e.message }

func newClientError(code, message string) error {
	return &clientError{code: code, message: message}
}

var (
	errUnauthenticated = newClientError(CodeUnauthenticated, "authentication required")
	errForbidden       = newClientError(CodeForbidden, "admin access required")
	errOrderNotFound   = newClientError(CodeNotFound, "order not found")
)

func badInput(format string, args ...any) error {
	return newClientError(CodeBadUserInput, fmt.Sprintf(format, args...))
}

// classify maps an error to a code and a client-safe message. internal is
// true when the original error text must stay in the logs only.
func classify(err error) (code, message string, internal bool) {
	var ce *clientError
	if errors.As(err, &ce) {
		return ce.code, ce.message, false
	}

	st, ok := status.FromError(err)
	if !ok {
		return CodeInternal, internalMessage, true
	}
	switch st.Code() {
	case codes.InvalidArgument:
		return CodeBadUserInput, st.Message(), false
	case codes.NotFound:
		return CodeNotFound, st.Message(), false
	case codes.AlreadyExists:
		return CodeConflict, st.Message(), false
	case codes.Unauthenticated:
		return CodeUnauthenticated, st.Message(), false
	case codes.PermissionDenied:
		return CodeForbidden, st.Message(), false
	case codes.FailedPrecondition:
		return CodeFailedPrecondition, st.Message(), false
	case codes.Unavailable, codes.DeadlineExceeded:
		return CodeUnavailable, "service temporarily unavailable", true
	default:
		return CodeInternal, internalMessage, true
	}
}

// ErrorPresenter turns resolver errors into GraphQL errors with an
// extensions.code, logging anything that is not shown to the client.
func ErrorPresenter(log *logger.Logger) graphql.ErrorPresenterFunc {
	return func(ctx context.Context, err error) *gqlerror.Error {
		var gerr *gqlerror.Error
		if !errors.As(err, &gerr) {
			gerr = gqlerror.WrapPath(graphql.GetPath(ctx), err)
		}
		// Errors built by gqlgen itself (parse, validation, complexity) carry
		// no wrapped cause and are already meant for the client.
		if gerr.Err == nil {
			return gerr
		}

		code, message, internal := classify(gerr.Err)
		if internal {
			log.Error(ctx, "Request failed", map[string]interface{}{
				"error": gerr.Err.Error(),
				"path":  gerr.Path.String(),
				"code":  code,
			})
		}
		return &gqlerror.Error{
			Err:        gerr.Err,
			Message:    message,
			Path:       gerr.Path,
			Locations:  gerr.Locations,
			Extensions: map[string]interface{}{"code": code},
		}
	}
}

// RecoverFunc logs a resolver panic with its stack and hides it from the client.
func RecoverFunc(log *logger.Logger) graphql.RecoverFunc {
	return func(ctx context.Context, p any) error {
		log.Error(ctx, "Resolver panicked", map[string]interface{}{
			"panic": fmt.Sprint(p),
			"stack": string(debug.Stack()),
		})
		return newClientError(CodeInternal, internalMessage)
	}
}
