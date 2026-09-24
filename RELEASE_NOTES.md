```
  ┌─────────────────────────────────────────────────────────────┐
  │  ▄▄ GoVault ▄▄   S T R A T U M   T E R M I N A L   //  v1.4.1 │
  │  ── an ASICpool transmission ── 0% custody · 0% bunker ──     │
  └─────────────────────────────────────────────────────────────┘
```

> **INCOMING TRANSMISSION — GoVault v1.4.1**
> A small, sharp follow-up to v1.4.0: tidier block detection and a knob for the
> ZMQ fallback poll.

---

## 🎛️ NEW — set your ZMQ fallback poll interval

With **ZMQ** enabled, GoVault hears new blocks *instantly*; the RPC poll is only a
safety net for the rare case a ZMQ message is missed. You can now set how often
that fallback runs, right in **Setup → ZMQ Block Endpoint → Fallback Poll Interval
(seconds)**. Leave it at **30s**, or dial it up to **60s+** for a lighter touch on
your node — ZMQ still catches blocks the instant they land either way.

## 🧹 FIX — no more double work-restart on a new block

When ZMQ and the fallback poll noticed the same block at almost the same moment,
they could each broadcast a fresh job for it — a harmless but wasteful extra
work-restart. Block detection now records the new tip **atomically**, so whichever
path sees it first wins and the other stays quiet. One block, one clean job.

*(Everything from v1.4.0 still applies — including the critical **CashAddr
(BCH/XEC/BCH2) payout fix**. If you skipped it, grab this build.)*

---

## 💾 GET IT

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
