package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

const (
	githubRepo   = "jake-landersweb/previewctl"
	checkTimeout = 2 * time.Second
)

var (
	dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24"))
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399"))
)

// CheckForUpdate queries GitHub for the latest release and prints
// an update notice to stderr if a newer version exists.
// Fails silently on network errors or dev builds.
func CheckForUpdate() {
	current := Get()
	latest := fetchLatestVersion()
	if latest == "" {
		return
	}

	printUpdateStatus(current, latest)
}

// PrintVersionWithUpdateCheck prints the current version and whether
// an update is available. Used by the `version` subcommand.
func PrintVersionWithUpdateCheck() {
	current := Get()
	latest := fetchLatestVersion()

	if latest == "" {
		fmt.Fprintf(os.Stderr, "  %s %s\n", dim.Render("Latest:"), dim.Render("unable to check"))
		return
	}

	currentClean := strings.TrimPrefix(current, "v")
	latestClean := strings.TrimPrefix(latest, "v")

	if current == "dev" {
		fmt.Fprintf(os.Stderr, "  %s %s\n", dim.Render("Latest:"), green.Render("v"+latestClean))
	} else if latestClean == currentClean || !isNewer(latestClean, currentClean) {
		fmt.Fprintf(os.Stderr, "  %s %s\n", dim.Render("Latest:"), green.Render("v"+latestClean+" (up to date)"))
	} else {
		fmt.Fprintf(os.Stderr, "  %s %s\n", dim.Render("Latest:"), yellow.Render("v"+latestClean))
		fmt.Fprintf(os.Stderr, "\n  %s %s → %s\n",
			yellow.Render("Update available:"),
			dim.Render("v"+currentClean),
			green.Render("v"+latestClean),
		)
		fmt.Fprintf(os.Stderr, "  %s\n",
			dim.Render("go install github.com/"+githubRepo+"/src/cmd/previewctl@latest"),
		)
	}
}

// printUpdateStatus prints an update notice only if a newer version exists.
func printUpdateStatus(current, latest string) {
	currentClean := strings.TrimPrefix(current, "v")
	latestClean := strings.TrimPrefix(latest, "v")

	if latestClean != "" && latestClean != currentClean && isNewer(latestClean, currentClean) {
		fmt.Fprintf(os.Stderr, "\n%s %s → %s %s\n",
			yellow.Render("A new version of previewctl is available:"),
			dim.Render("v"+currentClean),
			green.Render("v"+latestClean),
			dim.Render("(go install github.com/"+githubRepo+"/src/cmd/previewctl@latest)"),
		)
	}
}

// fetchLatestVersion queries the GitHub releases API. Returns "" on any error.
func fetchLatestVersion() string {
	client := &http.Client{Timeout: checkTimeout}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	resp, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ""
	}

	return release.TagName
}

// isNewer returns true if latest is a newer semver than current.
func isNewer(latest, current string) bool {
	lParts := parseSemver(latest)
	cParts := parseSemver(current)
	if lParts == nil || cParts == nil {
		return latest > current
	}
	for i := range 3 {
		if lParts[i] > cParts[i] {
			return true
		}
		if lParts[i] < cParts[i] {
			return false
		}
	}
	return false
}

func parseSemver(v string) []int {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	result := make([]int, 3)
	for i, p := range parts {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return nil
			}
			n = n*10 + int(c-'0')
		}
		result[i] = n
	}
	return result
}
