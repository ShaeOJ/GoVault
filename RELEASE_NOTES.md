```
  ┌─────────────────────────────────────────────────────────────┐
  │  ▄▄ GoVault ▄▄   S T R A T U M   T E R M I N A L   //  v1.4.0 │
  │  ── an ASICpool transmission ── 0% custody · 0% bunker ──     │
  └─────────────────────────────────────────────────────────────┘
```

> **INCOMING TRANSMISSION — GoVault v1.4.0**
> A correctness release. If you solo-mine **Bitcoin Cash (BCH)**, **eCash (XEC)**,
> or **Bitcoin Cash II (BCH2)** with a `bitcoincash:` / `ecash:` (CashAddr) payout
> address, **update before you mine another block.**

---

## 🛠️ CRITICAL FIX — CashAddr payout addresses were mis-decoded (BCH / XEC / BCH2)

GoVault's CashAddr decoder read the address version byte from the wrong bit
boundary, which **shifted the decoded hash by 3 bits** — so a `bitcoincash:` /
`ecash:` payout address was turned into a **different, valid-looking address**
when the coinbase was built. The address still *passed* validation (its checksum
was fine), so nothing looked wrong up front — but a block found while solo-mining
BCH/XEC/BCH2 to a CashAddr would have paid the **wrong destination**.

- **Who's affected:** solo mode, coin = BCH / XEC / BCH2, payout entered as a
  CashAddr (`bitcoincash:…`, `ecash:…`, `bitcoincashii:…`, with or without the
  prefix). **Legacy `1…`/`3…` addresses were never affected.**
- **BTC, DGB, LTC, BC2 were never affected** — their base58 / bech32 decoding was
  always correct. This bug was CashAddr-only.
- **The fix:** the version byte + hash are now decoded together from the full
  payload, so CashAddr → scriptPubKey is exact. Verified against canonical BCH
  vectors and an eCash round-trip; regression tests added so it can't drift again.

**What to do:** update to v1.4.0, then re-check **Settings → Payout Address** shows
the address you expect. If you'd been solo-mining BCH/XEC to a CashAddr, switch to
this build before continuing.

---

## 🛰️ ALSO IN THIS BUILD — payout-address mismatch warning

Rigs moved over from public-pool / solo.ckpool often send their **wallet address
as the Stratum username** — but in GoVault **solo mode the coinbase pays the single
configured Payout Address**, and the username is just a worker label. To stop that
mismatch from being silent, GoVault now logs a clear warning at authorize time when
a miner connects with a username that's a valid address different from your
configured payout — so "why isn't my address in the coinbase?" answers itself.

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
