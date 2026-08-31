```
  ┌─────────────────────────────────────────────────────────────┐
  │  ▄▄ GoVault ▄▄   S T R A T U M   T E R M I N A L   //  v1.3.0 │
  │  ── an ASICpool transmission ── 0% custody · 0% bunker ──     │
  └─────────────────────────────────────────────────────────────┘
```

> **INCOMING TRANSMISSION — GoVault v1.3.0**
> Point your machinery at your own node. Keep the block. Keep the reward.
> This build tightens the loop between your node and your miners to milliseconds.

---

## ⚡ FEATURE SPOTLIGHT — ZMQ Instant Block Detection

GoVault no longer waits around asking your node "any new blocks yet?" on a timer. In **solo mode** it can now plug straight into your node's **ZMQ `hashblock`** broadcast and react the *instant* a new block lands on the network.

- **Instant new-work broadcast.** The moment your node hears a new block, GoVault pushes fresh work to every connected miner — cutting the window where your rigs waste hashes on a dead tip (fewer stale shares, less orphaned effort).
- **Pure-Go ZMQ.** No `libzmq`, no cgo, no extra install — it's baked into the single portable binary. Nothing new to deploy.
- **Self-healing.** ZMQ is the fast path; an RPC poll runs underneath as a heartbeat. If the ZMQ link ever hiccups, GoVault keeps mining on the poll fallback and reconnects automatically.
- **Tunable.** `Fallback Poll` interval defaults to 30s and can be dropped for fast-block coins or remote nodes so an outage can never strand you on a stale tip for long.

**Switch it on:** `Node → ZMQ Block Endpoint` → e.g. `tcp://127.0.0.1:28332`
**Node side:** run your daemon with `-zmqpubhashblock=tcp://127.0.0.1:28332`
Leave the field blank to stay in classic poll-only mode — no node changes required.

---

## 🛰️ ALSO IN THIS BUILD

- **FIXED — the app now launches, everywhere.** Squashed a WebView2 startup crash (`"We couldn't create the data directory"`) that could leave the window refusing to open. GoVault now keeps its WebView2 data **local to the executable**, so it's fully portable and never trips over a stale `%APPDATA%` path again.
- **NEW — ASICpool is built into the proxy.** The Upstream Pool presets now lead with **ASICpool** endpoints for **BTC**, **BTC (low-diff)**, **BCH**, and **DGB** — one click to relay your fleet to the 0% Canadian solo pool. CKPool and Public Pool remain as alternatives.
- **NEW — Windows installer.** Prefer Start-Menu shortcuts over a loose `.exe`? Grab `GoVault-amd64-installer.exe` below. The standalone `GoVault-windows-amd64.exe` is still here for portable / no-install use.

---

## 📥 GET IT RUNNING

### Windows
- **Installer (recommended):** download **`GoVault-amd64-installer.exe`**, run it, launch from the Start Menu.
- **Portable:** download **`GoVault-windows-amd64.exe`** and double-click — no install.
- Windows 10/11 ships WebView2. If you hit a WebView2 error, grab the runtime from [Microsoft](https://developer.microsoft.com/en-us/microsoft-edge/webview2/).

### macOS
- Apple Silicon: **`GoVault-macos-arm64.zip`** · Intel: **`GoVault-macos-amd64.zip`**
- Unzip, then **right-click `GoVault.app` → Open** (bypasses Gatekeeper; the app is unsigned). Opens normally after the first launch.

### Linux
- x64: **`GoVault-linux-amd64`** · ARM64 / Raspberry Pi: **`GoVault-linux-arm64`**
```bash
# Ubuntu 24.04+ / Pi OS Bookworm+
sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0   # (…-4.0-37 on 22.04 / older)
chmod +x GoVault-linux-* && ./GoVault-linux-*
```

### Headless (edge node / relay)
Prefer a screenless box? The `govault-headless-*` binaries run the same core with an HTTP + SSE dashboard — pure Go, every platform, no GUI libs required.

---

**Requirements:** GoVault talks to a Bitcoin Core (or compatible) node over RPC. Make sure your node is up and RPC is enabled before you start. For ZMQ instant-block mode, add `-zmqpubhashblock` to your node too.

```
  // end transmission ── mine like it's yours, because it is ──
```
