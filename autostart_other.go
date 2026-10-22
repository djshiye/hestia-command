//go:build !windows

package main

func autostartSupported() bool { return false }
func autostartEnabled() bool   { return false }
func setAutostart(bool) error  { return nil }
func refreshAutostartPath()    {}
