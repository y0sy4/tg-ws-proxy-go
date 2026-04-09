// Package proxy provides the main TG WS Proxy server implementation.
package proxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/y0sy4/telegram-proxy/internal/config"
	"github.com/y0sy4/telegram-proxy/internal/mtproto"
	"github.com/y0sy4/telegram-proxy/internal/pool"
	"github.com/y0sy4/telegram-proxy/internal/socks5"
	"github.com/y0sy4/telegram-proxy/internal/websocket"
	"golang.org/x/net/proxy"
)

const (
	defaultRecvBuf     = 256 * 1024
	defaultSendBuf     = 256 * 1024
	defaultPoolSize    = 4
	defaultPoolMaxAge  = 120 * time.Second
	dcFailCooldown     = 30 * time.Second
	wsFailTimeout      = 2 * time.Second
	wsConnectTimeout   = 10 * time.Second
)

// bufPool provides reusable 256KB buffers to reduce GC pressure.
// Each buffer is defaultRecvBuf bytes (256KB).
var bufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, defaultRecvBuf)
		return &buf
	},
}

// Telegram IP ranges
var tgRanges = []struct {
	lo, hi uint32
}{
	{ipToUint32("185.76.151.0"), ipToUint32("185.76.151.255")},
	{ipToUint32("149.154.160.0"), ipToUint32("149.154.175.255")},
	{ipToUint32("91.105.192.0"), ipToUint32("91.105.193.255")},
	{ipToUint32("91.108.0.0"), ipToUint32("91.108.255.255")},
}

// IP to DC mapping - полный список всех IP Telegram DC + CDN/media серверов
// Обновлён на основе community reports (Issues #5, #72, #199) и анализа tg-ws-proxy
var ipToDC = map[string]struct {
	DC      int
	IsMedia bool
}{
	// DC1
	"149.154.175.50": {1, false}, "149.154.175.51": {1, false},
	"149.154.175.52": {1, true}, "149.154.175.53": {1, false},
	"149.154.175.54": {1, false},
	// DC2
	"149.154.167.41": {2, false}, "149.154.167.50": {2, false},
	"149.154.167.51": {2, false}, "149.154.167.220": {2, false},
	"95.161.76.100": {2, false},
	"149.154.167.151": {2, true}, "149.154.167.222": {2, true},
	"149.154.167.223": {2, true}, "149.154.162.123": {2, true},
	// DC2 CDN/media — дополнительные IP из community reports
	"149.154.167.42": {2, true}, "149.154.167.43": {2, true},
	"149.154.166.222": {2, true},
	// DC3
	"149.154.175.100": {3, false}, "149.154.175.101": {3, false},
	"149.154.175.102": {3, true},
	// DC3 CDN/media — дополнительные IP
	"149.154.175.103": {3, true}, "149.154.175.104": {3, true},
	// DC4
	"149.154.167.91": {4, false}, "149.154.167.92": {4, false},
	"149.154.164.250": {4, true}, "149.154.166.120": {4, true},
	"149.154.166.121": {4, true}, "149.154.167.118": {4, true},
	"149.154.165.111": {4, true},
	// DC4 CDN/media — дополнительные IP из community reports
	"149.154.167.93": {4, true}, "149.154.167.94": {4, true},
	"149.154.166.122": {4, true},
	// DC5
	"91.108.56.100": {5, false}, "91.108.56.101": {5, false},
	"91.108.56.116": {5, false}, "91.108.56.126": {5, false},
	"149.154.171.5": {5, false},
	"91.108.56.102": {5, true}, "91.108.56.128": {5, true},
	"91.108.56.151": {5, true},
	// DC5 CDN/media — дополнительные IP
	"91.108.56.103": {5, true}, "91.108.56.129": {5, true},
	// DC203 (Test DC) — основной + media IP для CDN
	"91.105.192.100": {203, false},
	"91.105.192.101": {203, true},
	// Примечание: DC203 маппится на DC2 через dcOverrides, поэтому WS использует домены DC2
}

// DC overrides
var dcOverrides = map[int]int{
	203: 2,
}

// Stats holds proxy statistics.
type Stats struct {
	ConnectionsTotal atomic.Int64
	ConnectionsWS    atomic.Int64
	ConnectionsTCP   atomic.Int64
	ConnectionsHTTP  atomic.Int64
	ConnectionsPass  atomic.Int64
	WSErrors         atomic.Int64
	BytesUp          atomic.Int64
	BytesDown        atomic.Int64
	PoolHits         atomic.Int64
	PoolMisses       atomic.Int64
}

func (s *Stats) addConnectionsTotal(n int64) {
	s.ConnectionsTotal.Add(n)
}

func (s *Stats) addConnectionsWS(n int64) {
	s.ConnectionsWS.Add(n)
}

func (s *Stats) addConnectionsTCP(n int64) {
	s.ConnectionsTCP.Add(n)
}

func (s *Stats) addConnectionsHTTP(n int64) {
	s.ConnectionsHTTP.Add(n)
}

func (s *Stats) addConnectionsPass(n int64) {
	s.ConnectionsPass.Add(n)
}

func (s *Stats) addWSErrors(n int64) {
	s.WSErrors.Add(n)
}

func (s *Stats) addBytesUp(n int64) {
	s.BytesUp.Add(n)
}

func (s *Stats) addBytesDown(n int64) {
	s.BytesDown.Add(n)
}

func (s *Stats) addPoolHits(n int64) {
	s.PoolHits.Add(n)
}

func (s *Stats) addPoolMisses(n int64) {
	s.PoolMisses.Add(n)
}

func (s *Stats) Summary() string {
	hits := s.PoolHits.Load()
	misses := s.PoolMisses.Load()
	return fmt.Sprintf("total=%d ws=%d tcp=%d http=%d pass=%d err=%d pool=%d/%d up=%s down=%s",
		s.ConnectionsTotal.Load(), s.ConnectionsWS.Load(), s.ConnectionsTCP.Load(),
		s.ConnectionsHTTP.Load(), s.ConnectionsPass.Load(), s.WSErrors.Load(),
		hits, hits+misses,
		humanBytes(s.BytesUp.Load()), humanBytes(s.BytesDown.Load()))
}

// Server represents the TG WS Proxy server.
type Server struct {
	config          *config.Config
	dcOpt           map[int]string
	dcOptMedia      map[int]string
	wsPool          *pool.WSPool
	stats           *Stats
	wsBlacklist     map[pool.DCKey]time.Time // blacklist with expiry time (TTL)
	dcFailUntil     map[pool.DCKey]time.Time
	mu              sync.RWMutex
	listener        net.Listener
	logger          *log.Logger
	upstreamProxy   string
	rateLimiter     *connRateLimiter
	activeConns     sync.WaitGroup // track active connections for graceful shutdown
}

const blacklistTTL = 10 * time.Minute // auto-clear blacklist after this duration
const maxConnPerSecPerIP = 10          // max new connections per second from single IP

// connRateLimiter tracks connection rates per IP.
type connRateLimiter struct {
	mu       sync.Mutex
	counters map[string]*ipCounter
}

type ipCounter struct {
	count    int
	windowStart time.Time
}

func newConnRateLimiter() *connRateLimiter {
	return &connRateLimiter{
		counters: make(map[string]*ipCounter),
	}
}

// Allow returns true if connection from this IP is allowed.
func (rl *connRateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	c, ok := rl.counters[ip]
	if !ok || now.Sub(c.windowStart) > time.Second {
		rl.counters[ip] = &ipCounter{count: 1, windowStart: now}
		return true
	}

	if c.count >= maxConnPerSecPerIP {
		return false
	}
	c.count++
	return true
}

// NewServer creates a new proxy server.
func NewServer(cfg *config.Config, logger *log.Logger, upstreamProxy string) (*Server, error) {
	dcOpt, dcOptMedia, err := config.ParseDCIPList(cfg.DCIP)
	if err != nil {
		return nil, err
	}

	s := &Server{
		config:        cfg,
		dcOpt:         dcOpt,
		dcOptMedia:    dcOptMedia,
		wsPool:        pool.NewWSPool(cfg.PoolSize, defaultPoolMaxAge),
		stats:         &Stats{},
		wsBlacklist:   make(map[pool.DCKey]time.Time),
		dcFailUntil:   make(map[pool.DCKey]time.Time),
		logger:        logger,
		upstreamProxy: upstreamProxy,
		rateLimiter:   newConnRateLimiter(),
	}

	return s, nil
}

// dialWithUpstream creates a connection, optionally routing through an upstream proxy.
func (s *Server) dialWithUpstream(network, addr string, timeout time.Duration) (net.Conn, error) {
	if s.upstreamProxy == "" {
		return net.DialTimeout(network, addr, timeout)
	}

	// Parse upstream proxy URL
	u, err := url.Parse(s.upstreamProxy)
	if err != nil {
		return nil, fmt.Errorf("parse upstream proxy: %w", err)
	}

	switch u.Scheme {
	case "socks5", "socks":
		var auth *proxy.Auth
		if u.User != nil {
			password, _ := u.User.Password()
			auth = &proxy.Auth{
				User:     u.User.Username(),
				Password: password,
			}
		}
		dialer, err := proxy.SOCKS5(network, u.Host, auth, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("create SOCKS5 dialer: %w", err)
		}
		return dialer.Dial(network, addr)

	case "http", "https":
		// Use http.Transport with Proxy for HTTP CONNECT
		transport := &http.Transport{
			Proxy:           http.ProxyURL(u),
			TLSHandshakeTimeout: timeout,
		}
		return transport.Dial(network, addr)

	default:
		return nil, fmt.Errorf("unsupported upstream proxy scheme: %s", u.Scheme)
	}
}

// Start starts the proxy server.
func (s *Server) Start(ctx context.Context) error {
	addr := net.JoinHostPort(s.config.Host, fmt.Sprintf("%d", s.config.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	s.listener = listener

	// Set TCP_NODELAY
	if tcpListener, ok := listener.(*net.TCPListener); ok {
		if tcpConn, err := tcpListener.SyscallConn(); err == nil {
			tcpConn.Control(func(fd uintptr) {
				// Platform-specific socket options
			})
		}
	}

	s.logInfo("Telegram WS Bridge Proxy")
	s.logInfo("Listening on %s:%d", s.config.Host, s.config.Port)
	s.logInfo("Target DC IPs:")
	for dc, ip := range s.dcOpt {
		s.logInfo("  DC%d: %s", dc, ip)
	}

	// Warmup pool
	s.warmupPool()

	// Start stats logging
	go s.logStats(ctx)

	// Accept connections
	go func() {
		<-ctx.Done()
		s.logInfo("shutting down: stopping listener, waiting for active connections (10s)...")
		s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				// Graceful shutdown — wait for active connections to finish
				done := make(chan struct{})
				go func() {
					s.activeConns.Wait()
					close(done)
				}()
				select {
				case <-done:
					s.logInfo("all active connections closed gracefully")
				case <-time.After(10 * time.Second):
					s.logWarning("shutdown timeout: some connections still active")
				}
				return nil
			}
			s.logError("accept: %v", err)
			continue
		}

		// Rate limit check
		peerIP := extractIPFromAddr(conn.RemoteAddr().String())
		if !s.rateLimiter.Allow(peerIP) {
			s.logWarning("[%s] rate limit exceeded, connection dropped", peerIP)
			conn.Close()
			continue
		}

		s.activeConns.Add(1)
		go func(c net.Conn) {
			defer s.activeConns.Done()
			s.handleClient(c)
		}(conn)
	}
}

func extractIPFromAddr(addr string) string {
	// addr is usually "ip:port"
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func (s *Server) handleClient(conn net.Conn) {
	defer conn.Close()

	s.stats.addConnectionsTotal(1)
	peerAddr := conn.RemoteAddr().String()
	label := peerAddr

	// Set buffer sizes
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetReadBuffer(defaultRecvBuf)
		tcpConn.SetWriteBuffer(defaultSendBuf)
		tcpConn.SetNoDelay(true)
	}

	// Parse auth config
	authCfg := &socks5.AuthConfig{}
	if s.config.Auth != "" {
		parts := strings.SplitN(s.config.Auth, ":", 2)
		if len(parts) == 2 {
			authCfg.Enabled = true
			authCfg.Username = parts[0]
			authCfg.Password = parts[1]
		}
	}

	// SOCKS5 greeting
	if _, err := socks5.HandleGreeting(conn, authCfg); err != nil {
		s.logDebug("[%s] SOCKS5 greeting failed: %v", label, err)
		return
	}

	// Read CONNECT request
	req, err := socks5.ReadRequest(conn)
	if err != nil {
		s.logDebug("[%s] read request failed: %v", label, err)
		return
	}

	// Check for IPv6
	if strings.Contains(req.DestAddr, ":") {
		s.logInfo("[%s] IPv6 address %s:%d - using NAT64 fallback", label, req.DestAddr, req.DestPort)
		// Try to resolve via DNS64 or use IPv4 mapping
		s.handleIPv6Connection(conn, req.DestAddr, req.DestPort, label)
		return
	}

	// Check if Telegram IP
	if !isTelegramIP(req.DestAddr) {
		s.stats.addConnectionsPass(1)
		s.logDebug("[%s] passthrough to %s:%d", label, req.DestAddr, req.DestPort)
		s.handlePassthrough(conn, req.DestAddr, req.DestPort, label)
		return
	}

	// Send success reply
	conn.Write(socks5.Reply(socks5.ReplySucc))

	// Read init packet (64 bytes)
	initBuf := make([]byte, 64)
	if _, err := io.ReadFull(conn, initBuf); err != nil {
		s.logDebug("[%s] client disconnected before init", label)
		return
	}

	// Check for HTTP transport
	if isHTTPTransport(initBuf) {
		s.stats.addConnectionsHTTP(1)
		s.logDebug("[%s] HTTP transport rejected", label)
		conn.Close()
		return
	}

	// Extract DC from init
	dcInfo := mtproto.ExtractDCFromInit(initBuf)
	initData := initBuf

	// Fallback to IP mapping if DC extraction failed
	if !dcInfo.Valid {
		if dcMapping, ok := ipToDC[req.DestAddr]; ok {
			dcInfo.DC = dcMapping.DC
			dcInfo.IsMedia = dcMapping.IsMedia
			dcInfo.Valid = true
			// Patch init if we have DC override (regular or media)
			if _, ok := s.dcOpt[dcInfo.DC]; ok || s.getTargetIP(dcInfo.DC, dcInfo.IsMedia) != "" {
				if patched, ok := mtproto.PatchInitDC(initBuf, dcInfo.DC); ok {
					initData = patched
					dcInfo.Patched = true
				}
			}
		}
	}

	if !dcInfo.Valid {
		s.logWarning("[%s] ⚠️  unknown DC for %s:%d -> TCP fallback", label, req.DestAddr, req.DestPort)
		s.logWarning("[%s] 💡 If media fails, this IP may be a CDN server. Try adding --dc-ip or report at github.com/y0sy4/telegram-proxy/issues", label)
		s.handleTCPFallback(conn, req.DestAddr, req.DestPort, initData, label, dcInfo.DC, dcInfo.IsMedia)
		return
	}

	dcKey := pool.DCKey{DC: dcInfo.DC, IsMedia: dcInfo.IsMedia}
	mediaTag := s.mediaTag(dcInfo.IsMedia)

	// Check WS blacklist (with TTL auto-expiry)
	s.mu.RLock()
	blTime, isBlacklisted := s.wsBlacklist[dcKey]
	s.mu.RUnlock()

	if isBlacklisted && time.Now().Before(blTime) {
		s.logDebug("[%s] DC%d%s WS blacklisted (expires in %.0fs) -> TCP fallback",
			label, dcInfo.DC, mediaTag, time.Until(blTime).Seconds())
		s.handleTCPFallback(conn, req.DestAddr, req.DestPort, initData, label, dcInfo.DC, dcInfo.IsMedia)
		return
	} else if isBlacklisted {
		// Blacklist expired — auto-clear
		s.mu.Lock()
		delete(s.wsBlacklist, dcKey)
		s.mu.Unlock()
		s.logInfo("[%s] DC%d%s blacklist expired, attempting WS again", label, dcInfo.DC, mediaTag)
	}

	// Get WS timeout based on recent failures
	wsTimeout := s.getWSTimeout(dcKey)
	domains := s.getWSDomains(dcInfo.DC, dcInfo.IsMedia)

	// Get target IP from config (media-specific if configured), or use destination IP
	targetIP := s.getTargetIP(dcInfo.DC, dcInfo.IsMedia)
	if targetIP == "" {
		targetIP = req.DestAddr
		s.logDebug("[%s] No target IP configured for DC%d%s, using request dest %s", label, dcInfo.DC, mediaTag, targetIP)
	}

	// Try to get WS from pool
	ws, fromPool := s.getWebSocket(dcKey, targetIP, domains, wsTimeout, label, dcInfo.DC, req.DestAddr, req.DestPort, mediaTag)

	if ws == nil {
		// WS failed -> TCP fallback
		s.handleTCPFallback(conn, req.DestAddr, req.DestPort, initData, label, dcInfo.DC, dcInfo.IsMedia)
		return
	}

	if fromPool {
		s.logInfo("[%s] DC%d%s (%s:%d) -> pool hit via %s", label, dcInfo.DC, mediaTag, req.DestAddr, req.DestPort, targetIP)
	} else {
		s.logInfo("[%s] DC%d%s (%s:%d) -> WS via %s", label, dcInfo.DC, mediaTag, req.DestAddr, req.DestPort, targetIP)
	}

	// Send init packet
	if err := ws.Send(initData); err != nil {
		s.logError("[%s] send init failed: %v", label, err)
		ws.Close()
		return
	}

	s.stats.addConnectionsWS(1)

	// Create splitter if init was patched
	var splitter *mtproto.MsgSplitter
	if dcInfo.Patched {
		splitter, _ = mtproto.NewMsgSplitter(initData)
	}

	// Bridge traffic
	s.bridgeWS(conn, ws, label, dcKey, req.DestAddr, req.DestPort, splitter)
}

func (s *Server) getWebSocket(dcKey pool.DCKey, targetIP string, domains []string,
	wsTimeout time.Duration, label string, dc int, dst string, port uint16, mediaTag string) (*websocket.WebSocket, bool) {

	// Try pool first
	ws := s.wsPool.Get(dcKey)
	if ws != nil {
		s.stats.addPoolHits(1)
		return ws, true
	}

	s.stats.addPoolMisses(1)

	// Try to connect
	var wsErr error
	allRedirects := true

	// Use targetIP for connection, domain for TLS/SNI
	for _, domain := range domains {
		url := fmt.Sprintf("wss://%s/apiws", domain)
		s.logInfo("[%s] DC%d%s (%s:%d) -> %s via %s", label, dc, mediaTag, dst, port, url, targetIP)

		// Connect using targetIP, but use domain for TLS handshake
		ws, wsErr = websocket.ConnectWithDialer(targetIP, domain, "/apiws", wsTimeout, func(network, addr string) (net.Conn, error) {
			return s.dialWithUpstream(network, addr, wsTimeout)
		})
		if wsErr == nil {
			allRedirects = false
			break
		}

		s.stats.addWSErrors(1)

		if he, ok := wsErr.(*websocket.HandshakeError); ok {
			if he.IsRedirect() {
				s.logWarning("[%s] DC%d%s got %d from %s -> %s", label, dc, mediaTag, he.StatusCode, domain, he.Location)
				continue
			}
			allRedirects = false
			s.logWarning("[%s] DC%d%s handshake: %s", label, dc, mediaTag, he.Status)
		} else {
			allRedirects = false
			s.logWarning("[%s] DC%d%s connect failed: %v", label, dc, mediaTag, wsErr)
		}
	}

	if ws == nil {
		// Update blacklist/cooldown
		s.mu.Lock()
		if he, ok := wsErr.(*websocket.HandshakeError); ok && he.IsRedirect() && allRedirects {
			s.wsBlacklist[dcKey] = time.Now().Add(blacklistTTL)
			s.logWarning("[%s] DC%d%s blacklisted for WS (all 302, expires in %.0fm)",
				label, dc, mediaTag, blacklistTTL.Minutes())
		} else {
			s.dcFailUntil[dcKey] = time.Now().Add(dcFailCooldown)
		}
		s.mu.Unlock()
		return nil, false
	}

	// Clear cooldown on success
	s.mu.Lock()
	delete(s.dcFailUntil, dcKey)
	s.mu.Unlock()

	return ws, false
}

func (s *Server) handlePassthrough(conn net.Conn, dst string, port uint16, label string) {
	remoteConn, err := s.dialWithUpstream("tcp", net.JoinHostPort(dst, fmt.Sprintf("%d", port)), 10*time.Second)
	if err != nil {
		s.logWarning("[%s] passthrough failed to %s: %v", label, dst, err)
		conn.Write(socks5.Reply(socks5.ReplyFail))
		return
	}
	defer remoteConn.Close()

	conn.Write(socks5.Reply(socks5.ReplySucc))
	s.bridgeTCP(conn, remoteConn, label)
}

// handleIPv6Connection handles IPv6 connections via dual-stack or IPv4-mapped addresses.
func (s *Server) handleIPv6Connection(conn net.Conn, ipv6Addr string, port uint16, label string) {
	// Try direct IPv6 first
	remoteConn, err := s.dialWithUpstream("tcp6", net.JoinHostPort(ipv6Addr, fmt.Sprintf("%d", port)), 10*time.Second)
	if err == nil {
		s.logInfo("[%s] IPv6 direct connection successful", label)
		defer remoteConn.Close()
		conn.Write(socks5.Reply(socks5.ReplySucc))
		s.bridgeTCP(conn, remoteConn, label)
		return
	}

	s.logDebug("[%s] IPv6 direct failed, trying IPv4-mapped: %v", label, err)

	// Try to extract IPv4 from IPv6 (IPv4-mapped IPv6 address)
	if ipv4 := extractIPv4(ipv6Addr); ipv4 != "" {
		s.logInfo("[%s] Using IPv4-mapped address: %s", label, ipv4)
		s.handlePassthrough(conn, ipv4, port, label)
		return
	}

	// Try NAT64/DNS64 well-known prefixes
	nat64Prefixes := []string{
		"64:ff9b::",    // Well-known NAT64 prefix
		"2001:67c:2e8::", // RIPE NCC NAT64
		"2a00:1098::",  // Some providers
	}

	for _, prefix := range nat64Prefixes {
		if strings.HasPrefix(strings.ToLower(ipv6Addr), strings.ToLower(prefix)) {
			// Extract IPv4 from NAT64 address
			ipv4 := extractIPv4FromNAT64(ipv6Addr, prefix)
			if ipv4 != "" {
				s.logInfo("[%s] NAT64 detected, using IPv4: %s", label, ipv4)
				s.handlePassthrough(conn, ipv4, port, label)
				return
			}
		}
	}

	s.logWarning("[%s] IPv6 connection failed - no working path", label)
	conn.Write(socks5.Reply(socks5.ReplyHostUn))
}

// extractIPv4 tries to extract IPv4 from IPv4-mapped IPv6 address.
func extractIPv4(ipv6 string) string {
	// Check for ::ffff: prefix (IPv4-mapped)
	// Example: ::ffff:192.0.2.1
	if strings.HasPrefix(strings.ToLower(ipv6), "::ffff:") {
		return strings.TrimPrefix(ipv6, "::ffff:")
	}
	return ""
}

// extractIPv4FromNAT64 extracts IPv4 from NAT64 IPv6 address.
// Currently returns empty string as NAT64 is not fully supported.
func extractIPv4FromNAT64(ipv6, prefix string) string {
	// NAT64 embeds IPv4 in last 32 bits of the IPv6 address
	// This is a placeholder for future implementation
	return ""
}

func (s *Server) handleTCPFallback(conn net.Conn, dst string, port uint16, init []byte, label string, dc int, isMedia bool) {
	mediaTag := s.mediaTag(isMedia)
	s.logDebug("[%s] TCP fallback to %s:%d (DC%d%s)", label, dst, port, dc, mediaTag)

	remoteConn, err := s.dialWithUpstream("tcp", net.JoinHostPort(dst, fmt.Sprintf("%d", port)), 10*time.Second)
	if err != nil {
		s.logWarning("[%s] ⚠️  TCP fallback to %s:%d failed: %v", label, dst, port, err)
		s.logWarning("[%s] 💡 If media doesn't load, this DC may be blocked. Try --dc-ip with a working IP", label)
		return
	}
	defer remoteConn.Close()

	s.stats.addConnectionsTCP(1)

	// Send init
	remoteConn.Write(init)

	s.bridgeTCP(conn, remoteConn, label)
}

func (s *Server) bridgeWS(clientConn net.Conn, ws *websocket.WebSocket, label string,
	dcKey pool.DCKey, dst string, port uint16, splitter *mtproto.MsgSplitter) {

	mediaTag := s.mediaTag(dcKey.IsMedia)
	dcTag := fmt.Sprintf("DC%d%s", dcKey.DC, mediaTag)
	dstTag := fmt.Sprintf("%s:%d", dst, port)

	startTime := time.Now()
	var upBytes, downBytes int64
	var upPkts, downPkts int64
	var hasError atomic.Int32 // 0 = no error, 1 = error

	done := make(chan struct{}, 2)
	var wg sync.WaitGroup

	// Start heartbeat (ping/pong) to detect dead connections
	go ws.StartPingLoop(30 * time.Second)

	// Client -> WS
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { done <- struct{}{} }()

		bufPtr := bufPool.Get().(*[]byte)
		buf := *bufPtr
		defer bufPool.Put(bufPtr)
		for {
			n, err := clientConn.Read(buf)
			if n > 0 {
				s.stats.addBytesUp(int64(n))
				upBytes += int64(n)
				upPkts++

				if splitter != nil {
					parts := splitter.Split(buf[:n])
					if len(parts) > 1 {
						ws.SendBatch(parts)
					} else {
						ws.Send(parts[0])
					}
				} else {
					ws.Send(buf[:n])
				}
			}
			if err != nil {
				if err != io.EOF {
					s.logDebug("[%s] client->ws: %v", label, err)
				}
				hasError.Store(1)
				return
			}
		}
	}()

	// WS -> Client
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { done <- struct{}{} }()

		for {
			data, err := ws.Recv()
			if err != nil {
				if err != io.EOF {
					s.logDebug("[%s] ws->client: %v", label, err)
				}
				hasError.Store(1)
				return
			}
			n := len(data)
			s.stats.addBytesDown(int64(n))
			downBytes += int64(n)
			downPkts++

			if _, err := clientConn.Write(data); err != nil {
				s.logDebug("[%s] write client: %v", label, err)
				return
			}
		}
	}()

	// Wait for either direction to close
	<-done
	clientConn.Close()

	// Wait for goroutines to finish
	wg.Wait()

	// Return WS to pool if still alive (reusable connection)
	// Only reuse if no error occurred during the session
	if ws != nil && hasError.Load() == 0 {
		s.wsPool.Put(dcKey, ws)
		s.logDebug("[%s] %s (%s) WS returned back to pool", label, dcTag, dstTag)
	} else if ws != nil {
		ws.Close()
	}

	elapsed := time.Since(startTime).Seconds()
	s.logInfo("[%s] %s (%s) session closed: ^%s (%d pkts) v%s (%d pkts) in %.1fs",
		label, dcTag, dstTag,
		humanBytes(upBytes), upPkts,
		humanBytes(downBytes), downPkts,
		elapsed)
}

func (s *Server) bridgeTCP(conn, remoteConn net.Conn, label string) {
	done := make(chan struct{}, 2)

	copyFunc := func(dst, src net.Conn, isUp bool) {
		defer func() { done <- struct{}{} }()
		bufPtr := bufPool.Get().(*[]byte)
		buf := *bufPtr
		defer bufPool.Put(bufPtr)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				if isUp {
					s.stats.addBytesUp(int64(n))
				} else {
					s.stats.addBytesDown(int64(n))
				}
				dst.Write(buf[:n])
			}
			if err != nil {
				if err != io.EOF {
					s.logDebug("[%s] copy: %v", label, err)
				}
				return
			}
		}
	}

	go copyFunc(remoteConn, conn, true)
	go copyFunc(conn, remoteConn, false)

	<-done
	conn.Close()
	remoteConn.Close()
}

func (s *Server) warmupPool() {
	s.logInfo("WS pool warmup started for %d DC(s)", len(s.dcOpt))
	for dc := range s.dcOpt {
		for isMedia := range []int{0, 1} {
			dcKey := pool.DCKey{DC: dc, IsMedia: isMedia == 1}
			targetIP := s.getTargetIP(dc, isMedia == 1)
			if targetIP == "" {
				continue
			}
			domains := s.getWSDomains(dc, isMedia == 1)
			go func(dcKey pool.DCKey, targetIP string, domains []string) {
				for s.wsPool.NeedRefill(dcKey) {
					for _, domain := range domains {
						ws, err := websocket.ConnectWithDialer(targetIP, domain, "/apiws", wsConnectTimeout, func(network, addr string) (net.Conn, error) {
							return s.dialWithUpstream(network, addr, wsConnectTimeout)
						})
						if err == nil {
							s.wsPool.Put(dcKey, ws)
							break
						}
					}
					if !s.wsPool.NeedRefill(dcKey) {
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
			}(dcKey, targetIP, domains)
		}
	}
}

func (s *Server) logStats(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.RLock()
			bl := s.formatBlacklist()
			s.mu.RUnlock()
			s.logInfo("stats: %s | ws_bl: %s", s.stats.Summary(), bl)
		}
	}
}

func (s *Server) getWSTimeout(dcKey pool.DCKey) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if failUntil, ok := s.dcFailUntil[dcKey]; ok && time.Now().Before(failUntil) {
		return wsFailTimeout
	}
	return wsConnectTimeout
}

func (s *Server) getWSDomains(dc int, isMedia bool) []string {
	if override, ok := dcOverrides[dc]; ok {
		dc = override
	}

	if isMedia {
		// Media/CDN домены — расширенный список для надёжной загрузки картинок и видео
		// Основано на community reports (Issues #5, #72, #199) и hosts-конфигурациях OpenWRT
		return []string{
			fmt.Sprintf("kws%d-1.web.telegram.org", dc),
			fmt.Sprintf("kws%d.web.telegram.org", dc),
			fmt.Sprintf("kws%d-2.web.telegram.org", dc),
			fmt.Sprintf("pluto-%d.web.telegram.org", dc),
			"pluto.web.telegram.org",
			"venus.web.telegram.org",
		}
	}
	return []string{
		fmt.Sprintf("kws%d.web.telegram.org", dc),
		fmt.Sprintf("kws%d-1.web.telegram.org", dc),
	}
}

func (s *Server) getTargetIP(dc int, isMedia bool) string {
	if isMedia {
		if ip, ok := s.dcOptMedia[dc]; ok {
			return ip
		}
	}
	if ip, ok := s.dcOpt[dc]; ok {
		return ip
	}
	return ""
}

func (s *Server) mediaTag(isMedia bool) string {
	if isMedia {
		return "m"
	}
	return ""
}

func (s *Server) formatBlacklist() string {
	if len(s.wsBlacklist) == 0 {
		return "none"
	}

	var entries []string
	now := time.Now()
	for dcKey, blTime := range s.wsBlacklist {
		mediaTag := ""
		if dcKey.IsMedia {
			mediaTag = "m"
		}
		remaining := blTime.Sub(now)
		if remaining > 0 {
			entries = append(entries, fmt.Sprintf("DC%d%s(%dm%ds)", dcKey.DC, mediaTag,
				int(remaining.Minutes()), int(remaining.Seconds())%60))
		}
	}
	sort.Strings(entries)
	return strings.Join(entries, ", ")
}

func (s *Server) logInfo(format string, args ...interface{}) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}

func (s *Server) logWarning(format string, args ...interface{}) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}

func (s *Server) logError(format string, args ...interface{}) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}

func (s *Server) logDebug(format string, args ...interface{}) {
	if s.logger != nil && s.config.Verbose {
		s.logger.Printf(format, args...)
	}
}

// Helper functions

func ipToUint32(ip string) uint32 {
	ipObj := net.ParseIP(ip)
	if ipObj == nil {
		return 0
	}
	ipObj = ipObj.To4()
	if ipObj == nil {
		return 0
	}
	return binary.BigEndian.Uint32(ipObj)
}

func isTelegramIP(ip string) bool {
	ipNum := ipToUint32(ip)
	for _, r := range tgRanges {
		if ipNum >= r.lo && ipNum <= r.hi {
			return true
		}
	}
	return false
}

func isHTTPTransport(data []byte) bool {
	if len(data) < 5 {
		return false
	}
	return bytes.HasPrefix(data, []byte("POST ")) ||
		bytes.HasPrefix(data, []byte("GET ")) ||
		bytes.HasPrefix(data, []byte("HEAD ")) ||
		bytes.HasPrefix(data, []byte("OPTIONS "))
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for n := n; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n*unit/div), "KMGTPE"[exp])
}
