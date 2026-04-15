package updater

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const releaseAPI = "https://api.github.com/repos/AgusRdz/probe/releases/latest"

// NotifyIfUpdateAvailable checks GitHub for a newer release and prints a
// one-line notification to stderr if one is available. It returns silently
// on any network or parse error — probe should never fail to start because
// an update check failed.
func NotifyIfUpdateAvailable(currentVersion string) {
	latest, err := fetchLatestTag()
	if err != nil {
		return
	}
	if isNewer(latest, currentVersion) {
		fmt.Printf("\nA new version of probe is available: %s → %s\n"+
			"  https://github.com/AgusRdz/probe/releases/latest\n\n",
			currentVersion, latest)
	}
}

// fetchLatestTag calls the GitHub releases API and returns the tag_name of the
// latest release. Returns an error if the request fails or the response cannot
// be parsed.
func fetchLatestTag() (string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(releaseAPI)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("updater: empty tag_name in response")
	}
	return payload.TagName, nil
}

// isNewer reports whether candidate is a strictly higher version than current.
// Both values are expected in vX.Y.Z form. Falls back to a simple string
// comparison which is correct as long as components are zero-padded equally,
// but handles common vX.Y.Z tags correctly via lexicographic comparison of the
// numeric segments.
func isNewer(candidate, current string) bool {
	c := normalise(candidate)
	v := normalise(current)
	if c == "" || v == "" {
		return false
	}
	cp := splitVersion(c)
	vp := splitVersion(v)
	for i := 0; i < len(cp) && i < len(vp); i++ {
		ci := parseSegment(cp[i])
		vi := parseSegment(vp[i])
		if ci > vi {
			return true
		}
		if ci < vi {
			return false
		}
	}
	return len(cp) > len(vp)
}

func normalise(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

func splitVersion(v string) []string {
	return strings.SplitN(v, ".", 3)
}

func parseSegment(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
