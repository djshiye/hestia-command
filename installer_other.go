//go:build !windows

package main

func installSupported() bool                 { return false }
func installState() (bool, bool)             { return false, false }
func installDir() string                     { return "" }
func installSelf(int) (string, error)        { return "", nil }
func launchDetached(string, ...string) error { return nil }
func uninstall(bool)                         {}
func cleanupInstallDir(string)               {}
