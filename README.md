# GoVault

**Your Private Mining Vault**

A personal solo mining stratum server, built for the home miner who refuses to hand over their hashrate to some pool operator in a bunker somewhere. GoVault runs on your desktop, connects to your own Bitcoin node, and puts you in full control of your mining operation.

No middlemen. No pool fees. Just you, your hardware, and the blockchain.

---

## About

GoVault is a **Stratum V1 solo mining server** packaged as a native desktop application. It's designed from the ground up for home mining hardware — the Bitaxes, NerdMiners, and other small-scale devices that make solo mining a hobby worth pursuing.

Connect your miners, point them at GoVault, and mine directly against your own full node. Every hash goes toward finding *your* block.

Built with Go on the backend and Svelte on the frontend, delivered as a single executable via [Wails](https://wails.io).

## Features

- **Solo mining stratum server** — Full Stratum V1 implementation, no pool required
- **Multi-coin support** — BTC, BCH, DGB, BC2, XEC
- **Built for home mining hardware** — Bitaxe, NerdAxe, NerdMiner, BitDSK, Avalon Q
- **Real-time dashboard** — Live hashrate charts, share counters, and network stats
- **Auto-discovery** — Finds miners on your local network automatically
- **Variable difficulty** — Tuned for home miners, from NerdMiner (~0.001 diff) to Avalon Q
- **6 UI themes** — Nuclear, TRON, Vault-Tec, Crimson, Ultraviolet, Plasma
- **SQLite persistence** — Stats, shares, and history survive restarts
- **Desktop app** — Native Windows, macOS, and Linux via Wails

## Screenshots

*Coming soon — Dashboard, Miners, and Settings views.*

<!--
![Dashboard](screenshots/dashboard.png)
![Miners](screenshots/miners.png)
![Settings](screenshots/settings.png)
-->

## Quick Start

### Prerequisites

- **Go** 1.21+
- **Node.js** 18+
- **Wails CLI** v2 — install with `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- A **Bitcoin full node** (or other supported coin) with RPC enabled

### Build

```bash
# Clone the repo
git clone https://github.com/yourusername/GoVault.git
cd GoVault

# Build the desktop app
wails build

# Run it
./build/bin/GoVault
```

### Development

```bash
# Live development with hot reload
wails dev
```

## Configuration

On first launch, GoVault creates a config file at:
- **Windows:** `%APPDATA%\GoVault\config.json`
- **macOS:** `~/Library/Application Support/GoVault/config.json`
- **Linux:** `~/.config/GoVault/config.json`

### Key settings

| Setting | Default | Description |
|---------|---------|-------------|
| Stratum Port | `10333` | Port your miners connect to |
| Payout Address | — | Your wallet address for the coinbase transaction |
| Coinbase Tag | — | Custom text embedded in blocks you find |
| Coin | `btc` | Which network to mine (btc, bch, dgb, bc2, xec) |

### Node RPC

GoVault needs a connection to a full node's RPC interface. Configure the host, port, username, and password in the Settings page. The app provides a generated config snippet for your node software.

## Supported Hardware

| Device | Type | Typical Hashrate | Status |
|--------|------|-------------------|--------|
| Bitaxe | ASIC | ~500 GH/s - 1.2 TH/s | Fully supported |
| NerdAxe | ASIC | ~500 GH/s | Fully supported |
| NerdMiner | ESP32 | ~50 KH/s | Fully supported |
| BitDSK | ASIC | ~1 TH/s | Fully supported |
| Avalon Q | ASIC | ~90 TH/s | Fully supported |

The variable difficulty system handles the full range — from NerdMiner's ~0.001 difficulty up to Avalon Q's high hashrate.

## Tech Stack

| Component | Technology |
|-----------|------------|
| Backend | Go |
| Frontend | Svelte 4 |
| Desktop Framework | Wails v2 |
| Styling | TailwindCSS |
| Charts | Chart.js |
| Database | SQLite (via modernc.org/sqlite, pure Go) |
| Fonts | Share Tech Mono, JetBrains Mono |

## License

GoVault is licensed under the [GNU General Public License v3.0](LICENSE).

You are free to use, modify, and distribute this software under the terms of the GPL v3. See the `LICENSE` file for the full text.
