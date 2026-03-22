// Package telegram provides Telegram Desktop integration utilities.
package telegram

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ConfigureProxy opens Telegram's SOCKS5 proxy configuration URL.
// Returns true if successful, false otherwise.
func ConfigureProxy(host string, port int, username, password string) bool {
	return ConfigureProxyWithType(host, port, username, password, "", "socks5")
}

// ConfigureProxyWithType opens Telegram's proxy configuration URL with specified type.
// proxyType: "socks5" or "mtproto"
// For MTProto, provide secret parameter
// Note: HTTP proxy is NOT supported by Telegram Desktop via tg:// URLs
// Returns true if successful, false otherwise.
func ConfigureProxyWithType(host string, port int, username, password, secret, proxyType string) bool {
	var proxyURL string
	
	switch proxyType {
	case "mtproto":
		// MTProto proxy format: tg://proxy?server=host&port=port&secret=secret
		if secret == "" {
			secret = "ee000000000000000000000000000000" // default dummy secret
		}
		proxyURL = fmt.Sprintf("tg://proxy?server=%s&port=%d&secret=%s", host, port, secret)
	default:
		// SOCKS5 proxy format: tg://socks?server=host&port=port
		// This is the only type our local proxy supports
		proxyURL = fmt.Sprintf("tg://socks?server=%s&port=%d", host, port)
	}
	
	// Open URL using system default handler
	return openURL(proxyURL)
}

// openURL opens a URL in the default browser/application.
func openURL(url string) bool {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		// Use rundll32 to open URL - more reliable for protocol handlers
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	default:
		return false
	}

	err := exec.Command(cmd, args...).Start()
	return err == nil
}

// IsTelegramRunning checks if Telegram Desktop is running.
func IsTelegramRunning() bool {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "tasklist"
		args = []string{"/FI", "IMAGENAME eq Telegram.exe"}
	case "darwin":
		cmd = "pgrep"
		args = []string{"-x", "Telegram"}
	case "linux":
		cmd = "pgrep"
		args = []string{"-x", "telegram-desktop"}
	default:
		return false
	}

	output, err := exec.Command(cmd, args...).Output()
	if err != nil {
		return false
	}

	return len(strings.TrimSpace(string(output))) > 0
}

// GetTelegramPath returns the path to Telegram Desktop executable.
func GetTelegramPath() string {
	switch runtime.GOOS {
	case "windows":
		// Common installation paths
		paths := []string{
			"%APPDATA%\\Telegram Desktop\\Telegram.exe",
			"%LOCALAPPDATA%\\Programs\\Telegram Desktop\\Telegram.exe",
			"%PROGRAMFILES%\\Telegram Desktop\\Telegram.exe",
		}
		for _, path := range paths {
			cmd := exec.Command("cmd", "/c", "echo", path)
			output, err := cmd.Output()
			if err == nil {
				return strings.TrimSpace(string(output))
			}
		}
		return ""
	case "darwin":
		return "/Applications/Telegram.app"
	case "linux":
		return "telegram-desktop"
	default:
		return ""
	}
}
