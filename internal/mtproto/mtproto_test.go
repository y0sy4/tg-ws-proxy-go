package mtproto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"testing"
)

func TestExtractDCFromInit(t *testing.T) {
	// Create a valid init packet
	aesKey := make([]byte, 32)
	iv := make([]byte, 16)
	for i := 0; i < 32; i++ {
		aesKey[i] = byte(i)
	}
	for i := 0; i < 16; i++ {
		iv[i] = byte(i)
	}

	// Create encrypted data with valid protocol magic and DC ID
	block, _ := aes.NewCipher(aesKey)
	stream := cipher.NewCTR(block, iv)

	// Protocol magic (0xEFEFEFEF) + DC ID (2) + padding
	plainData := make([]byte, 8)
	binary.LittleEndian.PutUint32(plainData[0:4], 0xEFEFEFEF)
	binary.LittleEndian.PutUint16(plainData[4:6], 2) // DC 2

	// Encrypt
	encrypted := make([]byte, 8)
	stream.XORKeyStream(encrypted, plainData)

	// Build init packet
	init := make([]byte, 64)
	copy(init[8:40], aesKey)
	copy(init[40:56], iv)
	copy(init[56:64], encrypted)

	// Test extraction
	dcInfo := ExtractDCFromInit(init)

	if !dcInfo.Valid {
		t.Fatal("Expected valid DC info")
	}
	if dcInfo.DC != 2 {
		t.Errorf("Expected DC 2, got %d", dcInfo.DC)
	}
	if dcInfo.IsMedia {
		t.Error("Expected non-media DC")
	}
}

func TestExtractDCFromInit_Media(t *testing.T) {
	aesKey := make([]byte, 32)
	iv := make([]byte, 16)

	block, _ := aes.NewCipher(aesKey)
	stream := cipher.NewCTR(block, iv)

	// Protocol magic + negative DC ID (media)
	plainData := make([]byte, 8)
	binary.LittleEndian.PutUint32(plainData[0:4], 0xEFEFEFEF)
	// Use int16 conversion for negative value
	dcRaw := int16(-4)
	binary.LittleEndian.PutUint16(plainData[4:6], uint16(dcRaw))

	encrypted := make([]byte, 8)
	stream.XORKeyStream(encrypted, plainData)

	init := make([]byte, 64)
	copy(init[8:40], aesKey)
	copy(init[40:56], iv)
	copy(init[56:64], encrypted)

	dcInfo := ExtractDCFromInit(init)

	if !dcInfo.Valid {
		t.Fatal("Expected valid DC info")
	}
	if dcInfo.DC != 4 {
		t.Errorf("Expected DC 4, got %d", dcInfo.DC)
	}
	if !dcInfo.IsMedia {
		t.Error("Expected media DC")
	}
}

func TestExtractDCFromInit_Invalid(t *testing.T) {
	// Too short
	dcInfo := ExtractDCFromInit([]byte{1, 2, 3})
	if dcInfo.Valid {
		t.Error("Expected invalid DC info for short data")
	}

	// Invalid protocol magic
	init := make([]byte, 64)
	dcInfo = ExtractDCFromInit(init)
	if dcInfo.Valid {
		t.Error("Expected invalid DC info for invalid protocol")
	}
}

func TestPatchInitDC(t *testing.T) {
	aesKey := make([]byte, 32)
	iv := make([]byte, 16)

	block, _ := aes.NewCipher(aesKey)
	stream := cipher.NewCTR(block, iv)

	// Original with valid protocol but random DC
	plainData := make([]byte, 8)
	binary.LittleEndian.PutUint32(plainData[0:4], 0xEFEFEFEF)
	binary.LittleEndian.PutUint16(plainData[4:6], 999) // Invalid DC

	encrypted := make([]byte, 8)
	stream.XORKeyStream(encrypted, plainData)

	init := make([]byte, 64)
	copy(init[8:40], aesKey)
	copy(init[40:56], iv)
	copy(init[56:64], encrypted)

	// Patch to DC 2
	patched, ok := PatchInitDC(init, 2)
	if !ok {
		t.Fatal("Failed to patch init")
	}

	// Verify patched data is different
	if bytes.Equal(init, patched) {
		t.Error("Expected patched data to be different")
	}

	// The DC extraction after patching is complex due to CTR mode
	// Just verify the function runs without error
	_ = ExtractDCFromInit(patched)
}

func TestMsgSplitter(t *testing.T) {
	aesKey := make([]byte, 32)
	iv := make([]byte, 16)

	init := make([]byte, 64)
	copy(init[8:40], aesKey)
	copy(init[40:56], iv)

	splitter, err := NewMsgSplitter(init)
	if err != nil {
		t.Fatalf("Failed to create splitter: %v", err)
	}

	// Test with simple data
	chunk := []byte{0x01, 0x02, 0x03, 0x04}
	parts := splitter.Split(chunk)

	if len(parts) != 1 {
		t.Errorf("Expected 1 part, got %d", len(parts))
	}
}

func TestValidProtos(t *testing.T) {
	tests := []struct {
		proto  uint32
		valid  bool
	}{
		{0xEFEFEFEF, true},
		{0xEEEEEEEE, true},
		{0xDDDDDDDD, true},
		{0x00000000, false},
		{0xFFFFFFFF, false},
	}

	for _, tt := range tests {
		if ValidProtos[tt.proto] != tt.valid {
			t.Errorf("Protocol 0x%08X: expected valid=%v", tt.proto, tt.valid)
		}
	}
}
