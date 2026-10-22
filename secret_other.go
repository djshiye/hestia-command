//go:build !windows

package main

import "encoding/base64"

// Development builds on other systems only: the file is kept user-readable only (0600).
func protect(plain string) (string, error) {
	return "plain:" + base64.StdEncoding.EncodeToString([]byte(plain)), nil
}

func unprotect(enc string) (string, error) {
	if len(enc) < 6 || enc[:6] != "plain:" {
		return "", errNotProtected
	}
	b, err := base64.StdEncoding.DecodeString(enc[6:])
	return string(b), err
}
