//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func autostartSupported() bool { return true }

func autostartCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return `"` + exe + `" -minimized`, nil
}

// setAutostartFor points the startup entry at a specific copy of Hestia.
func setAutostartFor(exe string, on bool) error {
	if !on {
		return setAutostart(false)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("Hestia", `"`+exe+`" -minimized`)
}

// autostartEnabled reports whether Hestia starts when this user signs in.
func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("Hestia")
	return err == nil
}

// setAutostart adds or removes Hestia from this user's sign-in programs.
// It only touches the current user's settings, so no admin rights are needed.
func setAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue("Hestia"); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	cmd, err := autostartCommand()
	if err != nil {
		return err
	}
	return k.SetStringValue("Hestia", cmd)
}

// refreshAutostartPath keeps the startup entry pointing at this copy of
// Hestia if the program file was moved.
func refreshAutostartPath() {
	if autostartEnabled() {
		_ = setAutostart(true)
	}
}
