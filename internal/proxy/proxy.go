// Package proxy provides the main TG WS Proxy server implementation.
package proxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Flowseal/tg-ws-proxy/internal/config"
	"github.com/Flowseal/tg-ws-proxy/internal/mtproto"
	"github.com/Flowseal/tg-ws-proxy/internal/pool"
	"github.com/Flowseal/tg-ws-proxy/internal/socks5"
	"github.com/Flowseal/tg-ws-proxy/internal/websocket"
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

// Telegram IP ranges
var tgRanges = []struct {
	lo, hi uint32
}{
	{ipToUint32("185.76.151.0"), ipToUint32("185.76.151.255")},
	{ipToUint32("149.154.160.0"), ipToUint32("149.154.175.255")},
	{ipToUint32("91.105.192.0"), ipToUint32("91.105.193.255")},
	{ipToUint32("91.108.0.0"), ipToUint32("91.108.255.255")},
}

// IP to DC mapping - полный список всех IP Telegram DC
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
	// DC3
	"149.154.175.100": {3, false}, "149.154.175.101": {3, false},
	"149.154.175.102": {3, true},
	// DC4
	"149.154.167.91": {4, false}, "149.154.167.92": {4, false},
	"149.154.164.250": {4, true}, "149.154.166.120": {4, true},
	"149.154.166.121": {4, true}, "149.154.167.118": {4, true},
	"149.154.165.111": {4, true},
	// DC5
	"91.108.56.100": {5, false}, "91.108.56.101": {5, false},
	"91.108.56.116": {5, false}, "91.108.56.126": {5, false},
	"149.154.171.5": {5, false},
	"91.108.56.102": {5, true}, "91.108.56.128": {5, true},
	"91.108.56.151": {5, true},
	// DC203 (Test DC)
	"91.105.192.100": {203, false},
}

// DC overrides
var dcOverrides = map[int]int{
	203: 2,
}

// Stats holds proxy statistics.
type Stats struct {
	mu                 sync.Mutex
	ConnectionsTotal   int64
	ConnectionsWS      int64
	ConnectionsTCP     int64
	ConnectionsHTTP    int64
	ConnectionsPass    int64
	WSErrors           int64
	BytesUp            int64
	BytesDown          int64
	PoolHits           int64
	PoolMisses         int64
}

func (s *Stats) addConnectionsTotal(n int64) {
	s.mu.Lock()
	s.ConnectionsTotal += n
	s.mu.Unlock()
}

func (s *Stats) addConnectionsWS(n int64) {
	s.mu.Lock()
	s.ConnectionsWS += n
	s.mu.Unlock()
}

func (s *Stats) addConnectionsTCP(n int64) {
	s.mu.Lock()
	s.ConnectionsTCP += n
	s.mu.Unlock()
}

func (s *Stats) addConnectionsHTTP(n int64) {
	s.mu.Lock()
	s.ConnectionsHTTP += n
	s.mu.Unlock()
}

func (s *Stats) addConnectionsPass(n int64) {
	s.mu.Lock()
	s.ConnectionsPass += n
	s.mu.Unlock()
}

func (s *Stats) addWSErrors(n int64) {
	s.mu.Lock()
	s.WSErrors += n
	s.mu.Unlock()
}

func (s *Stats) addBytesUp(n int64) {
	s.mu.Lock()
	s.BytesUp += n
	s.mu.Unlock()
}

func (s *Stats) addBytesDown(n int64) {
	s.mu.Lock()
	s.BytesDown += n
	s.mu.Unlock()
}

func (s *Stats) addPoolHits(n int64) {
	s.mu.Lock()
	s.PoolHits += n
	s.mu.Unlock()
}

func (s *Stats) addPoolMisses(n int64) {
	s.mu.Lock()
	s.PoolMisses += n
	s.mu.Unlock()
}

func (s *Stats) Summary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("total=%d ws=%d tcp=%d http=%d pass=%d err=%d pool=%d/%d up=%s down=%s",
		s.ConnectionsTotal, s.ConnectionsWS, s.ConnectionsTCP,
		s.ConnectionsHTTP, s.ConnectionsPass, s.WSErrors,
		s.PoolHits, s.PoolHits+s.PoolMisses,
		humanBytes(s.BytesUp), humanBytes(s.BytesDown))
}

// Server represents the TG WS Proxy server.
type Server struct {
	config      *config.Config
	dcOpt       map[int]string
	wsPool      *pool.WSPool
	stats       *Stats
	wsBlacklist map[pool.DCKey]bool
	dcFailUntil map[pool.DCKey]time.Time
	mu          sync.RWMutex
	listener    net.Listener
	logger      *log.Logger
}

// NewServer creates a new proxy server.
func NewServer(cfg *config.Config, logger *log.Logger) (*Server, error) {
	dcOpt, err := config.ParseDCIPList(cfg.DCIP)
	if err != nil {
		return nil, err
	}

	s := &Server{
		config:      cfg,
		dcOpt:       dcOpt,
		wsPool:      pool.NewWSPool(cfg.PoolSize, defaultPoolMaxAge),
		stats:       &Stats{},
		wsBlacklist: make(map[pool.DCKey]bool),
		dcFailUntil: make(map[pool.DCKey]time.Time),
		logger:      logger,
	}

	return s, nil
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
		s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.logError("accept: %v", err)
			continue
		}
		go s.handleClient(conn)
	}
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
			// Patch init if we have DC override
			if _, ok := s.dcOpt[dcInfo.DC]; ok {
				if patched, ok := mtproto.PatchInitDC(initBuf, dcInfo.DC); ok {
					initData = patched
					dcInfo.Patched = true
				}
			}
		}
	}

	if !dcInfo.Valid {
		s.logWarning("[%s] unknown DC for %s:%d -> TCP fallback", label, req.DestAddr, req.DestPort)
		s.handleTCPFallback(conn, req.DestAddr, req.DestPort, initData, label, dcInfo.DC, dcInfo.IsMedia)
		return
	}

	dcKey := pool.DCKey{DC: dcInfo.DC, IsMedia: dcInfo.IsMedia}
	mediaTag := s.mediaTag(dcInfo.IsMedia)

	// Check WS blacklist
	s.mu.RLock()
	blacklisted := s.wsBlacklist[dcKey]
	s.mu.RUnlock()

	if blacklisted {
		s.logDebug("[%s] DC%d%s WS blacklisted -> TCP fallback", label, dcInfo.DC, mediaTag)
		s.handleTCPFallback(conn, req.DestAddr, req.DestPort, initData, label, dcInfo.DC, dcInfo.IsMedia)
		return
	}

	// Get WS timeout based on recent failures
	wsTimeout := s.getWSTimeout(dcKey)
	domains := s.getWSDomains(dcInfo.DC, dcInfo.IsMedia)
	
	// Get target IP from config, or use the destination IP from request
	targetIP := s.dcOpt[dcInfo.DC]
	if targetIP == "" {
		// Fallback: use the destination IP from the request
		targetIP = req.DestAddr
		s.logDebug("[%s] No target IP configured for DC%d, using request dest %s", label, dcInfo.DC, targetIP)
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
	s.bridgeWS(conn, ws, label, dcInfo.DC, req.DestAddr, req.DestPort, dcInfo.IsMedia, splitter)
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
		ws, wsErr = websocket.Connect(targetIP, domain, "/apiws", wsTimeout)
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
			s.wsBlacklist[dcKey] = true
			s.logWarning("[%s] DC%d%s blacklisted for WS (all 302)", label, dc, mediaTag)
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
	remoteConn, err := net.DialTimeout("tcp", net.JoinHostPort(dst, fmt.Sprintf("%d", port)), 10*time.Second)
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
	remoteConn, err := net.DialTimeout("tcp6", net.JoinHostPort(ipv6Addr, fmt.Sprintf("%d", port)), 10*time.Second)
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
	if strings.HasPrefix(strings.ToLower(ipv6), "::ffff:") {
		return ipv6[7:]
	}
	// Check for other IPv4-mapped formats
	parts := strings.Split(ipv6, ":")
	if len(parts) >= 6 {
		// Try to parse last 2 parts as hex IPv4
		if len(parts[6]) == 4 && len(parts[7]) == 4 {
			// This is a more complex case, skip for now
		}
	}
	return ""
}

// extractIPv4FromNAT64 extracts IPv4 from NAT64 IPv6 address.
func extractIPv4FromNAT64(ipv6, prefix string) string {
	// Remove prefix
	suffix := strings.TrimPrefix(ipv6, prefix)
	// NAT64 embeds IPv4 in last 32 bits
	parts := strings.Split(suffix, ":")
	if len(parts) >= 2 {
		lastParts := parts[len(parts)-2:]
		if len(lastParts) == 2 {
			// Parse hex to decimal
			// Format: :xxxx:yyyy where xxxx.yyyy is IPv4 in hex
			// This is simplified - real implementation would parse properly
			return "" // For now, return empty to indicate not supported
		}
	}
	return ""
}

func (s *Server) handleTCPFallback(conn net.Conn, dst string, port uint16, init []byte, label string, dc int, isMedia bool) {
	remoteConn, err := net.DialTimeout("tcp", net.JoinHostPort(dst, fmt.Sprintf("%d", port)), 10*time.Second)
	if err != nil {
		s.logWarning("[%s] TCP fallback to %s:%d failed: %v", label, dst, port, err)
		return
	}
	defer remoteConn.Close()

	s.stats.addConnectionsTCP(1)

	// Send init
	remoteConn.Write(init)

	s.bridgeTCP(conn, remoteConn, label)
}

func (s *Server) bridgeWS(clientConn net.Conn, ws *websocket.WebSocket, label string,
	dc int, dst string, port uint16, isMedia bool, splitter *mtproto.MsgSplitter) {

	mediaTag := s.mediaTag(isMedia)
	dcTag := fmt.Sprintf("DC%d%s", dc, mediaTag)
	dstTag := fmt.Sprintf("%s:%d", dst, port)

	startTime := time.Now()
	var upBytes, downBytes int64
	var upPkts, downPkts int64

	done := make(chan struct{}, 2)
	var wg sync.WaitGroup

	// Client -> WS
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { done <- struct{}{} }()

		buf := make([]byte, 65536)
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
	ws.Close()
	clientConn.Close()

	// Wait for goroutines to finish
	wg.Wait()

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
		buf := make([]byte, 65536)
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
	for dc, targetIP := range s.dcOpt {
		for isMedia := range []int{0, 1} {
			dcKey := pool.DCKey{DC: dc, IsMedia: isMedia == 1}
			domains := s.getWSDomains(dc, isMedia == 1)
			go func(dcKey pool.DCKey, targetIP string, domains []string) {
				for s.wsPool.NeedRefill(dcKey) {
					for _, domain := range domains {
						ws, err := websocket.Connect(targetIP, domain, "/apiws", wsConnectTimeout)
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
		return []string{
			fmt.Sprintf("kws%d-1.web.telegram.org", dc),
			fmt.Sprintf("kws%d.web.telegram.org", dc),
		}
	}
	return []string{
		fmt.Sprintf("kws%d.web.telegram.org", dc),
		fmt.Sprintf("kws%d-1.web.telegram.org", dc),
	}
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
	for dcKey := range s.wsBlacklist {
		mediaTag := ""
		if dcKey.IsMedia {
			mediaTag = "m"
		}
		entries = append(entries, fmt.Sprintf("DC%d%s", dcKey.DC, mediaTag))
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
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return 0
	}
	var result uint32
	for i, part := range parts {
		var n uint32
		fmt.Sscanf(part, "%d", &n)
		result |= n << (24 - uint(i)*8)
	}
	return result
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
	return bytesEqual(data[:5], []byte("POST ")) ||
		bytesEqual(data[:4], []byte("GET ")) ||
		bytesEqual(data[:5], []byte("HEAD ")) ||
		bytesEqual(data[:8], []byte("OPTIONS "))
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
