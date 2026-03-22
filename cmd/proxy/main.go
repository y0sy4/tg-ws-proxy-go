// TG WS Proxy - CLI application
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Flowseal/tg-ws-proxy/internal/config"
	"github.com/Flowseal/tg-ws-proxy/internal/proxy"
	"github.com/Flowseal/tg-ws-proxy/internal/telegram"
	"github.com/Flowseal/tg-ws-proxy/internal/version"
)

var appVersion = "2.0.0"

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
	dcIP := flag.String("dc-ip", "", "Target DC IPs (comma-separated, e.g., 2:149.154.167.220,4:149.154.167.220)")
	verbose := flag.Bool("v", false, "Verbose logging")
	logFile := flag.String("log-file", "", "Log file path (default: proxy.log in app dir)")
	logMaxMB := flag.Float64("log-max-mb", 5, "Max log file size in MB")
	bufKB := flag.Int("buf-kb", 256, "Socket buffer size in KB")
	poolSize := flag.Int("pool-size", 4, "WS pool size per DC")
	auth := flag.String("auth", "", "SOCKS5 authentication (username:password)")
	
	// Advanced features (for experienced users)
	httpPort := flag.Int("http-port", 0, "Enable HTTP proxy on port (0 = disabled)")
	upstreamProxy := flag.String("upstream-proxy", "", "Upstream SOCKS5/HTTP proxy (format: socks5://user:pass@host:port or http://user:pass@host:port)")
	
	showVersion := flag.Bool("version", false, "Show version")

	flag.Parse()

	if *showVersion {
		fmt.Printf("TG WS Proxy v%s\n", appVersion)
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

	// Setup logging - default to file if not specified
	logPath := *logFile
	if logPath == "" {
		// Use default log file in app config directory
		appDir := getAppDir()
		logPath = filepath.Join(appDir, "proxy.log")
	}
	logger := setupLogging(logPath, cfg.LogMaxMB, cfg.Verbose)

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
	server, err := proxy.NewServer(cfg, logger)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Auto-configure Telegram Desktop (always attempt on first run)
	log.Println("Attempting to configure Telegram Desktop...")
	username, password := "", ""
	if cfg.Auth != "" {
		parts := strings.SplitN(cfg.Auth, ":", 2)
		if len(parts) == 2 {
			username, password = parts[0], parts[1]
		}
	}
	if telegram.ConfigureProxy(cfg.Host, cfg.Port, username, password) {
		log.Println("✓ Telegram Desktop proxy configuration opened")
	} else {
		log.Println("✗ Failed to auto-configure Telegram.")
		log.Println("  Manual setup: Settings → Advanced → Connection Type → Proxy")
		log.Println("  Or open: tg://socks?server=127.0.0.1&port=1080")
	}

	// Check for updates and auto-download (non-blocking)
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
	if verbose {
		flags |= log.Lshortfile
	}

	// Ensure directory exists
	dir := filepath.Dir(logFile)
	os.MkdirAll(dir, 0755)

	// Open log file with rotation
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Warning: failed to open log file %s: %v, using stdout", logFile, err)
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
	result := []string{}
	for _, part := range splitString(s, ",") {
		part = trimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func splitString(s, sep string) []string {
	result := []string{}
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i = start - 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
