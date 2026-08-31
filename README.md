```
 ██████╗  ██████╗ ██╗   ██╗ █████╗ ██╗   ██╗██╗  ████████╗
██╔════╝ ██╔═══██╗██║   ██║██╔══██╗██║   ██║██║  ╚══██╔══╝
██║  ███╗██║   ██║██║   ██║███████║██║   ██║██║     ██║
██║   ██║██║   ██║╚██╗ ██╔╝██╔══██║██║   ██║██║     ██║
╚██████╔╝╚██████╔╝ ╚████╔╝ ██║  ██║╚██████╔╝███████╗██║
 ╚═════╝  ╚═════╝   ╚═══╝  ╚═╝  ╚═╝ ╚═════╝ ╚══════╝╚═╝
      S T R A T U M   T E R M I N A L  ·  by ASICpool
```

# GoVault — Your Private Mining Vault

> **Point your machinery at your own node. Keep the block. Keep the reward.**
> A solo-mining stratum terminal for the home operator who refuses to hand their hashrate to some pool operator in a bunker somewhere.

No middlemen. No custody. No pool fees. Just you, your hardware, and the chain — wrapped in a native desktop app you can double-click.

---

## ▚ WHAT IT IS

GoVault is a **Stratum V1 mining server** packaged as a single-file native desktop app (Go core + Svelte UI, built with [Wails](https://wails.io)). It runs two ways:

- **SOLO** — plug into your own full node and build every block yourself. Every hash goes toward finding *your* block.
- **RELAY (proxy)** — no node? Forward your whole fleet to an upstream solo pool (**ASICpool** presets built in) over one clean connection, with optional per-miner pass-through.

Built for the small stuff — Bitaxes, NerdMiners, NerdQAxe boards, and the rest of the home-lab wasteland.

## ▚ FEATURE MATRIX

- **⚡ ZMQ instant block detection** — subscribe to your node's `hashblock` broadcast and push fresh work to miners the *millisecond* a block lands. Pure-Go (no libzmq), with an automatic RPC poll fallback if the link drops. Fewer stale shares, less wasted effort.
- **🖥 ASIC device recognition** — miners are auto-identified from their user-agent and shown as `4× BM1370`, `Bitaxe Gamma`, `NerdQAxe++`, and friends — chip count × model, right in the fleet view.
- **🛰 Solo *or* relay** — run against your own node, or relay to an upstream pool. ASICpool BTC / BCH / DGB presets one click away.
- **📡 Auto-discovery** — finds AxeOS miners on your LAN automatically, with power/thermal telemetry.
- **🎚 Variable difficulty** — tuned for the full home range, from NerdMiner (~0.001) to big ASICs.
- **📊 Live dashboard** — real-time hashrate charts, best-share tracking, share counters, network stats.
- **🎨 6 cathode themes** — Nuclear, TRON, Vault-Tec, Crimson, Ultraviolet, Plasma.
- **💾 Portable by design** — config, SQLite DB, logs, and WebView2 data all live in a `data/` folder *next to the exe*. Copy the folder, take your vault with you.
- **🔄 In-app updater** — checks GitHub releases and updates itself in place.
- **🪟🍎🐧 Native everywhere** — Windows, macOS, Linux (incl. ARM64 / Raspberry Pi), plus a headless edge-node build.

## ▚ INSTALL

Grab the latest from **[Releases](https://github.com/ShaeOJ/GoVault/releases/latest)**:

- **Windows** — `GoVault-amd64-installer.exe` (installer) or `GoVault-windows-amd64.exe` (portable)
- **macOS** — `GoVault-macos-arm64.zip` (Apple Silicon) / `GoVault-macos-amd64.zip` (Intel)
- **Linux** — `GoVault-linux-amd64` / `GoVault-linux-arm64`
- **Headless** — `govault-headless-*` (HTTP + SSE dashboard, no GUI libs)

## ▚ BUILD FROM SOURCE

**Prerequisites:** Go 1.21+, Node.js 18+, Wails CLI v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@v2.11.0`), and a full node with RPC enabled.

```bash
git clone https://github.com/ShaeOJ/GoVault.git
cd GoVault

wails build            # desktop app -> build/bin/GoVault
wails build -nsis      # + Windows installer -> build/bin/GoVault-amd64-installer.exe
wails dev              # live dev with hot reload
```

## ▚ CONFIGURATION

On first launch GoVault writes its config, database, and logs to a **`data/` folder beside the executable** (portable). Override with `GOVAULT_CONFIG_FILE` / `GOVAULT_DATA_DIR` for appliance/headless setups.

| Setting | Default | Description |
|---------|---------|-------------|
| Stratum Port | `10333` | Port your miners connect to |
| Mining Mode | `solo` | `solo` (own node) or `proxy` (upstream pool) |
| Payout Address | — | Your wallet address for the coinbase |
| Coinbase Tag | — | Custom text embedded in blocks you find |
| ZMQ Block Endpoint | — | e.g. `tcp://127.0.0.1:28332` — enables instant block detection |
| Fallback Poll | `30s` | RPC heartbeat while ZMQ is primary |

**For ZMQ mode**, run your node with `-zmqpubhashblock=tcp://127.0.0.1:28332`. Leave the endpoint blank to stay in classic poll-only mode.

## ▚ SUPPORTED HARDWARE

| Device | Type | Typical Hashrate |
|--------|------|-------------------|
| Bitaxe (Ultra / Supra / Gamma) | ASIC | ~500 GH/s – 1.2 TH/s |
| NerdAxe / NerdQAxe / NerdQAxe++ | ASIC | ~500 GH/s – 2 TH/s |
| NerdMiner | ESP32 | ~50 KH/s |
| BitDSK | ASIC | ~1 TH/s |
| Avalon Q | ASIC | ~90 TH/s |

Variable difficulty covers the whole span — from NerdMiner's ~0.001 up to big-ASIC hashrate.

## ▚ TECH STACK

Go · Svelte 4 · Wails v2 · TailwindCSS · Chart.js · SQLite (pure-Go `modernc.org/sqlite`) · Share Tech Mono / JetBrains Mono

## ▚ LICENSE

GoVault is licensed under the [GNU General Public License v3.0](LICENSE) — free to use, modify, and distribute under the terms of the GPL v3.

```
  // mine like it's yours, because it is //
```
