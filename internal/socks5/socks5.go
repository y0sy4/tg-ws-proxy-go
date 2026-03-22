// Package socks5 provides SOCKS5 protocol utilities.
package socks5

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
)

const (
	Version5    = 0x05
	NoAuth      = 0x00
	UserPassAuth = 0x02
	ConnectCmd  = 0x01
	IPv4Atyp    = 0x01
	DomainAtyp  = 0x03
	IPv6Atyp    = 0x04
	ReplySucc   = 0x00
	ReplyFail   = 0x05
	ReplyHostUn = 0x07
	ReplyNetUn  = 0x08
)

var (
	ErrUnsupportedVersion = errors.New("unsupported SOCKS version")
	ErrUnsupportedCmd     = errors.New("unsupported command")
	ErrUnsupportedAtyp    = errors.New("unsupported address type")
	ErrNoAuthAccepted     = errors.New("no acceptable authentication method")
	ErrAuthFailed         = errors.New("authentication failed")
)

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	Enabled  bool
	Username string
	Password string
}

// Request represents a SOCKS5 connection request.
type Request struct {
	DestAddr string
	DestPort uint16
}

// Reply lookup table for common status codes.
var replyTable = map[byte][]byte{
	ReplySucc:   {0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0},
	ReplyFail:   {0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0},
	ReplyHostUn: {0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0},
	ReplyNetUn:  {0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0},
}

// Reply generates a SOCKS5 reply packet.
func Reply(status byte) []byte {
	if reply, ok := replyTable[status]; ok {
		return reply
	}
	return []byte{0x05, status, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
}

// HandleGreeting reads and validates SOCKS5 greeting.
// Returns number of methods or error.
func HandleGreeting(conn net.Conn, authCfg *AuthConfig) (int, error) {
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return 0, err
	}

	if buf[0] != Version5 {
		return 0, ErrUnsupportedVersion
	}

	nmethods := int(buf[1])
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return 0, err
	}

	// Check authentication methods
	noAuth := false
	userPass := false
	for _, m := range methods {
		if m == NoAuth {
			noAuth = true
		}
		if m == UserPassAuth && authCfg.Enabled {
			userPass = true
		}
	}

	// Select authentication method
	if authCfg.Enabled && userPass {
		// Use username/password auth
		conn.Write([]byte{Version5, UserPassAuth})
		if err := handleUserPassAuth(conn, authCfg); err != nil {
			return 0, err
		}
		return nmethods, nil
	}

	if noAuth {
		// Use no authentication
		conn.Write([]byte{Version5, NoAuth})
		return nmethods, nil
	}

	conn.Write([]byte{Version5, 0xFF})
	return 0, ErrNoAuthAccepted
}

// handleUserPassAuth handles username/password authentication.
func handleUserPassAuth(conn net.Conn, authCfg *AuthConfig) error {
	// Read version
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return err
	}
	if buf[0] != 0x01 {
		return ErrAuthFailed
	}

	// Read username length
	if _, err := io.ReadFull(conn, buf[:1]); err != nil {
		return err
	}
	ulen := int(buf[0])

	// Read username
	username := make([]byte, ulen)
	if _, err := io.ReadFull(conn, username); err != nil {
		return err
	}

	// Read password length
	if _, err := io.ReadFull(conn, buf[:1]); err != nil {
		return err
	}
	plen := int(buf[0])

	// Read password
	password := make([]byte, plen)
	if _, err := io.ReadFull(conn, password); err != nil {
		return err
	}

	// Validate credentials
	if string(username) == authCfg.Username && string(password) == authCfg.Password {
		// Success
		conn.Write([]byte{0x01, 0x00})
		return nil
	}

	// Failure
	conn.Write([]byte{0x01, 0x01})
	return ErrAuthFailed
}

// ReadRequest reads a SOCKS5 CONNECT request.
func ReadRequest(conn net.Conn) (*Request, error) {
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}

	cmd := buf[1]
	atyp := buf[3]

	if cmd != ConnectCmd {
		conn.Write(Reply(ReplyFail))
		return nil, ErrUnsupportedCmd
	}

	var destAddr string

	switch atyp {
	case IPv4Atyp:
		addrBuf := make([]byte, 4)
		if _, err := io.ReadFull(conn, addrBuf); err != nil {
			return nil, err
		}
		destAddr = net.IP(addrBuf).String()

	case DomainAtyp:
		dlenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, dlenBuf); err != nil {
			return nil, err
		}
		dlen := int(dlenBuf[0])
		domainBuf := make([]byte, dlen)
		if _, err := io.ReadFull(conn, domainBuf); err != nil {
			return nil, err
		}
		destAddr = string(domainBuf)

	case IPv6Atyp:
		addrBuf := make([]byte, 16)
		if _, err := io.ReadFull(conn, addrBuf); err != nil {
			return nil, err
		}
		destAddr = net.IP(addrBuf).String()

	default:
		conn.Write(Reply(ReplyFail))
		return nil, ErrUnsupportedAtyp
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return nil, err
	}
	destPort := binary.BigEndian.Uint16(portBuf)

	return &Request{
		DestAddr: destAddr,
		DestPort: destPort,
	}, nil
}
