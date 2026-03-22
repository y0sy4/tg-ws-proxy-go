// Package mtproto provides MTProto protocol utilities for Telegram.
package mtproto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
)

var (
	// Valid protocol magic constants for MTProto obfuscation
	ValidProtos = map[uint32]bool{
		0xEFEFEFEF: true,
		0xEEEEEEEE: true,
		0xDDDDDDDD: true,
	}

	zero64 = make([]byte, 64)
)

// DCInfo contains extracted DC information from init packet.
type DCInfo struct {
	DC       int
	IsMedia  bool
	Valid    bool
	Patched  bool
}

// ExtractDCFromInit extracts DC ID from the 64-byte MTProto obfuscation init packet.
// Returns DCInfo with Valid=true if successful.
func ExtractDCFromInit(data []byte) DCInfo {
	if len(data) < 64 {
		return DCInfo{Valid: false}
	}

	// AES key is at [8:40], IV at [40:56]
	aesKey := data[8:40]
	iv := data[40:56]

	// Create AES-CTR decryptor
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return DCInfo{Valid: false}
	}
	stream := cipher.NewCTR(block, iv)

	// Decrypt bytes [56:64] to get protocol magic and DC ID
	plaintext := make([]byte, 8)
	stream.XORKeyStream(plaintext, data[56:64])

	// Parse protocol magic (4 bytes) and DC raw (int16)
	proto := binary.LittleEndian.Uint32(plaintext[0:4])
	dcRaw := int16(binary.LittleEndian.Uint16(plaintext[4:6]))

	if ValidProtos[proto] {
		dc := int(dcRaw)
		if dc < 0 {
			dc = -dc
		}
		if dc >= 1 && dc <= 5 || dc == 203 {
			return DCInfo{
				DC:      dc,
				IsMedia: dcRaw < 0,
				Valid:   true,
			}
		}
	}

	return DCInfo{Valid: false}
}

// PatchInitDC patches the dc_id in the 64-byte MTProto init packet.
// Mobile clients with useSecret=0 leave bytes 60-61 as random.
// The WS relay needs a valid dc_id to route correctly.
func PatchInitDC(data []byte, dc int) ([]byte, bool) {
	if len(data) < 64 {
		return data, false
	}

	aesKey := data[8:40]
	iv := data[40:56]

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return data, false
	}
	stream := cipher.NewCTR(block, iv)

	// Generate keystream for bytes 56-64
	keystream := make([]byte, 8)
	stream.XORKeyStream(keystream, zero64[56:64])

	// Patch bytes 60-61 with the correct DC ID
	patched := make([]byte, len(data))
	copy(patched, data)

	newDC := make([]byte, 2)
	binary.LittleEndian.PutUint16(newDC, uint16(dc))

	patched[60] = keystream[0] ^ newDC[0]
	patched[61] = keystream[1] ^ newDC[1]

	return patched, true
}

// MsgSplitter splits client TCP data into individual MTProto messages.
// Telegram WS relay processes one MTProto message per WS frame.
type MsgSplitter struct {
	aesKey []byte
	iv     []byte
	stream cipher.Stream
}

// NewMsgSplitter creates a new message splitter from init data.
func NewMsgSplitter(initData []byte) (*MsgSplitter, error) {
	if len(initData) < 64 {
		return nil, errors.New("init data too short")
	}

	aesKey := initData[8:40]
	iv := initData[40:56]

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(block, iv)

	// Skip init packet (64 bytes of keystream)
	stream.XORKeyStream(make([]byte, 64), zero64[:64])

	return &MsgSplitter{
		aesKey: aesKey,
		iv:     iv,
		stream: stream,
	}, nil
}

// Split decrypts chunk and finds message boundaries.
// Returns split ciphertext parts.
func (s *MsgSplitter) Split(chunk []byte) [][]byte {
	// Decrypt to find boundaries
	plaintext := make([]byte, len(chunk))
	s.stream.XORKeyStream(plaintext, chunk)

	boundaries := []int{}
	pos := 0
	plainLen := len(plaintext)

	for pos < plainLen {
		first := plaintext[pos]
		var msgLen int

		if first == 0x7f {
			if pos+4 > plainLen {
				break
			}
			// Read 3 bytes starting from pos+1 (skip the 0x7f byte)
			msgLen = int(binary.LittleEndian.Uint32(append(plaintext[pos+1:pos+4], 0))) & 0xFFFFFF
			msgLen *= 4
			pos += 4
		} else {
			msgLen = int(first) * 4
			pos += 1
		}

		if msgLen == 0 || pos+msgLen > plainLen {
			break
		}

		pos += msgLen
		boundaries = append(boundaries, pos)
	}

	if len(boundaries) <= 1 {
		return [][]byte{chunk}
	}

	parts := make([][]byte, 0, len(boundaries)+1)
	prev := 0
	for _, b := range boundaries {
		parts = append(parts, chunk[prev:b])
		prev = b
	}
	if prev < len(chunk) {
		parts = append(parts, chunk[prev:])
	}

	return parts
}
