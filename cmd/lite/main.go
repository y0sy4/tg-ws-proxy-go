// TG WS Proxy Lite - Minimal build for routers and low-resource devices
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/y0sy4/telegram-proxy/internal/config"
	"github.com/y0sy4/telegram-proxy/internal/proxy"
)

var appVersion = "2.0.6-lite"

func main() {
	// Parse flags - only essential ones
	port := flag.Int("port", 1080, "Listen port")
	host := flag.String("host", "127.0.0.1", "Listen host")
	dcIP := flag.String("dc-ip", "", "Target DC IPs (comma-separated)")
	verbose := flag.Bool("v", false, "Verbose logging")

	showVersion := flag.Bool("version", false, "Show version")
	testDC := flag.Bool("test-dc", false, "Test DC connectivity")

	flag.Parse()

	if *showVersion {
		fmt.Printf("TG WS Proxy Lite v%s\n", appVersion)
		os.Exit(0)
	}

	// Test DC connectivity
	if *testDC {
		testDCConnectivity(*dcIP)
		os.Exit(0)
	}

	// Load config with defaults
	cfg := config.DefaultConfig()

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
	cfg.Verbose = *verbose

	// Setup logging to stdout
	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lshortfile)

	// Create server (no upstream proxy in lite version)
	server, err := proxy.NewServer(cfg, logger, "")
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
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

	log.Printf("TG WS Proxy Lite v%s", appVersion)
	log.Printf("Listening on %s:%d", cfg.Host, cfg.Port)

	// Start server
	if err := server.Start(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}
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
func testDCConnectivity(dcIPList string) {
	fmt.Println("Testing Telegram DC connectivity...")
	fmt.Println()

	defaultDCs := []string{
		"2:149.154.167.50",
		"2:149.154.167.220",
		"4:149.154.167.91",
		"4:149.154.167.92",
		"5:91.108.56.100",
	}

	testDCs := defaultDCs
	if dcIPList != "" {
		testDCs = splitDCIP(dcIPList)
	}

	type result struct {
		dc      string
		ip      string
		latency time.Duration
		success bool
	}

	results := make([]result, 0, len(testDCs))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, dc := range testDCs {
		wg.Add(1)
		go func(dcEntry string) {
			defer wg.Done()

			parts := strings.SplitN(dcEntry, ":", 2)
			if len(parts) != 2 {
				return
			}

			dcNum, ip := parts[0], parts[1]

			start := time.Now()
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, "443"), 5*time.Second)
			latency := time.Since(start)

			if err != nil {
				return
			}
			conn.Close()

			mu.Lock()
			results = append(results, result{
				dc:      dcNum,
				ip:      ip,
				latency: latency,
				success: true,
			})
			mu.Unlock()
		}(dc)
	}

	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		if results[i].success != results[j].success {
			return results[i].success
		}
		return results[i].latency < results[j].latency
	})

	successCount := 0
	var recommended []string

	for _, r := range results {
		status := "❌"
		latencyStr := ""

		if r.success {
			status = "✅"
			latencyStr = fmt.Sprintf("%dms", r.latency.Milliseconds())
			successCount++
			recommended = append(recommended, fmt.Sprintf("%s:%s", r.dc, r.ip))
		} else {
			latencyStr = "timeout"
		}

		fmt.Printf("%s DC%-3s %-15s %s\n", status, r.dc, r.ip, latencyStr)
	}

	fmt.Println()
	fmt.Printf("Results: %d/%d reachable\n", successCount, len(results))

	if len(recommended) > 0 {
		fmt.Println()
		fmt.Printf("Recommended: --dc-ip \"%s\"\n", strings.Join(recommended, ","))
	}
}
