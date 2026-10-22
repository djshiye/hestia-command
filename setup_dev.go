//go:build dev

package main

import (
	"net"
	"net/url"
	"os"
)

// Development builds only: point Setup at a simulated device and allow a
// test network that is not a private range.
func init() {
	if h := os.Getenv("HESTIA_SETUP_HOST"); h != "" {
		setupHost = h
	}
	if b := os.Getenv("HESTIA_UPDATE_BASE"); b != "" {
		updateAPIBase = b
		updateAllowHTTP = true
		if u, err := url.Parse(b); err == nil {
			updateHosts[u.Hostname()] = true
		}
	}
	if k := os.Getenv("HESTIA_UPDATE_PUBKEY"); k != "" {
		updatePublicKey = k
	}
	if c := os.Getenv("HESTIA_DEV_NET"); c != "" {
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			extraLocalNet = n
		}
	}
}
