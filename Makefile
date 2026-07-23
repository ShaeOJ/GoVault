## GoVault + GoVault Edge Node + GoVault Admin build system
##
## Usage:
##   make edge          Build edge node for current platform
##   make edge-all      Cross-compile edge node for all platforms
##   make admin         Build admin panel for current platform
##   make admin-linux   Cross-compile admin panel for all platforms
##   make govault       Build GoVault desktop app (requires Wails)
##   make clean         Remove build artifacts

GOFLAGS   := -ldflags="-s -w"
EDGE_PKG  := ./cmd/edgenode
EDGE_OUT  := build/edgenode
ADMIN_PKG := ./cmd/admin
ADMIN_OUT := build/admin

.PHONY: edge edge-all admin admin-linux govault clean help

## ── Edge Node ────────────────────────────────────────────────────────────────

edge: $(EDGE_OUT)
	go build $(GOFLAGS) -o $(EDGE_OUT)/gostratum-edge$(shell go env GOEXE) $(EDGE_PKG)
	@echo "Built: $(EDGE_OUT)/gostratum-edge$(shell go env GOEXE)"

edge-all: $(EDGE_OUT)
	@echo "==> linux/amd64"
	GOOS=linux   GOARCH=amd64 go build $(GOFLAGS) -o $(EDGE_OUT)/gostratum-edge-linux-amd64   $(EDGE_PKG)
	@echo "==> linux/arm64  (Orange Pi 5, Raspberry Pi 4/5)"
	GOOS=linux   GOARCH=arm64 go build $(GOFLAGS) -o $(EDGE_OUT)/gostratum-edge-linux-arm64   $(EDGE_PKG)
	@echo "==> linux/arm    (Raspberry Pi 2/3, 32-bit)"
	GOOS=linux   GOARCH=arm   go build $(GOFLAGS) -o $(EDGE_OUT)/gostratum-edge-linux-arm     $(EDGE_PKG)
	@echo "==> windows/amd64 (NUC boxes)"
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -o $(EDGE_OUT)/gostratum-edge-windows.exe   $(EDGE_PKG)
	@echo "==> darwin/amd64"
	GOOS=darwin  GOARCH=amd64 go build $(GOFLAGS) -o $(EDGE_OUT)/gostratum-edge-macos-amd64   $(EDGE_PKG)
	@echo "==> darwin/arm64 (Apple Silicon)"
	GOOS=darwin  GOARCH=arm64 go build $(GOFLAGS) -o $(EDGE_OUT)/gostratum-edge-macos-arm64   $(EDGE_PKG)
	@echo
	@ls -lh $(EDGE_OUT)/
	@echo
	@echo "All builds complete."

$(EDGE_OUT):
	mkdir -p $(EDGE_OUT)

## ── Admin Panel ──────────────────────────────────────────────────────────────

admin: $(ADMIN_OUT)
	GOOS=$(shell go env GOOS) GOARCH=$(shell go env GOARCH) go build $(GOFLAGS) -o $(ADMIN_OUT)/gostratum-admin$(shell go env GOEXE) $(ADMIN_PKG)
	@echo "Built: $(ADMIN_OUT)/gostratum-admin$(shell go env GOEXE)"

admin-linux: $(ADMIN_OUT)
	@echo "==> linux/amd64"
	GOOS=linux   GOARCH=amd64 go build $(GOFLAGS) -o $(ADMIN_OUT)/gostratum-admin-linux-amd64   $(ADMIN_PKG)
	@echo "==> linux/arm64"
	GOOS=linux   GOARCH=arm64 go build $(GOFLAGS) -o $(ADMIN_OUT)/gostratum-admin-linux-arm64   $(ADMIN_PKG)
	@echo "==> windows/amd64"
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -o $(ADMIN_OUT)/gostratum-admin-windows.exe   $(ADMIN_PKG)
	@ls -lh $(ADMIN_OUT)/

$(ADMIN_OUT):
	mkdir -p $(ADMIN_OUT)

## ── GoVault Desktop ──────────────────────────────────────────────────────────

govault:
	wails build

## ── Housekeeping ─────────────────────────────────────────────────────────────

clean:
	rm -rf $(EDGE_OUT)
	rm -rf $(ADMIN_OUT)
	rm -rf build/bin

help:
	@echo ""
	@echo "  make edge          Build edge node for this machine"
	@echo "  make edge-all      Build for linux/amd64, arm64, arm, windows, macos"
	@echo "  make admin         Build admin panel for this machine"
	@echo "  make admin-linux   Build admin panel for linux/amd64, arm64, windows"
	@echo "  make govault       Build GoVault desktop (requires Wails)"
	@echo "  make clean         Remove build output"
	@echo ""
	@echo "  Firepool coins:    BC2(3333/3339) BCH(5333/5339) DGB(6333/6339)"
	@echo "                     BTC(4333/4339) BTCS(7333/7339) XEC(9333/9339)"
	@echo ""
