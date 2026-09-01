package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"time"

	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/mailmodel"
	"github.com/spf13/cobra"
)

type watchEvent struct {
	SchemaVersion       string              `json:"schema_version"`
	Event               string              `json:"event"`
	ObservedAt          time.Time           `json:"observed_at"`
	Folder              string              `json:"folder"`
	UIDValidity         uint32              `json:"uidvalidity"`
	PreviousUIDValidity uint32              `json:"previous_uidvalidity,omitempty"`
	Envelope            *mailmodel.Envelope `json:"envelope,omitempty"`
}

func newWatchCommand(rt *Runtime) *cobra.Command {
	var jsonl, once bool
	var interval time.Duration
	cmd := &cobra.Command{Use: "watch", Short: "Poll for new UIDs and emit one NDJSON event per change"}
	cmd.Flags().BoolVar(&jsonl, "jsonl", false, "required: emit newline-delimited JSON events")
	cmd.Flags().DurationVar(&interval, "interval", 60*time.Second, "poll interval (minimum 5s)")
	cmd.Flags().BoolVar(&once, "once", false, "poll once and exit (for automation and tests)")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if !jsonl {
			return &errmap.Error{Kind: errmap.Usage, Message: "watch 要求显式指定 --jsonl"}
		}
		if !once && interval < 5*time.Second {
			return &errmap.Error{Kind: errmap.Usage, Message: "--interval 不得短于 5s"}
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		reader, named, err := rt.connect(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Logout(context.Background()) }()
		watermark, uidValidity := uint32(0), uint32(0)
		if inspection, inspectErr := rt.IndexInspect(named.Name); inspectErr == nil && inspection.Exists {
			store, openErr := rt.IndexOpen(named.Name, false)
			if openErr != nil {
				return openErr
			}
			state, ok, stateErr := store.FolderState(ctx, rt.Folder)
			_ = store.Close()
			if stateErr != nil {
				return stateErr
			}
			if ok {
				watermark, uidValidity = state.LastSeenUID, state.UIDValidity
			}
		}
		encoder := json.NewEncoder(rt.Out)
		poll := func() error {
			events, err := pollWatch(ctx, reader, rt.Folder, &watermark, &uidValidity)
			if err != nil {
				return err
			}
			for _, event := range events {
				if err := encoder.Encode(event); err != nil {
					return err
				}
			}
			return nil
		}
		if err := poll(); err != nil {
			return err
		}
		if once {
			return nil
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if err := poll(); err != nil {
					return fmt.Errorf("watch poll failed: %w", err)
				}
			}
		}
	}
	return cmd
}

func pollWatch(ctx context.Context, reader imapx.Reader, folder string, watermark, knownValidity *uint32) ([]watchEvent, error) {
	uidValidity, _, err := reader.Examine(ctx, folder)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if *knownValidity == 0 || *knownValidity != uidValidity {
		previous := *knownValidity
		latest, err := reader.Search(ctx, imapx.SearchFilter{Limit: 1})
		if err != nil {
			return nil, err
		}
		*watermark = 0
		if len(latest) > 0 {
			*watermark = latest[0]
		}
		*knownValidity = uidValidity
		if previous != 0 {
			return []watchEvent{{SchemaVersion: "1", Event: "folder_reset", ObservedAt: now, Folder: folder, UIDValidity: uidValidity, PreviousUIDValidity: previous}}, nil
		}
		return []watchEvent{}, nil
	}
	ids, err := reader.Search(ctx, imapx.SearchFilter{AfterUID: *watermark})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []watchEvent{}, nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	envelopes, err := reader.FetchEnvelopes(ctx, folder, uidValidity, ids)
	if err != nil {
		return nil, err
	}
	byUID := make(map[uint32]mailmodel.Envelope, len(envelopes))
	for _, envelope := range envelopes {
		byUID[envelope.UID] = envelope
	}
	events := []watchEvent{}
	for _, uid := range ids {
		if uid > *watermark {
			*watermark = uid
		}
		envelope, ok := byUID[uid]
		if !ok {
			continue
		}
		copy := envelope
		events = append(events, watchEvent{SchemaVersion: "1", Event: "new_message", ObservedAt: now, Folder: folder, UIDValidity: uidValidity, Envelope: &copy})
	}
	return events, nil
}
