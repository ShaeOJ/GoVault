package coin

import (
	"fmt"
	"strings"
)

// ValidateAddress checks if an address is valid for the given coin.
// Returns whether it's valid and the address type description.
func ValidateAddress(coinDef *CoinDef, addr string) (bool, string) {
	if len(addr) < 10 {
		return false, ""
	}

	// Try CashAddr first if coin supports it
	if coinDef.CashAddrPrefix != "" {
		addrType, _, err := DecodeCashAddr(coinDef.CashAddrPrefix, addr)
		if err == nil {
			switch addrType {
			case 0:
				return true, "P2PKH (CashAddr)"
			case 1:
				return true, "P2SH (CashAddr)"
			default:
				return true, fmt.Sprintf("CashAddr type %d", addrType)
			}
		}
		// CashAddr coins also accept legacy base58 addresses
		if coinDef.P2PKHVersion == 0x00 {
			if addr[0] == '1' {
				_, err := base58CheckDecodeWithVersion(addr)
				if err == nil {
					return true, "P2PKH (Legacy)"
				}
			}
			if addr[0] == '3' {
				_, err := base58CheckDecodeWithVersion(addr)
				if err == nil {
					return true, "P2SH (Legacy)"
				}
			}
		}
		return false, ""
	}

	// Try bech32 if coin supports it
	if coinDef.Bech32HRP != "" {
		hrpPrefix := coinDef.Bech32HRP + "1"
		lowerAddr := strings.ToLower(addr)
		if strings.HasPrefix(lowerAddr, hrpPrefix) {
			// P2WPKH (42 chars for 20-byte program)
			if len(addr) == len(hrpPrefix)+38 && lowerAddr[len(hrpPrefix)] == 'q' {
				_, err := Bech32Decode(coinDef.Bech32HRP, addr)
				if err == nil {
					return true, "P2WPKH (SegWit)"
				}
			}
			// P2WSH (62 chars for 32-byte program) or P2TR
			if len(addr) == len(hrpPrefix)+58 {
				_, err := Bech32Decode(coinDef.Bech32HRP, addr)
				if err == nil {
					if lowerAddr[len(hrpPrefix)] == 'q' {
						return true, "P2WSH (SegWit)"
					}
					if lowerAddr[len(hrpPrefix)] == 'p' {
						return true, "P2TR (Taproot)"
					}
					return true, "Bech32 SegWit"
				}
			}
		}
	}

	// Try base58check
	if coinDef.P2PKHPrefixChar != 0 && addr[0] == coinDef.P2PKHPrefixChar {
		result, err := base58CheckDecodeWithVersion(addr)
		if err == nil && result.version == coinDef.P2PKHVersion {
			return true, "P2PKH (Legacy)"
		}
	}
	if coinDef.P2SHPrefixChar != 0 && addr[0] == coinDef.P2SHPrefixChar {
		result, err := base58CheckDecodeWithVersion(addr)
		if err == nil && result.version == coinDef.P2SHVersion {
			return true, "P2SH"
		}
	}

	// BTC/BC2 testnet addresses
	if coinDef.CoinID == "btc" || coinDef.CoinID == "bc2" {
		if addr[0] == 'm' || addr[0] == 'n' {
			_, err := base58CheckDecodeWithVersion(addr)
			if err == nil {
				return true, "P2PKH (Testnet)"
			}
		}
		if addr[0] == '2' {
			_, err := base58CheckDecodeWithVersion(addr)
			if err == nil {
				return true, "P2SH (Testnet)"
			}
		}
		if strings.HasPrefix(strings.ToLower(addr), "tb1") {
			_, err := Bech32Decode("tb", addr)
			if err == nil {
				if len(addr) == 42 {
					return true, "P2WPKH (Testnet SegWit)"
				}
				if len(addr) == 62 {
					return true, "P2TR (Testnet Taproot)"
				}
				return true, "Bech32 (Testnet)"
			}
		}
	}

	return false, ""
}

// AddressToScriptPubKey converts an address to its scriptPubKey for the given coin.
func AddressToScriptPubKey(coinDef *CoinDef, addr string) ([]byte, error) {
	if len(addr) == 0 {
		return nil, fmt.Errorf("empty address")
	}

	// Try CashAddr first
	if coinDef.CashAddrPrefix != "" {
		addrType, hash, err := DecodeCashAddr(coinDef.CashAddrPrefix, addr)
		if err == nil {
			return cashAddrToScript(addrType, hash)
		}
		// CashAddr coins also accept legacy base58 addresses (same format as BTC since they forked)
		result, decErr := base58CheckDecodeWithVersion(addr)
		if decErr == nil {
			if result.version == coinDef.P2PKHVersion && len(result.payload) == 20 {
				// P2PKH: OP_DUP OP_HASH160 <20 bytes> OP_EQUALVERIFY OP_CHECKSIG
				script := []byte{0x76, 0xa9, 0x14}
				script = append(script, result.payload...)
				script = append(script, 0x88, 0xac)
				return script, nil
			}
			if result.version == coinDef.P2SHVersion && len(result.payload) == 20 {
				// P2SH: OP_HASH160 <20 bytes> OP_EQUAL
				script := []byte{0xa9, 0x14}
				script = append(script, result.payload...)
				script = append(script, 0x87)
				return script, nil
			}
		}
	}

	// Try bech32 (SegWit) if coin supports it
	if coinDef.Bech32HRP != "" {
		lowerAddr := strings.ToLower(addr)
		hrpPrefix := coinDef.Bech32HRP + "1"

		if strings.HasPrefix(lowerAddr, hrpPrefix) {
			witnessProgram, err := Bech32Decode(coinDef.Bech32HRP, addr)
			if err != nil {
				return nil, fmt.Errorf("bech32 decode: %w", err)
			}

			// Determine witness version from the character after the separator
			witnessVersionChar := lowerAddr[len(hrpPrefix)]

			switch {
			case witnessVersionChar == 'q' && len(witnessProgram) == 20:
				// P2WPKH: OP_0 <20 bytes>
				script := []byte{0x00, 0x14}
				script = append(script, witnessProgram...)
				return script, nil
			case witnessVersionChar == 'q' && len(witnessProgram) == 32:
				// P2WSH: OP_0 <32 bytes>
				script := []byte{0x00, 0x20}
				script = append(script, witnessProgram...)
				return script, nil
			case witnessVersionChar == 'p' && len(witnessProgram) == 32:
				// P2TR: OP_1 <32 bytes>
				script := []byte{0x51, 0x20}
				script = append(script, witnessProgram...)
				return script, nil
			default:
				return nil, fmt.Errorf("unsupported witness program: version=%c len=%d", witnessVersionChar, len(witnessProgram))
			}
		}

		// Also handle testnet bech32 for BTC/BC2
		if (coinDef.CoinID == "btc" || coinDef.CoinID == "bc2") && strings.HasPrefix(lowerAddr, "tb1") {
			witnessProgram, err := Bech32Decode("tb", addr)
			if err != nil {
				return nil, fmt.Errorf("bech32 testnet decode: %w", err)
			}
			witnessVersionChar := lowerAddr[3] // character after "tb1"
			switch {
			case witnessVersionChar == 'q' && len(witnessProgram) == 20:
				script := []byte{0x00, 0x14}
				script = append(script, witnessProgram...)
				return script, nil
			case witnessVersionChar == 'q' && len(witnessProgram) == 32:
				script := []byte{0x00, 0x20}
				script = append(script, witnessProgram...)
				return script, nil
			case witnessVersionChar == 'p' && len(witnessProgram) == 32:
				script := []byte{0x51, 0x20}
				script = append(script, witnessProgram...)
				return script, nil
			}
		}
	}

	// Try base58check: P2PKH
	if coinDef.P2PKHPrefixChar != 0 && addr[0] == coinDef.P2PKHPrefixChar {
		result, err := base58CheckDecodeWithVersion(addr)
		if err == nil && result.version == coinDef.P2PKHVersion {
			// OP_DUP OP_HASH160 <20 bytes> OP_EQUALVERIFY OP_CHECKSIG
			script := []byte{0x76, 0xa9, 0x14}
			script = append(script, result.payload...)
			script = append(script, 0x88, 0xac)
			return script, nil
		}
	}

	// Try base58check: P2SH
	if coinDef.P2SHPrefixChar != 0 && addr[0] == coinDef.P2SHPrefixChar {
		result, err := base58CheckDecodeWithVersion(addr)
		if err == nil && result.version == coinDef.P2SHVersion {
			// OP_HASH160 <20 bytes> OP_EQUAL
			script := []byte{0xa9, 0x14}
			script = append(script, result.payload...)
			script = append(script, 0x87)
			return script, nil
		}
	}

	// BTC/BC2 testnet legacy addresses
	if coinDef.CoinID == "btc" || coinDef.CoinID == "bc2" {
		if addr[0] == 'm' || addr[0] == 'n' {
			result, err := base58CheckDecodeWithVersion(addr)
			if err == nil {
				script := []byte{0x76, 0xa9, 0x14}
				script = append(script, result.payload...)
				script = append(script, 0x88, 0xac)
				return script, nil
			}
		}
		if addr[0] == '2' {
			result, err := base58CheckDecodeWithVersion(addr)
			if err == nil {
				script := []byte{0xa9, 0x14}
				script = append(script, result.payload...)
				script = append(script, 0x87)
				return script, nil
			}
		}
	}

	return nil, fmt.Errorf("unsupported address format for %s: %s", coinDef.Name, addr)
}

// cashAddrToScript converts a decoded CashAddr to a scriptPubKey.
func cashAddrToScript(addrType int, hash []byte) ([]byte, error) {
	switch addrType {
	case 0: // P2PKH
		if len(hash) != 20 {
			return nil, fmt.Errorf("P2PKH hash must be 20 bytes, got %d", len(hash))
		}
		script := []byte{0x76, 0xa9, 0x14}
		script = append(script, hash...)
		script = append(script, 0x88, 0xac)
		return script, nil
	case 1: // P2SH
		if len(hash) != 20 {
			return nil, fmt.Errorf("P2SH hash must be 20 bytes, got %d", len(hash))
		}
		script := []byte{0xa9, 0x14}
		script = append(script, hash...)
		script = append(script, 0x87)
		return script, nil
	default:
		return nil, fmt.Errorf("unsupported CashAddr type: %d", addrType)
	}
}

// --- Base58Check decoding ---

type base58Result struct {
	version byte
	payload []byte
}

// base58CheckDecodeWithVersion decodes a base58check address returning the version byte and payload.
func base58CheckDecodeWithVersion(addr string) (*base58Result, error) {
	alphabet := "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

	// Decode base58
	result := make([]byte, 0, 25)
	for _, c := range addr {
		idx := -1
		for i, a := range alphabet {
			if a == c {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("invalid base58 character: %c", c)
		}

		carry := idx
		for j := len(result) - 1; j >= 0; j-- {
			carry += 58 * int(result[j])
			result[j] = byte(carry & 0xff)
			carry >>= 8
		}
		for carry > 0 {
			result = append([]byte{byte(carry & 0xff)}, result...)
			carry >>= 8
		}
	}

	// Add leading zeros
	for _, c := range addr {
		if c != '1' {
			break
		}
		result = append([]byte{0x00}, result...)
	}

	if len(result) < 5 {
		return nil, fmt.Errorf("base58check too short")
	}

	version := result[0]
	payload := result[1 : len(result)-4]
	return &base58Result{version: version, payload: payload}, nil
}

// Base58CheckDecode decodes a base58check address and returns just the payload (no version byte).
// Provided for backward compatibility.
func Base58CheckDecode(addr string) ([]byte, error) {
	r, err := base58CheckDecodeWithVersion(addr)
	if err != nil {
		return nil, err
	}
	return r.payload, nil
}

// --- Bech32 decoding ---

// Bech32Decode decodes a bech32/bech32m address with the given HRP and returns the witness program.
func Bech32Decode(hrp, addr string) ([]byte, error) {
	addr = strings.ToLower(addr)

	// Find the separator (last '1')
	sep := -1
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == '1' {
			sep = i
			break
		}
	}
	if sep < 0 {
		return nil, fmt.Errorf("no separator found")
	}

	// Verify HRP
	if addr[:sep] != strings.ToLower(hrp) {
		return nil, fmt.Errorf("HRP mismatch: expected %s, got %s", hrp, addr[:sep])
	}

	data := addr[sep+1:]
	if len(data) < 7 {
		return nil, fmt.Errorf("bech32 data too short")
	}

	// Decode base32
	charset := "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
	values := make([]int, len(data))
	for i, c := range data {
		idx := -1
		for j, a := range charset {
			if a == c {
				idx = j
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("invalid bech32 character: %c", c)
		}
		values[i] = idx
	}

	// Strip checksum (last 6 values) and witness version (first value)
	if len(values) < 8 {
		return nil, fmt.Errorf("bech32 data too short after stripping")
	}
	conv := values[1 : len(values)-6]

	// Convert from 5-bit groups to 8-bit groups
	var result []byte
	acc := 0
	bits := 0
	for _, v := range conv {
		acc = (acc << 5) | v
		bits += 5
		for bits >= 8 {
			bits -= 8
			result = append(result, byte((acc>>bits)&0xff))
		}
	}

	return result, nil
}
