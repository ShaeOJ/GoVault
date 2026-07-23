#!/usr/bin/env bash
# GoVault Edge Node — Linux install script
# Usage: sudo bash install.sh [binary-path]
# Default binary: ./gostratum-edge-linux-amd64  (or arm64 on ARM hardware)

set -e

INSTALL_DIR="/opt/gostratum-edge"
SERVICE_NAME="gostratum-edge"
SERVICE_USER="gostratum"
BINARY="${1:-}"

# ── Detect binary ─────────────────────────────────────────────────────────────
if [ -z "$BINARY" ]; then
  ARCH=$(uname -m)
  if [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
    BINARY="./gostratum-edge-linux-arm64"
  else
    BINARY="./gostratum-edge-linux-amd64"
  fi
fi

if [ ! -f "$BINARY" ]; then
  echo "ERROR: binary not found: $BINARY"
  echo "       Build it first:  make edge-all"
  echo "       Or pass path:    sudo bash install.sh /path/to/gostratum-edge"
  exit 1
fi

echo "==> Installing GoVault Edge Node"
echo "    binary : $BINARY"
echo "    install: $INSTALL_DIR"
echo "    user   : $SERVICE_USER"
echo

# ── Create service user ───────────────────────────────────────────────────────
if ! id "$SERVICE_USER" &>/dev/null; then
  echo "==> Creating service user: $SERVICE_USER"
  useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
fi

# ── Create install directory ──────────────────────────────────────────────────
echo "==> Creating $INSTALL_DIR"
mkdir -p "$INSTALL_DIR/data"
cp "$BINARY" "$INSTALL_DIR/gostratum-edge"
chmod +x "$INSTALL_DIR/gostratum-edge"
chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR"

# ── First-run setup ───────────────────────────────────────────────────────────
if [ ! -f "$INSTALL_DIR/data/config.json" ]; then
  echo
  echo "==> No config found. Running setup wizard..."
  echo
  cd "$INSTALL_DIR"
  sudo -u "$SERVICE_USER" ./gostratum-edge --init
  cd - >/dev/null
fi

# ── Install systemd service ───────────────────────────────────────────────────
echo
echo "==> Installing systemd service: $SERVICE_NAME"
cp "$(dirname "$0")/gostratum-edge.service" "/etc/systemd/system/$SERVICE_NAME.service"
systemctl daemon-reload
systemctl enable "$SERVICE_NAME"

echo
echo "==> Starting service"
systemctl start "$SERVICE_NAME"
systemctl status "$SERVICE_NAME" --no-pager

echo
echo "==> Done!"
echo
echo "    Useful commands:"
echo "      systemctl status  $SERVICE_NAME"
echo "      systemctl restart $SERVICE_NAME"
echo "      journalctl -u $SERVICE_NAME -f"
echo
echo "    Dashboard: http://$(hostname -I | awk '{print $1}'):8080"
echo "    Stratum:   $(hostname -I | awk '{print $1}'):3333  (after port forward)"
echo
