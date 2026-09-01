package errmap

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/situker/qqmailctl/internal/output"
)

type Kind string

const (
	Internal          Kind = "internal"
	Usage             Kind = "usage"
	Config            Kind = "config"
	AuthFailed        Kind = "auth_failed"
	AuthCodeMissing   Kind = "auth_code_missing"
	ServiceNotEnabled Kind = "service_not_enabled"
	Network           Kind = "network"
	TLS               Kind = "tls"
	RateLimited       Kind = "rate_limited"
	NotFound          Kind = "not_found"
	StaleID           Kind = "stale_id"
	PolicyDenied      Kind = "policy_denied"
	ParseError        Kind = "parse_error"
)

type Error struct {
	Kind       Kind
	Message    string
	Suggestion string
	Context    map[string]any
	Cause      error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

func New(kind Kind, message string) *Error { return &Error{Kind: kind, Message: message} }

func Classify(err error) *Error {
	if err == nil {
		return nil
	}
	var app *Error
	if errors.As(err, &app) {
		return app
	}
	var certErr x509.UnknownAuthorityError
	if errors.As(err, &certErr) {
		return &Error{Kind: TLS, Message: "TLS 证书校验失败", Suggestion: "检查系统时间、证书链或企业中间人代理", Cause: err}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return &Error{Kind: Network, Message: "网络连接失败", Suggestion: "检查网络后重试；若刚刚频繁登录，请等待 10-15 分钟", Cause: err}
	}
	lower := strings.ToLower(err.Error())
	// Cobra usage errors must be classified before the server-error substring
	// heuristics below: `qqmailctl login` produces `unknown command "login"`,
	// which the auth substring match would otherwise misreport as auth_failed.
	if isUsageErrorText(lower) {
		return &Error{Kind: Usage, Message: "命令用法错误：" + err.Error(), Suggestion: "运行 qqmailctl --help 查看命令与参数", Cause: err}
	}
	switch {
	case strings.Contains(lower, "rate limit"), strings.Contains(lower, "too many"), strings.Contains(lower, "frequency"):
		return &Error{Kind: RateLimited, Message: "QQ 邮箱暂时拒绝了频繁连接", Suggestion: "停止重试并等待 10-15 分钟", Cause: err}
	case strings.Contains(lower, "login"), strings.Contains(lower, "password"), strings.Contains(lower, "auth"):
		return &Error{Kind: AuthFailed, Message: "认证失败：服务器拒绝了登录", Suggestion: "确认使用的是 16 位授权码而不是 QQ 密码；若最近改过 QQ 密码，请重新生成授权码", Cause: err}
	case strings.Contains(lower, "not found"), strings.Contains(lower, "does not exist"):
		return &Error{Kind: NotFound, Message: "请求的邮件或文件夹不存在", Cause: err}
	default:
		return &Error{Kind: Internal, Message: "未预期的内部错误", Cause: err}
	}
}

func isUsageErrorText(lower string) bool {
	for _, pattern := range []string{
		"unknown command", "unknown flag", "unknown shorthand",
		"flag needs an argument", "invalid argument", "required flag",
		"requires at least", "accepts at most", "arg(s), received",
	} {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func Details(err error) (output.Error, int) {
	app := Classify(err)
	retryable := app.Kind == Network || app.Kind == RateLimited
	out := output.Error{Code: string(app.Kind), Message: app.Message, Retryable: retryable, Suggestion: app.Suggestion, Context: app.Context}
	switch app.Kind {
	case Usage:
		return out, output.ExitUsage
	case Config:
		return out, output.ExitConfig
	case AuthFailed, AuthCodeMissing:
		return out, output.ExitAuth
	case ServiceNotEnabled:
		return out, output.ExitServiceNotEnabled
	case Network:
		return out, output.ExitNetwork
	case TLS:
		return out, output.ExitTLS
	case RateLimited:
		return out, output.ExitRateLimited
	case NotFound, StaleID:
		return out, output.ExitNotFound
	case PolicyDenied:
		return out, output.ExitPolicyDenied
	case ParseError:
		return out, output.ExitParse
	default:
		return out, output.ExitInternal
	}
}
