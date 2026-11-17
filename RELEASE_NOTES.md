```
  ┌─────────────────────────────────────────────────────────────┐
  │  ▄▄ TheVault ▄▄   S T R A T U M   T E R M I N A L   //  v1.0.0-beta │
  │  ── an ASICpool transmission ── 0% custody · 0% bunker ──     │
  └─────────────────────────────────────────────────────────────┘
```

> **INCOMING TRANSMISSION — TheVault Beta v1.0.0**
> GoVault is now **TheVault**. Same project, same code, same GPLv3 license — new
> name, new frameless UI, and the version counter resets to mark the rebrand.
> If you're coming from a v1.x GoVault build: this isn't a downgrade, it's a
> fresh start under the new name.

---

## 🏷️ REBRANDED — GoVault → TheVault

The app now shows **TheVault** everywhere a user sees it: window title, title
bar, system tray, installer, and the default coinbase tag (`/TheVault/`). The
repo, Go module, and built executable keep the `GoVault`/`govault` name for
continuity — same source, same install path, just a new label on the tin.

Official download page: **[vault.undergroundroots.ca](https://vault.undergroundroots.ca)**

## 🪟 NEW — custom frameless title bar

Replaced the native Windows title bar with a themed one: minimize/maximize/
close buttons that recolor with your active theme's accent, a draggable bar,
and double-click-to-maximize. No more mismatched OS chrome around a themed app.

## 🛰️ NEW — dual-pool hashrate splitting (proxy mode)

Split your fleet's hashrate between two upstream pools by percentage, with
**zero-reconnect pool switching** — TheVault swaps which pool's job is active
job-by-job so your miners never drop. Dashboard and Miners pages show each
pool's own diff, shares, and network difficulty live during a split, with
automatic failover if one pool goes unhealthy.

## 📊 NEW — second line on the hashrate chart

"Hashrate Over Time" now plots **shares/sec** alongside hashrate (dashed,
right-side axis) — a difficulty-independent read on how active your fleet
actually is.

## 🎨 NEW — ASICpool pool presets with live logos

The pool-preset list is back to just the ASICpool endpoints (BTC, BTC
low-diff, BCH, BTCS, DGB, BSV), now shown as a compact grid with each coin's
actual logo pulled live from asicpool.space.

## ⚡ FIX — idle CPU cut roughly 3x

The miner-fleet globe (always rendering in the sidebar, not just on the
Dashboard) was redrawing unthrottled at full display refresh rate. Capped to
30fps and paused entirely while the window is hidden/minimized — idle CPU use
dropped from ~53% to ~19% of a core in testing, with a meaningful drop in
total WebView2 memory too.

## 🧮 TWEAK — pool-ping smoothing widened

The "Pool Ping" stat now smooths over roughly the last 10 shares (was ~5),
for a steadier read under bursty share timing.

*(Everything from prior GoVault releases still applies — including the
critical **CashAddr (BCH/XEC/BCH2) payout fix** and the v1.4.1 ZMQ tidy-ups.
If you're new here, this build has it all.)*

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
