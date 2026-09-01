package cleaner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/cleanupplan"
	exporter "github.com/situker/qqmailctl/internal/export"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/mailmodel"
	"github.com/situker/qqmailctl/internal/secrets"
)

type Eligible struct {
	ID       mailmodel.MsgID       `json:"-"`
	IDString string                `json:"id"`
	Identity imapx.MessageIdentity `json:"-"`
}

type Failure struct {
	ID     string `json:"id"`
	Gate   string `json:"gate"`
	Reason string `json:"reason"`
}

type GateResult struct {
	Eligible []Eligible `json:"eligible"`
	Failures []Failure  `json:"failures"`
	// AlreadyGone lists plan entries that no longer exist on the server while a
	// verified local backup is present. They are safe to skip: the mail cannot
	// be lost (the backup passed the local gate) and re-running a partially
	// executed plan must not dead-end the remaining messages.
	AlreadyGone []Failure `json:"already_gone"`
}

func Verify(ctx context.Context, reader imapx.Reader, plan cleanupplan.Plan, named account.Named, provider secrets.Provider, paranoid bool) (GateResult, error) {
	if strings.TrimSpace(plan.BackupRoot) == "" {
		return GateResult{}, fmt.Errorf("plan has no backup_root; run backup --plan first")
	}
	manifest, _, err := exporter.LoadVerified(plan.BackupRoot, named, provider)
	if err != nil {
		return GateResult{}, fmt.Errorf("local backup gate failed: %w", err)
	}
	if manifest.Account != named.Name {
		return GateResult{}, fmt.Errorf("verified manifest account does not match selected account")
	}
	byID := make(map[string]exporter.Entry, len(manifest.Messages))
	for _, entry := range manifest.Messages {
		byID[entry.ID] = entry
	}
	result := GateResult{Eligible: []Eligible{}, Failures: []Failure{}, AlreadyGone: []Failure{}}
	seen := map[string]bool{}
	for _, item := range plan.Items {
		if seen[item.ID] {
			result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "local", Reason: "duplicate message ID in plan"})
			continue
		}
		seen[item.ID] = true
		entry, ok := byID[item.ID]
		if !ok {
			result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "local", Reason: "message is absent from verified manifest"})
			continue
		}
		id, parseErr := mailmodel.ParseMsgID(item.ID)
		if parseErr != nil || entry.UID != id.UID || entry.UIDValidity != id.UIDValidity || entry.FolderRaw != id.Folder {
			result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "local", Reason: "plan ID and manifest identity disagree"})
			continue
		}
		if strings.TrimSpace(entry.MessageID) == "" {
			result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "server", Reason: "manifest lacks Message-ID required for server-truth comparison"})
			continue
		}
		uidValidity, _, examineErr := reader.Examine(ctx, id.Folder)
		if examineErr != nil {
			result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "server", Reason: "folder examination failed"})
			continue
		}
		if uidValidity != entry.UIDValidity {
			result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "server", Reason: "UIDVALIDITY changed"})
			continue
		}
		envelopes, fetchErr := reader.FetchEnvelopes(ctx, id.Folder, uidValidity, []uint32{id.UID})
		if fetchErr != nil {
			result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "server", Reason: "server envelope fetch failed"})
			continue
		}
		if len(envelopes) == 0 {
			// Absent on the server but fully backed up locally (the manifest
			// gate already passed): typically a re-run of a partially executed
			// plan, or the message was moved/deleted elsewhere.
			result.AlreadyGone = append(result.AlreadyGone, Failure{ID: item.ID, Gate: "server", Reason: "message no longer exists on the server; verified local backup is present"})
			continue
		}
		if len(envelopes) != 1 || envelopes[0].Size != entry.Size {
			result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "server", Reason: "RFC822.SIZE does not match verified backup"})
			continue
		}
		if hasServerFlag(envelopes[0].Flags, `\Flagged`) {
			result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "server", Reason: "message is flagged (starred) on the server; flagged mail is protected from cleanup"})
			continue
		}
		headers, headerErr := reader.FetchHeaderFields(ctx, []uint32{id.UID})
		if headerErr != nil {
			result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "server", Reason: "server header fetch failed"})
			continue
		}
		if len(headers) != 1 || normalizeMessageID(headers[0].MessageID) != normalizeMessageID(entry.MessageID) {
			result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "server", Reason: "Message-ID does not match verified backup"})
			continue
		}
		if paranoid {
			raw, truncated, bodyErr := reader.FetchBodyPeek(ctx, id, imapx.MaxMessageBytes)
			if bodyErr != nil || truncated {
				result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "paranoid", Reason: "full message refetch failed or was truncated"})
				continue
			}
			digest := sha256.Sum256(raw)
			if !strings.EqualFold(hex.EncodeToString(digest[:]), entry.SHA256) {
				result.Failures = append(result.Failures, Failure{ID: item.ID, Gate: "paranoid", Reason: "server body SHA-256 does not match verified backup"})
				continue
			}
		}
		result.Eligible = append(result.Eligible, Eligible{ID: id, IDString: item.ID, Identity: imapx.MessageIdentity{MessageID: entry.MessageID, SizeBytes: entry.Size}})
	}
	return result, nil
}

func TrashFolder(ctx context.Context, reader imapx.Reader) (string, error) {
	folders, err := reader.ListFolders(ctx)
	if err != nil {
		return "", err
	}
	for _, folder := range folders {
		for _, attribute := range folder.Attributes {
			if strings.EqualFold(attribute, `\Trash`) {
				return folder.Name, nil
			}
		}
	}
	for _, candidate := range []string{"Deleted Messages", "Trash", "已删除", "已删除邮件"} {
		for _, folder := range folders {
			if strings.EqualFold(folder.Name, candidate) {
				return folder.Name, nil
			}
		}
	}
	return "", fmt.Errorf("server trash folder could not be identified")
}

func normalizeMessageID(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(value), "<>"))
}

func hasServerFlag(flags []string, want string) bool {
	for _, flag := range flags {
		if strings.EqualFold(flag, want) {
			return true
		}
	}
	return false
}
