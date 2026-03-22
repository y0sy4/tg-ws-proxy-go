// Package telegram provides Telegram Desktop integration utilities.
package telegram

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ConfigureProxy opens Telegram's proxy configuration URL.
// Returns true if successful, false otherwise.
func ConfigureProxy(host string, port int, username, password string) bool {
	// Build tg:// proxy URL
	url := fmt.Sprintf("tg://socks?server=%s&port=%d", host, port)
	
	if username != "" {
		url += fmt.Sprintf("&user=%s", username)
	}
	if password != "" {
		url += fmt.Sprintf("&pass=%s", password)
	}

	return openURL(url)
}

// openURL opens a URL in the default browser/application.
func openURL(url string) bool {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	case "linux":
		cmd = "xdg-open"
	default:
		return false
	}

	args = append(args, url)
	
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
