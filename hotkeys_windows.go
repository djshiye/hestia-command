//go:build windows

package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procPostThreadMsgW   = user32.NewProc("PostThreadMessageW")
	procPeekMessageW     = user32.NewProc("PeekMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
)

const (
	wmHotkey    = 0x0312
	wmApply     = 0x8001 // WM_APP + 1
	wmRun       = 0x8002 // WM_APP + 2: run the next queued function on this thread
	modAlt      = 0x0001
	modControl  = 0x0002
	modNoRepeat = 0x4000
)

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
	private uint32
}

type Hotkeys struct {
	onPress  func(deviceID string)
	threadID uint32
	mu       sync.Mutex
	pending  map[string]string // device ID -> hotkey
	result   chan map[string]string
	active   map[int]string // hotkey id -> device ID
	jobs     chan func()
}

func NewHotkeys(onPress func(string)) *Hotkeys {
	h := &Hotkeys{onPress: onPress, result: make(chan map[string]string, 1), active: map[int]string{}, jobs: make(chan func(), 16)}
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		h.threadID = windows.GetCurrentThreadId()
		var m winMsg
		// Make sure this thread has a message queue before anyone posts to it.
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 0)
		close(ready)
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			if m.hwnd != 0 {
				procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
				procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
				continue
			}
			switch m.message {
			case wmHotkey:
				if id, ok := h.active[int(m.wParam)]; ok {
					go h.onPress(id)
				}
			case wmApply:
				h.apply()
			case wmRun:
				select {
				case fn := <-h.jobs:
					fn()
				default:
				}
			}
		}
	}()
	<-ready
	return h
}

// apply runs on the hotkey thread: RegisterHotKey binds to the calling thread.
func (h *Hotkeys) apply() {
	h.mu.Lock()
	want := h.pending
	h.mu.Unlock()
	for id := range h.active {
		procUnregisterHotKey.Call(0, uintptr(id))
	}
	h.active = map[int]string{}
	errs := map[string]string{}
	next := 1
	for dev, key := range want {
		vk, ok := hotkeyVK(key)
		if !ok {
			continue
		}
		r, _, err := procRegisterHotKey.Call(0, uintptr(next), modControl|modAlt|modNoRepeat, uintptr(vk))
		if r == 0 {
			errs[dev] = fmt.Sprintf("%s is already used by another program (%v)", key, err)
			continue
		}
		h.active[next] = dev
		next++
	}
	h.result <- errs
}

// Set replaces all shortcuts. It returns, per device, any shortcut that could not be registered.
func (h *Hotkeys) Set(bindings map[string]string) map[string]string {
	h.mu.Lock()
	h.pending = bindings
	h.mu.Unlock()
	if r, _, _ := procPostThreadMsgW.Call(uintptr(h.threadID), wmApply, 0, 0); r == 0 {
		return map[string]string{"": "keyboard shortcuts are unavailable"}
	}
	select {
	case errs := <-h.result:
		return errs
	case <-time.After(3 * time.Second):
		return map[string]string{"": "keyboard shortcuts did not respond"}
	}
}

// Run executes fn on the message-loop thread, which windows and tray icons
// must be created on. It waits for fn to finish.
func (h *Hotkeys) Run(fn func()) {
	done := make(chan struct{})
	h.jobs <- func() { fn(); close(done) }
	if r, _, _ := procPostThreadMsgW.Call(uintptr(h.threadID), wmRun, 0, 0); r == 0 {
		<-h.jobs
		return
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func (h *Hotkeys) Supported() bool { return true }
