// Command load drives the GraphQL gateway with a mix of catalog reads, searches and checkouts and
// reports throughput and latency percentiles per operation.
//
//	RATE_LIMIT_RPS=100000 RATE_LIMIT_BURST=100000 docker compose up -d   # one client IP, so lift the per-IP limit
//	go run ./tests/load -users 20 -duration 30s
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

type result struct {
	op      string
	latency time.Duration
	err     bool
}

type client struct {
	url  string
	http *http.Client
}

func (c *client) do(token, query string, vars map[string]interface{}, out interface{}) error {
	body, _ := json.Marshal(map[string]interface{}{"query": query, "variables": vars})
	req, _ := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var r struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}
	if len(r.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", r.Errors[0])
	}
	if out != nil {
		return json.Unmarshal(r.Data, out)
	}
	return nil
}

func main() {
	url := flag.String("url", "http://localhost:8000/graphql", "gateway GraphQL endpoint")
	users := flag.Int("users", 20, "concurrent virtual users")
	duration := flag.Duration("duration", 30*time.Second, "how long to run")
	adminEmail := flag.String("admin-email", "admin@example.com", "admin login used to create test products")
	adminPassword := flag.String("admin-password", "admin-password-123", "admin password")
	flag.Parse()

	c := &client{url: *url, http: &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{MaxIdleConnsPerHost: *users * 2},
	}}
	run := time.Now().UnixNano()

	var admin struct {
		Login struct{ AccessToken string } `json:"login"`
	}
	if err := c.do("", `mutation($e: String!, $p: String!) { login(email: $e, password: $p) { accessToken } }`,
		map[string]interface{}{"e": *adminEmail, "p": *adminPassword}, &admin); err != nil {
		fail("admin login: %v", err)
	}
	var productIDs []string
	for i := 0; i < 20; i++ {
		var p struct {
			CreateProduct struct{ ID string } `json:"createProduct"`
		}
		err := c.do(admin.Login.AccessToken, `mutation($in: CreateProductInput!) { createProduct(input: $in) { id } }`,
			map[string]interface{}{"in": map[string]interface{}{
				"name": fmt.Sprintf("Load test item %d", i), "description": "stainless steel kettle for load testing",
				"priceCents": 1000 + i, "sku": fmt.Sprintf("LOAD-%d-%d", run, i), "stock": 1000000, "category": "load",
			}}, &p)
		if err != nil {
			fail("create product: %v", err)
		}
		productIDs = append(productIDs, p.CreateProduct.ID)
	}

	results := make(chan result, 4096)
	var collected []result
	done := make(chan struct{})
	go func() {
		for r := range results {
			collected = append(collected, r)
		}
		close(done)
	}()

	deadline := time.Now().Add(*duration)
	var wg sync.WaitGroup
	for u := 0; u < *users; u++ {
		wg.Add(1)
		go func(u int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(u) + run))
			var reg struct {
				Register struct{ AccessToken string } `json:"register"`
			}
			if err := c.do("", `mutation($in: RegisterInput!) { register(input: $in) { accessToken } }`,
				map[string]interface{}{"in": map[string]interface{}{
					"email": fmt.Sprintf("load-%d-%d@example.com", run, u), "password": "password123", "name": "Load User",
				}}, &reg); err != nil {
				results <- result{op: "register", err: true}
				return
			}
			token := reg.Register.AccessToken

			for time.Now().Before(deadline) {
				id := productIDs[rng.Intn(len(productIDs))]
				var op, q string
				var vars map[string]interface{}
				switch n := rng.Intn(100); {
				case n < 70:
					op, q, vars = "product", `query($id: ID!) { product(id: $id) { id name priceCents stock } }`, map[string]interface{}{"id": id}
				case n < 90:
					op, q, vars = "searchProducts", `{ searchProducts(query: "kettle", pageSize: 10) { total items { id } } }`, nil
				default:
					op = "createOrder"
					q = `mutation($in: CreateOrderInput!) { createOrder(input: $in) { id status } }`
					vars = map[string]interface{}{"in": map[string]interface{}{
						"items":           []map[string]interface{}{{"productId": id, "quantity": 1}},
						"shippingAddress": "1 Load Street", "billingAddress": "1 Load Street", "paymentMethod": "card",
					}}
				}
				start := time.Now()
				err := c.do(token, q, vars, nil)
				results <- result{op: op, latency: time.Since(start), err: err != nil}
			}
		}(u)
	}
	wg.Wait()
	close(results)
	<-done
	report(collected, *duration, *users)
}

func report(rs []result, d time.Duration, users int) {
	byOp := map[string][]time.Duration{}
	errs := map[string]int{}
	for _, r := range rs {
		if r.err {
			errs[r.op]++
			continue
		}
		byOp[r.op] = append(byOp[r.op], r.latency)
	}
	ops := make([]string, 0, len(byOp))
	for op := range byOp {
		ops = append(ops, op)
	}
	sort.Strings(ops)

	fmt.Printf("%d users for %s\n\n", users, d)
	fmt.Printf("%-16s %8s %8s %8s %8s %8s %7s\n", "operation", "ok", "req/s", "p50", "p95", "p99", "errors")
	total := 0
	for _, op := range ops {
		l := byOp[op]
		sort.Slice(l, func(i, j int) bool { return l[i] < l[j] })
		total += len(l)
		fmt.Printf("%-16s %8d %8.1f %8s %8s %8s %7d\n", op, len(l), float64(len(l))/d.Seconds(),
			pct(l, 50), pct(l, 95), pct(l, 99), errs[op])
	}
	fmt.Printf("\ntotal %d successful requests, %.1f req/s\n", total, float64(total)/d.Seconds())
	for op, n := range errs {
		if _, ok := byOp[op]; !ok {
			fmt.Printf("%s: %d errors\n", op, n)
		}
	}
}

func pct(sorted []time.Duration, p int) string {
	if len(sorted) == 0 {
		return "-"
	}
	i := (len(sorted)*p + 99) / 100
	if i > 0 {
		i--
	}
	return sorted[i].Round(100 * time.Microsecond).String()
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
