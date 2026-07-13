// Package mailer sends transactional emails via SMTP. In local development it
// points at Mailpit (http://localhost:8025) so messages are captured and
// inspectable rather than delivered.
package mailer

import (
	"context"
	"fmt"
	"strings"

	gomail "github.com/wneessen/go-mail"

	"pulsar/internal/config"
)

// Mailer abstracts outbound email so tests can swap implementations.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Message is a single outbound email.
type Message struct {
	To      string
	Subject string
	Plain   string
	HTML    string
}

// SMTPMailer is the production implementation backed by go-mail.
type SMTPMailer struct {
	from string
	host string
	port int
	user string
	pass string
}

// New builds an SMTPMailer from configuration.
func New(cfg config.SMTPConfig) *SMTPMailer {
	return &SMTPMailer{
		from: cfg.From, host: cfg.Host, port: cfg.Port,
		user: cfg.Username, pass: cfg.Password,
	}
}

// Send dispatches a message through SMTP.
//
// Auth behaviour: when username/password are empty (the common local/dev case
// against Mailpit) we skip SMTP AUTH entirely, which Mailpit accepts by default.
// When credentials are present we use AUTH PLAIN.
//
// TLS is selected automatically by port:
//   - port 465  → implicit SSL  (WithSSL — wraps the connection in TLS from the start)
//   - port 587  → STARTTLS mandatory (TLSMandatory — upgrades plain→TLS via STARTTLS)
//   - anything  → no TLS (Mailpit on 1025, etc.)
func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	em := gomail.NewMsg()
	if err := em.From(m.from); err != nil {
		return fmt.Errorf("invalid from: %w", err)
	}
	if err := em.To(msg.To); err != nil {
		return fmt.Errorf("invalid to: %w", err)
	}
	em.Subject(msg.Subject)
	if msg.HTML != "" {
		em.SetBodyString(gomail.TypeTextHTML, msg.HTML)
	}
	if msg.Plain != "" {
		em.AddAlternativeString(gomail.TypeTextPlain, msg.Plain)
	}

	// Base options — port is always set.
	opts := []gomail.Option{gomail.WithPort(m.port)}

	// TLS mode depends on the well-known port convention:
	//   465, 2465 → implicit SSL  (WithSSL — TLS from the first byte)
	//   587, 2587 → STARTTLS mandatory
	//   any       → no TLS (local Mailpit, etc.)
	switch m.port {
	case 465, 2465:
		opts = append(opts, gomail.WithSSL())
	case 1025, 1026:
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	default:
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
	}

	if m.user != "" || m.pass != "" {
		opts = append(opts,
			gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(m.user),
			gomail.WithPassword(m.pass),
		)
	}
	c, err := gomail.NewClient(m.host, opts...)
	if err != nil {
		return fmt.Errorf("create smtp client: %w", err)
	}
	if err := c.DialAndSend(em); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

// LogMailer captures messages in-memory; used by tests and when SMTP is not
// reachable (local-only fallback). Never use in production.
type LogMailer struct {
	Messages []Message
}

// NewLogMailer returns an in-memory mailer.
func NewLogMailer() *LogMailer { return &LogMailer{} }

// Send appends the message to the in-memory log without network I/O.
func (m *LogMailer) Send(_ context.Context, msg Message) error {
	m.Messages = append(m.Messages, msg)
	return nil
}

// Last returns the most recently sent message, or empty.
func (m *LogMailer) Last() Message {
	if len(m.Messages) == 0 {
		return Message{}
	}
	return m.Messages[len(m.Messages)-1]
}

// ParseAddress extracts the email portion of a "Name <addr>" string.
func ParseAddress(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "<"); i >= 0 {
		if j := strings.Index(s, ">"); j > i {
			return strings.TrimSpace(s[i+1 : j])
		}
	}
	return s
}
