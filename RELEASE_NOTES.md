```
  ┌─────────────────────────────────────────────────────────────┐
  │  ▄▄ TheVault ▄▄   S T R A T U M   T E R M I N A L   //  v1.0.1 │
  │  ── an ASICpool transmission ── 0% custody · 0% bunker ──     │
  └─────────────────────────────────────────────────────────────┘
```

> **INCOMING TRANSMISSION — TheVault v1.0.1**
> A patch release fixing Linux launch failures on current-gen distros. Windows
> and macOS users have nothing new to chase here, but the rebuild keeps the
> version number consistent across all platforms.

---

## 🐧 FIX — Linux launch failure (`libwebkit2gtk-4.0.so.37: cannot open shared object file`)

Wails links against `webkit2gtk-4.0` by default, but current-gen distros
(Ubuntu 24.04, Debian 12+, current Raspberry Pi OS) only ship
`webkit2gtk-4.1` — so the app refused to even start. The Linux ARM64 build is
now compiled with Wails' `webkit2_41` tag against `libwebkit2gtk-4.1`
instead. If you hit this error on v1.0.0, re-download; no action needed
otherwise.

```bash
# Ubuntu 24.04+ / Pi OS Bookworm+
sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0
```

## 🛰️ FIX — proxy now identifies itself as TheVault, not GoVault

`mining.subscribe` sent `GoVault/<version>` as the client signature even
after the rebrand. Now correctly sends `TheVault/<version>`.

*(Everything from the TheVault v1.0.0 and prior GoVault releases still
applies — dual-pool hashrate splitting, the shares/sec chart line, ASICpool
pool presets, the CashAddr payout fix, and the v1.4.1 ZMQ tidy-ups.)*

---

## 💾 GET IT

### Windows (full desktop GUI)
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

### Headless (edge node / relay — Windows, Linux, macOS)
Prefer a screenless box, or no native GUI build for your platform yet? The
`govault-headless-*` binaries run the same mining core with the same
dashboard over HTTP — pure Go, every platform, no GUI libs required.

---

**Requirements:** TheVault talks to a Bitcoin Core (or compatible) node over RPC in solo mode. Make sure your node is up and RPC is enabled before you start. For ZMQ instant-block mode, add `-zmqpubhashblock` to your node too. Proxy mode just needs an upstream pool URL — no node required.

```
  // end transmission ── mine like it's yours, because it is ──
```
