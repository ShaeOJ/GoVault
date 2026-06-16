package main

import "fmt"

// FirepoolHost is the upstream pool hostname. Edge nodes connect to this.
//
// ⚠️ PLACEHOLDER: FirePool2 currently runs on the home LAN (fracpool), reachable
// as "vault.local" / 10.0.0.114 — fine for LAN testing only. mDNS (.local) does
// NOT resolve for remote users. Before shipping the edge node to end users,
// change this to a publicly reachable host: a DDNS name pointed at a port-forward
// of the stratum ports, or a public VPS running FirePool2. (The old "firepool.ca"
// is decommissioned.)
const FirepoolHost = "vault.local"

// PoolMode distinguishes proportional (PROP) from solo mining.
type PoolMode string

const (
	ModeProp PoolMode = "prop"
	ModeSolo PoolMode = "solo"
)

// FirepoolPool describes one Firepool stratum endpoint.
type FirepoolPool struct {
	Coin        string   // display symbol
	CoinID      string   // internal coin package ID
	Mode        PoolMode // "prop" or "solo"
	PrimaryPort int      // main port
	AltPort     int      // backup port
	Description string
	WorkerName  string   // Firepool node wallet address for this coin
}

// URL returns the primary stratum+tcp URL for this pool.
func (p FirepoolPool) URL() string {
	return fmt.Sprintf("stratum+tcp://%s:%d", FirepoolHost, p.PrimaryPort)
}

// AltURL returns the backup stratum+tcp URL for this pool.
func (p FirepoolPool) AltURL() string {
	return fmt.Sprintf("stratum+tcp://%s:%d", FirepoolHost, p.AltPort)
}

// Firepool node wallet addresses — the default operator/payout address pre-filled
// for each endpoint (used as the upstream auth worker when an operator doesn't
// override it). These are FirePool2's own pool payout addresses, so they are
// guaranteed valid for the coin. Individual miners normally connect with their
// OWN address.worker, which the edge node forwards transparently for payout.
const (
	workerBC2  = "13aKW4XogG7aGyN8Wfx5xAQfGYyEfPTo3s"
	workerBTC  = "14J6VZEGmW9n8dLinRZiN4yHFDUJodKM8U"
	workerBCH  = "1EjpigKqvF9N4KoDnnCTRbbSy6xcpaAwc6"
	workerBCH2 = "bitcoincashii:qp3mdmxvcyzyd7ynfpwjw7tmwqukxdplsvjc8kxnwt"
	workerBTCS = "BmxjZjS2LmeL5TsgCVGFq7nRbu3Ww46bif"
	workerFIX  = "RqkrJm8fJk3sHSzg9njTAVKqx4isVxN7k"
)

// FirepoolPools lists every FirePool2 stratum endpoint.
// Source: FirePool2 3-tier port scheme (general/solo). Coin prefix digits:
// btc 4, bch 5, bc2 3, bch2 7, btcs 6, fix 9. Per the scheme, X333 = general
// PROP, X339 = general SOLO, X334 = nerdminer PROP, X343 = nerdminer SOLO.
// (DGB and XEC are not deployed on FirePool2, so they are intentionally absent;
// XEC's old 9333 also collides with fix.)
var FirepoolPools = []FirepoolPool{
	// ── BTC (Bitcoin) ─────────────────────────────────────────────────────
	{Coin: "BTC",  CoinID: "btc",  Mode: ModeProp, PrimaryPort: 4333, AltPort: 4334, WorkerName: workerBTC,  Description: "Bitcoin — proportional"},
	{Coin: "BTC",  CoinID: "btc",  Mode: ModeSolo, PrimaryPort: 4339, AltPort: 4343, WorkerName: workerBTC,  Description: "Bitcoin — solo"},

	// ── BCH (Bitcoin Cash) ────────────────────────────────────────────────
	{Coin: "BCH",  CoinID: "bch",  Mode: ModeProp, PrimaryPort: 5333, AltPort: 5334, WorkerName: workerBCH,  Description: "Bitcoin Cash — proportional"},
	{Coin: "BCH",  CoinID: "bch",  Mode: ModeSolo, PrimaryPort: 5339, AltPort: 5343, WorkerName: workerBCH,  Description: "Bitcoin Cash — solo"},

	// ── BC2 (Bitcoin II) ──────────────────────────────────────────────────
	{Coin: "BC2",  CoinID: "bc2",  Mode: ModeProp, PrimaryPort: 3333, AltPort: 3334, WorkerName: workerBC2,  Description: "Bitcoin II — proportional"},
	{Coin: "BC2",  CoinID: "bc2",  Mode: ModeSolo, PrimaryPort: 3339, AltPort: 3343, WorkerName: workerBC2,  Description: "Bitcoin II — solo"},

	// ── BCH2 (Bitcoin Cash II) ────────────────────────────────────────────
	{Coin: "BCH2", CoinID: "bch2", Mode: ModeProp, PrimaryPort: 7333, AltPort: 7334, WorkerName: workerBCH2, Description: "Bitcoin Cash II — proportional"},
	{Coin: "BCH2", CoinID: "bch2", Mode: ModeSolo, PrimaryPort: 7339, AltPort: 7343, WorkerName: workerBCH2, Description: "Bitcoin Cash II — solo"},

	// ── BTCS (Bitcoin Silver) ─────────────────────────────────────────────
	{Coin: "BTCS", CoinID: "btcs", Mode: ModeProp, PrimaryPort: 6333, AltPort: 6334, WorkerName: workerBTCS, Description: "Bitcoin Silver — proportional"},
	{Coin: "BTCS", CoinID: "btcs", Mode: ModeSolo, PrimaryPort: 6339, AltPort: 6343, WorkerName: workerBTCS, Description: "Bitcoin Silver — solo"},

	// ── FIX (FixedCoin) ───────────────────────────────────────────────────
	{Coin: "FIX",  CoinID: "fix",  Mode: ModeProp, PrimaryPort: 9333, AltPort: 9334, WorkerName: workerFIX,  Description: "FixedCoin — proportional"},
	{Coin: "FIX",  CoinID: "fix",  Mode: ModeSolo, PrimaryPort: 9339, AltPort: 9343, WorkerName: workerFIX,  Description: "FixedCoin — solo"},
}

// FindPool returns the matching FirepoolPool or nil.
func FindPool(coin string, mode PoolMode) *FirepoolPool {
	for _, p := range FirepoolPools {
		if p.Coin == coin && p.Mode == mode {
			return &p
		}
	}
	return nil
}
