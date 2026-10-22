//go:build windows

package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// openWindow shows the app in a clean Edge app window (no tabs or address
// bar), falling back to the default browser.
func openWindow(url string) {
	var candidates []string
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
		if base := os.Getenv(env); base != "" {
			candidates = append(candidates, filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
		}
	}
	dir, _ := appDir()
	for _, exe := range candidates {
		if _, err := os.Stat(exe); err != nil {
			continue
		}
		cmd := exec.Command(exe, "--app="+url, "--user-data-dir="+filepath.Join(dir, "window"),
			"--no-first-run", "--no-default-browser-check", "--window-size=1120,820")
		if err := cmd.Start(); err == nil {
			go cmd.Wait()
			return
		}
	}
	log.Println("Edge not found; using the default browser")
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
}

func unsafePtr(p *uint16) unsafe.Pointer { return unsafe.Pointer(p) }

func showInfo(title, text string) { messageBox(title, text, 0x40) }

func showError(title, text string) { messageBox(title, text, 0x10) }

func messageBox(title, text string, flags uintptr) {
	user := windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	user.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
}
