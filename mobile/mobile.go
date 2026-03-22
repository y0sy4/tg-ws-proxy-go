// Package mobile provides a Go mobile binding for the TG WS Proxy.
package mobile

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/Flowseal/tg-ws-proxy/internal/config"
	"github.com/Flowseal/tg-ws-proxy/internal/proxy"
)

var server *proxy.Server
var cancel context.CancelFunc

// Start starts the proxy server with the given configuration.
// Returns "OK" on success or an error message.
func Start(host string, port int, dcIP string, verbose bool) string {
	cfg := config.DefaultConfig()
	cfg.Host = host
	cfg.Port = port
	if dcIP != "" {
		cfg.DCIP = parseDCIP(dcIP)
	}
	cfg.Verbose = verbose

	// Setup logging to file
	logDir := getLogDir()
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Sprintf("Failed to create log dir: %v", err)
	}

	logFile := filepath.Join(logDir, "proxy.log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Sprintf("Failed to open log file: %v", err)
	}
	log.SetOutput(f)
	log.SetFlags(log.Ldate | log.Ltime)

	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())

	server = proxy.NewServer(cfg)
	if err := server.Start(ctx); err != nil {
		cancel()
		return fmt.Sprintf("Failed to start proxy: %v", err)
	}

	return "OK"
}

// Stop stops the proxy server.
func Stop() string {
	if cancel != nil {
		cancel()
	}
	if server != nil {
		server.Stop()
	}
	return "OK"
}

// GetStatus returns the current proxy status.
func GetStatus() string {
	if server == nil {
		return "Not running"
	}
	stats := server.GetStats()
	return fmt.Sprintf("Connections: %d | WS: %d | TCP: %d | Bytes Up: %d | Bytes Down: %d",
		stats.ConnectionsTotal,
		stats.ConnectionsWS,
		stats.ConnectionsTCP,
		stats.BytesUp,
		stats.BytesDown)
}

// parseDCIP parses DC IP configuration string.
func parseDCIP(s string) []string {
	if s == "" {
		return nil
	}
	result := []string{}
	for _, part := range split(s, ",") {
		trimmed := trim(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// getLogDir returns the log directory for Android.
func getLogDir() string {
	// On Android, use app-specific directory
	if dataDir := os.Getenv("ANDROID_DATA"); dataDir != "" {
		return filepath.Join(dataDir, "tg-ws-proxy")
	}
	// Fallback to temp directory
	return os.TempDir()
}

// Helper functions for string manipulation (avoiding strings package issues with gomobile)
func split(s, sep string) []string {
	result := []string{}
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
		}
	}
	result = append(result, s[start:])
	return result
}

func trim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// Dummy function to use net package (required for SOCKS5)
func init() {
	_ = net.Dial
}
