package notify

import (
	"fmt"
	"net/smtp"
)

type smtpMailer struct {
	cfg Config
}

func newSMTPMailer(cfg Config) *smtpMailer {
	return &smtpMailer{cfg: cfg}
}

func (m *smtpMailer) Send(to, subject, htmlBody string) error {
	port := m.cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", m.cfg.SMTPHost, port)

	var auth smtp.Auth
	if m.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", m.cfg.SMTPUser, m.cfg.SMTPPassword, m.cfg.SMTPHost)
	}

	msg := []byte(
		"From: " + m.cfg.FromAddress + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/html; charset=\"UTF-8\"\r\n" +
			"\r\n" +
			htmlBody,
	)

	return smtp.SendMail(addr, auth, m.cfg.FromAddress, []string{to}, msg)
}
