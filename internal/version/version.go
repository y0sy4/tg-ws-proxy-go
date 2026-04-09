// Package version provides version checking and update notification.
package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	CurrentVersion = "2.0.7"
	RepoURL        = "https://api.github.com/repos/y0sy4/telegram-proxy/releases/latest"
)

type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Body    string  `json:"body"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// CheckUpdate checks for new version on GitHub.
// Returns (hasUpdate, latestVersion, releaseURL, error).
func CheckUpdate() (bool, string, string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	
	resp, err := client.Get(RepoURL)
	if err != nil {
		return false, "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, "", "", err
	}

	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return false, "", "", err
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := CurrentVersion

	if compareVersions(latest, current) > 0 {
		return true, latest, release.HTMLURL, nil
	}

	return false, current, "", nil
}

// DownloadUpdate downloads the latest version for current platform.
// Returns path to downloaded file or error.
func DownloadUpdate(latestVersion string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	
	resp, err := client.Get(RepoURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return "", err
	}

	// Find asset for current platform
	assetName := getAssetName()
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return downloadAsset(client, asset.BrowserDownloadURL, assetName)
		}
	}

	return "", fmt.Errorf("no asset found for %s", runtime.GOOS)
}

func getAssetName() string {
	switch runtime.GOOS {
	case "windows":
		return "TgWsProxy_windows_amd64.exe"
	case "linux":
		return "TgWsProxy_linux_amd64"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "TgWsProxy_darwin_arm64"
		}
		return "TgWsProxy_darwin_amd64"
	default:
		return ""
	}
}

func downloadAsset(client *http.Client, url, filename string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Get executable directory
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exeDir := filepath.Dir(exe)
	
	// Download to temp file first
	tempPath := filepath.Join(exeDir, filename+".new")
	out, err := os.Create(tempPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(tempPath)
		return "", err
	}

	return tempPath, nil
}

// compareVersions compares two semantic versions.
// Returns: 1 if v1 > v2, -1 if v1 < v2, 0 if equal.
func compareVersions(v1, v2 string) int {
	parts1 := splitVersion(v1)
	parts2 := splitVersion(v2)

	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		if parts1[i] > parts2[i] {
			return 1
		}
		if parts1[i] < parts2[i] {
			return -1
		}
	}

	if len(parts1) > len(parts2) {
		return 1
	}
	if len(parts1) < len(parts2) {
		return -1
	}

	return 0
}

func splitVersion(v string) []int {
	parts := strings.Split(v, ".")
	result := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			result[i] = 0
		} else {
			result[i] = n
		}
	}
	return result
}
