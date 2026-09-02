package imapx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"sort"
	"strings"
	"time"

	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/mailmodel"
)

const (
	DefaultDialTimeout    = 15 * time.Second
	DefaultCommandTimeout = 60 * time.Second
	MaxMessageBytes       = 64 << 20
)

type SearchFilter struct {
	Unread    bool
	From      string
	Subject   string
	Since     time.Time
	BeforeUID uint32
	AfterUID  uint32
	Limit     int
}

// Reader is deliberately read-only. Its method whitelist is tested so server
// mutation cannot be introduced accidentally in v0.1.
type Reader interface {
	Capabilities() (before, after []string)
	ListFolders(context.Context) ([]mailmodel.Folder, error)
	Examine(context.Context, string) (uidValidity uint32, count uint32, err error)
	Search(context.Context, SearchFilter) ([]uint32, error)
	FetchEnvelopes(context.Context, string, uint32, []uint32) ([]mailmodel.Envelope, error)
	FetchHeaderFields(context.Context, []uint32) ([]mailmodel.HeaderFields, error)
	FetchMessage(context.Context, mailmodel.MsgID) ([]byte, error)
	FetchBodyPeek(context.Context, mailmodel.MsgID, int64) ([]byte, bool, error)
	Logout(context.Context) error
}

type Client struct {
	raw            *imapclient.Client
	conn           net.Conn
	commandTimeout time.Duration
	capBefore      []string
	capAfter       []string
}

func Dial(ctx context.Context, cfg account.Named, authCode string) (*Client, error) {
	return dialWithVersion(ctx, cfg, authCode, nil, DefaultCommandTimeout, "dev")
}

func dial(ctx context.Context, cfg account.Named, authCode string, tlsOverride *tls.Config, commandTimeout time.Duration) (*Client, error) {
	return dialWithVersion(ctx, cfg, authCode, tlsOverride, commandTimeout, "dev")
}

func DialWithVersion(ctx context.Context, cfg account.Named, authCode, version string) (*Client, error) {
	return dialWithVersion(ctx, cfg, authCode, nil, DefaultCommandTimeout, version)
}

func dialWithVersion(ctx context.Context, cfg account.Named, authCode string, tlsOverride *tls.Config, commandTimeout time.Duration, version string) (*Client, error) {
	dialer := &net.Dialer{Timeout: DefaultDialTimeout}
	plain, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(cfg.IMAPHost, fmt.Sprint(cfg.IMAPPort)))
	if err != nil {
		return nil, &errmap.Error{Kind: errmap.Network, Message: "无法连接 IMAP 服务器", Context: map[string]any{"server": cfg.IMAPHost + ":" + fmt.Sprint(cfg.IMAPPort)}, Cause: err}
	}
	tlsConfig := tlsOverride
	if tlsConfig == nil {
		tlsConfig = &tls.Config{ServerName: cfg.IMAPHost, MinVersion: tls.VersionTLS12}
	} else {
		tlsConfig = tlsConfig.Clone()
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = cfg.IMAPHost
		}
		if tlsConfig.MinVersion == 0 {
			tlsConfig.MinVersion = tls.VersionTLS12
		}
	}
	tlsConn := tls.Client(plain, tlsConfig)
	if deadline, ok := deadlineFor(ctx, DefaultCommandTimeout); ok {
		_ = plain.SetDeadline(deadline)
	}
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = plain.Close()
		return nil, &errmap.Error{Kind: errmap.TLS, Message: "TLS 握手或证书校验失败", Suggestion: "检查系统时间、证书链或企业中间人代理", Context: map[string]any{"server": cfg.IMAPHost + ":" + fmt.Sprint(cfg.IMAPPort)}, Cause: err}
	}
	client := &Client{conn: tlsConn, commandTimeout: commandTimeout}
	client.raw = imapclient.New(tlsConn, &imapclient.Options{WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader}})

	if err := client.setDeadline(ctx); err != nil {
		client.close()
		return nil, err
	}
	stopBefore := client.watchdog(ctx)
	before, err := client.raw.Capability().Wait()
	stopBefore()
	if err != nil {
		client.close()
		return nil, err
	}
	client.capBefore = capStrings(before)

	if err := client.setDeadline(ctx); err != nil {
		client.close()
		return nil, err
	}
	stopLogin := client.watchdog(ctx)
	if err := client.raw.Login(cfg.Email, authCode).Wait(); err != nil {
		stopLogin()
		client.close()
		return nil, classifyLoginError(err, cfg)
	}
	stopLogin()

	if err := client.setDeadline(ctx); err != nil {
		client.close()
		return nil, err
	}
	stopAfter := client.watchdog(ctx)
	after, err := client.raw.Capability().Wait()
	stopAfter()
	if err != nil {
		client.close()
		return nil, err
	}
	client.capAfter = capStrings(after)
	if after.Has(imap.CapID) {
		if err := client.setDeadline(ctx); err != nil {
			client.close()
			return nil, err
		}
		stopID := client.watchdog(ctx)
		_, err := client.raw.ID(&imap.IDData{Name: "qqmailctl", Version: version}).Wait()
		stopID()
		if err != nil {
			client.close()
			return nil, fmt.Errorf("IMAP ID failed: %w", err)
		}
	}
	return client, nil
}

func classifyLoginError(cause error, cfg account.Named) error {
	var networkError net.Error
	if errors.As(cause, &networkError) || errors.Is(cause, io.EOF) || errors.Is(cause, io.ErrUnexpectedEOF) {
		return &errmap.Error{Kind: errmap.Network, Message: "等待 IMAP 登录响应超时", Suggestion: "停止本次连接；若刚刚频繁登录，请等待 10-15 分钟", Context: map[string]any{"server": cfg.IMAPHost + ":" + fmt.Sprint(cfg.IMAPPort)}, Cause: cause}
	}
	message := strings.ToLower(cause.Error())
	contextData := map[string]any{"server": cfg.IMAPHost + ":" + fmt.Sprint(cfg.IMAPPort)}
	if strings.Contains(message, "too many") || strings.Contains(message, "rate") || strings.Contains(message, "frequency") || strings.Contains(message, "temporarily blocked") {
		return &errmap.Error{Kind: errmap.RateLimited, Message: "QQ 邮箱暂时拒绝了频繁连接", Suggestion: "停止重试并等待 10-15 分钟", Context: contextData, Cause: cause}
	}
	if strings.Contains(message, "not enabled") || strings.Contains(message, "service disabled") || strings.Contains(message, "imap disabled") {
		return &errmap.Error{Kind: errmap.ServiceNotEnabled, Message: "QQ 邮箱 IMAP 服务可能尚未开启", Suggestion: "在 QQ 邮箱网页端的账号与安全设置中开启 IMAP 服务并生成授权码", Context: contextData, Cause: cause}
	}
	return &errmap.Error{Kind: errmap.AuthFailed, Message: "认证失败：QQ 邮箱拒绝了登录", Suggestion: "确认填的是 16 位授权码而不是 QQ 密码；若最近改过 QQ 密码，请重新生成授权码", Context: contextData, Cause: cause}
}

func Verify(ctx context.Context, cfg account.Named, authCode string) error {
	client, err := Dial(ctx, cfg, authCode)
	if err != nil {
		return err
	}
	return client.Logout(ctx)
}

func (c *Client) Capabilities() ([]string, []string) {
	return append([]string(nil), c.capBefore...), append([]string(nil), c.capAfter...)
}

func (c *Client) ListFolders(ctx context.Context) ([]mailmodel.Folder, error) {
	if err := c.setDeadline(ctx); err != nil {
		return nil, err
	}
	stop := c.watchdog(ctx)
	defer stop()
	items, err := c.raw.List("", "*", nil).Collect()
	if err != nil {
		return nil, err
	}
	result := make([]mailmodel.Folder, 0, len(items))
	for _, item := range items {
		attrs := make([]string, len(item.Attrs))
		for i, attr := range item.Attrs {
			attrs[i] = string(attr)
		}
		result = append(result, mailmodel.Folder{Name: item.Mailbox, Delimiter: string(item.Delim), Attributes: attrs})
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result, nil
}

func (c *Client) Examine(ctx context.Context, folder string) (uint32, uint32, error) {
	if err := c.setDeadline(ctx); err != nil {
		return 0, 0, err
	}
	stop := c.watchdog(ctx)
	defer stop()
	selected, err := c.raw.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return 0, 0, &errmap.Error{Kind: errmap.NotFound, Message: "无法以只读方式打开文件夹", Context: map[string]any{"folder": folder}, Cause: err}
	}
	return selected.UIDValidity, selected.NumMessages, nil
}

func (c *Client) Search(ctx context.Context, filter SearchFilter) ([]uint32, error) {
	criteria := &imap.SearchCriteria{Since: filter.Since}
	if filter.Unread {
		criteria.NotFlag = []imap.Flag{imap.FlagSeen}
	}
	if filter.From != "" {
		criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{Key: "From", Value: filter.From})
	}
	if filter.Subject != "" {
		criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{Key: "Subject", Value: filter.Subject})
	}
	if filter.AfterUID > 0 || filter.BeforeUID > 1 {
		start, stop := imap.UID(1), imap.UID(0)
		if filter.AfterUID > 0 {
			start = imap.UID(filter.AfterUID + 1)
		}
		if filter.BeforeUID > 1 {
			stop = imap.UID(filter.BeforeUID - 1)
		}
		if stop == 0 || start <= stop {
			criteria.UID = []imap.UIDSet{{imap.UIDRange{Start: start, Stop: stop}}}
		} else {
			return []uint32{}, nil
		}
	}
	if err := c.setDeadline(ctx); err != nil {
		return nil, err
	}
	stop := c.watchdog(ctx)
	defer stop()
	data, err := c.raw.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, err
	}
	ids := filterUIDWindow(data.AllUIDs(), filter)
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	if filter.Limit > 0 && len(ids) > filter.Limit {
		ids = ids[:filter.Limit]
	}
	result := make([]uint32, len(ids))
	for i, id := range ids {
		result[i] = uint32(id)
	}
	return result, nil
}

// bodySectionBytes returns the requested body section's bytes, tolerating
// servers whose FETCH responses label the section differently from the
// request. Live observation (2026-09-02, docs/compat/qq-20260902.md): QQ's
// response section spec does not match what was requested, so the library's
// FindBodySection silently returns nil. Every fetch here requests exactly one
// section, so a response carrying exactly one is unambiguous.
func bodySectionBytes(item *imapclient.FetchMessageBuffer, section *imap.FetchItemBodySection) []byte {
	if data := item.FindBodySection(section); data != nil {
		return data
	}
	if len(item.BodySection) == 1 {
		return item.BodySection[0].Bytes
	}
	return nil
}

// filterUIDWindow drops UIDs outside the requested window. RFC 3501 defines
// "N:*" as always matching the highest UID in the mailbox even when N exceeds
// it, so a watermark search with no new mail still returns the newest message;
// without this filter watch/sync would re-announce it on every poll.
func filterUIDWindow(ids []imap.UID, filter SearchFilter) []imap.UID {
	if filter.AfterUID == 0 && filter.BeforeUID <= 1 {
		return ids
	}
	filtered := ids[:0]
	for _, id := range ids {
		if filter.AfterUID > 0 && uint32(id) <= filter.AfterUID {
			continue
		}
		if filter.BeforeUID > 1 && uint32(id) >= filter.BeforeUID {
			continue
		}
		filtered = append(filtered, id)
	}
	return filtered
}

func (c *Client) FetchHeaderFields(ctx context.Context, ids []uint32) ([]mailmodel.HeaderFields, error) {
	if len(ids) == 0 {
		return []mailmodel.HeaderFields{}, nil
	}
	uids := make([]imap.UID, len(ids))
	for i, id := range ids {
		uids[i] = imap.UID(id)
	}
	// Full header, not a HEADER.FIELDS subset: QQ answers HEADER.FIELDS with
	// an empty two-byte body while returning the complete header normally
	// (live probe 2026-09-02, docs/compat/qq-20260902.md). Headers average a
	// few KB per message; the fields are picked out locally.
	section := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, Peek: true}
	if err := c.setDeadline(ctx); err != nil {
		return nil, err
	}
	stop := c.watchdog(ctx)
	defer stop()
	items, err := c.raw.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{section}}).Collect()
	if err != nil {
		return nil, err
	}
	result := make([]mailmodel.HeaderFields, 0, len(items))
	for _, item := range items {
		fields := mailmodel.HeaderFields{UID: uint32(item.UID)}
		message, readErr := mail.ReadMessage(strings.NewReader(string(bodySectionBytes(item, section)) + "\r\n"))
		if readErr == nil {
			fields.MessageID = strings.TrimSpace(message.Header.Get("Message-ID"))
			fields.ListUnsubscribe = strings.TrimSpace(message.Header.Get("List-Unsubscribe"))
			fields.Precedence = strings.TrimSpace(message.Header.Get("Precedence"))
		}
		result = append(result, fields)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UID > result[j].UID })
	return result, nil
}

func (c *Client) FetchEnvelopes(ctx context.Context, folder string, uidValidity uint32, ids []uint32) ([]mailmodel.Envelope, error) {
	if len(ids) == 0 {
		return []mailmodel.Envelope{}, nil
	}
	uids := make([]imap.UID, len(ids))
	for i, id := range ids {
		uids[i] = imap.UID(id)
	}
	// QQ dialect hardening (2026-09-02, docs/compat/qq-20260902.md): QQ emits
	// malformed ENVELOPE and BODYSTRUCTURE responses for some real messages,
	// and either one desyncs go-imap's wire decoder and kills the session —
	// one poison message would permanently break sync. The server is
	// therefore trusted only for numbers and flags; every piece of structured
	// text (subject, addresses, date) arrives as raw header bytes in a
	// length-prefixed literal — which a noncompliant serializer cannot
	// corrupt — and is parsed locally. The full header is requested because
	// QQ answers HEADER.FIELDS subsets with an empty body. Attachment
	// detection likewise parses the raw message locally.
	section := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, Peek: true}
	if err := c.setDeadline(ctx); err != nil {
		return nil, err
	}
	stop := c.watchdog(ctx)
	defer stop()
	items, err := c.raw.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, Flags: true, InternalDate: true, RFC822Size: true, BodySection: []*imap.FetchItemBodySection{section}}).Collect()
	if err != nil {
		return nil, err
	}
	result := make([]mailmodel.Envelope, 0, len(items))
	for _, item := range items {
		if item.UID == 0 {
			continue
		}
		flags := make([]string, len(item.Flags))
		for i, flag := range item.Flags {
			flags[i] = string(flag)
		}
		envelope := mailmodel.Envelope{
			ID: mailmodel.MsgID{Folder: folder, UIDValidity: uidValidity, UID: uint32(item.UID)}.String(), UID: uint32(item.UID), UIDValidity: uidValidity,
			Folder: folder, From: []mailmodel.Address{}, To: []mailmodel.Address{}, Date: item.InternalDate,
			InternalDate: item.InternalDate, Size: item.RFC822Size, Flags: flags, HasAttachments: false,
		}
		if message, readErr := mail.ReadMessage(strings.NewReader(string(bodySectionBytes(item, section)) + "\r\n")); readErr == nil {
			envelope.Subject = mailmodel.DecodeHeaderText(message.Header.Get("Subject"))
			envelope.From = mailmodel.ParseAddressListLenient(message.Header.Get("From"))
			envelope.To = mailmodel.ParseAddressListLenient(message.Header.Get("To"))
			if date, dateErr := message.Header.Date(); dateErr == nil {
				envelope.Date = date
			}
		}
		result = append(result, envelope)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UID > result[j].UID })
	return result, nil
}

func (c *Client) FetchMessage(ctx context.Context, id mailmodel.MsgID) ([]byte, error) {
	raw, _, err := c.FetchBodyPeek(ctx, id, MaxMessageBytes)
	return raw, err
}

func (c *Client) FetchBodyPeek(ctx context.Context, id mailmodel.MsgID, maxBytes int64) ([]byte, bool, error) {
	validity, _, err := c.Examine(ctx, id.Folder)
	if err != nil {
		return nil, false, err
	}
	if validity != id.UIDValidity {
		return nil, false, &errmap.Error{Kind: errmap.StaleID, Message: "邮件 id 已失效：文件夹 UIDVALIDITY 已变化", Suggestion: "重新运行 envelope list 获取新 id", Context: map[string]any{"folder": id.Folder}}
	}
	if maxBytes <= 0 || maxBytes > MaxMessageBytes {
		maxBytes = MaxMessageBytes
	}
	section := &imap.FetchItemBodySection{Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: maxBytes + 1}}
	if err := c.setDeadline(ctx); err != nil {
		return nil, false, err
	}
	stop := c.watchdog(ctx)
	defer stop()
	items, err := c.raw.Fetch(imap.UIDSetNum(imap.UID(id.UID)), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{section}}).Collect()
	if err != nil {
		return nil, false, err
	}
	if len(items) == 0 {
		return nil, false, &errmap.Error{Kind: errmap.NotFound, Message: "邮件不存在", Context: map[string]any{"id": id.String()}}
	}
	raw := bodySectionBytes(items[0], section)
	truncated := int64(len(raw)) > maxBytes
	if truncated {
		raw = raw[:maxBytes]
	}
	return raw, truncated, nil
}

func (c *Client) Logout(ctx context.Context) error {
	if c.raw == nil {
		return nil
	}
	_ = c.setDeadline(ctx)
	stop := c.watchdog(ctx)
	defer stop()
	err := c.raw.Logout().Wait()
	c.close()
	return err
}

func (c *Client) close() {
	if c.raw != nil {
		_ = c.raw.Close()
	} else if c.conn != nil {
		_ = c.conn.Close()
	}
}

func (c *Client) setDeadline(ctx context.Context) error {
	deadline, ok := deadlineFor(ctx, c.commandTimeout)
	if !ok {
		return &errmap.Error{Kind: errmap.Network, Message: "操作已超时", Cause: context.DeadlineExceeded}
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return &errmap.Error{Kind: errmap.Network, Message: "无法设置网络超时", Cause: err}
	}
	return nil
}

// watchdog closes the socket at the command deadline. This is intentionally
// separate from net.Conn.SetDeadline: a dialect-violating server can leave the
// upstream client's command waiter blocked even after its reader observes a
// deadline error.
func (c *Client) watchdog(ctx context.Context) func() {
	done := make(chan struct{})
	deadline, ok := deadlineFor(ctx, c.commandTimeout)
	if !ok {
		_ = c.conn.Close()
		return func() {}
	}
	delay := time.Until(deadline)
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	go func() {
		select {
		case <-ctx.Done():
			_ = c.conn.Close()
		case <-timer.C:
			_ = c.conn.Close()
		case <-done:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}()
	return func() { close(done) }
}

func deadlineFor(ctx context.Context, timeout time.Duration) (time.Time, bool) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, false
	}
	want := time.Now().Add(timeout)
	if existing, ok := ctx.Deadline(); ok && existing.Before(want) {
		want = existing
	}
	return want, true
}

func capStrings(caps imap.CapSet) []string {
	result := make([]string, 0, len(caps))
	for cap := range caps {
		result = append(result, string(cap))
	}
	sort.Strings(result)
	return result
}

func addresses(values []imap.Address) []mailmodel.Address {
	result := make([]mailmodel.Address, 0, len(values))
	for _, value := range values {
		if value.IsGroupStart() || value.IsGroupEnd() {
			continue
		}
		result = append(result, mailmodel.Address{Name: mailmodel.DecodeHeaderText(value.Name), Email: value.Addr()})
	}
	return result
}
