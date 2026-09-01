package imapx

import (
	"context"
	"strings"

	imap "github.com/emersion/go-imap/v2"
	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/mailmodel"
)

// Mutator extends the read surface with the only server mutations allowed by
// qqmailctl. Command code must reach these methods through internal/policy.
// LocateByIdentity is itself read-only (EXAMINE + UID SEARCH + PEEK fetches);
// it lives here because its only consumers are mutation flows (restore, and
// the conservative copy confirmation).
type Mutator interface {
	Reader
	SetSeen(context.Context, mailmodel.MsgID) error
	MoveUID(context.Context, mailmodel.MsgID, string) (MutationResult, error)
	CopyMarkDeletedUID(context.Context, mailmodel.MsgID, string, MessageIdentity) (MutationResult, error)
	LocateByIdentity(context.Context, string, MessageIdentity) ([]mailmodel.MsgID, error)
}

type MessageIdentity struct {
	MessageID string
	SizeBytes int64
}

type MutationResult struct {
	Method              string `json:"method"`
	Destination         string `json:"destination,omitempty"`
	DestinationVerified bool   `json:"destination_verified,omitempty"`
	SourceMarkedDeleted bool   `json:"source_marked_deleted,omitempty"`
	SourceRetained      bool   `json:"source_retained,omitempty"`
}

func DialMutatorWithVersion(ctx context.Context, cfg account.Named, authCode, version string) (Mutator, error) {
	return DialWithVersion(ctx, cfg, authCode, version)
}

func (c *Client) SetSeen(ctx context.Context, id mailmodel.MsgID) error {
	if err := c.selectWritable(ctx, id); err != nil {
		return err
	}
	if err := c.setDeadline(ctx); err != nil {
		return err
	}
	stop := c.watchdog(ctx)
	defer stop()
	command := c.raw.Store(imap.UIDSetNum(imap.UID(id.UID)), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil)
	return command.Close()
}

func (c *Client) MoveUID(ctx context.Context, id mailmodel.MsgID, destination string) (MutationResult, error) {
	if !hasCapability(c.capAfter, "MOVE") {
		return MutationResult{}, &errmap.Error{Kind: errmap.PolicyDenied, Message: "服务器未声明 MOVE；拒绝调用可能降级为不安全流程的库方法"}
	}
	if err := c.selectWritable(ctx, id); err != nil {
		return MutationResult{}, err
	}
	if err := c.setDeadline(ctx); err != nil {
		return MutationResult{}, err
	}
	stop := c.watchdog(ctx)
	defer stop()
	if _, err := c.raw.Move(imap.UIDSetNum(imap.UID(id.UID)), destination).Wait(); err != nil {
		return MutationResult{}, err
	}
	return MutationResult{Method: "uid_move", Destination: destination, DestinationVerified: true}, nil
}

func (c *Client) CopyMarkDeletedUID(ctx context.Context, id mailmodel.MsgID, destination string, identity MessageIdentity) (MutationResult, error) {
	if err := c.selectWritable(ctx, id); err != nil {
		return MutationResult{}, err
	}
	if err := c.setDeadline(ctx); err != nil {
		return MutationResult{}, err
	}
	stopCopy := c.watchdog(ctx)
	copyData, err := c.raw.Copy(imap.UIDSetNum(imap.UID(id.UID)), destination).Wait()
	stopCopy()
	if err != nil {
		return MutationResult{}, err
	}
	confirmed := false
	if copyData != nil && copyData.UIDValidity > 0 {
		sourceUIDs, sourceOK := copyData.SourceUIDs.Nums()
		if destinationUIDs, ok := copyData.DestUIDs.Nums(); sourceOK && ok && len(sourceUIDs) == 1 && sourceUIDs[0] == imap.UID(id.UID) && len(destinationUIDs) == 1 {
			confirmed = true
		}
	}
	if !confirmed {
		confirmed, err = c.confirmDestination(ctx, destination, identity)
		if err != nil {
			return MutationResult{}, err
		}
	}
	if !confirmed {
		return MutationResult{}, &errmap.Error{Kind: errmap.PolicyDenied, Message: "无法按 Message-ID 与 RFC822.SIZE 确认目标夹副本；源邮件保持不变"}
	}
	if err := c.selectWritable(ctx, id); err != nil {
		return MutationResult{}, err
	}
	if err := c.setDeadline(ctx); err != nil {
		return MutationResult{}, err
	}
	stopStore := c.watchdog(ctx)
	defer stopStore()
	command := c.raw.Store(imap.UIDSetNum(imap.UID(id.UID)), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil)
	if err := command.Close(); err != nil {
		return MutationResult{}, err
	}
	return MutationResult{Method: "copy_store_deleted", Destination: destination, DestinationVerified: true, SourceMarkedDeleted: true, SourceRetained: true}, nil
}

func (c *Client) selectWritable(ctx context.Context, id mailmodel.MsgID) error {
	if err := c.setDeadline(ctx); err != nil {
		return err
	}
	stop := c.watchdog(ctx)
	defer stop()
	selected, err := c.raw.Select(id.Folder, &imap.SelectOptions{ReadOnly: false}).Wait()
	if err != nil {
		return err
	}
	if selected.UIDValidity != id.UIDValidity {
		return &errmap.Error{Kind: errmap.StaleID, Message: "邮件 id 已失效：文件夹 UIDVALIDITY 已变化", Context: map[string]any{"folder": id.Folder}}
	}
	return nil
}

func (c *Client) confirmDestination(ctx context.Context, destination string, identity MessageIdentity) (bool, error) {
	matches, err := c.LocateByIdentity(ctx, destination, identity)
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

// LocateByIdentity finds messages in a folder by Message-ID plus RFC822.SIZE.
// It is read-only and powers both the conservative-copy confirmation and the
// restore command's trash lookup.
func (c *Client) LocateByIdentity(ctx context.Context, folder string, identity MessageIdentity) ([]mailmodel.MsgID, error) {
	if strings.TrimSpace(identity.MessageID) == "" || identity.SizeBytes < 0 {
		return []mailmodel.MsgID{}, nil
	}
	uidValidity, _, err := c.Examine(ctx, folder)
	if err != nil {
		return nil, err
	}
	if err := c.setDeadline(ctx); err != nil {
		return nil, err
	}
	stop := c.watchdog(ctx)
	criteria := &imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: identity.MessageID}}}
	data, err := c.raw.UIDSearch(criteria, nil).Wait()
	stop()
	if err != nil {
		return nil, err
	}
	all := data.AllUIDs()
	if len(all) == 0 {
		return []mailmodel.MsgID{}, nil
	}
	ids := make([]uint32, len(all))
	for i, uid := range all {
		ids[i] = uint32(uid)
	}
	envelopes, err := c.FetchEnvelopes(ctx, folder, uidValidity, ids)
	if err != nil {
		return nil, err
	}
	headers, err := c.FetchHeaderFields(ctx, ids)
	if err != nil {
		return nil, err
	}
	headerByUID := map[uint32]string{}
	for _, header := range headers {
		headerByUID[header.UID] = normalizeMessageID(header.MessageID)
	}
	matches := []mailmodel.MsgID{}
	for _, envelope := range envelopes {
		if envelope.Size == identity.SizeBytes && headerByUID[envelope.UID] == normalizeMessageID(identity.MessageID) {
			matches = append(matches, mailmodel.MsgID{Folder: folder, UIDValidity: uidValidity, UID: envelope.UID})
		}
	}
	return matches, nil
}

func hasCapability(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

func normalizeMessageID(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(value), "<>"))
}

var _ Mutator = (*Client)(nil)
