package updater

import (
	_ "embed"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

//go:embed public_key.pem
var publicKeyPEM []byte

const releaseAPI = "https://api.github.com/repos/AgusRdz/probe/releases/latest"
const releaseDownload = "https://github.com/AgusRdz/probe/releases/download"

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
			"  Run 'probe update' or visit https://github.com/AgusRdz/probe/releases/latest\n\n",
			currentVersion, latest)
	}
}

// RunUpdate downloads the latest release binary, verifies its Ed25519
// signature, and atomically replaces the running executable.
func RunUpdate(currentVersion string) error {
	fmt.Println("Checking for updates...")

	latest, err := fetchLatestTag()
	if err != nil {
		return fmt.Errorf("update: failed to fetch latest release: %w", err)
	}

	if !isNewer(latest, currentVersion) {
		fmt.Printf("Already up to date (%s).\n", currentVersion)
		return nil
	}

	fmt.Printf("Updating %s → %s\n", currentVersion, latest)

	pubKey, err := parsePublicKey(publicKeyPEM)
	if err != nil {
		return fmt.Errorf("update: failed to parse embedded public key: %w", err)
	}

	binaryName := binaryFilename()
	binaryURL := fmt.Sprintf("%s/%s/%s", releaseDownload, latest, binaryName)
	sigURL := binaryURL + ".sig"

	fmt.Printf("Downloading %s...\n", binaryName)
	binaryData, err := download(binaryURL)
	if err != nil {
		return fmt.Errorf("update: download failed: %w", err)
	}

	sigData, err := download(sigURL)
	if err != nil {
		return fmt.Errorf("update: signature download failed: %w", err)
	}

	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigData)))
	if err != nil {
		return fmt.Errorf("update: invalid signature encoding: %w", err)
	}

	if !ed25519.Verify(pubKey, binaryData, sig) {
		return fmt.Errorf("update: signature verification failed — binary may be tampered")
	}

	fmt.Println("Signature verified.")

	if err := atomicReplace(binaryData); err != nil {
		return fmt.Errorf("update: failed to replace binary: %w", err)
	}

	fmt.Printf("probe updated to %s.\n", latest)
	return nil
}

func parsePublicKey(pemData []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an Ed25519 public key")
	}
	return key, nil
}

func binaryFilename() string {
	os_ := runtime.GOOS
	arch := runtime.GOARCH
	name := fmt.Sprintf("probe-%s-%s", os_, arch)
	if os_ == "windows" {
		name += ".exe"
	}
	return name
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func atomicReplace(data []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	tmp := exe + ".tmp"
	if err := os.WriteFile(tmp, data, 0755); err != nil { //nolint:gosec
		return err
	}

	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp) //nolint:errcheck
		return err
	}
	return nil
}

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
