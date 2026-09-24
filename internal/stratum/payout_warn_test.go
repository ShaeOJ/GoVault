package stratum

import (
	"testing"

	"govault/internal/coin"
)

func TestPayoutMismatch(t *testing.T) {
	btc := coin.Get("btc")
	if btc == nil {
		t.Fatal("btc coin not in registry")
	}
	const payout = "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa" // configured payout
	other := "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4"

	cases := []struct {
		name       string
		payout     string
		worker     string
		wantOK     bool
		wantCandid string
	}{
		{"different valid address -> warn", payout, other, true, other},
		{"different valid address with .worker -> warn", payout, other + ".bitaxe", true, other},
		{"same address -> no warn", payout, payout, false, ""},
		{"same address with .worker -> no warn", payout, payout + ".rig1", false, ""},
		{"plain worker label -> no warn", payout, "bitaxe1", false, ""},
		{"empty payout configured -> no warn", "", other, false, ""},
		{"empty worker -> no warn", payout, "", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := payoutMismatch(btc, c.payout, c.worker)
			if ok != c.wantOK || got != c.wantCandid {
				t.Errorf("payoutMismatch(%q,%q) = (%q,%v), want (%q,%v)",
					c.payout, c.worker, got, ok, c.wantCandid, c.wantOK)
			}
		})
	}

	// nil coinDef must never warn (guards against a not-yet-configured server).
	if _, ok := payoutMismatch(nil, payout, other); ok {
		t.Error("nil coinDef should not warn")
	}
}
