package coin

import (
	"encoding/hex"
	"testing"
)

// The canonical CashAddr spec P2PKH example and its legacy (base58) twin share
// the SAME hash160. The base58 path is already known-correct (genesis test), so
// legacy is the ground truth we cross-check the CashAddr decoder against.
func TestCashAddrDecodeVectors(t *testing.T) {
	bch := Get("bch")
	if bch == nil {
		t.Fatal("bch not in registry")
	}
	const wantScript = "76a91476a04053bda0a88bda5177b86a15c3b29f55987388ac"

	legacyScript, err := AddressToScriptPubKey(bch, "1BpEi6DfDAUFd7GtittLSdBeYJvcoaVggu")
	if err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	if hex.EncodeToString(legacyScript) != wantScript {
		t.Fatalf("legacy ground-truth mismatch: got %s want %s", hex.EncodeToString(legacyScript), wantScript)
	}

	for _, addr := range []string{
		"bitcoincash:qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a", // with prefix
		"qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a",             // bare (BitAxe often sends this)
		"bitcoincash:QPM2QSZNHKS23Z7629MMS6S4CWEF74VCWVY22GDX6A", // uppercase
	} {
		got, err := AddressToScriptPubKey(bch, addr)
		if err != nil {
			t.Errorf("%s: error %v", addr, err)
			continue
		}
		if hex.EncodeToString(got) != wantScript {
			t.Errorf("%s:\n got  %s\n want %s", addr, hex.EncodeToString(got), wantScript)
		} else {
			t.Logf("OK %s", addr)
		}
	}

	// XEC (eCash) uses the identical DecodeCashAddr path, only the prefix differs.
	// Encode an eCash P2PKH address for the SAME hash160 from the in-package
	// helpers, then decode it — a round-trip that needs no memorized vector.
	xec := Get("xec")
	if xec == nil {
		t.Fatal("xec not in registry")
	}
	hash160, _ := hex.DecodeString("76a04053bda0a88bda5177b86a15c3b29f559873")
	xecAddr := encodeCashAddrP2PKH("ecash", hash160)
	t.Logf("encoded XEC addr = %s", xecAddr)
	for _, addr := range []string{xecAddr, xecAddr[len("ecash:"):]} {
		got, err := AddressToScriptPubKey(xec, addr)
		if err != nil {
			t.Errorf("XEC %s: error %v", addr, err)
			continue
		}
		if hex.EncodeToString(got) != wantScript {
			t.Errorf("XEC %s:\n got  %s\n want %s", addr, hex.EncodeToString(got), wantScript)
		} else {
			t.Logf("OK XEC %s", addr)
		}
	}
}

// encodeCashAddrP2PKH builds a P2PKH CashAddr for the given prefix and 20-byte
// hash, using the same primitives DecodeCashAddr relies on (test-only helper).
func encodeCashAddrP2PKH(prefix string, hash20 []byte) string {
	payload := append([]uint64{0x00}, bytesToU64(hash20)...) // version byte 0x00 + hash
	conv, _ := convertBits(payload, 8, 5, true)
	payload5 := bytesToU64(conv)

	values := append(cashAddrExpandPrefix(prefix), payload5...)
	values = append(values, 0, 0, 0, 0, 0, 0, 0, 0) // checksum template
	mod := cashAddrPolymod(values)
	var checksum []uint64
	for i := 0; i < 8; i++ {
		checksum = append(checksum, (mod>>uint(5*(7-i)))&0x1f)
	}

	out := prefix + ":"
	for _, v := range append(payload5, checksum...) {
		out += string(cashAddrCharset[v])
	}
	return out
}

func bytesToU64(b []byte) []uint64 {
	u := make([]uint64, len(b))
	for i, x := range b {
		u[i] = uint64(x)
	}
	return u
}
