package sendmail

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	smtp "github.com/emersion/go-smtp"
	"github.com/situker/qqmail-cli/internal/errmap"
)

func fixtureTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	clientTLS := &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}
	return serverTLS, clientTLS
}

// startTLSServer speaks just enough plaintext SMTP to complete STARTTLS and a
// post-upgrade EHLO, then stalls.
func startTLSServer(t *testing.T, serverTLS *tls.Config, accepted *atomic.Int32) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			if accepted != nil {
				accepted.Add(1)
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				writer := bufio.NewWriter(conn)
				reader := bufio.NewReader(conn)
				_, _ = writer.WriteString("220 fixture ESMTP\r\n")
				_ = writer.Flush()
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					upper := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(upper, "EHLO"):
						_, _ = writer.WriteString("250-fixture\r\n250 STARTTLS\r\n")
						_ = writer.Flush()
					case upper == "STARTTLS":
						_, _ = writer.WriteString("220 go ahead\r\n")
						_ = writer.Flush()
						secured := tls.Server(conn, serverTLS)
						if err := secured.Handshake(); err != nil {
							return
						}
						securedReader := bufio.NewReader(secured)
						securedWriter := bufio.NewWriter(secured)
						for {
							line, err := securedReader.ReadString('\n')
							if err != nil {
								return
							}
							if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "EHLO") {
								_, _ = securedWriter.WriteString("250 fixture\r\n")
								_ = securedWriter.Flush()
							} else {
								return
							}
						}
					default:
						return
					}
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func TestDialFallsBackTo587OnlyOnConnectFailure(t *testing.T) {
	serverTLS, clientTLS := fixtureTLS(t)
	fallback := startTLSServer(t, serverTLS, nil)
	_, fallbackPort, _ := net.SplitHostPort(fallback.Addr().String())
	// Primary port has no listener: connection failure must trigger fallback.
	deadPrimary, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, primaryPort, _ := net.SplitHostPort(deadPrimary.Addr().String())
	_ = deadPrimary.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := dialWithFallback(ctx, "127.0.0.1", primaryPort, fallbackPort, clientTLS)
	if err != nil {
		t.Fatalf("fallback dial failed: %v", err)
	}
	_ = client.Close()
}

func TestDialNeverTouchesFallbackWhenPrimaryWorks(t *testing.T) {
	serverTLS, clientTLS := fixtureTLS(t)
	primary, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = primary.Close() })
	go func() {
		for {
			conn, err := primary.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				secured := tls.Server(conn, serverTLS)
				if err := secured.Handshake(); err != nil {
					return
				}
				writer := bufio.NewWriter(secured)
				_, _ = writer.WriteString("220 fixture ESMTP\r\n")
				_ = writer.Flush()
				reader := bufio.NewReader(secured)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "EHLO") {
						_, _ = writer.WriteString("250 fixture\r\n")
						_ = writer.Flush()
					} else {
						return
					}
				}
			}(conn)
		}
	}()
	var fallbackAccepts atomic.Int32
	fallback := startTLSServer(t, serverTLS, &fallbackAccepts)
	_, fallbackPort, _ := net.SplitHostPort(fallback.Addr().String())
	_, primaryPort, _ := net.SplitHostPort(primary.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := dialWithFallback(ctx, "127.0.0.1", primaryPort, fallbackPort, clientTLS)
	if err != nil {
		t.Fatalf("primary dial failed: %v", err)
	}
	_ = client.Close()
	if fallbackAccepts.Load() != 0 {
		t.Fatalf("fallback endpoint was touched %d times despite a working primary", fallbackAccepts.Load())
	}
}

func TestClassifySMTPSemantics(t *testing.T) {
	if err := classifySMTP(&smtp.SMTPError{Code: 450, Message: "too many"}); errmap.Classify(err).Kind != errmap.RateLimited {
		t.Fatalf("450 not classified as rate_limited: %v", err)
	}
	if err := classifySMTP(&smtp.SMTPError{Code: 550, Message: "IP frequency limited"}); errmap.Classify(err).Kind != errmap.RateLimited {
		t.Fatalf("550 frequency not classified as rate_limited: %v", err)
	}
	if err := classifySMTP(&smtp.SMTPError{Code: 535, Message: "Login Fail"}); errmap.Classify(err).Kind != errmap.AuthFailed {
		t.Fatalf("535 not classified as auth_failed: %v", err)
	}
	if err := classifySMTP(&smtp.SMTPError{Code: 554, Message: "spam rejected"}); errmap.Classify(err).Kind != errmap.Network {
		t.Fatalf("554 not classified as network: %v", err)
	}
}
