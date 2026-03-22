// Package websocket provides a lightweight WebSocket client over TLS.
package websocket

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	OpContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	OpClose        = 0x8
	OpPing         = 0x9
	OpPong         = 0xA
)

var (
	ErrHandshakeFailed = errors.New("websocket handshake failed")
	ErrClosed          = errors.New("websocket closed")
)

// WebSocket represents a WebSocket connection over TLS.
type WebSocket struct {
	conn    *tls.Conn
	reader  *bufio.Reader
	writer  *bufio.Writer
	closed  bool
	maskKey []byte
	mu      sync.Mutex
}

// Connect establishes a WebSocket connection to the given domain via IP.
func Connect(ip, domain, path string, timeout time.Duration) (*WebSocket, error) {
	if path == "" {
		path = "/apiws"
	}

	// Generate Sec-WebSocket-Key
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, err
	}
	wsKey := base64.StdEncoding.EncodeToString(keyBytes)

	// Dial TLS connection
	dialer := &net.Dialer{Timeout: timeout}
	tlsConfig := &tls.Config{
		ServerName:         domain,
		InsecureSkipVerify: true,
	}
	rawConn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(ip, "443"), tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("tls dial: %w", err)
	}

	// Set TCP_NODELAY and buffer sizes
	if tcpConn, ok := rawConn.NetConn().(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
		tcpConn.SetReadBuffer(256 * 1024)
		tcpConn.SetWriteBuffer(256 * 1024)
	}

	// Build HTTP upgrade request
	req := &http.Request{
		Method: "GET",
		URL:    &url.URL{Path: path},
		Host:   domain,
		Header: http.Header{
			"Upgrade":               []string{"websocket"},
			"Connection":            []string{"Upgrade"},
			"Sec-WebSocket-Key":     []string{wsKey},
			"Sec-WebSocket-Version": []string{"13"},
			"Sec-WebSocket-Protocol": []string{"binary"},
			"Origin":                []string{"https://web.telegram.org"},
			"User-Agent":            []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"},
		},
	}

	// Write request
	if err := req.Write(rawConn); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read response
	reader := bufio.NewReader(rawConn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("read response: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		rawConn.Close()
		location := resp.Header.Get("Location")
		return nil, &HandshakeError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Location:   location,
		}
	}

	return &WebSocket{
		conn:    rawConn,
		reader:  reader,
		writer:  bufio.NewWriter(rawConn),
		maskKey: make([]byte, 4),
	}, nil
}

// HandshakeError is returned when WebSocket handshake fails.
type HandshakeError struct {
	StatusCode int
	Status     string
	Location   string
}

func (e *HandshakeError) Error() string {
	return fmt.Sprintf("websocket handshake: HTTP %d %s", e.StatusCode, e.Status)
}

// IsRedirect returns true if the error is a redirect.
func (e *HandshakeError) IsRedirect() bool {
	return e.StatusCode >= 300 && e.StatusCode < 400
}

// Send sends a binary WebSocket frame with masking.
func (w *WebSocket) Send(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return ErrClosed
	}

	frame := BuildFrame(OpBinary, data, true)
	_, err := w.writer.Write(frame)
	if err != nil {
		return err
	}
	return w.writer.Flush()
}

// SendBatch sends multiple binary frames with a single flush.
func (w *WebSocket) SendBatch(parts [][]byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return ErrClosed
	}

	for _, part := range parts {
		frame := BuildFrame(OpBinary, part, true)
		if _, err := w.writer.Write(frame); err != nil {
			return err
		}
	}
	return w.writer.Flush()
}

// Recv receives the next data frame.
func (w *WebSocket) Recv() ([]byte, error) {
	for {
		opcode, payload, err := w.readFrame()
		if err != nil {
			return nil, err
		}

		switch opcode {
		case OpClose:
			w.mu.Lock()
			w.closed = true
			w.mu.Unlock()
			// Send close response
			w.SendFrame(OpClose, payload[:2], true)
			return nil, io.EOF

		case OpPing:
			// Respond with pong
			if err := w.SendFrame(OpPong, payload, true); err != nil {
				return nil, err
			}
			continue

		case OpPong:
			continue

		case OpBinary, OpText:
			return payload, nil
		}
	}
}

// SendFrame sends a raw WebSocket frame.
func (w *WebSocket) SendFrame(opcode int, data []byte, mask bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return ErrClosed
	}

	frame := BuildFrame(opcode, data, mask)
	_, err := w.writer.Write(frame)
	if err != nil {
		return err
	}
	return w.writer.Flush()
}

// Close sends a close frame and closes the connection.
func (w *WebSocket) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	w.closed = true

	// Send close frame
	frame := BuildFrame(OpClose, []byte{}, true)
	w.writer.Write(frame)
	w.writer.Flush()

	return w.conn.Close()
}

// BuildFrame creates a WebSocket frame.
func BuildFrame(opcode int, data []byte, mask bool) []byte {
	length := len(data)
	fb := byte(0x80 | opcode)

	var header []byte
	var maskKey []byte

	if !mask {
		if length < 126 {
			header = []byte{fb, byte(length)}
		} else if length < 65536 {
			header = make([]byte, 4)
			header[0] = fb
			header[1] = 126
			binary.BigEndian.PutUint16(header[2:4], uint16(length))
		} else {
			header = make([]byte, 10)
			header[0] = fb
			header[1] = 127
			binary.BigEndian.PutUint64(header[2:10], uint64(length))
		}
		return append(header, data...)
	}

	// Generate mask key
	maskKey = make([]byte, 4)
	rand.Read(maskKey)

	masked := XORMask(data, maskKey)

	if length < 126 {
		header = make([]byte, 6)
		header[0] = fb
		header[1] = 0x80 | byte(length)
		copy(header[2:6], maskKey)
	} else if length < 65536 {
		header = make([]byte, 8)
		header[0] = fb
		header[1] = 0x80 | 126
		binary.BigEndian.PutUint16(header[2:4], uint16(length))
		copy(header[4:8], maskKey)
	} else {
		header = make([]byte, 14)
		header[0] = fb
		header[1] = 0x80 | 127
		binary.BigEndian.PutUint64(header[2:10], uint64(length))
		copy(header[10:14], maskKey)
	}

	return append(header, masked...)
}

// XORMask applies XOR mask to data.
func XORMask(data, mask []byte) []byte {
	if len(data) == 0 {
		return data
	}
	result := make([]byte, len(data))
	for i := range data {
		result[i] = data[i] ^ mask[i%4]
	}
	return result
}

// readFrame reads a WebSocket frame from the connection.
func (w *WebSocket) readFrame() (opcode int, payload []byte, err error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(w.reader, header); err != nil {
		return 0, nil, err
	}

	opcode = int(header[0] & 0x0F)
	length := int(header[1] & 0x7F)
	masked := (header[1] & 0x80) != 0

	if length == 126 {
		extLen := make([]byte, 2)
		if _, err := io.ReadFull(w.reader, extLen); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint16(extLen))
	} else if length == 127 {
		extLen := make([]byte, 8)
		if _, err := io.ReadFull(w.reader, extLen); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint64(extLen))
	}

	var maskKey []byte
	if masked {
		maskKey = make([]byte, 4)
		if _, err := io.ReadFull(w.reader, maskKey); err != nil {
			return 0, nil, err
		}
	}

	payload = make([]byte, length)
	if _, err := io.ReadFull(w.reader, payload); err != nil {
		return 0, nil, err
	}

	if masked {
		payload = XORMask(payload, maskKey)
	}

	return opcode, payload, nil
}

// GenerateSecWebSocketAccept generates the expected accept key.
func GenerateSecWebSocketAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
