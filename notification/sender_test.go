package notification

import (
	"bufio"
	"context"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMessageStripsHeaderInjection(t *testing.T) {
	from := &mail.Address{Name: "Shop", Address: "orders@example.com"}
	to := &mail.Address{Address: "asha@example.com"}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	msg := string(buildMessage(from, to, Email{
		Subject: "Order 1\r\nBcc: attacker@example.com",
		Body:    "line one\nline two\n",
	}, now))

	headers, body, ok := strings.Cut(msg, "\r\n\r\n")
	require.True(t, ok)
	assert.Contains(t, headers, "Subject: Order 1 Bcc: attacker@example.com")
	assert.NotContains(t, headers, "\r\nBcc:")
	assert.Contains(t, headers, `From: "Shop" <orders@example.com>`)
	assert.Contains(t, headers, "To: <asha@example.com>")
	assert.Contains(t, headers, "Date: Fri, 02 Jan 2026 03:04:05 +0000")
	assert.Equal(t, "line one\r\nline two\r\n", body)
}

func TestNewSMTPSenderValidatesConfig(t *testing.T) {
	_, err := NewSMTPSender(SMTPConfig{From: "orders@example.com"})
	assert.Error(t, err, "host is required")

	_, err = NewSMTPSender(SMTPConfig{Host: "smtp.example.com", From: "not an address"})
	assert.Error(t, err)

	s, err := NewSMTPSender(SMTPConfig{Host: "smtp.example.com", From: "orders@example.com"})
	require.NoError(t, err)
	assert.Equal(t, 587, s.cfg.Port)
}

// fakeSMTPServer accepts one plain-text session and returns the DATA it received.
func fakeSMTPServer(t *testing.T) (port int, received <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	out := make(chan string, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		reply("220 localhost ESMTP")
		var data strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				reply("250-localhost")
				reply("250 8BITMIME")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				reply("250 OK")
			case cmd == "DATA":
				reply("354 go ahead")
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					data.WriteString(l)
				}
				out <- data.String()
				reply("250 queued")
			case cmd == "QUIT":
				reply("221 bye")
				return
			default:
				reply("502 not implemented")
			}
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ = strconv.Atoi(portStr)
	return port, out
}

func TestSMTPSenderDeliversMessage(t *testing.T) {
	port, received := fakeSMTPServer(t)
	s, err := NewSMTPSender(SMTPConfig{Host: "127.0.0.1", Port: port, From: "orders@example.com"})
	require.NoError(t, err)

	err = s.Send(context.Background(), Email{To: "asha@example.com", Subject: "Hello", Body: "Your order is confirmed.\n"})
	require.NoError(t, err)

	select {
	case data := <-received:
		assert.Contains(t, data, "Subject: Hello\r\n")
		assert.Contains(t, data, "Your order is confirmed.\r\n")
	case <-time.After(5 * time.Second):
		t.Fatal("smtp server received nothing")
	}
}

func TestSMTPSenderRejectsBadRecipient(t *testing.T) {
	s, err := NewSMTPSender(SMTPConfig{Host: "127.0.0.1", Port: 1, From: "orders@example.com"})
	require.NoError(t, err)
	assert.Error(t, s.Send(context.Background(), Email{To: "nope"}))
}
