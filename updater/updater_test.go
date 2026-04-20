package updater

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		candidate string
		current   string
		want      bool
	}{
		{"v0.6.0", "v0.5.0", true},
		{"v0.5.0", "v0.5.0", false},
		{"v0.4.9", "v0.5.0", false},
		{"v1.0.0", "v0.9.9", true},
		{"v0.5.1", "v0.5.0", true},
		{"v0.5.0", "v0.5.1", false},
		// stripped v prefix
		{"0.6.0", "0.5.0", true},
		// empty strings → never newer
		{"", "v0.5.0", false},
		{"v0.6.0", "", false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_vs_%s", tc.candidate, tc.current), func(t *testing.T) {
			got := isNewer(tc.candidate, tc.current)
			if got != tc.want {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
			}
		})
	}
}

func TestParsePublicKey_Valid(t *testing.T) {
	pub, _, err := generateTestKeyPEM(t)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	key, err := parsePublicKey(pub)
	if err != nil {
		t.Fatalf("parsePublicKey: %v", err)
	}
	if len(key) != ed25519.PublicKeySize {
		t.Errorf("key size = %d, want %d", len(key), ed25519.PublicKeySize)
	}
}

func TestParsePublicKey_InvalidPEM(t *testing.T) {
	_, err := parsePublicKey([]byte("not a pem block"))
	if err == nil {
		t.Fatal("expected error for invalid PEM, got nil")
	}
}

func TestParsePublicKey_WrongKeyType(t *testing.T) {
	// Encode raw bytes that are valid DER but not a PKIX public key.
	badPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("notvalidpkix")})
	_, err := parsePublicKey(badPEM)
	if err == nil {
		t.Fatal("expected error for invalid PKIX bytes, got nil")
	}
}

func TestBinaryFilename(t *testing.T) {
	name := binaryFilename()
	if name == "" {
		t.Fatal("binaryFilename returned empty string")
	}
	// Must contain os and arch
	if len(name) < 7 { // "probe-" + at least one char for os
		t.Errorf("binaryFilename = %q, looks too short", name)
	}
}

func TestPsEscape(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`C:\Users\probe.exe`, `C:\Users\probe.exe`},
		{`C:\O'Brian\probe.exe`, `C:\O''Brian\probe.exe`},
		{`it's a 'test'`, `it''s a ''test''`},
		{`no quotes here`, `no quotes here`},
	}
	for _, tc := range cases {
		got := psEscape(tc.input)
		if got != tc.want {
			t.Errorf("psEscape(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// generateTestKeyPEM creates a fresh Ed25519 key pair and returns PEM-encoded
// public and private keys for use in tests.
func generateTestKeyPEM(t *testing.T) (pubPEM []byte, privKey ed25519.PrivateKey, err error) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, nil, err
	}
	pubPEM = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return pubPEM, priv, nil
}
