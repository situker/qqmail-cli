package main

import (
	"bufio"
	"net"
	"testing"
)

func TestCommandStopsOnUntaggedBAD(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()
	client := &rawIMAP{conn: clientConn, reader: bufio.NewReader(clientConn), writer: bufio.NewWriter(clientConn)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		reader := bufio.NewReader(serverConn)
		_, _ = reader.ReadString('\n')
		_, _ = serverConn.Write([]byte("* BAD Command!\r\n"))
	}()
	reply, err := client.command("XUNKNOWN")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Status != "UNTAGGED_BAD" {
		t.Fatalf("status=%q, want UNTAGGED_BAD", reply.Status)
	}
	<-done
}

func TestLiteralStopsOnUntaggedBAD(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()
	client := &rawIMAP{conn: clientConn, reader: bufio.NewReader(clientConn), writer: bufio.NewWriter(clientConn)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		reader := bufio.NewReader(serverConn)
		_, _ = reader.ReadString('\n')
		_, _ = serverConn.Write([]byte("* BAD Command!\r\n"))
	}()
	reply, err := client.commandLiteral("UID SEARCH SUBJECT", []byte("测试"))
	if err != nil {
		t.Fatal(err)
	}
	if reply.Status != "UNTAGGED_BAD" {
		t.Fatalf("status=%q, want UNTAGGED_BAD", reply.Status)
	}
	<-done
}

func TestLiteralDrainsImmediateSearchToTaggedStatus(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()
	client := &rawIMAP{conn: clientConn, reader: bufio.NewReader(clientConn), writer: bufio.NewWriter(clientConn)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		reader := bufio.NewReader(serverConn)
		_, _ = reader.ReadString('\n')
		_, _ = serverConn.Write([]byte("* SEARCH 1 2 3\r\nA0001 OK SEARCH completed\r\n"))
	}()
	reply, err := client.commandLiteral("UID SEARCH CHARSET UTF-8 SUBJECT", []byte("测试"))
	if err != nil {
		t.Fatal(err)
	}
	if reply.Status != "OK" || len(parseSearch(reply.Lines)) != 3 || reply.LiteralContinuation {
		t.Fatalf("unexpected reply: %#v", reply)
	}
	<-done
}
