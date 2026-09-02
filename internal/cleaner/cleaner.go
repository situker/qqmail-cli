package cleaner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/situker/qqmail-cli/internal/account"
	"github.com/situker/qqmail-cli/internal/cleanupplan"
	exporter "github.com/situker/qqmail-cli/internal/export"
	"github.com/situker/qqmail-cli/internal/imapx"
	"github.com/situker/qqmail-cli/internal/mailmodel"
	"github.com/situker/qqmail-cli/internal/secrets"
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

// Verify runs the three-level gate. progress may be nil; when set it is
// called after every message so long runs can show a heartbeat.
func Verify(ctx context.Context, reader imapx.Reader, plan cleanupplan.Plan, named account.Named, provider secrets.Provider, paranoid bool, progress func(done, total int)) (GateResult, error) {
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

	// Per-item outcome, filled in original plan order at the end. Local checks
	// resolve immediately; server checks are batched per folder because a
	// per-message EXAMINE would be thousands of round trips and trip QQ's
	// connection-rate limits (live observation 2026-09-02).
	type pending struct {
		id    mailmodel.MsgID
		entry exporter.Entry
	}
	outcomes := make([]*Failure, len(plan.Items)) // failure if set
	gone := make([]bool, len(plan.Items))
	eligible := make([]*Eligible, len(plan.Items))
	needServer := map[string][]int{}
	pendings := make([]pending, len(plan.Items))
	seen := map[string]bool{}

	fail := func(index int, gate, reason string) {
		f := Failure{ID: plan.Items[index].ID, Gate: gate, Reason: reason}
		outcomes[index] = &f
	}

	for index, item := range plan.Items {
		if seen[item.ID] {
			fail(index, "local", "duplicate message ID in plan")
			continue
		}
		seen[item.ID] = true
		entry, ok := byID[item.ID]
		if !ok {
			fail(index, "local", "message is absent from verified manifest")
			continue
		}
		id, parseErr := mailmodel.ParseMsgID(item.ID)
		if parseErr != nil || entry.UID != id.UID || entry.UIDValidity != id.UIDValidity || entry.FolderRaw != id.Folder {
			fail(index, "local", "plan ID and manifest identity disagree")
			continue
		}
		pendings[index] = pending{id: id, entry: entry}
		needServer[id.Folder] = append(needServer[id.Folder], index)
	}

	done := 0
	for folder, indexes := range needServer {
		uidValidity, _, examineErr := reader.Examine(ctx, folder)
		if examineErr != nil {
			for _, index := range indexes {
				fail(index, "server", "folder examination failed")
			}
			done += len(indexes)
			if progress != nil {
				progress(done, len(plan.Items))
			}
			continue
		}
		uids := make([]uint32, 0, len(indexes))
		for _, index := range indexes {
			uids = append(uids, pendings[index].id.UID)
		}
		envByUID, envErr := fetchEnvelopesByUID(ctx, reader, folder, uidValidity, uids)
		if envErr != nil {
			for _, index := range indexes {
				fail(index, "server", "server envelope fetch failed")
			}
			done += len(indexes)
			if progress != nil {
				progress(done, len(plan.Items))
			}
			continue
		}
		headerByUID, headerErr := fetchHeadersByUID(ctx, reader, uids)
		if headerErr != nil {
			for _, index := range indexes {
				fail(index, "server", "server header fetch failed")
			}
			done += len(indexes)
			if progress != nil {
				progress(done, len(plan.Items))
			}
			continue
		}
		for _, index := range indexes {
			p := pendings[index]
			done++
			if progress != nil {
				progress(done, len(plan.Items))
			}
			if uidValidity != p.entry.UIDValidity {
				fail(index, "server", "UIDVALIDITY changed")
				continue
			}
			envelope, present := envByUID[p.id.UID]
			if !present {
				// Absent on the server but fully backed up locally (the manifest
				// gate passed): typically a re-run of a partially executed plan,
				// or the message was moved/deleted elsewhere.
				gone[index] = true
				continue
			}
			if envelope.Size != p.entry.Size {
				fail(index, "server", "RFC822.SIZE does not match verified backup")
				continue
			}
			if hasServerFlag(envelope.Flags, `\Flagged`) {
				fail(index, "server", "message is flagged (starred) on the server; flagged mail is protected from cleanup")
				continue
			}
			header, hasHeader := headerByUID[p.id.UID]
			if !hasHeader {
				fail(index, "server", "server header fetch returned unexpected count")
				continue
			}
			// Message-ID is one of three server-truth dimensions, and wild
			// email legitimately lacks the header. A missing dimension is not
			// a failed comparison: with no Message-ID in the verified backup,
			// the message passes on UIDVALIDITY+UID+RFC822.SIZE — but only if
			// the server side has none either. Any one-sided presence rejects.
			if normalizeMessageID(header.MessageID) != normalizeMessageID(p.entry.MessageID) {
				reason := "Message-ID does not match verified backup"
				if strings.TrimSpace(p.entry.MessageID) == "" {
					reason = "server reports a Message-ID but the verified backup has none"
				}
				fail(index, "server", reason)
				continue
			}
			if paranoid {
				raw, truncated, bodyErr := reader.FetchBodyPeek(ctx, p.id, imapx.MaxMessageBytes)
				if bodyErr != nil || truncated {
					fail(index, "paranoid", "full message refetch failed or was truncated")
					continue
				}
				digest := sha256.Sum256(raw)
				if !strings.EqualFold(hex.EncodeToString(digest[:]), p.entry.SHA256) {
					fail(index, "paranoid", "server body SHA-256 does not match verified backup")
					continue
				}
			}
			eligible[index] = &Eligible{ID: p.id, IDString: plan.Items[index].ID, Identity: imapx.MessageIdentity{MessageID: p.entry.MessageID, SizeBytes: p.entry.Size}}
		}
	}

	for index := range plan.Items {
		switch {
		case outcomes[index] != nil:
			result.Failures = append(result.Failures, *outcomes[index])
		case gone[index]:
			result.AlreadyGone = append(result.AlreadyGone, Failure{ID: plan.Items[index].ID, Gate: "server", Reason: "message no longer exists on the server; verified local backup is present"})
		case eligible[index] != nil:
			result.Eligible = append(result.Eligible, *eligible[index])
		}
	}
	return result, nil
}

// fetchEnvelopesByUID batches envelope fetches in bounded chunks (a single
// FETCH with thousands of UIDs risks server limits) and returns them keyed by
// UID. A missing UID means the message is no longer on the server.
func fetchEnvelopesByUID(ctx context.Context, reader imapx.Reader, folder string, uidValidity uint32, uids []uint32) (map[uint32]mailmodel.Envelope, error) {
	byUID := make(map[uint32]mailmodel.Envelope, len(uids))
	for _, chunk := range chunkUIDs(uids, gateFetchChunk) {
		envelopes, err := reader.FetchEnvelopes(ctx, folder, uidValidity, chunk)
		if err != nil {
			return nil, err
		}
		for _, envelope := range envelopes {
			byUID[envelope.UID] = envelope
		}
	}
	return byUID, nil
}

func fetchHeadersByUID(ctx context.Context, reader imapx.Reader, uids []uint32) (map[uint32]mailmodel.HeaderFields, error) {
	byUID := make(map[uint32]mailmodel.HeaderFields, len(uids))
	for _, chunk := range chunkUIDs(uids, gateFetchChunk) {
		headers, err := reader.FetchHeaderFields(ctx, chunk)
		if err != nil {
			return nil, err
		}
		for _, header := range headers {
			byUID[header.UID] = header
		}
	}
	return byUID, nil
}

const gateFetchChunk = 500

func chunkUIDs(uids []uint32, size int) [][]uint32 {
	var chunks [][]uint32
	for start := 0; start < len(uids); start += size {
		end := start + size
		if end > len(uids) {
			end = len(uids)
		}
		chunks = append(chunks, uids[start:end])
	}
	return chunks
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
