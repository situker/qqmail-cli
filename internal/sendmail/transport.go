package sendmail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	smtp "github.com/emersion/go-smtp"
	"github.com/situker/qqmail-cli/internal/account"
	"github.com/situker/qqmail-cli/internal/errmap"
)

const (
	MaxMessagesPerInvocation = 1
	DefaultMessageInterval   = 2 * time.Second
)

func Send(ctx context.Context, named account.Named, authCode string, draft Draft, raw []byte) error {
	client, err := dial(ctx, named)
	if err != nil {
		return classifySMTP(err)
	}
	defer func() { _ = client.Close() }()
	client.CommandTimeout = 60 * time.Second
	client.SubmissionTimeout = 120 * time.Second
	if err := client.Auth(sasl.NewPlainClient("", named.Email, authCode)); err != nil {
		return classifySMTP(err)
	}
	if err := client.SendMail(draft.From.Address, Recipients(draft), bytes.NewReader(raw)); err != nil {
		return classifySMTP(err)
	}
	if err := client.Quit(); err != nil {
		return classifySMTP(err)
	}
	return nil
}

// tlsOverride is a test seam: production code never sets it, tests inject a
// config that trusts the local fixture certificate.
var tlsOverride *tls.Config

func dial(ctx context.Context, named account.Named) (*smtp.Client, error) {
	tlsConfig := &tls.Config{ServerName: named.SMTPHost, MinVersion: tls.VersionTLS12}
	if tlsOverride != nil {
		tlsConfig = tlsOverride
	}
	if named.SMTPHost == "smtp.qq.com" && named.SMTPPort == 465 {
		return dialWithFallback(ctx, named.SMTPHost, "465", "587", tlsConfig)
	}
	return dialEndpoint(ctx, net.JoinHostPort(named.SMTPHost, fmt.Sprint(named.SMTPPort)), tlsConfig, named.SMTPPort == 465)
}

// dialWithFallback tries the implicit-TLS primary port and falls back to
// STARTTLS on the secondary ONLY when the primary connection or TLS setup
// fails. Authentication or submission rejection on a working primary returns
// as-is and is never retried elsewhere — retrying could double-deliver.
func dialWithFallback(ctx context.Context, host, primaryPort, fallbackPort string, tlsConfig *tls.Config) (*smtp.Client, error) {
	client, err := dialEndpoint(ctx, net.JoinHostPort(host, primaryPort), tlsConfig, true)
	if err == nil {
		return client, nil
	}
	fallback, fallbackErr := dialEndpoint(ctx, net.JoinHostPort(host, fallbackPort), tlsConfig, false)
	if fallbackErr == nil {
		return fallback, nil
	}
	return nil, errors.Join(err, fallbackErr)
}

func dialEndpoint(ctx context.Context, address string, tlsConfig *tls.Config, implicitTLS bool) (*smtp.Client, error) {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	plain, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(60 * time.Second)
	if existing, ok := ctx.Deadline(); ok && existing.Before(deadline) {
		deadline = existing
	}
	_ = plain.SetDeadline(deadline)
	if implicitTLS {
		secured := tls.Client(plain, tlsConfig)
		if err := secured.HandshakeContext(ctx); err != nil {
			_ = plain.Close()
			return nil, err
		}
		client := smtp.NewClient(secured)
		client.CommandTimeout = 60 * time.Second
		return client, nil
	}
	client, err := smtp.NewClientStartTLS(plain, tlsConfig)
	if err != nil {
		_ = plain.Close()
		return nil, err
	}
	client.CommandTimeout = 60 * time.Second
	return client, nil
}

func classifySMTP(cause error) error {
	var smtpError *smtp.SMTPError
	if errors.As(cause, &smtpError) {
		message := strings.ToLower(smtpError.Message)
		if smtpError.Code == 421 || smtpError.Code == 450 || smtpError.Code == 451 || smtpError.Code == 452 || smtpError.Code == 454 || strings.Contains(message, "rate") || strings.Contains(message, "frequency") || strings.Contains(message, "too many") || strings.Contains(message, "limit") {
			return &errmap.Error{Kind: errmap.RateLimited, Message: "SMTP 发送被服务器限流", Suggestion: "停止重试并等待 10-15 分钟", Cause: cause}
		}
		if smtpError.Code == 530 || smtpError.Code == 534 || smtpError.Code == 535 {
			return &errmap.Error{Kind: errmap.AuthFailed, Message: "SMTP 认证失败", Suggestion: "确认 QQ 邮箱 SMTP 已开启且使用的是授权码", Cause: cause}
		}
		return &errmap.Error{Kind: errmap.Network, Message: "SMTP 服务器拒绝发送请求", Cause: cause}
	}
	lower := strings.ToLower(cause.Error())
	if strings.Contains(lower, "tls") || strings.Contains(lower, "x509") || strings.Contains(lower, "certificate") {
		return &errmap.Error{Kind: errmap.TLS, Message: "SMTP TLS 握手或证书校验失败", Suggestion: "检查系统时间、证书链或企业中间人代理", Cause: cause}
	}
	if strings.Contains(lower, "rate") || strings.Contains(lower, "too many") {
		return &errmap.Error{Kind: errmap.RateLimited, Message: "SMTP 发送被服务器限流", Suggestion: "停止重试并等待 10-15 分钟", Cause: cause}
	}
	return &errmap.Error{Kind: errmap.Network, Message: "SMTP 连接或发送失败", Cause: cause}
}
