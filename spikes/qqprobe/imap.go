package main

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/output"
)

type rawIMAP struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
	next   int
	email  string
}

type imapReply struct {
	Status              string
	Text                string
	Lines               []string
	LiteralContinuation bool
}

func dialRawIMAP(named account.Named) (*rawIMAP, string, error) {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(named.IMAPHost, strconv.Itoa(named.IMAPPort)), &tls.Config{ServerName: named.IMAPHost, MinVersion: tls.VersionTLS12})
	if err != nil {
		return nil, "", err
	}
	client := &rawIMAP{conn: conn, reader: bufio.NewReaderSize(conn, 64<<10), writer: bufio.NewWriterSize(conn, 16<<10), email: named.Email}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	greeting, err := client.readLine()
	if err != nil {
		_ = conn.Close()
		return nil, "", err
	}
	return client, client.redact(greeting), nil
}

func (c *rawIMAP) close() { _ = c.conn.Close() }

func (c *rawIMAP) nextTag() string {
	c.next++
	return fmt.Sprintf("A%04d", c.next)
}

func (c *rawIMAP) readLine() (string, error) {
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) > 64<<10 {
		return "", fmt.Errorf("IMAP response line exceeded 64 KiB")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (c *rawIMAP) command(command string) (imapReply, error) {
	tag := c.nextTag()
	_ = c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := fmt.Fprintf(c.writer, "%s %s\r\n", tag, command); err != nil {
		return imapReply{}, err
	}
	if err := c.writer.Flush(); err != nil {
		return imapReply{}, err
	}
	return c.readTagged(tag)
}

func (c *rawIMAP) commandLiteral(prefix string, literal []byte) (imapReply, error) {
	tag := c.nextTag()
	_ = c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := fmt.Fprintf(c.writer, "%s %s {%d}\r\n", tag, prefix, len(literal)); err != nil {
		return imapReply{}, err
	}
	if err := c.writer.Flush(); err != nil {
		return imapReply{}, err
	}
	first, err := c.readLine()
	if err != nil {
		return imapReply{}, err
	}
	if !strings.HasPrefix(first, "+") {
		if strings.HasPrefix(strings.ToUpper(first), "* BAD") {
			return imapReply{Status: "UNTAGGED_BAD", Text: strings.TrimSpace(first[5:]), Lines: []string{first}}, nil
		}
		if strings.HasPrefix(strings.ToUpper(first), tag+" ") {
			return taggedReply(tag, []string{first}), nil
		}
		// QQ has been observed returning untagged SEARCH data immediately
		// instead of the RFC continuation. Do not send the literal into a
		// stream whose state is now ambiguous; drain only to the tagged end.
		lines := []string{first}
		for len(lines) < 10000 {
			line, readErr := c.readLine()
			if readErr != nil {
				return imapReply{}, readErr
			}
			lines = append(lines, line)
			if strings.HasPrefix(strings.ToUpper(line), "* BAD") {
				return imapReply{Status: "UNTAGGED_BAD", Text: strings.TrimSpace(line[5:]), Lines: lines}, nil
			}
			if strings.HasPrefix(strings.ToUpper(line), tag+" ") {
				return taggedReply(tag, lines), nil
			}
		}
		return imapReply{}, fmt.Errorf("too many IMAP response lines while draining literal command")
	}
	if _, err := c.writer.Write(literal); err != nil {
		return imapReply{}, err
	}
	if _, err := c.writer.WriteString("\r\n"); err != nil {
		return imapReply{}, err
	}
	if err := c.writer.Flush(); err != nil {
		return imapReply{}, err
	}
	reply, err := c.readTagged(tag)
	reply.LiteralContinuation = true
	return reply, err
}

func (c *rawIMAP) readTagged(tag string) (imapReply, error) {
	lines := make([]string, 0, 4)
	for len(lines) < 10000 {
		line, err := c.readLine()
		if err != nil {
			return imapReply{}, err
		}
		lines = append(lines, line)
		if strings.HasPrefix(strings.ToUpper(line), "* BAD") {
			return imapReply{Status: "UNTAGGED_BAD", Text: strings.TrimSpace(line[5:]), Lines: lines}, nil
		}
		if strings.HasPrefix(strings.ToUpper(line), tag+" ") {
			return taggedReply(tag, lines), nil
		}
	}
	return imapReply{}, fmt.Errorf("too many IMAP response lines")
}

func taggedReply(tag string, lines []string) imapReply {
	last := lines[len(lines)-1]
	parts := strings.SplitN(last, " ", 3)
	reply := imapReply{Lines: lines}
	if len(parts) >= 2 && strings.EqualFold(parts[0], tag) {
		reply.Status = strings.ToUpper(parts[1])
	}
	if len(parts) == 3 {
		reply.Text = parts[2]
	}
	return reply
}

func (c *rawIMAP) login(authCode string) (imapReply, error) {
	reply, err := c.command("LOGIN " + quoteIMAP(c.email) + " " + quoteIMAP(authCode))
	if err == nil {
		reply.Text = c.redact(reply.Text)
		reply.Lines = nil
	}
	return reply, err
}

func (c *rawIMAP) authenticatePlain(authCode string) (imapReply, error) {
	tag := c.nextTag()
	_ = c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := fmt.Fprintf(c.writer, "%s AUTHENTICATE PLAIN\r\n", tag); err != nil {
		return imapReply{}, err
	}
	if err := c.writer.Flush(); err != nil {
		return imapReply{}, err
	}
	line, err := c.readLine()
	if err != nil {
		return imapReply{}, err
	}
	if strings.HasPrefix(line, "+") {
		payload := base64.StdEncoding.EncodeToString([]byte("\x00" + c.email + "\x00" + authCode))
		if _, err := c.writer.WriteString(payload + "\r\n"); err != nil {
			return imapReply{}, err
		}
		if err := c.writer.Flush(); err != nil {
			return imapReply{}, err
		}
		return c.readTagged(tag)
	}
	if strings.HasPrefix(strings.ToUpper(line), tag+" ") {
		return taggedReply(tag, []string{line}), nil
	}
	return imapReply{}, fmt.Errorf("unexpected AUTHENTICATE response: %s", c.redact(line))
}

func (c *rawIMAP) logout() {
	_, _ = c.command("LOGOUT")
	c.close()
}

func (c *rawIMAP) redact(value string) string {
	value = output.RedactString(value)
	value = strings.ReplaceAll(value, c.email, "<email>")
	return emailPattern.ReplaceAllString(value, "<email>")
}

func quoteIMAP(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

func statusOK(reply imapReply) bool { return reply.Status == "OK" }

func responseSummary(client *rawIMAP, reply imapReply) map[string]any {
	return map[string]any{"status": reply.Status, "text": client.redact(reply.Text)}
}

func capabilities(lines ...string) []string {
	set := map[string]bool{}
	for _, line := range lines {
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "* CAPABILITY ") {
			for _, item := range strings.Fields(line)[2:] {
				set[strings.ToUpper(item)] = true
			}
		}
		if start := strings.Index(upper, "[CAPABILITY "); start >= 0 {
			rest := line[start+len("[CAPABILITY "):]
			if end := strings.Index(rest, "]"); end >= 0 {
				for _, item := range strings.Fields(rest[:end]) {
					set[strings.ToUpper(item)] = true
				}
			}
		}
	}
	items := make([]string, 0, len(set))
	for item := range set {
		items = append(items, item)
	}
	sort.Strings(items)
	return items
}

func hasCapability(caps []string, wanted string) bool {
	for _, item := range caps {
		if strings.EqualFold(item, wanted) {
			return true
		}
	}
	return false
}

func parseSearch(lines []string) []uint32 {
	for _, line := range lines {
		if !strings.HasPrefix(strings.ToUpper(line), "* SEARCH") {
			continue
		}
		fields := strings.Fields(line)
		result := make([]uint32, 0, len(fields)-2)
		for _, field := range fields[2:] {
			value, err := strconv.ParseUint(field, 10, 32)
			if err == nil {
				result = append(result, uint32(value))
			}
		}
		return result
	}
	return []uint32{}
}

func extractUIDValidity(lines []string) uint32 {
	for _, line := range lines {
		match := uidValidityPattern.FindStringSubmatch(line)
		if len(match) == 2 {
			value, _ := strconv.ParseUint(match[1], 10, 32)
			return uint32(value)
		}
	}
	return 0
}

func normalizeFetch(lines []string) []string {
	result := []string{}
	for _, line := range lines {
		if strings.Contains(strings.ToUpper(line), " FETCH ") {
			line = quotedPattern.ReplaceAllString(line, `"<redacted>"`)
			line = numberPattern.ReplaceAllString(line, "N")
			result = append(result, line)
		}
	}
	return result
}

var (
	emailPattern       = regexp.MustCompile(`(?i)[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}`)
	uidValidityPattern = regexp.MustCompile(`(?i)\[UIDVALIDITY\s+(\d+)\]`)
	numberPattern      = regexp.MustCompile(`\d+`)
	quotedPattern      = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
)
