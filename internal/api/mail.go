package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

type smtpConfig struct {
	Address  string
	Host     string
	Username string
	Password string
	From     string
	Implicit bool
	StartTLS bool
}

func (s *Server) sendMail(ctx context.Context, to string, subject string, body string) (string, string, string, map[string]any) {
	to = strings.TrimSpace(to)
	metadata := map[string]any{"target_host": notificationTargetHost(to)}
	if !validEmailTarget(to) {
		return "failed", "invalid email target", "email_outbox", metadata
	}
	cfg, err := parseSMTPURL(s.cfg.SMTPURL)
	if err != nil {
		metadata["outbox"] = true
		if strings.TrimSpace(s.cfg.SMTPURL) != "" {
			metadata["smtp_configured"] = true
			return "failed", err.Error(), "smtp", metadata
		}
		return "sent", "", "email_outbox", metadata
	}
	metadata["smtp_host"] = cfg.Host
	metadata["smtp_tls"] = cfg.Implicit || cfg.StartTLS
	if err := sendSMTP(ctx, cfg, to, subject, body); err != nil {
		return "failed", err.Error(), "smtp", metadata
	}
	return "sent", "", "smtp", metadata
}

func parseSMTPURL(raw string) (smtpConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return smtpConfig{}, fmt.Errorf("SMTP_URL is not configured")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return smtpConfig{}, fmt.Errorf("invalid SMTP_URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "smtp" && scheme != "smtps" && scheme != "smtp+tls" {
		return smtpConfig{}, fmt.Errorf("SMTP_URL must use smtp:// or smtps://")
	}
	host := parsed.Hostname()
	if host == "" {
		return smtpConfig{}, fmt.Errorf("SMTP_URL host is required")
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "smtps" || scheme == "smtp+tls" {
			port = "465"
		} else {
			port = "587"
		}
	}
	from := strings.TrimSpace(parsed.Query().Get("from"))
	if from == "" {
		return smtpConfig{}, fmt.Errorf("SMTP_URL query parameter from is required")
	}
	if !validEmailTarget(from) {
		return smtpConfig{}, fmt.Errorf("SMTP_URL from address is invalid")
	}
	username := ""
	password := ""
	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
	}
	return smtpConfig{
		Address:  net.JoinHostPort(host, port),
		Host:     host,
		Username: username,
		Password: password,
		From:     from,
		Implicit: scheme == "smtps" || scheme == "smtp+tls",
		StartTLS: strings.EqualFold(parsed.Query().Get("starttls"), "true") || (scheme == "smtp" && port == "587"),
	}, nil
}

func sendSMTP(ctx context.Context, cfg smtpConfig, to string, subject string, body string) error {
	type result struct{ err error }
	done := make(chan result, 1)
	go func() {
		done <- result{err: sendSMTPBlocking(cfg, to, subject, body)}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-done:
		return res.err
	case <-time.After(10 * time.Second):
		return fmt.Errorf("smtp send timed out")
	}
}

func sendSMTPBlocking(cfg smtpConfig, to string, subject string, body string) error {
	var client *smtp.Client
	var err error
	if cfg.Implicit {
		conn, dialErr := tls.Dial("tcp", cfg.Address, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12})
		if dialErr != nil {
			return dialErr
		}
		client, err = smtp.NewClient(conn, cfg.Host)
	} else {
		client, err = smtp.Dial(cfg.Address)
	}
	if err != nil {
		return err
	}
	defer client.Close()

	if cfg.StartTLS && !cfg.Implicit {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("smtp server does not support STARTTLS")
		}
	}
	if cfg.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("smtp server does not support AUTH")
		}
	}
	if err := client.Mail(cfg.From); err != nil {
		return err
	}
	if err := client.Rcpt(strings.TrimSpace(to)); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(buildEmailMessage(cfg.From, to, subject, body)); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func buildEmailMessage(from string, to string, subject string, body string) []byte {
	headers := []string{
		"From: " + from,
		"To: " + strings.TrimSpace(to),
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body + "\r\n")
}
