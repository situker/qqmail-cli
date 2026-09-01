package imapx

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/mailmodel"
)

type literal struct{ *bytes.Reader }

func (l literal) Size() int64 { return int64(l.Len()) }

func TestReadOnlyFlowAgainstMemoryServer(t *testing.T) {
	serverTLS, clientTLS := testTLS(t)
	mem := imapmemserver.New()
	user := imapmemserver.NewUser("user@qq.com", "abcdefghijklmnop")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	if err := user.Create("客户邮件", nil); err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: \"=?UTF-8?B?5byg5LiJ?=\" <sender@example.com>\r\nTo: user@qq.com\r\nSubject: =?UTF-8?B?5rWL6K+V?=\r\nDate: Tue, 01 Sep 2026 08:00:00 +0800\r\n\r\nfixture body\r\n")
	if _, err := user.Append("INBOX", literal{bytes.NewReader(raw)}, &imap.AppendOptions{}); err != nil {
		t.Fatal(err)
	}
	mem.AddUser(user)
	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:      imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapNamespace: {}, imap.CapUIDPlus: {}},
		TLSConfig: serverTLS,
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsListener := tls.NewListener(listener, serverTLS)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(tlsListener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
		select {
		case err := <-serveDone:
			if err != nil && !strings.Contains(err.Error(), "closed") {
				t.Errorf("server: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("memory server did not stop")
		}
	})
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := dial(ctx, account.Named{Name: "test", Email: "user@qq.com", IMAPHost: host, IMAPPort: port}, "abcdefghijklmnop", clientTLS, DefaultCommandTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Logout(context.Background()) }()

	before, after := client.Capabilities()
	if len(before) == 0 || len(after) == 0 {
		t.Fatal("capability discovery did not run before and after login")
	}
	folders, err := client.ListFolders(ctx)
	if err != nil || len(folders) != 2 {
		t.Fatalf("folders: %#v, %v", folders, err)
	}
	validity, _, err := client.Examine(ctx, "INBOX")
	if err != nil || validity == 0 {
		t.Fatalf("examine: %d, %v", validity, err)
	}
	ids, err := client.Search(ctx, SearchFilter{Unread: true, Limit: 10})
	if err != nil || len(ids) != 1 {
		t.Fatalf("search: %v, %v", ids, err)
	}
	envelopes, err := client.FetchEnvelopes(ctx, "INBOX", validity, ids)
	if err != nil || len(envelopes) != 1 || envelopes[0].Subject != "测试" || len(envelopes[0].From) != 1 || envelopes[0].From[0].Name != "张三" {
		t.Fatalf("envelopes: %#v, %v", envelopes, err)
	}
	id := mailmodel.MsgID{Folder: "INBOX", UIDValidity: validity, UID: ids[0]}
	gotRaw, truncated, err := client.FetchBodyPeek(ctx, id, MaxMessageBytes)
	if err != nil || truncated || !bytes.Equal(gotRaw, raw) {
		t.Fatalf("peek: truncated=%v err=%v raw=%q", truncated, err, gotRaw)
	}
	idsAfter, err := client.Search(ctx, SearchFilter{Unread: true, Limit: 10})
	if err != nil || len(idsAfter) != 1 {
		t.Fatalf("BODY.PEEK changed unread flags: %v, %v", idsAfter, err)
	}
	_, _, err = client.FetchBodyPeek(ctx, mailmodel.MsgID{Folder: "INBOX", UIDValidity: validity + 1, UID: ids[0]}, MaxMessageBytes)
	var appErr *errmap.Error
	if !errors.As(err, &appErr) || appErr.Kind != errmap.StaleID {
		t.Fatalf("expected stale_id, got %v", err)
	}
}

func testTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "qqmailctl test"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	server := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}, MinVersion: tls.VersionTLS12}
	client := &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}
	return server, client
}
