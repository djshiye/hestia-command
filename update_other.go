//go:build !windows

package main

import "errors"

func updateSupported() bool { return false }
func stageUpdate(string) (string, error) {
	return "", errors.New("updates are only supported on Windows")
}
func cleanupOldUpdate() {}
