package delivery

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	SMTPAddress, SMTPUser, SMTPPassword, From, WebhookURL, WebhookSecret string
	LocalSMTP, LocalWebhook                                              bool
}
type NetworkSender struct {
	config Config
	client *http.Client
}

func NewSender(c Config) (*NetworkSender, error) {
	if c.SMTPAddress != "" {
		if _, _, e := net.SplitHostPort(c.SMTPAddress); e != nil {
			return nil, errors.New("invalid SMTP address")
		}
		from, e := mail.ParseAddress(c.From)
		if e != nil || from.Address != c.From {
			return nil, errors.New("invalid mail sender")
		}
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		for _, a := range ips {
			if !c.LocalWebhook && (a.IP.IsPrivate() || a.IP.IsLoopback() || a.IP.IsLinkLocalUnicast() || a.IP.IsUnspecified() || a.IP.IsMulticast()) {
				return nil, errors.New("private webhook address denied")
			}
		}
		for _, a := range ips {
			conn, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, errors.New("webhook connection failed")
	}
	if c.WebhookURL != "" {
		u, e := url.Parse(c.WebhookURL)
		if e != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || !(u.Scheme == "https" || (c.LocalWebhook && u.Scheme == "http")) {
			return nil, errors.New("webhook must use HTTPS without embedded credentials")
		}
	}
	return &NetworkSender{config: c, client: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (s *NetworkSender) Send(ctx context.Context, kind string, m Message) error {
	if kind == "email" {
		return s.email(ctx, m)
	}
	if kind != "webhook" || s.config.WebhookURL == "" {
		return errors.New("destination disabled")
	}
	data, _ := json.Marshal(map[string]string{"text": m.Text, "content": m.Text, "event": m.Event, "id": m.ID})
	req, e := http.NewRequestWithContext(ctx, "POST", s.config.WebhookURL, bytes.NewReader(data))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", m.ID)
	if s.config.WebhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(s.config.WebhookSecret))
		mac.Write(data)
		req.Header.Set("X-Mini-Fury-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	res, e := s.client.Do(req)
	if e != nil {
		return errors.New("webhook failed")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return errors.New("webhook rejected")
	}
	return nil
}
func (s *NetworkSender) email(ctx context.Context, m Message) error {
	c := s.config
	if c.SMTPAddress == "" {
		return errors.New("email disabled")
	}
	address, e := mail.ParseAddress(m.To)
	if e != nil || address.Address != m.To || strings.ContainsAny(m.Subject, "\r\n") {
		return errors.New("invalid envelope")
	}
	host, _, _ := net.SplitHostPort(c.SMTPAddress)
	conn, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", c.SMTPAddress)
	if e != nil {
		return errors.New("smtp connection failed")
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if deadline.IsZero() {
		deadline = time.Now().Add(10 * time.Second)
	}
	conn.SetDeadline(deadline)
	client, e := smtp.NewClient(conn, host)
	if e != nil {
		return errors.New("smtp unavailable")
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if e = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); e != nil {
			return errors.New("smtp TLS failed")
		}
	} else if !c.LocalSMTP {
		return errors.New("SMTP requires STARTTLS")
	}
	if c.SMTPUser != "" {
		if e = client.Auth(smtp.PlainAuth("", c.SMTPUser, c.SMTPPassword, host)); e != nil {
			return errors.New("smtp authentication failed")
		}
	}
	if e = client.Mail(c.From); e != nil {
		return errors.New("smtp sender rejected")
	}
	if e = client.Rcpt(m.To); e != nil {
		return errors.New("smtp recipient rejected")
	}
	w, e := client.Data()
	if e != nil {
		return e
	}
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n", c.From, m.To, m.Subject)
	// Quoted-printable is unnecessary for the ASCII recovery template; base64 prevents header/body injection.
	body += encodeBody(m.Text)
	if _, e = w.Write([]byte(body)); e != nil {
		return e
	}
	if e = w.Close(); e != nil {
		return e
	}
	return client.Quit()
}
func encodeBody(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) + "\r\n" }
