package main

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

// appVersion is set at build time with -ldflags "-X main.appVersion=...".
var appVersion = "dev"

type Event struct {
	T      time.Time `json:"t"`
	Kind   string    `json:"kind"`
	Device string    `json:"device,omitempty"`
	Text   string    `json:"text"`
}

type liveState struct {
	Switch   string          `json:"switch,omitempty"`
	Outlets  []outletState   `json:"outlets,omitempty"`
	Raw      json.RawMessage `json:"-"`
	Updated  time.Time       `json:"updated"`
	Online   bool            `json:"online"`
	Since    time.Time       `json:"since"`
	Drops    int             `json:"drops"`
	UpSecs   float64         `json:"-"`
	AllSecs  float64         `json:"-"`
	PortOK   *bool           `json:"portOk,omitempty"`
	DecryptE string          `json:"decryptError,omitempty"`
}

type outletState struct {
	Outlet int    `json:"outlet"`
	Switch string `json:"switch"`
}

type App struct {
	store   *Store
	browser *Browser
	hotkeys *Hotkeys
	token   string
	port    int
	started time.Time

	mu        sync.Mutex
	live      map[string]*liveState
	events    []Event
	lastBeat  time.Time
	hotkeyErr map[string]string
	quit      chan struct{}
	quitOnce  sync.Once
	relaunch  string // program to start after this one exits
}

func (a *App) stop() { a.quitOnce.Do(func() { close(a.quit) }) }

func NewApp(store *Store, token string) *App {
	a := &App{store: store, token: token, started: time.Now(), live: map[string]*liveState{}, quit: make(chan struct{})}
	a.browser = NewBrowser(a.onDeviceChange)
	a.hotkeys = NewHotkeys(a.onHotkey)
	return a
}

func (a *App) addEvent(kind, device, text string) {
	device, text = cleanLabel(device, 32), cleanLabel(text, 300)
	a.mu.Lock()
	a.events = append(a.events, Event{T: time.Now(), Kind: kind, Device: device, Text: text})
	if len(a.events) > 2000 {
		a.events = a.events[len(a.events)-2000:]
	}
	a.mu.Unlock()
	log.Printf("[%s] %s %s", kind, device, text)
}

func (a *App) liveFor(id string) *liveState {
	l := a.live[id]
	if l == nil {
		l = &liveState{}
		a.live[id] = l
	}
	return l
}

// onDeviceChange runs when an announcement brings new state for a device.
func (a *App) onDeviceChange(id string) {
	d, ok := a.browser.Device(id)
	if !ok {
		return
	}
	if d.Goodbye {
		a.addEvent("offline", id, "Switch said goodbye on the network")
		return
	}
	a.decodeState(d)
}

func (a *App) decodeState(d Discovered) {
	data, iv := d.payload()
	if data == "" {
		return
	}
	var plain []byte
	if d.Encrypted {
		key, ok := a.store.DeviceKey(d.ID)
		if !ok {
			return
		}
		p, err := decryptPayload(key, data, iv)
		a.mu.Lock()
		l := a.liveFor(d.ID)
		if err != nil {
			l.DecryptE = err.Error()
			a.mu.Unlock()
			return
		}
		l.DecryptE = ""
		a.mu.Unlock()
		plain = p
		a.markVerified(d.ID, "its live status decrypted correctly")
	} else {
		plain = []byte(data)
	}
	var st struct {
		Switch   string        `json:"switch"`
		Switches []outletState `json:"switches"`
	}
	if json.Unmarshal(plain, &st) != nil {
		return
	}
	a.mu.Lock()
	l := a.liveFor(d.ID)
	changed := l.Switch != "" && st.Switch != "" && l.Switch != st.Switch
	l.Switch, l.Outlets, l.Raw, l.Updated = st.Switch, st.Switches, plain, time.Now()
	a.mu.Unlock()
	if changed {
		a.addEvent("update", d.ID, "Switch reports it is "+strings.ToUpper(st.Switch))
	}
}

func (a *App) markVerified(id, why string) {
	already := false
	_ = a.store.Update(func(c *Config) error {
		if dc := c.device(id); dc != nil {
			already = dc.Verified
			dc.Verified = true
		}
		return nil
	})
	if !already {
		a.addEvent("info", id, "Key confirmed: "+why)
	}
}

// deviceAddr picks the best known address for a device.
func (a *App) deviceAddr(id string) (ip string, port int, encrypted bool, ok bool) {
	if d, found := a.browser.Device(id); found && !d.Goodbye {
		return d.IP, d.Port, d.Encrypted, true
	}
	c := a.store.Get()
	if dc := c.device(id); dc != nil && dc.ManualIP != "" {
		return dc.ManualIP, 8081, true, true
	}
	return "", 0, false, false
}

func (a *App) setSwitch(id string, on bool, outlet *int) error {
	ip, port, encrypted, ok := a.deviceAddr(id)
	if !ok {
		return errors.New("this switch has not been found on the network yet")
	}
	key, hasKey := a.store.DeviceKey(id)
	if encrypted && !hasKey {
		return errors.New("no key saved for this switch yet; run Setup first")
	}
	if !encrypted {
		key = ""
	}
	state := map[bool]string{true: "on", false: "off"}[on]
	var err error
	if outlet != nil {
		err = sendCommand(ip, port, id, key, "switches", map[string]any{"switches": []outletState{{Outlet: *outlet, Switch: state}}})
	} else {
		err = sendCommand(ip, port, id, key, "switch", map[string]any{"switch": state})
	}
	if err != nil {
		a.addEvent("error", id, "Turn "+state+" failed: "+err.Error())
		return err
	}
	a.markVerified(id, "the switch accepted an encrypted command")
	a.mu.Lock()
	l := a.liveFor(id)
	if outlet != nil {
		for i := range l.Outlets {
			if l.Outlets[i].Outlet == *outlet {
				l.Outlets[i].Switch = state
			}
		}
	} else {
		l.Switch = state
	}
	l.Updated = time.Now()
	a.mu.Unlock()
	a.addEvent("command", id, "Turned "+state)
	go func() { time.Sleep(700 * time.Millisecond); a.browser.Query() }()
	return nil
}

func (a *App) onHotkey(id string) {
	a.mu.Lock()
	cur := a.liveFor(id).Switch
	a.mu.Unlock()
	_ = a.setSwitch(id, cur != "on", nil)
}

func (a *App) applyHotkeys() {
	c := a.store.Get()
	want := map[string]string{}
	for _, d := range c.Devices {
		if d.Hotkey != "" {
			want[d.ID] = d.Hotkey
		}
	}
	errs := a.hotkeys.Set(want)
	a.mu.Lock()
	a.hotkeyErr = errs
	a.mu.Unlock()
}

// monitor keeps asking for announcements and checks each switch's port,
// recording every time a switch drops off or comes back.
func (a *App) monitor() {
	for _, d := range []time.Duration{0, time.Second, 3 * time.Second} {
		time.Sleep(d)
		a.browser.Query()
	}
	tick := time.NewTicker(15 * time.Second)
	last := time.Now()
	n := 0
	for {
		select {
		case <-a.quit:
			return
		case <-tick.C:
		}
		n++
		if n%2 == 0 {
			a.browser.Query()
		}
		now := time.Now()
		elapsed := now.Sub(last).Seconds()
		last = now
		ids := map[string]bool{}
		for _, d := range a.browser.Devices() {
			ids[d.ID] = true
		}
		for _, d := range a.store.Get().Devices {
			ids[d.ID] = true
		}
		for id := range ids {
			ip, port, _, ok := a.deviceAddr(id)
			portOK := ok && tcpReachable(ip, port)
			d, seen := a.browser.Device(id)
			recent := seen && !d.Goodbye && time.Since(d.LastSeen) < 3*time.Minute
			online := portOK || (recent && time.Since(d.LastSeen) < 45*time.Second)
			a.mu.Lock()
			l := a.liveFor(id)
			l.PortOK = &portOK
			l.AllSecs += elapsed
			if online {
				l.UpSecs += elapsed
			}
			was := l.Online
			first := l.Since.IsZero()
			if online != was || first {
				l.Online = online
				l.Since = now
				if !online && !first {
					l.Drops++
				}
			}
			a.mu.Unlock()
			if online != was && !first {
				if online {
					a.addEvent("online", id, "Switch is back on the network")
				} else {
					a.addEvent("offline", id, "Switch stopped answering on the network")
				}
			}
		}
	}
}

func tcpReachable(ip string, port int) bool {
	if ip == "" || !isLocalIP(net.ParseIP(ip)) {
		return false
	}
	if port == 0 {
		port = 8081
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// ---------- HTTP API ----------

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	static := http.FileServer(http.FS(sub))
	mux.Handle("GET /", static)
	api := func(pattern string, h func(w http.ResponseWriter, r *http.Request) (any, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Hestia-Token")), []byte(a.token)) != 1 {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
			out, err := h(w, r)
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			if out == nil {
				out = map[string]bool{"ok": true}
			}
			_ = json.NewEncoder(w).Encode(out)
		})
	}
	api("GET /api/state", a.apiState)
	api("POST /api/test", a.apiTest)
	api("POST /api/scan", func(http.ResponseWriter, *http.Request) (any, error) { a.browser.Query(); return nil, nil })
	api("POST /api/setup/read", a.apiSetupRead)
	api("POST /api/setup/manual", a.apiSetupManual)
	api("POST /api/device/{id}/switch", a.apiSwitch)
	api("POST /api/device/{id}/settings", a.apiDeviceSettings)
	api("POST /api/device/{id}/remove", a.apiRemove)
	api("POST /api/device/{id}/reveal", func(_ http.ResponseWriter, r *http.Request) (any, error) {
		id := r.PathValue("id")
		key, ok := a.store.DeviceKey(id)
		if !ok {
			return nil, errors.New("no key is saved for this switch")
		}
		a.addEvent("info", id, "Key shown on screen so it can be copied to another PC")
		return map[string]string{"id": id, "key": key}, nil
	})
	api("POST /api/settings", a.apiSettings)
	api("POST /api/heartbeat", func(http.ResponseWriter, *http.Request) (any, error) {
		a.mu.Lock()
		a.lastBeat = time.Now()
		a.mu.Unlock()
		return nil, nil
	})
	api("POST /api/quit", func(http.ResponseWriter, *http.Request) (any, error) {
		go func() { time.Sleep(300 * time.Millisecond); a.stop() }()
		return nil, nil
	})
	api("GET /api/log", a.apiLog)
	api("POST /api/install", func(http.ResponseWriter, *http.Request) (any, error) {
		target, err := installSelf(a.port)
		if err != nil {
			a.addEvent("error", "", "Install failed: "+err.Error())
			return nil, err
		}
		a.addEvent("info", "", "Installed to "+target)
		a.mu.Lock()
		a.relaunch = target
		a.mu.Unlock()
		go func() { time.Sleep(500 * time.Millisecond); a.stop() }()
		return map[string]string{"path": target}, nil
	})
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Hestia-Token")), []byte(a.token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("hestia"))
	})
	host := "127.0.0.1:" + strconv.Itoa(a.port)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only answer requests addressed to us, to defeat DNS-rebinding pages.
		if r.Host != host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

type deviceView struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	IP         string        `json:"ip"`
	Port       int           `json:"port"`
	Type       string        `json:"type"`
	Encrypted  bool          `json:"encrypted"`
	Discovered bool          `json:"discovered"`
	LastSeen   *time.Time    `json:"lastSeen,omitempty"`
	HasKey     bool          `json:"hasKey"`
	Verified   bool          `json:"verified"`
	Saved      bool          `json:"saved"`
	Hotkey     string        `json:"hotkey"`
	HotkeyErr  string        `json:"hotkeyError,omitempty"`
	ManualIP   string        `json:"manualIp,omitempty"`
	Switch     string        `json:"switch"`
	Outlets    []outletState `json:"outlets,omitempty"`
	Online     bool          `json:"online"`
	Since      *time.Time    `json:"since,omitempty"`
	Drops      int           `json:"drops"`
	Uptime     *float64      `json:"uptime,omitempty"`
	PortOK     *bool         `json:"portOk,omitempty"`
	DecryptErr string        `json:"decryptError,omitempty"`
}

func (a *App) views() []deviceView {
	cfg := a.store.Get()
	byID := map[string]*deviceView{}
	var order []string
	for _, d := range a.browser.Devices() {
		v := &deviceView{ID: d.ID, IP: d.IP, Port: d.Port, Type: d.Type, Encrypted: d.Encrypted, Discovered: true}
		t := d.LastSeen
		v.LastSeen = &t
		byID[d.ID] = v
		order = append(order, d.ID)
	}
	for _, dc := range cfg.Devices {
		v := byID[dc.ID]
		if v == nil {
			v = &deviceView{ID: dc.ID, Encrypted: true, IP: dc.ManualIP}
			byID[dc.ID] = v
			order = append(order, dc.ID)
		}
		v.Saved, v.Name, v.HasKey, v.Verified, v.Hotkey, v.ManualIP = true, dc.Name, dc.KeyEnc != "", dc.Verified, dc.Hotkey, dc.ManualIP
	}
	a.mu.Lock()
	for id, v := range byID {
		if l := a.live[id]; l != nil {
			v.Switch, v.Outlets, v.Online, v.Drops, v.PortOK, v.DecryptErr = l.Switch, l.Outlets, l.Online, l.Drops, l.PortOK, l.DecryptE
			if !l.Since.IsZero() {
				s := l.Since
				v.Since = &s
			} else {
				v.Online = v.Discovered
			}
			if l.AllSecs > 0 {
				u := l.UpSecs / l.AllSecs * 100
				v.Uptime = &u
			}
		} else if v.Discovered {
			v.Online = true
		}
		v.HotkeyErr = a.hotkeyErr[id]
	}
	a.mu.Unlock()
	sort.SliceStable(order, func(i, j int) bool {
		vi, vj := byID[order[i]], byID[order[j]]
		if vi.Saved != vj.Saved {
			return vi.Saved
		}
		return order[i] < order[j]
	})
	out := make([]deviceView, 0, len(order))
	for _, id := range order {
		v := byID[id]
		if v.Name == "" {
			v.Name = "Sonoff " + id
		}
		out = append(out, *v)
	}
	return out
}

func (a *App) apiState(http.ResponseWriter, *http.Request) (any, error) {
	packets, ewPackets, notes, listening := a.browser.Stats()
	cfg := a.store.Get()
	a.mu.Lock()
	ev := a.events
	if len(ev) > 150 {
		ev = ev[len(ev)-150:]
	}
	ev = append([]Event{}, ev...)
	a.mu.Unlock()
	return map[string]any{
		"version":            appVersion,
		"started":            a.started,
		"interfaces":         a.browser.Interfaces(),
		"packets":            packets,
		"ewelinkPackets":     ewPackets,
		"notes":              notes,
		"listening":          listening,
		"devices":            a.views(),
		"testPassed":         cfg.TestPassed,
		"lastTest":           cfg.LastTest,
		"background":         cfg.Background,
		"autostart":          autostartEnabled(),
		"install":            a.installView(),
		"autostartSupported": autostartSupported(),
		"events":             ev,
		"hotkeysSupported":   a.hotkeys.Supported(),
		"hotkeyChoices":      hotkeyChoices(),
	}, nil
}

type testCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass, fail, warn
	Detail string `json:"detail"`
}

func (a *App) apiTest(http.ResponseWriter, *http.Request) (any, error) {
	a.addEvent("info", "", "Network test started")
	for i := 0; i < 4; i++ {
		a.browser.Query()
		time.Sleep(2 * time.Second)
	}
	var checks []testCheck
	ifs := a.browser.Interfaces()
	if len(ifs) == 0 {
		checks = append(checks, testCheck{"Network adapter", "fail", "This PC has no active network connection with an IPv4 address."})
	} else {
		checks = append(checks, testCheck{"Network adapter", "pass", strings.Join(ifs, ", ")})
	}
	packets, ewPackets, _, listening := a.browser.Stats()
	switch {
	case packets > 0:
		checks = append(checks, testCheck{"Local discovery traffic", "pass", fmt.Sprintf("Received %d discovery messages from devices on this network.", packets)})
	case listening == 0:
		checks = append(checks, testCheck{"Local discovery traffic", "warn", "Could not listen for announcements. Only direct replies can be received."})
	default:
		checks = append(checks, testCheck{"Local discovery traffic", "warn", "No discovery messages seen yet. If Windows asked about the firewall, choose Allow."})
	}
	devs := a.browser.Devices()
	passed := false
	if len(devs) == 0 || ewPackets == 0 {
		checks = append(checks, testCheck{"Sonoff local mode", "fail", "No Sonoff switch announced local control. Make sure this PC is on the same Wi-Fi as the switch, not a guest network."})
	}
	for _, d := range devs {
		reach := tcpReachable(d.IP, d.Port)
		checks = append(checks, testCheck{"Switch " + d.ID + " found", "pass", fmt.Sprintf("At %s, port %d, type %q, encrypted: %v", d.IP, d.Port, d.Type, d.Encrypted)})
		if reach {
			passed = true
			checks = append(checks, testCheck{"Switch " + d.ID + " accepts local connections", "pass", "Port " + strconv.Itoa(d.Port) + " is open on the switch."})
		} else {
			checks = append(checks, testCheck{"Switch " + d.ID + " accepts local connections", "fail", "The switch did not accept a connection on its local port."})
		}
		if _, ok := a.store.DeviceKey(d.ID); ok {
			a.decodeState(d)
			cfg := a.store.Get()
			if dc := cfg.device(d.ID); dc != nil && dc.Verified {
				checks = append(checks, testCheck{"Key for " + d.ID, "pass", "The saved key decrypts this switch's status."})
			} else {
				checks = append(checks, testCheck{"Key for " + d.ID, "warn", "A key is saved but has not been confirmed yet."})
			}
		}
	}
	_ = a.store.Update(func(c *Config) error {
		c.LastTest = time.Now()
		if passed {
			c.TestPassed = true
		}
		return nil
	})
	if passed {
		a.addEvent("info", "", "Network test passed")
	} else {
		a.addEvent("error", "", "Network test did not find a usable switch")
	}
	return map[string]any{"passed": passed, "checks": checks}, nil
}

var (
	reDeviceID = regexp.MustCompile(`^[0-9a-fA-F]{10}$`)
	reKey      = regexp.MustCompile(`^[0-9a-fA-F-]{16,64}$`)
)

func (a *App) saveKey(id, key, how string) error {
	id = strings.ToLower(strings.TrimSpace(id))
	key = strings.TrimSpace(key)
	if !reDeviceID.MatchString(id) {
		return errors.New("the device ID should be 10 letters and numbers, like 1000a1b2c3")
	}
	if !reKey.MatchString(key) {
		return errors.New("that does not look like a Sonoff device key")
	}
	enc, err := protect(key)
	if err != nil {
		return fmt.Errorf("could not protect the key: %w", err)
	}
	err = a.store.Update(func(c *Config) error {
		if dc := c.device(id); dc != nil {
			dc.KeyEnc, dc.Verified = enc, false
			return nil
		}
		c.Devices = append(c.Devices, DeviceCfg{ID: id, KeyEnc: enc, Added: time.Now()})
		return nil
	})
	if err != nil {
		return err
	}
	a.addEvent("info", id, "Key saved ("+how+")")
	if d, ok := a.browser.Device(id); ok {
		a.decodeState(d)
	}
	a.browser.Query()
	return nil
}

func (a *App) apiSetupRead(http.ResponseWriter, *http.Request) (any, error) {
	info, err := readSetupKey()
	if err != nil {
		return map[string]any{"found": false, "detail": "Not connected to the switch's setup hotspot yet."}, nil
	}
	if err := a.saveKey(info.DeviceID, info.APIKey, "read from the switch in setup mode"); err != nil {
		return nil, err
	}
	return map[string]any{"found": true, "deviceid": strings.ToLower(info.DeviceID)}, nil
}

func (a *App) apiSetupManual(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct{ ID, Key string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return nil, errors.New("bad request")
	}
	return nil, a.saveKey(in.ID, in.Key, "entered by hand")
}

func (a *App) apiSwitch(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		On     bool `json:"on"`
		Outlet *int `json:"outlet"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return nil, errors.New("bad request")
	}
	return nil, a.setSwitch(r.PathValue("id"), in.On, in.Outlet)
}

func (a *App) apiDeviceSettings(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Name     *string `json:"name"`
		Hotkey   *string `json:"hotkey"`
		ManualIP *string `json:"manualIp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return nil, errors.New("bad request")
	}
	id := r.PathValue("id")
	hotkeysChanged := false
	err := a.store.Update(func(c *Config) error {
		dc := c.device(id)
		if dc == nil {
			return errors.New("save a key for this switch first")
		}
		if in.Name != nil {
			n := strings.TrimSpace(*in.Name)
			if len([]rune(n)) > 40 {
				return errors.New("names can be up to 40 characters")
			}
			dc.Name = n
		}
		if in.Hotkey != nil {
			if *in.Hotkey != "" {
				if _, ok := hotkeyVK(*in.Hotkey); !ok {
					return errors.New("unknown shortcut")
				}
				for i := range c.Devices {
					if c.Devices[i].ID != id && c.Devices[i].Hotkey == *in.Hotkey {
						c.Devices[i].Hotkey = ""
					}
				}
			}
			dc.Hotkey = *in.Hotkey
			hotkeysChanged = true
		}
		if in.ManualIP != nil {
			ip := strings.TrimSpace(*in.ManualIP)
			if ip != "" && !isLocalIP(net.ParseIP(ip)) {
				return errors.New("enter a local address like 192.168.1.50")
			}
			dc.ManualIP = ip
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if hotkeysChanged {
		a.applyHotkeys()
	}
	return nil, nil
}

func (a *App) apiRemove(_ http.ResponseWriter, r *http.Request) (any, error) {
	id := r.PathValue("id")
	err := a.store.Update(func(c *Config) error {
		out := c.Devices[:0]
		for _, d := range c.Devices {
			if d.ID != id {
				out = append(out, d)
			}
		}
		c.Devices = out
		return nil
	})
	if err == nil {
		a.applyHotkeys()
		a.addEvent("info", id, "Removed from Hestia (key deleted)")
	}
	return nil, err
}

func (a *App) apiSettings(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Background *bool `json:"background"`
		Autostart  *bool `json:"autostart"`
		ResetTest  bool  `json:"resetTest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return nil, errors.New("bad request")
	}
	if in.Autostart != nil {
		if err := setAutostart(*in.Autostart); err != nil {
			return nil, fmt.Errorf("could not change the startup setting: %w", err)
		}
		if *in.Autostart {
			t := true
			in.Background = &t // starting with Windows only makes sense if Hestia stays running
		}
	}
	return nil, a.store.Update(func(c *Config) error {
		if in.Background != nil {
			c.Background = *in.Background
		}
		if in.ResetTest {
			c.TestPassed = false
		}
		return nil
	})
}

func (a *App) apiLog(w http.ResponseWriter, _ *http.Request) (any, error) {
	a.mu.Lock()
	ev := append([]Event{}, a.events...)
	a.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "Hestia %s event log, exported %s\n\n", appVersion, time.Now().Format(time.RFC1123))
	for _, e := range ev {
		fmt.Fprintf(&b, "%s  %-8s %-11s %s\n", e.T.Format("2006-01-02 15:04:05"), e.Kind, e.Device, e.Text)
	}
	for _, v := range a.views() {
		up := "n/a"
		if v.Uptime != nil {
			up = fmt.Sprintf("%.1f%%", *v.Uptime)
		}
		fmt.Fprintf(&b, "\nSummary %s (%s): drops %d, online %s\n", v.ID, v.Name, v.Drops, up)
	}
	return map[string]string{"text": b.String()}, nil
}

// watchdog closes the app when its window has been closed, unless the user
// chose to keep it running for shortcuts.
func (a *App) watchdog() {
	for {
		select {
		case <-a.quit:
			return
		case <-time.After(5 * time.Second):
		}
		a.mu.Lock()
		beat := a.lastBeat
		a.mu.Unlock()
		if beat.IsZero() || a.store.Get().Background {
			continue
		}
		if time.Since(beat) > 25*time.Second {
			log.Println("window closed; exiting")
			a.stop()
			return
		}
	}
}

func fatal(msg string, err error) {
	log.Printf("%s: %v", msg, err)
	showError("Hestia could not start", msg+"\n\n"+err.Error())
	os.Exit(1)
}

func (a *App) installView() map[string]any {
	running, present := installState()
	return map[string]any{"supported": installSupported(), "installed": running, "present": present, "dir": installDir()}
}
