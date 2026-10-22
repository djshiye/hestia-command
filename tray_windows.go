//go:build windows

package main

import (
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32                   = windows.NewLazySystemDLL("shell32.dll")
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procShellNotifyIconW      = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW      = user32.NewProc("RegisterClassExW")
	procCreateWindowExW       = user32.NewProc("CreateWindowExW")
	procDefWindowProcW        = user32.NewProc("DefWindowProcW")
	procLoadImageW            = user32.NewProc("LoadImageW")
	procCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	procAppendMenuW           = user32.NewProc("AppendMenuW")
	procSetMenuDefaultItem    = user32.NewProc("SetMenuDefaultItem")
	procTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	procDestroyMenu           = user32.NewProc("DestroyMenu")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procPostMessageW          = user32.NewProc("PostMessageW")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	procGetDpiForSystem       = user32.NewProc("GetDpiForSystem")
	procRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
)

const (
	wmTray        = 0x8003 // WM_APP + 3: tray icon callback
	wmCommand     = 0x0111
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmContextMenu = 0x007B
	ninSelect     = 0x0400
	ninKeySelect  = 0x0401

	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4
	nifMessage    = 0x01
	nifIcon       = 0x02
	nifTip        = 0x04
	nifInfo       = 0x10
	nifShowTip    = 0x80
	niifInfo      = 0x01
	trayVersion4  = 4

	menuOpen   = 1
	menuQuit   = 2
	menuDevice = 100 // + index of the saved switch

	popupWidth  = 340
	popupHeight = 440
)

type notifyIconData struct {
	Size        uint32
	Wnd         uintptr
	ID          uint32
	Flags       uint32
	CallbackMsg uint32
	Icon        uintptr
	Tip         [128]uint16
	State       uint32
	StateMask   uint32
	Info        [256]uint16
	Version     uint32
	InfoTitle   [64]uint16
	InfoFlags   uint32
	GUID        windows.GUID
	BalloonIcon uintptr
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type rect struct{ Left, Top, Right, Bottom int32 }

// Tray is the flame icon next to the clock. A left click opens a small
// control popup, a right click shows a menu, and the tooltip shows each
// switch's state.
type Tray struct {
	app  *App
	hwnd uintptr
	icon uintptr
	v4   bool // Windows accepted NOTIFYICON_VERSION_4 (one event per click)

	mu        sync.Mutex
	popup     *exec.Cmd
	lastTip   string
	lastClick time.Time
	menuIDs   map[int]string // menu command -> device ID
	taskbarUp uint32         // "TaskbarCreated" message: re-add the icon after Explorer restarts
}

var trayInstance *Tray // the window procedure has no user pointer

func newTray(a *App) *Tray { return &Tray{app: a} }

func traySupported() bool { return true }

// Start creates the icon. It must run after the web server is listening.
func (t *Tray) Start() {
	trayInstance = t
	t.app.hotkeys.Run(t.create)
	if t.hwnd == 0 {
		return
	}
	go t.tipLoop()
}

func (t *Tray) create() {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := windows.UTF16PtrFromString("HestiaTray")
	wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: syscall.NewCallback(trayWndProc), Instance: hInst, ClassName: className}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		log.Println("tray: window class:", err)
		return
	}
	title, _ := windows.UTF16PtrFromString("Hestia")
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		log.Println("tray: window:", err)
		return
	}
	t.hwnd = hwnd
	msgName, _ := windows.UTF16PtrFromString("TaskbarCreated")
	m, _, _ := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(msgName)))
	t.taskbarUp = uint32(m)
	t.loadIcon(hInst)
	t.add()
}

func (t *Tray) loadIcon(hInst uintptr) {
	const imageIcon, lrShared, lrDefaultSize = 1, 0x8000, 0x40
	name, _ := windows.UTF16PtrFromString("APP") // the icon compiled in by go-winres
	h, _, _ := procLoadImageW.Call(hInst, uintptr(unsafe.Pointer(name)), imageIcon, 16, 16, lrShared)
	if h == 0 { // development build without resources
		h, _, _ = procLoadImageW.Call(0, 32512 /* IDI_APPLICATION */, imageIcon, 0, 0, lrShared|lrDefaultSize)
	}
	t.icon = h
}

func (t *Tray) data(flags uint32) *notifyIconData {
	d := &notifyIconData{Wnd: t.hwnd, ID: 1, Flags: flags, CallbackMsg: wmTray, Icon: t.icon}
	d.Size = uint32(unsafe.Sizeof(*d))
	return d
}

func (t *Tray) add() {
	d := t.data(nifMessage | nifIcon | nifTip | nifShowTip)
	copyUTF16(d.Tip[:], "Hestia")
	if r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(d))); r == 0 {
		log.Println("tray: icon not added:", err)
		return
	}
	d.Version = trayVersion4
	r, _, _ := procShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(d)))
	t.v4 = r != 0
	t.mu.Lock()
	t.lastTip = ""
	t.mu.Unlock()
	log.Println("tray icon ready")
}

// Remove takes the icon away and closes the popup. Safe from any goroutine.
func (t *Tray) Remove() {
	t.ClosePopup()
	if t.hwnd == 0 {
		return
	}
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(t.data(0))))
}

// tipLoop keeps the tooltip showing each switch's current state.
func (t *Tray) tipLoop() {
	for {
		select {
		case <-t.app.quit:
			return
		case <-time.After(3 * time.Second):
		}
		tip := "Hestia\n" + t.app.summary()
		t.mu.Lock()
		same := tip == t.lastTip
		t.lastTip = tip
		t.mu.Unlock()
		if same {
			continue
		}
		d := t.data(nifTip | nifShowTip)
		copyUTF16(d.Tip[:], tip)
		procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(d)))
	}
}

// WindowClosed shows, once, that Hestia is still running in the tray.
func (t *Tray) WindowClosed() {
	if t.hwnd == 0 || t.app.store.Get().TrayHintShown {
		return
	}
	_ = t.app.store.Update(func(c *Config) error { c.TrayHintShown = true; return nil })
	d := t.data(nifInfo)
	d.InfoFlags = niifInfo
	copyUTF16(d.InfoTitle[:], "Hestia is still running")
	copyUTF16(d.Info[:], "Click the flame icon by the clock for quick control. Right-click it to quit.")
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(d)))
}

func trayWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	t := trayInstance
	if t == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return r
	}
	switch {
	case msg == wmTray:
		event := lParam
		if t.v4 {
			event = lParam & 0xffff
		}
		switch event {
		case ninSelect, ninKeySelect:
			t.clicked()
		case wmLButtonUp:
			if !t.v4 {
				t.clicked()
			}
		case wmContextMenu, wmRButtonUp:
			if t.v4 == (event == wmContextMenu) {
				t.showMenu()
			}
		}
		return 0
	case msg == wmCommand:
		t.command(int(wParam & 0xffff))
		return 0
	case t.taskbarUp != 0 && uint32(msg) == t.taskbarUp:
		t.add()
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// clicked toggles the control popup, ignoring the double events some
// Windows versions send for one click.
func (t *Tray) clicked() {
	t.mu.Lock()
	recent := time.Since(t.lastClick) < 400*time.Millisecond
	t.lastClick = time.Now()
	t.mu.Unlock()
	if recent {
		return
	}
	if t.popupRunning() {
		t.ClosePopup()
		return
	}
	go t.openPopup()
}

func (t *Tray) popupRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.popup != nil && t.popup.ProcessState == nil
}

// openPopup shows the compact control page in its own small Edge window at
// the bottom-right corner, where the tray is.
func (t *Tray) openPopup() {
	exe := edgePath()
	if exe == "" {
		t.app.openMainWindow()
		return
	}
	dir, _ := appDir()
	url := "http://127.0.0.1:" + strconv.Itoa(t.app.port) + "/popup.html#t=" + t.app.token
	x, y := popupPosition()
	cmd := exec.Command(exe, "--app="+url, "--user-data-dir="+filepath.Join(dir, "popup"),
		"--no-first-run", "--no-default-browser-check", "--disable-features=msEdgeSplitScreen",
		"--window-size="+strconv.Itoa(popupWidth)+","+strconv.Itoa(popupHeight),
		"--window-position="+strconv.Itoa(x)+","+strconv.Itoa(y))
	if err := cmd.Start(); err != nil {
		log.Println("tray: popup:", err)
		t.app.openMainWindow()
		return
	}
	t.mu.Lock()
	t.popup = cmd
	t.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		t.mu.Lock()
		if t.popup == cmd {
			t.popup = nil
		}
		t.mu.Unlock()
	}()
}

// ClosePopup closes the control popup if it is open. Safe from any goroutine.
func (t *Tray) ClosePopup() {
	t.mu.Lock()
	cmd := t.popup
	t.popup = nil
	t.mu.Unlock()
	if cmd != nil && cmd.Process != nil && cmd.ProcessState == nil {
		_ = cmd.Process.Kill()
	}
}

// popupPosition puts the popup just above the taskbar at the screen's
// bottom-right corner, in Edge's device-independent pixels.
func popupPosition() (int, int) {
	const spiGetWorkArea = 0x30
	var r rect
	if ok, _, _ := procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0); ok == 0 {
		r = rect{Right: 1920, Bottom: 1040}
	}
	scale := 1.0
	if dpi, _, _ := procGetDpiForSystem.Call(); dpi >= 96 {
		scale = float64(dpi) / 96
	}
	x := int(float64(r.Right)/scale) - popupWidth - 12
	y := int(float64(r.Bottom)/scale) - popupHeight - 12
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

func (t *Tray) showMenu() {
	const mfString, mfSeparator, mfGrayed = 0, 0x800, 0x1
	const tpmReturnCmd, tpmRightButton, tpmBottomAlign, tpmRightAlign = 0x100, 0x2, 0x20, 0x8
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	appendMenu := func(id int, flags uintptr, text string) {
		s, _ := windows.UTF16PtrFromString(text)
		procAppendMenuW.Call(menu, flags, uintptr(id), uintptr(unsafe.Pointer(s)))
	}
	appendMenu(menuOpen, mfString, "Open Hestia")
	procSetMenuDefaultItem.Call(menu, menuOpen, 0)
	ids := map[int]string{}
	n := 0
	for _, d := range t.app.views() {
		if !d.Saved || !d.HasKey {
			continue
		}
		if n == 0 {
			appendMenu(0, mfSeparator, "")
		}
		id := menuDevice + n
		ids[id] = d.ID
		n++
		flags := uintptr(mfString)
		label := "Turn " + d.Name + " on"
		switch {
		case !d.Online:
			flags |= mfGrayed
			label = d.Name + " (offline)"
		case d.Switch == "on":
			label = "Turn " + d.Name + " off"
		}
		appendMenu(id, flags, label)
	}
	appendMenu(0, mfSeparator, "")
	appendMenu(menuQuit, mfString, "Quit Hestia")
	t.mu.Lock()
	t.menuIDs = ids
	t.mu.Unlock()

	var pt struct{ X, Y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(t.hwnd) // so the menu closes when the user clicks elsewhere
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmReturnCmd|tpmRightButton|tpmBottomAlign|tpmRightAlign,
		uintptr(pt.X), uintptr(pt.Y), 0, t.hwnd, 0)
	procPostMessageW.Call(t.hwnd, 0 /* WM_NULL */, 0, 0)
	if cmd != 0 {
		t.command(int(cmd))
	}
}

func (t *Tray) command(id int) {
	switch {
	case id == menuOpen:
		go t.app.openMainWindow()
	case id == menuQuit:
		go t.app.stop()
	case id >= menuDevice:
		t.mu.Lock()
		dev := t.menuIDs[id]
		t.mu.Unlock()
		if dev != "" {
			go t.app.onHotkey(dev)
		}
	}
}

func copyUTF16(dst []uint16, s string) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return
	}
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
}
