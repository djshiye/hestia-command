package main

import (
	"net"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	const key = "7f0a3b1c-2d4e-4f60-8a9b-0c1d2e3f4a5b"
	data, iv, err := encryptPayload(key, []byte(`{"switch":"on"}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := decryptPayload(key, data, iv)
	if err != nil || string(got) != `{"switch":"on"}` {
		t.Fatalf("round trip = %q, %v", got, err)
	}
	if _, err := decryptPayload("00000000-0000-0000-0000-000000000000", data, iv); err == nil {
		t.Fatal("a wrong key must not decrypt")
	}
}

func TestIsLocalIP(t *testing.T) {
	for ip, want := range map[string]bool{"192.168.1.20": true, "10.0.0.5": true, "169.254.1.1": true, "8.8.8.8": false, "": false} {
		if got := isLocalIP(net.ParseIP(ip)); got != want {
			t.Errorf("isLocalIP(%q) = %v, want %v", ip, got, want)
		}
	}
}

func TestHotkeyChoicesAreValid(t *testing.T) {
	for _, k := range hotkeyChoices() {
		if _, ok := hotkeyVK(k); !ok {
			t.Errorf("%s has no key code", k)
		}
	}
	if _, ok := hotkeyVK("Ctrl+Alt+F13"); ok {
		t.Error("F13 should be rejected")
	}
}
