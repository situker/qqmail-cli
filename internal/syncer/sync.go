package syncer

import (
	"context"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/index"
	"github.com/situker/qqmailctl/internal/mailmodel"
	"github.com/situker/qqmailctl/internal/mimeparse"
)

const (
	recentFlagWindow = 200
	previewBytes     = 2 << 10
)

type Options struct {
	CachePreviews bool
	CacheBodies   bool
}

type FolderResult struct {
	Name               string `json:"name"`
	Indexed            int    `json:"indexed"`
	UIDValidity        uint32 `json:"uidvalidity"`
	UIDValidityChanged bool   `json:"uidvalidity_changed"`
	LastSeenUID        uint32 `json:"last_seen_uid"`
}

type Result struct {
	Folders  []FolderResult `json:"folders"`
	Indexed  int            `json:"indexed"`
	Warnings []string       `json:"warnings"`
}

func Run(ctx context.Context, reader imapx.Reader, store *index.DB, opts Options) (Result, error) {
	folders, err := reader.ListFolders(ctx)
	if err != nil {
		return Result{}, err
	}
	result := Result{Folders: []FolderResult{}, Warnings: []string{}}
	for _, folder := range folders {
		if hasAttribute(folder.Attributes, `\Noselect`) {
			continue
		}
		folderResult, err := syncFolder(ctx, reader, store, folder, opts)
		if err != nil {
			return result, err
		}
		result.Folders = append(result.Folders, folderResult)
		result.Indexed += folderResult.Indexed
		if folderResult.UIDValidityChanged {
			result.Warnings = append(result.Warnings, "folder "+folder.Name+" UIDVALIDITY changed; its local rows were cleared and rebuilt")
		}
	}
	return result, nil
}

func syncFolder(ctx context.Context, reader imapx.Reader, store *index.DB, folder mailmodel.Folder, opts Options) (FolderResult, error) {
	uidValidity, _, err := reader.Examine(ctx, folder.Name)
	if err != nil {
		return FolderResult{}, err
	}
	state, reset, err := store.PrepareFolder(ctx, folder.Name, folder.Delimiter, uidValidity)
	if err != nil {
		return FolderResult{}, err
	}
	newUIDs, err := reader.Search(ctx, imapx.SearchFilter{AfterUID: state.LastSeenUID})
	if err != nil {
		return FolderResult{}, err
	}
	recentUIDs, err := reader.Search(ctx, imapx.SearchFilter{Limit: recentFlagWindow})
	if err != nil {
		return FolderResult{}, err
	}
	uids := uniqueUIDs(newUIDs, recentUIDs)
	envelopes, err := reader.FetchEnvelopes(ctx, folder.Name, uidValidity, uids)
	if err != nil {
		return FolderResult{}, err
	}
	headers, err := reader.FetchHeaderFields(ctx, uids)
	if err != nil {
		return FolderResult{}, err
	}
	headerByUID := make(map[uint32]mailmodel.HeaderFields, len(headers))
	for _, header := range headers {
		headerByUID[header.UID] = header
	}
	messages := make([]index.Message, 0, len(envelopes))
	for _, envelope := range envelopes {
		header := headerByUID[envelope.UID]
		message := index.Message{
			ID: envelope.ID, UID: envelope.UID, UIDValidity: envelope.UIDValidity, Folder: envelope.Folder,
			Flags: envelope.Flags, InternalDate: envelope.InternalDate, SizeBytes: envelope.Size, Subject: envelope.Subject,
			DateHeader: envelope.Date, MessageID: header.MessageID, ListUnsubscribe: header.ListUnsubscribe,
			Precedence: header.Precedence, HasAttachments: envelope.HasAttachments,
		}
		if len(envelope.From) > 0 {
			message.FromAddr, message.FromName = envelope.From[0].Email, envelope.From[0].Name
		}
		for _, address := range envelope.To {
			message.ToAddrs = append(message.ToAddrs, address.Email)
		}
		if opts.CachePreviews || opts.CacheBodies {
			id, parseErr := mailmodel.ParseMsgID(envelope.ID)
			if parseErr != nil {
				return FolderResult{}, parseErr
			}
			raw, _, fetchErr := reader.FetchBodyPeek(ctx, id, imapx.MaxMessageBytes)
			if fetchErr != nil {
				return FolderResult{}, fetchErr
			}
			parsed := mimeparse.Parse(raw)
			if parsed.Text != nil {
				if opts.CachePreviews {
					preview, _ := truncateUTF8(*parsed.Text, previewBytes)
					message.BodyPreview = &preview
				}
				if opts.CacheBodies {
					body := *parsed.Text
					message.BodyText = &body
				}
			}
		}
		messages = append(messages, message)
	}
	if err := store.UpsertMessages(ctx, state.ID, messages); err != nil {
		return FolderResult{}, err
	}
	lastSeen := state.LastSeenUID
	for _, uid := range newUIDs {
		if uid > lastSeen {
			lastSeen = uid
		}
	}
	return FolderResult{Name: folder.Name, Indexed: len(newUIDs), UIDValidity: uidValidity, UIDValidityChanged: reset, LastSeenUID: lastSeen}, nil
}

func uniqueUIDs(groups ...[]uint32) []uint32 {
	seen := map[uint32]bool{}
	result := []uint32{}
	for _, group := range groups {
		for _, uid := range group {
			if uid == 0 || seen[uid] {
				continue
			}
			seen[uid] = true
			result = append(result, uid)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] > result[j] })
	return result
}

func hasAttribute(attributes []string, want string) bool {
	for _, attribute := range attributes {
		if strings.EqualFold(attribute, want) {
			return true
		}
	}
	return false
}

func truncateUTF8(value string, max int) (string, bool) {
	if len(value) <= max {
		return value, false
	}
	value = value[:max]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value, true
}
