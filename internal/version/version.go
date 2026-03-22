// Package version provides version checking and update notification.
package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	CurrentVersion = "2.0.0"
	RepoURL        = "https://api.github.com/repos/y0sy4/tg-ws-proxy-go/releases/latest"
)

type Release struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
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
		fmt.Sscanf(p, "%d", &result[i])
	}
	return result
}
