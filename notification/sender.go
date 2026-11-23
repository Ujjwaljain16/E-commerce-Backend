package notification

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
)

// Email is a plain-text message to one recipient.
type Email struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers emails.
type Sender interface {
	Send(ctx context.Context, e Email) error
}

// LogSender writes emails to the log instead of delivering them, for local development.
type LogSender struct {
	log *logger.Logger
}

// NewLogSender creates a LogSender.
func NewLogSender(log *logger.Logger) *LogSender {
	return &LogSender{log: log}
}

// Send implements Sender.
func (s *LogSender) Send(ctx context.Context, e Email) error {
	s.log.Info(ctx, "Email sent", map[string]interface{}{"to": e.To, "subject": e.Subject, "body": e.Body})
	return nil
}

// SMTPConfig configures SMTPSender.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// SMTPSender delivers email through an SMTP server, upgrading to TLS with STARTTLS when offered.
type SMTPSender struct {
	cfg SMTPConfig
}

// NewSMTPSender validates cfg and creates an SMTPSender.
func NewSMTPSender(cfg SMTPConfig) (*SMTPSender, error) {
	if cfg.Host == "" {
		return nil, errors.New("smtp host is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return nil, fmt.Errorf("invalid smtp from address %q: %w", cfg.From, err)
	}
	return &SMTPSender{cfg: cfg}, nil
}

// smtpTimeout bounds a whole delivery; net/smtp has no deadlines of its own and would otherwise hang the consumer.
const smtpTimeout = 30 * time.Second

// Send implements Sender.
func (s *SMTPSender) Send(ctx context.Context, e Email) error {
	from, _ := mail.ParseAddress(s.cfg.From)
	to, err := mail.ParseAddress(e.To)
	if err != nil {
		return fmt.Errorf("invalid recipient %q: %w", e.To, err)
	}
	msg := buildMessage(from, to, e, time.Now())

	ctx, cancel := context.WithTimeout(ctx, smtpTimeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port)))
	if err != nil {
		return fmt.Errorf("connect to smtp server: %w", err)
	}
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return err
	}

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if s.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return c.Quit()
}

// buildMessage renders RFC 5322 headers and body. The subject is stripped of line breaks so event
// data can never inject extra headers.
func buildMessage(from, to *mail.Address, e Email, now time.Time) []byte {
	subject := strings.Join(strings.Fields(e.Subject), " ")
	body := strings.ReplaceAll(strings.ReplaceAll(e.Body, "\r\n", "\n"), "\n", "\r\n")

	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from.String())
	fmt.Fprintf(&b, "To: %s\r\n", to.String())
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", now.Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.Bytes()
}
