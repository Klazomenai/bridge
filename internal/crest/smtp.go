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

	msg := []byte(
		"From: " + sanitiseHeader(cfg.From) + "\r\n" +
			"To: " + sanitiseHeader(to) + "\r\n" +
			"Subject: " + sanitiseHeader(subject) + "\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\n" +
			"\r\n" +
			body + "\r\n",
	)

	if err := smtp.SendMail(addr, auth, cfg.From, []string{to}, msg); err != nil {
		return fmt.Errorf("smtp send to %s: %w", to, err)
	}
	return nil
}
