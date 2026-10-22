//go:build dev && windows

package main

import "errors"

func init() {
	devTrayClick = func(a *App, event string) error {
		if a.tray.hwnd == 0 {
			return errors.New("no tray icon")
		}
		var lp uintptr
		switch event {
		case "left":
			lp = ninSelect
		case "right":
			lp = wmContextMenu
		case "open":
			a.tray.command(menuOpen)
			return nil
		case "toggle":
			a.tray.command(menuDevice)
			return nil
		default:
			return errors.New("unknown event")
		}
		if !a.tray.v4 {
			lp = map[uintptr]uintptr{ninSelect: wmLButtonUp, wmContextMenu: wmRButtonUp}[lp]
		}
		procPostMessageW.Call(a.tray.hwnd, wmTray, 0, lp)
		return nil
	}
}
