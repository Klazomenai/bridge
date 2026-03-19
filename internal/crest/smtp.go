package crest

import (
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// SMTPConfig holds connection details for the ProtonMail bridge SMTP endpoint.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// sanitiseHeader strips CR and LF from a header field value to prevent
// email header injection.
func sanitiseHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

// SendMail sends a plain-text email via the ProtonMail bridge.
func SendMail(cfg SMTPConfig, to, subject, body string) error {
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)

	// Sanitise all user-controlled values before use — both in message headers
	// and in the SMTP envelope to prevent header/command injection.
	cleanFrom := sanitiseHeader(cfg.From)
	cleanTo := sanitiseHeader(to)

	msg := []byte(
		"From: " + cleanFrom + "\r\n" +
			"To: " + cleanTo + "\r\n" +
			"Subject: " + sanitiseHeader(subject) + "\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\n" +
			"\r\n" +
			body + "\r\n",
	)

	if err := smtp.SendMail(addr, auth, cleanFrom, []string{cleanTo}, msg); err != nil {
		return fmt.Errorf("smtp send to %s: %w", cleanTo, err)
	}
	return nil
}
