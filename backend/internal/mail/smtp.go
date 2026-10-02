package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host                              string
	Port                              int
	From, Username, Password, TLSMode string
	Development                       bool
}
type Message struct{ Recipient, Subject, Body, ID string }
type Sender interface {
	Send(context.Context, Message) error
}
type SMTP struct{ Config Config }

func (c Config) Validate() error {
	if c.Host == "" || c.Port < 1 || c.Port > 65535 {
		return errors.New("invalid SMTP address")
	}
	from, err := mail.ParseAddress(c.From)
	if err != nil || from.Address != c.From || strings.ContainsAny(c.From, "\r\n") {
		return errors.New("invalid SMTP from")
	}
	if c.TLSMode != "starttls" && c.TLSMode != "implicit" && !(c.Development && c.TLSMode == "plain") {
		return errors.New("SMTP TLS is required")
	}
	if c.TLSMode == "plain" {
		ip := net.ParseIP(c.Host)
		if c.Host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("plaintext SMTP is restricted to loopback")
		}
	}
	return nil
}
func (s SMTP) Send(ctx context.Context, m Message) error {
	if err := s.Config.Validate(); err != nil {
		return err
	}
	recipient, err := mail.ParseAddress(m.Recipient)
	if err != nil || recipient.Address != m.Recipient || strings.ContainsAny(m.Recipient+m.Subject+m.ID, "\r\n") {
		return errors.New("invalid mail headers")
	}
	if len(m.Body) > 65536 || len(m.Subject) > 200 || len(m.ID) > 200 || !strings.HasPrefix(m.ID, "<") || !strings.HasSuffix(m.ID, ">") {
		return errors.New("invalid mail content")
	}
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	address := net.JoinHostPort(s.Config.Host, strconv.Itoa(s.Config.Port))
	tlsConfig := &tls.Config{ServerName: s.Config.Host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	if s.Config.TLSMode == "implicit" {
		td := tls.Dialer{NetDialer: &dialer, Config: tlsConfig}
		conn, err = td.DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return errors.New("SMTP connection failed")
	}
	defer conn.Close()
	conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	client, err := smtp.NewClient(conn, s.Config.Host)
	if err != nil {
		return errors.New("SMTP handshake failed")
	}
	defer client.Close()
	if s.Config.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP STARTTLS unavailable")
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return errors.New("SMTP TLS verification failed")
		}
	}
	if s.Config.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", s.Config.Username, s.Config.Password, s.Config.Host)); err != nil {
			return errors.New("SMTP authentication failed")
		}
	}
	if err = client.Mail(s.Config.From); err != nil {
		return errors.New("SMTP sender rejected")
	}
	if err = client.Rcpt(m.Recipient); err != nil {
		return errors.New("SMTP recipient rejected")
	}
	writer, err := client.Data()
	if err != nil {
		return errors.New("SMTP DATA failed")
	}
	// MIME subject encoding preserves Cyrillic without relying on SMTPUTF8.
	headers := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMessage-ID: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n", s.Config.From, m.Recipient, encodeSubject(m.Subject), m.ID, time.Now().UTC().Format(time.RFC1123Z))
	if _, err = writer.Write([]byte(headers + encodeBody(m.Body))); err != nil {
		return errors.New("SMTP write failed")
	}
	if err = writer.Close(); err != nil {
		return errors.New("SMTP acceptance unknown")
	}
	// DATA 250 is the accepted outcome; a subsequent QUIT failure must not resend.
	_ = client.Quit()
	return nil
}
