package catalog

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/cache"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/metrics"
)

const productCacheTTL = 5 * time.Minute

// cachedRepository caches single-product reads and invalidates on every write that changes a
// product (including stock reservations). Cache failures never fail a request; they fall
// through to Postgres.
type cachedRepository struct {
	Repository
	cache cache.Cache
	log   *logger.Logger
}

// NewCachedRepository wraps repo with a read-through product cache.
func NewCachedRepository(repo Repository, c cache.Cache, log *logger.Logger) Repository {
	return &cachedRepository{Repository: repo, cache: c, log: log}
}

func productKey(id string) string { return "catalog:product:" + id }

func (r *cachedRepository) GetByID(ctx context.Context, id string) (*Product, error) {
	if raw, ok, err := r.cache.Get(ctx, productKey(id)); err != nil {
		r.log.Warn(ctx, "cache get failed", map[string]interface{}{"error": err.Error()})
	} else if ok {
		var p Product
		if json.Unmarshal(raw, &p) == nil {
			metrics.CacheHitsTotal.WithLabelValues("catalog-service", "product").Inc()
			return &p, nil
		}
	}
	metrics.CacheMissesTotal.WithLabelValues("catalog-service", "product").Inc()

	p, err := r.Repository.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if raw, err := json.Marshal(p); err == nil {
		if err := r.cache.Set(ctx, productKey(id), raw, productCacheTTL); err != nil {
			r.log.Warn(ctx, "cache set failed", map[string]interface{}{"error": err.Error()})
		}
	}
	return p, nil
}

func (r *cachedRepository) Update(ctx context.Context, product *Product) (*Product, error) {
	p, err := r.Repository.Update(ctx, product)
	r.invalidate(ctx, product.ID)
	return p, err
}

func (r *cachedRepository) Delete(ctx context.Context, id string) error {
	err := r.Repository.Delete(ctx, id)
	r.invalidate(ctx, id)
	return err
}

func (r *cachedRepository) ReserveStock(ctx context.Context, orderID string, items []StockItem) ([]ReservedItem, error) {
	reserved, err := r.Repository.ReserveStock(ctx, orderID, items)
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ProductID
	}
	r.invalidate(ctx, ids...)
	return reserved, err
}

func (r *cachedRepository) ReleaseStock(ctx context.Context, orderID string) ([]string, error) {
	ids, err := r.Repository.ReleaseStock(ctx, orderID)
	r.invalidate(ctx, ids...)
	return ids, err
}

func (r *cachedRepository) CommitReservation(ctx context.Context, orderID string) ([]string, error) {
	ids, err := r.Repository.CommitReservation(ctx, orderID)
	r.invalidate(ctx, ids...)
	return ids, err
}

func (r *cachedRepository) ExpireReservation(ctx context.Context, orderID string) ([]string, error) {
	ids, err := r.Repository.ExpireReservation(ctx, orderID)
	r.invalidate(ctx, ids...)
	return ids, err
}

func (r *cachedRepository) invalidate(ctx context.Context, ids ...string) {
	if len(ids) == 0 {
		return
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = productKey(id)
	}
	if err := r.cache.Delete(ctx, keys...); err != nil {
		r.log.Warn(ctx, "cache invalidation failed", map[string]interface{}{"error": err.Error()})
	}
}
