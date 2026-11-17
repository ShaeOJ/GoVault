# SV2 Solo — Stratum V2 for solo-mine-to-own-node in GoVault

Status: **DESIGN / not started.** Scope agreed: **solo mode only** (mine to your own
node) and **BTC only**. This is the recommended *first* real SV2 product — it is
far simpler to build and ship than a public SV2 pool, because GoVault already owns
the entire template→block pipeline.

Prior art: SV2 was validated end-to-end against the SRI reference stack
(`pool_sv2` + `sv2-tp` + a multiprocess Bitcoin Core v32) with a real BitAxe
(AxeOS v2.15) on testnet4. That proved the *protocol* + the miner interop. This
doc is about doing SV2 **natively in GoVault's Go code** for the solo case.

---

## Why solo-in-GoVault avoids the entire heavy stack

The asicpool/SRI pool path needed a Template Provider (`sv2-tp`), a **multiprocess
Bitcoin Core v32** (`-ipcbind`), the Template Distribution Protocol over IPC, and
(for multi-user) a Postgres payout bridge. **None of that is needed here**, because
GoVault's solo mode (`appcore.startSolo`) already:

- pulls templates from the user's **plain stock bitcoind via RPC `getblocktemplate`**,
- builds the coinbase (split at the extranonce boundary → `Coinbase1`/`Coinbase2`)
  and merkle branches (`internal/node/blocktemplate.go`),
- serves jobs + validates shares (`internal/stratum/share.go`), and
- on a hash meeting the network target → `buildFullBlock` → `node.Client.SubmitBlock`
  (`submitblock`) to the user's node.

| Pool SV2 needed | GoVault SV2-solo |
|---|---|
| `sv2-tp` Template Provider | ❌ — GoVault builds templates itself via RPC |
| multiprocess Core v32 / IPC | ❌ — **user keeps their stock bitcoind** |
| Template Distribution Protocol | ❌ |
| Job Declaration | ❌ — GoVault sets coinbase = user's address |
| Postgres / PROP accounting | ❌ — solo, one user, own coinbase |
| Bundling Rust into the desktop app | ❌ — stays one Go binary |

The user's node stays a **stock bitcoind**. That removes the one genuine blocker
(multiprocess Core) and the packaging headache (Rust bundling). GoVault remains a
single self-contained Go binary that simply gains an SV2 listener.

---

## The key fact: GoVault's `Job` already holds everything SV2 needs

`internal/stratum/jobs.go`:
```go
type Job struct {
    ID             string
    PrevHash       string   // stratum-formatted
    Coinbase1      string   // hex: before extranonce insertion point
    Coinbase2      string   // hex: after extranonce insertion point
    MerkleBranches []string
    Version        string
    NBits          string
    NTime          string
    Template       *node.BlockTemplate
    SegWit         bool
}
```
SV2 Extended Channels need exactly these: a coinbase split into
prefix/suffix around the extranonce, the merkle path, version, nBits, ntime, and
prevhash. **SV2-solo is "re-encode this same `Job` onto the SV2 wire".** No new
template machinery.

Likewise the inbound side reuses the existing validator unchanged:
```go
type ShareResult struct { Valid, BlockFound bool; Difficulty float64; BlockHash, BlockHex string }
func (sv *ShareValidator) ValidateShare(extranonce1 string, sub ShareSubmission) (*ShareResult, *StratumError)
```
An SV2 `SubmitSharesExtended` carries (channel extranonce, nonce, ntime, version,
job_id) — the shim maps it to a `ShareSubmission` and calls `ValidateShare`. If
`BlockFound`, the existing `buildFullBlock` + `SubmitBlock` path fires. Identical
block-submission code for V1 and SV2.

---

## Architecture — one new listener, ~80% reuse

```
BitAxe (SV2) ──Noise + SV2 binary──▶ [NEW] GoVault SV2 listener ─┐
                                                                 ├─▶ SHARED: JobManager / template (RPC getblocktemplate)
user miner (V1) ────mining.notify───▶ existing V1 listener ──────┘   SHARED: ValidateShare → buildFullBlock → SubmitBlock → user's node
```

Both listeners run off the same `JobManager` and the same solo template loop in
`appcore`. A new template → broadcast to V1 miners (`mining.notify`) **and** SV2
miners (`NewExtendedMiningJob` + `SetNewPrevHash`).

### New code (bounded) — `internal/stratum/sv2/`
- `noise.go` — Noise_NX **responder** handshake. Use the mature `github.com/flynn/noise`
  Go library (SV2 handshake = `Noise_NX_25519_ChaChaPoly_BLAKE2s`). Pool holds a
  static authority keypair; pubkey is shown in the UI so the miner can pin it.
- `codec.go` — encode/decode the **downstream message subset only** (the SV2 frame
  header + these messages — everything else in the spec is out of scope):
  - common: `SetupConnection` / `SetupConnection.Success` / `.Error`
  - mining: `OpenExtendedMiningChannel` / `.Success` / `.Error`
  - mining: `NewExtendedMiningJob`, `SetNewPrevHash`
  - mining: `SubmitSharesExtended` / `SubmitShares.Success` / `.Error`
  - (optional) `SetTarget` for vardiff, `UpdateChannel`
- `server.go` — SV2 TCP listener, per-connection Noise session, channel state,
  extranonce allocation, vardiff (reuse the existing vardiff logic/targets).
- `shim.go` — `Job → NewExtendedMiningJob` encoder, and
  `SubmitSharesExtended → ShareSubmission` decoder into `ValidateShare`.

### Reused unchanged
`JobManager`, coinbase/merkle building (`node/blocktemplate.go`), `ValidateShare`
+ `buildFullBlock` (`stratum/share.go`), `node.Client` (RPC + `submitblock`),
config, the solo template loop in `appcore`, vardiff. Rough split: ~80% reuse,
new code is the SV2 wire layer + shim.

---

## Config (additive)

```jsonc
{
  "miningMode": "solo",
  "mining": { "payoutAddress": "bc1q..." },   // SV2 coinbase pays this (user's own)
  "sv2": {
    "enabled": true,
    "listen": "0.0.0.0:34333",                // SV2 port (V1 port unchanged)
    "authoritySecretKey": "...",              // generated on first run; pubkey shown in UI
    "requireAuth": false
  }
}
```
No change to the existing V1 path; SV2 is a parallel opt-in listener.

The AxeOS "you don't have a share in the mining reward" warning clears when the
miner's **User field == the coinbase address** (confirmed in testing). In solo
that is simply the user's own `payoutAddress` — GoVault's UI should tell the user
to set the BitAxe User field to their payout address.

---

## Build / test plan (staged)

1. **S0 — codec + Noise against a known-good reference.** Implement `noise.go` +
   `codec.go`; unit-test encode/decode round-trips. Cross-validate the wire format
   against the SRI reference implementation (`pool_sv2`, and/or decode captured SRI
   frames) before any hardware.
2. **S1 — SV2 listener serving jobs (regtest).** Wire `server.go` + `shim.go` into
   the solo loop; GoVault serves `NewExtendedMiningJob` from a regtest template;
   confirm a software SV2 test-miner opens a channel and gets jobs.
3. **S2 — real BitAxe (regtest/testnet).** Point the BitAxe (AxeOS v2.15, SV2,
   Extended Channels, User=payout addr, Authority pubkey) at GoVault; confirm Noise
   handshake + `valid share` + (regtest) a found block submitted to the user's node
   via `submitblock`.
4. **S3 — polish:** vardiff tuning, authority-key UI, reconnect handling, the
   "set User = payout address" guidance, docs.

---

## Scope / honesty
- **The real work is the SV2 codec + Noise in Go, from scratch** — there is no
  battle-tested Go SV2 library. Bounded by scope (solo + Extended Channel + BTC
  trims the message set to ~8), rough order ~1.5–3k lines + testing. Everything
  downstream of the wire (template, validation, block submit) is reuse.
- **Payoff:** ships as the existing single Go binary + an opt-in SV2 port; no Rust,
  no `sv2-tp`, no multiprocess node, no custody. Dramatically simpler to ship than
  the pool path, and the purest expression of sovereign solo mining (your block,
  your node, your coinbase) — on brand.
- **Out of scope (by decision):** proxy mode SV2, non-BTC coins, Job Declaration,
  multi-user/PROP. Those belong to the pool roadmap, not here.
- **Reference for testing:** the SRI `pool_sv2` + a v32 multiprocess node make a
  ready reference implementation to validate GoVault's SV2 encoder against, and a
  BitAxe on AxeOS v2.15 is a proven SV2 test rig.
