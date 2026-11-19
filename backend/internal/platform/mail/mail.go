// Package mail sends transactional e-mail.
package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
)

const (
	queueSize   = 256
	workers     = 2
	sendTimeout = 15 * time.Second
)

// Message is a plain text e-mail.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers one message.
type Sender interface {
	Send(ctx context.Context, message Message) error
}

// SMTPSender delivers messages through an SMTP server.
type SMTPSender struct {
	cfg config.SMTP
}

// NewSMTPSender creates an SMTPSender.
func NewSMTPSender(cfg config.SMTP) *SMTPSender {
	return &SMTPSender{cfg: cfg}
}

// Send implements Sender.
func (s *SMTPSender) Send(ctx context.Context, message Message) error {
	if strings.ContainsAny(message.To+message.Subject, "\r\n") {
		return fmt.Errorf("message headers contain line breaks")
	}
	host, _, err := net.SplitHostPort(s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("parse smtp address: %w", err)
	}

	dialer := net.Dialer{Timeout: sendTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("connect to smtp: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("set smtp deadline: %w", err)
		}
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("start smtp session: %w", err)
	}
	defer func() { _ = client.Close() }()

	if s.cfg.RequireTLS {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("start smtp tls: %w", err)
		}
	}
	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password.Reveal(), host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("authenticate smtp: %w", err)
		}
	}
	if err := client.Mail(s.cfg.From); err != nil {
		return fmt.Errorf("set smtp sender: %w", err)
	}
	if err := client.Rcpt(message.To); err != nil {
		return fmt.Errorf("set smtp recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open smtp data: %w", err)
	}
	content := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n",
		s.cfg.From, message.To, mime.QEncoding.Encode("utf-8", message.Subject), message.Body)
	if _, err := writer.Write([]byte(content)); err != nil {
		return fmt.Errorf("write smtp data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish smtp data: %w", err)
	}
	return client.Quit()
}

// Queue sends messages in the background. The response time of a request then
// does not reveal whether an e-mail was sent, which hides if an account exists.
type Queue struct {
	sender Sender
	logger *slog.Logger
	jobs   chan Message
	wg     sync.WaitGroup
}

// NewQueue starts the background workers.
func NewQueue(sender Sender, logger *slog.Logger) *Queue {
	queue := &Queue{sender: sender, logger: logger, jobs: make(chan Message, queueSize)}
	for range workers {
		queue.wg.Go(queue.work)
	}
	return queue
}

func (q *Queue) work() {
	for message := range q.jobs {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		if err := q.sender.Send(ctx, message); err != nil {
			q.logger.Error("mail.send_failed", "error", err)
		}
		cancel()
	}
}

// Enqueue schedules a message. It never blocks the request.
func (q *Queue) Enqueue(message Message) {
	select {
	case q.jobs <- message:
	default:
		q.logger.Error("mail.queue_full")
	}
}

// Close sends the queued messages and stops the workers.
func (q *Queue) Close() {
	close(q.jobs)
	q.wg.Wait()
}
