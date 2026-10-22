//go:build !windows

package main

type Hotkeys struct{}

func NewHotkeys(func(string)) *Hotkeys                     { return &Hotkeys{} }
func (h *Hotkeys) Set(map[string]string) map[string]string { return nil }
func (h *Hotkeys) Supported() bool                         { return false }
