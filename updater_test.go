package main

import "testing"

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

func TestExpectedSum(t *testing.T) {
	sums := []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  Hestia-Setup.exe\n")
	if h, err := expectedSum(sums); err != nil || h != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("checksum not read: %v", err)
	}
	if _, err := expectedSum([]byte("abc  Other.exe\n")); err == nil {
		t.Fatal("checksum file without the program accepted")
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
