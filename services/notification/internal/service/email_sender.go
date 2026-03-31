package service

import (
	"context"
	"errors"
	"fmt"
	"net/smtp"
)

type EmailMessage struct {
	To      string
	Subject string
	Body    string
}

type EmailSender interface {
	Send(ctx context.Context, message EmailMessage) error
}

type smtpSender struct {
	host     string
	port     int
	username string
	password string
	from     string
}

func NewSMTPSender(host string, port int, username, password, from string) EmailSender {
	return &smtpSender{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
	}
}

func (s *smtpSender) Send(ctx context.Context, message EmailMessage) error {
	if message.To == "" {
		return errors.New("recipient email is required")
	}
	if s.host == "" || s.port == 0 {
		return errors.New("smtp host and port are required")
	}

	from := s.from
	if from == "" {
		from = s.username
	}
	if from == "" {
		return errors.New("smtp sender address is required")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	var auth smtp.Auth
	if s.username != "" {
		auth = smtp.PlainAuth("", s.username, s.password, s.host)
	}

	rawMessage := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", from, message.To, message.Subject, message.Body))

	return smtp.SendMail(addr, auth, from, []string{message.To}, rawMessage)
}
