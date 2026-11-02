package calendar

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"officerdashboard/internal/auth"
)

// Message is one invitation email.
type Message struct {
	To      string
	Subject string
	Text    string
	ICS     string
	Method  string // REQUEST or CANCEL
}

// Mailer sends invitation emails.
type Mailer interface {
	Send(ctx context.Context, m Message) error
	From() string
}

// SMTP sends through an authenticated relay. Port 465 uses implicit TLS; any other port
// must offer STARTTLS, except on localhost (a dev relay such as Mailpit).
type SMTP struct {
	Host, Port, Username, Password, Sender string
}

func (s *SMTP) From() string { return s.Sender }

func header(v string) string { return mime.QEncoding.Encode("utf-8", v) }

func wrap76(s string) string {
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	b.WriteString(s)
	return b.String()
}

// Build renders the MIME message: a text part, the text/calendar part mail clients use
// for the Accept/Decline UI, and the same calendar as an invite.ics attachment.
func Build(from string, m Message, now time.Time) []byte {
	boundary := "cseed-" + auth.RandomToken(12)
	method := m.Method
	if method == "" {
		method = "REQUEST"
	}
	cal := wrap76(base64.StdEncoding.EncodeToString([]byte(m.ICS)))
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@cseed-dashboard>\r\nMIME-Version: 1.0\r\n",
		(&mail.Address{Name: "CSEED Officer Dashboard", Address: from}).String(), m.To, header(m.Subject), now.UTC().Format(time.RFC1123Z), auth.RandomToken(16))
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", boundary, wrap76(base64.StdEncoding.EncodeToString([]byte(m.Text))))
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/calendar; charset=utf-8; method=%s\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", boundary, method, cal)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: application/ics; name=\"invite.ics\"\r\nContent-Disposition: attachment; filename=\"invite.ics\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", boundary, cal)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return []byte(b.String())
}

func (s *SMTP) Send(ctx context.Context, m Message) error {
	if strings.ContainsAny(m.To, "\r\n") {
		return errors.New("invalid recipient")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	addr := net.JoinHostPort(s.Host, s.Port)
	d := net.Dialer{}
	var conn net.Conn
	var err error
	tlsCfg := &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	if s.Port == "465" {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp: %w", err)
	}
	defer c.Close()
	if s.Port != "465" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		} else if !isLocal(s.Host) {
			return errors.New("smtp server does not offer STARTTLS")
		}
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(s.Sender); err != nil {
		return fmt.Errorf("smtp from: %w", err)
	}
	if err := c.Rcpt(m.To); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(Build(s.Sender, m, time.Now())); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return c.Quit()
}

func isLocal(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
