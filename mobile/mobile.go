// Package mobile provides a Go mobile binding for the TG WS Proxy.
package mobile

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/y0sy4/telegram-proxy/internal/config"
	"github.com/y0sy4/telegram-proxy/internal/proxy"
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
	logger := log.New(f, "", log.Ldate|log.Ltime)

	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())

	server, err = proxy.NewServer(cfg, logger, "")
	if err != nil {
		cancel()
		return fmt.Sprintf("Failed to create server: %v", err)
	}

	go func() {
		if err := server.Start(ctx); err != nil {
			cancel()
		}
	}()

	return "OK"
}

// Stop stops the proxy server.
func Stop() string {
	if cancel != nil {
		cancel()
	}
	return "OK"
}

// GetStatus returns the current proxy status.
func GetStatus() string {
	if server == nil {
		return "Not running"
	}
	return "Running" // Simplified for mobile
}

// parseDCIP parses DC IP configuration string.
func parseDCIP(s string) []string {
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

// getLogDir returns the log directory for Android.
func getLogDir() string {
	// On Android, use app-specific directory
	if dataDir := os.Getenv("ANDROID_DATA"); dataDir != "" {
		return filepath.Join(dataDir, "tg-ws-proxy")
	}
	// Fallback to temp directory
	return os.TempDir()
}

// Dummy function to use net package (required for SOCKS5)
func init() {
	_ = net.Dial
}
