package exporter

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/mailmodel"
	"github.com/situker/qqmailctl/internal/mimeparse"
	"github.com/situker/qqmailctl/internal/safeio"
	"github.com/situker/qqmailctl/internal/secrets"
)

const ManifestSchema = 1

type Entry struct {
	ID          string              `json:"id"`
	Folder      string              `json:"folder"`
	FolderRaw   string              `json:"folder_raw"`
	UID         uint32              `json:"uid"`
	UIDValidity uint32              `json:"uidvalidity"`
	Subject     string              `json:"subject"`
	From        []mailmodel.Address `json:"from"`
	Date        time.Time           `json:"date"`
	Size        int64               `json:"size_bytes"`
	MessageID   string              `json:"message_id,omitempty"`
	SHA256      string              `json:"sha256"`
	Path        string              `json:"path"`
}

type Manifest struct {
	Account     string    `json:"account"`
	ExportedAt  time.Time `json:"exported_at"`
	ToolVersion string    `json:"tool_version"`
	Schema      int       `json:"schema"`
	Messages    []Entry   `json:"messages"`
	HMAC        string    `json:"hmac"`
}

type Fetcher interface {
	FetchBodyPeek(context.Context, mailmodel.MsgID, int64) ([]byte, bool, error)
}

type Result struct {
	ManifestPath string   `json:"manifest_path"`
	Exported     int      `json:"exported"`
	Skipped      int      `json:"skipped"`
	Verified     int      `json:"verified"`
	Failures     []string `json:"failures"`
	// FailureDetails carries the classified error per failed message so agents
	// can decide what to retry; Failures keeps the original string shape for
	// contract compatibility.
	FailureDetails []FailureEntry `json:"failure_details"`
}

type FailureEntry struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Reason    string `json:"reason"`
	Retryable bool   `json:"retryable"`
}

func Export(ctx context.Context, fetcher Fetcher, named account.Named, ids []mailmodel.MsgID, outputDir string, keyring secrets.Provider, toolVersion string) (Result, error) {
	root := filepath.Join(outputDir, safeio.SanitizeFilename(named.Name, "account"))
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Result{}, err
	}
	manifestPath := filepath.Join(root, "manifest.json")
	manifest := Manifest{Account: named.Name, ExportedAt: time.Now().UTC(), ToolVersion: toolVersion, Schema: ManifestSchema, Messages: []Entry{}}
	key, err := getOrCreateKey(keyring, named.Email)
	if err != nil {
		return Result{}, err
	}
	if existing, err := loadManifest(manifestPath); err == nil {
		if err := verifySignature(existing, key); err != nil {
			return Result{}, fmt.Errorf("existing manifest failed HMAC verification: %w", err)
		}
		manifest.Messages = existing.Messages
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	byID := make(map[string]Entry, len(manifest.Messages))
	for _, entry := range manifest.Messages {
		byID[entry.ID] = entry
	}
	result := Result{ManifestPath: manifestPath, Failures: []string{}, FailureDetails: []FailureEntry{}}
	recordFailure := func(id mailmodel.MsgID, code, reason string, retryable bool) {
		result.Failures = append(result.Failures, id.String()+": "+reason)
		result.FailureDetails = append(result.FailureDetails, FailureEntry{ID: id.String(), Code: code, Reason: reason, Retryable: retryable})
	}
	for _, id := range ids {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		if old, ok := byID[id.String()]; ok {
			path := filepath.Join(root, filepath.FromSlash(old.Path))
			if hash, err := hashFile(path); err == nil && hmac.Equal([]byte(hash), []byte(old.SHA256)) {
				result.Skipped++
				continue
			}
		}
		raw, truncated, err := fetcher.FetchBodyPeek(ctx, id, imapx.MaxMessageBytes)
		if err != nil {
			detail, _ := errmap.Details(err)
			recordFailure(id, detail.Code, err.Error(), detail.Retryable)
			continue
		}
		if truncated {
			// A permanent condition: retrying cannot shrink the message.
			recordFailure(id, "parse_error", "message exceeds 64 MiB export limit", false)
			continue
		}
		folder := safeio.SanitizeFilename(id.Folder, "folder")
		relative := filepath.ToSlash(filepath.Join(folder, fmt.Sprintf("%d-%d.eml", id.UIDValidity, id.UID)))
		absolute := filepath.Join(root, filepath.FromSlash(relative))
		if err := writeAtomic(absolute, raw, 0o600); err != nil {
			recordFailure(id, "internal", err.Error(), false)
			continue
		}
		parsed := mimeparse.Parse(raw)
		hash := sha256.Sum256(raw)
		byID[id.String()] = Entry{ID: id.String(), Folder: folder, FolderRaw: id.Folder, UID: id.UID, UIDValidity: id.UIDValidity, Subject: parsed.Subject, From: parsed.From, Date: parsed.Date, Size: int64(len(raw)), MessageID: parsed.MessageID, SHA256: hex.EncodeToString(hash[:]), Path: relative}
		result.Exported++
	}
	manifest.Messages = manifest.Messages[:0]
	for _, entry := range byID {
		manifest.Messages = append(manifest.Messages, entry)
	}
	sort.Slice(manifest.Messages, func(i, j int) bool { return manifest.Messages[i].ID < manifest.Messages[j].ID })
	manifest.ExportedAt = time.Now().UTC()
	manifest.HMAC, err = sign(manifest, key)
	if err != nil {
		return result, err
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return result, err
	}
	if err := writeAtomic(manifestPath, append(raw, '\n'), 0o600); err != nil {
		return result, err
	}
	return result, nil
}

func Verify(outputDir string, named account.Named, keyring secrets.Provider) (Result, error) {
	root := filepath.Join(outputDir, safeio.SanitizeFilename(named.Name, "account"))
	manifestPath := filepath.Join(root, "manifest.json")
	manifest, err := loadManifest(manifestPath)
	if err != nil {
		return Result{}, err
	}
	key, err := getKey(keyring, named.Email)
	if err != nil {
		return Result{}, err
	}
	if err := verifySignature(manifest, key); err != nil {
		return Result{}, err
	}
	result := Result{ManifestPath: manifestPath, Failures: []string{}, FailureDetails: []FailureEntry{}}
	for _, entry := range manifest.Messages {
		absolute := filepath.Join(root, filepath.FromSlash(entry.Path))
		rel, relErr := filepath.Rel(root, absolute)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			result.Failures = append(result.Failures, entry.ID+": unsafe manifest path")
			continue
		}
		hash, hashErr := hashFile(absolute)
		if hashErr != nil {
			result.Failures = append(result.Failures, entry.ID+": "+hashErr.Error())
			continue
		}
		if !hmac.Equal([]byte(hash), []byte(entry.SHA256)) {
			result.Failures = append(result.Failures, entry.ID+": SHA-256 mismatch")
			continue
		}
		result.Verified++
	}
	if len(result.Failures) > 0 {
		return result, fmt.Errorf("backup verification failed for %d file(s)", len(result.Failures))
	}
	return result, nil
}

// LoadVerified returns a manifest only after its HMAC and every referenced
// local file hash have been verified.
func LoadVerified(outputDir string, named account.Named, keyring secrets.Provider) (Manifest, string, error) {
	result, err := Verify(outputDir, named, keyring)
	if err != nil {
		return Manifest{}, "", err
	}
	manifest, err := loadManifest(result.ManifestPath)
	if err != nil {
		return Manifest{}, "", err
	}
	return manifest, filepath.Dir(result.ManifestPath), nil
}

func getOrCreateKey(provider secrets.Provider, email string) ([]byte, error) {
	key, err := getKey(provider, email)
	if err == nil {
		return key, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	if err := provider.Set("export-hmac:"+email, base64.RawStdEncoding.EncodeToString(raw)); err != nil {
		return nil, err
	}
	return raw, nil
}

func getKey(provider secrets.Provider, email string) ([]byte, error) {
	encoded, err := provider.Get("export-hmac:" + email)
	if err != nil {
		return nil, err
	}
	key, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("invalid export HMAC key")
	}
	return key, nil
}

func sign(manifest Manifest, key []byte) (string, error) {
	manifest.HMAC = ""
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	digest := hmac.New(sha256.New, key)
	_, _ = digest.Write(raw)
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func verifySignature(manifest Manifest, key []byte) error {
	want, err := sign(manifest, key)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(want), []byte(manifest.HMAC)) {
		return fmt.Errorf("manifest HMAC mismatch")
	}
	return nil
}

func loadManifest(path string) (Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.Schema != ManifestSchema {
		return Manifest{}, fmt.Errorf("unsupported manifest schema %d", manifest.Schema)
	}
	return manifest, nil
}

func hashFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

func writeAtomic(path string, raw []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".qqmailctl-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err == nil {
		return nil
	}
	// Windows cannot atomically replace an existing file on every filesystem.
	backup := path + ".previous"
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Rename(backup, path)
		return err
	}
	_ = os.Remove(backup)
	return nil
}
