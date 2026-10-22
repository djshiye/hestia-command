package main

// Built-in updater. Once a day (and on request) Hestia asks GitHub for the
// latest release. A release is only installed when all of these hold:
//
//   1. its SHA256SUMS.txt carries a valid Ed25519 signature from the Hestia
//      release key embedded below (so a tampered or impostor release fails,
//      even if GitHub itself were compromised),
//   2. the downloaded program's SHA-256 matches that signed file,
//   3. its version is newer than the one running.
//
// This is the only time Hestia ever talks to the internet, and only to GitHub.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// updatePublicKey is the Hestia release signing key (Ed25519, base64).
// The matching private key lives only in the repository's GitHub secrets.
var updatePublicKey = "qx0mVylXL9xR4r4iiuoVtkdwhuwMoyTX8TltaSS1y+I="

const (
	updateRepo       = "djshiye/hestia-command"
	updateAssetName  = "Hestia-Setup.exe"
	updateSumsName   = "SHA256SUMS.txt"
	updateSigName    = "SHA256SUMS.txt.sig"
	updateMaxSize    = 64 << 20
	updateInterval   = 24 * time.Hour
	signaturePrefix  = "hestia-release-v1\n"
	updateUserAgent  = "Hestia-updater/" + "1"
	updateCheckDelay = 20 * time.Second
)

// updateAPIBase and updateAllowHTTP are only changed by development builds.
var (
	updateAPIBase   = "https://api.github.com"
	updateAllowHTTP = false
	updateHosts     = map[string]bool{
		"api.github.com":                       true,
		"github.com":                           true,
		"objects.githubusercontent.com":        true,
		"release-assets.githubusercontent.com": true,
	}
)

type updateInfo struct {
	Version string
	Tag     string
	ExeURL  string
	SumsURL string
	SigURL  string
	Size    int64
}

type UpdateState struct {
	Enabled     bool      `json:"enabled"`
	Checking    bool      `json:"checking"`
	LastCheck   time.Time `json:"lastCheck"`
	Available   string    `json:"available,omitempty"` // newest version seen, if newer
	Downloading bool      `json:"downloading"`
	Ready       string    `json:"ready,omitempty"` // version downloaded and verified
	Error       string    `json:"error,omitempty"`
	Supported   bool      `json:"supported"`
}

type Updater struct {
	app       *App
	mu        sync.Mutex
	state     UpdateState
	readyPath string
	client    *http.Client
	kick      chan struct{}
}

func newUpdater(a *App) *Updater {
	return &Updater{
		app:  a,
		kick: make(chan struct{}, 1),
		client: &http.Client{
			Timeout:   5 * time.Minute,
			Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 15 * time.Second},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				return checkUpdateURL(req.URL)
			},
		},
	}
}

// checkUpdateURL only lets the updater talk to GitHub over HTTPS.
func checkUpdateURL(u *url.URL) error {
	if u.Scheme != "https" && !(updateAllowHTTP && u.Scheme == "http") {
		return fmt.Errorf("refusing non-HTTPS update address %s", u.Host)
	}
	if !updateHosts[u.Hostname()] {
		return fmt.Errorf("refusing update address outside GitHub: %s", u.Hostname())
	}
	return nil
}

func (u *Updater) State() UpdateState {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.state
	s.Enabled = u.app.store.Get().AutoUpdate()
	s.Supported = updateSupported() && parseVersion(appVersion) != nil
	return s
}

func (u *Updater) set(fn func(s *UpdateState)) {
	u.mu.Lock()
	fn(&u.state)
	u.mu.Unlock()
}

// Kick asks the update loop to check now.
func (u *Updater) Kick() {
	select {
	case u.kick <- struct{}{}:
	default:
	}
}

// run is the update loop: a check shortly after start, then daily, plus any
// manual checks. With automatic updates on, a verified update is applied as
// soon as nobody is looking at the window.
func (u *Updater) run() {
	if !updateSupported() || parseVersion(appVersion) == nil {
		return // development builds never update themselves
	}
	timer := time.NewTimer(updateCheckDelay)
	for {
		select {
		case <-u.app.quit:
			return
		case <-timer.C:
		case <-u.kick:
		}
		timer.Reset(updateInterval)
		if u.State().Enabled || u.manualPending() {
			u.checkAndFetch()
		}
		u.maybeApply()
	}
}

var manualFlag sync.Mutex
var manualRequested bool

func (u *Updater) manualPending() bool {
	manualFlag.Lock()
	defer manualFlag.Unlock()
	r := manualRequested
	manualRequested = false
	return r
}

func (u *Updater) CheckNow() {
	manualFlag.Lock()
	manualRequested = true
	manualFlag.Unlock()
	u.Kick()
}

func (u *Updater) checkAndFetch() {
	u.set(func(s *UpdateState) { s.Checking = true; s.Error = "" })
	info, err := u.check()
	u.set(func(s *UpdateState) {
		s.Checking = false
		s.LastCheck = time.Now()
		if err != nil {
			s.Error = err.Error()
		}
	})
	if err != nil {
		log.Println("update check:", err)
		return
	}
	if info == nil {
		u.set(func(s *UpdateState) { s.Available = "" })
		return
	}
	u.set(func(s *UpdateState) { s.Available = info.Version })
	if u.State().Ready == info.Version {
		return
	}
	u.app.addEvent("info", "", "Hestia "+info.Version+" is available")
	u.set(func(s *UpdateState) { s.Downloading = true })
	path, err := u.fetchVerified(info)
	u.set(func(s *UpdateState) { s.Downloading = false })
	if err != nil {
		log.Println("update download:", err)
		u.app.addEvent("error", "", "Update to "+info.Version+" rejected: "+err.Error())
		u.set(func(s *UpdateState) { s.Error = err.Error() })
		return
	}
	u.mu.Lock()
	u.readyPath = path
	u.state.Ready = info.Version
	u.mu.Unlock()
	u.app.addEvent("info", "", "Hestia "+info.Version+" downloaded and verified")
}

// check returns the latest release if it is newer than this build.
func (u *Updater) check() (*updateInfo, error) {
	req, _ := http.NewRequest("GET", updateAPIBase+"/repos/"+updateRepo+"/releases/latest", nil)
	if err := checkUpdateURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, errors.New("could not reach GitHub")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub answered HTTP %d", resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Draft   bool   `json:"draft"`
		Prerel  bool   `json:"prerelease"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, errors.New("unexpected reply from GitHub")
	}
	if rel.Draft || rel.Prerel {
		return nil, nil
	}
	version := strings.TrimPrefix(rel.TagName, "v")
	newer, err := isNewer(version, appVersion)
	if err != nil {
		return nil, err
	}
	if !newer {
		return nil, nil
	}
	info := &updateInfo{Version: version, Tag: rel.TagName}
	for _, a := range rel.Assets {
		switch a.Name {
		case updateAssetName:
			info.ExeURL, info.Size = a.URL, a.Size
		case updateSumsName:
			info.SumsURL = a.URL
		case updateSigName:
			info.SigURL = a.URL
		}
	}
	if info.ExeURL == "" || info.SumsURL == "" || info.SigURL == "" {
		return nil, fmt.Errorf("release %s is missing its signed files", rel.TagName)
	}
	if info.Size <= 0 || info.Size > updateMaxSize {
		return nil, errors.New("release file has an unexpected size")
	}
	for _, s := range []string{info.ExeURL, info.SumsURL, info.SigURL} {
		pu, err := url.Parse(s)
		if err != nil {
			return nil, errors.New("bad release address")
		}
		if err := checkUpdateURL(pu); err != nil {
			return nil, err
		}
	}
	return info, nil
}

func (u *Updater) getSmall(addr string, limit int64) ([]byte, error) {
	req, _ := http.NewRequest("GET", addr, nil)
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, errors.New("download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download failed (HTTP %d)", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// verifySums checks the signature on the checksum file and returns the
// expected SHA-256 of the program file.
func verifySums(sums, sigB64 []byte) (string, error) {
	pub, err := base64.StdEncoding.DecodeString(updatePublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return "", errors.New("updater key is invalid")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return "", errors.New("release signature is malformed")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), append([]byte(signaturePrefix), sums...), sig) {
		return "", errors.New("release signature does not match the Hestia key")
	}
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == updateAssetName && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", errors.New("signed checksum file does not list " + updateAssetName)
}

// fetchVerified downloads the release and returns the path of the verified
// program file, or an error if anything about it is wrong.
func (u *Updater) fetchVerified(info *updateInfo) (string, error) {
	sums, err := u.getSmall(info.SumsURL, 64<<10)
	if err != nil {
		return "", err
	}
	sig, err := u.getSmall(info.SigURL, 4<<10)
	if err != nil {
		return "", err
	}
	want, err := verifySums(sums, sig)
	if err != nil {
		return "", err
	}
	dir, err := appDir()
	if err != nil {
		return "", err
	}
	updDir := filepath.Join(dir, "updates")
	if err := os.MkdirAll(updDir, 0o700); err != nil {
		return "", err
	}
	if entries, err := os.ReadDir(updDir); err == nil {
		for _, e := range entries {
			_ = os.Remove(filepath.Join(updDir, e.Name()))
		}
	}
	part := filepath.Join(updDir, "Hestia-"+info.Version+".exe.part")
	final := filepath.Join(updDir, "Hestia-"+info.Version+".exe")

	req, _ := http.NewRequest("GET", info.ExeURL, nil)
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := u.client.Do(req)
	if err != nil {
		return "", errors.New("download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download failed (HTTP %d)", resp.StatusCode)
	}
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, updateMaxSize+1))
	f.Close()
	if err != nil {
		os.Remove(part)
		return "", errors.New("download interrupted")
	}
	if n != info.Size || n > updateMaxSize {
		os.Remove(part)
		return "", errors.New("downloaded file has the wrong size")
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(part)
		return "", errors.New("downloaded file does not match its signed checksum")
	}
	if err := os.Rename(part, final); err != nil {
		return "", err
	}
	return final, nil
}

// maybeApply installs a verified update when it is safe to restart: either
// the user asked, or updates are automatic and the window is not in use.
func (u *Updater) maybeApply() {
	u.mu.Lock()
	path, ready := u.readyPath, u.state.Ready
	u.mu.Unlock()
	if ready == "" || path == "" {
		return
	}
	if !u.app.store.Get().AutoUpdate() {
		return
	}
	u.app.mu.Lock()
	beat := u.app.lastBeat
	u.app.mu.Unlock()
	if !beat.IsZero() && time.Since(beat) < 60*time.Second {
		return // someone has the window open; the banner offers a restart instead
	}
	u.Apply()
}

// Apply swaps in the verified update and restarts Hestia.
func (u *Updater) Apply() error {
	u.mu.Lock()
	path, ready := u.readyPath, u.state.Ready
	u.mu.Unlock()
	if ready == "" || path == "" {
		return errors.New("no verified update is ready")
	}
	u.app.addEvent("info", "", "Restarting to install Hestia "+ready)
	u.app.mu.Lock()
	beat := u.app.lastBeat
	u.app.mu.Unlock()
	withWindow := !beat.IsZero() && time.Since(beat) < 60*time.Second
	next, err := stageUpdate(path)
	if err != nil {
		u.app.addEvent("error", "", "Update failed: "+err.Error())
		return err
	}
	u.app.mu.Lock()
	u.app.relaunch = next
	if !withWindow {
		u.app.relaunchArgs = []string{"-minimized"}
	}
	u.app.mu.Unlock()
	go func() { time.Sleep(400 * time.Millisecond); u.app.stop() }()
	return nil
}

// ---- versions ----

func parseVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return out
}

func isNewer(candidate, current string) (bool, error) {
	a, b := parseVersion(candidate), parseVersion(current)
	if a == nil {
		return false, fmt.Errorf("release has an unexpected version %q", candidate)
	}
	if b == nil {
		return false, nil
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i], nil
		}
	}
	return false, nil
}

func parseURL(s string) (*url.URL, error) { return url.Parse(s) }
