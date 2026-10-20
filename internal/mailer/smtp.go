// Adapted from Wei-Shaw/sub2api b0a02a3, LGPL-3.0.
package mailer

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

const smtpDialTimeout = 10 * time.Second
const smtpIOTimeout = 15 * time.Second

type EmailService struct{}
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
	UseTLS   bool
}

func (s *EmailService) SendEmailWithConfig(config *SMTPConfig, to, subject, body string) error {
	message, err := buildSMTPMessage(config, to, subject, body)
	if err != nil {
		return err
	}

	client, err := s.connectSMTP(config)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err = client.Mail(message.envelopeFrom); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err = client.Rcpt(message.envelopeTo); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err = w.Write(message.data); err != nil {
		return fmt.Errorf("write msg: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("close writer: %w", err)
	}
	// Email is sent successfully after w.Close(), ignore Quit errors
	// Some SMTP servers return non-standard responses on QUIT
	_ = client.Quit()
	return nil
}

// smtpTestRootCAs 仅供单元测试注入自签 CA，生产环境始终为 nil（走系统信任链）。
var smtpTestRootCAs *x509.CertPool

func smtpTLSConfig(host string) *tls.Config {
	return &tls.Config{
		ServerName: host,
		// 强制 TLS 1.2+，避免协议降级导致的弱加密风险。
		MinVersion: tls.VersionTLS12,
		RootCAs:    smtpTestRootCAs,
	}
}

// connectSMTP 按配置建立 SMTP 会话，发送与测试连接共用此路径，
// 保证"测试连接成功 ⇔ 实际发信可用"：
//   - UseTLS=true：先尝试隐式 TLS（465 语义）；若服务器以明文应答
//     （587/25 等提交端口的 STARTTLS 语义），自动改走"明文连接 + 强制 STARTTLS"。
//     两种方式都无法建立加密连接时报错，绝不明文继续。
//   - UseTLS=false：明文连接后若服务器支持 STARTTLS 则机会式升级，
//     与 smtp.SendMail 的默认行为一致。
func (s *EmailService) connectSMTP(config *SMTPConfig) (*smtp.Client, error) {
	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	dialer := &net.Dialer{Timeout: smtpDialTimeout}
	tlsConfig := smtpTLSConfig(config.Host)

	if config.UseTLS {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
		if err == nil {
			return newSMTPClient(conn, config.Host)
		}
		var recordErr tls.RecordHeaderError
		if !errors.As(err, &recordErr) {
			return nil, fmt.Errorf("tls dial: %w", err)
		}
		// SMTP 服务器先发问候语：明文问候会让 TLS 握手立刻返回
		// RecordHeaderError，据此可靠判定对端期望 STARTTLS。
		return s.connectSMTPStartTLS(dialer, addr, config.Host, tlsConfig, true)
	}

	return s.connectSMTPStartTLS(dialer, addr, config.Host, tlsConfig, false)
}

// connectSMTPStartTLS 建立明文连接并按需升级 STARTTLS。
// mandatory 为 true 时服务器必须支持 STARTTLS，否则报错。
func (s *EmailService) connectSMTPStartTLS(dialer *net.Dialer, addr, host string, tlsConfig *tls.Config, mandatory bool) (*smtp.Client, error) {
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("smtp dial: %w", err)
	}
	client, err := newSMTPClient(conn, host)
	if err != nil {
		return nil, err
	}
	if ok, _ := client.Extension("STARTTLS"); !ok {
		if mandatory {
			_ = client.Close()
			return nil, errors.New("smtp server does not support STARTTLS")
		}
		return client, nil
	}
	if err := client.StartTLS(tlsConfig); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("starttls: %w", err)
	}
	return client, nil
}

func newSMTPClient(conn net.Conn, host string) (*smtp.Client, error) {
	_ = conn.SetDeadline(time.Now().Add(smtpIOTimeout))
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("new smtp client: %w", err)
	}
	return client, nil
}
