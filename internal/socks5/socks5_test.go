package socks5

import (
	"bytes"
	"net"
	"testing"
)

func TestReply(t *testing.T) {
	tests := []struct {
		status   byte
		expected []byte
	}{
		{ReplySucc, []byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}},
		{ReplyFail, []byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0}},
		{ReplyHostUn, []byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}},
		{ReplyNetUn, []byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}},
		{0xFF, []byte{0x05, 0xFF, 0x00, 0x01, 0, 0, 0, 0, 0, 0}},
	}

	for _, tt := range tests {
		result := Reply(tt.status)
		if !bytes.Equal(result, tt.expected) {
			t.Errorf("Reply(0x%02X) = %v, expected %v", tt.status, result, tt.expected)
		}
	}
}

func TestHandleGreeting_Success(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Send valid greeting with no-auth method
	go client.Write([]byte{0x05, 0x01, 0x00})

	nmethods, err := HandleGreeting(server)
	if err != nil {
		t.Fatalf("HandleGreeting failed: %v", err)
	}
	if nmethods != 1 {
		t.Errorf("Expected 1 method, got %d", nmethods)
	}

	// Read response
	buf := make([]byte, 2)
	server.Read(buf)
	if !bytes.Equal(buf, []byte{0x05, 0x00}) {
		t.Errorf("Expected accept response, got %v", buf)
	}
}

func TestHandleGreeting_UnsupportedVersion(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Send SOCKS4 greeting
	go client.Write([]byte{0x04, 0x01, 0x00})

	_, err := HandleGreeting(server)
	if err != ErrUnsupportedVersion {
		t.Errorf("Expected ErrUnsupportedVersion, got %v", err)
	}
}

func TestHandleGreeting_NoAuthNotSupported(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Send greeting without no-auth method
	go client.Write([]byte{0x05, 0x01, 0x01})

	_, err := HandleGreeting(server)
	if err != ErrNoAuthAccepted {
		t.Errorf("Expected ErrNoAuthAccepted, got %v", err)
	}
}

func TestReadRequest_IPv4(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Send CONNECT request for IPv4
	// ver=5, cmd=1, rsv=0, atyp=1, addr=127.0.0.1, port=8080
	go client.Write([]byte{
		0x05, 0x01, 0x00, 0x01,
		127, 0, 0, 1,
		0x1F, 0x90, // port 8080
	})

	req, err := ReadRequest(server)
	if err != nil {
		t.Fatalf("ReadRequest failed: %v", err)
	}
	if req.DestAddr != "127.0.0.1" {
		t.Errorf("Expected addr 127.0.0.1, got %s", req.DestAddr)
	}
	if req.DestPort != 8080 {
		t.Errorf("Expected port 8080, got %d", req.DestPort)
	}
}

func TestReadRequest_Domain(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Send CONNECT request for domain
	// ver=5, cmd=1, rsv=0, atyp=3, len=9, domain=example.com, port=80
	go client.Write([]byte{
		0x05, 0x01, 0x00, 0x03,
		0x0B, // length of "example.com"
	})
	go client.Write([]byte("example.com"))
	go client.Write([]byte{0x00, 0x50}) // port 80

	req, err := ReadRequest(server)
	if err != nil {
		t.Fatalf("ReadRequest failed: %v", err)
	}
	if req.DestAddr != "example.com" {
		t.Errorf("Expected addr example.com, got %s", req.DestAddr)
	}
	if req.DestPort != 80 {
		t.Errorf("Expected port 80, got %d", req.DestPort)
	}
}

func TestReadRequest_IPv6(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Send CONNECT request for IPv6 ::1
	go client.Write([]byte{
		0x05, 0x01, 0x00, 0x04,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1,
		0x00, 0x50, // port 80
	})

	req, err := ReadRequest(server)
	if err != nil {
		t.Fatalf("ReadRequest failed: %v", err)
	}
	if req.DestAddr != "::1" {
		t.Errorf("Expected addr ::1, got %s", req.DestAddr)
	}
}

func TestReadRequest_UnsupportedCmd(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Send UDP ASSOCIATE request
	go client.Write([]byte{0x05, 0x03, 0x00, 0x01, 127, 0, 0, 1, 0, 80})

	_, err := ReadRequest(server)
	if err != ErrUnsupportedCmd {
		t.Errorf("Expected ErrUnsupportedCmd, got %v", err)
	}
}
