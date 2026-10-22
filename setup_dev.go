//go:build dev

package main

import (
	"net"
	"net/http"
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

// devTrayClick lets tests poke the tray icon on platforms that have one.
var devTrayClick = func(a *App, event string) error { return nil }

func init() {
	devRoutes = func(a *App, api func(string, func(http.ResponseWriter, *http.Request) (any, error))) {
		api("POST /api/dev/tray", func(_ http.ResponseWriter, r *http.Request) (any, error) {
			return nil, devTrayClick(a, r.URL.Query().Get("event"))
		})
	}
}
