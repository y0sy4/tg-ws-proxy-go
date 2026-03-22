# TG WS Proxy Go

[![Go Version](https://img.shields.io/github/go-mod/go-version/y0sy4/tg-ws-proxy-go?label=Go)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/y0sy4/tg-ws-proxy-go)](https://github.com/y0sy4/tg-ws-proxy-go/releases)

> **Go rewrite** of [Flowseal/tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy)

**Local SOCKS5 proxy for Telegram Desktop written in Go**

Speeds up Telegram by routing traffic through direct WebSocket connections to Telegram servers.

---

## 🚀 Quick Start

### Installation

```bash
# Download binary from Releases
# Or build from source
go build -o TgWsProxy.exe ./cmd/proxy
```

### Run

```bash
# Windows
start run.bat

# Linux/macOS
./TgWsProxy

# With options
./TgWsProxy --port 9050 --dc-ip 2:149.154.167.220
```

### Configure Telegram Desktop

1. **Settings** → **Advanced** → **Connection Type** → **Proxy**
2. Add proxy:
   - **Type:** SOCKS5
   - **Server:** `127.0.0.1`
   - **Port:** `1080`
   - **Login/Password:** empty (or your credentials if using `--auth`)

Or open link: `tg://socks?server=127.0.0.1&port=1080`

---

## 🔧 Command Line

```bash
./TgWsProxy [options]

Options:
  --port int        SOCKS5 port (default 1080)
  --host string     SOCKS5 host (default "127.0.0.1")
  --dc-ip string    DC:IP comma-separated (default "1:149.154.175.50,2:149.154.167.220,3:149.154.175.100,4:149.154.167.220,5:91.108.56.100")
  --auth string     SOCKS5 authentication (username:password)
  -v                Verbose logging
  --log-file string Log file path
  --log-max-mb float Max log size in MB (default 5)
  --buf-kb int      Buffer size in KB (default 256)
  --pool-size int   WS pool size (default 4)
  --version         Show version
```

### Examples

```bash
# Without authentication
./TgWsProxy -v

# With authentication (protect from unauthorized access)
./TgWsProxy --auth "myuser:mypassword"

# Custom DC configuration
./TgWsProxy --dc-ip "2:149.154.167.220,4:149.154.167.220"
```

---

## 📦 Supported Platforms

| Platform | Architectures | Status |
|----------|---------------|--------|
| Windows | x86_64 | ✅ Ready |
| Linux | x86_64 | ✅ Ready |
| macOS | Intel + Apple Silicon | ✅ Ready |
| Android | arm64, arm, x86_64 | 📝 See [android/README.md](android/README.md) |
| iOS | arm64 | 🚧 Planned |

**macOS Catalina (10.15)** — supported! Use `TgWsProxy_macos_amd64`.

---

## ✨ Features

- ✅ **WebSocket pooling** — connection pool for low latency
- ✅ **TCP fallback** — automatic switch when WS unavailable
- ✅ **MTProto parsing** — DC ID extraction from init packet
- ✅ **SOCKS5** — full RFC 1928 support
- ✅ **Logging** — with file rotation
- ✅ **Zero-copy** — optimized memory operations
- ✅ **IPv6 support** — via NAT64 and IPv4-mapped addresses
- ✅ **Authentication** — SOCKS5 username/password

---

## 📊 Performance

| Metric | Value |
|--------|-------|
| Binary size | ~6 MB |
| Memory usage | ~10 MB |
| Startup time | <100 ms |
| Latency (pool hit) | <1 ms |

### Comparison: Python vs Go

| Metric | Python | Go |
|--------|--------|-----|
| Size | ~50 MB | **~6 MB** |
| Dependencies | pip | **stdlib** |
| Startup | ~500 ms | **~50 ms** |
| Memory | ~50 MB | **~10 MB** |

---

## 📱 Mobile Support

### Android

See [android/README.md](android/README.md) for build instructions.

Quick build (requires Android SDK):
```bash
make android
```

### iOS

Planned for future release.

---

## 🔒 Security

- No personal data in code
- No passwords or tokens hardcoded
- `.gitignore` properly configured
- Security audit: see `SECURITY_AUDIT.md`

---

## 🛠️ Build

```bash
# All platforms
make all

# Specific platform
make windows    # Windows (.exe)
make linux      # Linux (amd64)
make darwin     # macOS Intel + Apple Silicon
make android    # Android (.aar library)
```

---

## 📋 Configuration

Config file location:

- **Windows:** `%APPDATA%/TgWsProxy/config.json`
- **Linux:** `~/.config/TgWsProxy/config.json`
- **macOS:** `~/Library/Application Support/TgWsProxy/config.json`

```json
{
  "port": 1080,
  "host": "127.0.0.1",
  "dc_ip": [
    "1:149.154.175.50",
    "2:149.154.167.220",
    "3:149.154.175.100",
    "4:149.154.167.220",
    "5:91.108.56.100"
  ],
  "verbose": false,
  "log_max_mb": 5,
  "buf_kb": 256,
  "pool_size": 4,
  "auth": ""
}
```

---

## 🐛 Known Issues

1. **IPv6** — supported via IPv4-mapped addresses (::ffff:x.x.x.x) and NAT64
2. **DC3 WebSocket** — may be unavailable in some regions

---

## 📈 Project Statistics

| Metric | Value |
|--------|-------|
| Lines of Go code | ~2800 |
| Files in repo | 19 |
| Dependencies | 0 (stdlib only) |
| Supported platforms | 4 |

---

## 🎯 Fixed Issues from Original

All reported issues from [Flowseal/tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy/issues) are resolved:

- ✅ #386 — SOCKS5 authentication
- ✅ #380 — Too many open files
- ✅ #388 — Infinite connection
- ✅ #378 — Media not loading
- ✅ #373 — Auto DC detection

See `ISSUES_ANALYSIS.md` for details.

---

## 📄 License

MIT License

---

## 🔗 Links

- **Repository:** https://github.com/y0sy4/tg-ws-proxy-go
- **Releases:** https://github.com/y0sy4/tg-ws-proxy-go/releases
- **Original (Python):** https://github.com/Flowseal/tg-ws-proxy

---

**Built with ❤️ using Go 1.21**
