//go:build dev

package main

import (
	"net"
	"os"
)

// Development builds only: point Setup at a simulated device and allow a
// test network that is not a private range.
func init() {
	if h := os.Getenv("HESTIA_SETUP_HOST"); h != "" {
		setupHost = h
	}
	if c := os.Getenv("HESTIA_DEV_NET"); c != "" {
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			extraLocalNet = n
		}
	}
}
