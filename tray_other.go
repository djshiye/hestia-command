//go:build !windows

package main

// Tray is a no-op outside Windows.
type Tray struct{}

func newTray(*App) *Tray      { return &Tray{} }
func (t *Tray) Start()        {}
func (t *Tray) Remove()       {}
func (t *Tray) ClosePopup()   {}
func (t *Tray) WindowClosed() {}
func traySupported() bool     { return false }

var _ = (*App).summary // used by the Windows tray tooltip
