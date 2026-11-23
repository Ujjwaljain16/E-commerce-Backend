package kafka

import (
	"context"
	"net"
	"testing"
	"time"
)

// A broker that accepts the connection but never answers (e.g. a pod that is going away) must not
// block topic setup past the caller's deadline, or every consumer stalls behind it.
func TestEnsureTopicsHonoursDeadlineWhenBrokerIsSilent(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		var conns []net.Conn
		defer func() {
			for _, c := range conns {
				c.Close()
			}
		}()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- EnsureTopics(ctx, []string{ln.Addr().String()}, "orders") }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from a broker that never answers")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EnsureTopics ignored the context deadline")
	}
}
