package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type instanceInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

func appURL(port int, token string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/#t=%s", port, token)
}

// existingInstance returns the address of an already running Hestia, if any.
func existingInstance(path string) (*instanceInfo, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var inf instanceInfo
	if json.Unmarshal(raw, &inf) != nil || inf.Port == 0 {
		return nil, false
	}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(inf.Port)+"/api/ping", nil)
	req.Header.Set("X-Hestia-Token", inf.Token)
	c := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := c.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	return &inf, resp.StatusCode == 200 && string(body) == "hestia"
}

func main() {
	noWindow := flag.Bool("no-window", false, "do not open the app window")
	minimized := flag.Bool("minimized", false, "start in the background without a window (used when starting with Windows)")
	doUninstall := flag.Bool("uninstall", false, "remove Hestia from this PC")
	quiet := flag.Bool("quiet", false, "with -uninstall: no questions, keep saved keys")
	cleanup := flag.String("cleanup", "", "internal: remove the install folder after uninstalling")
	fixedPort := flag.Int("port", 0, "listen on this local port (default: random)")
	flag.Parse()
	if *cleanup != "" {
		time.Sleep(time.Second)
		cleanupInstallDir(*cleanup)
		return
	}
	if *doUninstall {
		uninstall(*quiet)
		return
	}
	if *minimized {
		*noWindow = true
	}

	dir, err := appDir()
	if err != nil {
		fatal("Could not create the settings folder", err)
	}
	logPath := filepath.Join(dir, "hestia.log")
	if st, err := os.Stat(logPath); err == nil && st.Size() > 1<<20 {
		_ = os.Rename(logPath, logPath+".old")
	}
	if lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		log.SetOutput(io.MultiWriter(lf, os.Stderr))
	}
	log.Printf("Hestia %s starting", appVersion)

	instPath := filepath.Join(dir, "instance.json")
	if inf, ok := existingInstance(instPath); ok {
		log.Println("already running; opening its window")
		if !*noWindow {
			openWindow(appURL(inf.Port, inf.Token))
		}
		return
	}

	store, err := OpenStore()
	if err != nil {
		fatal("Could not read Hestia's settings", err)
	}
	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		fatal("Could not create a session token", err)
	}
	token := hex.EncodeToString(tok)

	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(*fixedPort))
	if err != nil {
		fatal("Could not open Hestia's local window port", err)
	}
	app := NewApp(store, token)
	app.port = ln.Addr().(*net.TCPAddr).Port

	raw, _ := json.Marshal(instanceInfo{Port: app.port, Token: token})
	_ = os.WriteFile(instPath, raw, 0o600)
	defer os.Remove(instPath)

	refreshAutostartPath()
	cleanupOldUpdate()
	app.browser.Start()
	app.applyHotkeys()
	go app.monitor()
	go app.watchdog()
	go app.updater.run()

	srv := &http.Server{
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Println("server:", err)
		}
	}()
	log.Printf("listening on 127.0.0.1:%d", app.port)
	if !*noWindow {
		openWindow(appURL(app.port, token))
	} else {
		fmt.Println(appURL(app.port, token))
	}
	<-app.quit
	_ = srv.Close()
	os.Remove(instPath)
	app.mu.Lock()
	next, nextArgs := app.relaunch, app.relaunchArgs
	app.mu.Unlock()
	if next != "" {
		if err := launchDetached(next, nextArgs...); err != nil {
			log.Println("could not start the installed copy:", err)
		}
	}
	log.Println("Hestia stopped")
}
