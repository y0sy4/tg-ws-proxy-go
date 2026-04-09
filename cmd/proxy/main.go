// TG WS Proxy - CLI application
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/y0sy4/telegram-proxy/internal/config"
	"github.com/y0sy4/telegram-proxy/internal/proxy"
	"github.com/y0sy4/telegram-proxy/internal/telegram"
	"github.com/y0sy4/telegram-proxy/internal/version"
)

var appVersion = "2.0.6"

// checkAndKillExisting checks if another instance is running and terminates it
func checkAndKillExisting() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exeName := filepath.Base(exe)
	
	// Find existing process (excluding current one)
	cmd := exec.Command("wmic", "process", "where", fmt.Sprintf("name='%s' AND processid!='%d'", exeName, os.Getpid()), "get", "processid")
	output, err := cmd.Output()
	if err != nil {
		return
	}
	
	// Parse PIDs and kill them
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "ProcessId" {
			continue
		}
		// Kill the old process
		exec.Command("taskkill", "/F", "/PID", line).Run()
	}
	
	// Wait for processes to terminate
	time.Sleep(1 * time.Second)
}

func main() {
	// Check for existing instances and terminate them (Windows only)
	if os.PathSeparator == '\\' {
		checkAndKillExisting()
	}

	// Parse flags
	port := flag.Int("port", 1080, "Listen port")
	host := flag.String("host", "127.0.0.1", "Listen host")
	dcIP := flag.String("dc-ip", "", "Target DC IPs (comma-separated, e.g., 2:149.154.167.220,2m:149.154.167.222,4:149.154.167.220)")
	verbose := flag.Bool("v", false, "Verbose logging")
	logFile := flag.String("log-file", "", "Log file path (default: proxy.log in app dir)")
	logMaxMB := flag.Float64("log-max-mb", 5, "Max log file size in MB")
	bufKB := flag.Int("buf-kb", 256, "Socket buffer size in KB")
	poolSize := flag.Int("pool-size", 4, "WS pool size per DC")
	auth := flag.String("auth", "", "SOCKS5 authentication (username:password)")
	
	// Advanced features (for experienced users)
	httpPort := flag.Int("http-port", 0, "Enable HTTP proxy on port (0 = disabled)")
	upstreamProxy := flag.String("upstream-proxy", "", "Upstream SOCKS5/HTTP proxy (format: socks5://user:pass@host:port or http://user:pass@host:port)")
	mtprotoSecret := flag.String("mtproto-secret", "", "MTProto proxy secret (enables MTProto mode)")
	mtprotoPort := flag.Int("mtproto-port", 0, "MTProto proxy port (requires --mtproto-secret)")

	// Security & diagnostics
	autoUpdate := flag.Bool("auto-update", false, "Enable automatic update checks (default: false for security)")
	testDC := flag.Bool("test-dc", false, "Test Telegram DC connectivity and exit")
	testDCMedia := flag.Bool("test-dc-media", false, "Test both text and media DC connectivity (slower)")

	showVersion := flag.Bool("version", false, "Show version")

	flag.Parse()

	if *showVersion {
		fmt.Printf("TG WS Proxy v%s\n", appVersion)
		os.Exit(0)
	}

	// Test DC connectivity mode
	if *testDC {
		testDCConnectivity(*dcIP, *verbose, false)
		os.Exit(0)
	}

	// Test DC + media connectivity mode
	if *testDCMedia {
		testDCConnectivity(*dcIP, *verbose, true)
		os.Exit(0)
	}

	// Load config file
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Warning: failed to load config: %v, using defaults", err)
		cfg = config.DefaultConfig()
	}

	// Override with CLI flags
	if *port != 1080 {
		cfg.Port = *port
	}
	if *host != "127.0.0.1" {
		cfg.Host = *host
	}
	if *dcIP != "" {
		cfg.DCIP = splitDCIP(*dcIP)
	}
	if *verbose {
		cfg.Verbose = *verbose
	}
	if *logMaxMB != 5 {
		cfg.LogMaxMB = *logMaxMB
	}
	if *bufKB != 256 {
		cfg.BufKB = *bufKB
	}
	if *poolSize != 4 {
		cfg.PoolSize = *poolSize
	}
	if *auth != "" {
		cfg.Auth = *auth
	}
	if *upstreamProxy != "" {
		cfg.UpstreamProxy = *upstreamProxy
	}

	// Setup logging - log to stdout if verbose, otherwise to file
	var logger *log.Logger
	logPath := *logFile
	if cfg.Verbose && logPath == "" {
		// Verbose mode: log to stdout
		logger = setupLogging("", cfg.LogMaxMB, cfg.Verbose)
	} else {
		// File mode: log to file (default to app dir if not specified)
		if logPath == "" {
			appDir := getAppDir()
			logPath = filepath.Join(appDir, "proxy.log")
		}
		logger = setupLogging(logPath, cfg.LogMaxMB, cfg.Verbose)
	}

	// Log advanced features usage and start HTTP proxy
	if *httpPort != 0 {
		log.Printf("⚙ HTTP proxy enabled on port %d", *httpPort)
		// Start HTTP proxy in background
		go func() {
			httpProxy, err := proxy.NewHTTPProxy(*httpPort, cfg.Verbose, logger, *upstreamProxy)
			if err != nil {
				log.Printf("Failed to create HTTP proxy: %v", err)
				return
			}
			if err := httpProxy.Start(); err != nil {
				log.Printf("HTTP proxy error: %v", err)
			}
		}()
	}
	if *upstreamProxy != "" {
		log.Printf("⚙ Upstream proxy: %s", *upstreamProxy)
	}

	// Create and start server
	server, err := proxy.NewServer(cfg, logger, cfg.UpstreamProxy)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Auto-configure Telegram Desktop with correct proxy type
	log.Println("Attempting to configure Telegram Desktop...")
	
	// Determine proxy type and configure Telegram accordingly
	// Note: Our local proxy only supports SOCKS5
	// HTTP port is for other applications (browsers, etc.)
	// MTProto requires external MTProxy server
	proxyType := "socks5"  // Always SOCKS5 for our local proxy
	proxyPort := cfg.Port
	proxySecret := ""
	
	// Log HTTP mode if enabled (for other apps, not Telegram)
	if *httpPort != 0 {
		log.Printf("⚙ HTTP proxy enabled on port %d (for browsers/other apps)", *httpPort)
	}
	
	// Log MTProto mode info
	if *mtprotoPort != 0 && *mtprotoSecret != "" {
		log.Printf("⚙ MTProto mode: Use external MTProxy or configure manually")
		log.Printf("  tg://proxy?server=%s&port=%d&secret=%s", cfg.Host, *mtprotoPort, *mtprotoSecret)
	}
	
	username, password := "", ""
	if cfg.Auth != "" {
		parts := strings.SplitN(cfg.Auth, ":", 2)
		if len(parts) == 2 {
			username, password = parts[0], parts[1]
		}
	}
	
	if telegram.ConfigureProxyWithType(cfg.Host, proxyPort, username, password, proxySecret, proxyType) {
		log.Printf("✓ Telegram Desktop %s proxy configuration opened", strings.ToUpper(proxyType))
	} else {
		log.Println("✗ Failed to auto-configure Telegram.")
		log.Println("  Manual setup: Settings → Advanced → Connection Type → Proxy")
		log.Printf("  Or open: tg://socks?server=%s&port=%d", cfg.Host, proxyPort)
	}

	// Check for updates only if explicitly enabled (security first)
	if *autoUpdate {
		go func() {
			hasUpdate, latest, url, err := version.CheckUpdate()
			if err != nil {
				return // Silent fail
			}
			if hasUpdate {
				log.Printf("⚡ NEW VERSION AVAILABLE: v%s (current: v%s)", latest, version.CurrentVersion)
				log.Printf("   Downloading update...")

				// Try to download update
				downloadedPath, err := version.DownloadUpdate(latest)
				if err != nil {
					log.Printf("   Download failed: %v", err)
					log.Printf("   Manual download: %s", url)
					return
				}

				log.Printf("   ✓ Downloaded to: %s", downloadedPath)
				log.Printf("   Restart the proxy to apply update")
			}
		}()
	}

	// Handle shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel()
	}()

	// Start server
	if err := server.Start(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func getAppDir() string {
	// Get app directory based on OS
	appData := os.Getenv("APPDATA")
	if appData != "" {
		// Windows
		return filepath.Join(appData, "TgWsProxy")
	}
	// Linux/macOS
	home, _ := os.UserHomeDir()
	if home != "" {
		return filepath.Join(home, ".TgWsProxy")
	}
	return "."
}

func setupLogging(logFile string, logMaxMB float64, verbose bool) *log.Logger {
	flags := log.LstdFlags | log.Lshortfile

	// If verbose and no log file specified, log to stdout
	if verbose && logFile == "" {
		log.SetOutput(os.Stdout)
		log.SetFlags(flags)
		return log.New(os.Stdout, "", flags)
	}

	// Ensure directory exists
	dir := filepath.Dir(logFile)
	os.MkdirAll(dir, 0755)

	// Open log file with rotation
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Warning: failed to open log file %s: %v, using stdout", logFile, err)
		log.SetOutput(os.Stdout)
		log.SetFlags(flags)
		return log.New(os.Stdout, "", flags)
	}

	// Check file size and rotate if needed
	info, _ := f.Stat()
	maxBytes := int64(logMaxMB * 1024 * 1024)
	if info.Size() > maxBytes {
		f.Close()
		os.Rename(logFile, logFile+".old")
		f, _ = os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	}

	log.SetOutput(f)
	log.SetFlags(flags)
	return log.New(f, "", flags)
}

func splitDCIP(s string) []string {
	if s == "" {
		return nil
	}
	result := make([]string, 0)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// testDCConnectivity tests connectivity to Telegram DCs
func testDCConnectivity(dcIPList string, verbose bool, testMedia bool) {
	fmt.Println("📡 Testing Telegram DC connectivity...")
	if testMedia {
		fmt.Println("   Including media/CDN servers...")
	}
	fmt.Println()

	// Default DCs to test (text + media)
	type dcEntry struct {
		dc      int
		ip      string
		isMedia bool
	}

	defaultDCs := []dcEntry{
		{2, "149.154.167.220", false},
		{2, "149.154.167.220", true},
		{4, "149.154.167.220", false},
		{4, "149.154.167.220", true},
	}

	if testMedia {
		defaultDCs = append(defaultDCs,
			dcEntry{2, "149.154.167.151", true},
			dcEntry{2, "149.154.167.222", true},
			dcEntry{4, "149.154.166.120", true},
			dcEntry{4, "149.154.167.118", true},
		)
	}

	// Use provided DCs or defaults
	var testEntries []dcEntry
	if dcIPList != "" {
		dcMap := splitDCIP(dcIPList)
		for dc, ip := range dcMap {
			testEntries = append(testEntries, dcEntry{dc, ip, false})
			if testMedia {
				testEntries = append(testEntries, dcEntry{dc, ip, true})
			}
		}
	} else {
		testEntries = defaultDCs
	}

	type result struct {
		dc       int
		ip       string
		isMedia  bool
		latency  time.Duration
		success  bool
		errorMsg string
	}

	results := make([]result, 0, len(testEntries))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, entry := range testEntries {
		wg.Add(1)
		go func(e dcEntry) {
			defer wg.Done()

			// Test TCP connection to port 443 (HTTPS/WS)
			start := time.Now()
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(e.ip, "443"), 5*time.Second)
			latency := time.Since(start)

			if err != nil {
				mu.Lock()
				results = append(results, result{
					dc: e.dc, ip: e.ip, isMedia: e.isMedia,
					success: false, errorMsg: err.Error(),
				})
				mu.Unlock()
				return
			}
			conn.Close()

			mu.Lock()
			results = append(results, result{
				dc: e.dc, ip: e.ip, isMedia: e.isMedia,
				latency: latency, success: true,
			})
			mu.Unlock()
		}(entry)
	}

	wg.Wait()

	// Sort: successful first, then by latency
	sort.Slice(results, func(i, j int) bool {
		if results[i].success != results[j].success {
			return results[i].success
		}
		return results[i].latency < results[j].latency
	})

	// Print results
	successCount := 0
	mediaSuccess := 0
	textSuccess := 0
	var recommendedText []string
	var recommendedMedia []string

	for _, r := range results {
		mediaTag := ""
		if r.isMedia {
			mediaTag = "m"
		}
		status := "❌"
		latencyStr := ""

		if r.success {
			status = "✅"
			latencyStr = fmt.Sprintf("%dms", r.latency.Milliseconds())
			successCount++
			if r.isMedia {
				mediaSuccess++
				recommendedMedia = append(recommendedMedia, fmt.Sprintf("%dm:%s", r.dc, r.ip))
			} else {
				textSuccess++
				recommendedText = append(recommendedText, fmt.Sprintf("%d:%s", r.dc, r.ip))
			}
		} else {
			latencyStr = "timeout"
			if verbose {
				latencyStr = fmt.Sprintf("error: %s", r.errorMsg)
			}
		}

		fmt.Printf("%s DC%-3d%s %-18s %s\n", status, r.dc, mediaTag, r.ip, latencyStr)
	}

	fmt.Println()
	fmt.Printf("Results: %d/%d reachable", successCount, len(results))
	if testMedia {
		fmt.Printf(" (text: %d, media: %d)", textSuccess, mediaSuccess)
	}
	fmt.Println()

	if len(recommendedText) > 0 {
		fmt.Println()
		fmt.Println("Recommended configuration:")
		combined := recommendedText
		if len(recommendedMedia) > 0 {
			combined = append(combined, recommendedMedia...)
		}
		fmt.Printf("  --dc-ip \"%s\"\n", strings.Join(combined, ","))
	}

	if successCount == 0 {
		fmt.Println()
		fmt.Println("⚠️  No DCs reachable. Check your internet connection or firewall.")
		fmt.Println("   If you're in a restricted region, try using --upstream-proxy")
		fmt.Println("   Example: --upstream-proxy \"socks5://127.0.0.1:9050\" (Tor)")
	}
}
