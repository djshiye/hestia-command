//go:build !windows

package main

type Tray struct{}

func newTray(*App) *Tray      { return &Tray{} }
func (t *Tray) Start()        {}
func (t *Tray) Remove()       {}
func (t *Tray) ClosePopup()   {}
func (t *Tray) WindowClosed() {}
func traySupported() bool     { return false }
