package graph

import (
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
)

// HandlerOptions controls what the GraphQL endpoint allows.
type HandlerOptions struct {
	ComplexityLimit int
	Introspection   bool
	Logger          *logger.Logger
}

// NewHandler builds the GraphQL HTTP handler. Only plain GET/POST transports
// are enabled: no websockets, multipart uploads or batching.
func NewHandler(r *Resolver, opts HandlerOptions) *handler.Server {
	srv := handler.New(NewExecutableSchema(Config{Resolvers: r}))
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})

	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	if opts.Introspection {
		srv.Use(extension.Introspection{})
	}
	srv.Use(extension.FixedComplexityLimit(opts.ComplexityLimit))

	srv.SetErrorPresenter(ErrorPresenter(opts.Logger))
	srv.SetRecoverFunc(RecoverFunc(opts.Logger))
	return srv
}
