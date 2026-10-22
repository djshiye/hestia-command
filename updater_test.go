package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.2.0", "1.1.1", true}, {"1.1.1", "1.1.1", false}, {"1.1.0", "1.1.1", false},
		{"2.0.0", "1.9.9", true}, {"1.10.0", "1.9.0", true}, {"v1.2.0", "1.1.1", true},
	}
	for _, c := range cases {
		got, err := isNewer(c.a, c.b)
		if err != nil || got != c.want {
			t.Errorf("isNewer(%s,%s)=%v,%v want %v", c.a, c.b, got, err, c.want)
		}
	}
	if _, err := isNewer("1.2", "1.1.1"); err == nil {
		t.Error("malformed release version must be rejected")
	}
	if n, _ := isNewer("1.2.0", "dev"); n {
		t.Error("dev builds never update")
	}
}

func TestVerifySums(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	saved := updatePublicKey
	updatePublicKey = base64.StdEncoding.EncodeToString(pub)
	defer func() { updatePublicKey = saved }()
	sums := []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  Hestia-Setup.exe\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, append([]byte(signaturePrefix), sums...))))
	if h, err := verifySums(sums, sig); err != nil || h != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("good signature rejected: %v", err)
	}
	if _, err := verifySums(append(sums, '#'), sig); err == nil {
		t.Fatal("tampered checksum file accepted")
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	bad := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(other, append([]byte(signaturePrefix), sums...))))
	if _, err := verifySums(sums, bad); err == nil {
		t.Fatal("signature from another key accepted")
	}
	unprefixed := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)))
	if _, err := verifySums(sums, unprefixed); err == nil {
		t.Fatal("signature without the release prefix accepted")
	}
}

func TestUpdateURLPolicy(t *testing.T) {
	for addr, ok := range map[string]bool{
		"https://api.github.com/x": true, "https://objects.githubusercontent.com/x": true,
		"http://api.github.com/x": false, "https://evil.example/x": false, "https://api.github.com.evil.example/x": false,
	} {
		u, _ := parseURL(addr)
		if err := checkUpdateURL(u); (err == nil) != ok {
			t.Errorf("%s: allowed=%v want %v", addr, err == nil, ok)
		}
	}
}
