# Telegram Proxy

[![Release](https://img.shields.io/github/v/release/y0sy4/telegram-proxy)](https://github.com/y0sy4/telegram-proxy/releases)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/y0sy4/telegram-proxy)](go.mod)

**Local SOCKS5 proxy for Telegram Desktop written in Go.** Speeds up Telegram by routing traffic through direct WebSocket connections to Telegram servers. Works as a local bypass for regions where Telegram is blocked or slow.

---

## 📥 Download (v2.0.7)

### Full Version (~6.4 MB)

| Platform | File |
|----------|------|
| **Windows** (amd64) | [⬇️ TgWsProxy_windows_amd64.exe](https://github.com/y0sy4/telegram-proxy/releases/download/v2.0.7/TgWsProxy_windows_amd64.exe) |
| **Linux** (amd64) | [⬇️ TgWsProxy_linux_amd64](https://github.com/y0sy4/telegram-proxy/releases/download/v2.0.7/TgWsProxy_linux_amd64) |
| **macOS** (Intel) | [⬇️ TgWsProxy_darwin_amd64](https://github.com/y0sy4/telegram-proxy/releases/download/v2.0.7/TgWsProxy_darwin_amd64) |
| **macOS** (Apple Silicon) | [⬇️ TgWsProxy_darwin_arm64](https://github.com/y0sy4/telegram-proxy/releases/download/v2.0.7/TgWsProxy_darwin_arm64) |

### Lite Version (~5 MB) — for routers and servers

| Platform | File |
|----------|------|
| **Windows** (amd64) | [⬇️ TgWsProxy_lite_windows_amd64.exe](https://github.com/y0sy4/telegram-proxy/releases/download/v2.0.7/TgWsProxy_lite_windows_amd64.exe) |
| **Linux** (amd64) | [⬇️ TgWsProxy_lite_linux_amd64](https://github.com/y0sy4/telegram-proxy/releases/download/v2.0.7/TgWsProxy_lite_linux_amd64) |
| **Linux** (ARM64) | [⬇️ TgWsProxy_lite_linux_arm64](https://github.com/y0sy4/telegram-proxy/releases/download/v2.0.7/TgWsProxy_lite_linux_arm64) |
| **macOS** (Intel) | [⬇️ TgWsProxy_lite_darwin_amd64](https://github.com/y0sy4/telegram-proxy/releases/download/v2.0.7/TgWsProxy_lite_darwin_amd64) |
| **macOS** (Apple Silicon) | [⬇️ TgWsProxy_lite_darwin_arm64](https://github.com/y0sy4/telegram-proxy/releases/download/v2.0.7/TgWsProxy_lite_darwin_arm64) |

### Which version to choose?

| Version | Size | Features | For whom |
|---------|------|---------|----------|
| **Full** | ~6.4 MB | SOCKS5, HTTP proxy, upstream proxy, configs, auto-launch Telegram, `--test-dc`, `--test-dc-media` | Regular users |
| **Lite** | ~5 MB | SOCKS5 proxy only, `--test-dc` | Routers (OpenWRT), servers, minimalists |

---

## 🚀 Quick Start

### Windows
1. Download [TgWsProxy_windows_amd64.exe](https://github.com/y0sy4/telegram-proxy/releases/download/v2.0.7/TgWsProxy_windows_amd64.exe)
2. Double-click to run
3. Telegram will open proxy settings → click "Enable"

### Linux/macOS
```bash
chmod +x TgWsProxy_linux_amd64
./TgWsProxy_linux_amd64
```

**That's it!** Telegram works through the proxy.

---

## ⚙️ Options

```bash
TgWsProxy [flags]
```

| Flag | Description | Default |
|------|-----------|---------|
| `--port` | SOCKS5 port | 1080 |
| `--host` | Listen host | 127.0.0.1 |
| `--dc-ip` | DC:IP (comma-separated), `DCm:IP` for media | auto |
| `--auth` | Login:password for proxy | — |
| `--http-port` | HTTP proxy (for browsers) | 0 (disabled) |
| `--upstream-proxy` | Chain through another proxy | — |
| `-v` | Verbose logging | false |
| `--test-dc` | Test DC connectivity and exit | — |
| `--test-dc-media` | Test DC + media/CDN | — |
| `--auto-update` | Auto-update | false (security) |

### Examples

**Just run:**
```bash
TgWsProxy
```

**HTTP proxy for browsers:**
```bash
TgWsProxy --http-port 8080
```

**Through Tor:**
```bash
TgWsProxy --upstream-proxy "socks5://127.0.0.1:9050"
```

**With password:**
```bash
TgWsProxy --auth "user:pass"
```

**Media-specific DC (different IPs for text and media):**
```bash
TgWsProxy --dc-ip "2:149.154.167.220,2m:149.154.167.222,4:149.154.167.91,4m:149.154.167.118"
```

**DC Diagnostics:**
```bash
# Test text DC
TgWsProxy --test-dc

# Test text + media DC
TgWsProxy --test-dc-media
```

---

## 🔧 What's new in v2.0.7

| Category | Changes |
|----------|---------|
| 🌐 **Media CDN** | +13 CDN/media IPs, pluto/venus/kws-2 domains, media-specific `--dc-ip` |
| 🔧 **Pool** | WS returned to pool, blacklist TTL 10 min, IsClosed() check |
| ⚡ **Performance** | sync.Pool buffers (-80% GC), PatchInitDC in-place |
| 💓 **Stability** | Heartbeat ping/pong, rate limiting, graceful shutdown |
| 🧪 **Tests** | +16 unit tests (pool, config, websocket) |
| 🐛 **Fixes** | Close frame panic fix, module path → `github.com/y0sy4/...` |
| 📦 **Service** | systemd unit file `tg-ws-proxy.service` |

[📋 All changes](https://github.com/y0sy4/telegram-proxy/compare/v2.0.6...v2.0.7)

---

## 📊 Why Go?

| | Python | Go |
|--|--------|-----|
| Size | ~50 MB | **~6 MB** |
| Dependencies | pip | **stdlib** |
| Startup | ~500 ms | **~50 ms** |
| Memory | ~50 MB | **~10 MB** |

---

## 🗂️ Structure

```
telegram-proxy/
├── cmd/proxy/          # Full CLI
├── cmd/lite/           # Lite CLI (minimal)
├── internal/
│   ├── proxy/          # Proxy core
│   ├── socks5/         # SOCKS5 server
│   ├── websocket/      # WebSocket client
│   ├── mtproto/        # MTProto parsing
│   ├── pool/           # WebSocket pooling
│   ├── config/         # Configuration
│   └── telegram/       # Telegram auto-config
├── mobile/             # Android/iOS bindings
├── tg-ws-proxy.service # systemd unit
├── go.mod
└── Makefile
```

---

## 🛠️ Build

```bash
# All platforms
make all

# Or manually
go build -o TgWsProxy.exe ./cmd/proxy                     # Windows
GOOS=linux GOARCH=amd64 go build -o TgWsProxy_linux ./cmd/proxy
GOOS=darwin GOARCH=amd64 go build -o TgWsProxy_macos ./cmd/proxy
GOOS=darwin GOARCH=arm64 go build -o TgWsProxy_macos_arm64 ./cmd/proxy
```

---

## 📡 OpenWRT / Routers

### Quick start

```bash
# Cross-compile (on PC)
GOOS=linux GOARCH=arm64 go build -o tg-ws-proxy-go ./cmd/lite/

# Copy to router
scp tg-ws-proxy-go root@192.168.1.1:/usr/bin/
chmod +x /usr/bin/tg-ws-proxy-go

# Run
/usr/bin/tg-ws-proxy-go --host 0.0.0.0 --port 1080
```

### Media not loading?

```bash
# Test media DC
/usr/bin/tg-ws-proxy-go --test-dc-media

# Run with specific DC
/usr/bin/tg-ws-proxy-go --dc-ip "2:149.154.167.220,4:149.154.167.220" --host 0.0.0.0 --port 1080
```

### systemd (Linux server)

```bash
sudo cp tg-ws-proxy.service /etc/systemd/system/
sudo systemctl enable tg-ws-proxy
sudo systemctl start tg-ws-proxy
```

---

## 🔍 Troubleshooting

| Problem | Solution |
|---------|----------|
| Proxy not connecting | Check it's running, Telegram set to `127.0.0.1:1080` |
| Text loads, media doesn't | Use `--test-dc-media`, then `--dc-ip` with working IPs |
| Telegram won't open | Manually: `tg://socks?server=127.0.0.1&port=1080` |
| Antivirus blocks it | False positive. Code is open source, add to exceptions |
| Logs | `%APPDATA%\TgWsProxy\proxy.log` (Win), `~/.config/TgWsProxy/proxy.log` (Linux) |

---

## 🤝 Contributing

1. Fork → branch → PR
2. `go test ./...`
3. `gofmt -w .`
4. No drama. Just facts.

---

## 📄 License

MIT License

---

**v2.0.7** | Built with ❤️ using Go 1.21
