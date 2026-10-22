//go:build windows

package main

// Per-user, one-click install: copies Hestia into
// %LocalAppData%\Programs\Hestia, adds Start menu and desktop shortcuts and
// an entry in Windows' installed apps list. No admin rights are needed.

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Hestia`

func installSupported() bool { return true }

func installDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserCacheDir()
	}
	return filepath.Join(base, "Programs", "Hestia")
}

func installedExe() string { return filepath.Join(installDir(), "Hestia.exe") }

func samePath(a, b string) bool {
	ca, _ := filepath.Abs(a)
	cb, _ := filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(ca), filepath.Clean(cb))
}

// installState reports whether this copy is the installed one, and whether an
// installed copy exists at all.
func installState() (runningInstalled, installedPresent bool) {
	exe, _ := os.Executable()
	_, err := os.Stat(installedExe())
	return samePath(exe, installedExe()), err == nil
}

func shortcutPaths() []string {
	var out []string
	if p, err := windows.KnownFolderPath(windows.FOLDERID_Programs, 0); err == nil {
		out = append(out, filepath.Join(p, "Hestia.lnk"))
	}
	if p, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0); err == nil {
		out = append(out, filepath.Join(p, "Hestia.lnk"))
	}
	return out
}

func makeShortcut(lnk, target string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		var oe *ole.OleError
		if !errors.As(err, &oe) || (oe.Code() != 1 && oe.Code() != 0x80010106) { // S_FALSE / already initialised
			return err
		}
	}
	defer ole.CoUninitialize()
	obj, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return err
	}
	defer obj.Release()
	shell, err := obj.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer shell.Release()
	res, err := oleutil.CallMethod(shell, "CreateShortcut", lnk)
	if err != nil {
		return err
	}
	sc := res.ToIDispatch()
	defer sc.Release()
	for k, v := range map[string]string{
		"TargetPath":       target,
		"WorkingDirectory": filepath.Dir(target),
		"Description":      "Hestia - local control for Sonoff switches",
		"IconLocation":     target + ",0",
	} {
		if _, err := oleutil.PutProperty(sc, k, v); err != nil {
			return err
		}
	}
	_, err = oleutil.CallMethod(sc, "Save")
	return err
}

// stopOtherInstance asks an already running Hestia to quit, so its file can
// be replaced. ownPort is this process's own port, which is never stopped.
func stopOtherInstance(ownPort int) {
	dir, err := appDir()
	if err != nil {
		return
	}
	inf, ok := existingInstance(filepath.Join(dir, "instance.json"))
	if !ok || inf.Port == ownPort {
		return
	}
	req, _ := http.NewRequest("POST", "http://127.0.0.1:"+strconv.Itoa(inf.Port)+"/api/quit", nil)
	req.Header.Set("X-Hestia-Token", inf.Token)
	c := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	if resp, err := c.Do(req); err == nil {
		resp.Body.Close()
	}
	for i := 0; i < 20; i++ {
		if other, alive := existingInstance(filepath.Join(dir, "instance.json")); !alive || other.Port == ownPort {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	var lastErr error
	for i := 0; i < 20; i++ { // the old copy may take a moment to exit
		if lastErr = os.Rename(tmp, dst); lastErr == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	os.Remove(tmp)
	return lastErr
}

// installSelf copies this program into place and registers it. The caller is
// a copy running from somewhere else, such as the Downloads folder.
func installSelf(ownPort int) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	target := installedExe()
	if samePath(self, target) {
		return target, nil
	}
	stopOtherInstance(ownPort)
	if err := os.MkdirAll(installDir(), 0o755); err != nil {
		return "", err
	}
	if err := copyFile(self, target); err != nil {
		return "", fmt.Errorf("could not copy Hestia into place: %w", err)
	}
	for _, lnk := range shortcutPaths() {
		if err := makeShortcut(lnk, target); err != nil {
			log.Println("shortcut:", lnk, err)
		}
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return "", fmt.Errorf("could not register Hestia with Windows: %w", err)
	}
	defer k.Close()
	size := uint32(0)
	if st, err := os.Stat(target); err == nil {
		size = uint32(st.Size() / 1024)
	}
	_ = k.SetStringValue("DisplayName", "Hestia")
	_ = k.SetStringValue("DisplayVersion", appVersion)
	_ = k.SetStringValue("Publisher", "Hestia")
	_ = k.SetStringValue("DisplayIcon", target+",0")
	_ = k.SetStringValue("InstallLocation", installDir())
	_ = k.SetStringValue("UninstallString", `"`+target+`" -uninstall`)
	_ = k.SetStringValue("QuietUninstallString", `"`+target+`" -uninstall -quiet`)
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	_ = k.SetDWordValue("EstimatedSize", size)
	if autostartEnabled() {
		_ = setAutostartFor(target, true)
	}
	log.Println("installed to", target)
	return target, nil
}

func launchDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
	return cmd.Start()
}

func ask(title, text string) bool {
	user := windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	r, _, _ := user.Call(0, uintptr(unsafePtr(m)), uintptr(unsafePtr(t)), 0x4|0x20) // MB_YESNO | MB_ICONQUESTION
	return r == 6                                                                   // IDYES
}

// uninstall removes Hestia. quiet skips the questions and keeps saved keys.
func uninstall(quiet bool) {
	if !quiet && !ask("Uninstall Hestia", "Remove Hestia from this PC?") {
		return
	}
	removeData := !quiet && ask("Uninstall Hestia", "Also delete your saved switches and their keys?\n\nChoose No to keep them for a later reinstall.")
	stopOtherInstance(0)
	_ = setAutostart(false)
	for _, lnk := range shortcutPaths() {
		_ = os.Remove(lnk)
	}
	_ = registry.DeleteKey(registry.CURRENT_USER, uninstallKey)
	if removeData {
		if dir, err := appDir(); err == nil {
			_ = os.RemoveAll(dir)
		}
	}
	// A running program can't delete itself, so a temporary copy removes the
	// folder once this process has exited.
	dir := installDir()
	if self, err := os.Executable(); err == nil && samePath(filepath.Dir(self), dir) {
		helper := filepath.Join(os.TempDir(), "hestia-uninstall-"+strconv.FormatInt(time.Now().UnixNano(), 36)+".exe")
		if err := copyFile(self, helper); err == nil {
			_ = launchDetached(helper, "-cleanup", dir)
		}
	}
	if !quiet {
		showInfo("Hestia", "Hestia has been removed.")
	}
}

// cleanupInstallDir runs from a temporary copy after uninstalling and removes
// the install folder once the installed program has exited.
func cleanupInstallDir(dir string) {
	if !samePath(dir, installDir()) {
		return // only ever remove Hestia's own folder
	}
	for i := 0; i < 30; i++ {
		if err := os.RemoveAll(dir); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
