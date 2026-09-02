package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

func probeS10(result *result) error {
	const host = "smtp.qq.com"
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "587"), 15*time.Second)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	code, _, err := readSMTPReply(reader)
	if err != nil || code != 220 {
		return fmt.Errorf("unexpected SMTP greeting status %d: %w", code, err)
	}
	if _, err := writer.WriteString("EHLO qqmail-cli.invalid\r\n"); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	code, before, err := readSMTPReply(reader)
	if err != nil || code != 250 {
		return fmt.Errorf("EHLO failed with status %d: %w", code, err)
	}
	if !smtpHasCapability(before, "STARTTLS") {
		result.Observations["starttls_advertised"] = false
		result.Status = "completed_with_limitation"
		return nil
	}
	if _, err := writer.WriteString("STARTTLS\r\n"); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	code, _, err = readSMTPReply(reader)
	if err != nil || code != 220 {
		return fmt.Errorf("STARTTLS rejected with status %d: %w", code, err)
	}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err := tlsConn.Handshake(); err != nil {
		return err
	}
	state := tlsConn.ConnectionState()
	reader = bufio.NewReader(tlsConn)
	writer = bufio.NewWriter(tlsConn)
	if _, err := writer.WriteString("EHLO qqmail-cli.invalid\r\n"); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	code, after, err := readSMTPReply(reader)
	if err != nil || code != 250 {
		return fmt.Errorf("post-TLS EHLO failed with status %d: %w", code, err)
	}
	result.Observations["starttls_advertised"] = true
	result.Observations["tls"] = map[string]any{"version": tlsVersion(state.Version), "cipher_suite": tls.CipherSuiteName(state.CipherSuite), "verified_chains": len(state.VerifiedChains)}
	result.Observations["capabilities_before_tls"] = smtpCapabilities(before)
	result.Observations["capabilities_after_tls"] = smtpCapabilities(after)
	_, _ = writer.WriteString("QUIT\r\n")
	_ = writer.Flush()
	return nil
}

func readSMTPReply(reader *bufio.Reader) (int, []string, error) {
	lines := []string{}
	for len(lines) < 100 {
		line, err := reader.ReadString('\n')
		if err != nil {
			return 0, lines, err
		}
		line = strings.TrimRight(line, "\r\n")
		if len(line) < 3 {
			return 0, lines, fmt.Errorf("short SMTP reply")
		}
		code, err := strconv.Atoi(line[:3])
		if err != nil {
			return 0, lines, err
		}
		text := ""
		if len(line) > 4 {
			text = line[4:]
		}
		lines = append(lines, text)
		if len(line) == 3 || line[3] == ' ' {
			return code, lines, nil
		}
	}
	return 0, lines, fmt.Errorf("too many SMTP reply lines")
}

func smtpCapabilities(lines []string) []string {
	result := []string{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			result = append(result, strings.ToUpper(fields[0]))
		}
	}
	return result
}

func smtpHasCapability(lines []string, wanted string) bool {
	for _, item := range smtpCapabilities(lines) {
		if item == wanted {
			return true
		}
	}
	return false
}

func tlsVersion(version uint16) string {
	switch version {
	case tls.VersionTLS13:
		return "TLS1.3"
	case tls.VersionTLS12:
		return "TLS1.2"
	default:
		return fmt.Sprintf("0x%x", version)
	}
}
