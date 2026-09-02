package cli

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/situker/qqmail-cli/internal/errmap"
)

func confirmExactCount(rt *Runtime, count int) error {
	if rt.IsTerminal == nil || !rt.IsTerminal(rt.In) {
		return &errmap.Error{Kind: errmap.PolicyDenied, Message: "写操作要求真实 TTY 人工确认；stdin 不是 TTY", Suggestion: "请在人工在场的交互终端中重新执行；不存在 bypass flag"}
	}
	_, _ = fmt.Fprintf(rt.Err, "请输入邮件总数 %d 以确认：", count)
	value, err := bufio.NewReader(rt.In).ReadString('\n')
	if err != nil && strings.TrimSpace(value) == "" {
		return &errmap.Error{Kind: errmap.PolicyDenied, Message: "未收到人工确认", Cause: err}
	}
	if strings.TrimSpace(value) != strconv.Itoa(count) {
		return &errmap.Error{Kind: errmap.PolicyDenied, Message: "确认数字不匹配，操作已取消"}
	}
	return nil
}

func confirmToken(rt *Runtime, token string) error {
	if rt.IsTerminal == nil || !rt.IsTerminal(rt.In) {
		return &errmap.Error{Kind: errmap.PolicyDenied, Message: "操作要求真实 TTY 人工确认；stdin 不是 TTY", Suggestion: "请在人工在场的交互终端中重新执行；不存在 bypass flag"}
	}
	_, _ = fmt.Fprintf(rt.Err, "请输入 %s 以确认：", token)
	value, err := bufio.NewReader(rt.In).ReadString('\n')
	if err != nil && strings.TrimSpace(value) == "" {
		return &errmap.Error{Kind: errmap.PolicyDenied, Message: "未收到人工确认", Cause: err}
	}
	if strings.TrimSpace(value) != token {
		return &errmap.Error{Kind: errmap.PolicyDenied, Message: "确认文本不匹配，操作已取消"}
	}
	return nil
}
