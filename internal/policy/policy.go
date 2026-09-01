package policy

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/index"
	"github.com/situker/qqmailctl/internal/mailmodel"
)

const ReadonlyEnv = "QQMAILCTL_READONLY"

type Service struct {
	writer imapx.Mutator
	audit  *index.DB
}

func New(writer imapx.Mutator, audit *index.DB) *Service {
	return &Service{writer: writer, audit: audit}
}

func Readonly() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(ReadonlyEnv)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func RequireMutationAllowed() error {
	if Readonly() {
		return &errmap.Error{Kind: errmap.PolicyDenied, Message: "QQMAILCTL_READONLY 已启用，拒绝所有写操作", Suggestion: "仅在人工在场并确认风险后，于该次命令环境中关闭只读开关"}
	}
	return nil
}

func (s *Service) MarkRead(ctx context.Context, id mailmodel.MsgID, command, planRef string) error {
	if err := RequireMutationAllowed(); err != nil {
		return err
	}
	if err := s.record(ctx, command, "mark_read_attempt", id.String(), "attempt", planRef); err != nil {
		return err
	}
	err := s.writer.SetSeen(ctx, id)
	result := "ok"
	if err != nil {
		result = "failed"
	}
	if auditErr := s.record(ctx, command, "mark_read", id.String(), result, planRef); auditErr != nil && err == nil {
		return auditErr
	}
	return err
}

func (s *Service) Move(ctx context.Context, id mailmodel.MsgID, destination string, identity imapx.MessageIdentity, command, planRef string) (imapx.MutationResult, error) {
	if err := RequireMutationAllowed(); err != nil {
		return imapx.MutationResult{}, err
	}
	if err := s.record(ctx, command, "move_attempt", id.String(), "attempt", planRef); err != nil {
		return imapx.MutationResult{}, err
	}
	_, after := s.writer.Capabilities()
	var result imapx.MutationResult
	var err error
	if capability(after, "MOVE") {
		result, err = s.writer.MoveUID(ctx, id, destination)
	} else {
		result, err = s.writer.CopyMarkDeletedUID(ctx, id, destination, identity)
	}
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if auditErr := s.record(ctx, command, "move", id.String(), status+":"+result.Method, planRef); auditErr != nil && err == nil {
		return result, auditErr
	}
	return result, err
}

func (s *Service) record(ctx context.Context, command, action, id, result, planRef string) error {
	if s.audit == nil {
		return fmt.Errorf("policy audit store is required")
	}
	_, err := s.audit.RecordAudit(ctx, index.AuditEntry{Command: command, Action: action, MsgID: id, Result: result, PlanRef: planRef})
	return err
}

func capability(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}
