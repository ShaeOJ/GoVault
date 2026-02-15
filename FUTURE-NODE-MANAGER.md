# Future Plan: Node Connection Options

> Give users three ways to connect to a node — no mandatory downloads. Pick from a curated list, use an existing local node, or manually configure.

## The Constraint

Solo mining requires `getblocktemplate` (GBT) RPC access. Public blockchain explorers and light API nodes **do not** support GBT. You need either:

1. **Your own full node** (local or remote with RPC enabled)
2. **A solo mining pool service** that runs the node for you and exposes stratum

This means a "list of public nodes" in the traditional sense won't work. But a curated list of **solo pool endpoints** and **auto-detection of local nodes** covers the same user need without downloading anything.

---

## Three Connection Modes

### Mode 1: Solo Pool Services (no node needed)

For users who don't want to run a node at all. Connect to established solo mining pool services that handle the node internally.

**How it works:** GoVault connects to the solo pool's stratum endpoint as an upstream, then re-serves work to local miners. GoVault acts as a **stratum proxy** — miners connect to GoVault, GoVault connects to the solo pool.

**Curated list per coin:**

| Coin | Service | Endpoint | Fee | Notes |
|------|---------|----------|-----|-------|
| BTC | solo.ckpool.org | stratum+tcp://solo.ckpool.org:3333 | 2% | Run by ckpool author, most trusted |
| BTC | solo.coinindia.com | stratum+tcp://solo.coinindia.com:3333 | 0% | Community, verify uptime |
| DGB | -- | -- | -- | Research needed |
| BCH | -- | -- | -- | Research needed |
| XEC | -- | -- | -- | Research needed |

**UI flow:**
```
Node Connection
  (*) Solo Pool Service (no node required)
  ( ) Local Node (auto-detect)
  ( ) Manual Configuration

  Select Service:
  [v] solo.ckpool.org (BTC, 2% fee)        [Connected]
      Endpoint: stratum+tcp://solo.ckpool.org:3333
      Status: Connected, receiving work
      Fee: 2% of block reward

  Your Payout Address: bc1q...
  [Connect]
```

**Architecture change needed:**
- New `internal/proxy/` package — stratum client that connects upstream
- GoVault's stratum server gets work from the proxy instead of from GBT
- Block found notifications come from the upstream pool
- Payout address is set as the worker name (standard for solo pools)

### Mode 2: Auto-Detect Local Node

For users who already have a node installed. GoVault scans default ports and tries to connect.

**How it works:** On the Node settings page, a "Detect" button probes known ports with a quick RPC call. If a node responds, auto-fill the connection settings.

**Detection logic per coin:**

| Coin | Probe | Default Port | Default Datadir |
|------|-------|-------------|-----------------|
| BTC | `getblockchaininfo` on 127.0.0.1:8332 | 8332 | `%APPDATA%\Bitcoin` |
| BCH | `getblockchaininfo` on 127.0.0.1:8332 | 8332 | `%APPDATA%\Bitcoin` |
| DGB | `getblockchaininfo` on 127.0.0.1:14022 | 14022 | `%APPDATA%\DigiByte` |
| XEC | `getblockchaininfo` on 127.0.0.1:8332 | 8332 | `%APPDATA%\Bitcoin ABC` |

**Also try:**
- Read cookie auth from `{datadir}/.cookie` (Bitcoin Core auto-generates this)
- Parse existing `bitcoin.conf` for rpcuser/rpcpassword
- Check common alternative ports (18332 for testnet, etc.)

**UI flow:**
```
Node Connection
  ( ) Solo Pool Service
  (*) Local Node (auto-detect)
  ( ) Manual Configuration

  [Scan for Nodes]

  Found: Bitcoin Core v30.2 on 127.0.0.1:8332
    Chain: main | Height: 884,200 | Sync: 100%
    Auth: cookie file detected
    [Use This Node]
```

**Implementation:**
- New `DetectLocalNode(coinID string)` method in `internal/node/`
- Uses `NewQuickClient` (8s timeout) to probe
- Reads `.cookie` file if no credentials configured
- Parses `bitcoin.conf` from default datadir as fallback

### Mode 3: Manual Configuration (current behavior)

Existing Node page — enter host, port, username, password. Kept as-is for power users and remote node setups.

---

## Implementation Plan

### Phase 1: Auto-Detect Local Node (simplest, most useful)
**Scope:** Detect running local nodes, auto-fill RPC settings
**Effort:** Small — ~2 Go functions + 1 UI button

1. Add `DetectLocalNode(coinID)` to `internal/node/`
   - Probe default port with `getblockchaininfo`
   - Try cookie auth from default datadir
   - Parse config file for credentials
   - Return connection details or "not found"
2. Add `DetectNode()` Wails binding
3. Add "Detect Node" button on Node.svelte
   - On click: probe → if found, auto-fill host/port/user/pass → test connection
   - Show result: "Found Bitcoin Core v30.2" or "No node detected"

### Phase 2: Curated Solo Pool List
**Scope:** Connect to solo mining pools without running a node
**Effort:** Medium-large — new stratum proxy architecture

1. Research and verify solo pool endpoints for each coin
2. New `internal/proxy/` package
   - Stratum client: connect to upstream pool, subscribe, receive jobs
   - Job translation: convert upstream stratum jobs to GoVault's internal Job format
   - Share forwarding: forward miner shares upstream
   - Block notification: detect when upstream reports a block found
3. `internal/stratum/server.go` gets a new work source option:
   - Current: `JobManager` pulls from `node.Client.GetBlockTemplate()`
   - New: `ProxySource` pulls from upstream stratum connection
4. Frontend: Pool selector dropdown in Node settings
   - List of endpoints per coin with status indicators
   - Fee display
   - Connection status (connected/disconnected/error)
5. Persist selected pool in config

### Phase 3: Managed Node Download (original plan, optional)
**Scope:** Full node binary management for users who want their own node but don't know how
**Effort:** Large
**Details:** See "Managed Node Architecture" section below

---

## Managed Node Architecture (Phase 3, deferred)

> Kept for reference. Only pursue if Phase 1+2 don't cover enough users.

### Supported Binaries

| Coin | Repo | Windows Binary | Size | Data Dir | RPC Port |
|------|------|---------------|------|----------|----------|
| BTC | `bitcoin/bitcoin` | `bitcoin-{VER}-win64.zip` | ~44 MB | Custom | 8332 |
| BCH | `bitcoin-cash-node/bitcoin-cash-node` | `bitcoin-cash-node-{VER}-win64.zip` | ~28 MB | Custom | 8334 |
| DGB | `DigiByte-Core/digibyte` | Installer only (.exe) | ~30 MB | Custom | 14022 |
| XEC | `Bitcoin-ABC/bitcoin-abc` | `bitcoin-abc-{VER}-win64.zip` | ~55 MB | Custom | 8336 |

Data directory isolation (BTC/BCH/XEC conflict on defaults):
```
%APPDATA%\GoVault\nodes\{coin}\data\
%APPDATA%\GoVault\nodes\{coin}\bin\
```

### New Package

```
internal/nodemanager/
  manager.go       -- Orchestrator (install/start/stop/status)
  download.go      -- HTTP download + SHA256 verification
  extract.go       -- ZIP extraction
  process.go       -- os/exec child process management
  config.go        -- Generate coin-specific .conf files
```

### Key Types

```go
type ManagedNode struct {
    CoinID       string
    Version      string
    BinDir       string
    DataDir      string
    RPCPort      int
    RPCUser      string
    RPCPass      string
    Prune        int             // MB, 0 = full
    State        NodeState       // Downloading/Syncing/Ready/Stopped/Error
    Process      *exec.Cmd
    SyncProgress float64
}

type NodeState int
const (
    NodeNotInstalled NodeState = iota
    NodeDownloading
    NodeExtracting
    NodeSyncing
    NodeReady
    NodeStopped
    NodeError
)
```

### Download & Verification

```
1. Download ZIP from official source
2. Download SHA256SUMS + SHA256SUMS.asc
3. Verify checksum
4. Extract to GoVault nodes directory
5. Generate .conf with random RPC credentials + pruning
6. Start node, monitor sync via getblockchaininfo
```

### Estimated Sync Times (pruned, SSD, dbcache=4096)

| Coin | Time | Disk |
|------|------|------|
| BTC | 6-20 hours | 5-10 GB |
| BCH | 3-8 hours | 5-7 GB |
| DGB | 1-4 hours | 2-5 GB |
| XEC | 3-8 hours | 5-7 GB |

---

## UI Design: Unified Node Settings

The Node page gets a mode selector at the top:

```
┌─────────────────────────────────────────────────┐
│  How do you want to connect?                    │
│                                                 │
│  [Solo Pool]  [Local Node]  [Manual]            │
│   No setup     Auto-detect   Full control       │
└─────────────────────────────────────────────────┘
```

### Solo Pool tab
- Dropdown of curated endpoints for selected coin
- Shows: name, fee %, status indicator
- Payout address field (used as stratum worker name)
- Connect/Disconnect button

### Local Node tab
- [Detect Node] button
- Shows detected node info or "not found"
- Quick-setup: auto-fill credentials from cookie/config
- Falls through to Manual if detection fails

### Manual tab
- Current Node.svelte form (host, port, user, pass, SSL)
- Config generator
- Test + Save & Connect buttons

---

## Stratum Proxy Architecture (for Solo Pool mode)

```
                          ┌──────────────┐
   Miners ──stratum──►    │   GoVault    │  ──stratum──►  Solo Pool
   (Bitaxe, etc.)         │   Server     │               (ckpool etc.)
                          │              │
                          │  translates  │
                          │  jobs & shares│
                          └──────────────┘
```

GoVault's stratum server currently gets work from `node.Client.GetBlockTemplate()`. In proxy mode:

1. **ProxyClient** connects to upstream solo pool via stratum
2. Subscribes (`mining.subscribe`), authorizes (`mining.authorize` with payout address)
3. Receives `mining.notify` jobs from upstream
4. Translates to GoVault's internal `Job` struct
5. GoVault distributes to local miners (vardiff, etc. still works)
6. Miners submit shares to GoVault
7. GoVault forwards qualifying shares upstream
8. If upstream signals block found, GoVault fires the block-found event

Key differences from GBT mode:
- No `getblocktemplate` calls
- No block assembly — upstream pool handles that
- Extranonce management comes from upstream
- GoVault manages its own extranonce2 space within the upstream's allocation

---

## Security Notes

- Solo pool endpoints should be hardcoded, not user-editable (prevent phishing)
- Allow "Custom Pool" option for advanced users with a warning
- Cookie auth for local nodes avoids storing passwords
- RPC always bound to 127.0.0.1

---

## Priority Recommendation

1. **Phase 1 (auto-detect)** — Quick win, solves the "I have a node but setup is confusing" problem
2. **Phase 2 (solo pools)** — Biggest impact, solves the "I don't want to run a node" problem
3. **Phase 3 (managed node)** — Only if there's demand from users who want a node but can't install one themselves
