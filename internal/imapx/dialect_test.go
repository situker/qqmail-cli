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
