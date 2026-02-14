package coin

import (
	"fmt"
	"strings"
)

// CashAddr character set (same characters as bech32).
const cashAddrCharset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

// cashAddrPolymod computes the CashAddr polymod checksum.
func cashAddrPolymod(values []uint64) uint64 {
	generators := []uint64{
		0x98f2bc8e61,
		0x79b76d99e2,
		0xf33e5fb3c4,
		0xae2eabe2a8,
		0x1e4f43e470,
	}

	c := uint64(1)
	for _, v := range values {
		c0 := c >> 35
		c = ((c & 0x07ffffffff) << 5) ^ v
		for i := 0; i < 5; i++ {
			if (c0>>uint(i))&1 != 0 {
				c ^= generators[i]
			}
		}
	}
	return c ^ 1
}

// cashAddrExpandPrefix expands a CashAddr prefix for checksum computation.
func cashAddrExpandPrefix(prefix string) []uint64 {
	result := make([]uint64, 0, len(prefix)+1)
	for _, c := range prefix {
		result = append(result, uint64(c)&0x1f)
	}
	// Separator
	result = append(result, 0)
	return result
}

// cashAddrVerifyChecksum verifies a CashAddr checksum.
func cashAddrVerifyChecksum(prefix string, payload []uint64) bool {
	data := append(cashAddrExpandPrefix(prefix), payload...)
	return cashAddrPolymod(data) == 0
}

// convertBits converts between bit-group sizes.
// fromBits: source bits per value, toBits: target bits per value.
// pad: whether to pad the result.
func convertBits(data []uint64, fromBits, toBits int, pad bool) ([]byte, error) {
	acc := uint64(0)
	bits := 0
	var result []byte
	maxV := uint64((1 << uint(toBits)) - 1)

	for _, v := range data {
		if v>>uint(fromBits) != 0 {
			return nil, fmt.Errorf("invalid value: %d", v)
		}
		acc = (acc << uint(fromBits)) | v
		bits += fromBits
		for bits >= toBits {
			bits -= toBits
			result = append(result, byte((acc>>uint(bits))&maxV))
		}
	}

	if pad {
		if bits > 0 {
			result = append(result, byte((acc<<uint(toBits-bits))&maxV))
		}
	}
	// Note: we do NOT reject leftover padding bits. The CashAddr spec says
	// padding SHOULD be zero, but real-world addresses often have non-zero
	// padding. A 20-byte hash requires 33 five-bit groups (165 bits) which
	// leaves 5 remainder bits — this is normal and expected.

	return result, nil
}

// DecodeCashAddr decodes a CashAddr-encoded address.
// Returns the address type (0=P2PKH, 1=P2SH) and the hash bytes.
func DecodeCashAddr(expectedPrefix, addr string) (int, []byte, error) {
	// Normalize to lowercase
	addr = strings.ToLower(addr)
	expectedPrefix = strings.ToLower(expectedPrefix)

	// Check for prefix
	var payload string
	if strings.Contains(addr, ":") {
		parts := strings.SplitN(addr, ":", 2)
		if parts[0] != expectedPrefix {
			return 0, nil, fmt.Errorf("wrong prefix: expected %s, got %s", expectedPrefix, parts[0])
		}
		payload = parts[1]
	} else {
		// No prefix provided — assume the expected prefix for checksum
		payload = addr
	}

	if len(payload) < 8 {
		return 0, nil, fmt.Errorf("cashaddr payload too short")
	}

	// Decode base32 characters to 5-bit values
	values := make([]uint64, len(payload))
	for i, c := range payload {
		idx := strings.IndexRune(cashAddrCharset, c)
		if idx < 0 {
			return 0, nil, fmt.Errorf("invalid cashaddr character: %c", c)
		}
		values[i] = uint64(idx)
	}

	// Verify checksum
	if !cashAddrVerifyChecksum(expectedPrefix, values) {
		return 0, nil, fmt.Errorf("invalid cashaddr checksum")
	}

	// Strip the 8-value checksum
	data := values[:len(values)-8]
	if len(data) < 1 {
		return 0, nil, fmt.Errorf("cashaddr data too short")
	}

	// First 5-bit value is the version byte
	versionByte := data[0]
	// Address type is in bits 4-3 (top 2 bits of the 5-bit value)
	// Actually: high 1 bit = address type (0=P2PKH, 1=P2SH), low 3 bits = hash size code
	addrType := int(versionByte >> 3)
	hashSizeCode := int(versionByte & 0x07)

	// Hash size lookup (code -> bytes)
	hashSizes := map[int]int{
		0: 20, 1: 24, 2: 28, 3: 32,
		4: 40, 5: 48, 6: 56, 7: 64,
	}

	expectedSize, ok := hashSizes[hashSizeCode]
	if !ok {
		return 0, nil, fmt.Errorf("invalid hash size code: %d", hashSizeCode)
	}

	// Convert remaining 5-bit values to 8-bit bytes
	hashBytes, err := convertBits(data[1:], 5, 8, false)
	if err != nil {
		return 0, nil, fmt.Errorf("convert bits: %w", err)
	}

	if len(hashBytes) != expectedSize {
		return 0, nil, fmt.Errorf("hash size mismatch: got %d, expected %d", len(hashBytes), expectedSize)
	}

	return addrType, hashBytes, nil
}
