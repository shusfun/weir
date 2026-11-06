package mailer

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
)

type smtpMessage struct {
	envelopeFrom, envelopeTo string
	data                     []byte
}

func buildSMTPMessage(config *SMTPConfig, to, subject, body string) (smtpMessage, error) {
	if config == nil {
		return smtpMessage{}, errors.New("missing SMTP configuration")
	}
	parse := func(value, field string) (*mail.Address, error) {
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("invalid SMTP %s address: contains a line break", field)
		}
		addr, err := mail.ParseAddress(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("invalid SMTP %s address: %w", field, err)
		}
		return addr, nil
	}
	from, err := parse(config.From, "from")
	if err != nil {
		return smtpMessage{}, err
	}
	recipient, err := parse(to, "recipient")
	if err != nil {
		return smtpMessage{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return smtpMessage{}, fmt.Errorf("generate message ID: %w", err)
	}
	name := strings.NewReplacer("\r", "", "\n", "").Replace(config.FromName)
	if strings.TrimSpace(name) == "" {
		name = from.Name
	}
	domain := config.Host
	if i := strings.LastIndexByte(from.Address, '@'); i >= 0 && i+1 < len(from.Address) {
		domain = from.Address[i+1:]
	}
	domain = strings.Trim(strings.NewReplacer("\r", "", "\n", "").Replace(domain), "[]<>")
	if domain == "" {
		domain = "localhost"
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "From: %s\r\nTo: %s\r\nDate: %s\r\nMessage-ID: <%s@%s>\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", (&mail.Address{Name: name, Address: from.Address}).String(), recipient.String(), time.Now().UTC().Format(time.RFC1123Z), hex.EncodeToString(id[:]), domain, mime.QEncoding.Encode("UTF-8", strings.NewReplacer("\r", "", "\n", "").Replace(subject)))
	writer := quotedprintable.NewWriter(&out)
	if _, err := writer.Write([]byte(body)); err != nil {
		return smtpMessage{}, fmt.Errorf("encode email body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return smtpMessage{}, fmt.Errorf("close email body encoder: %w", err)
	}
	return smtpMessage{envelopeFrom: from.Address, envelopeTo: recipient.Address, data: out.Bytes()}, nil
}
