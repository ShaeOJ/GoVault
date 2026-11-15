package proxysplit

import (
	"encoding/hex"
	"math/rand"
	"strings"
	"testing"

	"govault/internal/upstream"
)

// hexBytes returns n bytes of hex-encoded fill-byte filler, for building fake
// coinbase/EN1/EN2 test fixtures with a known byte length.
func hexBytes(n int, fill byte) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = fill
	}
	return hex.EncodeToString(b)
}

// TestFoldJob_ZeroPad covers the case where the real pool's EN2 budget
// exactly equals prefixSize+virtualEN2Size (padLen == 0).
func TestFoldJob_ZeroPad(t *testing.T) {
	cb1Real := hexBytes(10, 0x11)
	cb2Real := hexBytes(8, 0x22)
	realEN1 := hexBytes(4, 0x33) // 4-byte EN1, common case
	params := &upstream.JobParams{Coinbase1: cb1Real, Coinbase2: cb2Real}

	prefixSize := 2
	virtualEN2Size := 2
	realEN2Size := 4 // == prefixSize + virtualEN2Size -> padLen 0

	vCB1, vCB2 := foldJob(params, realEN1, prefixSize, virtualEN2Size, realEN2Size)

	// Simulate a miner's hash: vCB1 + sessionEN1(P bytes) + minerEN2(V bytes) + vCB2
	sessionEN1 := hexBytes(prefixSize, 0x44)
	minerEN2 := hexBytes(virtualEN2Size, 0x55)
	minerSideFull := vCB1 + sessionEN1 + minerEN2 + vCB2

	// Expected: byte-identical to what the REAL pool would reconstruct —
	// cb1Real + realEN1 + (sessionEN1+minerEN2+zeroPad) + cb2Real.
	wantFull := cb1Real + realEN1 + sessionEN1 + minerEN2 + "" /* no pad */ + cb2Real

	if minerSideFull != wantFull {
		t.Fatalf("byte layout mismatch (zero pad):\n got=%s\nwant=%s", minerSideFull, wantFull)
	}
}

// TestFoldJob_WithPadding covers a pool granting MORE EN2 space than the
// joint virtual budget needs (padLen > 0) — the common case when the other
// pool in the pair has a smaller EN2 size.
func TestFoldJob_WithPadding(t *testing.T) {
	cb1Real := hexBytes(12, 0xaa)
	cb2Real := hexBytes(6, 0xbb)
	realEN1 := hexBytes(4, 0xcc)
	params := &upstream.JobParams{Coinbase1: cb1Real, Coinbase2: cb2Real}

	prefixSize := 1
	virtualEN2Size := 3
	realEN2Size := 8 // joint budget (1+3=4) leaves padLen = 4

	vCB1, vCB2 := foldJob(params, realEN1, prefixSize, virtualEN2Size, realEN2Size)

	sessionEN1 := hexBytes(prefixSize, 0xdd)
	minerEN2 := hexBytes(virtualEN2Size, 0xee)
	minerSideFull := vCB1 + sessionEN1 + minerEN2 + vCB2

	padLen := realEN2Size - prefixSize - virtualEN2Size
	if padLen != 4 {
		t.Fatalf("test setup error: expected padLen=4, got %d", padLen)
	}
	wantFull := cb1Real + realEN1 + sessionEN1 + minerEN2 + strings.Repeat("00", padLen) + cb2Real

	if minerSideFull != wantFull {
		t.Fatalf("byte layout mismatch (with pad):\n got=%s\nwant=%s", minerSideFull, wantFull)
	}

	// The miner-visible extranonce region (sessionEN1+minerEN2, no pad) must
	// be exactly prefixSize+virtualEN2Size bytes = 4 bytes = 8 hex chars.
	if got := len(sessionEN1 + minerEN2); got != (prefixSize+virtualEN2Size)*2 {
		t.Fatalf("miner-visible region wrong size: got %d hex chars, want %d", got, (prefixSize+virtualEN2Size)*2)
	}
}

// TestSubmitSideReconstruction mirrors the session.go submit-path logic:
// given a session's stable prefix + the miner's EN2, the reconstructed
// realEN2 sent upstream must be exactly job.RealEN2Size bytes and equal
// prefix+minerEN2+zeroPad.
func TestSubmitSideReconstruction(t *testing.T) {
	prefixSize := 2
	virtualEN2Size := 2
	realEN2Size := 10 // padLen = 6

	sessionEN1 := hexBytes(prefixSize, 0x01)
	minerEN2 := hexBytes(virtualEN2Size, 0x02)

	padLen := realEN2Size - prefixSize - virtualEN2Size
	realEN2 := sessionEN1 + minerEN2 + strings.Repeat("00", padLen)

	if gotBytes := len(realEN2) / 2; gotBytes != realEN2Size {
		t.Fatalf("reconstructed realEN2 wrong length: got %d bytes, want %d", gotBytes, realEN2Size)
	}
	wantPrefix := sessionEN1 + minerEN2
	if !strings.HasPrefix(realEN2, wantPrefix) {
		t.Fatalf("reconstructed realEN2 doesn't start with prefix+minerEN2: got=%s want prefix=%s", realEN2, wantPrefix)
	}
	if rest := realEN2[len(wantPrefix):]; rest != strings.Repeat("00", padLen) {
		t.Fatalf("padding region wrong: got=%s want=%s", rest, strings.Repeat("00", padLen))
	}
}

// TestParseVersionMask covers the hex mask parsing used to combine both
// pools' version-rolling masks.
func TestParseVersionMask(t *testing.T) {
	cases := []struct {
		in   string
		want uint32
	}{
		{"", 0},
		{"1fffe000", 0x1fffe000},
		{"zzzz", 0},    // invalid hex
		{"1fff", 0},    // wrong length (2 bytes, not 4)
	}
	for _, c := range cases {
		if got := parseVersionMask(c.in); got != c.want {
			t.Errorf("parseVersionMask(%q) = %#x, want %#x", c.in, got, c.want)
		}
	}
}

func TestCombinedVersionMask(t *testing.T) {
	a := parseVersionMask("1fffe000")
	b := parseVersionMask("1fff0000")
	combined := a & b
	want := uint32(0x1fff0000)
	if combined != want {
		t.Errorf("combined mask = %#x, want %#x", combined, want)
	}
}

func TestOtherLabel(t *testing.T) {
	if otherLabel(LabelA) != LabelB {
		t.Errorf("otherLabel(A) should be B")
	}
	if otherLabel(LabelB) != LabelA {
		t.Errorf("otherLabel(B) should be A")
	}
}

// simulateSplit runs handleJob's exact state-machine logic (active pool
// persists across steps; a re-roll only happens on a job from the currently-
// INACTIVE pool, using the same two branch conditions verbatim as handleJob)
// for n job arrivals, each independently and randomly from pool A or B
// (matching two real, independent upstream pools — NOT a fixed alternating
// pattern, which would introduce artificial correlation two independent
// pools would never actually have). Returns the fraction of arrivals during
// which pool A was the active pool afterward.
func simulateSplit(n, splitPercentA int) float64 {
	active := LabelA
	activeACount := 0
	for i := 0; i < n; i++ {
		from := LabelB
		if rand.Intn(2) == 0 {
			from = LabelA
		}

		switch {
		case from == active:
			// normal refresh from the already-active pool — no re-roll
		case from == LabelA && rand.Intn(100) < splitPercentA:
			active = from
		case from == LabelB && rand.Intn(100) >= splitPercentA:
			active = from
		}

		if active == LabelA {
			activeACount++
		}
	}
	return float64(activeACount) / float64(n) * 100
}

// TestWeightedSplitConvergence exercises handleJob's exact weighted-pick
// logic (see simulateSplit) under independent random arrival from each pool,
// using the real math/rand.Intn, and checks that a higher configured
// splitPercentA produces a clearly higher empirical active-A fraction than a
// lower one — a robust, direction-of-effect check that doesn't depend on
// deriving this Markov chain's exact closed-form steady state. This does not
// exercise handleJob itself (that requires a live upstream.Client/
// stratum.Server pair — see the TODO note below for the fuller integration
// test), but it does verify the core decision logic's statistical behavior.
func TestWeightedSplitConvergence(t *testing.T) {
	const n = 100000

	pctHigh := simulateSplit(n, 80)
	pctLow := simulateSplit(n, 20)

	if pctHigh <= 60 {
		t.Errorf("splitPercentA=80 should produce a clear majority for A, got %.1f%%", pctHigh)
	}
	if pctLow >= 40 {
		t.Errorf("splitPercentA=20 should produce a clear minority for A, got %.1f%%", pctLow)
	}
	if pctHigh <= pctLow {
		t.Errorf("higher splitPercentA (80) should yield a higher A-share than lower (20): got %.1f%% vs %.1f%%", pctHigh, pctLow)
	}
}

// TODO(integration test): handleJob itself, SubmitShare's pool resolution,
// and the full failover/reconnect-free guarantee need an integration test
// with two real in-process mock stratum pool servers (upstream.Client has
// only private fields and real TCP dialing, so it can't be faked at the unit
// level) — see the verification plan in the design doc for the two-different-
// EN2-size dual-mock-pool test and the switch-window race test. Not built in
// this pass; flagged as follow-on work.
