package imapx

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/mailmodel"
)

func TestUntaggedBADCannotHangLogin(t *testing.T) {
	serverTLS, clientTLS := testTLS(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		tlsConn := tls.Server(conn, serverTLS)
		if tlsConn.Handshake() != nil {
			return
		}
		_, _ = fmt.Fprint(tlsConn, "* OK [CAPABILITY IMAP4rev1] fixture ready\r\n")
		reader := bufio.NewReader(tlsConn)
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			tag := fields[0]
			switch strings.ToUpper(fields[1]) {
			case "CAPABILITY":
				_, _ = fmt.Fprintf(tlsConn, "* CAPABILITY IMAP4rev1\r\n%s OK capability\r\n", tag)
			case "LOGIN":
				// QQ has historically emitted this illegal untagged response and
				// omitted the tagged completion. The connection deadline must win.
				_, _ = fmt.Fprint(tlsConn, "* BAD Command!\r\n")
			}
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	_, err = dial(ctx, account.Named{Name: "dialect", Email: "user@qq.com", IMAPHost: host, IMAPPort: port}, "abcdefghijklmnop", clientTLS, 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected dialect timeout")
	}
	if time.Since(started) > time.Second {
		t.Fatalf("client hung too long: %v", time.Since(started))
	}
	classified := errmap.Classify(err)
	if classified.Kind != errmap.Network {
		t.Fatalf("expected network classification, got %s: %v", classified.Kind, err)
	}
	_ = listener.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dialect server did not stop")
	}
}

func TestConservativeCopyFlowNeverSendsBareExpunge(t *testing.T) {
	serverTLS, clientTLS := testTLS(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	commands := make(chan []string, 1)
	go func() {
		seen := []string{}
		defer func() { commands <- seen }()
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		tlsConn := tls.Server(conn, serverTLS)
		if tlsConn.Handshake() != nil {
			return
		}
		_, _ = fmt.Fprint(tlsConn, "* OK [CAPABILITY IMAP4rev1 UIDPLUS] fixture ready\r\n")
		reader := bufio.NewReader(tlsConn)
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return
			}
			seen = append(seen, strings.TrimSpace(line))
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			tag := fields[0]
			switch strings.ToUpper(fields[1]) {
			case "CAPABILITY":
				_, _ = fmt.Fprintf(tlsConn, "* CAPABILITY IMAP4rev1 UIDPLUS\r\n%s OK capability\r\n", tag)
			case "LOGIN":
				_, _ = fmt.Fprintf(tlsConn, "%s OK login\r\n", tag)
			case "SELECT":
				_, _ = fmt.Fprintf(tlsConn, "* FLAGS (\\Seen \\Deleted)\r\n* 1 EXISTS\r\n* OK [UIDVALIDITY 11] stable\r\n* OK [UIDNEXT 8] next\r\n%s OK [READ-WRITE] selected\r\n", tag)
			case "UID":
				if len(fields) > 2 && strings.EqualFold(fields[2], "COPY") {
					_, _ = fmt.Fprintf(tlsConn, "%s OK [COPYUID 22 7 99] copied\r\n", tag)
				} else {
					_, _ = fmt.Fprintf(tlsConn, "%s OK stored\r\n", tag)
				}
			case "LOGOUT":
				_, _ = fmt.Fprintf(tlsConn, "* BYE done\r\n%s OK logout\r\n", tag)
				return
			default:
				_, _ = fmt.Fprintf(tlsConn, "%s BAD unsupported\r\n", tag)
			}
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := dial(ctx, account.Named{Name: "dialect", Email: "user@qq.com", IMAPHost: host, IMAPPort: port}, "abcdefghijklmnop", clientTLS, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	id := mailmodel.MsgID{Folder: "INBOX", UIDValidity: 11, UID: 7}
	result, err := client.CopyMarkDeletedUID(ctx, id, "Trash", MessageIdentity{MessageID: "fixture@example.com", SizeBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !result.SourceRetained || !result.SourceMarkedDeleted {
		t.Fatalf("unexpected conservative result: %+v", result)
	}
	if err := client.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	for _, line := range <-commands {
		if strings.Contains(strings.ToUpper(line), " EXPUNGE") {
			t.Fatalf("unsafe command sent: %s", line)
		}
	}
}
